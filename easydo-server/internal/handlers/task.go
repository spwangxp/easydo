package handlers

import (
	"bytes"
	"context"
	"easydo-server/internal/models"
	"easydo-server/pkg/utils"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type TaskHandler struct {
	DB *gorm.DB
}

type taskScheduleListItem struct {
	ID            uint64    `json:"id"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	AgentID       uint64    `json:"agent_id"`
	PipelineRunID uint64    `json:"pipeline_run_id"`
	NodeID        string    `json:"node_id"`
	TaskType      string    `json:"task_type"`
	Name          string    `json:"name"`
	Params        string    `json:"params"`
	Script        string    `json:"script"`
	WorkDir       string    `json:"work_dir"`
	EnvVars       string    `json:"env_vars"`
	Status        string    `json:"status"`
	Priority      int       `json:"priority"`
	Timeout       int       `json:"timeout"`
	RetryCount    int       `json:"retry_count"`
	MaxRetries    int       `json:"max_retries"`
	ExitCode      int       `json:"exit_code"`
	ErrorMsg      string    `json:"error_msg"`
	StartTime     int64     `json:"start_time"`
	EndTime       int64     `json:"end_time"`
	Duration      int       `json:"duration"`
	ResultData    string    `json:"result_data"`
	RepoURL       string    `json:"repo_url"`
	RepoBranch    string    `json:"repo_branch"`
	RepoCommit    string    `json:"repo_commit"`
	RepoPath      string    `json:"repo_path"`
	CreatedBy     uint64    `json:"created_by"`

	PipelineID   uint64 `json:"pipeline_id"`
	PipelineName string `json:"pipeline_name"`
	BuildNumber  int    `json:"build_number"`
	RunStatus    string `json:"run_status"`
	TriggerUser  string `json:"trigger_user"`
	AgentName    string `json:"agent_name"`
}

func NewTaskHandler() *TaskHandler {
	return &TaskHandler{DB: models.DB}
}

// CreateTask creates a new task for an agent
func (h *TaskHandler) CreateTask(c *gin.Context) {
	var req struct {
		AgentID       uint64  `json:"agent_id" binding:"required"`
		PipelineRunID *uint64 `json:"pipeline_run_id"` // Use pointer to distinguish 0 from not provided
		NodeID        string  `json:"node_id"`
		TaskType      string  `json:"task_type" binding:"required"`
		Name          string  `json:"name"`
		Params        string  `json:"params"`
		Script        string  `json:"script"`
		WorkDir       string  `json:"work_dir"`
		EnvVars       string  `json:"env_vars"`
		Priority      int     `json:"priority"`
		Timeout       int     `json:"timeout"`
		MaxRetries    int     `json:"max_retries"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		fmt.Printf("[DEBUG] Task report binding error: %v, body: %s\n", err, c.Request.Body)
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数错误: " + err.Error(),
		})
		return
	}

	// Verify agent exists and is online
	var agent models.Agent
	if err := h.DB.First(&agent, req.AgentID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Agent不存在",
		})
		return
	}

	if agent.Status != models.AgentStatusOnline && agent.Status != models.AgentStatusBusy {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Agent当前不可用",
		})
		return
	}

	workspaceID := c.GetUint64("workspace_id")
	role := c.GetString("role")
	if !agentVisibleInWorkspace(&agent, workspaceID, role) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "无权使用该执行器",
		})
		return
	}

	// Get user ID from JWT
	userID, _ := c.Get("user_id")
	createdBy := uint64(0)
	if uid, ok := userID.(uint64); ok {
		createdBy = uid
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 3600 // Default 1 hour
	}

	maxRetries := req.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	task := &models.AgentTask{
		WorkspaceID: workspaceID,
		AgentID:     req.AgentID,
		NodeID:      req.NodeID,
		TaskType:    req.TaskType,
		Name:        req.Name,
		Params:      req.Params,
		Script:      req.Script,
		WorkDir:     req.WorkDir,
		EnvVars:     req.EnvVars,
		Status:      models.TaskStatusQueued,
		Priority:    req.Priority,
		Timeout:     timeout,
		MaxRetries:  maxRetries,
		CreatedBy:   createdBy,
	}

	if err := h.DB.Omit("PipelineRunID").Create(task).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建任务失败: " + err.Error(),
		})
		return
	}

	// Update agent status to busy if needed
	h.DB.Model(&agent).Update("status", models.AgentStatusBusy)
	_ = SharedWebSocketHandler().sendTaskAssign(*task)

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"task_id": task.ID,
			"status":  task.Status,
		},
	})
}

