package server

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewAuthRateLimiterOptions(t *testing.T) {
	t.Parallel()
	defaults := newAuthRateLimiter(AuthRateLimitOptions{})
	if defaults.byIP.limit != defaultAuthIPAttempts || defaults.byIP.window != defaultAuthIPWindow || defaults.byIdentifier.limit != defaultAuthIdentifierAttempts || defaults.byIdentifier.window != defaultAuthIdentifierWindow {
		t.Fatalf("default limiter = IP %d/%v identifier %d/%v", defaults.byIP.limit, defaults.byIP.window, defaults.byIdentifier.limit, defaults.byIdentifier.window)
	}

	configured := newAuthRateLimiter(AuthRateLimitOptions{
		IPAttempts:         2,
		IPWindow:           2 * time.Minute,
		IdentifierAttempts: 3,
		IdentifierWindow:   3 * time.Minute,
	})
	if configured.byIP.limit != 2 || configured.byIP.window != 2*time.Minute || configured.byIdentifier.limit != 3 || configured.byIdentifier.window != 3*time.Minute {
		t.Fatalf("configured limiter = IP %d/%v identifier %d/%v", configured.byIP.limit, configured.byIP.window, configured.byIdentifier.limit, configured.byIdentifier.window)
	}
}

func TestFixedWindowLimiterExhaustionAndRecovery(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	limiter := newFixedWindowLimiter(2, time.Minute, 10, func() time.Time { return now })

	for attempt := 1; attempt <= 2; attempt++ {
		if allowed, retry := limiter.allow("account"); !allowed || retry != 0 {
			t.Fatalf("attempt %d = allowed %v retry %v", attempt, allowed, retry)
		}
	}
	if allowed, retry := limiter.allow("account"); allowed || retry != time.Minute {
		t.Fatalf("exhausted = allowed %v retry %v, want false/1m", allowed, retry)
	}

	now = now.Add(30 * time.Second)
	if allowed, retry := limiter.allow("account"); allowed || retry != 30*time.Second {
		t.Fatalf("half window = allowed %v retry %v, want false/30s", allowed, retry)
	}
	now = now.Add(30 * time.Second)
	if allowed, retry := limiter.allow("account"); !allowed || retry != 0 {
		t.Fatalf("recovered = allowed %v retry %v, want true/0", allowed, retry)
	}
}

func TestFixedWindowLimiterBoundsEntries(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	limiter := newFixedWindowLimiter(2, time.Minute, 1, func() time.Time { return now })
	if allowed, _ := limiter.allow("first"); !allowed {
		t.Fatal("first key denied")
	}
	if allowed, retry := limiter.allow("second"); allowed || retry != time.Minute {
		t.Fatalf("capacity = allowed %v retry %v, want false/1m", allowed, retry)
	}
	now = now.Add(time.Minute)
	if allowed, retry := limiter.allow("second"); !allowed || retry != 0 {
		t.Fatalf("capacity recovery = allowed %v retry %v, want true/0", allowed, retry)
	}
}

func TestAuthIPRateLimited(t *testing.T) {
	t.Parallel()
	srv := NewWithOptions(nil, nil, Options{AuthRateLimit: AuthRateLimitOptions{
		IPAttempts: 1,
		IPWindow:   time.Minute,
	}})
	calls := 0
	handler := srv.authIPRateLimited(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	})

	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = "192.0.2.10:1234"
		recorder := httptest.NewRecorder()
		handler(recorder, req)
		return recorder
	}
	if got := request(); got.Code != http.StatusNoContent {
		t.Fatalf("first code = %d, want 204", got.Code)
	}
	if got := request(); got.Code != http.StatusTooManyRequests || got.Header().Get("Retry-After") != "60" {
		t.Fatalf("limited code = %d Retry-After = %q", got.Code, got.Header().Get("Retry-After"))
	}
	if calls != 1 {
		t.Fatalf("handler calls = %d, want 1", calls)
	}
}

