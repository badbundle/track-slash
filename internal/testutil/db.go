package testutil

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/bradleymackey/track-slash/internal/migrations"
)

const (
	maxPostgresIdentifierLen = 63
	testDatabaseTimeout      = 30 * time.Second
)

var activeTemplate struct {
	sync.Mutex
	manager *templateManager
}

// Database is an isolated Postgres database for one test.
type Database struct {
	URL  string
	SQL  *sql.DB
	Pool *pgxpool.Pool

	name    string
	manager *templateManager
}

// RunWithMigratedTemplate prepares a migrated template database for a test
// package, runs the package tests, then drops the template. If no test
// database URL is configured, it leaves setup to per-test skip checks.
func RunWithMigratedTemplate(m *testing.M) int {
	baseURL := testDatabaseURL()
	if baseURL == "" {
		return m.Run()
	}

	manager, err := newTemplateManager(baseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "test database template setup: %v\n", err)
		return 1
	}

	activeTemplate.Lock()
	activeTemplate.manager = manager
	activeTemplate.Unlock()

	code := m.Run()

	activeTemplate.Lock()
	activeTemplate.manager = nil
	activeTemplate.Unlock()

	if err := manager.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "test database template cleanup: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

// NewMigratedDatabase clones the migrated package template into a fresh
// database and registers cleanup to drop it after the test.
func NewMigratedDatabase(t testing.TB) *Database {
	t.Helper()
	manager := requireTemplateManager(t)
	return manager.newDatabase(t, true)
}

// NewEmptyDatabase creates an empty fresh database for tests that need to
// exercise migration from scratch.
func NewEmptyDatabase(t testing.TB) *Database {
	t.Helper()
	manager := requireTemplateManager(t)
	return manager.newDatabase(t, false)
}

// CleanDatabase removes all application rows while preserving migration state.
func CleanDatabase(t testing.TB, db *sql.DB) {
	t.Helper()

	rows, err := db.Query(`
		SELECT tablename
		FROM pg_tables
		WHERE schemaname = 'public'
		  AND tablename <> 'goose_db_version'
		ORDER BY tablename
	`)
	if err != nil {
		t.Fatalf("list tables for cleanup: %v", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatalf("scan table for cleanup: %v", err)
		}
		tables = append(tables, quoteIdent(table))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate tables for cleanup: %v", err)
	}
	if len(tables) == 0 {
		return
	}

	q := fmt.Sprintf("TRUNCATE TABLE %s RESTART IDENTITY CASCADE", strings.Join(tables, ", "))
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("clean database: %v", err)
	}
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

type templateManager struct {
	baseURL      string
	adminDB      *sql.DB
	templateName string
	prefix       string
	seq          atomic.Uint64
	mu           sync.Mutex
}

func newTemplateManager(baseURL string) (*templateManager, error) {
	baseName, err := databaseNameFromURL(baseURL)
	if err != nil {
		return nil, err
	}
	adminURL, err := databaseURLWithName(baseURL, "postgres")
	if err != nil {
		return nil, err
	}

	adminDB, err := sql.Open("pgx", adminURL)
	if err != nil {
		return nil, fmt.Errorf("open admin database: %w", err)
	}
	adminDB.SetMaxOpenConns(1)
	adminDB.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), testDatabaseTimeout)
	defer cancel()
	if err := adminDB.PingContext(ctx); err != nil {
		_ = adminDB.Close()
		return nil, fmt.Errorf("ping admin database: %w", err)
	}

	prefix := sanitizeDatabaseNamePrefix(baseName)
	manager := &templateManager{
		baseURL:      baseURL,
		adminDB:      adminDB,
		templateName: buildDatabaseName(prefix, "template", strconv.Itoa(os.Getpid()), strconv.FormatInt(time.Now().UnixNano(), 36)),
		prefix:       prefix,
	}
	if err := manager.createTemplate(); err != nil {
		_ = adminDB.Close()
		return nil, err
	}
	return manager, nil
}

func (m *templateManager) createTemplate() error {
	if err := m.createDatabase(m.templateName, ""); err != nil {
		return fmt.Errorf("create template database %s: %w", m.templateName, err)
	}

	templateURL, err := databaseURLWithName(m.baseURL, m.templateName)
	if err != nil {
		_ = m.dropDatabase(m.templateName)
		return err
	}
	templateDB, err := sql.Open("pgx", templateURL)
	if err != nil {
		_ = m.dropDatabase(m.templateName)
		return fmt.Errorf("open template database: %w", err)
	}
	templateDB.SetMaxOpenConns(1)
	templateDB.SetMaxIdleConns(1)
	if err := migrations.Up(templateDB); err != nil {
		_ = templateDB.Close()
		_ = m.dropDatabase(m.templateName)
		return fmt.Errorf("migrate template database: %w", err)
	}
	if err := templateDB.Close(); err != nil {
		_ = m.dropDatabase(m.templateName)
		return fmt.Errorf("close template database: %w", err)
	}
	if err := m.terminateConnections(m.templateName); err != nil {
		_ = m.dropDatabase(m.templateName)
		return fmt.Errorf("terminate template connections: %w", err)
	}
	return nil
}

