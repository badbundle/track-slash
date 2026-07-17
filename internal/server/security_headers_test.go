package server

import (
	"crypto/tls"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestSecurityHeadersPolicy(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name           string
		publicOrigin   string
		directTLS      bool
		forwardedProto string
		wantHSTS       bool
	}{
		{name: "localhost development"},
		{name: "direct TLS is not an HSTS opt-in", directTLS: true},
		{name: "forwarded TLS is not an HSTS opt-in", forwardedProto: "https"},
		{name: "configured HTTPS deployment", publicOrigin: "https://track.example.com", wantHSTS: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := NewWithOptions(nil, nil, Options{PublicOrigin: tt.publicOrigin})
			handler := srv.securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(http.MethodGet, "http://localhost:8080/test", nil)
			if tt.directTLS {
				req.TLS = &tls.ConnectionState{}
			}
			if tt.forwardedProto != "" {
				req.Header.Set("X-Forwarded-Proto", tt.forwardedProto)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			assertSecurityHeaders(t, rec.Header())
			if got := rec.Header().Get("Strict-Transport-Security"); (got != "") != tt.wantHSTS {
				t.Fatalf("Strict-Transport-Security = %q, want present %v", got, tt.wantHSTS)
			} else if tt.wantHSTS && got != strictTransportPolicy {
				t.Fatalf("Strict-Transport-Security = %q, want %q", got, strictTransportPolicy)
			}
		})
	}
}

func TestUITemplatesNeedNoInlineCSPExceptions(t *testing.T) {
	t.Parallel()
	paths, err := fs.Glob(uiTemplateFS, "templates/*.html")
	if err != nil {
		t.Fatalf("glob templates: %v", err)
	}
	inlineStyle := regexp.MustCompile(`(?i)<style\b|\sstyle\s*=`)
	scriptTag := regexp.MustCompile(`(?i)<script(?:\s[^>]*)?>`)
	scriptSource := regexp.MustCompile(`(?i)\ssrc\s*=`)
	for _, path := range paths {
		body, err := uiTemplateFS.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if match := inlineStyle.Find(body); match != nil {
			t.Fatalf("%s contains inline style %q", path, match)
		}
		for _, tag := range scriptTag.FindAll(body, -1) {
			if !scriptSource.Match(tag) {
				t.Fatalf("%s contains inline script tag %q", path, tag)
			}
		}
	}
	appJS, err := uiTemplateFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	if strings.Contains(string(appJS), ".style.") {
		t.Fatal("app.js writes inline styles")
	}
	shell, err := uiTemplateFS.ReadFile("templates/shell.html")
	if err != nil {
		t.Fatalf("read shell.html: %v", err)
	}
	for _, want := range []string{`"includeIndicatorStyles":false`, `"allowEval":false`, `"allowScriptTags":false`} {
		if !strings.Contains(string(shell), want) {
			t.Fatalf("shell HTMX config missing %s", want)
		}
	}
}

func TestSecurityHeadersOnPublicResponseTypes(t *testing.T) {
	t.Parallel()
	router := New(nil, nil, nil).Router()
	for _, tt := range []struct {
		name        string
		method      string
		path        string
		body        string
		contentType string
	}{
		{name: "login HTML", method: http.MethodGet, path: "/login", contentType: "text/html"},
		{name: "API JSON", method: http.MethodPost, path: "/api/v1/accounts", body: "{", contentType: "application/json"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			assertSecurityHeaders(t, rec.Header())
			if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, tt.contentType) {
				t.Fatalf("Content-Type = %q, want prefix %q", got, tt.contentType)
			}
			if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
				t.Fatalf("localhost Strict-Transport-Security = %q, want absent", got)
			}
		})
	}
}

func assertSecurityHeaders(t *testing.T, headers http.Header) {
	t.Helper()
	if got := headers.Get("Content-Security-Policy"); got != contentSecurityPolicy {
		t.Fatalf("Content-Security-Policy = %q, want %q", got, contentSecurityPolicy)
	}
	if strings.Contains(contentSecurityPolicy, "unsafe-inline") || strings.Contains(contentSecurityPolicy, "unsafe-eval") {
		t.Fatalf("Content-Security-Policy permits unsafe execution: %q", contentSecurityPolicy)
	}
	for name, want := range map[string]string{
		"Permissions-Policy":     permissionsPolicy,
		"Referrer-Policy":        "no-referrer",
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
	} {
		if got := headers.Get(name); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
}
