package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"easydo-server/internal/config"
	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestResourceHandler_CreateListAndPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "resource-maintainer", models.WorkspaceRoleMaintainer)
	viewer := seedResourceStoreMember(t, db, workspace.ID, "resource-viewer", models.WorkspaceRoleViewer)
	developer := seedResourceStoreMember(t, db, workspace.ID, "resource-developer", models.WorkspaceRoleDeveloper)

	h := NewResourceHandler()
	credential := seedResourceVerificationCredential(t, db, workspace.ID, maintainer.ID, models.TypePassword, map[string]interface{}{
		"username": "root",
		"password": "secret123",
	})
	validationTask := seedSuccessfulResourceValidationTask(t, db, workspace.ID, maintainer.ID, models.ResourceTypeVM, "10.0.0.8:22", credential)
	body := mustJSON(t, map[string]interface{}{
		"name":                 "prod-vm-01",
		"description":          "production vm",
		"type":                 string(models.ResourceTypeVM),
		"environment":          "production",
		"endpoint":             "10.0.0.8:22",
		"credential_id":        credential.ID,
		"verification_task_id": validationTask.ID,
	})

	forbidden := performResourceStoreRequest(t, h.CreateResource, developer.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, http.MethodPost, "/api/resources", body)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("expected developer create resource forbidden, got=%d body=%s", forbidden.Code, forbidden.Body.String())
	}

	create := performResourceStoreRequest(t, h.CreateResource, maintainer.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, http.MethodPost, "/api/resources", body)
	if create.Code != http.StatusOK {
		t.Fatalf("expected maintainer create resource success, got=%d body=%s", create.Code, create.Body.String())
	}
	resourceID := responseDataID(t, create.Body.Bytes())

	list := performResourceStoreRequest(t, h.ListResources, viewer.ID, "user", workspace.ID, models.WorkspaceRoleViewer, http.MethodGet, "/api/resources", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("expected viewer list resources success, got=%d body=%s", list.Code, list.Body.String())
	}
	if !bytes.Contains(list.Body.Bytes(), []byte("prod-vm-01")) {
		t.Fatalf("expected resource in list response, got=%s", list.Body.String())
	}

	var stored models.Resource
	if err := db.First(&stored, resourceID).Error; err != nil {
		t.Fatalf("load resource failed: %v", err)
	}
	if stored.Status != models.ResourceStatusOnline {
		t.Fatalf("expected verified resource status online, got=%s", stored.Status)
	}
	if stored.ProjectID != nil {
		t.Fatalf("expected resource project_id to be nil when omitted, got=%d", *stored.ProjectID)
	}
	var binding models.ResourceCredentialBinding
	if err := db.Where("resource_id = ? AND credential_id = ?", resourceID, credential.ID).First(&binding).Error; err != nil {
		t.Fatalf("expected primary credential binding to be created, got err=%v", err)
	}
}

func TestResourceHandler_VerifyResourceConnectionCreatesAgentTask(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "resource-verify-maintainer", models.WorkspaceRoleMaintainer)
	agent := seedApprovedResourceAgent(t, db, workspace.ID)
	credential := seedResourceVerificationCredential(t, db, workspace.ID, maintainer.ID, models.TypePassword, map[string]interface{}{
		"username": "root",
		"password": "secret123",
	})

	h := NewResourceHandler()
	body := mustJSON(t, map[string]interface{}{
		"type":          string(models.ResourceTypeVM),
		"endpoint":      "10.0.0.9:22",
		"credential_id": credential.ID,
	})

	resp := performResourceStoreRequest(t, h.VerifyResourceConnection, maintainer.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, http.MethodPost, "/api/resources/verify", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected verify resource connection success, got=%d body=%s", resp.Code, resp.Body.String())
	}

	taskData := decodeResponseData[map[string]interface{}](t, resp.Body.Bytes())
	taskID, ok := parseCredentialID(taskData["task_id"])
	if !ok || taskID == 0 {
		t.Fatalf("expected task_id in verify response, got=%v body=%s", taskData["task_id"], resp.Body.String())
	}

	var task models.AgentTask
	if err := db.First(&task, taskID).Error; err != nil {
		t.Fatalf("load validation task failed: %v", err)
	}
	if task.AgentID != agent.ID {
		t.Fatalf("expected validation task to use selected agent %d, got %d", agent.ID, task.AgentID)
	}
	if task.TaskType != "ssh" {
		t.Fatalf("expected validation task type ssh, got %s", task.TaskType)
	}
	if !bytes.Contains([]byte(task.Script), []byte("ssh")) {
		t.Fatalf("expected validation script to execute ssh, got=%s", task.Script)
	}
	if !bytes.Contains([]byte(task.Params), []byte("resource_connection_validation")) {
		t.Fatalf("expected validation params to include verification metadata, got=%s", task.Params)
	}
}

func TestResourceHandler_CreateResourceRequiresSuccessfulValidationTask(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "resource-create-verify", models.WorkspaceRoleMaintainer)
	credential := seedResourceVerificationCredential(t, db, workspace.ID, maintainer.ID, models.TypePassword, map[string]interface{}{
		"username": "root",
		"password": "secret123",
	})
	validationTask := seedSuccessfulResourceValidationTask(t, db, workspace.ID, maintainer.ID, models.ResourceTypeVM, "10.0.0.12:22", credential)

	h := NewResourceHandler()
	missingVerifyBody := mustJSON(t, map[string]interface{}{
		"name":          "prod-vm-no-verify",
		"type":          string(models.ResourceTypeVM),
		"environment":   "production",
		"endpoint":      "10.0.0.12:22",
		"credential_id": credential.ID,
	})
	missingVerifyResp := performResourceStoreRequest(t, h.CreateResource, maintainer.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, http.MethodPost, "/api/resources", missingVerifyBody)
	if missingVerifyResp.Code != http.StatusBadRequest {
		t.Fatalf("expected create without validation to fail, got=%d body=%s", missingVerifyResp.Code, missingVerifyResp.Body.String())
	}

	createBody := mustJSON(t, map[string]interface{}{
		"name":                 "prod-vm-verified",
		"type":                 string(models.ResourceTypeVM),
		"environment":          "production",
		"endpoint":             "10.0.0.12:22",
		"credential_id":        credential.ID,
		"verification_task_id": validationTask.ID,
	})
	createResp := performResourceStoreRequest(t, h.CreateResource, maintainer.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, http.MethodPost, "/api/resources", createBody)
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected create with successful validation to pass, got=%d body=%s", createResp.Code, createResp.Body.String())
	}
	resourceID := responseDataID(t, createResp.Body.Bytes())

	var stored models.Resource
	if err := db.First(&stored, resourceID).Error; err != nil {
		t.Fatalf("load verified resource failed: %v", err)
	}
	if stored.Status != models.ResourceStatusOnline {
		t.Fatalf("expected verified resource status online, got=%s", stored.Status)
	}
	if stored.LastCheckAt == 0 {
		t.Fatalf("expected verified resource last_check_at to be set")
	}
	if !bytes.Contains([]byte(stored.LastCheckResult), []byte("验证通过")) {
		t.Fatalf("expected verified resource last_check_result to mention success, got=%s", stored.LastCheckResult)
	}

	var binding models.ResourceCredentialBinding
	if err := db.Where("resource_id = ? AND credential_id = ?", resourceID, credential.ID).First(&binding).Error; err != nil {
		t.Fatalf("expected resource binding after verified create, err=%v", err)
	}

	replayBody := mustJSON(t, map[string]interface{}{
		"name":                 "prod-vm-replay",
		"type":                 string(models.ResourceTypeVM),
		"environment":          "production",
		"endpoint":             "10.0.0.12:22",
		"credential_id":        credential.ID,
		"verification_task_id": validationTask.ID,
	})
	replayResp := performResourceStoreRequest(t, h.CreateResource, maintainer.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, http.MethodPost, "/api/resources", replayBody)
	if replayResp.Code != http.StatusBadRequest {
		t.Fatalf("expected replaying consumed verification to fail, got=%d body=%s", replayResp.Code, replayResp.Body.String())
	}
}

func TestResourceHandler_GetResourceReturnsBaseInfoContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	viewer, workspace := seedResourceStoreUserAndWorkspace(t, db, "resource-base-info-viewer", models.WorkspaceRoleViewer)
	baseInfo := `{"schemaVersion":1,"status":"success","source":"remote_task","collectedAt":1710000000,"machine":{"cpu":{"logicalCores":8},"memory":{"totalBytes":34359738368},"storage":{"totalDiskBytes":536870912000},"gpu":{"count":1}}}`
	resource := models.Resource{
		WorkspaceID:         workspace.ID,
		Name:                "inventory-vm",
		Type:                models.ResourceTypeVM,
		Environment:         "production",
		Status:              models.ResourceStatusOnline,
		Endpoint:            "10.0.0.88:22",
		CreatedBy:           viewer.ID,
		BaseInfo:            baseInfo,
		BaseInfoStatus:      "success",
		BaseInfoCollectedAt: 1710000000,
		BaseInfoSource:      "remote_task",
		BaseInfoLastError:   "",
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	h := NewResourceHandler()
	resp := performResourceStoreRequest(t, h.GetResource, viewer.ID, "user", workspace.ID, models.WorkspaceRoleViewer, http.MethodGet, "/api/resources/1", nil, pathResourceStoreID(resource.ID))
	if resp.Code != http.StatusOK {
		t.Fatalf("expected get resource success, got=%d body=%s", resp.Code, resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"base_info"`)) {
		t.Fatalf("expected base_info in resource response, got=%s", resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"base_info_status":"success"`)) {
		t.Fatalf("expected base_info_status in resource response, got=%s", resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"logicalCores":8`)) {
		t.Fatalf("expected serialized base_info payload in response, got=%s", resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"base_info_source":"remote_task"`)) {
		t.Fatalf("expected base_info_source in resource response, got=%s", resp.Body.String())
	}
}

func TestResourceHandler_RefreshBaseInfoCreatesCollectionTask(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "resource-refresh-maintainer", models.WorkspaceRoleMaintainer)
	agent := seedApprovedResourceAgent(t, db, workspace.ID)
	credential := seedResourceVerificationCredential(t, db, workspace.ID, maintainer.ID, models.TypePassword, map[string]interface{}{
		"username": "root",
		"password": "secret123",
	})
	resource := models.Resource{
		WorkspaceID: workspace.ID,
		Name:        "refreshable-vm",
		Type:        models.ResourceTypeVM,
		Environment: "production",
		Status:      models.ResourceStatusOnline,
		Endpoint:    "10.0.0.31:22",
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
		t.Fatalf("create binding failed: %v", err)
	}

	h := NewResourceHandler()
	resp := performResourceStoreRequest(t, h.RefreshResourceBaseInfo, maintainer.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, http.MethodPost, "/api/resources/1/base-info/refresh", nil, pathResourceStoreID(resource.ID))
	if resp.Code != http.StatusOK {
		t.Fatalf("expected refresh base info success, got=%d body=%s", resp.Code, resp.Body.String())
	}

	taskData := decodeResponseData[map[string]interface{}](t, resp.Body.Bytes())
	taskID, ok := parseCredentialID(taskData["task_id"])
	if !ok || taskID == 0 {
		t.Fatalf("expected task_id in refresh response, got=%v body=%s", taskData["task_id"], resp.Body.String())
	}

	var task models.AgentTask
	if err := db.First(&task, taskID).Error; err != nil {
		t.Fatalf("load refresh task failed: %v", err)
	}
	if task.AgentID != agent.ID {
		t.Fatalf("expected refresh task to use selected agent %d, got %d", agent.ID, task.AgentID)
	}
	if !bytes.Contains([]byte(task.Params), []byte("resource_base_info_refresh")) {
		t.Fatalf("expected refresh params to include resource_base_info_refresh kind, got=%s", task.Params)
	}
	if !bytes.Contains([]byte(task.Script), []byte("EASYDO_BASE_INFO_BEGIN")) {
		t.Fatalf("expected refresh script to emit base info markers, got=%s", task.Script)
	}
}

func TestParseVMBaseInfoOutput_ParsesScientificNotationTotals(t *testing.T) {
	stdout := "EASYDO_BASE_INFO_BEGIN\n" +
		"EASYDO_HOSTNAME=ubuntu\n" +
		"EASYDO_PRIMARY_IPV4=10.0.0.8\n" +
		"EASYDO_OS_NAME=Ubuntu 22.04.4 LTS\n" +
		"EASYDO_OS_VERSION=22.04\n" +
		"EASYDO_KERNEL_VERSION=6.5.0-18-generic\n" +
		"EASYDO_ARCH=x86_64\n" +
		"EASYDO_CPU_MODEL=Intel(R) Core(TM) i7\n" +
		"EASYDO_CPU_LOGICAL_CORES=12\n" +
		"EASYDO_MEMORY_TOTAL_BYTES=6.5536e+10\n" +
		"EASYDO_ROOT_TOTAL_BYTES=485687422976\n" +
		"EASYDO_TOTAL_DISK_BYTES=2.51251e+12\n" +
		"EASYDO_GPU_COUNT=0\n" +
		"EASYDO_DISK_ROWS_BEGIN\n" +
		"NAME=\"nvme0n1p3\" SIZE=\"494598954496\" TYPE=\"part\" FSTYPE=\"ext4\" MOUNTPOINT=\"/\"\n" +
		"EASYDO_DISK_ROWS_END\n" +
		"EASYDO_GPU_CSV_BEGIN\n" +
		"EASYDO_GPU_CSV_END\n" +
		"EASYDO_BASE_INFO_END\n"

	baseInfoJSON, _, _, err := parseVMBaseInfoOutput(stdout, "remote_task")
	if err != nil {
		t.Fatalf("parseVMBaseInfoOutput returned error: %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(baseInfoJSON), &payload); err != nil {
		t.Fatalf("unmarshal base info failed: %v", err)
	}
	machine, _ := payload["machine"].(map[string]interface{})
	memory, _ := machine["memory"].(map[string]interface{})
	storage, _ := machine["storage"].(map[string]interface{})
	if memory["totalBytes"].(float64) == 0 {
		t.Fatalf("expected memory total parsed from scientific notation, got payload=%s", baseInfoJSON)
	}
	if storage["totalDiskBytes"].(float64) == 0 {
		t.Fatalf("expected total disk parsed from scientific notation, got payload=%s", baseInfoJSON)
	}
}

func TestStoreTemplateHandler_CreateListAndPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "template-maintainer", models.WorkspaceRoleMaintainer)
	viewer := seedResourceStoreMember(t, db, workspace.ID, "template-viewer", models.WorkspaceRoleViewer)
	developer := seedResourceStoreMember(t, db, workspace.ID, "template-developer", models.WorkspaceRoleDeveloper)

	h := NewStoreTemplateHandler()
	body := mustJSON(t, map[string]interface{}{
		"name":                 "nginx-vm-template",
		"description":          "nginx on docker",
		"template_type":        string(models.StoreTemplateTypeApp),
		"target_resource_type": string(models.ResourceTypeVM),
		"source":               string(models.StoreTemplateSourceWorkspace),
		"summary":              "deploy nginx to docker on vm",
		"category":             "web-service",
	})

	forbidden := performResourceStoreRequest(t, h.CreateTemplate, developer.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, http.MethodPost, "/api/store/templates", body)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("expected developer create template forbidden, got=%d body=%s", forbidden.Code, forbidden.Body.String())
	}

	create := performResourceStoreRequest(t, h.CreateTemplate, maintainer.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, http.MethodPost, "/api/store/templates", body)
	if create.Code != http.StatusOK {
		t.Fatalf("expected maintainer create template success, got=%d body=%s", create.Code, create.Body.String())
	}

	list := performResourceStoreRequest(t, h.ListTemplates, viewer.ID, "user", workspace.ID, models.WorkspaceRoleViewer, http.MethodGet, "/api/store/templates", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("expected viewer list templates success, got=%d body=%s", list.Code, list.Body.String())
	}
	if !bytes.Contains(list.Body.Bytes(), []byte("nginx-vm-template")) {
		t.Fatalf("expected template in list response, got=%s", list.Body.String())
	}
	if !bytes.Contains(list.Body.Bytes(), []byte(`"category":"web-service"`)) {
		t.Fatalf("expected template category in list response, got=%s", list.Body.String())
	}
}

func TestStoreTemplateHandler_ListTemplatesIncludesSupportedInfra(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "template-infra-maintainer", models.WorkspaceRoleMaintainer)
	viewer := seedResourceStoreMember(t, db, workspace.ID, "template-infra-viewer", models.WorkspaceRoleViewer)

	template := models.StoreTemplate{
		WorkspaceID:        workspace.ID,
		Name:               "Redis",
		Description:        "cache service",
		TemplateType:       models.StoreTemplateTypeApp,
		TargetResourceType: models.ResourceTypeVM,
		Source:             models.StoreTemplateSourceWorkspace,
		Status:             models.StoreTemplateStatusPublished,
		Summary:            "in-memory cache",
		Category:           "cache",
		CreatedBy:          maintainer.ID,
	}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}
	vmVersion := models.StoreTemplateVersion{
		WorkspaceID:    workspace.ID,
		TemplateID:     template.ID,
		Version:        "7.2.0",
		DeploymentMode: "vm_command",
		DefaultConfig:  `{"schema_version":1,"infra_type":"vm","version_description":"VM variant","vm":{"command_template":"docker run redis:{{version}}"}}`,
		Status:         models.StoreTemplateStatusPublished,
		CreatedBy:      maintainer.ID,
	}
	k8sVersion := models.StoreTemplateVersion{
		WorkspaceID:    workspace.ID,
		TemplateID:     template.ID,
		Version:        "7.2.0",
		DeploymentMode: "k8s_chart",
		DefaultConfig:  `{"schema_version":1,"infra_type":"k8s","version_description":"K8s variant","k8s":{"chart_source":{"type":"repo","repo_url":"https://charts.bitnami.com/bitnami","chart_name":"redis","chart_version":"19.6.0"},"base_values":"master:\n  count: 1\n"}}`,
		Status:         models.StoreTemplateStatusPublished,
		CreatedBy:      maintainer.ID,
	}
	if err := db.Create(&[]models.StoreTemplateVersion{vmVersion, k8sVersion}).Error; err != nil {
		t.Fatalf("create template versions failed: %v", err)
	}

	h := NewStoreTemplateHandler()
	resp := performResourceStoreRequest(t, h.ListTemplates, viewer.ID, "user", workspace.ID, models.WorkspaceRoleViewer, http.MethodGet, "/api/store/templates?template_type=app", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected list templates success, got=%d body=%s", resp.Code, resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"supported_infra":["k8s","vm"]`)) && !bytes.Contains(resp.Body.Bytes(), []byte(`"supported_infra":["vm","k8s"]`)) {
		t.Fatalf("expected supported infra in list response, got=%s", resp.Body.String())
	}
}

