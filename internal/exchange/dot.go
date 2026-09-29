package exchange

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/scanner"

	"github.com/xshoji/agents-workspace/internal/diagram"
)

type dotParser struct {
	scanner     scanner.Scanner
	token       rune
	text        string
	graph       *diagram.Builder
	groupID     int
	nodeScopes  map[diagram.NodeID][]diagram.GroupID
	groupParent map[diagram.GroupID]diagram.GroupID
	groupProxy  map[diagram.NodeID]diagram.GroupID
	semanticErr error
	lexErr      error
}

type dotCell struct {
	ID    string `json:"id,omitempty"`
	Label string `json:"label"`
}

type dotEncodedPort struct {
	ID        string `json:"id"`
	Direction uint8  `json:"direction"`
}

func ParseDOTProblem(input io.Reader) (*diagram.Problem, error) {
	encoded, err := io.ReadAll(input)
	if err != nil {
		return nil, err
	}
	// DOT quoted strings permit escaped physical newlines. Go's text/scanner
	// does not, so splice them before tokenization as the DOT grammar requires.
	normalized := strings.ReplaceAll(string(encoded), "\\\r\n", "")
	normalized = strings.ReplaceAll(normalized, "\\\n", "")
	p := &dotParser{
		graph: diagram.NewBuilder("dot"), nodeScopes: make(map[diagram.NodeID][]diagram.GroupID),
		groupParent: make(map[diagram.GroupID]diagram.GroupID), groupProxy: make(map[diagram.NodeID]diagram.GroupID),
	}
	p.scanner.Init(strings.NewReader(normalized))
	p.scanner.Mode = scanner.ScanIdents | scanner.ScanStrings | scanner.ScanRawStrings | scanner.ScanInts | scanner.SkipComments
	p.scanner.Error = func(current *scanner.Scanner, message string) {
		if message == "invalid char escape" {
			return
		}
		if p.lexErr == nil {
			p.lexErr = fmt.Errorf("DOT line %d, column %d: %s", current.Position.Line, current.Position.Column, message)
		}
	}
	p.next()
	if p.acceptWord("strict") {
		// Strictness affects duplicate-edge handling in Graphviz, which this
		// model intentionally does not emulate.
	}
	if !p.acceptWord("digraph") && !p.acceptWord("graph") {
		return nil, p.errorf("want graph or digraph")
	}
	if p.token != '{' {
		if _, err := p.takeID(); err != nil {
			return nil, err
		}
	}
	if err := p.expect('{'); err != nil {
		return nil, err
	}
	if _, err := p.parseBlock("", "", nil, nil, diagram.SourceSpan{}); err != nil {
		return nil, err
	}
	if p.lexErr != nil {
		return nil, p.lexErr
	}
	if p.semanticErr != nil {
		return nil, p.semanticErr
	}
	if err := p.resolveGroupProxies(); err != nil {
		return nil, err
	}
	if err := p.finalizeContainment(); err != nil {
		return nil, err
	}
	if p.token != scanner.EOF {
		return nil, p.errorf("unexpected token %q", p.text)
	}
	return p.graph.Build()
}

