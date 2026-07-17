package server_test

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func TestUIRedirectsUnauthenticatedApp(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	res := e.uiDoNoRedirect(t, http.MethodGet, "/", "", nil)
	defer res.Body.Close()

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("code = %d", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); !strings.HasPrefix(loc, "/login?next=") {
		t.Fatalf("Location = %q", loc)
	}
}

func TestUISecurityHeadersOnAuthenticatedHTML(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	res := e.uiDoNoRedirect(t, http.MethodGet, "/projects", e.authToken, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body = %s", res.StatusCode, readBody(t, res))
	}
	requireSecurityHeadersForTest(t, res.Header)
	if got := res.Header.Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("localhost Strict-Transport-Security = %q, want absent", got)
	}
}

func TestUILoginRejectsBadCredentials(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	form := url.Values{"username": {"not-a-user"}, "password": {"not-a-password"}}
	res := e.uiDoNoRedirect(t, http.MethodPost, "/login", "", strings.NewReader(form.Encode()))
	defer res.Body.Close()

	body := readBody(t, res)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("code = %d body = %s", res.StatusCode, body)
	}
	if strings.Contains(res.Header.Get("Set-Cookie"), uiCookieNameForTest) {
		t.Fatalf("unexpected auth cookie: %s", res.Header.Get("Set-Cookie"))
	}
	if !strings.Contains(body, "Username or password not accepted.") {
		t.Fatalf("body missing login error: %s", body)
	}
}

func TestUIAuthPagesRenderPasskeyControls(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	res := e.uiDoNoRedirect(t, http.MethodGet, "/login?next=%2Ftokens", "", nil)
	body := readBody(t, res)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login code = %d body = %s", res.StatusCode, body)
	}
	for _, want := range []string{"Sign in with passkey", `data-passkey-login`, `/static/auth.js`} {
		if !strings.Contains(body, want) {
			t.Fatalf("login body missing %q: %s", want, body)
		}
	}

	res = e.uiDoNoRedirect(t, http.MethodGet, "/signup?next=%2Ftokens", "", nil)
	body = readBody(t, res)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("signup code = %d body = %s", res.StatusCode, body)
	}
	for _, want := range []string{"Create with passkey", `data-passkey-signup`, `/static/auth.js`} {
		if !strings.Contains(body, want) {
			t.Fatalf("signup body missing %q: %s", want, body)
		}
	}

	res = e.uiDoNoRedirect(t, http.MethodGet, "/static/auth.js", "", nil)
	body = readBody(t, res)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("auth asset code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "This browser does not support passkeys.") {
		t.Fatalf("auth asset missing passkey fallback: %s", body)
	}
}

func TestUILoginSetsCookie(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	username := "uilogin" + strings.ToLower(uniqueProjectKey(t))
	password := "correct-horse-battery"
	if _, err := e.store.CreateAccount(e.ctx, store.CreateAccountParams{Username: username, Password: password, Name: "UI Login"}); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	next := e.projectPath() + "/about"
	form := url.Values{"username": {username}, "password": {password}, "next": {next}}
	res := e.uiDoNoRedirect(t, http.MethodPost, "/login", "", strings.NewReader(form.Encode()))
	defer res.Body.Close()

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("code = %d", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); loc != next {
		t.Fatalf("Location = %q", loc)
	}
	cookie := findUICookie(t, res.Cookies())
	if !cookie.HttpOnly {
		t.Fatal("ui auth cookie is not HttpOnly")
	}
	if cookie.Path != "/" {
		t.Fatalf("cookie Path = %q", cookie.Path)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie SameSite = %v", cookie.SameSite)
	}
	if cookie.Expires.IsZero() || cookie.MaxAge <= 0 {
		t.Fatalf("session cookie expiry = %v MaxAge = %d", cookie.Expires, cookie.MaxAge)
	}
	auth, err := e.store.AuthenticateToken(e.ctx, cookie.Value)
	if err != nil {
		t.Fatalf("AuthenticateToken cookie: %v", err)
	}
	if auth.Token.ExpiresAt == nil || auth.Token.ExpiresAt.Sub(cookie.Expires) > time.Second || cookie.Expires.Sub(*auth.Token.ExpiresAt) > time.Second {
		t.Fatalf("database expiry = %v, cookie expiry = %v", auth.Token.ExpiresAt, cookie.Expires)
	}
	if remaining := time.Until(*auth.Token.ExpiresAt); remaining < 7*24*time.Hour-time.Minute || remaining > 7*24*time.Hour+time.Minute {
		t.Fatalf("session expiry remaining = %v, want about 168h", remaining)
	}
}

