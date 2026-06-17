package store

import (
	"context"
	"database/sql"
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

type sprintScanner interface {
	Scan(dest ...any) error
}

func scanSprint(row sprintScanner) (model.Sprint, error) {
	var out model.Sprint
	var plannedOrder sql.NullInt64
	err := row.Scan(
		&out.ID, &out.ProjectID, &out.Number, &out.Name, &out.Goal, &out.Status, &plannedOrder,
		&out.StartDate, &out.EndDate, &out.CompletedAt, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return model.Sprint{}, err
	}
	out.Ref = model.SprintRef(out.Number)
	if plannedOrder.Valid {
		order := plannedOrder.Int64
		out.PlannedOrder = &order
	}
	return out, nil
}

func (s *Store) CreateSprint(ctx context.Context, p CreateSprintParams) (model.Sprint, error) {
	var out model.Sprint
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var (
			projectID uuid.UUID
			number    int
		)
		if err := tx.QueryRow(ctx, `
			SELECT id, next_sprint_number FROM projects WHERE id = $1 AND deleted_at IS NULL FOR UPDATE
		`, p.ProjectID).Scan(&projectID, &number); err != nil {
			if isNoRows(err) {
				return fmt.Errorf("project not found: %w", ErrNotFound)
			}
			return err
		}

		var plannedOrder int64
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(MAX(planned_order), 0) + 1
			FROM sprints
			WHERE project_id = $1 AND status = 'planned' AND deleted_at IS NULL
		`, projectID).Scan(&plannedOrder); err != nil {
			return err
		}

		var err error
		out, err = scanSprint(tx.QueryRow(ctx, `
			INSERT INTO sprints (project_id, number, name, goal, start_date, end_date, planned_order)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id, project_id, number, name, goal, status, planned_order, start_date, end_date,
			          completed_at, created_at, updated_at
		`, projectID, number, p.Name, p.Goal, p.StartDate, p.EndDate, plannedOrder))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			UPDATE projects SET next_sprint_number = next_sprint_number + 1, updated_at = now()
			WHERE id = $1
		`, projectID)
		return err
	})
	if err != nil {
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
		SELECT id, project_id, number, name, goal, status, planned_order, start_date, end_date,
		       completed_at, created_at, updated_at
		FROM sprints WHERE id = $1 AND deleted_at IS NULL
	`
	out, err := scanSprint(s.db.QueryRow(ctx, q, id))
	if err != nil {
		if isNoRows(err) {
			return model.Sprint{}, ErrNotFound
		}
		return model.Sprint{}, err
	}
	return out, nil
}

func (s *Store) GetSprintByProjectNumber(ctx context.Context, projectID uuid.UUID, number int) (model.Sprint, error) {
	const q = `
		SELECT id, project_id, number, name, goal, status, planned_order, start_date, end_date,
		       completed_at, created_at, updated_at
		FROM sprints
		WHERE project_id = $1 AND number = $2 AND deleted_at IS NULL
	`
	out, err := scanSprint(s.db.QueryRow(ctx, q, projectID, number))
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

// SprintsCursor keys off the active ordering. Planned lists use
// (planned_order, id); other lists use (start_date, created_at, id).
type SprintsCursor struct {
	PlannedOrder int64     `json:"p,omitempty"`
	StartDate    time.Time `json:"s"`
	CreatedAt    time.Time `json:"c"`
	ID           uuid.UUID `json:"i"`
}

func (s *Store) ListSprints(ctx context.Context, p ListSprintsParams) ([]model.Sprint, bool, error) {
	args := []any{p.ProjectID}
	q := `
		SELECT id, project_id, number, name, goal, status, planned_order, start_date, end_date,
		       completed_at, created_at, updated_at
		FROM sprints
		WHERE project_id = $1 AND deleted_at IS NULL
	`
	if p.Status != "" {
		args = append(args, string(p.Status))
		q += fmt.Sprintf(" AND status = $%d", len(args))
	}
	if p.Cursor != nil && p.Status == model.SprintStatusPlanned {
		args = append(args, p.Cursor.PlannedOrder, p.Cursor.ID)
		q += fmt.Sprintf(" AND (planned_order, id) > ($%d, $%d)",
			len(args)-1, len(args))
	} else if p.Cursor != nil {
		args = append(args, p.Cursor.StartDate, p.Cursor.CreatedAt, p.Cursor.ID)
		q += fmt.Sprintf(" AND (start_date, created_at, id) > ($%d, $%d, $%d)",
			len(args)-2, len(args)-1, len(args))
	}
	args = append(args, p.Limit+1)
	if p.Status == model.SprintStatusPlanned {
		q += fmt.Sprintf(" ORDER BY planned_order ASC NULLS LAST, id ASC LIMIT $%d", len(args))
	} else {
		q += fmt.Sprintf(" ORDER BY start_date ASC, created_at ASC, id ASC LIMIT $%d", len(args))
	}

	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	out := make([]model.Sprint, 0, p.Limit)
	for rows.Next() {
		sp, err := scanSprint(rows)
		if err != nil {
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
		if current == model.SprintStatusCompleted &&
			(p.Goal != nil || p.StartDate != nil || p.EndDate != nil || p.Status != nil) {
			return fmt.Errorf("completed sprint can only be renamed: %w", ErrConflict)
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
			if current == model.SprintStatusPlanned && *p.Status == model.SprintStatusActive {
				sets = append(sets, "planned_order = NULL")
			}
		}

		if len(sets) == 0 {
			var err error
			out, err = scanSprint(tx.QueryRow(ctx, `
				SELECT id, project_id, number, name, goal, status, planned_order, start_date, end_date,
				       completed_at, created_at, updated_at
				FROM sprints WHERE id = $1 AND deleted_at IS NULL
			`, id))
			return err
		}

		sets = append(sets, "updated_at = now()")
		args = append(args, id)
		q := fmt.Sprintf(`
			UPDATE sprints SET %s WHERE id = $%d AND deleted_at IS NULL
			RETURNING id, project_id, number, name, goal, status, planned_order, start_date, end_date,
			          completed_at, created_at, updated_at
	`, strings.Join(sets, ", "), i)

		out, err = scanSprint(tx.QueryRow(ctx, q, args...))
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
		return fmt.Errorf("complete via POST /sprints/sprint-N/complete: %w", ErrConflict)
	}
	return fmt.Errorf("invalid sprint transition %s -> %s: %w", from, to, ErrConflict)
}

// CompleteSprint marks the sprint completed. Non-done issues roll into the
// next planned sprint when one exists, otherwise they fall back to the backlog;
// done issues stay attached so historical velocity is preserved.
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

		var nextSprintID uuid.UUID
		nextErr := tx.QueryRow(ctx, `
			SELECT id
			FROM sprints
			WHERE project_id = $1 AND status = 'planned' AND deleted_at IS NULL
			ORDER BY planned_order ASC NULLS LAST, id ASC
			LIMIT 1
			FOR UPDATE
		`, projectID).Scan(&nextSprintID)
		if nextErr != nil && !isNoRows(nextErr) {
			return nextErr
		}

		if isNoRows(nextErr) {
			_, err = tx.Exec(ctx, `
				UPDATE issues SET sprint_id = NULL, updated_at = now()
				WHERE sprint_id = $1 AND status <> 'done' AND deleted_at IS NULL
			`, id)
		} else {
			_, err = tx.Exec(ctx, `
				UPDATE issues SET sprint_id = $2, updated_at = now()
				WHERE sprint_id = $1 AND status <> 'done' AND deleted_at IS NULL
			`, id, nextSprintID)
		}
		if err != nil {
			return err
		}

		out, err = scanSprint(tx.QueryRow(ctx, `
			UPDATE sprints
			SET status = 'completed', planned_order = NULL, completed_at = now(), updated_at = now()
			WHERE id = $1 AND deleted_at IS NULL
			RETURNING id, project_id, number, name, goal, status, planned_order, start_date, end_date,
			          completed_at, created_at, updated_at
		`, id))
		return err
	})
	if err != nil {
		return model.Sprint{}, err
	}
	return out, nil
}

func (s *Store) DeleteSprint(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE sprints SET deleted_at = now(), updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL AND status = 'planned'
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
	if status == model.SprintStatusActive || status == model.SprintStatusCompleted {
		return fmt.Errorf("cannot delete %s sprint: %w", status, ErrConflict)
	}
	return ErrNotFound
}

type ReorderPlannedSprintsParams struct {
	ProjectID uuid.UUID
	SprintIDs []uuid.UUID
}

func (s *Store) ReorderPlannedSprints(ctx context.Context, p ReorderPlannedSprintsParams) ([]model.Sprint, error) {
	var out []model.Sprint
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var projectID uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT id FROM projects WHERE id = $1 AND deleted_at IS NULL FOR UPDATE
		`, p.ProjectID).Scan(&projectID); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}

		rows, err := tx.Query(ctx, `
			SELECT id
			FROM sprints
			WHERE project_id = $1 AND status = 'planned' AND deleted_at IS NULL
			ORDER BY planned_order ASC NULLS LAST, id ASC
			FOR UPDATE
		`, projectID)
		if err != nil {
			return err
		}
		defer rows.Close()

		current := map[uuid.UUID]struct{}{}
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return err
			}
			current[id] = struct{}{}
		}
		if err := rows.Err(); err != nil {
			return err
		}

		if len(p.SprintIDs) != len(current) {
			return fmt.Errorf("sprint_refs must include every planned sprint exactly once: %w", ErrConflict)
		}
		seen := map[uuid.UUID]struct{}{}
		for _, id := range p.SprintIDs {
			if _, ok := current[id]; !ok {
				return fmt.Errorf("sprint_refs must include only planned sprints from this project: %w", ErrConflict)
			}
			if _, dup := seen[id]; dup {
				return fmt.Errorf("sprint_refs must not contain duplicates: %w", ErrConflict)
			}
			seen[id] = struct{}{}
		}
		if len(p.SprintIDs) == 0 {
			out = []model.Sprint{}
			return nil
		}

		offset := int64(len(p.SprintIDs) + 1)
		if _, err := tx.Exec(ctx, `
			UPDATE sprints
			SET planned_order = COALESCE(planned_order, 0) + $2
			WHERE project_id = $1 AND status = 'planned' AND deleted_at IS NULL
		`, projectID, offset); err != nil {
			return err
		}

		for i, id := range p.SprintIDs {
			if _, err := tx.Exec(ctx, `
				UPDATE sprints
				SET planned_order = $2, updated_at = now()
				WHERE id = $1 AND project_id = $3 AND status = 'planned' AND deleted_at IS NULL
			`, id, int64(i+1), projectID); err != nil {
				return err
			}
		}

		rows, err = tx.Query(ctx, `
			SELECT id, project_id, number, name, goal, status, planned_order, start_date, end_date,
			       completed_at, created_at, updated_at
			FROM sprints
			WHERE project_id = $1 AND status = 'planned' AND deleted_at IS NULL
			ORDER BY planned_order ASC, id ASC
		`, projectID)
		if err != nil {
			return err
		}
		defer rows.Close()

		out = make([]model.Sprint, 0, len(p.SprintIDs))
		for rows.Next() {
			sp, err := scanSprint(rows)
			if err != nil {
				return err
			}
			out = append(out, sp)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
