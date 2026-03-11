package handlers

import (
	"net/http"
	"strconv"
	"time"

	"easydo-server/internal/models"
	"easydo-server/internal/services"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CredentialHandler - 凭据管理处理器
// 提供凭据的 CRUD 操作和生命周期管理
type CredentialHandler struct {
	encryptionService *services.CredentialEncryptionService
}

// NewCredentialHandler - 创建凭据处理器实例
func NewCredentialHandler() *CredentialHandler {
	return &CredentialHandler{
		encryptionService: services.NewCredentialEncryptionService(),
	}
}

// CreateCredentialRequest - 创建凭据的请求结构
type CreateCredentialRequest struct {
	Name         string                    `json:"name" binding:"required,min=1,max=128"`
	Description  string                    `json:"description" binding:"max=512"`
	Type         models.CredentialType     `json:"type" binding:"required"`
	Category     models.CredentialCategory `json:"category"`
	SecretData   map[string]interface{}    `json:"secret_data" binding:"required"`
	Scope        models.CredentialScope    `json:"scope"`
	ProjectID    uint64                    `json:"project_id"`
	ExpiresAt    *int64                    `json:"expires_at"`
	AutoRotate   bool                      `json:"auto_rotate"`
	RotatePeriod int                       `json:"rotate_period"`
	Metadata     string                    `json:"metadata"`
}

// UpdateCredentialRequest - 更新凭据的请求结构
type UpdateCredentialRequest struct {
	Name         string                    `json:"name" binding:"min=1,max=128"`
	Description  string                    `json:"description" binding:"max=512"`
	Category     models.CredentialCategory `json:"category"`
	SecretData   map[string]interface{}    `json:"secret_data"`
	Status       models.CredentialStatus   `json:"status"`
	ExpiresAt    *int64                    `json:"expires_at"`
	AutoRotate   bool                      `json:"auto_rotate"`
	RotatePeriod int                       `json:"rotate_period"`
	Metadata     string                    `json:"metadata"`
}

// RotateCredentialRequest - 轮换凭据请求结构
type RotateCredentialRequest struct {
	SecretData map[string]interface{} `json:"secret_data" binding:"required"`
	Reason     string                 `json:"reason"`
}

type CredentialImpactReference struct {
	PipelineID     uint64 `json:"pipeline_id"`
	PipelineName   string `json:"pipeline_name"`
	NodeID         string `json:"node_id"`
	TaskType       string `json:"task_type"`
	CredentialSlot string `json:"credential_slot"`
	UpdatedAt      int64  `json:"updated_at"`
}

type CredentialImpactSummary struct {
	CredentialID   uint64                      `json:"credential_id"`
	CredentialName string                      `json:"credential_name"`
	ReferenceCount int64                       `json:"reference_count"`
	PipelineCount  int64                       `json:"pipeline_count"`
	References     []CredentialImpactReference `json:"references"`
}

// CreateCredential - 创建新凭据
// @Summary 创建凭据
// @Description 创建一个新的凭据，敏感数据会被加密存储
// @Tags credentials
// @Accept json
// @Produce json
// @Param request body CreateCredentialRequest true "凭据信息"
// @Success 200 {object} Response{data=models.CredentialResponse}
// @Failure 400 {object} Response
// @Failure 500 {object} Response
// @Router /api/v1/credentials [post]
func (h *CredentialHandler) CreateCredential(c *gin.Context) {
	var req CreateCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid request: " + err.Error(),
		})
		return
	}

	// 验证凭据类型
	if !models.IsValidType(req.Type) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid credential type: " + string(req.Type),
		})
		return
	}

	// 标准化类型（中文转英文，确保存储一致性）
	req.Type = models.NormalizeType(req.Type)

	// 验证过期时间
	if req.ExpiresAt != nil && *req.ExpiresAt > 0 {
		if *req.ExpiresAt <= time.Now().Unix() {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "Expiration time must be in the future",
			})
			return
		}
	}

	ownerID, role := getRequestUser(c)
	workspaceID, _ := getRequestWorkspace(c)
	if !userCanWriteWorkspaceResource(models.DB, workspaceID, ownerID, role) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "Access denied",
		})
		return
	}

	// 加密凭据数据
	encryptedData, iv, err := h.encryptionService.EncryptCredentialData(req.SecretData)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to encrypt credential data",
		})
		return
	}

	credential := models.Credential{
		Name:           req.Name,
		Description:    req.Description,
		Type:           req.Type,
		Category:       req.Category,
		EncryptedData:  encryptedData,
		EncryptionIV:   iv,
		EncryptionAlgo: "aes-256-gcm",
		Metadata:       req.Metadata,
		Scope:          req.Scope,
		WorkspaceID:    workspaceID,
		ProjectID:      req.ProjectID,
		OwnerID:        ownerID,
		Status:         models.CredentialStatusActive,
		ExpiresAt:      req.ExpiresAt,
		AutoRotate:     req.AutoRotate,
		RotatePeriod:   req.RotatePeriod,
	}

	if credential.Scope == "" {
		credential.Scope = models.ScopeUser
	}
	if credential.Scope == models.ScopeProject {
		if credential.ProjectID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "Project scope requires project_id",
			})
			return
		}
		if !projectBelongsToWorkspace(models.DB, credential.ProjectID, workspaceID) {
			c.JSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "Project does not belong to active workspace",
			})
			return
		}
	}
	if credential.Scope == models.ScopeGlobal && !isAdminRole(role) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "Only admin can create global credentials",
		})
		return
	}

	if err := models.DB.Create(&credential).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to create credential: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": credential.ToResponse(),
	})
}

