package solve

import (
	"github.com/xshoji/agents-workspace/internal/layout"
	"github.com/xshoji/agents-workspace/internal/route"
)

type compactionDiagnostics struct {
	Compactions, LayoutCloneCalls          int
	ClonedNodes, ClonedEdges, ClonedGroups int
	Route                                  route.Diagnostics
}

func prepareCandidate(result *layout.Layout, routes []*route.Edge, metrics route.Metrics, diagnostics *compactionDiagnostics) (*layout.Layout, []*route.Edge, route.Metrics, bool) {
	compactedLayout, compactedRoutes := cloneResult(result, routes, diagnostics)
	if !compact(compactedLayout, compactedRoutes) {
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
