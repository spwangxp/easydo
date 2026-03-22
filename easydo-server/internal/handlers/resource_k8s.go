package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"easydo-server/internal/models"
	"easydo-server/internal/services"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var k8sResourceNamePattern = regexp.MustCompile(`^[a-z0-9]([-.a-z0-9]*[a-z0-9])?$`)

var supportedK8sKinds = map[string]string{
	"ConfigMap":   "configmaps",
	"CronJob":     "cronjobs",
	"DaemonSet":   "daemonsets",
	"Deployment":  "deployments",
	"Ingress":     "ingresses",
	"Job":         "jobs",
	"Namespace":   "namespaces",
	"Pod":         "pods",
	"Secret":      "secrets",
	"Service":     "services",
	"StatefulSet": "statefulsets",
}

type resourceK8sTaskPayload struct {
	K8s        resourceK8sTaskSnapshot `json:"k8s"`
	TaskType   string                  `json:"task_type"`
	NodeConfig map[string]interface{}  `json:"node_config,omitempty"`
}

type resourceK8sTaskSnapshot struct {
	Kind         string              `json:"kind"`
	ResourceID   uint64              `json:"resource_id"`
	ResourceType models.ResourceType `json:"resource_type"`
	Namespace    string              `json:"namespace,omitempty"`
	Kinds        []string            `json:"kinds,omitempty"`
	Keyword      string              `json:"keyword,omitempty"`
	TargetKind   string              `json:"target_kind,omitempty"`
	TargetName   string              `json:"target_name,omitempty"`
	Action       string              `json:"action,omitempty"`
	Reason       string              `json:"reason,omitempty"`
	Replicas     *int                `json:"replicas,omitempty"`
	RequestedBy  uint64              `json:"requested_by"`
	RequestedAt  int64               `json:"requested_at"`
}

type queryK8sNamespacesRequest struct {
	Keyword string `json:"keyword"`
}

type queryK8sResourcesRequest struct {
	Namespace string   `json:"namespace"`
	Kinds     []string `json:"kinds"`
	Keyword   string   `json:"keyword"`
}

type createK8sActionRequest struct {
	Namespace  string `json:"namespace"`
	TargetKind string `json:"target_kind"`
	TargetName string `json:"target_name"`
	Action     string `json:"action"`
	Reason     string `json:"reason"`
	Replicas   *int   `json:"replicas"`
}

func (h *ResourceHandler) GetK8sOverview(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanAccessWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权访问该资源"})
		return
	}

	resource, err := h.loadK8sResource(workspaceID, c.Param("id"))
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "不存在") {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"code": status, "message": err.Error()})
		return
	}

	data := gin.H{
		"resource_id":            resource.ID,
		"name":                   resource.Name,
		"endpoint":               resource.Endpoint,
		"environment":            resource.Environment,
		"status":                 resource.Status,
		"base_info":              decodeJSONObjectField(resource.BaseInfo, map[string]interface{}{}),
		"base_info_status":       resource.BaseInfoStatus,
		"base_info_source":       resource.BaseInfoSource,
		"base_info_last_error":   resource.BaseInfoLastError,
		"base_info_collected_at": resource.BaseInfoCollectedAt,
		"last_check_at":          resource.LastCheckAt,
		"last_check_result":      resource.LastCheckResult,
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": data})
}

func (h *ResourceHandler) QueryK8sNamespaces(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanAccessWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权浏览 K8s 资源"})
		return
	}
	var req queryK8sNamespacesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return
	}
	resource, credential, decrypted, effectiveEndpoint, err := h.resolveK8sTaskContext(workspaceID, userID, role, c.Param("id"))
	if err != nil {
		h.writeK8sResourceError(c, err)
		return
	}
	payload := resourceK8sTaskPayload{K8s: resourceK8sTaskSnapshot{Kind: "resource_k8s_namespace_query", ResourceID: resource.ID, ResourceType: resource.Type, Keyword: strings.TrimSpace(req.Keyword), RequestedBy: userID, RequestedAt: time.Now().Unix()}}
	task, err := h.createK8sTask(workspaceID, userID, credential, decrypted, effectiveEndpoint, buildK8sNamespaceQueryCommand(), "浏览 K8s 命名空间", 70, payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": gin.H{"task_id": task.ID, "status": task.Status}})
}

