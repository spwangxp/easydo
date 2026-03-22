package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestCredentialHandler_CreateRevealVerifyAndUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "creator", models.WorkspaceRoleDeveloper)
	h := NewCredentialHandler()

	createBody := mustJSON(t, map[string]interface{}{
		"name":        "github-token",
		"description": "GitHub access token",
		"type":        string(models.TypeToken),
		"category":    string(models.CategoryGitHub),
		"scope":       string(models.ScopeWorkspace),
		"payload":     map[string]interface{}{"token": "ghp_test_token", "token_type": "bearer"},
	})
	createRecorder := performCredentialRequest(t, h.CreateCredential, user.ID, "user", workspace.ID, http.MethodPost, "/api/v1/credentials", createBody)
	if createRecorder.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", createRecorder.Code, createRecorder.Body.String())
	}
	createdID := responseDataID(t, createRecorder.Body.Bytes())

	var stored models.Credential
	if err := db.First(&stored, createdID).Error; err != nil {
		t.Fatalf("load credential failed: %v", err)
	}
	if stored.EncryptedPayload == "" {
		t.Fatalf("expected encrypted payload to be stored")
	}
	if stored.LockState != models.CredentialLockStateLocked {
		t.Fatalf("expected new credential to default to locked, got %s", stored.LockState)
	}

	revealRecorder := performCredentialRequest(t, h.GetCredentialPayload, user.ID, "user", workspace.ID, http.MethodGet, "/api/v1/credentials/1/payload", nil, pathID(createdID))
	if revealRecorder.Code != http.StatusOK {
		t.Fatalf("reveal status=%d body=%s", revealRecorder.Code, revealRecorder.Body.String())
	}
	if !bytes.Contains(revealRecorder.Body.Bytes(), []byte("ghp_test_token")) {
		t.Fatalf("expected revealed payload in response, got %s", revealRecorder.Body.String())
	}

	verifyRecorder := performCredentialRequest(t, h.VerifyCredential, user.ID, "user", workspace.ID, http.MethodPost, "/api/v1/credentials/1/verify", nil, pathID(createdID))
	if verifyRecorder.Code != http.StatusOK {
		t.Fatalf("verify status=%d body=%s", verifyRecorder.Code, verifyRecorder.Body.String())
	}
	if !bytes.Contains(verifyRecorder.Body.Bytes(), []byte(`"valid":true`)) {
		t.Fatalf("expected valid verify response, got %s", verifyRecorder.Body.String())
	}

	now := time.Now().Unix()
	if err := db.Model(&stored).Updates(map[string]interface{}{"used_count": 2, "last_used_at": now}).Error; err != nil {
		t.Fatalf("update usage counters failed: %v", err)
	}
	if err := db.Create(&models.CredentialEvent{CredentialID: stored.ID, Action: models.CredentialEventUsed, ActorType: "pipeline_run", ActorID: 11, Result: "success"}).Error; err != nil {
		t.Fatalf("create success event failed: %v", err)
	}
	if err := db.Create(&models.CredentialEvent{CredentialID: stored.ID, Action: models.CredentialEventUsed, ActorType: "pipeline_run", ActorID: 12, Result: "failed"}).Error; err != nil {
		t.Fatalf("create failed event failed: %v", err)
	}

	usageRecorder := performCredentialRequest(t, h.GetCredentialUsage, user.ID, "user", workspace.ID, http.MethodGet, "/api/v1/credentials/1/usage", nil, pathID(createdID))
	if usageRecorder.Code != http.StatusOK {
		t.Fatalf("usage status=%d body=%s", usageRecorder.Code, usageRecorder.Body.String())
	}
	if !bytes.Contains(usageRecorder.Body.Bytes(), []byte(`"used_count":2`)) || !bytes.Contains(usageRecorder.Body.Bytes(), []byte(`"failed_count":1`)) {
		t.Fatalf("unexpected usage response: %s", usageRecorder.Body.String())
	}

	var eventCount int64
	if err := db.Model(&models.CredentialEvent{}).Where("credential_id = ?", createdID).Count(&eventCount).Error; err != nil {
		t.Fatalf("count events failed: %v", err)
	}
	if eventCount < 4 {
		t.Fatalf("expected at least create/reveal/verify/use events, got %d", eventCount)
	}
}

