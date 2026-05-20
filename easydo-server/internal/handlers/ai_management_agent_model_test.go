package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return openHandlerTestDB(t)
}

func migrateAgentTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(
		&models.AIProvider{},
		&models.AIModelBinding{},
		&models.AIAgent{},
		&models.AIRuntimeProfile{},
		&models.AISession{},
	))
}

func TestBindingHandlersPersistRuntimeSelectionWithoutCapabilitiesPayload(t *testing.T) {
	db := newTestDB(t)
	migrateAgentTables(t, db)
	h := &AIProviderHandler{DB: db}

	admin := models.User{Username: "binding-runtime-admin", Role: "admin", Status: "active"}
	require.NoError(t, db.Create(&admin).Error)
	workspace := models.Workspace{Name: "binding-runtime-space", Slug: "binding-runtime-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindAdmin, CreatedBy: admin.ID}
	require.NoError(t, db.Create(&workspace).Error)
	provider := models.AIProvider{WorkspaceID: workspace.ID, Name: "binding-runtime-provider", ProviderType: "openai", Status: models.AIProviderStatusActive, CreatedBy: admin.ID}
	require.NoError(t, db.Create(&provider).Error)
	model := models.AIModelCatalog{Name: "binding-runtime-model", DisplayName: "Binding Runtime Model", Source: "seed", SourceModelID: "binding-runtime-model", ImportedBy: admin.ID}
	require.NoError(t, db.Create(&model).Error)

	createCtx, createW := newAIManagementTestContext(t, http.MethodPost, "/api/store/ai-providers/"+strconv.FormatUint(provider.ID, 10)+"/model-bindings", admin.ID, admin.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindAdmin, map[string]any{
		"model_id":           model.ID,
		"provider_model_key": "demo-model",
		"settings_json":      map[string]any{"temperature": 0.2},
		"status":             "active",
	})
	createCtx.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(provider.ID, 10)}}
	createCtx.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CreateBinding(createCtx)

	require.Equal(t, http.StatusOK, createW.Code, createW.Body.String())
	var createResp struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(createW.Body.Bytes(), &createResp))
	_, hasCreateCapabilities := createResp.Data["capabilities_json"]
	require.False(t, hasCreateCapabilities)

	var binding models.AIModelBinding
	require.NoError(t, db.Where("workspace_id = ? AND provider_id = ?", workspace.ID, provider.ID).First(&binding).Error)
	require.JSONEq(t, `{"temperature":0.2}`, binding.SettingsJSON)

	updateCtx, updateW := newAIManagementTestContext(t, http.MethodPut, "/api/store/ai-providers/"+strconv.FormatUint(provider.ID, 10)+"/model-bindings/"+strconv.FormatUint(binding.ID, 10), admin.ID, admin.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindAdmin, map[string]any{
		"model_id":           model.ID,
		"provider_model_key": "demo-model-v2",
		"settings_json":      map[string]any{"temperature": 0.4},
		"status":             "disabled",
	})
	updateCtx.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(provider.ID, 10)}, {Key: "binding_id", Value: strconv.FormatUint(binding.ID, 10)}}
	updateCtx.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.UpdateBinding(updateCtx)

	require.Equal(t, http.StatusOK, updateW.Code, updateW.Body.String())
	var updateResp struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(updateW.Body.Bytes(), &updateResp))
	_, hasUpdateCapabilities := updateResp.Data["capabilities_json"]
	require.False(t, hasUpdateCapabilities)
	require.NoError(t, db.First(&binding, binding.ID).Error)
	require.Equal(t, "demo-model-v2", binding.ProviderModelKey)
	require.JSONEq(t, `{"temperature":0.4}`, binding.SettingsJSON)
	require.Equal(t, models.AIModelBindingStatusDisabled, binding.Status)

	rejectCtx, rejectW := newAIManagementTestContext(t, http.MethodPost, "/api/store/ai-providers/"+strconv.FormatUint(provider.ID, 10)+"/model-bindings", admin.ID, admin.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindAdmin, map[string]any{
		"model_id":           model.ID,
		"provider_model_key": "legacy-model",
		"capabilities_json":  map[string]any{"chat": true},
		"settings_json":      map[string]any{"temperature": 0.6},
		"status":             "active",
	})
	rejectCtx.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(provider.ID, 10)}}
	rejectCtx.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CreateBinding(rejectCtx)

	require.Equal(t, http.StatusBadRequest, rejectW.Code, rejectW.Body.String())
	var rejectResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(rejectW.Body.Bytes(), &rejectResp))
	require.Contains(t, rejectResp.Message, "capabilities_json")

	missingModelCtx, missingModelW := newAIManagementTestContext(t, http.MethodPost, "/api/store/ai-providers/"+strconv.FormatUint(provider.ID, 10)+"/model-bindings", admin.ID, admin.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindAdmin, map[string]any{
		"provider_model_key": "missing-model",
		"settings_json":      map[string]any{"temperature": 0.7},
		"status":             "active",
	})
	missingModelCtx.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(provider.ID, 10)}}
	missingModelCtx.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CreateBinding(missingModelCtx)

	require.Equal(t, http.StatusBadRequest, missingModelW.Code, missingModelW.Body.String())
	var missingModelResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(missingModelW.Body.Bytes(), &missingModelResp))
	require.Contains(t, missingModelResp.Message, "model_id")

	missingUpdateModelCtx, missingUpdateModelW := newAIManagementTestContext(t, http.MethodPut, "/api/store/ai-providers/"+strconv.FormatUint(provider.ID, 10)+"/model-bindings/"+strconv.FormatUint(binding.ID, 10), admin.ID, admin.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindAdmin, map[string]any{
		"provider_model_key": "missing-update-model",
		"settings_json":      map[string]any{"temperature": 0.8},
		"status":             "active",
	})
	missingUpdateModelCtx.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(provider.ID, 10)}, {Key: "binding_id", Value: strconv.FormatUint(binding.ID, 10)}}
	missingUpdateModelCtx.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.UpdateBinding(missingUpdateModelCtx)

	require.Equal(t, http.StatusBadRequest, missingUpdateModelW.Code, missingUpdateModelW.Body.String())
	var missingUpdateModelResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(missingUpdateModelW.Body.Bytes(), &missingUpdateModelResp))
	require.Contains(t, missingUpdateModelResp.Message, "model_id")
}

