package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5"
)

// pgChannel is the single Postgres NOTIFY channel all triggers emit on.
// In-memory fanout in Hub does the per-topic routing.
const pgChannel = "track_events"

// Listener owns a dedicated Postgres connection that LISTENs on pgChannel
// and forwards every notification into the Hub.
//
// A dedicated connection is required because LISTEN is session-scoped:
// pooled connections would silently lose the subscription on release.
type Listener struct {
	dbURL           string
	hub             *Hub
	applicationName string
}

func NewListener(dbURL string, hub *Hub) *Listener {
	return &Listener{
		dbURL:           dbURL,
		hub:             hub,
		applicationName: fmt.Sprintf("track-slash-realtime-%016x", rand.Uint64()),
	}
}

// Run blocks until ctx is cancelled. On any connection / decode error it
// logs, backs off, and reconnects. Every successful LISTEN registration asks
// subscribed clients to refetch because events may have been lost while the
// listener was disconnected.
func (l *Listener) Run(ctx context.Context) {
	const (
		minBackoff = 250 * time.Millisecond
		maxBackoff = 30 * time.Second
	)
	backoff := minBackoff

	for {
		if ctx.Err() != nil {
			return
		}
		err := l.runOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("realtime listener: %v (reconnect in %s)", err, backoff)
		}

		jitter := time.Duration(rand.Int64N(int64(backoff)))
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff/2 + jitter):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (l *Listener) runOnce(ctx context.Context) error {
	config, err := pgx.ParseConfig(l.dbURL)
	if err != nil {
		return err
	}
	config.RuntimeParams["application_name"] = l.applicationName
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()

	if _, err := conn.Exec(ctx, "LISTEN "+pgChannel); err != nil {
		return err
	}
	log.Printf("realtime listener: connected, LISTEN %s", pgChannel)
	l.hub.ResyncAll(resyncListener)

	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			return err
		}
		var ev Event
		if err := json.Unmarshal([]byte(n.Payload), &ev); err != nil {
			log.Printf("realtime listener: bad payload %q: %v", n.Payload, err)
			continue
		}
		l.hub.Publish(ev)
	}
}
