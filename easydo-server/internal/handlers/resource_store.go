package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"easydo-server/internal/models"
	"easydo-server/internal/services"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ResourceHandler struct {
	DB *gorm.DB
}

type StoreTemplateHandler struct {
	DB *gorm.DB
}

type DeploymentHandler struct {
	DB *gorm.DB
}

type LLMModelHandler struct {
	DB *gorm.DB
}

func NewResourceHandler() *ResourceHandler {
	return &ResourceHandler{DB: models.DB}
}

func NewStoreTemplateHandler() *StoreTemplateHandler {
	return &StoreTemplateHandler{DB: models.DB}
}

func NewDeploymentHandler() *DeploymentHandler {
	return &DeploymentHandler{DB: models.DB}
}

func NewLLMModelHandler() *LLMModelHandler {
	return &LLMModelHandler{DB: models.DB}
}

type createResourceRequest struct {
	ProjectID          uint64              `json:"project_id"`
	Name               string              `json:"name"`
	Description        string              `json:"description"`
	Type               models.ResourceType `json:"type"`
	Environment        string              `json:"environment"`
	Endpoint           string              `json:"endpoint"`
	CredentialID       uint64              `json:"credential_id"`
	VerificationTaskID uint64              `json:"verification_task_id"`
	Labels             string              `json:"labels"`
	Metadata           string              `json:"metadata"`
}

type verifyResourceConnectionRequest struct {
	Type         models.ResourceType `json:"type"`
	Endpoint     string              `json:"endpoint"`
	CredentialID uint64              `json:"credential_id"`
}

type bindResourceCredentialRequest struct {
	CredentialID uint64 `json:"credential_id"`
	Purpose      string `json:"purpose"`
}

type resourceValidationTaskPayload struct {
	Verification resourceValidationSnapshot `json:"verification"`
	TaskType     string                     `json:"task_type"`
	NodeConfig   map[string]interface{}     `json:"node_config,omitempty"`
}

type resourceValidationSnapshot struct {
	Kind                string              `json:"kind"`
	ResourceType        models.ResourceType `json:"resource_type"`
	Endpoint            string              `json:"endpoint"`
	EffectiveEndpoint   string              `json:"effective_endpoint"`
	CredentialID        uint64              `json:"credential_id"`
	CredentialUpdatedAt int64               `json:"credential_updated_at"`
	DraftHash           string              `json:"draft_hash"`
}

type resourceValidationConsumeResult struct {
	Verification struct {
		ConsumedAt int64  `json:"consumed_at"`
		ResourceID uint64 `json:"resource_id"`
	} `json:"verification"`
}

type resourceBaseInfoTaskPayload struct {
	Collection resourceBaseInfoCollectionSnapshot `json:"collection"`
	TaskType   string                             `json:"task_type"`
	NodeConfig map[string]interface{}             `json:"node_config,omitempty"`
}

type resourceBaseInfoCollectionSnapshot struct {
	Kind              string              `json:"kind"`
	ResourceID        uint64              `json:"resource_id"`
	ResourceType      models.ResourceType `json:"resource_type"`
	Endpoint          string              `json:"endpoint"`
	EffectiveEndpoint string              `json:"effective_endpoint"`
	CredentialID      uint64              `json:"credential_id"`
	CollectorSource   string              `json:"collector_source"`
}

func (h *ResourceHandler) ListResources(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanAccessWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权访问该工作空间资源"})
		return
	}

	var resources []models.Resource
	query := h.DB.Where("workspace_id = ?", workspaceID).Order("created_at DESC, id DESC")
	if resourceType := strings.TrimSpace(c.Query("type")); resourceType != "" {
		query = query.Where("type = ?", resourceType)
	}
	if environment := strings.TrimSpace(c.Query("environment")); environment != "" {
		query = query.Where("environment = ?", environment)
	}
	if err := query.Preload("Bindings").Preload("Bindings.Credential").Find(&resources).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "加载资源列表失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": buildResourceListResponse(resources)})
}

func (h *ResourceHandler) CreateResource(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanManageWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权创建资源"})
		return
	}

	var req createResourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "资源名称不能为空"})
		return
	}
	if req.Type != models.ResourceTypeVM && req.Type != models.ResourceTypeK8sCluster {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "资源类型无效"})
		return
	}
	if req.CredentialID == 0 || req.VerificationTaskID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "新建资源前必须先完成连接验证"})
		return
	}

	var resource models.Resource
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		credential, verificationTask, validation, err := h.authorizeResourceCreationValidation(tx, workspaceID, userID, role, &req)
		if err != nil {
			return err
		}

		resource = models.Resource{
			WorkspaceID:     workspaceID,
			ProjectID:       optionalUint64(req.ProjectID),
			Name:            strings.TrimSpace(req.Name),
			Description:     strings.TrimSpace(req.Description),
			Type:            req.Type,
			Environment:     defaultIfEmpty(strings.TrimSpace(req.Environment), "development"),
			Status:          models.ResourceStatusOnline,
			Endpoint:        defaultIfEmpty(strings.TrimSpace(req.Endpoint), validation.Verification.EffectiveEndpoint),
			Labels:          req.Labels,
			Metadata:        req.Metadata,
			LastCheckAt:     time.Now().Unix(),
			LastCheckResult: fmt.Sprintf("验证通过：执行器任务 #%d 已确认资源可连通", verificationTask.ID),
			CreatedBy:       userID,
		}
		if err := tx.Create(&resource).Error; err != nil {
			return fmt.Errorf("创建资源失败")
		}

		binding := models.ResourceCredentialBinding{
			WorkspaceID:  workspaceID,
			ResourceID:   resource.ID,
			CredentialID: credential.ID,
			Purpose:      resourcePrimaryBindingPurpose(req.Type),
			BoundBy:      userID,
		}
		if err := tx.Create(&binding).Error; err != nil {
			return fmt.Errorf("绑定资源凭据失败")
		}

		consume := resourceValidationConsumeResult{}
		consume.Verification.ConsumedAt = time.Now().Unix()
		consume.Verification.ResourceID = resource.ID
		rawConsume, _ := json.Marshal(consume)
		if err := tx.Model(&verificationTask).Update("result_data", string(rawConsume)).Error; err != nil {
			return fmt.Errorf("标记验证结果失败")
		}
		return nil
	}); err != nil {
		statusCode := http.StatusBadRequest
		if strings.Contains(err.Error(), "失败") && !strings.Contains(err.Error(), "验证") && !strings.Contains(err.Error(), "凭据") {
			statusCode = http.StatusInternalServerError
		}
		c.JSON(statusCode, gin.H{"code": statusCode, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": buildResourceResponse(resource)})
}

func (h *ResourceHandler) VerifyResourceConnection(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanManageWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权验证资源连接"})
		return
	}

	var req verifyResourceConnectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return
	}

	task, err := h.createResourceValidationTask(workspaceID, userID, role, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": gin.H{"task_id": task.ID, "status": task.Status, "agent_id": task.AgentID}})
}

func (h *ResourceHandler) GetResource(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanAccessWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权访问该资源"})
		return
	}

	var resource models.Resource
	if err := h.DB.Preload("Bindings").Preload("Bindings.Credential").Where("workspace_id = ?", workspaceID).First(&resource, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "资源不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": buildResourceResponse(resource)})
}

func (h *ResourceHandler) ListResourceCredentialBindings(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanAccessWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权访问资源凭据绑定"})
		return
	}

	var resource models.Resource
	if err := h.DB.Where("workspace_id = ?", workspaceID).First(&resource, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "资源不存在"})
		return
	}

	var bindings []models.ResourceCredentialBinding
	if err := h.DB.Preload("Credential").Where("workspace_id = ? AND resource_id = ?", workspaceID, resource.ID).Order("created_at DESC, id DESC").Find(&bindings).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "加载资源凭据绑定失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": bindings})
}

func (h *ResourceHandler) BindResourceCredential(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanManageWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权绑定资源凭据"})
		return
	}

	var resource models.Resource
	if err := h.DB.Where("workspace_id = ?", workspaceID).First(&resource, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "资源不存在"})
		return
	}

	var req bindResourceCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return
	}
	if req.CredentialID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "凭据不能为空"})
		return
	}
	purpose := defaultIfEmpty(strings.TrimSpace(req.Purpose), "primary")

	var credential models.Credential
	if err := h.DB.First(&credential, req.CredentialID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "绑定凭据不存在"})
		return
	}
	if credential.WorkspaceID != workspaceID || !canReadCredential(h.DB, &credential, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权访问绑定凭据"})
		return
	}
	if err := validateResourceBindingCredential(&resource, &credential); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}

	binding := models.ResourceCredentialBinding{}
	err := h.DB.Where("workspace_id = ? AND resource_id = ? AND purpose = ?", workspaceID, resource.ID, purpose).First(&binding).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "加载资源凭据绑定失败"})
			return
		}
		binding = models.ResourceCredentialBinding{
			WorkspaceID:  workspaceID,
			ResourceID:   resource.ID,
			CredentialID: credential.ID,
			Purpose:      purpose,
			BoundBy:      userID,
		}
		if err := h.DB.Create(&binding).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "绑定资源凭据失败"})
			return
		}
	} else {
		binding.CredentialID = credential.ID
		binding.BoundBy = userID
		if err := h.DB.Save(&binding).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "绑定资源凭据失败"})
			return
		}
	}

	if err := h.DB.Preload("Credential").First(&binding, binding.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "加载资源凭据绑定失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": binding})
}

func (h *ResourceHandler) UnbindResourceCredential(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanManageWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权解绑资源凭据"})
		return
	}

	var binding models.ResourceCredentialBinding
	if err := h.DB.Where("workspace_id = ? AND resource_id = ?", workspaceID, c.Param("id")).First(&binding, c.Param("binding_id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "资源凭据绑定不存在"})
		return
	}
	if err := h.DB.Delete(&binding).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "解绑资源凭据失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "解绑成功"})
}

func (h *ResourceHandler) UpdateResource(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanManageWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权修改资源"})
		return
	}

	var resource models.Resource
	if err := h.DB.Where("workspace_id = ?", workspaceID).First(&resource, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "资源不存在"})
		return
	}

	var req createResourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "资源名称不能为空"})
		return
	}
	if req.Type != models.ResourceTypeVM && req.Type != models.ResourceTypeK8sCluster {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "资源类型无效"})
		return
	}
	resource.ProjectID = optionalUint64(req.ProjectID)
	resource.Name = strings.TrimSpace(req.Name)
	resource.Description = strings.TrimSpace(req.Description)
	resource.Type = req.Type
	resource.Environment = defaultIfEmpty(strings.TrimSpace(req.Environment), resource.Environment)
	resource.Endpoint = strings.TrimSpace(req.Endpoint)
	resource.Labels = req.Labels
	resource.Metadata = req.Metadata
	if err := h.DB.Save(&resource).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "修改资源失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": buildResourceResponse(resource)})
}

func (h *ResourceHandler) RefreshResourceBaseInfo(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanManageWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权刷新资源基础信息"})
		return
	}

	var resource models.Resource
	if err := h.DB.Preload("Bindings").Where("workspace_id = ?", workspaceID).First(&resource, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "资源不存在"})
		return
	}

	task, err := h.createResourceBaseInfoTask(workspaceID, userID, role, &resource)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": gin.H{"task_id": task.ID, "status": task.Status, "agent_id": task.AgentID}})
}

func (h *ResourceHandler) DeleteResource(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanManageWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权删除资源"})
		return
	}

	result := h.DB.Where("workspace_id = ?", workspaceID).Delete(&models.Resource{}, c.Param("id"))
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "删除资源失败"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "资源不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "删除成功"})
}

type createTemplateRequest struct {
	Name               string                     `json:"name"`
	Description        string                     `json:"description"`
	TemplateType       models.StoreTemplateType   `json:"template_type"`
	TargetResourceType models.ResourceType        `json:"target_resource_type"`
	Source             models.StoreTemplateSource `json:"source"`
	Summary            string                     `json:"summary"`
	Icon               string                     `json:"icon"`
}

type createTemplateVersionRequest struct {
	Version           string                     `json:"version"`
	PipelineID        uint64                     `json:"pipeline_id"`
	DeploymentMode    string                     `json:"deployment_mode"`
	DefaultConfig     string                     `json:"default_config"`
	DependencyConfig  string                     `json:"dependency_config"`
	TargetConstraints string                     `json:"target_constraints"`
	Status            models.StoreTemplateStatus `json:"status"`
	Parameters        []templateParameterRequest `json:"parameters"`
}

type templateParameterRequest struct {
	Name         string   `json:"name"`
	Label        string   `json:"label"`
	Description  string   `json:"description"`
	Type         string   `json:"type"`
	DefaultValue string   `json:"default_value"`
	OptionValues []string `json:"option_values"`
	Required     bool     `json:"required"`
	Mutable      *bool    `json:"mutable"`
	Advanced     bool     `json:"advanced"`
	SortOrder    int      `json:"sort_order"`
}

