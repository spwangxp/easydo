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
	if err := db.AutoMigrate(&models.AISession{}, &models.AISessionTurn{}); err != nil {
		t.Fatalf("migrate ai session failed: %v", err)
	}

	session := models.AISession{
		WorkspaceID: 1,
		TaskType:    "mr_quality_check",
		Status:      models.AISessionStatusQueued,
		RequestJSON: `{"input_text":"review this MR"}`,
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

	var completedTurn models.AISessionTurn
	if err := db.Where("session_id = ? AND turn_seq = ?", session.ID, 1).First(&completedTurn).Error; err != nil {
		t.Fatalf("load ai session turn failed: %v", err)
	}
	if completedTurn.TurnType != "mr_quality_check" {
		t.Fatalf("turn_type=%s, want mr_quality_check", completedTurn.TurnType)
	}
	if completedTurn.Role != "assistant" {
		t.Fatalf("role=%s, want assistant", completedTurn.Role)
	}
	if completedTurn.Status != string(models.AISessionStatusCompleted) {
		t.Fatalf("turn status=%s, want %s", completedTurn.Status, models.AISessionStatusCompleted)
	}
	if completedTurn.InputJSON != session.RequestJSON {
		t.Fatalf("input_json=%q, want %q", completedTurn.InputJSON, session.RequestJSON)
	}
	if completedTurn.OutputJSON == "" {
		t.Fatalf("expected turn output json to be stored")
	}
	if completedTurn.StartedAt == nil || *completedTurn.StartedAt != 123456 {
		t.Fatalf("started_at=%v, want 123456", completedTurn.StartedAt)
	}
	if completedTurn.CompletedAt == nil || *completedTurn.CompletedAt != 123456 {
		t.Fatalf("completed_at=%v, want 123456", completedTurn.CompletedAt)
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
	if err := db.Where("session_id = ? AND turn_seq = ?", session.ID, 1).First(&completedTurn).Error; err != nil {
		t.Fatalf("reload ai session turn failed: %v", err)
	}
	if completedTurn.Status != string(models.AISessionStatusCancelled) {
		t.Fatalf("turn status=%s, want %s", completedTurn.Status, models.AISessionStatusCancelled)
	}
	if completedTurn.ErrorMsg != "cancelled by user" {
		t.Fatalf("turn error_msg=%q, want cancelled by user", completedTurn.ErrorMsg)
	}
	if completedTurn.CompletedAt == nil || *completedTurn.CompletedAt != 123999 {
		t.Fatalf("completed_at=%v, want 123999", completedTurn.CompletedAt)
	}
}

func TestUpdateAISessionStateForTask_UsesAISessionIDFromParamsEvenWhenTaskTypeIsShell(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&models.AISession{}, &models.AISessionTurn{}); err != nil {
		t.Fatalf("migrate ai session failed: %v", err)
	}

	session := models.AISession{
		WorkspaceID: 1,
		TaskType:    "mr_quality_check",
		Status:      models.AISessionStatusQueued,
		RequestJSON: `{"input_text":"check nested request"}`,
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
	var turn models.AISessionTurn
	if err := db.Where("session_id = ? AND turn_seq = ?", session.ID, 1).First(&turn).Error; err != nil {
		t.Fatalf("load ai session turn failed: %v", err)
	}
	if turn.TurnType != "mr_quality_check" {
		t.Fatalf("turn_type=%s, want mr_quality_check", turn.TurnType)
	}
	if turn.InputJSON != session.RequestJSON {
		t.Fatalf("input_json=%q, want %q", turn.InputJSON, session.RequestJSON)
	}
}

func TestUpdateAISessionStateForTask_RunningDoesNotOverwriteStartedAtWhenAlreadySet(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&models.AISession{}, &models.AISessionTurn{}); err != nil {
		t.Fatalf("migrate ai session failed: %v", err)
	}

	session := models.AISession{
		WorkspaceID: 1,
		TaskType:    "mr_quality_check",
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
	var turn models.AISessionTurn
	if err := db.Where("session_id = ? AND turn_seq = ?", session.ID, 1).First(&turn).Error; err != nil {
		t.Fatalf("load ai session turn failed: %v", err)
	}
	if turn.StartedAt == nil || *turn.StartedAt != 100 {
		t.Fatalf("turn started_at=%v, want 100", turn.StartedAt)
	}
	if turn.Status != string(models.AISessionStatusRunning) {
		t.Fatalf("turn status=%s, want %s", turn.Status, models.AISessionStatusRunning)
	}
}

func TestUpdateAISessionStateForTask_UsesAttemptAsTurnSequence(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&models.AISession{}, &models.AISessionTurn{}); err != nil {
		t.Fatalf("migrate ai session failed: %v", err)
	}

	session := models.AISession{
		WorkspaceID: 1,
		TaskType:    "mr_quality_check",
		Status:      models.AISessionStatusQueued,
		RequestJSON: `{"input_text":"retry this review"}`,
		CreatedBy:   1,
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("create ai session failed: %v", err)
	}

	task := &models.AgentTask{
		BaseModel: models.BaseModel{ID: 99},
		TaskType:  "mr_quality_check",
		Params:    `{"ai_session_id":` + toJSONString(session.ID) + `}`,
	}

	updateAISessionStateForTask(db, task, taskUpdatePayloadV2{
		Attempt:  1,
		Status:   models.TaskStatusExecuteFailed,
		ErrorMsg: "attempt 1 failed",
	}, 110)
	updateAISessionStateForTask(db, task, taskUpdatePayloadV2{
		Attempt: 2,
		Status:  models.TaskStatusRunning,
	}, 210)
	updateAISessionStateForTask(db, task, taskUpdatePayloadV2{
		Attempt: 2,
		Status:  models.TaskStatusExecuteSuccess,
		Result:  map[string]interface{}{"summary": "ok"},
	}, 220)

	var firstTurn models.AISessionTurn
	if err := db.Where("session_id = ? AND turn_seq = ?", session.ID, 1).First(&firstTurn).Error; err != nil {
		t.Fatalf("load first ai session turn failed: %v", err)
	}
	if firstTurn.Status != string(models.AISessionStatusFailed) {
		t.Fatalf("first turn status=%s, want %s", firstTurn.Status, models.AISessionStatusFailed)
	}
	if firstTurn.ErrorMsg != "attempt 1 failed" {
		t.Fatalf("first turn error_msg=%q, want attempt 1 failed", firstTurn.ErrorMsg)
	}

	var secondTurn models.AISessionTurn
	if err := db.Where("session_id = ? AND turn_seq = ?", session.ID, 2).First(&secondTurn).Error; err != nil {
		t.Fatalf("load second ai session turn failed: %v", err)
	}
	if secondTurn.Status != string(models.AISessionStatusCompleted) {
		t.Fatalf("second turn status=%s, want %s", secondTurn.Status, models.AISessionStatusCompleted)
	}
	if secondTurn.StartedAt == nil || *secondTurn.StartedAt != 210 {
		t.Fatalf("second turn started_at=%v, want 210", secondTurn.StartedAt)
	}
	if secondTurn.CompletedAt == nil || *secondTurn.CompletedAt != 220 {
		t.Fatalf("second turn completed_at=%v, want 220", secondTurn.CompletedAt)
	}
	if secondTurn.OutputJSON == "" {
		t.Fatalf("expected second turn output json to be stored")
	}
}

