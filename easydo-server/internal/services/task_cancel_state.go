package services

import "easydo-server/internal/models"

func IsExecutionOwnedTaskStatus(status string) bool {
	switch status {
	case models.TaskStatusAcked, models.TaskStatusRunning, models.TaskStatusCancelRequested:
		return true
	default:
		return false
	}
}

func IsTaskCancelable(status string) bool {
	switch status {
	case models.TaskStatusQueued,
		models.TaskStatusAssigned,
		models.TaskStatusDispatching,
		models.TaskStatusPulling,
		models.TaskStatusAcked,
		models.TaskStatusRunning:
		return true
	default:
		return false
	}
}

func TaskCancelRequestedStatus(status string) string {
	if IsExecutionOwnedTaskStatus(status) {
		return models.TaskStatusCancelRequested
	}
	return models.TaskStatusCancelled
}

func BuildTaskCancelUpdates(task *models.AgentTask, now int64) map[string]interface{} {
	status := TaskCancelRequestedStatus(task.Status)
	updates := map[string]interface{}{
		"status": status,
	}
	if status != models.TaskStatusCancelled {
		return updates
	}
	duration := 0
	if task.StartTime > 0 {
		duration = int(now - task.StartTime)
	}
	updates["end_time"] = now
	updates["duration"] = duration
	return updates
}

func IsRunTerminalStatus(status string) bool {
	switch status {
	case models.PipelineRunStatusSuccess, models.PipelineRunStatusFailed, models.PipelineRunStatusCancelled:
		return true
	default:
		return false
	}
}

func IsRunActiveStatus(status string) bool {
	switch status {
	case models.PipelineRunStatusQueued,
		models.PipelineRunStatusPending,
		models.PipelineRunStatusRunning,
		models.PipelineRunStatusCancelRequested:
		return true
	default:
		return false
	}
}

func IsRunExecutingStatus(status string) bool {
	switch status {
	case models.PipelineRunStatusRunning, models.PipelineRunStatusCancelRequested:
		return true
	default:
		return false
	}
}
