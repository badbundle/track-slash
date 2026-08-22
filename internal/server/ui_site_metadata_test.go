package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// RFC 9116 makes Expires mandatory and requires a future value, so a stale one
// makes the document non-conforming.
func TestSecurityTxtIsServedAndConforms(t *testing.T) {
	t.Parallel()
	router := New(nil, nil, nil).Router()

	for _, path := range []string{"/.well-known/security.txt", "/security.txt"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "http://track.example.com"+path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
				t.Fatalf("Content-Type = %q, want text/plain", got)
			}
			fields := parseSecurityTxt(t, rec.Body.String())
			if fields["Contact"] != securityContactAddress {
				t.Fatalf("Contact = %q, want %q", fields["Contact"], securityContactAddress)
			}
			if want := "http://track.example.com/security"; fields["Policy"] != want {
				t.Fatalf("Policy = %q, want %q", fields["Policy"], want)
			}
			if want := "http://track.example.com/.well-known/security.txt"; fields["Canonical"] != want {
				t.Fatalf("Canonical = %q, want %q", fields["Canonical"], want)
			}
			expires, err := time.Parse(time.RFC3339, fields["Expires"])
			if err != nil {
				t.Fatalf("Expires %q: %v", fields["Expires"], err)
			}
			if !expires.After(time.Now()) {
				t.Fatalf("Expires = %s, want a future date", expires)
			}
		})
	}
}

// The configured canonical origin wins, so the document does not name whatever
// host a request happened to arrive on.
func TestSecurityTxtPrefersTheConfiguredOrigin(t *testing.T) {
	t.Parallel()
	srv := NewWithOptions(nil, nil, Options{PublicOrigin: "https://track.example.com"})

	req := httptest.NewRequest(http.MethodGet, "http://internal-lb.local/.well-known/security.txt", nil)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	fields := parseSecurityTxt(t, rec.Body.String())
	if want := "https://track.example.com/security"; fields["Policy"] != want {
		t.Fatalf("Policy = %q, want %q", fields["Policy"], want)
	}
}

func TestWebAppManifestIsServedAndValid(t *testing.T) {
	t.Parallel()
	router := New(nil, nil, nil).Router()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/manifest+json" {
		t.Fatalf("Content-Type = %q", got)
	}
	var manifest struct {
		Name       string `json:"name"`
		StartURL   string `json:"start_url"`
		Display    string `json:"display"`
		ThemeColor string `json:"theme_color"`
		Icons      []struct {
			Src   string `json:"src"`
			Sizes string `json:"sizes"`
			Type  string `json:"type"`
		} `json:"icons"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.Name == "" || manifest.StartURL != "/" || manifest.Display != "standalone" || manifest.ThemeColor == "" {
		t.Fatalf("manifest = %+v", manifest)
	}
	// Installability needs a large icon, and img-src 'self' means every icon
	// has to be served by this app.
	large := false
	for _, icon := range manifest.Icons {
		if !strings.HasPrefix(icon.Src, "/") {
			t.Fatalf("icon %q is not same-origin", icon.Src)
		}
		if icon.Sizes == "512x512" {
			large = true
		}
	}
	if !large {
		t.Fatal("manifest has no 512x512 icon")
	}
}

// Every icon the manifest and the shell reference must actually be served.
func TestSiteIconsAreServed(t *testing.T) {
	t.Parallel()
	router := New(nil, nil, nil).Router()

	for _, tt := range []struct {
		path        string
		contentType string
	}{
		{path: "/favicon.ico", contentType: "image/x-icon"},
		{path: "/static/icon.svg", contentType: "image/svg+xml"},
		{path: "/static/icon-192.png", contentType: "image/png"},
		{path: "/static/icon-512.png", contentType: "image/png"},
	} {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, tt.contentType) {
				t.Fatalf("Content-Type = %q, want prefix %q", got, tt.contentType)
			}
			if rec.Body.Len() == 0 {
				t.Fatal("empty body")
			}
		})
	}
}

// All three are public: a 404 for a signed-out visitor defeats the point.
func TestSiteMetadataNeedsNoAuthentication(t *testing.T) {
	t.Parallel()
	router := New(nil, nil, nil).Router()

	for _, path := range []string{"/.well-known/security.txt", "/security.txt", "/manifest.webmanifest", "/favicon.ico"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 without a session", rec.Code)
			}
		})
	}
}

func parseSecurityTxt(t *testing.T, body string) map[string]string {
	t.Helper()
	fields := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			t.Fatalf("malformed security.txt line %q", line)
		}
		fields[strings.TrimSpace(name)] = strings.TrimSpace(value)
	}
	return fields
}