func TestCreateAgentPersistsRuntimeCapabilityFields(t *testing.T) {
	db := newTestDB(t)
	migrateAgentTables(t, db)

	runtimeProfileID := uint64(7)
	agent := models.AIAgent{
		WorkspaceID:      11,
		Name:             "pipeline-review-agent",
		RuntimeProfileID: &runtimeProfileID,
		SystemPrompt:     "review runtime context",
		ToolsJSON:        `[{"name":"read_pipeline"}]`,
		SkillsJSON:       `[{"name":"summarize"}]`,
		MemoryJSON:       `{"mode":"short_term"}`,
		MCPServersJSON:   `[{"name":"grafana"}]`,
		SubAgentsJSON:    `[{"agent_id":22,"name":"log-agent"}]`,
		Status:           models.AIAgentStatusActive,
		CreatedBy:        1,
	}

	require.NoError(t, db.Create(&agent).Error)

	var stored models.AIAgent
	require.NoError(t, db.First(&stored, agent.ID).Error)
	var persisted sql.NullInt64
	require.NoError(t, db.Raw("SELECT runtime_profile_id FROM ai_agents WHERE id = ?", agent.ID).Scan(&persisted).Error)
	require.True(t, persisted.Valid)
	require.Equal(t, int64(7), persisted.Int64)
	require.Contains(t, stored.ToolsJSON, "read_pipeline")
	require.Contains(t, stored.SkillsJSON, "summarize")
	require.Contains(t, stored.MemoryJSON, "short_term")
	require.Contains(t, stored.MCPServersJSON, "grafana")
	require.Contains(t, stored.SubAgentsJSON, "log-agent")
}

