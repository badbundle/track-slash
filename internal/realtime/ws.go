package realtime

import (
	"net/http"

	"github.com/coder/websocket"
)

// Handler returns an http.Handler that upgrades incoming requests to a
// WebSocket and binds them to the hub.
//
// allowedOrigins is matched exactly against the request's Origin header.
// An empty slice disables the origin check entirely — appropriate for dev
// but not for public deployments.
func (h *Hub) Handler(allowedOrigins []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !originAllowed(r.Header.Get("Origin"), allowedOrigins) {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			// Origin already validated above; library check is redundant
			// and would require a different allow-list format (host patterns).
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

// originAllowed reports whether origin is permitted by the allow-list.
// An empty allow-list permits any origin (dev default). An empty origin
// header (non-browser clients) is also allowed since there's nothing to
// spoof — browser CORS / origin checks are about cross-site abuse.
func originAllowed(origin string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	if origin == "" {
		return true
	}
	for _, a := range allowed {
		if a == origin {
			return true
		}
	}
	return false
}
