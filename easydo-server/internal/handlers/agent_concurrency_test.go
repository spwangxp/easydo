package handlers

import (
	"testing"

	"easydo-server/internal/models"
)

func TestNormalizeAgentMaxConcurrentPipelines(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{name: "negative uses default", in: -1, want: defaultAgentMaxConcurrentPipelines},
		{name: "zero uses default", in: 0, want: defaultAgentMaxConcurrentPipelines},
		{name: "positive keeps value", in: 7, want: 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeAgentMaxConcurrentPipelines(tt.in)
			if got != tt.want {
				t.Fatalf("normalizeAgentMaxConcurrentPipelines(%d)=%d, want=%d", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeAgentTaskConcurrency(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{name: "negative uses default", in: -1, want: 5},
		{name: "zero uses default", in: 0, want: 5},
		{name: "positive keeps value", in: 7, want: 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeAgentTaskConcurrency(tt.in)
			if got != tt.want {
				t.Fatalf("normalizeAgentTaskConcurrency(%d)=%d, want=%d", tt.in, got, tt.want)
			}
		})
	}
}

func TestSelectAgentWithPipelineCapacity_PicksLeastLoaded(t *testing.T) {
	db := openHandlerTestDB(t)

	agents := []models.Agent{
		{
			Name:                   "at-capacity",
			Host:                   "a1",
			Port:                   1,
			Token:                  "t1",
			Status:                 models.AgentStatusOnline,
			RegistrationStatus:     models.AgentRegistrationStatusApproved,
			MaxConcurrentPipelines: 1,
			ScopeType:              models.AgentScopeWorkspace,
			WorkspaceID:            1,
		},
		{
			Name:                   "busy-with-capacity",
			Host:                   "a2",
			Port:                   2,
			Token:                  "t2",
			Status:                 models.AgentStatusBusy,
			RegistrationStatus:     models.AgentRegistrationStatusApproved,
			MaxConcurrentPipelines: 3,
			ScopeType:              models.AgentScopeWorkspace,
			WorkspaceID:            1,
		},
		{
			Name:                   "least-loaded",
			Host:                   "a3",
			Port:                   3,
			Token:                  "t3",
			Status:                 models.AgentStatusOnline,
			RegistrationStatus:     models.AgentRegistrationStatusApproved,
			MaxConcurrentPipelines: 2,
			ScopeType:              models.AgentScopeWorkspace,
			WorkspaceID:            1,
		},
		{
			Name:                   "pending-agent",
			Host:                   "a4",
			Port:                   4,
			Token:                  "t4",
			Status:                 models.AgentStatusOnline,
			RegistrationStatus:     models.AgentRegistrationStatusPending,
			MaxConcurrentPipelines: 2,
			ScopeType:              models.AgentScopeWorkspace,
			WorkspaceID:            1,
		},
	}
	if err := db.Create(&agents).Error; err != nil {
		t.Fatalf("create agents failed: %v", err)
	}

	runs := []models.PipelineRun{
		{WorkspaceID: 1, PipelineID: 1, BuildNumber: 1, AgentID: agents[0].ID, Status: models.PipelineRunStatusRunning},
		{WorkspaceID: 1, PipelineID: 2, BuildNumber: 1, AgentID: agents[1].ID, Status: models.PipelineRunStatusRunning},
	}
	if err := db.Create(&runs).Error; err != nil {
		t.Fatalf("create runs failed: %v", err)
	}

	selected := selectAgentWithPipelineCapacity(db, 1)
	if selected != agents[2].ID {
		t.Fatalf("selected=%d, want=%d", selected, agents[2].ID)
	}

	// Fill the remaining agent capacity so only agent[1] is available.
	fill := []models.PipelineRun{
		{WorkspaceID: 1, PipelineID: 3, BuildNumber: 1, AgentID: agents[2].ID, Status: models.PipelineRunStatusRunning},
		{WorkspaceID: 1, PipelineID: 4, BuildNumber: 1, AgentID: agents[2].ID, Status: models.PipelineRunStatusRunning},
	}
	if err := db.Create(&fill).Error; err != nil {
		t.Fatalf("fill runs failed: %v", err)
	}

	selected = selectAgentWithPipelineCapacity(db, 1)
	if selected != agents[1].ID {
		t.Fatalf("selected=%d, want=%d", selected, agents[1].ID)
	}
}

func TestSelectAgentWithPipelineCapacity_ReturnsZeroWhenAllAtCapacity(t *testing.T) {
	db := openHandlerTestDB(t)

	agent := models.Agent{
		Name:                   "only-agent",
		Host:                   "a1",
		Port:                   1,
		Token:                  "t1",
		Status:                 models.AgentStatusOnline,
		RegistrationStatus:     models.AgentRegistrationStatusApproved,
		MaxConcurrentPipelines: 1,
		ScopeType:              models.AgentScopeWorkspace,
		WorkspaceID:            1,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}
	if err := db.Create(&models.PipelineRun{
		WorkspaceID: 1, PipelineID: 1, BuildNumber: 1, AgentID: agent.ID, Status: models.PipelineRunStatusRunning,
	}).Error; err != nil {
		t.Fatalf("create running run failed: %v", err)
	}

	selected := selectAgentWithPipelineCapacity(db, 1)
	if selected != 0 {
		t.Fatalf("selected=%d, want=0", selected)
	}
}

func TestUpdateAgentStatusByPipelineConcurrency_Transitions(t *testing.T) {
	db := openHandlerTestDB(t)

	agent := models.Agent{
		Name:                   "transitions",
		Host:                   "host",
		Port:                   1,
		Token:                  "token",
		Status:                 models.AgentStatusOnline,
		RegistrationStatus:     models.AgentRegistrationStatusApproved,
		MaxConcurrentPipelines: 1,
		ScopeType:              models.AgentScopeWorkspace,
		WorkspaceID:            1,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	updateAgentStatusByPipelineConcurrency(db, agent.ID)
	var current models.Agent
	if err := db.First(&current, agent.ID).Error; err != nil {
		t.Fatalf("get agent failed: %v", err)
	}
	if current.Status != models.AgentStatusOnline {
		t.Fatalf("status=%s, want=%s", current.Status, models.AgentStatusOnline)
	}

	runningRun := models.PipelineRun{
		WorkspaceID: 1, PipelineID: 1, BuildNumber: 1, AgentID: agent.ID, Status: models.PipelineRunStatusRunning,
	}
	if err := db.Create(&runningRun).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}

	updateAgentStatusByPipelineConcurrency(db, agent.ID)
	if err := db.First(&current, agent.ID).Error; err != nil {
		t.Fatalf("get agent failed: %v", err)
	}
	if current.Status != models.AgentStatusBusy {
		t.Fatalf("status=%s, want=%s", current.Status, models.AgentStatusBusy)
	}

	if err := db.Model(&runningRun).Update("status", models.PipelineRunStatusSuccess).Error; err != nil {
		t.Fatalf("update run status failed: %v", err)
	}
	updateAgentStatusByPipelineConcurrency(db, agent.ID)
	if err := db.First(&current, agent.ID).Error; err != nil {
		t.Fatalf("get agent failed: %v", err)
	}
	if current.Status != models.AgentStatusOnline {
		t.Fatalf("status=%s, want=%s", current.Status, models.AgentStatusOnline)
	}
}

func TestUpdateAgentStatusByPipelineConcurrency_RespectsOfflineAndError(t *testing.T) {
	db := openHandlerTestDB(t)

	offline := models.Agent{
		Name:                   "offline-agent",
		Host:                   "host1",
		Port:                   1,
		Token:                  "token1",
		Status:                 models.AgentStatusOffline,
		RegistrationStatus:     models.AgentRegistrationStatusApproved,
		MaxConcurrentPipelines: 1,
		ScopeType:              models.AgentScopeWorkspace,
		WorkspaceID:            1,
	}
	errAgent := models.Agent{
		Name:                   "error-agent",
		Host:                   "host2",
		Port:                   2,
		Token:                  "token2",
		Status:                 models.AgentStatusError,
		RegistrationStatus:     models.AgentRegistrationStatusApproved,
		MaxConcurrentPipelines: 1,
		ScopeType:              models.AgentScopeWorkspace,
		WorkspaceID:            1,
	}
	if err := db.Create(&offline).Error; err != nil {
		t.Fatalf("create offline agent failed: %v", err)
	}
	if err := db.Create(&errAgent).Error; err != nil {
		t.Fatalf("create error agent failed: %v", err)
	}

	_ = db.Create(&models.PipelineRun{
		WorkspaceID: 1, PipelineID: 1, BuildNumber: 1, AgentID: offline.ID, Status: models.PipelineRunStatusRunning,
	}).Error
	_ = db.Create(&models.PipelineRun{
		WorkspaceID: 1, PipelineID: 2, BuildNumber: 1, AgentID: errAgent.ID, Status: models.PipelineRunStatusRunning,
	}).Error

	updateAgentStatusByPipelineConcurrency(db, offline.ID)
	updateAgentStatusByPipelineConcurrency(db, errAgent.ID)

	var gotOffline models.Agent
	var gotError models.Agent
	if err := db.First(&gotOffline, offline.ID).Error; err != nil {
		t.Fatalf("get offline failed: %v", err)
	}
	if err := db.First(&gotError, errAgent.ID).Error; err != nil {
		t.Fatalf("get error failed: %v", err)
	}
	if gotOffline.Status != models.AgentStatusOffline {
		t.Fatalf("offline status changed to %s", gotOffline.Status)
	}
	if gotError.Status != models.AgentStatusError {
		t.Fatalf("error status changed to %s", gotError.Status)
	}
}
