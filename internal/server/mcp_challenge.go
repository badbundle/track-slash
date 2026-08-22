package server

import "net/http"

// mcpBearerChallenge is what an unauthenticated /mcp request is told to do
// about it. The MCP SDK only emits WWW-Authenticate when it is configured with
// RFC 9728 resource metadata, which trackslash deliberately does not serve: it
// authenticates MCP with manually issued API tokens, not an OAuth flow.
//
// Without the header, a client that attempts OAuth discovery reports an opaque
// JSON parse failure against the 404 of a metadata document that was never
// meant to exist, and nothing in the error says a token is what is needed.
const mcpBearerChallenge = `Bearer realm="trackslash"`

func mcpBearerChallengeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&mcpChallengeWriter{ResponseWriter: w}, r)
	})
}

type mcpChallengeWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *mcpChallengeWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.wroteHeader = true
		if status == http.StatusUnauthorized && w.Header().Get("WWW-Authenticate") == "" {
			w.Header().Set("WWW-Authenticate", mcpBearerChallenge)
		}
	}
	w.ResponseWriter.WriteHeader(status)
}

// MCP responses can be streamed, so the wrapper must not hide the capabilities
// of the writer underneath it. Unwrap covers http.ResponseController; Flush
// covers code that type-asserts directly.
func (w *mcpChallengeWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *mcpChallengeWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