func (p *dotParser) parseBlock(group string, parent diagram.GroupID, inheritedNodeDefaults, inheritedEdgeDefaults map[string]string, groupSpan diagram.SourceSpan) ([]string, error) {
	var members []string
	blockAttrs := make(map[string]string)
	nodeDefaults := cloneAttrs(inheritedNodeDefaults)
	edgeDefaults := cloneAttrs(inheritedEdgeDefaults)
	isCluster := strings.HasPrefix(strings.ToLower(group), "cluster")
	semanticGroup := diagram.GroupID(group)
	registered := false
	registerGroup := func() error {
		if !isCluster || registered {
			return nil
		}
		if _, exists := p.groupParent[semanticGroup]; exists {
			return p.errorf("duplicate group ID %q", semanticGroup)
		}
		p.groupParent[semanticGroup] = parent
		registered = true
		return nil
	}
	for p.token != '}' && p.token != scanner.EOF {
		if p.token == ';' || p.token == ',' {
			p.next()
			continue
		}
		statementSpan := p.span()
		if p.acceptWord("subgraph") || p.token == '{' {
			if err := registerGroup(); err != nil {
				return nil, err
			}
			id := ""
			if p.token != '{' {
				var err error
				id, err = p.takeID()
				if err != nil {
					return nil, err
				}
			}
			if id == "" {
				id = fmt.Sprintf("__dot_subgraph_%d", p.groupID)
				p.groupID++
			}
			if err := p.expect('{'); err != nil {
				return nil, err
			}
			childParent := parent
			if isCluster {
				childParent = semanticGroup
			}
			childMembers, err := p.parseBlock(id, childParent, nodeDefaults, edgeDefaults, statementSpan)
			if err != nil {
				return nil, err
			}
			members = appendUnique(members, childMembers...)
			continue
		}
		span := statementSpan
		name, err := p.takeID()
		if err != nil {
			return nil, err
		}
		if p.token == '=' {
			p.next()
			value, err := p.takeID()
			if err != nil {
				return nil, err
			}
			blockAttrs[strings.ToLower(name)] = value
			if strings.EqualFold(name, "diagram_group_id") && isCluster && !registered {
				semanticGroup = diagram.GroupID(value)
			}
			if strings.EqualFold(name, "rankdir") {
				p.applyGraphAttrs(map[string]string{"rankdir": value})
			}
			continue
		}
		if (name == "graph" || name == "node" || name == "edge") && p.token == '[' {
			attrs, err := p.parseAttrs()
			if err != nil {
				return nil, err
			}
			switch name {
			case "graph":
				mergeAttrs(blockAttrs, attrs)
				if value := attrs["diagram_group_id"]; value != "" && isCluster && !registered {
					semanticGroup = diagram.GroupID(value)
				}
				if err := registerGroup(); err != nil {
					return nil, err
				}
				p.applyGraphAttrs(attrs)
			case "node":
				if err := registerGroup(); err != nil {
					return nil, err
				}
				mergeAttrs(nodeDefaults, attrs)
			case "edge":
				if err := registerGroup(); err != nil {
					return nil, err
				}
				mergeAttrs(edgeDefaults, attrs)
			}
			continue
		}
		if err := registerGroup(); err != nil {
			return nil, err
		}
		from := p.addNode(name, nodeDefaults, span)
		members = appendUnique(members, string(from.ID))
		owner := parent
		if isCluster {
			owner = semanticGroup
		}
		p.recordScope(from.ID, owner)
		startPort, err := p.parseEndpointPort()
		if err != nil {
			return nil, err
		}
		if p.token == '-' {
			firstEdgeIndex := len(p.graph.Edges())
			kind, err := p.takeEdgeOperator()
			if err != nil {
				return nil, err
			}
			current := from
			currentPort := startPort
			for {
				toSpan := p.span()
				toID, err := p.takeID()
				if err != nil {
					return nil, err
				}
				to := p.addNode(toID, nodeDefaults, toSpan)
				members = appendUnique(members, string(to.ID))
				p.recordScope(to.ID, owner)
				endPort, err := p.parseEndpointPort()
				if err != nil {
					return nil, err
				}
				edge := p.graph.AddEdge(diagram.NodeEndpoint(current.ID), diagram.NodeEndpoint(to.ID), "", kind, span)
				p.applyEdgeAttrs(edge, edgeDefaults)
				edge.Start, edge.End = currentPort, endPort
				current, currentPort = to, endPort
				if p.token != '-' {
					break
				}
				kind, err = p.takeEdgeOperator()
				if err != nil {
					return nil, err
				}
			}
			attrs, err := p.parseOptionalAttrs()
			if err != nil {
				return nil, err
			}
			// DOT applies a trailing attribute list to every edge in a chain.
			edges := p.graph.Edges()
			for index := firstEdgeIndex; index < len(edges); index++ {
				edge := edges[index]
				p.applyEdgeAttrs(edge, attrs)
				if _, hasSemantics := attrs["diagram_edge_semantics"]; !hasSemantics {
					edge.Source.PortHint, edge.Target.PortHint = edge.Start, edge.End
				}
			}
		} else {
			attrs, err := p.parseOptionalAttrs()
			if err != nil {
				return nil, err
			}
			p.applyNodeAttrs(from, attrs)
		}
	}
	if err := p.expect('}'); err != nil {
		return nil, err
	}
	if group != "" {
		attrs, err := p.parseOptionalAttrs()
		if err != nil {
			return nil, err
		}
		for key, value := range attrs {
			blockAttrs[key] = value
		}
		if isCluster {
			if err := registerGroup(); err != nil {
				return nil, err
			}
			label := blockAttrs["label"]
			created := p.graph.AddGroup(semanticGroup, nil, label, parent, 1, diagram.LineSolid, false, groupSpan)
			created.Label = label
			p.applyGroupAttrs(created, blockAttrs)
		}
		if strings.EqualFold(blockAttrs["rank"], "same") {
			rank := blockAttrs["diagram_rank_same"]
			if rank == "" {
				rank = "dot-rank-" + group
			}
			for _, id := range members {
				if node, ok := p.graph.Node(diagram.NodeID(id)); ok {
					node.Rank.Same = rank
				}
			}
		}
	}
	return members, nil
}

