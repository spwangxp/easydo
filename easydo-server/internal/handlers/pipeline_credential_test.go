package handlers

import (
	"encoding/json"
	"strings"
	"testing"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
)

func TestInjectCredentialEnv_RecordsUsageEventAndEnvValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "pipeline-user", models.WorkspaceRoleDeveloper)
	encrypted, err := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{
		"token":      "ghp_pipeline_token",
		"token_type": "bearer",
		"username":   "oauth2",
	})
	if err != nil {
		t.Fatalf("encrypt payload failed: %v", err)
	}
	credential := models.Credential{
		Name:             "repo-auth",
		Type:             models.TypeToken,
		Category:         models.CategoryGitHub,
		Scope:            models.ScopeWorkspace,
		WorkspaceID:      workspace.ID,
		OwnerID:          user.ID,
		EncryptedPayload: encrypted,
		Status:           models.CredentialStatusActive,
	}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatalf("create credential failed: %v", err)
	}

	handler := &PipelineHandler{}
	run := &models.PipelineRun{BaseModel: models.BaseModel{ID: 88}, WorkspaceID: workspace.ID}
	nodeConfig := map[string]interface{}{
		"credentials": map[string]interface{}{
			"repo_auth": map[string]interface{}{"credential_id": credential.ID},
		},
	}
	def := pipelineTaskDefinitions["git_clone"]
	if err := handler.injectCredentialEnv(db, "git_clone", def, nodeConfig, run, user.ID, "user"); err != nil {
		t.Fatalf("inject credential env failed: %v", err)
	}

	envMap, ok := nodeConfig["env"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected env map to be injected")
	}
	if envMap["EASYDO_CRED_REPO_AUTH_TOKEN"] != "ghp_pipeline_token" {
		t.Fatalf("expected token env injection, got %#v", envMap["EASYDO_CRED_REPO_AUTH_TOKEN"])
	}
	if envMap["EASYDO_CRED_REPO_AUTH_TYPE"] != string(models.TypeToken) {
		t.Fatalf("expected type env injection, got %#v", envMap["EASYDO_CRED_REPO_AUTH_TYPE"])
	}

	var stored models.Credential
	if err := db.First(&stored, credential.ID).Error; err != nil {
		t.Fatalf("reload credential failed: %v", err)
	}
	if stored.UsedCount != 1 {
		t.Fatalf("expected used_count=1, got %d", stored.UsedCount)
	}
	if stored.LastUsedAt == 0 {
		t.Fatalf("expected last_used_at to be updated")
	}

	var events []models.CredentialEvent
	if err := db.Where("credential_id = ? AND action = ?", credential.ID, models.CredentialEventUsed).Find(&events).Error; err != nil {
		t.Fatalf("query usage events failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 usage event, got %d", len(events))
	}
	if events[0].ActorType != "pipeline_run" || events[0].ActorID != run.ID {
		t.Fatalf("unexpected event actor: %#v", events[0])
	}
	var detail map[string]interface{}
	if err := json.Unmarshal([]byte(events[0].DetailJSON), &detail); err != nil {
		t.Fatalf("unmarshal event detail failed: %v", err)
	}
	if detail["run_id"] != float64(run.ID) {
		t.Fatalf("expected run_id in detail, got %#v", detail)
	}
}

func TestInjectCredentialEnv_RejectsInactiveCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "inactive-pipeline-user", models.WorkspaceRoleDeveloper)
	encrypted, err := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{"token": "ghp_inactive"})
	if err != nil {
		t.Fatalf("encrypt payload failed: %v", err)
	}
	credential := models.Credential{Name: "inactive-repo-auth", Type: models.TypeToken, Category: models.CategoryGitHub, Scope: models.ScopeWorkspace, WorkspaceID: workspace.ID, OwnerID: user.ID, EncryptedPayload: encrypted, Status: models.CredentialStatusInactive}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatalf("create credential failed: %v", err)
	}
	handler := &PipelineHandler{}
	nodeConfig := map[string]interface{}{
		"credentials": map[string]interface{}{
			"repo_auth": map[string]interface{}{"credential_id": credential.ID},
		},
	}
	run := &models.PipelineRun{BaseModel: models.BaseModel{ID: 99}, WorkspaceID: workspace.ID}
	err = handler.injectCredentialEnv(db, "git_clone", pipelineTaskDefinitions["git_clone"], nodeConfig, run, user.ID, "user")
	if err == nil {
		t.Fatalf("expected inactive credential to be rejected")
	}
}

