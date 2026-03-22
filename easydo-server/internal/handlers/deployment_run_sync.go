package handlers

import (
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
