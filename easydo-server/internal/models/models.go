package models

import (
	"easydo-server/internal/config"
	"fmt"
	"strings"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

const (
	dbInitMaxAttempts = 30
	dbInitRetryDelay  = time.Second
)

type BaseModel struct {
	ID        uint64    `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func InitDB() {
	var err error
	DB, err = openDBWithRetry(mysql.Open(config.GetDSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	}, dbInitMaxAttempts, dbInitRetryDelay)
	if err != nil {
		panic("Failed to connect to database: " + err.Error())
	}

	sqlDB, err := DB.DB()
	if err != nil {
		panic("Failed to get underlying sql.DB: " + err.Error())
	}

	sqlDB.SetMaxOpenConns(config.Config.GetInt("database.max_open_conns"))
	sqlDB.SetMaxIdleConns(config.Config.GetInt("database.max_idle_conns"))
	sqlDB.SetConnMaxLifetime(config.Config.GetDuration("database.max_life_time"))

	if err := validateMigratedSchema(DB); err != nil {
		panic("Failed to validate migrated schema: " + err.Error())
	}

	if _, err := loadOrCreateMasterKeyWithRetry(DB, dbInitMaxAttempts, dbInitRetryDelay); err != nil {
		panic("Failed to initialize master key: " + err.Error())
	}
}

func openDBWithRetry(dialector gorm.Dialector, cfg *gorm.Config, attempts int, delay time.Duration) (*gorm.DB, error) {
	var db *gorm.DB
	err := retry(attempts, delay, func() error {
		var openErr error
		db, openErr = gorm.Open(dialector, cfg)
		return openErr
	})
	if err != nil {
		return nil, err
	}
	return db, nil
}

func loadOrCreateMasterKeyWithRetry(db *gorm.DB, attempts int, delay time.Duration) ([]byte, error) {
	var key []byte
	err := retry(attempts, delay, func() error {
		var loadErr error
		key, loadErr = LoadOrCreateMasterKey(db)
		return loadErr
	})
	if err != nil {
		return nil, err
	}
	return key, nil
}

func retry(attempts int, delay time.Duration, operation func() error) error {
	if attempts <= 0 {
		attempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := operation(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt < attempts {
			time.Sleep(delay)
		}
	}
	return lastErr
}

type managedSchemaState int

const (
	managedSchemaEmpty managedSchemaState = iota
	managedSchemaComplete
	managedSchemaPartial
)

func managedModels() []interface{} {
	return []interface{}{
		&User{},
		&Workspace{},
		&WorkspaceMember{},
		&WorkspaceInvitation{},
		&Project{},
		&Pipeline{},
		&PipelineTrigger{},
		&PipelineRun{},
		&DeployRecord{},
		&Agent{},
		&AgentTask{},
		&TaskExecution{},
		&AgentHeartbeat{},
		&AgentLog{},
		&AgentTaskEvent{},
		&AgentLogSegment{},
		&WebhookConfig{},
		&WebhookEvent{},
		&NotificationEvent{},
		&NotificationAudience{},
		&Notification{},
		&InboxMessage{},
		&NotificationDelivery{},
		&NotificationPreference{},
		&Credential{},
		&CredentialEvent{},
		&PipelineCredentialRef{},
		&Resource{},
		&ResourceCredentialBinding{},
		&ResourceTerminalSession{},
		&ResourceHealthSnapshot{},
		&StoreTemplate{},
		&StoreTemplateVersion{},
		&TemplateParameter{},
		&LLMModelCatalog{},
		&DeploymentRequest{},
		&DeploymentRecord{},
		&MasterKey{},
	}
}

func validateMigratedSchema(db *gorm.DB) error {
	modelsToValidate := managedModels()
	schemaState, existingTables, err := detectManagedSchemaState(db, modelsToValidate)
	if err != nil {
		return err
	}

	switch schemaState {
	case managedSchemaEmpty:
		return fmt.Errorf("database schema is empty; run database migrations before startup")
	case managedSchemaPartial:
		return fmt.Errorf("partial migrated schema detected (%d/%d tables exist: %s); run database migrations before startup", len(existingTables), len(modelsToValidate), strings.Join(existingTables, ", "))
	case managedSchemaComplete:
	}

	missingColumns := missingManagedSchemaColumns(db)
	if len(missingColumns) > 0 {
		return fmt.Errorf("migrated schema is missing required columns: %s; run database migrations before startup", strings.Join(missingColumns, ", "))
	}

	return nil
}

func detectManagedSchemaState(db *gorm.DB, modelsToValidate []interface{}) (managedSchemaState, []string, error) {
	existingTables := make([]string, 0, len(modelsToValidate))
	for _, model := range modelsToValidate {
		if db.Migrator().HasTable(model) {
			existingTables = append(existingTables, modelTableName(db, model))
		}
	}

	switch len(existingTables) {
	case 0:
		return managedSchemaEmpty, nil, nil
	case len(modelsToValidate):
		return managedSchemaComplete, existingTables, nil
	default:
		return managedSchemaPartial, existingTables, nil
	}
}

type columnSync struct {
	model interface{}
	field string
}

func managedSchemaColumnSyncs() []columnSync {
	return []columnSync{
		{model: &PipelineTrigger{}, field: "PushBranchFilters"},
		{model: &PipelineTrigger{}, field: "TagFilters"},
		{model: &PipelineTrigger{}, field: "MergeRequestSourceBranchFilters"},
		{model: &PipelineTrigger{}, field: "MergeRequestTargetBranchFilters"},
		{model: &PipelineRun{}, field: "IdempotencyKey"},
		{model: &LLMModelCatalog{}, field: "ParameterSize"},
		{model: &InboxMessage{}, field: "NotificationID"},
		{model: &InboxMessage{}, field: "EventType"},
		{model: &NotificationDelivery{}, field: "NextRetryAt"},
		{model: &NotificationPreference{}, field: "RuleKey"},
	}
}

func missingManagedSchemaColumns(db *gorm.DB) []string {
	missing := make([]string, 0)
	for _, item := range managedSchemaColumnSyncs() {
		if db.Migrator().HasColumn(item.model, item.field) {
			continue
		}
		missing = append(missing, fmt.Sprintf("%s.%s", modelTableName(db, item.model), item.field))
	}
	return missing
}

func modelTableName(db *gorm.DB, model interface{}) string {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err == nil && stmt.Schema != nil {
		return stmt.Schema.Table
	}
	return fmt.Sprintf("%T", model)
}
