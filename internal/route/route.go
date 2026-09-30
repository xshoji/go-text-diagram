package route

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/geom"
	"github.com/xshoji/go-text-diagram/internal/layout"
)

type Point = geom.Point

type Edge struct {
	ID            string
	From          string
	To            string
	Label         string
	LabelRect     layout.Rect
	LabelPlaced   bool
	Points        []Point
	Lane          int
	Reversed      bool
	SelfLoop      bool
	Long          bool
	Optimized     bool
	Fallback      bool
	GroupDetour   bool
	Kind          diagram.EdgeKind
	StartSide     diagram.Side
	EndSide       diagram.Side
	StartAuto     bool
	AdaptiveStart bool
	Constrained   bool
	LineStyle     diagram.LineStyle
	ArrowStyle    diagram.ArrowStyle
	Source        diagram.Endpoint
	Target        diagram.Endpoint
}

// Diagnostics records deterministic work counts for benchmarks and snapshot
// comparisons. It does not participate in route selection or rendered output.
type Diagnostics struct {
	AStarExpansions       int
	RouteCloneCalls       int
	ClonedRoutes          int
	ClonedPoints          int
	AdaptiveCandidates    int
	AdaptiveCommits       int
	CandidateClonedRoutes int
	CandidateClonedPoints int
	LabelPasses           int
	FullEvaluations       int
}

func (d *Diagnostics) Add(other Diagnostics) {
	if d == nil {
		return
	}
	d.AStarExpansions += other.AStarExpansions
	d.RouteCloneCalls += other.RouteCloneCalls
	d.ClonedRoutes += other.ClonedRoutes
	d.ClonedPoints += other.ClonedPoints
	d.AdaptiveCandidates += other.AdaptiveCandidates
	d.AdaptiveCommits += other.AdaptiveCommits
	d.CandidateClonedRoutes += other.CandidateClonedRoutes
	d.CandidateClonedPoints += other.CandidateClonedPoints
	d.LabelPasses += other.LabelPasses
	d.FullEvaluations += other.FullEvaluations
}

func recordPathSearch(diagnostics *Diagnostics, result pathSearchResult) {
	if diagnostics != nil {
		diagnostics.AStarExpansions += result.gridExpansions
	}
}

func visibleEdge(edge *Edge) bool {
	return edge != nil && edge.LineStyle != diagram.LineInvisible
}

func DebugString(edges []*Edge) string {
	if len(edges) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("routes:\n")
	for _, edge := range edges {
		fmt.Fprintf(&out, "  %s %q -> %q lane=%d reversed=%t loop=%t long=%t optimized=%t fallback=%t label=%q label_placed=%t label_rect=(%d,%d,%d,%d) points=",
			edge.ID, edge.From, edge.To, edge.Lane, edge.Reversed, edge.SelfLoop, edge.Long, edge.Optimized, edge.Fallback,
			edge.Label, edge.LabelPlaced, edge.LabelRect.X, edge.LabelRect.Y, edge.LabelRect.Width, edge.LabelRect.Height)
		for index, point := range edge.Points {
			if index > 0 {
				out.WriteString(" -> ")
			}
			fmt.Fprintf(&out, "(%d,%d)", point.X, point.Y)
		}
		out.WriteByte('\n')
	}
	return out.String()
}

type interval = geom.Interval

type segmentRoute struct {
	edge   *layout.Edge
	points []Point
	lane   int
}

type rankKey struct {
	component int
	rank      int
}

func ComputeBaselineWithDiagnostics(result *layout.Layout) ([]*Edge, Metrics, Diagnostics) {
	var diagnostics Diagnostics
	edges, metrics := computeBaselineWithMetrics(result, &diagnostics)
	return edges, metrics, diagnostics
}

