package handlers

import (
	"math"

	"easydo-server/internal/models"
	"gorm.io/gorm"
)

const defaultAgentMaxConcurrentPipelines = 10

func normalizeAgentMaxConcurrentPipelines(max int) int {
	if max <= 0 {
		return defaultAgentMaxConcurrentPipelines
	}
	return max
}

func countAgentRunningPipelines(db *gorm.DB, agentID uint64) int64 {
	var running int64
	db.Model(&models.PipelineRun{}).
		Where("agent_id = ? AND status = ?", agentID, models.PipelineRunStatusRunning).
		Count(&running)
	return running
}

func selectAgentWithPipelineCapacity(db *gorm.DB) uint64 {
	var agents []models.Agent
	db.Where("registration_status = ? AND status IN ?",
		models.AgentRegistrationStatusApproved,
		[]string{models.AgentStatusOnline, models.AgentStatusBusy},
	).Find(&agents)

	if len(agents) == 0 {
		return 0
	}

	selected := uint64(0)
	minRunning := int64(math.MaxInt64)
	for _, agent := range agents {
		maxConcurrent := normalizeAgentMaxConcurrentPipelines(agent.MaxConcurrentPipelines)
		running := countAgentRunningPipelines(db, agent.ID)
		if running >= int64(maxConcurrent) {
			continue
		}
		if running < minRunning {
			minRunning = running
			selected = agent.ID
		}
	}

	return selected
}

func updateAgentStatusByPipelineConcurrency(db *gorm.DB, agentID uint64) {
	if agentID == 0 {
		return
	}

	var agent models.Agent
	if err := db.First(&agent, agentID).Error; err != nil {
		return
	}

	if agent.RegistrationStatus != models.AgentRegistrationStatusApproved {
		return
	}
	if agent.Status == models.AgentStatusOffline || agent.Status == models.AgentStatusError {
		return
	}

	maxConcurrent := normalizeAgentMaxConcurrentPipelines(agent.MaxConcurrentPipelines)
	running := countAgentRunningPipelines(db, agentID)

	targetStatus := models.AgentStatusOnline
	if running >= int64(maxConcurrent) {
		targetStatus = models.AgentStatusBusy
	}

	if agent.Status != targetStatus {
		db.Model(&agent).Update("status", targetStatus)
	}
}
