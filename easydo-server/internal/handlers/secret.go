package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"easydo-server/internal/models"
	"easydo-server/internal/services"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type SecretHandler struct {
	DB                *gorm.DB
	EncryptionService services.EncryptionService
}

func NewSecretHandler() *SecretHandler {
	svc, _ := services.NewEncryptionService()
	return &SecretHandler{
		DB:                models.DB,
		EncryptionService: svc,
	}
}

type CreateSecretRequest struct {
	Name        string                 `json:"name" binding:"required,min=2,max=128"`
	Description string                 `json:"description"`
	Type        string                 `json:"type" binding:"required"`
	Category    string                 `json:"category"`
	Value       string                 `json:"value" binding:"required"`
	Scope       string                 `json:"scope"`
	ProjectID   uint64                 `json:"project_id"`
	Metadata    map[string]interface{} `json:"metadata"`
}

type UpdateSecretRequest struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Value       string                 `json:"value"`
	Scope       string                 `json:"scope"`
	ProjectID   uint64                 `json:"project_id"`
	Metadata    map[string]interface{} `json:"metadata"`
	Status      string                 `json:"status"`
}

type SecretResponse struct {
	ID          uint64                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Type        string                 `json:"type"`
	Category    string                 `json:"category"`
	Metadata    map[string]interface{} `json:"metadata"`
	Scope       string                 `json:"scope"`
	ProjectID   uint64                 `json:"project_id"`
	CreatedBy   uint64                 `json:"created_by"`
	LastUsedAt  int64                  `json:"last_used_at"`
	UsedCount   int64                  `json:"used_count"`
	Version     int                    `json:"version"`
	Status      string                 `json:"status"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

func (h *SecretHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	secretType := c.Query("type")
	category := c.Query("category")
	scope := c.Query("scope")
	search := c.Query("search")
	projectID := c.Query("project_id")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	query := h.DB.Model(&models.Secret{})

	if secretType != "" {
		query = query.Where("type = ?", secretType)
	}
	if category != "" {
		query = query.Where("category = ?", category)
	}
	if scope != "" {
		query = query.Where("scope = ?", scope)
	}
	if search != "" {
		query = query.Where("name LIKE ?", "%"+search+"%")
	}
	if projectID != "" {
		query = query.Where("project_id = ?", projectID)
	}

	var total int64
	query.Count(&total)

	var secrets []models.Secret
	offset := (page - 1) * pageSize
	query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&secrets)

	var response []SecretResponse
	for _, secret := range secrets {
		var metadata map[string]interface{}
		if secret.Metadata != "" {
			json.Unmarshal([]byte(secret.Metadata), &metadata)
		}
		response = append(response, SecretResponse{
			ID:          secret.ID,
			Name:        secret.Name,
			Description: secret.Description,
			Type:        string(secret.Type),
			Category:    string(secret.Category),
			Metadata:    metadata,
			Scope:       string(secret.Scope),
			ProjectID:   secret.ProjectID,
			CreatedBy:   secret.CreatedBy,
			LastUsedAt:  secret.LastUsedAt,
			UsedCount:   secret.UsedCount,
			Version:     secret.Version,
			Status:      string(secret.Status),
			CreatedAt:   secret.CreatedAt,
			UpdatedAt:   secret.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"list":  response,
			"total": total,
			"page":  page,
			"size":  pageSize,
		},
	})
}

func (h *SecretHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的密钥ID",
		})
		return
	}

	var secret models.Secret
	if err := h.DB.First(&secret, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "密钥不存在",
		})
		return
	}

	var metadata map[string]interface{}
	if secret.Metadata != "" {
		json.Unmarshal([]byte(secret.Metadata), &metadata)
	}

	response := SecretResponse{
		ID:          secret.ID,
		Name:        secret.Name,
		Description: secret.Description,
		Type:        string(secret.Type),
		Category:    string(secret.Category),
		Metadata:    metadata,
		Scope:       string(secret.Scope),
		ProjectID:   secret.ProjectID,
		CreatedBy:   secret.CreatedBy,
		LastUsedAt:  secret.LastUsedAt,
		UsedCount:   secret.UsedCount,
		Version:     secret.Version,
		Status:      string(secret.Status),
		CreatedAt:   secret.CreatedAt,
		UpdatedAt:   secret.UpdatedAt,
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": response,
	})
}

func (h *SecretHandler) Create(c *gin.Context) {
	var req CreateSecretRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数错误: " + err.Error(),
		})
		return
	}

	userID := c.GetUint64("user_id")

	var existingCount int64
	h.DB.Model(&models.Secret{}).Where("name = ? AND scope = ? AND project_id = ?", req.Name, req.Scope, req.ProjectID).Count(&existingCount)
	if existingCount > 0 {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": "密钥名称已存在",
		})
		return
	}

	encryptedValue, err := h.EncryptionService.Encrypt([]byte(req.Value))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "加密失败: " + err.Error(),
		})
		return
	}

	metadataJSON, _ := json.Marshal(req.Metadata)

	secret := models.Secret{
		Name:           req.Name,
		Description:    req.Description,
		Type:           models.SecretType(req.Type),
		Category:       models.SecretCategory(req.Category),
		EncryptedValue: services.Base64Encode(encryptedValue),
		EncryptionAlgo: "aes-256-gcm",
		Metadata:       string(metadataJSON),
		Scope:          models.SecretScope(req.Scope),
		ProjectID:      req.ProjectID,
		CreatedBy:      userID,
		Status:         models.SecretStatusActive,
	}

	if err := h.DB.Create(&secret).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建密钥失败: " + err.Error(),
		})
		return
	}

	h.logAudit(&secret, models.AuditActionCreate, userID, c.ClientIP(), c.GetHeader("User-Agent"), nil)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "密钥创建成功",
		"data": gin.H{
			"id": secret.ID,
		},
	})
}

func (h *SecretHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的密钥ID",
		})
		return
	}

	var req UpdateSecretRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数错误: " + err.Error(),
		})
		return
	}

	var secret models.Secret
	if err := h.DB.First(&secret, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "密钥不存在",
		})
		return
	}

	userID := c.GetUint64("user_id")

	updates := make(map[string]interface{})

	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Description != "" {
		updates["description"] = req.Description
	}
	if req.Scope != "" {
		updates["scope"] = req.Scope
		updates["project_id"] = req.ProjectID
	}
	if req.Status != "" {
		updates["status"] = req.Status
	}
	if req.Metadata != nil {
		metadataJSON, _ := json.Marshal(req.Metadata)
		updates["metadata"] = string(metadataJSON)
	}

	if req.Value != "" {
		encryptedValue, err := h.EncryptionService.Encrypt([]byte(req.Value))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "加密失败: " + err.Error(),
			})
			return
		}
		updates["encrypted_value"] = services.Base64Encode(encryptedValue)
		updates["version"] = secret.Version + 1
	}

	if err := h.DB.Model(&secret).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新密钥失败: " + err.Error(),
		})
		return
	}

	h.logAudit(&secret, models.AuditActionUpdate, userID, c.ClientIP(), c.GetHeader("User-Agent"), updates)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "密钥更新成功",
	})
}

func (h *SecretHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的密钥ID",
		})
		return
	}

	var secret models.Secret
	if err := h.DB.First(&secret, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "密钥不存在",
		})
		return
	}

	userID := c.GetUint64("user_id")

	if err := h.DB.Delete(&secret).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除密钥失败: " + err.Error(),
		})
		return
	}

	h.logAudit(&secret, models.AuditActionDelete, userID, c.ClientIP(), c.GetHeader("User-Agent"), nil)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "密钥删除成功",
	})
}

func (h *SecretHandler) GetValue(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的密钥ID",
		})
		return
	}

	var secret models.Secret
	if err := h.DB.First(&secret, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "密钥不存在",
		})
		return
	}

	if secret.Status != models.SecretStatusActive {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "密钥已禁用或过期",
		})
		return
	}

	encryptedValue, err := services.Base64Decode(secret.EncryptedValue)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "解码失败: " + err.Error(),
		})
		return
	}

	value, err := h.EncryptionService.Decrypt(encryptedValue)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "解密失败: " + err.Error(),
		})
		return
	}

	userID := c.GetUint64("user_id")
	now := time.Now().Unix()
	h.DB.Model(&secret).Updates(map[string]interface{}{
		"last_used_at": now,
		"used_count":   secret.UsedCount + 1,
	})

	h.logAudit(&secret, models.AuditActionUse, userID, c.ClientIP(), c.GetHeader("User-Agent"), nil)

	var metadata map[string]interface{}
	if secret.Metadata != "" {
		json.Unmarshal([]byte(secret.Metadata), &metadata)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"id":       secret.ID,
			"name":     secret.Name,
			"type":     secret.Type,
			"category": secret.Category,
			"value":    string(value),
			"metadata": metadata,
		},
	})
}

func (h *SecretHandler) GenerateSSHKey(c *gin.Context) {
	var req struct {
		Bits    int    `json:"bits"`
		Comment string `json:"comment"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数错误: " + err.Error(),
		})
		return
	}

	if req.Bits != 2048 && req.Bits != 4096 {
		req.Bits = 2048
	}

	privateKey, publicKey, err := h.EncryptionService.GenerateSSHKey(req.Bits, req.Comment)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "生成SSH密钥失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"private_key": privateKey,
			"public_key":  publicKey,
		},
	})
}