func (h *StoreTemplateHandler) ListTemplates(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanAccessWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权访问模板"})
		return
	}

	var templates []models.StoreTemplate
	query := h.DB.Where("source = ? OR workspace_id = ?", models.StoreTemplateSourcePlatform, workspaceID).
		Order("created_at DESC, id DESC")
	if templateType := strings.TrimSpace(c.Query("template_type")); templateType != "" {
		query = query.Where("template_type = ?", templateType)
	}
	if targetType := strings.TrimSpace(c.Query("target_resource_type")); targetType != "" {
		query = query.Where("target_resource_type = ?", targetType)
	}
	if err := query.Find(&templates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "加载模板列表失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": templates})
}

func (h *StoreTemplateHandler) CreateTemplate(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanManageWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权创建模板"})
		return
	}

	var req createTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "模板名称不能为空"})
		return
	}
	if req.TemplateType != models.StoreTemplateTypeApp && req.TemplateType != models.StoreTemplateTypeLLM {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "模板类型无效"})
		return
	}
	if req.TargetResourceType != models.ResourceTypeVM && req.TargetResourceType != models.ResourceTypeK8sCluster {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "目标资源类型无效"})
		return
	}
	source := req.Source
	if source == "" {
		source = models.StoreTemplateSourceWorkspace
	}
	if source != models.StoreTemplateSourcePlatform && source != models.StoreTemplateSourceWorkspace {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "模板来源无效"})
		return
	}
	workspaceValue := workspaceID
	template := models.StoreTemplate{
		WorkspaceID:        workspaceValue,
		Name:               strings.TrimSpace(req.Name),
		Description:        strings.TrimSpace(req.Description),
		TemplateType:       req.TemplateType,
		TargetResourceType: req.TargetResourceType,
		Source:             source,
		Status:             models.StoreTemplateStatusDraft,
		Summary:            strings.TrimSpace(req.Summary),
		Icon:               strings.TrimSpace(req.Icon),
		CreatedBy:          userID,
	}
	if err := h.DB.Create(&template).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "创建模板失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": template})
}

func (h *StoreTemplateHandler) GetTemplate(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanAccessWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权访问模板"})
		return
	}

	var template models.StoreTemplate
	if err := h.DB.Where("source = ? OR workspace_id = ?", models.StoreTemplateSourcePlatform, workspaceID).
		First(&template, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "模板不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": template})
}

func (h *StoreTemplateHandler) UpdateTemplate(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanManageWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权修改模板"})
		return
	}

	var template models.StoreTemplate
	if err := h.DB.Where("workspace_id = ?", workspaceID).First(&template, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "模板不存在"})
		return
	}

	var req createTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "模板名称不能为空"})
		return
	}
	template.Name = strings.TrimSpace(req.Name)
	template.Description = strings.TrimSpace(req.Description)
	template.TemplateType = req.TemplateType
	template.TargetResourceType = req.TargetResourceType
	template.Summary = strings.TrimSpace(req.Summary)
	template.Icon = strings.TrimSpace(req.Icon)
	if err := h.DB.Save(&template).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "修改模板失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": template})
}

func (h *StoreTemplateHandler) DeleteTemplate(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanManageWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权删除模板"})
		return
	}

	result := h.DB.Where("workspace_id = ?", workspaceID).Delete(&models.StoreTemplate{}, c.Param("id"))
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "删除模板失败"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "模板不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "删除成功"})
}

func (h *StoreTemplateHandler) ListTemplateVersions(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanAccessWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权访问模板版本"})
		return
	}

	var versions []models.StoreTemplateVersion
	if err := h.DB.Where("template_id = ? AND workspace_id = ?", c.Param("id"), workspaceID).
		Preload("Parameters", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC, id ASC")
		}).
		Order("created_at DESC, id DESC").
		Find(&versions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "加载模板版本失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": versions})
}

func (h *StoreTemplateHandler) CreateTemplateVersion(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanManageWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权创建模板版本"})
		return
	}

	var template models.StoreTemplate
	if err := h.DB.Where("workspace_id = ?", workspaceID).First(&template, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "模板不存在"})
		return
	}

	var req createTemplateVersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return
	}
	if strings.TrimSpace(req.Version) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "版本号不能为空"})
		return
	}
	if req.PipelineID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "必须绑定流水线"})
		return
	}
	var pipeline models.Pipeline
	if err := h.DB.Where("id = ? AND workspace_id = ?", req.PipelineID, workspaceID).First(&pipeline).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "绑定流水线不存在"})
		return
	}
	if ok, msg := validateTemplatePipelineCompatibility(template.TargetResourceType, pipeline.Config); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": msg})
		return
	}
	status := req.Status
	if status == "" {
		status = models.StoreTemplateStatusDraft
	}
	version := models.StoreTemplateVersion{
		WorkspaceID:       workspaceID,
		TemplateID:        template.ID,
		PipelineID:        pipeline.ID,
		Version:           strings.TrimSpace(req.Version),
		DeploymentMode:    defaultIfEmpty(strings.TrimSpace(req.DeploymentMode), "pipeline"),
		DefaultConfig:     req.DefaultConfig,
		DependencyConfig:  req.DependencyConfig,
		TargetConstraints: req.TargetConstraints,
		Status:            status,
		CreatedBy:         userID,
	}
	parameters, err := buildTemplateParameters(req.Parameters, userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&version).Error; err != nil {
			return err
		}
		for i := range parameters {
			parameters[i].TemplateVersionID = version.ID
		}
		if len(parameters) > 0 {
			if err := tx.Create(&parameters).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "创建模板版本失败"})
		return
	}
	version.Parameters = parameters

	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": version})
}

func (h *LLMModelHandler) ListModels(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanAccessWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权访问模型目录"})
		return
	}

	var catalog []models.LLMModelCatalog
	query := h.DB.Order("updated_at DESC, id DESC")
	if source := normalizeLLMModelCatalogSource(c.Query("source")); source != "" {
		query = query.Where("source = ?", source)
	}
	if err := query.Find(&catalog).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "加载模型目录失败"})
		return
	}
	for i := range catalog {
		if strings.TrimSpace(catalog[i].ParameterSize) != "" {
			continue
		}
		metadata := decodeJSONObjectField(catalog[i].Metadata, map[string]interface{}{}).(map[string]interface{})
		catalog[i].ParameterSize = resolveImportedModelParameterSize(metadata)
	}

	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": catalog})
}

func (h *LLMModelHandler) ImportModel(c *gin.Context) {
	userID, role := getRequestUser(c)
	if !isAdminRole(role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "仅管理员可导入模型"})
		return
	}

	var req importLLMModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return
	}
	if strings.TrimSpace(req.SourceModelID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "模型 ID 不能为空"})
		return
	}
	if req.Metadata == nil {
		fetched, err := fetchImportedLLMModelMetadata(req.Source, req.SourceModelID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
			return
		}
		req.Name = defaultIfEmpty(strings.TrimSpace(req.Name), fetched.Name)
		req.DisplayName = defaultIfEmpty(strings.TrimSpace(req.DisplayName), fetched.DisplayName)
		req.Summary = defaultIfEmpty(strings.TrimSpace(req.Summary), fetched.Summary)
		req.License = defaultIfEmpty(strings.TrimSpace(req.License), fetched.License)
		if len(req.Tags) == 0 {
			req.Tags = fetched.Tags
		}
		req.Metadata = fetched.Metadata
	}

	model, err := buildImportedLLMModelCatalog(req, userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}

	var existing models.LLMModelCatalog
	err = h.DB.Where("source = ? AND source_model_id = ?", model.Source, model.SourceModelID).First(&existing).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "加载模型目录失败"})
		return
	}
	if err == nil {
		existing.Name = model.Name
		existing.DisplayName = model.DisplayName
		existing.ParameterSize = model.ParameterSize
		existing.Summary = model.Summary
		existing.License = model.License
		existing.Tags = model.Tags
		existing.Metadata = model.Metadata
		existing.ImportedBy = model.ImportedBy
		if err := h.DB.Save(&existing).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "更新模型目录失败"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": existing})
		return
	}

	if err := h.DB.Create(&model).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "导入模型失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": model})
}

type createDeploymentRequestRequest struct {
	TemplateVersionID uint64                 `json:"template_version_id"`
	TargetResourceID  uint64                 `json:"target_resource_id"`
	LLMModelID        uint64                 `json:"llm_model_id"`
	Parameters        map[string]interface{} `json:"parameters"`
}

type importLLMModelRequest struct {
	Source        string                 `json:"source"`
	SourceModelID string                 `json:"source_model_id"`
	Name          string                 `json:"name"`
	DisplayName   string                 `json:"display_name"`
	ParameterSize string                 `json:"parameter_size"`
	Summary       string                 `json:"summary"`
	License       string                 `json:"license"`
	Tags          []string               `json:"tags"`
	Metadata      map[string]interface{} `json:"metadata"`
}

func (h *DeploymentHandler) ListDeploymentRequests(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanAccessWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权访问部署请求"})
		return
	}
	var requests []models.DeploymentRequest
	if err := h.DB.Where("workspace_id = ?", workspaceID).Order("created_at DESC, id DESC").Find(&requests).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "加载部署请求失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": requests})
}

func (h *DeploymentHandler) GetDeploymentRequest(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanAccessWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权访问部署请求"})
		return
	}
	var request models.DeploymentRequest
	if err := h.DB.Where("workspace_id = ?", workspaceID).First(&request, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "部署请求不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": request})
}

func (h *DeploymentHandler) CreateDeploymentRequest(c *gin.Context) {
	workspaceID, workspaceRole := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanWriteWorkspaceResource(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权发起部署"})
		return
	}

	var req createDeploymentRequestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return
	}
	if req.TemplateVersionID == 0 || req.TargetResourceID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "模板版本和目标资源不能为空"})
		return
	}

	var version models.StoreTemplateVersion
	if err := h.DB.Preload("Template").Preload("Parameters", func(db *gorm.DB) *gorm.DB {
		return db.Order("sort_order ASC, id ASC")
	}).Where("workspace_id = ?", workspaceID).First(&version, req.TemplateVersionID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "模板版本不存在"})
		return
	}
	var resource models.Resource
	if err := h.DB.Where("workspace_id = ?", workspaceID).First(&resource, req.TargetResourceID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "目标资源不存在"})
		return
	}
	if version.Template == nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "模板信息不存在"})
		return
	}
	if version.Template.TargetResourceType != resource.Type {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "模板与资源类型不匹配"})
		return
	}
	var pipeline models.Pipeline
	if err := h.DB.Where("id = ? AND workspace_id = ?", version.PipelineID, workspaceID).First(&pipeline).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "绑定流水线不存在"})
		return
	}

	var llmModel *models.LLMModelCatalog
	if version.Template.TemplateType == models.StoreTemplateTypeLLM {
		if req.LLMModelID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "LLM 模板部署必须选择模型"})
			return
		}
		selectedModel := models.LLMModelCatalog{}
		if err := h.DB.First(&selectedModel, req.LLMModelID).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "所选模型不存在"})
			return
		}
		llmModel = &selectedModel
	}
	resolvedParameters, err := resolveDeploymentParameters(&version, req.Parameters)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "部署参数无效: " + err.Error()})
		return
	}
	resolvedParameters, err = resolveRuntimeModelMountParameters(&resource, version.Template, llmModel, resolvedParameters)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "部署参数无效: " + err.Error()})
		return
	}

	resolvedConfig, err := h.resolveDeploymentPipelineConfig(&pipeline, version.Template, &resource, llmModel, resolvedParameters)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "部署配置解析失败: " + err.Error()})
		return
	}
	if err := h.applyResourceCredentialBindings(&resolvedConfig, &resource); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "部署资源凭据绑定无效: " + err.Error()})
		return
	}
	configJSON, err := json.Marshal(resolvedConfig)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "部署配置序列化失败"})
		return
	}

	parameterSnapshot, _ := json.Marshal(resolvedParameters)
	resourceSnapshot, _ := json.Marshal(resource)
	versionSnapshot, _ := json.Marshal(version)
	llmModelSnapshot := []byte{}
	if llmModel != nil {
		llmModelSnapshot, _ = json.Marshal(llmModel)
	}

	triggerUsername := c.GetString("username")
	if triggerUsername == "" {
		triggerUsername = "system"
	}
	request := models.DeploymentRequest{
		WorkspaceID:             workspaceID,
		ProjectID:               resource.ProjectID,
		TemplateID:              version.TemplateID,
		TemplateVersionID:       version.ID,
		TemplateType:            version.Template.TemplateType,
		TargetResourceID:        resource.ID,
		TargetResourceType:      resource.Type,
		ParameterSnapshot:       string(parameterSnapshot),
		ResourceSnapshot:        string(resourceSnapshot),
		TemplateVersionSnapshot: string(versionSnapshot),
		LLMModelSnapshot:        string(llmModelSnapshot),
		Status:                  models.DeploymentRequestStatusValidating,
		RequestedBy:             userID,
	}
	if llmModel != nil {
		request.LLMModelID = llmModel.ID
	}
	resourceEnv := resource.Environment
	if resourceEnv == "" {
		resourceEnv = pipeline.Environment
	}

	ph := &PipelineHandler{DB: h.DB}
	var runConfig PipelineConfig
	if err := json.Unmarshal(configJSON, &runConfig); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "部署配置解析失败"})
		return
	}
	run, buildNumber, err := ph.launchPipelineRun(models.Pipeline{BaseModel: pipeline.BaseModel, Name: pipeline.Name, Description: pipeline.Description, WorkspaceID: pipeline.WorkspaceID, ProjectID: pipeline.ProjectID, OwnerID: pipeline.OwnerID, Environment: resourceEnv}, runConfig, pipelineRunTriggerContext{
		TriggerType:     "deployment_request",
		TriggerUser:     triggerUsername,
		TriggerUserID:   userID,
		TriggerUserRole: workspaceRole,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "创建部署运行失败: " + err.Error()})
		return
	}
	request.PipelineID = pipeline.ID
	request.PipelineRunID = run.ID
	request.Status = models.DeploymentRequestStatus(run.Status)
	if err := h.DB.Create(&request).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "创建部署请求失败"})
		return
	}
	record := models.DeploymentRecord{
		WorkspaceID:   workspaceID,
		RequestID:     request.ID,
		PipelineRunID: run.ID,
		Status:        models.DeploymentRequestStatus(run.Status),
		AuditSummary:  "deployment request created",
		ResultSummary: "build number " + convertToString(buildNumber),
	}
	if err := h.DB.Create(&record).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "创建部署记录失败"})
		return
	}
	emitDeploymentRequestNotification(h.DB, &request, NotificationEventTypeDeploymentRequestCreated, "部署请求已创建", "部署请求已创建并进入执行流程")
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": request})
}

