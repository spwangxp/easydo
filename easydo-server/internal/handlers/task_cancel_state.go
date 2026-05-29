package handlers

import (
	"easydo-server/internal/models"
	"easydo-server/internal/services"
)

func isExecutionOwnedTaskStatus(status string) bool {
	return services.IsExecutionOwnedTaskStatus(status)
}

func isTaskCancelable(status string) bool {
	return services.IsTaskCancelable(status)
}

func taskCancelRequestedStatus(status string) string {
	return services.TaskCancelRequestedStatus(status)
}

func buildTaskCancelUpdates(task *models.AgentTask, now int64) map[string]interface{} {
	return services.BuildTaskCancelUpdates(task, now)
}

func isRunTerminalStatus(status string) bool {
	return services.IsRunTerminalStatus(status)
}

func isRunActiveStatus(status string) bool {
	return services.IsRunActiveStatus(status)
}

func isRunExecutingStatus(status string) bool {
	return services.IsRunExecutingStatus(status)
}