func TestStoreTemplateVersionAndDeploymentRequest_CreatePipelineRun(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "deploy-maintainer", models.WorkspaceRoleMaintainer)
	developer := seedResourceStoreMember(t, db, workspace.ID, "deploy-developer", models.WorkspaceRoleDeveloper)

	project := models.Project{Name: "proj", WorkspaceID: workspace.ID, OwnerID: maintainer.ID}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project failed: %v", err)
	}

	pipelineConfig := PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{{
			ID:   "deploy",
			Type: "docker-run",
			Name: "Docker Deploy",
			Config: map[string]interface{}{
				"host":           "${inputs.resource_host}",
				"port":           "${inputs.resource_port}",
				"user":           "root",
				"image_name":     "${inputs.image_name}",
				"image_tag":      "${inputs.image_tag}",
				"container_name": "${inputs.app_name}",
				"run_args":       "-d",
			},
		}},
		Edges: []PipelineEdge{},
	}
	configJSON, _ := json.Marshal(pipelineConfig)
	pipeline := models.Pipeline{Name: "nginx-vm-pipeline", WorkspaceID: workspace.ID, ProjectID: project.ID, OwnerID: maintainer.ID, Config: string(configJSON)}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	projectID := project.ID
	resource := models.Resource{WorkspaceID: workspace.ID, ProjectID: &projectID, Name: "prod-vm-01", Type: models.ResourceTypeVM, Environment: "production", Status: models.ResourceStatusOnline, Endpoint: "10.0.0.8", CreatedBy: maintainer.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "nginx-vm-template", TemplateType: models.StoreTemplateTypeApp, TargetResourceType: models.ResourceTypeVM, Source: models.StoreTemplateSourceWorkspace, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}

	vh := NewStoreTemplateHandler()
	versionBody := mustJSON(t, map[string]interface{}{
		"version":            "1.0.0",
		"deployment_mode":    "pipeline",
		"pipeline_id":        pipeline.ID,
		"status":             string(models.StoreTemplateStatusPublished),
		"default_config":     "{}",
		"dependency_config":  "{}",
		"target_constraints": "{}",
	})
	versionResp := performResourceStoreRequest(t, vh.CreateTemplateVersion, maintainer.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, http.MethodPost, "/api/store/templates/1/versions", versionBody, pathResourceStoreID(template.ID))
	if versionResp.Code != http.StatusOK {
		t.Fatalf("expected create template version success, got=%d body=%s", versionResp.Code, versionResp.Body.String())
	}
	versionID := responseDataID(t, versionResp.Body.Bytes())

	dh := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{
		"template_version_id": versionID,
		"target_resource_id":  resource.ID,
		"parameters": map[string]interface{}{
			"app_name":   "nginx-web",
			"image_name": "nginx",
			"image_tag":  "latest",
		},
	})
	createReq := performResourceStoreRequest(t, dh.CreateDeploymentRequest, developer.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, http.MethodPost, "/api/deployments/requests", requestBody)
	if createReq.Code != http.StatusOK {
		t.Fatalf("expected create deployment request success, got=%d body=%s", createReq.Code, createReq.Body.String())
	}

	var req models.DeploymentRequest
	if err := db.First(&req, responseDataID(t, createReq.Body.Bytes())).Error; err != nil {
		t.Fatalf("load deployment request failed: %v", err)
	}
	if req.PipelineRunID == 0 {
		t.Fatalf("expected pipeline run to be created")
	}
	var run models.PipelineRun
	if err := db.First(&run, req.PipelineRunID).Error; err != nil {
		t.Fatalf("load pipeline run failed: %v", err)
	}
	if !bytes.Contains([]byte(run.Config), []byte("10.0.0.8")) || !bytes.Contains([]byte(run.Config), []byte("nginx-web")) {
		t.Fatalf("expected resolved resource/parameter values in pipeline config, got=%s", run.Config)
	}
	if req.Status != models.DeploymentRequestStatusQueued && req.Status != models.DeploymentRequestStatusRunning {
		t.Fatalf("unexpected deployment request status=%s", req.Status)
	}
	if req.ProjectID == nil || *req.ProjectID != project.ID {
		t.Fatalf("expected deployment request project_id=%d, got=%v", project.ID, req.ProjectID)
	}
}

func TestStoreTemplateHandler_ListTemplateVersionsScopesToWorkspace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainerA, workspaceA := seedResourceStoreUserAndWorkspace(t, db, "template-version-a", models.WorkspaceRoleMaintainer)
	maintainerB, workspaceB := seedResourceStoreUserAndWorkspace(t, db, "template-version-b", models.WorkspaceRoleMaintainer)

	template := models.StoreTemplate{
		WorkspaceID:        workspaceA.ID,
		Name:               "platform-k8s-template",
		TemplateType:       models.StoreTemplateTypeLLM,
		TargetResourceType: models.ResourceTypeK8sCluster,
		Source:             models.StoreTemplateSourcePlatform,
		Status:             models.StoreTemplateStatusPublished,
		CreatedBy:          maintainerA.ID,
	}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create platform template failed: %v", err)
	}

	pipelineA := models.Pipeline{Name: "workspace-a-k8s", WorkspaceID: workspaceA.ID, OwnerID: maintainerA.ID, Config: minimalKubernetesPipelineConfig(t)}
	if err := db.Create(&pipelineA).Error; err != nil {
		t.Fatalf("create workspace A pipeline failed: %v", err)
	}
	pipelineB := models.Pipeline{Name: "workspace-b-k8s", WorkspaceID: workspaceB.ID, OwnerID: maintainerB.ID, Config: minimalKubernetesPipelineConfig(t)}
	if err := db.Create(&pipelineB).Error; err != nil {
		t.Fatalf("create workspace B pipeline failed: %v", err)
	}

	versionA := models.StoreTemplateVersion{WorkspaceID: workspaceA.ID, TemplateID: template.ID, PipelineID: pipelineA.ID, Version: "1.0.0-a", DeploymentMode: "pipeline", Status: models.StoreTemplateStatusPublished, CreatedBy: maintainerA.ID}
	if err := db.Create(&versionA).Error; err != nil {
		t.Fatalf("create workspace A version failed: %v", err)
	}
	versionB := models.StoreTemplateVersion{WorkspaceID: workspaceB.ID, TemplateID: template.ID, PipelineID: pipelineB.ID, Version: "1.0.0-b", DeploymentMode: "pipeline", Status: models.StoreTemplateStatusPublished, CreatedBy: maintainerB.ID}
	if err := db.Create(&versionB).Error; err != nil {
		t.Fatalf("create workspace B version failed: %v", err)
	}

	h := NewStoreTemplateHandler()
	resp := performResourceStoreRequest(t, h.ListTemplateVersions, maintainerA.ID, "user", workspaceA.ID, models.WorkspaceRoleMaintainer, http.MethodGet, "/api/store/templates/1/versions", nil, pathResourceStoreID(template.ID))
	if resp.Code != http.StatusOK {
		t.Fatalf("expected list template versions success, got=%d body=%s", resp.Code, resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(versionA.Version)) {
		t.Fatalf("expected workspace A version in response, got=%s", resp.Body.String())
	}
	if bytes.Contains(resp.Body.Bytes(), []byte(versionB.Version)) {
		t.Fatalf("expected workspace B version to be hidden, got=%s", resp.Body.String())
	}
}

func TestResourceHandler_BindCredentialAndDeploymentInjectsClusterAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "k8s-bind-maintainer", models.WorkspaceRoleMaintainer)
	developer := seedResourceStoreMember(t, db, workspace.ID, "k8s-bind-developer", models.WorkspaceRoleDeveloper)

	project := models.Project{Name: "k8s-proj", WorkspaceID: workspace.ID, OwnerID: maintainer.ID}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project failed: %v", err)
	}

	encrypted, err := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{
		"server":     "https://kubernetes.example.com",
		"token":      "k8s-token-value",
		"kubeconfig": "apiVersion: v1\nclusters: []\ncontexts: []\ncurrent-context: \"\"\nusers: []\n",
	})
	if err != nil {
		t.Fatalf("encrypt kubernetes credential failed: %v", err)
	}
	credential := models.Credential{
		Name:             "cluster-auth",
		Type:             models.TypeToken,
		Category:         models.CategoryKubernetes,
		Scope:            models.ScopeWorkspace,
		WorkspaceID:      workspace.ID,
		OwnerID:          maintainer.ID,
		EncryptedPayload: encrypted,
		Status:           models.CredentialStatusActive,
	}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatalf("create credential failed: %v", err)
	}

	projectID := project.ID
	resource := models.Resource{
		WorkspaceID: workspace.ID,
		ProjectID:   &projectID,
		Name:        "prod-k8s-cluster",
		Type:        models.ResourceTypeK8sCluster,
		Environment: "production",
		Status:      models.ResourceStatusOnline,
		Endpoint:    "https://kubernetes.example.com",
		CreatedBy:   maintainer.ID,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	rh := NewResourceHandler()
	bindBody := mustJSON(t, map[string]interface{}{
		"credential_id": credential.ID,
		"purpose":       "cluster_auth",
	})
	bindResp := performResourceStoreRequest(t, rh.BindResourceCredential, maintainer.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, http.MethodPost, "/api/resources/1/credentials/bind", bindBody, pathResourceStoreID(resource.ID))
	if bindResp.Code != http.StatusOK {
		t.Fatalf("expected bind resource credential success, got=%d body=%s", bindResp.Code, bindResp.Body.String())
	}

	var binding models.ResourceCredentialBinding
	if err := db.Where("resource_id = ? AND credential_id = ?", resource.ID, credential.ID).First(&binding).Error; err != nil {
		t.Fatalf("load resource credential binding failed: %v", err)
	}
	if binding.Purpose != "cluster_auth" {
		t.Fatalf("expected cluster_auth binding purpose, got=%s", binding.Purpose)
	}

	pipelineConfig := PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{{
			ID:   "deploy",
			Type: "kubernetes",
			Name: "Kubernetes Deploy",
			Config: map[string]interface{}{
				"manifest": "./k8s/deploy.yaml",
			},
		}},
		Edges: []PipelineEdge{},
	}
	configJSON, _ := json.Marshal(pipelineConfig)
	pipeline := models.Pipeline{Name: "k8s-deploy-pipeline", WorkspaceID: workspace.ID, ProjectID: project.ID, OwnerID: maintainer.ID, Config: string(configJSON)}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "k8s-template", TemplateType: models.StoreTemplateTypeApp, TargetResourceType: models.ResourceTypeK8sCluster, Source: models.StoreTemplateSourceWorkspace, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}

	vh := NewStoreTemplateHandler()
	versionBody := mustJSON(t, map[string]interface{}{
		"version":            "1.0.0",
		"deployment_mode":    "pipeline",
		"pipeline_id":        pipeline.ID,
		"status":             string(models.StoreTemplateStatusPublished),
		"default_config":     "{}",
		"dependency_config":  "{}",
		"target_constraints": "{}",
	})
	versionResp := performResourceStoreRequest(t, vh.CreateTemplateVersion, maintainer.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, http.MethodPost, "/api/store/templates/1/versions", versionBody, pathResourceStoreID(template.ID))
	if versionResp.Code != http.StatusOK {
		t.Fatalf("expected create template version success, got=%d body=%s", versionResp.Code, versionResp.Body.String())
	}
	versionID := responseDataID(t, versionResp.Body.Bytes())

	dh := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{
		"template_version_id": versionID,
		"target_resource_id":  resource.ID,
		"parameters":          map[string]interface{}{},
	})
	createReq := performResourceStoreRequest(t, dh.CreateDeploymentRequest, developer.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, http.MethodPost, "/api/deployments/requests", requestBody)
	if createReq.Code != http.StatusOK {
		t.Fatalf("expected create deployment request success, got=%d body=%s", createReq.Code, createReq.Body.String())
	}

	var req models.DeploymentRequest
	if err := db.First(&req, responseDataID(t, createReq.Body.Bytes())).Error; err != nil {
		t.Fatalf("load deployment request failed: %v", err)
	}
	var run models.PipelineRun
	if err := db.First(&run, req.PipelineRunID).Error; err != nil {
		t.Fatalf("load pipeline run failed: %v", err)
	}
	if !bytes.Contains([]byte(run.Config), []byte("cluster_auth")) {
		t.Fatalf("expected deployment run config to include bound cluster_auth credential, got=%s", run.Config)
	}
	if !bytes.Contains([]byte(run.Config), []byte(strconv.FormatUint(credential.ID, 10))) {
		t.Fatalf("expected deployment run config to reference bound credential id=%d, got=%s", credential.ID, run.Config)
	}
}

func TestResourceHandler_BindPasswordCredentialAndDeploymentInjectsVMAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "vm-bind-maintainer", models.WorkspaceRoleMaintainer)
	developer := seedResourceStoreMember(t, db, workspace.ID, "vm-bind-developer", models.WorkspaceRoleDeveloper)

	project := models.Project{Name: "vm-proj", WorkspaceID: workspace.ID, OwnerID: maintainer.ID}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project failed: %v", err)
	}

	encryption := NewCredentialHandler().encryptionService
	passwordPayload, err := encryption.EncryptCredentialData(map[string]interface{}{
		"username": "root",
		"password": "secret123",
	})
	if err != nil {
		t.Fatalf("encrypt vm password credential failed: %v", err)
	}
	resourceCredential := models.Credential{
		Name:             "vm-password-auth",
		Type:             models.TypePassword,
		Category:         models.CategoryCustom,
		Scope:            models.ScopeWorkspace,
		WorkspaceID:      workspace.ID,
		OwnerID:          maintainer.ID,
		EncryptedPayload: passwordPayload,
		Status:           models.CredentialStatusActive,
	}
	if err := db.Create(&resourceCredential).Error; err != nil {
		t.Fatalf("create resource credential failed: %v", err)
	}

	keyPayload, err := encryption.EncryptCredentialData(map[string]interface{}{
		"private_key": "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----",
		"key_type":    "rsa",
	})
	if err != nil {
		t.Fatalf("encrypt existing ssh key credential failed: %v", err)
	}
	existingCredential := models.Credential{
		Name:             "existing-ssh-key-auth",
		Type:             models.TypeSSHKey,
		Category:         models.CategoryCustom,
		Scope:            models.ScopeWorkspace,
		WorkspaceID:      workspace.ID,
		OwnerID:          maintainer.ID,
		EncryptedPayload: keyPayload,
		Status:           models.CredentialStatusActive,
	}
	if err := db.Create(&existingCredential).Error; err != nil {
		t.Fatalf("create existing credential failed: %v", err)
	}

	projectID := project.ID
	resource := models.Resource{
		WorkspaceID: workspace.ID,
		ProjectID:   &projectID,
		Name:        "prod-vm-password",
		Type:        models.ResourceTypeVM,
		Environment: "production",
		Status:      models.ResourceStatusOnline,
		Endpoint:    "10.0.0.8:22",
		CreatedBy:   maintainer.ID,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	rh := NewResourceHandler()
	bindBody := mustJSON(t, map[string]interface{}{
		"credential_id": resourceCredential.ID,
		"purpose":       "ssh_auth",
	})
	bindResp := performResourceStoreRequest(t, rh.BindResourceCredential, maintainer.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, http.MethodPost, "/api/resources/1/credentials/bind", bindBody, pathResourceStoreID(resource.ID))
	if bindResp.Code != http.StatusOK {
		t.Fatalf("expected bind resource credential success, got=%d body=%s", bindResp.Code, bindResp.Body.String())
	}

	var binding models.ResourceCredentialBinding
	if err := db.Where("resource_id = ? AND credential_id = ?", resource.ID, resourceCredential.ID).First(&binding).Error; err != nil {
		t.Fatalf("load resource credential binding failed: %v", err)
	}
	if binding.Purpose != "ssh_auth" {
		t.Fatalf("expected ssh_auth binding purpose, got=%s", binding.Purpose)
	}

	pipelineConfig := PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{{
			ID:   "deploy",
			Type: "docker-run",
			Name: "Docker Deploy",
			Config: map[string]interface{}{
				"host":       "${inputs.resource_host}",
				"port":       "${inputs.resource_port}",
				"user":       "root",
				"image_name": "nginx",
				"image_tag":  "latest",
				"credentials": map[string]interface{}{
					"ssh_auth": map[string]interface{}{"credential_id": existingCredential.ID},
				},
			},
		}},
		Edges: []PipelineEdge{},
	}
	configJSON, _ := json.Marshal(pipelineConfig)
	pipeline := models.Pipeline{Name: "vm-deploy-pipeline", WorkspaceID: workspace.ID, ProjectID: project.ID, OwnerID: maintainer.ID, Config: string(configJSON)}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "vm-template", TemplateType: models.StoreTemplateTypeApp, TargetResourceType: models.ResourceTypeVM, Source: models.StoreTemplateSourceWorkspace, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}

	vh := NewStoreTemplateHandler()
	versionBody := mustJSON(t, map[string]interface{}{
		"version":            "1.0.0",
		"deployment_mode":    "pipeline",
		"pipeline_id":        pipeline.ID,
		"status":             string(models.StoreTemplateStatusPublished),
		"default_config":     "{}",
		"dependency_config":  "{}",
		"target_constraints": "{}",
	})
	versionResp := performResourceStoreRequest(t, vh.CreateTemplateVersion, maintainer.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, http.MethodPost, "/api/store/templates/1/versions", versionBody, pathResourceStoreID(template.ID))
	if versionResp.Code != http.StatusOK {
		t.Fatalf("expected create template version success, got=%d body=%s", versionResp.Code, versionResp.Body.String())
	}
	versionID := responseDataID(t, versionResp.Body.Bytes())

	dh := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{
		"template_version_id": versionID,
		"target_resource_id":  resource.ID,
		"parameters":          map[string]interface{}{},
	})
	createReq := performResourceStoreRequest(t, dh.CreateDeploymentRequest, developer.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, http.MethodPost, "/api/deployments/requests", requestBody)
	if createReq.Code != http.StatusOK {
		t.Fatalf("expected create deployment request success, got=%d body=%s", createReq.Code, createReq.Body.String())
	}

	var req models.DeploymentRequest
	if err := db.First(&req, responseDataID(t, createReq.Body.Bytes())).Error; err != nil {
		t.Fatalf("load deployment request failed: %v", err)
	}
	var run models.PipelineRun
	if err := db.First(&run, req.PipelineRunID).Error; err != nil {
		t.Fatalf("load pipeline run failed: %v", err)
	}
	if !bytes.Contains([]byte(run.Config), []byte("ssh_auth")) {
		t.Fatalf("expected deployment run config to include bound ssh_auth credential, got=%s", run.Config)
	}
	if !bytes.Contains([]byte(run.Config), []byte(`"credential_id":`+strconv.FormatUint(resourceCredential.ID, 10))) {
		t.Fatalf("expected deployment run config to reference bound credential id=%d, got=%s", resourceCredential.ID, run.Config)
	}
	if bytes.Contains([]byte(run.Config), []byte(`"credential_id":`+strconv.FormatUint(existingCredential.ID, 10))) {
		t.Fatalf("expected deployment run config to overwrite existing ssh_auth credential id=%d, got=%s", existingCredential.ID, run.Config)
	}
}

