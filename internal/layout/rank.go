package layout

import (
	"fmt"
	"sort"

	"github.com/xshoji/agents-workspace/internal/diagram"
)

func topologicalOrder(nodes []*workingNode, edges []*workingEdge) ([]*workingNode, error) {
	indegree := make(map[*workingNode]int, len(nodes))
	outgoing := make(map[*workingNode][]*workingNode, len(nodes))
	for _, edge := range edges {
		indegree[edge.to]++
		outgoing[edge.from] = append(outgoing[edge.from], edge.to)
	}

	var ready []*workingNode
	for _, node := range nodes {
		if indegree[node] == 0 {
			ready = append(ready, node)
		}
	}
	sortWorkingNodes(ready)

	order := make([]*workingNode, 0, len(nodes))
	for len(ready) > 0 {
		node := ready[0]
		ready = ready[1:]
		order = append(order, node)
		for _, target := range outgoing[node] {
			indegree[target]--
			if indegree[target] == 0 {
				ready = append(ready, target)
				sortWorkingNodes(ready)
			}
		}
	}
	if len(order) != len(nodes) {
		return nil, fmt.Errorf("%w: ranking accepts DAGs only", ErrCyclicGraph)
	}
	return order, nil
}

func sortWorkingNodes(nodes []*workingNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].inputOrder != nodes[j].inputOrder {
			return nodes[i].inputOrder < nodes[j].inputOrder
		}
		return nodes[i].id < nodes[j].id
	})
}

func assignRanks(order []*workingNode, edges []*workingEdge) error {
	groups := make(map[string][]*workingNode)
	fixed := make(map[*workingNode]int)
	for _, node := range order {
		if node.rankConstraint.Root {
			fixed[node] = 0
		}
		if node.rankConstraint.Fixed != nil {
			if rank, exists := fixed[node]; exists && rank != *node.rankConstraint.Fixed {
				return fmt.Errorf("%w: node %q has conflicting root and rank constraints", ErrInvalidGraph, node.id)
			}
			fixed[node] = *node.rankConstraint.Fixed
		}
		if node.rankConstraint.Same != "" {
			groups[node.rankConstraint.Same] = append(groups[node.rankConstraint.Same], node)
		}
	}
	for name, members := range groups {
		groupRank := -1
		for _, node := range members {
			if rank, ok := fixed[node]; ok {
				if groupRank >= 0 && groupRank != rank {
					return fmt.Errorf("%w: same-rank group %q has conflicting fixed ranks", ErrInvalidGraph, name)
				}
				groupRank = rank
			}
		}
		if groupRank >= 0 {
			for _, node := range members {
				fixed[node] = groupRank
			}
		}
	}

	for _, node := range order {
		if rank, ok := fixed[node]; ok {
			node.rank = rank
		}
	}
	limit := max(1, len(order)*(len(edges)+len(groups)+1))
	for iteration := 0; iteration < limit; iteration++ {
		changed := false
		for _, members := range groups {
			rank := 0
			for _, node := range members {
				rank = max(rank, node.rank)
			}
			for _, node := range members {
				if expected, ok := fixed[node]; ok && rank > expected {
					return fmt.Errorf("%w: constraints require node %q above fixed rank %d", ErrInvalidGraph, node.id, expected)
				}
				if node.rank != rank {
					node.rank = rank
					changed = true
				}
			}
		}
		for _, edge := range edges {
			candidate := edge.from.rank + max(1, edge.minLength)
			if candidate > diagram.MaximumRank {
				return fmt.Errorf("%w: edge %q requires rank %d above limit %d", ErrInvalidGraph, edge.originalEdgeID, candidate, diagram.MaximumRank)
			}
			if candidate <= edge.to.rank {
				continue
			}
			if expected, ok := fixed[edge.to]; ok {
				return fmt.Errorf("%w: edge %q requires rank %d but node %q is fixed at %d", ErrInvalidGraph, edge.originalEdgeID, candidate, edge.to.id, expected)
			}
			edge.to.rank = candidate
			changed = true
		}
		if !changed {
			return nil
		}
	}
	return fmt.Errorf("%w: rank constraints do not converge", ErrInvalidGraph)
}

const maximumDummyNodes = 10_000

func validateDummyExpansion(edges []*workingEdge) error {
	total := 0
	for _, edge := range edges {
		count := edge.to.rank - edge.from.rank - 1
		if count <= 0 {
			continue
		}
		if count > maximumDummyNodes-total {
			return fmt.Errorf("%w: layout requires more than %d dummy nodes", ErrInvalidGraph, maximumDummyNodes)
		}
		total += count
	}
	return nil
}

func insertDummyNodes(nodes []*workingNode, nodeByID map[string]*workingNode, edges []*workingEdge) ([]*workingNode, []*workingEdge, int) {
	resultEdges := make([]*workingEdge, 0, len(edges))
	longEdges := 0
	for _, edge := range edges {
		span := edge.to.rank - edge.from.rank
		if span <= 1 {
			resultEdges = append(resultEdges, edge)
			continue
		}

		longEdges++
		previous := edge.from
		for segment := 1; segment < span; segment++ {
			baseID := fmt.Sprintf("__dummy_%s_%d", edge.originalEdgeID, segment)
			id := baseID
			for suffix := 1; nodeByID[id] != nil; suffix++ {
				id = fmt.Sprintf("%s_%d", baseID, suffix)
			}
			dummy := &workingNode{id: id, ownerEdgeID: edge.originalEdgeID, inputOrder: len(nodes), rank: edge.from.rank + segment, dummy: true}
			nodes = append(nodes, dummy)
			nodeByID[id] = dummy
			start := diagram.PortHint{}
			if segment == 1 {
				start = edge.start
			}
			resultEdges = append(resultEdges, &workingEdge{
				id: fmt.Sprintf("%s:%d", edge.originalEdgeID, segment-1), originalEdgeID: edge.originalEdgeID,
				from: previous, to: dummy, label: edge.label, inputOrder: edge.inputOrder, segment: segment - 1,
				originalFrom: edge.originalFrom, originalTo: edge.originalTo, reversed: edge.reversed,
				kind: edge.kind, start: start, minLength: edge.minLength, lineStyle: edge.lineStyle, arrowStyle: edge.arrowStyle,
				source: edge.source, target: edge.target,
			})
			previous = dummy
		}
		resultEdges = append(resultEdges, &workingEdge{
			id: fmt.Sprintf("%s:%d", edge.originalEdgeID, span-1), originalEdgeID: edge.originalEdgeID,
			from: previous, to: edge.to, label: edge.label, inputOrder: edge.inputOrder, segment: span - 1,
			originalFrom: edge.originalFrom, originalTo: edge.originalTo, reversed: edge.reversed,
			kind: edge.kind, end: edge.end, minLength: edge.minLength, lineStyle: edge.lineStyle, arrowStyle: edge.arrowStyle,
			source: edge.source, target: edge.target,
		})
	}
	return nodes, resultEdges, longEdges
}

func makeLayers(nodes []*workingNode) [][]*workingNode {
	maxRank := 0
	for _, node := range nodes {
		maxRank = max(maxRank, node.rank)
	}
	layers := make([][]*workingNode, maxRank+1)
	for _, node := range nodes {
		layers[node.rank] = append(layers[node.rank], node)
	}
	for _, layer := range layers {
		sortWorkingNodes(layer)
		updateOrders(layer)
	}
	return layers
}

func updateOrders(layer []*workingNode) {
	for order, node := range layer {
		node.order = order
	}
}
