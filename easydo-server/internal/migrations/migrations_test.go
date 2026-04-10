package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	dbmigrations "easydo-server/db/migrations"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openMigrationTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "_", " ", "_", ":", "_").Replace(t.Name()))
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

	return sqlDB
}

type trackingLocker struct {
	acquireCalls int
	releaseCalls int
	acquired     bool
	connSeen     bool
	acquireErr   error
	releaseErr   error
}

func (l *trackingLocker) Acquire(_ context.Context, conn *sql.Conn) error {
	l.acquireCalls++
	l.connSeen = conn != nil
	if l.acquireErr != nil {
		return l.acquireErr
	}
	l.acquired = true
	return nil
}

func (l *trackingLocker) Release(_ context.Context, conn *sql.Conn) error {
	l.releaseCalls++
	l.connSeen = l.connSeen || conn != nil
	if l.releaseErr != nil {
		return l.releaseErr
	}
	l.acquired = false
	return nil
}

func TestRunAppliesMigrationsInVersionOrderAndWritesFlywayHistory(t *testing.T) {
	db := openMigrationTestDB(t)
	locker := &trackingLocker{}
	installedAt := time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC)

	err := Run(context.Background(), db, Options{
		MigrationsFS: fstest.MapFS{
			"V2__seed_users.sql":   {Data: []byte("INSERT INTO users(id, name) VALUES (1, 'demo');")},
			"V1__create_users.sql": {Data: []byte("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);")},
		},
		Locker:      locker,
		InstalledBy: "server",
		Now: func() time.Time {
			return installedAt
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	var userCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&userCount); err != nil {
		t.Fatalf("count users failed: %v", err)
	}
	if userCount != 1 {
		t.Fatalf("user count=%d, want 1", userCount)
	}

	rows, err := db.Query("SELECT installed_rank, version, description, type, script, checksum, installed_by, execution_time, success FROM " + HistoryTableName + " ORDER BY installed_rank")
	if err != nil {
		t.Fatalf("query history failed: %v", err)
	}
	defer rows.Close()

	type historyRow struct {
		InstalledRank int
		Version       string
		Description   string
		Type          string
		Script        string
		Checksum      int
		InstalledBy   string
		ExecutionTime int
		Success       bool
	}

	var history []historyRow
	for rows.Next() {
		var row historyRow
		if err := rows.Scan(&row.InstalledRank, &row.Version, &row.Description, &row.Type, &row.Script, &row.Checksum, &row.InstalledBy, &row.ExecutionTime, &row.Success); err != nil {
			t.Fatalf("scan history failed: %v", err)
		}
		history = append(history, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate history failed: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history row count=%d, want 2", len(history))
	}
	if history[0].Version != "1" || history[0].Description != "create users" || history[0].Type != "SQL" || history[0].Script != "V1__create_users.sql" || !history[0].Success {
		t.Fatalf("unexpected first history row: %+v", history[0])
	}
	if history[1].Version != "2" || history[1].Description != "seed users" || history[1].Type != "SQL" || history[1].Script != "V2__seed_users.sql" || !history[1].Success {
		t.Fatalf("unexpected second history row: %+v", history[1])
	}
	if history[0].InstalledBy != "server" || history[1].InstalledBy != "server" {
		t.Fatalf("unexpected installed_by values: %+v", history)
	}
	if history[0].Checksum == 0 || history[1].Checksum == 0 {
		t.Fatalf("expected non-zero checksums, got %+v", history)
	}
	if !locker.connSeen || locker.acquireCalls != 1 || locker.releaseCalls != 1 || locker.acquired {
		t.Fatalf("unexpected locker state: %+v", locker)
	}

	assertHistoryColumns(t, db, []string{
		"installed_rank",
		"version",
		"description",
		"type",
		"script",
		"checksum",
		"installed_by",
		"installed_on",
		"execution_time",
		"success",
	})
}

func TestRunIsIdempotentWhenHistoryMatchesChecksums(t *testing.T) {
	db := openMigrationTestDB(t)
	options := Options{
		MigrationsFS: fstest.MapFS{
			"V1__create_users.sql": {Data: []byte("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);")},
			"V2__seed_users.sql":   {Data: []byte("INSERT INTO users(id, name) VALUES (1, 'demo');")},
		},
		Locker:      &trackingLocker{},
		InstalledBy: "server",
		Now:         time.Now,
	}

	if err := Run(context.Background(), db, options); err != nil {
		t.Fatalf("first Run returned error: %v", err)
	}
	if err := Run(context.Background(), db, options); err != nil {
		t.Fatalf("second Run returned error: %v", err)
	}

	var historyCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + HistoryTableName).Scan(&historyCount); err != nil {
		t.Fatalf("count history rows failed: %v", err)
	}
	if historyCount != 2 {
		t.Fatalf("history count=%d, want 2", historyCount)
	}

	var userCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&userCount); err != nil {
		t.Fatalf("count users failed: %v", err)
	}
	if userCount != 1 {
		t.Fatalf("user count=%d, want 1", userCount)
	}
}

