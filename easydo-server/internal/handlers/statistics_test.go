package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type statisticsResponseEnvelope[T any] struct {
	Code int `json:"code"`
	Data T   `json:"data"`
}

func performStatisticsRequest(t *testing.T, handler func(*gin.Context), workspaceID uint64, target string) *httptest.ResponseRecorder {
	t.Helper()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	c.Set("workspace_id", workspaceID)

	handler(c)

	return w
}

func mustDecodeStatisticsResponse[T any](t *testing.T, recorder *httptest.ResponseRecorder) statisticsResponseEnvelope[T] {
	t.Helper()

	var response statisticsResponseEnvelope[T]
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response failed: %v body=%s", err, recorder.Body.String())
	}

	return response
}

func openStatisticsHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	name := strings.NewReplacer("/", "_", " ", "_", ":", "_").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", name)

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger:                                   logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite db failed: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db failed: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	if err := db.AutoMigrate(
		&models.User{},
		&models.Workspace{},
		&models.WorkspaceMember{},
		&models.Project{},
		&models.Pipeline{},
		&models.PipelineRun{},
	); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}

	return db
}

func seedStatisticsTestUserAndWorkspace(t *testing.T, db *gorm.DB, username, workspaceRole string) (models.User, models.Workspace) {
	t.Helper()

	user := models.User{Username: username, Role: "user", Status: "active"}
	if err := user.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	workspace := models.Workspace{
		Name:       username + "-workspace",
		Slug:       username + "-workspace",
		Status:     models.WorkspaceStatusActive,
		Visibility: models.WorkspaceVisibilityPrivate,
		CreatedBy:  user.ID,
	}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}

	member := models.WorkspaceMember{
		WorkspaceID: workspace.ID,
		UserID:      user.ID,
		Role:        workspaceRole,
		Status:      models.WorkspaceMemberStatusActive,
	}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create member failed: %v", err)
	}

	return user, workspace
}

func TestGetTopPipelines_ExcludesDeploymentRequestRuns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openStatisticsHandlerTestDB(t)
	h := &StatisticsHandler{DB: db}
	user, workspace := seedStatisticsTestUserAndWorkspace(t, db, "stats-top-user", models.WorkspaceRoleDeveloper)

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

func TestStatisticsHandlers_RejectInvalidDateRangeContracts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openStatisticsHandlerTestDB(t)
	h := &StatisticsHandler{DB: db}
	_, workspace := seedStatisticsTestUserAndWorkspace(t, db, "stats-range-contract-user", models.WorkspaceRoleDeveloper)

	testCases := []struct {
		name    string
		target  string
		handler func(*gin.Context)
	}{
		{name: "overview missing end date", target: "/api/stats/overview?start_date=2026-03-01", handler: h.GetOverview},
		{name: "overview invalid date", target: "/api/stats/overview?start_date=2026-03-01&end_date=2026-02-30", handler: h.GetOverview},
		{name: "overview reversed range", target: "/api/stats/overview?start_date=2026-03-02&end_date=2026-03-01", handler: h.GetOverview},
		{name: "trend missing start date", target: "/api/stats/trend?end_date=2026-03-01", handler: h.GetTrend},
		{name: "trend invalid date", target: "/api/stats/trend?start_date=bad&end_date=2026-03-01", handler: h.GetTrend},
		{name: "trend reversed range", target: "/api/stats/trend?start_date=2026-03-02&end_date=2026-03-01", handler: h.GetTrend},
		{name: "top pipelines missing end date", target: "/api/stats/top-pipelines?start_date=2026-03-01", handler: h.GetTopPipelines},
		{name: "top pipelines invalid date", target: "/api/stats/top-pipelines?start_date=2026-03-01&end_date=not-a-date", handler: h.GetTopPipelines},
		{name: "top pipelines reversed range", target: "/api/stats/top-pipelines?start_date=2026-03-02&end_date=2026-03-01", handler: h.GetTopPipelines},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			w := performStatisticsRequest(t, tc.handler, workspace.ID, tc.target)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestGetOverview_UsesInclusiveRequestedDateRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openStatisticsHandlerTestDB(t)
	h := &StatisticsHandler{DB: db}
	user, workspace := seedStatisticsTestUserAndWorkspace(t, db, "stats-overview-range-user", models.WorkspaceRoleDeveloper)

	pipeline := models.Pipeline{
		Name:        "range-overview-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Config:      `{"version":"2.0","nodes":[{"id":"1","type":"shell","name":"Build","config":{"script":"echo hi"}}],"edges":[]}`,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	runs := []models.PipelineRun{
		{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 1, Status: models.PipelineRunStatusSuccess, TriggerType: "manual", Duration: 90},
		{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 2, Status: models.PipelineRunStatusFailed, TriggerType: "manual", Duration: 30},
		{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 3, Status: models.PipelineRunStatusSuccess, TriggerType: "manual", Duration: 120},
	}
	createdAt := []time.Time{
		time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 3, 23, 59, 59, 0, time.UTC),
	}
	for i := range runs {
		if err := db.Create(&runs[i]).Error; err != nil {
			t.Fatalf("create run failed: %v", err)
		}
		if err := db.Model(&runs[i]).Update("created_at", createdAt[i]).Error; err != nil {
			t.Fatalf("update run created_at failed: %v", err)
		}
	}

	w := performStatisticsRequest(t, h.GetOverview, workspace.ID, "/api/stats/overview?start_date=2026-03-02&end_date=2026-03-03")
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	response := mustDecodeStatisticsResponse[OverviewResponse](t, w)
	if response.Data.TotalRuns != 2 {
		t.Fatalf("expected 2 runs in inclusive range, got %d body=%s", response.Data.TotalRuns, w.Body.String())
	}
	if response.Data.FailedCount != 1 {
		t.Fatalf("expected 1 failed run in inclusive range, got %d body=%s", response.Data.FailedCount, w.Body.String())
	}
	if response.Data.SuccessRate != 50 {
		t.Fatalf("expected 50 success rate, got %v body=%s", response.Data.SuccessRate, w.Body.String())
	}
}