func (m *templateManager) newDatabase(t testing.TB, migrated bool) *Database {
	t.Helper()

	kind := "test"
	if !migrated {
		kind = "empty"
	}
	dbName := buildDatabaseName(m.prefix, kind, strconv.Itoa(os.Getpid()), strconv.FormatUint(m.seq.Add(1), 36))
	templateName := ""
	if migrated {
		templateName = m.templateName
	}
	if err := m.createDatabase(dbName, templateName); err != nil {
		t.Fatalf("create ephemeral database %s: %v", dbName, err)
	}

	dbURL, err := databaseURLWithName(m.baseURL, dbName)
	if err != nil {
		_ = m.dropDatabase(dbName)
		t.Fatalf("build ephemeral database URL: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), testDatabaseTimeout)
	defer cancel()

	sqlDB, err := sql.Open("pgx", dbURL)
	if err != nil {
		_ = m.dropDatabase(dbName)
		t.Fatalf("sql.Open ephemeral database: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		_ = m.dropDatabase(dbName)
		t.Fatalf("ping ephemeral database: %v", err)
	}

	poolConfig, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		_ = sqlDB.Close()
		_ = m.dropDatabase(dbName)
		t.Fatalf("pgxpool.ParseConfig ephemeral database: %v", err)
	}
	poolConfig.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		_ = sqlDB.Close()
		_ = m.dropDatabase(dbName)
		t.Fatalf("pgxpool.New ephemeral database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		_ = sqlDB.Close()
		_ = m.dropDatabase(dbName)
		t.Fatalf("pgxpool.Ping ephemeral database: %v", err)
	}

	db := &Database{URL: dbURL, SQL: sqlDB, Pool: pool, name: dbName, manager: m}
	t.Cleanup(func() {
		db.Pool.Close()
		if err := db.SQL.Close(); err != nil {
			t.Errorf("close ephemeral database SQL handle: %v", err)
		}
		if err := db.manager.dropDatabase(db.name); err != nil {
			t.Errorf("drop ephemeral database %s: %v", db.name, err)
		}
	})
	return db
}

func (m *templateManager) createDatabase(name, templateName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), testDatabaseTimeout)
	defer cancel()

	if templateName != "" {
		if err := m.terminateConnections(templateName); err != nil {
			return err
		}
		_, err := m.adminDB.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", quoteIdent(name), quoteIdent(templateName)))
		return err
	}

	_, err := m.adminDB.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s", quoteIdent(name)))
	return err
}

func (m *templateManager) dropDatabase(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), testDatabaseTimeout)
	defer cancel()

	if err := m.terminateConnections(name); err != nil {
		return err
	}
	_, err := m.adminDB.ExecContext(ctx, "DROP DATABASE IF EXISTS "+quoteIdent(name))
	return err
}

func (m *templateManager) terminateConnections(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), testDatabaseTimeout)
	defer cancel()

	_, err := m.adminDB.ExecContext(ctx, `
		SELECT pg_terminate_backend(pid)
		FROM pg_stat_activity
		WHERE datname = $1
		  AND pid <> pg_backend_pid()
	`, name)
	return err
}

func (m *templateManager) Close() error {
	var closeErr error
	if err := m.dropDatabase(m.templateName); err != nil {
		closeErr = err
	}
	if err := m.adminDB.Close(); closeErr == nil && err != nil {
		closeErr = err
	}
	return closeErr
}

func requireTemplateManager(t testing.TB) *templateManager {
	t.Helper()

	if testDatabaseURL() == "" {
		t.Skip("TEST_DATABASE_URL / DATABASE_URL not set; skipping integration test")
	}

	activeTemplate.Lock()
	manager := activeTemplate.manager
	activeTemplate.Unlock()
	if manager == nil {
		t.Fatal("testutil.RunWithMigratedTemplate must be called from TestMain before NewMigratedDatabase/NewEmptyDatabase")
	}
	return manager
}

func testDatabaseURL() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return os.Getenv("DATABASE_URL")
}

func databaseNameFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse database URL: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", fmt.Errorf("unsupported database URL scheme %q", u.Scheme)
	}
	name, err := url.PathUnescape(strings.TrimPrefix(u.Path, "/"))
	if err != nil {
		return "", fmt.Errorf("parse database name: %w", err)
	}
	if name == "" {
		return "", fmt.Errorf("database URL must include a database name")
	}
	return name, nil
}

func databaseURLWithName(rawURL, dbName string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse database URL: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", fmt.Errorf("unsupported database URL scheme %q", u.Scheme)
	}
	u.Path = "/" + url.PathEscape(dbName)
	u.RawPath = ""
	u.Fragment = ""
	return u.String(), nil
}

func sanitizeDatabaseNamePrefix(raw string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(raw) {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		out = "db"
	}
	if out[0] >= '0' && out[0] <= '9' {
		out = "db_" + out
	}
	return out
}

func buildDatabaseName(parts ...string) string {
	name := strings.Join(parts, "_")
	if len(name) <= maxPostgresIdentifierLen {
		return name
	}

	suffixParts := parts[1:]
	suffix := "_" + strings.Join(suffixParts, "_")
	prefixLen := maxPostgresIdentifierLen - len(suffix)
	if prefixLen < 1 {
		return name[:maxPostgresIdentifierLen]
	}
	return parts[0][:min(len(parts[0]), prefixLen)] + suffix
}
