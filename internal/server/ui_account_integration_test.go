package server_test

import (
	"errors"
	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/go-webauthn/webauthn/webauthn"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestUITokensPageCreatesAndRevokesToken(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-tokens")

	body := e.uiGet(t, "/tokens", token)
	if !strings.Contains(body, "New API token") || !strings.Contains(body, "Tokens") {
		t.Fatalf("tokens page missing form/header: %s", body)
	}
	csrfToken := uiCSRFTokenForTest("session", token)
	if !strings.Contains(body, `name="csrf_token" value="`+csrfToken+`"`) {
		t.Fatalf("tokens page missing session-bound CSRF field: %s", body)
	}

	form := url.Values{"name": {"from ui"}, "csrf_token": {csrfToken}}
	res := e.uiDoNoRedirectWithHeaders(t, http.MethodPost, "/tokens", token, strings.NewReader(form.Encode()), map[string]string{
		"Origin":       e.ts.URL,
		"X-CSRF-Token": "",
	})
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("create token code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "Copy this token now.") {
		t.Fatalf("body missing created token notice: %s", body)
	}
	rawToken := createdTokenValue(t, body)
	if nextBody := e.uiGet(t, "/tokens", token); strings.Contains(nextBody, rawToken) || strings.Contains(nextBody, "Copy this token now.") {
		t.Fatalf("created token was shown after its one-time response: %s", nextBody)
	}
	tokens, err := e.store.ListAuthTokens(e.ctx, user.ID)
	if err != nil {
		t.Fatalf("ListAuthTokens: %v", err)
	}
	var created *model.AuthToken
	for i := range tokens {
		if tokens[i].Name == "from ui" {
			created = &tokens[i]
			break
		}
	}
	if created == nil {
		t.Fatalf("created token missing: %+v", tokens)
	}
	revokeForm := url.Values{"csrf_token": {csrfToken}}
	res = e.uiDoNoRedirectWithHeaders(t, http.MethodPost, "/tokens/"+created.ID.String()+"/revoke", token, strings.NewReader(revokeForm.Encode()), map[string]string{
		"Origin":       e.ts.URL,
		"X-CSRF-Token": "",
	})
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("revoke code = %d body = %s", res.StatusCode, readBody(t, res))
	}
	tokens, err = e.store.ListAuthTokens(e.ctx, user.ID)
	if err != nil {
		t.Fatalf("ListAuthTokens after revoke: %v", err)
	}
	for _, tok := range tokens {
		if tok.ID == created.ID && tok.RevokedAt == nil {
			t.Fatalf("token not revoked: %+v", tok)
		}
	}
}

