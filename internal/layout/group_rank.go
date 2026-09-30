package layout

import (
	"sort"

	"github.com/xshoji/go-text-diagram/internal/diagram"
)

type flowScopeUnit struct {
	nodes   map[*workingNode]bool
	order   int
	padding int
}

// applyGroupFlowRanks separates dependent sibling scopes into rank bands with
// enough room for their group padding. Unsupported rank constraints restore
// the original ranks and let the optimizer use the lateral group layout.
func applyGroupFlowRanks(groups []diagram.Group, nodes []*workingNode, edges []*workingEdge, layerSpacing int) bool {
	if len(groups) == 0 {
		return false
	}
	originalRanks := make(map[*workingNode]int, len(nodes))
	byNodeID := make(map[string]*workingNode, len(nodes))
	for _, node := range nodes {
		originalRanks[node] = node.rank
		byNodeID[node.id] = node
		if node.rankConstraint.Fixed != nil || node.rankConstraint.Root {
			return false
		}
	}
	restore := func() {
		for node, rank := range originalRanks {
			node.rank = rank
		}
	}

	byID := make(map[string]*diagram.Group, len(groups))
	children := make(map[string][]*diagram.Group)
	owner := make(map[string]string, len(nodes))
	for i := range groups {
		group := &groups[i]
		if group == nil || group.ID == "" || byID[group.ID] != nil {
			return false
		}
		byID[group.ID] = group
		children[group.Parent] = append(children[group.Parent], group)
		for _, member := range group.Members {
			if _, exists := owner[member]; exists {
				return false
			}
			owner[member] = group.ID
		}
	}
	for _, group := range groups {
		if group.Parent != "" && byID[group.Parent] == nil {
			return false
		}
	}
	for parent := range children {
		sort.SliceStable(children[parent], func(i, j int) bool {
			if children[parent][i].InputOrder != children[parent][j].InputOrder {
				return children[parent][i].InputOrder < children[parent][j].InputOrder
			}
			return children[parent][i].ID < children[parent][j].ID
		})
	}

	sameOwner := make(map[string]string)
	for _, node := range nodes {
		name := node.rankConstraint.Same
		if name == "" {
			continue
		}
		current := owner[node.id]
		if previous, exists := sameOwner[name]; exists && previous != current {
			return false
		}
		sameOwner[name] = current
	}

	descendants := make(map[string]map[*workingNode]bool)
	visiting := make(map[string]bool)
	var membersOf func(string) (map[*workingNode]bool, bool)
	membersOf = func(id string) (map[*workingNode]bool, bool) {
		if members := descendants[id]; members != nil {
			return members, true
		}
		if visiting[id] {
			return nil, false
		}
		visiting[id] = true
		members := make(map[*workingNode]bool)
		for _, memberID := range byID[id].Members {
			node := byNodeID[memberID]
			if node == nil {
				return nil, false
			}
			members[node] = true
		}
		for _, child := range children[id] {
			childMembers, ok := membersOf(child.ID)
			if !ok {
				return nil, false
			}
			for node := range childMembers {
				members[node] = true
			}
		}
		visiting[id] = false
		descendants[id] = members
		return members, true
	}
	for _, group := range groups {
		if _, ok := membersOf(group.ID); !ok {
			return false
		}
	}

	var arrange func(string) bool
	arrange = func(parent string) bool {
		for _, child := range children[parent] {
			if !arrange(child.ID) {
				return false
			}
		}
		var units []flowScopeUnit
		for _, child := range children[parent] {
			units = append(units, flowScopeUnit{nodes: descendants[child.ID], order: child.InputOrder, padding: child.Padding})
		}
		for _, node := range nodes {
			if owner[node.id] == parent {
				units = append(units, flowScopeUnit{nodes: map[*workingNode]bool{node: true}, order: node.inputOrder})
			}
		}
		return arrangeFlowUnits(units, edges, layerSpacing)
	}
	if !arrange("") || !validFlowRanks(nodes, edges) {
		restore()
		return false
	}
	return true
}

