package realtime

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/bradleymackey/track-slash/internal/migrations"
	"github.com/bradleymackey/track-slash/internal/testutil"
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
	testutil.CleanDatabase(t, sqlDB)
	t.Cleanup(func() { testutil.CleanDatabase(t, sqlDB) })

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
	projectID = insertRealtimeProject(ctx, t, pool, "rt-test")

	// Subscribe after the project insert. The project event may still be
	// in flight inside the listener at this moment, so we loop and discard
	// anything that isn't the issue insert we care about.
	c := newTestClient(8)
	hub.Subscribe(c, "project:"+projectID)

	if _, err := pool.Exec(ctx, `
		INSERT INTO issues (project_id, number, title) VALUES ($1, 1, 'hello')
	`, projectID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}

	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-c.send:
			if ev.Entity != EntityIssue {
				continue
			}
			if ev.Op != OpInsert {
				t.Errorf("op = %s, want insert", ev.Op)
			}
			if ev.ProjectID == nil || ev.ProjectID.String() != projectID {
				t.Errorf("project_id = %v, want %s", ev.ProjectID, projectID)
			}
			return
		case <-deadline:
			t.Fatal("did not receive issue event within 3s")
		}
	}
}

// TestListenerReceivesSubIssueEvent verifies child issue events include
// parent_issue_id so the hub can fan them out on the parent issue topic.
func TestListenerReceivesSubIssueEvent(t *testing.T) {
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
	testutil.CleanDatabase(t, sqlDB)
	t.Cleanup(func() { testutil.CleanDatabase(t, sqlDB) })

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

	time.Sleep(500 * time.Millisecond)

	var projectID string
	projectID = insertRealtimeProject(ctx, t, pool, "rt-sub-issue")
	var parentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issues (project_id, number, title) VALUES ($1, 1, 'parent') RETURNING id::text
	`, projectID).Scan(&parentID); err != nil {
		t.Fatalf("insert parent: %v", err)
	}

	parentSub := newTestClient(16)
	projSub := newTestClient(16)
	hub.Subscribe(parentSub, "issue:"+parentID)
	hub.Subscribe(projSub, "project:"+projectID)

	var childID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issues (project_id, number, title, parent_issue_id)
		VALUES ($1, 2, 'child', $2)
		RETURNING id::text
	`, projectID, parentID).Scan(&childID); err != nil {
		t.Fatalf("insert child: %v", err)
	}

	waitForSubIssueEvent(t, parentSub, childID, parentID, projectID, OpInsert)
	waitForSubIssueEvent(t, projSub, childID, parentID, projectID, OpInsert)
}