func TestRunRejectsChecksumDrift(t *testing.T) {
	db := openMigrationTestDB(t)
	baseFS := fstest.MapFS{
		"V1__create_users.sql": {Data: []byte("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);")},
	}
	if err := Run(context.Background(), db, Options{MigrationsFS: baseFS, Locker: &trackingLocker{}, InstalledBy: "server", Now: time.Now}); err != nil {
		t.Fatalf("initial Run returned error: %v", err)
	}

	err := Run(context.Background(), db, Options{
		MigrationsFS: fstest.MapFS{
			"V1__create_users.sql": {Data: []byte("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, email TEXT);")},
		},
		Locker:      &trackingLocker{},
		InstalledBy: "server",
		Now:         time.Now,
	})
	if err == nil {
		t.Fatal("expected checksum drift error")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "V1__create_users.sql") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunRejectsFailedPriorMigration(t *testing.T) {
	db := openMigrationTestDB(t)
	if err := createHistoryTable(context.Background(), db); err != nil {
		t.Fatalf("create history table failed: %v", err)
	}
	_, err := db.Exec("INSERT INTO " + HistoryTableName + " (installed_rank, version, description, type, script, checksum, installed_by, installed_on, execution_time, success) VALUES (1, '1', 'create users', 'SQL', 'V1__create_users.sql', 123, 'server', CURRENT_TIMESTAMP, 1, 0)")
	if err != nil {
		t.Fatalf("insert failed history row failed: %v", err)
	}

	err = Run(context.Background(), db, Options{
		MigrationsFS: fstest.MapFS{
			"V1__create_users.sql": {Data: []byte("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);")},
			"V2__seed_users.sql":   {Data: []byte("INSERT INTO users(id, name) VALUES (1, 'demo');")},
		},
		Locker:      &trackingLocker{},
		InstalledBy: "server",
		Now:         time.Now,
	})
	if err == nil {
		t.Fatal("expected failed prior migration error")
	}
	if !strings.Contains(err.Error(), "failed migration") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDiscoverEmbeddedMigrationsIncludesNotificationDomainMigration(t *testing.T) {
	migrations, err := discoverMigrations(dbmigrations.Files)
	if err != nil {
		t.Fatalf("discover embedded migrations failed: %v", err)
	}
	if len(migrations) != 2 {
		t.Fatalf("embedded migration count=%d, want 2", len(migrations))
	}
	latest := migrations[len(migrations)-1]
	if latest.Version != 2 {
		t.Fatalf("latest migration version=%d, want 2", latest.Version)
	}
	if latest.Script != "V2__bootstrap_seed_data.sql" {
		t.Fatalf("latest migration script=%s, want V2__bootstrap_seed_data.sql", latest.Script)
	}
}

func TestDiscoverMigrationsRejectsUnexpectedNames(t *testing.T) {
	_, err := discoverMigrations(fstest.MapFS{
		"bad_name.sql":         {Data: []byte("SELECT 1;")},
		"V2__seed_users.sql":   {Data: []byte("SELECT 1;")},
		"V1__create_users.sql": {Data: []byte("SELECT 1;")},
	})
	if err == nil {
		t.Fatal("expected invalid migration name error")
	}
	if !strings.Contains(err.Error(), "bad_name.sql") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDiscoverMigrationsParsesEmbeddedMigrationFiles(t *testing.T) {
	migrations, err := discoverMigrations(dbmigrations.Files)
	if err != nil {
		t.Fatalf("discover embedded migrations failed: %v", err)
	}
	if len(migrations) != 2 {
		t.Fatalf("embedded migration count=%d, want 2", len(migrations))
	}
	if migrations[0].VersionText != "1" || migrations[0].Script != "V1__schema.sql" {
		t.Fatalf("unexpected first embedded migration: %+v", migrations[0])
	}
	if migrations[1].VersionText != "2" || migrations[1].Script != "V2__bootstrap_seed_data.sql" {
		t.Fatalf("unexpected second embedded migration: %+v", migrations[1])
	}
	if len(migrations[0].Statements) == 0 || len(migrations[1].Statements) == 0 {
		t.Fatalf("expected parsed statements for embedded migrations, got %+v", migrations)
	}
}

func TestEmbeddedSchemaIncludesTemplateParametersExtraTipColumn(t *testing.T) {
	content, err := fs.ReadFile(dbmigrations.Files, "V1__schema.sql")
	if err != nil {
		t.Fatalf("read V1 schema failed: %v", err)
	}
	text := string(content)
	// Verify extra_tip column exists in template_parameters table
	if !strings.Contains(text, "`extra_tip` text NULL") {
		t.Fatalf("expected V1 schema to contain extra_tip column in template_parameters")
	}
}

func TestEmbeddedSeedDataIncludesVLLMExtraTipValues(t *testing.T) {
	content, err := fs.ReadFile(dbmigrations.Files, "V2__bootstrap_seed_data.sql")
	if err != nil {
		t.Fatalf("read V2 seed data failed: %v", err)
	}
	text := string(content)
	// Verify V8's refined extra_tip texts are present in V2 seed data for vLLM parameters
	// These are the "影响范围" texts from V8 that were folded into V2
	expectedTips := []string{
		"影响范围：决定模型权重拆到多少张 GPU",          // tensor_parallel_size
		"影响范围：决定模型层被切成多少个执行阶段",          // pipeline_parallel_size
		"影响范围：决定推理精度、显存占用和兼容性",          // dtype
		"影响范围：决定 vLLM 可使用的单卡显存比例",       // gpu_memory_utilization
		"影响范围：决定可承载的上下文长度和 KV Cache 占用", // max_model_len
		"影响范围：决定模型加载时使用的量化后端",           // quantization
		"影响范围：决定权重文件按什么格式加载",            // load_format
		"影响范围：决定是否允许运行模型仓库中的自定义代码",      // trust_remote_code
	}
	for _, expected := range expectedTips {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected V2 seed data to contain folded extra_tip %q", expected)
		}
	}
	// Verify extra_tip column is explicitly listed in INSERT
	if !strings.Contains(text, "`extra_tip`") {
		t.Fatalf("expected V2 seed data INSERT to explicitly list extra_tip column")
	}
}

func TestEmbeddedCredentialLockStateMigrationDeclaresCredentialsLockColumn(t *testing.T) {
	content, err := fs.ReadFile(dbmigrations.Files, "V1__schema.sql")
	if err != nil {
		t.Fatalf("read V1 schema failed: %v", err)
	}
	text := string(content)
	for _, expected := range []string{"CREATE TABLE `credentials`", "lock_state", "unlocked"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected V1 schema to contain %q, got %s", expected, text)
		}
	}
}

func TestEmbeddedNotificationEventPreferencesMigrationDeclaresEventTypeColumn(t *testing.T) {
	content, err := fs.ReadFile(dbmigrations.Files, "V1__schema.sql")
	if err != nil {
		t.Fatalf("read V1 schema failed: %v", err)
	}
	text := string(content)
	for _, expected := range []string{"CREATE TABLE `notification_preferences`", "event_type", "rule_key"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected V1 schema to contain %q, got %s", expected, text)
		}
	}
}

