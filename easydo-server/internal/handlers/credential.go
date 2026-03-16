package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"easydo-server/internal/models"
	"easydo-server/internal/services"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type CredentialHandler struct {
	encryptionService *services.CredentialEncryptionService
}

func NewCredentialHandler() *CredentialHandler {
	return &CredentialHandler{encryptionService: services.NewCredentialEncryptionService()}
}

type CreateCredentialRequest struct {
	Name        string                    `json:"name" binding:"required,min=1,max=128"`
	Description string                    `json:"description" binding:"max=512"`
	Type        models.CredentialType     `json:"type" binding:"required"`
	Category    models.CredentialCategory `json:"category"`
	Payload     map[string]interface{}    `json:"payload" binding:"required"`
	Scope       models.CredentialScope    `json:"scope"`
	ProjectID   uint64                    `json:"project_id"`
	ExpiresAt   *int64                    `json:"expires_at"`
}

type UpdateCredentialRequest struct {
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	Category    models.CredentialCategory `json:"category"`
	Payload     map[string]interface{}    `json:"payload"`
	Scope       models.CredentialScope    `json:"scope"`
	ProjectID   uint64                    `json:"project_id"`
	Status      models.CredentialStatus   `json:"status"`
	ExpiresAt   *int64                    `json:"expires_at"`
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

type CredentialImpactBatchRequest struct {
	IDs []uint64 `json:"ids" binding:"required,min=1"`
}

type BatchRequest struct {
	IDs []uint64 `json:"ids" binding:"required,min=1"`
}

type VerifyResponse struct {
	Valid   bool   `json:"valid"`
	Message string `json:"message"`
}

func (h *CredentialHandler) CreateCredential(c *gin.Context) {
	var req CreateCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "Invalid request: " + err.Error()})
		return
	}
	if len(req.Payload) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "payload is required"})
		return
	}
	if !models.IsValidType(req.Type) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "Invalid credential type: " + string(req.Type)})
		return
	}
	req.Type = models.NormalizeType(req.Type)
	if !validateExpiry(req.ExpiresAt) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "Expiration time must be in the future"})
		return
	}
	ownerID, role := getRequestUser(c)
	workspaceID, _ := getRequestWorkspace(c)
	if !userCanWriteWorkspaceResource(models.DB, workspaceID, ownerID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "Access denied"})
		return
	}
	scope, projectID, err := normalizeCredentialScope(models.DB, req.Scope, req.ProjectID, workspaceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}
	encryptedPayload, err := h.encryptionService.EncryptCredentialData(req.Payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "Failed to encrypt credential data"})
		return
	}
	credential := models.Credential{
		Name:             req.Name,
		Description:      req.Description,
		Type:             req.Type,
		Category:         req.Category,
		Scope:            scope,
		WorkspaceID:      workspaceID,
		ProjectID:        projectID,
		OwnerID:          ownerID,
		EncryptedPayload: encryptedPayload,
		Status:           models.CredentialStatusActive,
		ExpiresAt:        req.ExpiresAt,
	}
	if err := models.DB.Create(&credential).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "Failed to create credential: " + err.Error()})
		return
	}
	h.writeCredentialEvent(credential.ID, models.CredentialEventCreated, "user", ownerID, "success", gin.H{"scope": scope, "project_id": projectID})
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": credential.ToResponse()})
}

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
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "Invalid credential type"})
			return
		}
		query = query.Where("type = ?", models.NormalizeType(models.CredentialType(credentialType)))
	}
	if category != "" {
		query = query.Where("category = ?", category)
	}
	if scope != "" {
		query = query.Where("scope = ?", scope)
	}
	if keyword != "" {
		query = query.Where("name LIKE ? OR description LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	now := time.Now().Unix()
	if status == string(models.CredentialStatusExpired) {
		query = query.Where("status = ?", models.CredentialStatusActive).Where("expires_at IS NOT NULL AND expires_at > 0 AND expires_at <= ?", now)
	} else if status != "" {
		query = query.Where("status = ?", status)
	}
	var total int64
	query.Count(&total)
	var credentials []models.Credential
	offset := (page - 1) * size
	query.Order("created_at DESC").Offset(offset).Limit(size).Find(&credentials)
	responses := make([]models.CredentialResponse, len(credentials))
	for i := range credentials {
		responses[i] = credentials[i].ToResponse()
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"list": responses, "total": total, "page": page, "size": size}})
}

