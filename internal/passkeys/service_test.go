package passkeys

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/bradleymackey/track-slash/internal/testutil"
)

func TestMain(m *testing.M) {
	os.Exit(testutil.RunWithMigratedTemplate(m))
}

func TestOriginUsesConfiguredPublicOrigin(t *testing.T) {
	s := New(nil, "https://track.example.com")
	req := httptest.NewRequest("GET", "http://localhost:8080/login", nil)
	got, err := s.origin(req)
	if err != nil {
		t.Fatalf("origin: %v", err)
	}
	if got != "https://track.example.com" {
		t.Fatalf("origin = %q, want configured origin", got)
	}
}

func TestOriginLocalhostFallback(t *testing.T) {
	for _, raw := range []string{"http://localhost:8080/login", "http://127.0.0.1:8080/login"} {
		t.Run(raw, func(t *testing.T) {
			s := New(nil, "")
			req := httptest.NewRequest("GET", raw, nil)
			got, err := s.origin(req)
			if err != nil {
				t.Fatalf("origin: %v", err)
			}
			want := raw[:len(raw)-len("/login")]
			if got != want {
				t.Fatalf("origin = %q, want %q", got, want)
			}
		})
	}
}

func TestRelyingPartyLocalhostFallback(t *testing.T) {
	s := New(nil, "")
	req := httptest.NewRequest("GET", "http://localhost:8080/login", nil)
	rp, err := s.rp(req)
	if err != nil {
		t.Fatalf("rp: %v", err)
	}
	if rp.id != "localhost" || rp.origin != "http://localhost:8080" || rp.web == nil {
		t.Fatalf("rp = %+v", rp)
	}
}

func TestOriginRejectsNonLocalhostFallback(t *testing.T) {
	s := New(nil, "")
	req := httptest.NewRequest("GET", "http://track.example.com/login", nil)
	if _, err := s.origin(req); err == nil {
		t.Fatal("origin err = nil, want error")
	}
	if _, err := s.origin(nil); err == nil {
		t.Fatal("nil request origin err = nil, want error")
	}
}

