package exchange

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/xshoji/go-text-diagram/internal/diagram"
)

type checkedWriter struct {
	output io.Writer
	err    error
}

func (w *checkedWriter) printf(format string, args ...any) {
	if w.err == nil {
		_, w.err = fmt.Fprintf(w.output, format, args...)
	}
}

func (w *checkedWriter) println(value string) {
	if w.err == nil {
		_, w.err = fmt.Fprintln(w.output, value)
	}
}

func WriteDOTProblem(output io.Writer, problem *diagram.Problem) error {
	if problem == nil {
		return fmt.Errorf("problem is nil")
	}
	nodes := problem.Nodes()
	newRank := false
	for _, node := range nodes {
		if node.Rank.Same == "" {
			continue
		}
		if _, grouped := problem.ParentOf(diagram.ElementRef{Kind: diagram.ElementNode, Node: node.ID}); grouped {
			newRank = true
			break
		}
	}
	writer := &checkedWriter{output: output}
	if newRank {
		writer.printf("digraph G {\n  graph [rankdir=%s, newrank=true];\n", problemDOTDirection(problem.Direction()))
	} else {
		writer.printf("digraph G {\n  graph [rankdir=%s];\n", problemDOTDirection(problem.Direction()))
	}
	groups := problem.Groups()
	groupIndex := make(map[diagram.GroupID]int, len(groups))
	children := make(map[diagram.GroupID][]diagram.Group)
	for index, group := range groups {
		groupIndex[group.ID] = index
		children[group.Parent] = append(children[group.Parent], group)
	}
	nodeByID := make(map[diagram.NodeID]diagram.Node, len(nodes))
	for _, node := range nodes {
		nodeByID[node.ID] = node
	}
	for _, group := range children[""] {
		writeDOTGroup(writer, group, children, nodeByID, groupIndex, "  ")
	}
	for _, node := range nodes {
		if _, grouped := problem.ParentOf(diagram.ElementRef{Kind: diagram.ElementNode, Node: node.ID}); !grouped {
			writer.printf("  %s;\n", problemDOTNode(node))
		}
	}
	rankGroups := make(map[string][]diagram.NodeID)
	var rankOrder []string
	for _, node := range nodes {
		if node.Rank.Same == "" {
			continue
		}
		if _, exists := rankGroups[node.Rank.Same]; !exists {
			rankOrder = append(rankOrder, node.Rank.Same)
		}
		rankGroups[node.Rank.Same] = append(rankGroups[node.Rank.Same], node.ID)
	}
	for _, rank := range rankOrder {
		writer.printf("  subgraph {\n    rank=same;\n    diagram_rank_same=%s;\n", dotQuote(rank))
		for _, node := range rankGroups[rank] {
			writer.printf("    %s;\n", dotQuote(string(node)))
		}
		writer.println("  }")
	}

	usedIDs := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		usedIDs[string(node.ID)] = true
	}
	proxies := make(map[diagram.GroupID]string)
	for _, edge := range problem.Edges() {
		for _, endpoint := range []diagram.Endpoint{edge.Source, edge.Target} {
			if endpoint.Kind != diagram.EndpointGroup || proxies[endpoint.Group] != "" {
				continue
			}
			id := fmt.Sprintf("__diagram_group_endpoint_%d", groupIndex[endpoint.Group])
			for suffix := 0; usedIDs[id]; suffix++ {
				id = fmt.Sprintf("__diagram_group_endpoint_%d_%d", groupIndex[endpoint.Group], suffix)
			}
			usedIDs[id] = true
			proxies[endpoint.Group] = id
			writer.printf("  %s [label=%s, shape=point, style=invis, diagram_group_endpoint=%s];\n", dotQuote(id), dotQuote(""), dotQuote(string(endpoint.Group)))
		}
	}
	for _, edge := range problem.Edges() {
		from := problemDOTEndpoint(edge.Source, proxies)
		to := problemDOTEndpoint(edge.Target, proxies)
		attrs := problemDOTEdgeAttrs(edge)
		suffix := ""
		if len(attrs) > 0 {
			suffix = " [" + strings.Join(attrs, ", ") + "]"
		}
		writer.printf("  %s -> %s%s;\n", dotQuote(from), dotQuote(to), suffix)
	}
	writer.println("}")
	return writer.err
}

