package models

// Agent represents an execution agent that runs on hosts
type Agent struct {
	BaseModel
	Name                   string  `gorm:"size:128;not null" json:"name"`
	Host                   string  `gorm:"size:255;not null" json:"host"`
	Port                   int     `gorm:"not null" json:"port"`
	Token                  string  `gorm:"size:256;not null" json:"-"`                                                 // Secret token for authentication
	RegisterKey            string  `gorm:"size:256" json:"-"`                                                          // Registration key for fetching token after approval
	Status                 string  `gorm:"size:32;default:'offline'" json:"status"`                                    // online, offline, busy, error
	RegistrationStatus     string  `gorm:"size:32;default:'pending'" json:"registration_status"`                       // pending, approved, rejected
	MaxConcurrentPipelines int     `gorm:"column:max_concurrent_pipelines;default:10" json:"max_concurrent_pipelines"` // 最大并发流水线数
	ApprovedAt             int64   `json:"approved_at"`                                                                // Approval timestamp
	ApprovedBy             *uint64 `gorm:"index" json:"approved_by"`                                                   // Approver user ID
	ApprovedRemark         string  `gorm:"type:text" json:"approved_remark"`                                           // Approval remark
	Labels                 string  `gorm:"type:text" json:"labels"`                                                    // JSON array of labels ["linux", "docker", "cpu=8"]
	Tags                   string  `gorm:"type:text" json:"tags"`                                                      // JSON object of tags {"env": "prod", "region": "cn-east"}
	Version                string  `gorm:"size:32" json:"version"`                                                     // Agent version
	OS                     string  `gorm:"size:64" json:"os"`                                                          // Operating system
	Arch                   string  `gorm:"size:32" json:"arch"`                                                        // Architecture
	CPUCores               int     `json:"cpu_cores"`                                                                  // Number of CPU cores
	MemoryTotal            int64   `json:"memory_total"`                                                               // Total memory in bytes
	DiskTotal              int64   `json:"disk_total"`                                                                 // Total disk space in bytes
	BaseInfo               string  `gorm:"type:longtext" json:"base_info"`
	BaseInfoCollectedAt    int64   `json:"base_info_collected_at"`
	Hostname               string  `gorm:"size:128" json:"hostname"`
	IPAddress              string  `gorm:"size:64" json:"ip_address"`
	LastHeartAt            int64   `json:"last_heart_at"`                                                  // Last heartbeat timestamp
	HeartbeatInterval      int     `gorm:"column:heartbeat_interval;default:10" json:"heartbeat_interval"` // Heartbeat interval in seconds
	ConsecutiveSuccess     int     `gorm:"default:0" json:"consecutive_success"`                           // Consecutive successful heartbeats (max 3)
	ConsecutiveFailures    int     `gorm:"default:0" json:"consecutive_failures"`                          // Consecutive failed heartbeats (max 3)
	ScopeType              string  `gorm:"size:32;default:'platform';index" json:"scope_type"`
	WorkspaceID            uint64  `gorm:"index" json:"workspace_id"`
	CreatedBy              uint64  `gorm:"index" json:"created_by"`
	OwnerID                *uint64 `gorm:"index" json:"owner_id"` // Optional owner

	Workspace *Workspace `gorm:"-" json:"workspace,omitempty"`
	Owner     *User      `gorm:"foreignKey:OwnerID" json:"owner"`
}

// AgentStatus constants
const (
	AgentStatusOnline  = "online"
	AgentStatusOffline = "offline"
	AgentStatusBusy    = "busy"
	AgentStatusError   = "error"
)

// AgentRegistrationStatus constants
const (
	AgentRegistrationStatusPending  = "pending"  // 待接纳
	AgentRegistrationStatusApproved = "approved" // 已接纳
	AgentRegistrationStatusRejected = "rejected" // 已拒绝
)

