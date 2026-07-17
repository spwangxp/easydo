package agent

// ============================================================
// Message + Part — 前端 UI 直接消费的数据契约
// 参考 OpenCode session-message.ts 的 Message + Part 投影
// ============================================================

// ——— Message ———

type ProjectedMessage struct {
	ID        MessageID `json:"id"`
	SessionID string    `json:"session_id"`
	Role      string    `json:"role"` // "user" | "assistant"
	ParentID  string    `json:"parent_id,omitempty"`

	// User 字段
	Text  string    `json:"text,omitempty"`
	Files []FileRef `json:"files,omitempty"`

	// Assistant 字段
	Agent      string   `json:"agent,omitempty"`
	Model      ModelRef `json:"model,omitempty"`
	Finish     string   `json:"finish,omitempty"`
	Tokens     *TokenUsage `json:"tokens,omitempty"`
	Cost       float64  `json:"cost,omitempty"`
	Error      *ErrorInfo `json:"error,omitempty"`

	Time struct {
		Created   int64 `json:"created"`
		Completed int64 `json:"completed,omitempty"`
	} `json:"time"`

	Summary *MessageSummary `json:"summary,omitempty"`
}

type MessageSummary struct {
	Additions int             `json:"additions"`
	Deletions int             `json:"deletions"`
	Files     int             `json:"files"`
	Diffs     []SnapshotDiff  `json:"diffs,omitempty"`
}

type SnapshotDiff struct {
	File      string `json:"file"`
	Patch     string `json:"patch,omitempty"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Status    string `json:"status"` // "added" | "deleted" | "modified"
}

// ——— Part（从 Message 分离） ———

type ProjectedPart struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	MessageID MessageID `json:"message_id"`
	Type      string    `json:"type"` // "text" | "reasoning" | "tool" | "compaction"
	Index     int       `json:"index"`
}

// TextPart
type ProjectedTextPart struct {
	ProjectedPart
	Text string `json:"text"`
}

// ReasoningPart（前端默认折叠）
type ProjectedReasoningPart struct {
	ProjectedPart
	Text string `json:"text"`
}

// ToolPart（核心复杂类型）
type ProjectedToolPart struct {
	ProjectedPart
	Tool       string `json:"tool"`
	State      string `json:"state"` // "pending" | "running" | "completed" | "error"
	Input      map[string]interface{} `json:"input,omitempty"`
	Content    []ContentPart `json:"content,omitempty"`
	Structured map[string]interface{} `json:"structured,omitempty"`
	OutputPaths []string  `json:"output_paths,omitempty"`
	Result     interface{} `json:"result,omitempty"`
	Error      *ErrorInfo  `json:"error,omitempty"`
	Approval   *PartApproval `json:"approval,omitempty"`
	Timestamps struct {
		Created   int64 `json:"created"`
		Ran       int64 `json:"ran,omitempty"`
		Completed int64 `json:"completed,omitempty"`
	} `json:"timestamps"`
}

type PartApproval struct {
	ID     string `json:"id"`
	Status string `json:"status"` // "pending" | "approved" | "rejected"
	Reason string `json:"reason,omitempty"`
}

// CompactionPart
type ProjectedCompactionPart struct {
	ProjectedPart
	Reason  string `json:"reason"`
	Summary string `json:"summary"`
	Recent  string `json:"recent"`
}

// ——— Session Status（独立状态通道） ———

type SessionStatus struct {
	SessionID string `json:"session_id"`
	Type      string `json:"type"` // "idle" | "busy" | "retrying" | "interrupted"
	Message   string `json:"message,omitempty"`
	Attempt   int    `json:"attempt,omitempty"`
	Next      int64  `json:"next,omitempty"` // next retry after ms
}

// ——— Approval ———

type ProjectedApproval struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	ToolName  string `json:"tool_name"`
	Input     map[string]interface{} `json:"input"`
	Reason    string `json:"reason,omitempty"`
	Status    string `json:"status"` // "pending" | "approved" | "rejected"
	Message   string `json:"message"`
	CreatedAt int64  `json:"created_at"`
}

// ——— SessionInfo（给前端的完整 session 数据） ———

type SessionInfo struct {
	ID         string   `json:"id"`
	Slug       string   `json:"slug"`
	Title      string   `json:"title"`
	Agent      string   `json:"agent,omitempty"`
	Model      ModelRef `json:"model,omitempty"`
	Tokens     TokenUsage `json:"tokens"`
	Cost       float64  `json:"cost"`
	Status     string   `json:"status"`
	Time       struct {
		Created int64 `json:"created"`
		Updated int64 `json:"updated"`
	} `json:"time"`
}

// ——— FrontendStore 完整投影（给前端一次拉取用） ———

type SessionProjection struct {
	Session    SessionInfo            `json:"session"`
	Messages   []ProjectedMessage     `json:"messages"`
	Parts      map[string][]ProjectedPart `json:"parts"`       // key: messageID
	Status     SessionStatus          `json:"status"`
	Approvals  []ProjectedApproval    `json:"approvals,omitempty"`
	Diffs      []SnapshotDiff         `json:"diffs,omitempty"`
}
