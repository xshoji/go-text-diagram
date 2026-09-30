package route

import (
	"sort"

	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/layout"
)

const maximumAdaptivePortExpansions = 1_000_000
const maximumAdaptivePortCandidates = 12

// AdaptiveCandidate describes one isolated bundle reroute.
type AdaptiveCandidate struct {
	Replacements  []*Edge
	ChangedEdgeID string
	Side          diagram.Side
	Expansions    int
}

type AdaptiveEvaluation struct {
	Quality Quality
	Metrics Metrics
	Accept  bool
	// CommitReplacements may include evaluator-produced label replacements.
	// A nil value commits the routed candidate bundle for legacy evaluators.
	CommitReplacements []*Edge
}

type AdaptiveEvaluator func(base []*Edge, candidate AdaptiveCandidate) AdaptiveEvaluation

// OptimizeAutoStartSidesCOW searches adaptive ports without mutating routes.
// Only bundle members are deep-cloned for each candidate.
func OptimizeAutoStartSidesCOW(result *layout.Layout, routes []*Edge, metrics Metrics, evaluator AdaptiveEvaluator, diagnostics *Diagnostics) ([]*Edge, Metrics) {
	current := append([]*Edge(nil), routes...)
	if result == nil || result.Metrics.Width <= 0 || result.Metrics.Height <= 0 {
		return current, metrics
	}
	nodes := make(map[string]*layout.Node, len(result.Nodes))
	for _, node := range result.Nodes {
		nodes[node.ID] = node
	}
	bundles := adaptiveStartBundles(current, nodes)
	if len(bundles) == 0 {
		return current, metrics
	}

	assignment := make(map[string]diagram.Side)
	eligibleCount := 0
	for _, bundle := range bundles {
		for _, edge := range bundle {
			assignment[edge.ID] = edge.StartSide
			eligibleCount++
		}
	}
	remainingExpansions := maximumAdaptivePortExpansions
	maximumChanges := min(6, max(1, eligibleCount*2))
	groupBoundaries := newGroupBoundaryIndex(result.Groups)
	occupancyWorkspace := &routeOccupancy{}
	for change := 0; change < maximumChanges && remainingExpansions >= 64; change++ {
		geometry := newAdaptiveGeometrySnapshot(current)
		candidateEdges := adaptiveCandidateEdges(current, result, groupBoundaries, geometry, maximumAdaptivePortCandidates)
		if len(candidateEdges) == 0 {
			break
		}
		var best []*Edge
		var bestCommit []*Edge
		var bestQuality Quality
		var bestMetrics Metrics
		bestEdgeID := ""
		bestSide := diagram.SideAuto

		for _, node := range result.Nodes {
			bundle := bundles[node.ID]
			for _, changed := range bundle {
				if !candidateEdges[changed.ID] {
					continue
				}
				for _, side := range []diagram.Side{diagram.SideNorth, diagram.SideEast, diagram.SideSouth, diagram.SideWest} {
					if side == assignment[changed.ID] || remainingExpansions < 64 {
						continue
					}
					candidateAssignment := cloneSideAssignment(assignment)
					candidateAssignment[changed.ID] = side
					candidate := cloneAdaptiveBundle(bundle, current, diagnostics)
					if diagnostics != nil {
						diagnostics.AdaptiveCandidates++
					}
					candidateView := overlayRoutes(current, candidate)
					expansions, ok := rerouteAdaptiveBundle(result, candidateView, node, bundle, candidateAssignment, nodes, geometry, groupBoundaries, remainingExpansions, diagnostics, occupancyWorkspace)
					remainingExpansions -= expansions
					if !ok {
						continue
					}
					ensureArrowLeadSegments(result, candidateView, diagnostics, groupBoundaries)
					if evaluator == nil {
						continue
					}
					evaluation := evaluator(current, AdaptiveCandidate{Replacements: candidate, ChangedEdgeID: changed.ID, Side: side, Expansions: expansions})
					if !evaluation.Accept {
						continue
					}
					if best == nil || CompareQuality(evaluation.Quality, bestQuality) < 0 ||
						CompareQuality(evaluation.Quality, bestQuality) == 0 && adaptiveChoiceLess(changed.ID, side, bestEdgeID, bestSide) {
						best, bestQuality, bestMetrics = candidate, evaluation.Quality, evaluation.Metrics
						bestCommit = evaluation.CommitReplacements
						bestEdgeID, bestSide = changed.ID, side
					}
				}
			}
		}
		if best == nil {
			break
		}
		if bestCommit == nil {
			bestCommit = best
		}
		current = overlayRoutes(current, bestCommit)
		metrics = bestMetrics
		if diagnostics != nil {
			diagnostics.AdaptiveCommits++
		}
		assignment[bestEdgeID] = bestSide
	}
	return current, metrics
}

