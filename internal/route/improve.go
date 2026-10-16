package route

import (
	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/geom"
	"github.com/xshoji/go-text-diagram/internal/layout"
)

const (
	maximumFanInSwaps      = 16
	maximumFanInExpansions = 100_000
)

// Source-side optimization can change the approach order after ports were
// assigned. Exchange automatic target slots only when the whole diagram improves.
func untangleFanIn(result *layout.Layout, routes []*Edge, diagnostics *Diagnostics, boundaries groupBoundaryIndex) {
	fixedTargets := make(map[string]bool)
	for _, edge := range result.Edges {
		if !automaticNodeEndpoint(edge.Target, edge.End) {
			fixedTargets[edge.OriginalEdgeID] = true
		}
	}
	attempts := 0
	expansions := maximumFanInExpansions
	var baseline Metrics
	measured := false
	for i := range routes {
		for j := i + 1; j < len(routes); j++ {
			left, right := routes[i], routes[j]
			if left.To != right.To || left.From == right.From || fixedTargets[left.ID] || fixedTargets[right.ID] ||
				left.Constrained || right.Constrained || left.Reversed || right.Reversed ||
				left.SelfLoop || right.SelfLoop || !visibleEdge(left) || !visibleEdge(right) ||
				len(left.Points) < 2 || len(right.Points) < 2 ||
				!automaticNodeEndpoint(left.Target, diagram.PortHint{}) || !automaticNodeEndpoint(right.Target, diagram.PortHint{}) {
				continue
			}
			leftEnd, rightEnd := left.Points[len(left.Points)-1], right.Points[len(right.Points)-1]
			leftDirection := stepDirection(left.Points[len(left.Points)-2], leftEnd)
			rightDirection := stepDirection(right.Points[len(right.Points)-2], rightEnd)
			if leftDirection != rightDirection || leftEnd == rightEnd {
				continue
			}
			if crossings, _ := edgeInteractions(left, []*Edge{right}); crossings == 0 {
				continue
			}
			if attempts == maximumFanInSwaps {
				return
			}
			attempts++
			paths := [][2][]Point{{retargetPort(left.Points, rightEnd), retargetPort(right.Points, leftEnd)}}
			if len(paths[0][0]) == 4 && len(paths[0][1]) == 4 {
				// Port order and trunk order must agree to remove a crossing.
				first := append([]Point(nil), paths[0][0]...)
				second := append([]Point(nil), paths[0][1]...)
				if leftDirection.X != 0 {
					first[1].X, second[1].X = second[1].X, first[1].X
					first[2].X, second[2].X = second[2].X, first[2].X
				} else {
					first[1].Y, second[1].Y = second[1].Y, first[1].Y
					first[2].Y, second[2].Y = second[2].Y, first[2].Y
				}
				paths = append(paths, [2][]Point{normalize(first), normalize(second)})
			}
			for variant, pair := range paths {
				candidate := cloneRoutesWithDiagnostics(routes, diagnostics)
				candidate[i].Points, candidate[j].Points = pair[0], pair[1]
				if !sameTerminalDirections(left.Points, candidate[i].Points) || !sameTerminalDirections(right.Points, candidate[j].Points) ||
					!arrowLeadsSatisfied(candidate[i], candidate[i].Points) || !arrowLeadsSatisfied(candidate[j], candidate[j].Points) ||
					hitsEndpointNodes(result, left, candidate[i].Points) || hitsEndpointNodes(result, right, candidate[j].Points) {
					if variant != 0 {
						continue // Both variants share the same A* fallback.
					}
					others := make([]*Edge, 0, len(routes)-2)
					for k, edge := range routes {
						if k != i && k != j {
							others = append(others, edge)
						}
					}
					candidate[i].Points = fanInPath(result, left, rightEnd, others, boundaries, &expansions, diagnostics)
					if len(candidate[i].Points) == 0 {
						continue
					}
					others = append(others, candidate[i])
					candidate[j].Points = fanInPath(result, right, leftEnd, others, boundaries, &expansions, diagnostics)
					if len(candidate[j].Points) == 0 {
						continue
					}
				}
				if !orthogonalPoints(candidate[i].Points) || !orthogonalPoints(candidate[j].Points) ||
					!pointsWithin(candidate[i].Points, result.Metrics.Width, result.Metrics.Height) ||
					!pointsWithin(candidate[j].Points, result.Metrics.Width, result.Metrics.Height) {
					continue
				}
				if !measured {
					baseline = analyzeWithDiagnostics(result, routes, diagnostics, boundaries)
					measured = true
				}
				resetRouteLabels(candidate)
				placeLabelsWithDiagnostics(result, candidate, diagnostics)
				metrics := analyzeWithDiagnostics(result, candidate, diagnostics, boundaries)
				if metrics.Crossings >= baseline.Crossings || metrics.Overlaps > baseline.Overlaps ||
					metrics.EdgeNodeCollisions > baseline.EdgeNodeCollisions || metrics.GroupBoundaryOverlaps > baseline.GroupBoundaryOverlaps ||
					metrics.LabelNodeCollisions > baseline.LabelNodeCollisions || metrics.LabelLabelCollisions > baseline.LabelLabelCollisions ||
					metrics.LabelEdgeCollisions > baseline.LabelEdgeCollisions || metrics.UnplacedLabels > baseline.UnplacedLabels ||
					metrics.ReverseMoves > baseline.ReverseMoves ||
					CompareQuality(MeasureQuality(result, metrics, candidate), MeasureQuality(result, baseline, routes)) >= 0 {
					continue
				}
				copy(routes, candidate)
				baseline = metrics
			}
		}
	}
}

