package solve

import (
	"fmt"
	"sort"

	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/geom"
	"github.com/xshoji/go-text-diagram/internal/layout"
	"github.com/xshoji/go-text-diagram/internal/route"
	"github.com/xshoji/go-text-diagram/internal/solution"
	"github.com/xshoji/go-text-diagram/internal/textwidth"
)

const (
	maximumGroupFlowCandidates      = 4
	maximumGroupFlowAStarExpansions = 500_000
	groupFlowSpacing                = 2
)

type groupFlowCandidate struct {
	groupID string
	rank    int
	delta   int
}

func groupFlowCandidates(problem *diagram.Problem, placed *layout.Layout) []groupFlowCandidate {
	if problem == nil || placed == nil || len(problem.Groups()) == 0 {
		return nil
	}
	groups, _, scopes := groupScopes(problem)

	nodes := make(map[string]*layout.Node, len(placed.Nodes))
	for _, node := range placed.Nodes {
		nodes[node.ID] = node
	}
	candidates := make([]groupFlowCandidate, 0, maximumGroupFlowCandidates)
	for _, group := range groups {
		groupID := string(group.ID)
		bands := groupRankBands(scopes[groupID], nodes, placed.Direction)
		ranks := make([]int, 0, len(bands))
		for rank := range bands {
			ranks = append(ranks, rank)
		}
		sort.Ints(ranks)
		for index := 1; index < len(ranks) && len(candidates) < maximumGroupFlowCandidates; index++ {
			previous, next := bands[ranks[index-1]], bands[ranks[index]]
			gap := next.X - (previous.X + previous.Width)
			if placed.Direction.ForwardSign() < 0 {
				gap = previous.X - (next.X + next.Width)
			}
			maximum := gap - max(groupFlowSpacing, placed.LabelLaneSize+1)
			seenDeltas := make(map[int]bool)
			for _, delta := range []int{maximum / 8, maximum * 3 / 32, maximum / 16, maximum / 32} {
				if delta <= 0 || seenDeltas[delta] {
					continue
				}
				seenDeltas[delta] = true
				candidates = append(candidates, groupFlowCandidate{groupID: groupID, rank: ranks[index], delta: delta})
				if len(candidates) == maximumGroupFlowCandidates {
					break
				}
			}
		}
	}
	return candidates
}

func groupRankBands(members map[string]bool, nodes map[string]*layout.Node, direction diagram.Direction) map[int]geom.Rect {
	bands := make(map[int]geom.Rect)
	for id := range members {
		node := nodes[id]
		if node == nil || node.Dummy {
			continue
		}
		start, size := node.Rect.X, node.Rect.Width
		if direction.Vertical() {
			start, size = node.Rect.Y, node.Rect.Height
		}
		band := bands[node.Rank]
		if band.Width == 0 {
			bands[node.Rank] = geom.Rect{X: start, Width: size}
			continue
		}
		left := min(band.X, start)
		right := max(band.X+band.Width, start+size)
		bands[node.Rank] = geom.Rect{X: left, Width: right - left}
	}
	return bands
}

func groupFlowWhitespace(problem *diagram.Problem, placed *layout.Layout) int {
	groups, _, scopes := groupScopes(problem)
	nodes := make(map[string]*layout.Node, len(placed.Nodes))
	for _, node := range placed.Nodes {
		nodes[node.ID] = node
	}
	whitespace := 0
	minimumGap := max(groupFlowSpacing, placed.LabelLaneSize+1)
	for _, group := range groups {
		bands := groupRankBands(scopes[string(group.ID)], nodes, placed.Direction)
		ranks := make([]int, 0, len(bands))
		for rank := range bands {
			ranks = append(ranks, rank)
		}
		sort.Ints(ranks)
		for index := 1; index < len(ranks); index++ {
			previous, next := bands[ranks[index-1]], bands[ranks[index]]
			gap := next.X - (previous.X + previous.Width)
			if placed.Direction.ForwardSign() < 0 {
				gap = previous.X - (next.X + next.Width)
			}
			whitespace += max(0, gap-minimumGap)
		}
	}
	return whitespace
}