func (h *CredentialHandler) GetCredential(c *gin.Context) {
	credential, ok := h.authorizedCredential(c, false)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": credential.ToResponse()})
}

func (h *CredentialHandler) GetCredentialPayload(c *gin.Context) {
	credential, ok := h.authorizedCredential(c, true)
	if !ok {
		return
	}
	payload, err := h.encryptionService.DecryptCredentialData(credential.EncryptedPayload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "Failed to decrypt credential data"})
		return
	}
	ownerID, _ := getRequestUser(c)
	h.writeCredentialEvent(credential.ID, models.CredentialEventRevealed, "user", ownerID, "success", nil)
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"id": credential.ID, "payload": payload}})
}

func (h *CredentialHandler) UpdateCredential(c *gin.Context) {
	credential, ok := h.authorizedWritableCredential(c)
	if !ok {
		return
	}
	var req UpdateCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "Invalid request: " + err.Error()})
		return
	}
	if !validateExpiry(req.ExpiresAt) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "Expiration time must be in the future"})
		return
	}
	if req.Name != "" && len(req.Name) > 128 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "Credential name must be 1-128 characters"})
		return
	}
	if len(req.Description) > 512 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "Description must be 0-512 characters"})
		return
	}
	updates := map[string]interface{}{}
	if req.Scope != "" || req.ProjectID > 0 {
		workspaceID, _ := getRequestWorkspace(c)
		scope, projectID, err := normalizeCredentialScope(models.DB, req.Scope, req.ProjectID, workspaceID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
			return
		}
		updates["scope"] = scope
		updates["project_id"] = projectID
	}
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
		if req.Status != models.CredentialStatusActive && req.Status != models.CredentialStatusInactive && req.Status != models.CredentialStatusRevoked {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "Invalid credential status"})
			return
		}
		updates["status"] = req.Status
	}
	if req.ExpiresAt != nil {
		updates["expires_at"] = req.ExpiresAt
	}
	if req.Payload != nil {
		if len(req.Payload) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "payload cannot be empty"})
			return
		}
		encryptedPayload, err := h.encryptionService.EncryptCredentialData(req.Payload)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "Failed to encrypt credential data"})
			return
		}
		updates["encrypted_payload"] = encryptedPayload
	}
	if len(updates) == 0 {
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": credential.ToResponse()})
		return
	}
	if err := models.DB.Model(&credential).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "Failed to update credential: " + err.Error()})
		return
	}
	models.DB.First(&credential, credential.ID)
	ownerID, _ := getRequestUser(c)
	eventAction := models.CredentialEventUpdated
	if credential.Status == models.CredentialStatusInactive {
		eventAction = models.CredentialEventDisabled
	} else if credential.Status == models.CredentialStatusRevoked {
		eventAction = models.CredentialEventRevoked
	}
	h.writeCredentialEvent(credential.ID, eventAction, "user", ownerID, "success", updates)
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": credential.ToResponse()})
}

func (h *CredentialHandler) DeleteCredential(c *gin.Context) {
	credential, ok := h.authorizedWritableCredential(c)
	if !ok {
		return
	}
	if err := deleteCredentialWithRelations(models.DB, credential.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "Failed to delete credential: " + err.Error()})
		return
	}
	ownerID, _ := getRequestUser(c)
	h.writeCredentialEvent(credential.ID, models.CredentialEventDeleted, "user", ownerID, "success", nil)
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "Credential deleted successfully"})
}

func buildCredentialImpactSummary(db *gorm.DB, credential models.Credential) (CredentialImpactSummary, error) {
	refs := make([]CredentialImpactReference, 0)
	type refRow struct {
		PipelineID     uint64    `gorm:"column:pipeline_id"`
		PipelineName   string    `gorm:"column:pipeline_name"`
		NodeID         string    `gorm:"column:node_id"`
		TaskType       string    `gorm:"column:task_type"`
		CredentialSlot string    `gorm:"column:credential_slot"`
		UpdatedAt      time.Time `gorm:"column:updated_at"`
	}
	rows := make([]refRow, 0)
	if err := db.Table("pipeline_credential_refs AS r").
		Select("r.pipeline_id, p.name AS pipeline_name, r.node_id, r.task_type, r.credential_slot, r.updated_at").
		Joins("LEFT JOIN pipelines p ON p.id = r.pipeline_id").
		Where("r.credential_id = ?", credential.ID).
		Order("r.updated_at DESC").
		Scan(&rows).Error; err != nil {
		return CredentialImpactSummary{}, err
	}
	uniquePipelines := make(map[uint64]struct{})
	for _, row := range rows {
		refs = append(refs, CredentialImpactReference{PipelineID: row.PipelineID, PipelineName: row.PipelineName, NodeID: row.NodeID, TaskType: row.TaskType, CredentialSlot: row.CredentialSlot, UpdatedAt: row.UpdatedAt.Unix()})
		uniquePipelines[row.PipelineID] = struct{}{}
	}
	return CredentialImpactSummary{CredentialID: credential.ID, CredentialName: credential.Name, ReferenceCount: int64(len(refs)), PipelineCount: int64(len(uniquePipelines)), References: refs}, nil
}