// TestListenerReceivesSprintEvent verifies sprint INSERT fires the
// sprints_events trigger, the listener decodes the payload, and the hub fans
// it out on both the project topic and the sprint topic.
func TestListenerReceivesSprintEvent(t *testing.T) {
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
	testutil.CleanDatabase(t, sqlDB)
	t.Cleanup(func() { testutil.CleanDatabase(t, sqlDB) })

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

	time.Sleep(500 * time.Millisecond)

	var projectID string
	projectID = insertRealtimeProject(ctx, t, pool, "rt-sprint")

	projSub := newTestClient(8)
	hub.Subscribe(projSub, "project:"+projectID)

	var sprintID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO sprints (project_id, number, name, start_date, end_date)
		VALUES ($1, 1, 'S1', DATE '2026-06-01', DATE '2026-06-14')
		RETURNING id::text
	`, projectID).Scan(&sprintID); err != nil {
		t.Fatalf("insert sprint: %v", err)
	}

	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-projSub.send:
			if ev.Entity != EntitySprint {
				continue
			}
			if ev.Op != OpInsert {
				t.Errorf("op = %s, want insert", ev.Op)
			}
			if ev.ID.String() != sprintID {
				t.Errorf("id = %s, want %s", ev.ID, sprintID)
			}
			if ev.ProjectID == nil || ev.ProjectID.String() != projectID {
				t.Errorf("project_id = %v, want %s", ev.ProjectID, projectID)
			}
			return
		case <-deadline:
			t.Fatal("did not receive sprint event within 3s")
		}
	}
}

// TestListenerReceivesIssueLinkEvent verifies issue_links INSERT fires the
// issue_links_events trigger, the listener decodes the payload, and the hub
// fans it out on both the project topic and the issue_link topic. The
// duplicates link path also closes the source issue, producing a follow-up
// issue UPDATE event.
func TestListenerReceivesIssueLinkEvent(t *testing.T) {
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
	testutil.CleanDatabase(t, sqlDB)
	t.Cleanup(func() { testutil.CleanDatabase(t, sqlDB) })

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

	time.Sleep(500 * time.Millisecond)

	var projectID string
	projectID = insertRealtimeProject(ctx, t, pool, "rt-link")

	var srcID, tgtID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issues (project_id, number, title) VALUES ($1, 1, 'A') RETURNING id::text
	`, projectID).Scan(&srcID); err != nil {
		t.Fatalf("insert src: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO issues (project_id, number, title) VALUES ($1, 2, 'B') RETURNING id::text
	`, projectID).Scan(&tgtID); err != nil {
		t.Fatalf("insert tgt: %v", err)
	}

	projSub := newTestClient(16)
	hub.Subscribe(projSub, "project:"+projectID)

	var linkID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue_links (project_id, number, source_id, target_id, link_type)
		VALUES ($1, 1, $2, $3, 'blocks')
		RETURNING id::text
	`, projectID, srcID, tgtID).Scan(&linkID); err != nil {
		t.Fatalf("insert link: %v", err)
	}

	linkSub := newTestClient(8)
	hub.Subscribe(linkSub, "issue_link:"+linkID)

	gotProjLink := false
	deadline := time.After(3 * time.Second)
	for !gotProjLink {
		select {
		case ev := <-projSub.send:
			if ev.Entity != EntityIssueLink {
				continue
			}
			if ev.Op != OpInsert {
				t.Errorf("op = %s, want insert", ev.Op)
			}
			if ev.ID.String() != linkID {
				t.Errorf("id = %s, want %s", ev.ID, linkID)
			}
			if ev.ProjectID == nil || ev.ProjectID.String() != projectID {
				t.Errorf("project_id = %v, want %s", ev.ProjectID, projectID)
			}
			gotProjLink = true
		case <-deadline:
			t.Fatal("did not receive link event on project topic within 3s")
		}
	}

	// Delete the link and verify the OpDelete event arrives on the link topic.
	if _, err := pool.Exec(ctx, `DELETE FROM issue_links WHERE id = $1`, linkID); err != nil {
		t.Fatalf("delete link: %v", err)
	}
	deadline = time.After(3 * time.Second)
	for {
		select {
		case ev := <-linkSub.send:
			if ev.Entity != EntityIssueLink {
				continue
			}
			if ev.Op == OpDelete {
				return
			}
		case <-deadline:
			t.Fatal("did not receive link delete event within 3s")
		}
	}
}