// GetTaskList returns list of tasks
func (h *TaskHandler) GetTaskList(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}

	agentID := c.Query("agent_id")
	status := c.Query("status")
	pipelineRunID := c.Query("pipeline_run_id")
	runStatus := c.Query("run_status")
	keyword := strings.TrimSpace(c.Query("keyword"))
	includeSchedule := c.Query("include_schedule") == "1" || strings.EqualFold(c.Query("include_schedule"), "true")
	workspaceID := c.GetUint64("workspace_id")

	useScheduleQuery := includeSchedule || runStatus != "" || keyword != ""
	if useScheduleQuery {
		applyFilters := func(query *gorm.DB) *gorm.DB {
			query = query.Where("t.workspace_id = ?", workspaceID)
			if agentID != "" {
				query = query.Where("t.agent_id = ?", agentID)
			}
			if status != "" {
				query = query.Where("t.status = ?", status)
			}
			if pipelineRunID != "" {
				query = query.Where("t.pipeline_run_id = ?", pipelineRunID)
			}
			if runStatus != "" {
				query = query.Where("pr.status = ?", runStatus)
			}
			if keyword != "" {
				like := "%" + keyword + "%"
				query = query.Where(
					"(t.name LIKE ? OR t.node_id LIKE ? OR p.name LIKE ? OR a.name LIKE ?)",
					like, like, like, like,
				)
			}
			return query
		}

		baseJoin := func(query *gorm.DB) *gorm.DB {
			return query.
				Joins("LEFT JOIN pipeline_runs pr ON pr.id = t.pipeline_run_id").
				Joins("LEFT JOIN pipelines p ON p.id = pr.pipeline_id").
				Joins("LEFT JOIN agents a ON a.id = t.agent_id")
		}

		var total int64
		countQuery := applyFilters(baseJoin(h.DB.Table("agent_tasks AS t")))
		countQuery.Count(&total)

		offset := (page - 1) * pageSize
		var tasks []taskScheduleListItem
		listQuery := applyFilters(baseJoin(h.DB.Table("agent_tasks AS t")))
		if err := listQuery.
			Select(`
				t.id,
				t.created_at,
				t.updated_at,
				t.agent_id,
				t.pipeline_run_id,
				t.node_id,
				t.task_type,
				t.name,
				t.params,
				t.script,
				t.work_dir,
				t.env_vars,
				t.status,
				t.priority,
				t.timeout,
				t.retry_count,
				t.max_retries,
				t.exit_code,
				t.error_msg,
				t.start_time,
				t.end_time,
				t.duration,
				t.result_data,
				t.repo_url,
				t.repo_branch,
				t.repo_commit,
				t.repo_path,
				t.created_by,
				COALESCE(pr.pipeline_id, 0) AS pipeline_id,
				COALESCE(p.name, '') AS pipeline_name,
				COALESCE(pr.build_number, 0) AS build_number,
				COALESCE(pr.status, '') AS run_status,
				COALESCE(pr.trigger_user, '') AS trigger_user,
				COALESCE(a.name, '') AS agent_name
			`).
			Order("t.created_at DESC").
			Offset(offset).
			Limit(pageSize).
			Scan(&tasks).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "查询任务列表失败: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"code": 200,
			"data": gin.H{
				"list":  tasks,
				"total": total,
				"page":  page,
				"size":  pageSize,
			},
		})
		return
	}

	var tasks []models.AgentTask
	var total int64

	query := h.DB.Model(&models.AgentTask{})
	query = query.Where("workspace_id = ?", workspaceID)

	if agentID != "" {
		query = query.Where("agent_id = ?", agentID)
	}

	if status != "" {
		query = query.Where("status = ?", status)
	}

	if pipelineRunID != "" {
		query = query.Where("pipeline_run_id = ?", pipelineRunID)
	}

	query.Count(&total)

	offset := (page - 1) * pageSize
	query.Preload("Agent").Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&tasks)

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"list":  tasks,
			"total": total,
			"page":  page,
			"size":  pageSize,
		},
	})
}

