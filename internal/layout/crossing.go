package layout

import "sort"

type barycenter struct {
	sum   int
	count int
}

func reduceCrossings(layers [][]*workingNode, edges []*workingEdge, rounds int) (int, int) {
	bestCrossings := countCrossings(layers, edges)
	best := snapshotOrders(layers)
	for round := 0; round < rounds; round++ {
		for rank := 1; rank < len(layers); rank++ {
			reorderLayer(layers[rank], edges, rank-1, true, round%2 == 1)
		}
		for rank := len(layers) - 2; rank >= 0; rank-- {
			reorderLayer(layers[rank], edges, rank+1, false, round%2 == 1)
		}
		transposeLayers(layers, edges)
		if crossings := countCrossings(layers, edges); crossings < bestCrossings {
			bestCrossings, best = crossings, snapshotOrders(layers)
		}
	}
	restoreOrders(layers, best)
	return bestCrossings, rounds
}

func reorderLayer(layer []*workingNode, edges []*workingEdge, adjacentRank int, useIncoming, useMedian bool) {
	values := make(map[*workingNode]barycenter, len(layer))
	positions := make(map[*workingNode][]int, len(layer))
	medians := make(map[*workingNode]int, len(layer))
	hasNeighbor := make(map[*workingNode]bool, len(layer))
	for _, node := range layer {
		values[node] = barycenter{sum: node.order, count: 1}
	}
	for _, edge := range edges {
		var node, adjacent *workingNode
		if useIncoming {
			node, adjacent = edge.to, edge.from
		} else {
			node, adjacent = edge.from, edge.to
		}
		if adjacent.rank != adjacentRank {
			continue
		}
		value := values[node]
		if !hasNeighbor[node] {
			value = barycenter{}
			hasNeighbor[node] = true
		}
		value.sum += adjacent.order
		value.count++
		values[node] = value
		positions[node] = append(positions[node], adjacent.order)
	}
	if useMedian {
		for node, adjacentPositions := range positions {
			medians[node] = medianPosition(adjacentPositions)
		}
	}
	sort.SliceStable(layer, func(i, j int) bool {
		if useMedian && len(positions[layer[i]]) > 0 && len(positions[layer[j]]) > 0 {
			if left, right := medians[layer[i]], medians[layer[j]]; left != right {
				return left < right
			}
		}
		left, right := values[layer[i]], values[layer[j]]
		return left.sum*right.count < right.sum*left.count
	})
	updateOrders(layer)
}

func medianPosition(positions []int) int {
	sorted := append([]int(nil), positions...)
	sort.Ints(sorted)
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle] * 2
	}
	return sorted[middle-1] + sorted[middle]
}

func transposeLayers(layers [][]*workingNode, edges []*workingEdge) {
	for rank, layer := range layers {
		for index := 0; index+1 < len(layer); index++ {
			best := adjacentCrossings(rank, layers, edges)
			layer[index], layer[index+1] = layer[index+1], layer[index]
			updateOrders(layer)
			if crossings := adjacentCrossings(rank, layers, edges); crossings < best {
				continue
			}
			layer[index], layer[index+1] = layer[index+1], layer[index]
			updateOrders(layer)
		}
	}
}

func adjacentCrossings(rank int, layers [][]*workingNode, edges []*workingEdge) int {
	total := 0
	if rank > 0 {
		total += countCrossingsBetween(rank-1, edges)
	}
	if rank+1 < len(layers) {
		total += countCrossingsBetween(rank, edges)
	}
	return total
}

func countCrossings(layers [][]*workingNode, edges []*workingEdge) int {
	total := 0
	for rank := 0; rank+1 < len(layers); rank++ {
		total += countCrossingsBetween(rank, edges)
	}
	return total
}

func countCrossingsBetween(rank int, edges []*workingEdge) int {
	var rankEdges []*workingEdge
	for _, edge := range edges {
		if edge.from.rank == rank && edge.to.rank == rank+1 {
			rankEdges = append(rankEdges, edge)
		}
	}
	total := 0
	for i := 0; i < len(rankEdges); i++ {
		for j := i + 1; j < len(rankEdges); j++ {
			fromDelta := rankEdges[i].from.order - rankEdges[j].from.order
			toDelta := rankEdges[i].to.order - rankEdges[j].to.order
			if fromDelta*toDelta < 0 {
				total++
			}
		}
	}
	return total
}

func snapshotOrders(layers [][]*workingNode) [][]string {
	result := make([][]string, len(layers))
	for rank, layer := range layers {
		for _, node := range layer {
			result[rank] = append(result[rank], node.id)
		}
	}
	return result
}

func restoreOrders(layers [][]*workingNode, orders [][]string) {
	for rank, ids := range orders {
		byID := make(map[string]*workingNode, len(layers[rank]))
		for _, node := range layers[rank] {
			byID[node.id] = node
		}
		for order, id := range ids {
			layers[rank][order] = byID[id]
		}
		updateOrders(layers[rank])
	}
}
