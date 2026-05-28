package realtime

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bradleymackey/track-slash/internal/migrations"
)

func testDatabaseURL() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return os.Getenv("DATABASE_URL")
}

// TestListenerReceivesEventFromIssueInsert exercises the full pipeline:
// trigger fires pg_notify, Listener decodes payload, Hub fans out to a
// client subscribed to the project's topic.
func TestListenerReceivesEventFromIssueInsert(t *testing.T) {
	dbURL := testDatabaseURL()
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL / DATABASE_URL not set; skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sqlDB, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := migrations.Up(sqlDB); err != nil {
		t.Fatalf("migrations.Up: %v", err)
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	hub := NewHub()
	listener := NewListener(dbURL, hub)

	listenerCtx, stopListener := context.WithCancel(ctx)
	t.Cleanup(stopListener)
	go listener.Run(listenerCtx)

	// Wait for the listener's LISTEN to register before producing events
	// the test expects to observe.
	time.Sleep(500 * time.Millisecond)

	var projectID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects (key, name) VALUES ($1, $2) RETURNING id::text
	`, uniqueKey(t), "rt-test").Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	// The project insert itself fires an event; subscribe after creating it
	// so we only observe the subsequent issue insert.
	c := newTestClient(8)
	hub.Subscribe(c, "project:"+projectID)

	if _, err := pool.Exec(ctx, `
		INSERT INTO issues (project_id, number, title) VALUES ($1, 1, 'hello')
	`, projectID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}

	select {
	case ev := <-c.send:
		if ev.Op != OpInsert {
			t.Errorf("op = %s, want insert", ev.Op)
		}
		if ev.Entity != EntityIssue {
			t.Errorf("entity = %s, want issue", ev.Entity)
		}
		if ev.ProjectID == nil || ev.ProjectID.String() != projectID {
			t.Errorf("project_id = %v, want %s", ev.ProjectID, projectID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("did not receive event within 3s")
	}
}

func uniqueKey(t *testing.T) string {
	t.Helper()
	return "rt_" + time.Now().Format("150405.000000")
}
