package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"easydo-server/internal/models"

	"github.com/gin-gonic/gin"
)

// SecretVerifier 密钥验证器接口
type SecretVerifier interface {
	Verify(value string) (bool, string, error)
	GetMetadata(value string) map[string]interface{}
}

// SSHVerifier SSH密钥验证器
type SSHVerifier struct{}

func (v *SSHVerifier) Verify(value string) (bool, string, error) {
	if strings.Contains(value, "BEGIN OPENSSH PRIVATE KEY") ||
		strings.Contains(value, "BEGIN RSA PRIVATE KEY") ||
		strings.Contains(value, "BEGIN DSA PRIVATE KEY") ||
		strings.Contains(value, "BEGIN EC PRIVATE KEY") {
		return true, "有效的SSH私钥格式", nil
	}
	return false, "非法的SSH私钥格式", nil
}

func (v *SSHVerifier) GetMetadata(value string) map[string]interface{} {
	metadata := make(map[string]interface{})

	if strings.Contains(value, "RSA") {
		metadata["key_type"] = "RSA"
	} else if strings.Contains(value, "DSA") {
		metadata["key_type"] = "DSA"
	} else if strings.Contains(value, "EC") {
		metadata["key_type"] = "EC"
	} else {
		metadata["key_type"] = "OpenSSH"
	}

	// 检测是否加密
	if strings.Contains(value, "ENCRYPTED") {
		metadata["encrypted"] = true
	} else {
		metadata["encrypted"] = false
	}

	return metadata
}

// TokenVerifier 令牌验证器
type TokenVerifier struct{}

func (v *TokenVerifier) Verify(value string) (bool, string, error) {
	// 基本格式检查：Token通常是一串随机字符
	if len(value) < 10 {
		return false, "Token长度不足", nil
	}

	// 检查是否包含特殊字符（通常是有效的token格式）
	if strings.ContainsAny(value, "_-") {
		return true, "有效的Token格式", nil
	}

	return true, "Token格式验证通过", nil
}

func (v *TokenVerifier) GetMetadata(value string) map[string]interface{} {
	metadata := make(map[string]interface{})
	metadata["length"] = len(value)
	metadata["has_underscore"] = strings.Contains(value, "_")
	metadata["has_dash"] = strings.Contains(value, "-")
	return metadata
}

// RegistryVerifier 镜像仓库验证器
type RegistryVerifier struct{}

func (v *RegistryVerifier) Verify(value string) (bool, string, error) {
	// Docker Registry凭证通常是 username:password 格式
	if strings.Contains(value, ":") {
		parts := strings.SplitN(value, ":", 2)
		if len(parts[0]) > 0 && len(parts[1]) > 0 {
			return true, "有效的Registry凭证格式", nil
		}
	}
	return false, "非法的Registry凭证格式", nil
}

func (v *RegistryVerifier) GetMetadata(value string) map[string]interface{} {
	metadata := make(map[string]interface{})
	if strings.Contains(value, ":") {
		parts := strings.SplitN(value, ":", 2)
		metadata["username"] = parts[0]
		metadata["has_password"] = len(parts) > 1 && len(parts[1]) > 0
	}
	return metadata
}

// APIKeyVerifier API密钥验证器
type APIKeyVerifier struct{}

func (v *APIKeyVerifier) Verify(value string) (bool, string, error) {
	// API Key通常是一串随机字符
	if len(value) < 8 {
		return false, "API Key长度不足", nil
	}
	return true, "有效的API Key格式", nil
}

func (v *APIKeyVerifier) GetMetadata(value string) map[string]interface{} {
	metadata := make(map[string]interface{})
	metadata["length"] = len(value)
	return metadata
}

// KubernetesVerifier Kubernetes验证器
type KubernetesVerifier struct{}

func (v *KubernetesVerifier) Verify(value string) (bool, string, error) {
	// Kubeconfig通常是YAML格式
	if strings.Contains(value, "apiVersion:") &&
		strings.Contains(value, "kind:") &&
		(strings.Contains(value, "clusters:") || strings.Contains(value, "users:")) {
		return true, "有效的Kubeconfig格式", nil
	}

	// Service Account Token通常是JWT格式
	if strings.HasPrefix(value, "eyJ") {
		return true, "有效的Service Account Token格式", nil
	}

	return false, "非法的Kubernetes凭证格式", nil
}

