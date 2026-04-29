package handlers

import (
	"encoding/json"
	"fmt"
	"strings"

	"easydo-server/internal/models"
	"gorm.io/gorm"
)

func deploymentRequestStatusFromRunStatus(status string) (models.DeploymentRequestStatus, bool) {
	switch status {
	case models.PipelineRunStatusQueued:
		return models.DeploymentRequestStatusQueued, true
	case models.PipelineRunStatusRunning:
		return models.DeploymentRequestStatusRunning, true
	case models.PipelineRunStatusSuccess:
		return models.DeploymentRequestStatusSuccess, true
	case models.PipelineRunStatusFailed:
		return models.DeploymentRequestStatusFailed, true
	case models.PipelineRunStatusCancelled:
		return models.DeploymentRequestStatusCancelled, true
	default:
		return "", false
	}
}

func syncDeploymentStateFromRun(db *gorm.DB, run *models.PipelineRun) {
	if db == nil || run == nil || run.ID == 0 {
		return
	}

	deploymentStatus, ok := deploymentRequestStatusFromRunStatus(run.Status)
	if !ok {
		return
	}

	requestUpdates := map[string]interface{}{
		"status": deploymentStatus,
	}
	if err := db.Model(&models.DeploymentRequest{}).
		Where("pipeline_run_id = ?", run.ID).
		Updates(requestUpdates).Error; err != nil {
		return
	}
	var request models.DeploymentRequest
	if err := db.Where("pipeline_run_id = ?", run.ID).First(&request).Error; err == nil {
		if deploymentStatus == models.DeploymentRequestStatusSuccess {
			syncSelfDeployAIProviderAndBinding(db, &request)
		}
		eventType := deploymentRequestEventType(deploymentStatus)
		if eventType != "" {
			emitDeploymentRequestNotification(db, &request, eventType, deploymentRequestEventTitle(deploymentStatus), deploymentRequestEventContent(deploymentStatus))
		}
	}

	recordUpdates := map[string]interface{}{
		"status": deploymentStatus,
	}
	if deploymentStatus == models.DeploymentRequestStatusFailed || deploymentStatus == models.DeploymentRequestStatusCancelled {
		recordUpdates["failure_reason"] = strings.TrimSpace(run.ErrorMsg)
	} else {
		recordUpdates["failure_reason"] = ""
	}
	_ = db.Model(&models.DeploymentRecord{}).
		Where("pipeline_run_id = ?", run.ID).
		Updates(recordUpdates).Error
}

