package route

import (
	"testing"

	"github.com/xshoji/agents-workspace/internal/diagram"
	"github.com/xshoji/agents-workspace/internal/layout"
)

func TestAvoidGroupBoundaryOverlapsDetoursCollinearSegment(t *testing.T) {
	t.Parallel()

	result := &layout.Layout{
		Metrics: layout.Metrics{Width: 14, Height: 11},
		Groups:  []*layout.Group{{ID: "group", Rect: layout.Rect{X: 2, Y: 2, Width: 9, Height: 7}}},
	}
	edge := &Edge{ID: "edge", Points: []Point{{X: 0, Y: 8}, {X: 12, Y: 8}}}
	AvoidGroupBoundaryOverlaps(result, []*Edge{edge})
	if overlaps := newGroupBoundaryIndex(result.Groups).overlapCount(nil, edge.Points); overlaps != 0 {
		t.Fatalf("route still shares %d group-border segments: %v", overlaps, edge.Points)
	}
	if metrics := Analyze(result, []*Edge{edge}); metrics.AStarFallbacks != 0 {
		t.Fatalf("group detour counted as A* fallback: %+v", metrics)
	}
	assertOrthogonal(t, edge.Points)
}

func TestAvoidGroupBoundaryOverlapsKeepsPerpendicularCrossing(t *testing.T) {
	t.Parallel()

	result := &layout.Layout{
		Metrics: layout.Metrics{Width: 14, Height: 11},
		Groups:  []*layout.Group{{ID: "group", Rect: layout.Rect{X: 2, Y: 2, Width: 9, Height: 7}}},
	}
	want := []Point{{X: 0, Y: 5}, {X: 12, Y: 5}}
	edge := &Edge{ID: "edge", Points: append([]Point(nil), want...)}
	AvoidGroupBoundaryOverlaps(result, []*Edge{edge})
	if len(edge.Points) != len(want) || edge.Points[0] != want[0] || edge.Points[1] != want[1] {
		t.Fatalf("perpendicular boundary crossing changed: %v", edge.Points)
	}
}

func TestGroupBoundaryIndexExcludesOnlyEndpointGroup(t *testing.T) {
	t.Parallel()
	groups := []*layout.Group{
		{ID: "endpoint", Rect: layout.Rect{X: 2, Y: 2, Width: 5, Height: 5}},
		{ID: "other", Rect: layout.Rect{X: 2, Y: 2, Width: 5, Height: 5}},
	}
	points := []Point{{X: 2, Y: 2}, {X: 6, Y: 2}}
	index := newGroupBoundaryIndex(groups)
	edge := &Edge{Source: diagram.Endpoint{Kind: diagram.EndpointGroup, Group: "endpoint"}}
	if got := index.overlapCount(edge, points); got != 4 {
		t.Fatalf("shared non-endpoint boundary overlap count = %d, want 4", got)
	}
	edge.Target = diagram.Endpoint{Kind: diagram.EndpointGroup, Group: "other"}
	if got := index.overlapCount(edge, points); got != 0 {
		t.Fatalf("endpoint boundaries were not excluded: %d", got)
	}
}

func TestAvoidGroupBoundaryOverlapsPreservesConstrainedTerminalDirections(t *testing.T) {
	t.Parallel()

	result := &layout.Layout{
		Metrics: layout.Metrics{Width: 16, Height: 12},
		Groups:  []*layout.Group{{ID: "group", Rect: layout.Rect{X: 2, Y: 2, Width: 11, Height: 7}}},
	}
	edge := &Edge{ID: "edge", Constrained: true, Points: []Point{
		{X: 0, Y: 5}, {X: 1, Y: 5}, {X: 1, Y: 8}, {X: 14, Y: 8}, {X: 14, Y: 5}, {X: 15, Y: 5},
	}}
	startDirection := stepDirection(edge.Points[0], edge.Points[1])
	endDirection := stepDirection(edge.Points[len(edge.Points)-2], edge.Points[len(edge.Points)-1])
	AvoidGroupBoundaryOverlaps(result, []*Edge{edge})
	if overlaps := newGroupBoundaryIndex(result.Groups).overlapCount(nil, edge.Points); overlaps != 0 {
		t.Fatalf("route still shares %d group-border segments: %v", overlaps, edge.Points)
	}
	if startDirection != stepDirection(edge.Points[0], edge.Points[1]) ||
		endDirection != stepDirection(edge.Points[len(edge.Points)-2], edge.Points[len(edge.Points)-1]) {
		t.Fatalf("terminal directions changed: %v", edge.Points)
	}
}

func TestAvoidGroupBoundaryOverlapsPreservesAdaptiveStartDirection(t *testing.T) {
	t.Parallel()
	result := &layout.Layout{
		Metrics: layout.Metrics{Width: 16, Height: 12},
		Groups:  []*layout.Group{{ID: "group", Rect: layout.Rect{X: 2, Y: 2, Width: 11, Height: 7}}},
	}
	edge := &Edge{ID: "edge", AdaptiveStart: true, Points: []Point{
		{X: 0, Y: 5}, {X: 1, Y: 5}, {X: 1, Y: 8}, {X: 14, Y: 8}, {X: 14, Y: 5}, {X: 15, Y: 5},
	}}
	startDirection := stepDirection(edge.Points[0], edge.Points[1])
	AvoidGroupBoundaryOverlaps(result, []*Edge{edge})
	if startDirection != stepDirection(edge.Points[0], edge.Points[1]) {
		t.Fatalf("adaptive start direction changed: %v", edge.Points)
	}
}
