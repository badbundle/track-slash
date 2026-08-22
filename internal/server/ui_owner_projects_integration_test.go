package server_test

import (
	"net/http"
	"net/url"
	"testing"
)

// A malformed owner segment is plain user input, not an internal fault. It used
// to reach GetUserByUsername unvalidated, whose bare validation error
// writeUIStoreError could not map, producing 500 and a spurious
// `internal error: source="ui store"` log line.
func TestMalformedOwnerInProjectRoutesIsARequestError(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	_, session := e.mustProjectMemberToken(t, "owner-validation")

	for _, owner := range []string{
		"ab",                                  // shorter than 3 chars
		"bad.user",                            // illegal character
		"-x",                                  // must start with a letter or number
		"thisusernameisfartoolongtobevalidha", // longer than 32 chars
	} {
		for _, suffix := range []string{"", "/panel"} {
			path := "/" + url.PathEscape(owner) + "/projects" + suffix
			t.Run(path, func(t *testing.T) {
				res := e.uiDoNoRedirect(t, http.MethodGet, path, session, nil)
				body := readBody(t, res)
				res.Body.Close()
				if res.StatusCode != http.StatusBadRequest {
					t.Fatalf("GET %s code = %d body = %s", path, res.StatusCode, body)
				}
			})
		}
	}
}

// A well-formed owner who does not exist is still a 404, and the real owner
// still resolves.
func TestWellFormedOwnerInProjectRoutesKeepsWorking(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	_, session := e.mustProjectMemberToken(t, "owner-valid")

	missing := e.uiDoNoRedirect(t, http.MethodGet, "/nobody-here/projects", session, nil)
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("missing owner code = %d, want 404", missing.StatusCode)
	}

	for _, path := range []string{"/" + e.ownerUsername + "/projects", "/" + e.ownerUsername + "/projects/panel"} {
		res := e.uiDoNoRedirect(t, http.MethodGet, path, session, nil)
		body := readBody(t, res)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s code = %d body = %s", path, res.StatusCode, body)
		}
	}
}