func cloneAdaptiveBundle(bundle, current []*Edge, diagnostics *Diagnostics) []*Edge {
	ids := make(map[string]bool, len(bundle))
	for _, edge := range bundle {
		ids[edge.ID] = true
	}
	result := make([]*Edge, 0, len(bundle))
	for _, edge := range current {
		if !ids[edge.ID] {
			continue
		}
		clone := *edge
		clone.Points = append([]Point(nil), edge.Points...)
		result = append(result, &clone)
		if diagnostics != nil {
			diagnostics.CandidateClonedRoutes++
			diagnostics.CandidateClonedPoints += len(edge.Points)
		}
	}
	return result
}

type adaptiveCandidateScore struct {
	id            string
	structural    int
	intersections int
}

type adaptiveGeometrySnapshot struct {
	occupancy map[unitSegment]int
	byEdge    map[string]map[unitSegment]int
}

func newAdaptiveGeometrySnapshot(routes []*Edge) *adaptiveGeometrySnapshot {
	snapshot := &adaptiveGeometrySnapshot{
		occupancy: make(map[unitSegment]int),
		byEdge:    make(map[string]map[unitSegment]int),
	}
	for _, edge := range routes {
		snapshot.add(edge)
	}
	return snapshot
}

func (snapshot *adaptiveGeometrySnapshot) add(edge *Edge) {
	if !visibleEdge(edge) {
		return
	}
	if snapshot.byEdge[edge.ID] == nil {
		snapshot.byEdge[edge.ID] = make(map[unitSegment]int)
	}
	for index := 1; index < len(edge.Points); index++ {
		walkSegment(edge.Points[index-1], edge.Points[index], func(from, to Point) {
			segment := canonicalSegment(from, to)
			snapshot.occupancy[segment]++
			snapshot.byEdge[edge.ID][segment]++
		})
	}
}

func (snapshot *adaptiveGeometrySnapshot) remove(edgeID string) {
	if snapshot == nil {
		return
	}
	for segment, count := range snapshot.byEdge[edgeID] {
		snapshot.occupancy[segment] -= count
		if snapshot.occupancy[segment] == 0 {
			delete(snapshot.occupancy, segment)
		}
	}
	delete(snapshot.byEdge, edgeID)
}

func (snapshot *adaptiveGeometrySnapshot) count(segment unitSegment, edgeID string, excluded map[string]bool) int {
	count := snapshot.occupancy[segment] - snapshot.byEdge[edgeID][segment]
	for excludedID := range excluded {
		if excludedID != edgeID {
			count -= snapshot.byEdge[excludedID][segment]
		}
	}
	return count
}

type adaptiveGeometryView struct {
	base     *adaptiveGeometrySnapshot
	excluded map[string]bool
	added    *adaptiveGeometrySnapshot
}

func (view adaptiveGeometryView) count(segment unitSegment, edgeID string) int {
	count := view.base.count(segment, edgeID, view.excluded)
	if view.added != nil {
		count += view.added.count(segment, edgeID, nil)
	}
	return count
}

