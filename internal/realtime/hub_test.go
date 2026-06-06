package realtime

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func newTestClient(buffer int) *Client {
	return &Client{send: make(chan Event, buffer)}
}

func recv(t *testing.T, c *Client, timeout time.Duration) (Event, bool) {
	t.Helper()
	select {
	case ev := <-c.send:
		return ev, true
	case <-time.After(timeout):
		return Event{}, false
	}
}

func TestHubFanoutToSubscribers(t *testing.T) {
	hub := NewHub()
	projectID := uuid.New()
	issueID := uuid.New()

	projSub := newTestClient(4)
	issueSub := newTestClient(4)
	other := newTestClient(4)

	hub.Subscribe(projSub, ProjectTopic(projectID))
	hub.Subscribe(issueSub, IssueTopic(issueID))
	hub.Subscribe(other, ProjectTopic(uuid.New()))

	ev := Event{
		Op:        OpUpdate,
		Entity:    EntityIssue,
		ID:        issueID,
		ProjectID: &projectID,
		Version:   2,
	}
	hub.Publish(ev)

	if _, ok := recv(t, projSub, time.Second); !ok {
		t.Fatal("project subscriber did not receive event")
	}
	if _, ok := recv(t, issueSub, time.Second); !ok {
		t.Fatal("issue subscriber did not receive event")
	}
	if _, ok := recv(t, other, 100*time.Millisecond); ok {
		t.Fatal("unrelated subscriber received event")
	}
}

func TestSubIssueEventFansOutToChildParentAndProjectTopics(t *testing.T) {
	hub := NewHub()
	projectID := uuid.New()
	parentID := uuid.New()
	childID := uuid.New()

	childSub := newTestClient(4)
	parentSub := newTestClient(4)
	projectSub := newTestClient(4)
	unrelated := newTestClient(4)

	hub.Subscribe(childSub, IssueTopic(childID))
	hub.Subscribe(parentSub, IssueTopic(parentID))
	hub.Subscribe(projectSub, ProjectTopic(projectID))
	hub.Subscribe(unrelated, IssueTopic(uuid.New()))

	hub.Publish(Event{
		Op:            OpInsert,
		Entity:        EntityIssue,
		ID:            childID,
		ParentIssueID: &parentID,
		ProjectID:     &projectID,
		Version:       1,
	})

	if _, ok := recv(t, childSub, time.Second); !ok {
		t.Fatal("child issue subscriber did not receive event")
	}
	if _, ok := recv(t, parentSub, time.Second); !ok {
		t.Fatal("parent issue subscriber did not receive child event")
	}
	if _, ok := recv(t, projectSub, time.Second); !ok {
		t.Fatal("project subscriber did not receive child event")
	}
	if _, ok := recv(t, unrelated, 100*time.Millisecond); ok {
		t.Fatal("unrelated issue subscriber received event")
	}
}

func TestSprintEventFansOutToProjectAndSprintTopics(t *testing.T) {
	hub := NewHub()
	projectID := uuid.New()
	sprintID := uuid.New()

	projSub := newTestClient(4)
	sprintSub := newTestClient(4)
	unrelated := newTestClient(4)

	hub.Subscribe(projSub, ProjectTopic(projectID))
	hub.Subscribe(sprintSub, SprintTopic(sprintID))
	hub.Subscribe(unrelated, SprintTopic(uuid.New()))

	hub.Publish(Event{
		Op:        OpUpdate,
		Entity:    EntitySprint,
		ID:        sprintID,
		ProjectID: &projectID,
		Version:   3,
	})

	if _, ok := recv(t, projSub, time.Second); !ok {
		t.Fatal("project subscriber did not receive sprint event")
	}
	if _, ok := recv(t, sprintSub, time.Second); !ok {
		t.Fatal("sprint subscriber did not receive event")
	}
	if _, ok := recv(t, unrelated, 100*time.Millisecond); ok {
		t.Fatal("unrelated sprint subscriber received event")
	}
}