func TestGetTrend_UsesRequestedDateRangeInsteadOfTodayAlignedDays(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openStatisticsHandlerTestDB(t)
	h := &StatisticsHandler{DB: db}
	user, workspace := seedStatisticsTestUserAndWorkspace(t, db, "stats-trend-range-user", models.WorkspaceRoleDeveloper)

	pipeline := models.Pipeline{
		Name:        "trend-range-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Config:      `{"version":"2.0","nodes":[{"id":"1","type":"shell","name":"Build","config":{"script":"echo hi"}}],"edges":[]}`,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	runs := []struct {
		buildNumber int
		status      string
		createdAt   time.Time
	}{
		{buildNumber: 1, status: models.PipelineRunStatusSuccess, createdAt: time.Date(2026, 2, 10, 8, 0, 0, 0, time.UTC)},
		{buildNumber: 2, status: models.PipelineRunStatusFailed, createdAt: time.Date(2026, 2, 12, 15, 0, 0, 0, time.UTC)},
	}
	for _, run := range runs {
		pipelineRun := models.PipelineRun{
			WorkspaceID: workspace.ID,
			PipelineID:  pipeline.ID,
			BuildNumber: run.buildNumber,
			Status:      run.status,
			TriggerType: "manual",
			Duration:    60,
		}
		if err := db.Create(&pipelineRun).Error; err != nil {
			t.Fatalf("create run failed: %v", err)
		}
		if err := db.Model(&pipelineRun).Update("created_at", run.createdAt).Error; err != nil {
			t.Fatalf("update run created_at failed: %v", err)
		}
	}

	w := performStatisticsRequest(t, h.GetTrend, workspace.ID, "/api/stats/trend?start_date=2026-02-10&end_date=2026-02-12")
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	response := mustDecodeStatisticsResponse[TrendResponse](t, w)
	if len(response.Data.DailyRuns) != 3 {
		t.Fatalf("expected 3 daily points for requested range, got %d body=%s", len(response.Data.DailyRuns), w.Body.String())
	}

	if response.Data.DailyRuns[0].Date != "2026-02-10" || response.Data.DailyRuns[0].Total != 1 || response.Data.DailyRuns[0].Success != 1 {
		t.Fatalf("expected first trend point to match requested start date, got %+v body=%s", response.Data.DailyRuns[0], w.Body.String())
	}
	if response.Data.DailyRuns[1].Date != "2026-02-11" || response.Data.DailyRuns[1].Total != 0 {
		t.Fatalf("expected middle trend point to be zero-filled for 2026-02-11, got %+v body=%s", response.Data.DailyRuns[1], w.Body.String())
	}
	if response.Data.DailyRuns[2].Date != "2026-02-12" || response.Data.DailyRuns[2].Total != 1 || response.Data.DailyRuns[2].Failed != 1 {
		t.Fatalf("expected final trend point to match requested end date, got %+v body=%s", response.Data.DailyRuns[2], w.Body.String())
	}
}

