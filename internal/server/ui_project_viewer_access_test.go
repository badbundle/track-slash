package server

import (
	"testing"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/google/uuid"
)

func TestUIProjectViewerAccessForEachViewer(t *testing.T) {
	t.Parallel()

	project := model.Project{OwnerUsername: "bradley", Key: "TRACK"}
	signedIn := model.User{ID: uuid.MustParse("5d0c1f7e-0d37-4a43-9d38-1f0c9b0b5a01")}
	admin := model.User{ID: uuid.MustParse("5d0c1f7e-0d37-4a43-9d38-1f0c9b0b5a02"), IsAdmin: true}
	full := store.ProjectPermissions{CanRead: true, CanWrite: true, CanCreateIssues: true, CanManageMembers: true, CanDelete: true}
	for _, tc := range []struct {
		name        string
		user        model.User
		permissions store.ProjectPermissions
		role        uiAccessBadge
		summary     string
		signInNext  string
		hint        string
	}{
		{
			name:        "owner",
			user:        admin,
			permissions: func() store.ProjectPermissions { p := full; p.IsOwner = true; return p }(),
			role:        uiAccessBadge{Attr: `data-project-role="owner"`, Icon: "crown", Label: "Owner", Tinted: true},
			summary:     "You own this project.",
		},
		{
			name:        "site admin",
			user:        admin,
			permissions: full,
			role:        uiAccessBadge{Attr: `data-project-role="admin"`, Icon: "shield-check", Label: "Site admin", Tinted: true},
			summary:     "Site admins have full access to every project.",
		},
		{
			name:        "member",
			user:        signedIn,
			permissions: store.ProjectPermissions{Role: model.ProjectMemberRoleMember, CanRead: true, CanWrite: true, CanCreateIssues: true},
			role:        uiAccessBadge{Attr: `data-project-role="member"`, Icon: "user-round-check", Label: "Member", Tinted: true},
			summary:     "You can view and change this project.",
			hint:        "Only @bradley can manage access or delete it.",
		},
		{
			name:        "read-only member",
			user:        signedIn,
			permissions: store.ProjectPermissions{Role: model.ProjectMemberRoleReadonly, CanRead: true},
			role:        uiAccessBadge{Attr: `data-project-role="readonly"`, Icon: "eye", Label: "Read-only"},
			summary:     "You can view this project but not change it.",
			hint:        "Ask @bradley for member access to make changes.",
		},
		{
			name:        "non-member reader",
			user:        signedIn,
			permissions: store.ProjectPermissions{IsPublic: true, CanRead: true},
			role:        uiAccessBadge{Attr: `data-project-role="none"`, Icon: "user", Label: "Not a member"},
			summary:     "This project is public, so you can view it.",
			hint:        "Ask @bradley to add you as a member to make changes.",
		},
		{
			name:        "non-member issue creator",
			user:        signedIn,
			permissions: store.ProjectPermissions{IsPublic: true, PublicIssueCreation: true, CanRead: true, CanCreateIssues: true},
			role:        uiAccessBadge{Attr: `data-project-role="none"`, Icon: "user", Label: "Not a member"},
			summary:     "This project is public, so you can view it and open issues.",
			hint:        "Ask @bradley to add you as a member to make changes.",
		},
		{
			name:        "signed out",
			permissions: store.ProjectPermissions{IsPublic: true, CanRead: true},
			role:        uiAccessBadge{Attr: `data-project-role="signed-out"`, Icon: "user-x", Label: "Signed out"},
			summary:     "You are viewing this public project without signing in.",
			signInNext:  "/bradley/projects/TRACK/about",
			hint:        " if you are a member of this project.",
		},
		{
			name:        "signed out with public issue creation",
			permissions: store.ProjectPermissions{IsPublic: true, PublicIssueCreation: true, CanRead: true},
			role:        uiAccessBadge{Attr: `data-project-role="signed-out"`, Icon: "user-x", Label: "Signed out"},
			summary:     "You are viewing this public project without signing in.",
			signInNext:  "/bradley/projects/TRACK/issues/new",
			hint:        " to create issues.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := uiProjectViewerAccessFor(tc.user, project, tc.permissions)
			if got.Role != tc.role || got.RoleSummary != tc.summary || got.SignInNext != tc.signInNext || got.Hint != tc.hint {
				t.Fatalf("viewer access = %+v, want role %+v summary %q sign-in %q hint %q", got, tc.role, tc.summary, tc.signInNext, tc.hint)
			}
			// Every row mirrors one enforced permission, in a fixed order.
			want := []uiProjectPermission{
				{Key: "view", Label: "View the project and its issues", Allowed: tc.permissions.CanRead},
				{Key: "create-issues", Label: "Create issues", Allowed: tc.permissions.CanCreateIssues},
				{Key: "edit", Label: "Edit issues and project details", Allowed: tc.permissions.CanWrite},
				{Key: "manage", Label: "Manage access and settings", Allowed: tc.permissions.CanManageMembers},
				{Key: "delete", Label: "Delete the project", Allowed: tc.permissions.CanDelete},
			}
			if len(got.Permissions) != len(want) {
				t.Fatalf("permissions = %+v, want %+v", got.Permissions, want)
			}
			for i := range want {
				if got.Permissions[i] != want[i] {
					t.Fatalf("permission %d = %+v, want %+v", i, got.Permissions[i], want[i])
				}
			}
		})
	}
}