// Collision metrics exclude endpoint nodes; only the attachment cells may touch them.
func hitsEndpointNodes(result *layout.Layout, edge *Edge, points []Point) bool {
	for _, node := range result.Nodes {
		if node.Dummy || node.ID != edge.From && node.ID != edge.To {
			continue
		}
		for i := 1; i < len(points); i++ {
			from, to := points[i-1], points[i]
			direction := stepDirection(from, to)
			if i == 1 {
				from = moveBy(from, direction, 1)
			}
			if i == len(points)-1 {
				to = moveBy(to, direction, -1)
			}
			segment := geom.Rect{X: min(from.X, to.X), Y: min(from.Y, to.Y), Width: abs(to.X-from.X) + 1, Height: abs(to.Y-from.Y) + 1}
			if geom.Overlaps(segment, node.Rect) {
				return true
			}
		}
	}
	return false
}

func fanInPath(result *layout.Layout, edge *Edge, end Point, others []*Edge, boundaries groupBoundaryIndex, budget *int, diagnostics *Diagnostics) []Point {
	if *budget <= 0 {
		return nil
	}
	start := edge.Points[0]
	startStub := moveBy(start, stepDirection(start, edge.Points[1]), minimumArrowLeadLength)
	last := len(edge.Points) - 1
	direction := stepDirection(edge.Points[last-1], edge.Points[last])
	endStub := moveBy(end, Point{X: -direction.X, Y: -direction.Y}, minimumArrowLeadLength)
	if !pointsWithin([]Point{start, startStub, endStub, end}, result.Metrics.Width, result.Metrics.Height) {
		return nil
	}
	search := searchOrthogonalPath(searchGrid{
		width: result.Metrics.Width, height: result.Metrics.Height, direction: result.Direction,
		start: startStub, goal: endStub, obstacles: obstacleRects(result.Nodes),
		blockedSegments: boundaries.segmentsForEdge(edge), occupied: buildOccupancy(others),
		maximumExpansions: min(*budget, pathSearchBudget(result.Metrics.Width, result.Metrics.Height)),
	})
	*budget -= search.gridExpansions
	recordPathSearch(diagnostics, search)
	if !search.found {
		return nil
	}
	points := normalize(append(append([]Point{start}, search.points...), end))
	if !sameTerminalDirections(edge.Points, points) || !arrowLeadsSatisfied(edge, points) || hitsEndpointNodes(result, edge, points) {
		return nil
	}
	return points
}

func retargetPort(points []Point, end Point) []Point {
	last := len(points) - 1
	direction := stepDirection(points[last-1], points[last])
	if len(points) == 2 {
		stub := moveBy(end, Point{X: -direction.X, Y: -direction.Y}, minimumArrowLeadLength)
		bend := Point{X: stub.X, Y: points[0].Y}
		if direction.Y != 0 {
			bend = Point{X: points[0].X, Y: stub.Y}
		}
		return normalize([]Point{points[0], bend, stub, end})
	}
	result := append([]Point(nil), points...)
	result[last] = end
	if direction.X != 0 {
		result[last-1].Y = end.Y
	} else {
		result[last-1].X = end.X
	}
	return normalize(result)
}

