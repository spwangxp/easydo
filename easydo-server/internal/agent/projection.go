package agent

import (
	"encoding/json"
	"fmt"
	"sync"
)

// ============================================================
// Event Projection — Event → Frontend Store 的 Reducer
// 参考 OpenCode event-reducer.ts 的 applyDirectoryEvent
// ============================================================

// ProjectedStore — 前端 Store 的内存投影
type ProjectedStore struct {
	mu sync.RWMutex

	Messages           map[string][]ProjectedMessage  // sessionID → messages
	Parts              map[string]map[string]ProjectedPart // sessionID → messageID → parts
	Statuses           map[string]SessionStatus       // sessionID → status
	Approvals          map[string][]ProjectedApproval // sessionID → approvals
	Diffs              map[string][]SnapshotDiff      // sessionID → diffs
	PartTextDeltas     map[string]string              // partID → accumulated delta
	PartCounter        map[string]int                 // messageID → part counter
}

func NewProjectedStore() *ProjectedStore {
	return &ProjectedStore{
		Messages:       make(map[string][]ProjectedMessage),
		Parts:          make(map[string]map[string]ProjectedPart),
		Statuses:       make(map[string]SessionStatus),
		Approvals:      make(map[string][]ProjectedApproval),
		Diffs:          make(map[string][]SnapshotDiff),
		PartTextDeltas: make(map[string]string),
		PartCounter:    make(map[string]int),
	}
}

// ApplyEvent — 将 Event 应用到投影 Store
func (s *ProjectedStore) ApplyEvent(stored StoredEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	eventType := stored.Type
	sessionID := stored.SessionID
	now := stored.CreatedAt.UnixMilli()

	switch eventType {
	case "session.prompted":
		s.applyPrompted(stored, sessionID, now, stored.Data)
	case "session.step.started":
		s.applyStepStarted(stored, sessionID, now, stored.Data)
	case "session.step.ended":
		s.applyStepEnded(stored, sessionID, stored.Data)
	case "session.step.failed":
		s.applyStepFailed(stored, sessionID, stored.Data)
	case "session.text.started":
		s.applyTextStarted(stored, sessionID, stored.Data)
	case "session.text.delta":
		s.applyTextDelta(stored, sessionID, stored.Data)
	case "session.text.ended":
		s.applyTextEnded(stored, sessionID, stored.Data)
	case "session.reasoning.started":
		s.applyReasoningStarted(stored, sessionID, stored.Data)
	case "session.reasoning.delta":
		s.applyReasoningDelta(stored, sessionID, stored.Data)
	case "session.reasoning.ended":
		s.applyReasoningEnded(stored, sessionID, stored.Data)
	case "session.tool.input.started":
		s.applyToolInputStarted(stored, sessionID, stored.Data)
	case "session.tool.called":
		s.applyToolCalled(stored, sessionID, now, stored.Data)
	case "session.tool.progress":
		s.applyToolProgress(stored, sessionID, stored.Data)
	case "session.tool.success":
		s.applyToolSuccess(stored, sessionID, stored.Data)
	case "session.tool.failed":
		s.applyToolFailed(stored, sessionID, stored.Data)
	case "session.compaction.started":
		s.applyCompactionStarted(stored, sessionID, now, stored.Data)
	case "session.compaction.ended":
		s.applyCompactionEnded(stored, sessionID, stored.Data)
	case "session.model.switched":
		s.applyModelSwitched(stored, sessionID, now, stored.Data)
	case "session.agent.switched":
		s.applyAgentSwitched(stored, sessionID, now, stored.Data)
	case "permission.asked":
		s.applyPermissionAsked(stored, sessionID, now, stored.Data)
	case "permission.resolved":
		s.applyPermissionResolved(stored, sessionID, stored.Data)
	case "session.error":
		s.applySessionError(stored, sessionID, stored.Data)
	}
}

func (s *ProjectedStore) nextPartID(messageID string) string {
	counter := s.PartCounter[messageID]
	s.PartCounter[messageID] = counter + 1
	return fmt.Sprintf("part_%s_%d", messageID, counter)
}

