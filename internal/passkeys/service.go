package passkeys

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

const defaultCeremonyTTL = 5 * time.Minute

type Service struct {
	store        *store.Store
	publicOrigin string
	displayName  string
}

type SignupOptionsParams struct {
	Username string
	Name     string
}

type AddOptionsParams struct {
	User model.User
	Name string
}

type CreationOptions struct {
	CeremonyID uuid.UUID                                   `json:"ceremony_id"`
	PublicKey  protocol.PublicKeyCredentialCreationOptions `json:"publicKey"`
	Mediation  protocol.CredentialMediationRequirement     `json:"mediation,omitempty"`
}

type AssertionOptions struct {
	CeremonyID uuid.UUID                                  `json:"ceremony_id"`
	PublicKey  protocol.PublicKeyCredentialRequestOptions `json:"publicKey"`
	Mediation  protocol.CredentialMediationRequirement    `json:"mediation,omitempty"`
}

func New(st *store.Store, publicOrigin string) *Service {
	return &Service{
		store:        st,
		publicOrigin: strings.TrimSpace(publicOrigin),
		displayName:  "track-slash",
	}
}

func (s *Service) BeginSignup(ctx context.Context, r *http.Request, p SignupOptionsParams) (CreationOptions, error) {
	username, err := store.NormalizeUsername(p.Username)
	if err != nil {
		return CreationOptions{}, err
	}
	if _, err := s.store.GetUserByUsername(ctx, username); err == nil {
		return CreationOptions{}, store.ErrConflict
	} else if !errors.Is(err, store.ErrNotFound) {
		return CreationOptions{}, err
	}

	rp, err := s.rp(r)
	if err != nil {
		return CreationOptions{}, err
	}
	handle, err := randomHandle()
	if err != nil {
		return CreationOptions{}, err
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = username
	}
	user := webAuthnUser{
		handle:      handle,
		username:    username,
		displayName: name,
	}
	creation, session, err := rp.web.BeginRegistration(user)
	if err != nil {
		return CreationOptions{}, err
	}
	id, err := s.saveSession(ctx, store.CreatePasskeySessionParams{
		Kind:        store.PasskeyCeremonySignup,
		RPID:        rp.id,
		Username:    username,
		DisplayName: name,
		UserHandle:  handle,
		Challenge:   session.Challenge,
		SessionData: mustSessionJSON(session),
		ExpiresAt:   sessionExpires(session),
	})
	if err != nil {
		return CreationOptions{}, err
	}
	return CreationOptions{CeremonyID: id, PublicKey: creation.Response, Mediation: creation.Mediation}, nil
}

func (s *Service) FinishSignup(ctx context.Context, r *http.Request, ceremonyID uuid.UUID, response []byte) (model.User, error) {
	rp, session, err := s.consumeSession(ctx, r, ceremonyID, store.PasskeyCeremonySignup)
	if err != nil {
		return model.User{}, err
	}
	user := webAuthnUser{
		handle:      session.UserHandle,
		username:    session.Username,
		displayName: session.DisplayName,
	}
	credential, err := rp.web.FinishRegistration(user, webAuthnSession(session), requestWithJSONBody(r, response))
	if err != nil {
		return model.User{}, err
	}
	return s.store.CreatePasskeyOnlyAccount(ctx, store.CreatePasskeyOnlyAccountParams{
		Username:       session.Username,
		Name:           session.DisplayName,
		RPID:           session.RPID,
		UserHandle:     session.UserHandle,
		CredentialName: session.CredentialName,
		Credential:     *credential,
	})
}

func (s *Service) BeginLogin(ctx context.Context, r *http.Request) (AssertionOptions, error) {
	rp, err := s.rp(r)
	if err != nil {
		return AssertionOptions{}, err
	}
	assertion, session, err := rp.web.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return AssertionOptions{}, err
	}
	id, err := s.saveSession(ctx, store.CreatePasskeySessionParams{
		Kind:        store.PasskeyCeremonyLogin,
		RPID:        rp.id,
		Challenge:   session.Challenge,
		SessionData: mustSessionJSON(session),
		ExpiresAt:   sessionExpires(session),
	})
	if err != nil {
		return AssertionOptions{}, err
	}
	return AssertionOptions{CeremonyID: id, PublicKey: assertion.Response, Mediation: assertion.Mediation}, nil
}

func (s *Service) FinishLogin(ctx context.Context, r *http.Request, ceremonyID uuid.UUID, response []byte) (model.User, error) {
	rp, session, err := s.consumeSession(ctx, r, ceremonyID, store.PasskeyCeremonyLogin)
	if err != nil {
		return model.User{}, err
	}
	user, credential, err := rp.web.FinishPasskeyLogin(s.discoverableUserHandler(ctx, session.RPID), webAuthnSession(session), requestWithJSONBody(r, response))
	if err != nil {
		return model.User{}, err
	}
	appUser := user.(webAuthnUser).user
	if err := s.store.UpdatePasskeyCredentialUsage(ctx, appUser.ID, session.RPID, *credential); err != nil {
		return model.User{}, err
	}
	return appUser, nil
}

