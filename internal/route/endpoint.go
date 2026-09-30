package route

import (
	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/geom"
	"github.com/xshoji/go-text-diagram/internal/layout"
)

func applyStructuredEndpoints(result *layout.Layout, routes []*Edge) {
	groups := make(map[string]layout.Rect, len(result.Groups))
	for _, group := range result.Groups {
		groups[group.ID] = group.Rect
	}
	for _, edge := range routes {
		if edge.Source.Kind == diagram.EndpointGroup {
			edge.Points = trimFromRect(edge.Points, groups[edge.Source.ID()])
			edge.From = "group:" + edge.Source.ID()
			edge.Constrained = true
		}
		if edge.Target.Kind == diagram.EndpointGroup {
			reversePoints(edge.Points)
			edge.Points = trimFromRect(edge.Points, groups[edge.Target.ID()])
			reversePoints(edge.Points)
			edge.To = "group:" + edge.Target.ID()
			edge.Constrained = true
		}
		if edge.Source.Cell != "" || edge.Target.Cell != "" {
			edge.Constrained = true
		}
	}
}

func trimFromRect(points []Point, rect layout.Rect) []Point {
	if len(points) < 2 || rect.Width <= 0 || rect.Height <= 0 {
		return points
	}
	expanded := expandPoints(points)
	for index, point := range expanded {
		if onRectBoundary(rect, point) {
			return normalize(expanded[index:])
		}
	}
	if geom.Contains(rect, points[0]) {
		boundary := nearestBoundaryPoint(rect, points[0])
		return normalize(append([]Point{boundary}, points...))
	}
	return points
}

func nearestBoundaryPoint(rect layout.Rect, point Point) Point {
	type candidate struct {
		point    Point
		distance int
	}
	candidates := []candidate{
		{point: Point{X: point.X, Y: rect.Y}, distance: point.Y - rect.Y},
		{point: Point{X: rect.X + rect.Width - 1, Y: point.Y}, distance: rect.X + rect.Width - 1 - point.X},
		{point: Point{X: point.X, Y: rect.Y + rect.Height - 1}, distance: rect.Y + rect.Height - 1 - point.Y},
		{point: Point{X: rect.X, Y: point.Y}, distance: point.X - rect.X},
	}
	best := candidates[0]
	for _, current := range candidates[1:] {
		if current.distance < best.distance {
			best = current
		}
	}
	return best.point
}

func expandPoints(points []Point) []Point {
	result := []Point{points[0]}
	for index := 1; index < len(points); index++ {
		from, to := points[index-1], points[index]
		dx, dy := endpointSign(to.X-from.X), endpointSign(to.Y-from.Y)
		for from != to {
			from.X += dx
			from.Y += dy
			result = append(result, from)
		}
	}
	return result
}

func endpointSign(value int) int {
	if value < 0 {
		return -1
	}
	if value > 0 {
		return 1
	}
	return 0
}

func onRectBoundary(rect layout.Rect, point Point) bool {
	if !geom.Contains(rect, point) {
		return false
	}
	return point.X == rect.X || point.X == rect.X+rect.Width-1 ||
		point.Y == rect.Y || point.Y == rect.Y+rect.Height-1
}

func reversePoints(points []Point) {
	for left, right := 0, len(points)-1; left < right; left, right = left+1, right-1 {
		points[left], points[right] = points[right], points[left]
	}
}
