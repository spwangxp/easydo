package handlers

import (
	"testing"

	"easydo-server/internal/models"
	"easydo-server/pkg/utils"
)

func TestRebindExecutionTasksForReconnect_RebindsAckedAndRunningTasks(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	models.DB = db
	t.Cleanup(func() {
		models.DB = previousDB
	})

	agent := models.Agent{
		Name:               "agent-reconnect",
		Host:               "host",
		Port:               1,
		Token:              "token",
		Status:             models.AgentStatusOnline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	run := models.PipelineRun{PipelineID: 1, BuildNumber: 1, Status: models.PipelineRunStatusRunning, Config: `{"version":"2.0","nodes":[],"edges":[]}`}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}

	acked := models.AgentTask{AgentID: agent.ID, PipelineRunID: run.ID, WorkspaceID: 1, NodeID: "acked", Name: "acked", TaskType: "shell", Status: models.TaskStatusAcked, AgentSessionID: "session-old", OwnerServerID: "server-1"}
	running := models.AgentTask{AgentID: agent.ID, PipelineRunID: run.ID, WorkspaceID: 1, NodeID: "running", Name: "running", TaskType: "shell", Status: models.TaskStatusRunning, AgentSessionID: "session-old", OwnerServerID: "server-1"}
	pulling := models.AgentTask{AgentID: agent.ID, PipelineRunID: run.ID, WorkspaceID: 1, NodeID: "pulling", Name: "pulling", TaskType: "shell", Status: models.TaskStatusPulling, AgentSessionID: "session-old", OwnerServerID: "server-1"}
	completed := models.AgentTask{AgentID: agent.ID, PipelineRunID: run.ID, WorkspaceID: 1, NodeID: "done", Name: "done", TaskType: "shell", Status: models.TaskStatusExecuteSuccess, AgentSessionID: "session-old", OwnerServerID: "server-1"}
	for _, task := range []*models.AgentTask{&acked, &running, &pulling, &completed} {
		if err := db.Create(task).Error; err != nil {
			t.Fatalf("create task %s failed: %v", task.NodeID, err)
		}
	}

	handler := NewWebSocketHandler()
	client := &wsClient{agentID: agent.ID, sessionID: "session-new", serverID: "server-2"}
	handler.rebindExecutionTasksForReconnect(client, &agent)

	assertTask := func(taskID uint64, wantStatus, wantSession, wantOwner string) {
		t.Helper()
		var got models.AgentTask
		if err := db.First(&got, taskID).Error; err != nil {
			t.Fatalf("reload task %d failed: %v", taskID, err)
		}
		if got.Status != wantStatus {
			t.Fatalf("task %d status=%s, want=%s", taskID, got.Status, wantStatus)
		}
		if got.AgentSessionID != wantSession {
			t.Fatalf("task %d session=%s, want=%s", taskID, got.AgentSessionID, wantSession)
		}
		if got.OwnerServerID != wantOwner {
			t.Fatalf("task %d owner=%s, want=%s", taskID, got.OwnerServerID, wantOwner)
		}
	}

	assertTask(acked.ID, models.TaskStatusAcked, "session-new", "server-2")
	assertTask(running.ID, models.TaskStatusRunning, "session-new", "server-2")
	assertTask(pulling.ID, models.TaskStatusPulling, "session-old", "server-1")
	assertTask(completed.ID, models.TaskStatusExecuteSuccess, "session-old", "server-1")
}

func TestShouldDispatchAgentStreamEvent_OnlyDispatchStagesAreEligible(t *testing.T) {
	event := utils.AgentStreamEvent{TaskID: 11, DispatchToken: "token-1", DispatchAttempt: 2}

	cases := []struct {
		name string
		task models.AgentTask
		want bool
	}{
		{
			name: "dispatching task is eligible",
			task: models.AgentTask{BaseModel: models.BaseModel{ID: 11}, AgentID: 9, Status: models.TaskStatusDispatching, DispatchToken: "token-1", DispatchAttempt: 2},
			want: true,
		},
		{
			name: "pulling task is eligible",
			task: models.AgentTask{BaseModel: models.BaseModel{ID: 11}, AgentID: 9, Status: models.TaskStatusPulling, DispatchToken: "token-1", DispatchAttempt: 2},
			want: true,
		},
		{
			name: "running task is not eligible",
			task: models.AgentTask{BaseModel: models.BaseModel{ID: 11}, AgentID: 9, Status: models.TaskStatusRunning, DispatchToken: "token-1", DispatchAttempt: 2},
			want: false,
		},
		{
			name: "acked task is not eligible",
			task: models.AgentTask{BaseModel: models.BaseModel{ID: 11}, AgentID: 9, Status: models.TaskStatusAcked, DispatchToken: "token-1", DispatchAttempt: 2},
			want: false,
		},
		{
			name: "mismatched dispatch token is not eligible",
			task: models.AgentTask{BaseModel: models.BaseModel{ID: 11}, AgentID: 9, Status: models.TaskStatusDispatching, DispatchToken: "token-2", DispatchAttempt: 2},
			want: false,
		},
		{
			name: "mismatched dispatch attempt is not eligible",
			task: models.AgentTask{BaseModel: models.BaseModel{ID: 11}, AgentID: 9, Status: models.TaskStatusDispatching, DispatchToken: "token-1", DispatchAttempt: 3},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldDispatchAgentStreamEvent(&tc.task, event)
			if got != tc.want {
				t.Fatalf("shouldDispatchAgentStreamEvent()=%v, want=%v", got, tc.want)
			}
		})
	}
}