func arrangeFlowUnits(units []flowScopeUnit, edges []*workingEdge, layerSpacing int) bool {
	if len(units) < 2 {
		return true
	}
	sort.SliceStable(units, func(i, j int) bool { return units[i].order < units[j].order })
	unitOf := make(map[*workingNode]int)
	for index, unit := range units {
		for node := range unit.nodes {
			if _, exists := unitOf[node]; exists {
				return false
			}
			unitOf[node] = index
		}
	}
	adjacent := make([]map[int]bool, len(units))
	distances := make([]map[int]int, len(units))
	for index := range adjacent {
		adjacent[index] = make(map[int]bool)
		distances[index] = make(map[int]int)
	}
	for _, edge := range edges {
		from, fromOK := unitOf[edge.from]
		to, toOK := unitOf[edge.to]
		if fromOK && toOK && from != to {
			adjacent[from][to] = true
			distances[from][to] = max(distances[from][to], scopeRankDistance(units[from], units[to], layerSpacing))
		}
	}
	components, count := flowUnitComponents(adjacent)
	componentNodes := make([]map[*workingNode]bool, count)
	componentOrder := make([]int, count)
	for index := range componentOrder {
		componentOrder[index] = int(^uint(0) >> 1)
		componentNodes[index] = make(map[*workingNode]bool)
	}
	for unitIndex, component := range components {
		componentOrder[component] = min(componentOrder[component], units[unitIndex].order)
		for node := range units[unitIndex].nodes {
			componentNodes[component][node] = true
		}
	}
	componentEdges := make([]map[int]int, count)
	indegree := make([]int, count)
	for index := range componentEdges {
		componentEdges[index] = make(map[int]int)
	}
	for from, targets := range adjacent {
		for to := range targets {
			left, right := components[from], components[to]
			if left == right {
				continue
			}
			if componentEdges[left][right] == 0 {
				indegree[right]++
			}
			componentEdges[left][right] = max(componentEdges[left][right], distances[from][to])
		}
	}
	var ready []int
	for component := 0; component < count; component++ {
		if indegree[component] == 0 {
			ready = append(ready, component)
		}
	}
	sortComponents := func() {
		sort.SliceStable(ready, func(i, j int) bool {
			if componentOrder[ready[i]] != componentOrder[ready[j]] {
				return componentOrder[ready[i]] < componentOrder[ready[j]]
			}
			return ready[i] < ready[j]
		})
	}
	sortComponents()
	processed := 0
	for len(ready) > 0 {
		component := ready[0]
		ready = ready[1:]
		processed++
		maximum := flowMaximumRank(componentNodes[component])
		for target, distance := range componentEdges[component] {
			minimum := flowMinimumRank(componentNodes[target])
			if delta := maximum + distance - minimum; delta > 0 {
				for node := range componentNodes[target] {
					node.rank += delta
				}
			}
			indegree[target]--
			if indegree[target] == 0 {
				ready = append(ready, target)
				sortComponents()
			}
		}
	}
	return processed == count
}

func scopeRankDistance(from, to flowScopeUnit, layerSpacing int) int {
	missing := from.padding + to.padding - layerSpacing
	if missing <= 0 {
		return 1
	}
	return 1 + (missing+layerSpacing)/(layerSpacing+1)
}

func flowUnitComponents(adjacent []map[int]bool) ([]int, int) {
	index, componentCount := 0, 0
	indices := make([]int, len(adjacent))
	lowlink := make([]int, len(adjacent))
	components := make([]int, len(adjacent))
	onStack := make([]bool, len(adjacent))
	for i := range indices {
		indices[i] = -1
	}
	var stack []int
	var visit func(int)
	visit = func(node int) {
		indices[node], lowlink[node] = index, index
		index++
		stack = append(stack, node)
		onStack[node] = true
		var targets []int
		for target := range adjacent[node] {
			targets = append(targets, target)
		}
		sort.Ints(targets)
		for _, target := range targets {
			if indices[target] < 0 {
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
			components[member] = componentCount
			if member == node {
				break
			}
		}
		componentCount++
	}
	for node := range adjacent {
		if indices[node] < 0 {
			visit(node)
		}
	}
	return components, componentCount
}

func flowMinimumRank(nodes map[*workingNode]bool) int {
	minimum := int(^uint(0) >> 1)
	for node := range nodes {
		minimum = min(minimum, node.rank)
	}
	return minimum
}

func flowMaximumRank(nodes map[*workingNode]bool) int {
	maximum := 0
	for node := range nodes {
		maximum = max(maximum, node.rank)
	}
	return maximum
}

func validFlowRanks(nodes []*workingNode, edges []*workingEdge) bool {
	sameRanks := make(map[string]int)
	for _, node := range nodes {
		if node.rank < 0 {
			return false
		}
		if name := node.rankConstraint.Same; name != "" {
			if rank, exists := sameRanks[name]; exists && rank != node.rank {
				return false
			}
			sameRanks[name] = node.rank
		}
	}
	for _, edge := range edges {
		if edge.to.rank < edge.from.rank+max(1, edge.minLength) {
			return false
		}
	}
	return true
}