func (h *ResourceHandler) QueryK8sResources(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanAccessWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权浏览 K8s 资源"})
		return
	}
	var req queryK8sResourcesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return
	}
	namespace := strings.TrimSpace(req.Namespace)
	if !isSafeK8sName(namespace) {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "命名空间无效"})
		return
	}
	kinds, err := normalizeRequestedK8sKinds(req.Kinds)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	resource, credential, decrypted, effectiveEndpoint, err := h.resolveK8sTaskContext(workspaceID, userID, role, c.Param("id"))
	if err != nil {
		h.writeK8sResourceError(c, err)
		return
	}
	command, err := buildK8sResourceQueryCommand(namespace, kinds)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	payload := resourceK8sTaskPayload{K8s: resourceK8sTaskSnapshot{Kind: "resource_k8s_resource_query", ResourceID: resource.ID, ResourceType: resource.Type, Namespace: namespace, Kinds: kinds, Keyword: strings.TrimSpace(req.Keyword), RequestedBy: userID, RequestedAt: time.Now().Unix()}}
	task, err := h.createK8sTask(workspaceID, userID, credential, decrypted, effectiveEndpoint, command, "浏览 K8s 命名空间资源", 70, payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": gin.H{"task_id": task.ID, "status": task.Status}})
}

func (h *ResourceHandler) CreateK8sAction(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanManageWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权执行 K8s 操作"})
		return
	}
	var req createK8sActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return
	}
	namespace := strings.TrimSpace(req.Namespace)
	targetKind := strings.TrimSpace(req.TargetKind)
	targetName := strings.TrimSpace(req.TargetName)
	action := strings.TrimSpace(req.Action)
	reason := strings.TrimSpace(req.Reason)
	if !isSafeK8sName(namespace) || !isSafeK8sName(targetName) {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "命名空间或资源名称无效"})
		return
	}
	if reason == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "执行原因不能为空"})
		return
	}
	command, err := buildK8sActionCommand(targetKind, targetName, namespace, action, req.Replicas)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	resource, credential, decrypted, effectiveEndpoint, err := h.resolveK8sTaskContext(workspaceID, userID, role, c.Param("id"))
	if err != nil {
		h.writeK8sResourceError(c, err)
		return
	}
	payload := resourceK8sTaskPayload{K8s: resourceK8sTaskSnapshot{Kind: "resource_k8s_action", ResourceID: resource.ID, ResourceType: resource.Type, Namespace: namespace, TargetKind: targetKind, TargetName: targetName, Action: action, Reason: reason, Replicas: req.Replicas, RequestedBy: userID, RequestedAt: time.Now().Unix()}}
	task, err := h.createK8sTask(workspaceID, userID, credential, decrypted, effectiveEndpoint, command, "执行 K8s 资源操作", 80, payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	rawRequest, _ := json.Marshal(payload.K8s)
	taskID := task.ID
	audit := models.ResourceOperationAudit{
		WorkspaceID:     workspaceID,
		ResourceID:      resource.ID,
		ResourceType:    string(resource.Type),
		Domain:          "k8s",
		Namespace:       namespace,
		TargetKind:      targetKind,
		TargetName:      targetName,
		Action:          action,
		Reason:          reason,
		RequestSnapshot: string(rawRequest),
		TaskID:          &taskID,
		Status:          models.ResourceOperationStatusQueued,
		CreatedBy:       userID,
	}
	if err := h.DB.Create(&audit).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "写入资源操作审计失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": gin.H{"task_id": task.ID, "audit_id": audit.ID, "status": audit.Status}})
}

