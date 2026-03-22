package handlers

import (
	"encoding/json"
	"strconv"
	"strings"

	"easydo-server/internal/middleware"
	"easydo-server/internal/models"
	"gorm.io/gorm"
)

func getRequestUser(c ContextGetter) (uint64, string) {
	userID := c.GetUint64("user_id")
	role := strings.TrimSpace(c.GetString("role"))
	return userID, role
}

type ContextGetter interface {
	GetUint64(string) uint64
	GetString(string) string
}

func isAdminRole(role string) bool {
	return strings.EqualFold(strings.TrimSpace(role), "admin")
}

func getRequestWorkspace(c ContextGetter) (uint64, string) {
	workspaceID := c.GetUint64("workspace_id")
	workspaceRole := strings.TrimSpace(c.GetString("workspace_role"))
	return workspaceID, models.NormalizeWorkspaceRole(workspaceRole)
}

func userWorkspaceRole(db *gorm.DB, workspaceID, userID uint64) (string, bool) {
	if db == nil || workspaceID == 0 || userID == 0 {
		return "", false
	}
	var member models.WorkspaceMember
	if err := db.Where(
		"workspace_id = ? AND user_id = ? AND status = ?",
		workspaceID,
		userID,
		models.WorkspaceMemberStatusActive,
	).First(&member).Error; err != nil {
		return "", false
	}
	return models.NormalizeWorkspaceRole(member.Role), true
}

func userHasWorkspaceRole(db *gorm.DB, workspaceID, userID uint64, systemRole string, minRole string) bool {
	if isAdminRole(systemRole) {
		return true
	}
	role, ok := userWorkspaceRole(db, workspaceID, userID)
	if !ok {
		return false
	}
	return middleware.WorkspaceRoleAtLeast(role, minRole)
}

func userCanAccessWorkspace(db *gorm.DB, workspaceID, userID uint64, systemRole string) bool {
	return userHasWorkspaceRole(db, workspaceID, userID, systemRole, models.WorkspaceRoleViewer)
}

func userCanWriteWorkspaceResource(db *gorm.DB, workspaceID, userID uint64, systemRole string) bool {
	return userHasWorkspaceRole(db, workspaceID, userID, systemRole, models.WorkspaceRoleDeveloper)
}

func userCanManageWorkspace(db *gorm.DB, workspaceID, userID uint64, systemRole string) bool {
	return userHasWorkspaceRole(db, workspaceID, userID, systemRole, models.WorkspaceRoleMaintainer)
}

func projectWorkspaceID(db *gorm.DB, projectID uint64) uint64 {
	if db == nil || projectID == 0 {
		return 0
	}
	var workspaceID uint64
	db.Model(&models.Project{}).Where("id = ?", projectID).Pluck("workspace_id", &workspaceID)
	return workspaceID
}

func pipelineWorkspaceID(db *gorm.DB, pipelineID uint64) uint64 {
	if db == nil || pipelineID == 0 {
		return 0
	}
	var workspaceID uint64
	db.Model(&models.Pipeline{}).Where("id = ?", pipelineID).Pluck("workspace_id", &workspaceID)
	return workspaceID
}

func pipelineBelongsToWorkspace(db *gorm.DB, pipelineID, workspaceID uint64) bool {
	if db == nil || pipelineID == 0 || workspaceID == 0 {
		return false
	}
	var count int64
	db.Model(&models.Pipeline{}).Where("id = ? AND workspace_id = ?", pipelineID, workspaceID).Count(&count)
	return count > 0
}

func pipelineRunBelongsToWorkspace(db *gorm.DB, runID, workspaceID uint64) bool {
	if db == nil || runID == 0 || workspaceID == 0 {
		return false
	}
	var count int64
	db.Model(&models.PipelineRun{}).Where("id = ? AND workspace_id = ?", runID, workspaceID).Count(&count)
	return count > 0
}

func taskBelongsToWorkspace(db *gorm.DB, taskID, workspaceID uint64) bool {
	if db == nil || taskID == 0 || workspaceID == 0 {
		return false
	}
	var count int64
	db.Model(&models.AgentTask{}).Where("id = ? AND workspace_id = ?", taskID, workspaceID).Count(&count)
	return count > 0
}

func webhookConfigBelongsToWorkspace(db *gorm.DB, configID, workspaceID uint64) bool {
	if db == nil || configID == 0 || workspaceID == 0 {
		return false
	}
	var count int64
	db.Model(&models.WebhookConfig{}).Where("id = ? AND workspace_id = ?", configID, workspaceID).Count(&count)
	return count > 0
}

func projectBelongsToWorkspace(db *gorm.DB, projectID, workspaceID uint64) bool {
	if db == nil || projectID == 0 || workspaceID == 0 {
		return false
	}
	var count int64
	db.Model(&models.Project{}).Where("id = ? AND workspace_id = ?", projectID, workspaceID).Count(&count)
	return count > 0
}

func userOwnsProject(db *gorm.DB, projectID, userID uint64) bool {
	if projectID == 0 {
		return false
	}

	var count int64
	db.Model(&models.Project{}).
		Where("id = ? AND owner_id = ?", projectID, userID).
		Count(&count)
	return count > 0
}