func TestDeploymentHandler_FailedRunSyncsDeploymentRequestStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "deploy-status-maintainer", models.WorkspaceRoleMaintainer)

	run := models.PipelineRun{
		WorkspaceID: workspace.ID,
		PipelineID:  1,
		BuildNumber: 1,
		Status:      models.PipelineRunStatusRunning,
		StartTime:   time.Now().Unix() - 10,
		AgentID:     seedApprovedResourceAgent(t, db, workspace.ID).ID,
		Config:      `{"version":"2.0","nodes":[{"id":"deploy","type":"ssh","name":"deploy","ignore_failure":false}],"edges":[]}`,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}
	request := models.DeploymentRequest{
		WorkspaceID:        workspace.ID,
		TemplateID:         1,
		TemplateVersionID:  1,
		TemplateType:       models.StoreTemplateTypeLLM,
		TargetResourceID:   1,
		TargetResourceType: models.ResourceTypeVM,
		Status:             models.DeploymentRequestStatusQueued,
		PipelineRunID:      run.ID,
		RequestedBy:        maintainer.ID,
	}
	if err := db.Create(&request).Error; err != nil {
		t.Fatalf("create deployment request failed: %v", err)
	}
	if err := db.Create(&models.DeploymentRecord{
		WorkspaceID:   workspace.ID,
		RequestID:     request.ID,
		PipelineRunID: run.ID,
		Status:        models.DeploymentRequestStatusQueued,
	}).Error; err != nil {
		t.Fatalf("create deployment record failed: %v", err)
	}
	if err := db.Create(&models.AgentTask{
		WorkspaceID:   workspace.ID,
		AgentID:       run.AgentID,
		PipelineRunID: run.ID,
		NodeID:        "deploy",
		TaskType:      "ssh",
		Name:          "deploy",
		Status:        models.TaskStatusExecuteFailed,
		ErrorMsg:      "ssh auth denied",
		StartTime:     time.Now().Unix() - 9,
		EndTime:       time.Now().Unix() - 1,
		Duration:      8,
	}).Error; err != nil {
		t.Fatalf("create failed task failed: %v", err)
	}

	SharedWebSocketHandler().checkAndUpdatePipelineStatus(run.ID)

	var gotRequest models.DeploymentRequest
	if err := db.First(&gotRequest, request.ID).Error; err != nil {
		t.Fatalf("reload deployment request failed: %v", err)
	}
	if gotRequest.Status != models.DeploymentRequestStatusFailed {
		t.Fatalf("deployment request status=%s, want=%s", gotRequest.Status, models.DeploymentRequestStatusFailed)
	}
	var gotRecord models.DeploymentRecord
	if err := db.First(&gotRecord, 1).Error; err != nil {
		t.Fatalf("reload deployment record failed: %v", err)
	}
	if gotRecord.Status != models.DeploymentRequestStatusFailed {
		t.Fatalf("deployment record status=%s, want=%s", gotRecord.Status, models.DeploymentRequestStatusFailed)
	}
	if gotRecord.FailureReason != "ssh auth denied" {
		t.Fatalf("deployment record failure_reason=%q, want=%q", gotRecord.FailureReason, "ssh auth denied")
	}
}

func TestStoreTemplateHandler_ListTemplateVersionsIncludesParameterMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "template-params-maintainer", models.WorkspaceRoleMaintainer)
	template := models.StoreTemplate{
		WorkspaceID:        workspace.ID,
		Name:               "ollama-template",
		TemplateType:       models.StoreTemplateTypeLLM,
		TargetResourceType: models.ResourceTypeVM,
		Source:             models.StoreTemplateSourceWorkspace,
		Status:             models.StoreTemplateStatusPublished,
		CreatedBy:          maintainer.ID,
	}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}
	pipeline := models.Pipeline{Name: "ollama-hidden-pipeline", WorkspaceID: workspace.ID, OwnerID: maintainer.ID, Config: minimalKubernetesPipelineConfig(t)}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	version := models.StoreTemplateVersion{
		WorkspaceID:    workspace.ID,
		TemplateID:     template.ID,
		PipelineID:     pipeline.ID,
		Version:        "1.2.3",
		DeploymentMode: "pipeline",
		Status:         models.StoreTemplateStatusPublished,
		CreatedBy:      maintainer.ID,
	}
	if err := db.Create(&version).Error; err != nil {
		t.Fatalf("create version failed: %v", err)
	}
	parameters := []models.TemplateParameter{
		{TemplateVersionID: version.ID, Name: "model_tag", Label: "Model Tag", Description: "要发布的模型标签版本", Type: "string", DefaultValue: "latest", Required: true, SortOrder: 1},
		{TemplateVersionID: version.ID, Name: "gpu_memory_utilization", Label: "GPU Memory Utilization", Description: "控制显存占用比例", Type: "number", DefaultValue: "0.9", Required: false, Advanced: true, SortOrder: 2},
	}
	if err := db.Create(&parameters).Error; err != nil {
		t.Fatalf("create template parameters failed: %v", err)
	}

	h := NewStoreTemplateHandler()
	resp := performResourceStoreRequest(t, h.ListTemplateVersions, maintainer.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, http.MethodGet, "/api/store/templates/1/versions", nil, pathResourceStoreID(template.ID))
	if resp.Code != http.StatusOK {
		t.Fatalf("expected list template versions success, got=%d body=%s", resp.Code, resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte("model_tag")) || !bytes.Contains(resp.Body.Bytes(), []byte("GPU Memory Utilization")) || !bytes.Contains(resp.Body.Bytes(), []byte("控制显存占用比例")) || !bytes.Contains(resp.Body.Bytes(), []byte(`"advanced":true`)) {
		t.Fatalf("expected template version parameter metadata in response, got=%s", resp.Body.String())
	}
}

func TestStoreTemplateHandler_ListTemplatesIncludesCategoryAndSupportedInfras(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	viewer, workspace := seedResourceStoreUserAndWorkspace(t, db, "template-list-category-viewer", models.WorkspaceRoleViewer)
	template := models.StoreTemplate{
		WorkspaceID:        workspace.ID,
		Name:               "Redis",
		Summary:            "cache service",
		Description:        "redis app",
		TemplateType:       models.StoreTemplateTypeApp,
		TargetResourceType: models.ResourceTypeVM,
		Source:             models.StoreTemplateSourceWorkspace,
		Status:             models.StoreTemplateStatusPublished,
		Category:           "cache",
		CreatedBy:          viewer.ID,
	}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}
	vmVersion := models.StoreTemplateVersion{
		WorkspaceID:    workspace.ID,
		TemplateID:     template.ID,
		Version:        "7.2.0",
		DeploymentMode: "vm_command",
		DefaultConfig:  `{"schema_version":1,"infra_type":"vm","version_description":"vm variant","vm":{"command_template":"docker run redis:{{version}}"}}`,
		Status:         models.StoreTemplateStatusPublished,
		CreatedBy:      viewer.ID,
	}
	if err := db.Create(&vmVersion).Error; err != nil {
		t.Fatalf("create vm version failed: %v", err)
	}
	k8sVersion := models.StoreTemplateVersion{
		WorkspaceID:    workspace.ID,
		TemplateID:     template.ID,
		Version:        "7.2.0",
		DeploymentMode: "k8s_chart",
		DefaultConfig:  `{"schema_version":1,"infra_type":"k8s","version_description":"k8s variant","k8s":{"chart_source":{"type":"repo","repo_url":"https://charts.bitnami.com/bitnami","chart_name":"redis","chart_version":"19.6.0"},"base_values":"architecture: standalone\n"}}`,
		Status:         models.StoreTemplateStatusPublished,
		CreatedBy:      viewer.ID,
	}
	if err := db.Create(&k8sVersion).Error; err != nil {
		t.Fatalf("create k8s version failed: %v", err)
	}

	h := NewStoreTemplateHandler()
	resp := performResourceStoreRequest(t, h.ListTemplates, viewer.ID, "user", workspace.ID, models.WorkspaceRoleViewer, http.MethodGet, "/api/store/templates?template_type=app", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected list templates success, got=%d body=%s", resp.Code, resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"category":"cache"`)) {
		t.Fatalf("expected category in list response, got=%s", resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"supported_infras":["k8s","vm"]`)) && !bytes.Contains(resp.Body.Bytes(), []byte(`"supported_infras":["vm","k8s"]`)) {
		t.Fatalf("expected supported_infras in list response, got=%s", resp.Body.String())
	}
}

func TestStoreTemplateHandler_UpdateAndDeleteTemplateVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "template-version-update-maintainer", models.WorkspaceRoleMaintainer)
	template := models.StoreTemplate{
		WorkspaceID:        workspace.ID,
		Name:               "Redis",
		TemplateType:       models.StoreTemplateTypeApp,
		TargetResourceType: models.ResourceTypeVM,
		Source:             models.StoreTemplateSourceWorkspace,
		Status:             models.StoreTemplateStatusPublished,
		Category:           "cache",
		CreatedBy:          maintainer.ID,
	}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}
	version := models.StoreTemplateVersion{
		WorkspaceID:    workspace.ID,
		TemplateID:     template.ID,
		Version:        "7.2.0",
		DeploymentMode: "vm_command",
		DefaultConfig:  `{"schema_version":1,"infra_type":"vm","version_description":"old description","vm":{"command_template":"docker run redis:{{version}}"}}`,
		Status:         models.StoreTemplateStatusDraft,
		CreatedBy:      maintainer.ID,
	}
	if err := db.Create(&version).Error; err != nil {
		t.Fatalf("create version failed: %v", err)
	}
	if err := db.Create(&models.TemplateParameter{TemplateVersionID: version.ID, Name: "version", Label: "Version", Type: "text", DefaultValue: "7.2.0", Required: true, SortOrder: 1}).Error; err != nil {
		t.Fatalf("create version parameter failed: %v", err)
	}

	h := NewStoreTemplateHandler()
	updateBody := mustJSON(t, map[string]interface{}{
		"version":             "7.2.1",
		"status":              string(models.StoreTemplateStatusPublished),
		"infra_type":          "vm",
		"version_description": "stable vm variant",
		"command_template":    "docker run -d --name {{container_name}} redis:{{version}}",
		"parameters": []map[string]interface{}{
			{
				"name":          "container_name",
				"label":         "Container Name",
				"type":          "text",
				"default_value": "redis-main",
				"required":      true,
				"sort_order":    1,
			},
		},
	})
	updateResp := performResourceStoreRequest(
		t,
		h.UpdateTemplateVersion,
		maintainer.ID,
		"user",
		workspace.ID,
		models.WorkspaceRoleMaintainer,
		http.MethodPut,
		"/api/store/templates/1/versions/1",
		updateBody,
		pathTemplateVersionIDs(template.ID, version.ID),
	)
	if updateResp.Code != http.StatusOK {
		t.Fatalf("expected update template version success, got=%d body=%s", updateResp.Code, updateResp.Body.String())
	}

	listResp := performResourceStoreRequest(t, h.ListTemplateVersions, maintainer.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, http.MethodGet, "/api/store/templates/1/versions", nil, pathResourceStoreID(template.ID))
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected list template versions success, got=%d body=%s", listResp.Code, listResp.Body.String())
	}
	if !bytes.Contains(listResp.Body.Bytes(), []byte(`"version_description":"stable vm variant"`)) {
		t.Fatalf("expected version description in list response, got=%s", listResp.Body.String())
	}
	if !bytes.Contains(listResp.Body.Bytes(), []byte(`"infra_type":"vm"`)) {
		t.Fatalf("expected infra_type in list response, got=%s", listResp.Body.String())
	}
	if !bytes.Contains(listResp.Body.Bytes(), []byte(`"command_template":"docker run -d --name {{container_name}} redis:{{version}}"`)) {
		t.Fatalf("expected command template in list response, got=%s", listResp.Body.String())
	}
	if !bytes.Contains(listResp.Body.Bytes(), []byte(`"container_name"`)) {
		t.Fatalf("expected rewritten parameters in list response, got=%s", listResp.Body.String())
	}

	deleteResp := performResourceStoreRequest(
		t,
		h.DeleteTemplateVersion,
		maintainer.ID,
		"user",
		workspace.ID,
		models.WorkspaceRoleMaintainer,
		http.MethodDelete,
		"/api/store/templates/1/versions/1",
		nil,
		pathTemplateVersionIDs(template.ID, version.ID),
	)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("expected delete template version success, got=%d body=%s", deleteResp.Code, deleteResp.Body.String())
	}
	var count int64
	if err := db.Model(&models.StoreTemplateVersion{}).Where("id = ?", version.ID).Count(&count).Error; err != nil {
		t.Fatalf("count template version failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected template version deleted, count=%d", count)
	}
}

func TestStoreTemplateHandler_PreviewTemplateVersionVMCommand(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "template-preview-vm-maintainer", models.WorkspaceRoleMaintainer)
	template := models.StoreTemplate{
		WorkspaceID:        workspace.ID,
		Name:               "Redis",
		TemplateType:       models.StoreTemplateTypeApp,
		TargetResourceType: models.ResourceTypeVM,
		Source:             models.StoreTemplateSourceWorkspace,
		Status:             models.StoreTemplateStatusPublished,
		Category:           "cache",
		CreatedBy:          maintainer.ID,
	}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}
	version := models.StoreTemplateVersion{
		WorkspaceID:    workspace.ID,
		TemplateID:     template.ID,
		Version:        "7.2.0",
		DeploymentMode: "vm_command",
		DefaultConfig:  `{"schema_version":1,"infra_type":"vm","version_description":"stable vm variant","vm":{"command_template":"docker run -d --name {{container_name}} -p {{vm_port}}:{{redis_port}} redis:{{version}}"}}`,
		Status:         models.StoreTemplateStatusPublished,
		CreatedBy:      maintainer.ID,
	}
	if err := db.Create(&version).Error; err != nil {
		t.Fatalf("create version failed: %v", err)
	}
	parameters := []models.TemplateParameter{
		{TemplateVersionID: version.ID, Name: "container_name", Label: "Container Name", Type: "text", DefaultValue: "redis-main", Required: true, SortOrder: 1},
		{TemplateVersionID: version.ID, Name: "redis_port", Label: "Redis Port", Type: "number", DefaultValue: "6379", Required: true, SortOrder: 2},
		{TemplateVersionID: version.ID, Name: "vm_port", Label: "VM Port", Type: "number", DefaultValue: "16379", Required: true, SortOrder: 3},
		{TemplateVersionID: version.ID, Name: "version", Label: "Version", Type: "text", DefaultValue: "7.2.0", Required: true, SortOrder: 4},
	}
	if err := db.Create(&parameters).Error; err != nil {
		t.Fatalf("create parameters failed: %v", err)
	}
	resource := models.Resource{WorkspaceID: workspace.ID, Name: "vm-01", Type: models.ResourceTypeVM, Environment: "production", Status: models.ResourceStatusOnline, Endpoint: "10.0.0.8:22", CreatedBy: maintainer.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	h := NewStoreTemplateHandler()
	body := mustJSON(t, map[string]interface{}{
		"target_resource_id": resource.ID,
		"parameters": map[string]interface{}{
			"container_name": "redis-prod",
			"vm_port":        26379,
		},
	})
	resp := performResourceStoreRequest(
		t,
		h.PreviewTemplateVersion,
		maintainer.ID,
		"user",
		workspace.ID,
		models.WorkspaceRoleMaintainer,
		http.MethodPost,
		"/api/store/templates/1/versions/1/preview",
		body,
		pathTemplateVersionIDs(template.ID, version.ID),
	)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected preview template version success, got=%d body=%s", resp.Code, resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"infra_type":"vm"`)) {
		t.Fatalf("expected vm infra preview response, got=%s", resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`docker run -d --name redis-prod -p 26379:6379 redis:7.2.0`)) {
		t.Fatalf("expected rendered vm command in preview response, got=%s", resp.Body.String())
	}

	k8sResource := models.Resource{WorkspaceID: workspace.ID, Name: "cluster-01", Type: models.ResourceTypeK8sCluster, Environment: "production", Status: models.ResourceStatusOnline, Endpoint: "https://10.0.0.1:6443", CreatedBy: maintainer.ID}
	if err := db.Create(&k8sResource).Error; err != nil {
		t.Fatalf("create k8s resource failed: %v", err)
	}
	mismatchBody := mustJSON(t, map[string]interface{}{
		"target_resource_id": k8sResource.ID,
		"parameters":         map[string]interface{}{},
	})
	mismatchResp := performResourceStoreRequest(
		t,
		h.PreviewTemplateVersion,
		maintainer.ID,
		"user",
		workspace.ID,
		models.WorkspaceRoleMaintainer,
		http.MethodPost,
		"/api/store/templates/1/versions/1/preview",
		mismatchBody,
		pathTemplateVersionIDs(template.ID, version.ID),
	)
	if mismatchResp.Code != http.StatusBadRequest {
		t.Fatalf("expected mismatched resource preview to fail, got=%d body=%s", mismatchResp.Code, mismatchResp.Body.String())
	}
}