func (s *ProjectedStore) addPart(sessionID string, messageID string, part ProjectedPart) {
	if s.Parts[sessionID] == nil {
		s.Parts[sessionID] = make(map[string]ProjectedPart)
	}
	s.Parts[sessionID][part.ID] = part
}

func (s *ProjectedStore) getPart(sessionID, partID string) (ProjectedPart, bool) {
	parts, ok := s.Parts[sessionID]
	if !ok {
		return ProjectedPart{}, false
	}
	p, ok := parts[partID]
	return p, ok
}

func (s *ProjectedStore) updatePart(sessionID, partID string, update func(p *ProjectedPart)) {
	parts, ok := s.Parts[sessionID]
	if !ok {
		return
	}
	p, ok := parts[partID]
	if !ok {
		return
	}
	update(&p)
	parts[partID] = p
}

// ——— Event-specific reducers ———

func (s *ProjectedStore) applyPrompted(stored StoredEvent, sessionID string, now int64, data json.RawMessage) {
	var evt PromptedEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}
	msg := ProjectedMessage{
		ID:        evt.MessageID,
		SessionID: sessionID,
		Role:      "user",
		Text:      evt.Prompt,
		Files:     evt.Files,
	}
	msg.Time.Created = now
	s.Messages[sessionID] = append(s.Messages[sessionID], msg)
}

func (s *ProjectedStore) applyStepStarted(stored StoredEvent, sessionID string, now int64, data json.RawMessage) {
	var evt StepStartedEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}
	msg := ProjectedMessage{
		ID:        evt.AssistantMessageID,
		SessionID: sessionID,
		Role:      "assistant",
		Agent:     evt.Agent,
		Model:     evt.Model,
		Finish:    "",
	}
	msg.Time.Created = now
	s.Messages[sessionID] = append(s.Messages[sessionID], msg)
}

func (s *ProjectedStore) applyStepEnded(stored StoredEvent, sessionID string, data json.RawMessage) {
	var evt StepEndedEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}
	msgs := s.Messages[sessionID]
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].ID == evt.AssistantMessageID {
			msgs[i].Finish = evt.FinishReason
			msgs[i].Tokens = &evt.Tokens
			msgs[i].Cost = evt.Cost
			msgs[i].Time.Completed = stored.CreatedAt.UnixMilli()
			s.Messages[sessionID][i] = msgs[i]
			break
		}
	}
}

func (s *ProjectedStore) applyStepFailed(stored StoredEvent, sessionID string, data json.RawMessage) {
	var evt StepFailedEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}
	msgs := s.Messages[sessionID]
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].ID == evt.AssistantMessageID {
			msgs[i].Error = &evt.Error
			msgs[i].Time.Completed = stored.CreatedAt.UnixMilli()
			s.Messages[sessionID][i] = msgs[i]
			break
		}
	}
}

func (s *ProjectedStore) applyTextStarted(stored StoredEvent, sessionID string, data json.RawMessage) {
	var evt TextStartedEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}
	part := ProjectedTextPart{
		ProjectedPart: ProjectedPart{
			ID:        evt.TextID,
			SessionID: sessionID,
			MessageID: evt.AssistantMessageID,
			Type:      "text",
		},
		Text: "",
	}
	s.addPart(sessionID, string(evt.AssistantMessageID), part.ProjectedPart)
}

func (s *ProjectedStore) applyTextDelta(stored StoredEvent, sessionID string, data json.RawMessage) {
	var evt TextDeltaEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}
	s.PartTextDeltas[evt.TextID] += evt.Delta
}

func (s *ProjectedStore) applyTextEnded(stored StoredEvent, sessionID string, data json.RawMessage) {
	var evt TextEndedEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}
	s.updatePart(sessionID, evt.TextID, func(p *ProjectedPart) {})
}

func (s *ProjectedStore) applyReasoningStarted(stored StoredEvent, sessionID string, data json.RawMessage) {
	var evt ReasoningStartedEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}
	part := ProjectedReasoningPart{
		ProjectedPart: ProjectedPart{
			ID:        evt.ReasoningID,
			SessionID: sessionID,
			MessageID: evt.AssistantMessageID,
			Type:      "reasoning",
		},
		Text: "",
	}
	s.addPart(sessionID, string(evt.AssistantMessageID), part.ProjectedPart)
}

