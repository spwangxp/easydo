package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"easydo-server/internal/config"
	"easydo-server/internal/models"
	"easydo-server/pkg/utils"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

var pipelineScheduleMu sync.Mutex

const schedulerLeaderLockKey = "easydo:scheduler:leader"
const runningRunReconcileLeaderLockKey = "easydo:scheduler:running-reconcile:leader"
const defaultQueuedRunSchedulerInterval = 5 * time.Second
const defaultRunningRunReconcileLimit = 64

type QueuedRunScheduler struct {
	db       *gorm.DB
	interval time.Duration
	ticker   *time.Ticker
	stopChan chan struct{}
	stopOnce sync.Once
}

func NewQueuedRunScheduler(db *gorm.DB, interval time.Duration) *QueuedRunScheduler {
	if interval <= 0 {
		interval = defaultQueuedRunSchedulerInterval
	}
	return &QueuedRunScheduler{
		db:       db,
		interval: interval,
		stopChan: make(chan struct{}),
	}
}

func (s *QueuedRunScheduler) Start() {
	if s == nil || s.db == nil {
		return
	}
	if s.ticker != nil {
		return
	}

	s.ticker = time.NewTicker(s.interval)
	go func() {
		for {
			select {
			case <-s.ticker.C:
				runQueuedPipelineSchedulerTick(s.db)
			case <-s.stopChan:
				return
			}
		}
	}()
}

func (s *QueuedRunScheduler) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.ticker != nil {
			s.ticker.Stop()
		}
		close(s.stopChan)
	})
}

func runQueuedPipelineSchedulerTick(db *gorm.DB) int {
	if db == nil {
		db = models.DB
	}
	now := time.Now().Unix()
	_, _ = reconcileCancelRequestedTasks(db, now)
	reconcileExpiredPipelineRunsIfLeader(db, now, defaultRunningRunReconcileLimit)
	reconcileRunningPipelineTasksIfLeader(db, defaultRunningRunReconcileLimit)
	return NewPipelineHandler().scheduleQueuedPipelineRuns(db)
}

func schedulerLeaderTTL() time.Duration {
	return 30 * time.Second
}

func reconcileExpiredPipelineRunsIfLeader(db *gorm.DB, now int64, limit int) int {
	ok, err := tryAcquireSchedulerLock(context.Background(), runningRunReconcileLeaderLockKey)
	if err != nil || !ok {
		return 0
	}
	return reconcileExpiredPipelineRuns(db, now, limit)
}

func reconcileRunningPipelineTasksIfLeader(db *gorm.DB, limit int) int {
	ok, err := tryAcquireSchedulerLock(context.Background(), runningRunReconcileLeaderLockKey)
	if err != nil || !ok {
		return 0
	}
	return reconcileRunningPipelineTasks(db, limit)
}

func reconcileExpiredPipelineRuns(db *gorm.DB, now int64, limit int) int {
	if db == nil {
		db = models.DB
	}
	if db == nil {
		return 0
	}
	if now <= 0 {
		now = time.Now().Unix()
	}
	if limit <= 0 {
		limit = defaultRunningRunReconcileLimit
	}

	var runs []models.PipelineRun
	if err := db.Where("status = ? AND timeout_deadline > 0 AND timeout_deadline <= ?", models.PipelineRunStatusRunning, now).
		Order("timeout_deadline ASC, id ASC").
		Limit(limit).
		Find(&runs).Error; err != nil {
		return 0
	}

	reconciled := 0
	for i := range runs {
		if expirePipelineRun(db, runs[i], now) {
			reconciled++
		}
	}
	return reconciled
}

