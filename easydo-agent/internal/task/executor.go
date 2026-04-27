package task

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"easydo-agent/internal/system"
	"github.com/sirupsen/logrus"
)

// TaskParams represents task execution parameters
type TaskParams struct {
	TaskID        uint64
	PipelineRunID uint64
	NodeID        string
	TaskType      string
	Name          string
	Params        map[string]interface{}
	Script        string
	WorkDir       string
	EnvVars       map[string]string
	Timeout       int
}

func ParseStructuredParamsJSON(raw string) map[string]interface{} {
	if raw == "" {
		return nil
	}

	var params map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return nil
	}
	if len(params) == 0 {
		return nil
	}
	return params
}

// LogCallback is called for each log line
// taskID is included to support parallel task execution with shared executor
type LogCallback func(taskID uint64, level, message, source string, lineNumber int)

// Result represents the result of task execution
type Result struct {
	ExitCode int           `json:"exit_code"`
	Stdout   string        `json:"stdout"`
	Stderr   string        `json:"stderr"`
	Error    string        `json:"error"`
	Duration time.Duration `json:"duration"`
}

// Executor executes tasks
type Executor struct {
	log                  *logrus.Logger
	workspace            *WorkspaceManager
	runtime              system.RuntimeCapabilities
	logCallback          LogCallback
	embeddedBuildkitEnv  map[string]string
	mu                   sync.RWMutex
}

type outputCaptureWriter struct {
	executor *Executor
	buf      *bytes.Buffer
	pending  bytes.Buffer
	level    string
	source   string
	lineNum  int
	taskID   uint64
	callback LogCallback
}

func (w *outputCaptureWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if _, err := w.buf.Write(p); err != nil {
		return 0, err
	}
	if _, err := w.pending.Write(p); err != nil {
		return 0, err
	}
	w.emitPendingLines(false)
	return len(p), nil
}

func (w *outputCaptureWriter) Flush() {
	w.emitPendingLines(true)
}

func (w *outputCaptureWriter) emitPendingLines(flushRemainder bool) {
	for {
		data := w.pending.Bytes()
		newlineIndex := bytes.IndexByte(data, '\n')
		if newlineIndex < 0 {
			if flushRemainder && len(data) > 0 {
				w.emitLine(data)
				w.pending.Reset()
			}
			return
		}
		w.emitLine(data[:newlineIndex])
		w.pending.Next(newlineIndex + 1)
	}
}

func (w *outputCaptureWriter) emitLine(raw []byte) {
	line := raw
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	w.lineNum++
	if w.callback != nil {
		w.callback(w.taskID, w.level, string(line), w.source, w.lineNum)
	}
}

func stringifyEnvValue(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool, float64, float32, int, int32, int64, uint, uint32, uint64:
		return fmt.Sprint(v)
	default:
		if data, err := json.Marshal(v); err == nil {
			return string(data)
		}
		return fmt.Sprint(v)
	}
}

func ParseEnvVarsJSON(raw string) map[string]string {
	if raw == "" {
		return nil
	}

	var env map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return nil
	}
	if len(env) == 0 {
		return nil
	}

	envVars := make(map[string]string, len(env))
	for key, value := range env {
		envVars[key] = stringifyEnvValue(value)
	}
	return envVars
}

// NewExecutor creates a new task executor
func NewExecutor(log *logrus.Logger, basePath string, runtimeCaps system.RuntimeCapabilities) *Executor {
	return &Executor{
		log:       log,
		workspace: NewWorkspaceManager(basePath, log),
		runtime:   runtimeCaps,
	}
}

func (e *Executor) WorkspaceManager() *WorkspaceManager {
	if e == nil {
		return nil
	}
	return e.workspace
}

// SetLogCallback sets the callback for log reporting
func (e *Executor) SetLogCallback(callback LogCallback) {
	e.mu.Lock()
	e.logCallback = callback
	e.mu.Unlock()
}

// GetLogCallback returns the current log callback
func (e *Executor) GetLogCallback() LogCallback {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.logCallback
}

