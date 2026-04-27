package models

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"easydo-server/internal/config"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

func openModelsTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	config.Init()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
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

	return db
}

func createManagedSchemaForTest(t *testing.T, db *gorm.DB) {
	t.Helper()

	for _, model := range managedModels() {
		if err := db.Migrator().CreateTable(model); err != nil {
			t.Fatalf("create table for %T failed: %v", model, err)
		}
	}
}

func TestValidateMigratedSchemaRejectsEmptySchema(t *testing.T) {
	db := openModelsTestDB(t)

	err := validateMigratedSchema(db)
	if err == nil {
		t.Fatal("expected empty schema error")
	}
	if !strings.Contains(err.Error(), "database schema is empty") {
		t.Fatalf("unexpected error: %v", err)
	}
	if db.Migrator().HasTable(&MasterKey{}) {
		t.Fatal("master_keys table should not be created during schema validation")
	}
}

func TestValidateMigratedSchemaRejectsPartialSchema(t *testing.T) {
	db := openModelsTestDB(t)

	if err := db.Migrator().CreateTable(&User{}); err != nil {
		t.Fatalf("create users table failed: %v", err)
	}

	err := validateMigratedSchema(db)
	if err == nil {
		t.Fatal("expected partial schema error")
	}
	if !strings.Contains(err.Error(), "partial migrated schema detected") {
		t.Fatalf("unexpected error: %v", err)
	}
	if db.Migrator().HasTable(&Workspace{}) {
		t.Fatal("workspace table should not be created during schema validation")
	}
}

