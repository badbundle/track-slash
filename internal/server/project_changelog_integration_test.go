package server_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/bradleymackey/track-slash/internal/model"
)

func TestHTTPProjectChangelogAPI(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)

	code, body := e.do(t, http.MethodPost, e.projectIssuesPath(), map[string]any{
		"title":       "API changelog issue",
		"description": "created through API",
	})
	if code != http.StatusCreated {
		t.Fatalf("create issue code = %d body = %s", code, body)
	}
	created := decode[model.Issue](t, body)

	code, body = e.do(t, http.MethodGet, e.projectPath()+"/changelog?limit=10", nil)
	if code != http.StatusOK {
		t.Fatalf("changelog code = %d body = %s", code, body)
	}
	page := decodePage[model.ProjectChangelogEntry](t, body)
	var found bool
	for _, entry := range page.Items {
		if entry.Entity == "issue" && entry.Op == "insert" && entry.EntityID == created.ID {
			found = true
			if entry.Actor == nil || entry.Actor.ID != e.adminID {
				t.Fatalf("entry actor = %+v, want admin %s", entry.Actor, e.adminID)
			}
			if entry.TargetRef != created.Identifier || !strings.Contains(entry.Summary, created.Identifier) {
				t.Fatalf("entry target/summary = %s/%s", entry.TargetRef, entry.Summary)
			}
		}
	}
	if !found {
		t.Fatalf("issue changelog entry not found in %+v", page.Items)
	}

	code, body = e.do(t, http.MethodGet, e.projectPath()+"/changelog?cursor=not-base64", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("bad cursor code = %d body = %s", code, body)
	}
	code, body = e.do(t, http.MethodGet, e.projectPath()+"/changelog?limit=0", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("bad limit code = %d body = %s", code, body)
	}

	member, memberToken := e.mustProjectMemberToken(t, "changelog-member")
	code, body = e.doWithToken(t, memberToken, http.MethodGet, e.projectPath()+"/changelog", nil)
	if code != http.StatusOK {
		t.Fatalf("member changelog code = %d body = %s", code, body)
	}
	_, outsiderToken := e.mustUserToken(t, "changelog-outsider")
	code, body = e.doWithToken(t, outsiderToken, http.MethodGet, e.projectPath()+"/changelog", nil)
	if code != http.StatusForbidden {
		t.Fatalf("outsider changelog code = %d body = %s member=%s", code, body, member.ID)
	}
}