func (e *Executor) SetEmbeddedBuildkitEnv(env map[string]string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(env) == 0 {
		e.embeddedBuildkitEnv = nil
		return
	}
	copied := make(map[string]string, len(env))
	for k, v := range env {
		copied[k] = v
	}
	e.embeddedBuildkitEnv = copied
}

func (e *Executor) EmbeddedBuildkitEnv() map[string]string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if len(e.embeddedBuildkitEnv) == 0 {
		return nil
	}
	copied := make(map[string]string, len(e.embeddedBuildkitEnv))
	for k, v := range e.embeddedBuildkitEnv {
		copied[k] = v
	}
	return copied
}

// Execute executes a task
func (e *Executor) Execute(ctx context.Context, params TaskParams) *Result {
	startTime := time.Now()

	e.log.Infof("Executing task: id=%d, name=%s, type=%s, timeout=%d",
		params.TaskID, params.Name, params.TaskType, params.Timeout)

	// Create workspace directory: ${task.workspace}/workspace_${pipeline_id}/
	workspacePath, err := e.workspace.CreateWorkspace(params.PipelineRunID)
	if err != nil {
		e.log.Warnf("Failed to create workspace: %v", err)
	}
	if workspacePath != "" {
		if err := e.workspace.TouchWorkspaceMarker(workspacePath, startTime); err != nil {
			e.log.Warnf("Failed to update workspace last-used marker at start: %v", err)
		}
		e.workspace.MarkWorkspaceActive(workspacePath)
		defer func() {
			e.workspace.MarkWorkspaceInactive(workspacePath)
			if err := e.workspace.TouchWorkspaceMarker(workspacePath, time.Now()); err != nil {
				e.log.Warnf("Failed to update workspace last-used marker at end: %v", err)
			}
		}()
	}

	// Determine working directory: use workspacePath as default, fallback to params.WorkDir if provided
	workDir := workspacePath
	if params.WorkDir != "" {
		// If WorkDir is a relative path, join it with workspacePath
		if !filepath.IsAbs(params.WorkDir) {
			workDir = filepath.Join(workspacePath, params.WorkDir)
		} else {
			workDir = params.WorkDir
		}
	}

	if params.TaskType == "docker" {
		dockerScript, err := e.dockerBuildScript(params, workDir)
		if err != nil {
			return &Result{ExitCode: -1, Error: err.Error(), Duration: time.Since(startTime)}
		}
		params.Script = dockerScript
	}

	// Write task file
	if workspacePath != "" {
		_, err = e.workspace.WriteTaskFile(workspacePath, params.TaskID, params.Script)
		if err != nil {
			e.log.Warnf("Failed to write task file: %v", err)
		}
	}

	// Prepare execution context with timeout
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(params.Timeout)*time.Second)
	defer cancel()

	e.log.Infof("Task %d executing in workspace: %s", params.TaskID, workDir)

	// Execute the script in the workspace directory
	stdout, stderr, err := e.runScript(execCtx, params.TaskID, params.Script, workDir, params.EnvVars)

	duration := time.Since(startTime)

	// Determine status
	status := "success"
	if err != nil {
		status = "failed"
	}

	e.log.Infof("Task %d completed: status=%s, duration=%v, exit_code=%d",
		params.TaskID, status, duration, getExitCode(err))

	return &Result{
		ExitCode: getExitCode(err),
		Stdout:   stdout,
		Stderr:   stderr,
		Error:    errToString(err, stderr),
		Duration: duration,
	}
}