func TestCreateAgentPersistsRuntimeProfileAndCapabilityPayload(t *testing.T) {
	db := newTestDB(t)
	migrateAgentTables(t, db)
	h := &AIAgentHandler{DB: db}

	owner := models.User{Username: "ai-agent-create-owner", Role: "user", Status: "active"}
	require.NoError(t, db.Create(&owner).Error)
	workspace := models.Workspace{Name: "ai-agent-create-space", Slug: "ai-agent-create-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	require.NoError(t, db.Create(&workspace).Error)
	require.NoError(t, db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}).Error)
	model := models.AIModelCatalog{Name: "agent-create-model", DisplayName: "Agent Create Model", Source: "seed", SourceModelID: "agent-create-model", ImportedBy: owner.ID}
	require.NoError(t, db.Create(&model).Error)
	runtimeProfile := models.AIRuntimeProfile{WorkspaceID: workspace.ID, Name: "workspace-runtime", ModelID: model.ID, FallbackEnabled: true, Status: models.AIRuntimeProfileStatusActive, CreatedBy: owner.ID}
	require.NoError(t, db.Create(&runtimeProfile).Error)

	c, w := newAIManagementTestContext(t, http.MethodPost, "/api/ai/agents", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"name":               "runtime-agent",
		"runtime_profile_id": runtimeProfile.ID,
		"tools_json":         []map[string]any{{"name": "search_logs"}},
		"skills_json":        []map[string]any{{"name": "summarize"}},
		"memory_json":        map[string]any{"kind": "session"},
		"mcp_servers_json":   []map[string]any{{"name": "grafana"}},
		"sub_agents_json":    []map[string]any{{"name": "log-agent"}},
		"metadata_json":      map[string]any{"owner": "workspace"},
		"status":             "active",
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CreateAgent(c)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var stored models.AIAgent
	require.NoError(t, db.Where("workspace_id = ?", workspace.ID).First(&stored).Error)
	require.NotNil(t, stored.RuntimeProfileID)
	require.Equal(t, runtimeProfile.ID, *stored.RuntimeProfileID)
	require.Contains(t, stored.ToolsJSON, "search_logs")
	require.Contains(t, stored.SkillsJSON, "summarize")
	require.Contains(t, stored.MemoryJSON, "session")
	require.Contains(t, stored.MCPServersJSON, "grafana")
	require.Contains(t, stored.SubAgentsJSON, "log-agent")
	require.Contains(t, stored.MetadataJSON, "workspace")
}

func TestCreateAgentWithoutRuntimeProfilePersistsNull(t *testing.T) {
	db := newTestDB(t)
	migrateAgentTables(t, db)
	h := &AIAgentHandler{DB: db}

	owner := models.User{Username: "ai-agent-create-null-owner", Role: "user", Status: "active"}
	require.NoError(t, db.Create(&owner).Error)
	workspace := models.Workspace{Name: "ai-agent-create-null-space", Slug: "ai-agent-create-null-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	require.NoError(t, db.Create(&workspace).Error)
	require.NoError(t, db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}).Error)

	c, w := newAIManagementTestContext(t, http.MethodPost, "/api/ai/agents", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"name":   "runtime-agent-null-profile",
		"status": "active",
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CreateAgent(c)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var stored models.AIAgent
	require.NoError(t, db.Where("workspace_id = ?", workspace.ID).First(&stored).Error)
	require.Nil(t, stored.RuntimeProfileID)
	var persisted sql.NullInt64
	require.NoError(t, db.Raw("SELECT runtime_profile_id FROM ai_agents WHERE id = ?", stored.ID).Scan(&persisted).Error)
	require.False(t, persisted.Valid)
}

func TestUpdateAgentPersistsRuntimeProfileAndCapabilityPayload(t *testing.T) {
	db := newTestDB(t)
	migrateAgentTables(t, db)
	h := &AIAgentHandler{DB: db}

	owner := models.User{Username: "ai-agent-update-owner", Role: "user", Status: "active"}
	require.NoError(t, db.Create(&owner).Error)
	workspace := models.Workspace{Name: "ai-agent-update-space", Slug: "ai-agent-update-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	require.NoError(t, db.Create(&workspace).Error)
	require.NoError(t, db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}).Error)
	model := models.AIModelCatalog{Name: "agent-update-model", DisplayName: "Agent Update Model", Source: "seed", SourceModelID: "agent-update-model", ImportedBy: owner.ID}
	require.NoError(t, db.Create(&model).Error)
	runtimeProfile := models.AIRuntimeProfile{WorkspaceID: workspace.ID, Name: "workspace-runtime-update", ModelID: model.ID, FallbackEnabled: true, Status: models.AIRuntimeProfileStatusActive, CreatedBy: owner.ID}
	require.NoError(t, db.Create(&runtimeProfile).Error)
	agent := models.AIAgent{
		WorkspaceID:        workspace.ID,
		Name:               "mutable-agent",
		ScopeType:          models.AgentScopeWorkspace,
		UserPromptTemplate: "existing-user-prompt",
		InputSchemaJSON:    `{"type":"object","properties":{"input":{"type":"string"}}}`,
		OutputSchemaJSON:   `{"type":"object","properties":{"summary":{"type":"string"}}}`,
		ToolPolicyJSON:     `{"mode":"strict"}`,
		Status:             models.AIAgentStatusDraft,
		CreatedBy:          owner.ID,
	}
	require.NoError(t, db.Create(&agent).Error)

	c, w := newAIManagementTestContext(t, http.MethodPut, "/api/ai/agents/"+strconv.FormatUint(agent.ID, 10), owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"name":               "mutable-agent-v2",
		"runtime_profile_id": runtimeProfile.ID,
		"tools_json":         []map[string]any{{"name": "open_ticket"}},
		"skills_json":        []map[string]any{{"name": "route_alert"}},
		"memory_json":        map[string]any{"kind": "workspace"},
		"mcp_servers_json":   []map[string]any{{"name": "sentry"}},
		"sub_agents_json":    []map[string]any{{"name": "triage-agent"}},
		"metadata_json":      map[string]any{"team": "ops"},
		"status":             "active",
	})
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(agent.ID, 10)}}
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.UpdateAgent(c)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var stored models.AIAgent
	require.NoError(t, db.First(&stored, agent.ID).Error)
	require.NotNil(t, stored.RuntimeProfileID)
	require.Equal(t, runtimeProfile.ID, *stored.RuntimeProfileID)
	require.Contains(t, stored.ToolsJSON, "open_ticket")
	require.Contains(t, stored.SkillsJSON, "route_alert")
	require.Contains(t, stored.MemoryJSON, "workspace")
	require.Contains(t, stored.MCPServersJSON, "sentry")
	require.Contains(t, stored.SubAgentsJSON, "triage-agent")
	require.Contains(t, stored.MetadataJSON, "ops")
	require.Equal(t, "existing-user-prompt", stored.UserPromptTemplate)
	require.JSONEq(t, `{"type":"object","properties":{"input":{"type":"string"}}}`, stored.InputSchemaJSON)
	require.JSONEq(t, `{"type":"object","properties":{"summary":{"type":"string"}}}`, stored.OutputSchemaJSON)
	require.JSONEq(t, `{"mode":"strict"}`, stored.ToolPolicyJSON)
}

