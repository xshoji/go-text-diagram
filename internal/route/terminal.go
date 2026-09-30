package route

import (
	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/geom"
	"github.com/xshoji/go-text-diagram/internal/layout"
)

const minimumArrowLeadLength = 2

// EnsureArrowLeadSegments repairs arrow terminal geometry after coordinate
// transforms such as compaction, then replaces labels and returns fresh metrics.
func EnsureArrowLeadSegments(result *layout.Layout, routes []*Edge, diagnostics *Diagnostics) Metrics {
	if result == nil {
		return Metrics{}
	}
	boundaries := newGroupBoundaryIndex(result.Groups)
	ensureArrowLeadSegments(result, routes, diagnostics, boundaries)
	resetRouteLabels(routes)
	placeLabelsWithDiagnostics(result, routes, diagnostics)
	return analyzeWithDiagnostics(result, routes, diagnostics, boundaries)
}

// ensureArrowLeadSegments keeps at least one line cell between the final bend
// and every arrowhead attached to a node boundary.
func ensureArrowLeadSegments(result *layout.Layout, routes []*Edge, diagnostics *Diagnostics, boundaries groupBoundaryIndex) {
	if result == nil || result.Metrics.Width <= 0 || result.Metrics.Height <= 0 {
		return
	}
	for _, edge := range routes {
		if !needsArrowLead(edge) {
			continue
		}
		others := make([]*Edge, 0, len(routes)-1)
		for _, other := range routes {
			if other != edge {
				others = append(others, other)
			}
		}
		if candidate := localArrowLead(result, edge, others, boundaries); candidate != nil {
			edge.Points = candidate
			edge.LabelPlaced = false
			edge.LabelRect = layout.Rect{}
			continue
		}
		start := edge.Points[0]
		end := edge.Points[len(edge.Points)-1]
		startDirection, endDirection := terminalDirections(edge)
		if startDirection == (Point{}) {
			startDirection = stepDirection(start, edge.Points[1])
		}
		if endDirection == (Point{}) {
			endDirection = stepDirection(edge.Points[len(edge.Points)-2], end)
		}
		if startDirection == (Point{}) || endDirection == (Point{}) {
			continue
		}
		startLength := 1
		if edge.Kind == diagram.Bidirectional {
			startLength = minimumArrowLeadLength
		}
		startStub := moveBy(start, startDirection, startLength)
		endStub := moveBy(end, Point{X: -endDirection.X, Y: -endDirection.Y}, minimumArrowLeadLength)
		if !pointsWithin([]Point{start, startStub, endStub, end}, result.Metrics.Width, result.Metrics.Height) {
			continue
		}
		search := searchOrthogonalPath(searchGrid{
			width:             result.Metrics.Width,
			height:            result.Metrics.Height,
			direction:         result.Direction,
			start:             startStub,
			goal:              endStub,
			obstacles:         obstacleRects(result.Nodes),
			blockedSegments:   boundaries.segmentsForEdge(edge),
			occupied:          buildOccupancy(others),
			maximumExpansions: pathSearchBudget(result.Metrics.Width, result.Metrics.Height),
		})
		recordPathSearch(diagnostics, search)
		if !search.found {
			continue
		}
		candidate := normalize(append(append([]Point{start}, search.points...), end))
		if edgeNodeCollisionsForPoints(result, edge, candidate) != 0 ||
			boundaries.overlapCount(edge, candidate) != 0 {
			continue
		}
		edge.Points = candidate
		edge.LabelPlaced = false
		edge.LabelRect = layout.Rect{}
		edge.Fallback = true
	}
}

