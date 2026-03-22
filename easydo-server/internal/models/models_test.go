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
	required := []interface{}{
		&NotificationEvent{},
		&NotificationAudience{},
		&Notification{},
		&InboxMessage{},
		&NotificationDelivery{},
		&NotificationPreference{},
	}
	for _, model := range required {
		if _, ok := managedTypes[reflect.TypeOf(model)]; !ok {
			t.Fatalf("managedModels missing %T", model)
		}
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
