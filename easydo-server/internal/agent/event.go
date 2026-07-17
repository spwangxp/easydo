package agent

import (
	"encoding/json"
	"fmt"
	"time"
)

// ============================================================
// Event Types — 产品级 Event 定义, 参考 OpenCode session-event.ts
// ============================================================

type EventID string
type MessageID string

type EventBase struct {
	ID        EventID   `json:"id"`
	SessionID string    `json:"session_id"`
	Timestamp time.Time `json:"timestamp"`
}

// ——— Session Lifecycle ———

type PromptedEvent struct {
	Type      string    `json:"type"`
	EventBase
	MessageID MessageID `json:"message_id"`
	Prompt    string    `json:"prompt"`
	Files     []FileRef `json:"files,omitempty"`
	Delivery  string    `json:"delivery"`
}

type StepStartedEvent struct {
	Type                string    `json:"type"`
	EventBase
	AssistantMessageID  MessageID `json:"assistant_message_id"`
	Agent               string    `json:"agent,omitempty"`
	Model               ModelRef  `json:"model"`
	Snapshot            string    `json:"snapshot,omitempty"`
}

type StepEndedEvent struct {
	Type                string    `json:"type"`
	EventBase
	AssistantMessageID  MessageID `json:"assistant_message_id"`
	FinishReason        string    `json:"finish_reason"`
	Tokens              TokenUsage `json:"tokens"`
	Cost                float64   `json:"cost"`
	Files               []string  `json:"files,omitempty"`
}

type StepFailedEvent struct {
	Type                string    `json:"type"`
	EventBase
	AssistantMessageID  MessageID `json:"assistant_message_id"`
	Error               ErrorInfo `json:"error"`
}

// ——— Text ———

type TextStartedEvent struct {
	Type                string    `json:"type"`
	EventBase
	AssistantMessageID  MessageID `json:"assistant_message_id"`
	TextID              string    `json:"text_id"`
}

type TextDeltaEvent struct {
	Type                string    `json:"type"`
	EventBase
	AssistantMessageID  MessageID `json:"assistant_message_id"`
	TextID              string    `json:"text_id"`
	Delta               string    `json:"delta"`
}

type TextEndedEvent struct {
	Type                string    `json:"type"`
	EventBase
	AssistantMessageID  MessageID `json:"assistant_message_id"`
	TextID              string    `json:"text_id"`
	Text                string    `json:"text"`
}

// ——— Reasoning ———

type ReasoningStartedEvent struct {
	Type                string    `json:"type"`
	EventBase
	AssistantMessageID  MessageID `json:"assistant_message_id"`
	ReasoningID         string    `json:"reasoning_id"`
}

type ReasoningDeltaEvent struct {
	Type                string    `json:"type"`
	EventBase
	AssistantMessageID  MessageID `json:"assistant_message_id"`
	ReasoningID         string    `json:"reasoning_id"`
	Delta               string    `json:"delta"`
}

type ReasoningEndedEvent struct {
	Type                string    `json:"type"`
	EventBase
	AssistantMessageID  MessageID `json:"assistant_message_id"`
	ReasoningID         string    `json:"reasoning_id"`
	Text                string    `json:"text"`
}

// ——— Tool ———

type ToolInputStartedEvent struct {
	Type                string    `json:"type"`
	EventBase
	AssistantMessageID  MessageID `json:"assistant_message_id"`
	CallID              string    `json:"call_id"`
	ToolName            string    `json:"tool_name"`
}

type ToolInputDeltaEvent struct {
	Type                string    `json:"type"`
	EventBase
	AssistantMessageID  MessageID `json:"assistant_message_id"`
	CallID              string    `json:"call_id"`
	ToolName            string    `json:"tool_name"`
	Delta               string    `json:"delta"`
}

type ToolInputEndedEvent struct {
	Type                string    `json:"type"`
	EventBase
	AssistantMessageID  MessageID `json:"assistant_message_id"`
	CallID              string    `json:"call_id"`
	Text                string    `json:"text"`
}

