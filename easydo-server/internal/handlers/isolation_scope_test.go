package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
)

func TestStatisticsOverview_IsScopedToCurrentWorkspace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &StatisticsHandler{DB: db}

	workspaceA := models.Workspace{Name: "ws-a", Slug: "ws-a", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: 1}
	workspaceB := models.Workspace{Name: "ws-b", Slug: "ws-b", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: 2}
	if err := db.Create(&workspaceA).Error; err != nil {
		t.Fatalf("create workspaceA failed: %v", err)
	}
	if err := db.Create(&workspaceB).Error; err != nil {
		t.Fatalf("create workspaceB failed: %v", err)
	}
	if err := db.Create(&models.Pipeline{WorkspaceID: workspaceA.ID, Name: "pipe-a", OwnerID: 1, Environment: "testing", Config: `{"version":"2.0","nodes":[],"edges":[]}`}).Error; err != nil {
		t.Fatalf("create pipelineA failed: %v", err)
	}
	if err := db.Create(&models.Pipeline{WorkspaceID: workspaceB.ID, Name: "pipe-b", OwnerID: 2, Environment: "testing", Config: `{"version":"2.0","nodes":[],"edges":[]}`}).Error; err != nil {
		t.Fatalf("create pipelineB failed: %v", err)
	}
	var pipelines []models.Pipeline
	if err := db.Find(&pipelines).Error; err != nil {
		t.Fatalf("list pipelines failed: %v", err)
	}
	if err := db.Create(&models.Project{WorkspaceID: workspaceA.ID, Name: "proj-a", OwnerID: 1}).Error; err != nil {
		t.Fatalf("create projectA failed: %v", err)
	}
	if err := db.Create(&models.Project{WorkspaceID: workspaceB.ID, Name: "proj-b", OwnerID: 2}).Error; err != nil {
		t.Fatalf("create projectB failed: %v", err)
	}
	if err := db.Create(&models.PipelineRun{WorkspaceID: workspaceA.ID, PipelineID: pipelines[0].ID, BuildNumber: 1, Status: models.PipelineRunStatusSuccess, Duration: 60}).Error; err != nil {
		t.Fatalf("create runA failed: %v", err)
	}
	if err := db.Create(&models.PipelineRun{WorkspaceID: workspaceB.ID, PipelineID: pipelines[1].ID, BuildNumber: 1, Status: models.PipelineRunStatusFailed, Duration: 120}).Error; err != nil {
		t.Fatalf("create runB failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/stats/overview", nil)
	c.Set("workspace_id", workspaceA.ID)
	h.GetOverview(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			TotalRuns     int64 `json:"total_runs"`
			PipelineCount int64 `json:"pipeline_count"`
			ProjectCount  int64 `json:"project_count"`
			FailedCount   int64 `json:"failed_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response failed: %v", err)
	}
	if resp.Data.TotalRuns != 1 || resp.Data.PipelineCount != 1 || resp.Data.ProjectCount != 1 || resp.Data.FailedCount != 0 {
		t.Fatalf("unexpected scoped stats: %+v body=%s", resp.Data, w.Body.String())
	}
}

func TestSecretStatistics_IsScopedToCurrentWorkspace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &SecretHandler{DB: db}
	user := models.User{Username: "secret-user", Role: "user", Status: "active"}
	if err := user.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspaceA := models.Workspace{Name: "ws-secret-a", Slug: "ws-secret-a", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: user.ID}
	workspaceB := models.Workspace{Name: "ws-secret-b", Slug: "ws-secret-b", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: user.ID}
	if err := db.Create(&workspaceA).Error; err != nil {
		t.Fatalf("create workspaceA failed: %v", err)
	}
	if err := db.Create(&workspaceB).Error; err != nil {
		t.Fatalf("create workspaceB failed: %v", err)
	}
	for _, ws := range []models.Workspace{workspaceA, workspaceB} {
		if err := db.Create(&models.WorkspaceMember{WorkspaceID: ws.ID, UserID: user.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: user.ID}).Error; err != nil {
			t.Fatalf("create membership failed: %v", err)
		}
	}
	if err := db.Create(&models.Secret{WorkspaceID: workspaceA.ID, Name: "secret-a", Type: models.SecretTypeToken, Scope: models.SecretScopeAll, Status: models.SecretStatusActive, CreatedBy: user.ID}).Error; err != nil {
		t.Fatalf("create secretA failed: %v", err)
	}
	if err := db.Create(&models.Secret{WorkspaceID: workspaceB.ID, Name: "secret-b", Type: models.SecretTypeToken, Scope: models.SecretScopeAll, Status: models.SecretStatusActive, CreatedBy: user.ID}).Error; err != nil {
		t.Fatalf("create secretB failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/secrets/statistics", nil)
	c.Set("user_id", user.ID)
	c.Set("role", user.Role)
	c.Set("workspace_id", workspaceA.ID)
	h.Statistics(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			TotalSecrets int64 `json:"total_secrets"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response failed: %v", err)
	}
	if resp.Data.TotalSecrets != 1 {
		t.Fatalf("total_secrets=%d, want=1 body=%s", resp.Data.TotalSecrets, w.Body.String())
	}
}

