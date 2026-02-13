package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"easydo-agent/internal/client"
	"easydo-agent/internal/config"
	"easydo-agent/internal/task"
	"github.com/sirupsen/logrus"
)

// TaskHandler handles task polling and execution
type TaskHandler struct {
	httpClient *client.HTTPClient
	wsClient   *client.WebSocketClient
	cfg        *config.Config
	tokenMgr   *TokenManager
	agentID    uint64
	token      string
	log        *logrus.Logger
	executor   *task.Executor
	mu         sync.RWMutex
	running    bool
	stopChan   chan struct{}
}

// NewTaskHandler creates a new task handler
func NewTaskHandler(httpClient *client.HTTPClient, wsClient *client.WebSocketClient, cfg *config.Config, tokenMgr *TokenManager, log *logrus.Logger) *TaskHandler {
	return &TaskHandler{
		httpClient: httpClient,
		wsClient:   wsClient,
		cfg:        cfg,
		tokenMgr:   tokenMgr,
		log:        log,
		executor:   task.NewExecutor(log, cfg.GetWorkspacePath()),
		stopChan:   make(chan struct{}),
	}
}

// Task represents a task to be executed
type Task struct {
	ID            uint64 `json:"id"`
	AgentID       uint64 `json:"agent_id"`
	PipelineRunID uint64 `json:"pipeline_run_id"`
	NodeID        string `json:"node_id"`
	TaskType      string `json:"task_type"`
	Name          string `json:"name"`
	Params        string `json:"params"`
	Script        string `json:"script"`
	WorkDir       string `json:"work_dir"`
	EnvVars       string `json:"env_vars"`
	Status        string `json:"status"`
	Priority      int    `json:"priority"`
	Timeout       int    `json:"timeout"`
}

// SetToken sets the agent token
func (th *TaskHandler) SetToken(token string) {
	th.mu.Lock()
	th.token = token
	th.mu.Unlock()
}

// SetAgentID sets the agent ID
func (th *TaskHandler) SetAgentID(agentID uint64) {
	th.mu.Lock()
	th.agentID = agentID
	th.mu.Unlock()
}

// SetWebSocketClient sets the WebSocket client
func (th *TaskHandler) SetWebSocketClient(wsClient *client.WebSocketClient) {
	th.mu.Lock()
	th.wsClient = wsClient
	th.mu.Unlock()
}

// HandlePipelineAssign handles pipeline assignment from server via WebSocket
func (th *TaskHandler) HandlePipelineAssign(msg *client.PipelineAssignMessage) error {
	th.log.Infof("Handling pipeline assignment: run_id=%d", msg.RunID)

	go func() {
		if err := th.executePipeline(msg); err != nil {
			th.log.Errorf("Failed to execute pipeline run_id=%d: %v", msg.RunID, err)
		}
	}()

	return nil
}

// HandleTaskCancel handles task cancellation from server via WebSocket
func (th *TaskHandler) HandleTaskCancel(taskID uint64) error {
	th.log.Infof("Handling task cancellation: task_id=%d", taskID)
	// TODO: Implement task cancellation logic
	return nil
}

