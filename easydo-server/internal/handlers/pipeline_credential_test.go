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

func TestInjectCredentialEnv_AllowsLockedCredentialForRuntime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "locked-runtime-user", models.WorkspaceRoleDeveloper)
	encrypted, err := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{"token": "ghp_locked_runtime", "token_type": "bearer"})
	if err != nil {
		t.Fatalf("encrypt payload failed: %v", err)
	}
	credential := models.Credential{Name: "locked-runtime-auth", Type: models.TypeToken, Category: models.CategoryGitHub, Scope: models.ScopeWorkspace, WorkspaceID: workspace.ID, OwnerID: user.ID, EncryptedPayload: encrypted, Status: models.CredentialStatusActive, LockState: models.CredentialLockStateLocked}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatalf("create credential failed: %v", err)
	}

	handler := &PipelineHandler{}
	run := &models.PipelineRun{BaseModel: models.BaseModel{ID: 105}, WorkspaceID: workspace.ID}
	nodeConfig := map[string]interface{}{
		"credentials": map[string]interface{}{
			"repo_auth": map[string]interface{}{"credential_id": credential.ID},
		},
	}
	if err := handler.injectCredentialEnv(db, "git_clone", pipelineTaskDefinitions["git_clone"], nodeConfig, run, user.ID, "user"); err != nil {
		t.Fatalf("expected locked credential runtime injection to remain allowed, got %v", err)
	}

	envMap, ok := nodeConfig["env"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected env map to be injected")
	}
	if envMap["EASYDO_CRED_REPO_AUTH_TOKEN"] != "ghp_locked_runtime" {
		t.Fatalf("expected token env injection, got %#v", envMap["EASYDO_CRED_REPO_AUTH_TOKEN"])
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

func TestInjectCredentialEnv_SSHPasswordCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "ssh-password-user", models.WorkspaceRoleDeveloper)
	encrypted, err := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{
		"username": "root",
		"password": "secret123",
	})
	if err != nil {
		t.Fatalf("encrypt payload failed: %v", err)
	}
	credential := models.Credential{
		Name:             "vm-password-auth",
		Type:             models.TypePassword,
		Category:         models.CategoryCustom,
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
	run := &models.PipelineRun{BaseModel: models.BaseModel{ID: 103}, WorkspaceID: workspace.ID}
	nodeConfig := map[string]interface{}{
		"credentials": map[string]interface{}{
			"ssh_auth": map[string]interface{}{"credential_id": credential.ID},
		},
	}
	if err := handler.injectCredentialEnv(db, "ssh", pipelineTaskDefinitions["ssh"], nodeConfig, run, user.ID, "user"); err != nil {
		t.Fatalf("inject credential env failed: %v", err)
	}

	envMap, ok := nodeConfig["env"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected env map to be injected")
	}
	if envMap["EASYDO_CRED_SSH_AUTH_USERNAME"] != "root" {
		t.Fatalf("expected username env injection, got %#v", envMap["EASYDO_CRED_SSH_AUTH_USERNAME"])
	}
	if envMap["EASYDO_CRED_SSH_AUTH_PASSWORD"] != "secret123" {
		t.Fatalf("expected password env injection, got %#v", envMap["EASYDO_CRED_SSH_AUTH_PASSWORD"])
	}
	if envMap["EASYDO_CRED_SSH_AUTH_TYPE"] != string(models.TypePassword) {
		t.Fatalf("expected type env injection, got %#v", envMap["EASYDO_CRED_SSH_AUTH_TYPE"])
	}
}

func TestInjectCredentialEnv_AllowsResourceBoundDeploymentCredentialForRequester(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedCredentialTestUserAndWorkspace(t, db, "bound-credential-owner", models.WorkspaceRoleMaintainer)
	developer := seedCredentialMember(t, db, workspace.ID, "bound-credential-requester", models.WorkspaceRoleDeveloper)

	encrypted, err := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{
		"username": "root",
		"password": "secret123",
	})
	if err != nil {
		t.Fatalf("encrypt payload failed: %v", err)
	}
	credential := models.Credential{
		Name:             "deployment-bound-ssh-auth",
		Type:             models.TypePassword,
		Category:         models.CategoryCustom,
		Scope:            models.ScopeUser,
		WorkspaceID:      workspace.ID,
		OwnerID:          maintainer.ID,
		EncryptedPayload: encrypted,
		Status:           models.CredentialStatusActive,
	}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatalf("create credential failed: %v", err)
	}

	resource := models.Resource{
		WorkspaceID: workspace.ID,
		Name:        "bound-resource-vm",
		Type:        models.ResourceTypeVM,
		Environment: "development",
		Status:      models.ResourceStatusOnline,
		Endpoint:    "10.0.0.8:22",
		CreatedBy:   maintainer.ID,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}
	if err := db.Create(&models.ResourceCredentialBinding{
		WorkspaceID:  workspace.ID,
		ResourceID:   resource.ID,
		CredentialID: credential.ID,
		Purpose:      "ssh_auth",
		BoundBy:      maintainer.ID,
	}).Error; err != nil {
		t.Fatalf("create resource binding failed: %v", err)
	}

	run := &models.PipelineRun{
		BaseModel:       models.BaseModel{ID: 104},
		WorkspaceID:     workspace.ID,
		TriggerType:     "deployment_request",
		TriggerUserID:   developer.ID,
		TriggerUserRole: models.WorkspaceRoleDeveloper,
	}
	if err := db.Create(run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}
	if err := db.Create(&models.DeploymentRequest{
		WorkspaceID:        workspace.ID,
		TemplateID:         1,
		TemplateVersionID:  1,
		TemplateType:       models.StoreTemplateTypeLLM,
		TargetResourceID:   resource.ID,
		TargetResourceType: models.ResourceTypeVM,
		Status:             models.DeploymentRequestStatusQueued,
		PipelineRunID:      run.ID,
		RequestedBy:        developer.ID,
	}).Error; err != nil {
		t.Fatalf("create deployment request failed: %v", err)
	}

	handler := &PipelineHandler{}
	nodeConfig := map[string]interface{}{
		"credentials": map[string]interface{}{
			"ssh_auth": map[string]interface{}{"credential_id": credential.ID},
		},
	}
	err = handler.injectCredentialEnv(db, "ssh", pipelineTaskDefinitions["ssh"], nodeConfig, run, developer.ID, models.WorkspaceRoleDeveloper)
	if err != nil {
		t.Fatalf("expected resource-bound deployment credential injection to succeed, got %v", err)
	}

	envMap, ok := nodeConfig["env"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected env map to be injected")
	}
	if envMap["EASYDO_CRED_SSH_AUTH_USERNAME"] != "root" {
		t.Fatalf("expected username env injection, got %#v", envMap["EASYDO_CRED_SSH_AUTH_USERNAME"])
	}
	if envMap["EASYDO_CRED_SSH_AUTH_PASSWORD"] != "secret123" {
		t.Fatalf("expected password env injection, got %#v", envMap["EASYDO_CRED_SSH_AUTH_PASSWORD"])
	}
}