func TestCredentialHandler_PersonalScopeStillExposesMetadataToViewer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	owner, workspace := seedCredentialTestUserAndWorkspace(t, db, "owner", models.WorkspaceRoleDeveloper)
	viewer := seedCredentialMember(t, db, workspace.ID, "viewer", models.WorkspaceRoleViewer)
	encrypted, err := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{"username": "root", "password": "secret"})
	if err != nil {
		t.Fatalf("encrypt payload failed: %v", err)
	}
	credential := models.Credential{Name: "personal-db", Type: models.TypePassword, Category: models.CategoryCustom, Scope: models.ScopeUser, WorkspaceID: workspace.ID, OwnerID: owner.ID, EncryptedPayload: encrypted, Status: models.CredentialStatusActive}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatalf("create credential failed: %v", err)
	}

	h := NewCredentialHandler()
	getRecorder := performCredentialRequest(t, h.GetCredential, viewer.ID, "user", workspace.ID, http.MethodGet, "/api/v1/credentials/1", nil, pathID(credential.ID), withWorkspaceRole(models.WorkspaceRoleViewer))
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("expected 200 for personal credential metadata read, got %d body=%s", getRecorder.Code, getRecorder.Body.String())
	}
	if !bytes.Contains(getRecorder.Body.Bytes(), []byte(`"username":"root"`)) {
		t.Fatalf("expected safe username summary in detail response, got %s", getRecorder.Body.String())
	}
	if bytes.Contains(getRecorder.Body.Bytes(), []byte(`"password":"secret"`)) {
		t.Fatalf("expected metadata-only detail response without password leakage, got %s", getRecorder.Body.String())
	}
	if !bytes.Contains(getRecorder.Body.Bytes(), []byte(`"can_view_secret":false`)) {
		t.Fatalf("expected personal credential metadata response to include secret access flag, got %s", getRecorder.Body.String())
	}
}

func TestCredentialHandler_ListCredentialsIncludesSafeSummaryFieldsAndPermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "summary-user", models.WorkspaceRoleDeveloper)
	encryptionService := NewCredentialHandler().encryptionService

	passwordPayload, err := encryptionService.EncryptCredentialData(map[string]interface{}{"username": "ubuntu", "password": "vm-secret"})
	if err != nil {
		t.Fatalf("encrypt password payload failed: %v", err)
	}
	sshPayload, err := encryptionService.EncryptCredentialData(map[string]interface{}{"private_key": "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----", "key_type": "rsa"})
	if err != nil {
		t.Fatalf("encrypt ssh payload failed: %v", err)
	}
	kubePayload, err := encryptionService.EncryptCredentialData(map[string]interface{}{"auth_mode": "server_token", "server": "https://kubernetes.example.com", "namespace": "prod", "token": "cluster-secret"})
	if err != nil {
		t.Fatalf("encrypt kubernetes payload failed: %v", err)
	}

	credentials := []models.Credential{
		{Name: "vm-password", Type: models.TypePassword, Category: models.CategoryCustom, Scope: models.ScopeWorkspace, WorkspaceID: workspace.ID, OwnerID: user.ID, EncryptedPayload: passwordPayload, Status: models.CredentialStatusActive},
		{Name: "vm-ssh", Type: models.TypeSSHKey, Category: models.CategoryCustom, Scope: models.ScopeWorkspace, WorkspaceID: workspace.ID, OwnerID: user.ID, EncryptedPayload: sshPayload, Status: models.CredentialStatusActive},
		{Name: "prod-cluster", Type: models.TypeToken, Category: models.CategoryKubernetes, Scope: models.ScopeWorkspace, WorkspaceID: workspace.ID, OwnerID: user.ID, EncryptedPayload: kubePayload, Status: models.CredentialStatusActive},
	}
	for i := range credentials {
		if err := db.Create(&credentials[i]).Error; err != nil {
			t.Fatalf("create credential %d failed: %v", i, err)
		}
	}

	h := NewCredentialHandler()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/credentials?page=1&size=20", nil)
	c.Set("user_id", user.ID)
	c.Set("role", "user")
	c.Set("workspace_id", workspace.ID)
	c.Set("workspace_role", models.WorkspaceRoleDeveloper)

	h.ListCredentials(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}
	responseBody := w.Body.Bytes()
	for _, expected := range []string{
		`"lock_state":"unlocked"`,
		`"username":"ubuntu"`,
		`"key_type":"rsa"`,
		`"auth_mode":"server_token"`,
		`"server":"https://kubernetes.example.com"`,
		`"namespace":"prod"`,
		`"can_view_secret":true`,
		`"can_edit":true`,
		`"can_verify":true`,
		`"can_delete":true`,
		`"can_toggle_lock":true`,
	} {
		if !bytes.Contains(responseBody, []byte(expected)) {
			t.Fatalf("expected list response to contain %s, got %s", expected, w.Body.String())
		}
	}
	for _, forbidden := range []string{"vm-secret", "cluster-secret", "BEGIN PRIVATE KEY"} {
		if bytes.Contains(responseBody, []byte(forbidden)) {
			t.Fatalf("expected list response to omit secret value %q, got %s", forbidden, w.Body.String())
		}
	}
}