func (s *Service) BeginAdd(ctx context.Context, r *http.Request, p AddOptionsParams) (CreationOptions, error) {
	rp, err := s.rp(r)
	if err != nil {
		return CreationOptions{}, err
	}
	user, err := s.webAuthnUserForAppUser(ctx, p.User, rp.id)
	if err != nil {
		return CreationOptions{}, err
	}
	creation, session, err := rp.web.BeginRegistration(user)
	if err != nil {
		return CreationOptions{}, err
	}
	userID := p.User.ID
	id, err := s.saveSession(ctx, store.CreatePasskeySessionParams{
		Kind:           store.PasskeyCeremonyAdd,
		RPID:           rp.id,
		UserID:         &userID,
		CredentialName: p.Name,
		UserHandle:     user.handle,
		Challenge:      session.Challenge,
		SessionData:    mustSessionJSON(session),
		ExpiresAt:      sessionExpires(session),
	})
	if err != nil {
		return CreationOptions{}, err
	}
	return CreationOptions{CeremonyID: id, PublicKey: creation.Response, Mediation: creation.Mediation}, nil
}

func (s *Service) FinishAdd(ctx context.Context, r *http.Request, ceremonyID uuid.UUID, response []byte) (model.PasskeyCredential, error) {
	rp, session, err := s.consumeSession(ctx, r, ceremonyID, store.PasskeyCeremonyAdd)
	if err != nil {
		return model.PasskeyCredential{}, err
	}
	if session.UserID == nil {
		return model.PasskeyCredential{}, store.ErrUnauthorized
	}
	appUser, err := s.store.GetUser(ctx, *session.UserID)
	if err != nil {
		return model.PasskeyCredential{}, err
	}
	user, err := s.webAuthnUserForAppUser(ctx, appUser, session.RPID)
	if err != nil {
		return model.PasskeyCredential{}, err
	}
	credential, err := rp.web.FinishRegistration(user, webAuthnSession(session), requestWithJSONBody(r, response))
	if err != nil {
		return model.PasskeyCredential{}, err
	}
	return s.store.AddPasskeyCredential(ctx, appUser.ID, session.RPID, session.CredentialName, *credential)
}

func (s *Service) BeginReauth(ctx context.Context, r *http.Request, user model.User) (AssertionOptions, error) {
	rp, err := s.rp(r)
	if err != nil {
		return AssertionOptions{}, err
	}
	webUser, err := s.webAuthnUserForAppUser(ctx, user, rp.id)
	if err != nil {
		return AssertionOptions{}, err
	}
	assertion, session, err := rp.web.BeginLogin(webUser, webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return AssertionOptions{}, err
	}
	userID := user.ID
	id, err := s.saveSession(ctx, store.CreatePasskeySessionParams{
		Kind:        store.PasskeyCeremonyReauth,
		RPID:        rp.id,
		UserID:      &userID,
		UserHandle:  webUser.handle,
		Challenge:   session.Challenge,
		SessionData: mustSessionJSON(session),
		ExpiresAt:   sessionExpires(session),
	})
	if err != nil {
		return AssertionOptions{}, err
	}
	return AssertionOptions{CeremonyID: id, PublicKey: assertion.Response, Mediation: assertion.Mediation}, nil
}

func (s *Service) FinishReauth(ctx context.Context, r *http.Request, ceremonyID uuid.UUID, response []byte) (model.User, error) {
	rp, session, err := s.consumeSession(ctx, r, ceremonyID, store.PasskeyCeremonyReauth)
	if err != nil {
		return model.User{}, err
	}
	if session.UserID == nil {
		return model.User{}, store.ErrUnauthorized
	}
	appUser, err := s.store.GetUser(ctx, *session.UserID)
	if err != nil {
		return model.User{}, err
	}
	user, err := s.webAuthnUserForAppUser(ctx, appUser, session.RPID)
	if err != nil {
		return model.User{}, err
	}
	credential, err := rp.web.FinishLogin(user, webAuthnSession(session), requestWithJSONBody(r, response))
	if err != nil {
		return model.User{}, err
	}
	if err := s.store.UpdatePasskeyCredentialUsage(ctx, appUser.ID, session.RPID, *credential); err != nil {
		return model.User{}, err
	}
	return appUser, nil
}

func (s *Service) webAuthnUserForAppUser(ctx context.Context, user model.User, rpID string) (webAuthnUser, error) {
	handle, err := s.store.GetOrCreateWebAuthnHandle(ctx, user.ID, rpID)
	if err != nil {
		return webAuthnUser{}, err
	}
	creds, err := s.store.ActivePasskeyCredentialsForUser(ctx, user.ID, rpID)
	if err != nil {
		return webAuthnUser{}, err
	}
	return webAuthnUser{
		user:        user,
		handle:      handle,
		username:    user.Username,
		displayName: user.Name,
		credentials: passkeyCredentials(creds),
	}, nil
}