func localArrowLead(result *layout.Layout, edge *Edge, others []*Edge, boundaries groupBoundaryIndex) []Point {
	candidates := [][]Point{edge.Points}
	segments := make(segmentCountIndex)
	for _, other := range others {
		segments.addEdge(other)
	}
	startDirection, endDirection := terminalDirections(edge)
	if edge.Kind == diagram.Bidirectional &&
		(geom.Manhattan(edge.Points[0], edge.Points[1]) < minimumArrowLeadLength ||
			startDirection != (Point{}) && stepDirection(edge.Points[0], edge.Points[1]) != startDirection) {
		var expanded [][]Point
		for _, points := range candidates {
			reversed := append([]Point(nil), points...)
			reversePoints(reversed)
			for length := minimumArrowLeadLength; length <= minimumArrowLeadLength+4; length++ {
				direction := stepDirection(reversed[len(reversed)-2], reversed[len(reversed)-1])
				if startDirection != (Point{}) {
					direction = Point{X: -startDirection.X, Y: -startDirection.Y}
				}
				for _, candidate := range extendTerminalLead(reversed, direction, length) {
					reversePoints(candidate)
					expanded = append(expanded, candidate)
				}
			}
		}
		candidates = expanded
	}
	last := len(edge.Points) - 1
	if geom.Manhattan(edge.Points[last-1], edge.Points[last]) < minimumArrowLeadLength ||
		endDirection != (Point{}) && stepDirection(edge.Points[last-1], edge.Points[last]) != endDirection {
		var expanded [][]Point
		for _, points := range candidates {
			for length := minimumArrowLeadLength; length <= minimumArrowLeadLength+4; length++ {
				direction := stepDirection(points[len(points)-2], points[len(points)-1])
				if endDirection != (Point{}) {
					direction = endDirection
				}
				expanded = append(expanded, extendTerminalLead(points, direction, length)...)
			}
		}
		candidates = expanded
	}

	var best []Point
	bestPenalty := int(^uint(0) >> 1)
	for _, candidate := range candidates {
		if !arrowLeadsSatisfied(edge, candidate) || !pointsWithin(candidate, result.Metrics.Width, result.Metrics.Height) ||
			edgeNodeCollisionsForPoints(result, edge, candidate) != 0 ||
			boundaries.overlapCount(edge, candidate) != 0 {
			continue
		}
		penalty := pathInteractionPenalty(candidate, segments)*100 + max(0, len(candidate)-2)*5 + pathLength(candidate)
		if penalty < bestPenalty {
			best, bestPenalty = candidate, penalty
		}
	}
	return best
}

func extendTerminalLead(points []Point, direction Point, length int) [][]Point {
	if len(points) < 3 {
		return nil
	}
	last := len(points) - 1
	end := points[last]
	stub := moveBy(end, Point{X: -direction.X, Y: -direction.Y}, length)
	anchor := points[last-2]
	prefix := points[:last-1]
	return [][]Point{
		normalize(append(append(append([]Point(nil), prefix...), Point{X: stub.X, Y: anchor.Y}, stub), end)),
		normalize(append(append(append([]Point(nil), prefix...), Point{X: anchor.X, Y: stub.Y}, stub), end)),
	}
}

func arrowLeadsSatisfied(edge *Edge, points []Point) bool {
	if len(points) < 2 {
		return false
	}
	last := len(points) - 1
	if geom.Manhattan(points[last-1], points[last]) < minimumArrowLeadLength {
		return false
	}
	startDirection, endDirection := terminalDirections(edge)
	if endDirection != (Point{}) && stepDirection(points[last-1], points[last]) != endDirection {
		return false
	}
	return edge.Kind != diagram.Bidirectional ||
		geom.Manhattan(points[0], points[1]) >= minimumArrowLeadLength &&
			(startDirection == (Point{}) || stepDirection(points[0], points[1]) == startDirection)
}

func needsArrowLead(edge *Edge) bool {
	if edge == nil || len(edge.Points) < 2 || edge.Kind == diagram.Undirected ||
		edge.ArrowStyle == diagram.ArrowNone || !visibleEdge(edge) ||
		edge.Source.Kind != diagram.EndpointNode || edge.Target.Kind != diagram.EndpointNode {
		return false
	}
	last := len(edge.Points) - 1
	startDirection, endDirection := terminalDirections(edge)
	if geom.Manhattan(edge.Points[last-1], edge.Points[last]) < minimumArrowLeadLength ||
		endDirection != (Point{}) && stepDirection(edge.Points[last-1], edge.Points[last]) != endDirection {
		return true
	}
	return edge.Kind == diagram.Bidirectional &&
		(geom.Manhattan(edge.Points[0], edge.Points[1]) < minimumArrowLeadLength ||
			startDirection != (Point{}) && stepDirection(edge.Points[0], edge.Points[1]) != startDirection)
}

func terminalDirections(edge *Edge) (Point, Point) {
	if edge.Reversed || edge.SelfLoop {
		return Point{}, Point{}
	}
	start := sideDirection(edge.StartSide)
	end := sideDirection(edge.EndSide)
	return start, Point{X: -end.X, Y: -end.Y}
}

func sideDirection(side diagram.Side) Point {
	switch side {
	case diagram.SideNorth:
		return Point{Y: -1}
	case diagram.SideEast:
		return Point{X: 1}
	case diagram.SideSouth:
		return Point{Y: 1}
	case diagram.SideWest:
		return Point{X: -1}
	default:
		return Point{}
	}
}

func moveBy(point, direction Point, distance int) Point {
	return Point{X: point.X + direction.X*distance, Y: point.Y + direction.Y*distance}
}
