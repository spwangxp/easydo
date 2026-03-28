package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
)

func TestStoreTemplateHandler_AppCatalogAggregatesCategoryAndInfra(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "app-catalog-maintainer", models.WorkspaceRoleMaintainer)
	viewer := seedResourceStoreMember(t, db, workspace.ID, "app-catalog-viewer", models.WorkspaceRoleViewer)

	template := models.StoreTemplate{
		WorkspaceID:        workspace.ID,
		Name:               "Redis",
		Description:        string(mustJSON(t, map[string]interface{}{"body": "Redis cache service", "category": "cache"})),
		TemplateType:       models.StoreTemplateTypeApp,
		TargetResourceType: models.ResourceTypeVM,
		Source:             models.StoreTemplateSourceWorkspace,
		Status:             models.StoreTemplateStatusPublished,
		Summary:            "In-memory cache",
		CreatedBy:          maintainer.ID,
	}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}

	vmPipeline := models.Pipeline{Name: "redis-vm", WorkspaceID: workspace.ID, OwnerID: maintainer.ID, Config: minimalDockerRunPipelineConfig(t)}
	if err := db.Create(&vmPipeline).Error; err != nil {
		t.Fatalf("create vm pipeline failed: %v", err)
	}
	k8sPipeline := models.Pipeline{Name: "redis-k8s", WorkspaceID: workspace.ID, OwnerID: maintainer.ID, Config: minimalKubernetesPipelineConfig(t)}
	if err := db.Create(&k8sPipeline).Error; err != nil {
		t.Fatalf("create k8s pipeline failed: %v", err)
	}

	vmVariant := models.StoreTemplateVersion{
		WorkspaceID:       workspace.ID,
		TemplateID:        template.ID,
		PipelineID:        vmPipeline.ID,
		Version:           "7.2.0",
		DeploymentMode:    "pipeline",
		DefaultConfig:     string(mustJSON(t, buildAppVariantMetadata(models.ResourceTypeVM, "VM stable", map[string]interface{}{"command_template": "docker run redis:{{image_tag}}"}))),
		DependencyConfig:  "{}",
		TargetConstraints: "{}",
		Status:            models.StoreTemplateStatusPublished,
		CreatedBy:         maintainer.ID,
	}
	if err := db.Create(&vmVariant).Error; err != nil {
		t.Fatalf("create vm variant failed: %v", err)
	}

	k8sVariant := models.StoreTemplateVersion{
		WorkspaceID:       workspace.ID,
		TemplateID:        template.ID,
		PipelineID:        k8sPipeline.ID,
		Version:           "7.2.0-k8s",
		DeploymentMode:    "pipeline",
		DefaultConfig:     string(mustJSON(t, buildAppVariantMetadata(models.ResourceTypeK8sCluster, "K8s stable", map[string]interface{}{"chart_source_type": "repo", "chart_name": "redis"}))),
		DependencyConfig:  "{}",
		TargetConstraints: "{}",
		Status:            models.StoreTemplateStatusPublished,
		CreatedBy:         maintainer.ID,
	}
	if err := db.Create(&k8sVariant).Error; err != nil {
		t.Fatalf("create k8s variant failed: %v", err)
	}

	h := NewStoreTemplateHandler()
	resp := performResourceStoreRequest(t, h.ListAppCatalog, viewer.ID, "user", workspace.ID, models.WorkspaceRoleViewer, http.MethodGet, "/api/store/apps", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected list app catalog success, got=%d body=%s", resp.Code, resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"category":"cache"`)) {
		t.Fatalf("expected category in app catalog response, got=%s", resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"supported_resource_types":["vm","k8s"]`)) {
		t.Fatalf("expected aggregated infra in app catalog response, got=%s", resp.Body.String())
	}
}

func TestStoreTemplateHandler_CreateAppVariantCreatesHiddenPipeline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "app-create-variant-maintainer", models.WorkspaceRoleMaintainer)
	template := models.StoreTemplate{
		WorkspaceID:        workspace.ID,
		Name:               "Redis",
		Description:        "Redis cache service",
		TemplateType:       models.StoreTemplateTypeApp,
		TargetResourceType: models.ResourceTypeVM,
		Source:             models.StoreTemplateSourceWorkspace,
		Status:             models.StoreTemplateStatusPublished,
		Summary:            "In-memory cache",
		CreatedBy:          maintainer.ID,
	}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}

	h := NewStoreTemplateHandler()
	body := mustJSON(t, map[string]interface{}{
		"version":             "7.2.4",
		"status":              "published",
		"pipeline_id":         0,
		"infra_type":          "vm",
		"version_description": "Redis VM release",
		"command_template":    "docker run -d --name {{container_name}} redis:{{image_tag}}",
		"parameters": []map[string]interface{}{
			{
				"name":          "container_name",
				"label":         "Container Name",
				"type":          "text",
				"default_value": "redis-main",
				"sort_order":    1,
			},
		},
	})

	resp := performResourceStoreRequest(t, h.CreateTemplateVersion, maintainer.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, http.MethodPost, "/api/store/templates/1/versions", body, pathResourceStoreID(template.ID))
	if resp.Code != http.StatusOK {
		t.Fatalf("expected create app variant success, got=%d body=%s", resp.Code, resp.Body.String())
	}

	created := decodeResponseData[templateVersionResponse](t, resp.Body.Bytes())
	if created.PipelineID == 0 {
		t.Fatalf("expected app variant to allocate hidden pipeline, got pipeline_id=0 body=%s", resp.Body.String())
	}

	var pipeline models.Pipeline
	if err := db.First(&pipeline, created.PipelineID).Error; err != nil {
		t.Fatalf("load created hidden pipeline failed: %v", err)
	}
	if !pipeline.ManagementHidden {
		t.Fatalf("expected hidden pipeline to be management hidden")
	}
	if pipeline.WorkspaceID != workspace.ID {
		t.Fatalf("expected hidden pipeline workspace=%d, got=%d", workspace.ID, pipeline.WorkspaceID)
	}
}

func TestStoreTemplateHandler_DeleteAppTemplateRemovesVariantsAndHiddenPipelines(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "app-delete-template-maintainer", models.WorkspaceRoleMaintainer)
	template := models.StoreTemplate{
		WorkspaceID:        workspace.ID,
		Name:               "Redis",
		Description:        "Redis cache service",
		TemplateType:       models.StoreTemplateTypeApp,
		TargetResourceType: models.ResourceTypeVM,
		Source:             models.StoreTemplateSourceWorkspace,
		Status:             models.StoreTemplateStatusPublished,
		Summary:            "In-memory cache",
		CreatedBy:          maintainer.ID,
	}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}

	hiddenPipeline := models.Pipeline{
		Name:             "[app-store] Redis 7.2.4 VM",
		Description:      "hidden pipeline",
		Config:           minimalDockerRunPipelineConfig(t),
		WorkspaceID:      workspace.ID,
		OwnerID:          maintainer.ID,
		Environment:      "development",
		ManagementHidden: true,
	}
	if err := db.Create(&hiddenPipeline).Error; err != nil {
		t.Fatalf("create hidden pipeline failed: %v", err)
	}

	version := models.StoreTemplateVersion{
		WorkspaceID:       workspace.ID,
		TemplateID:        template.ID,
		PipelineID:        hiddenPipeline.ID,
		Version:           "7.2.4",
		DeploymentMode:    "vm_command",
		DefaultConfig:     string(mustJSON(t, buildAppVariantMetadata(models.ResourceTypeVM, "Redis VM release", map[string]interface{}{"command_template": "docker run redis"}))),
		DependencyConfig:  "{}",
		TargetConstraints: "{}",
		Status:            models.StoreTemplateStatusPublished,
		CreatedBy:         maintainer.ID,
	}
	if err := db.Create(&version).Error; err != nil {
		t.Fatalf("create version failed: %v", err)
	}

	parameter := models.TemplateParameter{
		TemplateVersionID: version.ID,
		Name:              "container_name",
		Label:             "Container Name",
		Type:              "text",
		DefaultValue:      "redis-main",
		SortOrder:         1,
	}
	if err := db.Create(&parameter).Error; err != nil {
		t.Fatalf("create parameter failed: %v", err)
	}

	h := NewStoreTemplateHandler()
	resp := performResourceStoreRequest(t, h.DeleteTemplate, maintainer.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, http.MethodDelete, "/api/store/templates/1", nil, pathResourceStoreID(template.ID))
	if resp.Code != http.StatusOK {
		t.Fatalf("expected delete app template success, got=%d body=%s", resp.Code, resp.Body.String())
	}

	if err := db.First(&models.StoreTemplate{}, template.ID).Error; err == nil {
		t.Fatalf("expected template to be deleted")
	}
	if err := db.First(&models.StoreTemplateVersion{}, version.ID).Error; err == nil {
		t.Fatalf("expected template version to be deleted")
	}
	if err := db.First(&models.TemplateParameter{}, parameter.ID).Error; err == nil {
		t.Fatalf("expected template parameter to be deleted")
	}
	if err := db.First(&models.Pipeline{}, hiddenPipeline.ID).Error; err == nil {
		t.Fatalf("expected hidden pipeline to be deleted")
	}
}

func TestStoreTemplateHandler_AppVariantPreviewRendersVMCommand(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "app-vm-preview-maintainer", models.WorkspaceRoleMaintainer)
	developer := seedResourceStoreMember(t, db, workspace.ID, "app-vm-preview-developer", models.WorkspaceRoleDeveloper)

	pipeline := models.Pipeline{Name: "vm-preview-pipeline", WorkspaceID: workspace.ID, OwnerID: maintainer.ID, Config: minimalDockerRunPipelineConfig(t)}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	resource := models.Resource{
		WorkspaceID: workspace.ID,
		Name:        "vm-redis-prod",
		Type:        models.ResourceTypeVM,
		Environment: "production",
		Status:      models.ResourceStatusOnline,
		Endpoint:    "10.0.0.21:22",
		CreatedBy:   maintainer.ID,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	template := models.StoreTemplate{
		WorkspaceID:        workspace.ID,
		Name:               "Redis",
		Description:        string(mustJSON(t, map[string]interface{}{"body": "Redis cache service", "category": "cache"})),
		TemplateType:       models.StoreTemplateTypeApp,
		TargetResourceType: models.ResourceTypeVM,
		Source:             models.StoreTemplateSourceWorkspace,
		Status:             models.StoreTemplateStatusPublished,
		Summary:            "In-memory cache",
		CreatedBy:          maintainer.ID,
	}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}

	version := models.StoreTemplateVersion{
		WorkspaceID:       workspace.ID,
		TemplateID:        template.ID,
		PipelineID:        pipeline.ID,
		Version:           "7.2.4",
		DeploymentMode:    "pipeline",
		DefaultConfig:     string(mustJSON(t, buildAppVariantMetadata(models.ResourceTypeVM, "Redis VM release", map[string]interface{}{"command_template": "docker run -d --name {{container_name}} -p {{vm_port}}:{{redis_port}} redis:{{image_tag}}"}))),
		DependencyConfig:  "{}",
		TargetConstraints: "{}",
		Status:            models.StoreTemplateStatusPublished,
		CreatedBy:         maintainer.ID,
	}
	if err := db.Create(&version).Error; err != nil {
		t.Fatalf("create version failed: %v", err)
	}
	parameters := []models.TemplateParameter{
		{TemplateVersionID: version.ID, Name: "container_name", Label: "Container Name", Type: "text", DefaultValue: "redis", Required: true, SortOrder: 1},
		{TemplateVersionID: version.ID, Name: "vm_port", Label: "Host Port", Type: "number", DefaultValue: "16379", Required: true, SortOrder: 2},
		{TemplateVersionID: version.ID, Name: "redis_port", Label: "Redis Port", Type: "number", DefaultValue: "6379", Required: true, SortOrder: 3},
		{TemplateVersionID: version.ID, Name: "image_tag", Label: "Image Tag", Type: "text", DefaultValue: "7.2.4", Required: true, SortOrder: 4},
	}
	if err := db.Create(&parameters).Error; err != nil {
		t.Fatalf("create parameters failed: %v", err)
	}

	h := NewStoreTemplateHandler()
	body := mustJSON(t, map[string]interface{}{
		"target_resource_id": resource.ID,
		"parameters": map[string]interface{}{
			"container_name": "redis-main",
			"vm_port":        "16379",
			"redis_port":     "6379",
			"image_tag":      "7.2.4",
		},
	})
	resp := performResourceStoreRequest(t, h.PreviewAppVariant, developer.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, http.MethodPost, "/api/store/apps/1/variants/1/preview", body, pathStoreTemplateVariant(template.ID, version.ID))
	if resp.Code != http.StatusOK {
		t.Fatalf("expected vm preview success, got=%d body=%s", resp.Code, resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`docker run -d --name redis-main -p 16379:6379 redis:7.2.4`)) {
		t.Fatalf("expected rendered vm command in preview response, got=%s", resp.Body.String())
	}
}

func TestStoreTemplateHandler_AppVariantPreviewBuildsK8sValuesDiff(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "app-k8s-preview-maintainer", models.WorkspaceRoleMaintainer)
	developer := seedResourceStoreMember(t, db, workspace.ID, "app-k8s-preview-developer", models.WorkspaceRoleDeveloper)

	pipeline := models.Pipeline{Name: "k8s-preview-pipeline", WorkspaceID: workspace.ID, OwnerID: maintainer.ID, Config: minimalKubernetesPipelineConfig(t)}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	resource := models.Resource{
		WorkspaceID: workspace.ID,
		Name:        "prod-k8s",
		Type:        models.ResourceTypeK8sCluster,
		Environment: "production",
		Status:      models.ResourceStatusOnline,
		Endpoint:    "https://k8s.example.com",
		CreatedBy:   maintainer.ID,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	template := models.StoreTemplate{
		WorkspaceID:        workspace.ID,
		Name:               "Redis",
		Description:        string(mustJSON(t, map[string]interface{}{"body": "Redis cache service", "category": "cache"})),
		TemplateType:       models.StoreTemplateTypeApp,
		TargetResourceType: models.ResourceTypeK8sCluster,
		Source:             models.StoreTemplateSourceWorkspace,
		Status:             models.StoreTemplateStatusPublished,
		Summary:            "In-memory cache",
		CreatedBy:          maintainer.ID,
	}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}

	version := models.StoreTemplateVersion{
		WorkspaceID:    workspace.ID,
		TemplateID:     template.ID,
		PipelineID:     pipeline.ID,
		Version:        "19.1.0",
		DeploymentMode: "pipeline",
		DefaultConfig: string(mustJSON(t, buildAppVariantMetadata(models.ResourceTypeK8sCluster, "Redis chart", map[string]interface{}{
			"chart_source_type": "repo",
			"chart_repo_url":    "https://charts.bitnami.com/bitnami",
			"chart_name":        "redis",
			"chart_version":     "19.1.0",
			"base_values_yaml":  "auth:\n  enabled: true\n  password: old-secret\nservice:\n  type: ClusterIP\n",
		}))),
		DependencyConfig:  "{}",
		TargetConstraints: "{}",
		Status:            models.StoreTemplateStatusPublished,
		CreatedBy:         maintainer.ID,
	}
	if err := db.Create(&version).Error; err != nil {
		t.Fatalf("create version failed: %v", err)
	}
	parameters := []models.TemplateParameter{
		{TemplateVersionID: version.ID, Name: "auth.password", Label: "Password", Type: "text", DefaultValue: "old-secret", Required: true, SortOrder: 1},
		{TemplateVersionID: version.ID, Name: "service.type", Label: "Service Type", Type: "select", DefaultValue: "ClusterIP", OptionValues: `["ClusterIP","LoadBalancer"]`, Required: true, SortOrder: 2},
	}
	if err := db.Create(&parameters).Error; err != nil {
		t.Fatalf("create parameters failed: %v", err)
	}

	h := NewStoreTemplateHandler()
	body := mustJSON(t, map[string]interface{}{
		"target_resource_id": resource.ID,
		"parameters": map[string]interface{}{
			"auth.password": "new-secret",
			"service.type":  "LoadBalancer",
		},
	})
	resp := performResourceStoreRequest(t, h.PreviewAppVariant, developer.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, http.MethodPost, "/api/store/apps/1/variants/1/preview", body, pathStoreTemplateVariant(template.ID, version.ID))
	if resp.Code != http.StatusOK {
		t.Fatalf("expected k8s preview success, got=%d body=%s", resp.Code, resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"override_values_yaml":"auth:`)) {
		t.Fatalf("expected override values yaml in preview response, got=%s", resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`new-secret`)) || !bytes.Contains(resp.Body.Bytes(), []byte(`LoadBalancer`)) {
		t.Fatalf("expected rendered override values in preview response, got=%s", resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`helm --kubeconfig`)) {
		t.Fatalf("expected helm command preview in response, got=%s", resp.Body.String())
	}
}

