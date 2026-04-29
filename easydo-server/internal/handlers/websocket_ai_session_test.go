package handlers

import (
	"strconv"
	"testing"

	"easydo-server/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestExtractAISessionIDFromTaskUsesParamsOnly(t *testing.T) {
	task := &models.AgentTask{
		ResultData: `{"ai_session_id":99,"status":"queued"}`,
		Params:     `{"ai_session_id":12}`,
	}
	if got := extractAISessionIDFromTask(task); got != 12 {
		t.Fatalf("extractAISessionIDFromTask()=%d, want 12", got)
	}
}

func TestExtractAISessionIDFromTaskDoesNotFallbackToResultData(t *testing.T) {
	task := &models.AgentTask{
		ResultData: `{"ai_session_id":99,"status":"queued"}`,
	}
	if got := extractAISessionIDFromTask(task); got != 0 {
		t.Fatalf("extractAISessionIDFromTask()=%d, want 0", got)
	}
}

func TestUpdateAISessionStateForTask_UpdatesTerminalStateAndResponse(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&models.AISession{}); err != nil {
		t.Fatalf("migrate ai session failed: %v", err)
	}

	session := models.AISession{
		WorkspaceID: 1,
		Scenario:    "mr_quality_check",
		Status:      models.AISessionStatusQueued,
		CreatedBy:   1,
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("create ai session failed: %v", err)
	}

	task := &models.AgentTask{
		BaseModel: models.BaseModel{ID: 77},
		TaskType:  "mr_quality_check",
		Params:    `{"ai_session_id":` + toJSONString(session.ID) + `}`,
	}
	updateAISessionStateForTask(db, task, taskUpdatePayloadV2{
		Status: models.TaskStatusExecuteSuccess,
		Result: map[string]interface{}{"summary": "ok", "issues_count": 0},
	}, 123456)

	var stored models.AISession
	if err := db.First(&stored, session.ID).Error; err != nil {
		t.Fatalf("reload ai session failed: %v", err)
	}
	if stored.Status != models.AISessionStatusCompleted {
		t.Fatalf("status=%s, want %s", stored.Status, models.AISessionStatusCompleted)
	}
	if stored.CompletedAt != 123456 {
		t.Fatalf("completed_at=%d, want 123456", stored.CompletedAt)
	}
	if stored.ResponseJSON == "" {
		t.Fatalf("expected response json to be stored")
	}

	updateAISessionStateForTask(db, task, taskUpdatePayloadV2{
		Status:   models.TaskStatusCancelled,
		ErrorMsg: "cancelled by user",
	}, 123999)
	if err := db.First(&stored, session.ID).Error; err != nil {
		t.Fatalf("reload ai session after cancel failed: %v", err)
	}
	if stored.Status != models.AISessionStatusCancelled {
		t.Fatalf("status=%s, want %s", stored.Status, models.AISessionStatusCancelled)
	}
	if stored.ErrorMsg != "cancelled by user" {
		t.Fatalf("error_msg=%q, want cancelled by user", stored.ErrorMsg)
	}
}

func TestUpdateAISessionStateForTask_UsesAISessionIDFromParamsEvenWhenTaskTypeIsShell(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&models.AISession{}); err != nil {
		t.Fatalf("migrate ai session failed: %v", err)
	}

	session := models.AISession{
		WorkspaceID: 1,
		Scenario:    "mr_quality_check",
		Status:      models.AISessionStatusQueued,
		CreatedBy:   1,
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("create ai session failed: %v", err)
	}

	task := &models.AgentTask{
		BaseModel: models.BaseModel{ID: 79},
		TaskType:  "shell",
		Params:    `{"mode":"ai-task","ai_session_id":` + toJSONString(session.ID) + `}`,
	}
	updateAISessionStateForTask(db, task, taskUpdatePayloadV2{
		Status: models.TaskStatusExecuteSuccess,
		Result: map[string]interface{}{"summary": "ok"},
	}, 123460)

	var stored models.AISession
	if err := db.First(&stored, session.ID).Error; err != nil {
		t.Fatalf("reload ai session failed: %v", err)
	}
	if stored.Status != models.AISessionStatusCompleted {
		t.Fatalf("status=%s, want %s", stored.Status, models.AISessionStatusCompleted)
	}
	if stored.CompletedAt != 123460 {
		t.Fatalf("completed_at=%d, want 123460", stored.CompletedAt)
	}
	if stored.ResponseJSON == "" {
		t.Fatalf("expected response json to be stored")
	}
}

func TestUpdateAISessionStateForTask_RunningDoesNotOverwriteStartedAtWhenAlreadySet(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&models.AISession{}); err != nil {
		t.Fatalf("migrate ai session failed: %v", err)
	}

	session := models.AISession{
		WorkspaceID: 1,
		Scenario:    "mr_quality_check",
		Status:      models.AISessionStatusRunning,
		StartedAt:   100,
		CreatedBy:   1,
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("create ai session failed: %v", err)
	}

	task := &models.AgentTask{
		BaseModel: models.BaseModel{ID: 88},
		TaskType:  "mr_quality_check",
		Params:    `{"ai_session_id":` + toJSONString(session.ID) + `}`,
	}
	updateAISessionStateForTask(db, task, taskUpdatePayloadV2{Status: models.TaskStatusRunning}, 200)

	var stored models.AISession
	if err := db.First(&stored, session.ID).Error; err != nil {
		t.Fatalf("reload ai session failed: %v", err)
	}
	if stored.StartedAt != 100 {
		t.Fatalf("started_at=%d, want 100", stored.StartedAt)
	}
	if stored.Status != models.AISessionStatusRunning {
		t.Fatalf("status=%s, want %s", stored.Status, models.AISessionStatusRunning)
	}
}

func toJSONString(v uint64) string {
	return strconv.FormatUint(v, 10)
}
