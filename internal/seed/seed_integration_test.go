package seed_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/bradleymackey/track-slash/internal/migrations"
	"github.com/bradleymackey/track-slash/internal/seed"
	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/bradleymackey/track-slash/internal/testutil"
)

func testDatabaseURL() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return os.Getenv("DATABASE_URL")
}

func TestSeedCreatesSubIssues(t *testing.T) {
	dbURL := testDatabaseURL()
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL / DATABASE_URL not set; skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

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
	st := store.New(pool)

	summary, err := seed.Run(ctx, st, seed.Options{
		Username:      "seedsub" + time.Now().Format("150405000000"),
		Password:      "correct-horse-battery",
		Name:          "Seed Sub-Issues",
		ProjectPrefix: "S" + time.Now().Format("04050"),
		Now:           time.Date(2026, 6, 6, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("seed.Run: %v", err)
	}

	totalSubIssues := 0
	for _, project := range summary.Projects {
		if !project.Seeded {
			t.Fatalf("project %s was not seeded: %+v", project.Project.Key, project)
		}
		if project.SubIssuesCreated == 0 {
			t.Fatalf("project %s SubIssuesCreated = 0", project.Project.Key)
		}
		totalSubIssues += project.SubIssuesCreated

		all, _, err := st.ListIssues(ctx, store.ListIssuesParams{
			ProjectID:        project.Project.ID,
			Limit:            200,
			IncludeSubIssues: true,
		})
		if err != nil {
			t.Fatalf("ListIssues include sub-issues: %v", err)
		}
		foundChild := false
		for _, issue := range all {
			if issue.ParentIssueID != nil {
				foundChild = true
				break
			}
		}
		if !foundChild {
			t.Fatalf("project %s had no persisted child issues", project.Project.Key)
		}
	}
	if totalSubIssues == 0 {
		t.Fatal("total SubIssuesCreated = 0")
	}
}
