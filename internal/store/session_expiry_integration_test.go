package store_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

// Web sessions accumulate indefinitely, so they age out inside Postgres on the
// back of a token refresh rather than through an external scheduler.
func TestSessionSweepOnTokenRefresh(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	user, err := env.store.CreateUser(env.ctx, "sweep-"+uniqueProjectKey(t)+"@example.com", "Sweep")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	oldSession := env.mustToken(t, user.ID, model.AuthTokenKindSession, "three years old")
	freshSession := env.mustToken(t, user.ID, model.AuthTokenKindSession, "recent")
	oldAPIToken := env.mustToken(t, user.ID, model.AuthTokenKindAPI, "ancient deploy key")
	refreshing := env.mustToken(t, user.ID, model.AuthTokenKindSession, "the one being used")

	env.backdateToken(t, oldSession.Token.ID, "4 years")
	env.backdateToken(t, oldAPIToken.Token.ID, "4 years")
	env.backdateToken(t, refreshing.Token.ID, "4 years")
	env.armSweep(t)

	if _, err := env.store.AuthenticateToken(env.ctx, refreshing.RawToken); err != nil {
		t.Fatalf("AuthenticateToken: %v", err)
	}

	if !env.tokenRevoked(t, oldSession.Token.ID) {
		t.Fatal("a three-year-old session survived the sweep")
	}
	if env.tokenRevoked(t, freshSession.Token.ID) {
		t.Fatal("a recent session was revoked")
	}
	if env.tokenRevoked(t, oldAPIToken.Token.ID) {
		t.Fatal("an API token was revoked; they are long-lived by design")
	}
	if env.tokenRevoked(t, refreshing.Token.ID) {
		t.Fatal("the session being refreshed was revoked out from under its own request")
	}
}

// Without the rate limit every authenticated request would pay for a sweep.
func TestSessionSweepIsRateLimited(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	user, err := env.store.CreateUser(env.ctx, "sweep-rate-"+uniqueProjectKey(t)+"@example.com", "Sweep")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	first := env.mustToken(t, user.ID, model.AuthTokenKindSession, "first")
	env.armSweep(t)
	if _, err := env.store.AuthenticateToken(env.ctx, first.RawToken); err != nil {
		t.Fatalf("AuthenticateToken: %v", err)
	}

	// A second refresh right afterwards must find the sweep already done, so an
	// old session created in between survives until the interval elapses.
	stale := env.mustToken(t, user.ID, model.AuthTokenKindSession, "old but unswept")
	env.backdateToken(t, stale.Token.ID, "4 years")
	second := env.mustToken(t, user.ID, model.AuthTokenKindSession, "second")
	if _, err := env.store.AuthenticateToken(env.ctx, second.RawToken); err != nil {
		t.Fatalf("AuthenticateToken second: %v", err)
	}
	if env.tokenRevoked(t, stale.Token.ID) {
		t.Fatal("a second sweep ran within the rate-limit interval")
	}

	// Once the interval has elapsed the next refresh sweeps again.
	env.armSweep(t)
	third := env.mustToken(t, user.ID, model.AuthTokenKindSession, "third")
	if _, err := env.store.AuthenticateToken(env.ctx, third.RawToken); err != nil {
		t.Fatalf("AuthenticateToken third: %v", err)
	}
	if !env.tokenRevoked(t, stale.Token.ID) {
		t.Fatal("the sweep did not resume after the interval elapsed")
	}
}

// The sweep writes to auth_tokens from a trigger on auth_tokens, so it must not
// re-enter itself.
func TestSessionSweepDoesNotReenterItsOwnTrigger(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	user, err := env.store.CreateUser(env.ctx, "sweep-recursion-"+uniqueProjectKey(t)+"@example.com", "Sweep")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	for range 5 {
		old := env.mustToken(t, user.ID, model.AuthTokenKindSession, "old")
		env.backdateToken(t, old.Token.ID, "4 years")
	}
	refreshing := env.mustToken(t, user.ID, model.AuthTokenKindSession, "current")
	env.armSweep(t)

	// A recursive trigger would exceed the nesting limit and error here.
	if _, err := env.store.AuthenticateToken(env.ctx, refreshing.RawToken); err != nil {
		t.Fatalf("AuthenticateToken: %v", err)
	}

	tokens, err := env.store.ListAuthTokens(env.ctx, user.ID)
	if err != nil {
		t.Fatalf("ListAuthTokens: %v", err)
	}
	live := 0
	for _, token := range tokens {
		if token.RevokedAt == nil {
			live++
		}
	}
	if live != 1 {
		t.Fatalf("live tokens = %d, want only the one being refreshed", live)
	}
}

func (e *sprintsTestEnv) mustToken(t *testing.T, userID uuid.UUID, kind model.AuthTokenKind, name string) store.CreatedAuthToken {
	t.Helper()
	created, err := e.store.CreateAuthToken(e.ctx, store.CreateAuthTokenParams{UserID: userID, Kind: kind, Name: name})
	if err != nil {
		t.Fatalf("CreateAuthToken %s: %v", name, err)
	}
	return created
}

func (e *sprintsTestEnv) backdateToken(t *testing.T, id uuid.UUID, age string) {
	t.Helper()
	if _, err := e.pool.Exec(e.ctx,
		"UPDATE auth_tokens SET created_at = now() - $2::interval WHERE id = $1", id, age); err != nil {
		t.Fatalf("backdate token: %v", err)
	}
}

// armSweep moves the rate-limit marker far enough into the past that the next
// refresh is allowed to sweep.
func (e *sprintsTestEnv) armSweep(t *testing.T) {
	t.Helper()
	if _, err := e.pool.Exec(e.ctx, "UPDATE auth_token_sweeps SET last_swept_at = '-infinity' WHERE id"); err != nil {
		t.Fatalf("arm sweep: %v", err)
	}
}

func (e *sprintsTestEnv) tokenRevoked(t *testing.T, id uuid.UUID) bool {
	t.Helper()
	var revoked bool
	if err := e.pool.QueryRow(e.ctx,
		"SELECT revoked_at IS NOT NULL FROM auth_tokens WHERE id = $1", id).Scan(&revoked); err != nil {
		t.Fatalf("read token: %v", err)
	}
	return revoked
}