// executePipeline executes a pipeline based on the assignment message
func (th *TaskHandler) executePipeline(msg *client.PipelineAssignMessage) error {
	th.log.Infof("Starting pipeline execution: run_id=%d", msg.RunID)

	// Build DAG engine - convert client.PipelineConfig to task.PipelineConfig
	pipelineConfig := task.PipelineConfig{
		Version:     msg.Config.Version,
		Nodes:       convertNodes(msg.Config.Nodes),
		Edges:       convertEdges(msg.Config.Edges),
		Connections: convertConnections(msg.Config.Connections),
	}
	dagEngine := task.NewDAGEngine(pipelineConfig, th.executor)

	if err := dagEngine.BuildGraph(); err != nil {
		return fmt.Errorf("failed to build DAG: %w", err)
	}

	// Set up log callback for real-time reporting
	dagEngine.SetLogCallback(func(taskID uint64, level, message, source string) {
		th.reportPipelineLog(msg.RunID, taskID, level, message, source)
	})

	// Execute nodes in topological order
	for !dagEngine.IsCompleted() {
		select {
		case <-th.stopChan:
			return fmt.Errorf("pipeline execution cancelled")
		default:
		}

		// Get executable nodes (in-degree = 0 and not completed)
		executableNodes := dagEngine.GetExecutableNodes()
		if len(executableNodes) == 0 && !dagEngine.IsCompleted() {
			return fmt.Errorf("no executable nodes but DAG not completed, possible circular dependency")
		}

		// Execute nodes concurrently
		var wg sync.WaitGroup
		nodeResults := make(map[string]*task.Result)
		nodeSuccess := make(map[string]bool)
		var resultMu sync.Mutex

		for _, nodeID := range executableNodes {
			node := dagEngine.GetNode(nodeID)
			if node == nil {
				continue
			}

			wg.Add(1)
			go func(n *task.PipelineNode, nid string) {
				defer wg.Done()

				result, success := th.executeNode(msg.RunID, n)

				resultMu.Lock()
				nodeResults[nid] = result
				nodeSuccess[nid] = success
				resultMu.Unlock()
			}(node, nodeID)
		}

		wg.Wait()

		for nodeID, result := range nodeResults {
			outputs := th.buildNodeOutputs(nodeID, result)
			success := nodeSuccess[nodeID]
			dagEngine.MarkCompleted(nodeID, success, outputs)

			status := "success"
			duration := result.Duration.Seconds()
			if !success {
				status = "failed"
			}
			th.reportPipelineStatus(msg.RunID, nodeID, status, result.ExitCode, result.Error, duration)
		}

		if dagEngine.HasFailedNodesBlockingExecution() {
			th.log.Errorf("Pipeline run_id=%d has failed nodes blocking execution", msg.RunID)
			return fmt.Errorf("pipeline execution blocked by failed nodes")
		}
	}

	th.log.Infof("Pipeline execution completed: run_id=%d", msg.RunID)
	return nil
}

// executeNode executes a single pipeline node with retry support
// Returns the result and a boolean indicating if the node should be considered successful for DAG progression
func (th *TaskHandler) executeNode(runID uint64, node *task.PipelineNode) (*task.Result, bool) {
	th.log.Infof("Executing node: run_id=%d, node_id=%s, type=%s", runID, node.ID, node.Type)

	// Get retry configuration
	maxRetries := node.RetryCount
	if maxRetries < 0 {
		maxRetries = 0
	}
	retryInterval := node.RetryInterval
	if retryInterval <= 0 {
		retryInterval = 5 // 默认重试间隔5秒
	}

	var lastResult *task.Result
	success := false

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			th.log.Infof("Retrying node: run_id=%d, node_id=%s, attempt=%d/%d",
				runID, node.ID, attempt, maxRetries)
			// Wait before retry
			time.Sleep(time.Duration(retryInterval) * time.Second)
		}

		// Report node as running
		th.reportPipelineStatus(runID, node.ID, "running", 0, "", 0)

		// Convert node to task parameters
		params := &task.TaskParams{
			TaskID:        uint64(time.Now().UnixNano()),
			PipelineRunID: runID,
			NodeID:        node.ID,
			TaskType:      node.Type,
			Name:          node.Name,
			Timeout:       node.Timeout,
		}

		// Extract script from node config
		config := node.GetNodeConfig()
		if script, ok := config["script"].(string); ok {
			params.Script = script
		}
		if workDir, ok := config["working_dir"].(string); ok {
			params.WorkDir = workDir
		}
		if env, ok := config["env"].(map[string]string); ok {
			params.EnvVars = env
		}

		// Set up log callback for this node
		nodeLineNumbers := make(map[string]int)
		th.executor.SetLogCallback(func(level, message, source string, lineNumber int) {
			nodeLineNumbers[node.ID]++
			th.reportPipelineLog(runID, 0, level, message, source)
		})

		// Execute the task
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(params.Timeout)*time.Second)
		result := th.executor.Execute(ctx, *params)
		cancel()

		lastResult = result

		// Check if execution was successful
		if result.ExitCode == 0 && result.Error == "" {
			success = true
			break
		}

		// Check if this was the last attempt
		if attempt >= maxRetries {
			break
		}
	}

	// If all retries failed but ignore_failure is set, treat as success
	if !success && node.IgnoreFailure {
		th.log.Infof("Node %s failed but ignore_failure is true, continuing...", node.ID)
		success = true
	}

	// Report final status
	finalStatus := "success"
	finalExitCode := 0
	finalErrorMsg := ""
	finalDuration := float64(0)

	if lastResult != nil {
		finalExitCode = lastResult.ExitCode
		finalDuration = lastResult.Duration.Seconds()
		if !success {
			finalStatus = "failed"
			finalErrorMsg = lastResult.Error
			if finalErrorMsg == "" && finalExitCode != 0 {
				finalErrorMsg = fmt.Sprintf("command exited with code %d", finalExitCode)
			}
		}
	}

	th.reportPipelineStatus(runID, node.ID, finalStatus, finalExitCode, finalErrorMsg, finalDuration)

	return lastResult, success
}

