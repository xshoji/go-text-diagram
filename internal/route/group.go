package route

import (
	"github.com/xshoji/agents-workspace/internal/diagram"
	"github.com/xshoji/agents-workspace/internal/geom"
	"github.com/xshoji/agents-workspace/internal/layout"
)

type groupBoundaryIndex map[unitSegment]map[string]struct{}

func newGroupBoundaryIndex(groups []*layout.Group) groupBoundaryIndex {
	boundaries := make(groupBoundaryIndex)
	for _, group := range groups {
		if group == nil || group.Rect.Width <= 0 || group.Rect.Height <= 0 {
			continue
		}
		left, top := group.Rect.X, group.Rect.Y
		right := left + group.Rect.Width - 1
		bottom := top + group.Rect.Height - 1
		for _, side := range [][2]Point{
			{{X: left, Y: top}, {X: right, Y: top}},
			{{X: right, Y: top}, {X: right, Y: bottom}},
			{{X: right, Y: bottom}, {X: left, Y: bottom}},
			{{X: left, Y: bottom}, {X: left, Y: top}},
		} {
			walkSegment(side[0], side[1], func(from, to Point) {
				segment := canonicalSegment(from, to)
				if boundaries[segment] == nil {
					boundaries[segment] = make(map[string]struct{})
				}
				boundaries[segment][group.ID] = struct{}{}
			})
		}
	}
	return boundaries
}

func (boundaries groupBoundaryIndex) segmentsForEdge(edge *Edge) map[unitSegment]bool {
	segments := make(map[unitSegment]bool, len(boundaries))
	for segment, owners := range boundaries {
		if boundaryAppliesToEdge(owners, edge) {
			segments[segment] = true
		}
	}
	return segments
}

func boundaryAppliesToEdge(owners map[string]struct{}, edge *Edge) bool {
	for groupID := range owners {
		if edge == nil ||
			!(edge.Source.Kind == diagram.EndpointGroup && edge.Source.ID() == groupID) &&
				!(edge.Target.Kind == diagram.EndpointGroup && edge.Target.ID() == groupID) {
			return true
		}
	}
	return false
}

func (boundaries groupBoundaryIndex) overlapCount(edge *Edge, points []Point) int {
	count := 0
	for index := 1; index < len(points); index++ {
		walkSegment(points[index-1], points[index], func(from, to Point) {
			if boundaryAppliesToEdge(boundaries[canonicalSegment(from, to)], edge) {
				count++
			}
		})
	}
	return count
}

// AvoidGroupBoundaryOverlaps repairs routes after a geometry-wide coordinate
// transform such as compaction. Perpendicular crossings remain valid; only
// segments that run along a group border are moved into a neighboring channel.
func AvoidGroupBoundaryOverlaps(result *layout.Layout, edges []*Edge) {
	boundaries := newGroupBoundaryIndex(result.Groups)
	for _, edge := range edges {
		if edge == nil || !visibleEdge(edge) || len(edge.Points) < 2 {
			continue
		}
		before := boundaries.overlapCount(edge, edge.Points)
		if before == 0 {
			continue
		}
		otherEdges := make([]*Edge, 0, len(edges)-1)
		for _, other := range edges {
			if other != edge {
				otherEdges = append(otherEdges, other)
			}
		}
		points := detourGroupBoundaries(result, edge, otherEdges, boundaries)
		if boundaries.overlapCount(edge, points) < before {
			edge.Points = points
			edge.GroupDetour = true
		}
	}
	for _, edge := range edges {
		edge.LabelPlaced = false
		edge.LabelRect = layout.Rect{}
	}
	placeLabels(result, edges)
}

func detourGroupBoundaries(result *layout.Layout, edge *Edge, otherEdges []*Edge, boundaries groupBoundaryIndex) []Point {
	points := append([]Point(nil), edge.Points...)
	segments := make(segmentCountIndex)
	for _, other := range otherEdges {
		segments.addEdge(other)
	}
	blocked := boundaries.segmentsForEdge(edge)
	for attempt := 0; attempt < 16; attempt++ {
		segmentIndex := -1
		for index := 1; index < len(points); index++ {
			overlaps := false
			walkSegment(points[index-1], points[index], func(from, to Point) {
				overlaps = overlaps || blocked[canonicalSegment(from, to)]
			})
			if overlaps {
				segmentIndex = index
				break
			}
		}
		if segmentIndex < 0 {
			break
		}

		currentOverlaps := boundaries.overlapCount(edge, points)
		currentCollisions := edgeNodeCollisionsForPoints(result, edge, points)
		best := points
		bestOverlaps := currentOverlaps
		bestInteractions := pathInteractionPenalty(points, segments)
		bestLength := pathLength(points)
		from, to := points[segmentIndex-1], points[segmentIndex]
		for _, offset := range []int{-1, 1, -2, 2, -3, 3} {
			shiftedFrom, shiftedTo := from, to
			if from.Y == to.Y {
				shiftedFrom.Y += offset
				shiftedTo.Y += offset
			} else {
				shiftedFrom.X += offset
				shiftedTo.X += offset
			}
			candidate := make([]Point, 0, len(points)+2)
			candidate = append(candidate, points[:segmentIndex]...)
			candidate = append(candidate, shiftedFrom, shiftedTo)
			candidate = append(candidate, points[segmentIndex:]...)
			candidate = normalize(candidate)
			if !pointsWithin(candidate, result.Metrics.Width, result.Metrics.Height) ||
				edgeNodeCollisionsForPoints(result, edge, candidate) > currentCollisions ||
				(edge.Constrained || edge.AdaptiveStart) && !sameTerminalDirections(edge.Points, candidate) {
				continue
			}
			overlaps := boundaries.overlapCount(edge, candidate)
			interactions := pathInteractionPenalty(candidate, segments)
			length := pathLength(candidate)
			if overlaps < bestOverlaps || overlaps == bestOverlaps && (interactions < bestInteractions ||
				interactions == bestInteractions && length < bestLength) {
				best, bestOverlaps, bestInteractions, bestLength = candidate, overlaps, interactions, length
			}
		}
		if bestOverlaps >= currentOverlaps {
			break
		}
		points = best
	}
	return points
}

func sameTerminalDirections(original, candidate []Point) bool {
	if len(original) < 2 || len(candidate) < 2 {
		return len(original) == len(candidate)
	}
	return stepDirection(original[0], original[1]) == stepDirection(candidate[0], candidate[1]) &&
		stepDirection(original[len(original)-2], original[len(original)-1]) ==
			stepDirection(candidate[len(candidate)-2], candidate[len(candidate)-1])
}

func stepDirection(from, to Point) Point {
	return Point{X: axisSign(to.X - from.X), Y: axisSign(to.Y - from.Y)}
}

func pathInteractionPenalty(points []Point, segments segmentCountIndex) int {
	crossings, overlaps := segments.interactions(points)
	return overlaps*30 + crossings*20
}

func edgeNodeCollisionsForPoints(result *layout.Layout, edge *Edge, points []Point) int {
	candidate := *edge
	candidate.Points = points
	return edgeNodeCollisions(result, &candidate)
}

func pointsWithin(points []Point, width, height int) bool {
	for _, point := range points {
		if point.X < 0 || point.X >= width || point.Y < 0 || point.Y >= height {
			return false
		}
	}
	return true
}

func pathLength(points []Point) int {
	length := 0
	for index := 1; index < len(points); index++ {
		length += geom.Manhattan(points[index-1], points[index])
	}
	return length
}
