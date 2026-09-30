package plantuml

import (
	"io"
	"strings"

	"github.com/xshoji/go-text-diagram/internal/diagram"
)

type Options struct {
	Strict bool
}

func ParseProblem(r io.Reader) (*diagram.Problem, error) {
	return ParseProblemWithOptions(r, Options{})
}

func ParseProblemWithOptions(r io.Reader, options Options) (*diagram.Problem, error) {
	document, err := ParseDocumentWithOptions(r, options)
	if err != nil {
		return nil, err
	}
	return resolve(document)
}

type symbol struct {
	id      string
	label   string
	kind    diagram.EndpointKind
	members map[string]bool
	ports   map[string]diagram.NamedPortDirection
}

type portSymbol struct {
	owner     *symbol
	direction diagram.NamedPortDirection
}

type pendingRelation struct {
	decl   *RelationDecl
	parent string
}

type resolver struct {
	graph        *diagram.Builder
	allowMixing  bool
	symbols      map[string]*symbol
	labels       map[string]*symbol
	ports        map[string]portSymbol
	relations    []pendingRelation
	groupMember  map[string]map[string]bool
	hasClass     bool
	hasComponent bool
	mixingLine   int
}

func resolve(document *Document) (*diagram.Problem, error) {
	r := &resolver{
		graph:       diagram.NewBuilder("plantuml"),
		allowMixing: document.AllowMixing,
		symbols:     make(map[string]*symbol),
		labels:      make(map[string]*symbol),
		ports:       make(map[string]portSymbol),
		groupMember: make(map[string]map[string]bool),
	}
	if err := r.register(document.Statements, ""); err != nil {
		return nil, err
	}
	if err := r.resolveRelations(); err != nil {
		return nil, err
	}
	if !r.allowMixing && r.hasClass && r.hasComponent {
		return nil, syntax(r.mixingLine, 1, "mixing class and component declarations requires allowmixing")
	}
	for groupID, members := range r.groupMember {
		group, ok := r.graph.Group(diagram.GroupID(groupID))
		if !ok {
			continue
		}
		group.Members = group.Members[:0]
		for _, node := range r.graph.Nodes() {
			if members[string(node.ID)] {
				group.Members = append(group.Members, node.ID)
			}
		}
	}
	return r.graph.Build()
}