func (h *DeploymentHandler) resolveDeploymentPipelineConfig(pipeline *models.Pipeline, template *models.StoreTemplate, resource *models.Resource, llmModel *models.LLMModelCatalog, parameters map[string]interface{}) (PipelineConfig, error) {
	var config PipelineConfig
	if err := json.Unmarshal([]byte(pipeline.Config), &config); err != nil {
		return PipelineConfig{}, err
	}
	resolver := NewVariableResolver()
	inputs := buildDeploymentInputs(resource, llmModel, parameters)
	resolver.SetInputs(inputs)
	for i := range config.Nodes {
		nodeCfg := normalizePipelineNodeConfig(config.Nodes[i].Type, normalizeTaskTypeForConfig(config.Nodes[i].Type), config.Nodes[i].getNodeConfig())
		resolved, err := resolver.ResolveNodeConfig(nodeCfg)
		if err == nil {
			resolved = sanitizeResolvedVLLMVMScript(template, resolved)
			resolved = applyPlatformLLMGPUSelection(template, resource, llmModel, parameters, resolved)
			config.Nodes[i].Config = resolved
			config.Nodes[i].Params = nil
		}
	}
	if ok, msg := config.ValidateDAG(); !ok {
		return PipelineConfig{}, fmt.Errorf(msg)
	}
	if ok, msg := config.ValidateTaskTypes(); !ok {
		return PipelineConfig{}, fmt.Errorf(msg)
	}
	return config, nil
}

var resolvedVLLMSwapSpacePattern = regexp.MustCompile(`if \[ -n "[^"]*" \]; then(?: |\n  )VLLM_ARGS="\$VLLM_ARGS --swap-space [^"]*";? fi\n?`)

func sanitizeResolvedVLLMVMScript(template *models.StoreTemplate, nodeConfig map[string]interface{}) map[string]interface{} {
	if template == nil || template.TemplateType != models.StoreTemplateTypeLLM || template.TargetResourceType != models.ResourceTypeVM {
		return nodeConfig
	}
	if strings.TrimSpace(template.Name) != "vLLM" {
		return nodeConfig
	}
	script, _ := nodeConfig["script"].(string)
	if script == "" || !strings.Contains(script, `RUNTIME_RUN_CMD="`) || !strings.Contains(script, `$IMAGE_REF vllm serve $MODEL_REF $VLLM_ARGS`) {
		return nodeConfig
	}
	script = resolvedVLLMSwapSpacePattern.ReplaceAllString(script, "")
	script = strings.ReplaceAll(script, `$IMAGE_REF vllm serve $MODEL_REF $VLLM_ARGS`, `$IMAGE_REF $MODEL_REF $VLLM_ARGS`)
	nodeConfig["script"] = script
	return nodeConfig
}

func applyPlatformLLMGPUSelection(template *models.StoreTemplate, resource *models.Resource, llmModel *models.LLMModelCatalog, parameters map[string]interface{}, nodeConfig map[string]interface{}) map[string]interface{} {
	if template == nil || resource == nil || nodeConfig == nil {
		return nodeConfig
	}
	if template.TemplateType != models.StoreTemplateTypeLLM || template.Source != models.StoreTemplateSourcePlatform {
		return nodeConfig
	}
	templateName := strings.TrimSpace(template.Name)
	selection := extractSelectedGPUConfig(parameters)
	if selection.VisibleDevices == "" && selection.VisibleUUIDs == "" && selection.Count == 0 {
		return nodeConfig
	}
	if selection.Count == 0 {
		selection.Count = 1
	}
	switch resource.Type {
	case models.ResourceTypeVM:
		switch templateName {
		case "vLLM":
			if script, ok := nodeConfig["script"].(string); ok && script != "" {
				nodeConfig["script"] = applySelectedGPUToVLLMVMScript(script, selection)
			}
		case "Ollama", "SGLang":
			runArgs := strings.TrimSpace(convertToString(nodeConfig["run_args"]))
			nodeConfig["run_args"] = mergeDockerRunArgs(runArgs, selection)
		}
	case models.ResourceTypeK8sCluster:
		switch templateName {
		case "vLLM", "vLLM K8s":
			if command, ok := nodeConfig["command"].(string); ok && command != "" {
				nodeConfig["command"] = applySelectedGPUToVLLMK8sCommand(command, selection)
			}
		case "Ollama", "Ollama K8s":
			nodeConfig["command"] = buildOllamaK8sCommand(parameters, selection)
		case "SGLang", "SGLang K8s":
			nodeConfig["command"] = buildSGLangK8sCommand(parameters, llmModel, selection)
		}
	}
	return nodeConfig
}