func expirePipelineRun(db *gorm.DB, run models.PipelineRun, now int64) bool {
	duration := 0
	if run.StartTime > 0 {
		duration = int(now - run.StartTime)
	}
	errorMsg := pipelineTimeoutErrorMessage(run.TimeoutSeconds)
	cancelledTasks := make([]models.AgentTask, 0)
	tasksToNotify := make([]models.AgentTask, 0)
	expired := false

	err := db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.PipelineRun{}).
			Where("id = ? AND status = ? AND timeout_deadline > 0 AND timeout_deadline <= ?", run.ID, models.PipelineRunStatusRunning, now).
			Updates(map[string]interface{}{
				"status":    models.PipelineRunStatusFailed,
				"end_time":  now,
				"duration":  duration,
				"error_msg": errorMsg,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}

		var tasks []models.AgentTask
		if err := tx.Where("pipeline_run_id = ? AND status IN ?", run.ID, []string{
			models.TaskStatusQueued,
			models.TaskStatusAssigned,
			models.TaskStatusDispatching,
			models.TaskStatusPulling,
			models.TaskStatusAcked,
			models.TaskStatusRunning,
		}).Find(&tasks).Error; err != nil {
			return err
		}

		for i := range tasks {
			task := &tasks[i]
			if !isTaskCancelable(task.Status) {
				continue
			}
			shouldNotifyAgent := isExecutionOwnedTaskStatus(task.Status)
			updates := buildTaskCancelUpdates(task, now)
			result := tx.Model(&models.AgentTask{}).
				Where("id = ? AND status = ?", task.ID, task.Status).
				Updates(updates)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				continue
			}
			if status, ok := updates["status"].(string); ok {
				task.Status = status
			}
			if endTime, ok := updates["end_time"].(int64); ok {
				task.EndTime = endTime
			}
			if taskDuration, ok := updates["duration"].(int); ok {
				task.Duration = taskDuration
			}
			cancelledTasks = append(cancelledTasks, *task)
			if shouldNotifyAgent {
				tasksToNotify = append(tasksToNotify, *task)
			}
		}

		appendRunEvent(tx, run.ID, "run_finished", map[string]interface{}{"status": models.PipelineRunStatusFailed, "error_msg": errorMsg})
		expired = true
		return nil
	})
	if err != nil || !expired {
		return false
	}

	run.Status = models.PipelineRunStatusFailed
	run.EndTime = now
	run.Duration = duration
	run.ErrorMsg = errorMsg
	syncLiveRunStateFromRun(&run)
	syncDeploymentStateFromRun(db, &run)
	emitPipelineRunTerminalNotification(db, &run, NotificationEventTypePipelineRunFailed)
	handler := SharedWebSocketHandler()
	for _, task := range tasksToNotify {
		_ = handler.sendTaskCancel(task)
	}
	for _, task := range cancelledTasks {
		message := "流水线整体超时，任务被取消"
		if task.Status == models.TaskStatusCancelRequested {
			message = "流水线整体超时，任务取消请求已提交，等待 agent 确认"
		}
		syncLiveTaskStateFromTask(&task, "")
		handler.BroadcastTaskStatus(run.ID, task.ID, task.NodeID, task.Status, 0, message, "")
	}
	handler.BroadcastRunStatus(run.ID, models.PipelineRunStatusFailed, errorMsg, duration)
	updateAgentStatusByPipelineConcurrency(db, run.AgentID)
	go NewPipelineHandler().scheduleQueuedPipelineRuns(db)
	return true
}

func pipelineTimeoutErrorMessage(timeoutSeconds int64) string {
	if timeoutSeconds <= 0 {
		return "流水线整体执行超时"
	}
	return fmt.Sprintf("流水线整体执行超时（限制 %d 分钟）", timeoutSeconds/60)
}

func reconcileRunningPipelineTasks(db *gorm.DB, limit int) int {
	if db == nil {
		db = models.DB
	}
	if db == nil {
		return 0
	}
	if limit <= 0 {
		limit = defaultRunningRunReconcileLimit
	}

	var runs []models.PipelineRun
	if err := db.Where("status = ?", models.PipelineRunStatusRunning).
		Order("updated_at ASC, id ASC").
		Limit(limit).
		Find(&runs).Error; err != nil {
		return 0
	}

	handler := SharedWebSocketHandler()
	reconciled := 0
	for i := range runs {
		run := runs[i]
		var tasks []models.AgentTask
		if err := db.Where("pipeline_run_id = ?", run.ID).Find(&tasks).Error; err != nil || len(tasks) == 0 {
			continue
		}
		if !runningRunNeedsTaskReconciliation(run, tasks) {
			continue
		}
		handler.triggerDownstreamTasks(run.ID, tasks)
		handler.checkAndUpdatePipelineStatus(run.ID)
		reconciled++
	}
	return reconciled
}

func runningRunNeedsTaskReconciliation(run models.PipelineRun, tasks []models.AgentTask) bool {
	if len(tasks) == 0 {
		return false
	}

	hasTerminal := false
	allTerminal := true
	taskNodeIDs := make(map[string]struct{}, len(tasks))
	for i := range tasks {
		taskNodeIDs[tasks[i].NodeID] = struct{}{}
		if models.IsTerminalTaskStatus(tasks[i].Status) {
			hasTerminal = true
		} else {
			allTerminal = false
		}
	}
	if !hasTerminal {
		return false
	}

	if strings.TrimSpace(run.PipelineSnapshot) != "" {
		var config PipelineConfig
		if err := json.Unmarshal([]byte(run.PipelineSnapshot), &config); err == nil {
			for i := range config.Nodes {
				if _, exists := taskNodeIDs[config.Nodes[i].ID]; !exists {
					return true
				}
			}
		}
	}

	return allTerminal
}

func (h *PipelineHandler) scheduleQueuedPipelineRuns(db *gorm.DB) int {
	if db == nil {
		db = h.DB
	}
	if db == nil {
		return 0
	}

	pipelineScheduleMu.Lock()
	defer pipelineScheduleMu.Unlock()
	ok, err := h.tryAcquireSchedulerLeadership(context.Background())
	if err != nil || !ok {
		return 0
	}

	scheduled := 0
	for {
		runID, ok := h.assignOneQueuedRun(db)
		if !ok {
			break
		}
		scheduled++
		go h.startQueuedPipelineRun(runID)
	}
	return scheduled
}

func (h *PipelineHandler) tryAcquireSchedulerLeadership(ctx context.Context) (bool, error) {
	return tryAcquireSchedulerLock(ctx, schedulerLeaderLockKey)
}