func TestStoreTemplateHandler_PreviewTemplateVersionK8sHelmDiff(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "template-preview-k8s-maintainer", models.WorkspaceRoleMaintainer)
	template := models.StoreTemplate{
		WorkspaceID:        workspace.ID,
		Name:               "Redis",
		TemplateType:       models.StoreTemplateTypeApp,
		TargetResourceType: models.ResourceTypeK8sCluster,
		Source:             models.StoreTemplateSourceWorkspace,
		Status:             models.StoreTemplateStatusPublished,
		Category:           "cache",
		CreatedBy:          maintainer.ID,
	}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}
	version := models.StoreTemplateVersion{
		WorkspaceID:    workspace.ID,
		TemplateID:     template.ID,
		Version:        "19.6.0",
		DeploymentMode: "k8s_chart",
		DefaultConfig:  `{"schema_version":1,"infra_type":"k8s","version_description":"stable k8s variant","k8s":{"chart_source":{"type":"repo","repo_url":"https://charts.bitnami.com/bitnami","chart_name":"redis","chart_version":"19.6.0"},"base_values":"architecture: standalone\nauth:\n  enabled: true\nmaster:\n  count: 1\n"}}`,
		Status:         models.StoreTemplateStatusPublished,
		CreatedBy:      maintainer.ID,
	}
	if err := db.Create(&version).Error; err != nil {
		t.Fatalf("create version failed: %v", err)
	}
	parameters := []models.TemplateParameter{
		{TemplateVersionID: version.ID, Name: "release_name", Label: "Release Name", Type: "text", DefaultValue: "redis", Required: true, SortOrder: 1},
		{TemplateVersionID: version.ID, Name: "namespace", Label: "Namespace", Type: "text", DefaultValue: "default", Required: true, SortOrder: 2},
		{TemplateVersionID: version.ID, Name: "auth.password", Label: "Password", Type: "text", DefaultValue: "", Required: true, SortOrder: 3},
		{TemplateVersionID: version.ID, Name: "master.count", Label: "Master Count", Type: "number", DefaultValue: "1", Required: false, SortOrder: 4},
	}
	if err := db.Create(&parameters).Error; err != nil {
		t.Fatalf("create parameters failed: %v", err)
	}
	resource := models.Resource{WorkspaceID: workspace.ID, Name: "cluster-01", Type: models.ResourceTypeK8sCluster, Environment: "production", Status: models.ResourceStatusOnline, Endpoint: "https://10.0.0.1:6443", CreatedBy: maintainer.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create k8s resource failed: %v", err)
	}

	h := NewStoreTemplateHandler()
	body := mustJSON(t, map[string]interface{}{
		"target_resource_id": resource.ID,
		"parameters": map[string]interface{}{
			"release_name":  "redis-prod",
			"namespace":     "middleware",
			"auth.password": "secret-pass",
			"master.count":  2,
		},
	})
	resp := performResourceStoreRequest(
		t,
		h.PreviewTemplateVersion,
		maintainer.ID,
		"user",
		workspace.ID,
		models.WorkspaceRoleMaintainer,
		http.MethodPost,
		"/api/store/templates/1/versions/1/preview",
		body,
		pathTemplateVersionIDs(template.ID, version.ID),
	)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected preview template version success, got=%d body=%s", resp.Code, resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"infra_type":"k8s"`)) {
		t.Fatalf("expected k8s infra preview response, got=%s", resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"override_values_yaml":"auth:`)) {
		t.Fatalf("expected override values yaml in preview response, got=%s", resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"diff_lines":[`)) {
		t.Fatalf("expected diff lines in preview response, got=%s", resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`helm --kubeconfig`)) || !bytes.Contains(resp.Body.Bytes(), []byte(`install redis-prod`)) {
		t.Fatalf("expected helm command preview in response, got=%s", resp.Body.String())
	}
}

func TestDeploymentHandler_CreateDeploymentRequestBuildsDirectAppRuns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "app-direct-deploy-maintainer", models.WorkspaceRoleMaintainer)
	developer := seedResourceStoreMember(t, db, workspace.ID, "app-direct-deploy-developer", models.WorkspaceRoleDeveloper)

	vmTemplate := models.StoreTemplate{
		WorkspaceID:        workspace.ID,
		Name:               "Redis",
		TemplateType:       models.StoreTemplateTypeApp,
		TargetResourceType: models.ResourceTypeVM,
		Source:             models.StoreTemplateSourceWorkspace,
		Status:             models.StoreTemplateStatusPublished,
		Category:           "cache",
		CreatedBy:          maintainer.ID,
	}
	if err := db.Create(&vmTemplate).Error; err != nil {
		t.Fatalf("create vm template failed: %v", err)
	}
	vmVersion := models.StoreTemplateVersion{
		WorkspaceID:    workspace.ID,
		TemplateID:     vmTemplate.ID,
		Version:        "7.2.0",
		DeploymentMode: "vm_command",
		DefaultConfig:  `{"schema_version":1,"infra_type":"vm","version_description":"stable vm variant","vm":{"command_template":"docker run -d --name {{container_name}} -p {{vm_port}}:{{redis_port}} redis:{{version}}"}}`,
		Status:         models.StoreTemplateStatusPublished,
		CreatedBy:      maintainer.ID,
	}
	if err := db.Create(&vmVersion).Error; err != nil {
		t.Fatalf("create vm version failed: %v", err)
	}
	vmParams := []models.TemplateParameter{
		{TemplateVersionID: vmVersion.ID, Name: "container_name", Label: "Container Name", Type: "text", DefaultValue: "redis-main", Required: true, SortOrder: 1},
		{TemplateVersionID: vmVersion.ID, Name: "redis_port", Label: "Redis Port", Type: "number", DefaultValue: "6379", Required: true, SortOrder: 2},
		{TemplateVersionID: vmVersion.ID, Name: "vm_port", Label: "VM Port", Type: "number", DefaultValue: "16379", Required: true, SortOrder: 3},
		{TemplateVersionID: vmVersion.ID, Name: "version", Label: "Version", Type: "text", DefaultValue: "7.2.0", Required: true, SortOrder: 4},
	}
	if err := db.Create(&vmParams).Error; err != nil {
		t.Fatalf("create vm parameters failed: %v", err)
	}

	encryption := NewCredentialHandler().encryptionService
	passwordPayload, err := encryption.EncryptCredentialData(map[string]interface{}{"username": "root", "password": "secret123"})
	if err != nil {
		t.Fatalf("encrypt vm credential failed: %v", err)
	}
	vmCredential := models.Credential{
		Name:             "vm-password-auth",
		Type:             models.TypePassword,
		Category:         models.CategoryCustom,
		Scope:            models.ScopeWorkspace,
		WorkspaceID:      workspace.ID,
		OwnerID:          maintainer.ID,
		EncryptedPayload: passwordPayload,
		Status:           models.CredentialStatusActive,
	}
	if err := db.Create(&vmCredential).Error; err != nil {
		t.Fatalf("create vm credential failed: %v", err)
	}
	vmResource := models.Resource{WorkspaceID: workspace.ID, Name: "vm-01", Type: models.ResourceTypeVM, Environment: "production", Status: models.ResourceStatusOnline, Endpoint: "10.0.0.8:22", CreatedBy: maintainer.ID}
	if err := db.Create(&vmResource).Error; err != nil {
		t.Fatalf("create vm resource failed: %v", err)
	}
	if err := db.Create(&models.ResourceCredentialBinding{WorkspaceID: workspace.ID, ResourceID: vmResource.ID, CredentialID: vmCredential.ID, Purpose: "primary", BoundBy: maintainer.ID}).Error; err != nil {
		t.Fatalf("bind vm resource credential failed: %v", err)
	}

	config.Init()
	config.Config.Set("server.public_url", "http://easydo.local")
	k8sTemplate := models.StoreTemplate{
		WorkspaceID:        workspace.ID,
		Name:               "Redis K8s",
		TemplateType:       models.StoreTemplateTypeApp,
		TargetResourceType: models.ResourceTypeK8sCluster,
		Source:             models.StoreTemplateSourceWorkspace,
		Status:             models.StoreTemplateStatusPublished,
		Category:           "cache",
		CreatedBy:          maintainer.ID,
	}
	if err := db.Create(&k8sTemplate).Error; err != nil {
		t.Fatalf("create k8s template failed: %v", err)
	}
	k8sVersion := models.StoreTemplateVersion{
		WorkspaceID:    workspace.ID,
		TemplateID:     k8sTemplate.ID,
		Version:        "19.6.0",
		DeploymentMode: "k8s_chart",
		DefaultConfig:  `{"schema_version":1,"infra_type":"k8s","version_description":"stable k8s variant","k8s":{"chart_source":{"type":"upload","chart_name":"redis","chart_version":"19.6.0","chart_file_name":"redis-19.6.0.tgz","chart_object_key":"store/charts/workspace-1/app-1/release-19/variant-19/redis-19.6.0.tgz"},"base_values":"architecture: standalone\nauth:\n  enabled: true\n"}}`,
		Status:         models.StoreTemplateStatusPublished,
		CreatedBy:      maintainer.ID,
	}
	if err := db.Create(&k8sVersion).Error; err != nil {
		t.Fatalf("create k8s version failed: %v", err)
	}
	k8sParams := []models.TemplateParameter{
		{TemplateVersionID: k8sVersion.ID, Name: "release_name", Label: "Release Name", Type: "text", DefaultValue: "redis", Required: true, SortOrder: 1},
		{TemplateVersionID: k8sVersion.ID, Name: "namespace", Label: "Namespace", Type: "text", DefaultValue: "default", Required: true, SortOrder: 2},
		{TemplateVersionID: k8sVersion.ID, Name: "auth.password", Label: "Password", Type: "text", DefaultValue: "", Required: true, SortOrder: 3},
	}
	if err := db.Create(&k8sParams).Error; err != nil {
		t.Fatalf("create k8s parameters failed: %v", err)
	}
	kubePayload, err := encryption.EncryptCredentialData(map[string]interface{}{
		"server":     "https://10.0.0.1:6443",
		"token":      "k8s-token",
		"kubeconfig": "apiVersion: v1\nclusters: []\ncontexts: []\ncurrent-context: \"\"\nusers: []\n",
	})
	if err != nil {
		t.Fatalf("encrypt k8s credential failed: %v", err)
	}
	k8sCredential := models.Credential{
		Name:             "cluster-auth",
		Type:             models.TypeToken,
		Category:         models.CategoryKubernetes,
		Scope:            models.ScopeWorkspace,
		WorkspaceID:      workspace.ID,
		OwnerID:          maintainer.ID,
		EncryptedPayload: kubePayload,
		Status:           models.CredentialStatusActive,
	}
	if err := db.Create(&k8sCredential).Error; err != nil {
		t.Fatalf("create k8s credential failed: %v", err)
	}
	k8sResource := models.Resource{WorkspaceID: workspace.ID, Name: "cluster-01", Type: models.ResourceTypeK8sCluster, Environment: "production", Status: models.ResourceStatusOnline, Endpoint: "https://10.0.0.1:6443", CreatedBy: maintainer.ID}
	if err := db.Create(&k8sResource).Error; err != nil {
		t.Fatalf("create k8s resource failed: %v", err)
	}
	if err := db.Create(&models.ResourceCredentialBinding{WorkspaceID: workspace.ID, ResourceID: k8sResource.ID, CredentialID: k8sCredential.ID, Purpose: "cluster_auth", BoundBy: maintainer.ID}).Error; err != nil {
		t.Fatalf("bind k8s resource credential failed: %v", err)
	}

	h := NewDeploymentHandler()
	vmReqBody := mustJSON(t, map[string]interface{}{
		"template_version_id": vmVersion.ID,
		"target_resource_id":  vmResource.ID,
		"parameters": map[string]interface{}{
			"container_name": "redis-prod",
			"vm_port":        26379,
		},
	})
	vmResp := performResourceStoreRequest(t, h.CreateDeploymentRequest, developer.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, http.MethodPost, "/api/deployments/requests", vmReqBody)
	if vmResp.Code != http.StatusOK {
		t.Fatalf("expected vm direct deployment request success, got=%d body=%s", vmResp.Code, vmResp.Body.String())
	}
	var vmReq models.DeploymentRequest
	if err := db.First(&vmReq, responseDataID(t, vmResp.Body.Bytes())).Error; err != nil {
		t.Fatalf("load vm deployment request failed: %v", err)
	}
	if vmReq.PipelineID == 0 {
		t.Fatalf("expected vm direct deployment request to persist hidden pipeline id")
	}
	if vmReq.LLMModelID != 0 {
		t.Fatalf("expected vm app deployment request llm_model_id to remain unset, got=%d", vmReq.LLMModelID)
	}
	var vmRun models.PipelineRun
	if err := db.First(&vmRun, vmReq.PipelineRunID).Error; err != nil {
		t.Fatalf("load vm pipeline run failed: %v", err)
	}
	if vmRun.PipelineID != vmReq.PipelineID {
		t.Fatalf("expected vm pipeline run to use deployment pipeline id=%d, got=%d", vmReq.PipelineID, vmRun.PipelineID)
	}
	var vmPipeline models.Pipeline
	if err := db.First(&vmPipeline, vmReq.PipelineID).Error; err != nil {
		t.Fatalf("load vm hidden pipeline failed: %v", err)
	}
	if !vmPipeline.ManagementHidden {
		t.Fatalf("expected vm direct deployment pipeline to be management hidden")
	}
	if !bytes.Contains([]byte(vmRun.Config), []byte(`docker run -d --name redis-prod -p 26379:6379 redis:7.2.0`)) {
		t.Fatalf("expected rendered vm command in pipeline run config, got=%s", vmRun.Config)
	}
	if !bytes.Contains([]byte(vmRun.Config), []byte(`"ssh_auth"`)) {
		t.Fatalf("expected vm pipeline run config to inject ssh auth binding, got=%s", vmRun.Config)
	}
	if bytes.Contains([]byte(vmRun.Config), []byte(`"user":"root"`)) {
		t.Fatalf("expected vm pipeline run config to avoid hardcoded root user, got=%s", vmRun.Config)
	}

	k8sReqBody := mustJSON(t, map[string]interface{}{
		"template_version_id": k8sVersion.ID,
		"target_resource_id":  k8sResource.ID,
		"parameters": map[string]interface{}{
			"release_name":  "redis-prod",
			"namespace":     "middleware",
			"auth.password": "secret-pass",
		},
	})
	k8sResp := performResourceStoreRequest(t, h.CreateDeploymentRequest, developer.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, http.MethodPost, "/api/deployments/requests", k8sReqBody)
	if k8sResp.Code != http.StatusOK {
		t.Fatalf("expected k8s direct deployment request success, got=%d body=%s", k8sResp.Code, k8sResp.Body.String())
	}
	var k8sReq models.DeploymentRequest
	if err := db.First(&k8sReq, responseDataID(t, k8sResp.Body.Bytes())).Error; err != nil {
		t.Fatalf("load k8s deployment request failed: %v", err)
	}
	if k8sReq.PipelineID == 0 {
		t.Fatalf("expected k8s direct deployment request to persist hidden pipeline id")
	}
	if k8sReq.LLMModelID != 0 {
		t.Fatalf("expected k8s app deployment request llm_model_id to remain unset, got=%d", k8sReq.LLMModelID)
	}
	var k8sRun models.PipelineRun
	if err := db.First(&k8sRun, k8sReq.PipelineRunID).Error; err != nil {
		t.Fatalf("load k8s pipeline run failed: %v", err)
	}
	if k8sRun.PipelineID != k8sReq.PipelineID {
		t.Fatalf("expected k8s pipeline run to use deployment pipeline id=%d, got=%d", k8sReq.PipelineID, k8sRun.PipelineID)
	}
	var k8sPipeline models.Pipeline
	if err := db.First(&k8sPipeline, k8sReq.PipelineID).Error; err != nil {
		t.Fatalf("load k8s hidden pipeline failed: %v", err)
	}
	if !k8sPipeline.ManagementHidden {
		t.Fatalf("expected k8s direct deployment pipeline to be management hidden")
	}
	if !bytes.Contains([]byte(k8sRun.Config), []byte(`helm --kubeconfig`)) || !bytes.Contains([]byte(k8sRun.Config), []byte(`redis-prod`)) {
		t.Fatalf("expected helm deploy command in pipeline run config, got=%s", k8sRun.Config)
	}
	if !bytes.Contains([]byte(k8sRun.Config), []byte(`cluster_auth`)) {
		t.Fatalf("expected k8s pipeline run config to inject cluster auth binding, got=%s", k8sRun.Config)
	}
}

func TestLLMModelHandler_AdminImportAndListCatalog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	admin, workspace := seedResourceStoreUserAndWorkspace(t, db, "catalog-admin", models.WorkspaceRoleOwner)
	admin.Role = "admin"
	if err := db.Save(&admin).Error; err != nil {
		t.Fatalf("update admin role failed: %v", err)
	}

	h := NewLLMModelHandler()
	importBody := mustJSON(t, map[string]interface{}{
		"source":          "huggingface",
		"source_model_id": "Qwen/Qwen2.5-7B-Instruct",
		"metadata": map[string]interface{}{
			"id":           "Qwen/Qwen2.5-7B-Instruct",
			"downloads":    1024,
			"likes":        88,
			"pipeline_tag": "text-generation",
			"tags":         []string{"transformers", "text-generation", "qwen"},
			"safetensors": map[string]interface{}{
				"parameters": map[string]interface{}{
					"BF16": float64(7600000000),
				},
			},
			"cardData": map[string]interface{}{
				"language":    []string{"en", "zh"},
				"license":     "apache-2.0",
				"base_model":  "Qwen2.5",
				"model_name":  "Qwen2.5-7B-Instruct",
				"description": "Instruction tuned Qwen model",
			},
		},
	})
	importResp := performResourceStoreRequest(t, h.ImportModel, admin.ID, "admin", workspace.ID, models.WorkspaceRoleOwner, http.MethodPost, "/api/store/llm-models/import", importBody)
	if importResp.Code != http.StatusOK {
		t.Fatalf("expected import model success, got=%d body=%s", importResp.Code, importResp.Body.String())
	}

	listResp := performResourceStoreRequest(t, h.ListModels, admin.ID, "admin", workspace.ID, models.WorkspaceRoleOwner, http.MethodGet, "/api/store/llm-models", nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected list models success, got=%d body=%s", listResp.Code, listResp.Body.String())
	}
	if !bytes.Contains(listResp.Body.Bytes(), []byte("Qwen/Qwen2.5-7B-Instruct")) || !bytes.Contains(listResp.Body.Bytes(), []byte("apache-2.0")) {
		t.Fatalf("expected imported model metadata in list response, got=%s", listResp.Body.String())
	}
	if !bytes.Contains(listResp.Body.Bytes(), []byte("huggingface")) {
		t.Fatalf("expected model source in list response, got=%s", listResp.Body.String())
	}
	if !bytes.Contains(listResp.Body.Bytes(), []byte(`"parameter_size":"7.60B"`)) {
		t.Fatalf("expected imported parameter_size in list response, got=%s", listResp.Body.String())
	}
}

func TestLLMModelHandler_ListModelsBackfillsParameterSizeFromMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	admin, workspace := seedResourceStoreUserAndWorkspace(t, db, "catalog-backfill-admin", models.WorkspaceRoleOwner)
	admin.Role = "admin"
	if err := db.Save(&admin).Error; err != nil {
		t.Fatalf("update admin role failed: %v", err)
	}
	metadataBytes, err := json.Marshal(map[string]interface{}{
		"id": "Qwen/Qwen3.5-2B",
		"safetensors": map[string]interface{}{
			"parameters": map[string]interface{}{
				"BF16": float64(2274067232),
				"F32":  float64(2592),
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal metadata failed: %v", err)
	}
	model := models.LLMModelCatalog{
		Name:          "Qwen3.5-2B",
		DisplayName:   "Qwen3.5-2B",
		Source:        "huggingface",
		SourceModelID: "Qwen/Qwen3.5-2B",
		Metadata:      string(metadataBytes),
		ImportedBy:    admin.ID,
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewLLMModelHandler()
	listResp := performResourceStoreRequest(t, h.ListModels, admin.ID, "admin", workspace.ID, models.WorkspaceRoleOwner, http.MethodGet, "/api/store/llm-models", nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected list models success, got=%d body=%s", listResp.Code, listResp.Body.String())
	}
	if !bytes.Contains(listResp.Body.Bytes(), []byte(`"parameter_size":"2.27B"`)) {
		t.Fatalf("expected parameter_size backfilled from metadata, got=%s", listResp.Body.String())
	}
}

func TestLLMModelHandler_ReimportUpdatesParameterSize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	admin, workspace := seedResourceStoreUserAndWorkspace(t, db, "catalog-reimport-admin", models.WorkspaceRoleOwner)
	admin.Role = "admin"
	if err := db.Save(&admin).Error; err != nil {
		t.Fatalf("update admin role failed: %v", err)
	}

	originalMetadata := map[string]interface{}{
		"id": "Qwen/Qwen2.5-7B-Instruct",
		"safetensors": map[string]interface{}{
			"parameters": map[string]interface{}{
				"BF16": float64(7600000000),
			},
		},
	}
	original := models.LLMModelCatalog{
		Name:          "Qwen2.5-7B-Instruct",
		DisplayName:   "Qwen2.5-7B-Instruct",
		Source:        "huggingface",
		SourceModelID: "Qwen/Qwen2.5-7B-Instruct",
		ParameterSize: "7.60B",
		Metadata:      string(mustJSON(t, originalMetadata)),
		ImportedBy:    admin.ID,
	}
	if err := db.Create(&original).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewLLMModelHandler()
	reimportBody := mustJSON(t, map[string]interface{}{
		"source":          "huggingface",
		"source_model_id": "Qwen/Qwen2.5-7B-Instruct",
		"metadata": map[string]interface{}{
			"id": "Qwen/Qwen2.5-7B-Instruct",
			"safetensors": map[string]interface{}{
				"parameters": map[string]interface{}{
					"BF16": float64(14200000000),
				},
			},
		},
	})
	reimportResp := performResourceStoreRequest(t, h.ImportModel, admin.ID, "admin", workspace.ID, models.WorkspaceRoleOwner, http.MethodPost, "/api/store/llm-models/import", reimportBody)
	if reimportResp.Code != http.StatusOK {
		t.Fatalf("expected reimport model success, got=%d body=%s", reimportResp.Code, reimportResp.Body.String())
	}

	var stored models.LLMModelCatalog
	if err := db.First(&stored, original.ID).Error; err != nil {
		t.Fatalf("load reimported llm model failed: %v", err)
	}
	if stored.ParameterSize != "14.2B" {
		t.Fatalf("expected reimport to refresh parameter_size, got=%q", stored.ParameterSize)
	}
	if !bytes.Contains(reimportResp.Body.Bytes(), []byte(`"parameter_size":"14.2B"`)) {
		t.Fatalf("expected reimport response to include refreshed parameter_size, got=%s", reimportResp.Body.String())
	}
}

func TestLLMModelHandler_ImportModelScopeModelResolvesNestedParameterSize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	admin, workspace := seedResourceStoreUserAndWorkspace(t, db, "catalog-modelscope-admin", models.WorkspaceRoleOwner)
	admin.Role = "admin"
	if err := db.Save(&admin).Error; err != nil {
		t.Fatalf("update admin role failed: %v", err)
	}

	h := NewLLMModelHandler()
	importBody := mustJSON(t, map[string]interface{}{
		"source":          "modelscope",
		"source_model_id": "Qwen/Qwen3.5-27B",
		"metadata": map[string]interface{}{
			"Name":        "Qwen3.5-27B",
			"ChineseName": "千问3.5-27B",
			"License":     "Apache License 2.0",
			"ModelInfos": map[string]interface{}{
				"safetensor": map[string]interface{}{
					"model_size": "27B",
				},
			},
		},
	})
	importResp := performResourceStoreRequest(t, h.ImportModel, admin.ID, "admin", workspace.ID, models.WorkspaceRoleOwner, http.MethodPost, "/api/store/llm-models/import", importBody)
	if importResp.Code != http.StatusOK {
		t.Fatalf("expected ModelScope import success, got=%d body=%s", importResp.Code, importResp.Body.String())
	}
	if !bytes.Contains(importResp.Body.Bytes(), []byte(`"parameter_size":"27.0B"`)) {
		t.Fatalf("expected ModelScope import response to include nested parameter_size, got=%s", importResp.Body.String())
	}

	listResp := performResourceStoreRequest(t, h.ListModels, admin.ID, "admin", workspace.ID, models.WorkspaceRoleOwner, http.MethodGet, "/api/store/llm-models", nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected list models success, got=%d body=%s", listResp.Code, listResp.Body.String())
	}
	if !bytes.Contains(listResp.Body.Bytes(), []byte(`"parameter_size":"27.0B"`)) {
		t.Fatalf("expected list models to surface nested ModelScope parameter_size, got=%s", listResp.Body.String())
	}
}

func TestDeploymentHandler_CreateDeploymentRequestSnapshotsSelectedLLMModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "llm-deploy-maintainer", models.WorkspaceRoleMaintainer)
	developer := seedResourceStoreMember(t, db, workspace.ID, "llm-deploy-developer", models.WorkspaceRoleDeveloper)

	project := models.Project{Name: "llm-proj", WorkspaceID: workspace.ID, OwnerID: maintainer.ID}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project failed: %v", err)
	}

	pipelineConfig := PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{{
			ID:   "deploy",
			Type: "ssh",
			Name: "SSH Deploy",
			Config: map[string]interface{}{
				"host":   "${inputs.resource_host}",
				"user":   "root",
				"script": "docker run -d --name ${inputs.model_name} -e MODEL_REF=${inputs.model_source_ref} -e GPU_MEM=${inputs.gpu_memory_utilization} ollama/ollama:${inputs.image_tag}",
			},
		}},
		Edges: []PipelineEdge{},
	}
	configJSON, _ := json.Marshal(pipelineConfig)
	pipeline := models.Pipeline{
		Name:             "llm-hidden-pipeline",
		WorkspaceID:      workspace.ID,
		ProjectID:        project.ID,
		OwnerID:          maintainer.ID,
		Config:           string(configJSON),
		ManagementHidden: true,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	projectID := project.ID
	resource := models.Resource{WorkspaceID: workspace.ID, ProjectID: &projectID, Name: "llm-vm-01", Type: models.ResourceTypeVM, Environment: "production", Status: models.ResourceStatusOnline, Endpoint: "10.0.0.9:22", CreatedBy: maintainer.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "Ollama", TemplateType: models.StoreTemplateTypeLLM, TargetResourceType: models.ResourceTypeVM, Source: models.StoreTemplateSourceWorkspace, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}
	version := models.StoreTemplateVersion{WorkspaceID: workspace.ID, TemplateID: template.ID, PipelineID: pipeline.ID, Version: "1.0.0", DeploymentMode: "pipeline", Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&version).Error; err != nil {
		t.Fatalf("create version failed: %v", err)
	}
	model := models.LLMModelCatalog{
		Name:          "Qwen2.5-7B-Instruct",
		Source:        "huggingface",
		SourceModelID: "Qwen/Qwen2.5-7B-Instruct",
		Summary:       "Instruction tuned model",
		License:       "apache-2.0",
		Metadata:      `{"pipeline_tag":"text-generation"}`,
		ImportedBy:    maintainer.ID,
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{
		"template_version_id": version.ID,
		"target_resource_id":  resource.ID,
		"llm_model_id":        model.ID,
		"parameters": map[string]interface{}{
			"image_tag":              "latest",
			"gpu_memory_utilization": "0.95",
		},
	})
	createResp := performResourceStoreRequest(t, h.CreateDeploymentRequest, developer.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, http.MethodPost, "/api/deployments/requests", requestBody)
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected create deployment request success, got=%d body=%s", createResp.Code, createResp.Body.String())
	}

	var req models.DeploymentRequest
	if err := db.First(&req, responseDataID(t, createResp.Body.Bytes())).Error; err != nil {
		t.Fatalf("load deployment request failed: %v", err)
	}
	if req.LLMModelID != model.ID {
		t.Fatalf("expected deployment request llm_model_id=%d, got=%d", model.ID, req.LLMModelID)
	}
	if !bytes.Contains([]byte(req.LLMModelSnapshot), []byte("Qwen/Qwen2.5-7B-Instruct")) {
		t.Fatalf("expected llm model snapshot to include source model id, got=%s", req.LLMModelSnapshot)
	}
	var run models.PipelineRun
	if err := db.First(&run, req.PipelineRunID).Error; err != nil {
		t.Fatalf("load pipeline run failed: %v", err)
	}
	if !bytes.Contains([]byte(run.Config), []byte("Qwen/Qwen2.5-7B-Instruct")) || !bytes.Contains([]byte(run.Config), []byte("0.95")) {
		t.Fatalf("expected resolved llm model/tool parameters in pipeline config, got=%s", run.Config)
	}
}

func TestDeploymentHandler_CreateDeploymentRequestResolvesPlatformLikeVLLMDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "llm-vllm-maintainer", models.WorkspaceRoleMaintainer)
	developer := seedResourceStoreMember(t, db, workspace.ID, "llm-vllm-developer", models.WorkspaceRoleDeveloper)

	project := models.Project{Name: "vllm-proj", WorkspaceID: workspace.ID, OwnerID: maintainer.ID}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project failed: %v", err)
	}

	pipelineConfig := PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{{
			ID:   "deploy",
			Type: "ssh",
			Name: "vLLM SSH Deploy",
			Config: map[string]interface{}{
				"host":   "${inputs.resource_host}",
				"user":   "root",
				"script": "vllm serve ${inputs.runtime_model_path} --host ${inputs.host} --port ${inputs.port} --load-format ${inputs.load_format} --gpu-memory-utilization ${inputs.gpu_memory_utilization} --quantization ${inputs.quantization} --mount ${inputs.host_model_dir}:${inputs.container_model_dir}",
			},
		}},
		Edges: []PipelineEdge{},
	}
	configJSON, _ := json.Marshal(pipelineConfig)
	pipeline := models.Pipeline{
		Name:             "platform-vllm-hidden-pipeline",
		WorkspaceID:      workspace.ID,
		ProjectID:        project.ID,
		OwnerID:          maintainer.ID,
		Config:           string(configJSON),
		ManagementHidden: true,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	projectID := project.ID
	resource := models.Resource{WorkspaceID: workspace.ID, ProjectID: &projectID, Name: "vllm-vm-01", Type: models.ResourceTypeVM, Environment: "production", Status: models.ResourceStatusOnline, Endpoint: "10.0.0.9:22", CreatedBy: maintainer.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "vLLM", TemplateType: models.StoreTemplateTypeLLM, TargetResourceType: models.ResourceTypeVM, Source: models.StoreTemplateSourceWorkspace, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}
	version := models.StoreTemplateVersion{WorkspaceID: workspace.ID, TemplateID: template.ID, PipelineID: pipeline.ID, Version: "2.0.0", DeploymentMode: "pipeline", Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&version).Error; err != nil {
		t.Fatalf("create version failed: %v", err)
	}
	parameters := []models.TemplateParameter{
		{TemplateVersionID: version.ID, Name: "host", Label: "监听地址", Type: "text", DefaultValue: "0.0.0.0", Required: false, SortOrder: 1},
		{TemplateVersionID: version.ID, Name: "port", Label: "服务端口", Type: "number", DefaultValue: "8000", Required: false, SortOrder: 2},
		{TemplateVersionID: version.ID, Name: "host_model_dir", Label: "宿主机模型目录", Type: "text", DefaultValue: "", Required: false, SortOrder: 3},
		{TemplateVersionID: version.ID, Name: "container_model_dir", Label: "容器模型目录", Type: "text", DefaultValue: "", Required: false, SortOrder: 4},
		{TemplateVersionID: version.ID, Name: "model_mount_readonly", Label: "只读挂载", Type: "switch", DefaultValue: "true", Required: false, SortOrder: 5},
		{TemplateVersionID: version.ID, Name: "load_format", Label: "模型加载格式", Type: "select", DefaultValue: "auto", OptionValues: `["auto","safetensors"]`, Required: false, SortOrder: 6},
		{TemplateVersionID: version.ID, Name: "gpu_memory_utilization", Label: "GPU 利用率", Type: "number", DefaultValue: "0.9", Required: false, SortOrder: 7},
		{TemplateVersionID: version.ID, Name: "quantization", Label: "量化方式", Type: "select", DefaultValue: "", OptionValues: `["","awq","gptq"]`, Required: false, SortOrder: 8},
	}
	if err := db.Create(&parameters).Error; err != nil {
		t.Fatalf("create vllm parameters failed: %v", err)
	}
	model := models.LLMModelCatalog{Name: "Qwen2.5-7B-Instruct", Source: "huggingface", SourceModelID: "Qwen/Qwen2.5-7B-Instruct", ImportedBy: maintainer.ID}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{
		"template_version_id": version.ID,
		"target_resource_id":  resource.ID,
		"llm_model_id":        model.ID,
		"parameters": map[string]interface{}{
			"model_path":             "/srv/models/Qwen/Qwen2.5-7B-Instruct",
			"host_model_dir":         "/srv/models",
			"container_model_dir":    "/models",
			"model_mount_readonly":   true,
			"gpu_memory_utilization": "0.95",
			"quantization":           "awq",
		},
	})
	createResp := performResourceStoreRequest(t, h.CreateDeploymentRequest, developer.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, http.MethodPost, "/api/deployments/requests", requestBody)
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected create deployment request success, got=%d body=%s", createResp.Code, createResp.Body.String())
	}

	var req models.DeploymentRequest
	if err := db.First(&req, responseDataID(t, createResp.Body.Bytes())).Error; err != nil {
		t.Fatalf("load deployment request failed: %v", err)
	}
	if !bytes.Contains([]byte(req.ParameterSnapshot), []byte(`"host":"0.0.0.0"`)) {
		t.Fatalf("expected parameter snapshot to include defaulted host, got=%s", req.ParameterSnapshot)
	}
	if !bytes.Contains([]byte(req.ParameterSnapshot), []byte(`"port":8000`)) {
		t.Fatalf("expected parameter snapshot to include defaulted port, got=%s", req.ParameterSnapshot)
	}
	if !bytes.Contains([]byte(req.ParameterSnapshot), []byte(`"load_format":"auto"`)) {
		t.Fatalf("expected parameter snapshot to include defaulted load_format, got=%s", req.ParameterSnapshot)
	}
	if !bytes.Contains([]byte(req.ParameterSnapshot), []byte(`"host_model_dir":"/srv/models"`)) {
		t.Fatalf("expected parameter snapshot to include host model dir, got=%s", req.ParameterSnapshot)
	}
	if !bytes.Contains([]byte(req.ParameterSnapshot), []byte(`"container_model_dir":"/models"`)) {
		t.Fatalf("expected parameter snapshot to include container model dir, got=%s", req.ParameterSnapshot)
	}
	if !bytes.Contains([]byte(req.ParameterSnapshot), []byte(`"model_mount_readonly":true`)) {
		t.Fatalf("expected parameter snapshot to include readonly mount switch, got=%s", req.ParameterSnapshot)
	}
	if !bytes.Contains([]byte(req.ParameterSnapshot), []byte(`"runtime_model_path":"/models/Qwen/Qwen2.5-7B-Instruct"`)) {
		t.Fatalf("expected parameter snapshot to include resolved runtime model path, got=%s", req.ParameterSnapshot)
	}

	var run models.PipelineRun
	if err := db.First(&run, req.PipelineRunID).Error; err != nil {
		t.Fatalf("load pipeline run failed: %v", err)
	}
	if !bytes.Contains([]byte(run.Config), []byte("--host 0.0.0.0 --port 8000 --load-format auto --gpu-memory-utilization 0.95 --quantization awq")) {
		t.Fatalf("expected resolved vllm parameters in pipeline config, got=%s", run.Config)
	}
	if !bytes.Contains([]byte(run.Config), []byte(`--mount /srv/models:/models`)) {
		t.Fatalf("expected resolved mount paths in pipeline config, got=%s", run.Config)
	}
	if !bytes.Contains([]byte(run.Config), []byte(`vllm serve /models/Qwen/Qwen2.5-7B-Instruct`)) {
		t.Fatalf("expected runtime model path in pipeline config, got=%s", run.Config)
	}
}

func TestDeploymentHandler_CreateDeploymentRequestResolvesEmptyOptionalVLLMInputs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "llm-vllm-empty-maintainer", models.WorkspaceRoleMaintainer)
	developer := seedResourceStoreMember(t, db, workspace.ID, "llm-vllm-empty-developer", models.WorkspaceRoleDeveloper)

	project := models.Project{Name: "vllm-empty-proj", WorkspaceID: workspace.ID, OwnerID: maintainer.ID}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project failed: %v", err)
	}

	pipelineConfig := PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{{
			ID:   "deploy",
			Type: "ssh",
			Name: "vLLM SSH Deploy",
			Config: map[string]interface{}{
				"host":   "${inputs.resource_host}",
				"script": "vllm serve ${inputs.runtime_model_path} --quantization ${inputs.quantization} --download-dir ${inputs.download_dir} --revision ${inputs.revision}",
			},
		}},
		Edges: []PipelineEdge{},
	}
	configJSON, _ := json.Marshal(pipelineConfig)
	pipeline := models.Pipeline{
		Name:             "platform-vllm-empty-hidden-pipeline",
		WorkspaceID:      workspace.ID,
		ProjectID:        project.ID,
		OwnerID:          maintainer.ID,
		Config:           string(configJSON),
		ManagementHidden: true,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	projectID := project.ID
	resource := models.Resource{WorkspaceID: workspace.ID, ProjectID: &projectID, Name: "vllm-vm-empty-01", Type: models.ResourceTypeVM, Environment: "production", Status: models.ResourceStatusOnline, Endpoint: "10.0.0.9:22", CreatedBy: maintainer.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "vLLM empty", TemplateType: models.StoreTemplateTypeLLM, TargetResourceType: models.ResourceTypeVM, Source: models.StoreTemplateSourceWorkspace, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}
	version := models.StoreTemplateVersion{WorkspaceID: workspace.ID, TemplateID: template.ID, PipelineID: pipeline.ID, Version: "2.0.0", DeploymentMode: "pipeline", Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&version).Error; err != nil {
		t.Fatalf("create version failed: %v", err)
	}
	parameters := []models.TemplateParameter{
		{TemplateVersionID: version.ID, Name: "quantization", Label: "量化方式", Type: "select", DefaultValue: "", OptionValues: `["","awq","gptq"]`, Required: false, SortOrder: 1},
		{TemplateVersionID: version.ID, Name: "download_dir", Label: "下载目录", Type: "text", DefaultValue: "", Required: false, SortOrder: 2},
		{TemplateVersionID: version.ID, Name: "revision", Label: "模型版本", Type: "text", DefaultValue: "", Required: false, SortOrder: 3},
	}
	if err := db.Create(&parameters).Error; err != nil {
		t.Fatalf("create vllm parameters failed: %v", err)
	}
	model := models.LLMModelCatalog{Name: "Qwen2.5-7B-Instruct", Source: "huggingface", SourceModelID: "Qwen/Qwen2.5-7B-Instruct", ImportedBy: maintainer.ID}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{
		"template_version_id": version.ID,
		"target_resource_id":  resource.ID,
		"llm_model_id":        model.ID,
		"parameters": map[string]interface{}{
			"model_path": "/srv/models/Qwen/Qwen2.5-7B-Instruct",
		},
	})
	createResp := performResourceStoreRequest(t, h.CreateDeploymentRequest, developer.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, http.MethodPost, "/api/deployments/requests", requestBody)
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected create deployment request success, got=%d body=%s", createResp.Code, createResp.Body.String())
	}

	var req models.DeploymentRequest
	if err := db.First(&req, responseDataID(t, createResp.Body.Bytes())).Error; err != nil {
		t.Fatalf("load deployment request failed: %v", err)
	}
	if !bytes.Contains([]byte(req.ParameterSnapshot), []byte(`"quantization":""`)) {
		t.Fatalf("expected parameter snapshot to include empty quantization, got=%s", req.ParameterSnapshot)
	}

	var run models.PipelineRun
	if err := db.First(&run, req.PipelineRunID).Error; err != nil {
		t.Fatalf("load pipeline run failed: %v", err)
	}
	if bytes.Contains([]byte(run.Config), []byte(`${inputs.quantization}`)) || bytes.Contains([]byte(run.Config), []byte(`${inputs.download_dir}`)) || bytes.Contains([]byte(run.Config), []byte(`${inputs.revision}`)) {
		t.Fatalf("expected optional inputs to be fully resolved in pipeline config, got=%s", run.Config)
	}
	if !bytes.Contains([]byte(run.Config), []byte(`--quantization `)) {
		t.Fatalf("expected pipeline config to keep quantization flag text after empty substitution, got=%s", run.Config)
	}
}

func TestDeploymentHandler_CreateDeploymentRequestAllowsRemoteVLLMModelWithoutMountPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "llm-vllm-remote-maintainer", models.WorkspaceRoleMaintainer)
	developer := seedResourceStoreMember(t, db, workspace.ID, "llm-vllm-remote-developer", models.WorkspaceRoleDeveloper)

	project := models.Project{Name: "vllm-remote-proj", WorkspaceID: workspace.ID, OwnerID: maintainer.ID}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project failed: %v", err)
	}

	pipelineConfig := PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{{
			ID:   "deploy",
			Type: "ssh",
			Name: "vLLM SSH Deploy",
			Config: map[string]interface{}{
				"host":   "${inputs.resource_host}",
				"script": "vllm serve ${inputs.model_source_ref}",
			},
		}},
		Edges: []PipelineEdge{},
	}
	configJSON, _ := json.Marshal(pipelineConfig)
	pipeline := models.Pipeline{
		Name:             "platform-vllm-remote-hidden-pipeline",
		WorkspaceID:      workspace.ID,
		ProjectID:        project.ID,
		OwnerID:          maintainer.ID,
		Config:           string(configJSON),
		ManagementHidden: true,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	projectID := project.ID
	resource := models.Resource{WorkspaceID: workspace.ID, ProjectID: &projectID, Name: "vllm-vm-remote-01", Type: models.ResourceTypeVM, Environment: "production", Status: models.ResourceStatusOnline, Endpoint: "10.0.0.9:22", CreatedBy: maintainer.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "vLLM", TemplateType: models.StoreTemplateTypeLLM, TargetResourceType: models.ResourceTypeVM, Source: models.StoreTemplateSourceWorkspace, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}
	version := models.StoreTemplateVersion{WorkspaceID: workspace.ID, TemplateID: template.ID, PipelineID: pipeline.ID, Version: "3.0.0", DeploymentMode: "pipeline", Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&version).Error; err != nil {
		t.Fatalf("create version failed: %v", err)
	}
	parameters := []models.TemplateParameter{
		{TemplateVersionID: version.ID, Name: "host", Label: "监听地址", Type: "text", DefaultValue: "0.0.0.0", Required: false, SortOrder: 1},
		{TemplateVersionID: version.ID, Name: "port", Label: "服务端口", Type: "number", DefaultValue: "8000", Required: false, SortOrder: 2},
		{TemplateVersionID: version.ID, Name: "host_model_dir", Label: "宿主机模型目录", Type: "text", DefaultValue: "", Required: false, SortOrder: 3},
		{TemplateVersionID: version.ID, Name: "container_model_dir", Label: "容器模型目录", Type: "text", DefaultValue: "", Required: false, SortOrder: 4},
	}
	if err := db.Create(&parameters).Error; err != nil {
		t.Fatalf("create vllm parameters failed: %v", err)
	}
	model := models.LLMModelCatalog{Name: "Qwen2.5-7B-Instruct", Source: "huggingface", SourceModelID: "Qwen/Qwen2.5-7B-Instruct", ImportedBy: maintainer.ID}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{
		"template_version_id": version.ID,
		"target_resource_id":  resource.ID,
		"llm_model_id":        model.ID,
		"parameters":          map[string]interface{}{},
	})
	createResp := performResourceStoreRequest(t, h.CreateDeploymentRequest, developer.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, http.MethodPost, "/api/deployments/requests", requestBody)
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected create deployment request success without mount paths, got=%d body=%s", createResp.Code, createResp.Body.String())
	}

	var req models.DeploymentRequest
	if err := db.First(&req, responseDataID(t, createResp.Body.Bytes())).Error; err != nil {
		t.Fatalf("load deployment request failed: %v", err)
	}
	if !bytes.Contains([]byte(req.ParameterSnapshot), []byte(`"runtime_model_path":""`)) {
		t.Fatalf("expected empty runtime model path when model_path is omitted, got=%s", req.ParameterSnapshot)
	}

	var run models.PipelineRun
	if err := db.First(&run, req.PipelineRunID).Error; err != nil {
		t.Fatalf("load pipeline run failed: %v", err)
	}
	if !bytes.Contains([]byte(run.Config), []byte(`vllm serve Qwen/Qwen2.5-7B-Instruct`)) {
		t.Fatalf("expected pipeline config to fall back to model_source_ref, got=%s", run.Config)
	}
}

func TestDeploymentHandler_CreateDeploymentRequestResolvesEmptyRemoteVLLMMountPlaceholders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "llm-vllm-platform-maintainer", models.WorkspaceRoleMaintainer)
	developer := seedResourceStoreMember(t, db, workspace.ID, "llm-vllm-platform-developer", models.WorkspaceRoleDeveloper)

	project := models.Project{Name: "vllm-platform-proj", WorkspaceID: workspace.ID, OwnerID: maintainer.ID}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project failed: %v", err)
	}

	pipelineConfig := PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{{
			ID:   "deploy",
			Type: "ssh",
			Name: "vLLM SSH Deploy",
			Config: map[string]interface{}{
				"host":   "${inputs.resource_host}",
				"script": "MODEL_REF=\"${inputs.runtime_model_path}\"\nif [ -z \"$MODEL_REF\" ]; then\n  MODEL_REF=\"${inputs.model_path}\"\nfi\nif [ -z \"$MODEL_REF\" ]; then\n  MODEL_REF=\"${inputs.model_source_ref}\"\nfi\nMOUNT_ARGS=\"\"\nif [ -n \"${inputs.host_model_dir}\" ] && [ -n \"${inputs.container_model_dir}\" ]; then\n  MOUNT_ARGS=\"--mount type=bind,src=${inputs.host_model_dir},dst=${inputs.container_model_dir}\"\nfi\nvllm serve $MODEL_REF $MOUNT_ARGS",
			},
		}},
		Edges: []PipelineEdge{},
	}
	configJSON, _ := json.Marshal(pipelineConfig)
	pipeline := models.Pipeline{
		Name:             "platform-vllm-remote-placeholders-pipeline",
		WorkspaceID:      workspace.ID,
		ProjectID:        project.ID,
		OwnerID:          maintainer.ID,
		Config:           string(configJSON),
		ManagementHidden: true,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	projectID := project.ID
	resource := models.Resource{WorkspaceID: workspace.ID, ProjectID: &projectID, Name: "vllm-vm-platform-01", Type: models.ResourceTypeVM, Environment: "production", Status: models.ResourceStatusOnline, Endpoint: "10.0.0.9:22", CreatedBy: maintainer.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "vLLM", TemplateType: models.StoreTemplateTypeLLM, TargetResourceType: models.ResourceTypeVM, Source: models.StoreTemplateSourceWorkspace, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}
	version := models.StoreTemplateVersion{WorkspaceID: workspace.ID, TemplateID: template.ID, PipelineID: pipeline.ID, Version: "4.0.0", DeploymentMode: "pipeline", Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&version).Error; err != nil {
		t.Fatalf("create version failed: %v", err)
	}
	parameters := []models.TemplateParameter{
		{TemplateVersionID: version.ID, Name: "host_model_dir", Label: "宿主机模型目录", Type: "text", DefaultValue: "", Required: false, SortOrder: 1},
		{TemplateVersionID: version.ID, Name: "container_model_dir", Label: "容器模型目录", Type: "text", DefaultValue: "", Required: false, SortOrder: 2},
	}
	if err := db.Create(&parameters).Error; err != nil {
		t.Fatalf("create vllm parameters failed: %v", err)
	}
	model := models.LLMModelCatalog{Name: "Qwen2.5-7B-Instruct", Source: "huggingface", SourceModelID: "Qwen/Qwen2.5-7B-Instruct", ImportedBy: maintainer.ID}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{
		"template_version_id": version.ID,
		"target_resource_id":  resource.ID,
		"llm_model_id":        model.ID,
		"parameters":          map[string]interface{}{},
	})
	createResp := performResourceStoreRequest(t, h.CreateDeploymentRequest, developer.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, http.MethodPost, "/api/deployments/requests", requestBody)
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected create deployment request success, got=%d body=%s", createResp.Code, createResp.Body.String())
	}

	var req models.DeploymentRequest
	if err := db.First(&req, responseDataID(t, createResp.Body.Bytes())).Error; err != nil {
		t.Fatalf("load deployment request failed: %v", err)
	}
	var run models.PipelineRun
	if err := db.First(&run, req.PipelineRunID).Error; err != nil {
		t.Fatalf("load pipeline run failed: %v", err)
	}
	if bytes.Contains([]byte(run.Config), []byte(`${inputs.`)) {
		t.Fatalf("expected remote vllm placeholders to resolve to concrete strings, got=%s", run.Config)
	}
	if !bytes.Contains([]byte(run.Config), []byte(`MODEL_REF=\"\"`)) {
		t.Fatalf("expected missing runtime model path to resolve to empty string, got=%s", run.Config)
	}
	if !bytes.Contains([]byte(run.Config), []byte(`vllm serve $MODEL_REF $MOUNT_ARGS`)) {
		t.Fatalf("expected final command to remain intact, got=%s", run.Config)
	}
}

func TestDeploymentHandler_CreateDeploymentRequestUsesUserProvidedMountedPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "llm-vllm-mounted-userpath-maintainer", models.WorkspaceRoleMaintainer)
	developer := seedResourceStoreMember(t, db, workspace.ID, "llm-vllm-mounted-userpath-developer", models.WorkspaceRoleDeveloper)

	project := models.Project{Name: "vllm-mounted-userpath-proj", WorkspaceID: workspace.ID, OwnerID: maintainer.ID}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project failed: %v", err)
	}

	pipelineConfig := PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{{
			ID:   "deploy",
			Type: "ssh",
			Name: "vLLM SSH Deploy",
			Config: map[string]interface{}{
				"host":   "${inputs.resource_host}",
				"script": "MODEL_REF=\"${inputs.runtime_model_path}\"\nif [ -z \"$MODEL_REF\" ]; then\n  MODEL_REF=\"${inputs.model_path}\"\nfi\nif [ -z \"$MODEL_REF\" ]; then\n  MODEL_REF=\"${inputs.model_source_ref}\"\nfi\nMOUNT_ARGS=\"\"\nif [ -n \"${inputs.host_model_dir}\" ] && [ -n \"${inputs.container_model_dir}\" ]; then\n  MOUNT_ARGS=\"--mount type=bind,src=${inputs.host_model_dir},dst=${inputs.container_model_dir}\"\nfi\nvllm serve $MODEL_REF $MOUNT_ARGS",
			},
		}},
		Edges: []PipelineEdge{},
	}
	configJSON, _ := json.Marshal(pipelineConfig)
	pipeline := models.Pipeline{
		Name:             "platform-vllm-mounted-userpath-pipeline",
		WorkspaceID:      workspace.ID,
		ProjectID:        project.ID,
		OwnerID:          maintainer.ID,
		Config:           string(configJSON),
		ManagementHidden: true,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	projectID := project.ID
	resource := models.Resource{WorkspaceID: workspace.ID, ProjectID: &projectID, Name: "vllm-vm-mounted-userpath-01", Type: models.ResourceTypeVM, Environment: "production", Status: models.ResourceStatusOnline, Endpoint: "10.0.0.9:22", CreatedBy: maintainer.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "vLLM", TemplateType: models.StoreTemplateTypeLLM, TargetResourceType: models.ResourceTypeVM, Source: models.StoreTemplateSourcePlatform, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}
	version := models.StoreTemplateVersion{WorkspaceID: workspace.ID, TemplateID: template.ID, PipelineID: pipeline.ID, Version: "6.0.0", DeploymentMode: "pipeline", Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&version).Error; err != nil {
		t.Fatalf("create version failed: %v", err)
	}
	parameters := []models.TemplateParameter{
		{TemplateVersionID: version.ID, Name: "host_model_dir", Label: "宿主机模型目录", Type: "text", DefaultValue: "", Required: false, SortOrder: 1},
		{TemplateVersionID: version.ID, Name: "container_model_dir", Label: "容器模型目录", Type: "text", DefaultValue: "", Required: false, SortOrder: 2},
	}
	if err := db.Create(&parameters).Error; err != nil {
		t.Fatalf("create vllm parameters failed: %v", err)
	}
	model := models.LLMModelCatalog{Name: "Qwen2.5-7B-Instruct", Source: "huggingface", SourceModelID: "Qwen/Qwen2.5-7B-Instruct", ImportedBy: maintainer.ID}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{
		"template_version_id": version.ID,
		"target_resource_id":  resource.ID,
		"llm_model_id":        model.ID,
		"parameters": map[string]interface{}{
			"host_model_dir":      "/srv/models/custom/Qwen3.5-2B",
			"container_model_dir": "/models/custom/Qwen3.5-2B",
		},
	})
	createResp := performResourceStoreRequest(t, h.CreateDeploymentRequest, developer.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, http.MethodPost, "/api/deployments/requests", requestBody)
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected create deployment request success, got=%d body=%s", createResp.Code, createResp.Body.String())
	}

	var req models.DeploymentRequest
	if err := db.First(&req, responseDataID(t, createResp.Body.Bytes())).Error; err != nil {
		t.Fatalf("load deployment request failed: %v", err)
	}
	if !bytes.Contains([]byte(req.ParameterSnapshot), []byte(`"model_path":"/srv/models/custom/Qwen3.5-2B"`)) {
		t.Fatalf("expected parameter snapshot to use user-provided host model path, got=%s", req.ParameterSnapshot)
	}
	if !bytes.Contains([]byte(req.ParameterSnapshot), []byte(`"runtime_model_path":"/models/custom/Qwen3.5-2B"`)) {
		t.Fatalf("expected parameter snapshot to use user-provided container model path, got=%s", req.ParameterSnapshot)
	}

	var run models.PipelineRun
	if err := db.First(&run, req.PipelineRunID).Error; err != nil {
		t.Fatalf("load pipeline run failed: %v", err)
	}
	if !bytes.Contains([]byte(run.Config), []byte(`--mount type=bind,src=/srv/models/custom/Qwen3.5-2B,dst=/models/custom/Qwen3.5-2B`)) {
		t.Fatalf("expected pipeline config to preserve user-provided mount args, got=%s", run.Config)
	}
	if !bytes.Contains([]byte(run.Config), []byte(`/models/custom/Qwen3.5-2B`)) {
		t.Fatalf("expected pipeline config to use user-provided runtime model path, got=%s", run.Config)
	}
}

