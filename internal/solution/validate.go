package solution

import (
	"fmt"

	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/geom"
)

const (
	maximumQualityBoundsCells int64 = 1_000_000
	maximumQualityRouteUnits  int64 = 10_000_000
)

func (s *Snapshot) validateQualityInputs() error {
	if s == nil {
		return fmt.Errorf("snapshot is nil")
	}
	if s.bounds.X != 0 || s.bounds.Y != 0 || s.bounds.Width < 0 || s.bounds.Height < 0 ||
		s.bounds.Width > int(maximumQualityBoundsCells) || s.bounds.Height > int(maximumQualityBoundsCells) ||
		int64(s.bounds.Width)*int64(s.bounds.Height) > maximumQualityBoundsCells {
		return fmt.Errorf("invalid or excessive bounds %+v", s.bounds)
	}
	total := int64(0)
	for _, edge := range s.routes {
		units, err := validateRoutePoints(edge, s.bounds)
		if err != nil {
			return err
		}
		if units > maximumQualityRouteUnits-total {
			return fmt.Errorf("routes exceed %d total cells", maximumQualityRouteUnits)
		}
		total += units
	}
	return nil
}

func validateRoutePoints(edge RoutedEdge, bounds geom.Rect) (int64, error) {
	if len(edge.Points) < 2 {
		return 0, fmt.Errorf("route %q has fewer than two points", edge.ID)
	}
	var units int64
	for index, point := range edge.Points {
		if !geom.Contains(bounds, point) {
			return 0, fmt.Errorf("route %q point %+v is outside bounds", edge.ID, point)
		}
		if index == 0 {
			continue
		}
		previous := edge.Points[index-1]
		if previous == point || (previous.X != point.X && previous.Y != point.Y) {
			return 0, fmt.Errorf("route %q has non-orthogonal segment %+v -> %+v", edge.ID, previous, point)
		}
		units += int64(magnitude(point.X-previous.X) + magnitude(point.Y-previous.Y))
	}
	return units, nil
}

