package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUIRedirectPreservingQuery(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name         string
		requestURI   string
		target       string
		wantLocation string
	}{
		{
			name:         "no query leaves the target alone",
			requestURI:   "/badbundle/projects/TRACK",
			target:       "/badbundle/projects/TRACK/sprint",
			wantLocation: "/badbundle/projects/TRACK/sprint",
		},
		{
			name:         "filters and sort survive",
			requestURI:   "/badbundle/projects/TRACK?assignee_id=7&sort=priority",
			target:       "/badbundle/projects/TRACK/sprint",
			wantLocation: "/badbundle/projects/TRACK/sprint?assignee_id=7&sort=priority",
		},
		{
			name:         "a target that already carries a query is appended to",
			requestURI:   "/badbundle/projects/TRACK/backlog?sort=priority",
			target:       "/badbundle/projects/TRACK/all?assignee_id=7",
			wantLocation: "/badbundle/projects/TRACK/all?assignee_id=7&sort=priority",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			uiRedirectPreservingQuery(rec, httptest.NewRequest(http.MethodGet, tt.requestURI, nil), tt.target)

			if rec.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
			}
			if got := rec.Header().Get("Location"); got != tt.wantLocation {
				t.Fatalf("Location = %q, want %q", got, tt.wantLocation)
			}
		})
	}
}