func compactGroupFlow(problem *diagram.Problem, placed *layout.Layout, baseline *solution.Snapshot, diagnostics *SolveDiagnostics, expansionBudget *int) (*layout.Layout, *solution.Snapshot, bool) {
	currentLayout, currentSnapshot := placed, baseline
	changed := false
	tried := 0
	if *expansionBudget <= 0 {
		diagnostics.GroupFlow = append(diagnostics.GroupFlow, "skipped: A* expansion budget exhausted")
		return currentLayout, currentSnapshot, false
	}
	for tried < maximumGroupFlowCandidates && *expansionBudget > 0 {
		var bestLayout *layout.Layout
		var bestSnapshot *solution.Snapshot
		bestWhitespace := groupFlowWhitespace(problem, currentLayout)
		bestGroup, bestRank, bestDelta := "", 0, 0
		for _, candidate := range groupFlowCandidates(problem, currentLayout) {
			if tried == maximumGroupFlowCandidates || *expansionBudget <= 0 {
				break
			}
			tried++
			candidateLayout, _ := cloneResult(currentLayout, nil, nil)
			if reason := moveGroupRankSuffix(problem, candidateLayout, candidate); reason != "" {
				diagnostics.GroupFlow = append(diagnostics.GroupFlow, fmt.Sprintf("%s/rank%d delta=%d: rejected: %s", candidate.groupID, candidate.rank, candidate.delta, reason))
				continue
			}
			beforeExpansions := diagnostics.Route.AStarExpansions
			candidateSnapshot := routeGroupFlowCandidate(problem, candidateLayout, diagnostics)
			expansions := diagnostics.Route.AStarExpansions - beforeExpansions
			*expansionBudget -= expansions
			if candidateSnapshot == nil {
				diagnostics.GroupFlow = append(diagnostics.GroupFlow, fmt.Sprintf("%s/rank%d delta=%d: rejected by routing or snapshot validation (A* expansions=%d)", candidate.groupID, candidate.rank, candidate.delta, expansions))
				continue
			}
			if reason := groupFlowQualityRegression(candidateSnapshot, currentSnapshot); reason != "" {
				diagnostics.GroupFlow = append(diagnostics.GroupFlow, fmt.Sprintf("%s/rank%d delta=%d: rejected: %s", candidate.groupID, candidate.rank, candidate.delta, reason))
				continue
			}
			whitespace := groupFlowWhitespace(problem, candidateLayout)
			var bestQuality *solution.Quality
			if bestSnapshot != nil {
				quality := bestSnapshot.Quality()
				bestQuality = &quality
			}
			if !groupFlowCandidateBetter(candidateSnapshot.Quality(), currentSnapshot.Quality(), bestQuality, whitespace, bestWhitespace) {
				diagnostics.GroupFlow = append(diagnostics.GroupFlow, fmt.Sprintf("%s/rank%d delta=%d: not selected; group whitespace=%d, best=%d", candidate.groupID, candidate.rank, candidate.delta, whitespace, bestWhitespace))
				continue
			}
			bestLayout, bestSnapshot, bestWhitespace = candidateLayout, candidateSnapshot, whitespace
			bestGroup, bestRank, bestDelta = candidate.groupID, candidate.rank, candidate.delta
		}
		if bestSnapshot == nil {
			if tried == 0 {
				diagnostics.GroupFlow = append(diagnostics.GroupFlow, "no candidates: no compressible rank gaps")
			}
			break
		}
		diagnostics.GroupFlow = append(diagnostics.GroupFlow, fmt.Sprintf("selected %s/rank%d delta=%d: group whitespace=%d", bestGroup, bestRank, bestDelta, bestWhitespace))
		currentLayout, currentSnapshot = bestLayout, bestSnapshot
		changed = true
	}
	return currentLayout, currentSnapshot, changed
}

