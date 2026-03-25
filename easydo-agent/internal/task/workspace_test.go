package task

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func newWorkspaceTestLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return logger
}

func TestWorkspaceManagerTouchWorkspaceMarker_CreatesAndUpdatesMarker(t *testing.T) {
	wm := NewWorkspaceManager(t.TempDir(), newWorkspaceTestLogger())
	workspacePath, err := wm.CreateWorkspace(101)
	if err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}

	if err := wm.TouchWorkspaceMarker(workspacePath, time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatalf("touch workspace marker failed: %v", err)
	}

	markerPath := filepath.Join(workspacePath, workspaceLastUsedMarkerName)
	infoBefore, err := os.Stat(markerPath)
	if err != nil {
		t.Fatalf("stat marker failed: %v", err)
	}

	later := time.Now().Add(-30 * time.Minute)
	if err := wm.TouchWorkspaceMarker(workspacePath, later); err != nil {
		t.Fatalf("retouch workspace marker failed: %v", err)
	}

	infoAfter, err := os.Stat(markerPath)
	if err != nil {
		t.Fatalf("stat updated marker failed: %v", err)
	}
	if !infoAfter.ModTime().After(infoBefore.ModTime()) {
		t.Fatalf("marker modtime=%v, want after %v", infoAfter.ModTime(), infoBefore.ModTime())
	}
	if got := infoAfter.ModTime(); got.Before(later.Add(-time.Second)) || got.After(later.Add(time.Second)) {
		t.Fatalf("marker modtime=%v, want near %v", got, later)
	}
}

func TestWorkspaceManagerSweepExpiredWorkspaces_RemovesOnlyExpiredInactiveWorkspaces(t *testing.T) {
	wm := NewWorkspaceManager(t.TempDir(), newWorkspaceTestLogger())
	now := time.Now()

	expiredPath, err := wm.CreateWorkspace(201)
	if err != nil {
		t.Fatalf("create expired workspace failed: %v", err)
	}
	if err := wm.TouchWorkspaceMarker(expiredPath, now.Add(-25*time.Hour)); err != nil {
		t.Fatalf("touch expired marker failed: %v", err)
	}

	recentPath, err := wm.CreateWorkspace(202)
	if err != nil {
		t.Fatalf("create recent workspace failed: %v", err)
	}
	if err := wm.TouchWorkspaceMarker(recentPath, now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("touch recent marker failed: %v", err)
	}

	activePath, err := wm.CreateWorkspace(203)
	if err != nil {
		t.Fatalf("create active workspace failed: %v", err)
	}
	if err := wm.TouchWorkspaceMarker(activePath, now.Add(-26*time.Hour)); err != nil {
		t.Fatalf("touch active marker failed: %v", err)
	}
	wm.MarkWorkspaceActive(activePath)
	t.Cleanup(func() {
		wm.MarkWorkspaceInactive(activePath)
	})

	deleted, err := wm.SweepExpiredWorkspaces(now, 24*time.Hour)
	if err != nil {
		t.Fatalf("sweep expired workspaces failed: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != expiredPath {
		t.Fatalf("deleted=%v, want [%s]", deleted, expiredPath)
	}
	if _, err := os.Stat(expiredPath); !os.IsNotExist(err) {
		t.Fatalf("expired workspace should be removed, stat err=%v", err)
	}
	if _, err := os.Stat(recentPath); err != nil {
		t.Fatalf("recent workspace should be kept: %v", err)
	}
	if _, err := os.Stat(activePath); err != nil {
		t.Fatalf("active workspace should be kept: %v", err)
	}
}

func TestWorkspaceSweeperRunOnce_RemovesExpiredWorkspace(t *testing.T) {
	wm := NewWorkspaceManager(t.TempDir(), newWorkspaceTestLogger())
	now := time.Now()

	expiredPath, err := wm.CreateWorkspace(301)
	if err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := wm.TouchWorkspaceMarker(expiredPath, now.Add(-25*time.Hour)); err != nil {
		t.Fatalf("touch marker failed: %v", err)
	}

	sweeper := NewWorkspaceSweeper(wm, newWorkspaceTestLogger(), 24*time.Hour, time.Hour)
	sweeper.now = func() time.Time { return now }

	deleted, err := sweeper.runOnce(context.Background())
	if err != nil {
		t.Fatalf("run sweeper once failed: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != expiredPath {
		t.Fatalf("deleted=%v, want [%s]", deleted, expiredPath)
	}
	if _, err := os.Stat(expiredPath); !os.IsNotExist(err) {
		t.Fatalf("expired workspace should be removed, stat err=%v", err)
	}
}
