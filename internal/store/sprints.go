package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bradleymackey/track-slash/internal/model"
)

type CreateSprintParams struct {
	ProjectID uuid.UUID
	Name      string
	Goal      string
	StartDate time.Time
	EndDate   time.Time
}

func (s *Store) CreateSprint(ctx context.Context, p CreateSprintParams) (model.Sprint, error) {
	const q = `
		INSERT INTO sprints (project_id, name, goal, start_date, end_date)
		SELECT id, $2, $3, $4, $5 FROM projects
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, project_id, name, goal, status, start_date, end_date,
		          completed_at, created_at, updated_at
	`
	var out model.Sprint
	err := s.db.QueryRow(ctx, q, p.ProjectID, p.Name, p.Goal, p.StartDate, p.EndDate).Scan(
		&out.ID, &out.ProjectID, &out.Name, &out.Goal, &out.Status,
		&out.StartDate, &out.EndDate, &out.CompletedAt, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		if isNoRows(err) {
			return model.Sprint{}, fmt.Errorf("project not found: %w", ErrNotFound)
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23503":
				return model.Sprint{}, fmt.Errorf("project not found: %w", ErrNotFound)
			case "23514":
				return model.Sprint{}, fmt.Errorf("end_date must be on or after start_date: %w", ErrConflict)
			}
		}
		return model.Sprint{}, err
	}
	return out, nil
}

func (s *Store) GetSprint(ctx context.Context, id uuid.UUID) (model.Sprint, error) {
	const q = `
		SELECT id, project_id, name, goal, status, start_date, end_date,
		       completed_at, created_at, updated_at
		FROM sprints WHERE id = $1 AND deleted_at IS NULL
	`
	var out model.Sprint
	err := s.db.QueryRow(ctx, q, id).Scan(
		&out.ID, &out.ProjectID, &out.Name, &out.Goal, &out.Status,
		&out.StartDate, &out.EndDate, &out.CompletedAt, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		if isNoRows(err) {
			return model.Sprint{}, ErrNotFound
		}
		return model.Sprint{}, err
	}
	return out, nil
}

type ListSprintsParams struct {
	ProjectID uuid.UUID
	Status    model.SprintStatus // empty = all
	Cursor    *SprintsCursor
	Limit     int
}

// SprintsCursor keys off (start_date, created_at, id) matching the list's
// stable ordering. All three are needed because start_date+created_at can tie.
type SprintsCursor struct {
	StartDate time.Time `json:"s"`
	CreatedAt time.Time `json:"c"`
	ID        uuid.UUID `json:"i"`
}