// TestListenerReceivesCommentEvent verifies comment events include both issue_id
// and project_id so the hub can fan them out on comment, issue, and project topics.
func TestListenerReceivesCommentEvent(t *testing.T) {
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
	testutil.CleanDatabase(t, sqlDB)
	t.Cleanup(func() { testutil.CleanDatabase(t, sqlDB) })

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

	time.Sleep(500 * time.Millisecond)

	var projectID, issueID, authorID string
	projectID = insertRealtimeProject(ctx, t, pool, "rt-comment")
	userKey := uniqueKey(t)
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (username, email, name) VALUES ($1, $2, 'Commenter') RETURNING id::text
	`, "rtcomment"+userKey, "rt-comment-"+userKey+"@example.com").Scan(&authorID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO issues (project_id, number, title) VALUES ($1, 1, 'A') RETURNING id::text
	`, projectID).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}

	issueSub := newTestClient(16)
	projSub := newTestClient(16)
	hub.Subscribe(issueSub, "issue:"+issueID)
	hub.Subscribe(projSub, "project:"+projectID)

	var commentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO comments (issue_id, number, author_id, body)
		VALUES ($1, 1, $2, 'hello')
		RETURNING id::text
	`, issueID, authorID).Scan(&commentID); err != nil {
		t.Fatalf("insert comment: %v", err)
	}

	waitForCommentEvent(t, issueSub, commentID, issueID, projectID, OpInsert)
	waitForCommentEvent(t, projSub, commentID, issueID, projectID, OpInsert)

	commentSub := newTestClient(16)
	hub.Subscribe(commentSub, "comment:"+commentID)
	if _, err := pool.Exec(ctx, `
		UPDATE comments SET body = 'edited' WHERE id = $1
	`, commentID); err != nil {
		t.Fatalf("update comment: %v", err)
	}
	waitForCommentEvent(t, commentSub, commentID, issueID, projectID, OpUpdate)

	if _, err := pool.Exec(ctx, `DELETE FROM comments WHERE id = $1`, commentID); err != nil {
		t.Fatalf("delete comment: %v", err)
	}
	waitForCommentEvent(t, commentSub, commentID, issueID, projectID, OpDelete)
}

// TestListenerReceivesSoftDeleteAsDelete verifies updating deleted_at emits a
// realtime delete op so subscribers see the same event kind as hard deletes.
func TestListenerReceivesSoftDeleteAsDelete(t *testing.T) {
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
	testutil.CleanDatabase(t, sqlDB)
	t.Cleanup(func() { testutil.CleanDatabase(t, sqlDB) })

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

	time.Sleep(500 * time.Millisecond)

	var projectID, issueID string
	projectID = insertRealtimeProject(ctx, t, pool, "rt-soft-delete")
	if err := pool.QueryRow(ctx, `
		INSERT INTO issues (project_id, number, title) VALUES ($1, 1, 'A') RETURNING id::text
	`, projectID).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}

	issueSub := newTestClient(16)
	hub.Subscribe(issueSub, "issue:"+issueID)

	if _, err := pool.Exec(ctx, `UPDATE issues SET deleted_at = now() WHERE id = $1`, issueID); err != nil {
		t.Fatalf("soft-delete issue: %v", err)
	}

	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-issueSub.send:
			if ev.Entity != EntityIssue {
				continue
			}
			if ev.Op != OpDelete && ev.ID.String() == issueID {
				continue
			}
			if ev.Op != OpDelete {
				t.Fatalf("op = %s, want delete", ev.Op)
			}
			if ev.ID.String() != issueID {
				t.Fatalf("id = %s, want %s", ev.ID, issueID)
			}
			return
		case <-deadline:
			t.Fatal("did not receive soft-delete event within 3s")
		}
	}
}

func waitForCommentEvent(t *testing.T, c *Client, commentID, issueID, projectID string, op Op) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-c.send:
			if ev.Entity != EntityComment || ev.Op != op {
				continue
			}
			if ev.ID.String() != commentID {
				t.Errorf("id = %s, want %s", ev.ID, commentID)
			}
			if ev.IssueID == nil || ev.IssueID.String() != issueID {
				t.Errorf("issue_id = %v, want %s", ev.IssueID, issueID)
			}
			if ev.ProjectID == nil || ev.ProjectID.String() != projectID {
				t.Errorf("project_id = %v, want %s", ev.ProjectID, projectID)
			}
			return
		case <-deadline:
			t.Fatalf("did not receive comment %s event within 3s", op)
		}
	}
}

func waitForSubIssueEvent(t *testing.T, c *Client, childID, parentID, projectID string, op Op) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-c.send:
			if ev.Entity != EntityIssue || ev.Op != op || ev.ID.String() != childID {
				continue
			}
			if ev.ParentIssueID == nil || ev.ParentIssueID.String() != parentID {
				t.Errorf("parent_issue_id = %v, want %s", ev.ParentIssueID, parentID)
			}
			if ev.ProjectID == nil || ev.ProjectID.String() != projectID {
				t.Errorf("project_id = %v, want %s", ev.ProjectID, projectID)
			}
			return
		case <-deadline:
			t.Fatalf("did not receive sub-issue %s event within 3s", op)
		}
	}
}

func uniqueKey(t *testing.T) string {
	t.Helper()
	return "rt_" + time.Now().Format("150405.000000")
}

func insertRealtimeProject(ctx context.Context, t *testing.T, pool *pgxpool.Pool, name string) string {
	t.Helper()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	var ownerID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (username, email, name)
		VALUES ($1, $2, 'Realtime Owner')
		RETURNING id::text
	`, "rtowner"+suffix, "rt-owner-"+suffix+"@example.com").Scan(&ownerID); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	var projectID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects (owner_id, key, name)
		VALUES ($1, $2, $3)
		RETURNING id::text
	`, ownerID, uniqueKey(t), name).Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	return projectID
}
