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

func TestCredentialHandler_PersonalScopeBlocksOtherDeveloper(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	owner, workspace := seedCredentialTestUserAndWorkspace(t, db, "owner", models.WorkspaceRoleDeveloper)
	other := seedCredentialMember(t, db, workspace.ID, "other", models.WorkspaceRoleDeveloper)
	encrypted, err := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{"password": "secret"})
	if err != nil {
		t.Fatalf("encrypt payload failed: %v", err)
	}
	credential := models.Credential{Name: "personal-db", Type: models.TypePassword, Category: models.CategoryCustom, Scope: models.ScopeUser, WorkspaceID: workspace.ID, OwnerID: owner.ID, EncryptedPayload: encrypted, Status: models.CredentialStatusActive}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatalf("create credential failed: %v", err)
	}

	h := NewCredentialHandler()
	getRecorder := performCredentialRequest(t, h.GetCredential, other.ID, "user", workspace.ID, http.MethodGet, "/api/v1/credentials/1", nil, pathID(credential.ID))
	if getRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for personal credential read, got %d body=%s", getRecorder.Code, getRecorder.Body.String())
	}

	revealRecorder := performCredentialRequest(t, h.GetCredentialPayload, other.ID, "user", workspace.ID, http.MethodGet, "/api/v1/credentials/1/payload", nil, pathID(credential.ID))
	if revealRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for personal credential reveal, got %d body=%s", revealRecorder.Code, revealRecorder.Body.String())
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