func TestCredentialHandler_LockedCredentialAllowsDeveloperVerifyButBlocksRevealAndMutation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	creator, workspace := seedCredentialTestUserAndWorkspace(t, db, "locked-creator", models.WorkspaceRoleDeveloper)
	developer := seedCredentialMember(t, db, workspace.ID, "locked-developer", models.WorkspaceRoleDeveloper)
	encrypted, err := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{"username": "root", "password": "locked-secret"})
	if err != nil {
		t.Fatalf("encrypt payload failed: %v", err)
	}
	credential := models.Credential{Name: "locked-vm", Type: models.TypePassword, Category: models.CategoryCustom, Scope: models.ScopeWorkspace, WorkspaceID: workspace.ID, OwnerID: creator.ID, EncryptedPayload: encrypted, Status: models.CredentialStatusActive, LockState: models.CredentialLockStateLocked}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatalf("create credential failed: %v", err)
	}

	h := NewCredentialHandler()
	detailRecorder := performCredentialRequest(t, h.GetCredential, developer.ID, "user", workspace.ID, http.MethodGet, "/api/v1/credentials/1", nil, pathID(credential.ID))
	if detailRecorder.Code != http.StatusOK {
		t.Fatalf("expected 200 for locked credential metadata, got %d body=%s", detailRecorder.Code, detailRecorder.Body.String())
	}
	for _, expected := range []string{`"lock_state":"locked"`, `"can_view_secret":false`, `"can_edit":false`, `"can_verify":true`, `"can_delete":false`, `"can_toggle_lock":false`} {
		if !bytes.Contains(detailRecorder.Body.Bytes(), []byte(expected)) {
			t.Fatalf("expected locked credential detail to contain %s, got %s", expected, detailRecorder.Body.String())
		}
	}

	payloadRecorder := performCredentialRequest(t, h.GetCredentialPayload, developer.ID, "user", workspace.ID, http.MethodGet, "/api/v1/credentials/1/payload", nil, pathID(credential.ID))
	if payloadRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for locked credential reveal, got %d body=%s", payloadRecorder.Code, payloadRecorder.Body.String())
	}
	verifyRecorder := performCredentialRequest(t, h.VerifyCredential, developer.ID, "user", workspace.ID, http.MethodPost, "/api/v1/credentials/1/verify", nil, pathID(credential.ID))
	if verifyRecorder.Code != http.StatusOK {
		t.Fatalf("expected 200 for locked credential verify, got %d body=%s", verifyRecorder.Code, verifyRecorder.Body.String())
	}
	if !bytes.Contains(verifyRecorder.Body.Bytes(), []byte(`"valid":true`)) {
		t.Fatalf("expected locked credential verify success for developer, got %s", verifyRecorder.Body.String())
	}
	updateRecorder := performCredentialRequest(t, h.UpdateCredential, developer.ID, "user", workspace.ID, http.MethodPut, "/api/v1/credentials/1", mustJSON(t, map[string]interface{}{"description": "blocked update"}), pathID(credential.ID))
	if updateRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for locked credential update, got %d body=%s", updateRecorder.Code, updateRecorder.Body.String())
	}
	deleteRecorder := performCredentialRequest(t, h.DeleteCredential, developer.ID, "user", workspace.ID, http.MethodDelete, "/api/v1/credentials/1", nil, pathID(credential.ID))
	if deleteRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for locked credential delete, got %d body=%s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
}

