package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

type meResp struct {
	User      model.User          `json:"user"`
	TokenKind model.AuthTokenKind `json:"token_kind"`
}

func (s *Server) getMe(w http.ResponseWriter, r *http.Request) {
	auth := currentAuth(r)
	writeJSON(w, http.StatusOK, meResp{User: auth.User, TokenKind: auth.Token.Kind})
}

type createTokenReq struct {
	Name      string               `json:"name"`
	Kind      *model.AuthTokenKind `json:"kind,omitempty"`
	ExpiresAt *time.Time           `json:"expires_at,omitempty"`
}

type createTokenResp struct {
	model.AuthToken
	Token string `json:"token"`
}

func (s *Server) createUserToken(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req createTokenReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 200 {
		writeError(w, http.StatusBadRequest, "name required, max 200 chars")
		return
	}
	kind := model.AuthTokenKindAPI
	if req.Kind != nil {
		if !req.Kind.Valid() {
			writeError(w, http.StatusBadRequest, "invalid token kind")
			return
		}
		kind = *req.Kind
	}
	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now()) {
		writeError(w, http.StatusBadRequest, "expires_at must be in the future")
		return
	}
	created, err := s.store.CreateAuthToken(r.Context(), store.CreateAuthTokenParams{
		UserID: userID, Kind: kind, Name: req.Name, ExpiresAt: req.ExpiresAt,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, createTokenResp{AuthToken: created.Token, Token: created.RawToken})
}

func (s *Server) listUserTokens(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	tokens, err := s.store.ListAuthTokens(r.Context(), userID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

func (s *Server) revokeToken(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.RevokeAuthToken(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