func (view adaptiveGeometryView) interactions(edgeID string, points []Point) (int, int) {
	crossings, overlaps := 0, 0
	for index := 1; index < len(points); index++ {
		walkSegment(points[index-1], points[index], func(from, to Point) {
			candidate := canonicalSegment(from, to)
			overlaps += view.count(candidate, edgeID)
			for _, crossing := range perpendicularUnitSegments(candidate) {
				crossings += view.count(crossing, edgeID)
			}
		})
	}
	return crossings, overlaps
}

func adaptiveCandidateEdges(routes []*Edge, result *layout.Layout, groupBoundaries groupBoundaryIndex, geometry *adaptiveGeometrySnapshot, limit int) map[string]bool {
	var scores []adaptiveCandidateScore
	for _, edge := range routes {
		if edge == nil || !edge.StartAuto || edge.Constrained || edge.SelfLoop || edge.Reversed || !visibleEdge(edge) {
			continue
		}
		crossings, overlaps := (adaptiveGeometryView{base: geometry}).interactions(edge.ID, edge.Points)
		structural := edgeNodeCollisions(result, edge) + groupBoundaries.overlapCount(edge, edge.Points)
		if structural == 0 && crossings == 0 && overlaps == 0 {
			continue
		}
		scores = append(scores, adaptiveCandidateScore{id: edge.ID, structural: structural, intersections: crossings + overlaps})
	}
	sort.SliceStable(scores, func(i, j int) bool {
		if scores[i].structural != scores[j].structural {
			return scores[i].structural > scores[j].structural
		}
		if scores[i].intersections != scores[j].intersections {
			return scores[i].intersections > scores[j].intersections
		}
		return scores[i].id < scores[j].id
	})
	resultSet := make(map[string]bool, min(limit, len(scores)))
	for _, score := range scores[:min(limit, len(scores))] {
		resultSet[score.id] = true
	}
	return resultSet
}

func adaptiveStartBundles(routes []*Edge, nodes map[string]*layout.Node) map[string][]*Edge {
	result := make(map[string][]*Edge)
	for _, edge := range routes {
		node := nodes[edge.From]
		if !eligibleAdaptiveStart(edge, node) {
			continue
		}
		result[edge.From] = append(result[edge.From], edge)
	}
	for id := range result {
		sort.SliceStable(result[id], func(i, j int) bool { return result[id][i].ID < result[id][j].ID })
	}
	return result
}

func eligibleAdaptiveStart(edge *Edge, source *layout.Node) bool {
	return edge != nil && source != nil && !source.Dummy && visibleEdge(edge) && edge.StartAuto &&
		!edge.SelfLoop && !edge.Reversed && !edge.Constrained &&
		edge.Source.Kind == diagram.EndpointNode && edge.Target.Kind == diagram.EndpointNode &&
		edge.Source.Cell == "" && edge.Target.Cell == "" &&
		edge.Source.NamedPort == "" && edge.Target.NamedPort == "" &&
		edge.Source.PortHint.Offset == nil
}

