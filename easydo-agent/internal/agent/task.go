package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"easydo-agent/internal/client"
	"easydo-agent/internal/config"
	"easydo-agent/internal/system"
	"easydo-agent/internal/task"
	"github.com/sirupsen/logrus"
)

// TaskHandler orchestrates agent-side execution and reporting.
//
// Besides launching work locally, it also preserves the reporting contract the
// server relies on to converge run/task state. Terminal status/log messages may
// need to survive temporary WS outages and be replayed after reconnect.
type TaskHandler struct {
	httpClient            *client.HTTPClient
	wsClient              *client.WebSocketClient
	cfg                   *config.Config
	tokenMgr              *TokenManager
	agentID               uint64
	token                 string
	log                   *logrus.Logger
	executor              *task.Executor
	embeddedBuildkit      *task.EmbeddedBuildkitManager
	mu                    sync.RWMutex
	running               bool
	stopChan              chan struct{}
	runCtx                context.Context
	inFlight              sync.Map
	runningTasks          sync.Map
	cancelledTasks        sync.Map
	pendingMu             sync.Mutex
	pendingWS             []pendingWebSocketMessage
	taskSlots             chan struct{}
	runtimeAgentCfg       client.AgentConfig
}

// pendingWebSocketMessage stores one outbound WS message that could not be sent
// immediately and therefore must be replayed after reconnect.
type pendingWebSocketMessage struct {
	messageType string
	payload     map[string]interface{}
}

type runningTaskExecution struct {
	cancel    context.CancelFunc
	cancelled atomic.Bool
}

// NewTaskHandler creates a new task handler
func NewTaskHandler(httpClient *client.HTTPClient, wsClient *client.WebSocketClient, cfg *config.Config, tokenMgr *TokenManager, runtimeCaps system.RuntimeCapabilities, log *logrus.Logger) *TaskHandler {
	executor := task.NewExecutor(log, cfg.GetWorkspacePath(), runtimeCaps)
	return &TaskHandler{
		httpClient:       httpClient,
		wsClient:         wsClient,
		cfg:              cfg,
		tokenMgr:         tokenMgr,
		log:              log,
		executor:         executor,
		embeddedBuildkit: task.NewEmbeddedBuildkitManager(log, cfg.GetWorkspacePath(), runtimeCaps),
		stopChan:         make(chan struct{}),
		taskSlots:        make(chan struct{}, defaultTaskConcurrencyLimit()),
	}
}

func (th *TaskHandler) WorkspaceManager() *task.WorkspaceManager {
	if th == nil || th.executor == nil {
		return nil
	}
	return th.executor.WorkspaceManager()
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

func defaultTaskConcurrencyLimit() int {
	return 5
}

func normalizeDockerHubMirrors(value any) []string {
	mirrors := make([]string, 0)
	appendMirror := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		for _, existing := range mirrors {
			if existing == raw {
				return
			}
		}
		mirrors = append(mirrors, raw)
	}
	switch v := value.(type) {
	case []string:
		for _, item := range v {
			appendMirror(item)
		}
	case []any:
		for _, item := range v {
			appendMirror(fmt.Sprint(item))
		}
	case string:
		for _, item := range strings.Split(v, ",") {
			appendMirror(item)
		}
	}
	return mirrors
}

func (th *TaskHandler) getTaskConcurrencyLimit(agentCfg client.AgentConfig) int {
	if agentCfg.TaskConcurrency > 0 {
		return agentCfg.TaskConcurrency
	}
	return defaultTaskConcurrencyLimit()
}

func (th *TaskHandler) updateTaskConcurrency(agentCfg client.AgentConfig) {
	limit := th.getTaskConcurrencyLimit(agentCfg)
	if limit <= 0 {
		limit = defaultTaskConcurrencyLimit()
	}
	th.mu.Lock()
	defer th.mu.Unlock()
	th.runtimeAgentCfg = agentCfg
	if th.taskSlots != nil && cap(th.taskSlots) == limit {
		return
	}
	th.taskSlots = make(chan struct{}, limit)
}

func (th *TaskHandler) updateRuntimeAgentConfig(agentCfg client.AgentConfig) {
	th.mu.Lock()
	th.runtimeAgentCfg = agentCfg
	th.mu.Unlock()
}

