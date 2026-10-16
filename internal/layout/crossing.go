package layout

import (
	"sort"

	"github.com/xshoji/go-text-diagram/internal/diagram"
)

const maximumProximityWork int64 = 10_000_000

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

func improveLayerProximity(layers [][]*workingNode, edges []*workingEdge, groups []diagram.Group, direction diagram.Direction, options Options) int {
	grouped := make(map[*workingNode]bool)
	byID := make(map[string]*workingNode)
	connected := make(map[*workingNode]bool)
	nodeCount := 0
	for _, layer := range layers {
		nodeCount += len(layer)
		for _, node := range layer {
			byID[node.id] = node
		}
	}
	for _, group := range groups {
		for _, member := range group.Members {
			grouped[byID[member]] = true
		}
	}
	for _, edge := range edges {
		if edge.lineStyle != diagram.LineInvisible {
			connected[edge.from], connected[edge.to] = true, true
		}
	}

	distance := layerEdgeDistance(layers, edges, direction, options)
	workPerSwap := int64(nodeCount+len(edges)) + int64(len(edges))*int64(len(edges))
	evaluationLimit := int(maximumProximityWork / max(workPerSwap, 1))
	evaluationLimit = min(evaluationLimit, 4096)
	evaluations := 0
	for sweep := 0; sweep < 2; sweep++ {
		for rank, layer := range layers {
			for step := 0; step+1 < len(layer); step++ {
				index := step
				if sweep == 1 {
					index = len(layer) - step - 2
				}
				left, right := layer[index], layer[index+1]
				if left.dummy || right.dummy || grouped[left] || grouped[right] || !connected[left] && !connected[right] {
					continue
				}
				if evaluations >= evaluationLimit {
					return countCrossings(layers, edges)
				}
				evaluations++

				beforeCrossings := adjacentCrossings(rank, layers, edges)
				layer[index], layer[index+1] = right, left
				updateOrders(layer)
				nextDistance := layerEdgeDistance(layers, edges, direction, options)
				nextCrossings := adjacentCrossings(rank, layers, edges)
				if nextCrossings > beforeCrossings || nextCrossings == beforeCrossings && nextDistance >= distance {
					layer[index], layer[index+1] = left, right
					updateOrders(layer)
					continue
				}
				distance = nextDistance
			}
		}
	}
	return countCrossings(layers, edges)
}

func layerEdgeDistance(layers [][]*workingNode, edges []*workingEdge, direction diagram.Direction, options Options) int64 {
	rects, _, _ := assignCoordinates(layers, direction, options)
	center := func(node *workingNode) int {
		rect := rects[node]
		if direction.Vertical() {
			return rect.X + rect.Width/2
		}
		return rect.Y + rect.Height/2
	}
	distance := int64(0)
	for _, edge := range edges {
		if edge.lineStyle == diagram.LineInvisible {
			continue
		}
		delta := center(edge.from) - center(edge.to)
		if delta < 0 {
			delta = -delta
		}
		distance += int64(delta)
	}
	return distance
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
