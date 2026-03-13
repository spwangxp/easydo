package handlers

import (
	"testing"
	"time"

	"easydo-server/internal/config"
	"easydo-server/internal/models"
)

func TestReconcileDispatchTimeouts_MarksExpiredDispatchingTask(t *testing.T) {
	config.Init()
	config.Config.Set("task.dispatch_timeout", "1s")

	db := openHandlerTestDB(t)
	previousDB := models.DB
	models.DB = db
	t.Cleanup(func() {
		models.DB = previousDB
	})

	agent := models.Agent{
		Name:               "timeout-agent",
		Host:               "host",
		Port:               1,
		Token:              "token",
		Status:             models.AgentStatusOnline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	task := models.AgentTask{
		AgentID:         agent.ID,
		PipelineRunID:   1,
		WorkspaceID:     1,
		NodeID:          "n1",
		Name:            "dispatch-timeout",
		TaskType:        "shell",
		Status:          models.TaskStatusDispatching,
		DispatchToken:   "dispatch-token",
		DispatchAttempt: 1,
		LeaseExpireAt:   time.Now().Add(-time.Minute).Unix(),
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	handler := NewWebSocketHandler()
	updated, err := handler.reconcileDispatchTimeouts(db, time.Now().Unix())
	if err != nil {
		t.Fatalf("reconcileDispatchTimeouts returned error: %v", err)
	}
	if updated != 1 {
		t.Fatalf("updated=%d, want=1", updated)
	}

	var reloaded models.AgentTask
	if err := db.First(&reloaded, task.ID).Error; err != nil {
		t.Fatalf("reload task failed: %v", err)
	}
	if reloaded.Status != models.TaskStatusDispatchTimeout {
		t.Fatalf("task status=%s, want=%s", reloaded.Status, models.TaskStatusDispatchTimeout)
	}
	if reloaded.OwnerServerID != "" || reloaded.AgentSessionID != "" {
		t.Fatalf("expected ownership cleared after timeout, got owner=%s session=%s", reloaded.OwnerServerID, reloaded.AgentSessionID)
	}
}

func TestReconcileDispatchTimeouts_DoesNotExpireExecutionStageTasks(t *testing.T) {
	config.Init()
	config.Config.Set("task.dispatch_timeout", "1s")

	db := openHandlerTestDB(t)
	previousDB := models.DB
	models.DB = db
	t.Cleanup(func() {
		models.DB = previousDB
	})

	task := models.AgentTask{
		AgentID:         1,
		PipelineRunID:   1,
		WorkspaceID:     1,
		NodeID:          "n1",
		Name:            "execution-stage",
		TaskType:        "shell",
		Status:          models.TaskStatusAcked,
		DispatchToken:   "dispatch-token",
		DispatchAttempt: 1,
		LeaseExpireAt:   time.Now().Add(-time.Minute).Unix(),
		AgentSessionID:  "session-old",
		OwnerServerID:   "server-old",
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	handler := NewWebSocketHandler()
	updated, err := handler.reconcileDispatchTimeouts(db, time.Now().Unix())
	if err != nil {
		t.Fatalf("reconcileDispatchTimeouts returned error: %v", err)
	}
	if updated != 0 {
		t.Fatalf("updated=%d, want=0", updated)
	}

	var reloaded models.AgentTask
	if err := db.First(&reloaded, task.ID).Error; err != nil {
		t.Fatalf("reload task failed: %v", err)
	}
	if reloaded.Status != models.TaskStatusAcked {
		t.Fatalf("task status=%s, want=%s", reloaded.Status, models.TaskStatusAcked)
	}
}
