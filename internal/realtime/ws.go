package realtime

import (
	"net/http"

	"github.com/coder/websocket"
)

// Handler returns an http.Handler that upgrades incoming requests to a
// WebSocket and binds them to the hub.
//
// Origin checking is disabled in v0 — the API itself has no auth yet.
// Tighten this (and add auth) before exposing the server publicly.
func (h *Hub) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()

		c := newClient(conn, h)
		c.run(r.Context())
	})
}