func TestDeploymentHandler_CreateDeploymentRequestSanitizesPlatformVLLMContainerArgs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "llm-vllm-platform-cli-maintainer", models.WorkspaceRoleMaintainer)
	developer := seedResourceStoreMember(t, db, workspace.ID, "llm-vllm-platform-cli-developer", models.WorkspaceRoleDeveloper)

	project := models.Project{Name: "vllm-platform-cli-proj", WorkspaceID: workspace.ID, OwnerID: maintainer.ID}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project failed: %v", err)
	}

	pipelineConfig := PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{{
			ID:   "deploy",
			Type: "ssh",
			Name: "vLLM SSH Deploy",
			Config: map[string]interface{}{
				"host":   "${inputs.resource_host}",
				"script": "MODEL_REF=\"${inputs.runtime_model_path}\"\nif [ -z \"$MODEL_REF\" ]; then\n  MODEL_REF=\"${inputs.model_path}\"\nfi\nif [ -z \"$MODEL_REF\" ]; then\n  MODEL_REF=\"${inputs.model_source_ref}\"\nfi\nIMAGE_REF=\"${inputs.image_name}:${inputs.image_tag}\"\nVLLM_ARGS=\"--host ${inputs.host} --port ${inputs.port}\"\nif [ -n \"${inputs.swap_space}\" ]; then VLLM_ARGS=\"$VLLM_ARGS --swap-space ${inputs.swap_space}\"; fi\nRUNTIME_RUN_CMD=\"$IMAGE_REF vllm serve $MODEL_REF $VLLM_ARGS\"",
			},
		}},
		Edges: []PipelineEdge{},
	}
	configJSON, _ := json.Marshal(pipelineConfig)
	pipeline := models.Pipeline{
		Name:             "platform-vllm-container-args-pipeline",
		WorkspaceID:      workspace.ID,
		ProjectID:        project.ID,
		OwnerID:          maintainer.ID,
		Config:           string(configJSON),
		ManagementHidden: true,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	projectID := project.ID
	resource := models.Resource{WorkspaceID: workspace.ID, ProjectID: &projectID, Name: "vllm-vm-platform-cli-01", Type: models.ResourceTypeVM, Environment: "production", Status: models.ResourceStatusOnline, Endpoint: "10.0.0.9:22", CreatedBy: maintainer.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "vLLM", TemplateType: models.StoreTemplateTypeLLM, TargetResourceType: models.ResourceTypeVM, Source: models.StoreTemplateSourcePlatform, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}
	version := models.StoreTemplateVersion{WorkspaceID: workspace.ID, TemplateID: template.ID, PipelineID: pipeline.ID, Version: "5.0.0", DeploymentMode: "pipeline", Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&version).Error; err != nil {
		t.Fatalf("create version failed: %v", err)
	}
	parameters := []models.TemplateParameter{
		{TemplateVersionID: version.ID, Name: "host", Label: "监听地址", Type: "text", DefaultValue: "0.0.0.0", Required: false, SortOrder: 1},
		{TemplateVersionID: version.ID, Name: "port", Label: "服务端口", Type: "number", DefaultValue: "8000", Required: false, SortOrder: 2},
		{TemplateVersionID: version.ID, Name: "image_name", Label: "镜像名称", Type: "text", DefaultValue: "vllm/vllm-openai", Required: true, SortOrder: 3},
		{TemplateVersionID: version.ID, Name: "image_tag", Label: "镜像标签", Type: "text", DefaultValue: "nightly", Required: true, SortOrder: 4},
		{TemplateVersionID: version.ID, Name: "swap_space", Label: "Swap 空间 (GiB)", Type: "number", DefaultValue: "4", Required: false, SortOrder: 5},
	}
	if err := db.Create(&parameters).Error; err != nil {
		t.Fatalf("create vllm parameters failed: %v", err)
	}
	model := models.LLMModelCatalog{Name: "Qwen2.5-0.5B-Instruct", Source: "huggingface", SourceModelID: "Qwen/Qwen2.5-0.5B-Instruct", ImportedBy: maintainer.ID}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{
		"template_version_id": version.ID,
		"target_resource_id":  resource.ID,
		"llm_model_id":        model.ID,
		"parameters": map[string]interface{}{
			"image_tag": "nightly",
		},
	})
	createResp := performResourceStoreRequest(t, h.CreateDeploymentRequest, developer.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, http.MethodPost, "/api/deployments/requests", requestBody)
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected create deployment request success, got=%d body=%s", createResp.Code, createResp.Body.String())
	}

	var req models.DeploymentRequest
	if err := db.First(&req, responseDataID(t, createResp.Body.Bytes())).Error; err != nil {
		t.Fatalf("load deployment request failed: %v", err)
	}
	var run models.PipelineRun
	if err := db.First(&run, req.PipelineRunID).Error; err != nil {
		t.Fatalf("load pipeline run failed: %v", err)
	}
	if bytes.Contains([]byte(run.Config), []byte(`vllm serve $MODEL_REF`)) {
		t.Fatalf("expected platform vllm container command to drop duplicated serve subcommand, got=%s", run.Config)
	}
	if bytes.Contains([]byte(run.Config), []byte(`--swap-space 4`)) {
		t.Fatalf("expected platform vllm container command to omit removed swap-space flag, got=%s", run.Config)
	}
	if !bytes.Contains([]byte(run.Config), []byte(`$IMAGE_REF $MODEL_REF $VLLM_ARGS`)) {
		t.Fatalf("expected platform vllm container command to use entrypoint-compatible args, got=%s", run.Config)
	}
}

