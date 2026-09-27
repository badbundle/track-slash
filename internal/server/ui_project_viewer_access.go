package server

import (
	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
	"github.com/google/uuid"
)

// uiProjectViewerAccess is the About page's "Your access" card: the viewer's
// role in the project and what that role lets them do. It is built from the
// same store.ProjectPermissions the handlers enforce, so the card can never
// promise an action the server would refuse, or hide one it would allow.
type uiProjectViewerAccess struct {
	Role        uiAccessBadge
	RoleSummary string
	Permissions []uiProjectPermission
	// SignInNext, when set, renders the hint as a "Sign in" link that returns
	// to this path, followed by Hint.
	SignInNext string
	Hint       string
}

type uiProjectPermission struct {
	Key     string
	Label   string
	Allowed bool
}

func uiProjectViewerAccessFor(user model.User, project model.Project, permissions store.ProjectPermissions) uiProjectViewerAccess {
	access := uiProjectViewerAccess{
		Permissions: []uiProjectPermission{
			{Key: "view", Label: "View the project and its issues", Allowed: permissions.CanRead},
			{Key: "create-issues", Label: "Create issues", Allowed: permissions.CanCreateIssues},
			{Key: "edit", Label: "Edit issues and project details", Allowed: permissions.CanWrite},
			{Key: "manage", Label: "Manage access and settings", Allowed: permissions.CanManageMembers},
			{Key: "delete", Label: "Delete the project", Allowed: permissions.CanDelete},
		},
	}
	owner := "@" + project.OwnerUsername
	switch {
	case user.ID == uuid.Nil:
		access.Role = uiAccessBadge{Attr: `data-project-role="signed-out"`, Icon: "user-x", Label: "Signed out"}
		access.RoleSummary = "You are viewing this public project without signing in."
		if permissions.IsPublic && permissions.PublicIssueCreation {
			access.SignInNext = uiProjectIssueNewPath(project)
			access.Hint = " to create issues."
		} else {
			access.SignInNext = uiProjectViewPath(project, "about")
			access.Hint = " if you are a member of this project."
		}
	case permissions.IsOwner:
		access.Role = uiAccessBadge{Attr: `data-project-role="owner"`, Icon: "crown", Label: "Owner", Tinted: true}
		access.RoleSummary = "You own this project."
	case user.IsAdmin:
		access.Role = uiAccessBadge{Attr: `data-project-role="admin"`, Icon: "shield-check", Label: "Site admin", Tinted: true}
		access.RoleSummary = "Site admins have full access to every project."
	case permissions.Role == model.ProjectMemberRoleMember:
		access.Role = uiAccessBadge{Attr: `data-project-role="member"`, Icon: "user-round-check", Label: "Member", Tinted: true}
		access.RoleSummary = "You can view and change this project."
		access.Hint = "Only " + owner + " can manage access or delete it."
	case permissions.Role == model.ProjectMemberRoleReadonly:
		access.Role = uiAccessBadge{Attr: `data-project-role="readonly"`, Icon: "eye", Label: "Read-only"}
		access.RoleSummary = "You can view this project but not change it."
		access.Hint = "Ask " + owner + " for member access to make changes."
	default:
		access.Role = uiAccessBadge{Attr: `data-project-role="none"`, Icon: "user", Label: "Not a member"}
		if permissions.CanCreateIssues {
			access.RoleSummary = "This project is public, so you can view it and open issues."
		} else {
			access.RoleSummary = "This project is public, so you can view it."
		}
		access.Hint = "Ask " + owner + " to add you as a member to make changes."
	}
	return access
}