func (p *dotParser) parseOptionalAttrs() (map[string]string, error) {
	attrs := make(map[string]string)
	for p.token == '[' {
		part, err := p.parseAttrs()
		if err != nil {
			return nil, err
		}
		for key, value := range part {
			attrs[key] = value
		}
	}
	return attrs, nil
}

func (p *dotParser) parseAttrs() (map[string]string, error) {
	if err := p.expect('['); err != nil {
		return nil, err
	}
	attrs := make(map[string]string)
	for p.token != ']' && p.token != scanner.EOF {
		if p.token == ',' || p.token == ';' {
			p.next()
			continue
		}
		name, err := p.takeID()
		if err != nil {
			return nil, err
		}
		value := "true"
		if p.token == '=' {
			p.next()
			value, err = p.takeID()
			if err != nil {
				return nil, err
			}
		}
		attrs[strings.ToLower(name)] = value
	}
	if err := p.expect(']'); err != nil {
		return nil, err
	}
	return attrs, nil
}

func (p *dotParser) takeEdgeOperator() (diagram.EdgeKind, error) {
	p.next()
	if p.token == '>' {
		p.next()
		return diagram.Directed, nil
	}
	if p.token == '-' {
		p.next()
		return diagram.Undirected, nil
	}
	return diagram.Directed, p.errorf("invalid edge operator")
}

func (p *dotParser) parseEndpointPort() (diagram.PortHint, error) {
	if p.token != ':' {
		return diagram.PortHint{}, nil
	}
	p.next()
	value, err := p.takeID()
	if err != nil {
		return diagram.PortHint{}, err
	}
	if p.token == ':' {
		p.next()
		compass, err := p.takeID()
		if err != nil {
			return diagram.PortHint{}, err
		}
		value += ":" + compass
	}
	return diagram.PortHint{Side: dotSide(value)}, nil
}

func (p *dotParser) applyGraphAttrs(attrs map[string]string) {
	switch strings.ToUpper(attrs["rankdir"]) {
	case "LR":
		p.graph.SetDirection(diagram.DirectionRight)
	case "RL":
		p.graph.SetDirection(diagram.DirectionLeft)
	case "BT":
		p.graph.SetDirection(diagram.DirectionUp)
	case "TB":
		p.graph.SetDirection(diagram.DirectionDown)
	}
}