func mergeDockerRunArgs(existing string, selection selectedGPUConfig) string {
	parts := []string{}
	if trimmed := strings.TrimSpace(existing); trimmed != "" {
		parts = append(parts, trimmed)
	}
	deviceSelector := selection.VisibleDevices
	if deviceSelector == "" {
		deviceSelector = selection.VisibleUUIDs
	}
	if deviceSelector != "" {
		parts = append(parts, fmt.Sprintf(`--gpus '"device=%s"'`, deviceSelector))
		if selection.VisibleDevices != "" {
			parts = append(parts, fmt.Sprintf(`-e NVIDIA_VISIBLE_DEVICES=%s`, selection.VisibleDevices))
			parts = append(parts, fmt.Sprintf(`-e CUDA_VISIBLE_DEVICES=%s`, selection.VisibleDevices))
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func applySelectedGPUToVLLMVMScript(script string, selection selectedGPUConfig) string {
	deviceSelector := selection.VisibleDevices
	if deviceSelector == "" {
		deviceSelector = selection.VisibleUUIDs
	}
	if deviceSelector == "" {
		return script
	}
	replacement := fmt.Sprintf(`--gpus '"device=%s"' -e NVIDIA_VISIBLE_DEVICES=%s -e CUDA_VISIBLE_DEVICES=%s`, deviceSelector, selection.VisibleDevices, selection.VisibleDevices)
	return strings.Replace(script, `--gpus all`, replacement, 1)
}

func applySelectedGPUToVLLMK8sCommand(command string, selection selectedGPUConfig) string {
	command = strings.Replace(command, "nvidia.com/gpu: 1", fmt.Sprintf("nvidia.com/gpu: %d", selection.Count), 1)
	envBlock := fmt.Sprintf("          env:\n            - name: NVIDIA_VISIBLE_DEVICES\n              value: \"%s\"\n            - name: CUDA_VISIBLE_DEVICES\n              value: \"%s\"\n          ports:", selection.VisibleDevices, selection.VisibleDevices)
	return strings.Replace(command, "          ports:", envBlock, 1)
}

func buildOllamaK8sCommand(parameters map[string]interface{}, selection selectedGPUConfig) string {
	appName := defaultIfEmpty(strings.TrimSpace(convertToString(parameters["app_name"])), "ollama")
	imageName := defaultIfEmpty(strings.TrimSpace(convertToString(parameters["image_name"])), "ollama/ollama")
	imageTag := defaultIfEmpty(strings.TrimSpace(convertToString(parameters["image_tag"])), "latest")
	port := defaultIfEmpty(strings.TrimSpace(convertToString(parameters["port"])), "11434")
	keepAlive := defaultIfEmpty(strings.TrimSpace(convertToString(parameters["ollama_keep_alive"])), "5m")
	numParallel := defaultIfEmpty(strings.TrimSpace(convertToString(parameters["ollama_num_parallel"])), "1")
	origin := defaultIfEmpty(strings.TrimSpace(convertToString(parameters["ollama_origin"])), "*")
	return fmt.Sprintf(`cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
spec:
  replicas: 1
  selector:
    matchLabels:
      app: %s
  template:
    metadata:
      labels:
        app: %s
    spec:
      containers:
        - name: %s
          image: %s:%s
          env:
            - name: OLLAMA_HOST
              value: 0.0.0.0:%s
            - name: OLLAMA_KEEP_ALIVE
              value: "%s"
            - name: OLLAMA_NUM_PARALLEL
              value: "%s"
            - name: OLLAMA_ORIGINS
              value: "%s"
            - name: NVIDIA_VISIBLE_DEVICES
              value: "%s"
            - name: CUDA_VISIBLE_DEVICES
              value: "%s"
          ports:
            - containerPort: %s
          resources:
            limits:
              nvidia.com/gpu: %d
---
apiVersion: v1
kind: Service
metadata:
  name: %s
spec:
  selector:
    app: %s
  ports:
    - port: %s
      targetPort: %s
  type: ClusterIP
EOF`, appName, appName, appName, appName, imageName, imageTag, port, keepAlive, numParallel, origin, selection.VisibleDevices, selection.VisibleDevices, port, selection.Count, appName, appName, port, port)
}

func buildSGLangK8sCommand(parameters map[string]interface{}, llmModel *models.LLMModelCatalog, selection selectedGPUConfig) string {
	appName := defaultIfEmpty(strings.TrimSpace(convertToString(parameters["app_name"])), "sglang-service")
	imageName := defaultIfEmpty(strings.TrimSpace(convertToString(parameters["image_name"])), "lmsysorg/sglang")
	imageTag := defaultIfEmpty(strings.TrimSpace(convertToString(parameters["image_tag"])), "latest")
	host := defaultIfEmpty(strings.TrimSpace(convertToString(parameters["host"])), "0.0.0.0")
	port := defaultIfEmpty(strings.TrimSpace(convertToString(parameters["port"])), "30000")
	tpSize := defaultIfEmpty(strings.TrimSpace(convertToString(parameters["tp_size"])), "1")
	memFraction := defaultIfEmpty(strings.TrimSpace(convertToString(parameters["mem_fraction_static"])), "0.9")
	modelRef := ""
	if llmModel != nil {
		modelRef = llmModel.SourceModelID
	}
	launch := fmt.Sprintf(`exec python3 -m sglang.launch_server --model %s --tp %s --host %s --port %s --mem-fraction-static %s`, modelRef, tpSize, host, port, memFraction)
	if strings.EqualFold(strings.TrimSpace(convertToString(parameters["enable_flashinfer"])), "true") {
		launch += ` --enable-flashinfer`
	}
	return fmt.Sprintf(`cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
spec:
  replicas: 1
  selector:
    matchLabels:
      app: %s
  template:
    metadata:
      labels:
        app: %s
    spec:
      containers:
        - name: %s
          image: %s:%s
          command: ["sh", "-lc"]
          args:
            - >-
              %s
          env:
            - name: NVIDIA_VISIBLE_DEVICES
              value: "%s"
            - name: CUDA_VISIBLE_DEVICES
              value: "%s"
          ports:
            - containerPort: %s
          resources:
            limits:
              nvidia.com/gpu: %d
---
apiVersion: v1
kind: Service
metadata:
  name: %s
spec:
  selector:
    app: %s
  ports:
    - port: %s
      targetPort: %s
  type: ClusterIP
EOF`, appName, appName, appName, appName, imageName, imageTag, launch, selection.VisibleDevices, selection.VisibleDevices, port, selection.Count, appName, appName, port, port)
}

func buildDeploymentInputs(resource *models.Resource, llmModel *models.LLMModelCatalog, parameters map[string]interface{}) map[string]interface{} {
	inputs := map[string]interface{}{}
	for k, v := range parameters {
		inputs[k] = v
	}
	if llmModel != nil {
		inputs["model_id"] = llmModel.ID
		inputs["model_name"] = defaultIfEmpty(strings.TrimSpace(llmModel.DisplayName), llmModel.Name)
		inputs["model_source"] = llmModel.Source
		inputs["model_source_ref"] = llmModel.SourceModelID
		inputs["model_summary"] = llmModel.Summary
		inputs["model_license"] = llmModel.License
	}
	if resource == nil {
		return inputs
	}
	inputs["resource_id"] = resource.ID
	inputs["resource_name"] = resource.Name
	inputs["resource_type"] = resource.Type
	inputs["resource_environment"] = resource.Environment
	inputs["resource_endpoint"] = resource.Endpoint
	host, port := parseEndpointHostPort(resource.Endpoint)
	inputs["resource_host"] = host
	inputs["resource_port"] = port
	return inputs
}

func buildTemplateParameters(reqs []templateParameterRequest, _ uint64) ([]models.TemplateParameter, error) {
	if len(reqs) == 0 {
		return nil, nil
	}
	parameters := make([]models.TemplateParameter, 0, len(reqs))
	for _, req := range reqs {
		name := strings.TrimSpace(req.Name)
		if name == "" {
			return nil, fmt.Errorf("模板参数名称不能为空")
		}
		paramType := strings.TrimSpace(req.Type)
		if paramType == "" {
			return nil, fmt.Errorf("模板参数 %s 类型不能为空", name)
		}
		optionValues, err := json.Marshal(req.OptionValues)
		if err != nil {
			return nil, fmt.Errorf("模板参数 %s 选项序列化失败", name)
		}
		mutable := true
		if req.Mutable != nil {
			mutable = *req.Mutable
		}
		parameters = append(parameters, models.TemplateParameter{
			Name:         name,
			Label:        defaultIfEmpty(strings.TrimSpace(req.Label), name),
			Description:  strings.TrimSpace(req.Description),
			Type:         paramType,
			DefaultValue: req.DefaultValue,
			OptionValues: string(optionValues),
			Required:     req.Required,
			Mutable:      mutable,
			Advanced:     req.Advanced,
			SortOrder:    req.SortOrder,
		})
	}
	return parameters, nil
}

func resolveDeploymentParameters(version *models.StoreTemplateVersion, submitted map[string]interface{}) (map[string]interface{}, error) {
	if version == nil || len(version.Parameters) == 0 {
		return cloneMap(submitted), nil
	}
	resolved := make(map[string]interface{}, len(version.Parameters))
	known := make(map[string]models.TemplateParameter, len(version.Parameters))
	for _, param := range version.Parameters {
		known[param.Name] = param
		value, hasValue := submitted[param.Name]
		if !hasValue || isEmptyParameterValue(value) {
			if defaultValue, ok := normalizeTemplateParameterValue(param, param.DefaultValue, true); ok {
				resolved[param.Name] = defaultValue
			} else if emptyValue, ok := emptyTemplateParameterValue(param); ok {
				resolved[param.Name] = emptyValue
			}
		} else {
			normalized, ok := normalizeTemplateParameterValue(param, value, false)
			if !ok {
				return nil, fmt.Errorf("参数 %s 值无效", param.Label)
			}
			resolved[param.Name] = normalized
		}
		if param.Required && isEmptyParameterValue(resolved[param.Name]) {
			return nil, fmt.Errorf("参数 %s 不能为空", param.Label)
		}
	}
	for key, value := range submitted {
		if _, ok := known[key]; ok {
			continue
		}
		if key == "model_path" {
			trimmed := strings.TrimSpace(convertToString(value))
			if trimmed != "" {
				resolved[key] = trimmed
			}
			continue
		}
		if version != nil && version.Template != nil && version.Template.TemplateType == models.StoreTemplateTypeLLM && isImplicitLLMGPUParameter(key) {
			if normalized, ok := normalizeImplicitLLMGPUParameter(key, value); ok {
				resolved[key] = normalized
			}
			continue
		}
		return nil, fmt.Errorf("不支持的参数 %s", key)
	}
	return resolved, nil
}

func isImplicitLLMGPUParameter(key string) bool {
	switch strings.TrimSpace(key) {
	case "cuda_visible_devices", "nvidia_visible_devices", "gpu_indices", "gpu_ids", "device_ids", "gpu_devices", "gpu_uuids", "gpu_count":
		return true
	default:
		return false
	}
}

func normalizeImplicitLLMGPUParameter(key string, raw interface{}) (interface{}, bool) {
	if key == "gpu_count" {
		text := strings.TrimSpace(convertToString(raw))
		if text == "" {
			return nil, false
		}
		count, err := strconv.ParseInt(text, 10, 64)
		if err != nil || count <= 0 {
			return nil, false
		}
		return count, true
	}
	text := normalizeGPUDeviceList(convertToString(raw))
	if text == "" {
		return nil, false
	}
	return text, true
}

type selectedGPUConfig struct {
	VisibleDevices string
	VisibleUUIDs   string
	Count          int
}

func extractSelectedGPUConfig(parameters map[string]interface{}) selectedGPUConfig {
	indices := firstNonEmptyString(
		convertToString(parameters["gpu_indices"]),
		convertToString(parameters["gpu_ids"]),
		convertToString(parameters["device_ids"]),
		convertToString(parameters["gpu_devices"]),
		convertToString(parameters["cuda_visible_devices"]),
		convertToString(parameters["nvidia_visible_devices"]),
	)
	indices = normalizeGPUDeviceList(indices)
	uuids := normalizeGPUDeviceList(convertToString(parameters["gpu_uuids"]))
	count := 0
	if rawCount := strings.TrimSpace(convertToString(parameters["gpu_count"])); rawCount != "" {
		if parsed, err := strconv.Atoi(rawCount); err == nil && parsed > 0 {
			count = parsed
		}
	}
	if count == 0 {
		if indices != "" {
			count = len(strings.Split(indices, ","))
		} else if uuids != "" {
			count = len(strings.Split(uuids, ","))
		}
	}
	return selectedGPUConfig{VisibleDevices: indices, VisibleUUIDs: uuids, Count: count}
}

func normalizeGPUDeviceList(value string) string {
	parts := strings.Split(strings.TrimSpace(value), ",")
	cleaned := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		cleaned = append(cleaned, trimmed)
	}
	return strings.Join(cleaned, ",")
}

func emptyTemplateParameterValue(param models.TemplateParameter) (interface{}, bool) {
	switch normalizeTemplateParameterType(param.Type) {
	case "switch":
		return false, true
	case "number", "select", "text":
		return "", true
	default:
		return "", true
	}
}

func normalizeTemplateParameterValue(param models.TemplateParameter, raw interface{}, fromDefault bool) (interface{}, bool) {
	typeName := normalizeTemplateParameterType(param.Type)
	if fromDefault && strings.TrimSpace(convertToString(raw)) == "" && typeName != "switch" {
		return nil, false
	}
	switch typeName {
	case "number":
		if isEmptyParameterValue(raw) {
			return nil, false
		}
		text := strings.TrimSpace(convertToString(raw))
		number, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, false
		}
		if float64(int64(number)) == number {
			return int64(number), true
		}
		return number, true
	case "switch":
		if isEmptyParameterValue(raw) {
			return false, true
		}
		if boolean, ok := raw.(bool); ok {
			return boolean, true
		}
		return strings.EqualFold(strings.TrimSpace(convertToString(raw)), "true"), true
	case "select":
		text := strings.TrimSpace(convertToString(raw))
		if text == "" {
			return "", !param.Required
		}
		options := parseTemplateParameterOptions(param.OptionValues)
		if len(options) > 0 {
			allowed := false
			for _, option := range options {
				if option == text {
					allowed = true
					break
				}
			}
			if !allowed {
				return nil, false
			}
		}
		return text, true
	default:
		text := strings.TrimSpace(convertToString(raw))
		if text == "" && param.Required {
			return nil, false
		}
		if text == "" {
			return nil, false
		}
		return text, true
	}
}

func normalizeTemplateParameterType(typeName string) string {
	switch strings.ToLower(strings.TrimSpace(typeName)) {
	case "number", "integer", "float":
		return "number"
	case "boolean", "switch", "toggle":
		return "switch"
	case "select", "enum", "dropdown":
		return "select"
	default:
		return "text"
	}
}

func parseTemplateParameterOptions(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var options []string
	if err := json.Unmarshal([]byte(raw), &options); err != nil {
		return nil
	}
	return options
}

func isEmptyParameterValue(value interface{}) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) == ""
	}
	return false
}

func resolveRuntimeModelMountParameters(resource *models.Resource, template *models.StoreTemplate, llmModel *models.LLMModelCatalog, parameters map[string]interface{}) (map[string]interface{}, error) {
	resolved := cloneMap(parameters)
	_ = llmModel
	if resource == nil || template == nil {
		return resolved, nil
	}
	if template.TemplateType != models.StoreTemplateTypeLLM || template.Name != "vLLM" || resource.Type != models.ResourceTypeVM {
		return resolved, nil
	}
	modelPath := strings.TrimSpace(convertToString(resolved["model_path"]))
	if strings.EqualFold(modelPath, "null") {
		modelPath = ""
		resolved["model_path"] = ""
	}
	hostModelDir := strings.TrimSpace(convertToString(resolved["host_model_dir"]))
	containerModelDir := strings.TrimSpace(convertToString(resolved["container_model_dir"]))
	if modelPath == "" {
		if hostModelDir == "" && containerModelDir == "" {
			resolved["model_path"] = ""
			resolved["runtime_model_path"] = ""
			resolved["host_model_dir"] = ""
			resolved["container_model_dir"] = ""
			return resolved, nil
		}
		if hostModelDir == "" || containerModelDir == "" {
			return nil, fmt.Errorf("本地模型部署到 VM 时必须同时填写宿主机模型路径和容器模型路径")
		}
		resolved["model_path"] = hostModelDir
		resolved["runtime_model_path"] = containerModelDir
		resolved["host_model_dir"] = hostModelDir
		resolved["container_model_dir"] = containerModelDir
		return resolved, nil
	}
	if modelPath == "" {
		resolved["model_path"] = ""
		resolved["runtime_model_path"] = ""
		resolved["host_model_dir"] = ""
		resolved["container_model_dir"] = ""
		return resolved, nil
	}
	if hostModelDir == "" || containerModelDir == "" {
		return nil, fmt.Errorf("本地模型部署到 VM 时必须填写宿主机模型目录和容器模型目录")
	}
	runtimeModelPath, err := resolveMountedModelPath(hostModelDir, containerModelDir, modelPath)
	if err != nil {
		return nil, err
	}
	resolved["host_model_dir"] = hostModelDir
	resolved["container_model_dir"] = containerModelDir
	resolved["runtime_model_path"] = runtimeModelPath
	return resolved, nil
}

func resolveMountedModelPath(hostModelDir, containerModelDir, hostModelPath string) (string, error) {
	hostRoot := filepath.Clean(hostModelDir)
	containerRoot := filepath.Clean(containerModelDir)
	modelPath := filepath.Clean(hostModelPath)
	rel, err := filepath.Rel(hostRoot, modelPath)
	if err != nil {
		return "", fmt.Errorf("模型目录挂载路径无效")
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("模型路径不在资源允许的宿主机目录内")
	}
	if rel == "." {
		return containerRoot, nil
	}
	return filepath.ToSlash(filepath.Join(containerRoot, rel)), nil
}

func normalizeLLMModelCatalogSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "huggingface", "hf":
		return "huggingface"
	case "modelscope", "ms":
		return "modelscope"
	default:
		return ""
	}
}

func buildImportedLLMModelCatalog(req importLLMModelRequest, importedBy uint64) (models.LLMModelCatalog, error) {
	source := normalizeLLMModelCatalogSource(req.Source)
	if source == "" {
		return models.LLMModelCatalog{}, fmt.Errorf("模型来源仅支持 huggingface 或 modelscope")
	}
	metadata := req.Metadata
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	sourceModelID := defaultIfEmpty(strings.TrimSpace(req.SourceModelID), firstNonEmptyString(
		stringValue(metadata["id"]),
		stringValue(metadata["model_id"]),
		stringValue(metadata["modelId"]),
	))
	if sourceModelID == "" {
		return models.LLMModelCatalog{}, fmt.Errorf("模型来源标识不能为空")
	}
	name := defaultIfEmpty(strings.TrimSpace(req.Name), firstNonEmptyString(
		stringValue(metadata["name"]),
		stringValue(metadata["model_name"]),
		stringValue(nestedMapValue(metadata, "cardData", "model_name")),
		lastPathSegment(sourceModelID),
	))
	if name == "" {
		return models.LLMModelCatalog{}, fmt.Errorf("模型名称不能为空")
	}
	tags := req.Tags
	if len(tags) == 0 {
		tags = stringSliceValue(metadata["tags"])
	}
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return models.LLMModelCatalog{}, fmt.Errorf("模型标签序列化失败")
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return models.LLMModelCatalog{}, fmt.Errorf("模型元数据序列化失败")
	}
	parameterSize := defaultIfEmpty(strings.TrimSpace(req.ParameterSize), resolveImportedModelParameterSize(metadata))
	if parameterSize != "" {
		metadata["parameter_size"] = parameterSize
		if _, exists := metadata["model_size"]; !exists {
			metadata["model_size"] = parameterSize
		}
		metadataJSON, err = json.Marshal(metadata)
		if err != nil {
			return models.LLMModelCatalog{}, fmt.Errorf("模型元数据序列化失败")
		}
	}
	return models.LLMModelCatalog{
		Name:          name,
		DisplayName:   defaultIfEmpty(strings.TrimSpace(req.DisplayName), name),
		Source:        source,
		SourceModelID: sourceModelID,
		ParameterSize: parameterSize,
		Summary: defaultIfEmpty(strings.TrimSpace(req.Summary), firstNonEmptyString(
			stringValue(metadata["description"]),
			stringValue(nestedMapValue(metadata, "cardData", "description")),
		)),
		License: defaultIfEmpty(strings.TrimSpace(req.License), firstNonEmptyString(
			stringValue(metadata["license"]),
			stringValue(nestedMapValue(metadata, "cardData", "license")),
		)),
		Tags:       string(tagsJSON),
		Metadata:   string(metadataJSON),
		ImportedBy: importedBy,
	}, nil
}