// buildNodeOutputs builds output map from execution result
func (th *TaskHandler) buildNodeOutputs(nodeID string, result *task.Result) map[string]interface{} {
	outputs := map[string]interface{}{
		"exit_code": result.ExitCode,
		"duration":  result.Duration.Seconds(),
	}

	if result.Error != "" {
		outputs["error"] = result.Error
	}

	return outputs
}

// reportPipelineStatus reports task status via WebSocket
func (th *TaskHandler) reportPipelineStatus(runID uint64, nodeID, status string, exitCode int, errorMsg string, duration float64) {
	th.mu.RLock()
	wsClient := th.wsClient
	th.mu.RUnlock()

	if wsClient == nil || !wsClient.IsConnected() {
		th.log.Debugf("WebSocket not available, cannot report status")
		return
	}

	result := map[string]interface{}{}
	if exitCode != 0 {
		result["exit_code"] = exitCode
	}
	if errorMsg != "" {
		result["error"] = errorMsg
	}
	result["duration"] = duration

	if err := wsClient.ReportTaskStatus(runID, nodeID, status, exitCode, errorMsg, result); err != nil {
		th.log.Warnf("Failed to report task status: %v", err)
	}
}

// reportPipelineLog reports log entry via WebSocket
func (th *TaskHandler) reportPipelineLog(runID uint64, taskID uint64, level, message, source string) {
	th.mu.RLock()
	wsClient := th.wsClient
	th.mu.RUnlock()

	if wsClient == nil || !wsClient.IsConnected() {
		return
	}

	// Use taskID as line number for ordering
	lineNumber := int(taskID)

	if err := wsClient.ReportTaskLog(runID, "", level, message, source, lineNumber); err != nil {
		th.log.Debugf("Failed to report log: %v", err)
	}
}

// Start starts the task polling loop
func (th *TaskHandler) Start(ctx context.Context) {
	th.mu.Lock()
	if th.running {
		th.mu.Unlock()
		return
	}
	th.running = true
	th.mu.Unlock()

	th.log.Info("Starting task handler")
	go th.run(ctx)
}

// Stop stops the task polling loop
func (th *TaskHandler) Stop() {
	th.mu.Lock()
	if !th.running {
		th.mu.Unlock()
		return
	}
	th.running = false
	th.mu.Unlock()

	close(th.stopChan)
}

// run is the main task polling loop
func (th *TaskHandler) run(ctx context.Context) {
	pollInterval := time.Duration(th.cfg.GetPollInterval()) * time.Second
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-th.stopChan:
			return
		case <-ticker.C:
			th.pollAndExecuteTasks(ctx)
		}
	}
}

// pollAndExecuteTasks polls for tasks and executes them
func (th *TaskHandler) pollAndExecuteTasks(ctx context.Context) {
	th.mu.RLock()
	agentID := th.agentID
	token := th.token
	th.mu.RUnlock()

	if agentID == 0 || token == "" {
		return
	}

	// Get pending tasks
	tasks, err := th.getPendingTasks(ctx, agentID)
	if err != nil {
		th.log.Warnf("Failed to get pending tasks: %v", err)
		return
	}

	if len(tasks) == 0 {
		return
	}

	th.log.Infof("Found %d pending tasks", len(tasks))

	// Execute each task
	for _, t := range tasks {
		select {
		case <-ctx.Done():
			return
		case <-th.stopChan:
			return
		default:
			th.executeTask(ctx, &t)
		}
	}
}

