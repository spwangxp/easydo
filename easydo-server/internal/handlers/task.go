package handlers

import (
	"bytes"
	"easydo-server/internal/models"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type TaskHandler struct {
	DB *gorm.DB
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
		AgentID:    req.AgentID,
		NodeID:     req.NodeID,
		TaskType:   req.TaskType,
		Name:       req.Name,
		Params:     req.Params,
		Script:     req.Script,
		WorkDir:    req.WorkDir,
		EnvVars:    req.EnvVars,
		Status:     models.TaskStatusPending,
		Priority:   req.Priority,
		Timeout:    timeout,
		MaxRetries: maxRetries,
		CreatedBy:  createdBy,
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
	agentID := c.Query("agent_id")
	status := c.Query("status")
	pipelineRunID := c.Query("pipeline_run_id")

	var tasks []models.AgentTask
	var total int64

	query := h.DB.Model(&models.AgentTask{})

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

	var task models.AgentTask
	if err := h.DB.Preload("Agent").Preload("Executions").First(&task, id).Error; err != nil {
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

	var logs []models.AgentLog
	query := h.DB.Where("task_id = ?", id)

	if level != "" {
		query = query.Where("level = ?", level)
	}

	query.Order("timestamp ASC").Find(&logs)

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"list":  logs,
			"total": len(logs),
		},
	})
}

// CancelTask cancels a pending or running task
func (h *TaskHandler) CancelTask(c *gin.Context) {
	id := c.Param("id")

	var task models.AgentTask
	if err := h.DB.First(&task, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "任务不存在",
		})
		return
	}

	if task.Status != models.TaskStatusPending && task.Status != models.TaskStatusRunning {
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

	var task models.AgentTask
	if err := h.DB.First(&task, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "任务不存在",
		})
		return
	}

	if task.Status != models.TaskStatusFailed {
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
		"status":      models.TaskStatusPending,
		"retry_count": task.RetryCount + 1,
		"start_time":  0,
		"end_time":    0,
		"duration":    0,
		"error_msg":   "",
	})

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

	now := time.Now().Unix()

	task.Status = req.Status
	task.EndTime = now
	// 使用 agent 传递的 duration，如果为0则使用服务器计算的值
	if req.Duration > 0 {
		task.Duration = int(req.Duration)
	} else if task.StartTime > 0 {
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
	wsHandler := NewWebSocketHandler()
	wsHandler.BroadcastTaskStatus(task.PipelineRunID, task.ID, task.Status, req.ExitCode, req.ErrorMsg, agent.Name)

	// Check and update pipeline status if task is completed (success or failed)
	if req.Status == models.TaskStatusSuccess || req.Status == models.TaskStatusFailed {
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

	timestamp := req.Timestamp
	if timestamp == 0 {
		timestamp = time.Now().Unix()
	}

	log := &models.AgentLog{
		TaskID:    req.TaskID,
		Level:     req.Level,
		Message:   req.Message,
		Timestamp: timestamp,
		Source:    req.Source,
	}
	h.DB.Create(log)

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{"log_id": log.ID},
	})
}

// GetPendingTasks returns pending tasks for an agent
func (h *TaskHandler) GetPendingTasks(c *gin.Context) {
	agentID := c.Param("agent_id")

	var tasks []models.AgentTask
	h.DB.Where("agent_id = ? AND status = ?", agentID, models.TaskStatusPending).Order("priority DESC, created_at ASC").Find(&tasks)

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

	var tasks []models.AgentTask
	h.DB.Where("pipeline_run_id = ?", runID).Preload("Agent").Order("created_at ASC").Find(&tasks)

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

	var logs []models.AgentLog
	query := h.DB.Where("pipeline_run_id = ?", runID)

	if level != "" {
		query = query.Where("level = ?", level)
	}
	if source != "" {
		query = query.Where("source = ?", source)
	}

	query.Order("timestamp ASC").Find(&logs)

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
	var runningTasks int64
	h.DB.Model(&models.AgentTask{}).Where("agent_id = ? AND status = ?", agentID, models.TaskStatusRunning).Count(&runningTasks)

	var pendingTasks int64
	h.DB.Model(&models.AgentTask{}).Where("agent_id = ? AND status = ?", agentID, models.TaskStatusPending).Count(&pendingTasks)

	status := models.AgentStatusOnline
	if runningTasks > 0 {
		status = models.AgentStatusBusy
	} else if pendingTasks > 0 {
		status = models.AgentStatusBusy
	}

	h.DB.Model(&models.Agent{}).Where("id = ?", agentID).Update("status", status)
}