type importedLLMModelMetadata struct {
	Name          string
	DisplayName   string
	ParameterSize string
	Summary       string
	License       string
	Tags          []string
	Metadata      map[string]interface{}
}

func fetchImportedLLMModelMetadata(source string, sourceModelID string) (importedLLMModelMetadata, error) {
	source = normalizeLLMModelCatalogSource(source)
	if source == "" {
		return importedLLMModelMetadata{}, fmt.Errorf("模型来源仅支持 huggingface 或 modelscope")
	}
	if strings.TrimSpace(sourceModelID) == "" {
		return importedLLMModelMetadata{}, fmt.Errorf("模型来源标识不能为空")
	}
	switch source {
	case "huggingface":
		return fetchHuggingFaceModelMetadata(sourceModelID)
	case "modelscope":
		return fetchModelScopeModelMetadata(sourceModelID)
	default:
		return importedLLMModelMetadata{}, fmt.Errorf("模型来源仅支持 huggingface 或 modelscope")
	}
}

func fetchHuggingFaceModelMetadata(sourceModelID string) (importedLLMModelMetadata, error) {
	var payload map[string]interface{}
	if err := fetchJSONPayload("https://huggingface.co/api/models/"+escapeModelPathSegment(sourceModelID), &payload); err != nil {
		return importedLLMModelMetadata{}, fmt.Errorf("拉取 Hugging Face 模型元数据失败: %w", err)
	}
	tags := stringSliceValue(payload["tags"])
	modelID := defaultIfEmpty(stringValue(payload["id"]), defaultIfEmpty(stringValue(payload["modelId"]), sourceModelID))
	return importedLLMModelMetadata{
		Name:          defaultIfEmpty(lastPathSegment(modelID), modelID),
		DisplayName:   defaultIfEmpty(lastPathSegment(modelID), modelID),
		ParameterSize: resolveImportedModelParameterSize(payload),
		Summary:       firstNonEmptyString(stringValue(payload["description"]), stringValue(nestedMapValue(payload, "cardData", "description")), stringValue(payload["pipeline_tag"])),
		License:       firstNonEmptyString(stringValue(payload["license"]), findTaggedLicense(tags), stringValue(nestedMapValue(payload, "cardData", "license"))),
		Tags:          tags,
		Metadata:      payload,
	}, nil
}

func fetchModelScopeModelMetadata(sourceModelID string) (importedLLMModelMetadata, error) {
	var payload map[string]interface{}
	if err := fetchJSONPayload("https://www.modelscope.cn/api/v1/models/"+escapeModelPathSegment(sourceModelID), &payload); err != nil {
		return importedLLMModelMetadata{}, fmt.Errorf("拉取 ModelScope 模型元数据失败: %w", err)
	}
	data, ok := payload["Data"].(map[string]interface{})
	if !ok || len(data) == 0 {
		return importedLLMModelMetadata{}, fmt.Errorf("ModelScope 模型元数据格式无效")
	}
	tags := uniqueStrings(append(stringSliceValue(data["Libraries"]), stringSliceValue(data["Language"])...))
	name := defaultIfEmpty(stringValue(data["Name"]), defaultIfEmpty(stringValue(data["ChineseName"]), lastPathSegment(sourceModelID)))
	return importedLLMModelMetadata{
		Name:          name,
		DisplayName:   defaultIfEmpty(stringValue(data["ChineseName"]), name),
		ParameterSize: resolveImportedModelParameterSize(data),
		Summary:       firstNonEmptyString(stringValue(data["Description"]), stringValue(data["Task"])),
		License:       stringValue(data["License"]),
		Tags:          tags,
		Metadata:      data,
	}, nil
}

func fetchJSONPayload(rawURL string, target interface{}) error {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return err
	}
	return nil
}

func findTaggedLicense(tags []string) string {
	for _, tag := range tags {
		if strings.HasPrefix(tag, "license:") {
			return strings.TrimSpace(strings.TrimPrefix(tag, "license:"))
		}
	}
	return ""
}

func escapeModelPathSegment(value string) string {
	return strings.ReplaceAll(url.PathEscape(strings.TrimSpace(value)), "%2F", "/")
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized := strings.TrimSpace(value)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func resolveImportedModelParameterSize(metadata map[string]interface{}) string {
	if metadata == nil {
		return ""
	}
	for _, candidate := range []interface{}{
		metadata["parameter_size"],
		metadata["model_size"],
		metadata["params"],
		nestedMapValue(metadata, "cardData", "model_size"),
		nestedMapValue(metadata, "transformersInfo", "model_size"),
		nestedMapPathValue(metadata, "ModelInfos", "safetensor", "model_size"),
		nestedMapPathValue(metadata, "modelInfos", "safetensor", "model_size"),
		nestedMapPathValue(metadata, "model_infos", "safetensor", "model_size"),
	} {
		if resolved := normalizeImportedParameterSize(candidate); resolved != "" {
			return resolved
		}
	}
	if safetensors, ok := metadata["safetensors"].(map[string]interface{}); ok {
		if parameters, ok := safetensors["parameters"].(map[string]interface{}); ok {
			var total float64
			for _, raw := range parameters {
				total += numericValue(raw)
			}
			if total > 0 {
				return formatParameterCountLabel(total)
			}
		}
	}
	return ""
}

func normalizeImportedParameterSize(value interface{}) string {
	text := strings.TrimSpace(stringValue(value))
	if text == "" {
		return ""
	}
	parsed := parseImportedParameterCount(text)
	if parsed <= 0 {
		return text
	}
	return formatParameterCountLabel(parsed)
}

func parseImportedParameterCount(raw string) float64 {
	text := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(raw, ",", "")))
	if text == "" {
		return 0
	}
	if value, err := strconv.ParseFloat(text, 64); err == nil {
		if value > 1000000 {
			return value
		}
		return value * 1e9
	}
	re := regexp.MustCompile(`(\d+(?:\.\d+)?)\s*([tbmk])`)
	matches := re.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return 0
	}
	var total float64
	for _, match := range matches {
		value, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			continue
		}
		switch match[2] {
		case "t":
			total += value * 1e12
		case "b":
			total += value * 1e9
		case "m":
			total += value * 1e6
		case "k":
			total += value * 1e3
		}
	}
	return total
}

func formatParameterCountLabel(value float64) string {
	if value <= 0 {
		return ""
	}
	units := []struct {
		suffix  string
		divisor float64
	}{
		{"T", 1e12},
		{"B", 1e9},
		{"M", 1e6},
		{"K", 1e3},
	}
	for _, unit := range units {
		if value >= unit.divisor {
			scaled := value / unit.divisor
			if scaled >= 100 {
				return fmt.Sprintf("%.0f%s", scaled, unit.suffix)
			}
			if scaled >= 10 {
				return fmt.Sprintf("%.1f%s", scaled, unit.suffix)
			}
			return fmt.Sprintf("%.2f%s", scaled, unit.suffix)
		}
	}
	return fmt.Sprintf("%.0f", value)
}

func numericValue(value interface{}) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case int32:
		return float64(typed)
	case json.Number:
		resolved, _ := typed.Float64()
		return resolved
	default:
		resolved, _ := strconv.ParseFloat(strings.TrimSpace(stringValue(value)), 64)
		return resolved
	}
}

func nestedMapValue(source map[string]interface{}, key string, child string) interface{} {
	return nestedMapPathValue(source, key, child)
}

func nestedMapPathValue(source map[string]interface{}, path ...string) interface{} {
	if source == nil {
		return nil
	}
	current := interface{}(source)
	for _, key := range path {
		nested, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		raw, exists := nested[key]
		if !exists {
			return nil
		}
		current = raw
	}
	return current
}

func stringValue(value interface{}) string {
	if value == nil {
		return ""
	}
	if str, ok := value.(string); ok {
		return strings.TrimSpace(str)
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func stringSliceValue(value interface{}) []string {
	items, ok := value.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if str := stringValue(item); str != "" {
			result = append(result, str)
		}
	}
	return result
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func lastPathSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parts := strings.Split(value, "/")
	return strings.TrimSpace(parts[len(parts)-1])
}

func parseEndpointHostPort(endpoint string) (string, string) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", ""
	}
	if host, port, err := net.SplitHostPort(endpoint); err == nil {
		return host, port
	}
	return endpoint, ""
}

func resourcePrimaryBindingPurpose(resourceType models.ResourceType) string {
	if resourceType == models.ResourceTypeK8sCluster {
		return "cluster_auth"
	}
	return "ssh_auth"
}

func buildResourceValidationTaskPayload(resourceType models.ResourceType, endpoint, effectiveEndpoint string, credential models.Credential) resourceValidationTaskPayload {
	normalizedEndpoint := strings.TrimSpace(endpoint)
	normalizedEffective := strings.TrimSpace(effectiveEndpoint)
	hashInput := fmt.Sprintf("%s|%s|%d|%d", resourceType, normalizedEffective, credential.ID, credential.UpdatedAt.UnixNano())
	hash := sha256.Sum256([]byte(hashInput))
	return resourceValidationTaskPayload{
		Verification: resourceValidationSnapshot{
			Kind:                "resource_connection_validation",
			ResourceType:        resourceType,
			Endpoint:            normalizedEndpoint,
			EffectiveEndpoint:   normalizedEffective,
			CredentialID:        credential.ID,
			CredentialUpdatedAt: credential.UpdatedAt.UnixNano(),
			DraftHash:           hex.EncodeToString(hash[:]),
		},
	}
}

func parseResourceValidationTaskPayload(raw string) (resourceValidationTaskPayload, error) {
	var payload resourceValidationTaskPayload
	if strings.TrimSpace(raw) == "" {
		return payload, fmt.Errorf("验证任务缺少草稿快照")
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return payload, fmt.Errorf("验证任务快照无效")
	}
	if payload.Verification.Kind != "resource_connection_validation" {
		return payload, fmt.Errorf("验证任务类型无效")
	}
	return payload, nil
}

func parseResourceValidationConsumeResult(raw string) resourceValidationConsumeResult {
	var result resourceValidationConsumeResult
	if strings.TrimSpace(raw) == "" {
		return result
	}
	_ = json.Unmarshal([]byte(raw), &result)
	return result
}

func buildResourceResponse(resource models.Resource) gin.H {
	raw, _ := json.Marshal(resource)
	resp := gin.H{}
	_ = json.Unmarshal(raw, &resp)
	resp["labels"] = decodeJSONObjectField(resource.Labels, map[string]interface{}{})
	resp["metadata"] = decodeJSONObjectField(resource.Metadata, map[string]interface{}{})
	resp["base_info"] = decodeJSONObjectField(resource.BaseInfo, nil)
	return resp
}

func buildResourceListResponse(resources []models.Resource) []gin.H {
	items := make([]gin.H, 0, len(resources))
	for _, resource := range resources {
		items = append(items, buildResourceResponse(resource))
	}
	return items
}

func decodeJSONObjectField(raw string, fallback interface{}) interface{} {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback
	}
	var value interface{}
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return fallback
	}
	return value
}

func buildResourceBaseInfoTaskPayload(resource *models.Resource, credential models.Credential, effectiveEndpoint, collectorSource string) resourceBaseInfoTaskPayload {
	return resourceBaseInfoTaskPayload{
		Collection: resourceBaseInfoCollectionSnapshot{
			Kind:              "resource_base_info_refresh",
			ResourceID:        resource.ID,
			ResourceType:      resource.Type,
			Endpoint:          strings.TrimSpace(resource.Endpoint),
			EffectiveEndpoint: strings.TrimSpace(effectiveEndpoint),
			CredentialID:      credential.ID,
			CollectorSource:   collectorSource,
		},
	}
}

