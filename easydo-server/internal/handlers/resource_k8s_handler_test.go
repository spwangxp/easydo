package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestResourceHandler_GetK8sOverviewReturnsBaseInfoForViewer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "k8s-overview-maintainer", models.WorkspaceRoleMaintainer)
	viewer := seedResourceStoreMember(t, db, workspace.ID, "k8s-overview-viewer", models.WorkspaceRoleViewer)
	resource := models.Resource{
		WorkspaceID:         workspace.ID,
		Name:                "overview-cluster",
		Type:                models.ResourceTypeK8sCluster,
		Environment:         "production",
		Status:              models.ResourceStatusOnline,
		Endpoint:            "https://10.0.0.9:6443",
		CreatedBy:           maintainer.ID,
		BaseInfoStatus:      "success",
		BaseInfoCollectedAt: 1710000000,
		BaseInfo:            `{"schemaVersion":1,"status":"success","k8s":{"cluster":{"serverVersion":"v1.31.0"},"summary":{"nodeCount":3}}}`,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	h := NewResourceHandler()
	resp := performResourceStoreRequest(t, h.GetK8sOverview, viewer.ID, "user", workspace.ID, models.WorkspaceRoleViewer, http.MethodGet, fmt.Sprintf("/api/resources/%d/k8s/overview", resource.ID), nil, pathResourceStoreID(resource.ID))
	if resp.Code != http.StatusOK {
		t.Fatalf("expected k8s overview success, got=%d body=%s", resp.Code, resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"serverVersion":"v1.31.0"`)) {
		t.Fatalf("expected overview payload to contain server version, got=%s", resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"nodeCount":3`)) {
		t.Fatalf("expected overview payload to contain node count, got=%s", resp.Body.String())
	}
}

func TestResourceHandler_QueryK8sNamespacesCreatesKubernetesTask(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "k8s-query-maintainer", models.WorkspaceRoleMaintainer)
	viewer := seedResourceStoreMember(t, db, workspace.ID, "k8s-query-viewer", models.WorkspaceRoleViewer)
	agent := seedApprovedResourceAgent(t, db, workspace.ID)
	credential := seedResourceVerificationCredential(t, db, workspace.ID, maintainer.ID, models.TypeToken, map[string]interface{}{
		"server":    "https://cluster.example.internal",
		"token":     "k8s-token",
		"namespace": "default",
	})
	resource := seedK8sResourceWithBinding(t, db, workspace.ID, maintainer.ID, credential.ID)

	h := NewResourceHandler()
	body := mustJSON(t, map[string]interface{}{})
	resp := performResourceStoreRequest(t, h.QueryK8sNamespaces, viewer.ID, "user", workspace.ID, models.WorkspaceRoleViewer, http.MethodPost, fmt.Sprintf("/api/resources/%d/k8s/namespaces/query", resource.ID), body, pathResourceStoreID(resource.ID))
	if resp.Code != http.StatusOK {
		t.Fatalf("expected k8s namespace query success, got=%d body=%s", resp.Code, resp.Body.String())
	}
	data := decodeResponseData[map[string]interface{}](t, resp.Body.Bytes())
	taskID, ok := parseCredentialID(data["task_id"])
	if !ok || taskID == 0 {
		t.Fatalf("expected namespace query task_id, got=%v", data["task_id"])
	}

	var task models.AgentTask
	if err := db.First(&task, taskID).Error; err != nil {
		t.Fatalf("load namespace task failed: %v", err)
	}
	if task.AgentID != agent.ID {
		t.Fatalf("expected namespace query to use selected agent %d, got %d", agent.ID, task.AgentID)
	}
	if task.TaskType != "kubernetes" {
		t.Fatalf("expected namespace query task type kubernetes, got=%s", task.TaskType)
	}
	if !bytes.Contains([]byte(task.Params), []byte("resource_k8s_namespace_query")) {
		t.Fatalf("expected namespace query params to include kind, got=%s", task.Params)
	}
	if !bytes.Contains([]byte(task.Script), []byte("kubectl get namespaces -o json")) {
		t.Fatalf("expected namespace query script to call kubectl get namespaces, got=%s", task.Script)
	}
}

