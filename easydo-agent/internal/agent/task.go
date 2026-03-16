package agent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"easydo-agent/internal/client"
	"easydo-agent/internal/config"
	"easydo-agent/internal/task"
	"github.com/sirupsen/logrus"
)

// TaskHandler orchestrates agent-side execution and reporting.
//
// Besides launching work locally, it also preserves the reporting contract the
// server relies on to converge run/task state. Terminal status/log messages may
// need to survive temporary WS outages and be replayed after reconnect.
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
	runCtx     context.Context
	inFlight   sync.Map
	pendingMu  sync.Mutex
	pendingWS  []pendingWebSocketMessage
}

// pendingWebSocketMessage stores one outbound WS message that could not be sent
// immediately and therefore must be replayed after reconnect.
type pendingWebSocketMessage struct {
	messageType string
	payload     map[string]interface{}
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
	ID              uint64 `json:"id"`
	AgentID         uint64 `json:"agent_id"`
	PipelineRunID   uint64 `json:"pipeline_run_id"`
	NodeID          string `json:"node_id"`
	TaskType        string `json:"task_type"`
	Name            string `json:"name"`
	Params          string `json:"params"`
	Script          string `json:"script"`
	WorkDir         string `json:"work_dir"`
	EnvVars         string `json:"env_vars"`
	Status          string `json:"status"`
	Priority        int    `json:"priority"`
	Timeout         int    `json:"timeout"`
	RetryCount      int    `json:"retry_count"`
	MaxRetries      int    `json:"max_retries"`
	DispatchToken   string `json:"dispatch_token"`
	DispatchAttempt int    `json:"dispatch_attempt"`
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

// SetWebSocketClient swaps the active WS transport and wires reconnect replay.
// Any successful reconnect should trigger a flush of buffered terminal/log
// messages so the server can continue state convergence after owner failover.
func (th *TaskHandler) SetWebSocketClient(wsClient *client.WebSocketClient) {
	th.mu.Lock()
	th.wsClient = wsClient
	th.mu.Unlock()
	if wsClient != nil {
		wsClient.SetReconnectHandler(th.flushPendingWebSocketMessages)
	}
}

// HandleTaskAssign handles concrete task assignment from server via WebSocket.
func (th *TaskHandler) HandleTaskAssign(msg *client.TaskAssignMessage) error {
	if msg == nil {
		return fmt.Errorf("task assignment is nil")
	}

	t := Task{
		ID:              msg.Task.ID,
		AgentID:         msg.Task.AgentID,
		PipelineRunID:   msg.Task.PipelineRunID,
		NodeID:          msg.Task.NodeID,
		TaskType:        msg.Task.TaskType,
		Name:            msg.Task.Name,
		Params:          msg.Task.Params,
		Script:          msg.Task.Script,
		WorkDir:         msg.Task.WorkDir,
		EnvVars:         msg.Task.EnvVars,
		Status:          msg.Task.Status,
		Priority:        msg.Task.Priority,
		Timeout:         msg.Task.Timeout,
		RetryCount:      msg.Task.RetryCount,
		MaxRetries:      msg.Task.MaxRetries,
		DispatchToken:   msg.Task.DispatchToken,
		DispatchAttempt: msg.Task.DispatchAttempt,
	}

	if t.ID == 0 {
		return fmt.Errorf("invalid task id")
	}
	if t.Status != "" && t.Status != "acked" && t.Status != "running" {
		th.log.Infof("Skip task assignment %d with status %s", t.ID, t.Status)
		return nil
	}

	if _, loaded := th.inFlight.LoadOrStore(t.ID, struct{}{}); loaded {
		th.log.Debugf("Task %d already in-flight, skip duplicate assignment", t.ID)
		return nil
	}

	th.mu.RLock()
	ctx := th.runCtx
	th.mu.RUnlock()
	if ctx == nil {
		ctx = context.Background()
	}

	go func(task Task) {
		defer th.inFlight.Delete(task.ID)
		th.executeTask(ctx, &task)
	}(t)

	return nil
}

func (th *TaskHandler) HandlePullTaskNow(taskID uint64, dispatchToken string) error {
	th.mu.RLock()
	wsClient := th.wsClient
	th.mu.RUnlock()
	if wsClient == nil || !wsClient.IsConnected() {
		return fmt.Errorf("websocket unavailable")
	}
	payload := map[string]interface{}{
		"task_id":        taskID,
		"dispatch_token": dispatchToken,
		"timestamp":      time.Now().Unix(),
	}
	if sessionID := wsClient.GetSessionID(); sessionID != "" {
		payload["agent_session_id"] = sessionID
	}
	return wsClient.SendMessage("pull_task", payload)
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

			status := "execute_success"
			duration := result.Duration.Seconds()
			if !success {
				status = "execute_failed"
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
	finalStatus := "execute_success"
	finalExitCode := 0
	finalErrorMsg := ""
	finalDuration := float64(0)

	if lastResult != nil {
		finalExitCode = lastResult.ExitCode
		finalDuration = lastResult.Duration.Seconds()
		if !success {
			finalStatus = "execute_failed"
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

// Start prepares the task handler for websocket-driven assignments.
func (th *TaskHandler) Start(ctx context.Context) {
	th.mu.Lock()
	if th.running {
		th.mu.Unlock()
		return
	}
	th.running = true
	th.runCtx = ctx
	th.mu.Unlock()

	th.log.Info("Task handler started in websocket assignment mode")
}

// Stop stops the task handler.
func (th *TaskHandler) Stop() {
	th.mu.Lock()
	if !th.running {
		th.mu.Unlock()
		return
	}
	th.running = false
	th.runCtx = nil
	th.mu.Unlock()

	close(th.stopChan)
}

// executeTask executes a single task
func (th *TaskHandler) executeTask(ctx context.Context, task *Task) {
	th.log.Infof("Executing task: id=%d, name=%s, type=%s", task.ID, task.Name, task.TaskType)

	attempt := task.RetryCount + 1

	// Parse task parameters
	params, err := task.ParseParams()
	if err != nil {
		th.log.Warnf("Failed to parse task params: %v", err)
		return
	}

	// Report task as running first. If this fails, keep task pending server-side and wait for redispatch.
	if err := th.reportTaskUpdateV2(task, attempt, "running", 0, "", 0, nil); err != nil {
		th.log.Warnf("Failed to report v2 task start: %v", err)
		return
	}

	// Set up log callback for real-time log reporting
	var logSeq int64
	th.executor.SetLogCallback(func(level, message, source string, _ int) {
		seq := atomic.AddInt64(&logSeq, 1)
		if err := th.reportTaskLogChunkV2(task, attempt, seq, source, message); err != nil {
			th.log.Warnf("Failed to report v2 log: %v", err)
		}
	})

	// Execute the task with workspace support
	result := th.executor.Execute(ctx, *params)

	// Report task completion with actual duration
	status := "execute_success"
	if result.Error != "" || result.ExitCode != 0 {
		status = "execute_failed"
	}

	finalResult := map[string]interface{}{
		"stdout_size": len(result.Stdout),
		"stderr_size": len(result.Stderr),
	}
	if err := th.reportTaskUpdateV2(task, attempt, status, result.ExitCode, result.Error, result.Duration.Milliseconds(), finalResult); err != nil {
		th.log.Warnf("Failed to report v2 task completion: %v", err)
	}
	if err := th.reportTaskLogEndV2(task, attempt, atomic.LoadInt64(&logSeq)); err != nil {
		th.log.Debugf("Failed to report v2 log end: %v", err)
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
		params.EnvVars = task.ParseEnvVarsJSON(t.EnvVars)
	}

	return params, nil
}

func (th *TaskHandler) reportTaskUpdateV2(t *Task, attempt int, status string, exitCode int, errorMsg string, durationMs int64, result map[string]interface{}) error {
	// Only terminal execution states are replay-buffered on failure. Non-terminal
	// progress can be regenerated by a still-running executor, but losing the final
	// completion/failure event would leave the server-side run stuck forever.
	th.mu.RLock()
	wsClient := th.wsClient
	th.mu.RUnlock()
	queueOnFailure := status == "execute_success" || status == "execute_failed"

	payload := map[string]interface{}{
		"task_id":         t.ID,
		"attempt":         attempt,
		"status":          status,
		"exit_code":       exitCode,
		"error_msg":       errorMsg,
		"duration_ms":     durationMs,
		"idempotency_key": fmt.Sprintf("%d:%d:%s:%d", t.ID, attempt, status, exitCode),
		"timestamp":       time.Now().Unix(),
	}
	if result != nil {
		payload["result"] = result
	}

	if wsClient == nil || !wsClient.IsConnected() {
		if queueOnFailure {
			th.enqueuePendingWebSocketMessage("task_update_v2", payload)
		}
		return fmt.Errorf("websocket unavailable")
	}

	if err := wsClient.SendMessage("task_update_v2", payload); err != nil {
		if queueOnFailure {
			th.enqueuePendingWebSocketMessage("task_update_v2", payload)
		}
		return err
	}
	return nil
}

func (th *TaskHandler) reportTaskLogChunkV2(t *Task, attempt int, seq int64, stream, chunk string) error {
	// Log chunks are safe to replay because the server persists them idempotently
	// by `(task_id, attempt, seq)`. In reconnect scenarios, duplication is less
	// harmful than silently dropping lines.
	th.mu.RLock()
	wsClient := th.wsClient
	th.mu.RUnlock()

	if stream == "" {
		stream = "stdout"
	}

	payload := map[string]interface{}{
		"task_id":   t.ID,
		"attempt":   attempt,
		"seq":       seq,
		"stream":    stream,
		"chunk":     chunk,
		"timestamp": time.Now().Unix(),
	}

	if wsClient == nil || !wsClient.IsConnected() {
		th.enqueuePendingWebSocketMessage("task_log_chunk_v2", payload)
		return fmt.Errorf("websocket unavailable")
	}
	if err := wsClient.SendMessage("task_log_chunk_v2", payload); err != nil {
		th.enqueuePendingWebSocketMessage("task_log_chunk_v2", payload)
		return err
	}
	return nil
}

func (th *TaskHandler) reportTaskLogEndV2(t *Task, attempt int, finalSeq int64) error {
	// The server uses log_end as the signal that it can seal completed-log history
	// for the attempt, so this marker must survive disconnect windows just like the
	// terminal task status event does.
	th.mu.RLock()
	wsClient := th.wsClient
	th.mu.RUnlock()

	payload := map[string]interface{}{
		"task_id":   t.ID,
		"attempt":   attempt,
		"final_seq": finalSeq,
		"timestamp": time.Now().Unix(),
	}

	if wsClient == nil || !wsClient.IsConnected() {
		th.enqueuePendingWebSocketMessage("task_log_end_v2", payload)
		return fmt.Errorf("websocket unavailable")
	}
	if err := wsClient.SendMessage("task_log_end_v2", payload); err != nil {
		th.enqueuePendingWebSocketMessage("task_log_end_v2", payload)
		return err
	}
	return nil
}

func (th *TaskHandler) enqueuePendingWebSocketMessage(messageType string, payload map[string]interface{}) {
	// Clone immediately so later mutations by the caller cannot corrupt the replay
	// queue.
	if messageType == "" || payload == nil {
		return
	}
	cloned := make(map[string]interface{}, len(payload))
	for k, v := range payload {
		cloned[k] = v
	}
	th.pendingMu.Lock()
	defer th.pendingMu.Unlock()
	th.pendingWS = append(th.pendingWS, pendingWebSocketMessage{messageType: messageType, payload: cloned})
}

func (th *TaskHandler) flushPendingWebSocketMessages() {
	th.mu.RLock()
	wsClient := th.wsClient
	th.mu.RUnlock()
	if wsClient == nil || !wsClient.IsConnected() {
		return
	}
	th.flushPendingWebSocketMessagesWithSender(func(messageType string, payload map[string]interface{}) error {
		return wsClient.SendMessage(messageType, payload)
	})
}

func (th *TaskHandler) flushPendingWebSocketMessagesWithSender(sender func(string, map[string]interface{}) error) {
	// Replay is stop-on-first-failure to preserve FIFO ordering. Skipping ahead
	// would allow later status/log messages to overtake earlier ones on the server.
	if sender == nil {
		return
	}
	for {
		th.pendingMu.Lock()
		if len(th.pendingWS) == 0 {
			th.pendingMu.Unlock()
			return
		}
		msg := th.pendingWS[0]
		th.pendingMu.Unlock()

		if err := sender(msg.messageType, msg.payload); err != nil {
			return
		}

		th.pendingMu.Lock()
		if len(th.pendingWS) > 0 && th.pendingWS[0].messageType == msg.messageType {
			th.pendingWS = th.pendingWS[1:]
		}
		th.pendingMu.Unlock()
	}
}

// reportTaskStatus reports task status to the server via WebSocket
func (th *TaskHandler) reportTaskStatus(ctx context.Context, taskID uint64, token, status string, exitCode int, stdout, stderr, errorMsg string, duration float64) error {
	_ = ctx
	_ = token
	th.mu.RLock()
	wsClient := th.wsClient
	th.mu.RUnlock()

	if wsClient == nil || !wsClient.IsConnected() {
		return fmt.Errorf("websocket unavailable")
	}

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
	_ = ctx
	th.mu.RLock()
	wsClient := th.wsClient
	th.mu.RUnlock()

	if wsClient == nil || !wsClient.IsConnected() {
		return fmt.Errorf("websocket unavailable")
	}

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
