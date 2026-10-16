package solve

import (
	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/layout"
	"github.com/xshoji/go-text-diagram/internal/route"
	"github.com/xshoji/go-text-diagram/internal/solution"
)

type compactionDiagnostics struct {
	Compactions, LayoutCloneCalls          int
	ClonedNodes, ClonedEdges, ClonedGroups int
	Route                                  route.Diagnostics
}

const maximumLocalCompactPasses = 3

func prepareCandidate(result *layout.Layout, routes []*route.Edge, metrics route.Metrics, diagnostics *compactionDiagnostics) (*layout.Layout, []*route.Edge, route.Metrics, bool) {
	compactedLayout, compactedRoutes := cloneResult(result, routes, diagnostics)
	if !compact(compactedLayout, compactedRoutes, compactInterior) {
		return result, routes, metrics, false
	}
	if diagnostics != nil {
		diagnostics.Compactions++
	}
	route.AvoidGroupBoundaryOverlaps(compactedLayout, compactedRoutes)
	var routeDiagnostics *route.Diagnostics
	if diagnostics != nil {
		routeDiagnostics = &diagnostics.Route
	}
	compactedMetrics := route.EnsureArrowLeadSegments(compactedLayout, compactedRoutes, routeDiagnostics)
	if route.CompareQuality(route.MeasureQuality(compactedLayout, compactedMetrics, compactedRoutes), route.MeasureQuality(result, metrics, routes)) <= 0 {
		return compactedLayout, compactedRoutes, compactedMetrics, true
	}
	return result, routes, metrics, false
}

// Final compaction shortens straight runs without rerouting. Reject the whole
// candidate if terminal spacing, containment, or any complexity metric regresses.
func compactFinal(problem *diagram.Problem, placed *layout.Layout, baseline *solution.Snapshot, diagnostics *SolveDiagnostics) (*layout.Layout, *solution.Snapshot, bool) {
	if len(placed.Groups) == 0 {
		return placed, baseline, false
	}
	snapshotRoutes := baseline.Routes()
	routes := make([]*route.Edge, 0, len(snapshotRoutes))
	for _, edge := range snapshotRoutes {
		routes = append(routes, &route.Edge{
			ID: edge.ID, From: edge.From, To: edge.To, Label: edge.Label,
			LabelRect: edge.LabelRect, LabelPlaced: edge.LabelPlaced, Points: edge.Points,
			Lane: edge.Lane, Reversed: edge.Reversed, SelfLoop: edge.SelfLoop, Long: edge.Long,
			Optimized: edge.Optimized, Fallback: edge.Fallback, GroupDetour: edge.GroupDetour,
			Kind: edge.Kind, StartSide: edge.StartSide, EndSide: edge.EndSide, StartAuto: edge.StartAuto,
			AdaptiveStart: edge.AdaptiveStart, Constrained: edge.Constrained,
			LineStyle: edge.LineStyle, ArrowStyle: edge.ArrowStyle, Source: edge.Source, Target: edge.Target,
		})
	}
	changed := false
	// Recentered labels can free columns for the next bounded local pass.
	for index := -1; index < len(placed.Groups)*maximumLocalCompactPasses; index++ {
		var clones compactionDiagnostics
		compacted, candidateRoutes := cloneResult(placed, routes, &clones)
		diagnostics.CompactionClones += clones.LayoutCloneCalls
		diagnostics.CompactionObjects += clones.ClonedNodes + clones.ClonedEdges + clones.ClonedGroups + clones.Route.ClonedRoutes
		diagnostics.Route.Add(clones.Route)
		compactedSpace := false
		if index < 0 {
			compactedSpace = compact(compacted, candidateRoutes, compactStraight)
		} else {
			compactedSpace = compactGroup(compacted, candidateRoutes, index%len(placed.Groups))
		}
		if !compactedSpace {
			continue
		}
		metrics := route.EnsureArrowLeadSegments(compacted, candidateRoutes, &diagnostics.Route)
		snapshot, err := solution.FromLegacyCandidate(problem, compacted, candidateRoutes, metrics)
		if err != nil || snapshot.Validate() != nil || !groupFlowQualityNoWorse(snapshot, baseline) {
			continue
		}
		placed, routes, baseline = compacted, candidateRoutes, snapshot
		changed = true
	}
	return placed, baseline, changed
}

func cloneResult(original *layout.Layout, originalRoutes []*route.Edge, diagnostics *compactionDiagnostics) (*layout.Layout, []*route.Edge) {
	if diagnostics != nil {
		diagnostics.LayoutCloneCalls++
		diagnostics.ClonedNodes += len(original.Nodes)
		diagnostics.ClonedEdges += len(original.Edges)
		diagnostics.ClonedGroups += len(original.Groups)
		diagnostics.Route.RouteCloneCalls++
		diagnostics.Route.ClonedRoutes += len(originalRoutes)
		for _, edge := range originalRoutes {
			diagnostics.Route.ClonedPoints += len(edge.Points)
		}
	}
	result := *original
	result.Nodes = make([]*layout.Node, len(original.Nodes))
	for index, node := range original.Nodes {
		clone := *node
		result.Nodes[index] = &clone
	}
	result.Edges = make([]*layout.Edge, len(original.Edges))
	for index, edge := range original.Edges {
		clone := *edge
		result.Edges[index] = &clone
	}
	result.Groups = make([]*layout.Group, len(original.Groups))
	for index, group := range original.Groups {
		clone := *group
		result.Groups[index] = &clone
	}
	routes := make([]*route.Edge, len(originalRoutes))
	for index, edge := range originalRoutes {
		clone := *edge
		clone.Points = append([]route.Point(nil), edge.Points...)
		routes[index] = &clone
	}
	return &result, routes
}
