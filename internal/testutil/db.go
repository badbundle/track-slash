package testutil

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

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