func (p *dotParser) applyNodeAttrs(node *diagram.Node, attrs map[string]string) {
	if label, ok := attrs["label"]; ok {
		node.Label = label
	}
	if group := attrs["diagram_group_endpoint"]; group != "" {
		p.groupProxy[node.ID] = diagram.GroupID(group)
	}
	if value := attrs["diagram_cells"]; value != "" {
		var rows [][]dotCell
		if json.Unmarshal([]byte(value), &rows) == nil {
			node.Cells = make([][]diagram.Cell, len(rows))
			for row, cells := range rows {
				for _, cell := range cells {
					node.Cells[row] = append(node.Cells[row], diagram.Cell{ID: diagram.CellID(cell.ID), Label: cell.Label, Source: node.Source})
				}
			}
		}
	}
	if value := attrs["diagram_ports"]; value != "" {
		node.Ports = decodeDOTPorts(value, node.Source)
	}
	if value, err := strconv.Atoi(attrs["diagram_rank_fixed"]); err == nil {
		node.Rank.Fixed = &value
	}
	node.Rank.Root = strings.EqualFold(attrs["diagram_rank_root"], "true")
	if same := attrs["diagram_rank_same"]; same != "" {
		node.Rank.Same = same
	}
	if value, err := strconv.Atoi(attrs["diagram_text_wrap"]); err == nil {
		node.TextWrap = value
	}
	if value, err := strconv.Atoi(attrs["diagram_align"]); err == nil {
		node.Align = diagram.Align(value)
	}
	switch strings.ToLower(attrs["shape"]) {
	case "point":
		node.Shape = diagram.ShapePoint
	case "plaintext", "plain", "none":
		node.Shape = diagram.ShapeNone
	case "record", "mrecord":
		rows := strings.Split(attrs["label"], "|")
		if len(rows) > 1 {
			node.Cells = make([][]diagram.Cell, 1)
			for _, label := range rows {
				node.Cells[0] = append(node.Cells[0], diagram.Cell{Label: label, Source: node.Source})
			}
		}
	}
	if strings.Contains(strings.ToLower(attrs["style"]), "rounded") {
		node.Shape = diagram.ShapeRounded
	}
	if encoded, exists := attrs["diagram_node_semantics"]; exists {
		id := node.ID
		var semantic diagram.Node
		if err := json.Unmarshal([]byte(encoded), &semantic); err != nil {
			p.setSemanticError("node", err)
		} else {
			// The DOT identifier owns the Builder lookup key. All other fields in
			// the semantic payload are the canonical exchange representation.
			semantic.ID = id
			*node = semantic
		}
	}
}

func (p *dotParser) applyEdgeAttrs(edge *diagram.Edge, attrs map[string]string) {
	if label, ok := attrs["label"]; ok {
		edge.Label = label
	}
	if value, ok := attrs["minlen"]; ok {
		if length, err := strconv.Atoi(value); err == nil && length > 0 {
			edge.MinLength = length
		}
	}
	if value, ok := attrs["tailport"]; ok {
		edge.Start.Side = dotSide(value)
	}
	if value, ok := attrs["headport"]; ok {
		edge.End.Side = dotSide(value)
	}
	edge.Source.Cell = diagram.CellID(attrs["diagram_source_cell"])
	edge.Source.NamedPort = diagram.PortID(attrs["diagram_source_port"])
	edge.Target.Cell = diagram.CellID(attrs["diagram_target_cell"])
	edge.Target.NamedPort = diagram.PortID(attrs["diagram_target_port"])
	if value, err := strconv.Atoi(attrs["diagram_arrow_style"]); err == nil {
		edge.ArrowStyle = diagram.ArrowStyle(value)
	}
	edge.LayoutReverse = strings.EqualFold(attrs["diagram_layout_reverse"], "true")
	switch strings.ToLower(attrs["dir"]) {
	case "none":
		edge.Kind = diagram.Undirected
	case "both":
		edge.Kind = diagram.Bidirectional
	}
	switch strings.ToLower(attrs["style"]) {
	case "dotted":
		edge.LineStyle = diagram.LineDotted
	case "dashed":
		edge.LineStyle = diagram.LineDashed
	case "bold":
		edge.LineStyle = diagram.LineBold
	case "invis":
		edge.LineStyle = diagram.LineInvisible
	}
	if encoded, exists := attrs["diagram_edge_semantics"]; exists {
		var semantic diagram.Edge
		if err := json.Unmarshal([]byte(encoded), &semantic); err != nil {
			p.setSemanticError("edge", err)
		} else {
			*edge = semantic
		}
	}
}

