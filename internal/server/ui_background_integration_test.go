package server_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

const (
	uiDefaultBackdropClass = `class="brand-backdrop brand-backdrop-ambient"`
	uiBackgroundPresetMark = "brand-backdrop-preset"
)

func uiPresetBackdropClass(preset string) string {
	return `class="brand-backdrop brand-backdrop-ambient brand-backdrop-preset brand-backdrop-preset-` + preset + `"`
}

func TestUIBackgroundPreset(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	_, token := e.mustSessionToken(t, "ui-background")
	postBackground := func(t *testing.T, body string, headers map[string]string) (int, string) {
		t.Helper()
		res := e.uiDoNoRedirectWithHeaders(t, http.MethodPost, "/settings/background", token, strings.NewReader(body), headers)
		defer res.Body.Close()
		return res.StatusCode, readBody(t, res)
	}

	// Nobody's look changes until they pick something.
	profile := e.uiGet(t, "/settings/profile", token)
	for _, want := range []string{"Appearance", `action="/settings/background"`, `hx-post="/settings/background"`, `name="background" value="indigo" checked`, "Emerald", "Save background", uiDefaultBackdropClass} {
		if !strings.Contains(profile, want) {
			t.Fatalf("profile missing %q: %s", want, profile)
		}
	}
	if strings.Contains(profile, uiBackgroundPresetMark) {
		t.Fatalf("default profile renders a preset backdrop: %s", profile)
	}

	// A plain form post lands on Profile with the new backdrop in place, and
	// every later page keeps it.
	code, body := postBackground(t, url.Values{"background": {"emerald"}}.Encode(), nil)
	if code != http.StatusOK {
		t.Fatalf("save emerald code = %d body = %s", code, body)
	}
	for _, want := range []string{"Background saved.", `name="background" value="emerald" checked`, uiPresetBackdropClass("emerald"), `data-sidebar-view="profile"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("saved profile missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `hx-swap-oob`) || strings.Count(body, "data-brand-backdrop") != 1 {
		t.Fatalf("full-page save rendered more than one backdrop: %s", body)
	}
	for _, path := range []string{"/projects", "/me", "/settings/login", "/tokens"} {
		if page := e.uiGet(t, path, token); !strings.Contains(page, uiPresetBackdropClass("emerald")) {
			t.Fatalf("%s missing the emerald backdrop: %s", path, page)
		}
	}
	// So does the branded error page a browser gets for a page it can't open.
	for path, want := range map[string]int{e.projectPath() + "/all": http.StatusForbidden, "/nobody-here/projects/NOPE/all": http.StatusNotFound} {
		res := e.uiDoNoRedirectWithHeaders(t, http.MethodGet, path, token, nil, map[string]string{"Accept": "text/html"})
		page := readBody(t, res)
		res.Body.Close()
		if res.StatusCode != want || !strings.Contains(page, uiPresetBackdropClass("emerald")) {
			t.Fatalf("error page %s code = %d missing the emerald backdrop: %s", path, res.StatusCode, page)
		}
	}

	// An htmx save replaces #main, so it carries the backdrop out of band.
	code, body = postBackground(t, url.Values{"background": {"slate"}}.Encode(), map[string]string{"HX-Request": "true"})
	if code != http.StatusOK || !strings.Contains(body, "Background saved.") || !strings.Contains(body, `id="brand-backdrop" data-brand-backdrop hx-swap-oob="true" `+uiPresetBackdropClass("slate")) {
		t.Fatalf("htmx save slate code = %d body = %s", code, body)
	}
	if strings.Contains(body, "<html") {
		t.Fatalf("htmx save rendered the whole document: %s", body)
	}
	// An htmx navigation leaves the backdrop alone, so it doesn't redraw it.
	res := e.uiDoNoRedirectWithHeaders(t, http.MethodGet, "/projects", token, nil, map[string]string{"HX-Request": "true"})
	page := readBody(t, res)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || strings.Contains(page, "data-brand-backdrop") {
		t.Fatalf("htmx navigation code = %d rendered a backdrop: %s", res.StatusCode, page)
	}

	// Bad values are rejected and change nothing.
	for name, form := range map[string]string{
		"unknown preset": url.Values{"background": {"rose"}}.Encode(),
		"missing preset": "",
	} {
		code, body := postBackground(t, form, nil)
		if code != http.StatusOK || !strings.Contains(body, "Choose one of the backgrounds.") || strings.Contains(body, "Background saved.") || !strings.Contains(body, uiPresetBackdropClass("slate")) {
			t.Fatalf("%s code = %d body = %s", name, code, body)
		}
	}
	code, body = postBackground(t, "background=%zz", nil)
	if code != http.StatusOK || !strings.Contains(body, "Unable to read form.") || !strings.Contains(body, uiPresetBackdropClass("slate")) {
		t.Fatalf("malformed form code = %d body = %s", code, body)
	}

	// Picking Indigo again goes back to the plain brand backdrop.
	code, body = postBackground(t, url.Values{"background": {"indigo"}}.Encode(), map[string]string{"HX-Request": "true"})
	if code != http.StatusOK || !strings.Contains(body, `id="brand-backdrop" data-brand-backdrop hx-swap-oob="true" `+uiDefaultBackdropClass) || strings.Contains(body, uiBackgroundPresetMark) {
		t.Fatalf("htmx save indigo code = %d body = %s", code, body)
	}
}

// Pages that don't draw the app shell keep the default brand backdrop, and a
// stored value that is no longer a preset renders as the default.
func TestUIBackgroundPresetFallbacks(t *testing.T) {
	t.Parallel()
	e := newHTTPEnv(t)
	user, token := e.mustSessionToken(t, "ui-background-fallback")
	res := e.uiDoNoRedirect(t, http.MethodPost, "/settings/background", token, strings.NewReader(url.Values{"background": {"amber"}}.Encode()))
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("save amber code = %d", res.StatusCode)
	}
	for _, path := range []string{"/terms", "/privacy"} {
		if page := e.uiGet(t, path, token); strings.Contains(page, uiBackgroundPresetMark) || !strings.Contains(page, uiDefaultBackdropClass) {
			t.Fatalf("%s uses the signed-in preset: %s", path, page)
		}
	}
	login := e.uiGet(t, "/login", "")
	if strings.Contains(login, uiBackgroundPresetMark) || !strings.Contains(login, `class="brand-backdrop"`) {
		t.Fatalf("login page backdrop changed: %s", login)
	}

	if _, err := e.pool.Exec(e.ctx, `UPDATE users SET background = 'violet' WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("store retired preset: %v", err)
	}
	for _, path := range []string{"/projects", "/settings/profile"} {
		page := e.uiGet(t, path, token)
		if strings.Contains(page, uiBackgroundPresetMark) || !strings.Contains(page, uiDefaultBackdropClass) {
			t.Fatalf("%s did not fall back to the default backdrop: %s", path, page)
		}
	}
	if profile := e.uiGet(t, "/settings/profile", token); !strings.Contains(profile, `name="background" value="indigo" checked`) {
		t.Fatalf("profile does not show the fallback as chosen: %s", profile)
	}
}