func userInSharedList(sharedWith string, userID uint64) bool {
	if strings.TrimSpace(sharedWith) == "" {
		return false
	}

	userIDStr := strconv.FormatUint(userID, 10)
	parts := strings.Split(sharedWith, ",")
	for _, part := range parts {
		if strings.TrimSpace(part) == userIDStr {
			return true
		}
	}

	var ids []uint64
	if err := json.Unmarshal([]byte(sharedWith), &ids); err == nil {
		for _, id := range ids {
			if id == userID {
				return true
			}
		}
	}

	var strIDs []string
	if err := json.Unmarshal([]byte(sharedWith), &strIDs); err == nil {
		for _, id := range strIDs {
			if strings.TrimSpace(id) == userIDStr {
				return true
			}
		}
	}

	return false
}

func canReadCredential(db *gorm.DB, credential *models.Credential, userID uint64, role string) bool {
	if credential == nil {
		return false
	}
	if !userCanAccessWorkspace(db, credential.WorkspaceID, userID, role) {
		return false
	}
	if credential.Scope == models.ScopeUser {
		return credential.OwnerID == userID || userCanManageWorkspace(db, credential.WorkspaceID, userID, role)
	}
	return true
}

func canWriteCredential(db *gorm.DB, credential *models.Credential, userID uint64, role string) bool {
	if credential == nil {
		return false
	}
	if !userCanWriteWorkspaceResource(db, credential.WorkspaceID, userID, role) {
		return false
	}
	if credential.Scope == models.ScopeUser {
		return credential.OwnerID == userID || userCanManageWorkspace(db, credential.WorkspaceID, userID, role)
	}
	return true
}

func canReadCredentialValue(db *gorm.DB, credential *models.Credential, userID uint64, role string) bool {
	if credential == nil {
		return false
	}
	if !userCanWriteWorkspaceResource(db, credential.WorkspaceID, userID, role) {
		return false
	}
	if credential.Scope == models.ScopeUser {
		return credential.OwnerID == userID || userCanManageWorkspace(db, credential.WorkspaceID, userID, role)
	}
	return true
}

func canUseCredentialOperationally(db *gorm.DB, credential *models.Credential, userID uint64, role string) bool {
	return canReadCredentialValue(db, credential, userID, role)
}

func canReadCredentialMetadata(db *gorm.DB, credential *models.Credential, userID uint64, role string) bool {
	if credential == nil {
		return false
	}
	return userCanAccessWorkspace(db, credential.WorkspaceID, userID, role)
}

func isWorkspaceOwner(db *gorm.DB, workspaceID, userID uint64, role string) bool {
	if isAdminRole(role) {
		return true
	}
	workspaceRole, ok := userWorkspaceRole(db, workspaceID, userID)
	return ok && workspaceRole == models.WorkspaceRoleOwner
}

func canAccessLockedCredentialSensitiveOperation(db *gorm.DB, credential *models.Credential, userID uint64, role string) bool {
	if credential == nil || !userCanAccessWorkspace(db, credential.WorkspaceID, userID, role) {
		return false
	}
	if isAdminRole(role) || credential.OwnerID == userID {
		return true
	}
	return isWorkspaceOwner(db, credential.WorkspaceID, userID, role)
}

func canAccessUnlockedCredentialSensitiveOperation(db *gorm.DB, credential *models.Credential, userID uint64, role string) bool {
	if credential == nil {
		return false
	}
	return userCanWriteWorkspaceResource(db, credential.WorkspaceID, userID, role)
}

func canViewCredentialSecret(db *gorm.DB, credential *models.Credential, userID uint64, role string) bool {
	if credential == nil {
		return false
	}
	if credential.EffectiveLockState() == models.CredentialLockStateLocked {
		return canAccessLockedCredentialSensitiveOperation(db, credential, userID, role)
	}
	return canAccessUnlockedCredentialSensitiveOperation(db, credential, userID, role)
}

func canEditCredential(db *gorm.DB, credential *models.Credential, userID uint64, role string) bool {
	return canViewCredentialSecret(db, credential, userID, role)
}

func canVerifyCredential(db *gorm.DB, credential *models.Credential, userID uint64, role string) bool {
	return canUseCredentialOperationally(db, credential, userID, role)
}

func canDeleteCredential(db *gorm.DB, credential *models.Credential, userID uint64, role string) bool {
	return canViewCredentialSecret(db, credential, userID, role)
}

func canToggleCredentialLock(db *gorm.DB, credential *models.Credential, userID uint64, role string) bool {
	if credential == nil {
		return false
	}
	if credential.EffectiveLockState() == models.CredentialLockStateLocked {
		return canAccessLockedCredentialSensitiveOperation(db, credential, userID, role)
	}
	return canAccessUnlockedCredentialSensitiveOperation(db, credential, userID, role)
}

func applyCredentialReadScope(db *gorm.DB, userID uint64, role string) *gorm.DB {
	if isAdminRole(role) {
		return db
	}
	workspaceSubQuery := db.Session(&gorm.Session{}).
		Model(&models.WorkspaceMember{}).
		Select("workspace_id").
		Where("user_id = ? AND status = ?", userID, models.WorkspaceMemberStatusActive)

	return db.Where("workspace_id IN (?)", workspaceSubQuery)
}

func accessibleCredentialIDsSubQuery(db *gorm.DB, userID uint64, role string) *gorm.DB {
	return applyCredentialReadScope(
		db.Session(&gorm.Session{}).Model(&models.Credential{}).Select("id"),
		userID,
		role,
	)
}
