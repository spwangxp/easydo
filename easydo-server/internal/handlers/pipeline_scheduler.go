package handlers

import (
	"context"
	"encoding/json"
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
const defaultQueuedRunSchedulerInterval = 5 * time.Second

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
	return NewPipelineHandler().scheduleQueuedPipelineRuns(db)
}

func schedulerLeaderTTL() time.Duration {
	return 30 * time.Second
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
	if utils.RedisClient == nil {
		return false, nil
	}
	owner := config.Config.GetString("server.id")
	if owner == "" {
		return false, nil
	}
	ttl := schedulerLeaderTTL()
	ok, err := utils.RedisClient.SetNX(ctx, schedulerLeaderLockKey, owner, ttl).Result()
	if err != nil {
		return false, err
	}
	if ok {
		return true, nil
	}
	currentOwner, err := utils.RedisClient.Get(ctx, schedulerLeaderLockKey).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if currentOwner != owner {
		return false, nil
	}
	if err := utils.RedisClient.Expire(ctx, schedulerLeaderLockKey, ttl).Err(); err != nil {
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
			result := tx.Model(&models.PipelineRun{}).
				Where("id = ? AND status = ?", run.ID, models.PipelineRunStatusQueued).
				Updates(map[string]interface{}{
					"status":     models.PipelineRunStatusRunning,
					"agent_id":   agentID,
					"start_time": now,
					"end_time":   int64(0),
					"duration":   0,
					"error_msg":  "",
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
	if err := json.Unmarshal([]byte(run.Config), &config); err != nil {
		h.updateRunStatus(runID, models.PipelineRunStatusFailed, "流水线配置解析失败: "+err.Error())
		return
	}

	h.executePipelineTasks(pipeline, &run, config, run.TriggerUserID, run.TriggerUserRole)
}