func writeDOTGroup(output *checkedWriter, group diagram.Group, children map[diagram.GroupID][]diagram.Group, nodes map[diagram.NodeID]diagram.Node, groupIndex map[diagram.GroupID]int, indent string) {
	physicalID := fmt.Sprintf("cluster_diagram_%d", groupIndex[group.ID])
	output.printf("%ssubgraph %s {\n", indent, dotQuote(physicalID))
	inner := indent + "  "
	output.printf("%sdiagram_group_id=%s;\n", inner, dotQuote(string(group.ID)))
	output.printf("%slabel=%s;\n", inner, dotQuote(group.Label))
	output.printf("%sdiagram_padding=%d;\n", inner, group.Padding)
	output.printf("%sdiagram_line_style=%d;\n", inner, group.LineStyle)
	output.printf("%sdiagram_anonymous=%t;\n", inner, group.Anonymous)
	output.printf("%sdiagram_group_semantics=%s;\n", inner, dotQuote(mustJSON(group)))
	if len(group.Ports) > 0 {
		output.printf("%sdiagram_ports=%s;\n", inner, dotQuote(encodeDOTPorts(group.Ports)))
	}
	for _, child := range children[group.ID] {
		writeDOTGroup(output, child, children, nodes, groupIndex, inner)
	}
	for _, member := range group.Members {
		if node, ok := nodes[member]; ok {
			output.printf("%s%s;\n", inner, problemDOTNode(node))
		}
	}
	output.printf("%s}\n", indent)
}

func problemDOTNode(node diagram.Node) string {
	attrs := []string{"label=" + dotQuote(node.Label)}
	if shape := problemDOTShape(node.Shape); shape != "" {
		attrs = append(attrs, "shape="+shape)
	}
	if len(node.Cells) > 0 {
		rows := make([][]dotCell, len(node.Cells))
		for row, cells := range node.Cells {
			for _, cell := range cells {
				rows[row] = append(rows[row], dotCell{ID: string(cell.ID), Label: cell.Label})
			}
		}
		attrs = append(attrs, "diagram_cells="+dotQuote(mustJSON(rows)))
	}
	if len(node.Ports) > 0 {
		attrs = append(attrs, "diagram_ports="+dotQuote(encodeDOTPorts(node.Ports)))
	}
	if node.Rank.Fixed != nil {
		attrs = append(attrs, "diagram_rank_fixed="+strconv.Itoa(*node.Rank.Fixed))
	}
	if node.Rank.Root {
		attrs = append(attrs, "diagram_rank_root=true")
	}
	if node.Rank.Same != "" {
		attrs = append(attrs, "diagram_rank_same="+dotQuote(node.Rank.Same))
	}
	if node.TextWrap != 0 {
		attrs = append(attrs, "diagram_text_wrap="+strconv.Itoa(node.TextWrap))
	}
	if node.Align != diagram.AlignCenter {
		attrs = append(attrs, "diagram_align="+strconv.Itoa(int(node.Align)))
	}
	attrs = append(attrs, "diagram_node_semantics="+dotQuote(mustJSON(node)))
	return dotQuote(string(node.ID)) + " [" + strings.Join(attrs, ", ") + "]"
}

