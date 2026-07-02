package server

import (
	"bytes"
	"io"
	"net/http"
	"strings"
)

const devReloadPath = "/__dev/reload"

const devReloadScript = `<script>
(() => {
  if (!("EventSource" in window)) return;
  let opened = false;
  const source = new EventSource("/__dev/reload");
  source.addEventListener("open", () => {
    if (opened) window.location.reload();
    opened = true;
  });
})();
</script>
`

func (s *Server) devReloadEvents(w http.ResponseWriter, r *http.Request) {
	if !s.devReload {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "event: ready\ndata: ok\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	<-r.Context().Done()
}

func (s *Server) devReloadMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if shouldSkipDevReloadInjection(r) {
			next.ServeHTTP(w, r)
			return
		}

		rec := &devReloadResponseWriter{ResponseWriter: w}
		next.ServeHTTP(rec, r)

		body := rec.body.Bytes()
		if shouldInjectDevReloadScript(rec.Header().Get("Content-Type")) {
			if injected, ok := injectDevReloadScript(body); ok {
				body = injected
				rec.Header().Del("Content-Length")
			}
		}
		rec.flush(body)
	})
}

func shouldSkipDevReloadInjection(r *http.Request) bool {
	if r.URL.Path == devReloadPath {
		return true
	}
	if isHTMXRequest(r) {
		return true
	}
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func shouldInjectDevReloadScript(contentType string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "text/html")
}

func injectDevReloadScript(body []byte) ([]byte, bool) {
	if bytes.Contains(body, []byte(devReloadPath)) {
		return body, false
	}
	const closeBody = "</body>"
	idx := bytes.LastIndex(body, []byte(closeBody))
	if idx < 0 {
		return body, false
	}
	injected := make([]byte, 0, len(body)+len(devReloadScript))
	injected = append(injected, body[:idx]...)
	injected = append(injected, devReloadScript...)
	injected = append(injected, body[idx:]...)
	return injected, true
}

type devReloadResponseWriter struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (w *devReloadResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
}

func (w *devReloadResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(p)
}

func (w *devReloadResponseWriter) flush(body []byte) {
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	w.ResponseWriter.WriteHeader(status)
	_, _ = w.ResponseWriter.Write(body)
}
