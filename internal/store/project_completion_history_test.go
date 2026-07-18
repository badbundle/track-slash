package store

import (
	"testing"
	"time"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/google/uuid"
)

func TestCompletionHistoryWeekStart(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		in   time.Time
		want time.Time
	}{
		{name: "Monday", in: time.Date(2026, 4, 20, 18, 30, 0, 0, time.UTC), want: time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)},
		{name: "Sunday", in: time.Date(2026, 4, 26, 23, 59, 0, 0, time.UTC), want: time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := completionHistoryWeekStart(tt.in); !got.Equal(tt.want) {
				t.Fatalf("completionHistoryWeekStart(%s) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

func TestCompletionHistoryPreviousStatus(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		raw     string
		want    model.Status
		wantOK  bool
		wantErr bool
	}{
		{name: "todo", raw: `{"changes":[{"field":"status","from":"To do","to":"Done"}]}`, want: model.StatusTodo, wantOK: true},
		{name: "in progress", raw: `{"changes":[{"field":"status","from":"In progress","to":"Done"}]}`, want: model.StatusInProgress, wantOK: true},
		{name: "done", raw: `{"changes":[{"field":"status","from":"Done","to":"To do"}]}`, want: model.StatusDone, wantOK: true},
		{name: "closed", raw: `{"changes":[{"field":"status","from":"Closed","to":"To do"}]}`, want: model.StatusClosed, wantOK: true},
		{name: "unrelated change", raw: `{"changes":[{"field":"title","from":"Old","to":"New"}]}`},
		{name: "unknown status", raw: `{"changes":[{"field":"status","from":"Blocked","to":"Done"}]}`, wantErr: true},
		{name: "invalid JSON", raw: `{`, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, err := completionHistoryPreviousStatus([]byte(tt.raw))
			if (err != nil) != tt.wantErr || got != tt.want || ok != tt.wantOK {
				t.Fatalf("completionHistoryPreviousStatus() = %q, %v, %v; want %q, %v, err=%v", got, ok, err, tt.want, tt.wantOK, tt.wantErr)
			}
		})
	}
}

func TestReverseCompletionHistoryEvent(t *testing.T) {
	t.Parallel()
	parentID := uuid.New()
	targetID := uuid.New()
	childID := uuid.New()
	missingID := uuid.New()
	newStates := func() map[uuid.UUID]*completionHistoryIssue {
		parent := parentID
		return map[uuid.UUID]*completionHistoryIssue{
			parentID: {Status: model.StatusTodo, Active: true},
			targetID: {ParentID: &parent, Status: model.StatusTodo, Active: true},
			childID:  {Status: model.StatusTodo, Active: true},
		}
	}
	children := map[uuid.UUID][]uuid.UUID{targetID: {childID}}

	states := newStates()
	err := reverseCompletionHistoryEvent(states, children, completionHistoryEvent{
		IssueID: targetID,
		Op:      "update",
		Details: []byte(`{"changes":[{"field":"status","from":"Done","to":"To do"}]}`),
	})
	if err != nil || states[targetID].Status != model.StatusDone {
		t.Fatalf("reverse update status = %s err = %v", states[targetID].Status, err)
	}

	states = newStates()
	states[targetID].Active = false
	states[childID].Active = false
	if err := reverseCompletionHistoryEvent(states, children, completionHistoryEvent{IssueID: targetID, Op: "delete", Details: []byte(`{}`)}); err != nil {
		t.Fatalf("reverse delete: %v", err)
	}
	if !states[targetID].Active || !states[childID].Active || !states[parentID].Active {
		t.Fatalf("reverse delete states = %+v", states)
	}

	states = newStates()
	if err := reverseCompletionHistoryEvent(states, children, completionHistoryEvent{IssueID: targetID, Op: "restore", Details: []byte(`{}`)}); err != nil {
		t.Fatalf("reverse restore: %v", err)
	}
	if states[targetID].Active || states[childID].Active || states[parentID].Active {
		t.Fatalf("reverse restore states = %+v", states)
	}

	states = newStates()
	if err := reverseCompletionHistoryEvent(states, children, completionHistoryEvent{IssueID: missingID, Op: "delete"}); err != nil {
		t.Fatalf("missing issue event: %v", err)
	}
	if err := reverseCompletionHistoryEvent(states, children, completionHistoryEvent{IssueID: targetID, Op: "other"}); err != nil {
		t.Fatalf("other event: %v", err)
	}
	if err := reverseCompletionHistoryEvent(states, children, completionHistoryEvent{IssueID: targetID, Op: "update", Details: []byte(`{`)}); err == nil {
		t.Fatal("invalid update details returned nil error")
	}
	setCompletionHistoryActive(states, []uuid.UUID{missingID}, false)
}