func TestPipelineRunEndpoints_RejectCrossWorkspaceAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	workspaceA := models.Workspace{Name: "ws-run-a", Slug: "ws-run-a", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: 1}
	workspaceB := models.Workspace{Name: "ws-run-b", Slug: "ws-run-b", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: 2}
	if err := db.Create(&workspaceA).Error; err != nil {
		t.Fatalf("create workspaceA failed: %v", err)
	}
	if err := db.Create(&workspaceB).Error; err != nil {
		t.Fatalf("create workspaceB failed: %v", err)
	}
	pipeline := models.Pipeline{Name: "pipe-b", WorkspaceID: workspaceB.ID, OwnerID: 2, Environment: "testing", Config: `{"version":"2.0","nodes":[],"edges":[]}`}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	run := models.PipelineRun{WorkspaceID: workspaceB.ID, PipelineID: pipeline.ID, BuildNumber: 1, Status: models.PipelineRunStatusSuccess}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}

	for _, tc := range []struct {
		name string
		path string
		hit  func(*gin.Context)
	}{
		{name: "runs", path: "/api/pipelines/1/history", hit: h.GetPipelineRuns},
		{name: "stats", path: "/api/pipelines/1/statistics", hit: h.GetPipelineStatistics},
		{name: "run-detail", path: "/api/pipelines/1/runs/1", hit: h.GetRunDetail},
		{name: "run-tasks", path: "/api/pipelines/1/runs/1/tasks", hit: h.GetRunTasks},
		{name: "run-logs", path: "/api/pipelines/1/runs/1/logs", hit: h.GetRunLogs},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, tc.path, nil)
			c.Params = gin.Params{{Key: "id", Value: "1"}, {Key: "run_id", Value: "1"}}
			c.Set("workspace_id", workspaceA.ID)
			tc.hit(c)
			if w.Code != http.StatusNotFound && w.Code != http.StatusForbidden {
				t.Fatalf("expected 404/403, got %d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestMessageList_IsScopedToRecipientAndWorkspace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &MessageHandler{DB: db}
	recipientA := uint64(1)
	recipientB := uint64(2)
	if err := db.Create(&models.Message{WorkspaceID: 10, RecipientID: &recipientA, Type: models.MessageTypeSystem, Title: "a", Content: "a"}).Error; err != nil {
		t.Fatalf("create msgA failed: %v", err)
	}
	if err := db.Create(&models.Message{WorkspaceID: 20, RecipientID: &recipientA, Type: models.MessageTypeSystem, Title: "b", Content: "b"}).Error; err != nil {
		t.Fatalf("create msgB failed: %v", err)
	}
	if err := db.Create(&models.Message{WorkspaceID: 10, RecipientID: &recipientB, Type: models.MessageTypeSystem, Title: "c", Content: "c"}).Error; err != nil {
		t.Fatalf("create msgC failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/messages?page=1&page_size=20", nil)
	c.Set("user_id", recipientA)
	c.Set("workspace_id", uint64(10))
	h.GetMessageList(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			List  []models.Message `json:"list"`
			Total int64            `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response failed: %v", err)
	}
	if resp.Data.Total != 1 || len(resp.Data.List) != 1 || resp.Data.List[0].Title != "a" {
		t.Fatalf("unexpected message scope result: total=%d list=%+v body=%s", resp.Data.Total, resp.Data.List, w.Body.String())
	}
}

func TestTaskEndpoints_AreScopedToCurrentWorkspace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &TaskHandler{DB: db}
	agent := models.Agent{Name: "agent-scope", Host: "host", Port: 9000, Token: "token", Status: models.AgentStatusOnline, RegistrationStatus: models.AgentRegistrationStatusApproved}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}
	if err := db.Create(&models.AgentTask{WorkspaceID: 20, AgentID: agent.ID, PipelineRunID: 1, Name: "cross-task", TaskType: "shell", Status: models.TaskStatusAssigned, Timeout: 60}).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	t.Run("list", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/tasks?page=1&page_size=20", nil)
		c.Set("workspace_id", uint64(10))
		h.GetTaskList(c)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		var resp struct {
			Data struct {
				Total int64 `json:"total"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("parse response failed: %v", err)
		}
		if resp.Data.Total != 0 {
			t.Fatalf("total=%d, want=0 body=%s", resp.Data.Total, w.Body.String())
		}
	})

	t.Run("detail", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/tasks/1", nil)
		c.Params = gin.Params{{Key: "id", Value: "1"}}
		c.Set("workspace_id", uint64(10))
		h.GetTaskDetail(c)
		if w.Code != http.StatusNotFound && w.Code != http.StatusForbidden {
			t.Fatalf("expected 404/403, got %d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestWebhookConfigs_AreScopedToCurrentWorkspace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &WebhookHandler{DB: db}
	if err := db.Create(&models.WebhookConfig{WorkspaceID: 10, Name: "cfg-a", URL: "https://a.example.com", Events: `[]`, IsActive: true}).Error; err != nil {
		t.Fatalf("create cfgA failed: %v", err)
	}
	if err := db.Create(&models.WebhookConfig{WorkspaceID: 20, Name: "cfg-b", URL: "https://b.example.com", Events: `[]`, IsActive: true}).Error; err != nil {
		t.Fatalf("create cfgB failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/webhooks", nil)
	c.Set("workspace_id", uint64(10))
	h.ListConfigs(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data []models.WebhookConfig `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response failed: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].Name != "cfg-a" {
		t.Fatalf("unexpected webhook scope result: %+v body=%s", resp.Data, w.Body.String())
	}
}