func canUseDeploymentBoundCredential(db *gorm.DB, credential *models.Credential, run *models.PipelineRun, slot string) bool {
	if db == nil || credential == nil || run == nil || run.ID == 0 {
		return false
	}
	if run.TriggerType != "deployment_request" || credential.WorkspaceID != run.WorkspaceID {
		return false
	}

	var request models.DeploymentRequest
	if err := db.Where("pipeline_run_id = ?", run.ID).First(&request).Error; err != nil {
		return false
	}
	if request.TargetResourceID == 0 {
		return false
	}

	allowedPurposes := []string{slot, "primary"}
	var count int64
	if err := db.Model(&models.ResourceCredentialBinding{}).
		Where("workspace_id = ? AND resource_id = ? AND credential_id = ? AND purpose IN ?", run.WorkspaceID, request.TargetResourceID, credential.ID, allowedPurposes).
		Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

func syncSelfDeployAIProviderAndBinding(db *gorm.DB, request *models.DeploymentRequest) {
	if db == nil || request == nil || request.ID == 0 || request.AIModelID == 0 {
		return
	}

	var deploymentResource models.Resource
	if err := db.First(&deploymentResource, request.TargetResourceID).Error; err != nil {
		return
	}
	baseURL := buildSelfDeployProviderBaseURL(&deploymentResource)
	if baseURL == "" {
		return
	}

	providerName := buildSelfDeployProviderName(db, request, &deploymentResource)
	metadataJSON := marshalJSONOrEmpty(map[string]interface{}{
		"deployment_request_id": request.ID,
		"resource_id":           request.TargetResourceID,
		"pipeline_run_id":       request.PipelineRunID,
		"template_version_id":   request.TemplateVersionID,
		"ai_model_id":           request.AIModelID,
	})

	provider := models.AIProvider{}
	providerErr := db.Where("workspace_id = ? AND provider_type = ? AND base_url = ?", request.WorkspaceID, "self_deploy", baseURL).Where("metadata_json LIKE ?", fmt.Sprintf("%%\"ai_model_id\":%d%%", request.AIModelID)).First(&provider).Error
	if providerErr != nil {
		provider = models.AIProvider{
			WorkspaceID:  request.WorkspaceID,
			Name:         providerName,
			Description:  "Auto materialized from successful self-deploy LLM deployment",
			ProviderType: "self_deploy",
			BaseURL:      baseURL,
			MetadataJSON: metadataJSON,
			Status:       models.AIProviderStatusActive,
			CreatedBy:    request.RequestedBy,
		}
		if err := db.Create(&provider).Error; err != nil {
			return
		}
	} else {
		_ = db.Model(&provider).Updates(map[string]interface{}{
			"name":          providerName,
			"metadata_json": metadataJSON,
			"status":        models.AIProviderStatusActive,
		}).Error
	}

	binding := models.AIModelBinding{}
	bindingErr := db.Where("workspace_id = ? AND provider_id = ? AND model_id = ?", request.WorkspaceID, provider.ID, request.AIModelID).First(&binding).Error
	if bindingErr != nil {
		binding = models.AIModelBinding{
			WorkspaceID:      request.WorkspaceID,
			ModelID:          request.AIModelID,
			ProviderID:       provider.ID,
			ProviderModelKey: extractSelfDeployAIProviderModelKey(request),
			MetadataJSON:     metadataJSON,
			Status:           models.AIModelBindingStatusActive,
			CreatedBy:        request.RequestedBy,
		}
		_ = db.Create(&binding).Error
	} else {
		_ = db.Model(&binding).Updates(map[string]interface{}{
			"provider_model_key": extractSelfDeployAIProviderModelKey(request),
			"metadata_json":      metadataJSON,
			"status":             models.AIModelBindingStatusActive,
		}).Error
	}
}

func buildSelfDeployProviderBaseURL(resource *models.Resource) string {
	if resource == nil {
		return ""
	}
	endpoint := strings.TrimSpace(resource.Endpoint)
	if endpoint == "" {
		return ""
	}
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		if strings.HasSuffix(endpoint, "/v1") {
			return endpoint
		}
		return strings.TrimRight(endpoint, "/") + "/v1"
	}
	return fmt.Sprintf("http://%s/v1", strings.TrimRight(endpoint, "/"))
}

func buildSelfDeployProviderName(db *gorm.DB, request *models.DeploymentRequest, resource *models.Resource) string {
	resourceName := "resource"
	if resource != nil && strings.TrimSpace(resource.Name) != "" {
		resourceName = strings.TrimSpace(resource.Name)
	}
	modelName := fmt.Sprintf("model-%d", request.AIModelID)
	if db != nil && request.AIModelID > 0 {
		var model models.AIModelCatalog
		if err := db.First(&model, request.AIModelID).Error; err == nil {
			if strings.TrimSpace(model.DisplayName) != "" {
				modelName = strings.TrimSpace(model.DisplayName)
			} else if strings.TrimSpace(model.Name) != "" {
				modelName = strings.TrimSpace(model.Name)
			}
		}
	}
	return fmt.Sprintf("%s · %s", resourceName, modelName)
}

func extractSelfDeployAIProviderModelKey(request *models.DeploymentRequest) string {
	if request == nil {
		return ""
	}
	if strings.TrimSpace(request.AIModelSnapshot) != "" {
		var snapshot map[string]interface{}
		if err := json.Unmarshal([]byte(request.AIModelSnapshot), &snapshot); err == nil {
			if sourceID := strings.TrimSpace(fmt.Sprint(snapshot["source_model_id"])); sourceID != "" {
				return sourceID
			}
			if name := strings.TrimSpace(fmt.Sprint(snapshot["name"])); name != "" {
				return name
			}
		}
	}
	return fmt.Sprintf("model-%d", request.AIModelID)
}
