package handlers

import (
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

func selectAgentWithPipelineCapacity(db *gorm.DB, workspaceID uint64) uint64 {
	type candidateAgent struct {
		ID           uint64
		RunningCount int64
	}

	query := db.Model(&models.Agent{}).
		Select(`agents.id, COALESCE(COUNT(pipeline_runs.id), 0) AS running_count`).
		Joins(`LEFT JOIN pipeline_runs ON pipeline_runs.agent_id = agents.id AND pipeline_runs.status = ?`, models.PipelineRunStatusRunning).
		Where("agents.registration_status = ? AND agents.status IN ?",
			models.AgentRegistrationStatusApproved,
			[]string{models.AgentStatusOnline, models.AgentStatusBusy},
		)
	if workspaceID == 0 {
		query = query.Where("agents.scope_type = ?", models.AgentScopePlatform)
	}
	if workspaceID > 0 {
		query = query.Where("agents.scope_type = ? OR (agents.scope_type = ? AND agents.workspace_id = ?)", models.AgentScopePlatform, models.AgentScopeWorkspace, workspaceID)
	}
	query = query.
		Group("agents.id, agents.max_concurrent_pipelines").
		Having(`COALESCE(COUNT(pipeline_runs.id), 0) < CASE WHEN agents.max_concurrent_pipelines <= 0 THEN ? ELSE agents.max_concurrent_pipelines END`, defaultAgentMaxConcurrentPipelines).
		Order("running_count ASC, agents.id ASC")

	var candidate candidateAgent
	if err := query.First(&candidate).Error; err != nil {
		return 0
	}
	return candidate.ID
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