func parseResourceBaseInfoTaskPayload(raw string) (resourceBaseInfoTaskPayload, error) {
	var payload resourceBaseInfoTaskPayload
	if strings.TrimSpace(raw) == "" {
		return payload, fmt.Errorf("基础信息采集任务缺少上下文")
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return payload, fmt.Errorf("基础信息采集任务上下文无效")
	}
	if payload.Collection.Kind != "resource_base_info_refresh" {
		return payload, fmt.Errorf("基础信息采集任务类型无效")
	}
	return payload, nil
}

func preferredResourceCredentialBinding(resourceType models.ResourceType, bindings []models.ResourceCredentialBinding) *models.ResourceCredentialBinding {
	preferredPurpose := resourcePrimaryBindingPurpose(resourceType)
	for i := range bindings {
		if bindings[i].Purpose == preferredPurpose {
			return &bindings[i]
		}
	}
	for i := range bindings {
		if bindings[i].Purpose == "primary" {
			return &bindings[i]
		}
	}
	if len(bindings) == 0 {
		return nil
	}
	return &bindings[0]
}

func buildVMBaseInfoCollectionScript() string {
	return `set -e
OS_NAME="$( ( . /etc/os-release 2>/dev/null && printf '%s' "${PRETTY_NAME:-${NAME:-linux}}" ) || printf 'linux')"
OS_VERSION="$( ( . /etc/os-release 2>/dev/null && printf '%s' "${VERSION_ID:-}" ) || true)"
CPU_MODEL="$(awk -F: '/model name/ {sub(/^[ \t]+/, "", $2); print $2; exit}' /proc/cpuinfo 2>/dev/null || true)"
CPU_CORES="$(nproc 2>/dev/null || getconf _NPROCESSORS_ONLN 2>/dev/null || printf '0')"
MEMORY_TOTAL="$(awk '/MemTotal/ {print $2 * 1024}' /proc/meminfo 2>/dev/null || printf '0')"
ROOT_TOTAL="$(df -B1 / 2>/dev/null | awk 'NR==2 {print $2}' || printf '0')"
DISK_TOTAL="$(lsblk -b -dn -o SIZE 2>/dev/null | awk '{sum += $1} END {if (sum > 0) print sum; else print 0}')"
if [ "$DISK_TOTAL" = "0" ] || [ -z "$DISK_TOTAL" ]; then
  DISK_TOTAL="$ROOT_TOTAL"
fi
GPU_COUNT="$(if command -v nvidia-smi >/dev/null 2>&1; then nvidia-smi -L 2>/dev/null | wc -l | tr -d ' '; else printf '0'; fi)"
printf '%s\n' 'EASYDO_BASE_INFO_BEGIN'
printf 'EASYDO_HOSTNAME=%s\n' "$(hostname 2>/dev/null || true)"
printf 'EASYDO_PRIMARY_IPV4=%s\n' "$(hostname -I 2>/dev/null | awk '{print $1}' || true)"
printf 'EASYDO_OS_NAME=%s\n' "$OS_NAME"
printf 'EASYDO_OS_VERSION=%s\n' "$OS_VERSION"
printf 'EASYDO_KERNEL_VERSION=%s\n' "$(uname -r 2>/dev/null || true)"
printf 'EASYDO_ARCH=%s\n' "$(uname -m 2>/dev/null || true)"
printf 'EASYDO_CPU_MODEL=%s\n' "$CPU_MODEL"
printf 'EASYDO_CPU_LOGICAL_CORES=%s\n' "$CPU_CORES"
printf 'EASYDO_MEMORY_TOTAL_BYTES=%s\n' "$MEMORY_TOTAL"
printf 'EASYDO_ROOT_TOTAL_BYTES=%s\n' "$ROOT_TOTAL"
printf 'EASYDO_TOTAL_DISK_BYTES=%s\n' "$DISK_TOTAL"
printf 'EASYDO_GPU_COUNT=%s\n' "$GPU_COUNT"
printf '%s\n' 'EASYDO_DISK_ROWS_BEGIN'
lsblk -b -P -o NAME,SIZE,TYPE,FSTYPE,MOUNTPOINT 2>/dev/null || true
printf '%s\n' 'EASYDO_DISK_ROWS_END'
printf '%s\n' 'EASYDO_GPU_CSV_BEGIN'
if command -v nvidia-smi >/dev/null 2>&1; then
  nvidia-smi --query-gpu=index,name,memory.total --format=csv,noheader,nounits 2>/dev/null || true
fi
printf '%s\n' 'EASYDO_GPU_CSV_END'
printf '%s\n' 'EASYDO_BASE_INFO_END'`
}

func buildK8sBaseInfoCollectionCommand() string {
	return "printf '%s\\n' 'EASYDO_K8S_VERSION_BEGIN'; kubectl version -o json; printf '%s\\n' 'EASYDO_K8S_VERSION_END'; printf '%s\\n' 'EASYDO_K8S_NODES_BEGIN'; kubectl get nodes -o json; printf '%s\\n' 'EASYDO_K8S_NODES_END'"
}

func (h *ResourceHandler) createResourceBaseInfoTask(workspaceID, userID uint64, role string, resource *models.Resource) (*models.AgentTask, error) {
	if resource == nil {
		return nil, fmt.Errorf("资源不存在")
	}
	var bindings []models.ResourceCredentialBinding
	if err := h.DB.Where("workspace_id = ? AND resource_id = ?", workspaceID, resource.ID).Order("created_at ASC, id ASC").Find(&bindings).Error; err != nil {
		return nil, fmt.Errorf("加载资源凭据绑定失败")
	}
	binding := preferredResourceCredentialBinding(resource.Type, bindings)
	if binding == nil || binding.CredentialID == 0 {
		return nil, fmt.Errorf("资源尚未绑定可用凭据")
	}
	var credential models.Credential
	if err := h.DB.First(&credential, binding.CredentialID).Error; err != nil {
		return nil, fmt.Errorf("资源绑定凭据不存在")
	}
	if credential.WorkspaceID != workspaceID || !canReadCredential(h.DB, &credential, userID, role) {
		return nil, fmt.Errorf("无权访问资源绑定凭据")
	}
	if err := validateResourceBindingCredential(resource, &credential); err != nil {
		return nil, err
	}
	decrypted, err := services.NewCredentialEncryptionService().DecryptCredentialData(credential.EncryptedPayload)
	if err != nil {
		return nil, fmt.Errorf("连接凭据解密失败: %w", err)
	}
	effectiveEndpoint := effectiveResourceValidationEndpoint(resource.Type, resource.Endpoint, decrypted)
	agent, err := selectAvailableWorkspaceAgent(h.DB, workspaceID)
	if err != nil {
		return nil, err
	}

	taskType := "ssh"
	slotName := "ssh_auth"
	nodeConfig := map[string]interface{}{
		"credentials": map[string]interface{}{
			slotName: map[string]interface{}{"credential_id": credential.ID},
		},
	}
	collectorSource := "remote_task"
	if resource.Type == models.ResourceTypeVM {
		host, port := parseEndpointHostPort(effectiveEndpoint)
		nodeConfig["host"] = host
		nodeConfig["script"] = buildVMBaseInfoCollectionScript()
		if parsedPort, err := strconv.Atoi(strings.TrimSpace(port)); err == nil && parsedPort > 0 {
			nodeConfig["port"] = parsedPort
		}
	} else {
		taskType = "kubernetes"
		slotName = "cluster_auth"
		collectorSource = "k8s_api"
		nodeConfig = map[string]interface{}{
			"command": buildK8sBaseInfoCollectionCommand(),
			"credentials": map[string]interface{}{
				slotName: map[string]interface{}{"credential_id": credential.ID},
			},
		}
	}

	envMap, err := buildResourceValidationEnv(taskType, slotName, credential, decrypted)
	if err != nil {
		return nil, fmt.Errorf("连接凭据不完整: %w", err)
	}
	if resource.Type == models.ResourceTypeK8sCluster && effectiveEndpoint != "" {
		prefix := slotEnvPrefix(slotName)
		envMap[prefix+"SERVER"] = effectiveEndpoint
		envMap[prefix+"API_SERVER"] = effectiveEndpoint
	}
	nodeConfig["env"] = envMap

	canonicalType, script, err := renderPipelineAgentScript(taskType, nodeConfig)
	if err != nil {
		return nil, fmt.Errorf("构建基础信息采集任务失败: %w", err)
	}
	payload := buildResourceBaseInfoTaskPayload(resource, credential, effectiveEndpoint, collectorSource)
	payload.TaskType = canonicalType
	payload.NodeConfig = nodeConfig
	rawParams, _ := json.Marshal(payload)
	rawEnv, _ := json.Marshal(envMap)
	task := &models.AgentTask{
		WorkspaceID: workspaceID,
		AgentID:     agent.ID,
		NodeID:      fmt.Sprintf("resource-base-info-%d", time.Now().UnixNano()),
		TaskType:    canonicalType,
		Name:        "采集资源基础信息",
		Params:      string(rawParams),
		Script:      script,
		EnvVars:     string(rawEnv),
		Status:      models.TaskStatusQueued,
		Priority:    90,
		Timeout:     180,
		MaxRetries:  0,
		CreatedBy:   userID,
	}
	if err := h.DB.Omit("PipelineRunID").Create(task).Error; err != nil {
		return nil, fmt.Errorf("创建基础信息采集任务失败: %w", err)
	}
	updates := map[string]interface{}{
		"base_info_status":     "pending",
		"base_info_last_error": "",
		"base_info_source":     collectorSource,
	}
	_ = h.DB.Model(&models.Resource{}).Where("id = ?", resource.ID).Updates(updates).Error
	_ = h.DB.Model(&models.Agent{}).Where("id = ?", agent.ID).Update("status", models.AgentStatusBusy).Error
	_ = SharedWebSocketHandler().sendTaskAssign(*task)
	return task, nil
}

func buildResourceBaseInfoJSON(task *models.AgentTask, result map[string]interface{}) (string, string, int64, error) {
	if task == nil {
		return "", "", 0, fmt.Errorf("任务不存在")
	}
	payload, err := parseResourceBaseInfoTaskPayload(task.Params)
	if err != nil {
		return "", "", 0, err
	}
	stdout, _ := result["stdout"].(string)
	stdout = strings.TrimSpace(stdout)
	if stdout == "" {
		return "", payload.Collection.CollectorSource, 0, fmt.Errorf("采集结果为空")
	}
	if payload.Collection.ResourceType == models.ResourceTypeK8sCluster {
		return parseK8sBaseInfoOutput(stdout, payload.Collection.CollectorSource)
	}
	return parseVMBaseInfoOutput(stdout, payload.Collection.CollectorSource)
}

