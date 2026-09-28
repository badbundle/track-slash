package store_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func TestPublicProjectAccessAndUserBlocks(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	owner, err := env.store.GetUser(env.ctx, project.OwnerID)
	if err != nil {
		t.Fatalf("GetUser owner: %v", err)
	}
	outsider, err := env.store.CreateUserProfile(env.ctx, "public-outsider-"+uniqueProjectKey(t), "public-outsider@example.com", "Public Outsider")
	if err != nil {
		t.Fatalf("CreateUserProfile outsider: %v", err)
	}
	readonly, err := env.store.CreateUserProfile(env.ctx, "public-readonly-"+uniqueProjectKey(t), "public-readonly@example.com", "Public Readonly")
	if err != nil {
		t.Fatalf("CreateUserProfile readonly: %v", err)
	}
	if _, err := env.store.SetProjectMemberRole(env.ctx, project.ID, readonly.ID, model.ProjectMemberRoleReadonly); err != nil {
		t.Fatalf("SetProjectMemberRole readonly: %v", err)
	}

	settings, err := env.store.GetProjectAccessSettings(env.ctx, project.ID)
	if err != nil || settings.AccessMode != model.ProjectAccessPrivate || settings.IsPublic || settings.PublicIssueCreation {
		t.Fatalf("default access settings = %+v, %v", settings, err)
	}
	for name, user := range map[string]model.User{"anonymous": {}, "outsider": outsider} {
		permissions, err := env.store.ProjectPermissionsForUser(env.ctx, user, project.ID)
		if err != nil || permissions.CanRead || permissions.CanWrite || permissions.CanCreateIssues || permissions.IsBlocked {
			t.Fatalf("%s private permissions = %+v, %v", name, permissions, err)
		}
	}

	settings, err = env.store.UpdateProjectAccessMode(env.ctx, project.ID, model.ProjectAccessPublicIssues)
	if err != nil || settings.AccessMode != model.ProjectAccessPublicIssues || !settings.IsPublic || !settings.PublicIssueCreation {
		t.Fatalf("updated access settings = %+v, %v", settings, err)
	}
	anonymousPermissions, err := env.store.ProjectPermissionsForUser(env.ctx, model.User{}, project.ID)
	if err != nil || !anonymousPermissions.CanRead || anonymousPermissions.CanWrite || anonymousPermissions.CanCreateIssues {
		t.Fatalf("anonymous public permissions = %+v, %v", anonymousPermissions, err)
	}
	outsiderPermissions, err := env.store.ProjectPermissionsForUser(env.ctx, outsider, project.ID)
	if err != nil || !outsiderPermissions.CanRead || outsiderPermissions.CanWrite || !outsiderPermissions.CanCreateIssues {
		t.Fatalf("outsider public permissions = %+v, %v", outsiderPermissions, err)
	}
	if canCreate, err := env.store.UserCanCreateProjectIssue(env.ctx, outsider, project.ID); err != nil || !canCreate {
		t.Fatalf("UserCanCreateProjectIssue outsider = %v, %v", canCreate, err)
	}
	if canCreate, err := env.store.UserCanCreateProjectIssue(env.ctx, model.User{}, project.ID); err != nil || canCreate {
		t.Fatalf("UserCanCreateProjectIssue anonymous = %v, %v", canCreate, err)
	}
	readonlyPermissions, err := env.store.ProjectPermissionsForUser(env.ctx, readonly, project.ID)
	if err != nil || !readonlyPermissions.CanRead || readonlyPermissions.CanWrite || readonlyPermissions.CanCreateIssues {
		t.Fatalf("readonly public permissions = %+v, %v", readonlyPermissions, err)
	}
	if issue, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{
		ProjectID:  project.ID,
		Title:      "Submitted by public user",
		ReporterID: &outsider.ID,
	}); err != nil || issue.ReporterID == nil || *issue.ReporterID != outsider.ID {
		t.Fatalf("CreateIssue public reporter = %+v, %v", issue, err)
	}
	if unchanged, err := env.store.UpdateProjectAccessMode(env.ctx, project.ID, settings.AccessMode); err != nil || unchanged != settings {
		t.Fatalf("idempotent access update = %+v, %v", unchanged, err)
	}
	if public, err := env.store.UpdateProjectAccessMode(env.ctx, project.ID, model.ProjectAccessPublic); err != nil || !public.IsPublic || public.PublicIssueCreation {
		t.Fatalf("public access update = %+v, %v", public, err)
	}
	entries, _, err := env.store.ListProjectChangelog(env.ctx, store.ListProjectChangelogParams{ProjectID: project.ID, Limit: 1})
	if err != nil || len(entries) != 1 || len(entries[0].Details.Changes) != 1 {
		t.Fatalf("access changelog = %+v, %v", entries, err)
	}
	if change := entries[0].Details.Changes[0]; change.Field != "access_mode" || change.From != "Public, open to issues" || change.To != "Public" {
		t.Fatalf("access changelog change = %+v", change)
	}
	if settings, err = env.store.UpdateProjectAccessMode(env.ctx, project.ID, model.ProjectAccessPublicIssues); err != nil {
		t.Fatalf("restore public issue creation: %v", err)
	}

	// Public projects are link-only: outsiders can open and file into one, but
	// lists name it only to its owner and members.
	for name, tc := range map[string]struct {
		params store.ListProjectsParams
		want   bool
	}{
		"anonymous member list": {params: store.ListProjectsParams{MemberUser: uuidPtr(uuid.Nil), Limit: 100}},
		"outsider member list":  {params: store.ListProjectsParams{MemberUser: &outsider.ID, Limit: 100}},
		"outsider writable":     {params: store.ListProjectsParams{WritableToUser: &outsider.ID, Limit: 100}},
		"readonly member list":  {params: store.ListProjectsParams{MemberUser: &readonly.ID, Limit: 100}, want: true},
		"readonly writable":     {params: store.ListProjectsParams{WritableToUser: &readonly.ID, Limit: 100}},
		"owner member list":     {params: store.ListProjectsParams{MemberUser: &owner.ID, Limit: 100}, want: true},
		"owner writable":        {params: store.ListProjectsParams{WritableToUser: &owner.ID, Limit: 100}, want: true},
	} {
		projects, _, err := env.store.ListProjects(env.ctx, tc.params)
		if err != nil {
			t.Fatalf("ListProjects %s: %v", name, err)
		}
		if seen := projectInList(projects, project.ID); seen != tc.want {
			t.Fatalf("ListProjects %s included public project = %v, want %v: %+v", name, seen, tc.want, projects)
		}
	}

	if _, err := env.store.SetProjectMemberRole(env.ctx, project.ID, outsider.ID, model.ProjectMemberRoleMember); err != nil {
		t.Fatalf("SetProjectMemberRole outsider: %v", err)
	}
	block, err := env.store.BlockProjectUser(env.ctx, project.ID, outsider.ID, owner.ID)
	if err != nil || block.ProjectID != project.ID || block.UserID != outsider.ID || block.CreatedByID != owner.ID {
		t.Fatalf("BlockProjectUser = %+v, %v", block, err)
	}
	if repeated, err := env.store.BlockProjectUser(env.ctx, project.ID, outsider.ID, owner.ID); err != nil || repeated.ID != block.ID {
		t.Fatalf("idempotent BlockProjectUser = %+v, %v", repeated, err)
	}
	blockedPermissions, err := env.store.ProjectPermissionsForUser(env.ctx, outsider, project.ID)
	if err != nil || !blockedPermissions.IsBlocked || blockedPermissions.CanRead || blockedPermissions.CanWrite || blockedPermissions.CanCreateIssues {
		t.Fatalf("blocked permissions = %+v, %v", blockedPermissions, err)
	}
	if canCreate, err := env.store.UserCanCreateProjectIssue(env.ctx, outsider, project.ID); err != nil || canCreate {
		t.Fatalf("UserCanCreateProjectIssue blocked = %v, %v", canCreate, err)
	}
	for name, params := range map[string]store.ListProjectsParams{
		"member list": {MemberUser: &outsider.ID, Limit: 100},
		"writable":    {WritableToUser: &outsider.ID, Limit: 100},
	} {
		if projects, _, err := env.store.ListProjects(env.ctx, params); err != nil || projectInList(projects, project.ID) {
			t.Fatalf("blocked member %s includes project: %+v, %v", name, projects, err)
		}
	}
	blocks, err := env.store.ListProjectUserBlocks(env.ctx, project.ID)
	if err != nil || len(blocks) != 1 || blocks[0].ID != block.ID {
		t.Fatalf("ListProjectUserBlocks = %+v, %v", blocks, err)
	}
	candidates, err := env.store.SearchAvailableProjectMembers(env.ctx, store.SearchAvailableProjectMembersParams{
		ProjectID: project.ID,
		Query:     outsider.Username,
		Limit:     10,
	})
	if err != nil || len(candidates) != 0 {
		t.Fatalf("blocked member candidates = %+v, %v", candidates, err)
	}
	if _, err := env.store.SetProjectMemberRole(env.ctx, project.ID, outsider.ID, model.ProjectMemberRoleMember); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("add blocked user err = %v, want ErrConflict", err)
	}
	if _, err := env.store.BlockProjectUser(env.ctx, project.ID, owner.ID, owner.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("block owner err = %v, want ErrConflict", err)
	}

	if err := env.store.UnblockProjectUser(env.ctx, project.ID, outsider.ID); err != nil {
		t.Fatalf("UnblockProjectUser: %v", err)
	}
	if err := env.store.UnblockProjectUser(env.ctx, project.ID, outsider.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second UnblockProjectUser err = %v, want ErrNotFound", err)
	}
	permissions, err := env.store.ProjectPermissionsForUser(env.ctx, outsider, project.ID)
	if err != nil || permissions.IsBlocked || !permissions.CanRead || !permissions.CanCreateIssues {
		t.Fatalf("unblocked permissions = %+v, %v", permissions, err)
	}

	for _, mode := range []model.ProjectAccessMode{"", "members", "everyone"} {
		if _, err := env.store.UpdateProjectAccessMode(env.ctx, project.ID, mode); !errors.Is(err, store.ErrInvalidProjectAccessMode) {
			t.Fatalf("UpdateProjectAccessMode(%q) err = %v, want ErrInvalidProjectAccessMode", mode, err)
		}
	}
	settings, err = env.store.UpdateProjectAccessMode(env.ctx, project.ID, model.ProjectAccessPrivate)
	if err != nil || settings.AccessMode != model.ProjectAccessPrivate || settings.IsPublic || settings.PublicIssueCreation {
		t.Fatalf("private access settings = %+v, %v", settings, err)
	}
	if _, err := env.store.GetProjectAccessSettings(env.ctx, uuid.New()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing access settings err = %v, want ErrNotFound", err)
	}
	if _, err := env.store.UpdateProjectAccessMode(env.ctx, uuid.New(), model.ProjectAccessPublic); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing access update err = %v, want ErrNotFound", err)
	}
	if _, err := env.store.BlockProjectUser(env.ctx, uuid.New(), outsider.ID, owner.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("block missing project err = %v, want ErrNotFound", err)
	}
	if _, err := env.store.BlockProjectUser(env.ctx, project.ID, uuid.New(), owner.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("block missing user err = %v, want ErrNotFound", err)
	}
	if _, err := env.store.BlockProjectUser(env.ctx, project.ID, outsider.ID, uuid.New()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("block missing creator err = %v, want ErrNotFound", err)
	}
}

func uuidPtr(id uuid.UUID) *uuid.UUID { return &id }

func projectInList(projects []model.Project, id uuid.UUID) bool {
	for _, project := range projects {
		if project.ID == id {
			return true
		}
	}
	return false
}
