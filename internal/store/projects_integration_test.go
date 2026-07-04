package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func TestUpdateProjectUpdatesFieldsAndChangelog(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)

	project, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	name := "Renamed project"
	description := "new project description"
	updated, err := env.store.UpdateProject(env.ctx, project.ID, store.UpdateProjectParams{
		Name:        &name,
		Description: &description,
	})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if updated.Name != name || updated.Description != description || !updated.UpdatedAt.After(project.UpdatedAt) {
		t.Fatalf("updated project = %+v, before = %+v", updated, project)
	}

	entries, _, err := env.store.ListProjectChangelog(env.ctx, store.ListProjectChangelogParams{
		ProjectID: project.ID,
		Limit:     5,
	})
	if err != nil {
		t.Fatalf("ListProjectChangelog: %v", err)
	}
	if len(entries) == 0 || entries[0].Entity != "project" || entries[0].Op != "update" {
		t.Fatalf("missing project update changelog: %+v", entries)
	}
	wantChanges := []model.ProjectChangelogChange{
		{Field: "name", Label: "Name", From: project.Name, To: name},
		{Field: "description", Label: "Description", From: "", To: description},
	}
	if len(entries[0].Details.Changes) != len(wantChanges) {
		t.Fatalf("changes = %+v, want %+v", entries[0].Details.Changes, wantChanges)
	}
	for i, want := range wantChanges {
		if entries[0].Details.Changes[i] != want {
			t.Fatalf("change[%d] = %+v, want %+v", i, entries[0].Details.Changes[i], want)
		}
	}

	badName := " "
	if _, err := env.store.UpdateProject(env.ctx, project.ID, store.UpdateProjectParams{Name: &badName}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("blank name err = %v, want ErrConflict", err)
	}
	got, err := env.store.GetProject(env.ctx, project.ID)
	if err != nil {
		t.Fatalf("GetProject after bad name: %v", err)
	}
	if got.Name != name {
		t.Fatalf("bad name changed project: %+v", got)
	}

	cleared := "   "
	updated, err = env.store.UpdateProject(env.ctx, project.ID, store.UpdateProjectParams{Description: &cleared})
	if err != nil {
		t.Fatalf("UpdateProject clear: %v", err)
	}
	if updated.Description != "" {
		t.Fatalf("cleared description = %q, want empty", updated.Description)
	}

	noop, err := env.store.UpdateProject(env.ctx, project.ID, store.UpdateProjectParams{})
	if err != nil {
		t.Fatalf("UpdateProject noop: %v", err)
	}
	if noop.ID != project.ID {
		t.Fatalf("noop project = %+v", noop)
	}
}