func groupFlowCandidateBetter(candidate, current solution.Quality, best *solution.Quality, whitespace, bestWhitespace int) bool {
	if solution.CompareQuality(candidate, current) > 0 {
		return false
	}
	if best == nil {
		return true
	}
	comparison := solution.CompareQuality(candidate, *best)
	return comparison < 0 || (comparison == 0 && whitespace < bestWhitespace)
}

func routeGroupFlowCandidate(problem *diagram.Problem, placed *layout.Layout, diagnostics *SolveDiagnostics) *solution.Snapshot {
	routes, metrics, routeDiagnostics := route.ComputeBaselineWithDiagnostics(placed)
	diagnostics.Route.Add(routeDiagnostics)
	snapshot, err := solution.FromLegacyCandidate(problem, placed, routes, metrics)
	if err != nil || snapshot.Validate() != nil {
		diagnostics.Transaction.RejectedContracts++
		return nil
	}
	index, err := solution.BuildIndex(snapshot)
	if err != nil {
		diagnostics.Transaction.RejectedContracts++
		return nil
	}
	adaptive, _ := route.OptimizeAutoStartSidesCOW(placed, routes, metrics,
		adaptiveEvaluator(snapshot, index, placed, diagnostics), &diagnostics.Route)
	metrics = route.EnsureArrowLeadSegments(placed, adaptive, &diagnostics.Route)
	updated, err := solution.FromLegacyCandidate(problem, placed, adaptive, metrics)
	if err != nil || updated.Validate() != nil {
		diagnostics.Transaction.RejectedContracts++
		return nil
	}
	return updated
}

func groupFlowQualityNoWorse(candidate, baseline *solution.Snapshot) bool {
	return groupFlowQualityRegression(candidate, baseline) == ""
}

func groupFlowQualityRegression(candidate, baseline *solution.Snapshot) string {
	candidateMetrics, baselineMetrics := candidate.RouteMetrics(), baseline.RouteMetrics()
	metrics := []struct {
		name      string
		candidate int
		baseline  int
	}{
		{"unrouted edges", candidateMetrics.UnroutedEdges, baselineMetrics.UnroutedEdges},
		{"node overlaps", candidateMetrics.NodeOverlaps, baselineMetrics.NodeOverlaps},
		{"edge-node collisions", candidateMetrics.EdgeNodeCollisions, baselineMetrics.EdgeNodeCollisions},
		{"group-boundary overlaps", candidateMetrics.GroupBoundaryOverlaps, baselineMetrics.GroupBoundaryOverlaps},
		{"label-node collisions", candidateMetrics.LabelNodeCollisions, baselineMetrics.LabelNodeCollisions},
		{"label-label collisions", candidateMetrics.LabelLabelCollisions, baselineMetrics.LabelLabelCollisions},
		{"label-edge collisions", candidateMetrics.LabelEdgeCollisions, baselineMetrics.LabelEdgeCollisions},
		{"unplaced labels", candidateMetrics.UnplacedLabels, baselineMetrics.UnplacedLabels},
		{"crossings", candidateMetrics.Crossings, baselineMetrics.Crossings},
		{"overlaps", candidateMetrics.Overlaps, baselineMetrics.Overlaps},
		{"bends", candidateMetrics.Bends, baselineMetrics.Bends},
		{"reverse moves", candidateMetrics.ReverseMoves, baselineMetrics.ReverseMoves},
	}
	for _, metric := range metrics {
		if metric.candidate > metric.baseline {
			return fmt.Sprintf("%s increased %d→%d", metric.name, metric.baseline, metric.candidate)
		}
	}
	if solution.CompareQuality(candidate.Quality(), baseline.Quality()) > 0 {
		before, after := baseline.Quality(), candidate.Quality()
		fields := []struct {
			name          string
			before, after int
		}{
			{"unrouted", before.Unrouted, after.Unrouted},
			{"structural", before.Structural, after.Structural},
			{"labels", before.Labels, after.Labels},
			{"intersections", before.Intersections, after.Intersections},
			{"reversed", before.Reversed, after.Reversed},
			{"directionality", before.Directionality, after.Directionality},
			{"excess", before.Excess, after.Excess},
			{"cross-axis", before.CrossAxis, after.CrossAxis},
			{"alignment", before.Alignment, after.Alignment},
			{"area", before.Area, after.Area},
			{"length", before.Length, after.Length},
		}
		for _, field := range fields {
			if field.after != field.before {
				return fmt.Sprintf("quality %s increased %d→%d", field.name, field.before, field.after)
			}
		}
		return "quality comparison worsened"
	}
	return ""
}

