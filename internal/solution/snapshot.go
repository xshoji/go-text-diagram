// Package solution owns immutable snapshots of fully laid out and routed diagrams.
package solution

import (
	"fmt"

	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/geom"
	"github.com/xshoji/go-text-diagram/internal/layout"
	"github.com/xshoji/go-text-diagram/internal/route"
)

type Node struct {
	ID             string
	OriginalNodeID string
	Component      int
	Label          string
	Rank           int
	Order          int
	Dummy          bool
	Rect           geom.Rect
	Shape          diagram.Shape
	Align          diagram.Align
	TextWrap       int
	Cells          [][]string
	CellIDs        [][]string
	CellRects      map[string]geom.Rect
}

type Group struct {
	ID        string
	Label     string
	Parent    string
	Rect      geom.Rect
	LineStyle diagram.LineStyle
}

type LayoutEdge struct {
	ID             string
	OriginalEdgeID string
	From           string
	To             string
	Segment        int
	Source         diagram.Endpoint
	Target         diagram.Endpoint
}

type RoutedEdge struct {
	ID            string
	From          string
	To            string
	Label         string
	LabelRect     geom.Rect
	LabelPlaced   bool
	Points        []geom.Point
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
	Start         ResolvedEndpoint
	End           ResolvedEndpoint
}

type ResolvedEndpoint struct {
	Semantic diagram.Endpoint
	Point    geom.Point
	Side     diagram.Side
}

type semanticManifest struct {
	nodes      map[string]string
	groups     map[string]string
	edges      map[string]semanticEdge
	nodeParent map[string]string
}

type semanticEdge struct {
	source     diagram.Endpoint
	target     diagram.Endpoint
	label      string
	kind       diagram.EdgeKind
	lineStyle  diagram.LineStyle
	arrowStyle diagram.ArrowStyle
}

type LayoutMetrics struct {
	Crossings     int
	DummyNodes    int
	LongEdges     int
	Width         int
	Height        int
	LayerCount    int
	SweepRounds   int
	ReversedEdges int
	SelfLoops     int
}

type RouteMetrics struct {
	NodeOverlaps          int
	EdgeNodeCollisions    int
	Crossings             int
	Overlaps              int
	TotalLength           int
	ExcessLength          int
	Bends                 int
	ReverseMoves          int
	AStarFallbacks        int
	UnroutedEdges         int
	LabelNodeCollisions   int
	LabelLabelCollisions  int
	LabelEdgeCollisions   int
	UnplacedLabels        int
	GroupBoundaryOverlaps int
}

type Quality struct {
	Unrouted       int
	Structural     int
	Labels         int
	Intersections  int
	Reversed       int
	Directionality int
	Excess         int
	CrossAxis      int
	Alignment      int
	Area           int
	Length         int
}

type LabelIssue struct {
	EdgeID         string
	Unplaced       bool
	NodeCollision  bool
	LabelCollision bool
	EdgeCollision  bool
}

type Snapshot struct {
	direction      diagram.Direction
	nodes          []Node
	groups         []Group
	layoutEdges    []LayoutEdge
	routes         []RoutedEdge
	bounds         geom.Rect
	layoutMetrics  LayoutMetrics
	routeMetrics   RouteMetrics
	quality        Quality
	revision       uint64
	outerLaneStart int
	labelLaneSize  int
	flowApplied    bool
	manifest       semanticManifest
}

// FromLegacy takes ownership of copies of the current mutable solver result.
// Future coordinate transactions must transform node/group rectangles, route
// points, placed label rectangles, bounds, and outerLaneStart atomically.
func FromLegacy(problem *diagram.Problem, result *layout.Layout, routes []*route.Edge, metrics route.Metrics) (*Snapshot, error) {
	snapshot, err := FromLegacyCandidate(problem, result, routes, metrics)
	if err != nil {
		return nil, err
	}
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	return snapshot, nil
}

