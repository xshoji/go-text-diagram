package layout

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/geom"
)

var (
	ErrInvalidGraph = errors.New("invalid graph")
	ErrCyclicGraph  = errors.New("cyclic graph")
)

const maximumLayoutCells int64 = 1_000_000

type Rect = geom.Rect

type Node struct {
	ID             string
	OriginalNodeID string
	Component      int
	Label          string
	Rank           int
	Order          int
	Dummy          bool
	Rect           Rect
	Shape          diagram.Shape
	Align          diagram.Align
	TextWrap       int
	Cells          [][]string
	CellIDs        [][]string
}

type Group struct {
	ID        string
	Label     string
	Parent    string
	Rect      Rect
	LineStyle diagram.LineStyle
}

type Edge struct {
	ID             string
	OriginalEdgeID string
	Component      int
	From           string
	To             string
	Label          string
	OriginalFrom   string
	OriginalTo     string
	Segment        int
	Reversed       bool
	SelfLoop       bool
	Kind           diagram.EdgeKind
	Start          diagram.PortHint
	End            diagram.PortHint
	MinLength      int
	LineStyle      diagram.LineStyle
	ArrowStyle     diagram.ArrowStyle
	Source         diagram.Endpoint
	Target         diagram.Endpoint
}

type Metrics struct {
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

type Layout struct {
	Direction      diagram.Direction
	Nodes          []*Node
	Edges          []*Edge
	Groups         []*Group
	Metrics        Metrics
	OuterLaneStart int
	LabelLaneSize  int
	FlowApplied    bool
}

type Options struct {
	NodeWidth    int
	NodeHeight   int
	NodeSpacing  int
	LayerSpacing int
	SweepRounds  int
	MaxWidth     int
	FeedbackArc  bool
	Straighten   bool
	FlowGroups   bool
	// RankCompactionSteps bounds greedy grouped-node rank moves. Zero keeps
	// the assigned ranks unchanged so callers can retain an uncompacted
	// quality baseline.
	RankCompactionSteps int
}

func DefaultOptions() Options {
	return Options{
		NodeWidth:    3,
		NodeHeight:   3,
		NodeSpacing:  3,
		LayerSpacing: 2,
		SweepRounds:  6,
	}
}

func normalizeOptions(options Options) Options {
	defaults := DefaultOptions()
	if options.NodeWidth <= 0 {
		options.NodeWidth = defaults.NodeWidth
	}
	if options.NodeHeight <= 0 {
		options.NodeHeight = defaults.NodeHeight
	}
	if options.NodeSpacing <= 0 {
		options.NodeSpacing = defaults.NodeSpacing
	}
	if options.LayerSpacing <= 0 {
		options.LayerSpacing = defaults.LayerSpacing
	}
	if options.SweepRounds <= 0 {
		options.SweepRounds = defaults.SweepRounds
	}
	return options
}

type workingNode struct {
	id             string
	originalNodeID string
	ownerEdgeID    string
	component      int
	label          string
	inputOrder     int
	rank           int
	order          int
	dummy          bool
	incomingPorts  int
	outgoingPorts  int
	width          int
	height         int
	rankConstraint diagram.RankConstraint
	shape          diagram.Shape
	align          diagram.Align
	textWrap       int
	cells          [][]string
	cellIDs        [][]string
}

type workingEdge struct {
	id             string
	originalEdgeID string
	from           *workingNode
	to             *workingNode
	label          string
	inputOrder     int
	segment        int
	originalFrom   string
	originalTo     string
	reversed       bool
	selfLoop       bool
	kind           diagram.EdgeKind
	start          diagram.PortHint
	end            diagram.PortHint
	minLength      int
	lineStyle      diagram.LineStyle
	arrowStyle     diagram.ArrowStyle
	source         diagram.Endpoint
	target         diagram.Endpoint
}

func Compute(graph *diagram.Problem, options Options) (*Layout, error) {
	options = normalizeOptions(options)
	if graph == nil {
		return nil, fmt.Errorf("%w: graph is nil", ErrInvalidGraph)
	}
	for _, option := range []struct {
		name  string
		value int
	}{
		{name: "node width", value: options.NodeWidth},
		{name: "node height", value: options.NodeHeight},
		{name: "node spacing", value: options.NodeSpacing},
		{name: "layer spacing", value: options.LayerSpacing},
	} {
		if int64(option.value) > maximumLayoutCells {
			return nil, fmt.Errorf("%w: %s %d exceeds %d cells", ErrInvalidGraph, option.name, option.value, maximumLayoutCells)
		}
	}
	if graph.Direction() != diagram.DirectionDown && graph.Direction() != diagram.DirectionRight &&
		graph.Direction() != diagram.DirectionUp && graph.Direction() != diagram.DirectionLeft {
		return nil, fmt.Errorf("%w: unsupported direction %v", ErrInvalidGraph, graph.Direction())
	}

	nodes, nodeByID, edges, err := buildWorkingGraph(graph)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return &Layout{Direction: graph.Direction()}, nil
	}
	edges, loops, reversedEdges := makeAcyclic(nodes, edges, options.FeedbackArc)
	order, err := topologicalOrder(nodes, edges)
	if err != nil {
		return nil, err
	}
	if err := assignRanks(order, edges); err != nil {
		return nil, err
	}
	flowApplied := false
	if options.FlowGroups && graph.Direction().Vertical() {
		flowApplied = applyGroupFlowRanks(graph.Groups(), nodes, edges, options.LayerSpacing)
	}
	if options.RankCompactionSteps > 0 {
		compactGroupRanks(graph.Groups(), nodes, edges, options.RankCompactionSteps)
	}
	if err := validateDummyExpansion(edges); err != nil {
		return nil, err
	}
	nodes, edges, longEdges := insertDummyNodes(nodes, nodeByID, edges)
	assignPortRequirements(nodes, edges)
	measureNodeSizes(nodes, graph.Direction(), options)
	layers := makeLayers(nodes)
	crossings, rounds := reduceCrossings(layers, edges, options.SweepRounds)
	labelLaneSize, labelMargin := measureEdgeLabels(append(edges, loops...), graph.Direction())
	if labelLaneSize > options.LayerSpacing {
		options.LayerSpacing = labelLaneSize
	}
	if spacing := maxSelfLoopsPerNode(loops) + 1; spacing > options.NodeSpacing {
		options.NodeSpacing = spacing
	}
	initialRects, _, _ := assignCoordinates(layers, graph.Direction(), options)
	const routeLaneSize = 1
	if spacing := requiredLayerSpacing(edges, initialRects, graph.Direction(), options.LayerSpacing, routeLaneSize); spacing > options.LayerSpacing {
		options.LayerSpacing = spacing
	}
	options.LayerSpacing += reversedEdges
	positionNodes := func() (map[*workingNode]Rect, int, int, error) {
		rects, _, _ := assignCoordinates(layers, graph.Direction(), options)
		if options.Straighten {
			straightenCoordinates(layers, edges, rects, graph.Direction().Vertical())
		}
		if flowApplied {
			alignFlowCoordinates(layers, edges, rects, graph.Direction().Vertical(), options.NodeSpacing)
		}
		width, height := rectExtent(rects)
		if options.MaxWidth > 0 {
			width, height = packComponents(nodes, append(edges, loops...), rects, options.NodeSpacing, options.MaxWidth)
		}
		if labelMargin > 0 {
			if graph.Direction().Vertical() {
				for node, rect := range rects {
					rect.X += labelMargin
					rects[node] = rect
				}
				width += labelMargin * 2
			} else {
				for node, rect := range rects {
					rect.Y += labelMargin
					rects[node] = rect
				}
				height += labelMargin * 2
			}
		}
		if err := packGroupScopes(graph.Groups(), nodes, append(edges, loops...), rects, graph.Direction().Vertical(), flowApplied, options.NodeSpacing); err != nil {
			return nil, 0, 0, err
		}
		if flowApplied {
			normalizeRects(rects)
		}
		width, height = rectExtent(rects)
		return rects, width, height, nil
	}
	rects, width, height, err := positionNodes()
	if err != nil {
		return nil, err
	}
	if flowApplied {
		if spacing := requiredLayerSpacing(edges, rects, graph.Direction(), options.LayerSpacing, routeLaneSize); spacing > options.LayerSpacing {
			options.LayerSpacing = spacing
			rects, width, height, err = positionNodes()
			if err != nil {
				return nil, err
			}
		}
	}
	outerLaneStart := 0
	width, height = reserveSpecialRouteSpace(
		rects, loops, graph.Direction(), reversedEdges, options.LayerSpacing, width, height, &outerLaneStart,
	)
	if hasExplicitPorts(edges, loops) {
		for node, rect := range rects {
			rect.X += 2
			rect.Y += 2
			rects[node] = rect
		}
		width += 4
		height += 4
		outerLaneStart += 2
	}
	groupRects, groupShift, err := buildGroupRects(graph.Groups(), rects, nodeByID)
	if err != nil {
		return nil, err
	}
	if groupShift > 0 {
		for node, rect := range rects {
			rect.X += groupShift
			rect.Y += groupShift
			rects[node] = rect
		}
		for id, rect := range groupRects {
			rect.X += groupShift
			rect.Y += groupShift
			groupRects[id] = rect
		}
		width += groupShift
		height += groupShift
		outerLaneStart += groupShift
	}
	for _, rect := range groupRects {
		width = max(width, rect.X+rect.Width)
		height = max(height, rect.Y+rect.Height)
	}
	if width < 0 || height < 0 || width > int(maximumLayoutCells) || height > int(maximumLayoutCells) ||
		int64(width)*int64(height) > maximumLayoutCells {
		return nil, fmt.Errorf("%w: layout bounds %dx%d exceed %d cells", ErrInvalidGraph, width, height, maximumLayoutCells)
	}
	edges = append(edges, loops...)

	result := &Layout{
		Direction: graph.Direction(),
		Metrics: Metrics{
			Crossings:     crossings,
			DummyNodes:    len(nodes) - len(graph.Nodes()),
			LongEdges:     longEdges,
			Width:         width,
			Height:        height,
			LayerCount:    len(layers),
			SweepRounds:   rounds,
			ReversedEdges: reversedEdges,
			SelfLoops:     len(loops),
		},
		OuterLaneStart: outerLaneStart,
		LabelLaneSize:  routeLaneSize,
		FlowApplied:    flowApplied,
	}

	for _, layer := range layers {
		for _, node := range layer {
			result.Nodes = append(result.Nodes, &Node{
				ID:             node.id,
				OriginalNodeID: node.originalNodeID,
				Component:      node.component,
				Label:          node.label,
				Rank:           node.rank,
				Order:          node.order,
				Dummy:          node.dummy,
				Rect:           rects[node],
				Shape:          node.shape,
				Align:          node.align,
				TextWrap:       node.textWrap,
				Cells:          node.cells,
				CellIDs:        node.cellIDs,
			})
		}
	}
	for _, group := range graph.Groups() {
		result.Groups = append(result.Groups, &Group{
			ID: group.ID, Label: group.Label, Parent: group.Parent, Rect: groupRects[group.ID], LineStyle: group.LineStyle,
		})
	}

	sort.SliceStable(edges, func(i, j int) bool {
		if edges[i].inputOrder != edges[j].inputOrder {
			return edges[i].inputOrder < edges[j].inputOrder
		}
		return edges[i].segment < edges[j].segment
	})
	for _, edge := range edges {
		result.Edges = append(result.Edges, &Edge{
			ID:             edge.id,
			OriginalEdgeID: edge.originalEdgeID,
			Component:      edge.from.component,
			From:           edge.from.id,
			To:             edge.to.id,
			Label:          edge.label,
			OriginalFrom:   edge.originalFrom,
			OriginalTo:     edge.originalTo,
			Segment:        edge.segment,
			Reversed:       edge.reversed,
			SelfLoop:       edge.selfLoop,
			Kind:           edge.kind,
			Start:          edge.start,
			End:            edge.end,
			MinLength:      edge.minLength,
			LineStyle:      edge.lineStyle,
			ArrowStyle:     edge.arrowStyle,
			Source:         edge.source,
			Target:         edge.target,
		})
	}

	return result, nil
}