func (p *dotParser) applyGroupAttrs(group *diagram.Group, attrs map[string]string) {
	if value, err := strconv.Atoi(attrs["diagram_padding"]); err == nil && value > 0 {
		group.Padding = value
	}
	if value, err := strconv.Atoi(attrs["diagram_line_style"]); err == nil {
		group.LineStyle = diagram.LineStyle(value)
	}
	group.Anonymous = strings.EqualFold(attrs["diagram_anonymous"], "true")
	if value := attrs["diagram_ports"]; value != "" {
		group.Ports = decodeDOTPorts(value, group.Source)
	}
	if encoded, exists := attrs["diagram_group_semantics"]; exists {
		id := group.ID
		var semantic diagram.Group
		if err := json.Unmarshal([]byte(encoded), &semantic); err != nil {
			p.setSemanticError("group", err)
		} else {
			// As with nodes, keep the identifier used by the Builder's index.
			semantic.ID = id
			// Canonical direct membership is reconstructed from nested DOT scopes.
			semantic.Members = nil
			*group = semantic
		}
	}
}

func (p *dotParser) setSemanticError(kind string, err error) {
	if p.semanticErr == nil {
		p.semanticErr = p.errorf("invalid diagram_%s_semantics: %v", kind, err)
	}
}

func decodeDOTPorts(value string, source diagram.SourceSpan) []diagram.NamedPort {
	var encoded []dotEncodedPort
	if json.Unmarshal([]byte(value), &encoded) != nil {
		return nil
	}
	ports := make([]diagram.NamedPort, 0, len(encoded))
	for _, port := range encoded {
		ports = append(ports, diagram.NamedPort{ID: diagram.PortID(port.ID), Direction: diagram.NamedPortDirection(port.Direction), Source: source})
	}
	return ports
}

func dotSide(value string) diagram.Side {
	parts := strings.Split(strings.ToLower(value), ":")
	for index := len(parts) - 1; index >= 0; index-- {
		switch parts[index] {
		case "n", "north":
			return diagram.SideNorth
		case "e", "east":
			return diagram.SideEast
		case "s", "south":
			return diagram.SideSouth
		case "w", "west":
			return diagram.SideWest
		}
	}
	return diagram.SideAuto
}

func (p *dotParser) addNode(id string, defaults map[string]string, span diagram.SourceSpan) *diagram.Node {
	if node, exists := p.graph.Node(diagram.NodeID(id)); exists {
		return node
	}
	node := p.graph.AddNode(diagram.NodeID(id), id, span)
	p.applyNodeAttrs(node, defaults)
	return node
}

func (p *dotParser) span() diagram.SourceSpan {
	position := p.scanner.Position
	return diagram.Span("dot", position.Line, position.Column)
}

func (p *dotParser) recordScope(node diagram.NodeID, group diagram.GroupID) {
	if group == "" {
		return
	}
	for _, existing := range p.nodeScopes[node] {
		if existing == group {
			return
		}
	}
	p.nodeScopes[node] = append(p.nodeScopes[node], group)
}

func (p *dotParser) finalizeContainment() error {
	for _, node := range p.graph.Nodes() {
		owner := diagram.GroupID("")
		for _, candidate := range p.nodeScopes[node.ID] {
			if owner == "" || p.groupAncestor(owner, candidate) {
				owner = candidate
				continue
			}
			if !p.groupAncestor(candidate, owner) {
				return p.errorf("node %q belongs to unrelated clusters %q and %q", node.ID, owner, candidate)
			}
		}
		if owner != "" {
			group, _ := p.graph.Group(owner)
			group.Members = append(group.Members, node.ID)
		}
	}
	return nil
}

