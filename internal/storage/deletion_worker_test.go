package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"testing"
	"time"
)

func TestNewDeletionWorkerOptions(t *testing.T) {
	t.Parallel()
	defaults := NewDeletionWorker(nil, nil, DeletionWorkerOptions{})
	if defaults.batchSize != 5 || defaults.maxAttempts != 8 || defaults.pollInterval != time.Second ||
		defaults.leaseTimeout != time.Minute || defaults.deleteTimeout != 10*time.Second ||
		defaults.minBackoff != 5*time.Second || defaults.maxBackoff != 5*time.Minute || defaults.logger == nil {
		t.Fatalf("default worker = %+v", defaults)
	}

	logger := log.New(io.Discard, "", 0)
	configured := NewDeletionWorker(nil, nil, DeletionWorkerOptions{
		BatchSize: 2, MaxAttempts: 3, PollInterval: 4 * time.Second, LeaseTimeout: 5 * time.Second,
		DeleteTimeout: 6 * time.Second, MinBackoff: 7 * time.Second, MaxBackoff: time.Second, Logger: logger,
	})
	if configured.batchSize != 2 || configured.maxAttempts != 3 || configured.pollInterval != 4*time.Second ||
		configured.leaseTimeout != 5*time.Second || configured.deleteTimeout != 6*time.Second ||
		configured.minBackoff != 7*time.Second || configured.maxBackoff != 7*time.Second || configured.logger != logger {
		t.Fatalf("configured worker = %+v", configured)
	}
}

func TestDeletionWorkerRunOnceRequiresDependencies(t *testing.T) {
	t.Parallel()
	worker := NewDeletionWorker(nil, nil, DeletionWorkerOptions{})
	if _, err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce err = nil, want dependency error")
	}
}

func TestDeletionWorkerRunStopsWithContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	NewDeletionWorker(nil, nil, DeletionWorkerOptions{PollInterval: time.Hour}).Run(ctx)
}

func TestDeletionWorkerRunLogsErrorsUntilCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	var logs bytes.Buffer
	worker := NewDeletionWorker(nil, nil, DeletionWorkerOptions{
		PollInterval: time.Millisecond,
		Logger:       log.New(cancelWriter{Writer: &logs, cancel: cancel}, "", 0),
	})
	worker.Run(ctx)
	if !strings.Contains(logs.String(), "storage deletion worker") {
		t.Fatalf("worker log = %q", logs.String())
	}
}

type cancelWriter struct {
	io.Writer
	cancel context.CancelFunc
}

func (w cancelWriter) Write(p []byte) (int, error) {
	w.cancel()
	return w.Writer.Write(p)
}

func TestDeletionBackoff(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: time.Second},
		{attempt: 1, want: time.Second},
		{attempt: 2, want: 2 * time.Second},
		{attempt: 3, want: 4 * time.Second},
		{attempt: 4, want: 5 * time.Second},
		{attempt: 100, want: 5 * time.Second},
		{attempt: 1, want: 5 * time.Second},
	} {
		minimum := time.Second
		maximum := 5 * time.Second
		if tc.attempt == 1 && tc.want == 5*time.Second {
			minimum = 10 * time.Second
		}
		if got := deletionBackoff(tc.attempt, minimum, maximum); got != tc.want {
			t.Fatalf("deletionBackoff(%d) = %v, want %v", tc.attempt, got, tc.want)
		}
	}
}

func TestBoundedDeletionError(t *testing.T) {
	t.Parallel()
	if got := boundedDeletionError(errors.New("  retry me  ")); got != "retry me" {
		t.Fatalf("short error = %q, want retry me", got)
	}
	long := strings.Repeat("x", maxDeletionErrorLength+1)
	if got := boundedDeletionError(errors.New(long)); len(got) != maxDeletionErrorLength {
		t.Fatalf("bounded error length = %d, want %d", len(got), maxDeletionErrorLength)
	}
}