func TestDeploymentHandler_CreateDeploymentRequestAppliesSelectedGPUsToPlatformVLLMVM(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "llm-vllm-gpu-vm-maintainer", models.WorkspaceRoleMaintainer)
	developer := seedResourceStoreMember(t, db, workspace.ID, "llm-vllm-gpu-vm-developer", models.WorkspaceRoleDeveloper)

	project := models.Project{Name: "vllm-gpu-vm-proj", WorkspaceID: workspace.ID, OwnerID: maintainer.ID}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project failed: %v", err)
	}

	pipelineConfig := PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{{
			ID:   "deploy",
			Type: "ssh",
			Name: "vLLM SSH Deploy",
			Config: map[string]interface{}{
				"host":   "${inputs.resource_host}",
				"script": "RUNTIME_RUN_CMD=\"docker run -d --name ${inputs.app_name} --gpus all --ipc=host ${inputs.image_name}:${inputs.image_tag} vllm serve ${inputs.model_source_ref}\"",
			},
		}},
		Edges: []PipelineEdge{},
	}
	configJSON, _ := json.Marshal(pipelineConfig)
	pipeline := models.Pipeline{Name: "vllm-gpu-vm-pipeline", WorkspaceID: workspace.ID, ProjectID: project.ID, OwnerID: maintainer.ID, Config: string(configJSON), ManagementHidden: true}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	projectID := project.ID
	resource := models.Resource{WorkspaceID: workspace.ID, ProjectID: &projectID, Name: "vllm-vm-gpu-01", Type: models.ResourceTypeVM, Environment: "production", Status: models.ResourceStatusOnline, Endpoint: "10.0.0.9:22", CreatedBy: maintainer.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "vLLM", TemplateType: models.StoreTemplateTypeLLM, TargetResourceType: models.ResourceTypeVM, Source: models.StoreTemplateSourcePlatform, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}
	version := models.StoreTemplateVersion{WorkspaceID: workspace.ID, TemplateID: template.ID, PipelineID: pipeline.ID, Version: "7.0.0", DeploymentMode: "pipeline", Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&version).Error; err != nil {
		t.Fatalf("create version failed: %v", err)
	}
	model := models.LLMModelCatalog{Name: "Qwen3.5-2B", Source: "huggingface", SourceModelID: "Qwen/Qwen3.5-2B", ImportedBy: maintainer.ID}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{
		"template_version_id": version.ID,
		"target_resource_id":  resource.ID,
		"llm_model_id":        model.ID,
		"parameters": map[string]interface{}{
			"gpu_indices": "2,3",
		},
	})
	createResp := performResourceStoreRequest(t, h.CreateDeploymentRequest, developer.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, http.MethodPost, "/api/deployments/requests", requestBody)
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected create deployment request success, got=%d body=%s", createResp.Code, createResp.Body.String())
	}

	var req models.DeploymentRequest
	if err := db.First(&req, responseDataID(t, createResp.Body.Bytes())).Error; err != nil {
		t.Fatalf("load deployment request failed: %v", err)
	}
	if !bytes.Contains([]byte(req.ParameterSnapshot), []byte(`"gpu_indices":"2,3"`)) {
		t.Fatalf("expected parameter snapshot to keep gpu indices, got=%s", req.ParameterSnapshot)
	}

	var run models.PipelineRun
	if err := db.First(&run, req.PipelineRunID).Error; err != nil {
		t.Fatalf("load pipeline run failed: %v", err)
	}
	if !bytes.Contains([]byte(run.Config), []byte(`device=2,3`)) {
		t.Fatalf("expected vm vllm config to use selected gpu device set, got=%s", run.Config)
	}
	if !bytes.Contains([]byte(run.Config), []byte(`NVIDIA_VISIBLE_DEVICES=2,3`)) || !bytes.Contains([]byte(run.Config), []byte(`CUDA_VISIBLE_DEVICES=2,3`)) {
		t.Fatalf("expected vm vllm config to expose selected gpu envs, got=%s", run.Config)
	}
}