func (v *KubernetesVerifier) GetMetadata(value string) map[string]interface{} {
	metadata := make(map[string]interface{})

	if strings.Contains(value, "apiVersion:") {
		metadata["type"] = "kubeconfig"
	} else if strings.HasPrefix(value, "eyJ") {
		metadata["type"] = "service_account_token"
	}

	return metadata
}

// 验证器工厂
func getVerifier(secretType string) SecretVerifier {
	switch models.SecretType(secretType) {
	case models.SecretTypeSSH:
		return &SSHVerifier{}
	case models.SecretTypeToken:
		return &TokenVerifier{}
	case models.SecretTypeRegistry:
		return &RegistryVerifier{}
	case models.SecretTypeAPIKey:
		return &APIKeyVerifier{}
	case models.SecretTypeKubernetes:
		return &KubernetesVerifier{}
	default:
		return &APIKeyVerifier{}
	}
}

// VerifySecret 验证密钥
func (h *SecretHandler) Verify(c *gin.Context) {
	userID, role := getRequestUser(c)

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
	if !canReadSecret(h.DB, &secret, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "无权限验证该密钥",
		})
		return
	}

	// 获取密钥值
	encryptedValue, err := models.DecryptSecret(secret.EncryptedValue)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "解密失败: " + err.Error(),
		})
		return
	}

	// 使用对应的验证器
	verifier := getVerifier(string(secret.Type))
	valid, message, verr := verifier.Verify(string(encryptedValue))

	// 获取额外元数据
	metadata := verifier.GetMetadata(string(encryptedValue))

	// 记录验证结果
	h.logAudit(&secret, models.AuditActionVerify, userID, c.ClientIP(), c.GetHeader("User-Agent"), map[string]interface{}{
		"valid":    valid,
		"message":  message,
		"metadata": metadata,
	})

	response := gin.H{
		"code":     200,
		"valid":    valid,
		"message":  message,
		"metadata": metadata,
	}

	if verr != nil {
		response["error"] = verr.Error()
	}

	c.JSON(http.StatusOK, response)
}

// RotateSecret 轮换密钥
func (h *SecretHandler) Rotate(c *gin.Context) {
	userID, role := getRequestUser(c)

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的密钥ID",
		})
		return
	}

	var req struct {
		NewValue   string `json:"new_value"`
		Regenerate bool   `json:"regenerate"`
	}

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
	if !canWriteSecret(h.DB, &secret, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "无权限轮换该密钥",
		})
		return
	}

	var newValue string

	if req.Regenerate {
		// 根据类型生成新的密钥
		switch secret.Type {
		case models.SecretTypeSSH:
			_, publicKey, _ := models.GenerateSSHKey(2048, secret.Name)
			// 生成新的私钥
			privateKey, _, _ := models.GenerateSSHKey(2048, secret.Name)
			newValue = privateKey
			req.NewValue = newValue

			// 更新metadata中的公钥
			var metadata map[string]interface{}
			if secret.Metadata != "" {
				json.Unmarshal([]byte(secret.Metadata), &metadata)
			}
			if metadata == nil {
				metadata = make(map[string]interface{})
			}
			metadata["public_key"] = publicKey
			metadataJSON, _ := json.Marshal(metadata)
			h.DB.Model(&secret).Update("metadata", string(metadataJSON))

		default:
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "该类型不支持自动生成",
			})
			return
		}
	} else if req.NewValue == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请提供新的密钥值或选择自动生成",
		})
		return
	} else {
		newValue = req.NewValue
	}

	// 加密新值
	encryptedValue, err := models.EncryptSecret(newValue)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "加密失败: " + err.Error(),
		})
		return
	}

	// 保存旧值到轮换历史
	rotationHistory := map[string]interface{}{
		"old_value":  secret.EncryptedValue,
		"rotated_at": time.Now().Unix(),
		"rotated_by": userID,
	}

	// 更新密钥
	updates := map[string]interface{}{
		"encrypted_value": encryptedValue,
		"version":         secret.Version + 1,
		"status":          models.SecretStatusActive,
		"last_used_at":    0,
	}

	if err := h.DB.Model(&secret).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "轮换失败: " + err.Error(),
		})
		return
	}

	// 记录轮换历史
	var rotation models.SecretRotation
	rotation.SecretID = secret.ID
	rotation.OldVersion = secret.Version
	rotation.NewVersion = secret.Version + 1
	rotation.OldValue = secret.EncryptedValue
	rotation.RotatedBy = userID
	rotation.Reason = "manual_rotation"
	h.DB.Create(&rotation)

	h.logAudit(&secret, models.AuditActionRotate, userID, c.ClientIP(), c.GetHeader("User-Agent"), rotationHistory)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "密钥轮换成功",
		"data": gin.H{
			"id":          secret.ID,
			"old_version": secret.Version,
			"new_version": secret.Version + 1,
		},
	})
}

