package layout

// makeAcyclic removes self-loops from the ranking graph and reverses only
// edges that run backwards in a deterministic order within an SCC.
func makeAcyclic(nodes []*workingNode, edges []*workingEdge, useFeedbackOrder bool) ([]*workingEdge, []*workingEdge, int) {
	component := stronglyConnectedComponents(nodes, edges)
	positions := make(map[*workingNode]int, len(nodes))
	byComponent := make(map[int][]*workingNode)
	for _, node := range nodes {
		byComponent[component[node]] = append(byComponent[component[node]], node)
	}
	for _, members := range byComponent {
		sortWorkingNodes(members)
		order := members
		if useFeedbackOrder {
			candidate := feedbackOrder(members, edges)
			if backwardEdgeCount(candidate, edges) < backwardEdgeCount(members, edges) {
				order = candidate
			}
		}
		for position, node := range order {
			positions[node] = position
		}
	}

	dagEdges := make([]*workingEdge, 0, len(edges))
	var loops []*workingEdge
	reversed := 0
	for _, edge := range edges {
		if edge.reversed {
			reversed++
		}
		if edge.from == edge.to {
			edge.selfLoop = true
			loops = append(loops, edge)
			continue
		}
		if component[edge.from] == component[edge.to] && positions[edge.from] > positions[edge.to] {
			edge.from, edge.to = edge.to, edge.from
			edge.reversed = !edge.reversed
			if edge.reversed {
				reversed++
			} else {
				reversed--
			}
		}
		dagEdges = append(dagEdges, edge)
	}
	return dagEdges, loops, reversed
}

// feedbackOrder applies the deterministic Eades source/sink heuristic. Nodes
// with no remaining incoming or outgoing edge are removed first; otherwise the
// node with the largest outdegree-indegree difference is selected.
func feedbackOrder(nodes []*workingNode, edges []*workingEdge) []*workingNode {
	remaining := make(map[*workingNode]bool, len(nodes))
	for _, node := range nodes {
		remaining[node] = true
	}
	left := make([]*workingNode, 0, len(nodes))
	right := make([]*workingNode, 0, len(nodes))
	for len(remaining) > 0 {
		incoming, outgoing := remainingDegrees(remaining, edges)
		if node := selectDegreeNode(remaining, incoming, outgoing, true); node != nil {
			right = append(right, node)
			delete(remaining, node)
			continue
		}
		if node := selectSource(remaining, incoming); node != nil {
			left = append(left, node)
			delete(remaining, node)
			continue
		}
		var selected *workingNode
		for node := range remaining {
			if selected == nil || outgoing[node]-incoming[node] > outgoing[selected]-incoming[selected] ||
				(outgoing[node]-incoming[node] == outgoing[selected]-incoming[selected] && workingNodeLess(node, selected)) {
				selected = node
			}
		}
		left = append(left, selected)
		delete(remaining, selected)
	}
	for i := len(right) - 1; i >= 0; i-- {
		left = append(left, right[i])
	}
	return left
}

func backwardEdgeCount(nodes []*workingNode, edges []*workingEdge) int {
	position := make(map[*workingNode]int, len(nodes))
	for index, node := range nodes {
		position[node] = index
	}
	count := 0
	for _, edge := range edges {
		from, fromOK := position[edge.from]
		to, toOK := position[edge.to]
		if fromOK && toOK && from > to {
			count++
		}
	}
	return count
}

func remainingDegrees(remaining map[*workingNode]bool, edges []*workingEdge) (map[*workingNode]int, map[*workingNode]int) {
	incoming := make(map[*workingNode]int, len(remaining))
	outgoing := make(map[*workingNode]int, len(remaining))
	for _, edge := range edges {
		if edge.from != edge.to && remaining[edge.from] && remaining[edge.to] {
			outgoing[edge.from]++
			incoming[edge.to]++
		}
	}
	return incoming, outgoing
}

func selectDegreeNode(remaining map[*workingNode]bool, incoming, outgoing map[*workingNode]int, sink bool) *workingNode {
	var selected *workingNode
	for node := range remaining {
		matches := outgoing[node] == 0
		if !sink {
			matches = incoming[node] == 0
		}
		if matches && (selected == nil || workingNodeLess(node, selected)) {
			selected = node
		}
	}
	return selected
}

func selectSource(remaining map[*workingNode]bool, incoming map[*workingNode]int) *workingNode {
	return selectDegreeNode(remaining, incoming, nil, false)
}

func workingNodeLess(left, right *workingNode) bool {
	if left.inputOrder != right.inputOrder {
		return left.inputOrder < right.inputOrder
	}
	return left.id < right.id
}

func stronglyConnectedComponents(nodes []*workingNode, edges []*workingEdge) map[*workingNode]int {
	outgoing := make(map[*workingNode][]*workingNode, len(nodes))
	for _, edge := range edges {
		outgoing[edge.from] = append(outgoing[edge.from], edge.to)
	}

	index := 0
	componentID := 0
	indices := make(map[*workingNode]int, len(nodes))
	lowlink := make(map[*workingNode]int, len(nodes))
	onStack := make(map[*workingNode]bool, len(nodes))
	stack := make([]*workingNode, 0, len(nodes))
	components := make(map[*workingNode]int, len(nodes))
	for _, node := range nodes {
		indices[node] = -1
	}

	var visit func(*workingNode)
	visit = func(node *workingNode) {
		indices[node] = index
		lowlink[node] = index
		index++
		stack = append(stack, node)
		onStack[node] = true

		for _, target := range outgoing[node] {
			if indices[target] == -1 {
				visit(target)
				lowlink[node] = min(lowlink[node], lowlink[target])
			} else if onStack[target] {
				lowlink[node] = min(lowlink[node], indices[target])
			}
		}
		if lowlink[node] != indices[node] {
			return
		}

		for {
			last := len(stack) - 1
			member := stack[last]
			stack = stack[:last]
			onStack[member] = false
			components[member] = componentID
			if member == node {
				break
			}
		}
		componentID++
	}

	ordered := append([]*workingNode(nil), nodes...)
	sortWorkingNodes(ordered)
	for _, node := range ordered {
		if indices[node] == -1 {
			visit(node)
		}
	}
	return components
}