// ListCredentials - 获取凭据列表
// @Summary 获取凭据列表
// @Description 分页获取凭据列表，支持类型、分类、状态筛选
// @Tags credentials
// @Produce json
// @Param page query int false "页码" default(1)
// @Param size query int false "每页数量" default(10)
// @Param type query string false "凭据类型筛选"
// @Param category query string false "分类筛选"
// @Param scope query string false "范围筛选"
// @Param status query string false "状态筛选"
// @Param keyword query string false "关键词搜索"
// @Success 200 {object} Response{data=ListResponse}
// @Router /api/v1/credentials [get]
func (h *CredentialHandler) ListCredentials(c *gin.Context) {
	ownerID, role := getRequestUser(c)
	workspaceID, _ := getRequestWorkspace(c)

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "10"))
	credentialType := c.Query("type")
	category := c.Query("category")
	scope := c.Query("scope")
	status := c.Query("status")
	keyword := c.Query("keyword")

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 10
	}

	query := applyCredentialReadScope(models.DB.Model(&models.Credential{}), ownerID, role).Where("workspace_id = ?", workspaceID)

	if credentialType != "" {
		if !models.IsValidType(models.CredentialType(credentialType)) {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "Invalid credential type",
			})
			return
		}
		// 标准化类型（中文转英文，确保查询匹配存储的英文值）
		normalizedType := models.NormalizeType(models.CredentialType(credentialType))
		query = query.Where("type = ?", normalizedType)
	}
	if category != "" {
		query = query.Where("category = ?", category)
	}
	if scope != "" {
		query = query.Where("scope = ?", scope)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if keyword != "" {
		query = query.Where("name LIKE ? OR description LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}

	var total int64
	query.Count(&total)

	var credentials []models.Credential
	offset := (page - 1) * size
	query.Order("created_at DESC").Offset(offset).Limit(size).Find(&credentials)

	// 转换为响应结构
	responses := make([]models.CredentialResponse, len(credentials))
	for i, cred := range credentials {
		responses[i] = cred.ToResponse()
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"list":  responses,
			"total": total,
			"page":  page,
			"size":  size,
		},
	})
}

