package store_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func mustCreateUser(t *testing.T, env *sprintsTestEnv, email string) model.User {
	t.Helper()
	u, err := env.store.CreateUser(env.ctx, email, "Commenter")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return u
}

func mustCreateComment(t *testing.T, env *sprintsTestEnv, issueID, authorID uuid.UUID, body string) model.Comment {
	t.Helper()
	c, err := env.store.CreateComment(env.ctx, store.CreateCommentParams{
		IssueID:  issueID,
		AuthorID: authorID,
		Body:     body,
	})
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	return c
}

func TestCreateGetUpdateDeleteComment(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	iss := mustCreateIssue(t, env, "commented")
	author := mustCreateUser(t, env, "commenter-"+uniqueDigits(timeNow(t), 8)+"@example.com")

	c := mustCreateComment(t, env, iss.ID, author.ID, "hello")
	if c.IssueID != iss.ID || c.AuthorID != author.ID || c.Body != "hello" {
		t.Fatalf("comment mismatch: %+v", c)
	}
	if c.EditedAt != nil {
		t.Fatalf("new comment EditedAt = %v, want nil", c.EditedAt)
	}

	got, err := env.store.GetComment(env.ctx, c.ID)
	if err != nil {
		t.Fatalf("GetComment: %v", err)
	}
	if got.ID != c.ID || got.Body != "hello" {
		t.Fatalf("GetComment mismatch: %+v", got)
	}
	if got.EditedAt != nil {
		t.Fatalf("GetComment EditedAt = %v, want nil", got.EditedAt)
	}

	updated, err := env.store.UpdateComment(env.ctx, store.UpdateCommentParams{
		ID:       c.ID,
		AuthorID: author.ID,
		Body:     "edited",
	})
	if err != nil {
		t.Fatalf("UpdateComment: %v", err)
	}
	if updated.Body != "edited" {
		t.Fatalf("Body = %q, want edited", updated.Body)
	}
	if updated.EditedAt == nil || !updated.EditedAt.Equal(updated.UpdatedAt) || !updated.UpdatedAt.After(updated.CreatedAt) {
		t.Fatalf("updated times = created %v updated %v edited %v, want edited_at from updated_at", updated.CreatedAt, updated.UpdatedAt, updated.EditedAt)
	}

	same, err := env.store.UpdateComment(env.ctx, store.UpdateCommentParams{
		ID:       c.ID,
		AuthorID: author.ID,
		Body:     "edited",
	})
	if err != nil {
		t.Fatalf("UpdateComment same body: %v", err)
	}
	if same.Body != "edited" || !same.UpdatedAt.Equal(updated.UpdatedAt) || same.EditedAt == nil || !same.EditedAt.Equal(*updated.EditedAt) {
		t.Fatalf("same-body update = %+v, want body and edit timestamp preserved from %+v", same, updated)
	}

	other := mustCreateUser(t, env, "other-commenter-"+uniqueDigits(timeNow(t), 8)+"@example.com")
	if _, err := env.store.UpdateComment(env.ctx, store.UpdateCommentParams{ID: c.ID, AuthorID: other.ID, Body: "not yours"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("wrong author err = %v, want ErrNotFound", err)
	}
	got, err = env.store.GetComment(env.ctx, c.ID)
	if err != nil {
		t.Fatalf("GetComment after wrong author: %v", err)
	}
	if got.Body != "edited" {
		t.Fatalf("wrong author changed body to %q", got.Body)
	}

	if err := env.store.DeleteComment(env.ctx, c.ID); err != nil {
		t.Fatalf("DeleteComment: %v", err)
	}
	if _, err := env.store.GetComment(env.ctx, c.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get deleted err = %v, want ErrNotFound", err)
	}
	if err := env.store.DeleteComment(env.ctx, c.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Delete second err = %v, want ErrNotFound", err)
	}
}

func TestCreateCommentForeignKeysAndBodyCheck(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	iss := mustCreateIssue(t, env, "commented")
	author := mustCreateUser(t, env, "commenter-"+uniqueDigits(timeNow(t), 8)+"@example.com")

	_, err := env.store.CreateComment(env.ctx, store.CreateCommentParams{
		IssueID:  uuid.New(),
		AuthorID: author.ID,
		Body:     "hello",
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown issue err = %v, want ErrNotFound", err)
	}

	_, err = env.store.CreateComment(env.ctx, store.CreateCommentParams{
		IssueID:  iss.ID,
		AuthorID: uuid.New(),
		Body:     "hello",
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown author err = %v, want ErrNotFound", err)
	}

	_, err = env.store.CreateComment(env.ctx, store.CreateCommentParams{
		IssueID:  iss.ID,
		AuthorID: author.ID,
		Body:     "",
	})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("empty body err = %v, want ErrConflict", err)
	}

	_, err = env.store.UpdateComment(env.ctx, store.UpdateCommentParams{
		ID:       uuid.New(),
		AuthorID: author.ID,
		Body:     "body",
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("update missing err = %v, want ErrNotFound", err)
	}

	c := mustCreateComment(t, env, iss.ID, author.ID, "ok")
	_, err = env.store.UpdateComment(env.ctx, store.UpdateCommentParams{
		ID:       c.ID,
		AuthorID: author.ID,
		Body:     strings.Repeat("x", 10001),
	})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("long update err = %v, want ErrConflict", err)
	}
}

func TestListCommentsForIssuePagination(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	iss := mustCreateIssue(t, env, "commented")
	author := mustCreateUser(t, env, "commenter-"+uniqueDigits(timeNow(t), 8)+"@example.com")

	empty, more, err := env.store.ListCommentsForIssue(env.ctx, store.ListCommentsForIssueParams{
		IssueID: iss.ID,
		Limit:   2,
	})
	if err != nil {
		t.Fatalf("ListCommentsForIssue empty: %v", err)
	}
	if len(empty) != 0 || more {
		t.Fatalf("empty len=%d more=%v, want 0 false", len(empty), more)
	}

	first := mustCreateComment(t, env, iss.ID, author.ID, "one")
	second := mustCreateComment(t, env, iss.ID, author.ID, "two")
	third := mustCreateComment(t, env, iss.ID, author.ID, "three")

	page1, more, err := env.store.ListCommentsForIssue(env.ctx, store.ListCommentsForIssueParams{
		IssueID: iss.ID,
		Limit:   2,
	})
	if err != nil {
		t.Fatalf("ListCommentsForIssue page1: %v", err)
	}
	if !more || len(page1) != 2 || page1[0].ID != first.ID || page1[1].ID != second.ID {
		t.Fatalf("page1 = %+v more=%v", page1, more)
	}

	page2, more, err := env.store.ListCommentsForIssue(env.ctx, store.ListCommentsForIssueParams{
		IssueID: iss.ID,
		Cursor:  &store.CommentsCursor{CreatedAt: page1[1].CreatedAt, ID: page1[1].ID},
		Limit:   2,
	})
	if err != nil {
		t.Fatalf("ListCommentsForIssue page2: %v", err)
	}
	if more || len(page2) != 1 || page2[0].ID != third.ID {
		t.Fatalf("page2 = %+v more=%v", page2, more)
	}

	newestPage1, more, err := env.store.ListCommentsForIssue(env.ctx, store.ListCommentsForIssueParams{
		IssueID:     iss.ID,
		Limit:       2,
		NewestFirst: true,
	})
	if err != nil {
		t.Fatalf("ListCommentsForIssue newest page1: %v", err)
	}
	if !more || len(newestPage1) != 2 || newestPage1[0].ID != third.ID || newestPage1[1].ID != second.ID {
		t.Fatalf("newest page1 = %+v more=%v", newestPage1, more)
	}

	newestPage2, more, err := env.store.ListCommentsForIssue(env.ctx, store.ListCommentsForIssueParams{
		IssueID:     iss.ID,
		Cursor:      &store.CommentsCursor{CreatedAt: newestPage1[1].CreatedAt, ID: newestPage1[1].ID},
		Limit:       2,
		NewestFirst: true,
	})
	if err != nil {
		t.Fatalf("ListCommentsForIssue newest page2: %v", err)
	}
	if more || len(newestPage2) != 1 || newestPage2[0].ID != first.ID {
		t.Fatalf("newest page2 = %+v more=%v", newestPage2, more)
	}
}

func TestListCommentsForUnknownIssue(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	_, _, err := env.store.ListCommentsForIssue(env.ctx, store.ListCommentsForIssueParams{
		IssueID: uuid.New(),
		Limit:   10,
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func timeNow(t *testing.T) int64 {
	t.Helper()
	return time.Now().UnixNano()
}
