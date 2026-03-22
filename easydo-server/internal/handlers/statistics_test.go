package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
)

func TestGetTopPipelines_ExcludesDeploymentRequestRuns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &StatisticsHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "stats-top-user", models.WorkspaceRoleDeveloper)

	manualPipeline := models.Pipeline{
		Name:        "manual-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Config:      `{"version":"2.0","nodes":[{"id":"1","type":"shell","name":"Build","config":{"script":"echo hi"}}],"edges":[]}`,
	}
	if err := db.Create(&manualPipeline).Error; err != nil {
		t.Fatalf("create manual pipeline failed: %v", err)
	}

	deploymentPipeline := models.Pipeline{
		Name:        "platform-k8s-deploy",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Config:      `{"version":"2.0","nodes":[{"id":"1","type":"kubernetes","name":"Deploy","config":{"command":"echo deploy"}}],"edges":[]}`,
	}
	if err := db.Create(&deploymentPipeline).Error; err != nil {
		t.Fatalf("create deployment pipeline failed: %v", err)
	}

	runs := []models.PipelineRun{
		{WorkspaceID: workspace.ID, PipelineID: manualPipeline.ID, BuildNumber: 1, Status: models.PipelineRunStatusSuccess, TriggerType: "manual", Duration: 90},
		{WorkspaceID: workspace.ID, PipelineID: deploymentPipeline.ID, BuildNumber: 1, Status: models.PipelineRunStatusRunning, TriggerType: "deployment_request", Duration: 0},
	}
	for i := range runs {
		if err := db.Create(&runs[i]).Error; err != nil {
			t.Fatalf("create run failed: %v", err)
		}
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/stats/top-pipelines?limit=10", nil)
	c.Set("workspace_id", workspace.ID)

	h.GetTopPipelines(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("manual-pipeline")) {
		t.Fatalf("expected regular pipeline in ranking, got %s", w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("platform-k8s-deploy")) {
		t.Fatalf("expected deployment-triggered pipeline excluded from ranking, got %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"run_count":1`)) {
		t.Fatalf("expected manual pipeline run count preserved, got %s", w.Body.String())
	}
}
