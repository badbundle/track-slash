package store_test

import (
	"strings"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/bradleymackey/track-slash/internal/migrations"
	"github.com/bradleymackey/track-slash/internal/testutil"
)

// Migration 0056 lets attachment copies share their original's key. Rolling
// it back drops the copies and queues the deletion of bytes only a copy kept.
func TestStorageObjectCopiesMigrationRollback(t *testing.T) {
	db := testutil.NewEmptyDatabase(t)
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("goose.SetDialect: %v", err)
	}
	if err := goose.UpTo(db.SQL, ".", 56); err != nil {
		t.Fatalf("goose.UpTo(56): %v", err)
	}
	var userID, projectID string
	if err := db.SQL.QueryRow(`
		INSERT INTO users (email, name, username) VALUES ('copies@example.com', 'Copies', 'copies') RETURNING id
	`).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := db.SQL.QueryRow(`INSERT INTO projects (key, name, owner_id) VALUES ('COPY', 'Copy', $1) RETURNING id`, userID).Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	insert := func(number int, key string, copiedFrom any) string {
		t.Helper()
		var id string
		if err := db.SQL.QueryRow(`
			INSERT INTO storage_objects (project_id, number, backend, bucket, object_key, filename, content_type, byte_size, sha256, created_by_id, copied_from_id)
			VALUES ($1, $2, 'local', 'local', $3, 'a.png', 'image/png', 1, $4, $5, $6)
			RETURNING id
		`, projectID, number, key, strings.Repeat("a", 64), userID, copiedFrom).Scan(&id); err != nil {
			t.Fatalf("insert object %d: %v", number, err)
		}
		return id
	}
	original := insert(1, "projects/p/objects/shared", nil)
	insert(2, "projects/p/objects/shared", original)
	kept := insert(3, "projects/p/objects/kept", nil)
	// Uploads still can't share a key.
	if _, err := db.SQL.Exec(`
		INSERT INTO storage_objects (project_id, number, backend, bucket, object_key, filename, content_type, byte_size, sha256, created_by_id)
		VALUES ($1, 9, 'local', 'local', 'projects/p/objects/kept', 'a.png', 'image/png', 1, $2, $3)
	`, projectID, strings.Repeat("a", 64), userID); err == nil {
		t.Fatal("a second upload took an existing key")
	}
	// The original is deleted and its job completed as kept, because the
	// copy still used the bytes.
	if _, err := db.SQL.Exec(`UPDATE storage_objects SET deleted_at = now() WHERE id = $1`, original); err != nil {
		t.Fatalf("delete original: %v", err)
	}
	if _, err := db.SQL.Exec(`DELETE FROM storage_object_deletions WHERE storage_object_id = $1`, original); err != nil {
		t.Fatalf("complete kept job: %v", err)
	}

	if err := goose.DownTo(db.SQL, ".", 55); err != nil {
		t.Fatalf("goose.DownTo(55): %v", err)
	}
	var copies, jobs int
	if err := db.SQL.QueryRow(`SELECT count(*) FROM storage_objects WHERE object_key = 'projects/p/objects/shared'`).Scan(&copies); err != nil || copies != 1 {
		t.Fatalf("rows for the shared key after rollback = %d, %v", copies, err)
	}
	if err := db.SQL.QueryRow(`SELECT count(*) FROM storage_object_deletions WHERE storage_object_id = $1 AND status = 'pending'`, original).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("requeued deletions for the original = %d, %v", jobs, err)
	}
	if err := db.SQL.QueryRow(`SELECT count(*) FROM storage_object_deletions WHERE storage_object_id = $1`, kept).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("deletions for a live upload = %d, %v", jobs, err)
	}
	if _, err := db.SQL.Exec(`
		INSERT INTO storage_objects (project_id, number, backend, bucket, object_key, filename, content_type, byte_size, sha256, created_by_id)
		VALUES ($1, 9, 'local', 'local', 'projects/p/objects/kept', 'a.png', 'image/png', 1, $2, $3)
	`, projectID, strings.Repeat("a", 64), userID); err == nil {
		t.Fatal("the unique key constraint is missing after rollback")
	}
	if err := goose.UpTo(db.SQL, ".", 56); err != nil {
		t.Fatalf("goose.UpTo(56) again: %v", err)
	}
}