func TestInjectCredentialEnv_RejectsTypeSpecificMissingPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "missing-payload-user", models.WorkspaceRoleDeveloper)
	encrypted, err := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{"username": "oauth2"})
	if err != nil {
		t.Fatalf("encrypt payload failed: %v", err)
	}
	credential := models.Credential{Name: "broken-repo-auth", Type: models.TypeToken, Category: models.CategoryGitHub, Scope: models.ScopeWorkspace, WorkspaceID: workspace.ID, OwnerID: user.ID, EncryptedPayload: encrypted, Status: models.CredentialStatusActive}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatalf("create credential failed: %v", err)
	}
	handler := &PipelineHandler{}
	nodeConfig := map[string]interface{}{
		"credentials": map[string]interface{}{
			"repo_auth": map[string]interface{}{"credential_id": credential.ID},
		},
	}
	run := &models.PipelineRun{BaseModel: models.BaseModel{ID: 100}, WorkspaceID: workspace.ID}
	err = handler.injectCredentialEnv(db, "git_clone", pipelineTaskDefinitions["git_clone"], nodeConfig, run, user.ID, "user")
	if err == nil {
		t.Fatalf("expected missing token payload to be rejected")
	}
	if !strings.Contains(err.Error(), "missing required payload") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInjectCredentialEnv_GitCloneAccessTokenOnlyPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "access-token-user", models.WorkspaceRoleDeveloper)
	encrypted, err := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{
		"access_token": "gho_access_only",
		"username":     "oauth2",
	})
	if err != nil {
		t.Fatalf("encrypt payload failed: %v", err)
	}
	credential := models.Credential{
		Name:             "access-token-repo-auth",
		Type:             models.TypeToken,
		Category:         models.CategoryGitHub,
		Scope:            models.ScopeWorkspace,
		WorkspaceID:      workspace.ID,
		OwnerID:          user.ID,
		EncryptedPayload: encrypted,
		Status:           models.CredentialStatusActive,
	}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatalf("create credential failed: %v", err)
	}

	handler := &PipelineHandler{}
	run := &models.PipelineRun{BaseModel: models.BaseModel{ID: 101}, WorkspaceID: workspace.ID}
	nodeConfig := map[string]interface{}{
		"credentials": map[string]interface{}{
			"repo_auth": map[string]interface{}{"credential_id": credential.ID},
		},
	}
	if err := handler.injectCredentialEnv(db, "git_clone", pipelineTaskDefinitions["git_clone"], nodeConfig, run, user.ID, "user"); err != nil {
		t.Fatalf("inject credential env failed: %v", err)
	}

	envMap, ok := nodeConfig["env"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected env map to be injected")
	}
	if envMap["EASYDO_CRED_REPO_AUTH_ACCESS_TOKEN"] != "gho_access_only" {
		t.Fatalf("expected access_token env injection, got %#v", envMap["EASYDO_CRED_REPO_AUTH_ACCESS_TOKEN"])
	}
	if envMap["EASYDO_CRED_REPO_AUTH_USERNAME"] != "oauth2" {
		t.Fatalf("expected username env injection, got %#v", envMap["EASYDO_CRED_REPO_AUTH_USERNAME"])
	}
}

func TestInjectCredentialEnv_GitClonePasswordCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "password-user", models.WorkspaceRoleDeveloper)
	encrypted, err := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{
		"username": "git-user",
		"password": "super-secret",
	})
	if err != nil {
		t.Fatalf("encrypt payload failed: %v", err)
	}
	credential := models.Credential{
		Name:             "password-repo-auth",
		Type:             models.TypePassword,
		Category:         models.CategoryGitHub,
		Scope:            models.ScopeWorkspace,
		WorkspaceID:      workspace.ID,
		OwnerID:          user.ID,
		EncryptedPayload: encrypted,
		Status:           models.CredentialStatusActive,
	}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatalf("create credential failed: %v", err)
	}

	handler := &PipelineHandler{}
	run := &models.PipelineRun{BaseModel: models.BaseModel{ID: 102}, WorkspaceID: workspace.ID}
	nodeConfig := map[string]interface{}{
		"credentials": map[string]interface{}{
			"repo_auth": map[string]interface{}{"credential_id": credential.ID},
		},
	}
	if err := handler.injectCredentialEnv(db, "git_clone", pipelineTaskDefinitions["git_clone"], nodeConfig, run, user.ID, "user"); err != nil {
		t.Fatalf("inject credential env failed: %v", err)
	}

	envMap, ok := nodeConfig["env"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected env map to be injected")
	}
	if envMap["EASYDO_CRED_REPO_AUTH_USERNAME"] != "git-user" {
		t.Fatalf("expected username env injection, got %#v", envMap["EASYDO_CRED_REPO_AUTH_USERNAME"])
	}
	if envMap["EASYDO_CRED_REPO_AUTH_PASSWORD"] != "super-secret" {
		t.Fatalf("expected password env injection, got %#v", envMap["EASYDO_CRED_REPO_AUTH_PASSWORD"])
	}
	if envMap["EASYDO_CRED_REPO_AUTH_TYPE"] != string(models.TypePassword) {
		t.Fatalf("expected type env injection, got %#v", envMap["EASYDO_CRED_REPO_AUTH_TYPE"])
	}
}