// FromLegacyCandidate performs the lossless legacy conversion without
// validation. It is intentionally unsafe and exists only for solve's atomic
// whole-candidate transaction: callers must validate before publishing it.
func FromLegacyCandidate(problem *diagram.Problem, result *layout.Layout, routes []*route.Edge, metrics route.Metrics) (*Snapshot, error) {
	if problem == nil {
		return nil, fmt.Errorf("problem is nil")
	}
	if result == nil {
		return nil, fmt.Errorf("layout is nil")
	}
	snapshot := &Snapshot{
		direction: result.Direction, bounds: geom.Rect{Width: result.Metrics.Width, Height: result.Metrics.Height},
		layoutMetrics:  copyLayoutMetrics(result.Metrics),
		outerLaneStart: result.OuterLaneStart, labelLaneSize: result.LabelLaneSize, flowApplied: result.FlowApplied,
		manifest: semanticManifest{
			nodes: make(map[string]string), groups: make(map[string]string), edges: make(map[string]semanticEdge), nodeParent: make(map[string]string),
		},
	}
	for _, node := range problem.Nodes() {
		snapshot.manifest.nodes[string(node.ID)] = string(node.ID)
		if parent, ok := problem.ParentOf(diagram.ElementRef{Kind: diagram.ElementNode, Node: node.ID}); ok {
			snapshot.manifest.nodeParent[string(node.ID)] = string(parent)
		}
	}
	for _, group := range problem.Groups() {
		snapshot.manifest.groups[string(group.ID)] = string(group.Parent)
	}
	for _, edge := range problem.Edges() {
		snapshot.manifest.edges[string(edge.ID)] = semanticEdge{
			source: cloneEndpoint(edge.Source), target: cloneEndpoint(edge.Target), label: edge.Label,
			kind: edge.Kind, lineStyle: edge.LineStyle, arrowStyle: edge.ArrowStyle,
		}
	}
	for _, source := range result.Nodes {
		if source == nil {
			return nil, fmt.Errorf("layout contains nil node")
		}
		node := Node{
			ID: source.ID, OriginalNodeID: source.OriginalNodeID, Component: source.Component, Label: source.Label,
			Rank: source.Rank, Order: source.Order, Dummy: source.Dummy, Rect: source.Rect,
			Shape: source.Shape, Align: source.Align, TextWrap: source.TextWrap,
			Cells: cloneStrings(source.Cells), CellIDs: cloneStrings(source.CellIDs), CellRects: make(map[string]geom.Rect),
		}
		for _, row := range source.CellIDs {
			for _, cellID := range row {
				if cellID != "" {
					if rect, ok := source.CellRect(cellID); ok {
						node.CellRects[cellID] = rect
					}
				}
			}
		}
		snapshot.nodes = append(snapshot.nodes, node)
	}
	for _, source := range result.Groups {
		if source == nil {
			return nil, fmt.Errorf("layout contains nil group")
		}
		parent := snapshot.manifest.groups[source.ID]
		snapshot.groups = append(snapshot.groups, Group{ID: source.ID, Label: source.Label, Parent: parent, Rect: source.Rect, LineStyle: source.LineStyle})
	}
	for _, source := range result.Edges {
		if source == nil {
			return nil, fmt.Errorf("layout contains nil edge")
		}
		snapshot.layoutEdges = append(snapshot.layoutEdges, LayoutEdge{
			ID: source.ID, OriginalEdgeID: source.OriginalEdgeID, From: source.From, To: source.To, Segment: source.Segment,
			Source: cloneEndpoint(source.Source), Target: cloneEndpoint(source.Target),
		})
	}
	nodesByID := make(map[string]Node, len(snapshot.nodes))
	for _, node := range snapshot.nodes {
		nodesByID[node.ID] = node
	}
	groupsByID := make(map[string]Group, len(snapshot.groups))
	for _, group := range snapshot.groups {
		groupsByID[group.ID] = group
	}
	for _, source := range routes {
		if source == nil {
			return nil, fmt.Errorf("routes contain nil edge")
		}
		edge := RoutedEdge{
			ID: source.ID, From: source.From, To: source.To, Label: source.Label, LabelRect: source.LabelRect,
			LabelPlaced: source.LabelPlaced, Points: append([]geom.Point(nil), source.Points...), Lane: source.Lane,
			Reversed: source.Reversed, SelfLoop: source.SelfLoop, Long: source.Long, Optimized: source.Optimized,
			Fallback: source.Fallback, GroupDetour: source.GroupDetour, Kind: source.Kind,
			StartSide: source.StartSide, EndSide: source.EndSide, StartAuto: source.StartAuto,
			AdaptiveStart: source.AdaptiveStart, Constrained: source.Constrained, LineStyle: source.LineStyle,
			ArrowStyle: source.ArrowStyle, Source: cloneEndpoint(source.Source), Target: cloneEndpoint(source.Target),
		}
		if len(edge.Points) > 0 {
			if len(edge.Points) > 1 {
				edge.StartSide = resolvedPhysicalSide(edge.Source, edge.Points[0], edge.Points[1], edge.StartSide, nodesByID, groupsByID)
				edge.EndSide = resolvedPhysicalSide(edge.Target, edge.Points[len(edge.Points)-1], edge.Points[len(edge.Points)-2], edge.EndSide, nodesByID, groupsByID)
			}
			edge.Start = ResolvedEndpoint{Semantic: cloneEndpoint(edge.Source), Point: edge.Points[0], Side: edge.StartSide}
			edge.End = ResolvedEndpoint{Semantic: cloneEndpoint(edge.Target), Point: edge.Points[len(edge.Points)-1], Side: edge.EndSide}
		}
		snapshot.routes = append(snapshot.routes, edge)
	}
	legacyMetrics := copyRouteMetrics(metrics)
	if err := snapshot.validateQualityInputs(); err != nil {
		return nil, err
	}
	evaluated := FullQuality(snapshot)
	if evaluated.Metrics != legacyMetrics {
		return nil, fmt.Errorf("solution quality differs from legacy analyzer: solution=%+v legacy=%+v", evaluated.Metrics, legacyMetrics)
	}
	snapshot.routeMetrics, snapshot.quality = evaluated.Metrics, evaluated.Quality
	return snapshot, nil
}