func (s *Service) discoverableUserHandler(ctx context.Context, rpID string) webauthn.DiscoverableUserHandler {
	return func(_, userHandle []byte) (webauthn.User, error) {
		user, err := s.store.PasskeyUserByHandle(ctx, rpID, userHandle)
		if err != nil {
			return nil, err
		}
		return webAuthnUser{
			user:        user.User,
			handle:      user.Handle,
			username:    user.User.Username,
			displayName: user.User.Name,
			credentials: user.Credentials,
		}, nil
	}
}

func passkeyCredentials(creds []store.PasskeyCredential) []webauthn.Credential {
	out := make([]webauthn.Credential, 0, len(creds))
	for _, cred := range creds {
		out = append(out, cred.Credential)
	}
	return out
}

type webAuthnUser struct {
	user        model.User
	handle      []byte
	username    string
	displayName string
	credentials []webauthn.Credential
}

func (u webAuthnUser) WebAuthnID() []byte {
	return u.handle
}

func (u webAuthnUser) WebAuthnName() string {
	return u.username
}

func (u webAuthnUser) WebAuthnDisplayName() string {
	if u.displayName != "" {
		return u.displayName
	}
	return u.username
}

func (u webAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	return u.credentials
}

type relyingParty struct {
	id     string
	origin string
	web    *webauthn.WebAuthn
}

func (s *Service) rp(r *http.Request) (relyingParty, error) {
	origin, err := s.origin(r)
	if err != nil {
		return relyingParty{}, err
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return relyingParty{}, errors.New("invalid passkey origin")
	}
	rpID := strings.ToLower(u.Hostname())
	web, err := webauthn.New(&webauthn.Config{
		RPID:                  rpID,
		RPDisplayName:         s.displayName,
		RPOrigins:             []string{origin},
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			RequireResidentKey: protocol.ResidentKeyRequired(),
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			UserVerification:   protocol.VerificationRequired,
		},
		Timeouts: webauthn.TimeoutsConfig{
			Login:        webauthn.TimeoutConfig{Enforce: true, Timeout: defaultCeremonyTTL},
			Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: defaultCeremonyTTL},
		},
	})
	if err != nil {
		return relyingParty{}, err
	}
	return relyingParty{id: rpID, origin: origin, web: web}, nil
}

func (s *Service) origin(r *http.Request) (string, error) {
	if s.publicOrigin != "" {
		return s.publicOrigin, nil
	}
	if r == nil {
		return "", errors.New("passkeys require TRACK_SLASH_PUBLIC_ORIGIN")
	}
	host := strings.TrimSpace(r.Host)
	hostname := hostOnly(host)
	if !isLocalHost(hostname) {
		return "", errors.New("passkeys require TRACK_SLASH_PUBLIC_ORIGIN for non-localhost hosts")
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return (&url.URL{Scheme: scheme, Host: strings.ToLower(host)}).String(), nil
}

func (s *Service) saveSession(ctx context.Context, p store.CreatePasskeySessionParams) (uuid.UUID, error) {
	return s.store.CreatePasskeySession(ctx, p)
}

func (s *Service) consumeSession(ctx context.Context, r *http.Request, id uuid.UUID, kind string) (relyingParty, store.PasskeySession, error) {
	session, err := s.store.ConsumePasskeySession(ctx, id, kind)
	if err != nil {
		return relyingParty{}, store.PasskeySession{}, err
	}
	rp, err := s.rp(r)
	if err != nil {
		return relyingParty{}, store.PasskeySession{}, err
	}
	if rp.id != session.RPID {
		return relyingParty{}, store.PasskeySession{}, store.ErrUnauthorized
	}
	return rp, session, nil
}

func webAuthnSession(s store.PasskeySession) webauthn.SessionData {
	var out webauthn.SessionData
	_ = json.Unmarshal(s.SessionData, &out)
	return out
}

func mustSessionJSON(session *webauthn.SessionData) []byte {
	data, err := json.Marshal(session)
	if err != nil {
		panic(err)
	}
	return data
}

func sessionExpires(session *webauthn.SessionData) time.Time {
	if session.Expires.IsZero() {
		return time.Now().Add(defaultCeremonyTTL)
	}
	return session.Expires
}

func requestWithJSONBody(r *http.Request, data []byte) *http.Request {
	next := r.Clone(r.Context())
	next.Body = http.NoBody
	if len(data) > 0 {
		next.Body = ioNopCloser{Reader: bytes.NewReader(data)}
	}
	next.ContentLength = int64(len(data))
	next.Header = r.Header.Clone()
	next.Header.Set("Content-Type", "application/json")
	return next
}

type ioNopCloser struct {
	*bytes.Reader
}

func (c ioNopCloser) Close() error {
	return nil
}

func randomHandle() ([]byte, error) {
	b := make([]byte, 64)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

func hostOnly(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return strings.Trim(h, "[]")
	}
	return strings.Trim(host, "[]")
}

func isLocalHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