func TestUpdateAgentOmittingRuntimeProfileKeepsExistingRelation(t *testing.T) {
	db := newTestDB(t)
	migrateAgentTables(t, db)
	h := &AIAgentHandler{DB: db}

	owner := models.User{Username: "ai-agent-keep-runtime-owner", Role: "user", Status: "active"}
	require.NoError(t, db.Create(&owner).Error)
	workspace := models.Workspace{Name: "ai-agent-keep-runtime-space", Slug: "ai-agent-keep-runtime-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	require.NoError(t, db.Create(&workspace).Error)
	require.NoError(t, db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}).Error)
	model := models.AIModelCatalog{Name: "agent-keep-runtime-model", DisplayName: "Agent Keep Runtime Model", Source: "seed", SourceModelID: "agent-keep-runtime-model", ImportedBy: owner.ID}
	require.NoError(t, db.Create(&model).Error)
	runtimeProfile := models.AIRuntimeProfile{WorkspaceID: workspace.ID, Name: "workspace-runtime-keep", ModelID: model.ID, FallbackEnabled: true, Status: models.AIRuntimeProfileStatusActive, CreatedBy: owner.ID}
	require.NoError(t, db.Create(&runtimeProfile).Error)
	runtimeProfileID := runtimeProfile.ID
	agent := models.AIAgent{
		WorkspaceID:      workspace.ID,
		Name:             "keep-runtime-agent",
		RuntimeProfileID: &runtimeProfileID,
		ScopeType:        models.AgentScopeWorkspace,
		SystemPrompt:     "existing-system-prompt",
		ToolsJSON:        `[{"name":"existing_tool"}]`,
		SkillsJSON:       `[{"name":"existing_skill"}]`,
		MemoryJSON:       `{"kind":"existing-memory"}`,
		MCPServersJSON:   `[{"name":"existing-mcp"}]`,
		SubAgentsJSON:    `[{"name":"existing-sub-agent"}]`,
		MetadataJSON:     `{"team":"existing-team"}`,
		Status:           models.AIAgentStatusDraft,
		CreatedBy:        owner.ID,
	}
	require.NoError(t, db.Create(&agent).Error)

	c, w := newAIManagementTestContext(t, http.MethodPut, "/api/ai/agents/"+strconv.FormatUint(agent.ID, 10), owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"name":   "keep-runtime-agent-v2",
		"status": "active",
	})
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(agent.ID, 10)}}
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.UpdateAgent(c)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var stored models.AIAgent
	require.NoError(t, db.First(&stored, agent.ID).Error)
	require.NotNil(t, stored.RuntimeProfileID)
	require.Equal(t, runtimeProfile.ID, *stored.RuntimeProfileID)
	require.Equal(t, "existing-system-prompt", stored.SystemPrompt)
	require.Contains(t, stored.ToolsJSON, "existing_tool")
	require.Contains(t, stored.SkillsJSON, "existing_skill")
	require.Contains(t, stored.MemoryJSON, "existing-memory")
	require.Contains(t, stored.MCPServersJSON, "existing-mcp")
	require.Contains(t, stored.SubAgentsJSON, "existing-sub-agent")
	require.Contains(t, stored.MetadataJSON, "existing-team")
}

