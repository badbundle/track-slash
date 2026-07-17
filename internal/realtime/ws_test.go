package realtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

func TestOriginAllowed(t *testing.T) {
	cases := []struct {
		name    string
		origin  string
		allowed []string
		want    bool
	}{
		{"empty allow-list allows any origin", "https://x.com", nil, true},
		{"empty allow-list allows empty origin", "", nil, true},
		{"non-empty allow-list allows empty origin (non-browser client)", "", []string{"https://app.example.com"}, true},
		{"exact match", "https://app.example.com", []string{"https://app.example.com"}, true},
		{"second entry matches", "http://localhost:3000", []string{"https://app.example.com", "http://localhost:3000"}, true},
		{"mismatch rejected", "https://evil.com", []string{"https://app.example.com"}, false},
		{"port mismatch rejected", "http://localhost:3001", []string{"http://localhost:3000"}, false},
		{"scheme mismatch rejected", "http://app.example.com", []string{"https://app.example.com"}, false},
	}
	for _, c := range cases {
		if got := originAllowed(c.origin, c.allowed); got != c.want {
			t.Errorf("%s: originAllowed(%q, %v) = %v, want %v", c.name, c.origin, c.allowed, got, c.want)
		}
	}
}

// TestHandlerRejectsDisallowedOrigin pins the 403 path so a regression in the
// origin gate is loud. A real WS upgrade isn't needed: the gate runs before
// Accept.
func TestHandlerRejectsDisallowedOrigin(t *testing.T) {
	hub := NewHub()
	h := hub.Handler([]string{"https://app.example.com"}, nil)

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Origin", "https://evil.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403", rec.Code)
	}
}

func TestHandlerAcceptsAllowedOriginPreUpgrade(t *testing.T) {
	// We can't complete a real upgrade against httptest.NewRecorder (no
	// hijacker). We only assert the gate doesn't 403 the request — the
	// downstream Accept will fail to upgrade, which is fine: nothing
	// writes a body and the handler returns.
	hub := NewHub()
	h := hub.Handler([]string{"https://app.example.com"}, nil)

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Fatalf("allowed origin was forbidden")
	}
}

func TestHandlerSendsResyncControlToSubscribedClient(t *testing.T) {
	hub := NewHub()
	ts := httptest.NewServer(hub.Handler(nil, nil))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	topic := ProjectTopic(uuid.New())
	subscribe, err := json.Marshal(controlMsg{Action: "subscribe", Topic: topic})
	if err != nil {
		t.Fatalf("marshal subscribe: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, subscribe); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	for hub.TopicCount() != 1 {
		select {
		case <-ctx.Done():
			t.Fatal("subscription was not registered")
		case <-time.After(time.Millisecond):
		}
	}

	hub.ResyncAll(resyncOverflow)
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read resync: %v", err)
	}
	var got serverControl
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal resync: %v", err)
	}
	if got.Type != resyncMessageType || got.Reason != resyncOverflow {
		t.Fatalf("control = %#v, want overflow resync", got)
	}
}
