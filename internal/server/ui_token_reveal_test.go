package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUITokenRevealCookieRoundTrip(t *testing.T) {
	t.Parallel()
	srv := New(nil, nil, nil)

	set := httptest.NewRequest(http.MethodPost, "/tokens", nil)
	set.AddCookie(&http.Cookie{Name: uiAuthCookieName, Value: "session-secret"})
	rec := httptest.NewRecorder()
	srv.setUITokenRevealCookie(rec, set, "raw-token-value")

	cookie := findSetCookie(t, rec.Result().Cookies(), uiTokenRevealCookieName)
	read := httptest.NewRequest(http.MethodGet, "/tokens", nil)
	read.AddCookie(&http.Cookie{Name: uiAuthCookieName, Value: "session-secret"})
	read.AddCookie(cookie)

	if got := srv.takeUITokenRevealCookie(httptest.NewRecorder(), read); got != "raw-token-value" {
		t.Fatalf("takeUITokenRevealCookie = %q, want %q", got, "raw-token-value")
	}
}

func TestUITokenRevealCookieRejectsUnusableValues(t *testing.T) {
	t.Parallel()
	srv := New(nil, nil, nil)

	valid := httptest.NewRequest(http.MethodPost, "/tokens", nil)
	valid.AddCookie(&http.Cookie{Name: uiAuthCookieName, Value: "session-secret"})
	rec := httptest.NewRecorder()
	srv.setUITokenRevealCookie(rec, valid, "raw-token-value")
	signed := findSetCookie(t, rec.Result().Cookies(), uiTokenRevealCookieName)

	for _, tt := range []struct {
		name        string
		sessionsErr bool
		session     string
		reveal      *http.Cookie
	}{
		{name: "no reveal cookie", session: "session-secret"},
		{name: "empty reveal cookie", session: "session-secret", reveal: &http.Cookie{Name: uiTokenRevealCookieName, Value: ""}},
		{name: "no separator", session: "session-secret", reveal: &http.Cookie{Name: uiTokenRevealCookieName, Value: "raw-token-value"}},
		{name: "empty token", session: "session-secret", reveal: &http.Cookie{Name: uiTokenRevealCookieName, Value: ".signature"}},
		{name: "wrong signature", session: "session-secret", reveal: &http.Cookie{Name: uiTokenRevealCookieName, Value: "raw-token-value.nope"}},
		{name: "different session", session: "other-secret", reveal: signed},
		{name: "no session at all", sessionsErr: true, reveal: signed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/tokens", nil)
			if !tt.sessionsErr {
				req.AddCookie(&http.Cookie{Name: uiAuthCookieName, Value: tt.session})
			}
			if tt.reveal != nil {
				req.AddCookie(tt.reveal)
			}
			if got := srv.takeUITokenRevealCookie(httptest.NewRecorder(), req); got != "" {
				t.Fatalf("takeUITokenRevealCookie = %q, want empty", got)
			}
		})
	}
}

// Nothing to carry means nothing to set; a stray empty cookie would only
// linger in the browser.
func TestUITokenRevealCookieIsNotSetForAnEmptyToken(t *testing.T) {
	t.Parallel()
	srv := New(nil, nil, nil)

	rec := httptest.NewRecorder()
	srv.setUITokenRevealCookie(rec, httptest.NewRequest(http.MethodPost, "/tokens", nil), "")

	if cookies := rec.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("cookies = %+v, want none", cookies)
	}
}

func findSetCookie(t *testing.T, cookies []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("no %s cookie in %+v", name, cookies)
	return nil
}