func buildWorkingGraph(graph *diagram.Problem) ([]*workingNode, map[string]*workingNode, []*workingEdge, error) {
	nodes := make([]*workingNode, 0, len(graph.Nodes()))
	nodeByID := make(map[string]*workingNode, len(graph.Nodes()))
	for _, original := range graph.Nodes() {
		if original.ID == "" {
			return nil, nil, nil, fmt.Errorf("%w: node ID is empty", ErrInvalidGraph)
		}
		if _, exists := nodeByID[original.ID]; exists {
			return nil, nil, nil, fmt.Errorf("%w: duplicate node %q", ErrInvalidGraph, original.ID)
		}
		cells, cellIDs := make([][]string, len(original.Cells)), make([][]string, len(original.Cells))
		for row := range original.Cells {
			for _, cell := range original.Cells[row] {
				cells[row] = append(cells[row], cell.Label)
				cellIDs[row] = append(cellIDs[row], cell.ID)
			}
		}
		node := &workingNode{
			id:             original.ID,
			originalNodeID: original.ID,
			label:          original.Label,
			inputOrder:     original.InputOrder,
			rankConstraint: original.Rank,
			shape:          original.Shape,
			align:          original.Align,
			textWrap:       original.TextWrap,
			cells:          cells,
			cellIDs:        cellIDs,
		}
		nodes = append(nodes, node)
		nodeByID[node.id] = node
	}

	edges := make([]*workingEdge, 0, len(graph.Edges()))
	edgeIDs := make(map[string]bool, len(graph.Edges()))
	for _, original := range graph.Edges() {
		if original.ID == "" || edgeIDs[original.ID] {
			return nil, nil, nil, fmt.Errorf("%w: edge ID %q is empty or duplicated", ErrInvalidGraph, original.ID)
		}
		edgeIDs[original.ID] = true
		source, target := original.Source, original.Target
		if source.PortHint.Side == diagram.SideAuto {
			source.PortHint = original.Start
		}
		if target.PortHint.Side == diagram.SideAuto {
			target.PortHint = original.End
		}
		from, err := endpointAnchor(source, graph, nodeByID)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%w: edge %q source: %v", ErrInvalidGraph, original.ID, err)
		}
		to, err := endpointAnchor(target, graph, nodeByID)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%w: edge %q target: %v", ErrInvalidGraph, original.ID, err)
		}
		originalFrom, originalTo := from.id, to.id
		if original.LayoutReverse && from != to {
			from, to = to, from
		}
		edges = append(edges, &workingEdge{
			id:             original.ID,
			originalEdgeID: original.ID,
			from:           from,
			to:             to,
			label:          original.Label,
			inputOrder:     original.InputOrder,
			originalFrom:   originalFrom,
			originalTo:     originalTo,
			reversed:       original.LayoutReverse,
			kind:           original.Kind,
			start:          source.PortHint,
			end:            target.PortHint,
			minLength:      max(1, original.MinLength),
			lineStyle:      original.LineStyle,
			arrowStyle:     original.ArrowStyle,
			source:         source,
			target:         target,
		})
	}
	return nodes, nodeByID, edges, nil
}