func improveRoutes(result *layout.Layout, initial []*Edge, diagnostics *Diagnostics, boundaries groupBoundaryIndex) []*Edge {
	baseline := improveRoutesWithBudget(result, initial, min(8, max(1, len(initial)/4)), diagnostics, boundaries)
	if len(initial) > 60 {
		return baseline
	}
	expanded := improveRoutesWithBudget(result, initial, min(24, max(8, len(initial)/2)), diagnostics, boundaries)
	baselineMetrics := analyzeWithGroupBoundaries(result, baseline, boundaries)
	expandedMetrics := analyzeWithGroupBoundaries(result, expanded, boundaries)
	if diagnostics != nil {
		diagnostics.FullEvaluations += 2
	}
	if CompareQuality(MeasureQuality(result, expandedMetrics, expanded), MeasureQuality(result, baselineMetrics, baseline)) < 0 {
		return expanded
	}
	return baseline
}

func improveRoutesWithBudget(result *layout.Layout, initial []*Edge, fallbackBudget int, diagnostics *Diagnostics, boundaries groupBoundaryIndex) []*Edge {
	accepted := make([]*Edge, 0, len(initial))
	segments := make(segmentCountIndex)
	accept := func(edge *Edge) {
		accepted = append(accepted, edge)
		segments.addEdge(edge)
	}
	fallbackAttempts := 0
	for _, edge := range initial {
		boundaryOverlaps := boundaries.overlapCount(edge, edge.Points)
		if edge.SelfLoop || !visibleEdge(edge) || len(edge.Points) < 2 {
			accept(edge)
			continue
		}
		collisions := edgeNodeCollisions(result, edge)
		if edge.Reversed && boundaryOverlaps == 0 && collisions == 0 {
			accept(edge)
			continue
		}
		crossings, overlaps := segments.interactions(edge.Points)
		if boundaryOverlaps == 0 && collisions == 0 && overlaps == 0 && crossings == 0 {
			accept(edge)
			continue
		}
		if edge.Constrained && boundaryOverlaps == 0 && collisions == 0 {
			accept(edge)
			continue
		}
		if boundaryOverlaps > 0 {
			detoured := *edge
			detoured.Points = detourGroupBoundaries(result, edge, accepted, boundaries)
			detouredBoundaryOverlaps := boundaries.overlapCount(edge, detoured.Points)
			if detouredBoundaryOverlaps < boundaryOverlaps && edgeNodeCollisions(result, &detoured) == 0 {
				detoured.GroupDetour = true
				accept(&detoured)
				continue
			}
		}
		if boundaryOverlaps == 0 && collisions == 0 && fallbackAttempts >= fallbackBudget {
			accept(edge)
			continue
		}
		if boundaryOverlaps == 0 {
			fallbackAttempts++
		}

		search := findPath(result, edge, accepted)
		recordPathSearch(diagnostics, search)
		if !search.found {
			accept(edge)
			continue
		}
		candidate := *edge
		candidate.Points = search.points
		candidate.Fallback = true
		candidateBoundaryOverlaps := boundaries.overlapCount(edge, candidate.Points)
		if candidateBoundaryOverlaps < boundaryOverlaps ||
			candidateBoundaryOverlaps == boundaryOverlaps && (edgeNodeCollisions(result, &candidate) < collisions ||
				localRoutePenalty(result, &candidate, segments, boundaries) < localRoutePenalty(result, edge, segments, boundaries)) {
			accept(&candidate)
		} else {
			accept(edge)
		}
	}
	return accepted
}

func edgeNodeCollisions(result *layout.Layout, edge *Edge) int {
	collided := make(map[string]bool)
	for index := 1; index < len(edge.Points); index++ {
		walkPoints(edge.Points[index-1], edge.Points[index], func(point Point) {
			for _, node := range result.Nodes {
				if node.Dummy || node.ID == edge.From || node.ID == edge.To || collided[node.ID] {
					continue
				}
				if geom.Contains(node.Rect, point) {
					collided[node.ID] = true
				}
			}
		})
	}
	return len(collided)
}

func edgeInteractions(edge *Edge, accepted []*Edge) (int, int) {
	segments := make(segmentCountIndex)
	for _, existing := range accepted {
		segments.addEdge(existing)
	}
	return segments.interactions(edge.Points)
}

func localRoutePenalty(result *layout.Layout, edge *Edge, segments segmentCountIndex, boundaries groupBoundaryIndex) int {
	crossings, overlaps := segments.interactions(edge.Points)
	length := 0
	for index := 1; index < len(edge.Points); index++ {
		length += geom.Manhattan(edge.Points[index-1], edge.Points[index])
	}
	return boundaries.overlapCount(edge, edge.Points)*500 + edgeNodeCollisions(result, edge)*1000 +
		overlaps*30 + crossings*20 + max(0, len(edge.Points)-2)*5 + length
}