func rerouteAdaptiveBundle(
	result *layout.Layout,
	routes []*Edge,
	source *layout.Node,
	baselineBundle []*Edge,
	assignment map[string]diagram.Side,
	nodes map[string]*layout.Node,
	geometry *adaptiveGeometrySnapshot,
	groupBoundaries groupBoundaryIndex,
	maximumExpansions int,
	diagnostics *Diagnostics,
	occupancyWorkspace *routeOccupancy,
) (int, bool) {
	byID := make(map[string]*Edge, len(routes))
	for _, edge := range routes {
		byID[edge.ID] = edge
	}
	bundle := make([]*Edge, 0, len(baselineBundle))
	bundleIDs := make(map[string]bool, len(baselineBundle))
	for _, edge := range baselineBundle {
		current := byID[edge.ID]
		if current == nil {
			return 0, false
		}
		bundle = append(bundle, current)
		bundleIDs[current.ID] = true
	}
	starts := allocateAdaptiveStartPorts(source, bundle, routes, assignment, nodes)
	accepted := make([]*Edge, 0, len(routes))
	for _, edge := range routes {
		if !bundleIDs[edge.ID] {
			accepted = append(accepted, edge)
		}
	}
	acceptedGeometry := newAdaptiveGeometrySnapshot(nil)
	geometryView := adaptiveGeometryView{base: geometry, excluded: bundleIDs, added: acceptedGeometry}
	acceptedOccupancy := resetOccupancy(occupancyWorkspace, accepted)

	expansions := 0
	for _, edge := range bundle {
		if len(edge.Points) < 2 {
			return expansions, false
		}
		if maximumExpansions-expansions < 64 {
			return expansions, false
		}
		start := starts[edge.ID]
		startStub := stepOutside(start, assignment[edge.ID])
		end := edge.Points[len(edge.Points)-1]
		endDirection := stepDirection(edge.Points[len(edge.Points)-2], end)
		if endDirection == (Point{}) {
			return expansions, false
		}
		endStub := Point{X: end.X - endDirection.X, Y: end.Y - endDirection.Y}
		if !pointsWithin([]Point{start, startStub, endStub, end}, result.Metrics.Width, result.Metrics.Height) {
			return expansions, false
		}
		points, searchExpansions, found := adaptivePortPath(
			result, edge, start, startStub, endStub, end, acceptedOccupancy, geometryView, groupBoundaries, maximumExpansions-expansions, diagnostics,
		)
		expansions += searchExpansions
		if !found {
			return expansions, false
		}
		if len(points) < 2 || stepDirection(points[0], points[1]) != sideStep(assignment[edge.ID]) ||
			stepDirection(points[len(points)-2], points[len(points)-1]) != endDirection ||
			!orthogonalPoints(points) {
			return expansions, false
		}
		edge.Points = points
		edge.StartSide = assignment[edge.ID]
		edge.AdaptiveStart = true
		// A direct adaptive path still satisfies the long-route optimization
		// contract even though it no longer uses the original optimized points.
		edge.Optimized = edge.Long
		edge.Fallback = false
		edge.GroupDetour = false
		edge.LabelPlaced = false
		edge.LabelRect = layout.Rect{}
		accepted = append(accepted, edge)
		addOccupancy(&acceptedOccupancy, edge)
		acceptedGeometry.add(edge)
	}
	return expansions, true
}