func TestUpdateAISessionStateForTask_ScheduleFailedTurnDoesNotBackfillStartedAt(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&models.AISession{}, &models.AISessionTurn{}); err != nil {
		t.Fatalf("migrate ai session failed: %v", err)
	}

	session := models.AISession{
		WorkspaceID: 1,
		TaskType:    "mr_quality_check",
		Status:      models.AISessionStatusQueued,
		RequestJSON: `{"input_text":"schedule failed"}`,
		CreatedBy:   1,
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("create ai session failed: %v", err)
	}

	task := &models.AgentTask{
		BaseModel: models.BaseModel{ID: 109},
		TaskType:  "mr_quality_check",
		Params:    `{"ai_session_id":` + toJSONString(session.ID) + `}`,
	}
	updateAISessionStateForTask(db, task, taskUpdatePayloadV2{
		Attempt:  1,
		Status:   models.TaskStatusScheduleFailed,
		ErrorMsg: "dispatch failed",
	}, 310)

	var turn models.AISessionTurn
	if err := db.Where("session_id = ? AND turn_seq = ?", session.ID, 1).First(&turn).Error; err != nil {
		t.Fatalf("load ai session turn failed: %v", err)
	}
	if turn.Status != string(models.AISessionStatusFailed) {
		t.Fatalf("turn status=%s, want %s", turn.Status, models.AISessionStatusFailed)
	}
	if turn.StartedAt != nil {
		t.Fatalf("turn started_at=%v, want nil", turn.StartedAt)
	}
	if turn.CompletedAt == nil || *turn.CompletedAt != 310 {
		t.Fatalf("turn completed_at=%v, want 310", turn.CompletedAt)
	}
}

func toJSONString(v uint64) string {
	return strconv.FormatUint(v, 10)
}
