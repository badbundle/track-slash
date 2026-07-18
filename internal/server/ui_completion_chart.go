package server

import (
	"fmt"
	"math"
	"strings"

	"github.com/bradleymackey/track-slash/internal/model"
)

func uiProjectCompletionChart(history model.ProjectCompletionHistory) uiProjectCompletionChartData {
	chart := uiProjectCompletionChartData{Points: make([]uiProjectCompletionChartPoint, 0, len(history.Points))}
	if len(history.Points) == 0 {
		return chart
	}
	chart.RangeLabel = history.Start.Format("Jan 2, 2006") + " to " + history.End.Format("Jan 2, 2006")
	chart.FirstLabel = history.Points[0].PeriodStart.Format("Jan 2")
	chart.LastLabel = history.Points[len(history.Points)-1].PeriodStart.Format("Jan 2")
	const (
		left   = 44
		right  = 580
		top    = 16
		bottom = 140
	)
	step := 0.0
	if len(history.Points) > 1 {
		step = float64(right-left) / float64(len(history.Points)-1)
	}
	dataPoints := 0
	var segment []string
	flushSegment := func() {
		if len(segment) > 1 {
			chart.Segments = append(chart.Segments, uiProjectCompletionChartSegment{Points: strings.Join(segment, " ")})
		}
		segment = nil
	}
	for i, point := range history.Points {
		x := int(math.Round(float64(left) + float64(i)*step))
		y := bottom - int(math.Round(point.Rate/100*float64(bottom-top)))
		item := uiProjectCompletionChartPoint{
			X:          x,
			Y:          y,
			WeekLabel:  "Week of " + point.PeriodStart.Format("Jan 2, 2006"),
			AsOfLabel:  point.AsOf.Format("Jan 2, 2006"),
			RateLabel:  fmt.Sprintf("%.0f%%", point.Rate),
			Total:      point.Total,
			Completed:  point.Completed,
			HasTickets: point.Total > 0,
		}
		chart.Points = append(chart.Points, item)
		if !item.HasTickets {
			flushSegment()
			continue
		}
		chart.HasData = true
		dataPoints++
		segment = append(segment, fmt.Sprintf("%d,%d", item.X, item.Y))
	}
	flushSegment()
	chart.HasTrend = dataPoints > 1
	return chart
}