type ToolCalledEvent struct {
	Type                string    `json:"type"`
	EventBase
	AssistantMessageID  MessageID `json:"assistant_message_id"`
	CallID              string    `json:"call_id"`
	Tool                string    `json:"tool"`
	Input               map[string]interface{} `json:"input"`
}

type ToolProgressEvent struct {
	Type                string    `json:"type"`
	EventBase
	AssistantMessageID  MessageID `json:"assistant_message_id"`
	CallID              string    `json:"call_id"`
	Content             []ContentPart `json:"content,omitempty"`
	Structured          map[string]interface{} `json:"structured,omitempty"`
}

type ToolSuccessEvent struct {
	Type                string    `json:"type"`
	EventBase
	AssistantMessageID  MessageID `json:"assistant_message_id"`
	CallID              string    `json:"call_id"`
	Content             []ContentPart `json:"content"`
	Structured          map[string]interface{} `json:"structured,omitempty"`
	OutputPaths         []string  `json:"output_paths,omitempty"`
}

type ToolFailedEvent struct {
	Type                string    `json:"type"`
	EventBase
	AssistantMessageID  MessageID `json:"assistant_message_id"`
	CallID              string    `json:"call_id"`
	Error               ErrorInfo `json:"error"`
}

// ——— Approval ———

type ApprovalRequestedEvent struct {
	Type      string `json:"type"`
	EventBase
	RequestID string `json:"request_id"`
	ToolName  string `json:"tool_name"`
	Input     map[string]interface{} `json:"input"`
	Reason    string `json:"reason,omitempty"`
	Message   string `json:"message"`
}

type ApprovalResolvedEvent struct {
	Type      string `json:"type"`
	EventBase
	RequestID string `json:"request_id"`
	Result    string `json:"result"`
}

// ——— Compaction ———

type CompactionStartedEvent struct {
	Type      string    `json:"type"`
	EventBase
	MessageID MessageID `json:"message_id"`
	Reason    string    `json:"reason"`
}

type CompactionEndedEvent struct {
	Type      string    `json:"type"`
	EventBase
	MessageID MessageID `json:"message_id"`
	Reason    string    `json:"reason"`
	Summary   string    `json:"summary"`
	Recent    string    `json:"recent"`
}

// ——— Config Changes ———

type ModelSwitchedEvent struct {
	Type      string   `json:"type"`
	EventBase
	MessageID MessageID `json:"message_id"`
	Model     ModelRef `json:"model"`
}

type AgentSwitchedEvent struct {
	Type      string `json:"type"`
	EventBase
	MessageID MessageID `json:"message_id"`
	Agent     string   `json:"agent"`
}

// ——— Error ———

