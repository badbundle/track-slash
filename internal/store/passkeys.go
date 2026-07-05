package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"

	"github.com/bradleymackey/track-slash/internal/model"
)

const (
	passkeyHandleBytes    = 64
	defaultPasskeyName    = "Passkey"
	maxPasskeyNameLength  = 100
	PasskeyReauthTokenTTL = 5 * time.Minute
	PasskeyCeremonySignup = "signup"
	PasskeyCeremonyLogin  = "login"
	PasskeyCeremonyAdd    = "add"
	PasskeyCeremonyReauth = "reauth"
)

type PasskeyCredential struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Identifier string
	RPID       string
	Name       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
	Credential webauthn.Credential
}

type PasskeyUser struct {
	User        model.User
	Handle      []byte
	Credentials []webauthn.Credential
}

type passkeyCredentialMetadata struct {
	Name       string              `json:"name"`
	RPID       string              `json:"rp_id"`
	Credential webauthn.Credential `json:"credential"`
}

type CreatePasskeySessionParams struct {
	Kind           string
	RPID           string
	UserID         *uuid.UUID
	Username       string
	DisplayName    string
	CredentialName string
	UserHandle     []byte
	Challenge      string
	SessionData    []byte
	ExpiresAt      time.Time
}

type PasskeySession struct {
	ID             uuid.UUID
	Kind           string
	RPID           string
	UserID         *uuid.UUID
	Username       string
	DisplayName    string
	CredentialName string
	UserHandle     []byte
	Challenge      string
	SessionData    []byte
	ExpiresAt      time.Time
}

type CreatePasskeyOnlyAccountParams struct {
	Username       string
	Name           string
	RPID           string
	UserHandle     []byte
	CredentialName string
	Credential     webauthn.Credential
}

func (s *Store) GetOrCreateWebAuthnHandle(ctx context.Context, userID uuid.UUID, rpID string) ([]byte, error) {
	rpID = strings.TrimSpace(rpID)
	if rpID == "" {
		return nil, errors.New("rp_id required")
	}
	for attempt := 0; attempt < 3; attempt++ {
		handle, err := randomBytes(passkeyHandleBytes)
		if err != nil {
			return nil, err
		}
		const insertQ = `
			INSERT INTO webauthn_user_handles (user_id, rp_id, handle)
			SELECT id, $2, $3 FROM users
			WHERE id = $1 AND deleted_at IS NULL
			ON CONFLICT (rp_id, user_id) DO NOTHING
			RETURNING handle
		`
		var out []byte
		err = s.db.QueryRow(ctx, insertQ, userID, rpID, handle).Scan(&out)
		if err == nil {
			return out, nil
		}
		if !isNoRows(err) {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				continue
			}
			return nil, err
		}

		const selectQ = `
			SELECT h.handle
			FROM webauthn_user_handles h
			JOIN users u ON u.id = h.user_id
			WHERE h.user_id = $1 AND h.rp_id = $2 AND u.deleted_at IS NULL
		`
		err = s.db.QueryRow(ctx, selectQ, userID, rpID).Scan(&out)
		if err != nil {
			if isNoRows(err) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		return out, nil
	}
	return nil, ErrConflict
}