func TestEmbeddedResourceOperationAuditMigrationDeclaresAuditTable(t *testing.T) {
	content, err := fs.ReadFile(dbmigrations.Files, "V1__schema.sql")
	if err != nil {
		t.Fatalf("read V1 schema failed: %v", err)
	}
	text := string(content)
	for _, expected := range []string{"resource_operation_audits", "target_kind", "resource_id"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected V1 schema to contain %q, got %s", expected, text)
		}
	}
}

func TestEmbeddedPipelineRunRecordFieldsMigrationDeclaresRunRecordColumns(t *testing.T) {
	content, err := fs.ReadFile(dbmigrations.Files, "V1__schema.sql")
	if err != nil {
		t.Fatalf("read V1 schema failed: %v", err)
	}
	text := string(content)
	for _, expected := range []string{"CREATE TABLE `pipeline_runs`", "run_config_json", "pipeline_snapshot_json", "resolved_nodes_json", "outputs_json", "bindings_snapshot_json", "events_json", "CREATE TABLE `pipelines`", "definition_json", "version"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected V1 schema to contain %q, got %s", expected, text)
		}
	}
}

func TestEmbeddedRestoreAgentLogChunksMigrationDeclaresAgentLogChunksTable(t *testing.T) {
	content, err := fs.ReadFile(dbmigrations.Files, "V1__schema.sql")
	if err != nil {
		t.Fatalf("read V1 schema failed: %v", err)
	}
	text := string(content)
	for _, expected := range []string{"CREATE TABLE `agent_log_chunks`", "task_id", "pipeline_run_id", "agent_id", "agent_session_id", "attempt", "seq", "stream", "chunk", "timestamp", "unique_key"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected V1 schema to contain %q, got %s", expected, text)
		}
	}
}

func TestCalculateChecksumUsesSigned32BitRange(t *testing.T) {
	content, err := fs.ReadFile(dbmigrations.Files, "V1__schema.sql")
	if err != nil {
		t.Fatalf("read V1 schema failed: %v", err)
	}

	got := calculateChecksum(content)
	if got >= 0 {
		t.Fatalf("checksum=%d, want signed 32-bit overflow result below zero", got)
	}
}

func assertHistoryColumns(t *testing.T, db *sql.DB, expected []string) {
	t.Helper()

	rows, err := db.Query("PRAGMA table_info(" + HistoryTableName + ")")
	if err != nil {
		t.Fatalf("query history table info failed: %v", err)
	}
	defer rows.Close()

	columns := make([]string, 0, len(expected))
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan history table info failed: %v", err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate history table info failed: %v", err)
	}

	for _, column := range expected {
		if !contains(columns, column) {
			t.Fatalf("expected history table column %q in %v", column, columns)
		}
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

var _ fs.FS = fstest.MapFS{}
