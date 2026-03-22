package migrations

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	dbmigrations "easydo-server/db/migrations"
	"easydo-server/internal/config"

	_ "github.com/go-sql-driver/mysql"
)

// 保持 Flyway 兼容的历史表命名，便于沿用成熟的版本化迁移语义，而不引入外部组件。
const HistoryTableName = "flyway_schema_history"

const (
	defaultLockName      = "easydo-server:flyway_schema_history"
	defaultLockTimeout   = 2 * time.Minute
	defaultOpenAttempts  = 30
	defaultOpenRetryWait = time.Second
	defaultInstalledBy   = "easydo-server"
)

var migrationNamePattern = regexp.MustCompile(`^V([0-9]+)__(.+)\.sql$`)

type Locker interface {
	Acquire(ctx context.Context, conn *sql.Conn) error
	Release(ctx context.Context, conn *sql.Conn) error
}

type Options struct {
	MigrationsFS fs.FS
	Locker       Locker
	InstalledBy  string
	LockName     string
	LockTimeout  time.Duration
	Now          func() time.Time
	DriverName   string
}

type migration struct {
	Version     int
	VersionText string
	Description string
	Script      string
	Checksum    int
	Content     string
	Statements  []string
}

type appliedMigration struct {
	InstalledRank int
	Version       string
	Description   string
	Type          string
	Script        string
	Checksum      sql.NullInt64
	Success       bool
	InstalledBy   string
	ExecutionTime int
	InstalledOn   time.Time
}

type mysqlLocker struct {
	name          string
	timeoutSecond int
}

func Run(ctx context.Context, db *sql.DB, opts Options) error {
	if db == nil {
		return errors.New("migration db is nil")
	}

	migrationsFS := opts.MigrationsFS
	if migrationsFS == nil {
		migrationsFS = dbmigrations.Files
	}

	migrations, err := discoverMigrations(migrationsFS)
	if err != nil {
		return err
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open migration connection: %w", err)
	}
	defer conn.Close()

	locker := opts.Locker
	if locker == nil {
		locker = defaultLocker(opts)
	}
	// 多副本同时启动时，先通过数据库锁串行化迁移，避免重复执行同一版本 SQL。
	if err := locker.Acquire(ctx, conn); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		_ = locker.Release(ctx, conn)
	}()

	if err := createHistoryTableOnConn(ctx, conn); err != nil {
		return err
	}

	history, err := loadAppliedMigrations(ctx, conn)
	if err != nil {
		return err
	}
	if err := validateAppliedHistory(history, migrations); err != nil {
		return err
	}

	installedBy := strings.TrimSpace(opts.InstalledBy)
	if installedBy == "" {
		installedBy = defaultInstalledBy
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	nextRank := nextInstalledRank(history)
	for _, item := range migrations {
		if applied, ok := history[item.VersionText]; ok {
			if applied.Checksum.Valid && int(applied.Checksum.Int64) != item.Checksum {
				return fmt.Errorf("checksum mismatch for %s: database=%d file=%d", item.Script, applied.Checksum.Int64, item.Checksum)
			}
			continue
		}

		startedAt := time.Now()
		if err := applyStatements(ctx, conn, item.Statements); err != nil {
			insertErr := insertHistoryRow(ctx, conn, historyInsertParams{
				InstalledRank: nextRank,
				Version:       item.VersionText,
				Description:   item.Description,
				Type:          "SQL",
				Script:        item.Script,
				Checksum:      item.Checksum,
				InstalledBy:   installedBy,
				InstalledOn:   now(),
				ExecutionTime: int(time.Since(startedAt).Milliseconds()),
				Success:       false,
			})
			if insertErr != nil {
				return fmt.Errorf("apply migration %s: %w (record failure: %v)", item.Script, err, insertErr)
			}
			return fmt.Errorf("apply migration %s: %w", item.Script, err)
		}

		if err := insertHistoryRow(ctx, conn, historyInsertParams{
			InstalledRank: nextRank,
			Version:       item.VersionText,
			Description:   item.Description,
			Type:          "SQL",
			Script:        item.Script,
			Checksum:      item.Checksum,
			InstalledBy:   installedBy,
			InstalledOn:   now(),
			ExecutionTime: int(time.Since(startedAt).Milliseconds()),
			Success:       true,
		}); err != nil {
			return fmt.Errorf("record migration %s: %w", item.Script, err)
		}
		nextRank++
	}

	return nil
}

func RunEmbeddedFromConfig(ctx context.Context) error {
	db, err := openMigrationDB(config.GetDSN(), defaultOpenAttempts, defaultOpenRetryWait)
	if err != nil {
		return err
	}
	defer db.Close()

	return Run(ctx, db, Options{
		MigrationsFS: dbmigrations.Files,
		InstalledBy:  config.Config.GetString("database.username"),
		DriverName:   "mysql",
	})
}

