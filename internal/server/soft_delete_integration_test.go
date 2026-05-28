package server_test

import (
	"fmt"
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

	code, body := e.do(t, http.MethodDelete, fmt.Sprintf("/issues/%s", iss.ID), nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete issue code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodGet, fmt.Sprintf("/issues/%s", iss.ID), nil)
	if code != http.StatusNotFound {
		t.Fatalf("get deleted issue code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodDelete, fmt.Sprintf("/issues/%s", iss.ID), nil)
	if code != http.StatusNotFound {
		t.Fatalf("delete issue second code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodPatch, fmt.Sprintf("/issues/%s", iss.ID), map[string]any{"description": "nope"})
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

	code, body := e.do(t, http.MethodDelete, fmt.Sprintf("/projects/%s", e.projectID), nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete project code = %d body = %s", code, body)
	}
	for _, path := range []string{
		fmt.Sprintf("/projects/%s", e.projectID),
		fmt.Sprintf("/issues/%s", iss.ID),
		fmt.Sprintf("/sprints/%s", sp.ID),
	} {
		code, body = e.do(t, http.MethodGet, path, nil)
		if code != http.StatusNotFound {
			t.Fatalf("GET %s code = %d body = %s", path, code, body)
		}
	}
	code, body = e.do(t, http.MethodDelete, fmt.Sprintf("/projects/%s", e.projectID), nil)
	if code != http.StatusNotFound {
		t.Fatalf("delete project second code = %d body = %s", code, body)
	}
}

func TestHTTPSoftDeleteUser(t *testing.T) {
	e := newHTTPEnv(t)
	u := mustHTTPUser(t, e)

	code, body := e.do(t, http.MethodDelete, fmt.Sprintf("/users/%s", u.ID), nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete user code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodGet, fmt.Sprintf("/users/%s", u.ID), nil)
	if code != http.StatusNotFound {
		t.Fatalf("get deleted user code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodDelete, fmt.Sprintf("/users/%s", u.ID), nil)
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

	code, body := e.do(t, http.MethodDelete, fmt.Sprintf("/sprints/%s", active.ID), nil)
	if code != http.StatusConflict {
		t.Fatalf("delete active sprint code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodDelete, fmt.Sprintf("/sprints/%s", planned.ID), nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete planned sprint code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodGet, fmt.Sprintf("/sprints/%s", planned.ID), nil)
	if code != http.StatusNotFound {
		t.Fatalf("get deleted sprint code = %d body = %s", code, body)
	}
}

func TestHTTPSoftDeleteBadIDs(t *testing.T) {
	e := newHTTPEnv(t)
	for _, path := range []string{"/users/nope", "/projects/nope", "/issues/nope", "/sprints/nope"} {
		code, body := e.do(t, http.MethodDelete, path, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("DELETE %s code = %d body = %s", path, code, body)
		}
	}
	for _, path := range []string{
		fmt.Sprintf("/users/%s", uuid.New()),
		fmt.Sprintf("/projects/%s", uuid.New()),
		fmt.Sprintf("/issues/%s", uuid.New()),
		fmt.Sprintf("/sprints/%s", uuid.New()),
	} {
		code, body := e.do(t, http.MethodDelete, path, nil)
		if code != http.StatusNotFound {
			t.Fatalf("DELETE %s code = %d body = %s", path, code, body)
		}
	}
}

func dateHTTP(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