func TestUITokenCreationCSRFVariants(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	user, token := e.mustProjectMemberToken(t, "ui-token-csrf")
	csrfToken := uiCSRFTokenForTest("session", token)

	htmxForm := url.Values{"name": {"from htmx"}}
	res := e.uiDoNoRedirectWithHeaders(t, http.MethodPost, "/tokens", token, strings.NewReader(htmxForm.Encode()), map[string]string{
		"HX-Request": "true",
		"Origin":     e.ts.URL,
	})
	body := readBody(t, res)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "Copy this token now.") {
		t.Fatalf("HTMX create token code = %d body = %s", res.StatusCode, body)
	}
	htmxRawToken := createdTokenValue(t, body)
	if nextBody := e.uiGet(t, "/tokens", token); strings.Contains(nextBody, htmxRawToken) {
		t.Fatalf("HTMX-created token was shown after its one-time response: %s", nextBody)
	}

	missingForm := url.Values{"name": {"missing csrf"}}
	res = e.uiDoNoRedirectWithHeaders(t, http.MethodPost, "/tokens", token, strings.NewReader(missingForm.Encode()), map[string]string{
		"Origin":       e.ts.URL,
		"X-CSRF-Token": "",
	})
	body = readBody(t, res)
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden || !strings.Contains(body, "CSRF validation failed.") {
		t.Fatalf("missing CSRF code = %d body = %s", res.StatusCode, body)
	}

	invalidForm := url.Values{"name": {"invalid csrf"}, "csrf_token": {csrfToken}}
	res = e.uiDoNoRedirectWithHeaders(t, http.MethodPost, "/tokens", token, strings.NewReader(invalidForm.Encode()), map[string]string{
		"Origin":       e.ts.URL,
		"X-CSRF-Token": "wrong",
	})
	body = readBody(t, res)
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden || !strings.Contains(body, "CSRF validation failed.") {
		t.Fatalf("invalid CSRF code = %d body = %s", res.StatusCode, body)
	}

	past := time.Now().Add(-time.Minute)
	expired, err := e.store.CreateAuthToken(e.ctx, store.CreateAuthTokenParams{
		UserID:    user.ID,
		Kind:      model.AuthTokenKindSession,
		Name:      "expired session",
		ExpiresAt: &past,
	})
	if err != nil {
		t.Fatalf("CreateAuthToken expired session: %v", err)
	}
	expiredForm := url.Values{
		"name":       {"expired session attempt"},
		"csrf_token": {uiCSRFTokenForTest("session", expired.RawToken)},
	}
	res = e.uiDoNoRedirectWithHeaders(t, http.MethodPost, "/tokens", expired.RawToken, strings.NewReader(expiredForm.Encode()), map[string]string{"Origin": e.ts.URL})
	body = readBody(t, res)
	res.Body.Close()
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/login?next=%2Ftokens" {
		t.Fatalf("expired session code = %d location = %q body = %s", res.StatusCode, res.Header.Get("Location"), body)
	}
	if cookie := res.Header.Get("Set-Cookie"); !strings.Contains(cookie, uiCookieNameForTest+"=") || !strings.Contains(cookie, "Max-Age=0") {
		t.Fatalf("expired session Set-Cookie = %q, want cleared session", cookie)
	}

	tokens, err := e.store.ListAuthTokens(e.ctx, user.ID)
	if err != nil {
		t.Fatalf("ListAuthTokens: %v", err)
	}
	for _, authToken := range tokens {
		switch authToken.Name {
		case "missing csrf", "invalid csrf", "expired session attempt":
			t.Fatalf("rejected token creation persisted %+v", authToken)
		}
	}
}

func createdTokenValue(t *testing.T, body string) string {
	t.Helper()
	codeStart := strings.Index(body, "<code")
	if codeStart < 0 {
		t.Fatalf("created token code missing: %s", body)
	}
	valueStart := strings.Index(body[codeStart:], ">")
	if valueStart < 0 {
		t.Fatalf("created token code malformed: %s", body)
	}
	valueStart += codeStart + 1
	valueEnd := strings.Index(body[valueStart:], "</code>")
	if valueEnd < 0 {
		t.Fatalf("created token code closing tag missing: %s", body)
	}
	value := strings.TrimSpace(body[valueStart : valueStart+valueEnd])
	if value == "" {
		t.Fatalf("created token value empty: %s", body)
	}
	return value
}