func (h *CredentialHandler) GetCredentialImpact(c *gin.Context) {
	credential, ok := h.authorizedCredential(c, false)
	if !ok {
		return
	}
	summary, err := buildCredentialImpactSummary(models.DB, *credential)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "Failed to query credential impact: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": summary})
}

func (h *CredentialHandler) BatchCredentialImpact(c *gin.Context) {
	ownerID, role := getRequestUser(c)
	workspaceID, _ := getRequestWorkspace(c)
	var req CredentialImpactBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "Invalid request: " + err.Error()})
		return
	}
	var credentials []models.Credential
	if err := applyCredentialReadScope(models.DB.Model(&models.Credential{}), ownerID, role).
		Where("workspace_id = ?", workspaceID).
		Where("id IN ?", req.IDs).
		Find(&credentials).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "Failed to query credentials: " + err.Error()})
		return
	}
	summaries := make([]CredentialImpactSummary, 0, len(credentials))
	var totalReferences int64
	for _, credential := range credentials {
		summary, err := buildCredentialImpactSummary(models.DB, credential)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "Failed to query credential impact: " + err.Error()})
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
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"total_credentials": len(summaries), "impacted_credentials": impactedCredentials, "total_references": totalReferences, "items": summaries}})
}

func (h *CredentialHandler) VerifyCredential(c *gin.Context) {
	credential, ok := h.authorizedCredential(c, false)
	if !ok {
		return
	}
	ownerID, _ := getRequestUser(c)
	_, err := h.encryptionService.DecryptCredentialData(credential.EncryptedPayload)
	if err != nil {
		h.writeCredentialEvent(credential.ID, models.CredentialEventVerified, "user", ownerID, "failed", gin.H{"error": err.Error()})
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": VerifyResponse{Valid: false, Message: "Failed to decrypt credential data"}})
		return
	}
	if !credential.IsUsable() {
		h.writeCredentialEvent(credential.ID, models.CredentialEventVerified, "user", ownerID, "failed", gin.H{"status": credential.EffectiveStatus()})
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": VerifyResponse{Valid: false, Message: "Credential is not active"}})
		return
	}
	h.writeCredentialEvent(credential.ID, models.CredentialEventVerified, "user", ownerID, "success", nil)
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": VerifyResponse{Valid: true, Message: "Credential is valid"}})
}

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
		{Value: models.TypeIAM, Label: models.TypeIAM.GetTypeLabel(), TypeInfo: models.TypeIAM.GetTypeInfo()},
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": types})
}

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
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": categories})
}

