package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
)

func TestProjectHandler_GetProjectListExcludesDeploymentTriggeredRunsFromLatestSummary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &ProjectHandler{DB: db}

	owner := models.User{Username: "project-summary-owner", Role: "user", Status: "active", Email: "project-summary-owner@example.com"}
	if err := owner.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set owner password failed: %v", err)
	}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner failed: %v", err)
	}
	workspace := models.Workspace{Name: "project-summary-ws", Slug: "project-summary-ws", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: owner.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}).Error; err != nil {
		t.Fatalf("create workspace member failed: %v", err)
	}
	project := models.Project{Name: "demo-project", WorkspaceID: workspace.ID, OwnerID: owner.ID}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	manualPipeline := models.Pipeline{Name: "manual-ci", WorkspaceID: workspace.ID, ProjectID: project.ID, OwnerID: owner.ID}
	if err := db.Create(&manualPipeline).Error; err != nil {
		t.Fatalf("create manual pipeline failed: %v", err)
	}
	deploymentPipeline := models.Pipeline{Name: "deploy-hidden", WorkspaceID: workspace.ID, ProjectID: project.ID, OwnerID: owner.ID}
	if err := db.Create(&deploymentPipeline).Error; err != nil {
		t.Fatalf("create deployment pipeline failed: %v", err)
	}

	manualRun := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: manualPipeline.ID, BuildNumber: 1, Status: models.PipelineRunStatusSuccess, TriggerType: "manual", TriggerUser: owner.Username, TriggerUserID: owner.ID}
	if err := db.Create(&manualRun).Error; err != nil {
		t.Fatalf("create manual run failed: %v", err)
	}
	if err := db.Model(&manualRun).Update("created_at", time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC)).Error; err != nil {
		t.Fatalf("update manual run created_at failed: %v", err)
	}
	deploymentRun := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: deploymentPipeline.ID, BuildNumber: 2, Status: models.PipelineRunStatusFailed, TriggerType: pipelineRunTriggerTypeDeploymentRequest, TriggerUser: "deployer", TriggerUserID: owner.ID}
	if err := db.Create(&deploymentRun).Error; err != nil {
		t.Fatalf("create deployment run failed: %v", err)
	}
	if err := db.Model(&deploymentRun).Update("created_at", time.Date(2026, 3, 31, 10, 0, 0, 0, time.UTC)).Error; err != nil {
		t.Fatalf("update deployment run created_at failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/projects?page=1&page_size=10", nil)
	c.Set("workspace_id", workspace.ID)
	c.Set("user_id", owner.ID)

	h.GetProjectList(c)

	if w.Code != http.StatusOK {
		t.Fatalf("project list status=%d body=%s", w.Code, w.Body.String())
	}

	var response struct {
		Code int `json:"code"`
		Data struct {
			List []struct {
				ID              uint64    `json:"id"`
				LatestRunner    string    `json:"latest_runner"`
				LatestRunTime   time.Time `json:"latest_run_time"`
				LatestRunStatus string    `json:"latest_run_status"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode project list response failed: %v body=%s", err, w.Body.String())
	}
	if len(response.Data.List) != 1 {
		t.Fatalf("project list count=%d, want 1 body=%s", len(response.Data.List), w.Body.String())
	}
	got := response.Data.List[0]
	if got.LatestRunStatus != models.PipelineRunStatusSuccess {
		t.Fatalf("latest run status=%s, want %s body=%s", got.LatestRunStatus, models.PipelineRunStatusSuccess, w.Body.String())
	}
	if got.LatestRunner != owner.Username {
		t.Fatalf("latest runner=%s, want %s body=%s", got.LatestRunner, owner.Username, w.Body.String())
	}
	if !got.LatestRunTime.Equal(time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("latest run time=%s, want manual run time body=%s", got.LatestRunTime.Format(time.RFC3339), w.Body.String())
	}
}
