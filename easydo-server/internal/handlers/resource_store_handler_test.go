package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"easydo-server/internal/config"
	"easydo-server/internal/models"
	"easydo-server/pkg/storage"
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

func TestResourceHandler_UpdateResourceLabelsStrictly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "resource-label-maintainer", models.WorkspaceRoleMaintainer)
	viewer := seedResourceStoreMember(t, db, workspace.ID, "resource-label-viewer", models.WorkspaceRoleViewer)
	developer := seedResourceStoreMember(t, db, workspace.ID, "resource-label-developer", models.WorkspaceRoleDeveloper)

	h := NewResourceHandler()
	resource := models.Resource{
		WorkspaceID: workspace.ID,
		Name:        "label-vm",
		Type:        models.ResourceTypeVM,
		Environment: "development",
		Status:      models.ResourceStatusOnline,
		Endpoint:    "10.0.0.9:22",
		Labels:      `{"old":"value"}`,
		Metadata:    `{"keep":"metadata"}`,
		CreatedBy:   maintainer.ID,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	updateBody := func(labels string) []byte {
		return mustJSON(t, map[string]any{
			"name":        "label-vm-updated",
			"type":        string(models.ResourceTypeVM),
			"environment": "production",
			"endpoint":    "10.0.0.9:22",
			"labels":      labels,
			"metadata":    `{"keep":"metadata"}`,
		})
	}

	viewerResp := performResourceStoreRequest(t, h.UpdateResource, viewer.ID, "user", workspace.ID, models.WorkspaceRoleViewer, http.MethodPut, "/api/resources/1", updateBody(`{"team":"infra"}`), pathResourceStoreID(resource.ID))
	if viewerResp.Code != http.StatusForbidden {
		t.Fatalf("expected viewer update labels forbidden, got=%d body=%s", viewerResp.Code, viewerResp.Body.String())
	}

	developerResp := performResourceStoreRequest(t, h.UpdateResource, developer.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, http.MethodPut, "/api/resources/1", updateBody(`{"team":"infra"}`), pathResourceStoreID(resource.ID))
	if developerResp.Code != http.StatusForbidden {
		t.Fatalf("expected developer update labels forbidden, got=%d body=%s", developerResp.Code, developerResp.Body.String())
	}

	invalidCases := []struct {
		name   string
		labels string
	}{
		{name: "array", labels: `["bad"]`},
		{name: "non string value", labels: `{"team":123}`},
		{name: "empty key", labels: `{"":"infra"}`},
		{name: "empty value", labels: `{"team":""}`},
		{name: "duplicate trimmed key", labels: `{" team":"infra","team":"platform"}`},
		{name: "duplicate raw key", labels: `{"team":"infra","team":"platform"}`},
	}
	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			resp := performResourceStoreRequest(t, h.UpdateResource, maintainer.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, http.MethodPut, "/api/resources/1", updateBody(tc.labels), pathResourceStoreID(resource.ID))
			if resp.Code != http.StatusBadRequest {
				t.Fatalf("expected invalid labels to return 400, got=%d body=%s", resp.Code, resp.Body.String())
			}
		})
	}

	resp := performResourceStoreRequest(t, h.UpdateResource, maintainer.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, http.MethodPut, "/api/resources/1", updateBody(`{" team ":" infra ","tier":"prod"}`), pathResourceStoreID(resource.ID))
	if resp.Code != http.StatusOK {
		t.Fatalf("expected maintainer update labels success, got=%d body=%s", resp.Code, resp.Body.String())
	}

	getResp := performResourceStoreRequest(t, h.GetResource, maintainer.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, http.MethodGet, "/api/resources/1", nil, pathResourceStoreID(resource.ID))
	if getResp.Code != http.StatusOK {
		t.Fatalf("expected get resource success, got=%d body=%s", getResp.Code, getResp.Body.String())
	}
	var getPayload struct {
		Data struct {
			Labels map[string]string `json:"labels"`
		} `json:"data"`
	}
	if err := json.Unmarshal(getResp.Body.Bytes(), &getPayload); err != nil {
		t.Fatalf("unmarshal get resource response failed: %v body=%s", err, getResp.Body.String())
	}
	if getPayload.Data.Labels["team"] != "infra" || getPayload.Data.Labels["tier"] != "prod" {
		t.Fatalf("expected labels in object form with trimmed entries, got=%v body=%s", getPayload.Data.Labels, getResp.Body.String())
	}

	var stored models.Resource
	if err := db.First(&stored, resource.ID).Error; err != nil {
		t.Fatalf("load updated resource failed: %v", err)
	}
	if stored.Metadata != `{"keep":"metadata"}` {
		t.Fatalf("expected metadata unchanged, got=%s", stored.Metadata)
	}
}

