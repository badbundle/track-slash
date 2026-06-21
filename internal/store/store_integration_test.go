package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/bradleymackey/track-slash/internal/migrations"
	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/bradleymackey/track-slash/internal/testutil"
)

func TestStoreConnectsAndMigrates(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := testutil.NewEmptyDatabase(t)
	if err := migrations.Up(db.SQL); err != nil {
		t.Fatalf("migrations.Up: %v", err)
	}

	pool := db.Pool

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