func TestResourceHandler_QueryK8sResourcesRejectsUnsupportedKind(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "k8s-kind-maintainer", models.WorkspaceRoleMaintainer)
	viewer := seedResourceStoreMember(t, db, workspace.ID, "k8s-kind-viewer", models.WorkspaceRoleViewer)
	credential := seedResourceVerificationCredential(t, db, workspace.ID, maintainer.ID, models.TypeToken, map[string]interface{}{
		"server": "https://cluster.example.internal",
		"token":  "k8s-token",
	})
	resource := seedK8sResourceWithBinding(t, db, workspace.ID, maintainer.ID, credential.ID)

	h := NewResourceHandler()
	body := mustJSON(t, map[string]interface{}{
		"namespace": "default",
		"kinds":     []string{"Node"},
	})
	resp := performResourceStoreRequest(t, h.QueryK8sResources, viewer.ID, "user", workspace.ID, models.WorkspaceRoleViewer, http.MethodPost, fmt.Sprintf("/api/resources/%d/k8s/resources/query", resource.ID), body, pathResourceStoreID(resource.ID))
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected unsupported kind to fail, got=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestResourceHandler_CreateK8sActionCreatesAuditAndTask(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "k8s-action-maintainer", models.WorkspaceRoleMaintainer)
	seedApprovedResourceAgent(t, db, workspace.ID)
	credential := seedResourceVerificationCredential(t, db, workspace.ID, maintainer.ID, models.TypeToken, map[string]interface{}{
		"server": "https://cluster.example.internal",
		"token":  "k8s-token",
	})
	resource := seedK8sResourceWithBinding(t, db, workspace.ID, maintainer.ID, credential.ID)

	h := NewResourceHandler()
	body := mustJSON(t, map[string]interface{}{
		"namespace":   "prod",
		"target_kind": "Deployment",
		"target_name": "api-server",
		"action":      "rollout_restart",
		"reason":      "repair workload",
	})
	resp := performResourceStoreRequest(t, h.CreateK8sAction, maintainer.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, http.MethodPost, fmt.Sprintf("/api/resources/%d/k8s/actions", resource.ID), body, pathResourceStoreID(resource.ID))
	if resp.Code != http.StatusOK {
		t.Fatalf("expected k8s action success, got=%d body=%s", resp.Code, resp.Body.String())
	}
	data := decodeResponseData[map[string]interface{}](t, resp.Body.Bytes())
	taskID, ok := parseCredentialID(data["task_id"])
	if !ok || taskID == 0 {
		t.Fatalf("expected action response task_id, got=%v", data["task_id"])
	}

	var task models.AgentTask
	if err := db.First(&task, taskID).Error; err != nil {
		t.Fatalf("load action task failed: %v", err)
	}
	if task.TaskType != "kubernetes" {
		t.Fatalf("expected k8s action task type kubernetes, got=%s", task.TaskType)
	}
	if !bytes.Contains([]byte(task.Params), []byte("resource_k8s_action")) {
		t.Fatalf("expected action params to include kind, got=%s", task.Params)
	}
	if !bytes.Contains([]byte(task.Script), []byte("kubectl rollout restart deployment api-server -n prod")) {
		t.Fatalf("expected rollout restart command in script, got=%s", task.Script)
	}

	var audit models.ResourceOperationAudit
	if err := db.Where("resource_id = ?", resource.ID).First(&audit).Error; err != nil {
		t.Fatalf("load action audit failed: %v", err)
	}
	if audit.Status != models.ResourceOperationStatusQueued {
		t.Fatalf("expected queued action audit, got=%s", audit.Status)
	}
	if audit.TaskID == nil || *audit.TaskID != task.ID {
		t.Fatalf("expected audit task binding, got=%+v", audit)
	}
	if audit.Action != "rollout_restart" || audit.Namespace != "prod" || audit.TargetKind != "Deployment" || audit.TargetName != "api-server" {
		t.Fatalf("unexpected audit payload: %+v", audit)
	}
}