func TestUISettingsPageUpdatesProfileAndPassword(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	username := "uisettings" + strings.ToLower(uniqueProjectKey(t))
	oldPassword := "correct-horse-battery"
	newPassword := "new-correct-horse"
	user, err := e.store.CreateAccount(e.ctx, store.CreateAccountParams{
		Username: username,
		Password: oldPassword,
		Name:     "Old UI",
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	token, err := e.store.CreateAuthToken(e.ctx, store.CreateAuthTokenParams{
		UserID: user.ID,
		Kind:   model.AuthTokenKindSession,
		Name:   "session",
	})
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}

	body := e.uiGet(t, "/settings", token.RawToken)
	for _, want := range []string{"Settings", "Display name", "Email", "Password login", "On", "Current password", "New password", "Passkeys", "Saved passkeys", "Add a passkey", "Passkey label", "Enter current password", "Required before changing passkeys.", "Continue", "Add passkey", "No passkeys added.", "Legal", "Review the terms, privacy notice, and security policy for trackslash.", `aria-label="Legal"`, `href="/terms"`, `href="/privacy"`, `href="/security"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("settings body missing %q: %s", want, body)
		}
	}
	if got := strings.Count(body, `aria-label="Legal"`); got != 1 {
		t.Fatalf("settings legal navigation count = %d, want 1: %s", got, body)
	}
	sidebarStart := strings.Index(body, `<aside id="app-sidebar"`)
	if sidebarStart < 0 {
		t.Fatalf("settings body missing sidebar: %s", body)
	}
	sidebarEnd := strings.Index(body[sidebarStart:], `</aside>`)
	if sidebarEnd < 0 {
		t.Fatalf("settings body has unterminated sidebar: %s", body)
	}
	sidebar := body[sidebarStart : sidebarStart+sidebarEnd]
	for _, notWant := range []string{`aria-label="Legal"`, `href="/terms"`, `href="/privacy"`, `href="/security"`} {
		if strings.Contains(sidebar, notWant) {
			t.Fatalf("settings sidebar still contains legal link %q: %s", notWant, sidebar)
		}
	}
	if strings.Contains(body, "Disable password login") || strings.Contains(body, "Enable password login") {
		t.Fatalf("settings body shows password login toggle without passkey: %s", body)
	}
	for _, rejected := range []string{"Use passkey", "Confirm with", "Security check", "Leave blank to confirm", "Needed to add or remove passkeys.", `for="passkey_name">Name`} {
		if strings.Contains(body, rejected) {
			t.Fatalf("settings body still shows confusing passkey copy %q: %s", rejected, body)
		}
	}
	if strings.Contains(body, `data-passkey-password-modal hidden class=`) || strings.Contains(body, `data-passkey-password-modal class="fixed inset-0 z-50 grid`) {
		t.Fatalf("settings body renders passkey password modal open by default: %s", body)
	}

	form := url.Values{"name": {"New UI"}, "email": {"ui@example.com"}}
	res := e.uiDoNoRedirect(t, http.MethodPost, "/settings/profile", token.RawToken, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("profile code = %d body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "Profile saved.") || !strings.Contains(body, "New UI") || !strings.Contains(body, "ui@example.com") {
		t.Fatalf("profile body missing saved values: %s", body)
	}

	form = url.Values{"current_password": {"wrong-password"}, "new_password": {newPassword}}
	res = e.uiDoNoRedirect(t, http.MethodPost, "/settings/password", token.RawToken, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "Current password not accepted.") {
		t.Fatalf("bad password code = %d body = %s", res.StatusCode, body)
	}

	form = url.Values{"current_password": {oldPassword}, "new_password": {newPassword}}
	res = e.uiDoNoRedirect(t, http.MethodPost, "/settings/password", token.RawToken, strings.NewReader(form.Encode()))
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "Password changed.") {
		t.Fatalf("password code = %d body = %s", res.StatusCode, body)
	}
	if _, err := e.store.AuthenticatePassword(e.ctx, username, oldPassword); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("old password err = %v, want ErrUnauthorized", err)
	}
	if _, err := e.store.AuthenticatePassword(e.ctx, username, newPassword); err != nil {
		t.Fatalf("new password auth: %v", err)
	}
}