func (h *ResourceHandler) ListResourceOperationAudits(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanAccessWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权访问资源操作审计"})
		return
	}
	resource, err := h.loadResourceForWorkspace(workspaceID, c.Param("id"))
	if err != nil {
		h.writeK8sResourceError(c, err)
		return
	}
	var audits []models.ResourceOperationAudit
	query := h.DB.Where("workspace_id = ? AND resource_id = ?", workspaceID, resource.ID).Order("created_at DESC, id DESC")
	if domain := strings.TrimSpace(c.Query("domain")); domain != "" {
		query = query.Where("domain = ?", domain)
	}
	if namespace := strings.TrimSpace(c.Query("namespace")); namespace != "" {
		query = query.Where("namespace = ?", namespace)
	}
	if action := strings.TrimSpace(c.Query("action")); action != "" {
		query = query.Where("action = ?", action)
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		query = query.Where("status = ?", status)
	}
	if err := query.Find(&audits).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "加载资源操作审计失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": audits})
}

func (h *ResourceHandler) resolveK8sTaskContext(workspaceID, userID uint64, role string, resourceID string) (*models.Resource, *models.Credential, map[string]interface{}, string, error) {
	resource, err := h.loadK8sResource(workspaceID, resourceID)
	if err != nil {
		return nil, nil, nil, "", err
	}
	var bindings []models.ResourceCredentialBinding
	if err := h.DB.Where("workspace_id = ? AND resource_id = ?", workspaceID, resource.ID).Order("created_at ASC, id ASC").Find(&bindings).Error; err != nil {
		return nil, nil, nil, "", fmt.Errorf("加载资源凭据绑定失败")
	}
	binding := preferredResourceCredentialBinding(resource.Type, bindings)
	if binding == nil || binding.CredentialID == 0 {
		return nil, nil, nil, "", fmt.Errorf("资源尚未绑定可用凭据")
	}
	var credential models.Credential
	if err := h.DB.First(&credential, binding.CredentialID).Error; err != nil {
		return nil, nil, nil, "", fmt.Errorf("资源绑定凭据不存在")
	}
	if credential.WorkspaceID != workspaceID || !canReadCredential(h.DB, &credential, userID, role) {
		return nil, nil, nil, "", fmt.Errorf("无权访问资源绑定凭据")
	}
	if err := validateResourceBindingCredential(resource, &credential); err != nil {
		return nil, nil, nil, "", err
	}
	decrypted, err := services.NewCredentialEncryptionService().DecryptCredentialData(credential.EncryptedPayload)
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("连接凭据解密失败: %w", err)
	}
	return resource, &credential, decrypted, effectiveResourceValidationEndpoint(resource.Type, resource.Endpoint, decrypted), nil
}

func (h *ResourceHandler) createK8sTask(workspaceID, userID uint64, credential *models.Credential, decrypted map[string]interface{}, effectiveEndpoint, command, name string, priority int, payload resourceK8sTaskPayload) (*models.AgentTask, error) {
	if credential == nil {
		return nil, fmt.Errorf("资源绑定凭据不存在")
	}
	agent, err := selectAvailableWorkspaceAgent(h.DB, workspaceID)
	if err != nil {
		return nil, err
	}
	nodeConfig := map[string]interface{}{
		"command": command,
		"credentials": map[string]interface{}{
			"cluster_auth": map[string]interface{}{"credential_id": credential.ID},
		},
	}
	envMap, err := buildResourceValidationEnv("kubernetes", "cluster_auth", *credential, decrypted)
	if err != nil {
		return nil, fmt.Errorf("连接凭据不完整: %w", err)
	}
	if strings.TrimSpace(effectiveEndpoint) != "" {
		prefix := slotEnvPrefix("cluster_auth")
		envMap[prefix+"SERVER"] = strings.TrimSpace(effectiveEndpoint)
		envMap[prefix+"API_SERVER"] = strings.TrimSpace(effectiveEndpoint)
	}
	nodeConfig["env"] = envMap
	canonicalType, script, err := renderPipelineAgentScript("kubernetes", nodeConfig)
	if err != nil {
		return nil, fmt.Errorf("构建 K8s 任务失败: %w", err)
	}
	payload.TaskType = canonicalType
	payload.NodeConfig = nodeConfig
	rawParams, _ := json.Marshal(payload)
	rawEnv, _ := json.Marshal(envMap)
	task := &models.AgentTask{
		WorkspaceID: workspaceID,
		AgentID:     agent.ID,
		NodeID:      fmt.Sprintf("resource-k8s-%d", time.Now().UnixNano()),
		TaskType:    canonicalType,
		Name:        name,
		Params:      string(rawParams),
		Script:      script,
		EnvVars:     string(rawEnv),
		Status:      models.TaskStatusQueued,
		Priority:    priority,
		Timeout:     180,
		MaxRetries:  0,
		CreatedBy:   userID,
	}
	if err := h.DB.Omit("PipelineRunID").Create(task).Error; err != nil {
		return nil, fmt.Errorf("创建 K8s 任务失败: %w", err)
	}
	_ = h.DB.Model(&models.Agent{}).Where("id = ?", agent.ID).Update("status", models.AgentStatusBusy).Error
	_ = SharedWebSocketHandler().sendTaskAssign(*task)
	return task, nil
}

