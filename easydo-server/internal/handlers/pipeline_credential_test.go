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
		TemplateType:       models.StoreTemplateTypeAI,
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

// Integration test: proves the exact DB shape that caused Docker 401 errors.
// The database stored credentials with flat keys ("credentials.registry_auth.credential_id")
// instead of nested structure. This test verifies injectCredentialEnv correctly
// expands and processes flat-key bindings.
func TestInjectCredentialEnv_FlatKeyBinding_Integration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "flat-key-integration-user", models.WorkspaceRoleDeveloper)
	encrypted, err := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{
		"token":      "ghp_flat_key_token",
		"token_type": "bearer",
		"username":   "oauth2",
	})
	if err != nil {
		t.Fatalf("encrypt payload failed: %v", err)
	}
	credential := models.Credential{
		Name:             "flat-key-registry-auth",
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
	run := &models.PipelineRun{BaseModel: models.BaseModel{ID: 200}, WorkspaceID: workspace.ID}
	// Flat-key binding format — this is the EXACT shape stored in the database
	// that caused injectCredentialEnv to return early (nil binding).
	// git_clone uses slot "repo_auth", not "registry_auth".
	nodeConfig := map[string]interface{}{
		"credentials.repo_auth.credential_id": credential.ID,
	}

	def := pipelineTaskDefinitions["git_clone"]
	if err := handler.injectCredentialEnv(db, "git_clone", def, nodeConfig, run, user.ID, "user"); err != nil {
		t.Fatalf("inject credential env failed (flat key path broken): %v", err)
	}

	envMap, ok := nodeConfig["env"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected env map to be injected")
	}
	if envMap["EASYDO_CRED_REPO_AUTH_TOKEN"] != "ghp_flat_key_token" {
		t.Fatalf("expected token env injection from flat key, got %#v", envMap["EASYDO_CRED_REPO_AUTH_TOKEN"])
	}
	if envMap["EASYDO_CRED_REPO_AUTH_TYPE"] != string(models.TypeToken) {
		t.Fatalf("expected type env injection, got %#v", envMap["EASYDO_CRED_REPO_AUTH_TYPE"])
	}
	// Verify the nodeConfig["credentials"] was expanded to nested by expandFlatCredentialBindings
	creds, ok := nodeConfig["credentials"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected nodeConfig[\"credentials\"] to be expanded to nested map, got %#v", nodeConfig["credentials"])
	}
	repoAuth, ok := creds["repo_auth"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected repo_auth slot in expanded credentials")
	}
	if repoAuth["credential_id"] != credential.ID {
		t.Fatalf("expected credential_id=%d in expanded binding, got %#v", credential.ID, repoAuth["credential_id"])
	}
}

// ---------------------------------------------------------------------------
// expandFlatCredentialBindings unit tests
// ---------------------------------------------------------------------------

func TestExpandFlatCredentialBindings_FlatKeys(t *testing.T) {
	// Simulate the flat-key format stored in database:
	// nodeConfig["credentials.registry_auth.credential_id"] = 2
	nodeConfig := map[string]interface{}{
		"credentials.registry_auth.credential_id": uint64(2),
	}

	result := expandFlatCredentialBindings(nodeConfig)

	if result == nil {
		t.Fatalf("expected non-nil result for flat keys")
	}
	registryAuth, ok := result["registry_auth"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected registry_auth slot, got %#v", result["registry_auth"])
	}
	if registryAuth["credential_id"] != uint64(2) {
		t.Fatalf("expected credential_id=2, got %#v", registryAuth["credential_id"])
	}
	// Verify nodeConfig was mutated
	if nodeConfig["credentials"] == nil {
		t.Fatalf("expected nodeConfig[\"credentials\"] to be set")
	}
}

// Regression test: when credentials already exists as nested AND flat keys also
// exist for other slots, expandFlatCredentialBindings must NOT overwrite the
// nested credentials. The nested format takes precedence (it's more explicit).
// This test makes that precedence behavior explicit and intentional.
func TestInjectCredentialEnv_MixedNestedAndFlatKeyBindings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "mixed-creds-user", models.WorkspaceRoleDeveloper)

	sshEnc, _ := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{
		"username": "ssh-user",
		"password": "ssh-pass",
	})
	sshCred := models.Credential{
		Name:             "mixed-ssh-auth",
		Type:             models.TypePassword,
		Category:         models.CategoryCustom,
		Scope:            models.ScopeWorkspace,
		WorkspaceID:      workspace.ID,
		OwnerID:          user.ID,
		EncryptedPayload: sshEnc,
		Status:           models.CredentialStatusActive,
	}
	if err := db.Create(&sshCred).Error; err != nil {
		t.Fatalf("create ssh credential failed: %v", err)
	}

	dockerEnc, _ := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{
		"username": "docker-user",
		"password": "docker-pass",
	})
	dockerCred := models.Credential{
		Name:             "mixed-docker-registry",
		Type:             models.TypePassword,
		Category:         models.CategoryDocker,
		Scope:            models.ScopeWorkspace,
		WorkspaceID:      workspace.ID,
		OwnerID:          user.ID,
		EncryptedPayload: dockerEnc,
		Status:           models.CredentialStatusActive,
	}
	if err := db.Create(&dockerCred).Error; err != nil {
		t.Fatalf("create docker credential failed: %v", err)
	}

	handler := &PipelineHandler{}
	run := &models.PipelineRun{BaseModel: models.BaseModel{ID: 201}, WorkspaceID: workspace.ID}
	// Mixed shape: ssh_auth uses nested format, registry_auth uses flat key format.
	// The nested ssh_auth should be preserved (nested wins over flat).
	nodeConfig := map[string]interface{}{
		"credentials": map[string]interface{}{
			"ssh_auth": map[string]interface{}{"credential_id": sshCred.ID},
		},
		// Flat key for registry_auth — should be IGNORED because nested credentials exists.
		// This is the intentional precedence behavior (nested is more explicit).
		"credentials.registry_auth.credential_id": dockerCred.ID,
	}

	def := pipelineTaskDefinitions["docker-run"]
	if err := handler.injectCredentialEnv(db, "docker-run", def, nodeConfig, run, user.ID, "user"); err != nil {
		t.Fatalf("inject credential env failed: %v", err)
	}

	envMap, ok := nodeConfig["env"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected env map to be injected")
	}
	// ssh_auth from nested binding should be injected
	if envMap["EASYDO_CRED_SSH_AUTH_USERNAME"] != "ssh-user" {
		t.Fatalf("expected ssh_auth from nested binding, got %#v", envMap["EASYDO_CRED_SSH_AUTH_USERNAME"])
	}
	// registry_auth from flat key should NOT be injected (flat ignored when nested exists)
	if _, exists := envMap["EASYDO_CRED_REGISTRY_AUTH_USERNAME"]; exists {
		t.Fatalf("expected registry_auth from flat key to be IGNORED (nested credentials exist), but got %#v", envMap["EASYDO_CRED_REGISTRY_AUTH_USERNAME"])
	}
}