func TestResourceHandler_CreateK8sActionRejectsDeveloper(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "k8s-action-guard-maintainer", models.WorkspaceRoleMaintainer)
	developer := seedResourceStoreMember(t, db, workspace.ID, "k8s-action-guard-developer", models.WorkspaceRoleDeveloper)
	credential := seedResourceVerificationCredential(t, db, workspace.ID, maintainer.ID, models.TypeToken, map[string]interface{}{
		"server": "https://cluster.example.internal",
		"token":  "k8s-token",
	})
	resource := seedK8sResourceWithBinding(t, db, workspace.ID, maintainer.ID, credential.ID)

	h := NewResourceHandler()
	body := mustJSON(t, map[string]interface{}{
		"namespace":   "prod",
		"target_kind": "Deployment",
		"target_name": "api-server",
		"action":      "rollout_restart",
		"reason":      "repair workload",
	})
	resp := performResourceStoreRequest(t, h.CreateK8sAction, developer.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, http.MethodPost, fmt.Sprintf("/api/resources/%d/k8s/actions", resource.ID), body, pathResourceStoreID(resource.ID))
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected developer action forbidden, got=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestResourceHandler_ListResourceOperationAuditsReturnsResourceScopedEntries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "k8s-audit-maintainer", models.WorkspaceRoleMaintainer)
	viewer := seedResourceStoreMember(t, db, workspace.ID, "k8s-audit-viewer", models.WorkspaceRoleViewer)
	resource := models.Resource{WorkspaceID: workspace.ID, Name: "audit-cluster", Type: models.ResourceTypeK8sCluster, Environment: "production", Status: models.ResourceStatusOnline, Endpoint: "https://10.0.0.9:6443", CreatedBy: maintainer.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}
	taskID := uint64(99)
	audit := models.ResourceOperationAudit{
		WorkspaceID:  workspace.ID,
		ResourceID:   resource.ID,
		ResourceType: string(models.ResourceTypeK8sCluster),
		Domain:       "k8s",
		Namespace:    "prod",
		TargetKind:   "Deployment",
		TargetName:   "api-server",
		Action:       "rollout_restart",
		Reason:       "repair workload",
		Status:       models.ResourceOperationStatusSuccess,
		TaskID:       &taskID,
		CreatedBy:    maintainer.ID,
		CompletedAt:  time.Now().Unix(),
	}
	if err := db.Create(&audit).Error; err != nil {
		t.Fatalf("create audit failed: %v", err)
	}

	h := NewResourceHandler()
	resp := performResourceStoreRequest(t, h.ListResourceOperationAudits, viewer.ID, "user", workspace.ID, models.WorkspaceRoleViewer, http.MethodGet, fmt.Sprintf("/api/resources/%d/actions", resource.ID), nil, pathResourceStoreID(resource.ID))
	if resp.Code != http.StatusOK {
		t.Fatalf("expected audit list success, got=%d body=%s", resp.Code, resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"rollout_restart"`)) || !bytes.Contains(resp.Body.Bytes(), []byte(`"api-server"`)) {
		t.Fatalf("expected audit entry in list response, got=%s", resp.Body.String())
	}
}

func TestSyncResourceOperationAuditRecordsReflectsTerminalTaskStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	maintainer, workspace := seedResourceStoreUserAndWorkspace(t, db, "k8s-audit-sync-maintainer", models.WorkspaceRoleMaintainer)
	resource := models.Resource{WorkspaceID: workspace.ID, Name: "sync-cluster", Type: models.ResourceTypeK8sCluster, Environment: "production", Status: models.ResourceStatusOnline, Endpoint: "https://10.0.0.9:6443", CreatedBy: maintainer.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}
	task := models.AgentTask{WorkspaceID: workspace.ID, AgentID: seedApprovedResourceAgent(t, db, workspace.ID).ID, NodeID: "k8s-action-sync", TaskType: "kubernetes", Name: "执行 K8s 操作", Status: models.TaskStatusQueued, CreatedBy: maintainer.ID}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	audit := models.ResourceOperationAudit{WorkspaceID: workspace.ID, ResourceID: resource.ID, ResourceType: string(models.ResourceTypeK8sCluster), Domain: "k8s", Namespace: "prod", TargetKind: "Deployment", TargetName: "api-server", Action: "rollout_restart", Reason: "repair workload", Status: models.ResourceOperationStatusQueued, TaskID: &task.ID, CreatedBy: maintainer.ID}
	if err := db.Create(&audit).Error; err != nil {
		t.Fatalf("create audit failed: %v", err)
	}

	resultJSON, err := json.Marshal(map[string]interface{}{"stdout": "rolled out", "stderr": ""})
	if err != nil {
		t.Fatalf("marshal task result failed: %v", err)
	}
	task.Status = models.TaskStatusExecuteSuccess
	task.ResultData = string(resultJSON)
	task.EndTime = time.Now().Unix()

	if err := syncResourceOperationAuditRecords(db, &task); err != nil {
		t.Fatalf("sync resource operation audit failed: %v", err)
	}

	var stored models.ResourceOperationAudit
	if err := db.First(&stored, audit.ID).Error; err != nil {
		t.Fatalf("reload audit failed: %v", err)
	}
	if stored.Status != models.ResourceOperationStatusSuccess {
		t.Fatalf("expected synced audit success, got=%s", stored.Status)
	}
	if stored.CompletedAt == 0 {
		t.Fatalf("expected completed_at to be set")
	}
}

func seedK8sResourceWithBinding(t *testing.T, db *gorm.DB, workspaceID, createdBy, credentialID uint64) models.Resource {
	t.Helper()
	resource := models.Resource{
		WorkspaceID: workspaceID,
		Name:        "k8s-cluster-01",
		Type:        models.ResourceTypeK8sCluster,
		Environment: "production",
		Status:      models.ResourceStatusOnline,
		Endpoint:    "https://cluster.example.internal",
		CreatedBy:   createdBy,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create k8s resource failed: %v", err)
	}
	if err := db.Create(&models.ResourceCredentialBinding{
		WorkspaceID:  workspaceID,
		ResourceID:   resource.ID,
		CredentialID: credentialID,
		Purpose:      "cluster_auth",
		BoundBy:      createdBy,
	}).Error; err != nil {
		t.Fatalf("create k8s binding failed: %v", err)
	}
	return resource
}