func openMigrationDB(dsn string, attempts int, delay time.Duration) (*sql.DB, error) {
	var db *sql.DB
	err := retry(attempts, delay, func() error {
		candidate, openErr := sql.Open("mysql", dsn)
		if openErr != nil {
			return openErr
		}
		if pingErr := candidate.Ping(); pingErr != nil {
			_ = candidate.Close()
			return pingErr
		}
		db = candidate
		return nil
	})
	if err != nil {
		return nil, err
	}
	return db, nil
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

func defaultLocker(opts Options) Locker {
	lockName := strings.TrimSpace(opts.LockName)
	if lockName == "" {
		lockName = defaultLockName
	}
	timeout := opts.LockTimeout
	if timeout <= 0 {
		timeout = defaultLockTimeout
	}
	if strings.EqualFold(opts.DriverName, "mysql") {
		return mysqlLocker{name: lockName, timeoutSecond: int(timeout.Seconds())}
	}
	return noopLocker{}
}

func (l mysqlLocker) Acquire(ctx context.Context, conn *sql.Conn) error {
	var result sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", l.name, l.timeoutSecond).Scan(&result); err != nil {
		return err
	}
	if !result.Valid || result.Int64 != 1 {
		return fmt.Errorf("GET_LOCK returned %v", result)
	}
	return nil
}

func (l mysqlLocker) Release(ctx context.Context, conn *sql.Conn) error {
	var result sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", l.name).Scan(&result); err != nil {
		return err
	}
	if !result.Valid || result.Int64 != 1 {
		return fmt.Errorf("RELEASE_LOCK returned %v", result)
	}
	return nil
}

type noopLocker struct{}

func (noopLocker) Acquire(context.Context, *sql.Conn) error { return nil }
func (noopLocker) Release(context.Context, *sql.Conn) error { return nil }

func discoverMigrations(migrationsFS fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, ".")
	if err != nil {
		return nil, fmt.Errorf("read migration directory: %w", err)
	}

	migrations := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		matches := migrationNamePattern.FindStringSubmatch(name)
		if matches == nil {
			return nil, fmt.Errorf("invalid migration filename: %s", name)
		}

		content, err := fs.ReadFile(migrationsFS, name)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		version, err := strconv.Atoi(matches[1])
		if err != nil {
			return nil, fmt.Errorf("parse migration version for %s: %w", name, err)
		}

		statements, err := splitStatements(string(content))
		if err != nil {
			return nil, fmt.Errorf("split migration %s: %w", name, err)
		}
		if len(statements) == 0 {
			return nil, fmt.Errorf("migration %s is empty", name)
		}

		migrations = append(migrations, migration{
			Version:     version,
			VersionText: matches[1],
			Description: strings.ReplaceAll(matches[2], "_", " "),
			Script:      filepath.Base(name),
			Checksum:    calculateChecksum(content),
			Content:     string(content),
			Statements:  statements,
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	for i := 1; i < len(migrations); i++ {
		if migrations[i-1].Version == migrations[i].Version {
			return nil, fmt.Errorf("duplicate migration version: %s and %s", migrations[i-1].Script, migrations[i].Script)
		}
	}

	return migrations, nil
}

func calculateChecksum(content []byte) int {
	crc := crc32.NewIEEE()
	reader := bufio.NewReader(bytes.NewReader(content))
	firstLine := true
	for {
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			panic(err)
		}
		line = strings.TrimRight(line, "\r\n")
		if firstLine {
			line = strings.TrimPrefix(line, "\uFEFF")
			firstLine = false
		}
		if len(line) > 0 {
			_, _ = crc.Write([]byte(line))
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	return int(int32(crc.Sum32()))
}

func splitStatements(sqlText string) ([]string, error) {
	statements := make([]string, 0)
	var current strings.Builder
	inSingle := false
	inDouble := false
	inBacktick := false
	inLineComment := false
	inBlockComment := false
	text := strings.ReplaceAll(sqlText, "\r\n", "\n")

	for i := 0; i < len(text); i++ {
		ch := text[i]
		next := byte(0)
		if i+1 < len(text) {
			next = text[i+1]
		}

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
			}
			continue
		}
		if inBlockComment {
			if ch == '*' && next == '/' {
				i++
				inBlockComment = false
			}
			continue
		}

		if !inSingle && !inDouble && !inBacktick {
			if ch == '-' && next == '-' {
				inLineComment = true
				i++
				continue
			}
			if ch == '/' && next == '*' {
				inBlockComment = true
				i++
				continue
			}
		}

		current.WriteByte(ch)

		switch ch {
		case '\'':
			if !inDouble && !inBacktick {
				if inSingle {
					if next == '\'' {
						current.WriteByte(next)
						i++
						continue
					}
					if i > 0 && text[i-1] == '\\' {
						continue
					}
				}
				inSingle = !inSingle
			}
		case '"':
			if !inSingle && !inBacktick {
				inDouble = !inDouble
			}
		case '`':
			if !inSingle && !inDouble {
				inBacktick = !inBacktick
			}
		case ';':
			if !inSingle && !inDouble && !inBacktick {
				statement := strings.TrimSpace(current.String())
				if statement != "" {
					statements = append(statements, statement)
				}
				current.Reset()
			}
		}
	}

	if inSingle || inDouble || inBacktick || inBlockComment {
		return nil, errors.New("unterminated SQL statement")
	}

	tail := strings.TrimSpace(current.String())
	if tail != "" {
		statements = append(statements, tail)
	}
	return filterExecutableStatements(statements), nil
}