func problemDOTEdgeAttrs(edge diagram.Edge) []string {
	var attrs []string
	if edge.Label != "" {
		attrs = append(attrs, "label="+dotQuote(edge.Label))
	}
	if edge.Kind == diagram.Undirected {
		attrs = append(attrs, "dir=none")
	} else if edge.Kind == diagram.Bidirectional {
		attrs = append(attrs, "dir=both")
	}
	if edge.MinLength > 1 {
		attrs = append(attrs, "minlen="+strconv.Itoa(edge.MinLength))
	}
	if edge.Start.Side != diagram.SideAuto {
		attrs = append(attrs, "tailport="+problemDOTPort(edge.Start.Side))
	}
	if edge.End.Side != diagram.SideAuto {
		attrs = append(attrs, "headport="+problemDOTPort(edge.End.Side))
	}
	if style := problemDOTLineStyle(edge.LineStyle); style != "" {
		attrs = append(attrs, "style="+style)
	}
	for _, entry := range []struct{ key, value string }{
		{"diagram_source_cell", string(edge.Source.Cell)}, {"diagram_source_port", string(edge.Source.NamedPort)},
		{"diagram_target_cell", string(edge.Target.Cell)}, {"diagram_target_port", string(edge.Target.NamedPort)},
	} {
		if entry.value != "" {
			attrs = append(attrs, entry.key+"="+dotQuote(entry.value))
		}
	}
	if edge.ArrowStyle != diagram.ArrowDefault {
		attrs = append(attrs, "diagram_arrow_style="+strconv.Itoa(int(edge.ArrowStyle)))
	}
	if edge.LayoutReverse {
		attrs = append(attrs, "diagram_layout_reverse=true")
	}
	attrs = append(attrs, "diagram_edge_semantics="+dotQuote(mustJSON(edge)))
	return attrs
}

func problemDOTEndpoint(endpoint diagram.Endpoint, proxies map[diagram.GroupID]string) string {
	if endpoint.Kind == diagram.EndpointGroup {
		return proxies[endpoint.Group]
	}
	return string(endpoint.Node)
}

func dotQuote(value string) string { return strconv.Quote(value) }

func encodeDOTPorts(ports []diagram.NamedPort) string {
	encoded := make([]dotEncodedPort, 0, len(ports))
	for _, port := range ports {
		encoded = append(encoded, dotEncodedPort{ID: string(port.ID), Direction: uint8(port.Direction)})
	}
	return mustJSON(encoded)
}

func mustJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func WriteGraphMLProblem(output io.Writer, problem *diagram.Problem) error {
	if problem == nil {
		return fmt.Errorf("problem is nil")
	}
	root := graphML{
		XMLNS: "http://graphml.graphdrawing.org/xmlns",
		Keys:  problemGraphMLKeys(),
		Graph: graphMLGraph{ID: "G", EdgeDefault: "directed", Data: []graphMLData{{Key: "direction", Value: problemDirectionString(problem.Direction())}}},
	}
	ids := make(map[string]string)
	for index, node := range problem.Nodes() {
		id := fmt.Sprintf("n%d", index)
		ids["node:"+string(node.ID)] = id
		root.Graph.Nodes = append(root.Graph.Nodes, graphMLNode{ID: id, Data: []graphMLData{
			{Key: "element-kind", Value: "node"}, {Key: "original-id", Value: string(node.ID)}, {Key: "label", Value: node.Label},
			{Key: "cells", Value: mustJSON(node.Cells)}, {Key: "ports", Value: mustJSON(node.Ports)}, {Key: "node-semantics", Value: mustJSON(node)},
		}})
	}
	for index, group := range problem.Groups() {
		id := fmt.Sprintf("g%d", index)
		ids["group:"+string(group.ID)] = id
		root.Graph.Nodes = append(root.Graph.Nodes, graphMLNode{ID: id, Data: []graphMLData{
			{Key: "element-kind", Value: "group"}, {Key: "original-id", Value: string(group.ID)}, {Key: "label", Value: group.Label},
			{Key: "parent", Value: string(group.Parent)}, {Key: "members", Value: mustJSON(group.Members)}, {Key: "ports", Value: mustJSON(group.Ports)}, {Key: "group-semantics", Value: mustJSON(group)},
		}})
	}
	for index, edge := range problem.Edges() {
		source := problemGraphMLEndpointID(edge.Source, ids)
		target := problemGraphMLEndpointID(edge.Target, ids)
		if source == "" || target == "" {
			return fmt.Errorf("edge %q has unresolved GraphML endpoint", edge.ID)
		}
		graphEdge := graphMLEdge{
			ID: fmt.Sprintf("e%d", index), Source: source, Target: target,
			Data: []graphMLData{
				{Key: "original-id", Value: string(edge.ID)}, {Key: "label", Value: edge.Label}, {Key: "kind", Value: problemEdgeKind(edge.Kind)},
				{Key: "source-endpoint", Value: mustJSON(edge.Source)}, {Key: "target-endpoint", Value: mustJSON(edge.Target)},
				{Key: "edge-semantics", Value: mustJSON(edge)},
			},
		}
		if edge.Kind == diagram.Undirected {
			directed := false
			graphEdge.Directed = &directed
		}
		root.Graph.Edges = append(root.Graph.Edges, graphEdge)
	}
	encoder := xml.NewEncoder(output)
	encoder.Indent("", "  ")
	if _, err := io.WriteString(output, xml.Header); err != nil {
		return err
	}
	if err := encoder.Encode(root); err != nil {
		return err
	}
	return encoder.Flush()
}