func (s *Snapshot) Validate() error {
	if s == nil {
		return fmt.Errorf("snapshot is nil")
	}
	if s.bounds.X != 0 || s.bounds.Y != 0 || s.bounds.Width < 0 || s.bounds.Height < 0 {
		return fmt.Errorf("invalid bounds %+v", s.bounds)
	}
	if s.layoutMetrics.Width != s.bounds.Width || s.layoutMetrics.Height != s.bounds.Height {
		return fmt.Errorf("bounds %+v differ from layout metrics %dx%d", s.bounds, s.layoutMetrics.Width, s.layoutMetrics.Height)
	}
	if err := s.validateQualityInputs(); err != nil {
		return err
	}
	nodes := make(map[string]Node, len(s.nodes))
	semanticNodes := make(map[string]bool)
	for _, node := range s.nodes {
		if node.ID == "" || node.Rect.Width <= 0 || node.Rect.Height <= 0 || (!node.Dummy && !rectContains(s.bounds, node.Rect)) {
			return fmt.Errorf("invalid node placement %q: %+v", node.ID, node.Rect)
		}
		if _, exists := nodes[node.ID]; exists {
			return fmt.Errorf("duplicate node placement %q", node.ID)
		}
		nodes[node.ID] = node
		if !node.Dummy {
			id := node.OriginalNodeID
			if id == "" {
				id = node.ID
			}
			if semanticNodes[id] {
				return fmt.Errorf("semantic node %q is placed more than once", id)
			}
			semanticNodes[id] = true
		}
	}
	if len(semanticNodes) != len(s.manifest.nodes) {
		return fmt.Errorf("placed semantic node count %d differs from Problem count %d", len(semanticNodes), len(s.manifest.nodes))
	}
	for id := range s.manifest.nodes {
		if !semanticNodes[id] {
			return fmt.Errorf("semantic node %q has no placement", id)
		}
	}
	groups := make(map[string]Group, len(s.groups))
	for _, group := range s.groups {
		if group.ID == "" || group.Rect.Width <= 0 || group.Rect.Height <= 0 || !rectContains(s.bounds, group.Rect) {
			return fmt.Errorf("invalid group placement %q: %+v", group.ID, group.Rect)
		}
		if _, exists := groups[group.ID]; exists {
			return fmt.Errorf("duplicate group placement %q", group.ID)
		}
		groups[group.ID] = group
	}
	if len(groups) != len(s.manifest.groups) {
		return fmt.Errorf("placed group count %d differs from Problem count %d", len(groups), len(s.manifest.groups))
	}
	for _, group := range s.groups {
		expectedParent, exists := s.manifest.groups[group.ID]
		if !exists {
			return fmt.Errorf("group placement %q is not in Problem", group.ID)
		}
		if group.Parent != expectedParent {
			return fmt.Errorf("group %q parent %q differs from Problem parent %q", group.ID, group.Parent, expectedParent)
		}
		if group.Parent == "" {
			continue
		}
		parent, exists := groups[group.Parent]
		if !exists {
			return fmt.Errorf("group %q has unknown parent %q", group.ID, group.Parent)
		}
		if !rectContains(parent.Rect, group.Rect) {
			return fmt.Errorf("group %q is outside parent %q", group.ID, group.Parent)
		}
		seen := map[string]bool{group.ID: true}
		for current := group.Parent; current != ""; current = groups[current].Parent {
			if seen[current] {
				return fmt.Errorf("group containment cycle at %q", current)
			}
			seen[current] = true
		}
	}
	for nodeID, parentID := range s.manifest.nodeParent {
		parent, exists := groups[parentID]
		if !exists {
			return fmt.Errorf("node %q has unknown parent group %q", nodeID, parentID)
		}
		node, _ := semanticNode(nodeID, nodes)
		if !rectContains(parent.Rect, node.Rect) {
			return fmt.Errorf("node %q is outside parent group %q", nodeID, parentID)
		}
	}
	seenLayoutEdges := make(map[string]bool)
	for _, edge := range s.layoutEdges {
		if edge.OriginalEdgeID == "" {
			return fmt.Errorf("layout edge %q has empty semantic edge ID", edge.ID)
		}
		expected, exists := s.manifest.edges[edge.OriginalEdgeID]
		if !exists {
			return fmt.Errorf("layout edge %q references unknown semantic edge %q", edge.ID, edge.OriginalEdgeID)
		}
		if !sameEndpoint(expected.source, edge.Source) || !sameEndpoint(expected.target, edge.Target) {
			return fmt.Errorf("layout edge %q endpoints differ from semantic edge %q", edge.ID, edge.OriginalEdgeID)
		}
		seenLayoutEdges[edge.OriginalEdgeID] = true
	}
	for edgeID := range s.manifest.edges {
		if !seenLayoutEdges[edgeID] {
			return fmt.Errorf("semantic edge %q has no layout edge", edgeID)
		}
	}
	routed := make(map[string]bool, len(s.routes))
	for _, edge := range s.routes {
		if edge.ID == "" || routed[edge.ID] {
			return fmt.Errorf("duplicate routed edge %q", edge.ID)
		}
		routed[edge.ID] = true
		expected, exists := s.manifest.edges[edge.ID]
		if !exists {
			return fmt.Errorf("route %q has no semantic edge", edge.ID)
		}
		if !sameEndpoint(expected.source, edge.Source) || !sameEndpoint(expected.target, edge.Target) {
			return fmt.Errorf("route %q endpoints differ from semantic edge", edge.ID)
		}
		if edge.Label != expected.label || edge.Kind != expected.kind || edge.LineStyle != expected.lineStyle || edge.ArrowStyle != expected.arrowStyle {
			return fmt.Errorf("route %q attributes differ from semantic edge", edge.ID)
		}
		if _, err := validateRoutePoints(edge, s.bounds); err != nil {
			return err
		}
		if err := validateEndpointPoint(edge.ID, "source", edge.Source, edge.Points[0], semanticNodes, nodes, groups); err != nil {
			return err
		}
		if err := validateEndpointPoint(edge.ID, "target", edge.Target, edge.Points[len(edge.Points)-1], semanticNodes, nodes, groups); err != nil {
			return err
		}
		if err := validateResolvedEndpoints(edge, nodes, groups); err != nil {
			return err
		}
		if edge.LabelPlaced && (edge.LabelRect.Width <= 0 || edge.LabelRect.Height <= 0 || !rectContains(s.bounds, edge.LabelRect)) {
			return fmt.Errorf("route %q label is outside bounds: %+v", edge.ID, edge.LabelRect)
		}
		if err := validateNodeInteriors(edge, nodes); err != nil {
			return err
		}
		if err := validateGroupBoundaryOverlaps(edge, groups); err != nil {
			return err
		}
	}
	for edge := range s.manifest.edges {
		if !routed[edge] {
			return fmt.Errorf("semantic edge %q has no route", edge)
		}
	}
	return nil
}

