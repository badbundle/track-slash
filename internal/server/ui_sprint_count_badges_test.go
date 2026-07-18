package server

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/google/uuid"
)

func TestUISprintIssueCountBadges(t *testing.T) {
	t.Parallel()

	project := model.Project{ID: uuid.New(), OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"}
	for _, tt := range []struct {
		name  string
		count int
		want  string
	}{
		{name: "zero", count: 0, want: "0 Issues"},
		{name: "one", count: 1, want: "1 Issue"},
		{name: "multiple", count: 2, want: "2 Issues"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			issues := make([]model.Issue, tt.count)
			activeItems := make([]uiIssueItem, tt.count)
			for i := range issues {
				issues[i] = model.Issue{
					ID:            uuid.New(),
					ProjectID:     project.ID,
					OwnerUsername: project.OwnerUsername,
					ProjectKey:    project.Key,
					Number:        i + 1,
					Identifier:    "TRACK-COUNT",
					Title:         "Counted issue",
					Status:        model.StatusTodo,
				}
				activeItems[i] = uiIssueItem{Issue: issues[i], Project: project}
			}

			activeSprint := model.Sprint{ID: uuid.New(), ProjectID: project.ID, Ref: "sprint-1", Name: "Active Sprint"}
			plannedSprint := model.Sprint{ID: uuid.New(), ProjectID: project.ID, Ref: "sprint-2", Name: "Planned Sprint"}
			historySprint := model.Sprint{ID: uuid.New(), ProjectID: project.ID, Ref: "sprint-3", Name: "Historical Sprint"}
			bodies := map[string]string{
				"active": renderSprintCountTemplate(t, "project-panel-sprint", &uiProjectPanelData{
					Project:      project,
					ActiveSprint: &activeSprint,
					SprintColumns: []uiIssueColumn{{
						Status: model.StatusTodo,
						Label:  "To do",
						Issues: activeItems,
					}},
				}),
				"planned": renderSprintCountTemplate(t, "project-panel-planned", &uiProjectPanelData{
					Project: project,
					PlannedSprints: []uiPlannedSprint{{
						Project: project,
						Sprint:  plannedSprint,
						Issues:  issues,
					}},
				}),
				"history": renderSprintCountTemplate(t, "project-panel-sprint-history", uiProjectSprintHistoryPageData{
					Project: project,
					Sprints: []model.Sprint{historySprint},
					StatusCounts: map[uuid.UUID]model.ProjectIssueStatusCounts{
						historySprint.ID: {Total: tt.count},
					},
				}),
			}
			wantBadge := `class="` + uiCountBadgeClass + `">` + tt.want + `</span>`
			for state, body := range bodies {
				if !strings.Contains(body, wantBadge) {
					t.Fatalf("%s sprint missing %q: %s", state, wantBadge, body)
				}
			}
		})
	}
}

func renderSprintCountTemplate(t *testing.T, name string, data any) string {
	t.Helper()
	var buf bytes.Buffer
	if err := uiTemplates.ExecuteTemplate(&buf, name, data); err != nil {
		t.Fatalf("ExecuteTemplate %s: %v", name, err)
	}
	return buf.String()
}