func endpointAnchor(endpoint diagram.Endpoint, graph *diagram.Problem, nodes map[string]*workingNode) (*workingNode, error) {
	if endpoint.Kind == diagram.EndpointNode {
		node := nodes[endpoint.ID()]
		if node == nil {
			return nil, fmt.Errorf("unknown node %q", endpoint.ID())
		}
		if endpoint.Cell != "" && !hasCellID(node.cellIDs, endpoint.Cell) {
			return nil, fmt.Errorf("node %q has no cell %q", endpoint.ID(), endpoint.Cell)
		}
		return node, nil
	}
	group, ok := graph.Group(endpoint.ID())
	if !ok {
		return nil, fmt.Errorf("unknown group %q", endpoint.ID())
	}
	visiting := make(map[string]bool)
	var find func(diagram.Group) (*workingNode, error)
	find = func(current diagram.Group) (*workingNode, error) {
		if visiting[current.ID] {
			return nil, fmt.Errorf("group parent cycle at %q", current.ID)
		}
		visiting[current.ID] = true
		defer delete(visiting, current.ID)
		for _, member := range current.Members {
			if node := nodes[member]; node != nil {
				return node, nil
			}
		}
		for _, child := range graph.Groups() {
			if child.Parent == current.ID {
				if node, err := find(child); err == nil && node != nil {
					return node, nil
				}
			}
		}
		return nil, fmt.Errorf("group %q has no node member", current.ID)
	}
	return find(group)
}

