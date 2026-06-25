package model

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const DateLayout = "2006-01-02"

type Date time.Time

func ParseDate(raw string) (Date, error) {
	t, err := time.Parse(DateLayout, raw)
	if err != nil {
		return Date{}, err
	}
	return DateFromTime(t), nil
}

func DateFromTime(t time.Time) Date {
	return Date(time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC))
}

func (d Date) Time() time.Time {
	return time.Time(d)
}

func (d Date) String() string {
	return d.Time().Format(DateLayout)
}

func (d Date) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (d *Date) UnmarshalJSON(b []byte) error {
	var raw string
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	parsed, err := ParseDate(raw)
	if err != nil {
		return fmt.Errorf("date must be YYYY-MM-DD")
	}
	*d = parsed
	return nil
}

type Status string

const (
	StatusTodo       Status = "todo"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
	StatusClosed     Status = "closed"
)

func (s Status) Valid() bool {
	switch s {
	case StatusTodo, StatusInProgress, StatusDone, StatusClosed:
		return true
	}
	return false
}

func (s Status) CountsAsDone() bool {
	switch s {
	case StatusDone, StatusClosed:
		return true
	}
	return false
}

type IssueCloseReason string

const (
	CloseReasonDuplicate IssueCloseReason = "duplicate"
	CloseReasonWontDo    IssueCloseReason = "wont_do"
	CloseReasonInvalid   IssueCloseReason = "invalid"
)

func (r IssueCloseReason) Valid() bool {
	switch r {
	case CloseReasonDuplicate, CloseReasonWontDo, CloseReasonInvalid:
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
	ID            uuid.UUID         `json:"id"`
	ProjectID     uuid.UUID         `json:"project_id"`
	OwnerUsername string            `json:"owner_username"`
	ProjectKey    string            `json:"project_key"`
	Number        int               `json:"number"`
	Identifier    string            `json:"identifier"`
	Title         string            `json:"title"`
	Description   string            `json:"description"`
	Status        Status            `json:"status"`
	CloseReason   *IssueCloseReason `json:"close_reason"`
	Priority      IssuePriority     `json:"priority"`
	AssigneeID    *uuid.UUID        `json:"assignee_id,omitempty"`
	ReporterID    *uuid.UUID        `json:"reporter_id,omitempty"`
	SprintID      *uuid.UUID        `json:"sprint_id,omitempty"`
	ParentIssueID *uuid.UUID        `json:"parent_issue_id,omitempty"`
	DueDate       *Date             `json:"due_date"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
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

type ProjectContextKind string

const (
	ProjectContextKindText ProjectContextKind = "text"
)

func (k ProjectContextKind) Valid() bool {
	switch k {
	case ProjectContextKindText:
		return true
	}
	return false
}

type ProjectContextScope string

const (
	ProjectContextScopeProject ProjectContextScope = "project"
	ProjectContextScopeIssue   ProjectContextScope = "issue"
)

func (s ProjectContextScope) Valid() bool {
	switch s {
	case ProjectContextScopeProject, ProjectContextScopeIssue:
		return true
	}
	return false
}

type ProjectContext struct {
	ID             uuid.UUID           `json:"id"`
	ProjectID      uuid.UUID           `json:"project_id"`
	Number         int                 `json:"number"`
	Ref            string              `json:"ref"`
	Scope          ProjectContextScope `json:"scope"`
	Title          string              `json:"title"`
	Kind           ProjectContextKind  `json:"kind"`
	ContentType    string              `json:"content_type"`
	Body           string              `json:"body"`
	SourceFilename *string             `json:"source_filename,omitempty"`
	CreatedByID    uuid.UUID           `json:"created_by_id"`
	UpdatedByID    uuid.UUID           `json:"updated_by_id"`
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
}

type ProjectContextSummary struct {
	ID               uuid.UUID           `json:"id"`
	ProjectID        uuid.UUID           `json:"project_id"`
	Number           int                 `json:"number"`
	Ref              string              `json:"ref"`
	Scope            ProjectContextScope `json:"scope"`
	Title            string              `json:"title"`
	Kind             ProjectContextKind  `json:"kind"`
	ContentType      string              `json:"content_type"`
	SourceFilename   *string             `json:"source_filename,omitempty"`
	CreatedByID      uuid.UUID           `json:"created_by_id"`
	UpdatedByID      uuid.UUID           `json:"updated_by_id"`
	LinkedIssueCount int                 `json:"linked_issue_count"`
	CreatedAt        time.Time           `json:"created_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
}

type IssueContextLink struct {
	ID        uuid.UUID `json:"id"`
	ProjectID uuid.UUID `json:"project_id"`
	IssueID   uuid.UUID `json:"issue_id"`
	ContextID uuid.UUID `json:"context_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func SprintRef(number int) string {
	return fmt.Sprintf("sprint-%d", number)
}

func IssueLinkRef(number int) string {
	return fmt.Sprintf("link-%d", number)
}

func ProjectContextRef(number int) string {
	return fmt.Sprintf("context-%d", number)
}

func CommentRef(number int) string {
	return fmt.Sprintf("comment-%d", number)
}

type Comment struct {
	ID        uuid.UUID  `json:"id"`
	IssueID   uuid.UUID  `json:"issue_id"`
	Number    int        `json:"number"`
	Ref       string     `json:"ref"`
	AuthorID  uuid.UUID  `json:"author_id"`
	Body      string     `json:"body"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	EditedAt  *time.Time `json:"edited_at"`
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