func (s *ProjectedStore) applyReasoningDelta(stored StoredEvent, sessionID string, data json.RawMessage) {
	var evt ReasoningDeltaEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}
	s.PartTextDeltas[evt.ReasoningID] += evt.Delta
}

func (s *ProjectedStore) applyReasoningEnded(stored StoredEvent, sessionID string, data json.RawMessage) {
	var evt ReasoningEndedEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}
	text := s.PartTextDeltas[evt.ReasoningID]
	if text == "" {
		text = evt.Text
	}
	s.updatePart(sessionID, evt.ReasoningID, func(p *ProjectedPart) {
		// For simplicity, store text in the part's metadata
	})
}

func (s *ProjectedStore) applyToolInputStarted(stored StoredEvent, sessionID string, data json.RawMessage) {
	// Tool input start is tracked as part of tool state
}

func (s *ProjectedStore) applyToolCalled(stored StoredEvent, sessionID string, now int64, data json.RawMessage) {
	var evt ToolCalledEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}
	tp := ProjectedToolPart{
		ProjectedPart: ProjectedPart{
			ID:        evt.CallID,
			SessionID: sessionID,
			MessageID: evt.AssistantMessageID,
			Type:      "tool",
		},
		Tool:  evt.Tool,
		State: "running",
		Input: evt.Input,
	}
	tp.Timestamps.Created = now
	tp.Timestamps.Ran = now
	s.addPart(sessionID, string(evt.AssistantMessageID), tp.ProjectedPart)
}

func (s *ProjectedStore) applyToolProgress(stored StoredEvent, sessionID string, data json.RawMessage) {
	var evt ToolProgressEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}
	s.updatePart(sessionID, evt.CallID, func(p *ProjectedPart) {
		tp := &ProjectedToolPart{ProjectedPart: *p}
		tp.State = "running"
		tp.Content = evt.Content
		tp.Structured = evt.Structured
		*p = tp.ProjectedPart
	})
}

func (s *ProjectedStore) applyToolSuccess(stored StoredEvent, sessionID string, data json.RawMessage) {
	var evt ToolSuccessEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}
	s.updatePart(sessionID, evt.CallID, func(p *ProjectedPart) {
		tp := &ProjectedToolPart{ProjectedPart: *p}
		tp.State = "completed"
		tp.Content = evt.Content
		tp.Structured = evt.Structured
		tp.OutputPaths = evt.OutputPaths
		tp.Timestamps.Completed = stored.CreatedAt.UnixMilli()
		*p = tp.ProjectedPart
	})
}

func (s *ProjectedStore) applyToolFailed(stored StoredEvent, sessionID string, data json.RawMessage) {
	var evt ToolFailedEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}
	s.updatePart(sessionID, evt.CallID, func(p *ProjectedPart) {
		tp := &ProjectedToolPart{ProjectedPart: *p}
		tp.State = "error"
		tp.Error = &evt.Error
		tp.Timestamps.Completed = stored.CreatedAt.UnixMilli()
		*p = tp.ProjectedPart
	})
}

func (s *ProjectedStore) applyCompactionStarted(stored StoredEvent, sessionID string, now int64, data json.RawMessage) {
	var evt CompactionStartedEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}
	msg := ProjectedMessage{
		ID:   evt.MessageID,
		Role: "assistant",
	}
	msg.Time.Created = now
	s.Messages[sessionID] = append(s.Messages[sessionID], msg)
}

func (s *ProjectedStore) applyCompactionEnded(stored StoredEvent, sessionID string, data json.RawMessage) {
	var evt CompactionEndedEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}
	part := ProjectedCompactionPart{
		ProjectedPart: ProjectedPart{
			ID:        s.nextPartID(string(evt.MessageID)),
			SessionID: sessionID,
			MessageID: evt.MessageID,
			Type:      "compaction",
		},
		Reason:  evt.Reason,
		Summary: evt.Summary,
		Recent:  evt.Recent,
	}
	s.addPart(sessionID, string(evt.MessageID), part.ProjectedPart)
}

func (s *ProjectedStore) applyModelSwitched(stored StoredEvent, sessionID string, now int64, data json.RawMessage) {
	var evt ModelSwitchedEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}
	msg := ProjectedMessage{
		ID:   evt.MessageID,
		Role: "assistant",
	}
	msg.Time.Created = now
	s.Messages[sessionID] = append(s.Messages[sessionID], msg)
}