func TestValidateMigratedSchemaRejectsMissingRequiredColumn(t *testing.T) {
	db := openModelsTestDB(t)
	createManagedSchemaForTest(t, db)

	if err := db.Migrator().DropColumn(&PipelineTrigger{}, "push_branch_filters"); err != nil {
		t.Fatalf("drop push_branch_filters failed: %v", err)
	}

	err := validateMigratedSchema(db)
	if err == nil {
		t.Fatal("expected missing column error")
	}
	if !strings.Contains(err.Error(), "missing required columns") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateMigratedSchemaAcceptsReadySchemaAndSupportsMasterKeyInitialization(t *testing.T) {
	db := openModelsTestDB(t)
	createManagedSchemaForTest(t, db)

	if err := validateMigratedSchema(db); err != nil {
		t.Fatalf("expected migrated schema to be accepted, got %v", err)
	}

	key, err := LoadOrCreateMasterKey(db)
	if err != nil {
		t.Fatalf("LoadOrCreateMasterKey failed: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("master key length=%d, want 32", len(key))
	}

	var count int64
	if err := db.Model(&MasterKey{}).Count(&count).Error; err != nil {
		t.Fatalf("count master keys failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("master key row count=%d, want 1", count)
	}
}

func TestManagedModelsIncludeNotificationDomainModels(t *testing.T) {
	managed := managedModels()
	managedTypes := make(map[reflect.Type]struct{}, len(managed))
	for _, model := range managed {
		managedTypes[reflect.TypeOf(model)] = struct{}{}
	}
	required := []any{
		&NotificationEvent{},
		&NotificationAudience{},
		&Notification{},
		&InboxMessage{},
		&NotificationDelivery{},
		&NotificationPreference{},
		&SystemSetting{},
	}
	for _, model := range required {
		if _, ok := managedTypes[reflect.TypeOf(model)]; !ok {
			t.Fatalf("managedModels missing %T", model)
		}
	}
}

func TestLoadOrCreateSystemDockerHubMirrorsSeedsAndReusesPersistedDefaults(t *testing.T) {
	db := openModelsTestDB(t)
	if err := db.AutoMigrate(&SystemSetting{}); err != nil {
		t.Fatalf("automigrate system settings failed: %v", err)
	}

	mirrors, err := LoadOrCreateSystemDockerHubMirrors(db, []string{" https://mirror-a.example ", "", "https://mirror-b.example"})
	if err != nil {
		t.Fatalf("initial LoadOrCreateSystemDockerHubMirrors failed: %v", err)
	}
	if !reflect.DeepEqual(mirrors, []string{"https://mirror-a.example", "https://mirror-b.example"}) {
		t.Fatalf("mirrors=%v, want seeded defaults", mirrors)
	}

	if err := db.Model(&SystemSetting{}).Where("key = ?", SystemSettingKeyDockerHubMirrors).Update("value", `["https://mirror-c.example"]`).Error; err != nil {
		t.Fatalf("update system setting failed: %v", err)
	}

	reloaded, err := LoadOrCreateSystemDockerHubMirrors(db, []string{"https://mirror-d.example"})
	if err != nil {
		t.Fatalf("reload LoadOrCreateSystemDockerHubMirrors failed: %v", err)
	}
	if !reflect.DeepEqual(reloaded, []string{"https://mirror-c.example"}) {
		t.Fatalf("mirrors=%v, want persisted value", reloaded)
	}
}

func TestInitializeSystemSettingsSeedsBootstrapDockerHubMirrorsOnce(t *testing.T) {
	db := openModelsTestDB(t)
	if err := db.AutoMigrate(&SystemSetting{}); err != nil {
		t.Fatalf("automigrate system settings failed: %v", err)
	}

	original := config.Config.GetString("buildkit.bootstrap_dockerhub_mirrors")
	config.Config.Set("buildkit.bootstrap_dockerhub_mirrors", " https://mirror-a.example , , https://mirror-b.example ")
	defer config.Config.Set("buildkit.bootstrap_dockerhub_mirrors", original)

	if err := initializeSystemSettings(db); err != nil {
		t.Fatalf("initializeSystemSettings failed: %v", err)
	}

	var setting SystemSetting
	if err := db.Where("key = ?", SystemSettingKeyDockerHubMirrors).First(&setting).Error; err != nil {
		t.Fatalf("load seeded system setting failed: %v", err)
	}
	if setting.Value != `["https://mirror-a.example","https://mirror-b.example"]` {
		t.Fatalf("stored value=%s, want seeded bootstrap mirrors", setting.Value)
	}

	var count int64
	if err := db.Model(&SystemSetting{}).Where("key = ?", SystemSettingKeyDockerHubMirrors).Count(&count).Error; err != nil {
		t.Fatalf("count system settings failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("system setting row count=%d, want 1", count)
	}
}

func TestInitializeSystemSettingsDoesNotOverwritePersistedDockerHubMirrors(t *testing.T) {
	db := openModelsTestDB(t)
	if err := db.AutoMigrate(&SystemSetting{}); err != nil {
		t.Fatalf("automigrate system settings failed: %v", err)
	}

	if err := db.Create(&SystemSetting{Key: SystemSettingKeyDockerHubMirrors, Value: `["https://persisted.example"]`}).Error; err != nil {
		t.Fatalf("create persisted system setting failed: %v", err)
	}

	original := config.Config.GetString("buildkit.bootstrap_dockerhub_mirrors")
	config.Config.Set("buildkit.bootstrap_dockerhub_mirrors", "https://bootstrap.example")
	defer config.Config.Set("buildkit.bootstrap_dockerhub_mirrors", original)

	if err := initializeSystemSettings(db); err != nil {
		t.Fatalf("initializeSystemSettings failed: %v", err)
	}

	var setting SystemSetting
	if err := db.Where("key = ?", SystemSettingKeyDockerHubMirrors).First(&setting).Error; err != nil {
		t.Fatalf("load persisted system setting failed: %v", err)
	}
	if setting.Value != `["https://persisted.example"]` {
		t.Fatalf("stored value=%s, want existing persisted mirrors", setting.Value)
	}

	var count int64
	if err := db.Model(&SystemSetting{}).Where("key = ?", SystemSettingKeyDockerHubMirrors).Count(&count).Error; err != nil {
		t.Fatalf("count system settings failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("system setting row count=%d, want 1", count)
	}
}

func TestSystemSettingLookupQuotesReservedKeyColumnForMySQL(t *testing.T) {
	db := openModelsTestDB(t)
	stmt := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var setting SystemSetting
		return tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Scopes(systemSettingKeyScope(SystemSettingKeyDockerHubMirrors)).
			First(&setting)
	})
	if !strings.Contains(stmt, "`key` =") {
		t.Fatalf("sql=%s, want quoted key column", stmt)
	}
}

func TestRetryReturnsNilAfterTransientFailures(t *testing.T) {
	attempts := 0
	err := retry(3, 0, func() error {
		attempts++
		if attempts < 3 {
			return errors.New("transient failure")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retry returned error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts=%d, want 3", attempts)
	}
}

func TestRetryReturnsLastErrorWhenAttemptsExhausted(t *testing.T) {
	wantErr := errors.New("still failing")
	start := time.Now()
	err := retry(2, 0, func() error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error=%v, want %v", err, wantErr)
	}
	if time.Since(start) > time.Second {
		t.Fatal("retry took unexpectedly long with zero delay")
	}
}
