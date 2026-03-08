package handlers

import (
	"testing"
	"time"

	"easydo-server/internal/models"
)

func TestAssignOneQueuedRun_AssignsOldestQueuedRun(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}

	agent := models.Agent{
		Name:                   "scheduler-agent",
		Host:                   "host",
		Port:                   1,
		Token:                  "token",
		Status:                 models.AgentStatusOnline,
		RegistrationStatus:     models.AgentRegistrationStatusApproved,
		MaxConcurrentPipelines: 1,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	oldRun := models.PipelineRun{
		PipelineID:  1,
		BuildNumber: 1,
		Status:      models.PipelineRunStatusQueued,
		Config:      `{"version":"2.0","nodes":[],"edges":[]}`,
	}
	newRun := models.PipelineRun{
		PipelineID:  2,
		BuildNumber: 1,
		Status:      models.PipelineRunStatusQueued,
		Config:      `{"version":"2.0","nodes":[],"edges":[]}`,
	}
	if err := db.Create(&oldRun).Error; err != nil {
		t.Fatalf("create old run failed: %v", err)
	}
	if err := db.Create(&newRun).Error; err != nil {
		t.Fatalf("create new run failed: %v", err)
	}
	if err := db.Model(&oldRun).Update("created_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatalf("set old run created_at failed: %v", err)
	}
	if err := db.Model(&newRun).Update("created_at", time.Now()).Error; err != nil {
		t.Fatalf("set new run created_at failed: %v", err)
	}

	runID, ok := h.assignOneQueuedRun(db)
	if !ok {
		t.Fatalf("assignOneQueuedRun returned not ok")
	}
	if runID != oldRun.ID {
		t.Fatalf("assigned run=%d, want oldest run=%d", runID, oldRun.ID)
	}

	var gotOld models.PipelineRun
	var gotNew models.PipelineRun
	if err := db.First(&gotOld, oldRun.ID).Error; err != nil {
		t.Fatalf("reload old run failed: %v", err)
	}
	if err := db.First(&gotNew, newRun.ID).Error; err != nil {
		t.Fatalf("reload new run failed: %v", err)
	}

	if gotOld.Status != models.PipelineRunStatusRunning {
		t.Fatalf("old run status=%s, want=%s", gotOld.Status, models.PipelineRunStatusRunning)
	}
	if gotOld.AgentID != agent.ID {
		t.Fatalf("old run agent_id=%d, want=%d", gotOld.AgentID, agent.ID)
	}
	if gotOld.StartTime == 0 {
		t.Fatalf("old run start_time should be set")
	}
	if gotNew.Status != models.PipelineRunStatusQueued {
		t.Fatalf("new run status=%s, want=%s", gotNew.Status, models.PipelineRunStatusQueued)
	}
}

func TestAssignOneQueuedRun_NoCapacityKeepsQueued(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}

	agent := models.Agent{
		Name:                   "scheduler-agent",
		Host:                   "host",
		Port:                   1,
		Token:                  "token",
		Status:                 models.AgentStatusOnline,
		RegistrationStatus:     models.AgentRegistrationStatusApproved,
		MaxConcurrentPipelines: 1,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	running := models.PipelineRun{
		PipelineID:  1,
		BuildNumber: 1,
		AgentID:     agent.ID,
		Status:      models.PipelineRunStatusRunning,
	}
	queued := models.PipelineRun{
		PipelineID:  2,
		BuildNumber: 1,
		Status:      models.PipelineRunStatusQueued,
		Config:      `{"version":"2.0","nodes":[],"edges":[]}`,
	}
	if err := db.Create(&running).Error; err != nil {
		t.Fatalf("create running run failed: %v", err)
	}
	if err := db.Create(&queued).Error; err != nil {
		t.Fatalf("create queued run failed: %v", err)
	}

	runID, ok := h.assignOneQueuedRun(db)
	if ok {
		t.Fatalf("expected not ok when no capacity, got runID=%d", runID)
	}

	var got models.PipelineRun
	if err := db.First(&got, queued.ID).Error; err != nil {
		t.Fatalf("reload queued run failed: %v", err)
	}
	if got.Status != models.PipelineRunStatusQueued {
		t.Fatalf("queued run status=%s, want=%s", got.Status, models.PipelineRunStatusQueued)
	}
}