func TestUpdateAgentClearingRuntimeProfilePersistsNull(t *testing.T) {
	db := newTestDB(t)
	migrateAgentTables(t, db)
	h := &AIAgentHandler{DB: db}

	owner := models.User{Username: "ai-agent-clear-runtime-owner", Role: "user", Status: "active"}
	require.NoError(t, db.Create(&owner).Error)
	workspace := models.Workspace{Name: "ai-agent-clear-runtime-space", Slug: "ai-agent-clear-runtime-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	require.NoError(t, db.Create(&workspace).Error)
	require.NoError(t, db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}).Error)
	model := models.AIModelCatalog{Name: "agent-clear-runtime-model", DisplayName: "Agent Clear Runtime Model", Source: "seed", SourceModelID: "agent-clear-runtime-model", ImportedBy: owner.ID}
	require.NoError(t, db.Create(&model).Error)
	runtimeProfile := models.AIRuntimeProfile{WorkspaceID: workspace.ID, Name: "workspace-runtime-clear", ModelID: model.ID, FallbackEnabled: true, Status: models.AIRuntimeProfileStatusActive, CreatedBy: owner.ID}
	require.NoError(t, db.Create(&runtimeProfile).Error)
	agent := models.AIAgent{WorkspaceID: workspace.ID, Name: "clearable-agent", RuntimeProfileID: &runtimeProfile.ID, ScopeType: models.AgentScopeWorkspace, Status: models.AIAgentStatusDraft, CreatedBy: owner.ID}
	require.NoError(t, db.Create(&agent).Error)

	c, w := newAIManagementTestContext(t, http.MethodPut, "/api/ai/agents/"+strconv.FormatUint(agent.ID, 10), owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"name":               "clearable-agent-v2",
		"runtime_profile_id": 0,
		"status":             "active",
	})
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(agent.ID, 10)}}
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.UpdateAgent(c)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var stored models.AIAgent
	require.NoError(t, db.First(&stored, agent.ID).Error)
	require.Nil(t, stored.RuntimeProfileID)
	var persisted sql.NullInt64
	require.NoError(t, db.Raw("SELECT runtime_profile_id FROM ai_agents WHERE id = ?", stored.ID).Scan(&persisted).Error)
	require.False(t, persisted.Valid)
}