// GetTaskDetail returns task details
func (h *TaskHandler) GetTaskDetail(c *gin.Context) {
	id := c.Param("id")
	workspaceID := c.GetUint64("workspace_id")

	var task models.AgentTask
	if err := h.DB.Preload("Agent").Preload("Executions").Where("workspace_id = ?", workspaceID).First(&task, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "任务不存在",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": task,
	})
}

// GetTaskLogs returns logs for a task
func (h *TaskHandler) GetTaskLogs(c *gin.Context) {
	id := c.Param("id")
	level := c.DefaultQuery("level", "")
	workspaceID := c.GetUint64("workspace_id")

	var task models.AgentTask
	if err := h.DB.Where("workspace_id = ?", workspaceID).First(&task, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "任务不存在",
		})
		return
	}

	logs, err := agentFileLogs.QueryTaskLogs(task.PipelineRunID, task.ID, level)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "读取日志失败: " + err.Error(),
		})
		return
	}
	if !models.IsTerminalTaskStatus(task.Status) {
		liveLogs, liveErr := h.fetchCrossServerLiveTaskLogs(c.Request.Context(), task, 0)
		if liveErr == nil && len(liveLogs) > 0 {
			logs = append(logs, liveLogs...)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"list":  logs,
			"total": len(logs),
		},
	})
}

func (h *TaskHandler) GetTaskLiveLogsInternal(c *gin.Context) {
	id := c.Param("id")
	sinceSeq, _ := strconv.ParseInt(c.DefaultQuery("since_seq", "0"), 10, 64)
	var task models.AgentTask
	if err := h.DB.First(&task, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "任务不存在"})
		return
	}
	attempt := task.RetryCount + 1
	entries, err := agentFileLogs.QueryLiveTaskLogs(task.ID, attempt, sinceSeq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}
	logs := make([]models.AgentLog, 0, len(entries))
	for _, entry := range entries {
		logs = append(logs, models.AgentLog{TaskID: entry.TaskID, PipelineRunID: entry.PipelineRunID, Level: entry.Level, Message: entry.Message, Timestamp: entry.Timestamp, Source: entry.Source})
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"list": logs, "total": len(logs)}})
}