func TestHostHelpers(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{raw: "localhost:8080", want: "localhost"},
		{raw: "[::1]:8080", want: "::1"},
		{raw: "track.example.com", want: "track.example.com"},
	} {
		if got := hostOnly(tc.raw); got != tc.want {
			t.Fatalf("hostOnly(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
	for _, host := range []string{"localhost", "127.0.0.1", "::1"} {
		if !isLocalHost(host) {
			t.Fatalf("isLocalHost(%q) = false, want true", host)
		}
	}
	if isLocalHost("track.example.com") {
		t.Fatal("isLocalHost(track.example.com) = true, want false")
	}
}

func TestBeginCeremoniesIntegration(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := testutil.NewMigratedDatabase(t)
	st := store.New(db.Pool)
	svc := New(st, "http://localhost:8080")
	req := httptest.NewRequest("POST", "http://localhost:8080/passkeys", nil)
	username := "pk" + time.Now().Format("150405000000000")

	signup, err := svc.BeginSignup(ctx, req, SignupOptionsParams{Username: username, Name: "Pass Key"})
	if err != nil {
		t.Fatalf("BeginSignup: %v", err)
	}
	if signup.CeremonyID == uuid.Nil || signup.PublicKey.RelyingParty.ID != "localhost" || signup.PublicKey.User.Name != username {
		t.Fatalf("signup options = %+v", signup)
	}
	if signup.PublicKey.AuthenticatorSelection.UserVerification != protocol.VerificationRequired {
		t.Fatalf("signup user verification = %q", signup.PublicKey.AuthenticatorSelection.UserVerification)
	}

	if _, err := st.CreateAccount(ctx, store.CreateAccountParams{
		Username: "existing" + username,
		Password: "correct-horse-battery",
	}); err != nil {
		t.Fatalf("CreateAccount existing: %v", err)
	}
	if _, err := svc.BeginSignup(ctx, req, SignupOptionsParams{Username: "existing" + username}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate BeginSignup err = %v, want ErrConflict", err)
	}

	login, err := svc.BeginLogin(ctx, req)
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	if login.CeremonyID == uuid.Nil || login.PublicKey.RelyingPartyID != "localhost" || login.PublicKey.UserVerification != protocol.VerificationRequired {
		t.Fatalf("login options = %+v", login)
	}

	user, err := st.CreateAccount(ctx, store.CreateAccountParams{
		Username: "add" + username,
		Password: "correct-horse-battery",
		Name:     "Add User",
	})
	if err != nil {
		t.Fatalf("CreateAccount add user: %v", err)
	}
	existing := webauthn.Credential{ID: []byte("existing-" + username), PublicKey: []byte("public-key"), Flags: webauthn.CredentialFlags{UserVerified: true}}
	if _, err := st.AddPasskeyCredential(ctx, user.ID, "localhost", "Existing", existing); err != nil {
		t.Fatalf("AddPasskeyCredential existing: %v", err)
	}
	add, err := svc.BeginAdd(ctx, req, AddOptionsParams{User: user, Name: "New laptop"})
	if err != nil {
		t.Fatalf("BeginAdd: %v", err)
	}
	if add.CeremonyID == uuid.Nil || add.PublicKey.User.Name != user.Username || add.PublicKey.AuthenticatorSelection.UserVerification != protocol.VerificationRequired {
		t.Fatalf("add options = %+v", add)
	}

	reauth, err := svc.BeginReauth(ctx, req, user)
	if err != nil {
		t.Fatalf("BeginReauth: %v", err)
	}
	if reauth.CeremonyID == uuid.Nil || len(reauth.PublicKey.AllowedCredentials) != 1 || reauth.PublicKey.UserVerification != protocol.VerificationRequired {
		t.Fatalf("reauth options = %+v", reauth)
	}
}

func TestRequestWithJSONBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/passkeys", nil)
	next := requestWithJSONBody(req, []byte(`{"ok":true}`))
	if next == req {
		t.Fatal("requestWithJSONBody returned original request")
	}
	if next.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type = %q", next.Header.Get("Content-Type"))
	}
	if next.ContentLength != int64(len(`{"ok":true}`)) {
		t.Fatalf("ContentLength = %d", next.ContentLength)
	}
	data, err := io.ReadAll(next.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != `{"ok":true}` {
		t.Fatalf("body = %q", data)
	}
	if err := next.Body.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	empty := requestWithJSONBody(req, nil)
	data, err = io.ReadAll(empty.Body)
	if err != nil {
		t.Fatalf("ReadAll empty: %v", err)
	}
	if len(data) != 0 || empty.ContentLength != 0 {
		t.Fatalf("empty body = %q length=%d", data, empty.ContentLength)
	}
}

func TestWebAuthnUserMethodsAndCredentialConversion(t *testing.T) {
	credential := webauthn.Credential{ID: []byte("credential-id")}
	user := webAuthnUser{
		handle:      []byte("handle"),
		username:    "member",
		displayName: "",
		credentials: []webauthn.Credential{credential},
	}
	if got := string(user.WebAuthnID()); got != "handle" {
		t.Fatalf("WebAuthnID = %q", got)
	}
	if user.WebAuthnName() != "member" || user.WebAuthnDisplayName() != "member" {
		t.Fatalf("user names = %q / %q", user.WebAuthnName(), user.WebAuthnDisplayName())
	}
	user.displayName = "Member Name"
	if user.WebAuthnDisplayName() != "Member Name" {
		t.Fatalf("display name = %q", user.WebAuthnDisplayName())
	}
	if got := user.WebAuthnCredentials(); len(got) != 1 || string(got[0].ID) != "credential-id" {
		t.Fatalf("WebAuthnCredentials = %+v", got)
	}

	converted := passkeyCredentials([]store.PasskeyCredential{{Credential: credential}})
	if len(converted) != 1 || string(converted[0].ID) != "credential-id" {
		t.Fatalf("passkeyCredentials = %+v", converted)
	}
}

func TestSessionHelpers(t *testing.T) {
	expires := time.Now().Add(time.Minute).Round(0)
	data := mustSessionJSON(&webauthn.SessionData{Challenge: "challenge", Expires: expires})
	session := webAuthnSession(store.PasskeySession{SessionData: data})
	if session.Challenge != "challenge" || !session.Expires.Equal(expires) {
		t.Fatalf("session = %+v", session)
	}
	if got := sessionExpires(&webauthn.SessionData{Expires: expires}); !got.Equal(expires) {
		t.Fatalf("sessionExpires = %v, want %v", got, expires)
	}
	if got := sessionExpires(&webauthn.SessionData{}); time.Until(got) < 4*time.Minute {
		t.Fatalf("default expiry too soon: %v", got)
	}
}