func (s *Store) CreatePasskeySession(ctx context.Context, p CreatePasskeySessionParams) (uuid.UUID, error) {
	if p.Kind == "" || p.RPID == "" || p.Challenge == "" || len(p.SessionData) == 0 || p.ExpiresAt.IsZero() {
		return uuid.Nil, errors.New("passkey session fields required")
	}
	const q = `
		INSERT INTO webauthn_sessions (
			kind, rp_id, user_id, username, display_name, credential_name,
			user_handle, challenge, session_data, expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10)
		RETURNING id
	`
	var id uuid.UUID
	err := s.db.QueryRow(ctx, q,
		p.Kind, p.RPID, p.UserID, strings.TrimSpace(p.Username), strings.TrimSpace(p.DisplayName),
		normalizePasskeyName(p.CredentialName), p.UserHandle, p.Challenge, p.SessionData, p.ExpiresAt,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

func (s *Store) ConsumePasskeySession(ctx context.Context, id uuid.UUID, kind string) (PasskeySession, error) {
	const q = `
		UPDATE webauthn_sessions
		SET consumed_at = now()
		WHERE id = $1
		  AND kind = $2
		  AND consumed_at IS NULL
		  AND expires_at > now()
		RETURNING id, kind, rp_id, user_id, username, display_name, credential_name,
		          user_handle, challenge, session_data, expires_at
	`
	var out PasskeySession
	err := s.db.QueryRow(ctx, q, id, kind).Scan(
		&out.ID, &out.Kind, &out.RPID, &out.UserID, &out.Username, &out.DisplayName, &out.CredentialName,
		&out.UserHandle, &out.Challenge, &out.SessionData, &out.ExpiresAt,
	)
	if err != nil {
		if isNoRows(err) {
			return PasskeySession{}, ErrUnauthorized
		}
		return PasskeySession{}, err
	}
	return out, nil
}

func (s *Store) CreatePasskeyOnlyAccount(ctx context.Context, p CreatePasskeyOnlyAccountParams) (model.User, error) {
	username, err := NormalizeUsername(p.Username)
	if err != nil {
		return model.User{}, err
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = username
	}
	if p.RPID == "" || len(p.UserHandle) == 0 || len(p.Credential.ID) == 0 {
		return model.User{}, errors.New("passkey account fields required")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return model.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const userQ = `
		INSERT INTO users (username, email, name)
		VALUES ($1, NULL, $2)
		RETURNING id, username, COALESCE(email, ''), name, is_admin, created_at
	`
	var u model.User
	err = tx.QueryRow(ctx, userQ, username, name).Scan(&u.ID, &u.Username, &u.Email, &u.Name, &u.IsAdmin, &u.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return model.User{}, ErrConflict
		}
		return model.User{}, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO webauthn_user_handles (user_id, rp_id, handle)
		VALUES ($1, $2, $3)
	`, u.ID, p.RPID, p.UserHandle); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return model.User{}, ErrConflict
		}
		return model.User{}, err
	}

	if _, err := insertPasskeyCredential(ctx, tx, u.ID, p.RPID, p.CredentialName, p.Credential); err != nil {
		return model.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.User{}, err
	}
	return u, nil
}

func (s *Store) AddPasskeyCredential(ctx context.Context, userID uuid.UUID, rpID, name string, credential webauthn.Credential) (model.PasskeyCredential, error) {
	if rpID == "" || len(credential.ID) == 0 {
		return model.PasskeyCredential{}, errors.New("passkey credential fields required")
	}
	if _, err := s.GetUser(ctx, userID); err != nil {
		return model.PasskeyCredential{}, err
	}
	created, err := insertPasskeyCredential(ctx, s.db, userID, rpID, name, credential)
	if err != nil {
		return model.PasskeyCredential{}, err
	}
	return created.toModel(), nil
}

func (s *Store) ListPasskeyCredentials(ctx context.Context, userID uuid.UUID) ([]model.PasskeyCredential, error) {
	if _, err := s.GetUser(ctx, userID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, user_id, identifier, metadata, created_at, last_used_at, revoked_at
		FROM auth_credentials
		WHERE user_id = $1 AND kind = $2
		  AND revoked_at IS NULL
		  AND disabled_at IS NULL
		ORDER BY created_at ASC, id ASC
	`, userID, string(model.AuthCredentialKindPasskey))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.PasskeyCredential{}
	for rows.Next() {
		cred, err := scanPasskeyCredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, cred.toModel())
	}
	return out, rows.Err()
}

func (s *Store) ActivePasskeyCredentialsForUser(ctx context.Context, userID uuid.UUID, rpID string) ([]PasskeyCredential, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, user_id, identifier, metadata, created_at, last_used_at, revoked_at
		FROM auth_credentials
		WHERE user_id = $1
		  AND kind = $2
		  AND revoked_at IS NULL
		  AND disabled_at IS NULL
		  AND metadata->>'rp_id' = $3
		ORDER BY created_at ASC, id ASC
	`, userID, string(model.AuthCredentialKindPasskey), rpID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []PasskeyCredential{}
	for rows.Next() {
		cred, err := scanPasskeyCredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, cred)
	}
	return out, rows.Err()
}

func (s *Store) PasskeyUserByHandle(ctx context.Context, rpID string, handle []byte) (PasskeyUser, error) {
	const q = `
		SELECT u.id, u.username, COALESCE(u.email, ''), u.name, u.is_admin, u.created_at, h.handle
		FROM webauthn_user_handles h
		JOIN users u ON u.id = h.user_id
		WHERE h.rp_id = $1 AND h.handle = $2 AND u.deleted_at IS NULL
	`
	var out PasskeyUser
	if err := s.db.QueryRow(ctx, q, rpID, handle).Scan(
		&out.User.ID, &out.User.Username, &out.User.Email, &out.User.Name, &out.User.IsAdmin, &out.User.CreatedAt, &out.Handle,
	); err != nil {
		if isNoRows(err) {
			return PasskeyUser{}, ErrNotFound
		}
		return PasskeyUser{}, err
	}
	creds, err := s.ActivePasskeyCredentialsForUser(ctx, out.User.ID, rpID)
	if err != nil {
		return PasskeyUser{}, err
	}
	out.Credentials = passkeyWebAuthnCredentials(creds)
	return out, nil
}

func (s *Store) UpdatePasskeyCredentialUsage(ctx context.Context, userID uuid.UUID, rpID string, credential webauthn.Credential) error {
	identifier := passkeyIdentifier(credential.ID)
	meta, err := passkeyMetadata(rpID, "", credential)
	if err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE auth_credentials
		SET public_key = $4,
		    sign_count = $5,
		    metadata = jsonb_set($6::jsonb, '{name}', COALESCE(metadata->'name', to_jsonb($7::text))),
		    last_used_at = now()
		WHERE user_id = $1
		  AND kind = $2
		  AND identifier = $3
		  AND revoked_at IS NULL
		  AND disabled_at IS NULL
		  AND metadata->>'rp_id' = $8
	`, userID, string(model.AuthCredentialKindPasskey), identifier, credential.PublicKey,
		int64(credential.Authenticator.SignCount), meta, defaultPasskeyName, rpID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RevokePasskeyCredentialForUser(ctx context.Context, userID, id uuid.UUID) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM auth_credentials
			WHERE id = $1
			  AND user_id = $2
			  AND kind = $3
			  AND revoked_at IS NULL
			  AND disabled_at IS NULL
		)
	`, id, userID, string(model.AuthCredentialKindPasskey)).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}

	var activePasskeys int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM auth_credentials
		WHERE user_id = $1
		  AND kind = $2
		  AND revoked_at IS NULL
		  AND disabled_at IS NULL
	`, userID, string(model.AuthCredentialKindPasskey)).Scan(&activePasskeys); err != nil {
		return err
	}

	var hasPassword bool
	if activePasskeys <= 1 {
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM auth_credentials
				WHERE user_id = $1
				  AND kind = $2
				  AND revoked_at IS NULL
			)
		`, userID, string(model.AuthCredentialKindPassword)).Scan(&hasPassword); err != nil {
			return err
		}
		if !hasPassword {
			return ErrConflict
		}
	}

	tag, err := tx.Exec(ctx, `
		UPDATE auth_credentials SET revoked_at = now()
		WHERE id = $1
		  AND user_id = $2
		  AND kind = $3
		  AND revoked_at IS NULL
		  AND disabled_at IS NULL
	`, id, userID, string(model.AuthCredentialKindPasskey))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if hasPassword {
		if _, err := tx.Exec(ctx, `
			UPDATE auth_credentials
			SET disabled_at = NULL
			WHERE user_id = $1
			  AND kind = $2
			  AND revoked_at IS NULL
		`, userID, string(model.AuthCredentialKindPassword)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) CreatePasskeyReauthToken(ctx context.Context, userID uuid.UUID) (string, error) {
	raw, err := generateToken()
	if err != nil {
		return "", err
	}
	hash := tokenHash(raw)
	const q = `
		INSERT INTO passkey_reauth_tokens (user_id, token_hash, expires_at)
		SELECT id, $2, now() + $3::interval FROM users
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id
	`
	var id uuid.UUID
	if err := s.db.QueryRow(ctx, q, userID, hash[:], PasskeyReauthTokenTTL.String()).Scan(&id); err != nil {
		if isNoRows(err) {
			return "", ErrNotFound
		}
		return "", err
	}
	return raw, nil
}

func (s *Store) ConsumePasskeyReauthToken(ctx context.Context, userID uuid.UUID, raw string) error {
	hash := tokenHash(raw)
	tag, err := s.db.Exec(ctx, `
		UPDATE passkey_reauth_tokens
		SET consumed_at = now()
		WHERE user_id = $1
		  AND token_hash = $2
		  AND consumed_at IS NULL
		  AND expires_at > now()
	`, userID, hash[:])
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUnauthorized
	}
	return nil
}

func (s *Store) VerifyUserPassword(ctx context.Context, userID uuid.UUID, password string) error {
	const q = `
		SELECT c.secret_hash
		FROM auth_credentials c
		JOIN users u ON u.id = c.user_id
		WHERE c.user_id = $1
		  AND c.kind = $2
		  AND c.revoked_at IS NULL
		  AND c.disabled_at IS NULL
		  AND u.deleted_at IS NULL
	`
	var hash string
	err := s.db.QueryRow(ctx, q, userID, string(model.AuthCredentialKindPassword)).Scan(&hash)
	if err != nil {
		if isNoRows(err) {
			return ErrUnauthorized
		}
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return ErrUnauthorized
	}
	return nil
}

type passkeyQueryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func insertPasskeyCredential(ctx context.Context, exec passkeyQueryRower, userID uuid.UUID, rpID, name string, credential webauthn.Credential) (PasskeyCredential, error) {
	name = normalizePasskeyName(name)
	identifier := passkeyIdentifier(credential.ID)
	meta, err := passkeyMetadata(rpID, name, credential)
	if err != nil {
		return PasskeyCredential{}, err
	}
	const q = `
		INSERT INTO auth_credentials (user_id, kind, identifier, public_key, sign_count, metadata)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb)
		RETURNING id, user_id, identifier, metadata, created_at, last_used_at, revoked_at
	`
	var out PasskeyCredential
	err = exec.QueryRow(ctx, q, userID, string(model.AuthCredentialKindPasskey), identifier,
		credential.PublicKey, int64(credential.Authenticator.SignCount), meta,
	).Scan(&out.ID, &out.UserID, &out.Identifier, &meta, &out.CreatedAt, &out.LastUsedAt, &out.RevokedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23503":
				return PasskeyCredential{}, ErrNotFound
			case "23505":
				return PasskeyCredential{}, ErrConflict
			}
		}
		return PasskeyCredential{}, err
	}
	if err := out.applyMetadata(meta); err != nil {
		return PasskeyCredential{}, err
	}
	return out, nil
}

type passkeyRows interface {
	Scan(...any) error
}

func scanPasskeyCredential(row passkeyRows) (PasskeyCredential, error) {
	var out PasskeyCredential
	var meta []byte
	if err := row.Scan(&out.ID, &out.UserID, &out.Identifier, &meta, &out.CreatedAt, &out.LastUsedAt, &out.RevokedAt); err != nil {
		return PasskeyCredential{}, err
	}
	if err := out.applyMetadata(meta); err != nil {
		return PasskeyCredential{}, err
	}
	return out, nil
}

func (p *PasskeyCredential) applyMetadata(data []byte) error {
	var meta passkeyCredentialMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return err
	}
	p.Name = normalizePasskeyName(meta.Name)
	p.RPID = meta.RPID
	p.Credential = meta.Credential
	if len(p.Credential.ID) == 0 && p.Identifier != "" {
		id, err := base64.RawURLEncoding.DecodeString(p.Identifier)
		if err != nil {
			return err
		}
		p.Credential.ID = id
	}
	return nil
}

func (p PasskeyCredential) toModel() model.PasskeyCredential {
	return model.PasskeyCredential{
		ID:             p.ID,
		UserID:         p.UserID,
		Name:           p.Name,
		CreatedAt:      p.CreatedAt,
		LastUsedAt:     p.LastUsedAt,
		BackupEligible: p.Credential.Flags.BackupEligible,
		BackupState:    p.Credential.Flags.BackupState,
		CloneWarning:   p.Credential.Authenticator.CloneWarning,
	}
}

func passkeyWebAuthnCredentials(creds []PasskeyCredential) []webauthn.Credential {
	out := make([]webauthn.Credential, 0, len(creds))
	for _, cred := range creds {
		out = append(out, cred.Credential)
	}
	return out
}

func passkeyMetadata(rpID, name string, credential webauthn.Credential) ([]byte, error) {
	return json.Marshal(passkeyCredentialMetadata{
		Name:       normalizePasskeyName(name),
		RPID:       rpID,
		Credential: credential,
	})
}

func passkeyIdentifier(id []byte) string {
	return base64.RawURLEncoding.EncodeToString(id)
}

func normalizePasskeyName(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		return defaultPasskeyName
	}
	if len(name) > maxPasskeyNameLength {
		return name[:maxPasskeyNameLength]
	}
	return name
}

func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}