func (s *Store) ListSprints(ctx context.Context, p ListSprintsParams) ([]model.Sprint, bool, error) {
	args := []any{p.ProjectID}
	q := `
		SELECT id, project_id, name, goal, status, start_date, end_date,
		       completed_at, created_at, updated_at
		FROM sprints
		WHERE project_id = $1 AND deleted_at IS NULL
	`
	if p.Status != "" {
		args = append(args, string(p.Status))
		q += fmt.Sprintf(" AND status = $%d", len(args))
	}
	if p.Cursor != nil {
		args = append(args, p.Cursor.StartDate, p.Cursor.CreatedAt, p.Cursor.ID)
		q += fmt.Sprintf(" AND (start_date, created_at, id) > ($%d, $%d, $%d)",
			len(args)-2, len(args)-1, len(args))
	}
	args = append(args, p.Limit+1)
	q += fmt.Sprintf(" ORDER BY start_date ASC, created_at ASC, id ASC LIMIT $%d", len(args))

	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	out := make([]model.Sprint, 0, p.Limit)
	for rows.Next() {
		var sp model.Sprint
		if err := rows.Scan(
			&sp.ID, &sp.ProjectID, &sp.Name, &sp.Goal, &sp.Status,
			&sp.StartDate, &sp.EndDate, &sp.CompletedAt, &sp.CreatedAt, &sp.UpdatedAt,
		); err != nil {
			return nil, false, err
		}
		out = append(out, sp)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(out) > p.Limit
	if hasMore {
		out = out[:p.Limit]
	}
	return out, hasMore, nil
}

type UpdateSprintParams struct {
	Name      *string
	Goal      *string
	StartDate *time.Time
	EndDate   *time.Time
	// Status: callers may only drive planned → active. Completion is
	// reserved for CompleteSprint so the rollover always runs atomically.
	Status *model.SprintStatus
}

func (s *Store) UpdateSprint(ctx context.Context, id uuid.UUID, p UpdateSprintParams) (model.Sprint, error) {
	var out model.Sprint
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var current model.SprintStatus
		err := tx.QueryRow(ctx, `SELECT status FROM sprints WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&current)
		if err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		if p.Status != nil {
			if err := validateSprintTransition(current, *p.Status); err != nil {
				return err
			}
		}

		sets := []string{}
		args := []any{}
		i := 1
		if p.Name != nil {
			sets = append(sets, fmt.Sprintf("name = $%d", i))
			args = append(args, *p.Name)
			i++
		}
		if p.Goal != nil {
			sets = append(sets, fmt.Sprintf("goal = $%d", i))
			args = append(args, *p.Goal)
			i++
		}
		if p.StartDate != nil {
			sets = append(sets, fmt.Sprintf("start_date = $%d", i))
			args = append(args, *p.StartDate)
			i++
		}
		if p.EndDate != nil {
			sets = append(sets, fmt.Sprintf("end_date = $%d", i))
			args = append(args, *p.EndDate)
			i++
		}
		if p.Status != nil {
			sets = append(sets, fmt.Sprintf("status = $%d", i))
			args = append(args, string(*p.Status))
			i++
		}

		if len(sets) == 0 {
			return tx.QueryRow(ctx, `
				SELECT id, project_id, name, goal, status, start_date, end_date,
				       completed_at, created_at, updated_at
				FROM sprints WHERE id = $1 AND deleted_at IS NULL
			`, id).Scan(
				&out.ID, &out.ProjectID, &out.Name, &out.Goal, &out.Status,
				&out.StartDate, &out.EndDate, &out.CompletedAt, &out.CreatedAt, &out.UpdatedAt,
			)
		}

		sets = append(sets, "updated_at = now()")
		args = append(args, id)
		q := fmt.Sprintf(`
			UPDATE sprints SET %s WHERE id = $%d AND deleted_at IS NULL
			RETURNING id, project_id, name, goal, status, start_date, end_date,
			          completed_at, created_at, updated_at
		`, strings.Join(sets, ", "), i)

		err = tx.QueryRow(ctx, q, args...).Scan(
			&out.ID, &out.ProjectID, &out.Name, &out.Goal, &out.Status,
			&out.StartDate, &out.EndDate, &out.CompletedAt, &out.CreatedAt, &out.UpdatedAt,
		)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) {
				switch pgErr.Code {
				case "23505":
					return fmt.Errorf("another sprint is already active in this project: %w", ErrConflict)
				case "23514":
					return fmt.Errorf("end_date must be on or after start_date: %w", ErrConflict)
				}
			}
			return err
		}
		return nil
	})
	if err != nil {
		return model.Sprint{}, err
	}
	return out, nil
}

func validateSprintTransition(from, to model.SprintStatus) error {
	if from == to {
		return nil
	}
	if from == model.SprintStatusPlanned && to == model.SprintStatusActive {
		return nil
	}
	if to == model.SprintStatusCompleted {
		return fmt.Errorf("complete via POST /sprints/{id}/complete: %w", ErrConflict)
	}
	return fmt.Errorf("invalid sprint transition %s -> %s: %w", from, to, ErrConflict)
}

// CompleteSprint marks the sprint completed and rolls non-done issues forward
// to the next planned sprint (by start_date). If no planned sprint exists,
// non-done issues fall back to the backlog (sprint_id NULL). Done issues stay
// attached to the completed sprint so historical velocity is preserved.
func (s *Store) CompleteSprint(ctx context.Context, id uuid.UUID) (model.Sprint, error) {
	var out model.Sprint
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var projectID uuid.UUID
		var status model.SprintStatus
		err := tx.QueryRow(ctx, `
			SELECT project_id, status FROM sprints WHERE id = $1 AND deleted_at IS NULL FOR UPDATE
		`, id).Scan(&projectID, &status)
		if err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		if status != model.SprintStatusActive {
			return fmt.Errorf("can only complete an active sprint (current: %s): %w", status, ErrConflict)
		}

		var target *uuid.UUID
		var t uuid.UUID
		err = tx.QueryRow(ctx, `
			SELECT id FROM sprints
			WHERE project_id = $1 AND status = 'planned' AND deleted_at IS NULL
			ORDER BY start_date ASC, created_at ASC
			LIMIT 1
		`, projectID).Scan(&t)
		if err != nil && !isNoRows(err) {
			return err
		}
		if err == nil {
			target = &t
		}

		if _, err := tx.Exec(ctx, `
			UPDATE issues SET sprint_id = $2, updated_at = now()
			WHERE sprint_id = $1 AND status <> 'done' AND deleted_at IS NULL
		`, id, target); err != nil {
			return err
		}

		return tx.QueryRow(ctx, `
			UPDATE sprints
			SET status = 'completed', completed_at = now(), updated_at = now()
			WHERE id = $1 AND deleted_at IS NULL
			RETURNING id, project_id, name, goal, status, start_date, end_date,
			          completed_at, created_at, updated_at
		`, id).Scan(
			&out.ID, &out.ProjectID, &out.Name, &out.Goal, &out.Status,
			&out.StartDate, &out.EndDate, &out.CompletedAt, &out.CreatedAt, &out.UpdatedAt,
		)
	})
	if err != nil {
		return model.Sprint{}, err
	}
	return out, nil
}

func (s *Store) DeleteSprint(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE sprints SET deleted_at = now(), updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL AND status <> 'active'
	`, id)
	if err != nil {
		// Defensive: soft-delete has no expected FK/check mapping.
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	var status model.SprintStatus
	err = s.db.QueryRow(ctx, `
		SELECT status FROM sprints WHERE id = $1 AND deleted_at IS NULL
	`, id).Scan(&status)
	if err != nil {
		if isNoRows(err) {
			return ErrNotFound
		}
		// Defensive: only no-rows has a domain mapping here.
		return err
	}
	if status == model.SprintStatusActive {
		return fmt.Errorf("cannot delete active sprint: %w", ErrConflict)
	}
	return ErrNotFound
}