func (h *ResourceHandler) loadResourceForWorkspace(workspaceID uint64, resourceID string) (*models.Resource, error) {
	var resource models.Resource
	if err := h.DB.Where("workspace_id = ?", workspaceID).First(&resource, resourceID).Error; err != nil {
		return nil, fmt.Errorf("资源不存在")
	}
	return &resource, nil
}

func (h *ResourceHandler) loadK8sResource(workspaceID uint64, resourceID string) (*models.Resource, error) {
	resource, err := h.loadResourceForWorkspace(workspaceID, resourceID)
	if err != nil {
		return nil, err
	}
	if resource.Type != models.ResourceTypeK8sCluster {
		return nil, fmt.Errorf("当前资源不是 K8s 集群")
	}
	return resource, nil
}

func (h *ResourceHandler) writeK8sResourceError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	if err != nil && strings.Contains(err.Error(), "不存在") {
		status = http.StatusNotFound
	}
	c.JSON(status, gin.H{"code": status, "message": err.Error()})
}

func normalizeRequestedK8sKinds(kinds []string) ([]string, error) {
	if len(kinds) == 0 {
		return []string{"Deployment", "StatefulSet", "DaemonSet", "Pod", "Service", "Ingress", "CronJob", "Job", "ConfigMap", "Secret"}, nil
	}
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		value := strings.TrimSpace(kind)
		resourceName, ok := supportedK8sKinds[value]
		if !ok || value == "Namespace" {
			return nil, fmt.Errorf("K8s 资源类型 %s 暂不支持浏览", value)
		}
		if _, exists := seen[resourceName]; exists {
			continue
		}
		seen[resourceName] = struct{}{}
		normalized = append(normalized, value)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func buildK8sNamespaceQueryCommand() string {
	return "kubectl get namespaces -o json"
}

func buildK8sResourceQueryCommand(namespace string, kinds []string) (string, error) {
	resources := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		resourceName, ok := supportedK8sKinds[kind]
		if !ok || kind == "Namespace" {
			return "", fmt.Errorf("K8s 资源类型 %s 暂不支持浏览", kind)
		}
		resources = append(resources, resourceName)
	}
	return fmt.Sprintf("kubectl get %s -n %s -o json", strings.Join(resources, ","), namespace), nil
}