func (h *TaskHandler) fetchCrossServerLiveTaskLogs(ctx context.Context, task models.AgentTask, sinceSeq int64) ([]models.AgentLog, error) {
	if task.AgentID == 0 || task.OwnerServerID == "" || task.OwnerServerID == utils.ServerID() {
		return nil, nil
	}
	presence, err := utils.GetAgentPresence(ctx, task.AgentID)
	if err != nil || presence == nil || presence.ServerID != task.OwnerServerID || presence.ServerURL == "" {
		return nil, err
	}
	url := fmt.Sprintf("%s/internal/tasks/%d/live-logs?since_seq=%d", strings.TrimRight(presence.ServerURL, "/"), task.ID, sinceSeq)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(utils.InternalTokenHeader, utils.ServerInternalToken())
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("owner live log request failed: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var data struct {
		Code int `json:"code"`
		Data struct {
			List []models.AgentLog `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	return data.Data.List, nil
}

// CancelTask cancels a pending or running task
func (h *TaskHandler) CancelTask(c *gin.Context) {
	id := c.Param("id")
	workspaceID := c.GetUint64("workspace_id")

	var task models.AgentTask
	if err := h.DB.Where("workspace_id = ?", workspaceID).First(&task, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "任务不存在",
		})
		return
	}

	if task.Status != models.TaskStatusAssigned && task.Status != models.TaskStatusDispatching && task.Status != models.TaskStatusPulling && task.Status != models.TaskStatusAcked && task.Status != models.TaskStatusRunning {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "任务已结束，无法取消",
		})
		return
	}

	h.DB.Model(&task).Updates(map[string]interface{}{
		"status":   models.TaskStatusCancelled,
		"end_time": time.Now().Unix(),
	})

	// Update agent status
	var agent models.Agent
	h.DB.First(&agent, task.AgentID)
	h.checkAgentStatus(agent.ID)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "任务已取消",
	})
}

// RetryTask retries a failed task
func (h *TaskHandler) RetryTask(c *gin.Context) {
	id := c.Param("id")
	workspaceID := c.GetUint64("workspace_id")

	var task models.AgentTask
	if err := h.DB.Where("workspace_id = ?", workspaceID).First(&task, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "任务不存在",
		})
		return
	}

	if task.Status != models.TaskStatusExecuteFailed && task.Status != models.TaskStatusScheduleFailed && task.Status != models.TaskStatusDispatchTimeout && task.Status != models.TaskStatusLeaseExpired {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "只能重试失败的任务",
		})
		return
	}

	if task.RetryCount >= task.MaxRetries {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "已达到最大重试次数",
		})
		return
	}

	h.DB.Model(&task).Updates(map[string]interface{}{
		"status":           models.TaskStatusQueued,
		"retry_count":      task.RetryCount + 1,
		"start_time":       0,
		"end_time":         0,
		"duration":         0,
		"exit_code":        0,
		"error_msg":        "",
		"result_data":      "",
		"dispatch_token":   "",
		"dispatch_attempt": 0,
		"lease_expire_at":  0,
		"agent_session_id": "",
		"owner_server_id":  "",
	})

	task.Status = models.TaskStatusQueued
	task.RetryCount = task.RetryCount + 1
	task.StartTime = 0
	task.EndTime = 0
	task.Duration = 0
	task.ExitCode = 0
	task.ErrorMsg = ""
	task.ResultData = ""
	task.DispatchToken = ""
	task.DispatchAttempt = 0
	task.LeaseExpireAt = 0
	task.AgentSessionID = ""
	task.OwnerServerID = ""
	_ = SharedWebSocketHandler().sendTaskAssign(task)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "任务已重新排队",
	})
}

// AgentReportTaskStatus reports task status from agent
func (h *TaskHandler) AgentReportTaskStatus(c *gin.Context) {
	var req struct {
		AgentID    uint64  `json:"agent_id" binding:"required"`
		Token      string  `json:"token" binding:"required"`
		TaskID     uint64  `json:"task_id" binding:"required"`
		Status     string  `json:"status" binding:"required"`
		ExitCode   int     `json:"exit_code"`
		Stdout     string  `json:"stdout"`
		Stderr     string  `json:"stderr"`
		ErrorMsg   string  `json:"error_msg"`
		ResultData string  `json:"result_data"`
		Duration   float64 `json:"duration"` // Agent 传递的执行时长（秒）
	}

	// Read body for debugging
	body, _ := io.ReadAll(c.Request.Body)
	fmt.Printf("[DEBUG] Task report request body: %s\n", string(body))
	c.Request.Body = io.NopCloser(bytes.NewBuffer(body))

	if err := c.ShouldBindJSON(&req); err != nil {
		fmt.Printf("[DEBUG] Task report binding error: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数错误: " + err.Error(),
		})
		return
	}
	fmt.Printf("[DEBUG] Task report binding succeeded: task_id=%d, status=%s\n", req.TaskID, req.Status)

	// Verify agent token
	var agent models.Agent
	if err := h.DB.Where("id = ? AND token = ?", req.AgentID, req.Token).First(&agent).Error; err != nil {
		fmt.Printf("[DEBUG] Agent token verification failed: %v\n", err)
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "认证失败",
		})
		return
	}

	var task models.AgentTask
	if err := h.DB.First(&task, req.TaskID).Error; err != nil {
		fmt.Printf("[DEBUG] Task not found: %v\n", err)
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "任务不存在",
		})
		return
	}
	if task.AgentID != req.AgentID {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "任务不属于当前执行器",
		})
		return
	}
	if !isValidTaskStatusTransition(task.Status, req.Status) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "非法任务状态迁移",
		})
		return
	}

	now := time.Now().Unix()

	task.Status = req.Status
	if req.Status == models.TaskStatusRunning && task.StartTime == 0 {
		task.StartTime = now
	}
	if req.Status == models.TaskStatusExecuteSuccess || req.Status == models.TaskStatusExecuteFailed || req.Status == models.TaskStatusScheduleFailed || req.Status == models.TaskStatusCancelled {
		task.EndTime = now
	}
	// 使用 agent 传递的 duration，如果为0则使用服务器计算的值
	if req.Duration > 0 {
		task.Duration = int(req.Duration)
	} else if task.StartTime > 0 && task.EndTime > 0 {
		task.Duration = int(now - task.StartTime)
	}
	task.ExitCode = req.ExitCode
	task.ErrorMsg = req.ErrorMsg
	task.ResultData = req.ResultData

	// Save task status (omit PipelineRunID to avoid FK constraint violations)
	if err := h.DB.Omit("PipelineRunID").Save(&task).Error; err != nil {
		fmt.Printf("[DEBUG] Task save error: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "保存任务状态失败: " + err.Error(),
		})
		return
	}
	fmt.Printf("[DEBUG] Task %d status saved as: %s\n", task.ID, task.Status)
	_ = syncResourceOperationAuditRecords(h.DB, &task)

	// Create execution record
	var taskDuration int
	if req.Duration > 0 {
		taskDuration = int(req.Duration)
	} else if task.StartTime > 0 {
		taskDuration = int(now - task.StartTime)
	}
	execution := &models.TaskExecution{
		TaskID:    req.TaskID,
		Attempt:   task.RetryCount + 1,
		Status:    req.Status,
		StartTime: task.StartTime,
		EndTime:   now,
		Duration:  taskDuration,
		ExitCode:  req.ExitCode,
		Stdout:    req.Stdout,
		Stderr:    req.Stderr,
		ErrorMsg:  req.ErrorMsg,
	}
	h.DB.Create(execution)

	// Update agent status
	h.checkAgentStatus(req.AgentID)

	// Broadcast task status to all frontend clients subscribed to this run
	wsHandler := SharedWebSocketHandler()
	wsHandler.BroadcastTaskStatus(task.PipelineRunID, task.ID, task.NodeID, task.Status, req.ExitCode, req.ErrorMsg, agent.Name)

	// Check and update pipeline status if task is completed
	if req.Status == models.TaskStatusExecuteSuccess || req.Status == models.TaskStatusExecuteFailed || req.Status == models.TaskStatusScheduleFailed || req.Status == models.TaskStatusCancelled {
		// Trigger downstream tasks first, then update pipeline status
		var completedTasks []models.AgentTask
		h.DB.Where("pipeline_run_id = ?", task.PipelineRunID).Find(&completedTasks)
		wsHandler.triggerDownstreamTasks(task.PipelineRunID, completedTasks)
		wsHandler.checkAndUpdatePipelineStatus(task.PipelineRunID)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"task_id": task.ID,
			"status":  task.Status,
		},
	})
}

// AgentReportLog reports log entry from agent
func (h *TaskHandler) AgentReportLog(c *gin.Context) {
	var req struct {
		AgentID    uint64 `json:"agent_id" binding:"required"`
		Token      string `json:"token" binding:"required"`
		TaskID     uint64 `json:"task_id" binding:"required"`
		Level      string `json:"level" binding:"required"`
		Message    string `json:"message" binding:"required"`
		Timestamp  int64  `json:"timestamp"`
		Source     string `json:"source"`
		LineNumber int    `json:"line_number"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数错误",
		})
		return
	}

	// Verify agent token
	var agent models.Agent
	if err := h.DB.Where("id = ? AND token = ?", req.AgentID, req.Token).First(&agent).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "认证失败",
		})
		return
	}

	// Get task to find PipelineRunID
	var task models.AgentTask
	if err := h.DB.First(&task, req.TaskID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "任务不存在",
		})
		return
	}
	if task.AgentID != req.AgentID {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "任务不属于当前执行器",
		})
		return
	}

	timestamp := req.Timestamp
	if timestamp == 0 {
		timestamp = time.Now().Unix()
	}

	if req.Source == "" {
		req.Source = "stdout"
	}
	if err := agentFileLogs.Append(fileLogEntry{
		AgentID:       req.AgentID,
		TaskID:        req.TaskID,
		PipelineRunID: task.PipelineRunID,
		Level:         req.Level,
		Message:       req.Message,
		Source:        req.Source,
		Timestamp:     timestamp,
		LineNumber:    req.LineNumber,
		Attempt:       task.RetryCount + 1,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "日志写入失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{"log_id": 0},
	})
}

