package handlers

import (
	"encoding/json"
	"sync"
	"time"

	"easydo-server/internal/models"
	"gorm.io/gorm"
)

var pipelineScheduleMu sync.Mutex

func (h *PipelineHandler) scheduleQueuedPipelineRuns(db *gorm.DB) int {
	if db == nil {
		db = h.DB
	}
	if db == nil {
		return 0
	}

	pipelineScheduleMu.Lock()
	defer pipelineScheduleMu.Unlock()

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

func (h *PipelineHandler) assignOneQueuedRun(db *gorm.DB) (uint64, bool) {
	var queuedRuns []models.PipelineRun
	if err := db.Where("status = ?", models.PipelineRunStatusQueued).
		Order("created_at ASC").
		Limit(64).
		Find(&queuedRuns).Error; err != nil {
		return 0, false
	}
	if len(queuedRuns) == 0 {
		return 0, false
	}

	for i := range queuedRuns {
		run := queuedRuns[i]
		agentID := selectAgentWithPipelineCapacity(db, run.WorkspaceID)
		if agentID == 0 {
			return 0, false
		}

		now := time.Now().Unix()
		result := db.Model(&models.PipelineRun{}).
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

		_ = db.Model(&models.AgentTask{}).
			Where("pipeline_run_id = ? AND status = ?", run.ID, models.TaskStatusQueued).
			Updates(map[string]interface{}{
				"status":   models.TaskStatusPending,
				"agent_id": agentID,
			}).Error

		updateAgentStatusByPipelineConcurrency(db, agentID)
		SharedWebSocketHandler().BroadcastRunStatus(run.ID, models.PipelineRunStatusRunning, "")
		return run.ID, true
	}

	return 0, false
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