func (p *dotParser) resolveGroupProxies() error {
	if len(p.groupProxy) == 0 {
		return nil
	}
	remove := make(map[diagram.NodeID]bool, len(p.groupProxy))
	for node, group := range p.groupProxy {
		if _, exists := p.graph.Group(group); !exists {
			return p.errorf("group endpoint proxy %q references unknown group %q", node, group)
		}
		remove[node] = true
	}
	for _, edge := range p.graph.Edges() {
		if group, exists := p.groupProxy[edge.Source.Node]; exists {
			cell, port := edge.Source.Cell, edge.Source.NamedPort
			edge.Source = diagram.GroupEndpoint(group)
			edge.Source.Cell, edge.Source.NamedPort, edge.Source.PortHint = cell, port, edge.Start
		}
		if group, exists := p.groupProxy[edge.Target.Node]; exists {
			cell, port := edge.Target.Cell, edge.Target.NamedPort
			edge.Target = diagram.GroupEndpoint(group)
			edge.Target.Cell, edge.Target.NamedPort, edge.Target.PortHint = cell, port, edge.End
		}
	}
	p.graph.RemoveNodes(remove)
	return nil
}

func (p *dotParser) groupAncestor(ancestor, group diagram.GroupID) bool {
	visited := make(map[diagram.GroupID]bool)
	for parent := p.groupParent[group]; parent != ""; parent = p.groupParent[parent] {
		if visited[parent] {
			return false
		}
		visited[parent] = true
		if parent == ancestor {
			return true
		}
	}
	return false
}

func cloneAttrs(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	mergeAttrs(result, source)
	return result
}

func mergeAttrs(target, source map[string]string) {
	for name, value := range source {
		target[name] = value
	}
}

func (p *dotParser) takeID() (string, error) {
	if p.token != scanner.Ident && p.token != scanner.Int && p.token != scanner.String && p.token != scanner.RawString {
		return "", p.errorf("want identifier, got %q", p.text)
	}
	value := p.text
	if p.token == scanner.String || p.token == scanner.RawString {
		unquoted, err := unquoteDOTString(value)
		if err != nil {
			return "", p.errorf("invalid quoted identifier")
		}
		value = unquoted
	}
	p.next()
	return value, nil
}

func unquoteDOTString(value string) (string, error) {
	if unquoted, err := strconv.Unquote(value); err == nil {
		return unquoted, nil
	}
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return "", fmt.Errorf("invalid quoted string")
	}
	var result strings.Builder
	for index := 1; index < len(value)-1; index++ {
		if value[index] != '\\' {
			result.WriteByte(value[index])
			continue
		}
		if index+1 >= len(value)-1 {
			return "", fmt.Errorf("unterminated escape")
		}
		index++
		switch value[index] {
		case '\n':
			// DOT quoted strings splice escaped physical newlines.
		case '\r':
			if index+1 < len(value)-1 && value[index+1] == '\n' {
				index++
			}
		case '"', '\\':
			result.WriteByte(value[index])
		case 'n':
			result.WriteByte('\n')
		case 'r':
			result.WriteByte('\r')
		case 't':
			result.WriteByte('\t')
		default:
			// Graphviz emits substitutions such as \N. Preserve unknown DOT
			// escapes rather than applying Go string-literal rules to them.
			result.WriteByte('\\')
			result.WriteByte(value[index])
		}
	}
	return result.String(), nil
}

func (p *dotParser) acceptWord(word string) bool {
	if p.token == scanner.Ident && strings.EqualFold(p.text, word) {
		p.next()
		return true
	}
	return false
}

func (p *dotParser) expect(token rune) error {
	if p.token != token {
		return p.errorf("want %q, got %q", string(token), p.text)
	}
	p.next()
	return nil
}

func (p *dotParser) next() {
	p.token = p.scanner.Scan()
	p.text = p.scanner.TokenText()
}

func (p *dotParser) errorf(format string, args ...any) error {
	return fmt.Errorf("DOT line %d, column %d: %s", p.scanner.Position.Line, p.scanner.Position.Column, fmt.Sprintf(format, args...))
}

func appendUnique(values []string, additions ...string) []string {
	for _, addition := range additions {
		found := false
		for _, value := range values {
			if value == addition {
				found = true
				break
			}
		}
		if !found {
			values = append(values, addition)
		}
	}
	return values
}