func buildAppVariantMetadata(resourceType models.ResourceType, releaseNotes string, detail map[string]interface{}) map[string]interface{} {
	metadata := map[string]interface{}{
		"schema_version": 1,
		"resource_type":  resourceType,
		"release_notes":  releaseNotes,
	}
	switch resourceType {
	case models.ResourceTypeVM:
		metadata["vm"] = detail
	case models.ResourceTypeK8sCluster:
		metadata["k8s"] = map[string]interface{}{
			"chart_source": map[string]interface{}{
				"type":          detail["chart_source_type"],
				"repo_url":      detail["chart_repo_url"],
				"oci_url":       detail["chart_oci_url"],
				"chart_name":    detail["chart_name"],
				"chart_version": detail["chart_version"],
				"object_key":    detail["chart_object_key"],
				"file_name":     detail["chart_file_name"],
			},
			"base_values_yaml": detail["base_values_yaml"],
		}
	}
	return metadata
}

func minimalDockerRunPipelineConfig(t *testing.T) string {
	t.Helper()
	config := PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{{
			ID:   "deploy",
			Type: "docker-run",
			Name: "Docker Deploy",
			Config: map[string]interface{}{
				"host":           "${inputs.resource_host}",
				"port":           "${inputs.resource_port}",
				"user":           "root",
				"image_name":     "busybox",
				"image_tag":      "latest",
				"container_name": "noop",
				"run_args":       "echo noop",
			},
		}},
		Edges: []PipelineEdge{},
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal docker run pipeline config failed: %v", err)
	}
	return string(raw)
}

func pathStoreTemplateVariant(templateID, variantID uint64) func(*gin.Context) {
	return func(c *gin.Context) {
		c.Params = gin.Params{
			{Key: "id", Value: convertToString(templateID)},
			{Key: "version_id", Value: convertToString(variantID)},
		}
	}
}