func validateResolvedEndpoints(edge RoutedEdge, nodes map[string]Node, groups map[string]Group) error {
	if !sameEndpoint(edge.Start.Semantic, edge.Source) || edge.Start.Point != edge.Points[0] || edge.Start.Side != edge.StartSide {
		return fmt.Errorf("route %q has inconsistent resolved source", edge.ID)
	}
	last := len(edge.Points) - 1
	if !sameEndpoint(edge.End.Semantic, edge.Target) || edge.End.Point != edge.Points[last] || edge.End.Side != edge.EndSide {
		return fmt.Errorf("route %q has inconsistent resolved target", edge.ID)
	}
	for _, endpoint := range []struct {
		name     string
		semantic diagram.Endpoint
		resolved ResolvedEndpoint
	}{
		{name: "source", semantic: edge.Source, resolved: edge.Start},
		{name: "target", semantic: edge.Target, resolved: edge.End},
	} {
		rect, node, ok := endpointRect(endpoint.semantic, nodes, groups)
		if !ok || !pointOnSide(rect, endpoint.resolved.Point, endpoint.resolved.Side) {
			return fmt.Errorf("route %q %s point %+v does not match resolved side %s", edge.ID, endpoint.name, endpoint.resolved.Point, endpoint.resolved.Side)
		}
		if endpoint.semantic.Cell != "" {
			if node == nil {
				return fmt.Errorf("route %q %s cell endpoint is not a node", edge.ID, endpoint.name)
			}
			cell, exists := node.CellRects[endpoint.semantic.Cell]
			if !exists || !cellPortalMatches(*node, cell, endpoint.resolved) {
				return fmt.Errorf("route %q %s does not use exposed boundary of cell %q", edge.ID, endpoint.name, endpoint.semantic.Cell)
			}
		}
	}
	return nil
}

func endpointRect(endpoint diagram.Endpoint, nodes map[string]Node, groups map[string]Group) (geom.Rect, *Node, bool) {
	if endpoint.Kind == diagram.EndpointGroup {
		group, ok := groups[endpoint.ID()]
		return group.Rect, nil, ok
	}
	node, ok := semanticNode(endpoint.ID(), nodes)
	return node.Rect, &node, ok
}

func semanticNode(id string, nodes map[string]Node) (Node, bool) {
	for _, node := range nodes {
		original := node.OriginalNodeID
		if original == "" {
			original = node.ID
		}
		if !node.Dummy && original == id {
			return node, true
		}
	}
	return Node{}, false
}

func pointOnSide(rect geom.Rect, point geom.Point, side diagram.Side) bool {
	switch side {
	case diagram.SideNorth:
		return point.Y == rect.Y && point.X >= rect.X && point.X < rect.X+rect.Width
	case diagram.SideEast:
		return point.X == rect.X+rect.Width-1 && point.Y >= rect.Y && point.Y < rect.Y+rect.Height
	case diagram.SideSouth:
		return point.Y == rect.Y+rect.Height-1 && point.X >= rect.X && point.X < rect.X+rect.Width
	case diagram.SideWest:
		return point.X == rect.X && point.Y >= rect.Y && point.Y < rect.Y+rect.Height
	default:
		return false
	}
}

func cellPortalMatches(node Node, cell geom.Rect, endpoint ResolvedEndpoint) bool {
	switch endpoint.Side {
	case diagram.SideNorth:
		return endpoint.Point.Y == node.Rect.Y && cell.Y == node.Rect.Y+1 && endpoint.Point.X == cell.X+(cell.Width-1)/2
	case diagram.SideEast:
		return endpoint.Point.X == node.Rect.X+node.Rect.Width-1 && cell.X+cell.Width == node.Rect.X+node.Rect.Width-1 && endpoint.Point.Y == cell.Y
	case diagram.SideSouth:
		return endpoint.Point.Y == node.Rect.Y+node.Rect.Height-1 && cell.Y+cell.Height == node.Rect.Y+node.Rect.Height-1 && endpoint.Point.X == cell.X+(cell.Width-1)/2
	case diagram.SideWest:
		return endpoint.Point.X == node.Rect.X && cell.X == node.Rect.X+1 && endpoint.Point.Y == cell.Y
	default:
		return false
	}
}

