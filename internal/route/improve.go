package route

import (
	"github.com/xshoji/agents-workspace/internal/geom"
	"github.com/xshoji/agents-workspace/internal/layout"
)

func improveRoutes(result *layout.Layout, initial []*Edge, diagnostics *Diagnostics, boundaries groupBoundaryIndex) []*Edge {
	baseline := improveRoutesWithBudget(result, initial, min(4, max(1, len(initial)/10)), diagnostics, boundaries)
	if len(initial) > 60 {
		return baseline
	}
	expanded := improveRoutesWithBudget(result, initial, min(16, max(4, len(initial)/4)), diagnostics, boundaries)
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
		if edge.SelfLoop || edge.Constrained || edge.Optimized || !visibleEdge(edge) || len(edge.Points) < 2 {
			accept(edge)
			continue
		}
		collisions := edgeNodeCollisions(result, edge)
		if edge.Reversed && boundaryOverlaps == 0 && collisions == 0 {
			accept(edge)
			continue
		}
		crossings, overlaps := segments.interactions(edge.Points)
		if boundaryOverlaps == 0 && collisions == 0 && overlaps <= 2 && crossings <= 3 {
			accept(edge)
			continue
		}
		if boundaryOverlaps > 0 {
			detoured := *edge
			detoured.Points = detourGroupBoundaries(result, edge, accepted, boundaries)
			detouredBoundaryOverlaps := boundaries.overlapCount(edge, detoured.Points)
			if detouredBoundaryOverlaps < boundaryOverlaps && edgeNodeCollisions(result, &detoured) <= collisions {
				detoured.GroupDetour = true
				accept(&detoured)
				continue
			}
		}
		if boundaryOverlaps == 0 && fallbackAttempts >= fallbackBudget {
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
				if geom.ContainsInterior(node.Rect, point) {
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