func (h *CredentialHandler) GetCredentialUsage(c *gin.Context) {
	credential, ok := h.authorizedCredential(c, false)
	if !ok {
		return
	}
	var usageCount int64
	var successCount int64
	var failedCount int64
	models.DB.Model(&models.CredentialEvent{}).Where("credential_id = ? AND action = ?", credential.ID, models.CredentialEventUsed).Count(&usageCount)
	models.DB.Model(&models.CredentialEvent{}).Where("credential_id = ? AND action = ? AND result = ?", credential.ID, models.CredentialEventUsed, "success").Count(&successCount)
	models.DB.Model(&models.CredentialEvent{}).Where("credential_id = ? AND action = ? AND result = ?", credential.ID, models.CredentialEventUsed, "failed").Count(&failedCount)
	response := gin.H{"used_count": usageCount, "last_used_at": credential.LastUsedAt, "success_count": successCount, "failed_count": failedCount, "success_rate": 0.0}
	if usageCount > 0 {
		response["success_rate"] = float64(successCount) / float64(usageCount) * 100
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": response})
}

func (h *CredentialHandler) BatchDeleteCredentials(c *gin.Context) {
	ownerID, role := getRequestUser(c)
	workspaceID, _ := getRequestWorkspace(c)
	var req BatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "Invalid request: " + err.Error()})
		return
	}
	var credentials []models.Credential
	if err := models.DB.Where("id IN ? AND workspace_id = ?", req.IDs, workspaceID).Find(&credentials).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "Failed to query credentials: " + err.Error()})
		return
	}
	if len(credentials) != len(req.IDs) {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "Some credentials not found"})
		return
	}
	for i := range credentials {
		if !canWriteCredential(models.DB, &credentials[i], ownerID, role) {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "Some credentials are not writable"})
			return
		}
	}
	for _, credential := range credentials {
		if err := deleteCredentialWithRelations(models.DB, credential.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "Failed to delete credentials: " + err.Error()})
			return
		}
		h.writeCredentialEvent(credential.ID, models.CredentialEventDeleted, "user", ownerID, "success", gin.H{"batch": true})
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"deleted": len(req.IDs)}, "message": "Credentials deleted successfully"})
}

func (h *CredentialHandler) authorizedCredential(c *gin.Context, includeValue bool) (*models.Credential, bool) {
	ownerID, role := getRequestUser(c)
	workspaceID, _ := getRequestWorkspace(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "Invalid credential ID"})
		return nil, false
	}
	var credential models.Credential
	if err := models.DB.First(&credential, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "Credential not found"})
		return nil, false
	}
	if credential.WorkspaceID != workspaceID {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "Access denied"})
		return nil, false
	}
	allowed := canReadCredential(models.DB, &credential, ownerID, role)
	if includeValue {
		allowed = canReadCredentialValue(models.DB, &credential, ownerID, role)
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "Access denied"})
		return nil, false
	}
	return &credential, true
}

func (h *CredentialHandler) authorizedWritableCredential(c *gin.Context) (*models.Credential, bool) {
	ownerID, role := getRequestUser(c)
	workspaceID, _ := getRequestWorkspace(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "Invalid credential ID"})
		return nil, false
	}
	var credential models.Credential
	if err := models.DB.First(&credential, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "Credential not found"})
		return nil, false
	}
	if credential.WorkspaceID != workspaceID || !canWriteCredential(models.DB, &credential, ownerID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "Access denied"})
		return nil, false
	}
	return &credential, true
}

func validateExpiry(expiresAt *int64) bool {
	if expiresAt == nil || *expiresAt <= 0 {
		return true
	}
	return *expiresAt > time.Now().Unix()
}

func normalizeCredentialScope(db *gorm.DB, scope models.CredentialScope, projectID, workspaceID uint64) (models.CredentialScope, uint64, error) {
	if scope == "" {
		scope = models.ScopeUser
	}
	switch scope {
	case models.ScopeUser, models.ScopeWorkspace:
		return scope, 0, nil
	case models.ScopeProject:
		if projectID == 0 {
			return "", 0, errors.New("Project scope requires project_id")
		}
		if !projectBelongsToWorkspace(db, projectID, workspaceID) {
			return "", 0, errors.New("Project does not belong to active workspace")
		}
		return scope, projectID, nil
	default:
		return "", 0, errors.New("Invalid credential scope")
	}
}

func deleteCredentialWithRelations(db *gorm.DB, credentialID uint64) error {
	if err := db.Where("credential_id = ?", credentialID).Delete(&models.PipelineCredentialRef{}).Error; err != nil {
		return err
	}
	if err := db.Where("credential_id = ?", credentialID).Delete(&models.CredentialEvent{}).Error; err != nil {
		return err
	}
	return db.Delete(&models.Credential{}, credentialID).Error
}

func (h *CredentialHandler) writeCredentialEvent(credentialID uint64, action, actorType string, actorID uint64, result string, detail interface{}) {
	detailJSON := ""
	if detail != nil {
		if payload, err := json.Marshal(detail); err == nil {
			detailJSON = string(payload)
		}
	}
	_ = models.DB.Create(&models.CredentialEvent{
		CredentialID: credentialID,
		Action:       action,
		ActorType:    actorType,
		ActorID:      actorID,
		Result:       result,
		DetailJSON:   detailJSON,
	}).Error
}