func TestCredentialHandler_LockedCredentialAllowsCreatorOwnerAndAdminSensitiveActions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	workspaceOwner, workspace := seedCredentialTestUserAndWorkspace(t, db, "workspace-owner", models.WorkspaceRoleOwner)
	creator := seedCredentialMember(t, db, workspace.ID, "credential-creator", models.WorkspaceRoleDeveloper)
	admin := models.User{Username: "sys-admin", Role: "admin", Status: "active"}
	if err := admin.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set admin password failed: %v", err)
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}
	encrypted, err := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{"token": "locked-token", "token_type": "bearer"})
	if err != nil {
		t.Fatalf("encrypt payload failed: %v", err)
	}

	creatorCredential := models.Credential{Name: "creator-locked", Type: models.TypeToken, Category: models.CategoryGitHub, Scope: models.ScopeWorkspace, WorkspaceID: workspace.ID, OwnerID: creator.ID, EncryptedPayload: encrypted, Status: models.CredentialStatusActive, LockState: models.CredentialLockStateLocked}
	ownerCredential := models.Credential{Name: "owner-locked", Type: models.TypeToken, Category: models.CategoryGitHub, Scope: models.ScopeWorkspace, WorkspaceID: workspace.ID, OwnerID: creator.ID, EncryptedPayload: encrypted, Status: models.CredentialStatusActive, LockState: models.CredentialLockStateLocked}
	adminCredential := models.Credential{Name: "admin-locked", Type: models.TypeToken, Category: models.CategoryGitHub, Scope: models.ScopeWorkspace, WorkspaceID: workspace.ID, OwnerID: creator.ID, EncryptedPayload: encrypted, Status: models.CredentialStatusActive, LockState: models.CredentialLockStateLocked}
	for _, credential := range []*models.Credential{&creatorCredential, &ownerCredential, &adminCredential} {
		if err := db.Create(credential).Error; err != nil {
			t.Fatalf("create locked credential failed: %v", err)
		}
	}

	h := NewCredentialHandler()
	creatorPayloadRecorder := performCredentialRequest(t, h.GetCredentialPayload, creator.ID, "user", workspace.ID, http.MethodGet, "/api/v1/credentials/1/payload", nil, pathID(creatorCredential.ID))
	if creatorPayloadRecorder.Code != http.StatusOK {
		t.Fatalf("expected creator reveal success, got %d body=%s", creatorPayloadRecorder.Code, creatorPayloadRecorder.Body.String())
	}
	ownerDeleteRecorder := performCredentialRequest(t, h.DeleteCredential, workspaceOwner.ID, "user", workspace.ID, http.MethodDelete, "/api/v1/credentials/1", nil, pathID(ownerCredential.ID), withWorkspaceRole(models.WorkspaceRoleOwner))
	if ownerDeleteRecorder.Code != http.StatusOK {
		t.Fatalf("expected workspace owner delete success, got %d body=%s", ownerDeleteRecorder.Code, ownerDeleteRecorder.Body.String())
	}
	adminVerifyRecorder := performCredentialRequest(t, h.VerifyCredential, admin.ID, "admin", workspace.ID, http.MethodPost, "/api/v1/credentials/1/verify", nil, pathID(adminCredential.ID))
	if adminVerifyRecorder.Code != http.StatusOK {
		t.Fatalf("expected system admin verify success, got %d body=%s", adminVerifyRecorder.Code, adminVerifyRecorder.Body.String())
	}
	if !bytes.Contains(adminVerifyRecorder.Body.Bytes(), []byte(`"valid":true`)) {
		t.Fatalf("expected system admin verify valid response, got %s", adminVerifyRecorder.Body.String())
	}
	adminUpdateRecorder := performCredentialRequest(t, h.UpdateCredential, admin.ID, "admin", workspace.ID, http.MethodPut, "/api/v1/credentials/1", mustJSON(t, map[string]interface{}{"lock_state": string(models.CredentialLockStateUnlocked)}), pathID(adminCredential.ID))
	if adminUpdateRecorder.Code != http.StatusOK {
		t.Fatalf("expected system admin unlock success, got %d body=%s", adminUpdateRecorder.Code, adminUpdateRecorder.Body.String())
	}
	var updated models.Credential
	if err := db.First(&updated, adminCredential.ID).Error; err != nil {
		t.Fatalf("reload admin credential failed: %v", err)
	}
	if updated.LockState != models.CredentialLockStateUnlocked {
		t.Fatalf("expected admin credential to be unlocked, got %s", updated.LockState)
	}
}

