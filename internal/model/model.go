package model

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusTodo       Status = "todo"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
)

func (s Status) Valid() bool {
	switch s {
	case StatusTodo, StatusInProgress, StatusDone:
		return true
	}
	return false
}

type IssuePriority string

const (
	PriorityP0 IssuePriority = "P0"
	PriorityP1 IssuePriority = "P1"
	PriorityP2 IssuePriority = "P2"
	PriorityP3 IssuePriority = "P3"
	PriorityP4 IssuePriority = "P4"
)

func (p IssuePriority) Valid() bool {
	switch p {
	case PriorityP0, PriorityP1, PriorityP2, PriorityP3, PriorityP4:
		return true
	}
	return false
}

type SprintStatus string

const (
	SprintStatusPlanned   SprintStatus = "planned"
	SprintStatusActive    SprintStatus = "active"
	SprintStatusCompleted SprintStatus = "completed"
)

func (s SprintStatus) Valid() bool {
	switch s {
	case SprintStatusPlanned, SprintStatusActive, SprintStatusCompleted:
		return true
	}
	return false
}

type User struct {
	ID        uuid.UUID `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email,omitempty"`
	Name      string    `json:"name"`
	IsAdmin   bool      `json:"is_admin"`
	CreatedAt time.Time `json:"created_at"`
}

type AuthCredentialKind string

const (
	AuthCredentialKindPassword AuthCredentialKind = "password"
	AuthCredentialKindPasskey  AuthCredentialKind = "passkey"
)

func (k AuthCredentialKind) Valid() bool {
	switch k {
	case AuthCredentialKindPassword, AuthCredentialKindPasskey:
		return true
	}
	return false
}

type AuthTokenKind string

const (
	AuthTokenKindAPI     AuthTokenKind = "api"
	AuthTokenKindSession AuthTokenKind = "session"
)

func (k AuthTokenKind) Valid() bool {
	switch k {
	case AuthTokenKindAPI, AuthTokenKindSession:
		return true
	}
	return false
}

type AuthToken struct {
	ID         uuid.UUID     `json:"id"`
	UserID     uuid.UUID     `json:"user_id"`
	Kind       AuthTokenKind `json:"kind"`
	Name       string        `json:"name"`
	CreatedAt  time.Time     `json:"created_at"`
	LastUsedAt *time.Time    `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time    `json:"expires_at,omitempty"`
	RevokedAt  *time.Time    `json:"revoked_at,omitempty"`
}

type ProjectMember struct {
	ProjectID uuid.UUID `json:"project_id"`
	UserID    uuid.UUID `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}

type ProjectAssignee struct {
	ID       uuid.UUID `json:"id"`
	Username string    `json:"username"`
	Name     string    `json:"name"`
}

type Project struct {
	ID            uuid.UUID `json:"id"`
	OwnerID       uuid.UUID `json:"owner_id"`
	OwnerUsername string    `json:"owner_username"`
	Key           string    `json:"key"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Issue struct {
	ID            uuid.UUID     `json:"id"`
	ProjectID     uuid.UUID     `json:"project_id"`
	OwnerUsername string        `json:"owner_username"`
	ProjectKey    string        `json:"project_key"`
	Number        int           `json:"number"`
	Identifier    string        `json:"identifier"`
	Title         string        `json:"title"`
	Description   string        `json:"description"`
	Status        Status        `json:"status"`
	Priority      IssuePriority `json:"priority"`
	AssigneeID    *uuid.UUID    `json:"assignee_id,omitempty"`
	ReporterID    *uuid.UUID    `json:"reporter_id,omitempty"`
	SprintID      *uuid.UUID    `json:"sprint_id,omitempty"`
	ParentIssueID *uuid.UUID    `json:"parent_issue_id,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

type LinkType string

const (
	LinkTypeBlocks     LinkType = "blocks"
	LinkTypeDuplicates LinkType = "duplicates"
	LinkTypeRelatesTo  LinkType = "relates_to"
	LinkTypeClones     LinkType = "clones"
)

func (t LinkType) Valid() bool {
	switch t {
	case LinkTypeBlocks, LinkTypeDuplicates, LinkTypeRelatesTo, LinkTypeClones:
		return true
	}
	return false
}

type IssueLink struct {
	ID        uuid.UUID `json:"id"`
	ProjectID uuid.UUID `json:"project_id"`
	Number    int       `json:"number"`
	Ref       string    `json:"ref"`
	SourceID  uuid.UUID `json:"source_id"`
	TargetID  uuid.UUID `json:"target_id"`
	LinkType  LinkType  `json:"link_type"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func SprintRef(number int) string {
	return fmt.Sprintf("sprint-%d", number)
}

func IssueLinkRef(number int) string {
	return fmt.Sprintf("link-%d", number)
}

func CommentRef(number int) string {
	return fmt.Sprintf("comment-%d", number)
}

type Comment struct {
	ID        uuid.UUID `json:"id"`
	IssueID   uuid.UUID `json:"issue_id"`
	Number    int       `json:"number"`
	Ref       string    `json:"ref"`
	AuthorID  uuid.UUID `json:"author_id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Sprint struct {
	ID           uuid.UUID    `json:"id"`
	ProjectID    uuid.UUID    `json:"project_id"`
	Number       int          `json:"number"`
	Ref          string       `json:"ref"`
	Name         string       `json:"name"`
	Goal         string       `json:"goal"`
	Status       SprintStatus `json:"status"`
	PlannedOrder *int64       `json:"planned_order,omitempty"`
	StartDate    time.Time    `json:"start_date"`
	EndDate      time.Time    `json:"end_date"`
	CompletedAt  *time.Time   `json:"completed_at,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}