func resolvedPhysicalSide(endpoint diagram.Endpoint, point, outside geom.Point, preferred diagram.Side, nodes map[string]Node, groups map[string]Group) diagram.Side {
	rect, node, ok := endpointRect(endpoint, nodes, groups)
	if !ok {
		return diagram.SideAuto
	}
	if endpoint.Cell != "" && node != nil {
		if cell, exists := node.CellRects[endpoint.Cell]; exists {
			for _, side := range []diagram.Side{diagram.SideNorth, diagram.SideEast, diagram.SideSouth, diagram.SideWest} {
				if cellPortalMatches(*node, cell, ResolvedEndpoint{Point: point, Side: side}) {
					return side
				}
			}
		}
	}
	if endpoint.Kind == diagram.EndpointGroup {
		if normal := terminalSide(point, outside); pointOnSide(rect, point, normal) {
			return normal
		}
	}
	if pointOnSide(rect, point, preferred) {
		return preferred
	}
	for _, side := range []diagram.Side{diagram.SideNorth, diagram.SideEast, diagram.SideSouth, diagram.SideWest} {
		if pointOnSide(rect, point, side) {
			return side
		}
	}
	return diagram.SideAuto
}

func terminalSide(portal, outside geom.Point) diagram.Side {
	switch {
	case outside.X == portal.X && outside.Y < portal.Y:
		return diagram.SideNorth
	case outside.X > portal.X && outside.Y == portal.Y:
		return diagram.SideEast
	case outside.X == portal.X && outside.Y > portal.Y:
		return diagram.SideSouth
	case outside.X < portal.X && outside.Y == portal.Y:
		return diagram.SideWest
	default:
		return diagram.SideAuto
	}
}

func (s *Snapshot) Direction() diagram.Direction { return s.direction }
func (s *Snapshot) Bounds() geom.Rect            { return s.bounds }
func (s *Snapshot) LayoutMetrics() LayoutMetrics { return s.layoutMetrics }
func (s *Snapshot) RouteMetrics() RouteMetrics   { return s.routeMetrics }
func (s *Snapshot) Quality() Quality             { return s.quality }
func (s *Snapshot) Revision() uint64             { return s.revision }