func TestCredentialHandler_ImpactAndBatchDelete(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "impact-user", models.WorkspaceRoleDeveloper)
	project := models.Project{Name: "proj", WorkspaceID: workspace.ID, OwnerID: user.ID}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	pipeline := models.Pipeline{Name: "build", WorkspaceID: workspace.ID, ProjectID: project.ID, OwnerID: user.ID, Config: "{}"}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	encrypted, err := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{"username": "demo", "password": "pass"})
	if err != nil {
		t.Fatalf("encrypt payload failed: %v", err)
	}
	credential := models.Credential{Name: "registry-auth", Type: models.TypePassword, Category: models.CategoryDocker, Scope: models.ScopeWorkspace, WorkspaceID: workspace.ID, OwnerID: user.ID, EncryptedPayload: encrypted, Status: models.CredentialStatusActive}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatalf("create credential failed: %v", err)
	}
	ref := models.PipelineCredentialRef{PipelineID: pipeline.ID, NodeID: "node-1", TaskType: "docker", CredentialSlot: "registry_auth", CredentialID: credential.ID}
	if err := db.Create(&ref).Error; err != nil {
		t.Fatalf("create ref failed: %v", err)
	}

	h := NewCredentialHandler()
	impactRecorder := performCredentialRequest(t, h.GetCredentialImpact, user.ID, "user", workspace.ID, http.MethodGet, "/api/v1/credentials/1/impact", nil, pathID(credential.ID))
	if impactRecorder.Code != http.StatusOK {
		t.Fatalf("impact status=%d body=%s", impactRecorder.Code, impactRecorder.Body.String())
	}
	if !bytes.Contains(impactRecorder.Body.Bytes(), []byte(`"reference_count":1`)) {
		t.Fatalf("unexpected impact response: %s", impactRecorder.Body.String())
	}

	batchBody := mustJSON(t, map[string]interface{}{"ids": []uint64{credential.ID}})
	batchRecorder := performCredentialRequest(t, h.BatchDeleteCredentials, user.ID, "user", workspace.ID, http.MethodPost, "/api/v1/credentials/batch/delete", batchBody)
	if batchRecorder.Code != http.StatusOK {
		t.Fatalf("batch delete status=%d body=%s", batchRecorder.Code, batchRecorder.Body.String())
	}
	var count int64
	db.Model(&models.Credential{}).Where("id = ?", credential.ID).Count(&count)
	if count != 0 {
		t.Fatalf("expected credential to be deleted")
	}
	db.Model(&models.PipelineCredentialRef{}).Where("credential_id = ?", credential.ID).Count(&count)
	if count != 0 {
		t.Fatalf("expected pipeline refs to be deleted")
	}
}

func TestCredentialHandler_LockedCredentialImpactRemainsMetadataAccessible(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	creator, workspace := seedCredentialTestUserAndWorkspace(t, db, "impact-creator", models.WorkspaceRoleDeveloper)
	viewer := seedCredentialMember(t, db, workspace.ID, "impact-viewer", models.WorkspaceRoleViewer)
	project := models.Project{Name: "impact-proj", WorkspaceID: workspace.ID, OwnerID: creator.ID}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	pipeline := models.Pipeline{Name: "impact-pipeline", WorkspaceID: workspace.ID, ProjectID: project.ID, OwnerID: creator.ID, Config: "{}"}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	encrypted, err := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{"username": "demo", "password": "pass"})
	if err != nil {
		t.Fatalf("encrypt payload failed: %v", err)
	}
	credential := models.Credential{Name: "locked-impact-auth", Type: models.TypePassword, Category: models.CategoryDocker, Scope: models.ScopeWorkspace, WorkspaceID: workspace.ID, OwnerID: creator.ID, EncryptedPayload: encrypted, LockState: models.CredentialLockStateLocked, Status: models.CredentialStatusActive}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatalf("create credential failed: %v", err)
	}
	ref := models.PipelineCredentialRef{PipelineID: pipeline.ID, NodeID: "node-1", TaskType: "docker", CredentialSlot: "registry_auth", CredentialID: credential.ID}
	if err := db.Create(&ref).Error; err != nil {
		t.Fatalf("create ref failed: %v", err)
	}

	h := NewCredentialHandler()
	impactRecorder := performCredentialRequest(t, h.GetCredentialImpact, viewer.ID, "user", workspace.ID, http.MethodGet, "/api/v1/credentials/1/impact", nil, pathID(credential.ID), withWorkspaceRole(models.WorkspaceRoleViewer))
	if impactRecorder.Code != http.StatusOK {
		t.Fatalf("expected metadata impact access for locked credential, got %d body=%s", impactRecorder.Code, impactRecorder.Body.String())
	}
	if !bytes.Contains(impactRecorder.Body.Bytes(), []byte(`"reference_count":1`)) {
		t.Fatalf("unexpected impact response: %s", impactRecorder.Body.String())
	}
}

