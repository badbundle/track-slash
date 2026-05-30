package store_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func TestAuthTokenLifecycle(t *testing.T) {
	env := newSprintsEnv(t)
	u, err := env.store.CreateOrUpdateAdminUser(env.ctx, "admin-"+uniqueProjectKey(t)+"@example.com", "Admin")
	if err != nil {
		t.Fatalf("CreateOrUpdateAdminUser: %v", err)
	}
	if !u.IsAdmin {
		t.Fatal("admin user IsAdmin = false")
	}

	created, err := env.store.CreateAuthToken(env.ctx, store.CreateAuthTokenParams{
		UserID: u.ID,
		Kind:   model.AuthTokenKindAPI,
		Name:   "api",
	})
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}
	if created.RawToken == "" {
		t.Fatal("raw token empty")
	}
	var encodedHash string
	if err := env.pool.QueryRow(env.ctx, `SELECT encode(token_hash, 'hex') FROM auth_tokens WHERE id = $1`, created.Token.ID).Scan(&encodedHash); err != nil {
		t.Fatalf("query token hash: %v", err)
	}
	if encodedHash == created.RawToken || len(encodedHash) != 64 {
		t.Fatalf("stored hash = %q, raw token = %q", encodedHash, created.RawToken)
	}

	auth, err := env.store.AuthenticateToken(env.ctx, created.RawToken)
	if err != nil {
		t.Fatalf("AuthenticateToken: %v", err)
	}
	if auth.User.ID != u.ID || auth.Token.Kind != model.AuthTokenKindAPI {
		t.Fatalf("auth = %+v", auth)
	}

	tokens, err := env.store.ListAuthTokens(env.ctx, u.ID)
	if err != nil {
		t.Fatalf("ListAuthTokens: %v", err)
	}
	if len(tokens) != 1 || tokens[0].ID != created.Token.ID || tokens[0].LastUsedAt == nil {
		t.Fatalf("tokens = %+v", tokens)
	}

	if err := env.store.RevokeAuthToken(env.ctx, created.Token.ID); err != nil {
		t.Fatalf("RevokeAuthToken: %v", err)
	}
	if _, err := env.store.AuthenticateToken(env.ctx, created.RawToken); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("revoked auth err = %v, want ErrUnauthorized", err)
	}
}

func TestAuthTokenExpiryAndKind(t *testing.T) {
	env := newSprintsEnv(t)
	u, err := env.store.CreateUser(env.ctx, "user-"+uniqueProjectKey(t)+"@example.com", "User")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	past := time.Now().Add(-time.Hour)
	expired, err := env.store.CreateAuthToken(env.ctx, store.CreateAuthTokenParams{
		UserID: u.ID, Kind: model.AuthTokenKindSession, Name: "session", ExpiresAt: &past,
	})
	if err != nil {
		t.Fatalf("CreateAuthToken expired: %v", err)
	}
	if expired.Token.Kind != model.AuthTokenKindSession {
		t.Fatalf("kind = %q, want session", expired.Token.Kind)
	}
	if _, err := env.store.AuthenticateToken(env.ctx, expired.RawToken); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("expired auth err = %v, want ErrUnauthorized", err)
	}
	if _, err := env.store.CreateAuthToken(env.ctx, store.CreateAuthTokenParams{
		UserID: u.ID, Kind: model.AuthTokenKind("bogus"), Name: "bad",
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("invalid kind err = %v, want ErrConflict", err)
	}
}

func TestAccountPasswordLifecycle(t *testing.T) {
	env := newSprintsEnv(t)
	password := "correct-horse-battery"
	u, err := env.store.CreateAccount(env.ctx, store.CreateAccountParams{
		Username: " Member_" + uniqueProjectKey(t),
		Password: password,
		Name:     "",
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if u.ID == uuid.Nil || u.Username == "" || u.Email != "" || u.Name != u.Username || u.IsAdmin {
		t.Fatalf("user = %+v", u)
	}

	var storedHash string
	if err := env.pool.QueryRow(env.ctx, `
		SELECT secret_hash FROM auth_credentials
		WHERE user_id = $1 AND kind = 'password' AND identifier = $2
	`, u.ID, u.Username).Scan(&storedHash); err != nil {
		t.Fatalf("query password credential: %v", err)
	}
	if storedHash == password || !strings.HasPrefix(storedHash, "$2") {
		t.Fatalf("stored password hash = %q", storedHash)
	}

	got, err := env.store.AuthenticatePassword(env.ctx, strings.ToUpper(u.Username), password)
	if err != nil {
		t.Fatalf("AuthenticatePassword: %v", err)
	}
	if got.ID != u.ID {
		t.Fatalf("authenticated user = %+v, want %s", got, u.ID)
	}
	if _, err := env.store.AuthenticatePassword(env.ctx, u.Username, "wrong-password"); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("bad password err = %v, want ErrUnauthorized", err)
	}

	if _, err := env.pool.Exec(env.ctx, `
		INSERT INTO auth_credentials (user_id, kind, identifier, public_key)
		VALUES ($1, 'passkey', $2, '\x01')
	`, u.ID, u.Username); err != nil {
		t.Fatalf("insert passkey credential: %v", err)
	}
	if _, err := env.store.AuthenticatePassword(env.ctx, u.Username, password); err != nil {
		t.Fatalf("AuthenticatePassword with passkey row present: %v", err)
	}

	if _, err := env.pool.Exec(env.ctx, `
		UPDATE auth_credentials SET revoked_at = now()
		WHERE user_id = $1 AND kind = 'password'
	`, u.ID); err != nil {
		t.Fatalf("revoke password credential: %v", err)
	}
	if _, err := env.store.AuthenticatePassword(env.ctx, u.Username, password); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("revoked credential err = %v, want ErrUnauthorized", err)
	}
}

