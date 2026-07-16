package server

import (
	"testing"
	"time"

	"github.com/bradleymackey/track-slash/internal/model"
)

func TestSessionTTLConfiguration(t *testing.T) {
	t.Parallel()

	configured := NewWithOptions(nil, nil, Options{SessionTTL: 90 * time.Minute})
	if configured.sessionTTL != 90*time.Minute {
		t.Fatalf("configured session TTL = %v, want 90m", configured.sessionTTL)
	}

	defaulted := NewWithOptions(nil, nil, Options{})
	if defaulted.sessionTTL != 7*24*time.Hour {
		t.Fatalf("default session TTL = %v, want 168h", defaulted.sessionTTL)
	}
}

func TestBoundedSessionExpiry(t *testing.T) {
	t.Parallel()

	absolute := time.Now().Add(time.Hour)
	before := absolute.Add(-time.Minute)
	after := absolute.Add(time.Minute)

	if got := boundedSessionExpiry(model.AuthTokenKindAPI, nil, &absolute); got != nil {
		t.Fatalf("API token expiry = %v, want nil", got)
	}
	if got := boundedSessionExpiry(model.AuthTokenKindSession, nil, &absolute); got != &absolute {
		t.Fatalf("default session expiry = %v, want absolute", got)
	}
	if got := boundedSessionExpiry(model.AuthTokenKindSession, &before, &absolute); got != &before {
		t.Fatalf("short session expiry = %v, want requested", got)
	}
	if got := boundedSessionExpiry(model.AuthTokenKindSession, &after, &absolute); got != &absolute {
		t.Fatalf("long session expiry = %v, want absolute", got)
	}
}