// runScript executes a shell script and captures output
func (e *Executor) runScript(ctx context.Context, taskID uint64, script, workDir string, envVars map[string]string) (string, string, error) {
	env := append([]string{}, os.Environ()...)
	seen := make(map[string]int, len(env))
	for i, item := range env {
		for j := 0; j < len(item); j++ {
			if item[j] == '=' {
				seen[item[:j]] = i
				break
			}
		}
	}
	for k, v := range envVars {
		entry := fmt.Sprintf("%s=%s", k, v)
		if idx, ok := seen[k]; ok {
			env[idx] = entry
			continue
		}
		seen[k] = len(env)
		env = append(env, entry)
	}

	// Determine shell command
	shell := "/bin/sh"
	if _, err := exec.LookPath("bash"); err == nil {
		shell = "/bin/bash"
	}

	// Create command
	cmd := exec.CommandContext(ctx, shell, "-c", script)
	cmd.Env = env
	cmd.Dir = workDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdoutBuf, stderrBuf bytes.Buffer
	callback := e.GetLogCallback()
	stdoutWriter := &outputCaptureWriter{executor: e, buf: &stdoutBuf, level: "info", source: "stdout", taskID: taskID, callback: callback}
	stderrWriter := &outputCaptureWriter{executor: e, buf: &stderrBuf, level: "error", source: "stderr", taskID: taskID, callback: callback}
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter

	// Start command
	if err := cmd.Start(); err != nil {
		return "", "", fmt.Errorf("failed to start command: %w", err)
	}

	if cmd.Process != nil {
		go func() {
			<-ctx.Done()
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}()
	}

	// Wait for command to complete
	err := cmd.Wait()
	stdoutWriter.Flush()
	stderrWriter.Flush()

	return stdoutBuf.String(), stderrBuf.String(), err
}

// ParseParams parses task parameters from the task object
func ParseParams(task interface{}) (*TaskParams, error) {
	taskMap, ok := task.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid task format")
	}

	params := &TaskParams{}

	if id, ok := taskMap["id"].(float64); ok {
		params.TaskID = uint64(id)
	}
	if pid, ok := taskMap["pipeline_run_id"].(float64); ok {
		params.PipelineRunID = uint64(pid)
	}
	if nodeID, ok := taskMap["node_id"].(string); ok {
		params.NodeID = nodeID
	}
	if taskType, ok := taskMap["task_type"].(string); ok {
		params.TaskType = taskType
	}
	if name, ok := taskMap["name"].(string); ok {
		params.Name = name
	}
	if rawParams, ok := taskMap["params"].(string); ok && rawParams != "" {
		params.Params = ParseStructuredParamsJSON(rawParams)
	}
	if script, ok := taskMap["script"].(string); ok {
		params.Script = script
	}
	if workDir, ok := taskMap["work_dir"].(string); ok {
		params.WorkDir = workDir
	}
	if envVars, ok := taskMap["env_vars"].(string); ok && envVars != "" {
		params.EnvVars = ParseEnvVarsJSON(envVars)
	}
	if timeout, ok := taskMap["timeout"].(float64); ok {
		params.Timeout = int(timeout)
	} else {
		params.Timeout = 3600 // Default 1 hour
	}

	return params, nil
}

// getExitCode extracts exit code from error
func getExitCode(err error) int {
	if err == nil {
		return 0
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}

	return -1
}

func errToString(err error, stderr string) string {
	if err == nil {
		return ""
	}

	msg := err.Error()
	stderrSummary := summarizeStderr(stderr)
	if exitErr, ok := err.(*exec.ExitError); ok {
		if code := exitErr.ExitCode(); code != 0 {
			if stderrSummary != "" {
				return fmt.Sprintf("command exited with code %d: %s", code, stderrSummary)
			}
			return fmt.Sprintf("command exited with code %d", code)
		}
	}
	if stderrSummary != "" && !strings.Contains(msg, stderrSummary) {
		return fmt.Sprintf("%s: %s", msg, stderrSummary)
	}

	return msg
}

func summarizeStderr(stderr string) string {
	trimmed := strings.TrimSpace(stderr)
	if trimmed == "" {
		return ""
	}
	parts := strings.FieldsFunc(trimmed, func(r rune) bool { return r == '\n' || r == '\r' })
	filtered := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		line := strings.TrimSpace(part)
		if line == "" {
			continue
		}
		if _, exists := seen[line]; exists {
			continue
		}
		seen[line] = struct{}{}
		filtered = append(filtered, line)
		if len(filtered) == 3 {
			break
		}
	}
	return strings.Join(filtered, " | ")
}