func parseVMBaseInfoOutput(stdout, source string) (string, string, int64, error) {
	sections := parseMarkedCollectorOutput(stdout)
	if !sections.hasMainBlock {
		return "", source, 0, fmt.Errorf("基础资源采集结果缺少主标记")
	}
	collectedAt := time.Now().Unix()
	disks := parseVMBaseInfoDisks(sections.diskRows)
	gpus := parseVMBaseInfoGPUDevices(sections.gpuRows)
	payload := map[string]interface{}{
		"schemaVersion": 1,
		"status":        "success",
		"source":        defaultIfEmpty(source, "remote_task"),
		"collectedAt":   collectedAt,
		"machine": map[string]interface{}{
			"hostname":    sections.scalars["EASYDO_HOSTNAME"],
			"primaryIpv4": sections.scalars["EASYDO_PRIMARY_IPV4"],
			"os": map[string]interface{}{
				"name":    sections.scalars["EASYDO_OS_NAME"],
				"version": sections.scalars["EASYDO_OS_VERSION"],
			},
			"kernelVersion": sections.scalars["EASYDO_KERNEL_VERSION"],
			"arch":          sections.scalars["EASYDO_ARCH"],
			"cpu": map[string]interface{}{
				"model":        sections.scalars["EASYDO_CPU_MODEL"],
				"logicalCores": parseIntValue(sections.scalars["EASYDO_CPU_LOGICAL_CORES"]),
			},
			"memory": map[string]interface{}{
				"totalBytes": parseInt64Value(sections.scalars["EASYDO_MEMORY_TOTAL_BYTES"]),
			},
			"storage": map[string]interface{}{
				"rootTotalBytes": parseInt64Value(sections.scalars["EASYDO_ROOT_TOTAL_BYTES"]),
				"totalDiskBytes": parseInt64Value(sections.scalars["EASYDO_TOTAL_DISK_BYTES"]),
				"disks":          disks,
			},
			"gpu": map[string]interface{}{
				"count":   parseIntValue(sections.scalars["EASYDO_GPU_COUNT"]),
				"devices": gpus,
			},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", source, 0, err
	}
	return string(data), defaultIfEmpty(source, "remote_task"), collectedAt, nil
}

func parseK8sBaseInfoOutput(stdout, source string) (string, string, int64, error) {
	versionRaw := extractMarkedSection(stdout, "EASYDO_K8S_VERSION_BEGIN", "EASYDO_K8S_VERSION_END")
	nodesRaw := extractMarkedSection(stdout, "EASYDO_K8S_NODES_BEGIN", "EASYDO_K8S_NODES_END")
	if versionRaw == "" || nodesRaw == "" {
		return "", source, 0, fmt.Errorf("K8s 采集结果缺少必要数据")
	}
	var versionDoc map[string]interface{}
	if err := json.Unmarshal([]byte(versionRaw), &versionDoc); err != nil {
		return "", source, 0, fmt.Errorf("K8s version 数据无效")
	}
	var nodesDoc map[string]interface{}
	if err := json.Unmarshal([]byte(nodesRaw), &nodesDoc); err != nil {
		return "", source, 0, fmt.Errorf("K8s nodes 数据无效")
	}
	items, _ := nodesDoc["items"].([]interface{})
	nodes := make([]map[string]interface{}, 0, len(items))
	summary := map[string]interface{}{
		"nodeCount":              len(items),
		"cpuCapacityMilli":       int64(0),
		"cpuAllocatableMilli":    int64(0),
		"memoryCapacityBytes":    int64(0),
		"memoryAllocatableBytes": int64(0),
		"podAllocatable":         int64(0),
		"gpuAllocatable":         int64(0),
	}
	for _, item := range items {
		node, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		metadata, _ := node["metadata"].(map[string]interface{})
		status, _ := node["status"].(map[string]interface{})
		nodeInfo, _ := status["nodeInfo"].(map[string]interface{})
		capacity, _ := status["capacity"].(map[string]interface{})
		allocatable, _ := status["allocatable"].(map[string]interface{})
		cpuCapacity := parseK8sCPUMilli(capacity["cpu"])
		cpuAllocatable := parseK8sCPUMilli(allocatable["cpu"])
		memoryCapacity := parseK8sBytes(capacity["memory"])
		memoryAllocatable := parseK8sBytes(allocatable["memory"])
		podAllocatable := parseK8sInteger(allocatable["pods"])
		gpuAllocatable := parseK8sGPUResourceCount(allocatable)
		summary["cpuCapacityMilli"] = summary["cpuCapacityMilli"].(int64) + cpuCapacity
		summary["cpuAllocatableMilli"] = summary["cpuAllocatableMilli"].(int64) + cpuAllocatable
		summary["memoryCapacityBytes"] = summary["memoryCapacityBytes"].(int64) + memoryCapacity
		summary["memoryAllocatableBytes"] = summary["memoryAllocatableBytes"].(int64) + memoryAllocatable
		summary["podAllocatable"] = summary["podAllocatable"].(int64) + podAllocatable
		summary["gpuAllocatable"] = summary["gpuAllocatable"].(int64) + gpuAllocatable
		nodes = append(nodes, map[string]interface{}{
			"name":                   stringValue(metadata["name"]),
			"roles":                  extractK8sNodeRoles(metadata),
			"arch":                   stringValue(nodeInfo["architecture"]),
			"osImage":                stringValue(nodeInfo["osImage"]),
			"kubeletVersion":         stringValue(nodeInfo["kubeletVersion"]),
			"cpuAllocatableMilli":    cpuAllocatable,
			"memoryAllocatableBytes": memoryAllocatable,
			"podAllocatable":         podAllocatable,
			"gpuAllocatable":         gpuAllocatable,
		})
	}
	collectedAt := time.Now().Unix()
	payload := map[string]interface{}{
		"schemaVersion": 1,
		"status":        "success",
		"source":        defaultIfEmpty(source, "k8s_api"),
		"collectedAt":   collectedAt,
		"k8s": map[string]interface{}{
			"cluster": map[string]interface{}{
				"serverVersion": nestedMapValue(versionDoc, "serverVersion", "gitVersion"),
			},
			"summary": summary,
			"nodes":   nodes,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", source, 0, err
	}
	return string(data), defaultIfEmpty(source, "k8s_api"), collectedAt, nil
}

type collectorOutputSections struct {
	hasMainBlock bool
	scalars      map[string]string
	diskRows     []string
	gpuRows      []string
}

func parseMarkedCollectorOutput(stdout string) collectorOutputSections {
	sections := collectorOutputSections{scalars: map[string]string{}}
	mode := ""
	inMainBlock := false
	for _, rawLine := range strings.Split(stdout, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		switch line {
		case "EASYDO_BASE_INFO_BEGIN":
			sections.hasMainBlock = true
			inMainBlock = true
			continue
		case "EASYDO_BASE_INFO_END":
			inMainBlock = false
			mode = ""
			continue
		case "EASYDO_DISK_ROWS_BEGIN":
			mode = "disk"
			continue
		case "EASYDO_DISK_ROWS_END":
			mode = ""
			continue
		case "EASYDO_GPU_CSV_BEGIN":
			mode = "gpu"
			continue
		case "EASYDO_GPU_CSV_END":
			mode = ""
			continue
		}
		if !inMainBlock {
			continue
		}
		switch mode {
		case "disk":
			sections.diskRows = append(sections.diskRows, line)
		case "gpu":
			sections.gpuRows = append(sections.gpuRows, line)
		default:
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				sections.scalars[parts[0]] = parts[1]
			}
		}
	}
	return sections
}

func extractMarkedSection(stdout, begin, end string) string {
	lines := strings.Split(stdout, "\n")
	collecting := false
	collected := make([]string, 0, len(lines))
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == begin {
			collecting = true
			continue
		}
		if line == end {
			break
		}
		if collecting {
			collected = append(collected, rawLine)
		}
	}
	return strings.TrimSpace(strings.Join(collected, "\n"))
}

func parseVMBaseInfoDisks(rows []string) []map[string]interface{} {
	disks := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		fields := strings.Fields(row)
		disk := map[string]interface{}{}
		for _, field := range fields {
			parts := strings.SplitN(field, "=", 2)
			if len(parts) != 2 {
				continue
			}
			value := strings.Trim(parts[1], `"`)
			switch parts[0] {
			case "NAME":
				disk["name"] = value
			case "SIZE":
				disk["totalBytes"] = parseInt64Value(value)
			case "TYPE":
				disk["type"] = value
			case "FSTYPE":
				disk["fsType"] = value
			case "MOUNTPOINT":
				disk["mountpoint"] = value
			}
		}
		if len(disk) > 0 {
			disks = append(disks, disk)
		}
	}
	return disks
}

func parseVMBaseInfoGPUDevices(rows []string) []map[string]interface{} {
	devices := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		parts := strings.Split(row, ",")
		if len(parts) < 3 {
			continue
		}
		devices = append(devices, map[string]interface{}{
			"index":       parseIntValue(strings.TrimSpace(parts[0])),
			"vendor":      "nvidia",
			"model":       strings.TrimSpace(parts[1]),
			"memoryBytes": parseInt64Value(strings.TrimSpace(parts[2])) * 1024 * 1024,
		})
	}
	return devices
}

func parseIntValue(raw string) int {
	value, _ := strconv.Atoi(strings.TrimSpace(raw))
	return value
}

func parseInt64Value(raw string) int64 {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0
	}
	if value, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return value
	}
	if value, err := strconv.ParseFloat(trimmed, 64); err == nil {
		return int64(value)
	}
	return 0
}

func extractK8sNodeRoles(metadata map[string]interface{}) []string {
	labels, _ := metadata["labels"].(map[string]interface{})
	roles := make([]string, 0, len(labels))
	for key, value := range labels {
		if !strings.HasPrefix(key, "node-role.kubernetes.io/") {
			continue
		}
		role := strings.TrimPrefix(key, "node-role.kubernetes.io/")
		if role == "" {
			role = stringValue(value)
		}
		if role == "" {
			role = "worker"
		}
		roles = append(roles, role)
	}
	if len(roles) == 0 {
		roles = append(roles, "worker")
	}
	return roles
}

func parseK8sCPUMilli(raw interface{}) int64 {
	text := strings.TrimSpace(convertToString(raw))
	if text == "" {
		return 0
	}
	if strings.HasSuffix(text, "m") {
		return parseInt64Value(strings.TrimSuffix(text, "m"))
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0
	}
	return int64(value * 1000)
}

func parseK8sBytes(raw interface{}) int64 {
	text := strings.TrimSpace(convertToString(raw))
	if text == "" {
		return 0
	}
	units := map[string]int64{
		"Ki": 1024,
		"Mi": 1024 * 1024,
		"Gi": 1024 * 1024 * 1024,
		"Ti": 1024 * 1024 * 1024 * 1024,
		"Pi": 1024 * 1024 * 1024 * 1024 * 1024,
		"Ei": 1024 * 1024 * 1024 * 1024 * 1024 * 1024,
		"K":  1000,
		"M":  1000 * 1000,
		"G":  1000 * 1000 * 1000,
		"T":  1000 * 1000 * 1000 * 1000,
	}
	for suffix, factor := range units {
		if strings.HasSuffix(text, suffix) {
			value, err := strconv.ParseFloat(strings.TrimSuffix(text, suffix), 64)
			if err != nil {
				return 0
			}
			return int64(value * float64(factor))
		}
	}
	return parseInt64Value(text)
}

func parseK8sInteger(raw interface{}) int64 {
	return parseInt64Value(convertToString(raw))
}

func parseK8sGPUResourceCount(resources map[string]interface{}) int64 {
	var total int64
	for key, value := range resources {
		if strings.Contains(strings.ToLower(key), "gpu") {
			total += parseK8sInteger(value)
		}
	}
	return total
}

func selectAvailableWorkspaceAgent(db *gorm.DB, workspaceID uint64) (*models.Agent, error) {
	var agents []models.Agent
	if err := db.Model(&models.Agent{}).
		Where("status IN ?", []string{models.AgentStatusOnline, models.AgentStatusBusy}).
		Where("registration_status = ?", models.AgentRegistrationStatusApproved).
		Where("scope_type = ? OR (scope_type = ? AND workspace_id = ?)", models.AgentScopePlatform, models.AgentScopeWorkspace, workspaceID).
		Find(&agents).Error; err != nil {
		return nil, err
	}
	if len(agents) == 0 {
		return nil, fmt.Errorf("没有可用的执行器")
	}
	bestAgent := &agents[0]
	minRuns := int64(1<<62 - 1)
	hasCandidate := false
	for _, agent := range agents {
		runningRuns := countAgentRunningPipelines(db, agent.ID)
		maxConcurrent := normalizeAgentMaxConcurrentPipelines(agent.MaxConcurrentPipelines)
		if runningRuns >= int64(maxConcurrent) {
			continue
		}
		if !hasCandidate || runningRuns < minRuns {
			bestAgent = &agent
			minRuns = runningRuns
			hasCandidate = true
		}
	}
	if !hasCandidate {
		return nil, fmt.Errorf("所有在线执行器已达到并发上限")
	}
	return bestAgent, nil
}

func buildResourceValidationEnv(taskType, slotName string, credential models.Credential, decrypted map[string]interface{}) (map[string]interface{}, error) {
	_, def, ok := getPipelineTaskDefinition(taskType)
	if !ok {
		return nil, fmt.Errorf("资源验证任务类型无效")
	}
	slot, ok := def.findCredentialSlot(slotName)
	if !ok {
		return nil, fmt.Errorf("资源验证凭据槽位无效")
	}
	if err := validateTaskCredentialPayload(taskType, slot, credential, decrypted); err != nil {
		return nil, err
	}
	envMap := make(map[string]interface{})
	prefix := slotEnvPrefix(slot.Slot)
	credentialJSON, _ := json.Marshal(decrypted)
	envMap[prefix+"ID"] = strconv.FormatUint(credential.ID, 10)
	envMap[prefix+"TYPE"] = string(credential.Type)
	envMap[prefix+"CATEGORY"] = string(credential.Category)
	envMap[prefix+"JSON"] = string(credentialJSON)
	for k, v := range decrypted {
		envKey := sanitizeEnvKey(k)
		if envKey == "" {
			continue
		}
		envMap[prefix+envKey] = convertToString(v)
	}
	return envMap, nil
}

func effectiveResourceValidationEndpoint(resourceType models.ResourceType, endpoint string, decrypted map[string]interface{}) string {
	if resourceType == models.ResourceTypeK8sCluster {
		if server := strings.TrimSpace(resolveResourceCredentialServer(decrypted)); server != "" {
			if strings.TrimSpace(endpoint) == "" {
				return server
			}
		}
	}
	return strings.TrimSpace(endpoint)
}

func resolveResourceCredentialServer(payload map[string]interface{}) string {
	return strings.TrimSpace(pickCredentialSecretValue(payload, "server", "api_server"))
}