// GetPendingTasks returns pending tasks for an agent
func (h *TaskHandler) GetPendingTasks(c *gin.Context) {
	agentID := c.Param("agent_id")
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "缺少token",
		})
		return
	}

	var agent models.Agent
	if err := h.DB.Where("id = ? AND token = ?", agentID, token).First(&agent).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "认证失败",
		})
		return
	}
	if agent.RegistrationStatus != models.AgentRegistrationStatusApproved {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "执行器未接纳",
		})
		return
	}

	var tasks []models.AgentTask
	h.DB.Where("agent_id = ? AND status IN ?", agent.ID, []string{models.TaskStatusAssigned, models.TaskStatusDispatching, models.TaskStatusPulling}).Order("priority DESC, created_at ASC").Find(&tasks)

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"list":  tasks,
			"total": len(tasks),
		},
	})
}

// GetPipelineRunTasks returns all tasks for a pipeline run
func (h *TaskHandler) GetPipelineRunTasks(c *gin.Context) {
	runID := c.Param("run_id")
	workspaceID := c.GetUint64("workspace_id")
	runIDNum, err := strconv.ParseUint(runID, 10, 64)
	if err != nil || !pipelineRunBelongsToWorkspace(h.DB, runIDNum, workspaceID) {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "运行记录不存在"})
		return
	}

	var tasks []models.AgentTask
	h.DB.Where("workspace_id = ? AND pipeline_run_id = ?", workspaceID, runID).Preload("Agent").Order("created_at ASC").Find(&tasks)

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"list":  tasks,
			"total": len(tasks),
		},
	})
}