// GetCredential - 获取单个凭据详情
// @Summary 获取凭据详情
// @Description 获取凭据的详细信息（不包含敏感数据）
// @Tags credentials
// @Produce json
// @Param id path int true "凭据ID"
// @Success 200 {object} Response{data=models.CredentialResponse}
// @Failure 404 {object} Response
// @Router /api/v1/credentials/{id} [get]
func (h *CredentialHandler) GetCredential(c *gin.Context) {
	ownerID, role := getRequestUser(c)
	workspaceID, _ := getRequestWorkspace(c)

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid credential ID",
		})
		return
	}

	var credential models.Credential
	if err := models.DB.First(&credential, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Credential not found",
		})
		return
	}

	if credential.WorkspaceID != workspaceID || !canReadCredential(models.DB, &credential, ownerID, role) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "Access denied",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": credential.ToResponse(),
	})
}

// GetCredentialSecretData - 获取凭据敏感数据（编辑回填）
// @Summary 获取凭据敏感数据
// @Description 仅用于有权限用户在编辑场景回填 secret_data
// @Tags credentials
// @Produce json
// @Param id path int true "凭据ID"
// @Success 200 {object} Response
// @Failure 404 {object} Response
// @Router /api/v1/credentials/{id}/secret-data [get]
func (h *CredentialHandler) GetCredentialSecretData(c *gin.Context) {
	ownerID, role := getRequestUser(c)
	workspaceID, _ := getRequestWorkspace(c)

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid credential ID",
		})
		return
	}

	var credential models.Credential
	if err := models.DB.First(&credential, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Credential not found",
		})
		return
	}

	if credential.WorkspaceID != workspaceID || !canReadCredentialValue(models.DB, &credential, ownerID, role) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "Access denied",
		})
		return
	}

	secretData, err := h.encryptionService.DecryptCredentialData(credential.EncryptedData, credential.EncryptionIV)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to decrypt credential data",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"id":          credential.ID,
			"secret_data": secretData,
		},
	})
}

// UpdateCredential - 更新凭据
// @Summary 更新凭据
// @Description 更新凭据的元数据或重新加密敏感数据
// @Tags credentials
// @Accept json
// @Produce json
// @Param id path int true "凭据ID"
// @Param request body UpdateCredentialRequest true "更新信息"
// @Success 200 {object} Response{data=models.CredentialResponse}
// @Failure 400 {object} Response
// @Failure 404 {object} Response
// @Router /api/v1/credentials/{id} [put]
func (h *CredentialHandler) UpdateCredential(c *gin.Context) {
	ownerID, role := getRequestUser(c)
	workspaceID, _ := getRequestWorkspace(c)

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid credential ID",
		})
		return
	}

	var credential models.Credential
	if err := models.DB.First(&credential, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Credential not found",
		})
		return
	}

	if credential.WorkspaceID != workspaceID || !canWriteCredential(models.DB, &credential, ownerID, role) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "Access denied",
		})
		return
	}

	var req UpdateCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid request: " + err.Error(),
		})
		return
	}

	// 验证过期时间
	if req.ExpiresAt != nil && *req.ExpiresAt > 0 {
		if *req.ExpiresAt <= time.Now().Unix() {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "Expiration time must be in the future",
			})
			return
		}
	}

	updates := make(map[string]interface{})

	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Description != "" {
		updates["description"] = req.Description
	}
	if req.Category != "" {
		updates["category"] = req.Category
	}
	if req.Status != "" {
		updates["status"] = req.Status
	}
	if req.ExpiresAt != nil {
		updates["expires_at"] = req.ExpiresAt
	}
	if req.Metadata != "" {
		updates["metadata"] = req.Metadata
	}
	updates["auto_rotate"] = req.AutoRotate
	updates["rotate_period"] = req.RotatePeriod
	updates["version"] = credential.Version + 1

	if req.SecretData != nil && len(req.SecretData) > 0 {
		encryptedData, iv, err := h.encryptionService.EncryptCredentialData(req.SecretData)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "Failed to encrypt credential data",
			})
			return
		}
		updates["encrypted_data"] = encryptedData
		updates["encryption_iv"] = iv
	}

	if err := models.DB.Model(&credential).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to update credential: " + err.Error(),
		})
		return
	}

	// 重新加载凭据
	models.DB.First(&credential, id)

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": credential.ToResponse(),
	})
}