func adaptivePortPath(result *layout.Layout, edge *Edge, start, startStub, endStub, end Point, occupied routeOccupancy, geometry adaptiveGeometryView, groupBoundaries groupBoundaryIndex, maximumExpansions int, diagnostics *Diagnostics) ([]Point, int, bool) {
	middleX := (startStub.X + endStub.X) / 2
	middleY := (startStub.Y + endStub.Y) / 2
	candidates := [][]Point{
		{start, startStub, {X: endStub.X, Y: startStub.Y}, endStub, end},
		{start, startStub, {X: startStub.X, Y: endStub.Y}, endStub, end},
		{start, startStub, {X: middleX, Y: startStub.Y}, {X: middleX, Y: endStub.Y}, endStub, end},
		{start, startStub, {X: startStub.X, Y: middleY}, {X: endStub.X, Y: middleY}, endStub, end},
	}
	if startStub.X == endStub.X || startStub.Y == endStub.Y {
		candidates = append([][]Point{{start, startStub, endStub, end}}, candidates...)
	}
	var best []Point
	var bestQuality adaptivePathQuality
	for _, points := range candidates {
		points = normalize(points)
		if !orthogonalPoints(points) || !pointsWithin(points, result.Metrics.Width, result.Metrics.Height) {
			continue
		}
		quality := measureAdaptivePath(result, edge, points, geometry, groupBoundaries)
		if best == nil || compareAdaptivePathQuality(quality, bestQuality) < 0 {
			best, bestQuality = points, quality
		}
	}
	if best == nil {
		return nil, 0, false
	}
	// Orthogonal candidates already account for occupied routes. Reserve the
	// substantially more expensive grid search for paths that hit a node.
	if edgeNodeCollisionsForPoints(result, edge, best) == 0 {
		return best, 0, true
	}

	grid := searchGrid{
		width:             result.Metrics.Width,
		height:            result.Metrics.Height,
		direction:         result.Direction,
		start:             startStub,
		goal:              endStub,
		obstacles:         obstacleRects(result.Nodes),
		blockedSegments:   groupBoundaries.segmentsForEdge(edge),
		occupied:          occupied,
		maximumExpansions: min(maximumExpansions, pathSearchBudget(result.Metrics.Width, result.Metrics.Height)),
	}
	search := searchOrthogonalPath(grid)
	recordPathSearch(diagnostics, search)
	if !search.found {
		return best, search.gridExpansions, true
	}
	searched := normalize(append(append([]Point{start}, search.points...), end))
	if quality := measureAdaptivePath(result, edge, searched, geometry, groupBoundaries); compareAdaptivePathQuality(quality, bestQuality) < 0 {
		best = searched
	}
	return best, search.gridExpansions, true
}

type adaptivePathQuality struct {
	structural     int
	intersections  int
	directionality int
	excess         int
	length         int
}

func measureAdaptivePath(result *layout.Layout, edge *Edge, points []Point, geometry adaptiveGeometryView, groupBoundaries groupBoundaryIndex) adaptivePathQuality {
	crossings, overlaps := geometry.interactions(edge.ID, points)
	length := pathLength(points)
	first, last := points[0], points[len(points)-1]
	reverseMoves := 0
	for index := 1; index < len(points); index++ {
		if isReverseMove(points[index-1], points[index], result.Direction) {
			reverseMoves++
		}
	}
	return adaptivePathQuality{
		structural:     groupBoundaries.overlapCount(edge, points)*500 + edgeNodeCollisionsForPoints(result, edge, points)*1000,
		intersections:  crossings + overlaps,
		directionality: max(0, len(points)-2) + reverseMoves,
		excess:         max(0, length-abs(last.X-first.X)-abs(last.Y-first.Y)),
		length:         length,
	}
}

func compareAdaptivePathQuality(left, right adaptivePathQuality) int {
	leftValues := [...]int{left.structural, left.intersections, left.directionality, left.excess, left.length}
	rightValues := [...]int{right.structural, right.intersections, right.directionality, right.excess, right.length}
	for index := range leftValues {
		if leftValues[index] < rightValues[index] {
			return -1
		}
		if leftValues[index] > rightValues[index] {
			return 1
		}
	}
	return 0
}

