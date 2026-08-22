package server

import (
	"net/http"
	"strings"
)

// uiStaticFileServer serves the embedded asset tree. Directory requests are
// refused: the tree ships no index.html, so Go's file server would answer them
// with a generated listing that enumerates every asset in the build.
//
// A 404 here stays plain rather than rendering the app shell, matching what a
// missing asset already returns and keeping asset misses off the database.
func uiStaticFileServer() http.Handler {
	files := http.FileServerFS(uiStaticFS)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// StripPrefix leaves "" for /static/ itself and "sub/" for a nested
		// directory; the file server rewrites both to a directory read.
		if r.URL.Path == "" || strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	})
}
