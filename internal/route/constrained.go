package route

import (
	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/geom"
	"github.com/xshoji/go-text-diagram/internal/layout"
)

func constrainedPortPoints(result *layout.Layout, edge *layout.Edge, from, to *layout.Node, start, end Point, diagnostics *Diagnostics) []Point {
	startSide := resolvedSide(edge.Start.Side, false, result.Direction)
	endSide := resolvedSide(edge.End.Side, true, result.Direction)
	startStub := stepOutside(start, startSide)
	endStub := stepOutside(end, endSide)

	candidates := [][]Point{
		{start, startStub, {X: endStub.X, Y: startStub.Y}, endStub, end},
		{start, startStub, {X: startStub.X, Y: endStub.Y}, endStub, end},
	}
	if startStub.X == endStub.X || startStub.Y == endStub.Y {
		candidates = append([][]Point{{start, startStub, endStub, end}}, candidates...)
	}
	middleX := (startStub.X + endStub.X) / 2
	middleY := (startStub.Y + endStub.Y) / 2
	candidates = append(candidates,
		[]Point{start, startStub, {X: middleX, Y: startStub.Y}, {X: middleX, Y: endStub.Y}, endStub, end},
		[]Point{start, startStub, {X: startStub.X, Y: middleY}, {X: endStub.X, Y: middleY}, endStub, end},
	)

	bestScore := int(^uint(0) >> 1)
	var best []Point
	for _, candidate := range candidates {
		candidate = normalize(candidate)
		score := constrainedPathScore(result, candidate, start, end)
		if score < bestScore {
			bestScore, best = score, candidate
		}
	}
	if bestScore < 1000 {
		return best
	}

	search := searchOrthogonalPath(searchGrid{
		width:             result.Metrics.Width,
		height:            result.Metrics.Height,
		direction:         result.Direction,
		start:             startStub,
		goal:              endStub,
		obstacles:         obstacleRects(result.Nodes),
		occupied:          routeOccupancy{segments: make(map[unitSegment]bool), points: make(map[Point]uint8)},
		maximumExpansions: pathSearchBudget(result.Metrics.Width, result.Metrics.Height),
	})
	recordPathSearch(diagnostics, search)
	if search.found {
		return normalize(append(append([]Point{start}, search.points...), end))
	}
	return best
}

func stepOutside(point Point, side diagram.Side) Point {
	switch side {
	case diagram.SideNorth:
		point.Y--
	case diagram.SideEast:
		point.X++
	case diagram.SideSouth:
		point.Y++
	case diagram.SideWest:
		point.X--
	}
	return point
}

func constrainedPathScore(result *layout.Layout, points []Point, start, end Point) int {
	collisions, length := 0, 0
	for index := 1; index < len(points); index++ {
		length += geom.Manhattan(points[index-1], points[index])
		walkPoints(points[index-1], points[index], func(point Point) {
			if point == start || point == end {
				return
			}
			for _, node := range result.Nodes {
				if node.Dummy {
					continue
				}
				if point.X >= node.Rect.X && point.X < node.Rect.X+node.Rect.Width &&
					point.Y >= node.Rect.Y && point.Y < node.Rect.Y+node.Rect.Height {
					collisions++
				}
			}
		})
	}
	return collisions*1000 + max(0, len(points)-2)*5 + length
}
