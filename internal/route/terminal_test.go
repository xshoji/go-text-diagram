package route

import (
	"testing"

	"github.com/xshoji/agents-workspace/internal/diagram"
	"github.com/xshoji/agents-workspace/internal/geom"
	"github.com/xshoji/agents-workspace/internal/layout"
)

func TestEnsureArrowLeadSegmentsLeavesLineCellBeforeArrowheads(t *testing.T) {
	t.Parallel()

	result := &layout.Layout{
		Direction: diagram.DirectionRight,
		Metrics:   layout.Metrics{Width: 10, Height: 8},
	}
	edge := &Edge{
		ID: "edge", Kind: diagram.Bidirectional,
		Points: []Point{{X: 1, Y: 5}, {X: 2, Y: 5}, {X: 2, Y: 2}, {X: 7, Y: 2}, {X: 7, Y: 3}, {X: 8, Y: 3}},
	}

	EnsureArrowLeadSegments(result, []*Edge{edge}, nil)

	last := len(edge.Points) - 1
	if start := geom.Manhattan(edge.Points[0], edge.Points[1]); start < minimumArrowLeadLength {
		t.Fatalf("source arrow lead length = %d, want at least %d: %v", start, minimumArrowLeadLength, edge.Points)
	}
	if end := geom.Manhattan(edge.Points[last-1], edge.Points[last]); end < minimumArrowLeadLength {
		t.Fatalf("target arrow lead length = %d, want at least %d: %v", end, minimumArrowLeadLength, edge.Points)
	}
}

func TestEnsureArrowLeadSegmentsEntersNodeNormalToBoundary(t *testing.T) {
	t.Parallel()

	result := &layout.Layout{
		Direction: diagram.DirectionDown,
		Metrics:   layout.Metrics{Width: 12, Height: 12},
	}
	edge := &Edge{
		ID: "edge", Kind: diagram.Directed, EndSide: diagram.SideNorth,
		Points: []Point{{X: 2, Y: 2}, {X: 2, Y: 8}, {X: 7, Y: 8}},
	}

	EnsureArrowLeadSegments(result, []*Edge{edge}, nil)

	last := len(edge.Points) - 1
	if got := stepDirection(edge.Points[last-1], edge.Points[last]); got != (Point{Y: 1}) {
		t.Fatalf("target direction = %v, want downward entry: %v", got, edge.Points)
	}
	if got := geom.Manhattan(edge.Points[last-1], edge.Points[last]); got < minimumArrowLeadLength {
		t.Fatalf("target arrow lead length = %d, want at least %d: %v", got, minimumArrowLeadLength, edge.Points)
	}
}

func TestEnsureArrowLeadSegmentsPreservesReversedTerminalDirection(t *testing.T) {
	t.Parallel()

	result := &layout.Layout{
		Direction: diagram.DirectionDown,
		Metrics:   layout.Metrics{Width: 12, Height: 12},
	}
	edge := &Edge{
		ID: "edge", Kind: diagram.Directed, Reversed: true, EndSide: diagram.SideNorth,
		Points: []Point{{X: 9, Y: 2}, {X: 9, Y: 7}, {X: 8, Y: 7}},
	}
	wantDirection := stepDirection(edge.Points[len(edge.Points)-2], edge.Points[len(edge.Points)-1])

	EnsureArrowLeadSegments(result, []*Edge{edge}, nil)

	last := len(edge.Points) - 1
	if got := stepDirection(edge.Points[last-1], edge.Points[last]); got != wantDirection {
		t.Fatalf("reversed target direction = %v, want %v: %v", got, wantDirection, edge.Points)
	}
	if got := geom.Manhattan(edge.Points[last-1], edge.Points[last]); got < minimumArrowLeadLength {
		t.Fatalf("target arrow lead length = %d, want at least %d: %v", got, minimumArrowLeadLength, edge.Points)
	}
}