func TestResourceHandler_UpdateResourceLabelsOnlyPreservesResourceFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "resource-label-only-maintainer", models.WorkspaceRoleMaintainer)
	viewer := seedResourceStoreMember(t, db, workspace.ID, "resource-label-only-viewer", models.WorkspaceRoleViewer)
	project := models.Project{Name: "label-project", WorkspaceID: workspace.ID, OwnerID: maintainer.ID}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	projectID := project.ID
	resource := models.Resource{
		WorkspaceID: workspace.ID,
		ProjectID:   &projectID,
		Name:        "label-only-vm",
		Description: "keep description",
		Type:        models.ResourceTypeVM,
		Environment: "production",
		Status:      models.ResourceStatusOnline,
		Endpoint:    "10.0.0.10:22",
		Labels:      `{"old":"value"}`,
		Metadata:    `{"keep":"metadata"}`,
		BaseInfo:    `{"schemaVersion":1}`,
		LastCheckAt: 1710000000,
		CreatedBy:   maintainer.ID,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	h := NewResourceHandler()
	body := mustJSON(t, map[string]any{"labels": `{" team ":" platform "}`})
	viewerResp := performResourceStoreRequest(t, h.UpdateResourceLabels, viewer.ID, "user", workspace.ID, models.WorkspaceRoleViewer, http.MethodPut, fmt.Sprintf("/api/resources/%d/labels", resource.ID), body, pathResourceStoreID(resource.ID))
	if viewerResp.Code != http.StatusForbidden {
		t.Fatalf("expected viewer update labels forbidden, got=%d body=%s", viewerResp.Code, viewerResp.Body.String())
	}

	invalidBody := mustJSON(t, map[string]any{"labels": `{"team":"infra","team":"platform"}`})
	invalidResp := performResourceStoreRequest(t, h.UpdateResourceLabels, maintainer.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, http.MethodPut, fmt.Sprintf("/api/resources/%d/labels", resource.ID), invalidBody, pathResourceStoreID(resource.ID))
	if invalidResp.Code != http.StatusBadRequest {
		t.Fatalf("expected duplicate labels rejected, got=%d body=%s", invalidResp.Code, invalidResp.Body.String())
	}

	resp := performResourceStoreRequest(t, h.UpdateResourceLabels, maintainer.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, http.MethodPut, fmt.Sprintf("/api/resources/%d/labels", resource.ID), body, pathResourceStoreID(resource.ID))
	if resp.Code != http.StatusOK {
		t.Fatalf("expected label-only update success, got=%d body=%s", resp.Code, resp.Body.String())
	}

	var stored models.Resource
	if err := db.First(&stored, resource.ID).Error; err != nil {
		t.Fatalf("load label-only resource failed: %v", err)
	}
	if stored.ProjectID == nil || *stored.ProjectID != project.ID {
		t.Fatalf("expected project_id preserved, got=%v", stored.ProjectID)
	}
	if stored.Name != resource.Name || stored.Description != resource.Description || stored.Endpoint != resource.Endpoint {
		t.Fatalf("expected non-label fields preserved, got=%+v", stored)
	}
	if stored.Metadata != resource.Metadata || stored.BaseInfo != resource.BaseInfo || stored.LastCheckAt != resource.LastCheckAt {
		t.Fatalf("expected metadata/base info fields preserved, got metadata=%s base_info=%s last_check_at=%d", stored.Metadata, stored.BaseInfo, stored.LastCheckAt)
	}
	if stored.Labels != `{"team":"platform"}` {
		t.Fatalf("expected normalized labels only, got=%s", stored.Labels)
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

func TestResourceHandler_GetResourceMergesRuntimeLabelsIntoBaseInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	viewer, workspace := seedResourceStoreUserAndWorkspace(t, db, "resource-runtime-label-viewer", models.WorkspaceRoleViewer)
	resource := models.Resource{
		WorkspaceID: workspace.ID,
		Name:        "inventory-vm",
		Type:        models.ResourceTypeVM,
		Environment: "production",
		Status:      models.ResourceStatusOnline,
		Endpoint:    "10.0.0.88:22",
		CreatedBy:   viewer.ID,
		BaseInfo:    `{"schemaVersion":2,"status":"success","source":"remote_task","collectedAt":1710000000,"labels":{"tier":"infra","cluster":"blue"},"vm":{"summary":{"gpuCount":1},"labels":{"tier":"infra","cluster":"blue"}}}`,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}
	if err := db.Create(&models.ResourceRuntimeLabel{
		WorkspaceID: workspace.ID,
		ResourceID:  resource.ID,
		TargetType:  "resource",
		TargetKey:   "resource:1:vm:inventory-vm",
		LabelsJSON:  `{"tier":"ops","owner":"platform","region":"cn-hangzhou"}`,
		CreatedBy:   viewer.ID,
	}).Error; err != nil {
		t.Fatalf("create runtime labels failed: %v", err)
	}
	if err := db.Create(&models.ResourceRuntimeLabel{
		WorkspaceID: workspace.ID,
		ResourceID:  resource.ID,
		TargetType:  "gpu",
		TargetKey:   "resource:1:gpu:0",
		LabelsJSON:  `{"gpuOnly":"yes","owner":"gpu-owner"}`,
		CreatedBy:   viewer.ID,
	}).Error; err != nil {
		t.Fatalf("create gpu runtime labels failed: %v", err)
	}

	h := NewResourceHandler()
	resp := performResourceStoreRequest(t, h.GetResource, viewer.ID, "user", workspace.ID, models.WorkspaceRoleViewer, http.MethodGet, "/api/resources/1", nil, pathResourceStoreID(resource.ID))
	if resp.Code != http.StatusOK {
		t.Fatalf("expected get resource success, got=%d body=%s", resp.Code, resp.Body.String())
	}
	var payload struct {
		Data struct {
			BaseInfo map[string]interface{} `json:"base_info"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("parse resource response failed: %v body=%s", err, resp.Body.String())
	}
	labels, _ := payload.Data.BaseInfo["labels"].(map[string]interface{})
	if labels["owner"] != "platform" {
		t.Fatalf("expected runtime label owner in base_info.labels, got=%#v body=%s", labels, resp.Body.String())
	}
	if labels["tier"] != "infra" {
		t.Fatalf("expected durable base_info label to win merge, got=%#v body=%s", labels, resp.Body.String())
	}
	if labels["region"] != "cn-hangzhou" {
		t.Fatalf("expected runtime label region in base_info.labels, got=%#v body=%s", labels, resp.Body.String())
	}
	if _, exists := payload.Data.BaseInfo["owner"]; exists {
		t.Fatalf("expected runtime labels to stay under base_info.labels, got=%s", resp.Body.String())
	}
	if labels["gpuOnly"] != nil {
		t.Fatalf("expected non-resource scoped labels to stay out of base_info.labels, got=%#v body=%s", labels, resp.Body.String())
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
		Labels:      `{"team":"platform","tier":"infra"}`,
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
	if !bytes.Contains([]byte(task.Script), []byte("EASYDO_RUNTIME_WORKLOADS_BEGIN")) {
		t.Fatalf("expected refresh script to emit runtime workload markers, got=%s", task.Script)
	}
	if !bytes.Contains([]byte(task.Script), []byte("EASYDO_GPU_PROCESS_CSV_BEGIN")) {
		t.Fatalf("expected refresh script to emit gpu process markers, got=%s", task.Script)
	}
	if !bytes.Contains([]byte(task.Script), []byte("EASYDO_PROCESS_TREE_BEGIN")) {
		t.Fatalf("expected refresh script to emit process tree markers, got=%s", task.Script)
	}
	for _, snippet := range []string{"mx-smi -L", "mx-smi --show-memory", "mx-smi --show-usage", "mx-smi query -d TEMPERATURE", "mx-smi select -f index temperature", "mx-smi --show-process", "mx-smi >\"$MX_SUMMARY_FILE\" 2>/dev/null || true"} {
		if !bytes.Contains([]byte(task.Script), []byte(snippet)) {
			t.Fatalf("expected refresh script to include %s for MetaX collection, got=%s", snippet, task.Script)
		}
	}
	if !bytes.Contains([]byte(task.Params), []byte("resource_labels")) {
		t.Fatalf("expected refresh task params to carry resource labels, got=%s", task.Params)
	}
}

func TestParseVMBaseInfoOutput_EmitsCanonicalBaseInfoV3(t *testing.T) {
	stdout := "EASYDO_BASE_INFO_BEGIN\n" +
		"EASYDO_HOSTNAME=ubuntu\n" +
		"EASYDO_PRIMARY_IPV4=10.0.0.8\n" +
		"EASYDO_OS_NAME=Ubuntu 22.04.4 LTS\n" +
		"EASYDO_OS_VERSION=22.04\n" +
		"EASYDO_KERNEL_VERSION=6.5.0-18-generic\n" +
		"EASYDO_ARCH=x86_64\n" +
		"EASYDO_CPU_MODEL=Intel(R) Xeon(R)\n" +
		"EASYDO_CPU_LOGICAL_CORES=16\n" +
		"EASYDO_CPU_USED_CORES=3.5\n" +
		"EASYDO_MEMORY_TOTAL_BYTES=68719476736\n" +
		"EASYDO_MEMORY_USED_BYTES=21474836480\n" +
		"EASYDO_ROOT_TOTAL_BYTES=485687422976\n" +
		"EASYDO_TOTAL_DISK_BYTES=2512510000000\n" +
		"EASYDO_GPU_COUNT=2\n" +
		"EASYDO_RESOURCE_LABELS_JSON={\"tier\":\"infra\",\"owner\":\"platform\"}\n" +
		"EASYDO_DISK_ROWS_BEGIN\n" +
		"NAME=\"nvme0n1p3\" SIZE=\"494598954496\" TYPE=\"part\" FSTYPE=\"ext4\" MOUNTPOINT=\"/\"\n" +
		"EASYDO_DISK_ROWS_END\n" +
		"EASYDO_GPU_CSV_BEGIN\n" +
		"0,NVIDIA H100 PCIe,81920,GPU-aaa,0000:01:00.0,NVIDIA,10240,,75,64\n" +
		"1,NVIDIA H100 PCIe,81920,GPU-bbb,0000:02:00.0,NVIDIA,2048,79872,12,41\n" +
		"EASYDO_GPU_CSV_END\n" +
		"EASYDO_RUNTIME_WORKLOADS_BEGIN\n" +
		"{\"name\":\"trainer\",\"pid\":4242,\"runtime\":\"docker\",\"containerId\":\"ctr-1\",\"containerName\":\"trainer-ctr\"}\n" +
		"{\"name\":\"trainer\",\"pid\":5252,\"runtime\":\"docker\",\"containerId\":\"ctr-2\",\"containerName\":\"trainer-ctr\"}\n" +
		"EASYDO_RUNTIME_WORKLOADS_END\n" +
		"EASYDO_PROCESS_TREE_BEGIN\n" +
		"EASYDO_PROCESS_TREE_END\n" +
		"EASYDO_GPU_PROCESS_CSV_BEGIN\n" +
		"{\"gpuIndex\":0,\"pid\":4242,\"memoryUsedBytes\":8589934592}\n" +
		"{\"gpuIndex\":1,\"pid\":5252,\"memoryUsedBytes\":2147483648}\n" +
		"{\"gpuIndex\":9,\"pid\":4242,\"memoryUsedBytes\":123}\n" +
		"EASYDO_GPU_PROCESS_CSV_END\n" +
		"EASYDO_BASE_INFO_END\n"

	baseInfoJSON, source, _, err := parseVMBaseInfoOutput(stdout, "remote_task")
	if err != nil {
		t.Fatalf("parseVMBaseInfoOutput returned error: %v", err)
	}
	if source != "remote_task" {
		t.Fatalf("source=%q, want remote_task", source)
	}
	var payload ResourceBaseInfoV3
	if err := json.Unmarshal([]byte(baseInfoJSON), &payload); err != nil {
		t.Fatalf("unmarshal canonical base info failed: %v", err)
	}
	if payload.SchemaVersion != 3 || payload.Status != "success" || payload.Source != "remote_task" || payload.CollectedAt == "" {
		t.Fatalf("unexpected top-level metadata: %+v", payload)
	}
	if payload.ResourceID != buildResourceBaseInfoResourcePrefix(0) {
		t.Fatalf("resourceId=%q, want %q", payload.ResourceID, buildResourceBaseInfoResourcePrefix(0))
	}
	if len(payload.Entities) != 1 || payload.Entities[0].ID != buildResourceBaseInfoEntityID(0, "host") {
		t.Fatalf("expected one host entity, got %+v", payload.Entities)
	}
	if payload.Labels["tier"] != "infra" || payload.Labels["owner"] != "platform" {
		t.Fatalf("expected durable labels in canonical top-level labels, got %#v", payload.Labels)
	}
	if len(payload.ResourceTypes) != len(approvedResourceBaseInfoResourceTypes()) {
		t.Fatalf("resourceTypes len=%d, want approved dictionary len=%d", len(payload.ResourceTypes), len(approvedResourceBaseInfoResourceTypes()))
	}
	instances := map[string]ResourceBaseInfoResourceInstance{}
	for _, instance := range payload.ResourceInstances {
		instances[instance.ID] = instance
		if instance.ResourceTypeID != "cpu" && instance.ResourceTypeID != "memory" && instance.ResourceTypeID != "gpu" && instance.ResourceTypeID != "storage" {
			t.Fatalf("unexpected non-canonical resourceTypeId=%q in %+v", instance.ResourceTypeID, instance)
		}
		for _, measure := range append(append([]ResourceBaseInfoMeasure{}, instance.Capacity...), instance.Metrics...) {
			if err := validateResourceBaseInfoMeasureShape(measure); err != nil {
				t.Fatalf("invalid measure emitted for %s: %v measure=%+v", instance.ID, err, measure)
			}
		}
	}
	cpuInstance := instances[buildResourceBaseInfoResourceInstanceID(0, "cpu", "pool")]
	memoryInstance := instances[buildResourceBaseInfoResourceInstanceID(0, "memory", "pool")]
	storagePool := instances[buildResourceBaseInfoResourceInstanceID(0, "storage", "pool")]
	storageDisk := instances[buildResourceBaseInfoResourceInstanceID(0, "storage", "nvme0n1p3")]
	gpu0 := instances[buildResourceBaseInfoResourceInstanceID(0, "gpu", "0")]
	gpu1 := instances[buildResourceBaseInfoResourceInstanceID(0, "gpu", "1")]
	if cpuInstance.ResourceTypeID != "cpu" || memoryInstance.ResourceTypeID != "memory" || storagePool.ResourceTypeID != "storage" || storageDisk.ResourceTypeID != "storage" || gpu0.ResourceTypeID != "gpu" || gpu1.ResourceTypeID != "gpu" {
		t.Fatalf("expected canonical cpu/memory/storage/gpu resource instances, got %+v", payload.ResourceInstances)
	}
	if len(cpuInstance.Capacity) < 2 || cpuInstance.Capacity[0].Allocatable == nil || cpuInstance.Capacity[1].Used == nil {
		t.Fatalf("expected cpu pool allocatable/used measures, got %+v", cpuInstance.Capacity)
	}
	if len(memoryInstance.Capacity) < 2 || memoryInstance.Capacity[0].Allocatable == nil || memoryInstance.Capacity[1].Used == nil {
		t.Fatalf("expected memory pool allocatable/used measures, got %+v", memoryInstance.Capacity)
	}
	if len(storagePool.Capacity) < 2 || storagePool.Capacity[0].Capacity == nil || storagePool.Capacity[1].Capacity == nil {
		t.Fatalf("expected storage pool total/root measures, got %+v", storagePool.Capacity)
	}
	if len(storageDisk.Capacity) != 1 || storageDisk.Capacity[0].Capacity == nil {
		t.Fatalf("expected disk row storage capacity measure, got %+v", storageDisk.Capacity)
	}
	if len(storageDisk.Identity) == 0 || len(storageDisk.Spec) == 0 {
		t.Fatalf("expected disk row storage identity/spec, got %+v", storageDisk)
	}
	if len(gpu0.Identity) == 0 || len(gpu0.Spec) == 0 || len(gpu0.Capacity) == 0 || len(gpu0.Metrics) == 0 {
		t.Fatalf("expected gpu instance identity/spec/capacity/metrics, got %+v", gpu0)
	}
	for _, measure := range append(gpu0.Capacity, gpu0.Metrics...) {
		if measure.Available != nil {
			t.Fatalf("expected gpu0 available to be omitted when collector did not report it, got %+v", gpu0)
		}
	}
	gpu1HasAvailable := false
	for _, measure := range append(gpu1.Capacity, gpu1.Metrics...) {
		if measure.Available != nil {
			gpu1HasAvailable = true
		}
	}
	if !gpu1HasAvailable {
		t.Fatalf("expected gpu1 available measure when collector reported it, got %+v", gpu1)
	}
	gpu0HasTemperature := false
	for _, measure := range gpu0.Metrics {
		if measure.Name == "temperatureGpuCelsius" && measure.Value != nil && *measure.Value == 64 {
			gpu0HasTemperature = true
		}
	}
	if !gpu0HasTemperature {
		t.Fatalf("expected gpu0 temperature when collector reported it, got %+v", gpu0.Metrics)
	}
	if len(payload.Services) != 2 {
		t.Fatalf("services len=%d, want 2 workloads", len(payload.Services))
	}
	serviceIDs := map[string]struct{}{}
	for _, service := range payload.Services {
		serviceIDs[service.ID] = struct{}{}
		if service.EntityID != buildResourceBaseInfoEntityID(0, "host") {
			t.Fatalf("service entityId=%q, want host entity", service.EntityID)
		}
		if len(service.ResourceInstanceIDs) != 1 {
			t.Fatalf("expected each runtime workload service to link one emitted gpu instance, got %+v", service)
		}
		if service.ResourceInstanceIDs[0] != gpu0.ID && service.ResourceInstanceIDs[0] != gpu1.ID {
			t.Fatalf("expected service linkage to emitted gpu instance only, got %+v", service)
		}
	}
	if payload.Services[0].ID == payload.Services[1].ID {
		t.Fatalf("expected duplicate workload names/container names to still produce distinct service ids, got %+v", payload.Services)
	}
	if len(payload.Allocations) != 2 {
		t.Fatalf("allocations len=%d, want 2 emitted gpu references only", len(payload.Allocations))
	}
	for _, allocation := range payload.Allocations {
		if len(allocation.Claims) != 1 {
			t.Fatalf("expected one claim per allocation, got %+v", allocation)
		}
		claim := allocation.Claims[0]
		if _, ok := serviceIDs[claim.ServiceID]; !ok {
			t.Fatalf("allocation references unknown service: %+v", allocation)
		}
		if claim.ResourceInstanceID != gpu0.ID && claim.ResourceInstanceID != gpu1.ID {
			t.Fatalf("allocation references unexpected or dangling gpu instance: %+v", allocation)
		}
		if len(claim.Dimensions) == 0 {
			t.Fatalf("expected allocation claim dimensions, got %+v", allocation)
		}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(baseInfoJSON), &raw); err != nil {
		t.Fatalf("unmarshal raw canonical base info failed: %v", err)
	}
	for _, banned := range []string{"machine", "vm", "k8s", "nodes", "gpus", "carriers", "matrix", "axes", "cells"} {
		if _, exists := raw[banned]; exists {
			t.Fatalf("unexpected banned section %q in canonical payload: %s", banned, baseInfoJSON)
		}
	}
	if serviceRaw, ok := raw["services"].([]interface{}); ok {
		for _, item := range serviceRaw {
			serviceMap, _ := item.(map[string]interface{})
			if _, exists := serviceMap["allocations"]; exists {
				t.Fatalf("unexpected banned service.allocations section: %s", baseInfoJSON)
			}
		}
	}
	if instanceRaw, ok := raw["resourceInstances"].([]interface{}); ok {
		for _, item := range instanceRaw {
			instanceMap, _ := item.(map[string]interface{})
			if _, exists := instanceMap["usedBy"]; exists {
				t.Fatalf("unexpected banned resourceInstance.usedBy section: %s", baseInfoJSON)
			}
		}
	}
}

func TestParseVMBaseInfoOutput_NvidiaWhitespaceGPUDeviceRow(t *testing.T) {
	stdout := "EASYDO_BASE_INFO_BEGIN\n" +
		"EASYDO_HOSTNAME=ubuntu\n" +
		"EASYDO_PRIMARY_IPV4=10.0.0.8\n" +
		"EASYDO_OS_NAME=Ubuntu 22.04.4 LTS\n" +
		"EASYDO_OS_VERSION=22.04\n" +
		"EASYDO_KERNEL_VERSION=6.5.0-18-generic\n" +
		"EASYDO_ARCH=x86_64\n" +
		"EASYDO_CPU_MODEL=Intel(R) Xeon(R)\n" +
		"EASYDO_CPU_LOGICAL_CORES=12\n" +
		"EASYDO_CPU_USED_CORES=1.5\n" +
		"EASYDO_MEMORY_TOTAL_BYTES=6.5536e+10\n" +
		"EASYDO_MEMORY_USED_BYTES=2147483648\n" +
		"EASYDO_ROOT_TOTAL_BYTES=536870912000\n" +
		"EASYDO_TOTAL_DISK_BYTES=1099511627776\n" +
		"EASYDO_GPU_COUNT=1\n" +
		"EASYDO_RESOURCE_LABELS_JSON={}\n" +
		"EASYDO_DISK_ROWS_BEGIN\n" +
		"EASYDO_DISK_ROWS_END\n" +
		"EASYDO_GPU_CSV_BEGIN\n" +
		"0 NVIDIA GeForce GTX 1660 6144 GPU-e64306ad-7e13-c641-0308-a46034657962 00000000:01:00.0 19 5729 0 47\n" +
		"EASYDO_GPU_CSV_END\n" +
		"EASYDO_RUNTIME_WORKLOADS_BEGIN\n" +
		"EASYDO_RUNTIME_WORKLOADS_END\n" +
		"EASYDO_PROCESS_TREE_BEGIN\n" +
		"EASYDO_PROCESS_TREE_END\n" +
		"EASYDO_GPU_PROCESS_CSV_BEGIN\n" +
		"EASYDO_GPU_PROCESS_CSV_END\n" +
		"EASYDO_BASE_INFO_END\n"

	baseInfoJSON, _, _, err := parseVMBaseInfoOutput(stdout, "remote_task")
	if err != nil {
		t.Fatalf("parseVMBaseInfoOutput returned error: %v", err)
	}
	var payload ResourceBaseInfoV3
	if err := json.Unmarshal([]byte(baseInfoJSON), &payload); err != nil {
		t.Fatalf("unmarshal canonical base info failed: %v", err)
	}
	gpuID := buildResourceBaseInfoResourceInstanceID(0, "gpu", "0")
	instances := map[string]ResourceBaseInfoResourceInstance{}
	for _, instance := range payload.ResourceInstances {
		instances[instance.ID] = instance
	}
	gpu := instances[gpuID]
	if gpu.ResourceTypeID != "gpu" {
		t.Fatalf("expected parsed whitespace nvidia row to emit gpu resource instance, got %+v", payload.ResourceInstances)
	}
	if len(gpu.Spec) == 0 || len(gpu.Capacity) == 0 || len(gpu.Metrics) == 0 {
		t.Fatalf("expected parsed gpu spec/capacity/metrics, got %+v", gpu)
	}
	identityValues := map[string]interface{}{}
	for _, field := range gpu.Identity {
		identityValues[field.Name] = field.Value
	}
	if identityValues["uuid"] != "GPU-e64306ad-7e13-c641-0308-a46034657962" {
		t.Fatalf("expected whitespace nvidia uuid parsed correctly, got %+v", gpu.Identity)
	}
	if identityValues["busId"] != "00000000:01:00.0" {
		t.Fatalf("expected whitespace nvidia busId parsed correctly, got %+v", gpu.Identity)
	}
	hasMemoryCapacity := false
	hasTemperature := false
	for _, measure := range append(gpu.Capacity, gpu.Metrics...) {
		if measure.Name == "memoryBytes" && measure.Capacity != nil && *measure.Capacity == 6144*1024*1024 {
			hasMemoryCapacity = true
		}
		if measure.Name == "temperatureGpuCelsius" && measure.Value != nil && *measure.Value == 47 {
			hasTemperature = true
		}
	}
	if !hasMemoryCapacity {
		t.Fatalf("expected parsed whitespace nvidia row to emit gpu total memory, got %+v", gpu.Capacity)
	}
	if !hasTemperature {
		t.Fatalf("expected parsed whitespace nvidia row to emit gpu temperature, got %+v", gpu.Metrics)
	}
}

func TestParseVMBaseInfoOutput_NvidiaGPUDeviceMalformedTemperatureOmitted(t *testing.T) {
	stdout := "EASYDO_BASE_INFO_BEGIN\n" +
		"EASYDO_HOSTNAME=ubuntu\n" +
		"EASYDO_PRIMARY_IPV4=10.0.0.8\n" +
		"EASYDO_OS_NAME=Ubuntu 22.04.4 LTS\n" +
		"EASYDO_OS_VERSION=22.04\n" +
		"EASYDO_KERNEL_VERSION=6.5.0-18-generic\n" +
		"EASYDO_ARCH=x86_64\n" +
		"EASYDO_CPU_MODEL=Intel(R) Xeon(R)\n" +
		"EASYDO_CPU_LOGICAL_CORES=12\n" +
		"EASYDO_CPU_USED_CORES=1.5\n" +
		"EASYDO_MEMORY_TOTAL_BYTES=6.5536e+10\n" +
		"EASYDO_MEMORY_USED_BYTES=2147483648\n" +
		"EASYDO_ROOT_TOTAL_BYTES=536870912000\n" +
		"EASYDO_TOTAL_DISK_BYTES=1099511627776\n" +
		"EASYDO_GPU_COUNT=1\n" +
		"EASYDO_RESOURCE_LABELS_JSON={}\n" +
		"EASYDO_DISK_ROWS_BEGIN\n" +
		"EASYDO_DISK_ROWS_END\n" +
		"EASYDO_GPU_CSV_BEGIN\n" +
		"0,NVIDIA GeForce GTX 1660,6144,GPU-e64306ad-7e13-c641-0308-a46034657962,00000000:01:00.0,NVIDIA,19,5729,0,N/A\n" +
		"EASYDO_GPU_CSV_END\n" +
		"EASYDO_RUNTIME_WORKLOADS_BEGIN\n" +
		"EASYDO_RUNTIME_WORKLOADS_END\n" +
		"EASYDO_PROCESS_TREE_BEGIN\n" +
		"EASYDO_PROCESS_TREE_END\n" +
		"EASYDO_GPU_PROCESS_CSV_BEGIN\n" +
		"EASYDO_GPU_PROCESS_CSV_END\n" +
		"EASYDO_BASE_INFO_END\n"

	baseInfoJSON, _, _, err := parseVMBaseInfoOutput(stdout, "remote_task")
	if err != nil {
		t.Fatalf("parseVMBaseInfoOutput returned error: %v", err)
	}
	var payload ResourceBaseInfoV3
	if err := json.Unmarshal([]byte(baseInfoJSON), &payload); err != nil {
		t.Fatalf("unmarshal canonical base info failed: %v", err)
	}
	gpuID := buildResourceBaseInfoResourceInstanceID(0, "gpu", "0")
	for _, instance := range payload.ResourceInstances {
		if instance.ID != gpuID {
			continue
		}
		for _, measure := range instance.Metrics {
			if measure.Name == "temperatureGpuCelsius" {
				t.Fatalf("expected malformed temperature token to be omitted, got %+v", instance.Metrics)
			}
		}
		return
	}
	t.Fatalf("expected gpu instance %q in parsed payload, got %+v", gpuID, payload.ResourceInstances)
}

func TestParseVMBaseInfoOutput_NvidiaGPUDeviceZeroTemperaturePreserved(t *testing.T) {
	stdout := "EASYDO_BASE_INFO_BEGIN\n" +
		"EASYDO_HOSTNAME=ubuntu\n" +
		"EASYDO_PRIMARY_IPV4=10.0.0.8\n" +
		"EASYDO_OS_NAME=Ubuntu 22.04.4 LTS\n" +
		"EASYDO_OS_VERSION=22.04\n" +
		"EASYDO_KERNEL_VERSION=6.5.0-18-generic\n" +
		"EASYDO_ARCH=x86_64\n" +
		"EASYDO_CPU_MODEL=Intel(R) Xeon(R)\n" +
		"EASYDO_CPU_LOGICAL_CORES=12\n" +
		"EASYDO_CPU_USED_CORES=1.5\n" +
		"EASYDO_MEMORY_TOTAL_BYTES=6.5536e+10\n" +
		"EASYDO_MEMORY_USED_BYTES=2147483648\n" +
		"EASYDO_ROOT_TOTAL_BYTES=536870912000\n" +
		"EASYDO_TOTAL_DISK_BYTES=1099511627776\n" +
		"EASYDO_GPU_COUNT=1\n" +
		"EASYDO_RESOURCE_LABELS_JSON={}\n" +
		"EASYDO_DISK_ROWS_BEGIN\n" +
		"EASYDO_DISK_ROWS_END\n" +
		"EASYDO_GPU_CSV_BEGIN\n" +
		"0,NVIDIA GeForce GTX 1660,6144,GPU-e64306ad-7e13-c641-0308-a46034657962,00000000:01:00.0,NVIDIA,19,5729,0,0\n" +
		"EASYDO_GPU_CSV_END\n" +
		"EASYDO_RUNTIME_WORKLOADS_BEGIN\n" +
		"EASYDO_RUNTIME_WORKLOADS_END\n" +
		"EASYDO_PROCESS_TREE_BEGIN\n" +
		"EASYDO_PROCESS_TREE_END\n" +
		"EASYDO_GPU_PROCESS_CSV_BEGIN\n" +
		"EASYDO_GPU_PROCESS_CSV_END\n" +
		"EASYDO_BASE_INFO_END\n"

	baseInfoJSON, _, _, err := parseVMBaseInfoOutput(stdout, "remote_task")
	if err != nil {
		t.Fatalf("parseVMBaseInfoOutput returned error: %v", err)
	}
	var payload ResourceBaseInfoV3
	if err := json.Unmarshal([]byte(baseInfoJSON), &payload); err != nil {
		t.Fatalf("unmarshal canonical base info failed: %v", err)
	}
	gpuID := buildResourceBaseInfoResourceInstanceID(0, "gpu", "0")
	for _, instance := range payload.ResourceInstances {
		if instance.ID != gpuID {
			continue
		}
		for _, measure := range instance.Metrics {
			if measure.Name == "temperatureGpuCelsius" && measure.Value != nil && *measure.Value == 0 {
				return
			}
		}
		t.Fatalf("expected numeric zero temperature to be preserved, got %+v", instance.Metrics)
	}
	t.Fatalf("expected gpu instance %q in parsed payload, got %+v", gpuID, payload.ResourceInstances)
}

func TestParseVMBaseInfoOutput_MetaXSummaryTemperatureRow(t *testing.T) {
	stdout := "EASYDO_BASE_INFO_BEGIN\n" +
		"EASYDO_HOSTNAME=metax-host\n" +
		"EASYDO_PRIMARY_IPV4=10.0.0.9\n" +
		"EASYDO_OS_NAME=Ubuntu 22.04.4 LTS\n" +
		"EASYDO_OS_VERSION=22.04\n" +
		"EASYDO_KERNEL_VERSION=6.5.0-18-generic\n" +
		"EASYDO_ARCH=x86_64\n" +
		"EASYDO_CPU_MODEL=Intel(R) Xeon(R)\n" +
		"EASYDO_CPU_LOGICAL_CORES=16\n" +
		"EASYDO_CPU_USED_CORES=2.5\n" +
		"EASYDO_MEMORY_TOTAL_BYTES=68719476736\n" +
		"EASYDO_MEMORY_USED_BYTES=21474836480\n" +
		"EASYDO_ROOT_TOTAL_BYTES=485687422976\n" +
		"EASYDO_TOTAL_DISK_BYTES=2512510000000\n" +
		"EASYDO_GPU_COUNT=2\n" +
		"EASYDO_RESOURCE_LABELS_JSON={}\n" +
		"EASYDO_DISK_ROWS_BEGIN\n" +
		"EASYDO_DISK_ROWS_END\n" +
		"EASYDO_GPU_CSV_BEGIN\n" +
		"0,MetaX C550,, , ,MetaX,48758,,95,43\n" +
		"4,MetaX C550,65536,,,MetaX,63634,1902,88,51\n" +
		"EASYDO_GPU_CSV_END\n" +
		"EASYDO_RUNTIME_WORKLOADS_BEGIN\n" +
		"EASYDO_RUNTIME_WORKLOADS_END\n" +
		"EASYDO_PROCESS_TREE_BEGIN\n" +
		"EASYDO_PROCESS_TREE_END\n" +
		"EASYDO_GPU_PROCESS_CSV_BEGIN\n" +
		"EASYDO_GPU_PROCESS_CSV_END\n" +
		"EASYDO_BASE_INFO_END\n"

	baseInfoJSON, source, _, err := parseVMBaseInfoOutput(stdout, "remote_task")
	if err != nil {
		t.Fatalf("parseVMBaseInfoOutput returned error: %v", err)
	}
	if source != "remote_task" {
		t.Fatalf("source=%q, want remote_task", source)
	}
	var payload ResourceBaseInfoV3
	if err := json.Unmarshal([]byte(baseInfoJSON), &payload); err != nil {
		t.Fatalf("unmarshal canonical base info failed: %v", err)
	}
	gpu0ID := buildResourceBaseInfoResourceInstanceID(0, "gpu", "0")
	gpu4ID := buildResourceBaseInfoResourceInstanceID(0, "gpu", "4")
	gpuMetricsByID := map[string]map[string]float64{}
	for _, instance := range payload.ResourceInstances {
		if instance.ResourceTypeID != "gpu" {
			continue
		}
		metrics := map[string]float64{}
		for _, measure := range instance.Metrics {
			if measure.Value != nil {
				metrics[measure.Name] = *measure.Value
			}
		}
		gpuMetricsByID[instance.ID] = metrics
	}
	if gpuMetricsByID[gpu0ID]["temperatureGpuCelsius"] != 43 {
		t.Fatalf("expected gpu0 metax summary temperature, got %+v", gpuMetricsByID[gpu0ID])
	}
	if gpuMetricsByID[gpu4ID]["temperatureGpuCelsius"] != 51 {
		t.Fatalf("expected gpu4 metax summary temperature, got %+v", gpuMetricsByID[gpu4ID])
	}
}

func TestParseVMBaseInfoOutput_MetaXSelectTemperatureRow(t *testing.T) {
	stdout := "EASYDO_BASE_INFO_BEGIN\n" +
		"EASYDO_HOSTNAME=metax-host\n" +
		"EASYDO_PRIMARY_IPV4=10.0.0.9\n" +
		"EASYDO_OS_NAME=Ubuntu 22.04.4 LTS\n" +
		"EASYDO_OS_VERSION=22.04\n" +
		"EASYDO_KERNEL_VERSION=6.5.0-18-generic\n" +
		"EASYDO_ARCH=x86_64\n" +
		"EASYDO_CPU_MODEL=Intel(R) Xeon(R)\n" +
		"EASYDO_CPU_LOGICAL_CORES=16\n" +
		"EASYDO_CPU_USED_CORES=2.5\n" +
		"EASYDO_MEMORY_TOTAL_BYTES=68719476736\n" +
		"EASYDO_MEMORY_USED_BYTES=21474836480\n" +
		"EASYDO_ROOT_TOTAL_BYTES=485687422976\n" +
		"EASYDO_TOTAL_DISK_BYTES=2512510000000\n" +
		"EASYDO_GPU_COUNT=2\n" +
		"EASYDO_RESOURCE_LABELS_JSON={}\n" +
		"EASYDO_DISK_ROWS_BEGIN\n" +
		"EASYDO_DISK_ROWS_END\n" +
		"EASYDO_GPU_CSV_BEGIN\n" +
		"0,MetaX C550,, , ,MetaX,48758,,95,43\n" +
		"4,MetaX C550,65536,,,MetaX,63634,1902,88,51\n" +
		"EASYDO_GPU_CSV_END\n" +
		"EASYDO_RUNTIME_WORKLOADS_BEGIN\n" +
		"EASYDO_RUNTIME_WORKLOADS_END\n" +
		"EASYDO_PROCESS_TREE_BEGIN\n" +
		"EASYDO_PROCESS_TREE_END\n" +
		"EASYDO_GPU_PROCESS_CSV_BEGIN\n" +
		"EASYDO_GPU_PROCESS_CSV_END\n" +
		"EASYDO_BASE_INFO_END\n"

	baseInfoJSON, source, _, err := parseVMBaseInfoOutput(stdout, "remote_task")
	if err != nil {
		t.Fatalf("parseVMBaseInfoOutput returned error: %v", err)
	}
	if source != "remote_task" {
		t.Fatalf("source=%q, want remote_task", source)
	}
	var payload ResourceBaseInfoV3
	if err := json.Unmarshal([]byte(baseInfoJSON), &payload); err != nil {
		t.Fatalf("unmarshal canonical base info failed: %v", err)
	}
	gpu0ID := buildResourceBaseInfoResourceInstanceID(0, "gpu", "0")
	gpu4ID := buildResourceBaseInfoResourceInstanceID(0, "gpu", "4")
	gpuMetricsByID := map[string]map[string]float64{}
	for _, instance := range payload.ResourceInstances {
		if instance.ResourceTypeID != "gpu" {
			continue
		}
		metrics := map[string]float64{}
		for _, measure := range instance.Metrics {
			if measure.Value != nil {
				metrics[measure.Name] = *measure.Value
			}
		}
		gpuMetricsByID[instance.ID] = metrics
	}
	if gpuMetricsByID[gpu0ID]["temperatureGpuCelsius"] != 43 {
		t.Fatalf("expected gpu0 metax select temperature, got %+v", gpuMetricsByID[gpu0ID])
	}
	if gpuMetricsByID[gpu4ID]["temperatureGpuCelsius"] != 51 {
		t.Fatalf("expected gpu4 metax select temperature, got %+v", gpuMetricsByID[gpu4ID])
	}
}

func TestParseVMBaseInfoOutput_MetaXDescendantPIDMapsToRootWorkload(t *testing.T) {
	stdout := "EASYDO_BASE_INFO_BEGIN\n" +
		"EASYDO_HOSTNAME=metax-host\n" +
		"EASYDO_PRIMARY_IPV4=10.0.0.9\n" +
		"EASYDO_OS_NAME=Ubuntu 22.04.4 LTS\n" +
		"EASYDO_OS_VERSION=22.04\n" +
		"EASYDO_KERNEL_VERSION=6.5.0-18-generic\n" +
		"EASYDO_ARCH=x86_64\n" +
		"EASYDO_CPU_MODEL=Intel(R) Xeon(R)\n" +
		"EASYDO_CPU_LOGICAL_CORES=16\n" +
		"EASYDO_CPU_USED_CORES=2.5\n" +
		"EASYDO_MEMORY_TOTAL_BYTES=68719476736\n" +
		"EASYDO_MEMORY_USED_BYTES=21474836480\n" +
		"EASYDO_ROOT_TOTAL_BYTES=485687422976\n" +
		"EASYDO_TOTAL_DISK_BYTES=2512510000000\n" +
		"EASYDO_GPU_COUNT=2\n" +
		"EASYDO_RESOURCE_LABELS_JSON={}\n" +
		"EASYDO_DISK_ROWS_BEGIN\n" +
		"EASYDO_DISK_ROWS_END\n" +
		"EASYDO_GPU_CSV_BEGIN\n" +
		"0,MetaX C550,, , ,MetaX,48758,,95,43\n" +
		"4,MetaX C550,65536,,,MetaX,63634,1902,88,51\n" +
		"EASYDO_GPU_CSV_END\n" +
		"EASYDO_RUNTIME_WORKLOADS_BEGIN\n" +
		"{\"name\":\"text-generation\",\"pid\":2348402,\"runtime\":\"process\"}\n" +
		"{\"name\":\"VLLM::Worker_TP\",\"pid\":302642,\"runtime\":\"process\"}\n" +
		"{\"name\":\"text-generation\",\"pid\":2347992,\"runtime\":\"docker\",\"containerId\":\"ctr-1\",\"containerName\":\"text-generation\"}\n" +
		"{\"name\":\"vllm\",\"pid\":293763,\"runtime\":\"docker\",\"containerId\":\"ctr-2\",\"containerName\":\"vllm\"}\n" +
		"EASYDO_RUNTIME_WORKLOADS_END\n" +
		"EASYDO_PROCESS_TREE_BEGIN\n" +
		"2347992,1\n" +
		"2348027,2347992\n" +
		"2348402,2348027\n" +
		"293763,1\n" +
		"302642,293763\n" +
		"EASYDO_PROCESS_TREE_END\n" +
		"EASYDO_GPU_PROCESS_CSV_BEGIN\n" +
		"{\"gpuIndex\":0,\"pid\":2348402,\"memoryUsedBytes\":51126452224}\n" +
		"{\"gpuIndex\":4,\"pid\":302642,\"memoryUsedBytes\":66730340352}\n" +
		"EASYDO_GPU_PROCESS_CSV_END\n" +
		"EASYDO_BASE_INFO_END\n"

	baseInfoJSON, source, _, err := parseVMBaseInfoOutput(stdout, "remote_task")
	if err != nil {
		t.Fatalf("parseVMBaseInfoOutput returned error: %v", err)
	}
	if source != "remote_task" {
		t.Fatalf("source=%q, want remote_task", source)
	}
	var payload ResourceBaseInfoV3
	if err := json.Unmarshal([]byte(baseInfoJSON), &payload); err != nil {
		t.Fatalf("unmarshal canonical base info failed: %v", err)
	}
	gpu0ID := buildResourceBaseInfoResourceInstanceID(0, "gpu", "0")
	gpu4ID := buildResourceBaseInfoResourceInstanceID(0, "gpu", "4")
	serviceByPID := map[float64]ResourceBaseInfoService{}
	for _, service := range payload.Services {
		for _, field := range service.Fields {
			if field.Name == "pid" {
				if pid, ok := field.Value.(float64); ok {
					serviceByPID[pid] = service
				}
			}
		}
	}
	if len(payload.Services) != 2 {
		t.Fatalf("services len=%d, want 2 workloads", len(payload.Services))
	}
	if got := serviceByPID[2347992].ResourceInstanceIDs; len(got) != 1 || got[0] != gpu0ID {
		t.Fatalf("expected text-generation root workload to bind gpu0, got %+v", serviceByPID[2347992])
	}
	if got := serviceByPID[293763].ResourceInstanceIDs; len(got) != 1 || got[0] != gpu4ID {
		t.Fatalf("expected vllm root workload to bind gpu4, got %+v", serviceByPID[293763])
	}
	gpuMetricsByID := map[string]map[string]float64{}
	for _, instance := range payload.ResourceInstances {
		if instance.ResourceTypeID != "gpu" {
			continue
		}
		metrics := map[string]float64{}
		for _, measure := range instance.Metrics {
			if measure.Value != nil {
				metrics[measure.Name] = *measure.Value
			}
		}
		gpuMetricsByID[instance.ID] = metrics
	}
	if gpuMetricsByID[gpu0ID]["temperatureGpuCelsius"] != 43 {
		t.Fatalf("expected gpu0 metax temperature, got %+v", gpuMetricsByID[gpu0ID])
	}
	if gpuMetricsByID[gpu4ID]["temperatureGpuCelsius"] != 51 {
		t.Fatalf("expected gpu4 metax temperature, got %+v", gpuMetricsByID[gpu4ID])
	}
	if len(payload.Allocations) != 2 {
		t.Fatalf("allocations len=%d, want 2", len(payload.Allocations))
	}
	for _, allocation := range payload.Allocations {
		if len(allocation.Claims) != 1 {
			t.Fatalf("expected one claim per allocation, got %+v", allocation)
		}
		claim := allocation.Claims[0]
		if claim.ResourceInstanceID != gpu0ID && claim.ResourceInstanceID != gpu4ID {
			t.Fatalf("unexpected gpu resource instance: %+v", claim)
		}
		dimensions := map[string]interface{}{}
		for _, field := range claim.Dimensions {
			dimensions[field.Name] = field.Value
		}
		pid, _ := dimensions["pid"].(float64)
		observedPID, _ := dimensions["observedPid"].(float64)
		if claim.ResourceInstanceID == gpu0ID {
			if pid != float64(2347992) || observedPID != float64(2348402) {
				t.Fatalf("expected reconciled and observed pid dimensions for text-generation, got %+v", claim.Dimensions)
			}
		}
		if claim.ResourceInstanceID == gpu4ID {
			if pid != float64(293763) || observedPID != float64(302642) {
				t.Fatalf("expected reconciled and observed pid dimensions for vllm, got %+v", claim.Dimensions)
			}
		}
	}
}

func TestParseVMBaseInfoOutput_DedupesRepeatedGPUProcessRows(t *testing.T) {
	stdout := "EASYDO_BASE_INFO_BEGIN\n" +
		"EASYDO_HOSTNAME=dup-host\n" +
		"EASYDO_PRIMARY_IPV4=10.0.0.10\n" +
		"EASYDO_OS_NAME=Ubuntu 22.04.4 LTS\n" +
		"EASYDO_OS_VERSION=22.04\n" +
		"EASYDO_KERNEL_VERSION=6.5.0-18-generic\n" +
		"EASYDO_ARCH=x86_64\n" +
		"EASYDO_CPU_MODEL=Intel(R) Xeon(R)\n" +
		"EASYDO_CPU_LOGICAL_CORES=16\n" +
		"EASYDO_CPU_USED_CORES=1.0\n" +
		"EASYDO_MEMORY_TOTAL_BYTES=68719476736\n" +
		"EASYDO_MEMORY_USED_BYTES=21474836480\n" +
		"EASYDO_ROOT_TOTAL_BYTES=485687422976\n" +
		"EASYDO_TOTAL_DISK_BYTES=2512510000000\n" +
		"EASYDO_GPU_COUNT=1\n" +
		"EASYDO_RESOURCE_LABELS_JSON={}\n" +
		"EASYDO_DISK_ROWS_BEGIN\n" +
		"EASYDO_DISK_ROWS_END\n" +
		"EASYDO_GPU_CSV_BEGIN\n" +
		"0,NVIDIA H100 PCIe,81920,GPU-aaa,0000:01:00.0,NVIDIA,2048,79872,12,41\n" +
		"EASYDO_GPU_CSV_END\n" +
		"EASYDO_RUNTIME_WORKLOADS_BEGIN\n" +
		"{\"name\":\"trainer\",\"pid\":4242,\"runtime\":\"docker\",\"containerId\":\"ctr-1\",\"containerName\":\"trainer\"}\n" +
		"EASYDO_RUNTIME_WORKLOADS_END\n" +
		"EASYDO_PROCESS_TREE_BEGIN\n" +
		"4242,1\n" +
		"4243,4242\n" +
		"EASYDO_PROCESS_TREE_END\n" +
		"EASYDO_GPU_PROCESS_CSV_BEGIN\n" +
		"{\"gpuIndex\":0,\"pid\":4243,\"memoryUsedBytes\":8589934592}\n" +
		"{\"gpuIndex\":0,\"pid\":4243,\"memoryUsedBytes\":8589934592}\n" +
		"EASYDO_GPU_PROCESS_CSV_END\n" +
		"EASYDO_BASE_INFO_END\n"

	baseInfoJSON, source, _, err := parseVMBaseInfoOutput(stdout, "remote_task")
	if err != nil {
		t.Fatalf("parseVMBaseInfoOutput returned error: %v", err)
	}
	if source != "remote_task" {
		t.Fatalf("source=%q, want remote_task", source)
	}
	var payload ResourceBaseInfoV3
	if err := json.Unmarshal([]byte(baseInfoJSON), &payload); err != nil {
		t.Fatalf("unmarshal canonical base info failed: %v", err)
	}
	if len(payload.Allocations) != 1 {
		t.Fatalf("allocations len=%d, want 1 deduped allocation", len(payload.Allocations))
	}
	if len(payload.Allocations[0].Claims) != 1 {
		t.Fatalf("claims len=%d, want 1 deduped claim", len(payload.Allocations[0].Claims))
	}
	claim := payload.Allocations[0].Claims[0]
	dimensions := map[string]interface{}{}
	for _, field := range claim.Dimensions {
		dimensions[field.Name] = field.Value
	}
	if pid, _ := dimensions["pid"].(float64); pid != 4242 {
		t.Fatalf("expected reconciled root pid dimension, got %+v", claim.Dimensions)
	}
	if observedPID, _ := dimensions["observedPid"].(float64); observedPID != 4243 {
		t.Fatalf("expected observed descendant pid dimension, got %+v", claim.Dimensions)
	}
	if memoryUsedBytes, _ := dimensions["memoryUsedBytes"].(float64); memoryUsedBytes != 8589934592 {
		t.Fatalf("expected deduped memoryUsedBytes dimension, got %+v", claim.Dimensions)
	}
	if got := len(payload.Services[0].ResourceInstanceIDs); got != 1 {
		t.Fatalf("service gpu bindings len=%d, want 1 deduped binding", got)
	}
}

func TestParseVMBaseInfoOutput_SynthesizesProcessWorkloadWhenRuntimeWorkloadsMissing(t *testing.T) {
	stdout := "EASYDO_BASE_INFO_BEGIN\n" +
		"EASYDO_HOSTNAME=fallback-host\n" +
		"EASYDO_PRIMARY_IPV4=10.0.0.11\n" +
		"EASYDO_OS_NAME=Ubuntu 22.04.4 LTS\n" +
		"EASYDO_OS_VERSION=22.04\n" +
		"EASYDO_KERNEL_VERSION=6.5.0-18-generic\n" +
		"EASYDO_ARCH=x86_64\n" +
		"EASYDO_CPU_MODEL=Intel(R) Xeon(R)\n" +
		"EASYDO_CPU_LOGICAL_CORES=16\n" +
		"EASYDO_CPU_USED_CORES=1.0\n" +
		"EASYDO_MEMORY_TOTAL_BYTES=68719476736\n" +
		"EASYDO_MEMORY_USED_BYTES=21474836480\n" +
		"EASYDO_ROOT_TOTAL_BYTES=485687422976\n" +
		"EASYDO_TOTAL_DISK_BYTES=2512510000000\n" +
		"EASYDO_GPU_COUNT=1\n" +
		"EASYDO_RESOURCE_LABELS_JSON={}\n" +
		"EASYDO_DISK_ROWS_BEGIN\n" +
		"EASYDO_DISK_ROWS_END\n" +
		"EASYDO_GPU_CSV_BEGIN\n" +
		"0,NVIDIA H100 PCIe,81920,GPU-aaa,0000:01:00.0,NVIDIA,2048,79872,12,41\n" +
		"EASYDO_GPU_CSV_END\n" +
		"EASYDO_RUNTIME_WORKLOADS_BEGIN\n" +
		"EASYDO_RUNTIME_WORKLOADS_END\n" +
		"EASYDO_PROCESS_TREE_BEGIN\n" +
		"4242,1\n" +
		"EASYDO_PROCESS_TREE_END\n" +
		"EASYDO_GPU_PROCESS_CSV_BEGIN\n" +
		"{\"gpuIndex\":0,\"pid\":4242,\"memoryUsedBytes\":8589934592}\n" +
		"EASYDO_GPU_PROCESS_CSV_END\n" +
		"EASYDO_BASE_INFO_END\n"

	baseInfoJSON, source, _, err := parseVMBaseInfoOutput(stdout, "remote_task")
	if err != nil {
		t.Fatalf("parseVMBaseInfoOutput returned error: %v", err)
	}
	if source != "remote_task" {
		t.Fatalf("source=%q, want remote_task", source)
	}
	var payload ResourceBaseInfoV3
	if err := json.Unmarshal([]byte(baseInfoJSON), &payload); err != nil {
		t.Fatalf("unmarshal canonical base info failed: %v", err)
	}
	if len(payload.Services) != 1 {
		t.Fatalf("services len=%d, want 1 synthesized process workload", len(payload.Services))
	}
	service := payload.Services[0]
	runtimeByName := map[string]interface{}{}
	for _, field := range service.Fields {
		runtimeByName[field.Name] = field.Value
	}
	if runtimeByName["runtime"] != "process" {
		t.Fatalf("expected synthesized process runtime, got %+v", service.Fields)
	}
	if pid, _ := runtimeByName["pid"].(float64); pid != 4242 {
		t.Fatalf("expected synthesized process pid, got %+v", service.Fields)
	}
	if service.Name != "pid-4242" {
		t.Fatalf("expected synthesized process fallback name, got %+v", service)
	}
	gpu0ID := buildResourceBaseInfoResourceInstanceID(0, "gpu", "0")
	if got := len(service.ResourceInstanceIDs); got != 1 || service.ResourceInstanceIDs[0] != gpu0ID {
		t.Fatalf("expected synthesized service to bind gpu0, got %+v", service)
	}
	if len(payload.Allocations) != 1 {
		t.Fatalf("allocations len=%d, want 1 synthesized allocation", len(payload.Allocations))
	}
	if len(payload.Allocations[0].Claims) != 1 {
		t.Fatalf("claims len=%d, want 1 synthesized claim", len(payload.Allocations[0].Claims))
	}
	claim := payload.Allocations[0].Claims[0]
	if claim.ResourceInstanceID != gpu0ID {
		t.Fatalf("expected synthesized allocation to target gpu0, got %+v", claim)
	}
	dimensions := map[string]interface{}{}
	for _, field := range claim.Dimensions {
		dimensions[field.Name] = field.Value
	}
	if pid, _ := dimensions["pid"].(float64); pid != 4242 {
		t.Fatalf("expected synthesized claim pid, got %+v", claim.Dimensions)
	}
	if memoryUsedBytes, _ := dimensions["memoryUsedBytes"].(float64); memoryUsedBytes != 8589934592 {
		t.Fatalf("expected synthesized claim memoryUsedBytes, got %+v", claim.Dimensions)
	}
}

func TestParseVMBaseInfoOutput_DoesNotSynthesizeProcessWorkloadForUnknownGPUIndex(t *testing.T) {
	stdout := "EASYDO_BASE_INFO_BEGIN\n" +
		"EASYDO_HOSTNAME=fallback-host\n" +
		"EASYDO_PRIMARY_IPV4=10.0.0.11\n" +
		"EASYDO_OS_NAME=Ubuntu 22.04.4 LTS\n" +
		"EASYDO_OS_VERSION=22.04\n" +
		"EASYDO_KERNEL_VERSION=6.5.0-18-generic\n" +
		"EASYDO_ARCH=x86_64\n" +
		"EASYDO_CPU_MODEL=Intel(R) Xeon(R)\n" +
		"EASYDO_CPU_LOGICAL_CORES=16\n" +
		"EASYDO_CPU_USED_CORES=1.0\n" +
		"EASYDO_MEMORY_TOTAL_BYTES=68719476736\n" +
		"EASYDO_MEMORY_USED_BYTES=21474836480\n" +
		"EASYDO_ROOT_TOTAL_BYTES=485687422976\n" +
		"EASYDO_TOTAL_DISK_BYTES=2512510000000\n" +
		"EASYDO_GPU_COUNT=1\n" +
		"EASYDO_RESOURCE_LABELS_JSON={}\n" +
		"EASYDO_DISK_ROWS_BEGIN\n" +
		"EASYDO_DISK_ROWS_END\n" +
		"EASYDO_GPU_CSV_BEGIN\n" +
		"0,NVIDIA H100 PCIe,81920,GPU-aaa,0000:01:00.0,NVIDIA,2048,79872,12,41\n" +
		"EASYDO_GPU_CSV_END\n" +
		"EASYDO_RUNTIME_WORKLOADS_BEGIN\n" +
		"EASYDO_RUNTIME_WORKLOADS_END\n" +
		"EASYDO_PROCESS_TREE_BEGIN\n" +
		"4242,1\n" +
		"EASYDO_PROCESS_TREE_END\n" +
		"EASYDO_GPU_PROCESS_CSV_BEGIN\n" +
		"{\"gpuIndex\":9,\"pid\":4242,\"memoryUsedBytes\":8589934592}\n" +
		"EASYDO_GPU_PROCESS_CSV_END\n" +
		"EASYDO_BASE_INFO_END\n"

	baseInfoJSON, source, _, err := parseVMBaseInfoOutput(stdout, "remote_task")
	if err != nil {
		t.Fatalf("parseVMBaseInfoOutput returned error: %v", err)
	}
	if source != "remote_task" {
		t.Fatalf("source=%q, want remote_task", source)
	}
	var payload ResourceBaseInfoV3
	if err := json.Unmarshal([]byte(baseInfoJSON), &payload); err != nil {
		t.Fatalf("unmarshal canonical base info failed: %v", err)
	}
	if len(payload.Services) != 0 {
		t.Fatalf("services len=%d, want 0 when gpu index is unknown", len(payload.Services))
	}
	if len(payload.Allocations) != 0 {
		t.Fatalf("allocations len=%d, want 0 when gpu index is unknown", len(payload.Allocations))
	}
}

func TestBuildResourceBaseInfoJSON_VMCanonicalMergeKeepsOnlyTopLevelDurableLabels(t *testing.T) {
	paramsJSON, err := json.Marshal(resourceBaseInfoTaskPayload{
		Collection: resourceBaseInfoCollectionSnapshot{
			Kind:            "resource_base_info_refresh",
			ResourceID:      42,
			ResourceType:    models.ResourceTypeVM,
			CollectorSource: "remote_task",
		},
		NodeConfig: map[string]interface{}{
			"resource_labels": map[string]interface{}{
				"tier":       "infra",
				"owner":      "platform",
				"resourceId": "bad",
				"vm":         map[string]interface{}{"bad": true},
			},
			"labels": map[string]interface{}{
				"fromGeneric": "should-not-merge",
				"owner":       "generic-owner",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal task params failed: %v", err)
	}
	stdout := "EASYDO_BASE_INFO_BEGIN\n" +
		"EASYDO_HOSTNAME=ubuntu\n" +
		"EASYDO_PRIMARY_IPV4=10.0.0.8\n" +
		"EASYDO_OS_NAME=Ubuntu 22.04.4 LTS\n" +
		"EASYDO_OS_VERSION=22.04\n" +
		"EASYDO_KERNEL_VERSION=6.5.0-18-generic\n" +
		"EASYDO_ARCH=x86_64\n" +
		"EASYDO_CPU_MODEL=Intel(R) Xeon(R)\n" +
		"EASYDO_CPU_LOGICAL_CORES=12\n" +
		"EASYDO_CPU_USED_CORES=1.5\n" +
		"EASYDO_MEMORY_TOTAL_BYTES=6.5536e+10\n" +
		"EASYDO_MEMORY_USED_BYTES=2147483648\n" +
		"EASYDO_GPU_COUNT=0\n" +
		"EASYDO_RESOURCE_LABELS_JSON={\"tier\":\"infra\"}\n" +
		"EASYDO_DISK_ROWS_BEGIN\n" +
		"EASYDO_DISK_ROWS_END\n" +
		"EASYDO_GPU_CSV_BEGIN\n" +
		"EASYDO_GPU_CSV_END\n" +
		"EASYDO_RUNTIME_WORKLOADS_BEGIN\n" +
		"EASYDO_RUNTIME_WORKLOADS_END\n" +
		"EASYDO_PROCESS_TREE_BEGIN\n" +
		"EASYDO_PROCESS_TREE_END\n" +
		"EASYDO_GPU_PROCESS_CSV_BEGIN\n" +
		"EASYDO_GPU_PROCESS_CSV_END\n" +
		"EASYDO_BASE_INFO_END\n"

	baseInfoJSON, _, _, err := buildResourceBaseInfoJSON(&models.AgentTask{Params: string(paramsJSON)}, map[string]interface{}{"stdout": stdout})
	if err != nil {
		t.Fatalf("buildResourceBaseInfoJSON returned error: %v", err)
	}
	var payload ResourceBaseInfoV3
	if err := json.Unmarshal([]byte(baseInfoJSON), &payload); err != nil {
		t.Fatalf("unmarshal canonical base info failed: %v", err)
	}
	if payload.ResourceID != buildResourceBaseInfoResourcePrefix(42) {
		t.Fatalf("resourceId=%q, want %q", payload.ResourceID, buildResourceBaseInfoResourcePrefix(42))
	}
	if payload.Labels["tier"] != "infra" || payload.Labels["owner"] != "platform" {
		t.Fatalf("expected durable top-level labels merged, got %#v", payload.Labels)
	}
	if _, exists := payload.Labels["resourceId"]; exists {
		t.Fatalf("expected reserved resourceId label to be filtered, got %#v", payload.Labels)
	}
	if _, exists := payload.Labels["vm"]; exists {
		t.Fatalf("expected legacy vm label block to stay out of canonical labels, got %#v", payload.Labels)
	}
	if _, exists := payload.Labels["fromGeneric"]; exists {
		t.Fatalf("expected generic nodeConfig.labels to stay out of canonical labels, got %#v", payload.Labels)
	}
	if payload.Labels["owner"] != "platform" {
		t.Fatalf("expected resource_labels owner to win and generic labels owner to be ignored, got %#v", payload.Labels)
	}
}

func TestParseK8sBaseInfoOutput_EmitsCanonicalBaseInfoV3(t *testing.T) {
	stdout := "EASYDO_K8S_VERSION_BEGIN\n" +
		`{"serverVersion":{"gitVersion":"v1.29.3"}}` + "\n" +
		"EASYDO_K8S_VERSION_END\n" +
		"EASYDO_K8S_NODES_BEGIN\n" +
		`{"items":[{"metadata":{"name":"node-a","labels":{"node-role.kubernetes.io/control-plane":"","kubernetes.io/hostname":"node-a"},"annotations":{"easydo.io/gpu-cards":"[{\"name\":\"gpu0\",\"uuid\":\"GPU-aaa\",\"busId\":\"0000:81:00.0\",\"vendor\":\"NVIDIA\",\"model\":\"NVIDIA H100 PCIe\",\"memoryBytes\":85899345920}]"}},"status":{"nodeInfo":{"architecture":"amd64","osImage":"Ubuntu 22.04.4 LTS","kubeletVersion":"v1.29.3"},"capacity":{"cpu":"8","memory":"32Gi","pods":"110","nvidia.com/gpu":"1"},"allocatable":{"cpu":"7500m","memory":"30Gi","pods":"100","nvidia.com/gpu":"1"}}},{"metadata":{"name":"node-b","labels":{"node-role.kubernetes.io/worker":""}},"status":{"nodeInfo":{"architecture":"amd64","osImage":"Ubuntu 22.04.4 LTS","kubeletVersion":"v1.29.3"},"capacity":{"cpu":"16","memory":"64Gi","pods":"110"},"allocatable":{"cpu":"15500m","memory":"60Gi","pods":"100"}}}]}` + "\n" +
		"EASYDO_K8S_NODES_END\n" +
		"EASYDO_K8S_PODS_BEGIN\n" +
		`{"items":[{"metadata":{"uid":"pod-uid-1","name":"trainer-a","namespace":"ml"},"spec":{"nodeName":"node-a","containers":[{"name":"trainer","resources":{"requests":{"cpu":"500m","memory":"1Gi","nvidia.com/gpu":"1"}}}]},"status":{"phase":"Running"}},{"metadata":{"uid":"pod-uid-2","name":"trainer-b","namespace":"ml"},"spec":{"nodeName":"node-b","containers":[{"name":"trainer","resources":{"requests":{"cpu":"250m","memory":"512Mi"}}}]},"status":{"phase":"Pending"}},{"metadata":{"name":"missing-uid","namespace":"default"},"spec":{"nodeName":"node-a","containers":[{"name":"sidecar","resources":{"requests":{"cpu":"100m","memory":"128Mi"}}}]},"status":{"phase":"Running"}}]}` + "\n" +
		"EASYDO_K8S_PODS_END\n"

	baseInfoJSON, source, _, err := parseK8sBaseInfoOutput(stdout, "k8s_api")
	if err != nil {
		t.Fatalf("parseK8sBaseInfoOutput returned error: %v", err)
	}
	if source != "k8s_api" {
		t.Fatalf("source=%q, want k8s_api", source)
	}
	var payload ResourceBaseInfoV3
	if err := json.Unmarshal([]byte(baseInfoJSON), &payload); err != nil {
		t.Fatalf("unmarshal canonical k8s base info failed: %v", err)
	}
	if payload.SchemaVersion != 3 || payload.Status != "success" || payload.Source != "k8s_api" || payload.CollectedAt == "" {
		t.Fatalf("unexpected top-level metadata: %+v", payload)
	}
	if payload.ResourceID != buildResourceBaseInfoResourcePrefix(0) {
		t.Fatalf("resourceId=%q, want %q", payload.ResourceID, buildResourceBaseInfoResourcePrefix(0))
	}
	if len(payload.Entities) != 2 {
		t.Fatalf("entities len=%d, want 2 nodes", len(payload.Entities))
	}
	if len(payload.ResourceTypes) != len(approvedResourceBaseInfoResourceTypes()) {
		t.Fatalf("resourceTypes len=%d, want approved dictionary len=%d", len(payload.ResourceTypes), len(approvedResourceBaseInfoResourceTypes()))
	}
	for _, resourceType := range payload.ResourceTypes {
		approved := false
		for _, approvedType := range approvedResourceBaseInfoResourceTypes() {
			if resourceType.ID == approvedType.ID {
				approved = true
				break
			}
		}
		if !approved {
			t.Fatalf("unexpected resourceType id=%q in %+v", resourceType.ID, payload.ResourceTypes)
		}
	}
	instances := map[string]ResourceBaseInfoResourceInstance{}
	for _, instance := range payload.ResourceInstances {
		instances[instance.ID] = instance
		if instance.ResourceTypeID != "cpu" && instance.ResourceTypeID != "memory" && instance.ResourceTypeID != "gpu" {
			t.Fatalf("unexpected non-canonical resourceTypeId=%q in %+v", instance.ResourceTypeID, instance)
		}
		for _, measure := range append(append([]ResourceBaseInfoMeasure{}, instance.Capacity...), instance.Metrics...) {
			if err := validateResourceBaseInfoMeasureShape(measure); err != nil {
				t.Fatalf("invalid measure emitted for %s: %v measure=%+v", instance.ID, err, measure)
			}
			if measure.Available != nil {
				t.Fatalf("expected available to be omitted unless directly observable, got %+v in %+v", measure, instance)
			}
		}
	}
	nodeAEntityID := buildResourceBaseInfoEntityID(0, "node-a")
	nodeBEntityID := buildResourceBaseInfoEntityID(0, "node-b")
	cpuA := instances[buildResourceBaseInfoResourceInstanceID(0, "cpu", "node-a-pool")]
	memoryA := instances[buildResourceBaseInfoResourceInstanceID(0, "memory", "node-a-pool")]
	cpuB := instances[buildResourceBaseInfoResourceInstanceID(0, "cpu", "node-b-pool")]
	memoryB := instances[buildResourceBaseInfoResourceInstanceID(0, "memory", "node-b-pool")]
	gpu0 := instances[buildResourceBaseInfoResourceInstanceID(0, "gpu", "GPU-aaa")]
	if cpuA.EntityID != nodeAEntityID || memoryA.EntityID != nodeAEntityID || cpuB.EntityID != nodeBEntityID || memoryB.EntityID != nodeBEntityID {
		t.Fatalf("expected node-scoped cpu/memory pools, got %+v", payload.ResourceInstances)
	}
	if len(cpuA.Capacity) < 2 || cpuA.Capacity[0].Allocatable == nil || cpuA.Capacity[1].Used == nil {
		t.Fatalf("expected node-a cpu pool allocatable/used measures, got %+v", cpuA.Capacity)
	}
	if *cpuA.Capacity[0].Allocatable != 7500 || *cpuA.Capacity[1].Used != 500 {
		t.Fatalf("expected node-a cpu allocatable=7500 used=500, got %+v", cpuA.Capacity)
	}
	if len(memoryA.Capacity) < 2 || memoryA.Capacity[0].Allocatable == nil || memoryA.Capacity[1].Used == nil {
		t.Fatalf("expected node-a memory pool allocatable/used measures, got %+v", memoryA.Capacity)
	}
	if *memoryA.Capacity[1].Used != float64(1024*1024*1024) {
		t.Fatalf("expected node-a memory used from observable pod requests, got %+v", memoryA.Capacity)
	}
	if len(gpu0.Identity) == 0 || len(gpu0.Spec) == 0 || len(gpu0.Capacity) != 1 || gpu0.ResourceTypeID != "gpu" || gpu0.EntityID != nodeAEntityID {
		t.Fatalf("expected gpu card resource instance when identity is observable, got %+v", gpu0)
	}
	if len(payload.Services) != 2 {
		t.Fatalf("services len=%d, want 2 pods with uid only", len(payload.Services))
	}
	serviceIDs := map[string]ResourceBaseInfoService{}
	for _, service := range payload.Services {
		serviceIDs[service.ID] = service
		if service.ID != buildResourceBaseInfoServiceID(0, "pod-uid-1") && service.ID != buildResourceBaseInfoServiceID(0, "pod-uid-2") {
			t.Fatalf("expected service ids derived from pod uid only, got %+v", service)
		}
		if len(service.ResourceInstanceIDs) != 0 {
			t.Fatalf("expected no fabricated card bindings on services, got %+v", service)
		}
	}
	if serviceIDs[buildResourceBaseInfoServiceID(0, "pod-uid-1")].EntityID != nodeAEntityID {
		t.Fatalf("expected pod-uid-1 service bound to node-a entity, got %+v", serviceIDs[buildResourceBaseInfoServiceID(0, "pod-uid-1")])
	}
	if len(payload.Allocations) != 0 {
		t.Fatalf("expected no allocations when card-level binding is unavailable, got %+v", payload.Allocations)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(baseInfoJSON), &raw); err != nil {
		t.Fatalf("unmarshal raw canonical base info failed: %v", err)
	}
	for _, banned := range []string{"machine", "vm", "k8s", "nodes", "gpus", "carriers", "matrix", "axes", "cells"} {
		if _, exists := raw[banned]; exists {
			t.Fatalf("unexpected banned section %q in canonical payload: %s", banned, baseInfoJSON)
		}
	}
	if serviceRaw, ok := raw["services"].([]interface{}); ok {
		for _, item := range serviceRaw {
			serviceMap, _ := item.(map[string]interface{})
			if _, exists := serviceMap["allocations"]; exists {
				t.Fatalf("unexpected banned service.allocations section: %s", baseInfoJSON)
			}
		}
	}
	if instanceRaw, ok := raw["resourceInstances"].([]interface{}); ok {
		for _, item := range instanceRaw {
			instanceMap, _ := item.(map[string]interface{})
			if _, exists := instanceMap["usedBy"]; exists {
				t.Fatalf("unexpected banned resourceInstance.usedBy section: %s", baseInfoJSON)
			}
		}
	}
}

func TestParseK8sBaseInfoOutput_MergesExplicitAndAnnotationGPUCards(t *testing.T) {
	stdout := "EASYDO_K8S_VERSION_BEGIN\n" +
		`{"serverVersion":{"gitVersion":"v1.29.3"}}` + "\n" +
		"EASYDO_K8S_VERSION_END\n" +
		"EASYDO_K8S_NODES_BEGIN\n" +
		`{"items":[{"metadata":{"name":"node-a"},"status":{"nodeInfo":{"architecture":"amd64"},"allocatable":{"cpu":"4000m","memory":"8Gi","pods":"100"}}},{"metadata":{"name":"node-b","annotations":{"easydo.io/gpu-cards":"[{\"name\":\"ann-gpu\",\"uuid\":\"GPU-ann-b\",\"memoryBytes\":24576}]"}},"status":{"nodeInfo":{"architecture":"amd64"},"allocatable":{"cpu":"4000m","memory":"8Gi","pods":"100"}}}]}` + "\n" +
		"EASYDO_K8S_NODES_END\n" +
		"EASYDO_K8S_GPU_CARDS_BEGIN\n" +
		`{"items":[{"nodeName":"node-a","name":"doc-gpu","uuid":"GPU-doc-a","memoryBytes":49152}]}` + "\n" +
		"EASYDO_K8S_GPU_CARDS_END\n" +
		`EASYDO_K8S_PODS_BEGIN
{"items":[]}
EASYDO_K8S_PODS_END
`

	baseInfoJSON, _, _, err := parseK8sBaseInfoOutput(stdout, "k8s_api")
	if err != nil {
		t.Fatalf("parseK8sBaseInfoOutput returned error: %v", err)
	}
	var payload ResourceBaseInfoV3
	if err := json.Unmarshal([]byte(baseInfoJSON), &payload); err != nil {
		t.Fatalf("unmarshal canonical k8s base info failed: %v", err)
	}
	gpuCount := 0
	seen := map[string]bool{}
	for _, instance := range payload.ResourceInstances {
		if instance.ResourceTypeID != "gpu" {
			continue
		}
		gpuCount++
		for _, field := range instance.Identity {
			if field.Name == "uuid" {
				seen[convertToString(field.Value)] = true
			}
		}
	}
	if gpuCount != 2 {
		t.Fatalf("gpu resourceInstances len=%d, want 2 from mixed explicit+annotation sources", gpuCount)
	}
	if !seen["GPU-doc-a"] || !seen["GPU-ann-b"] {
		t.Fatalf("expected both explicit and annotation gpu cards, got %+v", payload.ResourceInstances)
	}
}

func TestParseK8sBaseInfoOutput_NodeScopedFallbackGPUIdentityAndBindings(t *testing.T) {
	stdout := "EASYDO_K8S_VERSION_BEGIN\n" +
		`{"serverVersion":{"gitVersion":"v1.29.3"}}` + "\n" +
		"EASYDO_K8S_VERSION_END\n" +
		"EASYDO_K8S_NODES_BEGIN\n" +
		`{"items":[{"metadata":{"name":"node-a","annotations":{"easydo.io/gpu-cards":"[{\"name\":\"gpu0\",\"memoryBytes\":24576}]"}},"status":{"nodeInfo":{"architecture":"amd64"},"allocatable":{"cpu":"4000m","memory":"8Gi","pods":"100"}}},{"metadata":{"name":"node-b","annotations":{"easydo.io/gpu-cards":"[{\"name\":\"gpu0\",\"memoryBytes\":24576}]"}},"status":{"nodeInfo":{"architecture":"amd64"},"allocatable":{"cpu":"4000m","memory":"8Gi","pods":"100"}}}]}` + "\n" +
		"EASYDO_K8S_NODES_END\n" +
		"EASYDO_K8S_PODS_BEGIN\n" +
		`{"items":[{"metadata":{"uid":"pod-uid-1","name":"trainer-a","namespace":"ml","annotations":{"easydo.io/gpu-card-ids":"gpu0"}},"spec":{"nodeName":"node-a","containers":[{"name":"trainer","resources":{"requests":{"cpu":"250m","memory":"512Mi"}}}]},"status":{"phase":"Running"}},{"metadata":{"uid":"pod-uid-2","name":"trainer-b","namespace":"ml","annotations":{"easydo.io/gpu-card-ids":"gpu0"}},"spec":{"nodeName":"node-b","containers":[{"name":"trainer","resources":{"requests":{"cpu":"250m","memory":"512Mi"}}}]},"status":{"phase":"Running"}}]}` + "\n" +
		"EASYDO_K8S_PODS_END\n"

	baseInfoJSON, _, _, err := parseK8sBaseInfoOutput(stdout, "k8s_api")
	if err != nil {
		t.Fatalf("parseK8sBaseInfoOutput returned error: %v", err)
	}
	var payload ResourceBaseInfoV3
	if err := json.Unmarshal([]byte(baseInfoJSON), &payload); err != nil {
		t.Fatalf("unmarshal canonical k8s base info failed: %v", err)
	}
	nodeAGPU := buildResourceBaseInfoResourceInstanceID(0, "gpu", k8sGPUCardIdentityKey(k8sGPUCardIdentity{NodeName: "node-a", Name: "gpu0"}))
	nodeBGPU := buildResourceBaseInfoResourceInstanceID(0, "gpu", k8sGPUCardIdentityKey(k8sGPUCardIdentity{NodeName: "node-b", Name: "gpu0"}))
	if nodeAGPU == nodeBGPU {
		t.Fatalf("expected node-scoped fallback gpu ids to differ, got %q", nodeAGPU)
	}
	serviceByID := map[string]ResourceBaseInfoService{}
	for _, service := range payload.Services {
		serviceByID[service.ID] = service
	}
	if got := serviceByID[buildResourceBaseInfoServiceID(0, "pod-uid-1")].ResourceInstanceIDs; len(got) != 1 || got[0] != nodeAGPU {
		t.Fatalf("expected pod-uid-1 to bind node-a gpu, got %+v", got)
	}
	if got := serviceByID[buildResourceBaseInfoServiceID(0, "pod-uid-2")].ResourceInstanceIDs; len(got) != 1 || got[0] != nodeBGPU {
		t.Fatalf("expected pod-uid-2 to bind node-b gpu, got %+v", got)
	}
	allocationTargets := map[string]bool{}
	for _, allocation := range payload.Allocations {
		for _, claim := range allocation.Claims {
			allocationTargets[claim.ResourceInstanceID] = true
		}
	}
	if !allocationTargets[nodeAGPU] || !allocationTargets[nodeBGPU] || len(allocationTargets) != 2 {
		t.Fatalf("expected distinct node-scoped allocation targets, got %+v", payload.Allocations)
	}
}

func TestBuildResourceBaseInfoJSON_K8sCanonicalMergeKeepsOnlyTopLevelDurableLabels(t *testing.T) {
	paramsJSON, err := json.Marshal(resourceBaseInfoTaskPayload{
		Collection: resourceBaseInfoCollectionSnapshot{
			Kind:            "resource_base_info_refresh",
			ResourceID:      43,
			ResourceType:    models.ResourceTypeK8sCluster,
			CollectorSource: "k8s_api",
		},
		NodeConfig: map[string]interface{}{
			"resource_labels": map[string]interface{}{
				"tier":       "infra",
				"owner":      "platform",
				"resourceId": "bad",
				"k8s":        map[string]interface{}{"bad": true},
			},
			"labels": map[string]interface{}{
				"fromGeneric": "should-not-merge",
				"owner":       "generic-owner",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal task params failed: %v", err)
	}
	stdout := "EASYDO_K8S_VERSION_BEGIN\n" +
		`{"serverVersion":{"gitVersion":"v1.29.3"}}` + "\n" +
		"EASYDO_K8S_VERSION_END\n" +
		"EASYDO_K8S_NODES_BEGIN\n" +
		`{"items":[{"metadata":{"name":"node-a"},"status":{"nodeInfo":{"architecture":"amd64"},"allocatable":{"cpu":"4000m","memory":"8Gi","pods":"100"}}}]}` + "\n" +
		"EASYDO_K8S_NODES_END\n" +
		"EASYDO_K8S_PODS_BEGIN\n" +
		`{"items":[{"metadata":{"uid":"pod-uid-1","name":"trainer-a","namespace":"ml"},"spec":{"nodeName":"node-a","containers":[{"name":"trainer","resources":{"requests":{"cpu":"250m","memory":"512Mi"}}}]},"status":{"phase":"Running"}}]}` + "\n" +
		"EASYDO_K8S_PODS_END\n"

	baseInfoJSON, _, _, err := buildResourceBaseInfoJSON(&models.AgentTask{Params: string(paramsJSON)}, map[string]interface{}{"stdout": stdout})
	if err != nil {
		t.Fatalf("buildResourceBaseInfoJSON returned error: %v", err)
	}
	var payload ResourceBaseInfoV3
	if err := json.Unmarshal([]byte(baseInfoJSON), &payload); err != nil {
		t.Fatalf("unmarshal canonical k8s base info failed: %v", err)
	}
	if payload.ResourceID != buildResourceBaseInfoResourcePrefix(43) {
		t.Fatalf("resourceId=%q, want %q", payload.ResourceID, buildResourceBaseInfoResourcePrefix(43))
	}
	if payload.Labels["tier"] != "infra" || payload.Labels["owner"] != "platform" {
		t.Fatalf("expected durable top-level labels merged, got %#v", payload.Labels)
	}
	if _, exists := payload.Labels["resourceId"]; exists {
		t.Fatalf("expected reserved resourceId label to be filtered, got %#v", payload.Labels)
	}
	if _, exists := payload.Labels["k8s"]; exists {
		t.Fatalf("expected legacy k8s label block to stay out of canonical labels, got %#v", payload.Labels)
	}
	if _, exists := payload.Labels["fromGeneric"]; exists {
		t.Fatalf("expected generic nodeConfig.labels to stay out of canonical labels, got %#v", payload.Labels)
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
	var runConfig models.PipelineRunConfigSnapshot
	if err := json.Unmarshal([]byte(run.RunConfig), &runConfig); err != nil {
		t.Fatalf("unmarshal run config failed: %v", err)
	}
	if runConfig.Inputs["deploy"]["resource_host"] != "10.0.0.8" {
		t.Fatalf("expected deployment runtime inputs to include resource_host, got %#v", runConfig.Inputs)
	}
	if runConfig.Inputs["deploy"]["app_name"] != "nginx-web" {
		t.Fatalf("expected deployment runtime inputs to include app_name, got %#v", runConfig.Inputs)
	}
	var pipelineSnapshot PipelineConfig
	if err := json.Unmarshal([]byte(run.PipelineSnapshot), &pipelineSnapshot); err != nil {
		t.Fatalf("unmarshal pipeline snapshot failed: %v", err)
	}
	if len(pipelineSnapshot.Nodes[0].DefinitionParams) == 0 {
		t.Fatalf("expected pipeline snapshot to preserve authored params, got %#v", pipelineSnapshot.Nodes[0])
	}
	authoredParams := map[string]interface{}{}
	for _, param := range pipelineSnapshot.Nodes[0].DefinitionParams {
		authoredParams[param.Key] = param.Value
	}
	if authoredParams["host"] != "${inputs.resource_host}" {
		t.Fatalf("expected pipeline snapshot to preserve authored host template, got %#v", authoredParams)
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
		TemplateType:       models.StoreTemplateTypeAI,
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
		TemplateType:       models.StoreTemplateTypeAI,
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
		TemplateType:       models.StoreTemplateTypeAI,
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
		{TemplateVersionID: version.ID, Name: "model_tag", Label: "Model Tag", Description: "要发布的模型标签版本", ExtraTip: "推荐使用与目标模型文件匹配的标签。", Type: "string", DefaultValue: "latest", Required: true, SortOrder: 1},
		{TemplateVersionID: version.ID, Name: "gpu_memory_utilization", Label: "GPU Memory Utilization", Description: "控制显存占用比例", ExtraTip: "默认 0.9，显存紧张时可适度下调。", Type: "number", DefaultValue: "0.9", Required: false, Advanced: true, SortOrder: 2},
	}
	if err := db.Create(&parameters).Error; err != nil {
		t.Fatalf("create template parameters failed: %v", err)
	}

	h := NewStoreTemplateHandler()
	resp := performResourceStoreRequest(t, h.ListTemplateVersions, maintainer.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, http.MethodGet, "/api/store/templates/1/versions", nil, pathResourceStoreID(template.ID))
	if resp.Code != http.StatusOK {
		t.Fatalf("expected list template versions success, got=%d body=%s", resp.Code, resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte("model_tag")) || !bytes.Contains(resp.Body.Bytes(), []byte("GPU Memory Utilization")) || !bytes.Contains(resp.Body.Bytes(), []byte("控制显存占用比例")) || !bytes.Contains(resp.Body.Bytes(), []byte("默认 0.9，显存紧张时可适度下调。")) || !bytes.Contains(resp.Body.Bytes(), []byte(`"advanced":true`)) {
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
				"description":   "Redis 容器名称",
				"extra_tip":     "建议与部署环境保持一一对应，便于排查。",
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
	if !bytes.Contains(listResp.Body.Bytes(), []byte(`"extra_tip":"建议与部署环境保持一一对应，便于排查。"`)) {
		t.Fatalf("expected extra_tip in list response, got=%s", listResp.Body.String())
	}

	var persistedParameter models.TemplateParameter
	if err := db.Where("template_version_id = ? AND name = ?", version.ID, "container_name").First(&persistedParameter).Error; err != nil {
		t.Fatalf("load persisted template parameter failed: %v", err)
	}
	if persistedParameter.ExtraTip != "建议与部署环境保持一一对应，便于排查。" {
		t.Fatalf("expected persisted extra_tip, got=%q", persistedParameter.ExtraTip)
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

func TestStoreTemplateHandler_CreateTemplateVersionWithUploadedChartInMultipartSave(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "template-upload-create-maintainer", models.WorkspaceRoleMaintainer)
	template := models.StoreTemplate{
		WorkspaceID:        workspace.ID,
		Name:               "MySQL",
		TemplateType:       models.StoreTemplateTypeApp,
		TargetResourceType: models.ResourceTypeK8sCluster,
		Source:             models.StoreTemplateSourceWorkspace,
		Status:             models.StoreTemplateStatusPublished,
		Category:           "database",
		CreatedBy:          maintainer.ID,
	}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}

	store := newMemoryTestObjectStore()
	h := NewStoreTemplateHandler()
	h.objectStoreFactory = func() (storage.ObjectStore, error) { return store, nil }

	payload := map[string]interface{}{
		"version":             "14.0.3",
		"status":              string(models.StoreTemplateStatusPublished),
		"infra_type":          "k8s",
		"version_description": "mysql upload variant",
		"parameters": []map[string]interface{}{
			{
				"name":          "release_name",
				"label":         "Release Name",
				"description":   "Helm 发布名称",
				"extra_tip":     "建议与环境和服务名称保持一致。",
				"type":          "text",
				"default_value": "mysql",
				"required":      true,
				"sort_order":    1,
			},
		},
		"chart_source": map[string]interface{}{
			"type":          "upload",
			"chart_name":    "mysql",
			"chart_version": "14.0.3",
			"file_name":     "mysql-14.0.3.tgz",
		},
		"base_values_yaml": "auth:\n  enabled: true\n",
	}
	resp := performMultipartResourceStoreRequest(
		t,
		h.CreateTemplateVersion,
		maintainer.ID,
		"user",
		workspace.ID,
		models.WorkspaceRoleMaintainer,
		http.MethodPost,
		"/api/store/templates/1/versions",
		payload,
		"chart_file",
		"mysql-14.0.3.tgz",
		[]byte("fake-chart-body"),
		pathResourceStoreID(template.ID),
	)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected multipart create template version success, got=%d body=%s", resp.Code, resp.Body.String())
	}

	body := resp.Body.Bytes()
	if !bytes.Contains(body, []byte(`"type":"upload"`)) {
		t.Fatalf("expected upload chart source in response, got=%s", string(body))
	}
	if !bytes.Contains(body, []byte(`"file_name":"mysql-14.0.3.tgz"`)) {
		t.Fatalf("expected uploaded file name in response, got=%s", string(body))
	}
	if !bytes.Contains(body, []byte(`"object_key":"store/charts/`)) {
		t.Fatalf("expected object key in response, got=%s", string(body))
	}
	if !bytes.Contains(body, []byte(`"extra_tip":"建议与环境和服务名称保持一致。"`)) {
		t.Fatalf("expected extra_tip in create response, got=%s", string(body))
	}
	if len(store.objects) != 1 {
		t.Fatalf("expected object store to contain uploaded chart, got=%d", len(store.objects))
	}
	for _, saved := range store.objects {
		if string(saved) != "fake-chart-body" {
			t.Fatalf("expected uploaded chart body persisted, got=%q", string(saved))
		}
	}
	versionID := responseDataID(t, body)
	var version models.StoreTemplateVersion
	if err := db.First(&version, versionID).Error; err != nil {
		t.Fatalf("load created version failed: %v", err)
	}
	if !bytes.Contains([]byte(version.DefaultConfig), []byte(`"object_key":"store/charts/`)) {
		t.Fatalf("expected persisted object key in default config, got=%s", version.DefaultConfig)
	}
	var parameter models.TemplateParameter
	if err := db.Where("template_version_id = ? AND name = ?", version.ID, "release_name").First(&parameter).Error; err != nil {
		t.Fatalf("load created template parameter failed: %v", err)
	}
	if parameter.ExtraTip != "建议与环境和服务名称保持一致。" {
		t.Fatalf("expected persisted extra_tip on create, got=%q", parameter.ExtraTip)
	}
}

func TestStoreTemplateHandler_UpdateTemplateVersionWithUploadedChartInMultipartSave(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "template-upload-update-maintainer", models.WorkspaceRoleMaintainer)
	template := models.StoreTemplate{
		WorkspaceID:        workspace.ID,
		Name:               "MySQL",
		TemplateType:       models.StoreTemplateTypeApp,
		TargetResourceType: models.ResourceTypeK8sCluster,
		Source:             models.StoreTemplateSourceWorkspace,
		Status:             models.StoreTemplateStatusPublished,
		Category:           "database",
		CreatedBy:          maintainer.ID,
	}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}
	version := models.StoreTemplateVersion{
		WorkspaceID:    workspace.ID,
		TemplateID:     template.ID,
		Version:        "14.0.2",
		DeploymentMode: "k8s_chart",
		DefaultConfig:  `{"schema_version":1,"infra_type":"k8s","version_description":"old","k8s":{"chart_source":{"type":"upload","chart_name":"mysql","chart_version":"14.0.2","file_name":"mysql-14.0.2.tgz"},"base_values_yaml":"auth:\n  enabled: false\n"}}`,
		Status:         models.StoreTemplateStatusDraft,
		CreatedBy:      maintainer.ID,
	}
	if err := db.Create(&version).Error; err != nil {
		t.Fatalf("create version failed: %v", err)
	}

	store := newMemoryTestObjectStore()
	h := NewStoreTemplateHandler()
	h.objectStoreFactory = func() (storage.ObjectStore, error) { return store, nil }

	payload := map[string]interface{}{
		"version":             "14.0.3",
		"status":              string(models.StoreTemplateStatusPublished),
		"infra_type":          "k8s",
		"version_description": "mysql upload variant updated",
		"chart_source": map[string]interface{}{
			"type":          "upload",
			"chart_name":    "mysql",
			"chart_version": "14.0.3",
			"file_name":     "mysql-14.0.3.tgz",
		},
		"base_values_yaml": "auth:\n  enabled: true\n",
	}
	resp := performMultipartResourceStoreRequest(
		t,
		h.UpdateTemplateVersion,
		maintainer.ID,
		"user",
		workspace.ID,
		models.WorkspaceRoleMaintainer,
		http.MethodPut,
		"/api/store/templates/1/versions/1",
		payload,
		"chart_file",
		"mysql-14.0.3.tgz",
		[]byte("updated-chart-body"),
		pathTemplateVersionIDs(template.ID, version.ID),
	)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected multipart update template version success, got=%d body=%s", resp.Code, resp.Body.String())
	}
	if len(store.objects) != 1 {
		t.Fatalf("expected object store upload during update, got=%d", len(store.objects))
	}
	var updated models.StoreTemplateVersion
	if err := db.First(&updated, version.ID).Error; err != nil {
		t.Fatalf("load updated version failed: %v", err)
	}
	if updated.Version != "14.0.3" {
		t.Fatalf("expected updated version number, got=%s", updated.Version)
	}
	if !bytes.Contains([]byte(updated.DefaultConfig), []byte(`"file_name":"mysql-14.0.3.tgz"`)) {
		t.Fatalf("expected updated uploaded chart metadata, got=%s", updated.DefaultConfig)
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
	if vmReq.AIModelID != 0 {
		t.Fatalf("expected vm app deployment request ai_model_id to remain unset, got=%d", vmReq.AIModelID)
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
	if k8sReq.AIModelID != 0 {
		t.Fatalf("expected k8s app deployment request ai_model_id to remain unset, got=%d", k8sReq.AIModelID)
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

	h := NewAIModelCatalogHandler()
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
	importResp := performResourceStoreRequest(t, h.ImportModel, admin.ID, "admin", workspace.ID, models.WorkspaceRoleOwner, http.MethodPost, "/api/store/ai-models/import", importBody)
	if importResp.Code != http.StatusOK {
		t.Fatalf("expected import model success, got=%d body=%s", importResp.Code, importResp.Body.String())
	}

	listResp := performResourceStoreRequest(t, h.ListModels, admin.ID, "admin", workspace.ID, models.WorkspaceRoleOwner, http.MethodGet, "/api/store/ai-models", nil)
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
	model := models.AIModelCatalog{
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

	h := NewAIModelCatalogHandler()
	listResp := performResourceStoreRequest(t, h.ListModels, admin.ID, "admin", workspace.ID, models.WorkspaceRoleOwner, http.MethodGet, "/api/store/ai-models", nil)
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
	original := models.AIModelCatalog{
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

	h := NewAIModelCatalogHandler()
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
	reimportResp := performResourceStoreRequest(t, h.ImportModel, admin.ID, "admin", workspace.ID, models.WorkspaceRoleOwner, http.MethodPost, "/api/store/ai-models/import", reimportBody)
	if reimportResp.Code != http.StatusOK {
		t.Fatalf("expected reimport model success, got=%d body=%s", reimportResp.Code, reimportResp.Body.String())
	}

	var stored models.AIModelCatalog
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

	h := NewAIModelCatalogHandler()
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
	importResp := performResourceStoreRequest(t, h.ImportModel, admin.ID, "admin", workspace.ID, models.WorkspaceRoleOwner, http.MethodPost, "/api/store/ai-models/import", importBody)
	if importResp.Code != http.StatusOK {
		t.Fatalf("expected ModelScope import success, got=%d body=%s", importResp.Code, importResp.Body.String())
	}
	if !bytes.Contains(importResp.Body.Bytes(), []byte(`"parameter_size":"27.0B"`)) {
		t.Fatalf("expected ModelScope import response to include nested parameter_size, got=%s", importResp.Body.String())
	}

	listResp := performResourceStoreRequest(t, h.ListModels, admin.ID, "admin", workspace.ID, models.WorkspaceRoleOwner, http.MethodGet, "/api/store/ai-models", nil)
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

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "Ollama", TemplateType: models.StoreTemplateTypeAI, TargetResourceType: models.ResourceTypeVM, Source: models.StoreTemplateSourceWorkspace, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}
	version := models.StoreTemplateVersion{WorkspaceID: workspace.ID, TemplateID: template.ID, PipelineID: pipeline.ID, Version: "1.0.0", DeploymentMode: "pipeline", Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&version).Error; err != nil {
		t.Fatalf("create version failed: %v", err)
	}
	model := models.AIModelCatalog{
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
		"ai_model_id":         model.ID,
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
	if req.AIModelID != model.ID {
		t.Fatalf("expected deployment request ai_model_id=%d, got=%d", model.ID, req.AIModelID)
	}
	if !bytes.Contains([]byte(req.AIModelSnapshot), []byte("Qwen/Qwen2.5-7B-Instruct")) {
		t.Fatalf("expected ai model snapshot to include source model id, got=%s", req.AIModelSnapshot)
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

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "vLLM", TemplateType: models.StoreTemplateTypeAI, TargetResourceType: models.ResourceTypeVM, Source: models.StoreTemplateSourceWorkspace, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
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
	model := models.AIModelCatalog{Name: "Qwen2.5-7B-Instruct", Source: "huggingface", SourceModelID: "Qwen/Qwen2.5-7B-Instruct", ImportedBy: maintainer.ID}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{
		"template_version_id": version.ID,
		"target_resource_id":  resource.ID,
		"ai_model_id":         model.ID,
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

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "vLLM empty", TemplateType: models.StoreTemplateTypeAI, TargetResourceType: models.ResourceTypeVM, Source: models.StoreTemplateSourceWorkspace, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
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
	model := models.AIModelCatalog{Name: "Qwen2.5-7B-Instruct", Source: "huggingface", SourceModelID: "Qwen/Qwen2.5-7B-Instruct", ImportedBy: maintainer.ID}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{
		"template_version_id": version.ID,
		"target_resource_id":  resource.ID,
		"ai_model_id":         model.ID,
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

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "vLLM", TemplateType: models.StoreTemplateTypeAI, TargetResourceType: models.ResourceTypeVM, Source: models.StoreTemplateSourceWorkspace, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
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
	model := models.AIModelCatalog{Name: "Qwen2.5-7B-Instruct", Source: "huggingface", SourceModelID: "Qwen/Qwen2.5-7B-Instruct", ImportedBy: maintainer.ID}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{
		"template_version_id": version.ID,
		"target_resource_id":  resource.ID,
		"ai_model_id":         model.ID,
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

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "vLLM", TemplateType: models.StoreTemplateTypeAI, TargetResourceType: models.ResourceTypeVM, Source: models.StoreTemplateSourceWorkspace, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
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
	model := models.AIModelCatalog{Name: "Qwen2.5-7B-Instruct", Source: "huggingface", SourceModelID: "Qwen/Qwen2.5-7B-Instruct", ImportedBy: maintainer.ID}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{
		"template_version_id": version.ID,
		"target_resource_id":  resource.ID,
		"ai_model_id":         model.ID,
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

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "vLLM", TemplateType: models.StoreTemplateTypeAI, TargetResourceType: models.ResourceTypeVM, Source: models.StoreTemplateSourcePlatform, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
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
	model := models.AIModelCatalog{Name: "Qwen2.5-7B-Instruct", Source: "huggingface", SourceModelID: "Qwen/Qwen2.5-7B-Instruct", ImportedBy: maintainer.ID}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{
		"template_version_id": version.ID,
		"target_resource_id":  resource.ID,
		"ai_model_id":         model.ID,
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

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "vLLM", TemplateType: models.StoreTemplateTypeAI, TargetResourceType: models.ResourceTypeVM, Source: models.StoreTemplateSourcePlatform, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
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
	model := models.AIModelCatalog{Name: "Qwen2.5-0.5B-Instruct", Source: "huggingface", SourceModelID: "Qwen/Qwen2.5-0.5B-Instruct", ImportedBy: maintainer.ID}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{
		"template_version_id": version.ID,
		"target_resource_id":  resource.ID,
		"ai_model_id":         model.ID,
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

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "vLLM", TemplateType: models.StoreTemplateTypeAI, TargetResourceType: models.ResourceTypeVM, Source: models.StoreTemplateSourcePlatform, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&template).Error; err != nil {
		t.Fatalf("create template failed: %v", err)
	}
	version := models.StoreTemplateVersion{WorkspaceID: workspace.ID, TemplateID: template.ID, PipelineID: pipeline.ID, Version: "7.0.0", DeploymentMode: "pipeline", Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
	if err := db.Create(&version).Error; err != nil {
		t.Fatalf("create version failed: %v", err)
	}
	model := models.AIModelCatalog{Name: "Qwen3.5-2B", Source: "huggingface", SourceModelID: "Qwen/Qwen3.5-2B", ImportedBy: maintainer.ID}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{
		"template_version_id": version.ID,
		"target_resource_id":  resource.ID,
		"ai_model_id":         model.ID,
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

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "Ollama", TemplateType: models.StoreTemplateTypeAI, TargetResourceType: models.ResourceTypeVM, Source: models.StoreTemplateSourcePlatform, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
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
	model := models.AIModelCatalog{Name: "Qwen3.5-2B", Source: "huggingface", SourceModelID: "Qwen/Qwen3.5-2B", ImportedBy: maintainer.ID}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{"template_version_id": version.ID, "target_resource_id": resource.ID, "ai_model_id": model.ID, "parameters": map[string]interface{}{"gpu_indices": "2,3"}})
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

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "vLLM", TemplateType: models.StoreTemplateTypeAI, TargetResourceType: models.ResourceTypeK8sCluster, Source: models.StoreTemplateSourcePlatform, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
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
	model := models.AIModelCatalog{Name: "Qwen3.5-2B", Source: "huggingface", SourceModelID: "Qwen/Qwen3.5-2B", ImportedBy: maintainer.ID}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{"template_version_id": version.ID, "target_resource_id": resource.ID, "ai_model_id": model.ID, "parameters": map[string]interface{}{"gpu_indices": "2,3"}})
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

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "Ollama", TemplateType: models.StoreTemplateTypeAI, TargetResourceType: models.ResourceTypeK8sCluster, Source: models.StoreTemplateSourcePlatform, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
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
	model := models.AIModelCatalog{Name: "Qwen3.5-2B", Source: "huggingface", SourceModelID: "Qwen/Qwen3.5-2B", ImportedBy: maintainer.ID}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{"template_version_id": version.ID, "target_resource_id": resource.ID, "ai_model_id": model.ID, "parameters": map[string]interface{}{"gpu_indices": "2,3"}})
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

	template := models.StoreTemplate{WorkspaceID: workspace.ID, Name: "SGLang", TemplateType: models.StoreTemplateTypeAI, TargetResourceType: models.ResourceTypeK8sCluster, Source: models.StoreTemplateSourcePlatform, Status: models.StoreTemplateStatusPublished, CreatedBy: maintainer.ID}
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
	model := models.AIModelCatalog{Name: "Qwen3.5-2B", Source: "huggingface", SourceModelID: "Qwen/Qwen3.5-2B", ImportedBy: maintainer.ID}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create llm model failed: %v", err)
	}

	h := NewDeploymentHandler()
	requestBody := mustJSON(t, map[string]interface{}{"template_version_id": version.ID, "target_resource_id": resource.ID, "ai_model_id": model.ID, "parameters": map[string]interface{}{"gpu_indices": "2,3"}})
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

func performMultipartResourceStoreRequest(t *testing.T, handler gin.HandlerFunc, userID uint64, role string, workspaceID uint64, workspaceRole string, method, url string, payload map[string]interface{}, fileField, fileName string, fileBody []byte, pathParams ...func(*gin.Context)) *httptest.ResponseRecorder {
	t.Helper()
	buffer := &bytes.Buffer{}
	writer := multipart.NewWriter(buffer)
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal multipart payload failed: %v", err)
	}
	if err := writer.WriteField("payload", string(payloadJSON)); err != nil {
		t.Fatalf("write multipart payload failed: %v", err)
	}
	if fileField != "" {
		part, err := writer.CreateFormFile(fileField, fileName)
		if err != nil {
			t.Fatalf("create multipart file failed: %v", err)
		}
		if _, err := part.Write(fileBody); err != nil {
			t.Fatalf("write multipart file body failed: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, url, bytes.NewReader(buffer.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
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
