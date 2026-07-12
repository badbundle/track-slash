package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

type authContextKey struct{}

type authContext struct {
	User  model.User
	Token model.AuthToken
}

func (s *Server) authenticateRequest(r *http.Request) (authContext, error) {
	raw, ok := bearerToken(r)
	if !ok {
		return authContext{}, store.ErrUnauthorized
	}
	auth, err := s.store.AuthenticateToken(r.Context(), raw)
	if err != nil {
		return authContext{}, err
	}
	return authContext{User: auth.User, Token: auth.Token}, nil
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, err := s.authenticateRequest(r)
		if err != nil {
			if errors.Is(err, store.ErrUnauthorized) {
				writeUnauthorized(w)
				return
			}
			writeStoreError(w, err)
			return
		}
		ctx := context.WithValue(r.Context(), authContextKey{}, auth)
		ctx = store.WithActor(ctx, auth.User.ID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func bearerToken(r *http.Request) (string, bool) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return "", false
	}
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

func currentAuth(r *http.Request) authContext {
	auth, _ := r.Context().Value(authContextKey{}).(authContext)
	return auth
}

func currentUser(r *http.Request) model.User {
	return currentAuth(r).User
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !currentUser(r).IsAdmin {
		writeForbidden(w)
		return false
	}
	return true
}

func (s *Server) requireProjectAccess(w http.ResponseWriter, r *http.Request, projectID uuid.UUID) bool {
	user := currentUser(r)
	ok, err := s.store.UserCanAccessProject(r.Context(), user, projectID)
	if err != nil {
		writeStoreError(w, err)
		return false
	}
	if !ok {
		writeForbidden(w)
		return false
	}
	return true
}

func (s *Server) requireProjectWriteAccess(w http.ResponseWriter, r *http.Request, projectID uuid.UUID) bool {
	ok, err := s.store.UserCanWriteProject(r.Context(), currentUser(r), projectID)
	if err != nil {
		writeStoreError(w, err)
		return false
	}
	if !ok {
		writeForbidden(w)
		return false
	}
	return true
}

func (s *Server) requireProjectMemberManagement(w http.ResponseWriter, r *http.Request, projectID uuid.UUID) bool {
	ok, err := s.store.UserCanManageProjectMembers(r.Context(), currentUser(r), projectID)
	if err != nil {
		writeStoreError(w, err)
		return false
	}
	if !ok {
		writeForbidden(w)
		return false
	}
	return true
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	writeError(w, http.StatusUnauthorized, "unauthorized")
}

func writeForbidden(w http.ResponseWriter) {
	writeError(w, http.StatusForbidden, "forbidden")
}