func TestAccountValidationAndLegacyTokenOnlyUser(t *testing.T) {
	env := newSprintsEnv(t)
	for _, tc := range []struct {
		name     string
		username string
		password string
	}{
		{name: "short username", username: "ab", password: "correct-horse-battery"},
		{name: "bad start", username: "_abc", password: "correct-horse-battery"},
		{name: "bad char", username: "abc!", password: "correct-horse-battery"},
		{name: "short password", username: "validname", password: "short"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := env.store.CreateAccount(env.ctx, store.CreateAccountParams{Username: tc.username, Password: tc.password}); err == nil {
				t.Fatal("CreateAccount err = nil")
			}
		})
	}

	u, err := env.store.CreateAccount(env.ctx, store.CreateAccountParams{Username: "dup" + strings.ToLower(uniqueProjectKey(t)), Password: "correct-horse-battery"})
	if err != nil {
		t.Fatalf("CreateAccount unique: %v", err)
	}
	if _, err := env.store.CreateAccount(env.ctx, store.CreateAccountParams{Username: strings.ToUpper(u.Username), Password: "correct-horse-battery"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate account err = %v, want ErrConflict", err)
	}

	legacy, err := env.store.CreateUser(env.ctx, "legacy-"+uuid.NewString()+"@example.com", "Legacy")
	if err != nil {
		t.Fatalf("CreateUser legacy: %v", err)
	}
	if _, err := env.store.AuthenticatePassword(env.ctx, legacy.Username, "correct-horse-battery"); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("legacy password err = %v, want ErrUnauthorized", err)
	}
}

func TestRevokeAuthTokenForUser(t *testing.T) {
	env := newSprintsEnv(t)
	u, err := env.store.CreateUser(env.ctx, "self-revoke-"+uuid.NewString()+"@example.com", "Self")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	other, err := env.store.CreateUser(env.ctx, "other-revoke-"+uuid.NewString()+"@example.com", "Other")
	if err != nil {
		t.Fatalf("CreateUser other: %v", err)
	}
	created, err := env.store.CreateAuthToken(env.ctx, store.CreateAuthTokenParams{
		UserID: u.ID,
		Kind:   model.AuthTokenKindAPI,
		Name:   "self",
	})
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}
	if err := env.store.RevokeAuthTokenForUser(env.ctx, other.ID, created.Token.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("wrong user revoke err = %v, want ErrNotFound", err)
	}
	if err := env.store.RevokeAuthTokenForUser(env.ctx, u.ID, created.Token.ID); err != nil {
		t.Fatalf("RevokeAuthTokenForUser: %v", err)
	}
	if _, err := env.store.AuthenticateToken(env.ctx, created.RawToken); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("revoked auth err = %v, want ErrUnauthorized", err)
	}
}

func TestUserSettingsProfileAndPassword(t *testing.T) {
	env := newSprintsEnv(t)
	oldPassword := "correct-horse-battery"
	newPassword := "new-correct-horse"
	u, err := env.store.CreateAccount(env.ctx, store.CreateAccountParams{
		Username: "settings" + strings.ToLower(uniqueProjectKey(t)),
		Password: oldPassword,
		Name:     "Old Name",
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	updated, err := env.store.UpdateUserProfile(env.ctx, u.ID, "New Name", "new@example.com")
	if err != nil {
		t.Fatalf("UpdateUserProfile: %v", err)
	}
	if updated.Name != "New Name" || updated.Email != "new@example.com" || updated.Username != u.Username {
		t.Fatalf("updated = %+v", updated)
	}
	updated, err = env.store.UpdateUserProfile(env.ctx, u.ID, "New Name", "")
	if err != nil {
		t.Fatalf("UpdateUserProfile clear email: %v", err)
	}
	if updated.Email != "" {
		t.Fatalf("email = %q, want empty", updated.Email)
	}
	if _, err := env.store.UpdateUserProfile(env.ctx, u.ID, "", "ok@example.com"); err == nil {
		t.Fatal("blank name err = nil")
	}
	if _, err := env.store.UpdateUserProfile(env.ctx, u.ID, "Name", "bad-email"); err == nil {
		t.Fatal("bad email err = nil")
	}
	if err := env.store.ChangePassword(env.ctx, u.ID, "wrong-password", newPassword); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("wrong password err = %v, want ErrUnauthorized", err)
	}
	if err := env.store.ChangePassword(env.ctx, u.ID, oldPassword, "short"); err == nil {
		t.Fatal("short new password err = nil")
	}
	if err := env.store.ChangePassword(env.ctx, u.ID, oldPassword, newPassword); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if _, err := env.store.AuthenticatePassword(env.ctx, u.Username, oldPassword); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("old password err = %v, want ErrUnauthorized", err)
	}
	if _, err := env.store.AuthenticatePassword(env.ctx, u.Username, newPassword); err != nil {
		t.Fatalf("new password auth: %v", err)
	}

	legacy, err := env.store.CreateUser(env.ctx, "settings-legacy-"+uuid.NewString()+"@example.com", "Legacy")
	if err != nil {
		t.Fatalf("CreateUser legacy: %v", err)
	}
	if err := env.store.ChangePassword(env.ctx, legacy.ID, oldPassword, newPassword); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("legacy change password err = %v, want ErrUnauthorized", err)
	}
}