// getPendingTasks retrieves pending tasks from the server
func (th *TaskHandler) getPendingTasks(ctx context.Context, agentID uint64) ([]Task, error) {
	path := fmt.Sprintf("/api/tasks/agent/%d/pending", agentID)

	resp, err := th.httpClient.Get(ctx, path)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to get tasks: status %d", resp.StatusCode)
	}

	data, ok := resp.Body["data"].(map[string]interface{})
	if !ok {
		return nil, nil
	}

	listData, ok := data["list"].([]interface{})
	if !ok || len(listData) == 0 {
		return nil, nil
	}

	tasks := make([]Task, 0, len(listData))
	for _, item := range listData {
		taskData, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		task := Task{
			ID:            uint64(taskData["id"].(float64)),
			AgentID:       uint64(taskData["agent_id"].(float64)),
			PipelineRunID: uint64(taskData["pipeline_run_id"].(float64)),
			NodeID:        taskData["node_id"].(string),
			TaskType:      taskData["task_type"].(string),
			Name:          taskData["name"].(string),
			Params:        taskData["params"].(string),
			Script:        taskData["script"].(string),
			WorkDir:       taskData["work_dir"].(string),
			EnvVars:       taskData["env_vars"].(string),
			Priority:      int(taskData["priority"].(float64)),
			Timeout:       int(taskData["timeout"].(float64)),
		}

		if status, ok := taskData["status"].(string); ok {
			task.Status = status
		}

		tasks = append(tasks, task)
	}

	return tasks, nil
}

// executeTask executes a single task
func (th *TaskHandler) executeTask(ctx context.Context, task *Task) {
	th.log.Infof("Executing task: id=%d, name=%s, type=%s", task.ID, task.Name, task.TaskType)

	th.mu.RLock()
	token := th.token
	th.mu.RUnlock()

	// Parse task parameters
	params, err := task.ParseParams()
	if err != nil {
		th.log.Warnf("Failed to parse task params: %v", err)
		return
	}

	// Report task as running with duration 0
	if err := th.reportTaskStatus(ctx, task.ID, token, "running", 0, "", "", "", 0); err != nil {
		th.log.Warnf("Failed to report task start: %v", err)
	}

	// Set up log callback for real-time log reporting
	th.executor.SetLogCallback(func(level, message, source string, lineNumber int) {
		if err := th.ReportLog(ctx, task.ID, level, message, source, lineNumber); err != nil {
			th.log.Warnf("Failed to report log: %v", err)
		}
	})

	// Execute the task with workspace support
	result := th.executor.Execute(ctx, *params)

	// Report task completion with actual duration
	status := "success"
	if result.Error != "" || result.ExitCode != 0 {
		status = "failed"
	}

	if err := th.reportTaskStatus(ctx, task.ID, token, status, result.ExitCode, result.Stdout, result.Stderr, result.Error, result.Duration.Seconds()); err != nil {
		th.log.Warnf("Failed to report task completion: %v", err)
	}

	th.log.Infof("Task %d completed: status=%s, exit_code=%d, duration=%v",
		task.ID, status, result.ExitCode, result.Duration)
}

// ParseParams converts Task to TaskParams
func (t *Task) ParseParams() (*task.TaskParams, error) {
	params := &task.TaskParams{
		TaskID:        t.ID,
		PipelineRunID: t.PipelineRunID,
		NodeID:        t.NodeID,
		TaskType:      t.TaskType,
		Name:          t.Name,
		Script:        t.Script,
		WorkDir:       t.WorkDir,
		Timeout:       t.Timeout,
	}

	// Parse environment variables
	if t.EnvVars != "" {
		var env map[string]string
		if err := json.Unmarshal([]byte(t.EnvVars), &env); err == nil {
			params.EnvVars = env
		}
	}

	return params, nil
}

