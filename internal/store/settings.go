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

func ValidateEmail(email string) error {
	email = strings.TrimSpace(email)
	if email != "" && !strings.Contains(email, "@") {
		return errors.New("invalid email")
	}
	return nil
}

func (s *Store) UpdateUserProfile(ctx context.Context, userID uuid.UUID, name, email string) (model.User, error) {
	name = strings.TrimSpace(name)
	email = strings.TrimSpace(email)
	if name == "" {
		return model.User{}, errors.New("name required")
	}
	if err := ValidateEmail(email); err != nil {
		return model.User{}, err
	}
	var emailValue any
	if email != "" {
		emailValue = email
	}
	const q = `
		UPDATE users
		SET name = $2, email = $3
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, username, COALESCE(email, ''), name, is_admin, created_at
	`
	var u model.User
	err := s.db.QueryRow(ctx, q, userID, name, emailValue).Scan(&u.ID, &u.Username, &u.Email, &u.Name, &u.IsAdmin, &u.CreatedAt)
	if err != nil {
		if isNoRows(err) {
			return model.User{}, ErrNotFound
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return model.User{}, ErrConflict
		}
		return model.User{}, err
	}
	return u, nil
}

func (s *Store) ChangePassword(ctx context.Context, userID uuid.UUID, currentPassword, newPassword string) error {
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	const q = `
		SELECT c.id, c.secret_hash
		FROM auth_credentials c
		JOIN users u ON u.id = c.user_id
		WHERE c.user_id = $1
		  AND c.kind = $2
		  AND c.revoked_at IS NULL
		  AND u.deleted_at IS NULL
	`
	var credentialID uuid.UUID
	var hash string
	err := s.db.QueryRow(ctx, q, userID, string(model.AuthCredentialKindPassword)).Scan(&credentialID, &hash)
	if err != nil {
		if isNoRows(err) {
			return ErrUnauthorized
		}
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(currentPassword)) != nil {
		return ErrUnauthorized
	}
	nextHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE auth_credentials SET secret_hash = $2, last_used_at = now()
		WHERE id = $1
	`, credentialID, string(nextHash)); err != nil {
		// Defensive: credential row was just read; failure here is a DB/runtime fault.
		return err
	}
	return nil
}

func (s *Store) HasPasswordCredential(ctx context.Context, userID uuid.UUID) (bool, error) {
	const q = `
		SELECT EXISTS (
			SELECT 1
			FROM auth_credentials c
			JOIN users u ON u.id = c.user_id
			WHERE c.user_id = $1
			  AND c.kind = $2
			  AND c.revoked_at IS NULL
			  AND u.deleted_at IS NULL
		)
	`
	var ok bool
	if err := s.db.QueryRow(ctx, q, userID, string(model.AuthCredentialKindPassword)).Scan(&ok); err != nil {
		return false, err
	}
	return ok, nil
}
