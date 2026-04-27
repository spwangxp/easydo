package handlers

import (
	"fmt"
	"strings"
	"testing"

	"easydo-server/internal/config"
	"easydo-server/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	serverID := ""
	serverPublicURL := ""
	serverInternalURL := ""
	serverInternalToken := ""
	if config.Config != nil {
		serverID = config.Config.GetString("server.id")
		serverPublicURL = config.Config.GetString("server.public_url")
		serverInternalURL = config.Config.GetString("server.internal_url")
		serverInternalToken = config.Config.GetString("server.internal_token")
	}
	config.Init()
	if serverID != "" {
		config.Config.Set("server.id", serverID)
	}
	if serverPublicURL != "" {
		config.Config.Set("server.public_url", serverPublicURL)
	}
	if serverInternalURL != "" {
		config.Config.Set("server.internal_url", serverInternalURL)
	}
	if serverInternalToken != "" {
		config.Config.Set("server.internal_token", serverInternalToken)
	}

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
		&models.PipelineTrigger{},
		&models.PipelineRun{},
		&models.Agent{},
		&models.AgentTask{},
		&models.TaskExecution{},
		&models.AgentLog{},
		&models.AgentHeartbeat{},
		&models.AgentTaskEvent{},
		&models.AgentLogChunk{},
		&models.AgentLogSegment{},
		&models.NotificationEvent{},
		&models.NotificationAudience{},
		&models.Notification{},
		&models.InboxMessage{},
		&models.NotificationDelivery{},
		&models.NotificationPreference{},
		&models.WebhookConfig{},
		&models.WebhookEvent{},
		&models.Credential{},
		&models.CredentialEvent{},
		&models.PipelineCredentialRef{},
		&models.Resource{},
		&models.ResourceCredentialBinding{},
		&models.ResourceTerminalSession{},
		&models.ResourceHealthSnapshot{},
		&models.ResourceOperationAudit{},
		&models.StoreTemplate{},
		&models.StoreTemplateVersion{},
		&models.TemplateParameter{},
		&models.LLMModelCatalog{},
		&models.DeploymentRequest{},
		&models.DeploymentRecord{},
		&models.MasterKey{},
		&models.SystemSetting{},
	); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}
	if _, err := models.LoadOrCreateMasterKey(db); err != nil {
		t.Fatalf("load master key failed: %v", err)
	}

	return db
}