func TestCredentialHandler_ProjectScopeValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "project-scope-user", models.WorkspaceRoleDeveloper)
	h := NewCredentialHandler()

	missingProjectBody := mustJSON(t, map[string]interface{}{
		"name":     "missing-project",
		"type":     string(models.TypeToken),
		"category": string(models.CategoryGitHub),
		"scope":    string(models.ScopeProject),
		"payload":  map[string]interface{}{"token": "ghp_missing_project"},
	})
	recorder := performCredentialRequest(t, h.CreateCredential, user.ID, "user", workspace.ID, http.MethodPost, "/api/v1/credentials", missingProjectBody)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing project_id, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	otherWorkspace := models.Workspace{Name: "other-space", Slug: "other-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: user.ID}
	if err := db.Create(&otherWorkspace).Error; err != nil {
		t.Fatalf("create other workspace failed: %v", err)
	}
	otherProject := models.Project{Name: "other-project", WorkspaceID: otherWorkspace.ID, OwnerID: user.ID}
	if err := db.Create(&otherProject).Error; err != nil {
		t.Fatalf("create other project failed: %v", err)
	}
	wrongWorkspaceBody := mustJSON(t, map[string]interface{}{
		"name":       "wrong-workspace-project",
		"type":       string(models.TypeToken),
		"category":   string(models.CategoryGitHub),
		"scope":      string(models.ScopeProject),
		"project_id": otherProject.ID,
		"payload":    map[string]interface{}{"token": "ghp_wrong_project"},
	})
	recorder = performCredentialRequest(t, h.CreateCredential, user.ID, "user", workspace.ID, http.MethodPost, "/api/v1/credentials", wrongWorkspaceBody)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for project outside workspace, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	project := models.Project{Name: "valid-project", WorkspaceID: workspace.ID, OwnerID: user.ID}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create valid project failed: %v", err)
	}
	validBody := mustJSON(t, map[string]interface{}{
		"name":       "valid-project-cred",
		"type":       string(models.TypeToken),
		"category":   string(models.CategoryGitHub),
		"scope":      string(models.ScopeProject),
		"project_id": project.ID,
		"payload":    map[string]interface{}{"token": "ghp_valid_project"},
	})
	recorder = performCredentialRequest(t, h.CreateCredential, user.ID, "user", workspace.ID, http.MethodPost, "/api/v1/credentials", validBody)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid project scope, got %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCredentialHandler_VerifyDisabledAndRevokedCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "verify-state-user", models.WorkspaceRoleDeveloper)
	encrypted, err := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{"token": "ghp_state"})
	if err != nil {
		t.Fatalf("encrypt payload failed: %v", err)
	}
	h := NewCredentialHandler()
	for _, status := range []models.CredentialStatus{models.CredentialStatusInactive, models.CredentialStatusRevoked} {
		credential := models.Credential{Name: "state-test-" + string(status), Type: models.TypeToken, Category: models.CategoryGitHub, Scope: models.ScopeWorkspace, WorkspaceID: workspace.ID, OwnerID: user.ID, EncryptedPayload: encrypted, Status: status}
		if err := db.Create(&credential).Error; err != nil {
			t.Fatalf("create credential failed: %v", err)
		}
		recorder := performCredentialRequest(t, h.VerifyCredential, user.ID, "user", workspace.ID, http.MethodPost, "/api/v1/credentials/verify", nil, pathID(credential.ID))
		if recorder.Code != http.StatusOK {
			t.Fatalf("verify status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		if !bytes.Contains(recorder.Body.Bytes(), []byte(`"valid":false`)) {
			t.Fatalf("expected invalid verify response for %s, got %s", status, recorder.Body.String())
		}
	}
}

func TestCredentialHandler_PartialStatusUpdateWithoutName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "partial-update-user", models.WorkspaceRoleDeveloper)
	encrypted, err := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{"token": "ghp_partial"})
	if err != nil {
		t.Fatalf("encrypt payload failed: %v", err)
	}
	credential := models.Credential{Name: "partial-update-cred", Type: models.TypeToken, Category: models.CategoryGitHub, Scope: models.ScopeWorkspace, WorkspaceID: workspace.ID, OwnerID: user.ID, EncryptedPayload: encrypted, Status: models.CredentialStatusActive}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatalf("create credential failed: %v", err)
	}

	h := NewCredentialHandler()
	body := mustJSON(t, map[string]interface{}{"status": "inactive"})
	recorder := performCredentialRequest(t, h.UpdateCredential, user.ID, "user", workspace.ID, http.MethodPut, "/api/v1/credentials/1", body, pathID(credential.ID))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 for partial status update, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if err := db.First(&credential, credential.ID).Error; err != nil {
		t.Fatalf("reload credential failed: %v", err)
	}
	if credential.Status != models.CredentialStatusInactive {
		t.Fatalf("expected inactive status after partial update, got %s", credential.Status)
	}
}