func validateGroupBoundaryOverlaps(edge RoutedEdge, groups map[string]Group) error {
	for index := 1; index < len(edge.Points); index++ {
		from, to := edge.Points[index-1], edge.Points[index]
		for _, group := range groups {
			if index == 1 && endpointIsGroup(edge.Source, group.ID) ||
				index == len(edge.Points)-1 && endpointIsGroup(edge.Target, group.ID) {
				continue
			}
			if overlapsGroupBoundary(from, to, group.Rect) {
				return fmt.Errorf("route %q overlaps group %q boundary", edge.ID, group.ID)
			}
		}
	}
	return nil
}

func endpointIsGroup(endpoint diagram.Endpoint, groupID string) bool {
	return endpoint.Kind == diagram.EndpointGroup && endpoint.ID() == groupID
}

func overlapsGroupBoundary(from, to geom.Point, rect geom.Rect) bool {
	if from.X == to.X && (from.X == rect.X || from.X == rect.X+rect.Width-1) {
		return max(min(from.Y, to.Y), rect.Y) < min(max(from.Y, to.Y), rect.Y+rect.Height-1)
	}
	if from.Y == to.Y && (from.Y == rect.Y || from.Y == rect.Y+rect.Height-1) {
		return max(min(from.X, to.X), rect.X) < min(max(from.X, to.X), rect.X+rect.Width-1)
	}
	return false
}

func validateEndpointPoint(edgeID, end string, endpoint diagram.Endpoint, point geom.Point, semanticNodes map[string]bool, nodes map[string]Node, groups map[string]Group) error {
	var rect geom.Rect
	switch endpoint.Kind {
	case diagram.EndpointNode:
		if !semanticNodes[endpoint.ID()] {
			return fmt.Errorf("route %q %s references unknown node %q", edgeID, end, endpoint.ID())
		}
		for _, node := range nodes {
			id := node.OriginalNodeID
			if id == "" {
				id = node.ID
			}
			if !node.Dummy && id == endpoint.ID() {
				rect = node.Rect
				break
			}
		}
	case diagram.EndpointGroup:
		group, exists := groups[endpoint.ID()]
		if !exists {
			return fmt.Errorf("route %q %s references unknown group %q", edgeID, end, endpoint.ID())
		}
		rect = group.Rect
	default:
		return fmt.Errorf("route %q %s has unknown endpoint kind %d", edgeID, end, endpoint.Kind)
	}
	if !onBoundary(rect, point) {
		return fmt.Errorf("route %q %s point %+v is not on endpoint %q boundary", edgeID, end, point, endpoint.ID())
	}
	return nil
}

func sameEndpoint(left, right diagram.Endpoint) bool {
	if left.Kind != right.Kind || left.ID() != right.ID() || left.Cell != right.Cell || left.NamedPort != right.NamedPort || left.PortHint.Side != right.PortHint.Side {
		return false
	}
	if left.PortHint.Offset == nil || right.PortHint.Offset == nil {
		return left.PortHint.Offset == nil && right.PortHint.Offset == nil
	}
	return *left.PortHint.Offset == *right.PortHint.Offset
}

func validateNodeInteriors(edge RoutedEdge, nodes map[string]Node) error {
	for index := 1; index < len(edge.Points); index++ {
		from, to := edge.Points[index-1], edge.Points[index]
		dx, dy := sign(to.X-from.X), sign(to.Y-from.Y)
		for point := from; ; point.X, point.Y = point.X+dx, point.Y+dy {
			for _, node := range nodes {
				semanticID := node.OriginalNodeID
				if semanticID == "" {
					semanticID = node.ID
				}
				if node.Dummy || endpointIsNode(edge.Source, semanticID) || endpointIsNode(edge.Target, semanticID) {
					continue
				}
				if geom.ContainsInterior(node.Rect, point) {
					return fmt.Errorf("route %q crosses node %q interior", edge.ID, node.ID)
				}
			}
			if point == to {
				break
			}
		}
	}
	return nil
}

func endpointIsNode(endpoint diagram.Endpoint, nodeID string) bool {
	return endpoint.Kind == diagram.EndpointNode && endpoint.ID() == nodeID
}

func rectContains(outer, inner geom.Rect) bool {
	return inner.X >= outer.X && inner.Y >= outer.Y &&
		inner.X+inner.Width <= outer.X+outer.Width && inner.Y+inner.Height <= outer.Y+outer.Height
}

func onBoundary(rect geom.Rect, point geom.Point) bool {
	return geom.Contains(rect, point) && (point.X == rect.X || point.X == rect.X+rect.Width-1 || point.Y == rect.Y || point.Y == rect.Y+rect.Height-1)
}

func sign(value int) int {
	if value < 0 {
		return -1
	}
	if value > 0 {
		return 1
	}
	return 0
}