// GetStatistics 获取使用统计
func (h *SecretHandler) Statistics(c *gin.Context) {
	userID, role := getRequestUser(c)
	workspaceID, _ := getRequestWorkspace(c)

	// 按类型统计
	typeStats := make([]map[string]interface{}, 0)
	for _, st := range []models.SecretType{
		models.SecretTypeSSH,
		models.SecretTypeToken,
		models.SecretTypeRegistry,
		models.SecretTypeAPIKey,
		models.SecretTypeKubernetes,
	} {
		var count int64
		applySecretReadScope(h.DB.Model(&models.Secret{}), userID, role).
			Where("workspace_id = ?", workspaceID).
			Where("type = ?", st).
			Count(&count)

		typeStats = append(typeStats, map[string]interface{}{
			"type":  st,
			"count": count,
		})
	}

	// 按状态统计
	statusStats := make([]map[string]interface{}, 0)
	for _, ss := range []models.SecretStatus{
		models.SecretStatusActive,
		models.SecretStatusInactive,
		models.SecretStatusExpired,
		models.SecretStatusRevoked,
	} {
		var count int64
		applySecretReadScope(h.DB.Model(&models.Secret{}), userID, role).
			Where("workspace_id = ?", workspaceID).
			Where("status = ?", ss).
			Count(&count)

		statusStats = append(statusStats, map[string]interface{}{
			"status": ss,
			"count":  count,
		})
	}

	// 最近使用排行
	var recentUsage []map[string]interface{}
	applySecretReadScope(h.DB.Model(&models.Secret{}), userID, role).
		Where("workspace_id = ?", workspaceID).
		Where("last_used_at > ?", 0).
		Order("last_used_at DESC").
		Limit(10).
		Find(&recentUsage)

	// 使用频率统计（最近7天）
	var usageByDay []map[string]interface{}
	for i := 6; i >= 0; i-- {
		day := time.Now().AddDate(0, 0, -i)
		dayStart := day.Unix() / 86400 * 86400
		dayEnd := dayStart + 86400

		var count int64
		h.DB.Model(&models.SecretUsage{}).
			Where("secret_id IN (?)", accessibleSecretIDsInWorkspaceSubQuery(h.DB, workspaceID, userID, role)).
			Where("used_at >= ? AND used_at < ?", dayStart, dayEnd).
			Count(&count)

		usageByDay = append(usageByDay, map[string]interface{}{
			"date":  day.Format("2006-01-02"),
			"count": count,
		})
	}

	// 总统计
	var totalSecrets, totalUsages int64
	applySecretReadScope(h.DB.Model(&models.Secret{}), userID, role).Where("workspace_id = ?", workspaceID).Count(&totalSecrets)
	h.DB.Model(&models.SecretUsage{}).
		Where("secret_id IN (?)", accessibleSecretIDsInWorkspaceSubQuery(h.DB, workspaceID, userID, role)).
		Count(&totalUsages)

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"total_secrets": totalSecrets,
			"total_usages":  totalUsages,
			"by_type":       typeStats,
			"by_status":     statusStats,
			"recent_usage":  recentUsage,
			"usage_by_day":  usageByDay,
		},
	})
}