func computeBaselineWithMetrics(result *layout.Layout, diagnostics *Diagnostics) ([]*Edge, Metrics) {
	boundaries := newGroupBoundaryIndex(result.Groups)
	edges := computeInitial(result, diagnostics)
	applyStructuredEndpoints(result, edges)
	edges = optimizeLongRoutes(result, edges, diagnostics, boundaries)
	edges = improveRoutes(result, edges, diagnostics, boundaries)
	ensureArrowLeadSegments(result, edges, diagnostics, boundaries)
	baseline := cloneRoutesWithDiagnostics(edges, diagnostics)
	resetRouteLabels(baseline)
	placeLabelsWithDiagnostics(result, baseline, diagnostics)
	return baseline, analyzeWithDiagnostics(result, baseline, diagnostics, boundaries)
}

func placeLabelsWithDiagnostics(result *layout.Layout, routes []*Edge, diagnostics *Diagnostics) {
	if diagnostics != nil {
		diagnostics.LabelPasses++
	}
	placeLabels(result, routes)
}

func analyzeWithDiagnostics(result *layout.Layout, routes []*Edge, diagnostics *Diagnostics, boundaries groupBoundaryIndex) Metrics {
	if diagnostics != nil {
		diagnostics.FullEvaluations++
	}
	return analyzeWithGroupBoundaries(result, routes, boundaries)
}

