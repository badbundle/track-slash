package store

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"

	"github.com/bradleymackey/track-slash/internal/model"
)

const minPasswordLength = 12

func NormalizeUsername(raw string) (string, error) {
	username := strings.ToLower(strings.TrimSpace(raw))
	if len(username) < 3 || len(username) > 32 {
		return "", errors.New("username must be 3-32 chars")
	}
	for i, r := range username {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
		if !ok {
			return "", errors.New("username may only contain a-z, 0-9, _, -")
		}
		if i == 0 && !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return "", errors.New("username must start with a letter or number")
		}
	}
	return username, nil
}

func ValidatePassword(password string) error {
	if len(password) < minPasswordLength {
		return errors.New("password must be at least 12 chars")
	}
	return nil
}

func UsernameFromEmail(email string) string {
	base, _, _ := strings.Cut(strings.TrimSpace(email), "@")
	username, err := NormalizeUsername(base)
	if err == nil {
		return username
	}
	return "user_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:27]
}

type CreateAccountParams struct {
	Username string
	Password string
	Name     string
}

func (s *Store) CreateAccount(ctx context.Context, p CreateAccountParams) (model.User, error) {
	username, err := NormalizeUsername(p.Username)
	if err != nil {
		return model.User{}, err
	}
	if err := ValidatePassword(p.Password); err != nil {
		return model.User{}, err
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = username
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(p.Password), bcrypt.DefaultCost)
	if err != nil {
		return model.User{}, err
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

	const credQ = `
		INSERT INTO auth_credentials (user_id, kind, identifier, secret_hash)
		VALUES ($1, $2, $3, $4)
	`
	if _, err := tx.Exec(ctx, credQ, u.ID, string(model.AuthCredentialKindPassword), username, string(hash)); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return model.User{}, ErrConflict
		}
		return model.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.User{}, err
	}
	return u, nil
}

func (s *Store) AuthenticatePassword(ctx context.Context, username, password string) (model.User, error) {
	normalized, err := NormalizeUsername(username)
	if err != nil {
		return model.User{}, ErrUnauthorized
	}
	const q = `
		SELECT u.id, u.username, COALESCE(u.email, ''), u.name, u.is_admin, u.created_at, c.id, c.secret_hash
		FROM auth_credentials c
		JOIN users u ON u.id = c.user_id
		WHERE c.kind = $1
		  AND c.identifier = $2
		  AND c.revoked_at IS NULL
		  AND u.deleted_at IS NULL
	`
	var u model.User
	var credentialID string
	var hash string
	err = s.db.QueryRow(ctx, q, string(model.AuthCredentialKindPassword), normalized).Scan(
		&u.ID, &u.Username, &u.Email, &u.Name, &u.IsAdmin, &u.CreatedAt, &credentialID, &hash,
	)
	if err != nil {
		if isNoRows(err) {
			return model.User{}, ErrUnauthorized
		}
		return model.User{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return model.User{}, ErrUnauthorized
	}
	if _, err := s.db.Exec(ctx, `UPDATE auth_credentials SET last_used_at = now() WHERE id = $1`, credentialID); err != nil {
		// Defensive: credential row was just read; failure here is a DB/runtime fault.
		return model.User{}, err
	}
	return u, nil
}
