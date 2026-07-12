package store_test

import (
	"database/sql"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/bradleymackey/track-slash/internal/migrations"
	"github.com/bradleymackey/track-slash/internal/testutil"
)

func TestContextPagesMigrationBackfillAndRollback(t *testing.T) {
	db := testutil.NewEmptyDatabase(t)
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("goose.SetDialect: %v", err)
	}
	if err := goose.UpTo(db.SQL, ".", 28); err != nil {
		t.Fatalf("goose.UpTo(28): %v", err)
	}

	var userID, projectID string
	if err := db.SQL.QueryRow(`
		INSERT INTO users (email, name, username)
		VALUES ('context-migration@example.com', 'Context Migration', 'context-migration')
		RETURNING id
	`).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := db.SQL.QueryRow(`
		INSERT INTO projects (key, name, owner_id)
		VALUES ('CTXMIG', 'Context Migration', $1)
		RETURNING id
	`, userID).Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	for _, record := range []struct {
		number int
		scope  string
		title  string
	}{
		{number: 3, scope: "project", title: "Later page"},
		{number: 1, scope: "project", title: "First page"},
		{number: 2, scope: "issue", title: "Issue note"},
	} {
		if _, err := db.SQL.Exec(`
			INSERT INTO project_context
				(project_id, number, scope, title, body, created_by_id, updated_by_id)
			VALUES ($1, $2, $3, $4, 'legacy body', $5, $5)
		`, projectID, record.number, record.scope, record.title, userID); err != nil {
			t.Fatalf("insert %s: %v", record.title, err)
		}
	}

	if err := goose.UpTo(db.SQL, ".", 29); err != nil {
		t.Fatalf("goose.UpTo(29): %v", err)
	}

	rows, err := db.SQL.Query(`
		SELECT number, position
		FROM project_context
		WHERE project_id = $1 AND scope = 'project'
		ORDER BY number
	`, projectID)
	if err != nil {
		t.Fatalf("list migrated project context: %v", err)
	}
	defer rows.Close()
	for index, number := range []int{1, 3} {
		if !rows.Next() {
			t.Fatalf("missing project context row %d", number)
		}
		var gotNumber, gotPosition int
		if err := rows.Scan(&gotNumber, &gotPosition); err != nil {
			t.Fatalf("scan project context row: %v", err)
		}
		if gotNumber != number || gotPosition != index+1 {
			t.Fatalf("project context row = number %d position %d, want number %d position %d", gotNumber, gotPosition, number, index+1)
		}
	}
	if rows.Next() {
		t.Fatal("unexpected additional project context row")
	}

	var issuePosition sql.NullInt64
	if err := db.SQL.QueryRow(`
		SELECT position FROM project_context
		WHERE project_id = $1 AND scope = 'issue'
	`, projectID).Scan(&issuePosition); err != nil {
		t.Fatalf("read migrated issue context: %v", err)
	}
	if issuePosition.Valid {
		t.Fatalf("issue context position = %d, want NULL", issuePosition.Int64)
	}

	if _, err := db.SQL.Exec(`
		INSERT INTO project_context
			(project_id, number, scope, position, title, body, content_type, created_by_id, updated_by_id)
		VALUES ($1, 4, 'project', 3, 'Blank page', '', 'text/markdown; charset=utf-8', $2, $2)
	`, projectID, userID); err != nil {
		t.Fatalf("insert blank project page: %v", err)
	}
	if _, err := db.SQL.Exec(`
		INSERT INTO project_context
			(project_id, number, scope, title, body, created_by_id, updated_by_id)
		VALUES ($1, 5, 'issue', 'Blank issue note', '', $2, $2)
	`, projectID, userID); err == nil {
		t.Fatal("blank issue context insert succeeded")
	}

	if err := goose.DownTo(db.SQL, ".", 28); err != nil {
		t.Fatalf("goose.DownTo(28): %v", err)
	}
	var positionColumnCount, attachmentTableCount int
	if err := db.SQL.QueryRow(`
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'project_context' AND column_name = 'position'
	`).Scan(&positionColumnCount); err != nil {
		t.Fatalf("check rolled back position: %v", err)
	}
	if err := db.SQL.QueryRow(`
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = 'context_attachments'
	`).Scan(&attachmentTableCount); err != nil {
		t.Fatalf("check rolled back attachments: %v", err)
	}
	if positionColumnCount != 0 || attachmentTableCount != 0 {
		t.Fatalf("rollback left position columns=%d attachment tables=%d", positionColumnCount, attachmentTableCount)
	}
	var blankBody string
	if err := db.SQL.QueryRow(`
		SELECT body FROM project_context WHERE project_id = $1 AND number = 4
	`, projectID).Scan(&blankBody); err != nil {
		t.Fatalf("read rolled back blank body: %v", err)
	}
	if blankBody != " " {
		t.Fatalf("rolled back blank body = %q, want one space", blankBody)
	}
}