// reportTaskStatus reports task status to the server via WebSocket
func (th *TaskHandler) reportTaskStatus(ctx context.Context, taskID uint64, token, status string, exitCode int, stdout, stderr, errorMsg string, duration float64) error {
	th.mu.RLock()
	wsClient := th.wsClient
	th.mu.RUnlock()

	// Try WebSocket first
	if wsClient != nil && wsClient.IsConnected() {
		payload := map[string]interface{}{
			"task_id":   taskID,
			"status":    status,
			"exit_code": exitCode,
			"stdout":    stdout,
			"stderr":    stderr,
			"error_msg": errorMsg,
			"duration":  duration,
		}
		return wsClient.SendMessage("task_status", payload)
	}

	// Fallback to HTTP if WebSocket not available
	req := map[string]interface{}{
		"agent_id":  th.agentID,
		"token":     token,
		"task_id":   taskID,
		"status":    status,
		"exit_code": exitCode,
		"stdout":    stdout,
		"stderr":    stderr,
		"error_msg": errorMsg,
		"duration":  duration,
	}

	resp, err := th.httpClient.Post(ctx, "/api/tasks/report/status", req)
	if err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("report status failed: %d", resp.StatusCode)
	}

	return nil
}

// convertNodes converts client.PipelineNode slice to task.PipelineNode slice
func convertNodes(nodes []client.PipelineNode) []task.PipelineNode {
	result := make([]task.PipelineNode, len(nodes))
	for i, n := range nodes {
		node := task.PipelineNode{
			ID:      n.ID,
			Type:    n.Type,
			Name:    n.Name,
			Config:  n.Config,
			Params:  n.Params,
			Timeout: n.Timeout,
		}

		if n.Config != nil {
			if v, ok := n.Config["ignore_failure"].(bool); ok {
				node.IgnoreFailure = v
			}
			if v, ok := n.Config["retry_count"].(float64); ok {
				node.RetryCount = int(v)
			}
			if v, ok := n.Config["retry_interval"].(float64); ok {
				node.RetryInterval = int(v)
			}
		}

		result[i] = node
	}
	return result
}

// convertEdges converts client.PipelineEdge slice to task.PipelineEdge slice
func convertEdges(edges []client.PipelineEdge) []task.PipelineEdge {
	result := make([]task.PipelineEdge, len(edges))
	for i, e := range edges {
		result[i] = task.PipelineEdge{
			From:          e.From,
			To:            e.To,
			IgnoreFailure: e.IgnoreFailure,
		}
	}
	return result
}

// convertConnections converts client.PipelineConnection slice to task.PipelineConnection slice
func convertConnections(conns []client.PipelineConnection) []task.PipelineConnection {
	result := make([]task.PipelineConnection, len(conns))
	for i, c := range conns {
		result[i] = task.PipelineConnection{
			ID:   c.ID,
			From: c.From,
			To:   c.To,
		}
	}
	return result
}

// ReportLog reports a log entry to the server via WebSocket
func (th *TaskHandler) ReportLog(ctx context.Context, taskID uint64, level, message, source string, lineNumber int) error {
	th.mu.RLock()
	wsClient := th.wsClient
	th.mu.RUnlock()

	// Try WebSocket first
	if wsClient != nil && wsClient.IsConnected() {
		payload := map[string]interface{}{
			"task_id":     taskID,
			"level":       level,
			"message":     message,
			"source":      source,
			"line_number": lineNumber,
			"timestamp":   time.Now().Unix(),
		}
		return wsClient.SendMessage("task_log", payload)
	}

	// Fallback to HTTP if WebSocket not available
	req := map[string]interface{}{
		"agent_id":    th.agentID,
		"token":       th.token,
		"task_id":     taskID,
		"level":       level,
		"message":     message,
		"source":      source,
		"line_number": lineNumber,
		"timestamp":   time.Now().Unix(),
	}

	resp, err := th.httpClient.Post(ctx, "/api/tasks/report/log", req)
	if err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("report log failed: %d", resp.StatusCode)
	}

	return nil
}