// computeInitial assigns deterministic crossing-aware lanes to adjacent-rank
// edge segments, then joins segments belonging to the same original edge.
func computeInitial(result *layout.Layout, diagnostics *Diagnostics) []*Edge {
	if result == nil || len(result.Edges) == 0 {
		return nil
	}

	nodes := make(map[string]*layout.Node, len(result.Nodes))
	for _, node := range result.Nodes {
		nodes[node.ID] = node
	}

	groups := make(map[string][]*layout.Edge)
	var ids []string
	for _, edge := range result.Edges {
		if _, exists := groups[edge.OriginalEdgeID]; !exists {
			ids = append(ids, edge.OriginalEdgeID)
		}
		groups[edge.OriginalEdgeID] = append(groups[edge.OriginalEdgeID], edge)
	}

	byRank := make(map[rankKey][]*layout.Edge)
	for _, edge := range result.Edges {
		if edge.Reversed || edge.SelfLoop {
			continue
		}
		from := nodes[edge.From]
		if from != nil {
			key := rankKey{component: from.Component, rank: from.Rank}
			byRank[key] = append(byRank[key], edge)
		}
	}

	routedSegments := make(map[*layout.Edge]segmentRoute, len(result.Edges))
	normalLaneCounts := make(map[rankKey]int)
	for key, edges := range byRank {
		lanes := make([][]interval, 0)
		layerEnd := rankExtent(result.Nodes, key.component, key.rank, result.Direction)
		laneSize := max(1, result.LabelLaneSize)
		maximumLanes := (abs(rankStart(result.Nodes, key.component, key.rank+1, result.Direction)-layerEnd) - 1) / laneSize
		starts, ends := assignPorts(edges, nodes, result.Direction)
		orderConvergingEdges(edges, starts, ends, result.Direction)
		priorSegments := make(segmentCountIndex)
		for _, edge := range edges {
			from, to := nodes[edge.From], nodes[edge.To]
			if from == nil || to == nil {
				continue
			}
			start, end := starts[edge], ends[edge]
			span := crossInterval(start, end, result.Direction)
			lane := crossingAwareLane(lanes, span, start, end, layerEnd, maximumLanes, laneSize, result.Direction, priorSegments)
			for lane >= len(lanes) {
				lanes = append(lanes, nil)
			}
			lanes[lane] = append(lanes[lane], span)
			lanePosition := layerEnd + result.Direction.ForwardSign()*(1+lane*laneSize+(laneSize-1)/2)
			points := segmentPoints(start, end, lanePosition, result.Direction)
			if edge.Start.Side != diagram.SideAuto || edge.End.Side != diagram.SideAuto {
				points = constrainedPortPoints(result, edge, from, to, start, end, diagnostics)
			}
			routedSegments[edge] = segmentRoute{
				edge:   edge,
				points: points,
				lane:   lane,
			}
			priorSegments.addPoints(points)
		}
		normalLaneCounts[key] = len(lanes)
	}

	routes := make([]*Edge, 0, len(groups))
	reversedLane := 0
	loopLanes := make(map[string]int)
	loopCounts := make(map[string]int)
	for _, id := range ids {
		first := groups[id][0]
		if first.SelfLoop {
			loopCounts[first.OriginalFrom]++
		}
	}
	for _, id := range ids {
		layoutEdges := groups[id]
		first := layoutEdges[0]
		if first.SelfLoop {
			node := nodes[first.OriginalFrom]
			lane := loopLanes[first.OriginalFrom]
			loopLanes[first.OriginalFrom]++
			points := selfLoopPoints(node, lane, loopCounts[first.OriginalFrom], result.Direction, first.Label)
			constrained := first.Start.Side != diagram.SideAuto || first.End.Side != diagram.SideAuto ||
				first.Source.Cell != "" || first.Target.Cell != ""
			if constrained {
				start := endpointPort(node, first.Source, first.Start, false, result.Direction, 0, 1)
				end := endpointPort(node, first.Target, first.End, true, result.Direction, 0, 1)
				points = constrainedPortPoints(result, first, node, node, start, end, diagnostics)
			}
			routes = append(routes, &Edge{
				ID:        id,
				From:      first.OriginalFrom,
				To:        first.OriginalTo,
				Label:     first.Label,
				Points:    points,
				Lane:      lane,
				SelfLoop:  true,
				Kind:      first.Kind,
				StartSide: resolvedSide(first.Start.Side, false, result.Direction),
				EndSide:   resolvedSide(first.End.Side, true, result.Direction),
				StartAuto: first.Start.Side == diagram.SideAuto && first.Start.Offset == nil &&
					first.Source.PortHint.Side == diagram.SideAuto && first.Source.PortHint.Offset == nil,
				Constrained: constrained,
				LineStyle:   first.LineStyle, ArrowStyle: first.ArrowStyle,
				Source: first.Source, Target: first.Target,
			})
			continue
		}
		if first.Reversed {
			from, to := nodes[first.OriginalFrom], nodes[first.OriginalTo]
			points := reversedPoints(result, from, to, reversedLane, normalLaneCounts)
			startPort, endPort := originalPorts(layoutEdges)
			constrained := startPort.Side != diagram.SideAuto || endPort.Side != diagram.SideAuto ||
				first.Source.Cell != "" || first.Target.Cell != ""
			if constrained {
				portEdge := *first
				portEdge.Start, portEdge.End = startPort, endPort
				start := endpointPort(from, first.Source, startPort, false, result.Direction, 0, 1)
				end := endpointPort(to, first.Target, endPort, true, result.Direction, 0, 1)
				points = constrainedPortPoints(result, &portEdge, from, to, start, end, diagnostics)
			}
			routes = append(routes, &Edge{
				ID:        id,
				From:      first.OriginalFrom,
				To:        first.OriginalTo,
				Label:     first.Label,
				Points:    points,
				Lane:      reversedLane,
				Reversed:  true,
				Kind:      first.Kind,
				StartSide: resolvedSide(startPort.Side, false, result.Direction),
				EndSide:   resolvedSide(endPort.Side, true, result.Direction),
				StartAuto: startPort.Side == diagram.SideAuto && startPort.Offset == nil &&
					first.Source.PortHint.Side == diagram.SideAuto && first.Source.PortHint.Offset == nil,
				Constrained: constrained,
				LineStyle:   first.LineStyle, ArrowStyle: first.ArrowStyle,
				Source: first.Source, Target: first.Target,
			})
			reversedLane++
			continue
		}

		segments := make([]segmentRoute, 0, len(layoutEdges))
		for _, edge := range layoutEdges {
			if segment, ok := routedSegments[edge]; ok {
				segments = append(segments, segment)
			}
		}
		if len(segments) == 0 {
			continue
		}
		sort.SliceStable(segments, func(i, j int) bool {
			return segments[i].edge.Segment < segments[j].edge.Segment
		})
		points := make([]Point, 0, len(segments)*4)
		for _, segment := range segments {
			points = append(points, segment.points...)
		}
		startPort, endPort := originalPorts(layoutEdges)
		routes = append(routes, &Edge{
			ID:        id,
			From:      first.OriginalFrom,
			To:        first.OriginalTo,
			Label:     first.Label,
			Points:    normalize(points),
			Lane:      segments[0].lane,
			Long:      len(layoutEdges) > 1,
			Kind:      first.Kind,
			StartSide: firstNonAutoStart(layoutEdges, result.Direction),
			EndSide:   firstNonAutoEnd(layoutEdges, result.Direction),
			StartAuto: startPort.Side == diagram.SideAuto && startPort.Offset == nil &&
				first.Source.PortHint.Side == diagram.SideAuto && first.Source.PortHint.Offset == nil,
			Constrained: startPort.Side != diagram.SideAuto || endPort.Side != diagram.SideAuto,
			LineStyle:   first.LineStyle, ArrowStyle: first.ArrowStyle,
			Source: first.Source, Target: first.Target,
		})
	}
	return routes
}