func TestUISignupCreatesAccountAndSetsCookie(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	username := "uisignup" + strings.ToLower(uniqueProjectKey(t))
	form := url.Values{"username": {username}, "password": {"correct-horse-battery"}, "next": {"/tokens"}}
	res := e.uiDoNoRedirect(t, http.MethodPost, "/signup", "", strings.NewReader(form.Encode()))
	defer res.Body.Close()

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	if loc := res.Header.Get("Location"); loc != "/tokens" {
		t.Fatalf("Location = %q", loc)
	}
	cookie := findUICookie(t, res.Cookies())
	if cookie.Value == "" || !cookie.HttpOnly || cookie.Expires.IsZero() || cookie.MaxAge <= 0 {
		t.Fatalf("cookie = %+v", cookie)
	}
	if _, err := e.store.AuthenticatePassword(e.ctx, username, "correct-horse-battery"); err != nil {
		t.Fatalf("AuthenticatePassword after signup: %v", err)
	}
}

func TestUILogoutClearsCookie(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	res := e.uiDoNoRedirect(t, http.MethodPost, "/logout", e.authToken, nil)
	defer res.Body.Close()

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("code = %d", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); loc != "/login" {
		t.Fatalf("Location = %q", loc)
	}
	setCookie := res.Header.Get("Set-Cookie")
	if !strings.Contains(setCookie, uiCookieNameForTest+"=") || !strings.Contains(setCookie, "Max-Age=0") {
		t.Fatalf("Set-Cookie = %q", setCookie)
	}

	if _, err := e.store.AuthenticateToken(e.ctx, e.authToken); err != nil {
		t.Fatalf("logout revoked non-session token: %v", err)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, "/logout", "", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("no-cookie logout code = %d", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); loc != "/login" {
		t.Fatalf("no-cookie logout Location = %q", loc)
	}
}

func TestUILogoutClearsStaleOrInvalidSessionCookie(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	session, err := e.store.CreateAuthToken(e.ctx, store.CreateAuthTokenParams{
		UserID: e.adminID,
		Kind:   model.AuthTokenKindSession,
		Name:   "stale web session",
	})
	if err != nil {
		t.Fatalf("CreateAuthToken session: %v", err)
	}
	if err := e.store.RevokeAuthTokenForUser(e.ctx, e.adminID, session.Token.ID); err != nil {
		t.Fatalf("RevokeAuthTokenForUser: %v", err)
	}

	for _, token := range []string{session.RawToken, "not-a-real-token"} {
		res := e.uiDoNoRedirect(t, http.MethodPost, "/logout", token, nil)
		body := readBody(t, res)
		res.Body.Close()
		if res.StatusCode != http.StatusSeeOther {
			t.Fatalf("logout with token %q code = %d body = %s", token, res.StatusCode, body)
		}
		if loc := res.Header.Get("Location"); loc != "/login" {
			t.Fatalf("logout with token %q Location = %q", token, loc)
		}
		setCookie := res.Header.Get("Set-Cookie")
		if !strings.Contains(setCookie, uiCookieNameForTest+"=") || !strings.Contains(setCookie, "Max-Age=0") {
			t.Fatalf("logout with token %q Set-Cookie = %q", token, setCookie)
		}
	}
}

func TestUILogoutRevokesSessionCookie(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	session, err := e.store.CreateAuthToken(e.ctx, store.CreateAuthTokenParams{
		UserID: e.adminID,
		Kind:   model.AuthTokenKindSession,
		Name:   "web session",
	})
	if err != nil {
		t.Fatalf("CreateAuthToken session: %v", err)
	}

	res := e.uiDoNoRedirect(t, http.MethodGet, "/projects", session.RawToken, nil)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("pre-logout app code = %d", res.StatusCode)
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, "/logout", session.RawToken, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("logout code = %d", res.StatusCode)
	}

	if _, err := e.store.AuthenticateToken(e.ctx, session.RawToken); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("session auth after logout err = %v, want ErrUnauthorized", err)
	}

	res = e.uiDoNoRedirect(t, http.MethodGet, "/projects", session.RawToken, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("post-logout app code = %d", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); !strings.HasPrefix(loc, "/login?next=") {
		t.Fatalf("post-logout Location = %q", loc)
	}
}