func TestCommentEventFansOutToCommentIssueAndProjectTopics(t *testing.T) {
	hub := NewHub()
	projectID := uuid.New()
	issueID := uuid.New()
	commentID := uuid.New()

	commentSub := newTestClient(4)
	issueSub := newTestClient(4)
	projSub := newTestClient(4)
	unrelated := newTestClient(4)

	hub.Subscribe(commentSub, CommentTopic(commentID))
	hub.Subscribe(issueSub, IssueTopic(issueID))
	hub.Subscribe(projSub, ProjectTopic(projectID))
	hub.Subscribe(unrelated, CommentTopic(uuid.New()))

	hub.Publish(Event{
		Op:        OpInsert,
		Entity:    EntityComment,
		ID:        commentID,
		IssueID:   &issueID,
		ProjectID: &projectID,
		Version:   1,
	})

	if _, ok := recv(t, commentSub, time.Second); !ok {
		t.Fatal("comment subscriber did not receive event")
	}
	if _, ok := recv(t, issueSub, time.Second); !ok {
		t.Fatal("issue subscriber did not receive comment event")
	}
	if _, ok := recv(t, projSub, time.Second); !ok {
		t.Fatal("project subscriber did not receive comment event")
	}
	if _, ok := recv(t, unrelated, 100*time.Millisecond); ok {
		t.Fatal("unrelated comment subscriber received event")
	}
}

func TestHubDeduplicatesAcrossTopics(t *testing.T) {
	hub := NewHub()
	projectID := uuid.New()
	issueID := uuid.New()

	c := newTestClient(4)
	hub.Subscribe(c, ProjectTopic(projectID))
	hub.Subscribe(c, IssueTopic(issueID))

	hub.Publish(Event{
		Op:        OpUpdate,
		Entity:    EntityIssue,
		ID:        issueID,
		ProjectID: &projectID,
		Version:   1,
	})

	if _, ok := recv(t, c, time.Second); !ok {
		t.Fatal("expected one event")
	}
	if _, ok := recv(t, c, 100*time.Millisecond); ok {
		t.Fatal("expected no second (deduplicated) event")
	}
}

func TestHubDropsForSlowConsumer(t *testing.T) {
	hub := NewHub()
	projectID := uuid.New()

	slow := newTestClient(1)
	hub.Subscribe(slow, ProjectTopic(projectID))

	for i := 0; i < 5; i++ {
		hub.Publish(Event{
			Op:        OpInsert,
			Entity:    EntityIssue,
			ID:        uuid.New(),
			ProjectID: &projectID,
			Version:   1,
		})
	}

	if got := hub.Dropped(); got != 4 {
		t.Fatalf("dropped = %d, want 4", got)
	}
}

func TestHubUnsubscribe(t *testing.T) {
	hub := NewHub()
	projectID := uuid.New()
	c := newTestClient(4)

	topic := ProjectTopic(projectID)
	hub.Subscribe(c, topic)
	hub.Unsubscribe(c, topic)

	hub.Publish(Event{
		Op:      OpUpdate,
		Entity:  EntityProject,
		ID:      projectID,
		Version: 1,
	})

	if _, ok := recv(t, c, 100*time.Millisecond); ok {
		t.Fatal("client received event after unsubscribe")
	}
	if hub.TopicCount() != 0 {
		t.Fatalf("topic count = %d, want 0 (empty topic should be reaped)", hub.TopicCount())
	}
}

func TestHubRemoveDropsAllSubscriptions(t *testing.T) {
	hub := NewHub()
	c := newTestClient(4)
	hub.Subscribe(c, ProjectTopic(uuid.New()))
	hub.Subscribe(c, IssueTopic(uuid.New()))

	if hub.TopicCount() != 2 {
		t.Fatalf("setup: topic count = %d, want 2", hub.TopicCount())
	}

	hub.Remove(c)
	if hub.TopicCount() != 0 {
		t.Fatalf("after Remove: topic count = %d, want 0", hub.TopicCount())
	}
}

func TestTopicsUnknownEntity(t *testing.T) {
	got := Event{Entity: Entity("user"), ID: uuid.New()}.Topics()
	if got != nil {
		t.Fatalf("Topics for unknown entity = %v, want nil", got)
	}
}

func TestParseTopic(t *testing.T) {
	id := uuid.New()
	cases := []struct {
		topic    string
		wantKind string
		wantErr  bool
	}{
		{"issue:" + id.String(), "issue", false},
		{"project:" + id.String(), "project", false},
		{"sprint:" + id.String(), "sprint", false},
		{"comment:" + id.String(), "comment", false},
		{"user:" + id.String(), "", true},
		{"issue:not-a-uuid", "", true},
		{"sprint:not-a-uuid", "", true},
		{"comment:not-a-uuid", "", true},
		{"", "", true},
	}
	for _, tc := range cases {
		kind, _, err := ParseTopic(tc.topic)
		gotErr := err != nil
		if gotErr != tc.wantErr {
			t.Errorf("ParseTopic(%q) err=%v wantErr=%v", tc.topic, err, tc.wantErr)
		}
		if !tc.wantErr && kind != tc.wantKind {
			t.Errorf("ParseTopic(%q) kind=%q want %q", tc.topic, kind, tc.wantKind)
		}
	}
}
