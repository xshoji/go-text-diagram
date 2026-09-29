package layout

import "sort"

type isotonicBlock struct {
	start int
	end   int
	sum   int
	count int
}

// alignFlowCoordinates keeps the crossing-minimized layer order while moving
// nodes toward the median center of their neighbours. Isotonic projection
// enforces spacing without turning every collision into a rightward staircase.
func alignFlowCoordinates(layers [][]*workingNode, edges []*workingEdge, rects map[*workingNode]Rect, vertical bool, spacing int) {
	if !vertical {
		return
	}
	incoming := make(map[*workingNode][]*workingNode)
	outgoing := make(map[*workingNode][]*workingNode)
	for _, edge := range edges {
		incoming[edge.to] = append(incoming[edge.to], edge.from)
		outgoing[edge.from] = append(outgoing[edge.from], edge.to)
	}
	for rank := 1; rank < len(layers); rank++ {
		alignFlowLayer(layers[rank], incoming, rects, spacing)
	}
	for rank := len(layers) - 2; rank >= 0; rank-- {
		alignFlowLayer(layers[rank], outgoing, rects, spacing)
	}
	normalizeRects(rects)
}

func alignFlowLayer(layer []*workingNode, neighbours map[*workingNode][]*workingNode, rects map[*workingNode]Rect, spacing int) {
	if len(layer) == 0 {
		return
	}
	prefix := make([]int, len(layer))
	desired := make([]int, len(layer))
	offset := 0
	for index, node := range layer {
		prefix[index] = offset
		rect := rects[node]
		center := rect.X + (rect.Width-1)/2
		if adjacent := neighbours[node]; len(adjacent) > 0 {
			centers := make([]int, 0, len(adjacent))
			for _, neighbour := range adjacent {
				other := rects[neighbour]
				centers = append(centers, other.X+(other.Width-1)/2)
			}
			sort.Ints(centers)
			middle := len(centers) / 2
			if len(centers)%2 == 1 {
				center = centers[middle]
			} else {
				center = (centers[middle-1] + centers[middle]) / 2
			}
		}
		desired[index] = center - (rect.Width-1)/2 - prefix[index]
		offset += rect.Width + max(1, spacing)
	}
	projected := isotonicProjection(desired)
	for index, node := range layer {
		rect := rects[node]
		rect.X = prefix[index] + projected[index]
		rects[node] = rect
	}
}

func isotonicProjection(values []int) []int {
	blocks := make([]isotonicBlock, 0, len(values))
	for index, value := range values {
		blocks = append(blocks, isotonicBlock{start: index, end: index + 1, sum: value, count: 1})
		for len(blocks) >= 2 {
			left, right := blocks[len(blocks)-2], blocks[len(blocks)-1]
			if left.sum*right.count <= right.sum*left.count {
				break
			}
			blocks[len(blocks)-2] = isotonicBlock{
				start: left.start, end: right.end,
				sum: left.sum + right.sum, count: left.count + right.count,
			}
			blocks = blocks[:len(blocks)-1]
		}
	}
	result := make([]int, len(values))
	for _, block := range blocks {
		value := block.sum / block.count
		for index := block.start; index < block.end; index++ {
			result[index] = value
		}
	}
	return result
}