func TestListAgentsReturnsSingleRuntimeProfileRelation(t *testing.T) {
	db := newTestDB(t)
	migrateAgentTables(t, db)
	h := &AIAgentHandler{DB: db}

	owner := models.User{Username: "ai-agent-list-owner", Role: "user", Status: "active"}
	require.NoError(t, db.Create(&owner).Error)
	viewer := models.User{Username: "ai-agent-list-viewer", Role: "user", Status: "active"}
	require.NoError(t, db.Create(&viewer).Error)
	workspace := models.Workspace{Name: "ai-agent-list-space", Slug: "ai-agent-list-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	require.NoError(t, db.Create(&workspace).Error)
	require.NoError(t, db.Create(&[]models.WorkspaceMember{
		{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID},
		{WorkspaceID: workspace.ID, UserID: viewer.ID, Role: models.WorkspaceRoleViewer, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID},
	}).Error)
	model := models.AIModelCatalog{Name: "agent-list-model", DisplayName: "Agent List Model", Source: "seed", SourceModelID: "agent-list-model", ImportedBy: owner.ID}
	require.NoError(t, db.Create(&model).Error)
	runtimeProfile := models.AIRuntimeProfile{WorkspaceID: workspace.ID, Name: "list-runtime", ModelID: model.ID, FallbackEnabled: true, Status: models.AIRuntimeProfileStatusActive, CreatedBy: owner.ID}
	require.NoError(t, db.Create(&runtimeProfile).Error)
	runtimeProfileID := runtimeProfile.ID
	agent := models.AIAgent{
		WorkspaceID:      workspace.ID,
		Name:             "listed-agent",
		RuntimeProfileID: &runtimeProfileID,
		ScopeType:        models.AgentScopeWorkspace,
		SystemPrompt:     "sensitive-system-prompt",
		ToolsJSON:        `[{"name":"private_tool"}]`,
		Status:           models.AIAgentStatusActive,
		CreatedBy:        owner.ID,
	}
	require.NoError(t, db.Create(&agent).Error)

	c, w := newAIManagementTestContext(t, http.MethodGet, "/api/ai/agents", viewer.ID, viewer.Role, workspace.ID, models.WorkspaceRoleViewer, models.WorkspaceKindNormal, map[string]any{})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.ListAgents(c)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp struct {
		Code int              `json:"code"`
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	runtimeProfileValue, ok := resp.Data[0]["runtime_profile"].(map[string]any)
	require.True(t, ok, "runtime_profile should be present")
	require.Equal(t, float64(runtimeProfile.ID), runtimeProfileValue["id"])
	_, hasCollection := resp.Data[0]["runtime_profiles"]
	require.False(t, hasCollection)
	_, hasSystemPrompt := resp.Data[0]["system_prompt"]
	require.False(t, hasSystemPrompt)
	_, hasTools := resp.Data[0]["tools_json"]
	require.False(t, hasTools)
	_, hasBindingPriority := runtimeProfileValue["binding_priority_json"]
	require.False(t, hasBindingPriority)
	_, hasRuntimeSettings := runtimeProfileValue["runtime_settings_json"]
	require.False(t, hasRuntimeSettings)
}

func TestRuntimeProfileHandlersUseWorkspaceLevelOwnership(t *testing.T) {
	db := newTestDB(t)
	migrateAgentTables(t, db)
	h := &AIAgentHandler{DB: db}

	owner := models.User{Username: "runtime-profile-owner", Role: "user", Status: "active"}
	require.NoError(t, db.Create(&owner).Error)
	workspace := models.Workspace{Name: "runtime-profile-space", Slug: "runtime-profile-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	require.NoError(t, db.Create(&workspace).Error)
	require.NoError(t, db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}).Error)
	model := models.AIModelCatalog{Name: "runtime-profile-model", DisplayName: "Runtime Profile Model", Source: "seed", SourceModelID: "runtime-profile-model", ImportedBy: owner.ID}
	require.NoError(t, db.Create(&model).Error)

	createCtx, createW := newAIManagementTestContext(t, http.MethodPost, "/api/ai/runtime-profiles", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"name":     "workspace-runtime-profile",
		"model_id": model.ID,
		"status":   "active",
	})
	createCtx.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CreateRuntimeProfile(createCtx)

	require.Equal(t, http.StatusOK, createW.Code, createW.Body.String())
	var profile models.AIRuntimeProfile
	require.NoError(t, db.Where("workspace_id = ?", workspace.ID).First(&profile).Error)

	listCtx, listW := newAIManagementTestContext(t, http.MethodGet, "/api/ai/runtime-profiles", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{})
	listCtx.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.ListRuntimeProfiles(listCtx)

	require.Equal(t, http.StatusOK, listW.Code, listW.Body.String())
	var listResp struct {
		Code int              `json:"code"`
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(listW.Body.Bytes(), &listResp))
	require.Len(t, listResp.Data, 1)
	require.Equal(t, float64(profile.ID), listResp.Data[0]["id"])

	updateCtx, updateW := newAIManagementTestContext(t, http.MethodPut, "/api/ai/runtime-profiles/"+strconv.FormatUint(profile.ID, 10), owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"name":     "workspace-runtime-profile-v2",
		"model_id": model.ID,
		"status":   "disabled",
	})
	updateCtx.Params = gin.Params{{Key: "profile_id", Value: strconv.FormatUint(profile.ID, 10)}}
	updateCtx.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.UpdateRuntimeProfile(updateCtx)

	require.Equal(t, http.StatusOK, updateW.Code, updateW.Body.String())
	require.NoError(t, db.First(&profile, profile.ID).Error)
	require.Equal(t, "workspace-runtime-profile-v2", profile.Name)
	require.Equal(t, models.AIRuntimeProfileStatusDisabled, profile.Status)

	deleteCtx, deleteW := newAIManagementTestContext(t, http.MethodDelete, "/api/ai/runtime-profiles/"+strconv.FormatUint(profile.ID, 10), owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{})
	deleteCtx.Params = gin.Params{{Key: "profile_id", Value: strconv.FormatUint(profile.ID, 10)}}
	deleteCtx.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.DeleteRuntimeProfile(deleteCtx)

	require.Equal(t, http.StatusOK, deleteW.Code, deleteW.Body.String())
	var count int64
	require.NoError(t, db.Model(&models.AIRuntimeProfile{}).Where("workspace_id = ?", workspace.ID).Count(&count).Error)
	require.Zero(t, count)
}

