package route

import (
	"testing"

	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/geom"
	"github.com/xshoji/go-text-diagram/internal/layout"
)

func TestFanInKeepsExplicitTarget(t *testing.T) {
	offset := 4
	for _, test := range []struct {
		name          string
		endpoint, end diagram.PortHint
	}{
		{name: "endpoint side", endpoint: diagram.PortHint{Side: diagram.SideWest}},
		{name: "layout offset", end: diagram.PortHint{Offset: &offset}},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := diagram.NodeEndpoint("target")
			target.PortHint = test.endpoint
			result := &layout.Layout{
				Direction: diagram.DirectionRight, Metrics: layout.Metrics{Width: 100, Height: 40},
				Edges: []*layout.Edge{{OriginalEdgeID: "adapter", Target: target, End: test.end}},
			}
			routes := []*Edge{
				{ID: "adapter", From: "adapter", To: "target", Source: diagram.NodeEndpoint("adapter"), Target: target,
					StartSide: diagram.SideEast, EndSide: diagram.SideWest,
					Points: []Point{{X: 60, Y: 18}, {X: 62, Y: 18}, {X: 62, Y: 14}, {X: 78, Y: 14}}},
				{ID: "patient", From: "patient", To: "target", Source: diagram.NodeEndpoint("patient"), Target: diagram.NodeEndpoint("target"),
					StartSide: diagram.SideNorth, EndSide: diagram.SideWest, AdaptiveStart: true,
					Points: []Point{{X: 9, Y: 17}, {X: 9, Y: 15}, {X: 76, Y: 15}, {X: 76, Y: 18}, {X: 78, Y: 18}}},
			}
			metrics := EnsureArrowLeadSegments(result, routes, nil)
			if metrics.Crossings != 1 {
				t.Fatalf("explicit target constraint was relaxed: %+v", metrics)
			}
			if routes[0].Points[len(routes[0].Points)-1] != (Point{X: 78, Y: 14}) ||
				routes[1].Points[len(routes[1].Points)-1] != (Point{X: 78, Y: 18}) {
				t.Fatal("explicit target port was exchanged")
			}
		})
	}
}

func TestFanInAvoidsEndpointNodes(t *testing.T) {
	result := &layout.Layout{
		Direction: diagram.DirectionDown, Metrics: layout.Metrics{Width: 40, Height: 40},
		Nodes: []*layout.Node{
			{ID: "A", Rect: layout.Rect{X: 12, Y: 10, Width: 7, Height: 5}},
			{ID: "B", Rect: layout.Rect{X: 0, Y: 14, Width: 3, Height: 3}},
			{ID: "T", Rect: layout.Rect{X: 3, Y: 30, Width: 15, Height: 3}},
		},
	}
	routes := []*Edge{
		{ID: "a", From: "A", To: "T", Source: diagram.NodeEndpoint("A"), Target: diagram.NodeEndpoint("T"),
			StartSide: diagram.SideEast, EndSide: diagram.SideNorth, AdaptiveStart: true,
			Points: []Point{{X: 18, Y: 12}, {X: 20, Y: 12}, {X: 20, Y: 8}, {X: 5, Y: 8}, {X: 5, Y: 30}}},
		{ID: "b", From: "B", To: "T", Source: diagram.NodeEndpoint("B"), Target: diagram.NodeEndpoint("T"),
			StartSide: diagram.SideSouth, EndSide: diagram.SideNorth,
			Points: []Point{{X: 1, Y: 16}, {X: 1, Y: 25}, {X: 15, Y: 25}, {X: 15, Y: 30}}},
	}
	EnsureArrowLeadSegments(result, routes, nil)
	for _, edge := range routes {
		for i := 1; i < len(edge.Points); i++ {
			walkPoints(edge.Points[i-1], edge.Points[i], func(point Point) {
				for _, node := range result.Nodes {
					if geom.ContainsInterior(node.Rect, point) {
						t.Errorf("route %q enters node %q at %v: %v", edge.ID, node.ID, point, edge.Points)
					}
				}
			})
		}
	}
}

func TestEnsureArrowLeadSegmentsLeavesLineCellBeforeArrowheads(t *testing.T) {
	t.Parallel()

	result := &layout.Layout{
		Direction: diagram.DirectionRight,
		Metrics:   layout.Metrics{Width: 10, Height: 8},
	}
	edge := &Edge{
		ID: "edge", Kind: diagram.Bidirectional, Constrained: true,
		StartSide: diagram.SideEast, EndSide: diagram.SideWest,
		Source: diagram.NodeEndpoint("source"), Target: diagram.NodeEndpoint("target"),
		Points: []Point{{X: 1, Y: 5}, {X: 2, Y: 5}, {X: 2, Y: 2}, {X: 7, Y: 2}, {X: 7, Y: 3}, {X: 9, Y: 3}},
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

func TestEnsureArrowLeadSegmentsRepairsDirectedSourceLead(t *testing.T) {
	t.Parallel()

	result := &layout.Layout{
		Direction: diagram.DirectionRight,
		Metrics:   layout.Metrics{Width: 12, Height: 10},
	}
	edge := &Edge{
		ID: "edge", Kind: diagram.Directed, StartSide: diagram.SideSouth, EndSide: diagram.SideNorth, AdaptiveStart: true,
		Source: diagram.NodeEndpoint("source"), Target: diagram.NodeEndpoint("target"),
		Points: []Point{{X: 2, Y: 2}, {X: 2, Y: 3}, {X: 8, Y: 3}, {X: 8, Y: 6}, {X: 8, Y: 7}},
	}

	EnsureArrowLeadSegments(result, []*Edge{edge}, nil)

	if got := geom.Manhattan(edge.Points[0], edge.Points[1]); got < minimumArrowLeadLength {
		t.Fatalf("source lead length = %d, want at least %d: %v", got, minimumArrowLeadLength, edge.Points)
	}
	if got := stepDirection(edge.Points[0], edge.Points[1]); got != (Point{Y: 1}) {
		t.Fatalf("source direction = %v, want downward: %v", got, edge.Points)
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