func TestUISettingsPasswordLoginDisabledUsesPasskeyReauth(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	username := "uipwdoff" + strings.ToLower(uniqueProjectKey(t))
	user, err := e.store.CreateAccount(e.ctx, store.CreateAccountParams{
		Username: username,
		Password: "correct-horse-battery",
		Name:     "UI Password Off",
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if _, err := e.store.AddPasskeyCredential(e.ctx, user.ID, "localhost", "Laptop", serverPasskeyCredential("credential-"+uniqueProjectKey(t), 1)); err != nil {
		t.Fatalf("AddPasskeyCredential: %v", err)
	}
	if _, err := e.store.SetPasswordLoginEnabled(e.ctx, user.ID, false); err != nil {
		t.Fatalf("SetPasswordLoginEnabled: %v", err)
	}
	token, err := e.store.CreateAuthToken(e.ctx, store.CreateAuthTokenParams{
		UserID: user.ID,
		Kind:   model.AuthTokenKindSession,
		Name:   "session",
	})
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}

	body := e.uiGet(t, "/settings", token.RawToken)
	for _, want := range []string{"Password login", "Off", "Enable password login", "Password login is off.", "Passkeys", "Laptop"} {
		if !strings.Contains(body, want) {
			t.Fatalf("settings body missing %q: %s", want, body)
		}
	}
	for _, rejected := range []string{`id="current_password"`, `for="current_password"`, "New password", "Change password", "Enter current password", "Required before changing passkeys.", "<div data-passkey-password-modal", "Disable password login", "Security check", "Confirm with"} {
		if strings.Contains(body, rejected) {
			t.Fatalf("disabled password settings still shows %q: %s", rejected, body)
		}
	}

	reauth, err := e.store.CreatePasskeyReauthToken(e.ctx, user.ID)
	if err != nil {
		t.Fatalf("CreatePasskeyReauthToken: %v", err)
	}
	res := e.uiDoNoRedirectWithHeaders(t, http.MethodPost, "/settings/password-login", token.RawToken, strings.NewReader(`{"enabled":true,"reauth_token":"`+reauth+`"}`), map[string]string{
		"Content-Type": "application/json",
	})
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("ui password-login code = %d body = %s", res.StatusCode, body)
	}
	state := decode[model.PasswordLoginState](t, []byte(body))
	if !state.Enabled || !state.CanDisable {
		t.Fatalf("ui password-login state = %+v", state)
	}
	if _, err := e.store.AuthenticatePassword(e.ctx, username, "correct-horse-battery"); err != nil {
		t.Fatalf("AuthenticatePassword after UI enable: %v", err)
	}
}

func TestUISettingsPasskeyOnlyAccountHidesPasskeyPasswordField(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	user, err := e.store.CreatePasskeyOnlyAccount(e.ctx, store.CreatePasskeyOnlyAccountParams{
		Username:       "uipasskey" + strings.ToLower(uniqueProjectKey(t)),
		Name:           "UI Passkey",
		RPID:           "localhost",
		UserHandle:     []byte("ui-handle-" + uniqueProjectKey(t)),
		CredentialName: "MacBook",
		Credential: webauthn.Credential{
			ID:        []byte("ui-credential-" + uniqueProjectKey(t)),
			PublicKey: []byte("ui-public-key"),
			Flags: webauthn.CredentialFlags{
				UserPresent:  true,
				UserVerified: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("CreatePasskeyOnlyAccount: %v", err)
	}
	token, err := e.store.CreateAuthToken(e.ctx, store.CreateAuthTokenParams{
		UserID: user.ID,
		Kind:   model.AuthTokenKindSession,
		Name:   "session",
	})
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}

	body := e.uiGet(t, "/settings", token.RawToken)
	for _, want := range []string{"Password login", "No password", "No password is set.", "Passkeys", "Saved passkeys", "Add a passkey", "Passkey label", "MacBook"} {
		if !strings.Contains(body, want) {
			t.Fatalf("settings body missing %q: %s", want, body)
		}
	}
	for _, rejected := range []string{"Security check", "Use passkey", "Confirm with", "Leave blank to confirm", "Needed to add or remove passkeys.", `id="current_password"`, `for="current_password"`, "New password", "Change password", "Enter current password", "Required before changing passkeys.", "<div data-passkey-password-modal", `id="passkey_current_password"`, "Enable password login", "Disable password login"} {
		if strings.Contains(body, rejected) {
			t.Fatalf("settings body still shows password passkey copy %q: %s", rejected, body)
		}
	}
}