func TestAssignOneQueuedRun_UpdatesAgentStatusToBusyAfterAssignment(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}

	agent := models.Agent{
		Name:                   "scheduler-agent",
		Host:                   "host",
		Port:                   1,
		Token:                  "token",
		Status:                 models.AgentStatusOnline,
		RegistrationStatus:     models.AgentRegistrationStatusApproved,
		MaxConcurrentPipelines: 1,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}
	queued := models.PipelineRun{
		PipelineID:  1,
		BuildNumber: 1,
		Status:      models.PipelineRunStatusQueued,
		Config:      `{"version":"2.0","nodes":[],"edges":[]}`,
	}
	if err := db.Create(&queued).Error; err != nil {
		t.Fatalf("create queued run failed: %v", err)
	}

	if _, ok := h.assignOneQueuedRun(db); !ok {
		t.Fatalf("assignOneQueuedRun returned not ok")
	}

	var gotAgent models.Agent
	if err := db.First(&gotAgent, agent.ID).Error; err != nil {
		t.Fatalf("reload agent failed: %v", err)
	}
	if gotAgent.Status != models.AgentStatusBusy {
		t.Fatalf("agent status=%s, want=%s", gotAgent.Status, models.AgentStatusBusy)
	}
}

func TestAssignOneQueuedRun_UpdatesExistingQueuedTasksToPendingAndSelectedAgent(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}

	agentOld := models.Agent{
		Name:                   "old-agent",
		Host:                   "old-host",
		Port:                   1,
		Token:                  "old-token",
		Status:                 models.AgentStatusOnline,
		RegistrationStatus:     models.AgentRegistrationStatusApproved,
		MaxConcurrentPipelines: 1,
	}
	agentSelected := models.Agent{
		Name:                   "selected-agent",
		Host:                   "selected-host",
		Port:                   2,
		Token:                  "selected-token",
		Status:                 models.AgentStatusOnline,
		RegistrationStatus:     models.AgentRegistrationStatusApproved,
		MaxConcurrentPipelines: 1,
	}
	if err := db.Create(&agentOld).Error; err != nil {
		t.Fatalf("create old agent failed: %v", err)
	}
	if err := db.Create(&agentSelected).Error; err != nil {
		t.Fatalf("create selected agent failed: %v", err)
	}

	// Fill old agent capacity so scheduler must pick selected agent.
	if err := db.Create(&models.PipelineRun{
		PipelineID: 1, BuildNumber: 1, AgentID: agentOld.ID, Status: models.PipelineRunStatusRunning,
	}).Error; err != nil {
		t.Fatalf("create running run for old agent failed: %v", err)
	}

	queuedRun := models.PipelineRun{
		PipelineID:  2,
		BuildNumber: 1,
		Status:      models.PipelineRunStatusQueued,
		Config:      `{"version":"2.0","nodes":[],"edges":[]}`,
	}
	if err := db.Create(&queuedRun).Error; err != nil {
		t.Fatalf("create queued run failed: %v", err)
	}
	queuedTask := models.AgentTask{
		AgentID:       agentOld.ID,
		PipelineRunID: queuedRun.ID,
		NodeID:        "entry-node",
		TaskType:      "sleep",
		Name:          "entry-task",
		Status:        models.TaskStatusQueued,
		Timeout:       30,
	}
	if err := db.Create(&queuedTask).Error; err != nil {
		t.Fatalf("create queued task failed: %v", err)
	}

	if _, ok := h.assignOneQueuedRun(db); !ok {
		t.Fatalf("assignOneQueuedRun returned not ok")
	}

	var gotRun models.PipelineRun
	if err := db.First(&gotRun, queuedRun.ID).Error; err != nil {
		t.Fatalf("reload run failed: %v", err)
	}
	if gotRun.AgentID != agentSelected.ID {
		t.Fatalf("run agent_id=%d, want=%d", gotRun.AgentID, agentSelected.ID)
	}

	var gotTask models.AgentTask
	if err := db.First(&gotTask, queuedTask.ID).Error; err != nil {
		t.Fatalf("reload task failed: %v", err)
	}
	if gotTask.Status != models.TaskStatusPending {
		t.Fatalf("task status=%s, want=%s", gotTask.Status, models.TaskStatusPending)
	}
	if gotTask.AgentID != agentSelected.ID {
		t.Fatalf("task agent_id=%d, want=%d", gotTask.AgentID, agentSelected.ID)
	}
}

