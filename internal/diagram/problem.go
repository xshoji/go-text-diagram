// Package diagram owns the immutable semantic input to the solver.
package diagram

import "fmt"

type NodeID = string
type GroupID = string
type EdgeID = string
type CellID = string
type PortID = string

type Position struct {
	Line   int
	Column int
}

type SourceSpan struct {
	Format string
	Start  Position
	End    Position
}

type Direction uint8

const (
	DirectionDown Direction = iota
	DirectionRight
	DirectionUp
	DirectionLeft
)

func (d Direction) Vertical() bool { return d == DirectionDown || d == DirectionUp }

func (d Direction) ForwardSign() int {
	if d == DirectionUp || d == DirectionLeft {
		return -1
	}
	return 1
}

func (d Direction) String() string {
	switch d {
	case DirectionDown:
		return "down"
	case DirectionRight:
		return "right"
	case DirectionUp:
		return "up"
	case DirectionLeft:
		return "left"
	default:
		return fmt.Sprintf("direction(%d)", d)
	}
}

type EdgeKind uint8

const (
	Directed EdgeKind = iota
	Undirected
	Bidirectional
)

type LineStyle uint8

const (
	LineSolid LineStyle = iota
	LineDotted
	LineDashed
	LineDouble
	LineBold
	LineInvisible
)

type ArrowStyle uint8

const (
	ArrowDefault ArrowStyle = iota
	ArrowClosed
	ArrowOpen
	ArrowFilled
	ArrowNone
)

type Shape uint8

const (
	ShapeRect Shape = iota
	ShapeRounded
	ShapePoint
	ShapeNone
	ShapeInvisible
)

type Align uint8

const (
	AlignCenter Align = iota
	AlignLeft
	AlignRight
)

type Side uint8

const (
	SideAuto Side = iota
	SideNorth
	SideEast
	SideSouth
	SideWest
	SideFront
	SideBack
	SideLeft
	SideRight
)

type PortHint struct {
	Side   Side
	Offset *int
}

func (s Side) String() string {
	switch s {
	case SideNorth:
		return "north"
	case SideEast:
		return "east"
	case SideSouth:
		return "south"
	case SideWest:
		return "west"
	case SideFront:
		return "front"
	case SideBack:
		return "back"
	case SideLeft:
		return "left"
	case SideRight:
		return "right"
	default:
		return "auto"
	}
}

type EndpointKind uint8

const (
	EndpointNode EndpointKind = iota
	EndpointGroup
)

type Endpoint struct {
	Kind      EndpointKind
	Node      NodeID
	Group     GroupID
	Cell      CellID
	NamedPort PortID
	PortHint  PortHint
}

func NodeEndpoint(id NodeID) Endpoint { return Endpoint{Kind: EndpointNode, Node: id} }

func GroupEndpoint(id GroupID) Endpoint { return Endpoint{Kind: EndpointGroup, Group: id} }

func (e Endpoint) ID() string {
	if e.Kind == EndpointGroup {
		return string(e.Group)
	}
	return string(e.Node)
}

type NamedPortDirection uint8

const (
	PortUnspecified NamedPortDirection = iota
	PortInput
	PortOutput
)

type NamedPort struct {
	ID        PortID
	Direction NamedPortDirection
	Source    SourceSpan
}

type Cell struct {
	ID     CellID
	Label  string
	Source SourceSpan
}

type RankConstraint struct {
	Fixed *int
	Root  bool
	Same  string
}

type Node struct {
	ID         NodeID
	Label      string
	InputOrder int
	Rank       RankConstraint
	Shape      Shape
	Align      Align
	TextWrap   int
	Cells      [][]Cell
	Ports      []NamedPort
	Source     SourceSpan
}

type Edge struct {
	ID            EdgeID
	Source        Endpoint
	Target        Endpoint
	Label         string
	InputOrder    int
	Kind          EdgeKind
	MinLength     int
	Start         PortHint
	End           PortHint
	LineStyle     LineStyle
	ArrowStyle    ArrowStyle
	LayoutReverse bool
	SourceSpan    SourceSpan
}

type Group struct {
	ID         GroupID
	Label      string
	Members    []NodeID
	Parent     GroupID
	InputOrder int
	Padding    int
	LineStyle  LineStyle
	Anonymous  bool
	Ports      []NamedPort
	Source     SourceSpan
}

type ElementKind uint8

const (
	ElementNode ElementKind = iota
	ElementGroup
)

type ElementRef struct {
	Kind  ElementKind
	Node  NodeID
	Group GroupID
}

type Problem struct {
	format    string
	direction Direction
	nodes     []Node
	groups    []Group
	edges     []Edge
	nodeByID  map[NodeID]int
	groupByID map[GroupID]int
	edgeByID  map[EdgeID]int
	parentOf  map[ElementRef]GroupID
}

func (p *Problem) Format() string {
	if p == nil {
		return ""
	}
	return p.format
}

func (p *Problem) Direction() Direction {
	if p == nil {
		return DirectionDown
	}
	return p.direction
}

func (p *Problem) Nodes() []Node {
	if p == nil {
		return nil
	}
	return cloneNodes(p.nodes)
}

func (p *Problem) Groups() []Group {
	if p == nil {
		return nil
	}
	return cloneGroups(p.groups)
}

func (p *Problem) Edges() []Edge {
	if p == nil {
		return nil
	}
	return cloneEdges(p.edges)
}

func (p *Problem) Node(id NodeID) (Node, bool) {
	if p == nil {
		return Node{}, false
	}
	index, ok := p.nodeByID[id]
	if !ok {
		return Node{}, false
	}
	return cloneNode(p.nodes[index]), true
}

func (p *Problem) Group(id GroupID) (Group, bool) {
	if p == nil {
		return Group{}, false
	}
	index, ok := p.groupByID[id]
	if !ok {
		return Group{}, false
	}
	return cloneGroup(p.groups[index]), true
}

func (p *Problem) Edge(id EdgeID) (Edge, bool) {
	if p == nil {
		return Edge{}, false
	}
	index, ok := p.edgeByID[id]
	if !ok {
		return Edge{}, false
	}
	return cloneEdge(p.edges[index]), true
}

func (p *Problem) ParentOf(ref ElementRef) (GroupID, bool) {
	if p == nil {
		return "", false
	}
	parent, ok := p.parentOf[ref]
	return parent, ok
}

func (p *Problem) WithDirection(direction Direction) *Problem {
	if p == nil {
		return nil
	}
	clone := *p
	clone.direction = direction
	return &clone
}

func (p *Problem) String() string {
	if p == nil {
		return "<nil>"
	}
	return fmt.Sprintf("Problem{nodes:%d groups:%d edges:%d}", len(p.nodes), len(p.groups), len(p.edges))
}