func groupScopes(problem *diagram.Problem) ([]diagram.Group, map[string]diagram.Group, map[string]map[string]bool) {
	groups := problem.Groups()
	groupByID := make(map[string]diagram.Group, len(groups))
	children := make(map[string][]string)
	for _, group := range groups {
		id := string(group.ID)
		groupByID[id] = group
		children[string(group.Parent)] = append(children[string(group.Parent)], id)
	}
	scopes := make(map[string]map[string]bool, len(groups))
	var scopeOf func(string) map[string]bool
	scopeOf = func(id string) map[string]bool {
		if members, ok := scopes[id]; ok {
			return members
		}
		members := make(map[string]bool)
		for _, member := range groupByID[id].Members {
			members[string(member)] = true
		}
		for _, child := range children[id] {
			for member := range scopeOf(child) {
				members[member] = true
			}
		}
		scopes[id] = members
		return members
	}
	for _, group := range groups {
		scopeOf(string(group.ID))
	}
	return groups, groupByID, scopes
}

func endpointInside(endpoint diagram.Endpoint, scopes map[string]map[string]bool, groups map[string]diagram.Group, groupID string) bool {
	if endpoint.Kind == diagram.EndpointNode {
		return scopes[groupID][endpoint.ID()]
	}
	for current := endpoint.ID(); current != ""; current = string(groups[current].Parent) {
		if current == groupID {
			return true
		}
		if _, ok := groups[current]; !ok {
			break
		}
	}
	return false
}

func moveGroupRankSuffix(problem *diagram.Problem, placed *layout.Layout, candidate groupFlowCandidate) string {
	previous, _ := cloneResult(placed, nil, nil)
	groups, groupByID, scopes := groupScopes(problem)
	members := scopes[candidate.groupID]
	if len(members) == 0 {
		return "group has no movable members"
	}
	nodes := make(map[string]*layout.Node, len(placed.Nodes))
	for _, node := range placed.Nodes {
		nodes[node.ID] = node
	}
	moving := make(map[string]bool)
	for id := range members {
		if node := nodes[id]; node != nil && !node.Dummy && node.Rank >= candidate.rank {
			moving[id] = true
		}
	}
	if len(moving) == 0 {
		return "rank suffix contains no movable nodes"
	}

	// A nested group is moved as a unit; do not cut through its ranks.
	for _, child := range groups {
		if string(child.Parent) != candidate.groupID {
			continue
		}
		before, after := false, false
		for id := range scopes[string(child.ID)] {
			node := nodes[id]
			if node == nil || node.Dummy {
				continue
			}
			before = before || node.Rank < candidate.rank
			after = after || node.Rank >= candidate.rank
		}
		if before && after {
			return fmt.Sprintf("cut would split nested group %q", child.ID)
		}
	}

	insideEdges := make(map[string]bool)
	for _, edge := range placed.Edges {
		if endpointInside(edge.Source, scopes, groupByID, candidate.groupID) &&
			endpointInside(edge.Target, scopes, groupByID, candidate.groupID) {
			insideEdges[edge.OriginalEdgeID] = true
		}
	}
	for _, edge := range placed.Edges {
		if !insideEdges[edge.OriginalEdgeID] {
			continue
		}
		for _, id := range []string{edge.From, edge.To} {
			if node := nodes[id]; node != nil && node.Dummy && node.Rank >= candidate.rank {
				moving[id] = true
			}
		}
	}

	axis, sign := axisFor(placed.Direction), placed.Direction.ForwardSign()
	shift := -sign * candidate.delta
	for id := range moving {
		node := nodes[id]
		if node.Dummy {
			continue
		}
		movedRect := shiftRect(node.Rect, axis, shift)
		for _, fixed := range placed.Nodes {
			if fixed.Dummy || moving[fixed.ID] {
				continue
			}
			if geom.Overlaps(movedRect, fixed.Rect) {
				return fmt.Sprintf("moved node %q overlaps fixed node %q", id, fixed.ID)
			}
		}
	}
	if !preservesEdgeDirection(placed, nodes, moving, axis, sign, shift) {
		return "movement would reverse an existing edge"
	}
	for id := range moving {
		node := nodes[id]
		node.Rect = shiftRect(node.Rect, axis, shift)
	}
	if !rebuildGroupRects(problem, placed) {
		return "group bounds could not be rebuilt"
	}
	if !noNewGroupCollisions(problem, previous, placed) {
		return "movement creates a new group-boundary collision"
	}
	return ""
}

