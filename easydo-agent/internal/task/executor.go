package task

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// TaskParams represents task execution parameters
type TaskParams struct {
	TaskID        uint64
	PipelineRunID uint64
	NodeID        string
	TaskType      string
	Name          string
	Script        string
	WorkDir       string
	EnvVars       map[string]string
	Timeout       int
}

// LogCallback is called for each log line
type LogCallback func(level, message, source string, lineNumber int)

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
	log         *logrus.Logger
	workspace   *WorkspaceManager
	logCallback LogCallback
	mu          sync.RWMutex
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
func NewExecutor(log *logrus.Logger, basePath string) *Executor {
	return &Executor{
		log:       log,
		workspace: NewWorkspaceManager(basePath, log),
	}
}

// SetLogCallback sets the callback for log reporting
func (e *Executor) SetLogCallback(callback LogCallback) {
	e.mu.Lock()
	e.logCallback = callback
	e.mu.Unlock()
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

	e.log.Infof("Task %d executing in workspace: %s", params.TaskID, workDir)

	// Execute the script in the workspace directory
	stdout, stderr, err := e.runScript(execCtx, params.Script, workDir, params.EnvVars)

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
		Error:    errToString(err),
		Duration: duration,
	}
}

// runScript executes a shell script and captures output
func (e *Executor) runScript(ctx context.Context, script, workDir string, envVars map[string]string) (string, string, error) {
	// Build environment variables
	env := []string{}
	for k, v := range envVars {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
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

	// Create pipes for output
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", "", fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start command
	if err := cmd.Start(); err != nil {
		return "", "", fmt.Errorf("failed to start command: %w", err)
	}

	// Read output concurrently
	var stdoutBuf, stderrBuf bytes.Buffer
	var wg sync.WaitGroup

	wg.Add(2)
	go e.readOutput(ctx, stdoutPipe, &stdoutBuf, "info", "stdout", &wg)
	go e.readOutput(ctx, stderrPipe, &stderrBuf, "error", "stderr", &wg)

	// Wait for command to complete
	err = cmd.Wait()

	wg.Wait()

	return stdoutBuf.String(), stderrBuf.String(), err
}

// readOutput reads output from a pipe and reports logs
func (e *Executor) readOutput(ctx context.Context, pipe interface{}, buf *bytes.Buffer, level, source string, wg *sync.WaitGroup) {
	defer wg.Done()

	var scanner *bufio.Scanner

	if p, ok := pipe.(interface{ Read([]byte) (int, error) }); ok {
		scanner = bufio.NewScanner(p)
	} else {
		return
	}

	lineNum := 0
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
			line := scanner.Text()
			buf.WriteString(line)
			buf.WriteString("\n")
			lineNum++

			// Report log via callback
			e.mu.RLock()
			if e.logCallback != nil {
				e.logCallback(level, line, source, lineNum)
			}
			e.mu.RUnlock()
		}
	}
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

// errToString converts error to string, extracting exit code from exit errors
func errToString(err error) string {
	if err == nil {
		return ""
	}

	msg := err.Error()
	// Extract exit code from exit errors for cleaner error messages
	if exitErr, ok := err.(*exec.ExitError); ok {
		if code := exitErr.ExitCode(); code != 0 {
			return fmt.Sprintf("command exited with code %d", code)
		}
	}

	return msg
}