func TestAuthIdentifierNormalization(t *testing.T) {
	t.Parallel()
	srv := NewWithOptions(nil, nil, Options{AuthRateLimit: AuthRateLimitOptions{
		IdentifierAttempts: 1,
		IdentifierWindow:   time.Minute,
	}})
	if !srv.allowAuthIdentifier(httptest.NewRecorder(), " User ") {
		t.Fatal("first normalized identifier denied")
	}
	limited := httptest.NewRecorder()
	allowed := srv.allowAuthIdentifier(limited, "user")
	if allowed || limited.Code != http.StatusTooManyRequests {
		t.Fatalf("normalized identifier allowed=%v code=%d, want false/429", allowed, limited.Code)
	}
	if !srv.allowAuthIdentifier(httptest.NewRecorder(), "") {
		t.Fatal("first unknown identifier denied")
	}
	if srv.allowAuthIdentifier(httptest.NewRecorder(), "   ") {
		t.Fatal("repeated unknown identifier allowed")
	}
}

func TestClientIPUsesImmediatePeer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		remote string
		want   string
	}{
		{remote: "192.0.2.10:1234", want: "192.0.2.10"},
		{remote: "[2001:db8::1]:443", want: "2001:db8::1"},
		{remote: "192.0.2.11", want: "192.0.2.11"},
		{remote: "local-peer", want: "local-peer"},
		{remote: "", want: "unknown"},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = tt.remote
		req.Header.Set("X-Forwarded-For", "203.0.113.50")
		if got := clientIP(req, nil); got != tt.want {
			t.Fatalf("clientIP(%q) = %q, want %q", tt.remote, got, tt.want)
		}
	}
}

func TestClientIPUsesForwardedChainFromTrustedPeer(t *testing.T) {
	t.Parallel()
	trusted := []net.IPNet{mustIPNet(t, "192.0.2.0/24"), mustIPNet(t, "2001:db8:1::/48")}
	tests := []struct {
		name      string
		remote    string
		forwarded string
		want      string
	}{
		{name: "direct client", remote: "192.0.2.10:1234", forwarded: "203.0.113.50", want: "203.0.113.50"},
		{name: "trusted chain", remote: "192.0.2.10:1234", forwarded: "203.0.113.50, 192.0.2.20", want: "203.0.113.50"},
		{name: "IPv6 chain", remote: "192.0.2.10:1234", forwarded: "2001:db8:2::1, 2001:db8:1::20", want: "2001:db8:2::1"},
		{name: "invalid chain", remote: "192.0.2.10:1234", forwarded: "not-an-ip", want: "192.0.2.10"},
		{name: "missing chain", remote: "192.0.2.10:1234", want: "192.0.2.10"},
		{name: "only trusted hops", remote: "192.0.2.10:1234", forwarded: "192.0.2.20", want: "192.0.2.10"},
		{name: "untrusted peer", remote: "198.51.100.10:1234", forwarded: "203.0.113.50", want: "198.51.100.10"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remote
			if tt.forwarded != "" {
				req.Header.Set("X-Forwarded-For", tt.forwarded)
			}
			if got := clientIP(req, trusted); got != tt.want {
				t.Fatalf("clientIP(%q, %q) = %q, want %q", tt.remote, tt.forwarded, got, tt.want)
			}
		})
	}
}

func mustIPNet(t *testing.T, raw string) net.IPNet {
	t.Helper()
	_, network, err := net.ParseCIDR(raw)
	if err != nil {
		t.Fatalf("ParseCIDR(%q): %v", raw, err)
	}
	return *network
}

func TestRetryAfterSeconds(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		duration time.Duration
		want     int
	}{
		{duration: 0, want: 1},
		{duration: time.Millisecond, want: 1},
		{duration: 1500 * time.Millisecond, want: 2},
	} {
		if got := retryAfterSeconds(tt.duration); got != tt.want {
			t.Fatalf("retryAfterSeconds(%v) = %d, want %d", tt.duration, got, tt.want)
		}
	}
}