type flowAxis uint8

const (
	axisX flowAxis = iota
	axisY
)

func axisFor(direction diagram.Direction) flowAxis {
	if direction.Vertical() {
		return axisY
	}
	return axisX
}

func shiftRect(rect geom.Rect, axis flowAxis, shift int) geom.Rect {
	if axis == axisX {
		rect.X += shift
	} else {
		rect.Y += shift
	}
	return rect
}

func axisCenter(rect geom.Rect, axis flowAxis) int {
	if axis == axisX {
		return rect.X + rect.Width/2
	}
	return rect.Y + rect.Height/2
}

func preservesEdgeDirection(placed *layout.Layout, nodes map[string]*layout.Node, moving map[string]bool, axis flowAxis, sign, shift int) bool {
	seen := make(map[string]bool)
	for _, edge := range placed.Edges {
		if seen[edge.OriginalEdgeID] {
			continue
		}
		seen[edge.OriginalEdgeID] = true
		from, to := nodes[edge.OriginalFrom], nodes[edge.OriginalTo]
		if from == nil || to == nil || (!moving[from.ID] && !moving[to.ID]) {
			continue
		}
		before := sign * (axisCenter(to.Rect, axis) - axisCenter(from.Rect, axis))
		fromRect, toRect := from.Rect, to.Rect
		if moving[from.ID] {
			fromRect = shiftRect(fromRect, axis, shift)
		}
		if moving[to.ID] {
			toRect = shiftRect(toRect, axis, shift)
		}
		after := sign * (axisCenter(toRect, axis) - axisCenter(fromRect, axis))
		if (before > 0 && after <= 0) || (before < 0 && after >= 0) {
			return false
		}
	}
	return true
}

