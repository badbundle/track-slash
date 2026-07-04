package server_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func TestHTTPUpdateProject(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)

	code, body := e.do(t, http.MethodPatch, e.projectPath(), map[string]any{
		"name":        "Renamed API Project",
		"description": "updated through API",
	})
	if code != http.StatusOK {
		t.Fatalf("update code = %d body = %s", code, body)
	}
	project := decode[model.Project](t, body)
	if project.Name != "Renamed API Project" || project.Description != "updated through API" {
		t.Fatalf("project = %+v", project)
	}
	entries, _, err := e.store.ListProjectChangelog(e.ctx, store.ListProjectChangelogParams{
		ProjectID: e.projectID,
		Limit:     5,
	})
	if err != nil {
		t.Fatalf("ListProjectChangelog: %v", err)
	}
	if len(entries) == 0 || entries[0].Entity != "project" || entries[0].Op != "update" || entries[0].TargetRef != e.projKey {
		t.Fatalf("missing update changelog: %+v", entries)
	}

	code, body = e.do(t, http.MethodPatch, e.projectPath(), map[string]any{"name": " "})
	if code != http.StatusBadRequest || !strings.Contains(string(body), "name must be 1..200 chars") {
		t.Fatalf("bad name code = %d body = %s", code, body)
	}
	got, err := e.store.GetProject(e.ctx, e.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Name != "Renamed API Project" {
		t.Fatalf("bad name changed project: %+v", got)
	}

	code, body = e.do(t, http.MethodPatch, e.projectPath(), map[string]any{"description": "   "})
	if code != http.StatusOK {
		t.Fatalf("clear description code = %d body = %s", code, body)
	}
	project = decode[model.Project](t, body)
	if project.Description != "" {
		t.Fatalf("cleared description = %q, want empty", project.Description)
	}

	outsider, token := e.mustUserToken(t, "project-update-denied")
	_ = outsider
	code, body = e.doWithToken(t, token, http.MethodPatch, e.projectPath(), map[string]any{"description": "denied"})
	if code != http.StatusForbidden {
		t.Fatalf("denied code = %d body = %s", code, body)
	}
}
