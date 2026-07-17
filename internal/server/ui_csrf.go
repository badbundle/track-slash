package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	uiCSRFHeaderName        = "X-CSRF-Token"
	uiCSRFFormName          = "csrf_token"
	uiPreAuthCSRFCookieName = "track_slash_login_csrf"
	uiPreAuthCSRFMaxAge     = time.Hour
)

var errUICSRFRandom = errors.New("generate CSRF token")

type uiCSRFContextKey struct{}

func uiDerivedCSRFToken(purpose, secret string) string {
	digest := sha256.Sum256([]byte("track-slash csrf " + purpose + ":" + secret))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func uiSessionCSRFToken(r *http.Request) string {
	if token, ok := r.Context().Value(uiCSRFContextKey{}).(string); ok && token != "" {
		return token
	}
	cookie, err := r.Cookie(uiAuthCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return ""
	}
	return uiDerivedCSRFToken("session", cookie.Value)
}

func (s *Server) ensureUIPreAuthCSRFToken(w http.ResponseWriter, r *http.Request) (string, error) {
	if token, ok := r.Context().Value(uiCSRFContextKey{}).(string); ok && token != "" {
		return token, nil
	}
	if cookie, err := r.Cookie(uiPreAuthCSRFCookieName); err == nil && validUIPreAuthCSRFSecret(cookie.Value) {
		return uiDerivedCSRFToken("pre-auth", cookie.Value), nil
	}
	secretBytes := make([]byte, 32)
	random := s.csrfRandom
	if random == nil {
		random = rand.Reader
	}
	if _, err := io.ReadFull(random, secretBytes); err != nil {
		return "", errors.Join(errUICSRFRandom, err)
	}
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)
	http.SetCookie(w, &http.Cookie{
		Name:     uiPreAuthCSRFCookieName,
		Value:    secret,
		Path:     "/",
		MaxAge:   int(uiPreAuthCSRFMaxAge.Seconds()),
		Expires:  time.Now().Add(uiPreAuthCSRFMaxAge),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   s.secureCookies || r.TLS != nil,
	})
	return uiDerivedCSRFToken("pre-auth", secret), nil
}

func validUIPreAuthCSRFSecret(secret string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(secret)
	return err == nil && len(decoded) == 32
}

func (s *Server) clearUIPreAuthCSRFCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     uiPreAuthCSRFCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   s.secureCookies || r.TLS != nil,
	})
}

func (s *Server) uiPreAuthCSRFMiddleware(next http.Handler) http.Handler {
	return s.uiCSRFMiddleware(func(r *http.Request) string {
		cookie, err := r.Cookie(uiPreAuthCSRFCookieName)
		if err != nil || !validUIPreAuthCSRFSecret(cookie.Value) {
			return ""
		}
		return uiDerivedCSRFToken("pre-auth", cookie.Value)
	})(next)
}

func (s *Server) uiSessionCSRFMiddleware(next http.Handler) http.Handler {
	return s.uiCSRFMiddleware(uiSessionCSRFToken)(next)
}

func (s *Server) uiCSRFMiddleware(expectedToken func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !uiUnsafeMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			expected := expectedToken(r)
			provided := uiRequestCSRFToken(r)
			if expected == "" || provided == "" || subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) != 1 || !s.uiCSRFSourceAllowed(r) {
				http.Error(w, "CSRF validation failed.", http.StatusForbidden)
				return
			}
			ctx := context.WithValue(r.Context(), uiCSRFContextKey{}, expected)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func uiUnsafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}

func uiRequestCSRFToken(r *http.Request) string {
	if token := strings.TrimSpace(r.Header.Get(uiCSRFHeaderName)); token != "" {
		return token
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		return ""
	}
	if err := r.ParseForm(); err != nil {
		return ""
	}
	return strings.TrimSpace(r.Form.Get(uiCSRFFormName))
}

func (s *Server) uiCSRFSourceAllowed(r *http.Request) bool {
	expected := s.publicOrigin
	if expected == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		expected = scheme + "://" + r.Host
	}
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		return uiSameOrigin(origin, expected)
	}
	if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" {
		return uiSameOrigin(referer, expected)
	}
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))) {
	case "", "same-origin", "none":
		return true
	default:
		return false
	}
}

func uiSameOrigin(candidate, expected string) bool {
	candidateURL, err := url.Parse(candidate)
	if err != nil || candidateURL.User != nil || candidateURL.Scheme == "" || candidateURL.Host == "" {
		return false
	}
	expectedURL, err := url.Parse(expected)
	if err != nil || expectedURL.User != nil || expectedURL.Scheme == "" || expectedURL.Host == "" {
		return false
	}
	return strings.EqualFold(candidateURL.Scheme, expectedURL.Scheme) && strings.EqualFold(candidateURL.Host, expectedURL.Host)
}