func TestExpandFlatCredentialBindings_AlreadyNested(t *testing.T) {
	nodeConfig := map[string]interface{}{
		"credentials": map[string]interface{}{
			"repo_auth": map[string]interface{}{"credential_id": uint64(5)},
		},
	}

	result := expandFlatCredentialBindings(nodeConfig)

	if result == nil {
		t.Fatalf("expected non-nil result for nested credentials")
	}
	repoAuth, ok := result["repo_auth"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected repo_auth slot, got %#v", result["repo_auth"])
	}
	if repoAuth["credential_id"] != uint64(5) {
		t.Fatalf("expected credential_id=5, got %#v", repoAuth["credential_id"])
	}
}

func TestExpandFlatCredentialBindings_NilInput(t *testing.T) {
	result := expandFlatCredentialBindings(nil)
	if result != nil {
		t.Fatalf("expected nil for nil input, got %#v", result)
	}
}

func TestExpandFlatCredentialBindings_NoCredentials(t *testing.T) {
	// nodeConfig has no credentials keys at all
	nodeConfig := map[string]interface{}{
		"some_field": "some_value",
	}

	result := expandFlatCredentialBindings(nodeConfig)

	// Returns empty map (not nil) when no flat keys found
	// The function builds credentials map, finds nothing, returns empty map
	if result == nil {
		t.Fatalf("expected non-nil empty map when no credentials present")
	}
	if len(result) != 0 {
		t.Fatalf("expected empty credentials map, got %#v", result)
	}
}

func TestExpandFlatCredentialBindings_MissingFieldName(t *testing.T) {
	// Key ends with dot but no field after it: "credentials.registry_auth."
	// The function should skip such malformed keys
	nodeConfig := map[string]interface{}{
		"credentials.registry_auth.": uint64(2),
	}

	result := expandFlatCredentialBindings(nodeConfig)

	// Should return empty since no valid flat keys were processed
	if len(result) != 0 {
		t.Fatalf("expected empty result for malformed key, got %#v", result)
	}
}

func TestExpandFlatCredentialBindings_MultipleFlatKeys(t *testing.T) {
	// Multiple flat keys for different slots
	nodeConfig := map[string]interface{}{
		"credentials.registry_auth.credential_id": uint64(2),
		"credentials.docker_auth.credential_id":   uint64(3),
	}

	result := expandFlatCredentialBindings(nodeConfig)

	if len(result) != 2 {
		t.Fatalf("expected 2 slots, got %d", len(result))
	}
	registryAuth, ok := result["registry_auth"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected registry_auth slot")
	}
	if registryAuth["credential_id"] != uint64(2) {
		t.Fatalf("expected registry_auth.credential_id=2")
	}
	dockerAuth, ok := result["docker_auth"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected docker_auth slot")
	}
	if dockerAuth["credential_id"] != uint64(3) {
		t.Fatalf("expected docker_auth.credential_id=3")
	}
}

func TestExpandFlatCredentialBindings_PrefersNestedOverFlat(t *testing.T) {
	// When BOTH nested "credentials" AND flat keys exist,
	// nested should be returned as-is (flat keys ignored)
	nodeConfig := map[string]interface{}{
		"credentials": map[string]interface{}{
			"repo_auth": map[string]interface{}{"credential_id": uint64(99)},
		},
		"credentials.registry_auth.credential_id": uint64(2), // flat key — should be ignored
	}

	result := expandFlatCredentialBindings(nodeConfig)

	repoAuth := result["repo_auth"].(map[string]interface{})
	if repoAuth["credential_id"] != uint64(99) {
		t.Fatalf("expected nested credentials to take precedence, got %#v", repoAuth["credential_id"])
	}
	// Flat key should NOT have been processed
	if _, exists := result["registry_auth"]; exists {
		t.Fatalf("flat key should not be processed when nested exists")
	}
}
