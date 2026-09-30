package route

import "github.com/xshoji/go-text-diagram/internal/layout"

const maximumLongRouteExpansions = 1_000_000

// optimizeLongRoutes treats dummy-node routes as safe baselines rather than
// mandatory waypoints. Once all final node and group coordinates are known, it
// tries a direct obstacle-aware route between the established endpoint ports.
func optimizeLongRoutes(result *layout.Layout, routes []*Edge, diagnostics *Diagnostics, boundaries groupBoundaryIndex) []*Edge {
	if result == nil || result.Metrics.Width <= 0 || result.Metrics.Height <= 0 {
		return routes
	}

	area := int64(result.Metrics.Width) * int64(result.Metrics.Height)
	perSearch := pathSearchBudget(result.Metrics.Width, result.Metrics.Height)
	remaining := cappedExpansionBudget(area * 16)
	if remaining > maximumLongRouteExpansions {
		remaining = maximumLongRouteExpansions
	}

	accepted := make([]*Edge, 0, len(routes))
	geometry := newAdaptiveGeometrySnapshot(routes)
	for _, edge := range routes {
		if !edge.Long || edge.Reversed || edge.SelfLoop || !visibleEdge(edge) {
			accepted = append(accepted, edge)
		}
	}

	for _, edge := range routes {
		if !edge.Long || edge.Reversed || edge.SelfLoop || !visibleEdge(edge) {
			continue
		}
		if remaining < 64 {
			accepted = append(accepted, edge)
			continue
		}

		maximumExpansions := min(perSearch, remaining)
		search := findLongPath(result, edge, accepted, boundaries, maximumExpansions)
		remaining -= search.gridExpansions
		recordPathSearch(diagnostics, search)
		if search.found && preferLongCandidate(result, edge, search.points, geometry, boundaries) {
			geometry.remove(edge.ID)
			edge.Points = search.points
			edge.Optimized = true
			geometry.add(edge)
		}
		accepted = append(accepted, edge)
	}
	return routes
}

func cappedExpansionBudget(value int64) int {
	maximumInt := int64(^uint(0) >> 1)
	if value > maximumInt {
		return int(maximumInt)
	}
	if value < 0 {
		return 0
	}
	return int(value)
}

func findLongPath(result *layout.Layout, edge *Edge, accepted []*Edge, boundaries groupBoundaryIndex, maximumExpansions int) pathSearchResult {
	if edge == nil || len(edge.Points) < 2 || maximumExpansions <= 0 {
		return pathSearchResult{}
	}

	start := edge.Points[0]
	end := edge.Points[len(edge.Points)-1]
	startDirection := stepDirection(start, edge.Points[1])
	endDirection := stepDirection(edge.Points[len(edge.Points)-2], end)
	if startDirection == (Point{}) || endDirection == (Point{}) {
		return pathSearchResult{}
	}
	startStub := Point{X: start.X + startDirection.X, Y: start.Y + startDirection.Y}
	endStub := Point{X: end.X - endDirection.X, Y: end.Y - endDirection.Y}
	if !pointsWithin([]Point{start, startStub, endStub, end}, result.Metrics.Width, result.Metrics.Height) {
		return pathSearchResult{}
	}

	search := searchOrthogonalPath(searchGrid{
		width:             result.Metrics.Width,
		height:            result.Metrics.Height,
		direction:         result.Direction,
		start:             startStub,
		goal:              endStub,
		obstacles:         obstacleRects(result.Nodes),
		blockedSegments:   boundaries.segmentsForEdge(edge),
		occupied:          buildOccupancy(accepted),
		maximumExpansions: maximumExpansions,
	})
	if !search.found {
		return search
	}
	search.points = normalize(append(append([]Point{start}, search.points...), end))
	return search
}

func preferLongCandidate(result *layout.Layout, edge *Edge, points []Point, geometry *adaptiveGeometrySnapshot, boundaries groupBoundaryIndex) bool {
	if len(points) < 2 || points[0] != edge.Points[0] || points[len(points)-1] != edge.Points[len(edge.Points)-1] ||
		!sameTerminalDirections(edge.Points, points) || !orthogonalPoints(points) ||
		!pointsWithin(points, result.Metrics.Width, result.Metrics.Height) {
		return false
	}

	baselineCollisions := edgeNodeCollisionsForPoints(result, edge, edge.Points)
	candidateCollisions := edgeNodeCollisionsForPoints(result, edge, points)
	baselineBoundaries := boundaries.overlapCount(edge, edge.Points)
	candidateBoundaries := boundaries.overlapCount(edge, points)
	if candidateCollisions > baselineCollisions || candidateBoundaries > baselineBoundaries {
		return false
	}

	view := adaptiveGeometryView{base: geometry}
	baselineCrossings, baselineOverlaps := view.interactions(edge.ID, edge.Points)
	candidateCrossings, candidateOverlaps := view.interactions(edge.ID, points)
	if candidateCrossings > baselineCrossings || candidateOverlaps > baselineOverlaps {
		return false
	}
	if candidateCrossings+candidateOverlaps != baselineCrossings+baselineOverlaps {
		return candidateCrossings+candidateOverlaps < baselineCrossings+baselineOverlaps
	}
	baselineGeometry := max(0, len(edge.Points)-2)*5 + pathLength(edge.Points)
	candidateGeometry := max(0, len(points)-2)*5 + pathLength(points)
	return candidateGeometry < baselineGeometry
}

func orthogonalPoints(points []Point) bool {
	for index := 1; index < len(points); index++ {
		if points[index-1].X != points[index].X && points[index-1].Y != points[index].Y {
			return false
		}
	}
	return true
}