func (th *TaskHandler) runtimeAgentConfig() client.AgentConfig {
	th.mu.RLock()
	defer th.mu.RUnlock()
	return th.runtimeAgentCfg
}

func (th *TaskHandler) withTaskSlot(run func()) {
	th.mu.RLock()
	taskSlots := th.taskSlots
	th.mu.RUnlock()
	if taskSlots == nil {
		run()
		return
	}
	taskSlots <- struct{}{}
	defer func() { <-taskSlots }()
	run()
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

	if _, cancelled := th.cancelledTasks.Load(t.ID); cancelled {
		th.log.Infof("Skip cancelled task assignment: task_id=%d", t.ID)
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
	taskCtx, cancel := context.WithCancel(ctx)
	execution := &runningTaskExecution{cancel: cancel}
	th.runningTasks.Store(t.ID, execution)

	go func(task Task) {
		defer th.inFlight.Delete(task.ID)
		defer th.runningTasks.Delete(task.ID)
		defer th.cancelledTasks.Delete(task.ID)
		defer cancel()
		th.withTaskSlot(func() {
			th.executeTask(taskCtx, &task)
		})
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
	th.updateTaskConcurrency(msg.AgentConfig)

	go func() {
		if err := th.executePipeline(msg); err != nil {
			th.log.Errorf("Failed to execute pipeline run_id=%d: %v", msg.RunID, err)
		}
	}()

	return nil
}

// HandleTaskCancel handles task cancellation from server via WebSocket
func (th *TaskHandler) HandleTaskCancel(taskID uint64) error {
	if th.log != nil {
		th.log.Infof("Handling task cancellation: task_id=%d", taskID)
	}
	if value, ok := th.runningTasks.Load(taskID); ok {
		execution, ok := value.(*runningTaskExecution)
		if !ok || execution == nil || execution.cancel == nil {
			return fmt.Errorf("task %d has no cancellable execution", taskID)
		}
		execution.cancelled.Store(true)
		execution.cancel()
		return nil
	}
	th.cancelledTasks.Store(taskID, struct{}{})
	if _, ok := th.inFlight.Load(taskID); ok {
		return nil
	}
	return fmt.Errorf("task %d is not running", taskID)
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
	dagEngine.SetLogCallback(func(taskID uint64, level, message, source string, _ int) {
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
		nodeTypes := make(map[string]string)
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
				nodeTypes[nid] = n.Type
				resultMu.Unlock()
			}(node, nodeID)
		}

		wg.Wait()

		for nodeID, result := range nodeResults {
			taskType := nodeTypes[nodeID]
			outputs := th.buildNodeOutputs(nodeID, taskType, result)
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
		th.executor.SetLogCallback(func(logTaskID uint64, level, message, source string, lineNumber int) {
			// Only report logs for the current task (supports parallel execution)
			if logTaskID != params.TaskID {
				return
			}
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

// buildNodeOutputs constructs the output map for a completed task node.
// This output map is stored in AgentTask.ResultData and used by downstream tasks
// via the ${outputs.<node_id>.<field>} variable substitution syntax.
//
// Output structure (common fields):
//   - exit_code: task execution exit code
//   - duration: execution time in seconds
//   - error: error message if task failed (optional)
//
// Type-specific fields are extracted based on taskType (see extractTypeSpecificOutputs).
func (th *TaskHandler) buildNodeOutputs(nodeID, taskType string, result *task.Result) map[string]interface{} {
	outputs := map[string]interface{}{
		"exit_code": result.ExitCode,
		"duration":  result.Duration.Seconds(),
	}

	if result.Error != "" {
		outputs["error"] = result.Error
	}

	th.extractTypeSpecificOutputs(taskType, result, outputs)

	return outputs
}

// extractTypeSpecificOutputs dispatches to type-specific extractors based on taskType.
// Each extractor parses the task's stdout to extract type-specific output fields.
func (th *TaskHandler) extractTypeSpecificOutputs(taskType string, result *task.Result, outputs map[string]interface{}) {
	switch taskType {
	case "git_clone":
		// git_clone 任务：从 stdout 中提取 git 信息
		th.extractGitCloneOutputs(result.Stdout, outputs)
	case "docker":
		// docker 任务：从 stdout 中提取镜像信息
		th.extractDockerOutputs(result.Stdout, outputs)
	case "npm", "maven", "gradle":
		// 构建任务：提取构建产物路径等
		th.extractBuildOutputs(result.Stdout, outputs)
	case "unit", "integration", "e2e":
		// 测试任务：提取测试结果
		th.extractTestOutputs(result.Stdout, outputs)
	case "coverage":
		// 覆盖率任务
		th.extractCoverageOutputs(result.Stdout, outputs)
	case "docker-run":
		// docker-run 任务：提取容器信息
		th.extractDockerRunOutputs(result.Stdout, outputs)
	}
}

// extractGitCloneOutputs parses git_clone stdout for git_commit, git_repo_url, git_ref, git_checkout_path.
// Expected stdout format: git_info:{"url":"...","git_ref":"...","commit":"...","path":"..."}
func (th *TaskHandler) extractGitCloneOutputs(stdout string, outputs map[string]interface{}) {
	if strings.Contains(stdout, "git_info") {
		// 解析 git info JSON
		lines := strings.Split(stdout, "\n")
		for _, line := range lines {
			if strings.Contains(line, "git_info") {
				// 尝试提取 JSON 部分
				if idx := strings.Index(line, "{"); idx >= 0 {
					jsonStr := line[idx:]
					var gitInfo map[string]interface{}
					if err := json.Unmarshal([]byte(jsonStr), &gitInfo); err != nil {
						continue
					}
					if value := strings.TrimSpace(fmt.Sprint(gitInfo["git_commit"])); value != "" && value != "<nil>" {
						outputs["git_commit"] = value
					}
					if value := strings.TrimSpace(fmt.Sprint(gitInfo["git_repo_url"])); value != "" && value != "<nil>" {
						outputs["git_repo_url"] = value
					}
					if value := strings.TrimSpace(fmt.Sprint(gitInfo["git_ref"])); value != "" && value != "<nil>" {
						outputs["git_ref"] = value
					}
					if value := strings.TrimSpace(fmt.Sprint(gitInfo["git_checkout_path"])); value != "" && value != "<nil>" {
						outputs["git_checkout_path"] = value
					}
				}
			}
		}
	}
}

// extractDockerOutputs parses docker build stdout for image_name, image_tag, image_full_name, pushed.
// Looks for [easydo][info] image=<name:tag> format in stdout.
func (th *TaskHandler) extractDockerOutputs(stdout string, outputs map[string]interface{}) {
	if strings.Contains(stdout, "image=") {
		lines := strings.Split(stdout, "\n")
		for _, line := range lines {
			if strings.Contains(line, "image=") && strings.Contains(line, "[easydo][info]") {
				// 格式: [easydo][info] image=myapp:tag
				if idx := strings.Index(line, "image="); idx >= 0 {
					imageStr := strings.TrimSpace(line[idx+6:])
					parts := strings.Split(imageStr, ":")
					if len(parts) >= 2 {
						outputs["image_name"] = parts[0]
						outputs["image_tag"] = parts[1]
						outputs["image_full_name"] = imageStr
					} else {
						outputs["image_name"] = imageStr
						outputs["image_tag"] = "latest"
						outputs["image_full_name"] = imageStr + ":latest"
					}
				}
			}
		}
	}
	// 检查是否已推送
	if strings.Contains(stdout, "docker push") || strings.Contains(stdout, "pushed to") {
		outputs["pushed"] = true
	} else {
		outputs["pushed"] = false
	}
}

// extractBuildOutputs parses build task stdout for artifact_path if present.
func (th *TaskHandler) extractBuildOutputs(stdout string, outputs map[string]interface{}) {
	// 构建任务默认输出 exit_code 和 duration
	// 产物路径等需要从 stdout 中提取
	if strings.Contains(stdout, "artifact_path=") {
		lines := strings.Split(stdout, "\n")
		for _, line := range lines {
			if strings.Contains(line, "artifact_path=") {
				if idx := strings.Index(line, "artifact_path="); idx >= 0 {
					outputs["artifact_path"] = strings.TrimSpace(line[idx+13:])
				}
			}
		}
	}
}

// extractTestOutputs parses test task stdout for tests_passed, tests_failed, tests_skipped.
func (th *TaskHandler) extractTestOutputs(stdout string, outputs map[string]interface{}) {
	// 尝试从 stdout 中提取测试结果统计
	// 格式可能是: tests: 10 passed, 2 failed, 1 skipped
	if strings.Contains(stdout, "passed") {
		outputs["tests_passed"] = th.extractNumber(stdout, "passed")
	}
	if strings.Contains(stdout, "failed") {
		outputs["tests_failed"] = th.extractNumber(stdout, "failed")
	}
	if strings.Contains(stdout, "skipped") {
		outputs["tests_skipped"] = th.extractNumber(stdout, "skipped")
	}
}

// extractCoverageOutputs parses coverage task stdout for coverage_percentage.
func (th *TaskHandler) extractCoverageOutputs(stdout string, outputs map[string]interface{}) {
	// 格式可能是: coverage: 85.5%
	if strings.Contains(stdout, "coverage") {
		outputs["coverage_percentage"] = th.extractNumber(stdout, "coverage")
	}
}

// extractDockerRunOutputs parses docker-run stdout for container_id and image_ref.
func (th *TaskHandler) extractDockerRunOutputs(stdout string, outputs map[string]interface{}) {
	// docker-run 任务：提取容器 ID 和名称
	if strings.Contains(stdout, "container_id=") {
		lines := strings.Split(stdout, "\n")
		for _, line := range lines {
			if strings.Contains(line, "container_id=") {
				if idx := strings.Index(line, "container_id="); idx >= 0 {
					outputs["container_id"] = strings.TrimSpace(line[idx+12:])
				}
			}
		}
	}
	if strings.Contains(stdout, "image_ref=") {
		lines := strings.Split(stdout, "\n")
		for _, line := range lines {
			if strings.Contains(line, "image_ref=") {
				if idx := strings.Index(line, "image_ref="); idx >= 0 {
					outputs["image_ref"] = strings.TrimSpace(line[idx+10:])
				}
			}
		}
	}
}

// extractJSONField extracts a string or number field value from JSON using regex.
// Returns the captured value or empty string if not found.
func (th *TaskHandler) extractJSONField(json, field string) string {
	pattern := fmt.Sprintf(`"%s"\s*:\s*"([^"]*)"`, field)
	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(json)
	if len(matches) >= 2 {
		return matches[1]
	}
	// 尝试数字类型
	pattern = fmt.Sprintf(`"%s"\s*:\s*([0-9.]+)`, field)
	re = regexp.MustCompile(pattern)
	matches = re.FindStringSubmatch(json)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

// extractNumber finds the first integer after keyword in string.
// Example: extractNumber("passed: 42", "passed") returns 42.
func (th *TaskHandler) extractNumber(s, keyword string) int {
	pattern := fmt.Sprintf(`%s\s*[:\s]*([0-9]+)`, keyword)
	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(s)
	if len(matches) >= 2 {
		n, _ := strconv.Atoi(matches[1])
		return n
	}
	return 0
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

	if th.embeddedBuildkit != nil {
		if err := th.embeddedBuildkit.Stop(); err != nil {
			th.log.Warnf("Failed to stop embedded buildkit: %v", err)
		}
	}
	close(th.stopChan)
}

// executeTask executes a single task
func (th *TaskHandler) executeTask(ctx context.Context, task *Task) {
	th.log.Infof("Executing task: id=%d, name=%s, type=%s", task.ID, task.Name, task.TaskType)

	attempt := task.RetryCount + 1

	// Parse task parameters
	params, err := task.ParseParamsWithRuntimeConfig(th.runtimeAgentConfig())
	if err != nil {
		th.log.Warnf("Failed to parse task params: %v", err)
		return
	}
	if params.TaskType == "docker" && th.embeddedBuildkit != nil {
		mirrors := normalizeDockerHubMirrors(params.Params["dockerhub_mirrors"])
		if err := th.embeddedBuildkit.EnsureRunning(mirrors); err != nil {
			th.log.Warnf("Failed to prepare embedded buildkit: %v", err)
			return
		}
		th.executor.SetEmbeddedBuildkitEnv(th.embeddedBuildkit.Env())
	}

	if _, cancelled := th.cancelledTasks.Load(task.ID); cancelled {
		th.log.Infof("Task %d was cancelled before execution started", task.ID)
		return
	}

	// Report task as running first. If this fails, keep task pending server-side and wait for redispatch.
	if err := th.reportTaskUpdateV2(task, attempt, "running", 0, "", 0, nil); err != nil {
		th.log.Warnf("Failed to report v2 task start: %v", err)
		return
	}

	// Set up log callback for real-time log reporting
	var logSeq int64
	th.executor.SetLogCallback(func(logTaskID uint64, level, message, source string, _ int) {
		// Only report logs for the current task (supports parallel execution)
		if logTaskID != task.ID {
			return
		}
		seq := atomic.AddInt64(&logSeq, 1)
		if err := th.reportTaskLogChunkV2(task, attempt, seq, source, message); err != nil {
			th.log.Warnf("Failed to report v2 log: %v", err)
		}
	})

	// Execute the task with workspace support
	result := th.executor.Execute(ctx, *params)

	if value, ok := th.runningTasks.Load(task.ID); ok {
		if execution, ok := value.(*runningTaskExecution); ok && execution != nil && execution.cancelled.Load() {
			if err := th.reportTaskLogEndV2(task, attempt, atomic.LoadInt64(&logSeq)); err != nil {
				th.log.Debugf("Failed to report v2 log end: %v", err)
			}
			th.log.Infof("Task %d stopped after cancellation", task.ID)
			return
		}
	}

	finalResult, status, errorMsg := th.buildTaskResultPayload(task, result)
	if err := th.reportTaskUpdateV2(task, attempt, status, result.ExitCode, errorMsg, result.Duration.Milliseconds(), finalResult); err != nil {
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
	return t.ParseParamsWithRuntimeConfig(client.AgentConfig{})
}

func (t *Task) ParseParamsWithRuntimeConfig(agentCfg client.AgentConfig) (*task.TaskParams, error) {
	params := &task.TaskParams{
		TaskID:        t.ID,
		PipelineRunID: t.PipelineRunID,
		NodeID:        t.NodeID,
		TaskType:      t.TaskType,
		Name:          t.Name,
		Params:        task.ParseStructuredParamsJSON(t.Params),
		Script:        t.Script,
		WorkDir:       t.WorkDir,
		Timeout:       t.Timeout,
	}
	if params.Params == nil {
		params.Params = map[string]any{}
	}
	if t.TaskType == "docker" && len(agentCfg.DockerHubMirrors) > 0 {
		mirrors := make([]any, 0, len(agentCfg.DockerHubMirrors))
		for _, mirror := range agentCfg.DockerHubMirrors {
			mirrors = append(mirrors, mirror)
		}
		params.Params["dockerhub_mirrors"] = mirrors
	}

	// Parse environment variables
	if t.EnvVars != "" {
		params.EnvVars = task.ParseEnvVarsJSON(t.EnvVars)
	}

	return params, nil
}

func (th *TaskHandler) buildTaskResultPayload(task *Task, result *task.Result) (map[string]interface{}, string, string) {
	status := "execute_success"
	errorMsg := ""
	if result != nil {
		errorMsg = result.Error
		if result.Error != "" || result.ExitCode != 0 {
			status = "execute_failed"
		}
	}
	payload := map[string]interface{}{}
	if result != nil {
		payload["stdout_size"] = len(result.Stdout)
		payload["stderr_size"] = len(result.Stderr)
		for k, v := range result.StructuredOutput {
			payload[k] = v
		}
	}
	if result == nil {
		return payload, status, errorMsg
	}
	if shouldEmbedTaskStdStreams(task) {
		payload["stdout"] = result.Stdout
		payload["stderr"] = result.Stderr
	}
	// 获取任务输出变量
	if status == "execute_success" {
		taskOutputs := getTaskOutputs(task, result)
		for k, v := range taskOutputs {
			payload[k] = v
		}
	}
	if !isResourceBaseInfoTask(task) {
		return payload, status, errorMsg
	}
	hasVMMarkers := strings.Contains(result.Stdout, "EASYDO_BASE_INFO_BEGIN")
	hasK8sMarkers := strings.Contains(result.Stdout, "EASYDO_K8S_VERSION_BEGIN") && strings.Contains(result.Stdout, "EASYDO_K8S_NODES_BEGIN")
	if status == "execute_success" && !hasVMMarkers && !hasK8sMarkers {
		status = "execute_failed"
		if errorMsg == "" {
			errorMsg = "基础资源采集结果格式无效"
		}
	}
	return payload, status, errorMsg
}

// getTaskOutputs 根据任务类型获取输出变量
func getTaskOutputs(t *Task, result *task.Result) map[string]interface{} {
	outputs := make(map[string]interface{})

	if t == nil {
		return outputs
	}
	if isAITaskOutputTask(t) {
		return getAITaskOutputs(result)
	}

	switch t.TaskType {
	case "git_clone":
		return getGitCloneOutputs(t)
	case "docker":
		return getDockerOutputs(t)
	case "npm":
		return getNpmOutputs(t)
	case "maven":
		return getMavenOutputs(t)
	case "gradle":
		return getGradleOutputs(t)
	case "shell":
		return getShellOutputs(t, result)
	}
	return outputs
}

func isAITaskOutputTask(t *Task) bool {
	if t == nil {
		return false
	}
	return task.IsAITaskPayload(t.TaskType, task.ParseStructuredParamsJSON(t.Params))
}

func getAITaskOutputs(result *task.Result) map[string]interface{} {
	outputs := make(map[string]interface{})
	if result == nil {
		return outputs
	}
	if len(result.StructuredOutput) > 0 {
		for k, v := range result.StructuredOutput {
			outputs[k] = v
		}
		return outputs
	}
	if strings.TrimSpace(result.Stdout) == "" {
		return outputs
	}
	_ = json.Unmarshal([]byte(result.Stdout), &outputs)
	return outputs
}

// isGitCloneTask checks if the task type is git_clone
func isGitCloneTask(t *Task) bool {
	if t == nil {
		return false
	}
	return t.TaskType == "git_clone"
}

func getGitCloneOutputs(t *Task) map[string]interface{} {
	outputs := make(map[string]interface{})

	params := task.ParseStructuredParamsJSON(t.Params)

	checkoutPath, ok := params["git_checkout_path"].(string)
	if !ok || checkoutPath == "" {
		checkoutPath = "./app"
	}

	targetDir := checkoutPath
	if !filepath.IsAbs(targetDir) {
		targetDir = filepath.Join("/data/agent/workspace", fmt.Sprintf("workspace_%d", t.PipelineRunID), targetDir)
	}

	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		return outputs
	}

	if cmd := exec.Command("git", "-C", targetDir, "rev-parse", "HEAD"); cmd != nil {
		if out, err := cmd.Output(); err == nil {
			commitSHA := strings.TrimSpace(string(out))
			if commitSHA != "" {
				outputs["git_commit"] = commitSHA
			}
		}
	}

	if cmd := exec.Command("git", "-C", targetDir, "rev-parse", "--short", "HEAD"); cmd != nil {
		if out, err := cmd.Output(); err == nil {
			shortCommitSHA := strings.TrimSpace(string(out))
			if shortCommitSHA != "" {
				outputs["git_commit_short"] = shortCommitSHA
			}
		}
	}

	if cmd := exec.Command("git", "-C", targetDir, "rev-parse", "--abbrev-ref", "HEAD"); cmd != nil {
		if out, err := cmd.Output(); err == nil {
			ref := strings.TrimSpace(string(out))
			if ref != "" {
				outputs["git_ref"] = ref
			}
		}
	}

	if cmd := exec.Command("git", "-C", targetDir, "remote", "get-url", "origin"); cmd != nil {
		if out, err := cmd.Output(); err == nil {
			repoURL := strings.TrimSpace(string(out))
			if repoURL != "" {
				outputs["git_repo_url"] = repoURL
			}
		}
	}

	outputs["git_checkout_path"] = checkoutPath

	return outputs
}

// getDockerOutputs 获取 docker 任务的输出变量
// 从任务参数中获取镜像信息
func getDockerOutputs(t *Task) map[string]interface{} {
	outputs := make(map[string]interface{})

	params := task.ParseStructuredParamsJSON(t.Params)
	if len(params) == 0 {
		return outputs
	}

	var imageName, imageTag, registry string

	if v, ok := params["image_name"].(string); ok && v != "" {
		imageName = v
		outputs["image_name"] = imageName
	}

	if v, ok := params["image_tag"].(string); ok && v != "" {
		imageTag = v
	} else {
		imageTag = "latest"
	}
	outputs["image_tag"] = imageTag

	if v, ok := params["registry"].(string); ok && v != "" {
		registry = v
		outputs["registry"] = registry
	}

	if dockerfile, ok := params["dockerfile"].(string); ok && dockerfile != "" {
		outputs["dockerfile"] = dockerfile
	}

	if context, ok := params["context"].(string); ok && context != "" {
		outputs["context"] = context
	}

	if imageName != "" {
		fullName := imageName + ":" + imageTag
		if registry != "" {
			fullName = registry + "/" + fullName
		}
		outputs["image_full_name"] = fullName
	}

	if push, ok := params["push"].(bool); ok {
		outputs["pushed"] = push
	} else {
		outputs["pushed"] = false
	}

	return outputs
}

// getNpmOutputs 获取 npm 任务的输出变量
// 从 package.json 中获取包信息
func getNpmOutputs(t *Task) map[string]interface{} {
	outputs := make(map[string]interface{})

	params := task.ParseStructuredParamsJSON(t.Params)
	if len(params) == 0 {
		return outputs
	}

	// 获取工作目录
	workDir, ok := params["working_dir"].(string)
	if !ok || workDir == "" {
		workDir = "."
	}

	// 如果是相对路径，转换为绝对路径
	if !filepath.IsAbs(workDir) {
		workDir = filepath.Join("/data/agent/workspace", fmt.Sprintf("workspace_%d", t.PipelineRunID), workDir)
	}

	// 读取 package.json
	packageJSONPath := filepath.Join(workDir, "package.json")
	if _, err := os.Stat(packageJSONPath); os.IsNotExist(err) {
		return outputs
	}

	data, err := os.ReadFile(packageJSONPath)
	if err != nil {
		return outputs
	}

	var packageJSON map[string]interface{}
	if err := json.Unmarshal(data, &packageJSON); err != nil {
		return outputs
	}

	if name, ok := packageJSON["name"].(string); ok && name != "" {
		outputs["package_name"] = name
	}

	if version, ok := packageJSON["version"].(string); ok && version != "" {
		outputs["package_version"] = version
	}

	return outputs
}

// getMavenOutputs 获取 maven 任务的输出变量
// 从 pom.xml 中获取项目信息
func getMavenOutputs(t *Task) map[string]interface{} {
	outputs := make(map[string]interface{})

	params := task.ParseStructuredParamsJSON(t.Params)
	if len(params) == 0 {
		return outputs
	}

	// 获取工作目录
	workDir, ok := params["working_dir"].(string)
	if !ok || workDir == "" {
		workDir = "."
	}

	// 如果是相对路径，转换为绝对路径
	if !filepath.IsAbs(workDir) {
		workDir = filepath.Join("/data/agent/workspace", fmt.Sprintf("workspace_%d", t.PipelineRunID), workDir)
	}

	// 读取 pom.xml
	pomPath := filepath.Join(workDir, "pom.xml")
	if _, err := os.Stat(pomPath); os.IsNotExist(err) {
		return outputs
	}

	data, err := os.ReadFile(pomPath)
	if err != nil {
		return outputs
	}

	content := string(data)

	// 简单的 XML 解析获取 artifactId 和 version
	if artifactID := extractXMLValue(content, "artifactId"); artifactID != "" {
		outputs["artifact_id"] = artifactID
	}

	if version := extractXMLValue(content, "version"); version != "" {
		outputs["version"] = version
	}

	if groupID := extractXMLValue(content, "groupId"); groupID != "" {
		outputs["group_id"] = groupID
	}

	return outputs
}

// getGradleOutputs 获取 gradle 任务的输出变量
// 从 build.gradle 中获取项目信息
func getGradleOutputs(t *Task) map[string]interface{} {
	outputs := make(map[string]interface{})

	params := task.ParseStructuredParamsJSON(t.Params)
	if len(params) == 0 {
		return outputs
	}

	// 获取工作目录
	workDir, ok := params["working_dir"].(string)
	if !ok || workDir == "" {
		workDir = "."
	}

	// 如果是相对路径，转换为绝对路径
	if !filepath.IsAbs(workDir) {
		workDir = filepath.Join("/data/agent/workspace", fmt.Sprintf("workspace_%d", t.PipelineRunID), workDir)
	}

	// 读取 build.gradle
	buildGradlePath := filepath.Join(workDir, "build.gradle")
	if _, err := os.Stat(buildGradlePath); os.IsNotExist(err) {
		// 尝试 build.gradle.kts
		buildGradlePath = filepath.Join(workDir, "build.gradle.kts")
		if _, err := os.Stat(buildGradlePath); os.IsNotExist(err) {
			return outputs
		}
	}

	data, err := os.ReadFile(buildGradlePath)
	if err != nil {
		return outputs
	}

	content := string(data)

	// 简单的文本解析获取 group 和 version
	if group := extractGradleValue(content, "group"); group != "" {
		outputs["group"] = group
	}

	if version := extractGradleValue(content, "version"); version != "" {
		outputs["version"] = version
	}

	if artifactID := extractGradleValue(content, "rootProject.name"); artifactID != "" {
		outputs["artifact_id"] = artifactID
	}

	return outputs
}

// getShellOutputs 获取 shell 任务的输出变量
// 支持从环境变量或文件中读取输出
func getShellOutputs(t *Task, result *task.Result) map[string]interface{} {
	outputs := make(map[string]interface{})

	// 从环境变量 EASYDO_OUTPUT 中读取输出
	if outputJSON := os.Getenv("EASYDO_OUTPUT"); outputJSON != "" {
		var outputMap map[string]interface{}
		if err := json.Unmarshal([]byte(outputJSON), &outputMap); err == nil {
			for k, v := range outputMap {
				outputs[k] = v
			}
		}
	}

	// 从工作目录中的 .easydo_output.json 文件读取输出
	params := task.ParseStructuredParamsJSON(t.Params)
	workDir, ok := params["working_dir"].(string)
	if !ok || workDir == "" {
		workDir = "."
	}

	if !filepath.IsAbs(workDir) {
		workDir = filepath.Join("/data/agent/workspace", fmt.Sprintf("workspace_%d", t.PipelineRunID), workDir)
	}

	outputFile := filepath.Join(workDir, ".easydo_output.json")
	if _, err := os.Stat(outputFile); err == nil {
		if data, err := os.ReadFile(outputFile); err == nil {
			var fileOutput map[string]interface{}
			if err := json.Unmarshal(data, &fileOutput); err == nil {
				for k, v := range fileOutput {
					outputs[k] = v
				}
			}
		}
	}

	return outputs
}

// extractXMLValue 从 XML 内容中提取指定标签的值
func extractXMLValue(content, tag string) string {
	startTag := "<" + tag + ">"
	endTag := "</" + tag + ">"

	startIdx := strings.Index(content, startTag)
	if startIdx == -1 {
		return ""
	}

	startIdx += len(startTag)
	endIdx := strings.Index(content[startIdx:], endTag)
	if endIdx == -1 {
		return ""
	}

	return strings.TrimSpace(content[startIdx : startIdx+endIdx])
}

// extractGradleValue 从 Gradle 配置中提取指定属性的值
func extractGradleValue(content, key string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, key+" ") || strings.HasPrefix(line, key+"=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				value := strings.TrimSpace(parts[1])
				// 移除引号
				value = strings.Trim(value, "'\"")
				return value
			}
			parts = strings.SplitN(line, " ", 2)
			if len(parts) == 2 {
				value := strings.TrimSpace(parts[1])
				value = strings.Trim(value, "'\"")
				return value
			}
		}
	}
	return ""
}

func shouldEmbedTaskStdStreams(assignedTask *Task) bool {
	if assignedTask == nil {
		return false
	}
	params := task.ParseStructuredParamsJSON(assignedTask.Params)
	if len(params) == 0 {
		return false
	}
	if isResourceBaseInfoTask(assignedTask) {
		return true
	}
	k8s, ok := params["k8s"].(map[string]interface{})
	if !ok {
		return false
	}
	kind, _ := k8s["kind"].(string)
	return kind == "resource_k8s_namespace_query" || kind == "resource_k8s_resource_query"
}

func isResourceBaseInfoTask(assignedTask *Task) bool {
	if assignedTask == nil {
		return false
	}
	params := task.ParseStructuredParamsJSON(assignedTask.Params)
	if len(params) == 0 {
		return false
	}
	collection, ok := params["collection"].(map[string]interface{})
	if !ok {
		return false
	}
	kind, _ := collection["kind"].(string)
	return kind == "resource_base_info_refresh"
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

	const taskUpdateAckTimeout = 20 * time.Second
	if err := wsClient.SendMessageWithAck("task_update_v2", payload, taskUpdateAckTimeout); err != nil {
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
			ID:            n.ID,
			Type:          n.Type,
			Name:          n.Name,
			Config:        n.Config,
			Params:        n.Params,
			Timeout:       n.Timeout,
			IgnoreFailure: n.IgnoreFailure,
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
			From: e.From,
			To:   e.To,
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
