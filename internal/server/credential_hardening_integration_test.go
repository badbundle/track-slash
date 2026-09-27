package server_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/server"
	"github.com/bradleymackey/track-slash/internal/store"
)

// passwordUserWithSessions creates a password account with two web sessions.
func passwordUserWithSessions(t *testing.T, e *httpEnv, password string) (model.User, string, string) {
	t.Helper()
	u, err := e.store.CreateAccount(e.ctx, store.CreateAccountParams{
		Username: "pwchange" + strings.ToLower(uniqueProjectKey(t)),
		Password: password,
		Name:     "Password Change",
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	mint := func() string {
		t.Helper()
		created, err := e.store.CreateAuthToken(e.ctx, store.CreateAuthTokenParams{UserID: u.ID, Kind: model.AuthTokenKindSession, Name: "session"})
		if err != nil {
			t.Fatalf("CreateAuthToken: %v", err)
		}
		return created.RawToken
	}
	return u, mint(), mint()
}

// Checking the current password is a guess like any sign-in, so it spends the
// sign-in budget; and a new password ends every other session.
func TestHTTPPasswordChangeIsRateLimitedAndEndsOtherSessions(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	const password = "correct-horse-battery"
	_, current, other := passwordUserWithSessions(t, e, password)

	limited := httptest.NewServer(server.NewWithOptions(e.store, nil, server.Options{AuthRateLimit: server.AuthRateLimitOptions{
		IPAttempts: 100, IPWindow: time.Minute, IdentifierAttempts: 2, IdentifierWindow: 5 * time.Minute,
	}}).Router())
	t.Cleanup(limited.Close)
	guess := map[string]any{"current_password": "wrong-guess", "new_password": "attacker-password-1"}
	for attempt, want := range []int{http.StatusUnauthorized, http.StatusUnauthorized, http.StatusTooManyRequests} {
		req, _ := http.NewRequestWithContext(e.ctx, http.MethodPatch, limited.URL+apiPath("/me/settings"), strings.NewReader(`{"current_password":"wrong-guess","new_password":"attacker-password-1"}`))
		req.Header.Set("Authorization", "Bearer "+current)
		req.Header.Set("Content-Type", "application/json")
		res, err := limited.Client().Do(req)
		if err != nil {
			t.Fatalf("attempt %d: %v", attempt+1, err)
		}
		res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("guess %d code = %d, want %d", attempt+1, res.StatusCode, want)
		}
	}
	_ = guess

	code, body := e.doWithToken(t, current, http.MethodPatch, "/me/settings", map[string]any{"current_password": password, "new_password": "a-brand-new-password"})
	if code != http.StatusOK {
		t.Fatalf("password change code = %d body = %s", code, body)
	}
	if code, body := e.doWithToken(t, other, http.MethodGet, "/me", nil); code != http.StatusUnauthorized {
		t.Fatalf("other session after password change code = %d body = %s", code, body)
	}
	if code, body := e.doWithToken(t, current, http.MethodGet, "/me", nil); code != http.StatusOK {
		t.Fatalf("changing session after password change code = %d body = %s", code, body)
	}
}

func TestMCPPasswordChangeIsRateLimitedAndEndsOtherSessions(t *testing.T) {
	t.Parallel()
	e := newMCPHTTPEnv(t, nil)
	const password = "correct-horse-battery"
	u, _, other := passwordUserWithSessions(t, e, password)
	api, err := e.store.CreateAuthToken(e.ctx, store.CreateAuthTokenParams{UserID: u.ID, Kind: model.AuthTokenKindAPI, Name: "agent"})
	if err != nil {
		t.Fatalf("CreateAuthToken api: %v", err)
	}
	session := mcpConnect(t, e, api.RawToken)
	args := map[string]any{"current_password": "wrong-guess", "new_password": "attacker-password-1"}
	for attempt := 0; attempt < 10; attempt++ {
		requireMCPErrorCode(t, mcpCallExpectError(t, e, session, "track_update_my_settings", args), "unauthorized")
	}
	requireMCPErrorCode(t, mcpCallExpectError(t, e, session, "track_update_my_settings", args), "rate_limited")

	fresh := newMCPHTTPEnv(t, nil)
	u, current, other2 := passwordUserWithSessions(t, fresh, password)
	api, err = fresh.store.CreateAuthToken(fresh.ctx, store.CreateAuthTokenParams{UserID: u.ID, Kind: model.AuthTokenKindAPI, Name: "agent"})
	if err != nil {
		t.Fatalf("CreateAuthToken api: %v", err)
	}
	out := mcpCall(t, fresh, mcpConnect(t, fresh, api.RawToken), "track_update_my_settings", map[string]any{"current_password": password, "new_password": "a-brand-new-password"})
	if decodeMCPField[model.User](t, out, "user").ID != u.ID {
		t.Fatalf("MCP password change = %+v", out)
	}
	// An API token is not a session, so every browser session ends.
	for _, token := range []string{current, other2} {
		if code, body := fresh.doWithToken(t, token, http.MethodGet, "/me", nil); code != http.StatusUnauthorized {
			t.Fatalf("session after MCP password change code = %d body = %s", code, body)
		}
	}
	_ = other
}

func TestUIPasswordChangeIsRateLimitedAndEndsOtherSessions(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	const password = "correct-horse-battery"
	_, current, other := passwordUserWithSessions(t, e, password)
	post := func(currentPassword string) int {
		t.Helper()
		form := url.Values{"current_password": {currentPassword}, "new_password": {"a-brand-new-password"}}
		res := e.uiDoNoRedirect(t, http.MethodPost, "/settings/password", current, strings.NewReader(form.Encode()))
		defer res.Body.Close()
		return res.StatusCode
	}
	if code := post(password); code != http.StatusOK {
		t.Fatalf("UI password change code = %d", code)
	}
	if code, body := e.doWithToken(t, other, http.MethodGet, "/me", nil); code != http.StatusUnauthorized {
		t.Fatalf("other session after UI password change code = %d body = %s", code, body)
	}
	if code, body := e.doWithToken(t, current, http.MethodGet, "/me", nil); code != http.StatusOK {
		t.Fatalf("changing session after UI password change code = %d body = %s", code, body)
	}
	// The one success spent an attempt too; nine more wrong guesses use the
	// rest of the default budget of ten.
	for attempt := 0; attempt < 9; attempt++ {
		if code := post("wrong-guess"); code != http.StatusOK {
			t.Fatalf("wrong guess %d code = %d", attempt+1, code)
		}
	}
	if code := post("wrong-guess"); code != http.StatusTooManyRequests {
		t.Fatalf("guess past the budget code = %d", code)
	}
}
