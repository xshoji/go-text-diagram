package route

import (
	"slices"

	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/geom"
	"github.com/xshoji/go-text-diagram/internal/layout"
)

func constrainedPortPoints(result *layout.Layout, edge *layout.Edge, from, to *layout.Node, start, end Point, diagnostics *Diagnostics) []Point {
	endSide := resolvedSide(edge.End.Side, true, result.Direction)
	if edge.End.Side == diagram.SideAuto && edge.Target.PortHint.Side != diagram.SideAuto {
		endSide = resolvedSide(edge.Target.PortHint.Side, true, result.Direction)
	}
	if edge.Target.Cell != "" && edge.End.Side == diagram.SideAuto && edge.Target.PortHint.Side == diagram.SideAuto {
		endSide = endpointSide(to.Rect, end)
	}
	endStub := stepOutside(end, endSide)
	routedEdge := &Edge{Source: edge.Source, Target: edge.Target}
	boundaries := newGroupBoundaryIndex(result.Groups)

	bestScore := int(^uint(0) >> 1)
	var best []Point
	bestPort := start
	initialSide := endpointSide(from.Rect, start)
	if edge.Start.Side != diagram.SideAuto {
		initialSide = resolvedSide(edge.Start.Side, false, result.Direction)
	} else if edge.Source.PortHint.Side != diagram.SideAuto {
		initialSide = resolvedSide(edge.Source.PortHint.Side, false, result.Direction)
	}
	bestStart := stepOutside(start, initialSide)
	endOptions := []Point{end}
	if edge.Target.Cell != "" {
		endOptions = constrainedPorts(to, edge.Target, edge.End, end)
	}
	for _, candidateEnd := range endOptions {
		candidateEndSide := endSide
		if edge.Target.Cell != "" && edge.End.Side == diagram.SideAuto && edge.Target.PortHint.Side == diagram.SideAuto {
			candidateEndSide = endpointSide(to.Rect, candidateEnd)
		}
		candidateEndStub := stepOutside(candidateEnd, candidateEndSide)
		for _, candidateStart := range constrainedPorts(from, edge.Source, edge.Start, start) {
			startSide := endpointSide(from.Rect, candidateStart)
			if edge.Start.Side != diagram.SideAuto {
				startSide = resolvedSide(edge.Start.Side, false, result.Direction)
			} else if edge.Source.PortHint.Side != diagram.SideAuto {
				startSide = resolvedSide(edge.Source.PortHint.Side, false, result.Direction)
			}
			startStub := stepOutside(candidateStart, startSide)
			candidates := [][]Point{
				{candidateStart, startStub, {X: candidateEndStub.X, Y: startStub.Y}, candidateEndStub, candidateEnd},
				{candidateStart, startStub, {X: startStub.X, Y: candidateEndStub.Y}, candidateEndStub, candidateEnd},
			}
			if startStub.X == candidateEndStub.X || startStub.Y == candidateEndStub.Y {
				candidates = append([][]Point{{candidateStart, startStub, candidateEndStub, candidateEnd}}, candidates...)
			}
			middleX := (startStub.X + candidateEndStub.X) / 2
			middleY := (startStub.Y + candidateEndStub.Y) / 2
			candidates = append(candidates,
				[]Point{candidateStart, startStub, {X: middleX, Y: startStub.Y}, {X: middleX, Y: candidateEndStub.Y}, candidateEndStub, candidateEnd},
				[]Point{candidateStart, startStub, {X: startStub.X, Y: middleY}, {X: candidateEndStub.X, Y: middleY}, candidateEndStub, candidateEnd},
			)

			for _, candidate := range candidates {
				candidate = normalize(candidate)
				if len(candidate) < 2 || stepDirection(candidate[0], candidate[1]) != sideStep(startSide) ||
					stepDirection(candidate[len(candidate)-1], candidate[len(candidate)-2]) != sideStep(candidateEndSide) {
					continue
				}
				score := constrainedPathScore(result, candidate, candidateStart, candidateEnd, routedEdge, boundaries)
				if score < bestScore {
					bestScore, best, bestPort, bestStart = score, candidate, candidateStart, startStub
					end, endStub = candidateEnd, candidateEndStub
				}
			}
		}
	}
	if bestScore < 1000 {
		return best
	}

	search := searchOrthogonalPath(searchGrid{
		width:             result.Metrics.Width,
		height:            result.Metrics.Height,
		direction:         result.Direction,
		start:             bestStart,
		goal:              endStub,
		obstacles:         obstacleRects(result.Nodes),
		blockedSegments:   boundaries.segmentsForEdge(routedEdge),
		occupied:          routeOccupancy{segments: make(map[unitSegment]bool), points: make(map[Point]uint8)},
		maximumExpansions: pathSearchBudget(result.Metrics.Width, result.Metrics.Height),
	})
	recordPathSearch(diagnostics, search)
	if search.found {
		return normalize(append(append([]Point{bestPort}, search.points...), end))
	}
	if best == nil {
		return normalize([]Point{bestPort, bestStart, {X: endStub.X, Y: bestStart.Y}, endStub, end})
	}
	return best
}