// AgentTask represents a task assigned to an agent
type AgentTask struct {
	BaseModel
	WorkspaceID     uint64 `gorm:"index" json:"workspace_id"`
	AgentID         uint64 `gorm:"index;not null" json:"agent_id"`
	PipelineRunID   uint64 `gorm:"index;uniqueIndex:idx_task_run_node" json:"pipeline_run_id"`  // Associated pipeline run
	NodeID          string `gorm:"size:128;index;uniqueIndex:idx_task_run_node" json:"node_id"` // Node ID in pipeline config
	TaskType        string `gorm:"size:64;not null" json:"task_type"`                           // shell, docker, git_clone, email
	Name            string `gorm:"size:256" json:"name"`
	Params          string `gorm:"type:longtext" json:"params"` // 任务参数（执行时的快照）
	Script          string `gorm:"type:longtext" json:"script"` // 执行脚本（执行时的快照）
	WorkDir         string `gorm:"size:512" json:"work_dir"`    // 工作目录
	EnvVars         string `gorm:"type:text" json:"env_vars"`   // 环境变量
	Status          string `gorm:"size:32;default:'queued';index" json:"status"`
	DispatchToken   string `gorm:"size:64;index" json:"dispatch_token"`
	DispatchAttempt int    `gorm:"default:0" json:"dispatch_attempt"`
	LeaseExpireAt   int64  `gorm:"default:0;index" json:"lease_expire_at"`
	AgentSessionID  string `gorm:"size:64;index" json:"agent_session_id"`
	OwnerServerID   string `gorm:"size:128;index" json:"owner_server_id"`
	Priority        int    `gorm:"default:0" json:"priority"`        // 调度优先级
	Timeout         int    `gorm:"default:3600" json:"timeout"`      // 超时时间（秒）
	RetryCount      int    `gorm:"default:0" json:"retry_count"`     // 当前重试次数
	MaxRetries      int    `gorm:"default:3" json:"max_retries"`     // 最大重试次数
	ExitCode        int    `gorm:"default:0" json:"exit_code"`       // 退出码
	ErrorMsg        string `gorm:"type:text" json:"error_msg"`       // 错误信息
	StartTime       int64  `json:"start_time"`                       // 开始时间
	EndTime         int64  `json:"end_time"`                         // 结束时间
	Duration        int    `json:"duration"`                         // 缓存的执行时长（秒）
	ResultData      string `gorm:"type:longtext" json:"result_data"` // JSON 结果数据（outputs）

	// 代码仓库信息（每个任务可能对应不同仓库）
	RepoURL    string `gorm:"size:512" json:"repo_url"`    // 仓库地址
	RepoBranch string `gorm:"size:128" json:"repo_branch"` // 分支
	RepoCommit string `gorm:"size:64" json:"repo_commit"`  // 提交ID
	RepoPath   string `gorm:"size:512" json:"repo_path"`   // 本地检出路径

	CreatedBy uint64 `gorm:"index" json:"created_by"`

	Agent      *Agent          `gorm:"foreignKey:AgentID" json:"agent"`
	Executions []TaskExecution `gorm:"foreignKey:TaskID" json:"executions"` // 重试执行记录
	Logs       []AgentLog      `gorm:"foreignKey:TaskID" json:"logs"`       // 执行日志
}

// TaskStatus constants
const (
	TaskStatusQueued          = "queued"
	TaskStatusAssigned        = "assigned"
	TaskStatusDispatching     = "dispatching"
	TaskStatusPulling         = "pulling"
	TaskStatusAcked           = "acked"
	TaskStatusRunning         = "running"
	TaskStatusExecuteSuccess  = "execute_success"
	TaskStatusExecuteFailed   = "execute_failed"
	TaskStatusScheduleFailed  = "schedule_failed"
	TaskStatusDispatchTimeout = "dispatch_timeout"
	TaskStatusLeaseExpired    = "lease_expired"
	TaskStatusCancelled       = "cancelled"
)

// TaskExecution represents a single execution attempt of a task
type TaskExecution struct {
	BaseModel
	TaskID    uint64 `gorm:"index;not null" json:"task_id"`
	Attempt   int    `gorm:"not null" json:"attempt"` // Attempt number (1, 2, 3...)
	Status    string `gorm:"size:32;default:'assigned'" json:"status"`
	StartTime int64  `json:"start_time"`
	EndTime   int64  `json:"end_time"`
	Duration  int    `json:"duration"`                    // Duration in seconds
	ExitCode  int    `gorm:"default:0" json:"exit_code"`  // Process exit code
	Stdout    string `gorm:"type:longtext" json:"stdout"` // Standard output
	Stderr    string `gorm:"type:longtext" json:"stderr"` // Standard error
	ErrorMsg  string `gorm:"type:text" json:"error_msg"`

	Task *AgentTask `gorm:"foreignKey:TaskID" json:"task"`
}