func rebuildGroupRects(problem *diagram.Problem, placed *layout.Layout) bool {
	groups, groupByID, _ := groupScopes(problem)
	previous := make(map[string]geom.Rect, len(placed.Groups))
	for _, group := range placed.Groups {
		previous[group.ID] = group.Rect
	}
	children := make(map[string][]string)
	for _, group := range groups {
		children[string(group.Parent)] = append(children[string(group.Parent)], string(group.ID))
	}
	nodes := make(map[string]*layout.Node, len(placed.Nodes))
	for _, node := range placed.Nodes {
		if !node.Dummy {
			nodes[node.ID] = node
		}
	}
	rectByID := make(map[string]geom.Rect, len(groups))
	var build func(string) (geom.Rect, bool)
	build = func(id string) (geom.Rect, bool) {
		group, ok := groupByID[id]
		if !ok {
			return geom.Rect{}, false
		}
		var bounds geom.Rect
		set := false
		include := func(rect geom.Rect) {
			if !set {
				bounds, set = rect, true
				return
			}
			left, top := min(bounds.X, rect.X), min(bounds.Y, rect.Y)
			right, bottom := max(bounds.X+bounds.Width, rect.X+rect.Width), max(bounds.Y+bounds.Height, rect.Y+rect.Height)
			bounds = geom.Rect{X: left, Y: top, Width: right - left, Height: bottom - top}
		}
		for _, member := range group.Members {
			node := nodes[string(member)]
			if node == nil {
				return geom.Rect{}, false
			}
			include(node.Rect)
		}
		for _, child := range children[id] {
			rect, ok := build(child)
			if !ok {
				return geom.Rect{}, false
			}
			include(rect)
		}
		if !set {
			return geom.Rect{}, false
		}
		padding := max(1, group.Padding)
		rect := previous[id]
		if placed.Direction.Vertical() {
			if placed.Direction.ForwardSign() > 0 {
				end := max(bounds.Y+bounds.Height+padding, rect.Y+2*padding)
				rect.Height = end - rect.Y
			} else {
				end := rect.Y + rect.Height
				start := min(bounds.Y-padding, end-2*padding)
				rect.Y, rect.Height = start, end-start
			}
		} else if placed.Direction.ForwardSign() > 0 {
			end := max(bounds.X+bounds.Width+padding, rect.X+textwidth.String(group.Label)+4)
			rect.Width = end - rect.X
		} else {
			end := rect.X + rect.Width
			start := min(bounds.X-padding, end-(textwidth.String(group.Label)+4))
			rect.X, rect.Width = start, end-start
		}
		rectByID[id] = rect
		return rect, true
	}
	for _, group := range groups {
		if _, ok := build(string(group.ID)); !ok {
			return false
		}
	}
	for _, group := range placed.Groups {
		rect, ok := rectByID[group.ID]
		if !ok || rect.X < 0 || rect.Y < 0 {
			return false
		}
		group.Rect = rect
	}
	for _, group := range placed.Groups {
		placed.Metrics.Width = max(placed.Metrics.Width, group.Rect.X+group.Rect.Width)
		placed.Metrics.Height = max(placed.Metrics.Height, group.Rect.Y+group.Rect.Height)
	}
	for _, node := range placed.Nodes {
		placed.Metrics.Width = max(placed.Metrics.Width, node.Rect.X+node.Rect.Width)
		placed.Metrics.Height = max(placed.Metrics.Height, node.Rect.Y+node.Rect.Height)
	}
	return true
}

func noNewGroupCollisions(problem *diagram.Problem, previous, placed *layout.Layout) bool {
	groups, _, scopes := groupScopes(problem)
	groupRects := make(map[string]geom.Rect, len(placed.Groups))
	previousGroups := make(map[string]geom.Rect, len(previous.Groups))
	for _, group := range placed.Groups {
		groupRects[group.ID] = group.Rect
	}
	for _, group := range previous.Groups {
		previousGroups[group.ID] = group.Rect
	}
	previousNodes := make(map[string]geom.Rect, len(previous.Nodes))
	for _, node := range previous.Nodes {
		previousNodes[node.ID] = node.Rect
	}
	for _, group := range groups {
		id := string(group.ID)
		rect := groupRects[id]
		for _, node := range placed.Nodes {
			if node.Dummy || scopes[id][node.OriginalNodeID] || scopes[id][node.ID] {
				continue
			}
			if geom.Overlaps(rect, node.Rect) && !geom.Overlaps(previousGroups[id], previousNodes[node.ID]) {
				return false
			}
		}
	}
	for i, left := range groups {
		for _, right := range groups[i+1:] {
			if left.Parent != right.Parent {
				continue
			}
			leftID, rightID := string(left.ID), string(right.ID)
			if geom.Overlaps(groupRects[leftID], groupRects[rightID]) &&
				!geom.Overlaps(previousGroups[leftID], previousGroups[rightID]) {
				return false
			}
		}
	}
	return true
}