func problemGraphMLKeys() []graphMLKey {
	var keys []graphMLKey
	for _, key := range []struct{ id, target string }{
		{"original-id", "all"}, {"label", "all"}, {"element-kind", "node"}, {"parent", "node"}, {"members", "node"},
		{"cells", "node"}, {"ports", "node"}, {"node-semantics", "node"}, {"group-semantics", "node"}, {"kind", "edge"}, {"source-endpoint", "edge"}, {"target-endpoint", "edge"},
		{"edge-semantics", "edge"}, {"direction", "graph"},
	} {
		keys = append(keys, graphMLKey{ID: key.id, For: key.target, Name: key.id, Type: "string"})
	}
	return keys
}

func problemGraphMLEndpointID(endpoint diagram.Endpoint, ids map[string]string) string {
	if endpoint.Kind == diagram.EndpointGroup {
		return ids["group:"+string(endpoint.Group)]
	}
	return ids["node:"+string(endpoint.Node)]
}

func problemDOTDirection(direction diagram.Direction) string {
	switch direction {
	case diagram.DirectionRight:
		return "LR"
	case diagram.DirectionUp:
		return "BT"
	case diagram.DirectionLeft:
		return "RL"
	default:
		return "TB"
	}
}

func problemDirectionString(direction diagram.Direction) string {
	switch direction {
	case diagram.DirectionRight:
		return "right"
	case diagram.DirectionUp:
		return "up"
	case diagram.DirectionLeft:
		return "left"
	default:
		return "down"
	}
}

func problemDOTPort(side diagram.Side) string {
	switch side {
	case diagram.SideNorth:
		return "n"
	case diagram.SideEast:
		return "e"
	case diagram.SideSouth:
		return "s"
	case diagram.SideWest:
		return "w"
	default:
		return "c"
	}
}

func problemDOTShape(shape diagram.Shape) string {
	switch shape {
	case diagram.ShapeRounded:
		return "box, style=rounded"
	case diagram.ShapePoint:
		return "point"
	case diagram.ShapeNone, diagram.ShapeInvisible:
		return "plaintext"
	default:
		return ""
	}
}

func problemDOTLineStyle(style diagram.LineStyle) string {
	switch style {
	case diagram.LineDotted:
		return "dotted"
	case diagram.LineDashed:
		return "dashed"
	case diagram.LineBold:
		return "bold"
	case diagram.LineInvisible:
		return "invis"
	default:
		return ""
	}
}

func problemEdgeKind(kind diagram.EdgeKind) string {
	switch kind {
	case diagram.Undirected:
		return "undirected"
	case diagram.Bidirectional:
		return "bidirectional"
	default:
		return "directed"
	}
}