type SessionErrorEvent struct {
	Type      string `json:"type"`
	EventBase
	Level     string `json:"level"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// ============================================================
// Supporting Types
// ============================================================

type ModelRef struct {
	ID         string `json:"id"`
	ProviderID string `json:"provider_id"`
	Variant    string `json:"variant,omitempty"`
}

type TokenUsage struct {
	Input     int `json:"input"`
	Output    int `json:"output"`
	Reasoning int `json:"reasoning"`
	Cache     struct {
		Read  int `json:"read"`
		Write int `json:"write"`
	} `json:"cache"`
}

type ErrorInfo struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type FileRef struct {
	Path string `json:"path"`
	Mime string `json:"mime,omitempty"`
}

type ContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	URI  string `json:"uri,omitempty"`
	Mime string `json:"mime,omitempty"`
}

// ============================================================
// Event Union Interface
// ============================================================

type Event interface {
	EventType() string
}

func (e *PromptedEvent) EventType() string          { return "session.prompted" }
func (e *StepStartedEvent) EventType() string        { return "session.step.started" }
func (e *StepEndedEvent) EventType() string          { return "session.step.ended" }
func (e *StepFailedEvent) EventType() string         { return "session.step.failed" }
func (e *TextStartedEvent) EventType() string        { return "session.text.started" }
func (e *TextDeltaEvent) EventType() string          { return "session.text.delta" }
func (e *TextEndedEvent) EventType() string          { return "session.text.ended" }
func (e *ReasoningStartedEvent) EventType() string   { return "session.reasoning.started" }
func (e *ReasoningDeltaEvent) EventType() string     { return "session.reasoning.delta" }
func (e *ReasoningEndedEvent) EventType() string     { return "session.reasoning.ended" }
func (e *ToolInputStartedEvent) EventType() string   { return "session.tool.input.started" }
func (e *ToolInputDeltaEvent) EventType() string     { return "session.tool.input.delta" }
func (e *ToolInputEndedEvent) EventType() string     { return "session.tool.input.ended" }
func (e *ToolCalledEvent) EventType() string         { return "session.tool.called" }
func (e *ToolProgressEvent) EventType() string       { return "session.tool.progress" }
func (e *ToolSuccessEvent) EventType() string        { return "session.tool.success" }
func (e *ToolFailedEvent) EventType() string         { return "session.tool.failed" }
func (e *ApprovalRequestedEvent) EventType() string  { return "permission.asked" }
func (e *ApprovalResolvedEvent) EventType() string   { return "permission.resolved" }
func (e *CompactionStartedEvent) EventType() string  { return "session.compaction.started" }
func (e *CompactionEndedEvent) EventType() string    { return "session.compaction.ended" }
func (e *ModelSwitchedEvent) EventType() string      { return "session.model.switched" }
func (e *AgentSwitchedEvent) EventType() string      { return "session.agent.switched" }
func (e *SessionErrorEvent) EventType() string       { return "session.error" }

// ============================================================
// Event Serialization — Marshal/Unmarshal helpers
// ============================================================

// HasSessionIDSetter — 允许注入 SessionID（Pi Adapter 回调时使用）
type HasSessionIDSetter interface {
	SetSessionID(sessionID string)
}

func (b *EventBase) SetSessionID(sessionID string) { b.SessionID = sessionID }

var eventRegistry = map[string]func() Event{
	"session.prompted":              func() Event { return &PromptedEvent{} },
	"session.step.started":          func() Event { return &StepStartedEvent{} },
	"session.step.ended":            func() Event { return &StepEndedEvent{} },
	"session.step.failed":           func() Event { return &StepFailedEvent{} },
	"session.text.started":          func() Event { return &TextStartedEvent{} },
	"session.text.delta":            func() Event { return &TextDeltaEvent{} },
	"session.text.ended":            func() Event { return &TextEndedEvent{} },
	"session.reasoning.started":     func() Event { return &ReasoningStartedEvent{} },
	"session.reasoning.delta":       func() Event { return &ReasoningDeltaEvent{} },
	"session.reasoning.ended":       func() Event { return &ReasoningEndedEvent{} },
	"session.tool.input.started":    func() Event { return &ToolInputStartedEvent{} },
	"session.tool.input.delta":      func() Event { return &ToolInputDeltaEvent{} },
	"session.tool.input.ended":      func() Event { return &ToolInputEndedEvent{} },
	"session.tool.called":           func() Event { return &ToolCalledEvent{} },
	"session.tool.progress":         func() Event { return &ToolProgressEvent{} },
	"session.tool.success":          func() Event { return &ToolSuccessEvent{} },
	"session.tool.failed":           func() Event { return &ToolFailedEvent{} },
	"permission.asked":              func() Event { return &ApprovalRequestedEvent{} },
	"permission.resolved":           func() Event { return &ApprovalResolvedEvent{} },
	"session.compaction.started":    func() Event { return &CompactionStartedEvent{} },
	"session.compaction.ended":      func() Event { return &CompactionEndedEvent{} },
	"session.model.switched":        func() Event { return &ModelSwitchedEvent{} },
	"session.agent.switched":        func() Event { return &AgentSwitchedEvent{} },
	"session.error":                 func() Event { return &SessionErrorEvent{} },
}

// UnmarshalEvent — 根据 type 反序列化 JSON data 到对应 Event 类型
func UnmarshalEvent(eventType string, data []byte) (Event, error) {
	factory, ok := eventRegistry[eventType]
	if !ok {
		return nil, fmt.Errorf("unknown event type: %s", eventType)
	}
	evt := factory()
	return evt, json.Unmarshal(data, evt)
}