func TestDeploymentHandler_CreateDeploymentRequestAppliesSelectedGPUsToPlatformOllamaVM(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "llm-ollama-gpu-vm-maintainer", models.WorkspaceRoleMaintainer)
	developer := seedResourceStoreMember(t, db, workspace.ID, "llm-ollama-gpu-vm-developer", models.WorkspaceRoleDeveloper)

	project := models.Project{Name: "ollama-gpu-vm-proj", WorkspaceID: workspace.ID, OwnerID: maintainer.ID}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project failed: %v", err)
	}

	pipelineConfig := PipelineConfig{Version: "2.0", Nodes: []PipelineNode{{ID: "deploy", Type: "docker-run", Name: "Docker Deploy", Config: map[string]interface{}{"host": "${inputs.resource_host}", "image_name": "${inputs.image_name}", "image_tag": "${inputs.image_tag}", "container_name": "${inputs.app_name}", "run_args": "-d", "user": "root"}}}, Edges: []PipelineEdge{}}
	configJSON, _ := json.Marshal(pipelineConfig)
	pipeline := models.Pipeline{Name: "ollama-gpu-vm-pipeline", WorkspaceID: workspace.ID, ProjectID: project.ID, OwnerID: maintainer.ID, Config: string(configJSON), ManagementHidden: true}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	projectID := project.ID
	resource := models.Resource{WorkspaceID: workspace.ID, ProjectID: &projectID, Name: "ollama-vm-gpu-01", Type: models.ResourceTypeVM, Environment: "production", Status: models.ResourceStatusOnline, Endpoint: "10.0.0.9:22", CreatedBy: maintainer.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "Ollama", TemplateType: models.StoreTemplateTypeLLM, TargetResourceType: models.ResourceTypeVM, Source: models.StoreTemplateSourcePlatform, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}
	version := models.StoreTemplateVersion{WorkspaceID: workspace.ID, TemplateID: template.ID, PipelineID: pipeline.ID, Version: "2.0.0", DeploymentMode: "pipeline", Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&version).Error; err != nil {
		t.Fatalf("create version failed: %v", err)
	}
	parameters := []models.TemplateParameter{{TemplateVersionID: version.ID, Name: "app_name", Label: "服务名称", Type: "text", DefaultValue: "ollama", Required: true, SortOrder: 1}, {TemplateVersionID: version.ID, Name: "image_name", Label: "镜像名称", Type: "text", DefaultValue: "ollama/ollama", Required: true, SortOrder: 2}, {TemplateVersionID: version.ID, Name: "image_tag", Label: "镜像标签", Type: "text", DefaultValue: "latest", Required: true, SortOrder: 3}}
	if err := db.Create(&parameters).Error; err != nil {
		t.Fatalf("create ollama parameters failed: %v", err)
	}
	model := models.LLMModelCatalog{Name: "Qwen3.5-2B", Source: "huggingface", SourceModelID: "Qwen/Qwen3.5-2B", ImportedBy: maintainer.ID}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{"template_version_id": version.ID, "target_resource_id": resource.ID, "llm_model_id": model.ID, "parameters": map[string]interface{}{"gpu_indices": "2,3"}})
	createResp := performResourceStoreRequest(t, h.CreateDeploymentRequest, developer.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, http.MethodPost, "/api/deployments/requests", requestBody)
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected create deployment request success, got=%d body=%s", createResp.Code, createResp.Body.String())
	}
	var req models.DeploymentRequest
	if err := db.First(&req, responseDataID(t, createResp.Body.Bytes())).Error; err != nil {
		t.Fatalf("load deployment request failed: %v", err)
	}
	var run models.PipelineRun
	if err := db.First(&run, req.PipelineRunID).Error; err != nil {
		t.Fatalf("load pipeline run failed: %v", err)
	}
	if !bytes.Contains([]byte(run.Config), []byte(`device=2,3`)) || !bytes.Contains([]byte(run.Config), []byte(`NVIDIA_VISIBLE_DEVICES=2,3`)) {
		t.Fatalf("expected ollama vm config to use selected gpu device set, got=%s", run.Config)
	}
}

