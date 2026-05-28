package model

import (
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
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Project struct {
	ID          uuid.UUID `json:"id"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Issue struct {
	ID          uuid.UUID  `json:"id"`
	ProjectID   uuid.UUID  `json:"project_id"`
	Number      int        `json:"number"`
	Identifier  string     `json:"identifier"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      Status     `json:"status"`
	AssigneeID  *uuid.UUID `json:"assignee_id,omitempty"`
	ReporterID  *uuid.UUID `json:"reporter_id,omitempty"`
	SprintID    *uuid.UUID `json:"sprint_id,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
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
	SourceID  uuid.UUID `json:"source_id"`
	TargetID  uuid.UUID `json:"target_id"`
	LinkType  LinkType  `json:"link_type"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Sprint struct {
	ID          uuid.UUID    `json:"id"`
	ProjectID   uuid.UUID    `json:"project_id"`
	Name        string       `json:"name"`
	Goal        string       `json:"goal"`
	Status      SprintStatus `json:"status"`
	StartDate   time.Time    `json:"start_date"`
	EndDate     time.Time    `json:"end_date"`
	CompletedAt *time.Time   `json:"completed_at,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}