// DeleteCredential - 删除凭据
// @Summary 删除凭据
// @Description 永久删除凭据及其所有关联数据
// @Tags credentials
// @Produce json
// @Param id path int true "凭据ID"
// @Success 200 {object} Response
// @Failure 404 {object} Response
// @Router /api/v1/credentials/{id} [delete]
func (h *CredentialHandler) DeleteCredential(c *gin.Context) {
	ownerID, role := getRequestUser(c)
	workspaceID, _ := getRequestWorkspace(c)

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid credential ID",
		})
		return
	}

	var credential models.Credential
	if err := models.DB.First(&credential, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Credential not found",
		})
		return
	}

	if credential.WorkspaceID != workspaceID || !canWriteCredential(models.DB, &credential, ownerID, role) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "Access denied",
		})
		return
	}

	models.DB.Where("credential_id = ?", credential.ID).Delete(&models.PipelineCredentialRef{})

	if err := models.DB.Delete(&credential).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to delete credential: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "Credential deleted successfully",
	})
}

func buildCredentialImpactSummary(db *gorm.DB, credential models.Credential) (CredentialImpactSummary, error) {
	refs := make([]CredentialImpactReference, 0)

	type refRow struct {
		PipelineID     uint64 `gorm:"column:pipeline_id"`
		PipelineName   string `gorm:"column:pipeline_name"`
		NodeID         string `gorm:"column:node_id"`
		TaskType       string `gorm:"column:task_type"`
		CredentialSlot string `gorm:"column:credential_slot"`
		UpdatedAt      int64  `gorm:"column:updated_at"`
	}
	rows := make([]refRow, 0)
	if err := db.Table("pipeline_credential_refs AS r").
		Select("r.pipeline_id, p.name AS pipeline_name, r.node_id, r.task_type, r.credential_slot, CAST(UNIX_TIMESTAMP(r.updated_at) AS SIGNED) AS updated_at").
		Joins("LEFT JOIN pipelines p ON p.id = r.pipeline_id").
		Where("r.credential_id = ?", credential.ID).
		Order("r.updated_at DESC").
		Scan(&rows).Error; err != nil {
		return CredentialImpactSummary{}, err
	}

	uniquePipelines := make(map[uint64]struct{})
	for _, row := range rows {
		refs = append(refs, CredentialImpactReference{
			PipelineID:     row.PipelineID,
			PipelineName:   row.PipelineName,
			NodeID:         row.NodeID,
			TaskType:       row.TaskType,
			CredentialSlot: row.CredentialSlot,
			UpdatedAt:      row.UpdatedAt,
		})
		uniquePipelines[row.PipelineID] = struct{}{}
	}

	return CredentialImpactSummary{
		CredentialID:   credential.ID,
		CredentialName: credential.Name,
		ReferenceCount: int64(len(refs)),
		PipelineCount:  int64(len(uniquePipelines)),
		References:     refs,
	}, nil
}

func (h *CredentialHandler) GetCredentialImpact(c *gin.Context) {
	ownerID, role := getRequestUser(c)
	workspaceID, _ := getRequestWorkspace(c)

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid credential ID",
		})
		return
	}

	var credential models.Credential
	if err := models.DB.First(&credential, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Credential not found",
		})
		return
	}

	if credential.WorkspaceID != workspaceID || !canReadCredential(models.DB, &credential, ownerID, role) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "Access denied",
		})
		return
	}

	summary, err := buildCredentialImpactSummary(models.DB, credential)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to query credential impact: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": summary,
	})
}

type CredentialImpactBatchRequest struct {
	IDs []uint64 `json:"ids" binding:"required,min=1"`
}