func TestDeploymentHandler_CreateDeploymentRequestAppliesSelectedGPUsToPlatformVLLMK8s(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "llm-vllm-gpu-k8s-maintainer", models.WorkspaceRoleMaintainer)
	developer := seedResourceStoreMember(t, db, workspace.ID, "llm-vllm-gpu-k8s-developer", models.WorkspaceRoleDeveloper)

	project := models.Project{Name: "vllm-gpu-k8s-proj", WorkspaceID: workspace.ID, OwnerID: maintainer.ID}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project failed: %v", err)
	}

	pipelineConfig := PipelineConfig{Version: "2.0", Nodes: []PipelineNode{{ID: "deploy", Type: "kubernetes", Name: "vLLM K8s Deploy", Config: map[string]interface{}{"command": "cat <<EOF | kubectl apply -f -\napiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: ${inputs.app_name}\nspec:\n  template:\n    spec:\n      containers:\n        - name: ${inputs.app_name}\n          image: ${inputs.image_name}:${inputs.image_tag}\n          ports:\n            - containerPort: ${inputs.port}\n          resources:\n            limits:\n              nvidia.com/gpu: 1\nEOF"}}}, Edges: []PipelineEdge{}}
	configJSON, _ := json.Marshal(pipelineConfig)
	pipeline := models.Pipeline{Name: "vllm-gpu-k8s-pipeline", WorkspaceID: workspace.ID, ProjectID: project.ID, OwnerID: maintainer.ID, Config: string(configJSON), ManagementHidden: true}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	projectID := project.ID
	resource := models.Resource{WorkspaceID: workspace.ID, ProjectID: &projectID, Name: "vllm-k8s-gpu-01", Type: models.ResourceTypeK8sCluster, Environment: "production", Status: models.ResourceStatusOnline, Endpoint: "https://10.0.0.1:6443", CreatedBy: maintainer.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "vLLM", TemplateType: models.StoreTemplateTypeLLM, TargetResourceType: models.ResourceTypeK8sCluster, Source: models.StoreTemplateSourcePlatform, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}
	version := models.StoreTemplateVersion{WorkspaceID: workspace.ID, TemplateID: template.ID, PipelineID: pipeline.ID, Version: "8.0.0", DeploymentMode: "pipeline", Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&version).Error; err != nil {
		t.Fatalf("create version failed: %v", err)
	}
	parameters := []models.TemplateParameter{{TemplateVersionID: version.ID, Name: "app_name", Label: "服务名称", Type: "text", DefaultValue: "vllm-service", Required: true, SortOrder: 1}, {TemplateVersionID: version.ID, Name: "image_name", Label: "镜像名称", Type: "text", DefaultValue: "vllm/vllm-openai", Required: true, SortOrder: 2}, {TemplateVersionID: version.ID, Name: "image_tag", Label: "镜像标签", Type: "text", DefaultValue: "latest", Required: true, SortOrder: 3}, {TemplateVersionID: version.ID, Name: "port", Label: "端口", Type: "number", DefaultValue: "8000", Required: false, SortOrder: 4}}
	if err := db.Create(&parameters).Error; err != nil {
		t.Fatalf("create vllm k8s parameters failed: %v", err)
	}
	model := models.LLMModelCatalog{Name: "Qwen3.5-2B", Source: "huggingface", SourceModelID: "Qwen/Qwen3.5-2B", ImportedBy: maintainer.ID}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{"template_version_id": version.ID, "target_resource_id": resource.ID, "llm_model_id": model.ID, "parameters": map[string]interface{}{"gpu_indices": "2,3"}})
	createResp := performResourceStoreRequest(t, h.CreateDeploymentRequest, developer.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, http.MethodPost, "/api/deployments/requests", requestBody)
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected create deployment request success, got=%d body=%s", createResp.Code, createResp.Body.String())
	}
	var req models.DeploymentRequest
	if err := db.First(&req, responseDataID(t, createResp.Body.Bytes())).Error; err != nil {
		t.Fatalf("load deployment request failed: %v", err)
	}
	var run models.PipelineRun
	if err := db.First(&run, req.PipelineRunID).Error; err != nil {
		t.Fatalf("load pipeline run failed: %v", err)
	}
	if !bytes.Contains([]byte(run.Config), []byte(`NVIDIA_VISIBLE_DEVICES`)) || !bytes.Contains([]byte(run.Config), []byte(`CUDA_VISIBLE_DEVICES`)) {
		t.Fatalf("expected vllm k8s config to inject gpu env vars, got=%s", run.Config)
	}
	if !bytes.Contains([]byte(run.Config), []byte(`2,3`)) || !bytes.Contains([]byte(run.Config), []byte(`nvidia.com/gpu: 2`)) {
		t.Fatalf("expected vllm k8s config to use selected gpu values/count, got=%s", run.Config)
	}
}

func TestDeploymentHandler_CreateDeploymentRequestAppliesSelectedGPUsToPlatformOllamaK8s(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "llm-ollama-gpu-k8s-maintainer", models.WorkspaceRoleMaintainer)
	developer := seedResourceStoreMember(t, db, workspace.ID, "llm-ollama-gpu-k8s-developer", models.WorkspaceRoleDeveloper)

	project := models.Project{Name: "ollama-gpu-k8s-proj", WorkspaceID: workspace.ID, OwnerID: maintainer.ID}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project failed: %v", err)
	}

	pipelineConfig := PipelineConfig{Version: "2.0", Nodes: []PipelineNode{{ID: "deploy", Type: "kubernetes", Name: "Kubernetes Deploy", Config: map[string]interface{}{"command": "echo deploying ${inputs.resource_name}"}}}, Edges: []PipelineEdge{}}
	configJSON, _ := json.Marshal(pipelineConfig)
	pipeline := models.Pipeline{Name: "ollama-gpu-k8s-pipeline", WorkspaceID: workspace.ID, ProjectID: project.ID, OwnerID: maintainer.ID, Config: string(configJSON), ManagementHidden: true}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	projectID := project.ID
	resource := models.Resource{WorkspaceID: workspace.ID, ProjectID: &projectID, Name: "ollama-k8s-gpu-01", Type: models.ResourceTypeK8sCluster, Environment: "production", Status: models.ResourceStatusOnline, Endpoint: "https://10.0.0.1:6443", CreatedBy: maintainer.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "Ollama", TemplateType: models.StoreTemplateTypeLLM, TargetResourceType: models.ResourceTypeK8sCluster, Source: models.StoreTemplateSourcePlatform, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}
	version := models.StoreTemplateVersion{WorkspaceID: workspace.ID, TemplateID: template.ID, PipelineID: pipeline.ID, Version: "2.0.0", DeploymentMode: "pipeline", Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&version).Error; err != nil {
		t.Fatalf("create version failed: %v", err)
	}
	parameters := []models.TemplateParameter{{TemplateVersionID: version.ID, Name: "app_name", Label: "服务名称", Type: "text", DefaultValue: "ollama", Required: true, SortOrder: 1}, {TemplateVersionID: version.ID, Name: "image_name", Label: "镜像名称", Type: "text", DefaultValue: "ollama/ollama", Required: true, SortOrder: 2}, {TemplateVersionID: version.ID, Name: "image_tag", Label: "镜像标签", Type: "text", DefaultValue: "latest", Required: true, SortOrder: 3}, {TemplateVersionID: version.ID, Name: "port", Label: "端口", Type: "number", DefaultValue: "11434", Required: false, SortOrder: 4}, {TemplateVersionID: version.ID, Name: "ollama_keep_alive", Label: "保活", Type: "text", DefaultValue: "5m", Required: false, SortOrder: 5}, {TemplateVersionID: version.ID, Name: "ollama_num_parallel", Label: "并行", Type: "number", DefaultValue: "1", Required: false, SortOrder: 6}, {TemplateVersionID: version.ID, Name: "ollama_origin", Label: "来源", Type: "text", DefaultValue: "*", Required: false, SortOrder: 7}}
	if err := db.Create(&parameters).Error; err != nil {
		t.Fatalf("create ollama k8s parameters failed: %v", err)
	}
	model := models.LLMModelCatalog{Name: "Qwen3.5-2B", Source: "huggingface", SourceModelID: "Qwen/Qwen3.5-2B", ImportedBy: maintainer.ID}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{"template_version_id": version.ID, "target_resource_id": resource.ID, "llm_model_id": model.ID, "parameters": map[string]interface{}{"gpu_indices": "2,3"}})
	createResp := performResourceStoreRequest(t, h.CreateDeploymentRequest, developer.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, http.MethodPost, "/api/deployments/requests", requestBody)
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected create deployment request success, got=%d body=%s", createResp.Code, createResp.Body.String())
	}
	var req models.DeploymentRequest
	if err := db.First(&req, responseDataID(t, createResp.Body.Bytes())).Error; err != nil {
		t.Fatalf("load deployment request failed: %v", err)
	}
	var run models.PipelineRun
	if err := db.First(&run, req.PipelineRunID).Error; err != nil {
		t.Fatalf("load pipeline run failed: %v", err)
	}
	if !bytes.Contains([]byte(run.Config), []byte(`OLLAMA_NUM_PARALLEL`)) || !bytes.Contains([]byte(run.Config), []byte(`NVIDIA_VISIBLE_DEVICES`)) || !bytes.Contains([]byte(run.Config), []byte(`nvidia.com/gpu: 2`)) {
		t.Fatalf("expected ollama k8s config to include gpu-aware manifest, got=%s", run.Config)
	}
}

func TestDeploymentHandler_CreateDeploymentRequestAppliesSelectedGPUsToPlatformSGLangK8s(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "llm-sglang-gpu-k8s-maintainer", models.WorkspaceRoleMaintainer)
	developer := seedResourceStoreMember(t, db, workspace.ID, "llm-sglang-gpu-k8s-developer", models.WorkspaceRoleDeveloper)

	project := models.Project{Name: "sglang-gpu-k8s-proj", WorkspaceID: workspace.ID, OwnerID: maintainer.ID}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project failed: %v", err)
	}

	pipelineConfig := PipelineConfig{Version: "2.0", Nodes: []PipelineNode{{ID: "deploy", Type: "kubernetes", Name: "Kubernetes Deploy", Config: map[string]interface{}{"command": "echo deploying ${inputs.resource_name}"}}}, Edges: []PipelineEdge{}}
	configJSON, _ := json.Marshal(pipelineConfig)
	pipeline := models.Pipeline{Name: "sglang-gpu-k8s-pipeline", WorkspaceID: workspace.ID, ProjectID: project.ID, OwnerID: maintainer.ID, Config: string(configJSON), ManagementHidden: true}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	projectID := project.ID
	resource := models.Resource{WorkspaceID: workspace.ID, ProjectID: &projectID, Name: "sglang-k8s-gpu-01", Type: models.ResourceTypeK8sCluster, Environment: "production", Status: models.ResourceStatusOnline, Endpoint: "https://10.0.0.1:6443", CreatedBy: maintainer.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "SGLang", TemplateType: models.StoreTemplateTypeLLM, TargetResourceType: models.ResourceTypeK8sCluster, Source: models.StoreTemplateSourcePlatform, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}
	version := models.StoreTemplateVersion{WorkspaceID: workspace.ID, TemplateID: template.ID, PipelineID: pipeline.ID, Version: "2.0.0", DeploymentMode: "pipeline", Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&version).Error; err != nil {
		t.Fatalf("create version failed: %v", err)
	}
	parameters := []models.TemplateParameter{{TemplateVersionID: version.ID, Name: "app_name", Label: "服务名称", Type: "text", DefaultValue: "sglang-service", Required: true, SortOrder: 1}, {TemplateVersionID: version.ID, Name: "image_name", Label: "镜像名称", Type: "text", DefaultValue: "lmsysorg/sglang", Required: true, SortOrder: 2}, {TemplateVersionID: version.ID, Name: "image_tag", Label: "镜像标签", Type: "text", DefaultValue: "latest", Required: true, SortOrder: 3}, {TemplateVersionID: version.ID, Name: "tp_size", Label: "TP", Type: "number", DefaultValue: "1", Required: false, SortOrder: 4}, {TemplateVersionID: version.ID, Name: "host", Label: "host", Type: "text", DefaultValue: "0.0.0.0", Required: false, SortOrder: 5}, {TemplateVersionID: version.ID, Name: "port", Label: "端口", Type: "number", DefaultValue: "30000", Required: false, SortOrder: 6}, {TemplateVersionID: version.ID, Name: "mem_fraction_static", Label: "显存占比", Type: "number", DefaultValue: "0.9", Required: false, SortOrder: 7}, {TemplateVersionID: version.ID, Name: "enable_flashinfer", Label: "flashinfer", Type: "switch", DefaultValue: "false", Required: false, SortOrder: 8}}
	if err := db.Create(&parameters).Error; err != nil {
		t.Fatalf("create sglang k8s parameters failed: %v", err)
	}
	model := models.LLMModelCatalog{Name: "Qwen3.5-2B", Source: "huggingface", SourceModelID: "Qwen/Qwen3.5-2B", ImportedBy: maintainer.ID}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{"template_version_id": version.ID, "target_resource_id": resource.ID, "llm_model_id": model.ID, "parameters": map[string]interface{}{"gpu_indices": "2,3"}})
	createResp := performResourceStoreRequest(t, h.CreateDeploymentRequest, developer.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, http.MethodPost, "/api/deployments/requests", requestBody)
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected create deployment request success, got=%d body=%s", createResp.Code, createResp.Body.String())
	}
	var req models.DeploymentRequest
	if err := db.First(&req, responseDataID(t, createResp.Body.Bytes())).Error; err != nil {
		t.Fatalf("load deployment request failed: %v", err)
	}
	var run models.PipelineRun
	if err := db.First(&run, req.PipelineRunID).Error; err != nil {
		t.Fatalf("load pipeline run failed: %v", err)
	}
	if !bytes.Contains([]byte(run.Config), []byte(`sglang.launch_server`)) || !bytes.Contains([]byte(run.Config), []byte(`CUDA_VISIBLE_DEVICES`)) || !bytes.Contains([]byte(run.Config), []byte(`nvidia.com/gpu: 2`)) {
		t.Fatalf("expected sglang k8s config to include gpu-aware launch command, got=%s", run.Config)
	}
}

func seedResourceStoreUserAndWorkspace(t *testing.T, db *gorm.DB, username, workspaceRole string) (models.User, models.Workspace) {
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
		t.Fatalf("create workspace member failed: %v", err)
	}
	return user, workspace
}

func minimalKubernetesPipelineConfig(t *testing.T) string {
	t.Helper()
	config := PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{{
			ID:   "deploy",
			Type: "kubernetes",
			Name: "Kubernetes Deploy",
			Config: map[string]interface{}{
				"manifest": "./k8s/deploy.yaml",
			},
		}},
		Edges: []PipelineEdge{},
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal kubernetes pipeline config failed: %v", err)
	}
	return string(raw)
}

func seedResourceStoreMember(t *testing.T, db *gorm.DB, workspaceID uint64, username, workspaceRole string) models.User {
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
		t.Fatalf("create workspace member failed: %v", err)
	}
	return user
}

func seedApprovedResourceAgent(t *testing.T, db *gorm.DB, workspaceID uint64) models.Agent {
	t.Helper()
	agent := models.Agent{
		Name:               "resource-verify-agent",
		Host:               "agent.local",
		Port:               8080,
		Token:              "token-resource-verify",
		Status:             models.AgentStatusOnline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
		ScopeType:          models.AgentScopeWorkspace,
		WorkspaceID:        workspaceID,
		HeartbeatInterval:  10,
		LastHeartAt:        1710000000,
		ApprovedAt:         1710000000,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create approved agent failed: %v", err)
	}
	return agent
}

func seedResourceVerificationCredential(t *testing.T, db *gorm.DB, workspaceID, ownerID uint64, credentialType models.CredentialType, payload map[string]interface{}) models.Credential {
	t.Helper()
	encrypted, err := NewCredentialHandler().encryptionService.EncryptCredentialData(payload)
	if err != nil {
		t.Fatalf("encrypt resource verification credential failed: %v", err)
	}
	credential := models.Credential{
		Name:             "resource-verify-credential",
		Type:             credentialType,
		Category:         models.CategoryCustom,
		Scope:            models.ScopeWorkspace,
		WorkspaceID:      workspaceID,
		OwnerID:          ownerID,
		EncryptedPayload: encrypted,
		Status:           models.CredentialStatusActive,
	}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatalf("create resource verification credential failed: %v", err)
	}
	return credential
}

func seedSuccessfulResourceValidationTask(t *testing.T, db *gorm.DB, workspaceID, userID uint64, resourceType models.ResourceType, endpoint string, credential models.Credential) models.AgentTask {
	t.Helper()
	verification := buildResourceValidationTaskPayload(resourceType, endpoint, endpoint, credential)
	rawParams, err := json.Marshal(verification)
	if err != nil {
		t.Fatalf("marshal validation params failed: %v", err)
	}
	task := models.AgentTask{
		WorkspaceID: workspaceID,
		AgentID:     seedApprovedResourceAgent(t, db, workspaceID).ID,
		NodeID:      fmt.Sprintf("resource-verify-%d", time.Now().UnixNano()),
		TaskType:    map[models.ResourceType]string{models.ResourceTypeVM: "ssh", models.ResourceTypeK8sCluster: "kubernetes"}[resourceType],
		Name:        "验证资源连接",
		Params:      string(rawParams),
		Status:      models.TaskStatusExecuteSuccess,
		CreatedBy:   userID,
		StartTime:   time.Now().Unix() - 3,
		EndTime:     time.Now().Unix() - 1,
		Timeout:     120,
		MaxRetries:  0,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create successful validation task failed: %v", err)
	}
	return task
}

func performResourceStoreRequest(t *testing.T, handler gin.HandlerFunc, userID uint64, role string, workspaceID uint64, workspaceRole string, method, url string, body []byte, pathParams ...func(*gin.Context)) *httptest.ResponseRecorder {
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
	c.Set("workspace_role", workspaceRole)
	for _, setParam := range pathParams {
		setParam(c)
	}
	handler(c)
	return w
}

func pathResourceStoreID(id uint64) func(*gin.Context) {
	return func(c *gin.Context) {
		c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(id, 10)}}
	}
}

func pathTemplateVersionIDs(templateID, versionID uint64) func(*gin.Context) {
	return func(c *gin.Context) {
		c.Params = gin.Params{
			{Key: "id", Value: strconv.FormatUint(templateID, 10)},
			{Key: "version_id", Value: strconv.FormatUint(versionID, 10)},
		}
	}
}

func decodeResponseData[T any](t *testing.T, body []byte) T {
	t.Helper()
	var payload struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal response failed: %v body=%s", err, string(body))
	}
	return payload.Data
}