func (s *ProjectedStore) applyAgentSwitched(stored StoredEvent, sessionID string, now int64, data json.RawMessage) {
	var evt AgentSwitchedEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}
	msg := ProjectedMessage{
		ID:   evt.MessageID,
		Role: "assistant",
	}
	msg.Time.Created = now
	s.Messages[sessionID] = append(s.Messages[sessionID], msg)
}

func (s *ProjectedStore) applyPermissionAsked(stored StoredEvent, sessionID string, now int64, data json.RawMessage) {
	var evt ApprovalRequestedEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}
	approval := ProjectedApproval{
		ID:        evt.RequestID,
		SessionID: sessionID,
		ToolName:  evt.ToolName,
		Input:     evt.Input,
		Reason:    evt.Reason,
		Status:    "pending",
		Message:   evt.Message,
		CreatedAt: now,
	}
	s.Approvals[sessionID] = append(s.Approvals[sessionID], approval)
}

func (s *ProjectedStore) applyPermissionResolved(stored StoredEvent, sessionID string, data json.RawMessage) {
	var evt ApprovalResolvedEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}
	approvals := s.Approvals[sessionID]
	for i, a := range approvals {
		if a.ID == evt.RequestID {
			approvals[i].Status = evt.Result
			break
		}
	}
}

func (s *ProjectedStore) applySessionError(stored StoredEvent, sessionID string, data json.RawMessage) {
	var evt SessionErrorEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}
	s.Statuses[sessionID] = SessionStatus{
		SessionID: sessionID,
		Type:      "interrupted",
		Message:   evt.Message,
	}
}

// ——— Query Helpers ———

// GetProjection — 获取某个 session 的完整投影
func (s *ProjectedStore) GetProjection(sessionID string) *SessionProjection {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msgs := s.Messages[sessionID]
	status := s.Statuses[sessionID]
	approvals := s.Approvals[sessionID]

	// 计算汇总
	tokens := TokenUsage{}
	totalCost := 0.0
	for _, m := range msgs {
		if m.Tokens != nil {
			tokens.Input += m.Tokens.Input
			tokens.Output += m.Tokens.Output
			tokens.Reasoning += m.Tokens.Reasoning
			tokens.Cache.Read += m.Tokens.Cache.Read
			tokens.Cache.Write += m.Tokens.Cache.Write
		}
		totalCost += m.Cost
	}

	parts := make(map[string][]ProjectedPart)
	if partsByMsg, ok := s.Parts[sessionID]; ok {
		grouped := make(map[string][]ProjectedPart)
		for _, p := range partsByMsg {
			msgID := string(p.MessageID)
			grouped[msgID] = append(grouped[msgID], p)
		}
		parts = grouped
	}

	return &SessionProjection{
		Session: SessionInfo{
			ID:     sessionID,
			Status: status.Type,
			Tokens: tokens,
			Cost:   totalCost,
		},
		Messages:  msgs,
		Parts:     parts,
		Status:    status,
		Approvals: approvals,
		Diffs:     s.Diffs[sessionID],
	}
}

// GetMessages — 获取某个 session 的消息列表
func (s *ProjectedStore) GetMessages(sessionID string) []ProjectedMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Messages[sessionID]
}

// GetStatus — 获取某个 session 的实时状态
func (s *ProjectedStore) GetStatus(sessionID string) SessionStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Statuses[sessionID]
}

// GetApprovals — 获取某个 session 的审批队列
func (s *ProjectedStore) GetApprovals(sessionID string) []ProjectedApproval {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Approvals[sessionID]
}

// SetStatus — 外部设置 session 状态（非 Event 驱动）
func (s *ProjectedStore) SetStatus(sessionID string, status SessionStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Statuses[sessionID] = status
}

// ClearSession — 清空某个 session 的投影数据
func (s *ProjectedStore) ClearSession(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Messages, sessionID)
	delete(s.Parts, sessionID)
	delete(s.Statuses, sessionID)
	delete(s.Approvals, sessionID)
	delete(s.Diffs, sessionID)
	delete(s.PartCounter, sessionID)
}
