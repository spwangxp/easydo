package routers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"easydo-server/internal/config"
	"easydo-server/internal/middleware"
	"easydo-server/internal/models"

	"github.com/gin-gonic/gin"
)

func TestPipelineRunHistoryRoutes_ParameterViewAndRerunPreview(t *testing.T) {
	setupRouterUserCreateTestEnv(t)
	db := openRouterTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() {
		models.DB = originalDB
	})
	if err := db.AutoMigrate(&models.MasterKey{}, &models.PipelineTrigger{}); err != nil {
		t.Fatalf("auto migrate master key failed: %v", err)
	}
	if _, err := models.LoadOrCreateMasterKey(db); err != nil {
		t.Fatalf("load master key failed: %v", err)
	}

	user := models.User{Username: "route-pipeline-history-user", Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{
		Name:       "route-pipeline-history-workspace",
		Slug:       "route-pipeline-history-workspace",
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
		Role:        models.WorkspaceRoleDeveloper,
		Status:      models.WorkspaceMemberStatusActive,
		InvitedBy:   user.ID,
	}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create workspace member failed: %v", err)
	}

	pipeline := models.Pipeline{
		Name:        "route-pipeline-history-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Config:      `{"version":"2.0","nodes":[{"id":"node_1","type":"shell","name":"Build","config":{"script":"echo hi"}}],"edges":[]}`,
		Definition:  `{"version":"2.0","nodes":[{"node_id":"node_1","node_name":"Build","type":"shell","task_key":"shell","params":[{"key":"script","label":"脚本","value":"echo current","is_flexible":true}]}],"edges":[]}`,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	run := models.PipelineRun{
		WorkspaceID:      workspace.ID,
		PipelineID:       pipeline.ID,
		BuildNumber:      5,
		TriggerType:      "manual",
		TriggerSource:    "pipeline_detail",
		TriggerUser:      "alice",
		RunConfig:        `{"trigger":{"type":"manual","source":"pipeline_detail","operator":"alice"},"inputs":{"node_1":{"script":"echo override"}}}`,
		PipelineSnapshot: `{"nodes":[{"node_id":"node_1","node_name":"Build","params":[{"key":"script","label":"脚本","value":"echo default","is_flexible":true}]}]}`,
		ResolvedNodes:    `[{"node_id":"node_1","resolved_inputs":{"script":"echo historical"}}]`,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}

	config.Config.Set("server.mode", "test")
	gin.SetMode(gin.TestMode)
	router := InitRouter()
	token := issueRouterTestToken(t, &user)

	performRequest := func(method, url string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set(middleware.WorkspaceHeaderKey, fmt.Sprintf("%d", workspace.ID))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	t.Run("parameter view route returns historical snapshot view", func(t *testing.T) {
		w := performRequest(http.MethodGet, fmt.Sprintf("/api/pipelines/%d/runs/%d/parameter-view", pipeline.ID, run.ID))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
		var resp struct {
			Code int `json:"code"`
			Data struct {
				RunID uint64 `json:"run_id"`
				Nodes []struct {
					RuntimeParams []struct {
						Key   string      `json:"key"`
						Value interface{} `json:"value"`
					} `json:"runtime_params"`
				} `json:"nodes"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response failed: %v body=%s", err, w.Body.String())
		}
		if resp.Code != 200 || resp.Data.RunID != run.ID {
			t.Fatalf("unexpected response: %#v", resp)
		}
		if len(resp.Data.Nodes) != 1 || len(resp.Data.Nodes[0].RuntimeParams) != 1 {
			t.Fatalf("unexpected nodes payload: %#v", resp.Data.Nodes)
		}
		if resp.Data.Nodes[0].RuntimeParams[0].Key != "script" || resp.Data.Nodes[0].RuntimeParams[0].Value != "echo override" {
			t.Fatalf("unexpected runtime param payload: %#v", resp.Data.Nodes[0].RuntimeParams[0])
		}
	})

	t.Run("rerun preview route returns strict mapping result", func(t *testing.T) {
		w := performRequest(http.MethodPost, fmt.Sprintf("/api/pipelines/%d/runs/%d/rerun-preview", pipeline.ID, run.ID))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
		var resp struct {
			Code int `json:"code"`
			Data struct {
				CanEnterRunDialog bool                              `json:"can_enter_run_dialog"`
				MatchKey          string                            `json:"match_key"`
				Matched           []map[string]interface{}          `json:"matched"`
				Mismatched        []map[string]interface{}          `json:"mismatched"`
				PrefillInputs     map[string]map[string]interface{} `json:"prefill_inputs"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response failed: %v body=%s", err, w.Body.String())
		}
		if resp.Code != 200 || !resp.Data.CanEnterRunDialog {
			t.Fatalf("unexpected rerun preview response: %#v", resp)
		}
		if resp.Data.MatchKey != "node_id+param_key" {
			t.Fatalf("unexpected match key: %#v", resp.Data)
		}
		if len(resp.Data.Matched) != 1 || len(resp.Data.Mismatched) != 0 {
			t.Fatalf("unexpected match payload: %#v", resp.Data)
		}
		if got := resp.Data.PrefillInputs["node_1"]["script"]; got != "echo override" {
			t.Fatalf("unexpected prefill payload: %#v", resp.Data.PrefillInputs)
		}
	})
}
