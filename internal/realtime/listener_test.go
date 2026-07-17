package realtime

import (
	"context"
	"testing"
)

func TestListenerRunOnceRejectsInvalidDatabaseURL(t *testing.T) {
	t.Parallel()
	listener := NewListener("://invalid", NewHub())
	if err := listener.runOnce(context.Background()); err == nil {
		t.Fatal("runOnce accepted invalid database URL")
	}
}

func TestListenersUseDistinctApplicationNames(t *testing.T) {
	t.Parallel()
	first := NewListener("postgres://example", NewHub())
	second := NewListener("postgres://example", NewHub())
	if first.applicationName == second.applicationName {
		t.Fatalf("application names are both %q", first.applicationName)
	}
}
