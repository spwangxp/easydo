package handlers

import (
	"encoding/json"
	"strconv"
	"strings"

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
	if isAdminRole(role) || credential.OwnerID == userID {
		return true
	}
	if credential.Scope == models.ScopeGlobal {
		return true
	}
	if credential.IsShared && userInSharedList(credential.SharedWith, userID) {
		return true
	}
	if credential.Scope == models.ScopeProject && userOwnsProject(db, credential.ProjectID, userID) {
		return true
	}
	return false
}

func canWriteCredential(db *gorm.DB, credential *models.Credential, userID uint64, role string) bool {
	if credential == nil {
		return false
	}
	if isAdminRole(role) || credential.OwnerID == userID {
		return true
	}
	if credential.Scope == models.ScopeProject && userOwnsProject(db, credential.ProjectID, userID) {
		return true
	}
	return false
}

func canReadSecret(db *gorm.DB, secret *models.Secret, userID uint64, role string) bool {
	if secret == nil {
		return false
	}
	if isAdminRole(role) || secret.CreatedBy == userID {
		return true
	}
	if secret.Scope == models.SecretScopeAll {
		return true
	}
	if secret.IsShared && userInSharedList(secret.SharedWith, userID) {
		return true
	}
	if secret.Scope == models.SecretScopeProject && userOwnsProject(db, secret.ProjectID, userID) {
		return true
	}
	return false
}

func canWriteSecret(db *gorm.DB, secret *models.Secret, userID uint64, role string) bool {
	if secret == nil {
		return false
	}
	if isAdminRole(role) || secret.CreatedBy == userID {
		return true
	}
	if secret.Scope == models.SecretScopeProject && userOwnsProject(db, secret.ProjectID, userID) {
		return true
	}
	return false
}

func applyCredentialReadScope(db *gorm.DB, userID uint64, role string) *gorm.DB {
	if isAdminRole(role) {
		return db
	}
	projectSubQuery := db.Session(&gorm.Session{}).
		Model(&models.Project{}).
		Select("id").
		Where("owner_id = ?", userID)

	return db.Where(
		"owner_id = ? OR scope = ? OR (is_shared = ? AND (shared_with = '' OR shared_with LIKE ?)) OR (scope = ? AND project_id IN (?))",
		userID,
		models.ScopeGlobal,
		true,
		"%"+strconv.FormatUint(userID, 10)+"%",
		models.ScopeProject,
		projectSubQuery,
	)
}

func applySecretReadScope(db *gorm.DB, userID uint64, role string) *gorm.DB {
	if isAdminRole(role) {
		return db
	}
	projectSubQuery := db.Session(&gorm.Session{}).
		Model(&models.Project{}).
		Select("id").
		Where("owner_id = ?", userID)

	return db.Where(
		"created_by = ? OR scope = ? OR (is_shared = ? AND (shared_with = '' OR shared_with LIKE ?)) OR (scope = ? AND project_id IN (?))",
		userID,
		models.SecretScopeAll,
		true,
		"%"+strconv.FormatUint(userID, 10)+"%",
		models.SecretScopeProject,
		projectSubQuery,
	)
}

func accessibleSecretIDsSubQuery(db *gorm.DB, userID uint64, role string) *gorm.DB {
	return applySecretReadScope(
		db.Session(&gorm.Session{}).Model(&models.Secret{}).Select("id"),
		userID,
		role,
	)
}

func accessibleCredentialIDsSubQuery(db *gorm.DB, userID uint64, role string) *gorm.DB {
	return applyCredentialReadScope(
		db.Session(&gorm.Session{}).Model(&models.Credential{}).Select("id"),
		userID,
		role,
	)
}
