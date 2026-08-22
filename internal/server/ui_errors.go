package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/bradleymackey/track-slash/internal/store"
)

type uiErrorPanelData struct {
	Status  int
	Title   string
	Message string
}

// uiNotFound renders an unmatched portal URL inside the app shell instead of
// letting Go's plain-text default escape the layout.
func (s *Server) uiNotFound(w http.ResponseWriter, r *http.Request) {
	s.renderUIErrorPanel(w, r, uiErrorPanelData{
		Status:  http.StatusNotFound,
		Title:   "Page not found",
		Message: "That page does not exist, or you do not have access to it.",
	})
}

// uiMethodNotAllowed answers a known path addressed with the wrong method. Go's
// default writes a 405 with an empty body.
func (s *Server) uiMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	s.renderUIErrorPanel(w, r, uiErrorPanelData{
		Status:  http.StatusMethodNotAllowed,
		Title:   "Method not allowed",
		Message: "That page cannot be reached this way.",
	})
}

func (s *Server) renderUIErrorPanel(w http.ResponseWriter, r *http.Request, data uiErrorPanelData) {
	s.renderUIShell(w, r, data.Status, uiShellData{ErrorPanel: &data})
}

func apiNotFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, "not found")
}

func apiMethodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

// uiOptionalAuth resolves the session cookie when one is present so error pages
// render with the signed-in shell, and otherwise continues anonymously. Unlike
// uiAuthMiddleware it never redirects: an unauthenticated visitor who mistypes a
// URL should see the 404, not a login prompt for a page that does not exist.
func (s *Server) uiOptionalAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(uiAuthCookieName)
		// A store-less server still has to answer 404, so the shell degrades to
		// its anonymous form rather than dereferencing a nil store.
		if err != nil || strings.TrimSpace(cookie.Value) == "" || s.store == nil {
			next.ServeHTTP(w, r)
			return
		}
		auth, err := s.store.AuthenticateToken(r.Context(), cookie.Value)
		if err != nil {
			// An expired or revoked session simply renders anonymously; the
			// error page has nothing to protect. Any other error means the
			// database is unreachable, which only warrants a log line here.
			if !errors.Is(err, store.ErrUnauthorized) {
				logInternalError("ui optional auth authenticate token", err)
			}
			next.ServeHTTP(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), authContextKey{}, authContext{User: auth.User, Token: auth.Token})
		ctx = store.WithActor(ctx, auth.User.ID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}
