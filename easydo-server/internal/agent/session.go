package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"
)

// ============================================================
// Session Manager — 多 session 生命周期管理
// ============================================================

type SessionManager struct {
	db          *gorm.DB
	store       *EventStore
	projection  *ProjectedStore
	mu          sync.RWMutex
	sessions    map[string]*ManagedSession
	sessionSeq  map[string]int64 // sessionID → title counter
}

type ManagedSession struct {
	ID         string
	Agent      string
	Model      ModelRef
	CreatedAt  time.Time
	Status     string
	StatusChan chan SessionStatus
}

func NewSessionManager(db *gorm.DB) (*SessionManager, error) {
	eventStore := NewEventStore(db)
	projection := NewProjectedStore()

	mgr := &SessionManager{
		db:         db,
		store:      eventStore,
		projection: projection,
		sessions:   make(map[string]*ManagedSession),
		sessionSeq: make(map[string]int64),
	}

	// 订阅全部 Event → 自动投影
	eventStore.SubscribeAll(func(evt StoredEvent) {
		sessionID := evt.SessionID
		projection.ApplyEvent(evt)

		// 状态推导
		switch evt.Type {
		case "session.step.started":
			projection.SetStatus(sessionID, SessionStatus{
				SessionID: sessionID,
				Type:      "busy",
			})
			mgr.notifyStatus(sessionID, "busy")
		case "session.step.ended", "session.step.failed":
			projection.SetStatus(sessionID, SessionStatus{
				SessionID: sessionID,
				Type:      "idle",
			})
			mgr.notifyStatus(sessionID, "idle")
		case "session.error":
			mgr.notifyStatus(sessionID, "interrupted")
		}
	})

	return mgr, nil
}

// CreateSession — 创建新 session
func (m *SessionManager) CreateSession(ctx context.Context, agent string, model ModelRef) (string, error) {
	id := fmt.Sprintf("sess_%d", time.Now().UnixNano())
	now := time.Now().UTC()

	mgr := &ManagedSession{
		ID:         id,
		Agent:      agent,
		Model:      model,
		CreatedAt:  now,
		Status:     "idle",
		StatusChan: make(chan SessionStatus, 100),
	}

	m.mu.Lock()
	m.sessions[id] = mgr
	m.mu.Unlock()

	// 创建 sequence 记录
	seq := EventSequence{
		SessionID: id,
		Seq:       0,
		UpdatedAt: now,
	}
	if err := m.db.Create(&seq).Error; err != nil {
		return "", fmt.Errorf("create session seq: %w", err)
	}

	// 初始化状态
	m.projection.SetStatus(id, SessionStatus{
		SessionID: id,
		Type:      "idle",
	})

	return id, nil
}

// PublishEvent — 发布 Event 到某个 session
func (m *SessionManager) PublishEvent(ctx context.Context, event Event) (*StoredEvent, error) {
	m.mu.RLock()
	sessionID := event.(HasSessionID).GetSessionID()
	_, exists := m.sessions[sessionID]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	return m.store.Publish(ctx, event)
}

// GetSession — 获取 session 信息
func (m *SessionManager) GetSession(sessionID string) (*ManagedSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[sessionID]
	return s, ok
}

// GetProjection — 获取 session 完整投影
func (m *SessionManager) GetProjection(sessionID string) *SessionProjection {
	return m.projection.GetProjection(sessionID)
}

// ReplaySession — 重放 session 历史 Event 重建投影
func (m *SessionManager) ReplaySession(ctx context.Context, sessionID string) error {
	events, err := m.store.ReplaySession(ctx, sessionID, -1)
	if err != nil {
		return err
	}
	for _, evt := range events {
		m.projection.ApplyEvent(evt)
	}
	return nil
}

// SubscribeStatus — 订阅 session 状态变更
func (m *SessionManager) SubscribeStatus(sessionID string) (<-chan SessionStatus, func()) {
	m.mu.RLock()
	s, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		ch := make(chan SessionStatus)
		return ch, func() {}
	}

	m.mu.Lock()
	if s.StatusChan == nil {
		s.StatusChan = make(chan SessionStatus, 100)
	}
	m.mu.Unlock()

	// Create a new channel for each subscriber
	sub := make(chan SessionStatus, 100)

	go func() {
		for status := range s.StatusChan {
			select {
			case sub <- status:
			default:
			}
		}
	}()

	return sub, func() {
		// cleanup would need reference counting
	}
}

func (m *SessionManager) notifyStatus(sessionID string, status string) {
	m.mu.RLock()
	s, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return
	}
	ss := SessionStatus{
		SessionID: sessionID,
		Type:      status,
	}
	select {
	case s.StatusChan <- ss:
	default:
	}
}

// SubscribeEvents — 订阅 session 的 Event
func (m *SessionManager) SubscribeEvents(sessionID string, afterSeq int64) (<-chan StoredEvent, func()) {
	ch := make(chan StoredEvent, 100)

	cleanup := m.store.SubscribeAll(func(evt StoredEvent) {
		if evt.SessionID == sessionID {
			select {
			case ch <- evt:
			default:
			}
		}
	})

	// 重放历史
	ctx := context.Background()
	events, err := m.store.ReplaySession(ctx, sessionID, afterSeq)
	if err == nil {
		go func() {
			for _, evt := range events {
				select {
				case ch <- evt:
				default:
				}
			}
		}()
	}

	return ch, cleanup
}

// GetStore — 获取 EventStore 实例
func (m *SessionManager) GetStore() *EventStore {
	return m.store
}

// GetProjectionStore — 获取 ProjectedStore 实例
func (m *SessionManager) GetProjectionStore() *ProjectedStore {
	return m.projection
}

// ListSessions — 列出所有 session
func (m *SessionManager) ListSessions() []SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]SessionInfo, 0, len(m.sessions))
	for id, s := range m.sessions {
		proj := m.projection.GetProjection(id)
		var tokens TokenUsage
		var cost float64
		if proj != nil {
			tokens = proj.Session.Tokens
			cost = proj.Session.Cost
		}
		infos = append(infos, SessionInfo{
			ID:     id,
			Agent:  s.Agent,
			Model:  s.Model,
			Status: m.projection.GetStatus(id).Type,
			Tokens: tokens,
			Cost:   cost,
			Time: struct {
				Created int64 `json:"created"`
				Updated int64 `json:"updated"`
			}{
				Created: s.CreatedAt.UnixMilli(),
				Updated: s.CreatedAt.UnixMilli(),
			},
		})
	}
	return infos
}
