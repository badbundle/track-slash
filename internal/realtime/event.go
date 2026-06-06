package realtime

import (
	"fmt"

	"github.com/google/uuid"
)

// Op is the kind of mutation that produced the event.
type Op string

const (
	OpInsert Op = "insert"
	OpUpdate Op = "update"
	OpDelete Op = "delete"
)

// Entity is the table the event came from.
type Entity string

const (
	EntityIssue     Entity = "issue"
	EntityProject   Entity = "project"
	EntitySprint    Entity = "sprint"
	EntityIssueLink Entity = "issue_link"
	EntityComment   Entity = "comment"
)

// Event is the wire envelope sent over both pg_notify and the WebSocket.
// Kept minimal so it always fits inside Postgres' 8000-byte NOTIFY limit
// and so clients are responsible for refetching the full row via REST.
type Event struct {
	Op            Op         `json:"op"`
	Entity        Entity     `json:"entity"`
	ID            uuid.UUID  `json:"id"`
	IssueID       *uuid.UUID `json:"issue_id,omitempty"`
	ParentIssueID *uuid.UUID `json:"parent_issue_id,omitempty"`
	ProjectID     *uuid.UUID `json:"project_id,omitempty"`
	Version       int64      `json:"version"`
	Ts            string     `json:"ts"`
}

// Topics returns the topic names this event should be fanned out on.
// An issue event publishes to both its own issue topic and its project topic.
func (e Event) Topics() []string {
	switch e.Entity {
	case EntityIssue:
		topics := []string{IssueTopic(e.ID)}
		if e.ParentIssueID != nil {
			topics = append(topics, IssueTopic(*e.ParentIssueID))
		}
		if e.ProjectID != nil {
			topics = append(topics, ProjectTopic(*e.ProjectID))
		}
		return topics
	case EntityProject:
		return []string{ProjectTopic(e.ID)}
	case EntitySprint:
		topics := []string{SprintTopic(e.ID)}
		if e.ProjectID != nil {
			topics = append(topics, ProjectTopic(*e.ProjectID))
		}
		return topics
	case EntityIssueLink:
		topics := []string{IssueLinkTopic(e.ID)}
		if e.ProjectID != nil {
			topics = append(topics, ProjectTopic(*e.ProjectID))
		}
		return topics
	case EntityComment:
		topics := []string{CommentTopic(e.ID)}
		if e.IssueID != nil {
			topics = append(topics, IssueTopic(*e.IssueID))
		}
		if e.ProjectID != nil {
			topics = append(topics, ProjectTopic(*e.ProjectID))
		}
		return topics
	}
	return nil
}

func IssueTopic(id uuid.UUID) string     { return "issue:" + id.String() }
func ProjectTopic(id uuid.UUID) string   { return "project:" + id.String() }
func SprintTopic(id uuid.UUID) string    { return "sprint:" + id.String() }
func IssueLinkTopic(id uuid.UUID) string { return "issue_link:" + id.String() }
func CommentTopic(id uuid.UUID) string   { return "comment:" + id.String() }

// ParseTopic validates a client-supplied topic string and returns its
// prefix and uuid component.
func ParseTopic(t string) (kind string, id uuid.UUID, err error) {
	for _, prefix := range []string{"issue_link:", "comment:", "issue:", "project:", "sprint:"} {
		if len(t) > len(prefix) && t[:len(prefix)] == prefix {
			id, err = uuid.Parse(t[len(prefix):])
			if err != nil {
				return "", uuid.Nil, fmt.Errorf("invalid uuid in topic %q: %w", t, err)
			}
			return prefix[:len(prefix)-1], id, nil
		}
	}
	return "", uuid.Nil, fmt.Errorf("unknown topic format %q (want issue:<uuid>, project:<uuid>, sprint:<uuid>, issue_link:<uuid>, or comment:<uuid>)", t)
}