// GetPipelineRunLogs returns all logs for a pipeline run
func (h *TaskHandler) GetPipelineRunLogs(c *gin.Context) {
	runID := c.Param("run_id")
	level := c.DefaultQuery("level", "")
	source := c.DefaultQuery("source", "")
	workspaceID := c.GetUint64("workspace_id")

	runIDNum, err := strconv.ParseUint(runID, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的运行ID",
		})
		return
	}
	if !pipelineRunBelongsToWorkspace(h.DB, runIDNum, workspaceID) {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "运行记录不存在"})
		return
	}

	logs, err := agentFileLogs.QueryRunLogs(runIDNum, 0, level, source)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "读取日志失败: " + err.Error(),
		})
		return
	}
	var tasks []models.AgentTask
	if err := h.DB.Where("workspace_id = ? AND pipeline_run_id = ?", workspaceID, runIDNum).Find(&tasks).Error; err == nil {
		for _, task := range tasks {
			if models.IsTerminalTaskStatus(task.Status) {
				continue
			}
			liveLogs, liveErr := h.fetchCrossServerLiveTaskLogs(c.Request.Context(), task, 0)
			if liveErr == nil && len(liveLogs) > 0 {
				logs = append(logs, liveLogs...)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"list":  logs,
			"total": len(logs),
		},
	})
}

// checkAgentStatus updates agent status based on running tasks
func (h *TaskHandler) checkAgentStatus(agentID uint64) {
	updateAgentStatusByPipelineConcurrency(h.DB, agentID)
}
