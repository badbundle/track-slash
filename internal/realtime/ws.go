package realtime

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

type TopicAuthorizer func(context.Context, string, uuid.UUID) error

type OriginPolicy struct {
	AllowedOrigins        []string
	AllowMissingOrigin    bool
	AllowLocalhostOrigins bool
}

// Handler returns an http.Handler that upgrades incoming requests to a
// WebSocket and binds them to the hub.
//
// Browser origins are checked against policy before the upgrade. A missing
// Origin is only accepted when the caller explicitly enables non-browser use.
func (h *Hub) Handler(policy OriginPolicy, authorize TopicAuthorizer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !policy.Allows(r.Header.Get("Origin")) {
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

		c := newClient(conn, h, authorize)
		c.run(r.Context())
	})
}

func (p OriginPolicy) Allows(origin string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return p.AllowMissingOrigin
	}
	for _, a := range p.AllowedOrigins {
		if a == origin {
			return true
		}
	}
	return p.AllowLocalhostOrigins && localhostOrigin(origin)
}

func localhostOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if path := strings.TrimRight(u.EscapedPath(), "/"); path != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