func filterExecutableStatements(statements []string) []string {
	filtered := make([]string, 0, len(statements))
	for _, statement := range statements {
		trimmed := strings.TrimSpace(statement)
		if trimmed == "" {
			continue
		}
		filtered = append(filtered, trimmed)
	}
	return filtered
}

func createHistoryTable(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("migration db is nil")
	}
	_, err := db.ExecContext(ctx, historyTableDDL())
	if err != nil {
		return fmt.Errorf("create %s: %w", HistoryTableName, err)
	}
	return nil
}

func createHistoryTableOnConn(ctx context.Context, conn *sql.Conn) error {
	_, err := conn.ExecContext(ctx, historyTableDDL())
	if err != nil {
		return fmt.Errorf("create %s: %w", HistoryTableName, err)
	}
	return nil
}

func historyTableDDL() string {
	return `CREATE TABLE IF NOT EXISTS flyway_schema_history (
		installed_rank INT NOT NULL,
		version VARCHAR(50),
		description VARCHAR(200) NOT NULL,
		type VARCHAR(20) NOT NULL,
		script VARCHAR(1000) NOT NULL,
		checksum INT,
		installed_by VARCHAR(100) NOT NULL,
		installed_on TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		execution_time INT NOT NULL,
		success BOOLEAN NOT NULL,
		PRIMARY KEY (installed_rank)
	)`
}

func loadAppliedMigrations(ctx context.Context, conn *sql.Conn) (map[string]appliedMigration, error) {
	rows, err := conn.QueryContext(ctx, "SELECT installed_rank, version, description, type, script, checksum, installed_by, installed_on, execution_time, success FROM "+HistoryTableName+" ORDER BY installed_rank")
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", HistoryTableName, err)
	}
	defer rows.Close()

	history := make(map[string]appliedMigration)
	for rows.Next() {
		var item appliedMigration
		if err := rows.Scan(&item.InstalledRank, &item.Version, &item.Description, &item.Type, &item.Script, &item.Checksum, &item.InstalledBy, &item.InstalledOn, &item.ExecutionTime, &item.Success); err != nil {
			return nil, fmt.Errorf("scan %s: %w", HistoryTableName, err)
		}
		if _, exists := history[item.Version]; exists {
			return nil, fmt.Errorf("duplicate migration version in %s: %s", HistoryTableName, item.Version)
		}
		history[item.Version] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", HistoryTableName, err)
	}
	return history, nil
}

func validateAppliedHistory(history map[string]appliedMigration, migrations []migration) error {
	for _, applied := range history {
		if !applied.Success {
			return fmt.Errorf("failed migration already recorded in %s: version=%s script=%s", HistoryTableName, applied.Version, applied.Script)
		}
	}
	available := make(map[string]migration, len(migrations))
	for _, item := range migrations {
		available[item.VersionText] = item
	}
	for version, applied := range history {
		item, ok := available[version]
		if !ok {
			continue
		}
		if applied.Checksum.Valid && int(applied.Checksum.Int64) != item.Checksum {
			return fmt.Errorf("checksum mismatch for %s: database=%d file=%d", item.Script, applied.Checksum.Int64, item.Checksum)
		}
	}
	return nil
}

func nextInstalledRank(history map[string]appliedMigration) int {
	next := 1
	for _, item := range history {
		if item.InstalledRank >= next {
			next = item.InstalledRank + 1
		}
	}
	return next
}

func applyStatements(ctx context.Context, conn *sql.Conn, statements []string) error {
	for _, statement := range statements {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

type historyInsertParams struct {
	InstalledRank int
	Version       string
	Description   string
	Type          string
	Script        string
	Checksum      int
	InstalledBy   string
	InstalledOn   time.Time
	ExecutionTime int
	Success       bool
}

func insertHistoryRow(ctx context.Context, conn *sql.Conn, params historyInsertParams) error {
	_, err := conn.ExecContext(ctx, "INSERT INTO "+HistoryTableName+" (installed_rank, version, description, type, script, checksum, installed_by, installed_on, execution_time, success) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", params.InstalledRank, params.Version, params.Description, params.Type, params.Script, params.Checksum, params.InstalledBy, params.InstalledOn, params.ExecutionTime, params.Success)
	return err
}
