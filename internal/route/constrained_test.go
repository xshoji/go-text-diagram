package route

import (
	"testing"

	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/layout"
)

func TestConstrainedPortPointsKeepsExplicitSideAtCorner(t *testing.T) {
	t.Parallel()

	source := &layout.Node{ID: "source", Rect: layout.Rect{X: 1, Y: 1, Width: 5, Height: 5}}
	target := &layout.Node{ID: "target", Rect: layout.Rect{X: 10, Y: 4, Width: 3, Height: 3}}
	result := &layout.Layout{
		Direction: diagram.DirectionRight,
		Metrics:   layout.Metrics{Width: 16, Height: 12},
		Nodes:     []*layout.Node{source, target},
	}
	edge := &layout.Edge{
		ID: "edge", From: source.ID, To: target.ID,
		Start:  diagram.PortHint{Side: diagram.SideSouth},
		Source: diagram.NodeEndpoint(source.ID), Target: diagram.NodeEndpoint(target.ID),
	}
	start := Point{X: source.Rect.X + source.Rect.Width - 1, Y: source.Rect.Y + source.Rect.Height - 1}
	end := Point{X: target.Rect.X, Y: target.Rect.Y + 1}

	points := constrainedPortPoints(result, edge, source, target, start, end, nil)
	if len(points) < 2 || points[0] != start || points[1] != (Point{X: start.X, Y: start.Y + 1}) {
		t.Fatalf("explicit south port at corner routed as %v", points)
	}
}