func (h *SecretHandler) GetTypes(c *gin.Context) {
	types := []gin.H{
		{"value": "ssh", "label": "SSH密钥", "categories": []string{"custom"}},
		{"value": "token", "label": "访问令牌", "categories": []string{"github", "gitlab", "gitee", "custom"}},
		{"value": "registry", "label": "镜像仓库凭证", "categories": []string{"docker", "custom"}},
		{"value": "api_key", "label": "API密钥", "categories": []string{"dingtalk", "wechat", "custom"}},
		{"value": "kubernetes", "label": "Kubernetes凭证", "categories": []string{"kubernetes", "custom"}},
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": types,
	})
}

func (h *SecretHandler) logAudit(secret *models.Secret, action string, actorID uint64, actorIP, actorUA string, metadata interface{}) {
	metadataJSON, _ := json.Marshal(metadata)
	auditLog := models.SecretAuditLog{
		SecretID: secret.ID,
		Action:   action,
		ActorID:  actorID,
		ActorIP:  actorIP,
		ActorUA:  actorUA,
		Metadata: string(metadataJSON),
	}
	h.DB.Create(&auditLog)
}

func (h *SecretHandler) BatchDelete(c *gin.Context) {
	var ids []uint64
	if err := c.ShouldBindJSON(&ids); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数错误: " + err.Error(),
		})
		return
	}

	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "未选择要删除的密钥",
		})
		return
	}

	result := h.DB.Where("id IN ?", ids).Delete(&models.Secret{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "批量删除失败: " + result.Error.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "批量删除成功",
		"data": gin.H{
			"deleted_count": result.RowsAffected,
		},
	})
}
