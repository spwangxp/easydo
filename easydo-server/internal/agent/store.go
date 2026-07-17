package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"
)

// ============================================================
// Event Store — 持久化 Event 日志 + 序列 + 发布订阅
// 参考 OpenCode event.ts 的 publish/subscribe/durable/replay
// ============================================================

// StoredEvent — 数据库行映射
type StoredEvent struct {
	ID        string          `gorm:"column:id;primaryKey;size:64"`
	SessionID string          `gorm:"column:session_id;size:64;not null;index:idx_session_seq,unique"`
	Seq       int64           `gorm:"column:seq;not null;index:idx_session_seq,unique"`
	Type      string          `gorm:"column:type;size:128;not null"`
	Data      json.RawMessage `gorm:"column:data;type:json;not null"`
	CreatedAt time.Time       `gorm:"column:created_at;not null"`
}

func (StoredEvent) TableName() string { return "agent_events" }

// EventSequence — 序列表（保证 seq 单调递增）
type EventSequence struct {
	SessionID string    `gorm:"column:session_id;primaryKey;size:64"`
	Seq       int64     `gorm:"column:seq;not null"`
	OwnerID   string    `gorm:"column:owner_id;size:64"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
}

func (EventSequence) TableName() string { return "agent_event_sequences" }

// Subscriber — Event 订阅者
type Subscriber func(event StoredEvent)

// EventStore — Event 存储服务
type EventStore struct {
	db      *gorm.DB
	mu      sync.RWMutex
	subs    map[string][]Subscriber
	allSubs []Subscriber
}

func NewEventStore(db *gorm.DB) *EventStore {
	return &EventStore{
		db:      db,
		subs:    make(map[string][]Subscriber),
		allSubs: make([]Subscriber, 0),
	}
}

// Publish — 持久化 Event 并通知订阅者
func (s *EventStore) Publish(ctx context.Context, event Event) (*StoredEvent, error) {
	sessID := event.(interface{ GetSessionID() string }).(interface{ GetSessionID() string}).GetSessionID()
	eventType := event.EventType()

	// 非 durable 事件不持久化
	if !isDurable(eventType) {
		return nil, nil
	}

	data, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("event marshal: %w", err)
	}

	evtID := newEventID()
	now := time.Now().UTC()

	var stored StoredEvent
	err = s.db.Transaction(func(tx *gorm.DB) error {
		// 获取或创建 sequence
		var seq EventSequence
		result := tx.Where("session_id = ?", sessID).First(&seq)
		if result.Error != nil {
			if result.Error == gorm.ErrRecordNotFound {
				seq = EventSequence{
					SessionID: sessID,
					Seq:       0,
					UpdatedAt: now,
				}
				if err := tx.Create(&seq).Error; err != nil {
					return err
				}
			} else {
				return result.Error
			}
		}

		nextSeq := seq.Seq + 1
		stored = StoredEvent{
			ID:        evtID,
			SessionID: sessID,
			Seq:       nextSeq,
			Type:      eventType,
			Data:      data,
			CreatedAt: now,
		}

		if err := tx.Create(&stored).Error; err != nil {
			return err
		}

		return tx.Model(&EventSequence{}).
			Where("session_id = ? AND seq = ?", sessID, seq.Seq).
			Update("seq", nextSeq).Error
	})

	if err != nil {
		return nil, fmt.Errorf("event persist: %w", err)
	}

	// 通知订阅者
	s.notify(stored)

	return &stored, nil
}

// Subscribe — 订阅指定类型的 Event
func (s *EventStore) Subscribe(eventType string, sub Subscriber) func() {
	s.mu.Lock()
	s.subs[eventType] = append(s.subs[eventType], sub)
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		list := s.subs[eventType]
		for i, fn := range list {
			if fmt.Sprintf("%p", fn) == fmt.Sprintf("%p", sub) {
				s.subs[eventType] = append(list[:i], list[i+1:]...)
				break
			}
		}
	}
}

// SubscribeAll — 订阅所有 Event
func (s *EventStore) SubscribeAll(sub Subscriber) func() {
	s.mu.Lock()
	s.allSubs = append(s.allSubs, sub)
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i, fn := range s.allSubs {
			if fmt.Sprintf("%p", fn) == fmt.Sprintf("%p", sub) {
				s.allSubs = append(s.allSubs[:i], s.allSubs[i+1:]...)
				break
			}
		}
	}
}

// ReplaySession — 重放某个 session 的历史 Event
func (s *EventStore) ReplaySession(ctx context.Context, sessionID string, afterSeq int64) ([]StoredEvent, error) {
	var events []StoredEvent
	query := s.db.Where("session_id = ? AND seq > ?", sessionID, afterSeq).
		Order("seq ASC").
		Find(&events)
	if query.Error != nil {
		return nil, fmt.Errorf("replay events: %w", query.Error)
	}
	return events, nil
}

// LatestSeq — 获取 session 最新 seq
func (s *EventStore) LatestSeq(ctx context.Context, sessionID string) (int64, error) {
	var seq EventSequence
	result := s.db.Where("session_id = ?", sessionID).First(&seq)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return -1, nil
		}
		return -1, result.Error
	}
	return seq.Seq, nil
}

func (s *EventStore) notify(evt StoredEvent) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, sub := range s.allSubs {
		sub(evt)
	}
	if subs, ok := s.subs[evt.Type]; ok {
		for _, sub := range subs {
			sub(evt)
		}
	}
}

// ——— 辅助函数 ———

func isDurable(eventType string) bool {
	// Delta 事件不持久化
	switch eventType {
	case "session.text.delta",
		"session.reasoning.delta",
		"session.tool.input.delta":
		return false
	}
	return true
}

func newEventID() string {
	return fmt.Sprintf("evt_%d_%06d", time.Now().UnixNano(), time.Now().UnixMilli()%1000000)
}

// GetSessionID 接口用于从 Event 中提取 SessionID
type HasSessionID interface {
	GetSessionID() string
}

func (b EventBase) GetSessionID() string { return b.SessionID }
