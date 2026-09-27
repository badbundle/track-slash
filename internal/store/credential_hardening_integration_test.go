package store_test

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

// Two revokes racing on a passkey-only account must not both pass the
// last-credential guard and leave the account with no way to sign in.
func TestConcurrentPasskeyRevokesKeepOneSignInMethod(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	for round := 0; round < 5; round++ {
		key := strings.ToLower(uniqueProjectKey(t))
		u, err := env.store.CreatePasskeyOnlyAccount(env.ctx, store.CreatePasskeyOnlyAccountParams{
			Username:       "pkrace" + key,
			Name:           "Passkey Race",
			RPID:           "localhost",
			UserHandle:     []byte("handle-" + key),
			CredentialName: "First",
			Credential:     testPasskeyCredential("first-"+key, 1),
		})
		if err != nil {
			t.Fatalf("CreatePasskeyOnlyAccount: %v", err)
		}
		if _, err := env.store.AddPasskeyCredential(env.ctx, u.ID, "localhost", "Second", testPasskeyCredential("second-"+key, 1)); err != nil {
			t.Fatalf("AddPasskeyCredential: %v", err)
		}
		listed, err := env.store.ListPasskeyCredentials(env.ctx, u.ID)
		if err != nil || len(listed) != 2 {
			t.Fatalf("ListPasskeyCredentials = %+v, %v", listed, err)
		}

		var wg sync.WaitGroup
		errs := make([]error, len(listed))
		for i, credential := range listed {
			wg.Add(1)
			go func(i int, id uuid.UUID) {
				defer wg.Done()
				errs[i] = env.store.RevokePasskeyCredentialForUser(env.ctx, u.ID, id)
			}(i, credential.ID)
		}
		wg.Wait()
		succeeded := 0
		for _, err := range errs {
			switch {
			case err == nil:
				succeeded++
			case !errors.Is(err, store.ErrConflict):
				t.Fatalf("round %d revoke err = %v", round, err)
			}
		}
		remaining, err := env.store.ListPasskeyCredentials(env.ctx, u.ID)
		if err != nil || succeeded != 1 || len(remaining) != 1 {
			t.Fatalf("round %d: %d revokes succeeded, %d passkeys remain (%v)", round, succeeded, len(remaining), err)
		}
	}
	if err := env.store.RevokePasskeyCredentialForUser(env.ctx, uuid.New(), uuid.New()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoke for missing user err = %v, want ErrNotFound", err)
	}
	if _, err := env.store.SetPasswordLoginEnabled(env.ctx, uuid.New(), false); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("toggle for missing user err = %v, want ErrNotFound", err)
	}
}

func TestRevokeSessionAuthTokensForUserExcept(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	u, err := env.store.CreateUserProfile(env.ctx, "sessions"+strings.ToLower(uniqueProjectKey(t)), "", "Sessions")
	if err != nil {
		t.Fatalf("CreateUserProfile: %v", err)
	}
	mint := func(kind model.AuthTokenKind) model.AuthToken {
		t.Helper()
		created, err := env.store.CreateAuthToken(env.ctx, store.CreateAuthTokenParams{UserID: u.ID, Kind: kind, Name: string(kind)})
		if err != nil {
			t.Fatalf("CreateAuthToken %s: %v", kind, err)
		}
		return created.Token
	}
	kept, other, api := mint(model.AuthTokenKindSession), mint(model.AuthTokenKindSession), mint(model.AuthTokenKindAPI)
	revoked, err := env.store.RevokeSessionAuthTokensForUserExcept(env.ctx, u.ID, kept.ID)
	if err != nil || revoked != 1 {
		t.Fatalf("RevokeSessionAuthTokensForUserExcept = %d, %v", revoked, err)
	}
	tokens, err := env.store.ListAuthTokens(env.ctx, u.ID)
	if err != nil {
		t.Fatalf("ListAuthTokens: %v", err)
	}
	state := map[uuid.UUID]bool{}
	for _, token := range tokens {
		state[token.ID] = token.RevokedAt == nil
	}
	if !state[kept.ID] || state[other.ID] || !state[api.ID] {
		t.Fatalf("token liveness after revoke = %+v", state)
	}
	if revoked, err := env.store.RevokeSessionAuthTokensForUserExcept(env.ctx, u.ID, uuid.Nil); err != nil || revoked != 1 {
		t.Fatalf("revoke all sessions = %d, %v", revoked, err)
	}
}