func (h *ResourceHandler) createResourceValidationTask(workspaceID, userID uint64, role string, req verifyResourceConnectionRequest) (*models.AgentTask, error) {
	if req.CredentialID == 0 {
		return nil, fmt.Errorf("请选择连接凭据")
	}
	if req.Type != models.ResourceTypeVM && req.Type != models.ResourceTypeK8sCluster {
		return nil, fmt.Errorf("资源类型无效")
	}

	var credential models.Credential
	if err := h.DB.First(&credential, req.CredentialID).Error; err != nil {
		return nil, fmt.Errorf("连接凭据不存在")
	}
	if credential.WorkspaceID != workspaceID || !canReadCredential(h.DB, &credential, userID, role) {
		return nil, fmt.Errorf("无权访问连接凭据")
	}

	resourcePreview := &models.Resource{WorkspaceID: workspaceID, Type: req.Type, Endpoint: strings.TrimSpace(req.Endpoint)}
	if err := validateResourceBindingCredential(resourcePreview, &credential); err != nil {
		return nil, err
	}

	decrypted, err := services.NewCredentialEncryptionService().DecryptCredentialData(credential.EncryptedPayload)
	if err != nil {
		return nil, fmt.Errorf("连接凭据解密失败: %w", err)
	}
	effectiveEndpoint := effectiveResourceValidationEndpoint(req.Type, req.Endpoint, decrypted)
	if req.Type == models.ResourceTypeVM && effectiveEndpoint == "" {
		return nil, fmt.Errorf("VM 接入地址不能为空")
	}

	agent, err := selectAvailableWorkspaceAgent(h.DB, workspaceID)
	if err != nil {
		return nil, err
	}

	taskType := "ssh"
	slotName := "ssh_auth"
	nodeConfig := map[string]interface{}{
		"credentials": map[string]interface{}{
			slotName: map[string]interface{}{"credential_id": credential.ID},
		},
	}
	if req.Type == models.ResourceTypeVM {
		host, port := parseEndpointHostPort(effectiveEndpoint)
		nodeConfig["host"] = host
		nodeConfig["script"] = "true"
		if parsedPort, err := strconv.Atoi(strings.TrimSpace(port)); err == nil && parsedPort > 0 {
			nodeConfig["port"] = parsedPort
		}
	} else {
		taskType = "kubernetes"
		slotName = "cluster_auth"
		nodeConfig = map[string]interface{}{
			"command": "kubectl cluster-info >/dev/null",
			"credentials": map[string]interface{}{
				slotName: map[string]interface{}{"credential_id": credential.ID},
			},
		}
	}

	envMap, err := buildResourceValidationEnv(taskType, slotName, credential, decrypted)
	if err != nil {
		return nil, fmt.Errorf("连接凭据不完整: %w", err)
	}
	if req.Type == models.ResourceTypeK8sCluster && effectiveEndpoint != "" {
		prefix := slotEnvPrefix(slotName)
		envMap[prefix+"SERVER"] = effectiveEndpoint
		envMap[prefix+"API_SERVER"] = effectiveEndpoint
	}
	nodeConfig["env"] = envMap

	canonicalType, script, err := renderPipelineAgentScript(taskType, nodeConfig)
	if err != nil {
		return nil, fmt.Errorf("构建验证任务失败: %w", err)
	}

	validationPayload := buildResourceValidationTaskPayload(req.Type, req.Endpoint, effectiveEndpoint, credential)
	validationPayload.TaskType = canonicalType
	validationPayload.NodeConfig = nodeConfig
	rawParams, _ := json.Marshal(validationPayload)
	rawEnv, _ := json.Marshal(envMap)
	expectedMaxRetries := 0
	task := &models.AgentTask{
		WorkspaceID: workspaceID,
		AgentID:     agent.ID,
		NodeID:      fmt.Sprintf("resource-verify-%d", time.Now().UnixNano()),
		TaskType:    canonicalType,
		Name:        "验证资源连接",
		Params:      string(rawParams),
		Script:      script,
		EnvVars:     string(rawEnv),
		Status:      models.TaskStatusQueued,
		Priority:    100,
		Timeout:     120,
		MaxRetries:  expectedMaxRetries,
		CreatedBy:   userID,
	}
	if err := h.DB.Omit("PipelineRunID").Create(task).Error; err != nil {
		return nil, fmt.Errorf("创建验证任务失败: %w", err)
	}
	if expectedMaxRetries == 0 {
		if err := h.DB.Model(task).Update("max_retries", 0).Error; err != nil {
			return nil, fmt.Errorf("初始化验证任务重试策略失败: %w", err)
		}
		task.MaxRetries = 0
	}
	_ = h.DB.Model(&models.Agent{}).Where("id = ?", agent.ID).Update("status", models.AgentStatusBusy).Error
	_ = SharedWebSocketHandler().sendTaskAssign(*task)
	return task, nil
}

func (h *ResourceHandler) authorizeResourceCreationValidation(db *gorm.DB, workspaceID, userID uint64, role string, req *createResourceRequest) (*models.Credential, *models.AgentTask, resourceValidationTaskPayload, error) {
	var empty resourceValidationTaskPayload
	var credential models.Credential
	if err := db.First(&credential, req.CredentialID).Error; err != nil {
		return nil, nil, empty, fmt.Errorf("连接凭据不存在")
	}
	if credential.WorkspaceID != workspaceID || !canReadCredential(db, &credential, userID, role) {
		return nil, nil, empty, fmt.Errorf("无权访问连接凭据")
	}
	resourcePreview := &models.Resource{WorkspaceID: workspaceID, Type: req.Type, Endpoint: strings.TrimSpace(req.Endpoint)}
	if err := validateResourceBindingCredential(resourcePreview, &credential); err != nil {
		return nil, nil, empty, err
	}
	decrypted, err := services.NewCredentialEncryptionService().DecryptCredentialData(credential.EncryptedPayload)
	if err != nil {
		return nil, nil, empty, fmt.Errorf("连接凭据解密失败: %w", err)
	}

	var task models.AgentTask
	if err := db.Where("workspace_id = ?", workspaceID).First(&task, req.VerificationTaskID).Error; err != nil {
		return nil, nil, empty, fmt.Errorf("连接验证任务不存在")
	}
	if task.CreatedBy != userID {
		return nil, nil, empty, fmt.Errorf("无权使用该连接验证结果")
	}
	if task.Status != models.TaskStatusExecuteSuccess {
		return nil, nil, empty, fmt.Errorf("连接验证尚未成功，请先完成连接验证")
	}
	if task.EndTime <= 0 || time.Now().Unix()-task.EndTime > 600 {
		return nil, nil, empty, fmt.Errorf("连接验证结果已过期，请重新验证")
	}
	consume := parseResourceValidationConsumeResult(task.ResultData)
	if consume.Verification.ConsumedAt > 0 {
		return nil, nil, empty, fmt.Errorf("连接验证结果已使用，请重新验证")
	}

	validation, err := parseResourceValidationTaskPayload(task.Params)
	if err != nil {
		return nil, nil, empty, err
	}
	current := buildResourceValidationTaskPayload(req.Type, req.Endpoint, effectiveResourceValidationEndpoint(req.Type, req.Endpoint, decrypted), credential)
	if validation.Verification.DraftHash != current.Verification.DraftHash {
		return nil, nil, empty, fmt.Errorf("连接验证结果已失效，请重新验证")
	}
	if validation.Verification.CredentialID != credential.ID || validation.Verification.CredentialUpdatedAt != credential.UpdatedAt.UnixNano() {
		return nil, nil, empty, fmt.Errorf("连接验证结果已失效，请重新验证")
	}
	if validation.Verification.ResourceType != req.Type {
		return nil, nil, empty, fmt.Errorf("连接验证结果与当前资源类型不一致")
	}
	return &credential, &task, validation, nil
}

func deploymentHasAgentNode(config PipelineConfig) bool {
	for _, node := range config.Nodes {
		if isAgentPipelineTaskType(node.Type) {
			return true
		}
	}
	return false
}

func validateResourceBindingCredential(resource *models.Resource, credential *models.Credential) error {
	if resource == nil || credential == nil {
		return fmt.Errorf("资源或凭据不存在")
	}
	if !credential.IsUsable() {
		return fmt.Errorf("绑定凭据不是可用状态")
	}

	var taskType string
	var slotName string
	switch resource.Type {
	case models.ResourceTypeVM:
		taskType = "ssh"
		slotName = "ssh_auth"
	case models.ResourceTypeK8sCluster:
		taskType = "kubernetes"
		slotName = "cluster_auth"
	default:
		return fmt.Errorf("资源类型不支持绑定凭据")
	}

	_, def, ok := getPipelineTaskDefinition(taskType)
	if !ok {
		return fmt.Errorf("资源绑定任务类型无效")
	}
	slot, ok := def.findCredentialSlot(slotName)
	if !ok {
		return fmt.Errorf("资源绑定凭据槽位无效")
	}
	if !slot.allowsType(credential.Type) {
		return fmt.Errorf("资源类型 %s 不支持凭据类型 %s", resource.Type, credential.Type)
	}
	if !slot.allowsCategory(credential.Category) {
		return fmt.Errorf("资源类型 %s 不支持凭据分类 %s", resource.Type, credential.Category)
	}

	decrypted, err := services.NewCredentialEncryptionService().DecryptCredentialData(credential.EncryptedPayload)
	if err != nil {
		return fmt.Errorf("资源绑定凭据解密失败: %w", err)
	}
	return validateTaskCredentialPayload(taskType, slot, *credential, decrypted)
}

func deploymentResourceCredentialID(resourceType models.ResourceType, bindings map[string]uint64) uint64 {
	switch resourceType {
	case models.ResourceTypeVM:
		if id := bindings["ssh_auth"]; id > 0 {
			return id
		}
	case models.ResourceTypeK8sCluster:
		if id := bindings["cluster_auth"]; id > 0 {
			return id
		}
	}
	return bindings["primary"]
}

func (h *DeploymentHandler) applyResourceCredentialBindings(config *PipelineConfig, resource *models.Resource) error {
	if h == nil || h.DB == nil || config == nil || resource == nil {
		return nil
	}

	var bindings []models.ResourceCredentialBinding
	if err := h.DB.Where("workspace_id = ? AND resource_id = ?", resource.WorkspaceID, resource.ID).Order("created_at ASC, id ASC").Find(&bindings).Error; err != nil {
		return err
	}
	if len(bindings) == 0 {
		return nil
	}

	bindingByPurpose := make(map[string]uint64, len(bindings))
	for _, binding := range bindings {
		if binding.CredentialID == 0 {
			continue
		}
		if _, exists := bindingByPurpose[binding.Purpose]; !exists {
			bindingByPurpose[binding.Purpose] = binding.CredentialID
		}
	}
	resourceCredentialID := deploymentResourceCredentialID(resource.Type, bindingByPurpose)
	if resourceCredentialID == 0 {
		return nil
	}

	for i := range config.Nodes {
		node := &config.Nodes[i]
		canonical, _, ok := getPipelineTaskDefinition(node.Type)
		if !ok {
			continue
		}
		nodeCfg := normalizePipelineNodeConfig(node.Type, canonical, node.getNodeConfig())
		var slotName string
		switch {
		case canonical == "kubernetes" && resource.Type == models.ResourceTypeK8sCluster:
			slotName = "cluster_auth"
		case (canonical == "ssh" || canonical == "docker-run") && resource.Type == models.ResourceTypeVM:
			slotName = "ssh_auth"
		default:
			node.Config = nodeCfg
			node.Params = nil
			node.Type = canonical
			continue
		}
		credentials, _ := nodeCfg["credentials"].(map[string]interface{})
		if credentials == nil {
			credentials = make(map[string]interface{})
			nodeCfg["credentials"] = credentials
		}
		credentials[slotName] = map[string]interface{}{"credential_id": resourceCredentialID}
		node.Config = nodeCfg
		node.Params = nil
		node.Type = canonical
	}

	return nil
}

func normalizeTaskTypeForConfig(taskType string) string {
	canonical, _, ok := getPipelineTaskDefinition(taskType)
	if ok {
		return canonical
	}
	return taskType
}

func validateTemplatePipelineCompatibility(resourceType models.ResourceType, rawConfig string) (bool, string) {
	var config PipelineConfig
	if err := json.Unmarshal([]byte(rawConfig), &config); err != nil {
		return false, "流水线配置解析失败"
	}
	hasSSH := false
	hasK8s := false
	hasDockerRun := false
	for _, node := range config.Nodes {
		canonical, _, ok := getPipelineTaskDefinition(node.Type)
		if !ok {
			continue
		}
		if canonical == "ssh" {
			hasSSH = true
		}
		if canonical == "kubernetes" {
			hasK8s = true
		}
		if canonical == "docker-run" {
			hasDockerRun = true
		}
	}
	switch resourceType {
	case models.ResourceTypeVM:
		if !hasSSH && !hasDockerRun {
			return false, "VM 模板绑定的流水线必须包含 SSH 或 Docker 运行部署节点"
		}
	case models.ResourceTypeK8sCluster:
		if !hasK8s {
			return false, "K8s 模板绑定的流水线必须包含 Kubernetes 部署节点"
		}
	}
	return true, ""
}

func defaultIfEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func optionalUint64(value uint64) *uint64 {
	if value == 0 {
		return nil
	}
	copyValue := value
	return &copyValue
}