func originalPorts(edges []*layout.Edge) (diagram.PortHint, diagram.PortHint) {
	var start, end diagram.PortHint
	for _, edge := range edges {
		if edge.Start.Side != diagram.SideAuto {
			start = edge.Start
		}
		if edge.End.Side != diagram.SideAuto {
			end = edge.End
		}
	}
	return start, end
}

func rankExtent(nodes []*layout.Node, component, rank int, direction diagram.Direction) int {
	extent := 0
	if direction.ForwardSign() < 0 {
		extent = int(^uint(0) >> 1)
	}
	for _, node := range nodes {
		if node.Component != component || node.Rank != rank {
			continue
		}
		candidate := node.Rect.Y + node.Rect.Height - 1
		if direction == diagram.DirectionUp {
			candidate = node.Rect.Y
		} else if direction == diagram.DirectionRight {
			candidate = node.Rect.X + node.Rect.Width - 1
		} else if direction == diagram.DirectionLeft {
			candidate = node.Rect.X
		}
		if direction.ForwardSign() > 0 && candidate > extent || direction.ForwardSign() < 0 && candidate < extent {
			extent = candidate
		}
	}
	return extent
}

func rankStart(nodes []*layout.Node, component, rank int, direction diagram.Direction) int {
	start := int(^uint(0) >> 1)
	if direction.ForwardSign() < 0 {
		start = 0
	}
	for _, node := range nodes {
		if node.Component != component || node.Rank != rank {
			continue
		}
		candidate := node.Rect.Y
		if direction == diagram.DirectionUp {
			candidate = node.Rect.Y + node.Rect.Height - 1
		} else if direction == diagram.DirectionRight {
			candidate = node.Rect.X
		} else if direction == diagram.DirectionLeft {
			candidate = node.Rect.X + node.Rect.Width - 1
		}
		if direction.ForwardSign() > 0 && candidate < start || direction.ForwardSign() < 0 && candidate > start {
			start = candidate
		}
	}
	if start == int(^uint(0)>>1) {
		return 0
	}
	return start
}

func crossInterval(start, end Point, direction diagram.Direction) geom.Interval {
	a, b := start.X, end.X
	if !direction.Vertical() {
		a, b = start.Y, end.Y
	}
	if a > b {
		a, b = b, a
	}
	return geom.Interval{Start: a, End: b}
}

func crossingAwareLane(
	lanes [][]geom.Interval,
	candidate geom.Interval,
	start Point,
	end Point,
	layerEnd int,
	maximumLanes int,
	laneSize int,
	direction diagram.Direction,
	priorSegments segmentCountIndex,
) int {
	if maximumLanes < 1 {
		maximumLanes = len(lanes) + 1
	}
	bestLane, bestCost := -1, int(^uint(0)>>1)
	candidateLimit := min(maximumLanes, len(lanes)+2)
	for lane := 0; lane < candidateLimit; lane++ {
		if lane < len(lanes) && intervalOverlapsAny(candidate, lanes[lane]) {
			continue
		}
		points := segmentPoints(start, end, layerEnd+direction.ForwardSign()*(1+lane*laneSize+(laneSize-1)/2), direction)
		crossings, overlaps := priorSegments.interactions(points)
		cost := crossings*20 + overlaps*30 + lane
		if cost < bestCost {
			bestLane, bestCost = lane, cost
		}
	}
	if bestLane >= 0 {
		return bestLane
	}
	return geom.FirstAvailableLane(lanes, candidate)
}

