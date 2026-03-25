package handlers

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"easydo-server/internal/models"
)

var (
	maskedURLCredentialsPattern   = regexp.MustCompile(`(?i)(https?://)([^/@\s:]+):([^/@\s]+)@`)
	maskedAuthorizationPattern    = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*(?:basic|bearer)\s+)([^\s"']+)`)
	maskedCredentialKeyPattern    = regexp.MustCompile(`(?i)((?:access_token|token|password|secret|client_secret|api_key|private_key|cert_pem|key_pem|tls_client_key|tls_client_cert|tls_ca_cert)\s*[:=]\s*)([^\s,"';]+)`)
	maskedFlagValuePattern        = regexp.MustCompile(`(?i)(--(?:password|token|access-token|secret|client-secret|api-key|authorization)(?:=|\s+))([^\s"']+)`)
	maskedCredentialEnvVarPattern = regexp.MustCompile(`(?i)(EASYDO_CRED_[A-Z0-9_]*(?:TOKEN|PASSWORD|SECRET|KEY|CERT|AUTHORIZATION)[A-Z0-9_]*=)([^\s]+)`)
)

func sanitizeTaskLogText(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}
	masked := maskedURLCredentialsPattern.ReplaceAllString(trimmed, `${1}***:***@`)
	masked = maskedAuthorizationPattern.ReplaceAllString(masked, `${1}***`)
	masked = maskedFlagValuePattern.ReplaceAllString(masked, `${1}***`)
	masked = maskedCredentialEnvVarPattern.ReplaceAllString(masked, `${1}***`)
	masked = maskedCredentialKeyPattern.ReplaceAllString(masked, `${1}***`)
	return masked
}

func sanitizeTaskLogPreview(input string, maxLen int) string {
	masked := sanitizeTaskLogText(input)
	if masked == "" {
		return ""
	}
	if maxLen > 0 && len(masked) > maxLen {
		return masked[:maxLen] + "...<truncated>"
	}
	return masked
}

type taskProcessLogger struct {
	task    models.AgentTask
	attempt int
	seq     int64
	h       *WebSocketHandler
}

func newTaskProcessLogger(task models.AgentTask) *taskProcessLogger {
	return &taskProcessLogger{
		task:    task,
		attempt: task.RetryCount + 1,
		seq:     1,
		h:       SharedWebSocketHandler(),
	}
}

func (l *taskProcessLogger) emit(level, stream, message string) {
	if l == nil || strings.TrimSpace(message) == "" {
		return
	}
	_, _ = appendTaskLogChunk(l.h, l.task, taskLogChunkPayloadV2{
		TaskID:    l.task.ID,
		Attempt:   l.attempt,
		Seq:       l.seq,
		Level:     level,
		Stream:    stream,
		Chunk:     message,
		Timestamp: time.Now().Unix(),
	}, l.task.AgentID, "")
	l.seq++
}

func (l *taskProcessLogger) Step(message string) {
	l.emit("info", "server", "[easydo][step] "+sanitizeTaskLogPreview(message, 800))
}

func (l *taskProcessLogger) Info(message string) {
	l.emit("info", "server", "[easydo][info] "+sanitizeTaskLogPreview(message, 1200))
}

func (l *taskProcessLogger) Command(message string) {
	l.emit("info", "server", "[easydo][cmd] "+sanitizeTaskLogPreview(message, 1600))
}

func (l *taskProcessLogger) Warn(message string) {
	l.emit("warn", "server", "[easydo][warn] "+sanitizeTaskLogPreview(message, 1200))
}

func (l *taskProcessLogger) Error(message string) {
	l.emit("error", "server", "[easydo][error] "+sanitizeTaskLogPreview(message, 1200))
}

func appendTaskLogChunk(h *WebSocketHandler, task models.AgentTask, logChunk taskLogChunkPayloadV2, agentID uint64, agentSessionID string) (bool, error) {
	if task.ID == 0 || strings.TrimSpace(logChunk.Chunk) == "" {
		return false, fmt.Errorf("invalid task log chunk")
	}
	if logChunk.Attempt <= 0 {
		logChunk.Attempt = task.RetryCount + 1
	}
	if logChunk.Seq <= 0 {
		logChunk.Seq = time.Now().UnixNano()
	}
	if logChunk.Stream == "" {
		logChunk.Stream = "stdout"
	}
	if logChunk.Level == "" {
		if logChunk.Stream == "stderr" {
			logChunk.Level = "error"
		} else {
			logChunk.Level = "info"
		}
	}
	if logChunk.Timestamp == 0 {
		logChunk.Timestamp = time.Now().Unix()
	}
	normalizedTimestamp := normalizeUnixTimestamp(logChunk.Timestamp)

	if err := agentFileLogs.Append(fileLogEntry{
		AgentID:       agentID,
		TaskID:        logChunk.TaskID,
		PipelineRunID: task.PipelineRunID,
		Level:         logChunk.Level,
		Message:       logChunk.Chunk,
		Source:        logChunk.Stream,
		Timestamp:     normalizedTimestamp,
		LineNumber:    int(logChunk.Seq),
		Attempt:       logChunk.Attempt,
		Seq:           logChunk.Seq,
	}); err != nil {
		return false, err
	}

	if h != nil {
		h.broadcastToFrontend(task.PipelineRunID, "task_log", map[string]interface{}{
			"task_id":     logChunk.TaskID,
			"run_id":      task.PipelineRunID,
			"level":       logChunk.Level,
			"message":     logChunk.Chunk,
			"source":      logChunk.Stream,
			"line_number": logChunk.Seq,
			"timestamp":   normalizedTimestamp,
		})
	}

	return true, nil
}
