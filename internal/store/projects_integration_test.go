package store_test

import (
	"errors"
	"testing"

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
