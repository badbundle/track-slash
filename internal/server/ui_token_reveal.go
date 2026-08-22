package server

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"
)

const (
	uiTokenRevealCookieName = "track_slash_token_reveal"
	uiTokenRevealPath       = "/tokens"
	uiTokenRevealMaxAge     = time.Minute
)

// A newly created API token is shown once and cannot be recovered from the
// database, which is the whole reason POST /tokens used to answer 200 with the
// page instead of redirecting. This carries the raw token across the redirect in
// a cookie that is HttpOnly, scoped to /tokens, expires in a minute, and is
// cleared the moment it is read.
//
// The value is bound to the session so a same-site attacker cannot plant a
// cookie that makes the page display a token of their choosing. DEPLOYMENT.md
// treats sibling subdomains as untrusted, and SameSite alone does not.
func (s *Server) setUITokenRevealCookie(w http.ResponseWriter, r *http.Request, rawToken string) {
	if rawToken == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     uiTokenRevealCookieName,
		Value:    rawToken + "." + uiTokenRevealSignature(r, rawToken),
		Path:     uiTokenRevealPath,
		MaxAge:   int(uiTokenRevealMaxAge.Seconds()),
		Expires:  time.Now().Add(uiTokenRevealMaxAge),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   s.secureCookies || r.TLS != nil,
	})
}

// takeUITokenRevealCookie returns the raw token a preceding create stashed, and
// clears the cookie whether or not the value was usable.
func (s *Server) takeUITokenRevealCookie(w http.ResponseWriter, r *http.Request) string {
	cookie, err := r.Cookie(uiTokenRevealCookieName)
	if err != nil || cookie.Value == "" {
		return ""
	}
	s.clearUITokenRevealCookie(w, r)
	rawToken, signature, ok := strings.Cut(cookie.Value, ".")
	if !ok || rawToken == "" {
		return ""
	}
	if subtle.ConstantTimeCompare([]byte(signature), []byte(uiTokenRevealSignature(r, rawToken))) != 1 {
		return ""
	}
	return rawToken
}

func (s *Server) clearUITokenRevealCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     uiTokenRevealCookieName,
		Value:    "",
		Path:     uiTokenRevealPath,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   s.secureCookies || r.TLS != nil,
	})
}

func uiTokenRevealSignature(r *http.Request, rawToken string) string {
	cookie, err := r.Cookie(uiAuthCookieName)
	if err != nil {
		return ""
	}
	return uiDerivedCSRFToken("token-reveal:"+rawToken, cookie.Value)
}
