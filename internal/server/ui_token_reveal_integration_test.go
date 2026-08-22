package server_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The reported symptom: confirming the browser's resubmission prompt after
// creating a token silently created a second one.
func TestReloadingAfterTokenCreationDoesNotCreateASecondToken(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	user, session := e.mustProjectMemberToken(t, "token-prg")

	create := func() *http.Response {
		form := url.Values{"name": {"ci"}, "csrf_token": {uiCSRFTokenForTest("session", session)}}
		return e.uiDoNoRedirectWithHeaders(t, http.MethodPost, "/tokens", session,
			strings.NewReader(form.Encode()), map[string]string{"Origin": e.ts.URL, "X-CSRF-Token": ""})
	}

	res := create()
	res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("create code = %d, want 303", res.StatusCode)
	}

	// A reload of the redirect target is a GET, so it cannot create anything.
	reveal := findUICookieOrNil(res.Cookies(), uiTokenRevealCookieNameForTest)
	for range 3 {
		page := e.uiGetWithCookies(t, "/tokens", session, reveal)
		page.Body.Close()
		if page.StatusCode != http.StatusOK {
			t.Fatalf("tokens page code = %d", page.StatusCode)
		}
	}

	tokens, err := e.store.ListAuthTokens(e.ctx, user.ID)
	if err != nil {
		t.Fatalf("ListAuthTokens: %v", err)
	}
	named := 0
	for _, token := range tokens {
		if token.Name == "ci" {
			named++
		}
	}
	if named != 1 {
		t.Fatalf("found %d tokens named ci, want 1", named)
	}
}

// The raw token is unrecoverable from the database, so it rides across the
// redirect in a cookie that must be spent exactly once.
func TestTokenRevealCookieIsSpentOnFirstRead(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	_, session := e.mustProjectMemberToken(t, "token-reveal")

	form := url.Values{"name": {"reveal once"}, "csrf_token": {uiCSRFTokenForTest("session", session)}}
	res := e.uiDoNoRedirectWithHeaders(t, http.MethodPost, "/tokens", session,
		strings.NewReader(form.Encode()), map[string]string{"Origin": e.ts.URL, "X-CSRF-Token": ""})
	res.Body.Close()
	reveal := findUICookieOrNil(res.Cookies(), uiTokenRevealCookieNameForTest)
	if reveal == nil {
		t.Fatal("no reveal cookie")
	}
	if !reveal.HttpOnly || reveal.SameSite != http.SameSiteStrictMode || reveal.Path != "/tokens" {
		t.Fatalf("reveal cookie = %+v, want HttpOnly, SameSite=Strict, Path=/tokens", reveal)
	}

	first := e.uiGetWithCookies(t, "/tokens", session, reveal)
	body := readBody(t, first)
	first.Body.Close()
	if !strings.Contains(body, "Copy this token now.") {
		t.Fatalf("first read did not reveal the token: %s", body)
	}
	rawToken := createdTokenValue(t, body)
	if cleared := findUICookieOrNil(first.Cookies(), uiTokenRevealCookieNameForTest); cleared == nil || cleared.MaxAge >= 0 {
		t.Fatalf("first read did not clear the cookie: %+v", cleared)
	}

	// Once the browser has honoured that clearing header, the page stops
	// showing it. Replaying the cookie by hand would still reveal the value,
	// but the cookie *is* the secret, so holding it grants nothing new.
	second := e.uiGetWithCookies(t, "/tokens", session)
	body = readBody(t, second)
	second.Body.Close()
	if strings.Contains(body, rawToken) || strings.Contains(body, "Copy this token now.") {
		t.Fatalf("token still revealed after the cookie was cleared: %s", body)
	}
}

// A same-site attacker can set cookies for a sibling subdomain, so an unbound
// value would let them choose what the page displays as "your new token".
func TestTokenRevealCookieIsRejectedWhenNotBoundToTheSession(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	_, session := e.mustProjectMemberToken(t, "token-reveal-forged")

	forged := &http.Cookie{Name: uiTokenRevealCookieNameForTest, Value: "attacker-token.not-a-signature", Path: "/tokens"}
	res := e.uiGetWithCookies(t, "/tokens", session, forged)
	body := readBody(t, res)
	res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("code = %d body = %s", res.StatusCode, body)
	}
	if strings.Contains(body, "attacker-token") || strings.Contains(body, "Copy this token now.") {
		t.Fatalf("forged reveal cookie was displayed: %s", body)
	}
}