func (r *resolver) register(statements []Statement, parent string) error {
	for _, statement := range statements {
		switch current := statement.(type) {
		case *RelationDecl:
			r.relations = append(r.relations, pendingRelation{decl: current, parent: parent})
		case *ElementDecl:
			if current.Kind == ElementTogether {
				switch current.Alias {
				case "left to right direction":
					r.graph.SetDirection(diagram.DirectionRight)
				case "top to bottom direction", "":
					if current.Alias != "" {
						r.graph.SetDirection(diagram.DirectionDown)
					}
				}
				if err := r.register(current.Statements, parent); err != nil {
					return err
				}
				continue
			}
			if err := r.registerElement(current, parent); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *resolver) registerElement(decl *ElementDecl, parent string) error {
	if decl.Alias == "" {
		return syntax(decl.Position.Line, decl.Position.Column, "element has no identifier")
	}
	if _, exists := r.symbols[decl.Alias]; exists {
		return syntax(decl.Position.Line, decl.Position.Column, "duplicate element alias %q", decl.Alias)
	}
	if _, exists := r.ports[decl.Alias]; exists {
		return syntax(decl.Position.Line, decl.Position.Column, "element alias %q conflicts with a port", decl.Alias)
	}
	if decl.Kind == ElementClass {
		r.hasClass = true
	} else if decl.Kind == ElementComponent || decl.Kind == ElementRectangle || decl.Kind == ElementCircle {
		r.hasComponent, r.mixingLine = true, decl.Position.Line
	}
	isGroup := isGroupElement(decl)
	span := diagram.Span("plantuml", decl.Position.Line, decl.Position.Column)
	sym := &symbol{id: decl.Alias, label: decl.Label, members: make(map[string]bool), ports: make(map[string]diagram.NamedPortDirection)}
	if isGroup {
		sym.kind = diagram.EndpointGroup
		r.graph.AddGroup(diagram.GroupID(decl.Alias), nil, decl.Label, diagram.GroupID(parent), 2, diagram.LineSolid, false, span)
		r.groupMember[decl.Alias] = make(map[string]bool)
	} else {
		sym.kind = diagram.EndpointNode
		node := r.graph.AddNode(diagram.NodeID(decl.Alias), decl.Label, span)
		switch decl.Kind {
		case ElementCircle:
			node.Shape = diagram.ShapePoint
		case ElementClass:
			node.Cells = append(node.Cells, []diagram.Cell{{Label: decl.Label, Source: span}})
			for _, member := range decl.Members {
				node.Cells = append(node.Cells, []diagram.Cell{{
					ID: diagram.CellID(member.ID), Label: member.Label,
					Source: diagram.Span("plantuml", member.Position.Line, member.Position.Column),
				}})
				sym.members[member.ID] = true
			}
		}
	}
	r.symbols[decl.Alias] = sym
	if existing := r.labels[decl.Label]; existing == nil {
		r.labels[decl.Label] = sym
	} else {
		r.labels[decl.Label] = &symbol{}
	}
	if parent != "" && !isGroup {
		r.addGroupMember(parent, decl.Alias)
	}
	for _, port := range decl.Ports {
		if _, exists := r.ports[port.ID]; exists {
			return syntax(port.Position.Line, port.Position.Column, "duplicate port %q", port.ID)
		}
		if _, exists := r.symbols[port.ID]; exists {
			return syntax(port.Position.Line, port.Position.Column, "port %q conflicts with an element alias", port.ID)
		}
		direction := diagram.PortUnspecified
		if port.Direction == "portin" {
			direction = diagram.PortInput
		} else if port.Direction == "portout" {
			direction = diagram.PortOutput
		}
		sym.ports[port.ID] = direction
		r.ports[port.ID] = portSymbol{owner: sym, direction: direction}
		named := diagram.NamedPort{ID: diagram.PortID(port.ID), Direction: direction, Source: diagram.Span("plantuml", port.Position.Line, port.Position.Column)}
		if isGroup {
			group, _ := r.graph.Group(diagram.GroupID(decl.Alias))
			group.Ports = append(group.Ports, named)
		} else {
			node, _ := r.graph.Node(diagram.NodeID(decl.Alias))
			node.Ports = append(node.Ports, named)
		}
	}
	childParent := parent
	if isGroup {
		childParent = decl.Alias
	}
	return r.register(decl.Statements, childParent)
}

func (r *resolver) resolveRelations() error {
	for _, pending := range r.relations {
		source, err := r.endpoint(pending.decl.Source, pending.parent)
		if err != nil {
			return err
		}
		target, err := r.endpoint(pending.decl.Target, pending.parent)
		if err != nil {
			return err
		}
		kind, reverse := relationKind(pending.decl.Operator)
		if reverse {
			source, target = target, source
		}
		edge := r.graph.AddEdge(source, target, pending.decl.Label, kind, diagram.Span("plantuml", pending.decl.Position.Line, pending.decl.Position.Column))
		edge.MinLength = relationLength(pending.decl.Operator)
		edge.LineStyle = relationLineStyle(pending.decl.Operator)
		applyDirectionPorts(edge, pending.decl.Operator, reverse)
		if operatorDirection(pending.decl.Operator) != "" {
			edge.LayoutReverse = reverseForDirectionHint(r.graph.Direction(), pending.decl.Operator) != reverse
		}
	}
	return nil
}

func reverseForDirectionHint(flow diagram.Direction, operator string) bool {
	direction := operatorDirection(operator)
	return flow == diagram.DirectionDown && direction == "up" ||
		flow == diagram.DirectionUp && direction == "down" ||
		flow == diagram.DirectionRight && direction == "left" ||
		flow == diagram.DirectionLeft && direction == "right"
}

func (r *resolver) endpoint(ref EndpointRef, parent string) (diagram.Endpoint, error) {
	if port, ok := r.ports[ref.Name]; ok {
		if ref.Member != "" {
			return diagram.Endpoint{}, syntax(ref.Position.Line, ref.Position.Column, "port %q cannot have a member", ref.Name)
		}
		attachment := diagram.PortHint{Side: diagram.SideWest}
		if port.direction == diagram.PortOutput {
			attachment.Side = diagram.SideEast
		}
		return endpointForSymbol(port.owner, "", ref.Name, attachment), nil
	}
	sym := r.symbols[ref.Name]
	if sym == nil {
		if label := r.labels[ref.Name]; label != nil && label.id != "" {
			sym = label
		} else if label != nil {
			return diagram.Endpoint{}, syntax(ref.Position.Line, ref.Position.Column, "ambiguous element label %q; use an alias", ref.Name)
		}
	}
	if sym == nil {
		node := r.graph.EnsureNode(diagram.NodeID(ref.Name), ref.Name, diagram.Span("plantuml", ref.Position.Line, ref.Position.Column))
		sym = &symbol{id: string(node.ID), label: node.Label, kind: diagram.EndpointNode, members: make(map[string]bool), ports: make(map[string]diagram.NamedPortDirection)}
		r.symbols[ref.Name] = sym
		r.labels[ref.Name] = sym
		r.hasComponent, r.mixingLine = true, ref.Position.Line
		if parent != "" {
			r.addGroupMember(parent, ref.Name)
		}
	}
	if ref.Member != "" && !sym.members[ref.Member] {
		return diagram.Endpoint{}, syntax(ref.Position.Line, ref.Position.Column, "element %q has no member %q", ref.Name, ref.Member)
	}
	return endpointForSymbol(sym, ref.Member, "", diagram.PortHint{}), nil
}

func endpointForSymbol(sym *symbol, cell, port string, hint diagram.PortHint) diagram.Endpoint {
	if sym.kind == diagram.EndpointGroup {
		return diagram.Endpoint{Kind: diagram.EndpointGroup, Group: diagram.GroupID(sym.id), Cell: diagram.CellID(cell), NamedPort: diagram.PortID(port), PortHint: hint}
	}
	return diagram.Endpoint{Kind: diagram.EndpointNode, Node: diagram.NodeID(sym.id), Cell: diagram.CellID(cell), NamedPort: diagram.PortID(port), PortHint: hint}
}

func (r *resolver) addGroupMember(groupID, nodeID string) {
	if r.groupMember[groupID] == nil {
		r.groupMember[groupID] = make(map[string]bool)
	}
	r.groupMember[groupID][nodeID] = true
}

func isGroupElement(decl *ElementDecl) bool {
	switch decl.Kind {
	case ElementPackage, ElementNode, ElementFolder, ElementFrame, ElementCloud, ElementDatabase:
		return true
	case ElementComponent:
		return len(decl.Statements) > 0
	default:
		return false
	}
}

func relationKind(operator string) (diagram.EdgeKind, bool) {
	base := stripOperatorDecorations(operator)
	left, right := strings.Contains(base, "<"), strings.Contains(base, ">")
	if left && right {
		return diagram.Bidirectional, false
	}
	if left {
		return diagram.Directed, true
	}
	if right {
		return diagram.Directed, false
	}
	return diagram.Undirected, false
}

func relationLineStyle(operator string) diagram.LineStyle {
	for style, result := range map[string]diagram.LineStyle{
		"[bold]": diagram.LineBold, "[dashed]": diagram.LineDashed, "[dotted]": diagram.LineDotted, "[hidden]": diagram.LineInvisible,
	} {
		if strings.Contains(operator, style) {
			return result
		}
	}
	if strings.Contains(stripOperatorDecorations(operator), ".") {
		return diagram.LineDotted
	}
	return diagram.LineSolid
}

func relationLength(operator string) int {
	base := stripOperatorDecorations(operator)
	count := strings.Count(base, "-") + strings.Count(base, ".")
	return max(1, count-1)
}

func stripOperatorDecorations(operator string) string {
	base := operator
	if start := strings.IndexByte(base, '['); start >= 0 {
		if end := strings.IndexByte(base[start:], ']'); end >= 0 {
			base = base[:start] + base[start+end+1:]
		}
	}
	for _, direction := range []string{"left", "right", "up", "down"} {
		base = strings.Replace(base, direction, "", 1)
	}
	if len(base) > 2 {
		for _, direction := range []string{"l", "r", "u", "d"} {
			base = strings.Replace(base, direction, "", 1)
		}
	}
	return base
}

func applyDirectionPorts(edge *diagram.Edge, operator string, endpointsReversed bool) {
	direction := operatorDirection(operator)
	if direction == "" {
		return
	}
	switch direction {
	case "left":
		edge.Start.Side, edge.End.Side = diagram.SideWest, diagram.SideEast
	case "right":
		edge.Start.Side, edge.End.Side = diagram.SideEast, diagram.SideWest
	case "up":
		edge.Start.Side, edge.End.Side = diagram.SideNorth, diagram.SideSouth
	case "down":
		edge.Start.Side, edge.End.Side = diagram.SideSouth, diagram.SideNorth
	}
	if endpointsReversed {
		edge.Start, edge.End = edge.End, edge.Start
	}
	edge.Source.PortHint, edge.Target.PortHint = edge.Start, edge.End
}

func operatorDirection(operator string) string {
	withoutStyle := operator
	if start := strings.IndexByte(withoutStyle, '['); start >= 0 {
		if end := strings.IndexByte(withoutStyle[start:], ']'); end >= 0 {
			withoutStyle = withoutStyle[:start] + withoutStyle[start+end+1:]
		}
	}
	direction := ""
	for _, candidate := range []string{"left", "right", "up", "down"} {
		if strings.Contains(withoutStyle, candidate) {
			direction = candidate
			break
		}
	}
	if direction == "" {
		for _, abbreviation := range []struct{ short, full string }{{"-l", "left"}, {"-r", "right"}, {"-u", "up"}, {"-d", "down"}} {
			if strings.Contains(withoutStyle, abbreviation.short) {
				direction = abbreviation.full
				break
			}
		}
	}
	if direction == "" {
		return ""
	}
	return direction
}
