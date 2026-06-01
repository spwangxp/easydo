package services

type RefreshResourceBaseInfoRequest struct {
	Actor       ActorContext
	WorkspaceID uint64
	ResourceID  uint64
}

type RefreshResourceBaseInfoResult struct {
	TaskID  uint64 `json:"task_id"`
	Status  string `json:"status"`
	AgentID uint64 `json:"agent_id,omitempty"`
}