func hasCellID(rows [][]string, id string) bool {
	for _, row := range rows {
		for _, current := range row {
			if current == id {
				return true
			}
		}
	}
	return false
}

func hasExplicitPorts(groups ...[]*workingEdge) bool {
	for _, edges := range groups {
		for _, edge := range edges {
			if edge.start.Side != diagram.SideAuto || edge.end.Side != diagram.SideAuto {
				return true
			}
		}
	}
	return false
}

func (l *Layout) DebugString() string {
	var out strings.Builder
	fmt.Fprintf(&out, "direction: %s\n", l.Direction)
	fmt.Fprintf(&out, "bounds: %dx%d\n", l.Metrics.Width, l.Metrics.Height)
	fmt.Fprintf(&out, "metrics: layers=%d crossings=%d dummies=%d long_edges=%d sweeps=%d\n",
		l.Metrics.LayerCount,
		l.Metrics.Crossings,
		l.Metrics.DummyNodes,
		l.Metrics.LongEdges,
		l.Metrics.SweepRounds,
	)
	if l.Metrics.ReversedEdges > 0 || l.Metrics.SelfLoops > 0 {
		fmt.Fprintf(&out, "cycles: reversed_edges=%d self_loops=%d\n", l.Metrics.ReversedEdges, l.Metrics.SelfLoops)
	}

	lastRank := -1
	for _, node := range l.Nodes {
		if node.Rank != lastRank {
			fmt.Fprintf(&out, "rank %d:\n", node.Rank)
			lastRank = node.Rank
		}
		kind := "node"
		if node.Dummy {
			kind = "dummy"
		}
		fmt.Fprintf(&out, "  %s %q order=%d rect=(%d,%d %dx%d)\n",
			kind, node.ID, node.Order, node.Rect.X, node.Rect.Y, node.Rect.Width, node.Rect.Height)
	}
	if len(l.Edges) > 0 {
		out.WriteString("edges:\n")
		for _, edge := range l.Edges {
			fmt.Fprintf(&out, "  %s %q -> %q original=%s segment=%d\n",
				edge.ID, edge.From, edge.To, edge.OriginalEdgeID, edge.Segment)
		}
	}
	return out.String()
}
