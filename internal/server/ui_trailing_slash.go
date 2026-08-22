package server

import (
	"net/http"
	"strings"
)

// uiCanonicalPathExemptPrefixes are the route subtrees whose paths must reach
// their handlers byte for byte. JSON API and MCP clients address exact URLs, and
// the static file server resolves the trailing slash itself.
var uiCanonicalPathExemptPrefixes = []string{"/api", "/mcp", "/static"}

// redirectUITrailingSlash sends portal URLs that carry a trailing slash to their
// canonical slash-free form, so /owner/projects/ and /owner/projects resolve to
// the same page. It is restricted to GET and HEAD: a 301 on a POST invites
// clients to replay the request as a GET and drop the body.
func redirectUITrailingSlash(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target, ok := uiCanonicalPath(r)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusMovedPermanently)
	})
}

// uiCanonicalPath reports the slash-free path a request should be redirected to,
// or false when the request must be served as addressed.
func uiCanonicalPath(r *http.Request) (string, bool) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return "", false
	}
	path := r.URL.EscapedPath()
	if !strings.HasSuffix(path, "/") || path == "/" {
		return "", false
	}
	for _, prefix := range uiCanonicalPathExemptPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return "", false
		}
	}
	// Collapse a run of trailing slashes in one hop rather than redirecting
	// once per slash.
	trimmed := strings.TrimRight(path, "/")
	if trimmed == "" {
		trimmed = "/"
	}
	return trimmed, true
}
