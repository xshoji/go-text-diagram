package diagram

func cloneNodes(source []Node) []Node {
	result := make([]Node, len(source))
	for index, node := range source {
		result[index] = cloneNode(node)
	}
	return result
}

func cloneNode(node Node) Node {
	node.Rank.Fixed = cloneInt(node.Rank.Fixed)
	node.Cells = cloneCellMatrix(node.Cells)
	node.Ports = append([]NamedPort(nil), node.Ports...)
	return node
}

func cloneGroups(source []Group) []Group {
	result := make([]Group, len(source))
	for index, group := range source {
		result[index] = cloneGroup(group)
	}
	return result
}

func cloneGroup(group Group) Group {
	group.Members = append([]NodeID(nil), group.Members...)
	group.Ports = append([]NamedPort(nil), group.Ports...)
	return group
}

func cloneEdges(source []Edge) []Edge {
	result := make([]Edge, len(source))
	for index, edge := range source {
		result[index] = cloneEdge(edge)
	}
	return result
}

func cloneEdge(edge Edge) Edge {
	edge.Start.Offset = cloneInt(edge.Start.Offset)
	edge.End.Offset = cloneInt(edge.End.Offset)
	edge.Source.PortHint.Offset = cloneInt(edge.Source.PortHint.Offset)
	edge.Target.PortHint.Offset = cloneInt(edge.Target.PortHint.Offset)
	return edge
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneCellMatrix(source [][]Cell) [][]Cell {
	result := make([][]Cell, len(source))
	for index, row := range source {
		result[index] = append([]Cell(nil), row...)
	}
	return result
}