// orderConvergingEdges routes the edge nearest the targets first so later,
// outer lanes do not cross earlier terminal segments. Mixed, parallel, and
// constrained bundles retain input order because that nesting is ambiguous.
func orderConvergingEdges(edges []*layout.Edge, starts, ends map[*layout.Edge]Point, direction diagram.Direction) {
	indicesByTarget := make(map[string][]int)
	for index, edge := range edges {
		indicesByTarget[edge.To] = append(indicesByTarget[edge.To], index)
	}
	for _, indices := range indicesByTarget {
		if len(indices) < 2 {
			continue
		}
		sources := make(map[string]bool, len(indices))
		eligible := true
		minStart, maxStart := int(^uint(0)>>1), -int(^uint(0)>>1)-1
		minEnd, maxEnd := int(^uint(0)>>1), -int(^uint(0)>>1)-1
		for _, edgeIndex := range indices {
			edge := edges[edgeIndex]
			if sources[edge.OriginalFrom] || edge.Start.Side != diagram.SideAuto || edge.End.Side != diagram.SideAuto ||
				edge.Start.Offset != nil || edge.End.Offset != nil || edge.Source.PortHint.Side != diagram.SideAuto ||
				edge.Target.PortHint.Side != diagram.SideAuto || edge.Source.PortHint.Offset != nil || edge.Target.PortHint.Offset != nil ||
				edge.Source.Cell != "" || edge.Target.Cell != "" {
				eligible = false
				break
			}
			sources[edge.OriginalFrom] = true
			start, end := crossAxisPosition(starts[edge], direction), crossAxisPosition(ends[edge], direction)
			minStart, maxStart = min(minStart, start), max(maxStart, start)
			minEnd, maxEnd = min(minEnd, end), max(maxEnd, end)
		}
		if !eligible || maxStart >= minEnd && minStart <= maxEnd {
			continue
		}
		for left := 0; left < len(indices) && eligible; left++ {
			leftEdge := edges[indices[left]]
			leftStart, leftEnd := crossAxisPosition(starts[leftEdge], direction), crossAxisPosition(ends[leftEdge], direction)
			for right := left + 1; right < len(indices); right++ {
				rightEdge := edges[indices[right]]
				rightStart, rightEnd := crossAxisPosition(starts[rightEdge], direction), crossAxisPosition(ends[rightEdge], direction)
				if leftStart < rightStart && leftEnd > rightEnd || leftStart > rightStart && leftEnd < rightEnd {
					eligible = false
					break
				}
			}
		}
		if !eligible {
			continue
		}
		ordered := make([]*layout.Edge, len(indices))
		for index, edgeIndex := range indices {
			ordered[index] = edges[edgeIndex]
		}
		sort.SliceStable(ordered, func(i, j int) bool {
			leftPosition := crossAxisPosition(starts[ordered[i]], direction)
			rightPosition := crossAxisPosition(starts[ordered[j]], direction)
			if leftPosition != rightPosition {
				if maxStart < minEnd {
					return leftPosition > rightPosition
				}
				return leftPosition < rightPosition
			}
			return ordered[i].ID < ordered[j].ID
		})
		for index, edgeIndex := range indices {
			edges[edgeIndex] = ordered[index]
		}
	}
}

func crossAxisPosition(point Point, direction diagram.Direction) int {
	if direction.Vertical() {
		return point.X
	}
	return point.Y
}

func intervalOverlapsAny(candidate geom.Interval, occupied []geom.Interval) bool {
	for _, current := range occupied {
		if geom.OverlapsInterval(candidate, current) {
			return true
		}
	}
	return false
}
