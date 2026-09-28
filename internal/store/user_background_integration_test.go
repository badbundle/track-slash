package store_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func TestUserBackground(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	user, err := env.store.CreateUserProfile(env.ctx, "background-"+uniqueProjectKey(t), "background@example.com", "Background")
	if err != nil {
		t.Fatalf("CreateUserProfile: %v", err)
	}
	if got, err := env.store.GetUserBackground(env.ctx, user.ID); err != nil || got != model.BackgroundIndigo {
		t.Fatalf("default background = %q, %v", got, err)
	}
	for _, preset := range model.BackgroundPresets {
		if err := env.store.SetUserBackground(env.ctx, user.ID, preset); err != nil {
			t.Fatalf("SetUserBackground %q: %v", preset, err)
		}
		if got, err := env.store.GetUserBackground(env.ctx, user.ID); err != nil || got != preset {
			t.Fatalf("background after setting %q = %q, %v", preset, got, err)
		}
	}

	for _, invalid := range []model.BackgroundPreset{"", "rose", "Emerald"} {
		err := env.store.SetUserBackground(env.ctx, user.ID, invalid)
		if !errors.Is(err, store.ErrInvalidBackgroundPreset) || !errors.Is(err, store.ErrConflict) {
			t.Fatalf("SetUserBackground %q = %v, want ErrInvalidBackgroundPreset", invalid, err)
		}
	}
	if got, err := env.store.GetUserBackground(env.ctx, user.ID); err != nil || got != model.BackgroundAmber {
		t.Fatalf("background after rejected saves = %q, %v", got, err)
	}

	// A value that is no longer a preset, such as one since retired, reads as
	// the default.
	if _, err := env.pool.Exec(env.ctx, `UPDATE users SET background = 'violet' WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("store retired preset: %v", err)
	}
	if got, err := env.store.GetUserBackground(env.ctx, user.ID); err != nil || got != model.BackgroundIndigo {
		t.Fatalf("retired preset reads as %q, %v", got, err)
	}

	if _, err := env.store.GetUserBackground(env.ctx, uuid.New()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetUserBackground unknown user = %v", err)
	}
	if err := env.store.SetUserBackground(env.ctx, uuid.New(), model.BackgroundSky); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("SetUserBackground unknown user = %v", err)
	}
	if err := env.store.DeleteUser(env.ctx, user.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := env.store.GetUserBackground(env.ctx, user.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetUserBackground deleted user = %v", err)
	}
	if err := env.store.SetUserBackground(env.ctx, user.ID, model.BackgroundSky); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("SetUserBackground deleted user = %v", err)
	}
}