func constrainedPorts(node *layout.Node, endpoint diagram.Endpoint, hint diagram.PortHint, port Point) []Point {
	if hint.Side != diagram.SideAuto || hint.Offset != nil || endpoint.PortHint.Side != diagram.SideAuto ||
		endpoint.PortHint.Offset != nil || endpoint.NamedPort != "" {
		return []Point{port}
	}
	options := []Point{port}
	cell, cellOK := node.CellRect(endpoint.Cell)
	for _, side := range []diagram.Side{diagram.SideNorth, diagram.SideEast, diagram.SideSouth, diagram.SideWest} {
		point := portAtSide(node.Rect, side, port)
		if cellOK {
			point = cellPort(node.Rect, cell, closestExposedCellSide(node.Rect, cell, side))
		}
		if !slices.Contains(options, point) {
			options = append(options, point)
		}
	}
	return options
}

func portAtSide(rect layout.Rect, side diagram.Side, current Point) Point {
	position := func(start, size, oldStart, oldSize, value int) int {
		if size <= 1 || oldSize <= 1 {
			return start + (size-1)/2
		}
		offset := max(0, min(oldSize-1, value-oldStart))
		position := start + (offset*(size-1)+(oldSize-1)/2)/(oldSize-1)
		minimum, maximum := start+portCornerClearance, start+size-1-portCornerClearance
		if minimum > maximum {
			return start + (size-1)/2
		}
		return max(minimum, min(maximum, position))
	}
	switch side {
	case diagram.SideNorth:
		return Point{X: position(rect.X, rect.Width, rect.Y, rect.Height, current.Y), Y: rect.Y}
	case diagram.SideEast:
		return Point{X: rect.X + rect.Width - 1, Y: position(rect.Y, rect.Height, rect.X, rect.Width, current.X)}
	case diagram.SideSouth:
		return Point{X: position(rect.X, rect.Width, rect.Y, rect.Height, current.Y), Y: rect.Y + rect.Height - 1}
	default:
		return Point{X: rect.X, Y: position(rect.Y, rect.Height, rect.X, rect.Width, current.X)}
	}
}

func endpointSide(rect layout.Rect, point Point) diagram.Side {
	if point.Y == rect.Y {
		return diagram.SideNorth
	}
	if point.X == rect.X+rect.Width-1 {
		return diagram.SideEast
	}
	if point.Y == rect.Y+rect.Height-1 {
		return diagram.SideSouth
	}
	return diagram.SideWest
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

func constrainedPathScore(result *layout.Layout, points []Point, start, end Point, edge *Edge, boundaries groupBoundaryIndex) int {
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
	return (collisions+boundaries.overlapCount(edge, points))*1000 + max(0, len(points)-2)*5 + length
}