func tryAcquireSchedulerLock(ctx context.Context, lockKey string) (bool, error) {
	if utils.RedisClient == nil {
		return false, nil
	}
	owner := config.Config.GetString("server.id")
	if owner == "" || strings.TrimSpace(lockKey) == "" {
		return false, nil
	}
	ttl := schedulerLeaderTTL()
	ok, err := utils.RedisClient.SetNX(ctx, lockKey, owner, ttl).Result()
	if err != nil {
		return false, err
	}
	if ok {
		return true, nil
	}
	currentOwner, err := utils.RedisClient.Get(ctx, lockKey).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if currentOwner != owner {
		return false, nil
	}
	if err := utils.RedisClient.Expire(ctx, lockKey, ttl).Err(); err != nil {
		return false, err
	}
	return true, nil
}

func (h *PipelineHandler) assignOneQueuedRun(db *gorm.DB) (uint64, bool) {
	var scheduledRunID uint64
	var scheduledAgentID uint64

	err := db.Transaction(func(tx *gorm.DB) error {
		var queuedRuns []models.PipelineRun
		if err := tx.Where("status = ?", models.PipelineRunStatusQueued).
			Order("created_at ASC, id ASC").
			Limit(64).
			Find(&queuedRuns).Error; err != nil {
			return err
		}
		if len(queuedRuns) == 0 {
			return nil
		}

		for i := range queuedRuns {
			run := queuedRuns[i]
			agentID := selectAgentWithPipelineCapacity(tx, run.WorkspaceID)
			if agentID == 0 {
				return nil
			}

			now := time.Now().Unix()
			timeoutDeadline := pipelineTimeoutDeadline(now, run.TimeoutSeconds)
			result := tx.Model(&models.PipelineRun{}).
				Where("id = ? AND status = ?", run.ID, models.PipelineRunStatusQueued).
				Updates(map[string]interface{}{
					"status":           models.PipelineRunStatusRunning,
					"agent_id":         agentID,
					"start_time":       now,
					"end_time":         int64(0),
					"duration":         0,
					"timeout_deadline": timeoutDeadline,
					"error_msg":        "",
				})
			if result.Error != nil || result.RowsAffected == 0 {
				continue
			}

			if err := tx.Model(&models.AgentTask{}).
				Where("pipeline_run_id = ? AND status = ?", run.ID, models.TaskStatusQueued).
				Updates(map[string]interface{}{
					"status":   models.TaskStatusAssigned,
					"agent_id": agentID,
				}).Error; err != nil {
				return err
			}

			scheduledRunID = run.ID
			scheduledAgentID = agentID
			appendRunEvent(tx, run.ID, "run_started", map[string]interface{}{"agent_id": agentID})
			return nil
		}

		return nil
	})
	if err != nil || scheduledRunID == 0 {
		return 0, false
	}
	var run models.PipelineRun
	if err := db.First(&run, scheduledRunID).Error; err == nil {
		syncLiveRunStateFromRun(&run)
		syncDeploymentStateFromRun(db, &run)
	}
	var tasks []models.AgentTask
	if err := db.Where("pipeline_run_id = ?", scheduledRunID).Find(&tasks).Error; err == nil {
		for i := range tasks {
			syncLiveTaskStateFromTask(&tasks[i], "")
		}
	}

	updateAgentStatusByPipelineConcurrency(db, scheduledAgentID)
	SharedWebSocketHandler().BroadcastRunStatus(scheduledRunID, models.PipelineRunStatusRunning, "")
	return scheduledRunID, true
}

func (h *PipelineHandler) startQueuedPipelineRun(runID uint64) {
	var run models.PipelineRun
	if err := h.DB.First(&run, runID).Error; err != nil {
		return
	}
	if run.Status != models.PipelineRunStatusRunning {
		return
	}

	var pipeline models.Pipeline
	if err := h.DB.First(&pipeline, run.PipelineID).Error; err != nil {
		h.updateRunStatus(runID, models.PipelineRunStatusFailed, "流水线不存在")
		return
	}

	var config PipelineConfig
	if err := json.Unmarshal([]byte(run.PipelineSnapshot), &config); err != nil {
		h.updateRunStatus(runID, models.PipelineRunStatusFailed, "流水线快照解析失败: "+err.Error())
		return
	}

	var runConfig models.PipelineRunConfigSnapshot
	if strings.TrimSpace(run.RunConfig) != "" {
		if err := json.Unmarshal([]byte(run.RunConfig), &runConfig); err != nil {
			h.updateRunStatus(runID, models.PipelineRunStatusFailed, "运行配置解析失败: "+err.Error())
			return
		}
	}
	executionConfig, err := buildExecutionPipelineConfig(config, runConfig)
	if err != nil {
		h.updateRunStatus(runID, models.PipelineRunStatusFailed, "流水线配置解析失败: "+err.Error())
		return
	}

	h.executePipelineTasks(pipeline, &run, executionConfig, run.TriggerUserID, run.TriggerUserRole)
}
