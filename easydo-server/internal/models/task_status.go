package models

var terminalTaskStatuses = map[string]bool{
	TaskStatusExecuteSuccess: true,
	TaskStatusExecuteFailed:  true,
	TaskStatusScheduleFailed: true,
	TaskStatusCancelled:      true,
}

var taskStatusTransitions = map[string]map[string]bool{
	TaskStatusQueued: {
		TaskStatusAssigned:       true,
		TaskStatusCancelled:      true,
		TaskStatusScheduleFailed: true,
	},
	TaskStatusAssigned: {
		TaskStatusDispatching:    true,
		TaskStatusCancelled:      true,
		TaskStatusScheduleFailed: true,
	},
	TaskStatusDispatching: {
		TaskStatusPulling:         true,
		TaskStatusDispatchTimeout: true,
		TaskStatusCancelled:       true,
	},
	TaskStatusPulling: {
		TaskStatusAcked:           true,
		TaskStatusDispatchTimeout: true,
		TaskStatusCancelled:       true,
	},
	TaskStatusAcked: {
		TaskStatusRunning:      true,
		TaskStatusCancelled:    true,
		TaskStatusLeaseExpired: true,
	},
	TaskStatusRunning: {
		TaskStatusExecuteSuccess: true,
		TaskStatusExecuteFailed:  true,
		TaskStatusCancelled:      true,
		TaskStatusLeaseExpired:   true,
	},
	TaskStatusDispatchTimeout: {
		TaskStatusQueued: true,
	},
	TaskStatusLeaseExpired: {
		TaskStatusQueued:         true,
		TaskStatusScheduleFailed: true,
	},
	TaskStatusExecuteFailed: {
		TaskStatusQueued: true,
	},
	TaskStatusExecuteSuccess: {},
	TaskStatusScheduleFailed: {},
	TaskStatusCancelled:      {},
}

func IsTerminalTaskStatus(status string) bool {
	return terminalTaskStatuses[status]
}

func IsTaskStatusTransitionAllowed(from, to string) bool {
	next, ok := taskStatusTransitions[from]
	if !ok {
		return false
	}
	return next[to]
}

func IsDispatchStageTaskStatus(status string) bool {
	switch status {
	case TaskStatusAssigned, TaskStatusDispatching, TaskStatusPulling, TaskStatusDispatchTimeout, TaskStatusScheduleFailed:
		return true
	default:
		return false
	}
}

func IsExecutionStageTaskStatus(status string) bool {
	switch status {
	case TaskStatusAcked, TaskStatusRunning, TaskStatusExecuteSuccess, TaskStatusExecuteFailed, TaskStatusLeaseExpired, TaskStatusCancelled:
		return true
	default:
		return false
	}
}