func (s *Snapshot) Nodes() []Node {
	result := make([]Node, len(s.nodes))
	for index, node := range s.nodes {
		result[index] = node
		result[index].Cells = cloneStrings(node.Cells)
		result[index].CellIDs = cloneStrings(node.CellIDs)
		result[index].CellRects = cloneRects(node.CellRects)
	}
	return result
}

func (s *Snapshot) Groups() []Group { return append([]Group(nil), s.groups...) }

func (s *Snapshot) Routes() []RoutedEdge {
	result := make([]RoutedEdge, len(s.routes))
	for index, edge := range s.routes {
		result[index] = edge
		result[index].Points = append([]geom.Point(nil), edge.Points...)
		result[index].Source = cloneEndpoint(edge.Source)
		result[index].Target = cloneEndpoint(edge.Target)
		result[index].Start.Semantic = cloneEndpoint(edge.Start.Semantic)
		result[index].End.Semantic = cloneEndpoint(edge.End.Semantic)
	}
	return result
}

func (s *Snapshot) LabelIssues() []LabelIssue {
	index, err := BuildIndex(s)
	if err != nil {
		return nil
	}
	labelCollisions := make(map[string]bool)
	for pair, collided := range index.labelPair {
		if !collided {
			continue
		}
		separator := -1
		for position := range pair {
			if pair[position] == 0 {
				separator = position
				break
			}
		}
		if separator >= 0 {
			labelCollisions[pair[:separator]] = true
			labelCollisions[pair[separator+1:]] = true
		}
	}
	var issues []LabelIssue
	for _, edge := range s.routes {
		if edge.Label == "" || edge.LineStyle == diagram.LineInvisible {
			continue
		}
		issue := LabelIssue{
			EdgeID: edge.ID, Unplaced: !edge.LabelPlaced, NodeCollision: index.labelNode[edge.ID] > 0,
			LabelCollision: labelCollisions[edge.ID], EdgeCollision: index.labelEdge[edge.ID],
		}
		if issue.Unplaced || issue.NodeCollision || issue.LabelCollision || issue.EdgeCollision {
			issues = append(issues, issue)
		}
	}
	return issues
}

func cloneRects(source map[string]geom.Rect) map[string]geom.Rect {
	result := make(map[string]geom.Rect, len(source))
	for id, rect := range source {
		result[id] = rect
	}
	return result
}

func cloneStrings(source [][]string) [][]string {
	result := make([][]string, len(source))
	for index := range source {
		result[index] = append([]string(nil), source[index]...)
	}
	return result
}

func cloneEndpoint(source diagram.Endpoint) diagram.Endpoint {
	result := source
	if source.PortHint.Offset != nil {
		offset := *source.PortHint.Offset
		result.PortHint.Offset = &offset
	}
	return result
}

func copyLayoutMetrics(source layout.Metrics) LayoutMetrics {
	return LayoutMetrics{
		Crossings: source.Crossings, DummyNodes: source.DummyNodes, LongEdges: source.LongEdges,
		Width: source.Width, Height: source.Height, LayerCount: source.LayerCount, SweepRounds: source.SweepRounds,
		ReversedEdges: source.ReversedEdges, SelfLoops: source.SelfLoops,
	}
}

func copyRouteMetrics(source route.Metrics) RouteMetrics {
	return RouteMetrics{
		NodeOverlaps: source.NodeOverlaps, EdgeNodeCollisions: source.EdgeNodeCollisions,
		Crossings: source.Crossings, Overlaps: source.Overlaps, TotalLength: source.TotalLength,
		ExcessLength: source.ExcessLength, Bends: source.Bends, ReverseMoves: source.ReverseMoves,
		AStarFallbacks: source.AStarFallbacks, UnroutedEdges: source.UnroutedEdges,
		LabelNodeCollisions: source.LabelNodeCollisions, LabelLabelCollisions: source.LabelLabelCollisions,
		LabelEdgeCollisions: source.LabelEdgeCollisions, UnplacedLabels: source.UnplacedLabels,
		GroupBoundaryOverlaps: source.GroupBoundaryOverlaps,
	}
}