func (h *CredentialHandler) BatchCredentialImpact(c *gin.Context) {
	ownerID, role := getRequestUser(c)
	workspaceID, _ := getRequestWorkspace(c)

	var req CredentialImpactBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid request: " + err.Error(),
		})
		return
	}
	if len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "IDs cannot be empty",
		})
		return
	}

	var credentials []models.Credential
	if err := applyCredentialReadScope(models.DB.Model(&models.Credential{}), ownerID, role).
		Where("workspace_id = ?", workspaceID).
		Where("id IN ?", req.IDs).
		Find(&credentials).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to query credentials: " + err.Error(),
		})
		return
	}

	summaries := make([]CredentialImpactSummary, 0, len(credentials))
	var totalReferences int64
	for _, credential := range credentials {
		summary, err := buildCredentialImpactSummary(models.DB, credential)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "Failed to query credential impact: " + err.Error(),
			})
			return
		}
		totalReferences += summary.ReferenceCount
		summaries = append(summaries, summary)
	}

	impactedCredentials := 0
	for _, summary := range summaries {
		if summary.ReferenceCount > 0 {
			impactedCredentials++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"total_credentials":    len(summaries),
			"impacted_credentials": impactedCredentials,
			"total_references":     totalReferences,
			"items":                summaries,
		},
	})
}

// VerifyCredential - 验证凭据有效性
// @Summary 验证凭据
// @Description 验证凭据是否可以正常解密（不实际使用凭据）
// @Tags credentials
// @Produce json
// @Param id path int true "凭据ID"
// @Success 200 {object} Response{data=VerifyResponse}
// @Failure 404 {object} Response
// @Router /api/v1/credentials/{id}/verify [post]
func (h *CredentialHandler) VerifyCredential(c *gin.Context) {
	ownerID, role := getRequestUser(c)
	workspaceID, _ := getRequestWorkspace(c)

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid credential ID",
		})
		return
	}

	var credential models.Credential
	if err := models.DB.First(&credential, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Credential not found",
		})
		return
	}
	if credential.WorkspaceID != workspaceID || !canReadCredential(models.DB, &credential, ownerID, role) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "Access denied",
		})
		return
	}

	// 尝试解密
	_, err = h.encryptionService.DecryptCredentialData(credential.EncryptedData, credential.EncryptionIV)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 200,
			"data": gin.H{
				"valid":   false,
				"message": "Failed to decrypt credential data",
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"valid":   true,
			"message": "Credential is valid",
		},
	})
}

// VerifyResponse - 验证响应结构
type VerifyResponse struct {
	Valid   bool   `json:"valid"`
	Message string `json:"message"`
}

// GetCredentialTypes - 获取所有凭据类型
// @Summary 获取凭据类型列表
// @Description 返回系统支持的所有凭据类型及其详细信息
// @Tags credentials
// @Produce json
// @Success 200 {object} Response{data=[]models.TypeInfo}
// @Router /api/v1/credentials/types [get]
func (h *CredentialHandler) GetCredentialTypes(c *gin.Context) {
	type typeDef struct {
		Value models.CredentialType `json:"value"`
		Label string                `json:"label"`
		models.TypeInfo
	}

	types := []typeDef{
		{Value: models.TypePassword, Label: models.TypePassword.GetTypeLabel(), TypeInfo: models.TypePassword.GetTypeInfo()},
		{Value: models.TypeSSHKey, Label: models.TypeSSHKey.GetTypeLabel(), TypeInfo: models.TypeSSHKey.GetTypeInfo()},
		{Value: models.TypeToken, Label: models.TypeToken.GetTypeLabel(), TypeInfo: models.TypeToken.GetTypeInfo()},
		{Value: models.TypeOAuth2, Label: models.TypeOAuth2.GetTypeLabel(), TypeInfo: models.TypeOAuth2.GetTypeInfo()},
		{Value: models.TypeCert, Label: models.TypeCert.GetTypeLabel(), TypeInfo: models.TypeCert.GetTypeInfo()},
		{Value: models.TypePasskey, Label: models.TypePasskey.GetTypeLabel(), TypeInfo: models.TypePasskey.GetTypeInfo()},
		{Value: models.TypeMFA, Label: models.TypeMFA.GetTypeLabel(), TypeInfo: models.TypeMFA.GetTypeInfo()},
		{Value: models.TypeIAM, Label: models.TypeIAM.GetTypeLabel(), TypeInfo: models.TypeIAM.GetTypeInfo()},
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": types,
	})
}

