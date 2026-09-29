package diagram

import (
	"fmt"
	"sort"
)

type Builder struct {
	format    string
	direction Direction
	nodes     []*Node
	groups    []*Group
	edges     []*Edge
	nodeByID  map[NodeID]*Node
	groupByID map[GroupID]*Group
}

func NewBuilder(format string) *Builder {
	return &Builder{
		format:    format,
		direction: DirectionDown,
		nodeByID:  make(map[NodeID]*Node),
		groupByID: make(map[GroupID]*Group),
	}
}

func (b *Builder) Direction() Direction { return b.direction }

func (b *Builder) SetDirection(direction Direction) { b.direction = direction }

func (b *Builder) AddNode(id NodeID, label string, source SourceSpan) *Node {
	if label == "" {
		label = string(id)
	}
	node := &Node{ID: id, Label: label, InputOrder: len(b.nodes), Source: source}
	b.nodes = append(b.nodes, node)
	if b.nodeByID[id] == nil {
		b.nodeByID[id] = node
	}
	return node
}

func (b *Builder) EnsureNode(id NodeID, label string, source SourceSpan) *Node {
	if node := b.nodeByID[id]; node != nil {
		if label != "" && (node.Label == "" || node.Label == string(node.ID)) {
			node.Label = label
		}
		if node.Source == (SourceSpan{}) {
			node.Source = source
		}
		return node
	}
	return b.AddNode(id, label, source)
}

func (b *Builder) AddGroup(id GroupID, members []NodeID, label string, parent GroupID, padding int, lineStyle LineStyle, anonymous bool, source SourceSpan) *Group {
	group := &Group{
		ID: id, Members: append([]NodeID(nil), members...), Label: label, Parent: parent,
		InputOrder: len(b.groups), Padding: max(1, padding), LineStyle: lineStyle,
		Anonymous: anonymous, Source: source,
	}
	if group.Label == "" && !anonymous {
		group.Label = string(id)
	}
	b.groups = append(b.groups, group)
	if b.groupByID[id] == nil {
		b.groupByID[id] = group
	}
	return group
}

func (b *Builder) AddEdge(source, target Endpoint, label string, kind EdgeKind, span SourceSpan) *Edge {
	edge := &Edge{
		ID: EdgeID(fmt.Sprintf("e%d", len(b.edges))), Source: source, Target: target,
		Label: label, InputOrder: len(b.edges), Kind: kind, MinLength: 1,
		SourceSpan: span,
	}
	b.edges = append(b.edges, edge)
	return edge
}

func (b *Builder) Node(id NodeID) (*Node, bool) {
	node, ok := b.nodeByID[id]
	return node, ok
}

func (b *Builder) Group(id GroupID) (*Group, bool) {
	group, ok := b.groupByID[id]
	return group, ok
}

func (b *Builder) Nodes() []*Node { return b.nodes }

func (b *Builder) Edges() []*Edge { return b.edges }

func (b *Builder) RemoveNodes(ids map[NodeID]bool) {
	kept := b.nodes[:0]
	for _, node := range b.nodes {
		if ids[node.ID] {
			delete(b.nodeByID, node.ID)
			continue
		}
		kept = append(kept, node)
	}
	b.nodes = kept
}

func (b *Builder) Build() (*Problem, error) {
	problem := &Problem{
		format:    b.format,
		direction: b.direction,
		nodeByID:  make(map[NodeID]int, len(b.nodes)),
		groupByID: make(map[GroupID]int, len(b.groups)),
		edgeByID:  make(map[EdgeID]int, len(b.edges)),
		parentOf:  make(map[ElementRef]GroupID),
	}
	for _, node := range b.nodes {
		if node != nil {
			problem.nodes = append(problem.nodes, cloneNode(*node))
		}
	}
	for _, group := range b.groups {
		if group != nil {
			problem.groups = append(problem.groups, cloneGroup(*group))
		}
	}
	for _, edge := range b.edges {
		if edge != nil {
			problem.edges = append(problem.edges, cloneEdge(*edge))
		}
	}
	sort.SliceStable(problem.nodes, func(i, j int) bool { return problem.nodes[i].InputOrder < problem.nodes[j].InputOrder })
	sort.SliceStable(problem.groups, func(i, j int) bool { return problem.groups[i].InputOrder < problem.groups[j].InputOrder })
	sort.SliceStable(problem.edges, func(i, j int) bool { return problem.edges[i].InputOrder < problem.edges[j].InputOrder })
	if err := problem.initializeAndValidate(); err != nil {
		return nil, err
	}
	return problem, nil
}

func Span(format string, line, column int) SourceSpan {
	return SourceSpan{Format: format, Start: Position{Line: line, Column: column}, End: Position{Line: line, Column: column}}
}