func TestGetTopPipelines_UsesInclusiveRequestedDateRangeAndKeepsDeploymentRequestExclusion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openStatisticsHandlerTestDB(t)
	h := &StatisticsHandler{DB: db}
	user, workspace := seedStatisticsTestUserAndWorkspace(t, db, "stats-top-range-user", models.WorkspaceRoleDeveloper)

	manualPipeline := models.Pipeline{
		Name:        "manual-range-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Config:      `{"version":"2.0","nodes":[{"id":"1","type":"shell","name":"Build","config":{"script":"echo hi"}}],"edges":[]}`,
	}
	if err := db.Create(&manualPipeline).Error; err != nil {
		t.Fatalf("create manual pipeline failed: %v", err)
	}

	deploymentPipeline := models.Pipeline{
		Name:        "deployment-range-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Config:      `{"version":"2.0","nodes":[{"id":"1","type":"shell","name":"Deploy","config":{"script":"echo deploy"}}],"edges":[]}`,
	}
	if err := db.Create(&deploymentPipeline).Error; err != nil {
		t.Fatalf("create deployment pipeline failed: %v", err)
	}

	runs := []struct {
		pipelineID  uint64
		buildNumber int
		status      string
		triggerType string
		createdAt   time.Time
	}{
		{pipelineID: manualPipeline.ID, buildNumber: 1, status: models.PipelineRunStatusSuccess, triggerType: "manual", createdAt: time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)},
		{pipelineID: manualPipeline.ID, buildNumber: 2, status: models.PipelineRunStatusFailed, triggerType: "manual", createdAt: time.Date(2026, 3, 2, 9, 30, 0, 0, time.UTC)},
		{pipelineID: manualPipeline.ID, buildNumber: 3, status: models.PipelineRunStatusSuccess, triggerType: "manual", createdAt: time.Date(2026, 3, 4, 11, 0, 0, 0, time.UTC)},
		{pipelineID: deploymentPipeline.ID, buildNumber: 1, status: models.PipelineRunStatusSuccess, triggerType: "deployment_request", createdAt: time.Date(2026, 3, 2, 13, 0, 0, 0, time.UTC)},
	}
	for _, run := range runs {
		pipelineRun := models.PipelineRun{
			WorkspaceID: workspace.ID,
			PipelineID:  run.pipelineID,
			BuildNumber: run.buildNumber,
			Status:      run.status,
			TriggerType: run.triggerType,
			Duration:    60,
		}
		if err := db.Create(&pipelineRun).Error; err != nil {
			t.Fatalf("create run failed: %v", err)
		}
		if err := db.Model(&pipelineRun).Update("created_at", run.createdAt).Error; err != nil {
			t.Fatalf("update run created_at failed: %v", err)
		}
	}

	w := performStatisticsRequest(t, h.GetTopPipelines, workspace.ID, "/api/stats/top-pipelines?limit=10&start_date=2026-03-02&end_date=2026-03-02")
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	response := mustDecodeStatisticsResponse[TopPipelinesResponse](t, w)
	if len(response.Data.Pipelines) != 1 {
		t.Fatalf("expected exactly one ranked pipeline in date range, got %d body=%s", len(response.Data.Pipelines), w.Body.String())
	}
	if response.Data.Pipelines[0].Name != "manual-range-pipeline" {
		t.Fatalf("expected manual pipeline to remain after exclusion, got %+v body=%s", response.Data.Pipelines[0], w.Body.String())
	}
	if response.Data.Pipelines[0].RunCount != 1 {
		t.Fatalf("expected inclusive single-day run count of 1, got %d body=%s", response.Data.Pipelines[0].RunCount, w.Body.String())
	}
}
