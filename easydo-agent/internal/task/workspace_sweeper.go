package task

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type WorkspaceSweeper struct {
	manager   *WorkspaceManager
	log       *logrus.Logger
	retention time.Duration
	interval  time.Duration
	now       func() time.Time

	mu       sync.Mutex
	running  bool
	stopChan chan struct{}
}

func NewWorkspaceSweeper(manager *WorkspaceManager, log *logrus.Logger, retention, interval time.Duration) *WorkspaceSweeper {
	return &WorkspaceSweeper{
		manager:   manager,
		log:       log,
		retention: retention,
		interval:  interval,
		now:       time.Now,
		stopChan:  make(chan struct{}),
	}
}

func (s *WorkspaceSweeper) Start(ctx context.Context) {
	if s == nil || s.manager == nil || s.interval <= 0 || s.retention <= 0 {
		return
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()
	go s.run(ctx)
}

func (s *WorkspaceSweeper) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.stopChan)
	s.mu.Unlock()
}

func (s *WorkspaceSweeper) run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopChan:
			return
		case <-ticker.C:
			if _, err := s.runOnce(ctx); err != nil && s.log != nil {
				s.log.Warnf("Workspace sweeper failed: %v", err)
			}
		}
	}
}

func (s *WorkspaceSweeper) runOnce(ctx context.Context) ([]string, error) {
	if s == nil || s.manager == nil {
		return nil, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	deleted, err := s.manager.SweepExpiredWorkspaces(s.now(), s.retention)
	if err != nil {
		return deleted, err
	}
	if len(deleted) > 0 && s.log != nil {
		s.log.Infof("Workspace sweeper removed %d expired workspaces", len(deleted))
	}
	return deleted, nil
}
