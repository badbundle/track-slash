package server_test

import (
	"bytes"
	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/google/uuid"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

func createAssignedIssueForUI(t *testing.T, e *httpEnv, title string, assigneeID uuid.UUID) model.Issue {
	t.Helper()
	issue, err := e.store.CreateIssue(e.ctx, store.CreateIssueParams{
		ProjectID:  e.projectID,
		Title:      title,
		AssigneeID: &assigneeID,
	})
	if err != nil {
		t.Fatalf("CreateIssue %s: %v", title, err)
	}
	return issue
}

const uiCookieNameForTest = "track_slash_ui_token"

func (e *httpEnv) uiGet(t *testing.T, path, token string) string {
	t.Helper()
	res := e.uiDoNoRedirect(t, http.MethodGet, path, token, nil)
	defer res.Body.Close()
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s code = %d body = %s", path, res.StatusCode, body)
	}
	return body
}

func (e *httpEnv) uiDoNoRedirect(t *testing.T, method, path, token string, body io.Reader) *http.Response {
	t.Helper()
	return e.uiDoNoRedirectWithHeaders(t, method, path, token, body, nil)
}

func (e *httpEnv) uiDoNoRedirectWithHeaders(t *testing.T, method, path, token string, body io.Reader, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(e.ctx, method, e.ts.URL+path, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: uiCookieNameForTest, Value: token, Path: "/"})
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	client := *e.ts.Client()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return res
}

func (e *httpEnv) uiDoMultipartContext(t *testing.T, path, token string, fields map[string]string, filename, content string) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("WriteField: %v", err)
		}
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}
	return e.uiDoNoRedirectWithHeaders(t, http.MethodPost, path, token, &buf, map[string]string{
		"Content-Type": writer.FormDataContentType(),
	})
}

func findUICookie(t *testing.T, cookies []*http.Cookie) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == uiCookieNameForTest {
			return cookie
		}
	}
	t.Fatalf("ui auth cookie not found: %v", cookies)
	return nil
}

func readBody(t *testing.T, res *http.Response) string {
	t.Helper()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return string(data)
}

func requireSecurityHeadersForTest(t *testing.T, headers http.Header) {
	t.Helper()
	csp := headers.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "object-src 'none'", "base-uri 'none'", "form-action 'self'", "frame-ancestors 'none'", "script-src 'self'", "style-src 'self'"} {
		if !strings.Contains(csp, want) {
			t.Fatalf("Content-Security-Policy missing %q: %q", want, csp)
		}
	}
	if strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval") {
		t.Fatalf("Content-Security-Policy permits unsafe execution: %q", csp)
	}
	for name, want := range map[string]string{
		"Permissions-Policy":     "camera=(), geolocation=(), microphone=(), payment=(), usb=()",
		"Referrer-Policy":        "no-referrer",
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
	} {
		if got := headers.Get(name); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
}

func issueContextDetailBlock(t *testing.T, body string) string {
	t.Helper()
	contextLabel := strings.Index(body, ">Context</dt>")
	if contextLabel < 0 {
		t.Fatalf("missing issue context detail row: %s", body)
	}
	blockEnd := contextLabel + 1100
	if blockEnd > len(body) {
		blockEnd = len(body)
	}
	return body[contextLabel:blockEnd]
}

func mainContentBlock(t *testing.T, body string) string {
	t.Helper()
	mainStart := strings.Index(body, `<main id="main"`)
	if mainStart < 0 {
		t.Fatalf("missing main content: %s", body)
	}
	contentStart := strings.Index(body[mainStart:], ">")
	if contentStart < 0 {
		t.Fatalf("malformed main content: %s", body)
	}
	contentStart += mainStart + 1
	contentEnd := strings.Index(body[contentStart:], "</main>")
	if contentEnd < 0 {
		t.Fatalf("missing main content end: %s", body)
	}
	return body[contentStart : contentStart+contentEnd]
}

func requireMarkupOrder(t *testing.T, body, first, second string) {
	t.Helper()
	firstIndex := strings.Index(body, first)
	secondIndex := strings.Index(body, second)
	if firstIndex < 0 || secondIndex < 0 || firstIndex > secondIndex {
		t.Fatalf("%q should render before %q: %s", first, second, body)
	}
}

func (e *httpEnv) mustProjectMemberToken(t *testing.T, label string) (model.User, string) {
	t.Helper()
	user, token := e.mustUserToken(t, label)
	if _, err := e.store.GrantProjectAccess(e.ctx, e.projectID, user.ID); err != nil {
		t.Fatalf("GrantProjectAccess: %v", err)
	}
	return user, token
}