// GetCredentialCategories - 获取所有凭据分类
// @Summary 获取凭据分类列表
// @Description 返回系统支持的所有凭据分类及其详细信息
// @Tags credentials
// @Produce json
// @Success 200 {object} Response{data=[]models.CategoryInfo}
// @Router /api/v1/credentials/categories [get]
func (h *CredentialHandler) GetCredentialCategories(c *gin.Context) {
	type categoryDef struct {
		Value models.CredentialCategory `json:"value"`
		Label string                    `json:"label"`
		models.CategoryInfo
	}

	categories := []categoryDef{
		{Value: models.CategoryGitHub, Label: models.CategoryGitHub.GetCategoryLabel(), CategoryInfo: models.CategoryGitHub.GetCategoryInfo()},
		{Value: models.CategoryGitLab, Label: models.CategoryGitLab.GetCategoryLabel(), CategoryInfo: models.CategoryGitLab.GetCategoryInfo()},
		{Value: models.CategoryGitee, Label: models.CategoryGitee.GetCategoryLabel(), CategoryInfo: models.CategoryGitee.GetCategoryInfo()},
		{Value: models.CategoryDocker, Label: models.CategoryDocker.GetCategoryLabel(), CategoryInfo: models.CategoryDocker.GetCategoryInfo()},
		{Value: models.CategoryKubernetes, Label: models.CategoryKubernetes.GetCategoryLabel(), CategoryInfo: models.CategoryKubernetes.GetCategoryInfo()},
		{Value: models.CategoryDingTalk, Label: models.CategoryDingTalk.GetCategoryLabel(), CategoryInfo: models.CategoryDingTalk.GetCategoryInfo()},
		{Value: models.CategoryWeChat, Label: models.CategoryWeChat.GetCategoryLabel(), CategoryInfo: models.CategoryWeChat.GetCategoryInfo()},
		{Value: models.CategoryEmail, Label: models.CategoryEmail.GetCategoryLabel(), CategoryInfo: models.CategoryEmail.GetCategoryInfo()},
		{Value: models.CategoryAWS, Label: models.CategoryAWS.GetCategoryLabel(), CategoryInfo: models.CategoryAWS.GetCategoryInfo()},
		{Value: models.CategoryGCP, Label: models.CategoryGCP.GetCategoryLabel(), CategoryInfo: models.CategoryGCP.GetCategoryInfo()},
		{Value: models.CategoryAzure, Label: models.CategoryAzure.GetCategoryLabel(), CategoryInfo: models.CategoryAzure.GetCategoryInfo()},
		{Value: models.CategoryCustom, Label: models.CategoryCustom.GetCategoryLabel(), CategoryInfo: models.CategoryCustom.GetCategoryInfo()},
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": categories,
	})
}

// RotateCredential - 轮换凭据
// @Summary 轮换凭据
// @Description 更新凭据的敏感数据并记录轮换历史
// @Tags credentials
// @Accept json
// @Produce json
// @Param id path int true "凭据ID"
// @Param request body RotateCredentialRequest true "新的凭据数据"
// @Success 200 {object} Response
// @Failure 400 {object} Response
// @Router /api/v1/credentials/{id}/rotate [post]
func (h *CredentialHandler) RotateCredential(c *gin.Context) {
	ownerID, role := getRequestUser(c)
	workspaceID, _ := getRequestWorkspace(c)

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid credential ID",
		})
		return
	}

	var credential models.Credential
	if err := models.DB.First(&credential, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Credential not found",
		})
		return
	}

	if credential.WorkspaceID != workspaceID || !canWriteCredential(models.DB, &credential, ownerID, role) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "Access denied",
		})
		return
	}

	var req RotateCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid request: " + err.Error(),
		})
		return
	}

	// 加密新的凭据数据
	encryptedData, iv, err := h.encryptionService.EncryptCredentialData(req.SecretData)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to encrypt credential data",
		})
		return
	}

	oldVersion := credential.Version

	updates := map[string]interface{}{
		"encrypted_data": encryptedData,
		"encryption_iv":  iv,
		"version":        credential.Version + 1,
		"status":         models.CredentialStatusActive,
	}

	if err := models.DB.Model(&credential).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to rotate credential: " + err.Error(),
		})
		return
	}

	// 记录轮换日志
	rotationLog := models.CredentialRotationLog{
		CredentialID: credential.ID,
		RotatedBy:    ownerID,
		OldVersion:   oldVersion,
		NewVersion:   credential.Version + 1,
		Reason:       req.Reason,
	}
	models.DB.Create(&rotationLog)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "Credential rotated successfully",
	})
}

