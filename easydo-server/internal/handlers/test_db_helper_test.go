package handlers

import (
	"fmt"
	"strings"
	"testing"

	"easydo-server/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	name := strings.NewReplacer("/", "_", " ", "_", ":", "_").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", name)

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger:                                   logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite db failed: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db failed: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	if err := db.AutoMigrate(
		&models.User{},
		&models.Workspace{},
		&models.WorkspaceMember{},
		&models.WorkspaceInvitation{},
		&models.Project{},
		&models.Pipeline{},
		&models.PipelineRun{},
		&models.Agent{},
		&models.AgentTask{},
		&models.TaskExecution{},
		&models.AgentLog{},
		&models.AgentHeartbeat{},
		&models.AgentTaskEvent{},
		&models.AgentLogChunk{},
		&models.AgentLogSegment{},
		&models.Secret{},
		&models.SecretUsage{},
		&models.SecretAuditLog{},
		&models.SecretRotation{},
		&models.Message{},
		&models.WebhookConfig{},
		&models.WebhookEvent{},
		&models.Credential{},
		&models.CredentialUsage{},
		&models.PipelineCredentialRef{},
		&models.SecretPermission{},
	); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}

	return db
}
