package server

import "net/http"

const (
	contentSecurityPolicy = "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'self'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; media-src 'self'; frame-src 'none'; worker-src 'self'; manifest-src 'self'"
	permissionsPolicy     = "camera=(), geolocation=(), microphone=(), payment=(), usb=()"
	strictTransportPolicy = "max-age=31536000"
)

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers := w.Header()
		headers.Set("Content-Security-Policy", contentSecurityPolicy)
		headers.Set("Permissions-Policy", permissionsPolicy)
		headers.Set("Referrer-Policy", "no-referrer")
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("X-Frame-Options", "DENY")
		if s.httpsDeployment {
			headers.Set("Strict-Transport-Security", strictTransportPolicy)
		}
		next.ServeHTTP(w, r)
	})
}
