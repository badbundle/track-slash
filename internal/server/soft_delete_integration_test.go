package server_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func TestHTTPSoftDeleteIssue(t *testing.T) {
	e := newHTTPEnv(t)
	iss := mustHTTPIssue(t, e)

	code, body := e.do(t, http.MethodDelete, e.issuePath(iss), nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete issue code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodGet, e.issuePath(iss), nil)
	if code != http.StatusNotFound {
		t.Fatalf("get deleted issue code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodDelete, e.issuePath(iss), nil)
	if code != http.StatusNotFound {
		t.Fatalf("delete issue second code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodPatch, e.issuePath(iss), map[string]any{"description": "nope"})
	if code != http.StatusNotFound {
		t.Fatalf("patch deleted issue code = %d body = %s", code, body)
	}
}

func TestHTTPSoftDeleteProject(t *testing.T) {
	e := newHTTPEnv(t)
	iss := mustHTTPIssue(t, e)
	sp, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "S1",
		StartDate: dateHTTP(2026, 6, 1),
		EndDate:   dateHTTP(2026, 6, 14),
	})
	if err != nil {
		t.Fatalf("CreateSprint: %v", err)
	}

	code, body := e.do(t, http.MethodDelete, e.projectPath(), nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete project code = %d body = %s", code, body)
	}
	for _, path := range []string{
		e.projectPath(),
		e.issuePath(iss),
		e.sprintPath(sp),
	} {
		code, body = e.do(t, http.MethodGet, path, nil)
		if code != http.StatusNotFound {
			t.Fatalf("GET %s code = %d body = %s", path, code, body)
		}
	}
	code, body = e.do(t, http.MethodDelete, e.projectPath(), nil)
	if code != http.StatusNotFound {
		t.Fatalf("delete project second code = %d body = %s", code, body)
	}
}

func TestHTTPSoftDeleteUser(t *testing.T) {
	e := newHTTPEnv(t)
	u := mustHTTPUser(t, e)

	code, body := e.do(t, http.MethodDelete, "/users/"+u.ID.String(), nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete user code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodGet, "/users/"+u.ID.String(), nil)
	if code != http.StatusNotFound {
		t.Fatalf("get deleted user code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodDelete, "/users/"+u.ID.String(), nil)
	if code != http.StatusNotFound {
		t.Fatalf("delete user second code = %d body = %s", code, body)
	}
}

func TestHTTPSoftDeleteSprint(t *testing.T) {
	e := newHTTPEnv(t)
	planned, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "planned",
		StartDate: dateHTTP(2026, 7, 1),
		EndDate:   dateHTTP(2026, 7, 14),
	})
	if err != nil {
		t.Fatalf("CreateSprint planned: %v", err)
	}
	active, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "active",
		StartDate: dateHTTP(2026, 7, 15),
		EndDate:   dateHTTP(2026, 7, 28),
	})
	if err != nil {
		t.Fatalf("CreateSprint active: %v", err)
	}
	st := model.SprintStatusActive
	if _, err := e.store.UpdateSprint(e.ctx, active.ID, store.UpdateSprintParams{Status: &st}); err != nil {
		t.Fatalf("UpdateSprint active: %v", err)
	}
	completed, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "completed",
		StartDate: dateHTTP(2026, 7, 29),
		EndDate:   dateHTTP(2026, 8, 11),
	})
	if err != nil {
		t.Fatalf("CreateSprint completed: %v", err)
	}

	code, body := e.do(t, http.MethodDelete, e.sprintPath(active), nil)
	if code != http.StatusConflict {
		t.Fatalf("delete active sprint code = %d body = %s", code, body)
	}
	if _, err := e.store.CompleteSprint(e.ctx, active.ID); err != nil {
		t.Fatalf("CompleteSprint active: %v", err)
	}
	if _, err := e.store.UpdateSprint(e.ctx, completed.ID, store.UpdateSprintParams{Status: &st}); err != nil {
		t.Fatalf("UpdateSprint completed active: %v", err)
	}
	if _, err := e.store.CompleteSprint(e.ctx, completed.ID); err != nil {
		t.Fatalf("CompleteSprint completed: %v", err)
	}
	for _, sp := range []model.Sprint{active, completed} {
		code, body = e.do(t, http.MethodDelete, e.sprintPath(sp), nil)
		if code != http.StatusConflict {
			t.Fatalf("delete completed sprint %s code = %d body = %s", sp.ID, code, body)
		}
	}
	code, body = e.do(t, http.MethodDelete, e.sprintPath(planned), nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete planned sprint code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodGet, e.sprintPath(planned), nil)
	if code != http.StatusNotFound {
		t.Fatalf("get deleted sprint code = %d body = %s", code, body)
	}
}

func TestHTTPSoftDeleteBadIDs(t *testing.T) {
	e := newHTTPEnv(t)
	for _, path := range []string{
		"/users/nope",
		"/bad!/projects/" + e.projKey,
		"/" + e.ownerUsername + "/issues/nope",
		e.projectPath() + "/sprints/nope",
	} {
		code, body := e.do(t, http.MethodDelete, path, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("DELETE %s code = %d body = %s", path, code, body)
		}
	}
	for _, path := range []string{
		"/users/" + uuid.New().String(),
		"/" + e.ownerUsername + "/projects/" + uniqueProjectKey(t),
		"/" + e.ownerUsername + "/issues/" + e.projKey + "-999999",
		e.projectPath() + "/sprints/sprint-999999",
	} {
		code, body := e.do(t, http.MethodDelete, path, nil)
		if code != http.StatusNotFound {
			t.Fatalf("DELETE %s code = %d body = %s", path, code, body)
		}
	}
}

func TestHTTPOldUUIDObjectRoutesRemoved(t *testing.T) {
	e := newHTTPEnv(t)
	issue := mustHTTPIssue(t, e)
	sprint, err := e.store.CreateSprint(e.ctx, store.CreateSprintParams{
		ProjectID: e.projectID,
		Name:      "Old Route Sprint",
		StartDate: dateHTTP(2026, 6, 1),
		EndDate:   dateHTTP(2026, 6, 14),
	})
	if err != nil {
		t.Fatalf("CreateSprint: %v", err)
	}
	comment, err := e.store.CreateComment(e.ctx, store.CreateCommentParams{
		IssueID:  issue.ID,
		AuthorID: e.adminID,
		Body:     "old route comment",
	})
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	target, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID: e.projectID,
		Title:     "old route target",
	})
	if err != nil {
		t.Fatalf("CreateIssue target: %v", err)
	}
	link, err := e.store.CreateIssueLink(e.ctx, store.CreateIssueLinkParams{
		SourceID: issue.ID,
		TargetID: target.ID,
		LinkType: model.LinkTypeBlocks,
	})
	if err != nil {
		t.Fatalf("CreateIssueLink: %v", err)
	}

	for _, path := range []string{
		"/projects/" + e.projectID.String(),
		"/issues/" + issue.ID.String(),
		"/sprints/" + sprint.ID.String(),
		"/comments/" + comment.ID.String(),
		"/issue-links/" + link.ID.String(),
	} {
		code, body := e.do(t, http.MethodGet, path, nil)
		if code != http.StatusNotFound {
			t.Fatalf("GET %s code = %d body = %s", path, code, body)
		}
	}
}

func dateHTTP(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