// GetCredentialUsage - 获取凭据使用统计
// @Summary 获取凭据使用统计
// @Description 返回凭据的使用次数、最后使用时间等信息
// @Tags credentials
// @Produce json
// @Param id path int true "凭据ID"
// @Success 200 {object} Response{data=UsageStatsResponse}
// @Router /api/v1/credentials/{id}/usage [get]
func (h *CredentialHandler) GetCredentialUsage(c *gin.Context) {
	ownerID, role := getRequestUser(c)
	workspaceID, _ := getRequestWorkspace(c)

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid credential ID",
		})
		return
	}

	var credential models.Credential
	if err := models.DB.First(&credential, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Credential not found",
		})
		return
	}

	if credential.WorkspaceID != workspaceID || !canReadCredential(models.DB, &credential, ownerID, role) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "Access denied",
		})
		return
	}

	// 查询使用统计
	var usageCount int64
	var lastUsed int64
	var successCount int64
	var failedCount int64

	models.DB.Model(&models.CredentialUsage{}).
		Where("credential_id = ?", id).
		Count(&usageCount)

	models.DB.Model(&models.CredentialUsage{}).
		Where("credential_id = ?", id).
		Order("used_at DESC").
		Limit(1).
		Pluck("used_at", &lastUsed)

	models.DB.Model(&models.CredentialUsage{}).
		Where("credential_id = ? AND result = ?", id, "success").
		Count(&successCount)

	models.DB.Model(&models.CredentialUsage{}).
		Where("credential_id = ? AND result = ?", id, "failed").
		Count(&failedCount)

	response := gin.H{
		"used_count":    usageCount,
		"last_used_at":  lastUsed,
		"success_count": successCount,
		"failed_count":  failedCount,
		"success_rate":  0.0,
	}

	if usageCount > 0 {
		response["success_rate"] = float64(successCount) / float64(usageCount) * 100
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": response,
	})
}

// UsageStatsResponse - 使用统计响应
type UsageStatsResponse struct {
	UsedCount   int64   `json:"used_count"`
	LastUsedAt  int64   `json:"last_used_at"`
	SuccessRate float64 `json:"success_rate"`
}

// BatchVerifyCredentials - 批量验证凭据
// @Summary 批量验证凭据
// @Description 批量验证多个凭据的有效性
// @Tags credentials
// @Accept json
// @Produce json
// @Param request body BatchRequest true "凭据ID列表"
// @Success 200 {object} Response{data=BatchVerifyResponse}
// @Router /api/v1/credentials/batch/verify [post]
func (h *CredentialHandler) BatchVerifyCredentials(c *gin.Context) {
	ownerID, role := getRequestUser(c)
	workspaceID, _ := getRequestWorkspace(c)

	var req BatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid request: " + err.Error(),
		})
		return
	}

	if len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "IDs cannot be empty",
		})
		return
	}

	success := 0
	failed := 0
	results := make([]gin.H, 0, len(req.IDs))

	for _, id := range req.IDs {
		var credential models.Credential
		if err := models.DB.First(&credential, id).Error; err != nil {
			results = append(results, gin.H{
				"id":    id,
				"valid": false,
				"error": "Credential not found",
			})
			failed++
			continue
		}
		if credential.WorkspaceID != workspaceID || !canReadCredential(models.DB, &credential, ownerID, role) {
			results = append(results, gin.H{
				"id":    id,
				"valid": false,
				"error": "Access denied",
			})
			failed++
			continue
		}

		_, err := h.encryptionService.DecryptCredentialData(credential.EncryptedData, credential.EncryptionIV)
		if err != nil {
			results = append(results, gin.H{
				"id":    id,
				"valid": false,
				"error": "Failed to decrypt",
			})
			failed++
		} else {
			results = append(results, gin.H{
				"id":    id,
				"valid": true,
			})
			success++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"success": success,
			"failed":  failed,
			"total":   len(req.IDs),
			"results": results,
		},
	})
}

