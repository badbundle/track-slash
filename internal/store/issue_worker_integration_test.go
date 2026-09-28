package store_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func TestIssueWorker(t *testing.T) {
	t.Parallel()
	env := newSprintsEnv(t)
	agent := model.WorkerAgent
	human := model.WorkerHuman

	// A new issue says nothing about who works on it unless asked to.
	unmarked := mustCreateIssue(t, env, "unmarked")
	if unmarked.Worker != nil {
		t.Fatalf("default worker = %v, want nil", *unmarked.Worker)
	}
	forAgent, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{ProjectID: env.projectID, Title: "for an agent", Worker: &agent})
	if err != nil || forAgent.Worker == nil || *forAgent.Worker != model.WorkerAgent {
		t.Fatalf("CreateIssue agent = %+v, %v", forAgent.Worker, err)
	}
	sub, err := env.store.CreateSubIssue(env.ctx, store.CreateSubIssueParams{ParentIssueID: forAgent.ID, Title: "sub for a human", Worker: &human})
	if err != nil || sub.Worker == nil || *sub.Worker != model.WorkerHuman {
		t.Fatalf("CreateSubIssue human = %+v, %v", sub.Worker, err)
	}
	subs, _, err := env.store.ListSubIssuesForIssue(env.ctx, store.ListSubIssuesForIssueParams{ParentIssueID: forAgent.ID, Limit: 10})
	if err != nil || len(subs) != 1 || subs[0].Worker == nil || *subs[0].Worker != model.WorkerHuman {
		t.Fatalf("ListSubIssuesForIssue = %+v, %v", subs, err)
	}

	// An agent hands its issue to a person, then the mark is cleared.
	toHuman := mustCreateIssue(t, env, "handed to a human")
	updated, err := env.store.UpdateIssue(env.ctx, toHuman.ID, store.UpdateIssueParams{Worker: &agent})
	if err != nil || updated.Worker == nil || *updated.Worker != model.WorkerAgent {
		t.Fatalf("UpdateIssue agent = %+v, %v", updated.Worker, err)
	}
	updated, err = env.store.UpdateIssue(env.ctx, toHuman.ID, store.UpdateIssueParams{Worker: &human})
	if err != nil || updated.Worker == nil || *updated.Worker != model.WorkerHuman {
		t.Fatalf("UpdateIssue human = %+v, %v", updated.Worker, err)
	}
	got, err := env.store.GetIssue(env.ctx, toHuman.ID)
	if err != nil || got.Worker == nil || *got.Worker != model.WorkerHuman {
		t.Fatalf("GetIssue = %+v, %v", got.Worker, err)
	}
	cleared, err := env.store.UpdateIssue(env.ctx, forAgent.ID, store.UpdateIssueParams{ClearWorker: true, Worker: &human})
	if err != nil || cleared.Worker != nil {
		t.Fatalf("UpdateIssue clear = %+v, %v", cleared.Worker, err)
	}
	stillHuman := mustCreateIssue(t, env, "stays human")
	if _, err := env.store.UpdateIssue(env.ctx, stillHuman.ID, store.UpdateIssueParams{Worker: &human}); err != nil {
		t.Fatalf("UpdateIssue stays human: %v", err)
	}
	forAgent2, err := env.store.CreateIssue(env.ctx, store.CreateIssueParams{ProjectID: env.projectID, Title: "second agent issue", Worker: &agent})
	if err != nil {
		t.Fatalf("CreateIssue second agent: %v", err)
	}

	// Each change is in the changelog, labelled as people read it.
	entries, _, err := env.store.ListProjectChangelog(env.ctx, store.ListProjectChangelogParams{ProjectID: env.projectID, Limit: 100})
	if err != nil {
		t.Fatalf("ListProjectChangelog: %v", err)
	}
	var workerChanges []model.ProjectChangelogChange
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].TargetRef != toHuman.Identifier && entries[i].TargetRef != forAgent.Identifier {
			continue
		}
		for _, change := range entries[i].Details.Changes {
			if change.Field == "worker" {
				workerChanges = append(workerChanges, change)
			}
		}
	}
	want := []model.ProjectChangelogChange{
		{Field: "worker", Label: "Worker", From: "None", To: "Agent"},
		{Field: "worker", Label: "Worker", From: "Agent", To: "Human"},
		{Field: "worker", Label: "Worker", From: "Agent", To: "None"},
	}
	if len(workerChanges) != len(want) {
		t.Fatalf("worker changes = %+v, want %+v", workerChanges, want)
	}
	for i := range want {
		if workerChanges[i] != want[i] {
			t.Fatalf("worker change %d = %+v, want %+v", i, workerChanges[i], want[i])
		}
	}

	// Lists filter by worker, with none for the issues nobody has marked.
	for name, tc := range map[string]struct {
		workers []model.IssueWorker
		unset   bool
		want    []uuid.UUID
	}{
		"all":            {want: []uuid.UUID{unmarked.ID, forAgent.ID, toHuman.ID, stillHuman.ID, forAgent2.ID}},
		"agent":          {workers: []model.IssueWorker{agent}, want: []uuid.UUID{forAgent2.ID}},
		"human":          {workers: []model.IssueWorker{human, human}, want: []uuid.UUID{toHuman.ID, stillHuman.ID}},
		"agent or human": {workers: []model.IssueWorker{agent, human}, want: []uuid.UUID{toHuman.ID, stillHuman.ID, forAgent2.ID}},
		"none":           {unset: true, want: []uuid.UUID{unmarked.ID, forAgent.ID}},
		"agent or none":  {workers: []model.IssueWorker{agent}, unset: true, want: []uuid.UUID{unmarked.ID, forAgent.ID, forAgent2.ID}},
	} {
		issues, _, err := env.store.ListIssues(env.ctx, store.ListIssuesParams{
			ProjectID:   env.projectID,
			Workers:     tc.workers,
			UnsetWorker: tc.unset,
			Limit:       10,
		})
		if err != nil {
			t.Fatalf("%s: ListIssues: %v", name, err)
		}
		if got := issueIDs(issues); !equalIssueIDs(got, tc.want) {
			t.Fatalf("%s: ids = %v, want %v", name, got, tc.want)
		}
	}
}