func TestScheduleQueuedPipelineRuns_StopsAtCapacity(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}

	agent := models.Agent{
		Name:                   "cap-agent",
		Host:                   "host",
		Port:                   1,
		Token:                  "token",
		Status:                 models.AgentStatusOnline,
		RegistrationStatus:     models.AgentRegistrationStatusApproved,
		MaxConcurrentPipelines: 1,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	pipeline := models.Pipeline{
		Name:        "sched-cap",
		Description: "scheduler capacity test",
		OwnerID:     1,
		Environment: "test",
		Config: `{
			"version":"2.0",
			"nodes":[{"id":"n1","type":"sleep","name":"Sleep","config":{"seconds":1}}],
			"edges":[]
		}`,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	run1 := models.PipelineRun{
		PipelineID:  pipeline.ID,
		BuildNumber: 1,
		Status:      models.PipelineRunStatusQueued,
		Config:      pipeline.Config,
	}
	run2 := models.PipelineRun{
		PipelineID:  pipeline.ID,
		BuildNumber: 2,
		Status:      models.PipelineRunStatusQueued,
		Config:      pipeline.Config,
	}
	if err := db.Create(&run1).Error; err != nil {
		t.Fatalf("create run1 failed: %v", err)
	}
	if err := db.Create(&run2).Error; err != nil {
		t.Fatalf("create run2 failed: %v", err)
	}

	scheduled := h.scheduleQueuedPipelineRuns(db)
	if scheduled != 1 {
		t.Fatalf("scheduled=%d, want=1", scheduled)
	}

	time.Sleep(120 * time.Millisecond)

	var gotRun1 models.PipelineRun
	var gotRun2 models.PipelineRun
	if err := db.First(&gotRun1, run1.ID).Error; err != nil {
		t.Fatalf("reload run1 failed: %v", err)
	}
	if err := db.First(&gotRun2, run2.ID).Error; err != nil {
		t.Fatalf("reload run2 failed: %v", err)
	}

	if gotRun1.Status != models.PipelineRunStatusRunning {
		t.Fatalf("run1 status=%s, want=%s", gotRun1.Status, models.PipelineRunStatusRunning)
	}
	if gotRun2.Status != models.PipelineRunStatusQueued {
		t.Fatalf("run2 status=%s, want=%s", gotRun2.Status, models.PipelineRunStatusQueued)
	}

	var gotAgent models.Agent
	if err := db.First(&gotAgent, agent.ID).Error; err != nil {
		t.Fatalf("reload agent failed: %v", err)
	}
	if gotAgent.Status != models.AgentStatusBusy {
		t.Fatalf("agent status=%s, want=%s", gotAgent.Status, models.AgentStatusBusy)
	}
}

func TestScheduleQueuedPipelineRuns_FailedQueuedRunTriggersNextDispatch(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}

	agent := models.Agent{
		Name:                   "recover-agent",
		Host:                   "host",
		Port:                   1,
		Token:                  "token",
		Status:                 models.AgentStatusOnline,
		RegistrationStatus:     models.AgentRegistrationStatusApproved,
		MaxConcurrentPipelines: 1,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	pipeline := models.Pipeline{
		Name:        "sched-recover",
		Description: "scheduler recovery test",
		OwnerID:     1,
		Environment: "test",
		Config: `{
			"version":"2.0",
			"nodes":[{"id":"n1","type":"sleep","name":"Sleep","config":{"seconds":1}}],
			"edges":[]
		}`,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	invalidRun := models.PipelineRun{
		PipelineID:  pipeline.ID,
		BuildNumber: 1,
		Status:      models.PipelineRunStatusQueued,
		Config:      "{invalid-json",
	}
	validRun := models.PipelineRun{
		PipelineID:  pipeline.ID,
		BuildNumber: 2,
		Status:      models.PipelineRunStatusQueued,
		Config:      pipeline.Config,
	}
	if err := db.Create(&invalidRun).Error; err != nil {
		t.Fatalf("create invalid run failed: %v", err)
	}
	if err := db.Create(&validRun).Error; err != nil {
		t.Fatalf("create valid run failed: %v", err)
	}

	scheduled := h.scheduleQueuedPipelineRuns(db)
	if scheduled != 1 {
		t.Fatalf("initial scheduled=%d, want=1", scheduled)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var gotInvalid models.PipelineRun
		var gotValid models.PipelineRun
		_ = db.First(&gotInvalid, invalidRun.ID).Error
		_ = db.First(&gotValid, validRun.ID).Error

		if gotInvalid.Status == models.PipelineRunStatusFailed && gotValid.Status == models.PipelineRunStatusRunning {
			if gotValid.AgentID != agent.ID {
				t.Fatalf("valid run agent_id=%d, want=%d", gotValid.AgentID, agent.ID)
			}
			return
		}

		time.Sleep(50 * time.Millisecond)
	}

	var gotInvalid models.PipelineRun
	var gotValid models.PipelineRun
	_ = db.First(&gotInvalid, invalidRun.ID).Error
	_ = db.First(&gotValid, validRun.ID).Error
	t.Fatalf("timeout waiting dispatch recovery, invalid=%s valid=%s", gotInvalid.Status, gotValid.Status)
}