func TestProjectFavoritesArePerUserAndVisibleOnly(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)

	projectA, err := env.store.GetProject(env.ctx, env.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	user, err := env.store.CreateUser(env.ctx, "favorite-"+uniqueProjectKey(t)+"@example.com", "Favorite User")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := env.store.GrantProjectAccess(env.ctx, projectA.ID, user.ID); err != nil {
		t.Fatalf("GrantProjectAccess: %v", err)
	}
	projectB, err := env.store.CreateProjectForUser(env.ctx, user.ID, uniqueProjectKey(t), "Favorite B", "")
	if err != nil {
		t.Fatalf("CreateProjectForUser: %v", err)
	}
	other, err := env.store.CreateUser(env.ctx, "favorite-other-"+uniqueProjectKey(t)+"@example.com", "Other Favorite User")
	if err != nil {
		t.Fatalf("CreateUser other: %v", err)
	}
	if _, err := env.store.GrantProjectAccess(env.ctx, projectA.ID, other.ID); err != nil {
		t.Fatalf("GrantProjectAccess other: %v", err)
	}

	if err := env.store.FavoriteProject(env.ctx, user.ID, projectA.ID); err != nil {
		t.Fatalf("FavoriteProject: %v", err)
	}
	if err := env.store.FavoriteProject(env.ctx, user.ID, projectA.ID); err != nil {
		t.Fatalf("FavoriteProject idempotent: %v", err)
	}
	favorite, err := env.store.IsProjectFavorite(env.ctx, user.ID, projectA.ID)
	if err != nil {
		t.Fatalf("IsProjectFavorite: %v", err)
	}
	if !favorite {
		t.Fatal("project A is not favorite")
	}
	ids, err := env.store.FavoriteProjectIDs(env.ctx, user.ID, []uuid.UUID{projectA.ID, projectB.ID})
	if err != nil {
		t.Fatalf("FavoriteProjectIDs: %v", err)
	}
	if !ids[projectA.ID] || ids[projectB.ID] {
		t.Fatalf("favorite ids = %+v", ids)
	}
	emptyIDs, err := env.store.FavoriteProjectIDs(env.ctx, user.ID, nil)
	if err != nil {
		t.Fatalf("FavoriteProjectIDs empty: %v", err)
	}
	if len(emptyIDs) != 0 {
		t.Fatalf("empty favorite ids = %+v", emptyIDs)
	}
	projectBFavorite, err := env.store.IsProjectFavorite(env.ctx, user.ID, projectB.ID)
	if err != nil {
		t.Fatalf("IsProjectFavorite B: %v", err)
	}
	if projectBFavorite {
		t.Fatal("project B unexpectedly favorite")
	}

	if err := env.store.FavoriteProject(env.ctx, other.ID, projectA.ID); err != nil {
		t.Fatalf("FavoriteProject other: %v", err)
	}
	if err := env.store.UnfavoriteProject(env.ctx, user.ID, projectA.ID); err != nil {
		t.Fatalf("UnfavoriteProject: %v", err)
	}
	otherFavorite, err := env.store.IsProjectFavorite(env.ctx, other.ID, projectA.ID)
	if err != nil {
		t.Fatalf("IsProjectFavorite other: %v", err)
	}
	if !otherFavorite {
		t.Fatal("unfavorite affected another user's favorite")
	}
	if err := env.store.FavoriteProject(env.ctx, user.ID, projectA.ID); err != nil {
		t.Fatalf("FavoriteProject again: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := env.store.FavoriteProject(env.ctx, user.ID, projectB.ID); err != nil {
		t.Fatalf("FavoriteProject B: %v", err)
	}
	favorites, err := env.store.ListFavoriteProjects(env.ctx, store.ListFavoriteProjectsParams{User: user, Limit: 10})
	if err != nil {
		t.Fatalf("ListFavoriteProjects: %v", err)
	}
	if len(favorites) != 2 || favorites[0].ID != projectB.ID || favorites[1].ID != projectA.ID {
		t.Fatalf("favorites = %+v, want B then A", favorites)
	}
	favorites, err = env.store.ListFavoriteProjects(env.ctx, store.ListFavoriteProjectsParams{User: user})
	if err != nil {
		t.Fatalf("ListFavoriteProjects unlimited: %v", err)
	}
	if len(favorites) != 2 || favorites[0].ID != projectB.ID || favorites[1].ID != projectA.ID {
		t.Fatalf("unlimited favorites = %+v, want B then A", favorites)
	}
	if err := env.store.FavoriteProject(env.ctx, user.ID, uuid.New()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("favorite missing project err = %v, want ErrNotFound", err)
	}
	if err := env.store.UnfavoriteProject(env.ctx, user.ID, uuid.New()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unfavorite missing project err = %v, want ErrNotFound", err)
	}
	if err := env.store.FavoriteProject(env.ctx, uuid.New(), projectA.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("favorite missing user err = %v, want ErrNotFound", err)
	}

	if err := env.store.RevokeProjectAccess(env.ctx, projectA.ID, user.ID); err != nil {
		t.Fatalf("RevokeProjectAccess: %v", err)
	}
	favorites, err = env.store.ListFavoriteProjects(env.ctx, store.ListFavoriteProjectsParams{User: user, Limit: 10})
	if err != nil {
		t.Fatalf("ListFavoriteProjects after revoke: %v", err)
	}
	if len(favorites) != 1 || favorites[0].ID != projectB.ID {
		t.Fatalf("favorites after revoke = %+v, want B only", favorites)
	}
	if err := env.store.DeleteProject(env.ctx, projectB.ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	favorites, err = env.store.ListFavoriteProjects(env.ctx, store.ListFavoriteProjectsParams{User: user, Limit: 10})
	if err != nil {
		t.Fatalf("ListFavoriteProjects after delete: %v", err)
	}
	if len(favorites) != 0 {
		t.Fatalf("favorites after delete = %+v, want empty", favorites)
	}
}