// BatchDeleteCredentials - 批量删除凭据
// @Summary 批量删除凭据
// @Description 批量删除多个凭据
// @Tags credentials
// @Accept json
// @Produce json
// @Param request body BatchRequest true "凭据ID列表"
// @Success 200 {object} Response{data=BatchDeleteResponse}
// @Router /api/v1/credentials/batch/delete [post]
func (h *CredentialHandler) BatchDeleteCredentials(c *gin.Context) {
	ownerID, role := getRequestUser(c)
	workspaceID, _ := getRequestWorkspace(c)

	var req BatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid request: " + err.Error(),
		})
		return
	}

	if len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "IDs cannot be empty",
		})
		return
	}

	var credentials []models.Credential
	if err := models.DB.Where("id IN ? AND workspace_id = ?", req.IDs, workspaceID).Find(&credentials).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to query credentials: " + err.Error(),
		})
		return
	}
	if len(credentials) != len(req.IDs) {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "Some credentials not found",
		})
		return
	}
	for i := range credentials {
		if credentials[i].WorkspaceID != workspaceID || !canWriteCredential(models.DB, &credentials[i], ownerID, role) {
			c.JSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "Some credentials are not writable",
			})
			return
		}
	}
	models.DB.Where("credential_id IN ?", req.IDs).Delete(&models.PipelineCredentialRef{})

	// 删除凭据
	if err := models.DB.Where("id IN ?", req.IDs).Delete(&models.Credential{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to delete credentials: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"deleted": len(req.IDs),
		},
		"message": "Credentials deleted successfully",
	})
}

// BatchRequest - 批量请求结构
type BatchRequest struct {
	IDs []uint64 `json:"ids" binding:"required,min=1"`
}

// BatchVerifyResponse - 批量验证响应
type BatchVerifyResponse struct {
	Success int     `json:"success"`
	Failed  int     `json:"failed"`
	Total   int     `json:"total"`
	Results []gin.H `json:"results"`
}

// BatchDeleteResponse - 批量删除响应
type BatchDeleteResponse struct {
	Deleted int    `json:"deleted"`
	Message string `json:"message"`
}

// ExportCredentials - 导出凭据
// @Summary 导出凭据
// @Description 导出凭据列表（不包含敏感数据）
// @Tags credentials
// @Produce json
// @Param type query string false "凭据类型筛选"
// @Param category query string false "分类筛选"
// @Success 200 {object} Response{data=[]models.CredentialResponse}
// @Router /api/v1/credentials/export [get]
func (h *CredentialHandler) ExportCredentials(c *gin.Context) {
	ownerID, role := getRequestUser(c)
	workspaceID, _ := getRequestWorkspace(c)

	credentialType := c.Query("type")
	category := c.Query("category")

	query := applyCredentialReadScope(models.DB.Model(&models.Credential{}), ownerID, role).Where("workspace_id = ?", workspaceID)

	if credentialType != "" {
		if !models.IsValidType(models.CredentialType(credentialType)) {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "Invalid credential type",
			})
			return
		}
		// 标准化类型（中文转英文，确保查询匹配存储的英文值）
		normalizedType := models.NormalizeType(models.CredentialType(credentialType))
		query = query.Where("type = ?", normalizedType)
	}
	if category != "" {
		query = query.Where("category = ?", category)
	}

	var credentials []models.Credential
	query.Order("created_at DESC").Find(&credentials)

	// 转换为导出格式（不包含敏感数据）
	responses := make([]gin.H, len(credentials))
	for i, cred := range credentials {
		responses[i] = gin.H{
			"name":        cred.Name,
			"type":        cred.Type,
			"category":    cred.Category,
			"description": cred.Description,
			"scope":       cred.Scope,
			"status":      cred.Status,
			"version":     cred.Version,
			"created_at":  cred.CreatedAt,
			"updated_at":  cred.UpdatedAt,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": responses,
	})
}