func TestCredentialHandler_KubernetesCategoryExposesSupportedModes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	NewCredentialHandler().GetCredentialCategories(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"value":"kubernetes"`)) {
		t.Fatalf("expected kubernetes category in response, got %s", w.Body.String())
	}
	for _, mode := range []string{"kubeconfig", "server_token", "server_cert"} {
		if !bytes.Contains(w.Body.Bytes(), []byte(mode)) {
			t.Fatalf("expected kubernetes supported mode %q in response, got %s", mode, w.Body.String())
		}
	}
}

func TestCredentialHandler_KubernetesCredentialModeValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "kube-mode-user", models.WorkspaceRoleDeveloper)
	h := NewCredentialHandler()

	validBodies := []map[string]interface{}{
		{
			"name":     "kubeconfig-cluster",
			"type":     string(models.TypeToken),
			"category": string(models.CategoryKubernetes),
			"scope":    string(models.ScopeWorkspace),
			"payload": map[string]interface{}{
				"auth_mode":  "kubeconfig",
				"kubeconfig": "apiVersion: v1\nclusters: []\ncontexts: []\ncurrent-context: \"\"\nusers: []\n",
			},
		},
		{
			"name":     "server-token-cluster",
			"type":     string(models.TypeToken),
			"category": string(models.CategoryKubernetes),
			"scope":    string(models.ScopeWorkspace),
			"payload": map[string]interface{}{
				"auth_mode": "server_token",
				"server":    "https://kubernetes.example.com",
				"token":     "cluster-token",
			},
		},
		{
			"name":     "server-cert-cluster",
			"type":     string(models.TypeCert),
			"category": string(models.CategoryKubernetes),
			"scope":    string(models.ScopeWorkspace),
			"payload": map[string]interface{}{
				"auth_mode": "server_cert",
				"server":    "https://kubernetes.example.com",
				"cert_pem":  "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----",
				"key_pem":   "-----BEGIN PRIVATE KEY-----\nMIIB\n-----END PRIVATE KEY-----",
			},
		},
	}

	for _, body := range validBodies {
		recorder := performCredentialRequest(t, h.CreateCredential, user.ID, "user", workspace.ID, http.MethodPost, "/api/v1/credentials", mustJSON(t, body))
		if recorder.Code != http.StatusOK {
			t.Fatalf("expected kubernetes credential create success for %v, got %d body=%s", body["name"], recorder.Code, recorder.Body.String())
		}
	}

	invalidBody := mustJSON(t, map[string]interface{}{
		"name":     "invalid-k8s-password",
		"type":     string(models.TypePassword),
		"category": string(models.CategoryKubernetes),
		"scope":    string(models.ScopeWorkspace),
		"payload": map[string]interface{}{
			"username": "admin",
			"password": "secret",
		},
	})
	recorder := performCredentialRequest(t, h.CreateCredential, user.ID, "user", workspace.ID, http.MethodPost, "/api/v1/credentials", invalidBody)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unsupported kubernetes password credential, got %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCredentialHandler_SSHKeyValidationRequiresPrivateKeyAndKeyType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "ssh-key-user", models.WorkspaceRoleDeveloper)
	h := NewCredentialHandler()

	validBody := mustJSON(t, map[string]interface{}{
		"name":     "vm-ssh-key",
		"type":     string(models.TypeSSHKey),
		"category": string(models.CategoryCustom),
		"scope":    string(models.ScopeWorkspace),
		"payload": map[string]interface{}{
			"private_key": "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----",
			"key_type":    "rsa",
		},
	})
	validRecorder := performCredentialRequest(t, h.CreateCredential, user.ID, "user", workspace.ID, http.MethodPost, "/api/v1/credentials", validBody)
	if validRecorder.Code != http.StatusOK {
		t.Fatalf("expected ssh key credential create success, got %d body=%s", validRecorder.Code, validRecorder.Body.String())
	}

	missingPrivateKeyBody := mustJSON(t, map[string]interface{}{
		"name":     "broken-ssh-key",
		"type":     string(models.TypeSSHKey),
		"category": string(models.CategoryCustom),
		"scope":    string(models.ScopeWorkspace),
		"payload": map[string]interface{}{
			"key_type": "rsa",
		},
	})
	recorder := performCredentialRequest(t, h.CreateCredential, user.ID, "user", workspace.ID, http.MethodPost, "/api/v1/credentials", missingPrivateKeyBody)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing private_key, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	missingKeyTypeBody := mustJSON(t, map[string]interface{}{
		"name":     "missing-key-type",
		"type":     string(models.TypeSSHKey),
		"category": string(models.CategoryCustom),
		"scope":    string(models.ScopeWorkspace),
		"payload": map[string]interface{}{
			"private_key": "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----",
		},
	})
	recorder = performCredentialRequest(t, h.CreateCredential, user.ID, "user", workspace.ID, http.MethodPost, "/api/v1/credentials", missingKeyTypeBody)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing key_type, got %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCredentialHandler_PasswordValidationRequiresUsernameAndPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "password-validation-user", models.WorkspaceRoleDeveloper)
	h := NewCredentialHandler()

	validBody := mustJSON(t, map[string]interface{}{
		"name":     "vm-password",
		"type":     string(models.TypePassword),
		"category": string(models.CategoryCustom),
		"scope":    string(models.ScopeWorkspace),
		"payload": map[string]interface{}{
			"username": "root",
			"password": "secret123",
		},
	})
	validRecorder := performCredentialRequest(t, h.CreateCredential, user.ID, "user", workspace.ID, http.MethodPost, "/api/v1/credentials", validBody)
	if validRecorder.Code != http.StatusOK {
		t.Fatalf("expected password credential create success, got %d body=%s", validRecorder.Code, validRecorder.Body.String())
	}

	missingUsernameBody := mustJSON(t, map[string]interface{}{
		"name":     "broken-password-no-user",
		"type":     string(models.TypePassword),
		"category": string(models.CategoryCustom),
		"scope":    string(models.ScopeWorkspace),
		"payload": map[string]interface{}{
			"password": "secret123",
		},
	})
	recorder := performCredentialRequest(t, h.CreateCredential, user.ID, "user", workspace.ID, http.MethodPost, "/api/v1/credentials", missingUsernameBody)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing username, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	missingPasswordBody := mustJSON(t, map[string]interface{}{
		"name":     "broken-password-no-pass",
		"type":     string(models.TypePassword),
		"category": string(models.CategoryCustom),
		"scope":    string(models.ScopeWorkspace),
		"payload": map[string]interface{}{
			"username": "root",
		},
	})
	recorder = performCredentialRequest(t, h.CreateCredential, user.ID, "user", workspace.ID, http.MethodPost, "/api/v1/credentials", missingPasswordBody)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing password, got %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCredentialHandler_ListCredentialsHonorsLargePageSize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "list-credentials-user", models.WorkspaceRoleDeveloper)
	for i := 0; i < 15; i++ {
		credential := models.Credential{
			Name:             "ssh-credential-" + strconv.Itoa(i),
			Type:             models.TypeSSHKey,
			Category:         models.CategoryCustom,
			Scope:            models.ScopeWorkspace,
			WorkspaceID:      workspace.ID,
			OwnerID:          user.ID,
			EncryptedPayload: "payload",
			Status:           models.CredentialStatusActive,
		}
		if err := db.Create(&credential).Error; err != nil {
			t.Fatalf("create credential %d failed: %v", i, err)
		}
	}

	h := NewCredentialHandler()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/credentials?page=1&size=500", nil)
	c.Set("user_id", user.ID)
	c.Set("role", "user")
	c.Set("workspace_id", workspace.ID)

	h.ListCredentials(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"total":15`)) {
		t.Fatalf("expected all credentials returned in list response, got %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("ssh-credential-14")) {
		t.Fatalf("expected later credentials included with large page size, got %s", w.Body.String())
	}
}