func TestListRuntimeProfilesRedactsGovernanceOnlyFieldsForViewer(t *testing.T) {
	db := newTestDB(t)
	migrateAgentTables(t, db)
	h := &AIAgentHandler{DB: db}

	owner := models.User{Username: "runtime-profile-list-owner", Role: "user", Status: "active"}
	require.NoError(t, db.Create(&owner).Error)
	viewer := models.User{Username: "runtime-profile-list-viewer", Role: "user", Status: "active"}
	require.NoError(t, db.Create(&viewer).Error)
	workspace := models.Workspace{Name: "runtime-profile-list-space", Slug: "runtime-profile-list-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	require.NoError(t, db.Create(&workspace).Error)
	require.NoError(t, db.Create(&[]models.WorkspaceMember{
		{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID},
		{WorkspaceID: workspace.ID, UserID: viewer.ID, Role: models.WorkspaceRoleViewer, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID},
	}).Error)
	model := models.AIModelCatalog{Name: "runtime-profile-list-model", DisplayName: "Runtime Profile List Model", Source: "seed", SourceModelID: "runtime-profile-list-model", ImportedBy: owner.ID}
	require.NoError(t, db.Create(&model).Error)
	profile := models.AIRuntimeProfile{
		WorkspaceID:         workspace.ID,
		Name:                "viewer-runtime-profile",
		ModelID:             model.ID,
		BindingPriorityJSON: `[{"binding_id":1,"priority":0}]`,
		RuntimeSettingsJSON: `{"temperature":0.2}`,
		FallbackEnabled:     true,
		Status:              models.AIRuntimeProfileStatusActive,
		CreatedBy:           owner.ID,
	}
	require.NoError(t, db.Create(&profile).Error)

	c, w := newAIManagementTestContext(t, http.MethodGet, "/api/ai/runtime-profiles", viewer.ID, viewer.Role, workspace.ID, models.WorkspaceRoleViewer, models.WorkspaceKindNormal, map[string]any{})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.ListRuntimeProfiles(c)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp struct {
		Code int              `json:"code"`
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	require.Equal(t, float64(profile.ID), resp.Data[0]["id"])
	_, hasBindingPriority := resp.Data[0]["binding_priority_json"]
	require.False(t, hasBindingPriority)
	_, hasRuntimeSettings := resp.Data[0]["runtime_settings_json"]
	require.False(t, hasRuntimeSettings)
}

func TestDeleteRuntimeProfileRejectsAgentReference(t *testing.T) {
	db := newTestDB(t)
	migrateAgentTables(t, db)
	h := &AIAgentHandler{DB: db}

	owner := models.User{Username: "runtime-profile-delete-owner", Role: "user", Status: "active"}
	require.NoError(t, db.Create(&owner).Error)
	workspace := models.Workspace{Name: "runtime-profile-delete-space", Slug: "runtime-profile-delete-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	require.NoError(t, db.Create(&workspace).Error)
	require.NoError(t, db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}).Error)
	model := models.AIModelCatalog{Name: "runtime-profile-delete-model", DisplayName: "Runtime Profile Delete Model", Source: "seed", SourceModelID: "runtime-profile-delete-model", ImportedBy: owner.ID}
	require.NoError(t, db.Create(&model).Error)
	profile := models.AIRuntimeProfile{WorkspaceID: workspace.ID, Name: "runtime-profile-in-use", ModelID: model.ID, Status: models.AIRuntimeProfileStatusActive, CreatedBy: owner.ID}
	require.NoError(t, db.Create(&profile).Error)
	runtimeProfileID := profile.ID
	require.NoError(t, db.Create(&models.AIAgent{WorkspaceID: workspace.ID, Name: "runtime-profile-ref-agent", RuntimeProfileID: &runtimeProfileID, ScopeType: models.AgentScopeWorkspace, Status: models.AIAgentStatusActive, CreatedBy: owner.ID}).Error)

	deleteCtx, deleteW := newAIManagementTestContext(t, http.MethodDelete, "/api/ai/runtime-profiles/"+strconv.FormatUint(profile.ID, 10), owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{})
	deleteCtx.Params = gin.Params{{Key: "profile_id", Value: strconv.FormatUint(profile.ID, 10)}}
	deleteCtx.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.DeleteRuntimeProfile(deleteCtx)

	require.Equal(t, http.StatusBadRequest, deleteW.Code, deleteW.Body.String())
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(deleteW.Body.Bytes(), &resp))
	require.Contains(t, resp.Message, "AI Agent")
	var count int64
	require.NoError(t, db.Model(&models.AIRuntimeProfile{}).Where("id = ? AND workspace_id = ?", profile.ID, workspace.ID).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestDeleteRuntimeProfileRejectsAISessionReference(t *testing.T) {
	db := newTestDB(t)
	migrateAgentTables(t, db)
	h := &AIAgentHandler{DB: db}

	owner := models.User{Username: "runtime-profile-session-delete-owner", Role: "user", Status: "active"}
	require.NoError(t, db.Create(&owner).Error)
	workspace := models.Workspace{Name: "runtime-profile-session-delete-space", Slug: "runtime-profile-session-delete-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	require.NoError(t, db.Create(&workspace).Error)
	require.NoError(t, db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}).Error)
	model := models.AIModelCatalog{Name: "runtime-profile-session-delete-model", DisplayName: "Runtime Profile Session Delete Model", Source: "seed", SourceModelID: "runtime-profile-session-delete-model", ImportedBy: owner.ID}
	require.NoError(t, db.Create(&model).Error)
	profile := models.AIRuntimeProfile{WorkspaceID: workspace.ID, Name: "runtime-profile-session-in-use", ModelID: model.ID, Status: models.AIRuntimeProfileStatusActive, CreatedBy: owner.ID}
	require.NoError(t, db.Create(&profile).Error)
	require.NoError(t, db.Create(&models.AISession{WorkspaceID: workspace.ID, TaskType: "pipeline_agent", Status: models.AISessionStatusCompleted, RuntimeProfileID: profile.ID, CreatedBy: owner.ID}).Error)

	deleteCtx, deleteW := newAIManagementTestContext(t, http.MethodDelete, "/api/ai/runtime-profiles/"+strconv.FormatUint(profile.ID, 10), owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{})
	deleteCtx.Params = gin.Params{{Key: "profile_id", Value: strconv.FormatUint(profile.ID, 10)}}
	deleteCtx.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.DeleteRuntimeProfile(deleteCtx)

	require.Equal(t, http.StatusBadRequest, deleteW.Code, deleteW.Body.String())
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(deleteW.Body.Bytes(), &resp))
	require.Contains(t, resp.Message, "AI Session")
	var count int64
	require.NoError(t, db.Model(&models.AIRuntimeProfile{}).Where("id = ? AND workspace_id = ?", profile.ID, workspace.ID).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestDeleteRuntimeProfileRejectsPipelineDefinitionReference(t *testing.T) {
	db := newTestDB(t)
	migrateAgentTables(t, db)
	h := &AIAgentHandler{DB: db}

	owner := models.User{Username: "runtime-profile-pipeline-delete-owner", Role: "user", Status: "active"}
	require.NoError(t, db.Create(&owner).Error)
	workspace := models.Workspace{Name: "runtime-profile-pipeline-delete-space", Slug: "runtime-profile-pipeline-delete-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	require.NoError(t, db.Create(&workspace).Error)
	require.NoError(t, db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}).Error)
	model := models.AIModelCatalog{Name: "runtime-profile-pipeline-delete-model", DisplayName: "Runtime Profile Pipeline Delete Model", Source: "seed", SourceModelID: "runtime-profile-pipeline-delete-model", ImportedBy: owner.ID}
	require.NoError(t, db.Create(&model).Error)
	profile := models.AIRuntimeProfile{WorkspaceID: workspace.ID, Name: "runtime-profile-pipeline-in-use", ModelID: model.ID, Status: models.AIRuntimeProfileStatusActive, CreatedBy: owner.ID}
	require.NoError(t, db.Create(&profile).Error)
	definition := `{"version":"2.0","nodes":[{"id":"mr-review","type":"mr_quality_check","name":"MR Review","config":{"runtime_profile_id":` + strconv.FormatUint(profile.ID, 10) + `,"input_text":"review this MR"}}],"edges":[]}`
	require.NoError(t, db.Create(&models.Pipeline{WorkspaceID: workspace.ID, OwnerID: owner.ID, Name: "runtime-profile-pipeline-ref", Definition: definition}).Error)

	deleteCtx, deleteW := newAIManagementTestContext(t, http.MethodDelete, "/api/ai/runtime-profiles/"+strconv.FormatUint(profile.ID, 10), owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{})
	deleteCtx.Params = gin.Params{{Key: "profile_id", Value: strconv.FormatUint(profile.ID, 10)}}
	deleteCtx.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.DeleteRuntimeProfile(deleteCtx)

	require.Equal(t, http.StatusBadRequest, deleteW.Code, deleteW.Body.String())
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(deleteW.Body.Bytes(), &resp))
	require.Contains(t, resp.Message, "流水线定义")
	var count int64
	require.NoError(t, db.Model(&models.AIRuntimeProfile{}).Where("id = ? AND workspace_id = ?", profile.ID, workspace.ID).Count(&count).Error)
	require.Equal(t, int64(1), count)
}