func buildK8sActionCommand(targetKind, targetName, namespace, action string, replicas *int) (string, error) {
	resourceName, ok := supportedK8sKinds[targetKind]
	if !ok {
		return "", fmt.Errorf("K8s 资源类型 %s 暂不支持操作", targetKind)
	}
	resourceName = strings.TrimSuffix(resourceName, "s")
	switch action {
	case "rollout_restart":
		switch targetKind {
		case "Deployment", "StatefulSet", "DaemonSet":
			return fmt.Sprintf("kubectl rollout restart %s %s -n %s", strings.ToLower(targetKind), targetName, namespace), nil
		default:
			return "", fmt.Errorf("资源类型 %s 不支持 rollout_restart", targetKind)
		}
	case "scale":
		if targetKind != "Deployment" && targetKind != "StatefulSet" {
			return "", fmt.Errorf("资源类型 %s 不支持 scale", targetKind)
		}
		if replicas == nil || *replicas < 0 {
			return "", fmt.Errorf("scale 操作需要有效副本数")
		}
		return fmt.Sprintf("kubectl scale %s %s -n %s --replicas=%d", strings.ToLower(targetKind), targetName, namespace, *replicas), nil
	case "suspend":
		if targetKind != "CronJob" {
			return "", fmt.Errorf("资源类型 %s 不支持 suspend", targetKind)
		}
		return fmt.Sprintf(`kubectl patch cronjob %s -n %s --type merge -p '{"spec":{"suspend":true}}'`, targetName, namespace), nil
	case "resume":
		if targetKind != "CronJob" {
			return "", fmt.Errorf("资源类型 %s 不支持 resume", targetKind)
		}
		return fmt.Sprintf(`kubectl patch cronjob %s -n %s --type merge -p '{"spec":{"suspend":false}}'`, targetName, namespace), nil
	default:
		_ = resourceName
		return "", fmt.Errorf("K8s 操作 %s 暂不支持", action)
	}
}

func isSafeK8sName(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && k8sResourceNamePattern.MatchString(trimmed)
}

func syncResourceOperationAuditRecords(db *gorm.DB, task *models.AgentTask) error {
	if db == nil || task == nil || task.ID == 0 {
		return nil
	}
	var audits []models.ResourceOperationAudit
	if err := db.Where("task_id = ?", task.ID).Find(&audits).Error; err != nil {
		return err
	}
	if len(audits) == 0 {
		return nil
	}
	status := mapResourceOperationStatus(task.Status)
	completedAt := int64(0)
	if status == models.ResourceOperationStatusSuccess || status == models.ResourceOperationStatusFailed || status == models.ResourceOperationStatusCancelled {
		completedAt = task.EndTime
		if completedAt == 0 {
			completedAt = time.Now().Unix()
		}
	}
	summary := summarizeTaskResult(task)
	for _, audit := range audits {
		updates := map[string]interface{}{
			"status":         status,
			"result_summary": summary,
			"error_message":  task.ErrorMsg,
		}
		if completedAt > 0 {
			updates["completed_at"] = completedAt
		}
		if err := db.Model(&models.ResourceOperationAudit{}).Where("id = ?", audit.ID).Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}

func mapResourceOperationStatus(taskStatus string) models.ResourceOperationStatus {
	switch taskStatus {
	case models.TaskStatusExecuteSuccess:
		return models.ResourceOperationStatusSuccess
	case models.TaskStatusExecuteFailed, models.TaskStatusScheduleFailed, models.TaskStatusDispatchTimeout, models.TaskStatusLeaseExpired:
		return models.ResourceOperationStatusFailed
	case models.TaskStatusCancelled:
		return models.ResourceOperationStatusCancelled
	case models.TaskStatusRunning, models.TaskStatusAcked, models.TaskStatusDispatching, models.TaskStatusAssigned, models.TaskStatusPulling:
		return models.ResourceOperationStatusRunning
	default:
		return models.ResourceOperationStatusQueued
	}
}

func summarizeTaskResult(task *models.AgentTask) string {
	if task == nil {
		return ""
	}
	if strings.TrimSpace(task.ErrorMsg) != "" {
		return strings.TrimSpace(task.ErrorMsg)
	}
	if strings.TrimSpace(task.ResultData) == "" {
		return task.Status
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(task.ResultData), &payload); err == nil {
		if stdout := strings.TrimSpace(stringValue(payload["stdout"])); stdout != "" {
			if len(stdout) > 300 {
				return stdout[:300]
			}
			return stdout
		}
	}
	if len(task.ResultData) > 300 {
		return task.ResultData[:300]
	}
	return task.ResultData
}
