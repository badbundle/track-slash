package server

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/bradleymackey/track-slash/internal/model"
)

func TestUIProjectCompletionChart(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)
	history := model.ProjectCompletionHistory{
		Start: start,
		End:   start.AddDate(0, 0, 20),
		Points: []model.ProjectCompletionHistoryPoint{
			{PeriodStart: start, AsOf: start.AddDate(0, 0, 6)},
			{PeriodStart: start.AddDate(0, 0, 7), AsOf: start.AddDate(0, 0, 13), Total: 2, Completed: 1, Rate: 50},
			{PeriodStart: start.AddDate(0, 0, 14), AsOf: start.AddDate(0, 0, 20), Total: 4, Completed: 3, Rate: 75},
		},
	}
	chart := uiProjectCompletionChart(history)
	if !chart.HasData || !chart.HasTrend || len(chart.Points) != 3 || len(chart.Segments) != 1 {
		t.Fatalf("chart = %+v", chart)
	}
	if chart.Segments[0].Points != "312,78 580,47" {
		t.Fatalf("polyline = %q", chart.Segments[0].Points)
	}
	if chart.Points[1].RateLabel != "50%" || chart.Points[1].WeekLabel != "Week of Feb 9, 2026" || chart.FirstLabel != "Feb 2" || chart.LastLabel != "Feb 16" {
		t.Fatalf("chart labels = %+v", chart)
	}

	chart = uiProjectCompletionChart(model.ProjectCompletionHistory{Points: history.Points[:2]})
	if !chart.HasData || chart.HasTrend || len(chart.Segments) != 0 {
		t.Fatalf("single-point chart = %+v", chart)
	}
	if chart := uiProjectCompletionChart(model.ProjectCompletionHistory{}); chart.HasData || len(chart.Points) != 0 {
		t.Fatalf("empty chart = %+v", chart)
	}
}

func TestUIProjectCompletionChartRenderingAndEmptyState(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)
	panel := &uiProjectPanelData{
		Project: model.Project{OwnerUsername: "bradley", Key: "TRACK", Name: "Track Slash"},
		CompletionChart: uiProjectCompletionChart(model.ProjectCompletionHistory{
			Start: start,
			End:   start.AddDate(0, 0, 13),
			Points: []model.ProjectCompletionHistoryPoint{
				{PeriodStart: start, AsOf: start.AddDate(0, 0, 6), Total: 2, Completed: 1, Rate: 50},
				{PeriodStart: start.AddDate(0, 0, 7), AsOf: start.AddDate(0, 0, 13), Total: 2, Completed: 2, Rate: 100},
			},
		}),
	}
	var buf bytes.Buffer
	if err := uiTemplates.ExecuteTemplate(&buf, "project-panel-about", panel); err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}
	body := buf.String()
	for _, want := range []string{"Completion rate", `role="img"`, "Ticket completion rate over time", `polyline points="44,78 580,16"`, "Weekly ticket completion rate summary", "50%", "100%"} {
		if !strings.Contains(body, want) {
			t.Fatalf("completion chart missing %q: %s", want, body)
		}
	}

	panel.CompletionChart = uiProjectCompletionChart(model.ProjectCompletionHistory{})
	buf.Reset()
	if err := uiTemplates.ExecuteTemplate(&buf, "project-panel-about", panel); err != nil {
		t.Fatalf("ExecuteTemplate empty: %v", err)
	}
	body = buf.String()
	if !strings.Contains(body, "No ticket history is available for the last 12 weeks.") || strings.Contains(body, `role="img"`) {
		t.Fatalf("completion chart empty state = %s", body)
	}
}