func seedCredentialTestUserAndWorkspace(t *testing.T, db *gorm.DB, username, workspaceRole string) (models.User, models.Workspace) {
	t.Helper()
	user := models.User{Username: username, Role: "user", Status: "active"}
	if err := user.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: username + "-workspace", Slug: username + "-workspace", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: user.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: workspaceRole, Status: models.WorkspaceMemberStatusActive}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create member failed: %v", err)
	}
	return user, workspace
}

func seedCredentialMember(t *testing.T, db *gorm.DB, workspaceID uint64, username, workspaceRole string) models.User {
	t.Helper()
	user := models.User{Username: username, Role: "user", Status: "active"}
	if err := user.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	member := models.WorkspaceMember{WorkspaceID: workspaceID, UserID: user.ID, Role: workspaceRole, Status: models.WorkspaceMemberStatusActive}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create member failed: %v", err)
	}
	return user
}

func performCredentialRequest(t *testing.T, handler gin.HandlerFunc, userID uint64, role string, workspaceID uint64, method, url string, body []byte, pathParams ...func(*gin.Context)) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader([]byte{})
	} else {
		reader = bytes.NewReader(body)
	}
	c.Request = httptest.NewRequest(method, url, reader)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("user_id", userID)
	c.Set("role", role)
	c.Set("workspace_id", workspaceID)
	c.Set("workspace_role", models.WorkspaceRoleDeveloper)
	for _, setParam := range pathParams {
		setParam(c)
	}
	handler(c)
	return w
}

func pathID(id uint64) func(*gin.Context) {
	return func(c *gin.Context) {
		c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(id, 10)}}
	}
}

func withWorkspaceRole(role string) func(*gin.Context) {
	return func(c *gin.Context) {
		c.Set("workspace_role", role)
	}
}

func mustJSON(t *testing.T, payload interface{}) []byte {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal json failed: %v", err)
	}
	return body
}

func responseDataID(t *testing.T, body []byte) uint64 {
	t.Helper()
	var payload struct {
		Data struct {
			ID uint64 `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal response failed: %v body=%s", err, string(body))
	}
	return payload.Data.ID
}
