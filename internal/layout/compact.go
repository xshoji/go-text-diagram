package layout

import "sort"

type weakComponent struct {
	nodes      []*workingNode
	firstOrder int
	bounds     Rect
}

// straightenCoordinates aligns an unambiguous chain when both neighbour
// centers agree, without changing crowded layer order.
func straightenCoordinates(layers [][]*workingNode, edges []*workingEdge, rects map[*workingNode]Rect, vertical bool) {
	neighbours := make(map[*workingNode][]*workingNode)
	for _, edge := range edges {
		neighbours[edge.from] = append(neighbours[edge.from], edge.to)
		neighbours[edge.to] = append(neighbours[edge.to], edge.from)
	}
	for _, layer := range layers {
		// Moving a crowded layer can invalidate its crossing-minimized order.
		// Restrict straightening to an unambiguous one-node chain whose two
		// neighbour centers already agree.
		if len(layer) != 1 || len(neighbours[layer[0]]) != 2 {
			continue
		}
		node := layer[0]
		first, second := rects[neighbours[node][0]], rects[neighbours[node][1]]
		rect := rects[node]
		if vertical {
			firstCenter, secondCenter := first.X+first.Width/2, second.X+second.Width/2
			if firstCenter == secondCenter {
				rect.X = firstCenter - rect.Width/2
			}
		} else {
			firstCenter, secondCenter := first.Y+first.Height/2, second.Y+second.Height/2
			if firstCenter == secondCenter {
				rect.Y = firstCenter - rect.Height/2
			}
		}
		rects[node] = rect
	}
	normalizeRects(rects)
}

// packComponents places weakly connected components on deterministic shelves.
// maxWidth <= 0 keeps a single shelf.
func packComponents(nodes []*workingNode, edges []*workingEdge, rects map[*workingNode]Rect, spacing, maxWidth int) (int, int) {
	components := weakComponents(nodes, edges, rects)
	if len(components) <= 1 {
		return rectExtent(rects)
	}
	x, y, rowHeight, width := 0, 0, 0, 0
	for _, component := range components {
		componentWidth := component.bounds.Width
		if maxWidth > 0 && x > 0 && x+componentWidth > maxWidth {
			x = 0
			y += rowHeight + spacing
			rowHeight = 0
		}
		dx, dy := x-component.bounds.X, y-component.bounds.Y
		for _, node := range component.nodes {
			rect := rects[node]
			rect.X += dx
			rect.Y += dy
			rects[node] = rect
		}
		x += componentWidth + spacing
		rowHeight = max(rowHeight, component.bounds.Height)
		width = max(width, x-spacing)
	}
	return width, y + rowHeight
}

func weakComponents(nodes []*workingNode, edges []*workingEdge, rects map[*workingNode]Rect) []weakComponent {
	adjacent := make(map[*workingNode][]*workingNode, len(nodes))
	for _, edge := range edges {
		adjacent[edge.from] = append(adjacent[edge.from], edge.to)
		adjacent[edge.to] = append(adjacent[edge.to], edge.from)
	}
	ordered := append([]*workingNode(nil), nodes...)
	sortWorkingNodes(ordered)
	seen := make(map[*workingNode]bool, len(nodes))
	var result []weakComponent
	for _, start := range ordered {
		if seen[start] {
			continue
		}
		component := weakComponent{firstOrder: start.inputOrder}
		queue := []*workingNode{start}
		seen[start] = true
		for len(queue) > 0 {
			node := queue[0]
			queue = queue[1:]
			component.nodes = append(component.nodes, node)
			for _, next := range adjacent[node] {
				if !seen[next] {
					seen[next] = true
					queue = append(queue, next)
				}
			}
		}
		component.bounds = boundsOf(component.nodes, rects)
		result = append(result, component)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].firstOrder < result[j].firstOrder })
	for componentID := range result {
		for _, node := range result[componentID].nodes {
			node.component = componentID
		}
	}
	return result
}

func boundsOf(nodes []*workingNode, rects map[*workingNode]Rect) Rect {
	if len(nodes) == 0 {
		return Rect{}
	}
	first := rects[nodes[0]]
	minX, minY := first.X, first.Y
	maxX, maxY := first.X+first.Width, first.Y+first.Height
	for _, node := range nodes[1:] {
		rect := rects[node]
		minX, minY = min(minX, rect.X), min(minY, rect.Y)
		maxX, maxY = max(maxX, rect.X+rect.Width), max(maxY, rect.Y+rect.Height)
	}
	return Rect{X: minX, Y: minY, Width: maxX - minX, Height: maxY - minY}
}

func normalizeRects(rects map[*workingNode]Rect) {
	minX, minY := 0, 0
	for _, rect := range rects {
		minX, minY = min(minX, rect.X), min(minY, rect.Y)
	}
	if minX == 0 && minY == 0 {
		return
	}
	for node, rect := range rects {
		rect.X -= minX
		rect.Y -= minY
		rects[node] = rect
	}
}

func rectExtent(rects map[*workingNode]Rect) (int, int) {
	width, height := 0, 0
	for _, rect := range rects {
		width = max(width, rect.X+rect.Width)
		height = max(height, rect.Y+rect.Height)
	}
	return width, height
}