func TestProjectMembershipAndVisibleProjects(t *testing.T) {
	env := newSprintsEnv(t)
	u, err := env.store.CreateUser(env.ctx, "member-"+uniqueProjectKey(t)+"@example.com", "Member")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	other, err := env.store.CreateProject(env.ctx, uniqueProjectKey(t), "other", "")
	if err != nil {
		t.Fatalf("CreateProject other: %v", err)
	}
	member, err := env.store.GrantProjectAccess(env.ctx, env.projectID, u.ID)
	if err != nil {
		t.Fatalf("GrantProjectAccess: %v", err)
	}
	if member.ProjectID != env.projectID || member.UserID != u.ID {
		t.Fatalf("member = %+v", member)
	}
	members, err := env.store.ListProjectMembers(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("ListProjectMembers: %v", err)
	}
	if len(members) != 1 || members[0].UserID != u.ID {
		t.Fatalf("members = %+v", members)
	}
	ok, err := env.store.UserCanAccessProject(env.ctx, u, env.projectID)
	if err != nil || !ok {
		t.Fatalf("UserCanAccessProject granted = %v, %v", ok, err)
	}
	ok, err = env.store.UserCanAccessProject(env.ctx, u, other.ID)
	if err != nil || ok {
		t.Fatalf("UserCanAccessProject other = %v, %v", ok, err)
	}

	projects, _, err := env.store.ListProjects(env.ctx, store.ListProjectsParams{
		Limit:         100,
		VisibleToUser: &u.ID,
	})
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 1 || projects[0].ID != env.projectID {
		t.Fatalf("visible projects = %+v", projects)
	}

	if err := env.store.RevokeProjectAccess(env.ctx, env.projectID, u.ID); err != nil {
		t.Fatalf("RevokeProjectAccess: %v", err)
	}
	ok, err = env.store.UserCanAccessProject(env.ctx, u, env.projectID)
	if err != nil || ok {
		t.Fatalf("UserCanAccessProject revoked = %v, %v", ok, err)
	}
}

func TestProjectOwnershipLookupHelpers(t *testing.T) {
	env := newSprintsEnv(t)
	iss := mustCreateIssue(t, env, "owned")
	sp := mustCreateSprint(t, env, "owned", date(2026, 1, 1), date(2026, 1, 5))
	author := mustCreateUser(t, env, "owner-"+uuid.NewString()+"@example.com")
	comment := mustCreateComment(t, env, iss.ID, author.ID, "hello")
	link, err := env.store.CreateIssueLink(env.ctx, store.CreateIssueLinkParams{
		SourceID: iss.ID, TargetID: mustCreateIssue(t, env, "target").ID, LinkType: model.LinkTypeBlocks,
	})
	if err != nil {
		t.Fatalf("CreateIssueLink: %v", err)
	}

	got, err := env.store.ProjectIDForIssue(env.ctx, iss.ID)
	mustLookupProject(t, err)
	if got != env.projectID {
		t.Fatalf("issue project = %s, want %s", got, env.projectID)
	}
	got, err = env.store.ProjectIDForComment(env.ctx, comment.ID)
	mustLookupProject(t, err)
	if got != env.projectID {
		t.Fatalf("comment project = %s, want %s", got, env.projectID)
	}
	got, err = env.store.ProjectIDForSprint(env.ctx, sp.ID)
	mustLookupProject(t, err)
	if got != env.projectID {
		t.Fatalf("sprint project = %s, want %s", got, env.projectID)
	}
	got, err = env.store.ProjectIDForIssueLink(env.ctx, link.ID)
	mustLookupProject(t, err)
	if got != env.projectID {
		t.Fatalf("link project = %s, want %s", got, env.projectID)
	}
}

func mustLookupProject(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("lookup project: %v", err)
	}
}