func allocateAdaptiveStartPorts(source *layout.Node, bundle, routes []*Edge, assignment map[string]diagram.Side, nodes map[string]*layout.Node) map[string]Point {
	reserved := make(map[diagram.Side]map[Point]bool)
	bundleIDs := make(map[string]bool, len(bundle))
	for _, edge := range bundle {
		bundleIDs[edge.ID] = true
	}
	for _, edge := range routes {
		if edge.From != source.ID || bundleIDs[edge.ID] || len(edge.Points) == 0 {
			continue
		}
		side := boundarySide(source.Rect, edge.Points[0])
		if reserved[side] == nil {
			reserved[side] = make(map[Point]bool)
		}
		reserved[side][edge.Points[0]] = true
	}

	bySide := make(map[diagram.Side][]*Edge)
	for _, edge := range bundle {
		bySide[assignment[edge.ID]] = append(bySide[assignment[edge.ID]], edge)
	}
	result := make(map[string]Point, len(bundle))
	for _, side := range []diagram.Side{diagram.SideNorth, diagram.SideEast, diagram.SideSouth, diagram.SideWest} {
		edges := bySide[side]
		sort.SliceStable(edges, func(i, j int) bool {
			left, right := nodes[edges[i].To], nodes[edges[j].To]
			leftPosition, rightPosition := adaptiveTargetPosition(left, side), adaptiveTargetPosition(right, side)
			if leftPosition != rightPosition {
				return leftPosition < rightPosition
			}
			if edges[i].To != edges[j].To {
				return edges[i].To < edges[j].To
			}
			return edges[i].ID < edges[j].ID
		})
		slots := adaptiveSideSlots(source.Rect, side)
		free := slots[:0]
		for _, point := range slots {
			if !reserved[side][point] {
				free = append(free, point)
			}
		}
		if len(free) > 0 {
			slots = free
		}
		for index, edge := range edges {
			slot := 0
			if len(edges) > 1 && len(slots) > 1 {
				slot = index * (len(slots) - 1) / (len(edges) - 1)
			} else if len(slots) > 1 {
				slot = (len(slots) - 1) / 2
			}
			result[edge.ID] = slots[slot]
		}
	}
	return result
}

func adaptiveSideSlots(rect layout.Rect, side diagram.Side) []Point {
	var result []Point
	if side == diagram.SideNorth || side == diagram.SideSouth {
		y := rect.Y
		if side == diagram.SideSouth {
			y += rect.Height - 1
		}
		for x := rect.X + 1; x < rect.X+rect.Width-1; x++ {
			result = append(result, Point{X: x, Y: y})
		}
		if len(result) == 0 {
			result = append(result, Point{X: rect.X + (rect.Width-1)/2, Y: y})
		}
		return result
	}
	x := rect.X
	if side == diagram.SideEast {
		x += rect.Width - 1
	}
	for y := rect.Y + 1; y < rect.Y+rect.Height-1; y++ {
		result = append(result, Point{X: x, Y: y})
	}
	if len(result) == 0 {
		result = append(result, Point{X: x, Y: rect.Y + (rect.Height-1)/2})
	}
	return result
}

func adaptiveTargetPosition(node *layout.Node, side diagram.Side) int {
	if node == nil {
		return 0
	}
	if side == diagram.SideNorth || side == diagram.SideSouth {
		return node.Rect.X + (node.Rect.Width-1)/2
	}
	return node.Rect.Y + (node.Rect.Height-1)/2
}

func boundarySide(rect layout.Rect, point Point) diagram.Side {
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

func sideStep(side diagram.Side) Point {
	point := stepOutside(Point{}, side)
	return point
}

func resetRouteLabels(routes []*Edge) {
	for _, edge := range routes {
		edge.LabelPlaced = false
		edge.LabelRect = layout.Rect{}
	}
}

func cloneRoutes(routes []*Edge) []*Edge {
	return cloneRoutesWithDiagnostics(routes, nil)
}

func cloneRoutesWithDiagnostics(routes []*Edge, diagnostics *Diagnostics) []*Edge {
	if diagnostics != nil {
		diagnostics.RouteCloneCalls++
		diagnostics.ClonedRoutes += len(routes)
		for _, edge := range routes {
			diagnostics.ClonedPoints += len(edge.Points)
		}
	}
	result := make([]*Edge, len(routes))
	for index, edge := range routes {
		clone := *edge
		clone.Points = append([]Point(nil), edge.Points...)
		result[index] = &clone
	}
	return result
}

func cloneSideAssignment(assignment map[string]diagram.Side) map[string]diagram.Side {
	result := make(map[string]diagram.Side, len(assignment))
	for id, side := range assignment {
		result[id] = side
	}
	return result
}

func adaptiveChoiceLess(edgeID string, side diagram.Side, otherID string, otherSide diagram.Side) bool {
	if edgeID != otherID {
		return edgeID < otherID
	}
	return side < otherSide
}
