package store_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/bradleymackey/track-slash/internal/migrations"
	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/bradleymackey/track-slash/internal/testutil"
)

// testDatabaseURL returns TEST_DATABASE_URL first, falling back to DATABASE_URL.
// Returns "" if neither is set — the test will skip.
func testDatabaseURL() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return os.Getenv("DATABASE_URL")
}

func TestStoreConnectsAndMigrates(t *testing.T) {
	dbURL := testDatabaseURL()
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL / DATABASE_URL not set; skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Apply migrations via database/sql + pgx driver.
	sqlDB, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := migrations.Up(sqlDB); err != nil {
		t.Fatalf("migrations.Up: %v", err)
	}
	testutil.CleanDatabase(t, sqlDB)
	t.Cleanup(func() { testutil.CleanDatabase(t, sqlDB) })

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("pool.Ping: %v", err)
	}

	s := store.New(pool)

	t.Run("ping via Store", func(t *testing.T) {
		if err := s.Ping(ctx); err != nil {
			t.Fatalf("store.Ping: %v", err)
		}
	})

	t.Run("server_version reachable", func(t *testing.T) {
		var version string
		if err := pool.QueryRow(ctx, "SHOW server_version").Scan(&version); err != nil {
			t.Fatalf("SHOW server_version: %v", err)
		}
		if version == "" {
			t.Fatal("empty server_version")
		}
		t.Logf("connected to postgres %s", version)
	})
}