// AgentHeartbeat represents a heartbeat record from agent
type AgentHeartbeat struct {
	BaseModel
	AgentID      uint64  `gorm:"index;not null" json:"agent_id"`
	Timestamp    int64   `gorm:"not null" json:"timestamp"`
	CPUUsage     float64 `json:"cpu_usage"`               // CPU usage percentage
	MemoryUsage  float64 `json:"memory_usage"`            // Memory usage percentage
	DiskUsage    float64 `json:"disk_usage"`              // Disk usage percentage
	LoadAvg      string  `gorm:"size:64" json:"load_avg"` // System load average
	TasksRunning int     `json:"tasks_running"`           // Number of running tasks

	Agent *Agent `gorm:"foreignKey:AgentID" json:"agent"`
}

// AgentLog represents log entries from agent execution
type AgentLog struct {
	BaseModel
	TaskID        uint64 `gorm:"index;default:0" json:"task_id"`      // 关联任务（可选，0表示流水线级别日志）
	PipelineRunID uint64 `gorm:"index" json:"pipeline_run_id"`        // 关联流水线运行（可选）
	Level         string `gorm:"size:16;default:'info'" json:"level"` // debug/info/warn/error
	Message       string `gorm:"type:longtext" json:"message"`        // 日志内容
	Timestamp     int64  `gorm:"not null" json:"timestamp"`           // Unix 时间戳（毫秒）
	Source        string `gorm:"size:32" json:"source"`               // stdout/stderr/system

	Task *AgentTask `gorm:"foreignKey:TaskID" json:"task"`
}

// AgentTaskEvent represents idempotent task status update events
type AgentTaskEvent struct {
	BaseModel
	TaskID         uint64 `gorm:"not null;index;uniqueIndex:idx_task_attempt_idem" json:"task_id"`
	PipelineRunID  uint64 `gorm:"index" json:"pipeline_run_id"`
	AgentID        uint64 `gorm:"index" json:"agent_id"`
	AgentSessionID string `gorm:"size:64;index" json:"agent_session_id"`
	Attempt        int    `gorm:"not null;default:1;uniqueIndex:idx_task_attempt_idem" json:"attempt"`
	Status         string `gorm:"size:32;not null" json:"status"`
	IdempotencyKey string `gorm:"size:128;not null;uniqueIndex:idx_task_attempt_idem" json:"idempotency_key"`
	ExitCode       int    `gorm:"default:0" json:"exit_code"`
	DurationMs     int64  `gorm:"default:0" json:"duration_ms"`
	ErrorMsg       string `gorm:"type:text" json:"error_msg"`
}

type AgentLogSegment struct {
	BaseModel
	TaskID        uint64 `gorm:"not null;index;uniqueIndex:idx_task_attempt_segment" json:"task_id"`
	PipelineRunID uint64 `gorm:"index" json:"pipeline_run_id"`
	AgentID       uint64 `gorm:"index" json:"agent_id"`
	Attempt       int    `gorm:"not null;default:1;uniqueIndex:idx_task_attempt_segment" json:"attempt"`
	SegmentNo     int    `gorm:"not null;uniqueIndex:idx_task_attempt_segment" json:"segment_no"`
	StartSeq      int64  `gorm:"not null" json:"start_seq"`
	EndSeq        int64  `gorm:"not null" json:"end_seq"`
	LineCount     int    `gorm:"default:0" json:"line_count"`
	ObjectKey     string `gorm:"size:512;not null;index" json:"object_key"`
	ObjectBucket  string `gorm:"size:128;not null" json:"object_bucket"`
	ObjectSize    int64  `gorm:"default:0" json:"object_size"`
	ContentType   string `gorm:"size:64;default:'application/gzip'" json:"content_type"`
	Checksum      string `gorm:"size:128" json:"checksum"`
	Completed     bool   `gorm:"default:false" json:"completed"`
}
