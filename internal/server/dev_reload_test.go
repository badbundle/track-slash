package server

import (
	"bufio"
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInjectDevReloadScript(t *testing.T) {
	body := []byte("<!doctype html><html><body><main>ok</main></body></html>")

	got, ok := injectDevReloadScript(body)
	if !ok {
		t.Fatal("injectDevReloadScript ok = false, want true")
	}
	if !bytes.Contains(got, []byte(devReloadScript)) {
		t.Fatalf("injected body missing script: %s", got)
	}
	if bytes.Index(got, []byte(devReloadScript)) > bytes.Index(got, []byte("</body>")) {
		t.Fatalf("script injected after body close: %s", got)
	}
}

func TestInjectDevReloadScriptSkipsIneligibleBody(t *testing.T) {
	cases := []struct {
		name string
		body []byte
	}{
		{name: "fragment", body: []byte("<section>partial</section>")},
		{name: "already injected", body: []byte(`<html><body><script src="/__dev/reload"></script></body></html>`)},
	}

	for _, tc := range cases {
		got, ok := injectDevReloadScript(tc.body)
		if ok {
			t.Fatalf("%s: ok = true, want false", tc.name)
		}
		if !bytes.Equal(got, tc.body) {
			t.Fatalf("%s: body changed: %s", tc.name, got)
		}
	}
}

func TestDevReloadSkipRules(t *testing.T) {
	reloadReq := httptest.NewRequest(http.MethodGet, devReloadPath, nil)
	if !shouldSkipDevReloadInjection(reloadReq) {
		t.Fatal("reload endpoint was not skipped")
	}

	hxReq := httptest.NewRequest(http.MethodGet, "/me/panel", nil)
	hxReq.Header.Set("HX-Request", "true")
	if !shouldSkipDevReloadInjection(hxReq) {
		t.Fatal("HTMX request was not skipped")
	}

	wsReq := httptest.NewRequest(http.MethodGet, "/api/v1/ws", nil)
	wsReq.Header.Set("Upgrade", "websocket")
	if !shouldSkipDevReloadInjection(wsReq) {
		t.Fatal("websocket request was not skipped")
	}

	pageReq := httptest.NewRequest(http.MethodGet, "/me", nil)
	if shouldSkipDevReloadInjection(pageReq) {
		t.Fatal("normal page request was skipped")
	}
}

func TestShouldInjectDevReloadScript(t *testing.T) {
	if !shouldInjectDevReloadScript("text/html; charset=utf-8") {
		t.Fatal("text/html content type not injectable")
	}
	if shouldInjectDevReloadScript("application/json") {
		t.Fatal("application/json content type injectable")
	}
}

func TestDevReloadMiddlewareInjectsFullHTML(t *testing.T) {
	srv := &Server{devReload: true}
	handler := srv.devReloadMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><html><body><main>ok</main></body></html>"))
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/me", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), devReloadPath) {
		t.Fatalf("body missing dev reload script: %s", rec.Body.String())
	}
}

func TestDevReloadMiddlewareSkipsFragmentsAndJSON(t *testing.T) {
	srv := &Server{devReload: true}
	htmlHandler := srv.devReloadMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><html><body><main>ok</main></body></html>"))
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/me/panel", nil)
	req.Header.Set("HX-Request", "true")
	htmlHandler.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), devReloadPath) {
		t.Fatalf("HTMX response includes reload script: %s", rec.Body.String())
	}

	jsonHandler := srv.devReloadMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	rec = httptest.NewRecorder()
	jsonHandler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/users", nil))
	if strings.Contains(rec.Body.String(), devReloadPath) {
		t.Fatalf("JSON response includes reload script: %s", rec.Body.String())
	}
	if rec.Body.String() != `{"ok":true}` {
		t.Fatalf("JSON response changed: %s", rec.Body.String())
	}
}

func TestDevReloadEndpointStreamsReadyEvent(t *testing.T) {
	srv := NewWithOptions(nil, nil, Options{DevReload: true})
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)

	res, err := ts.Client().Get(ts.URL + devReloadPath)
	if err != nil {
		t.Fatalf("GET %s: %v", devReloadPath, err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	if got := res.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	if got := res.Header.Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", got)
	}

	reader := bufio.NewReader(res.Body)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read event line: %v", err)
	}
	if line != "event: ready\n" {
		t.Fatalf("event line = %q, want ready event", line)
	}
	line, err = reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read data line: %v", err)
	}
	if line != "data: ok\n" {
		t.Fatalf("data line = %q, want ok data", line)
	}
}

func TestDevReloadEndpointDisabledByDefault(t *testing.T) {
	srv := New(nil, nil, nil)
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)

	res, err := ts.Client().Get(ts.URL + devReloadPath)
	if err != nil {
		t.Fatalf("GET %s: %v", devReloadPath, err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusNotFound)
	}
}
