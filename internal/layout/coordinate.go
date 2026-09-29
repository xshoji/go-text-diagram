package layout

import (
	"github.com/xshoji/agents-workspace/internal/diagram"
	"github.com/xshoji/agents-workspace/internal/geom"
	"github.com/xshoji/agents-workspace/internal/textwidth"
)

func assignCoordinates(layers [][]*workingNode, direction diagram.Direction, options Options) (map[*workingNode]Rect, int, int) {
	rects := make(map[*workingNode]Rect)
	maxCrossSize := 0
	for _, layer := range layers {
		size := 0
		for i, node := range layer {
			if i > 0 {
				size += options.NodeSpacing
			}
			if direction.Vertical() {
				size += node.width
			} else {
				size += node.height
			}
		}
		maxCrossSize = max(maxCrossSize, size)
	}

	rankPosition, maxRankExtent := 0, 0
	for _, layer := range layers {
		crossSize, rankExtent := 0, 1
		for i, node := range layer {
			if i > 0 {
				crossSize += options.NodeSpacing
			}
			if direction.Vertical() {
				crossSize += node.width
				rankExtent = max(rankExtent, node.height)
			} else {
				crossSize += node.height
				rankExtent = max(rankExtent, node.width)
			}
		}
		crossPosition := (maxCrossSize - crossSize) / 2
		for _, node := range layer {
			width, height := node.width, node.height
			if direction.Vertical() {
				rects[node] = Rect{X: crossPosition, Y: rankPosition, Width: width, Height: height}
				crossPosition += width + options.NodeSpacing
			} else {
				rects[node] = Rect{X: rankPosition, Y: crossPosition, Width: width, Height: height}
				crossPosition += height + options.NodeSpacing
			}
		}
		rankPosition += rankExtent + options.LayerSpacing
		maxRankExtent = rankPosition - options.LayerSpacing
	}
	if direction.Vertical() {
		if direction == diagram.DirectionUp {
			for node, rect := range rects {
				rect.Y = maxRankExtent - rect.Y - rect.Height
				rects[node] = rect
			}
		}
		return rects, maxCrossSize, maxRankExtent
	}
	if direction == diagram.DirectionLeft {
		for node, rect := range rects {
			rect.X = maxRankExtent - rect.X - rect.Width
			rects[node] = rect
		}
	}
	return rects, maxRankExtent, maxCrossSize
}
func requiredLayerSpacing(edges []*workingEdge, rects map[*workingNode]Rect, direction diagram.Direction, minimum, laneSize int) int {
	byRank := make(map[int][]*workingEdge)
	for _, edge := range edges {
		if !edge.reversed && !edge.selfLoop {
			byRank[edge.from.rank] = append(byRank[edge.from.rank], edge)
		}
	}
	required := minimum
	for _, rankEdges := range byRank {
		var lanes [][]geom.Interval
		for _, edge := range rankEdges {
			from, to := rects[edge.from], rects[edge.to]
			candidate := geom.Interval{Start: from.X + (from.Width-1)/2, End: to.X + (to.Width-1)/2}
			if !direction.Vertical() {
				candidate.Start = from.Y + (from.Height-1)/2
				candidate.End = to.Y + (to.Height-1)/2
			}
			if candidate.Start > candidate.End {
				candidate.Start, candidate.End = candidate.End, candidate.Start
			}
			lane := geom.FirstAvailableLane(lanes, candidate)
			if lane == len(lanes) {
				lanes = append(lanes, nil)
			}
			lanes[lane] = append(lanes[lane], candidate)
		}
		// The label extent is already included in minimum. Routing lanes only
		// need a channel cell each; multiplying every lane by the longest label
		// made labeled, dense graphs grow by thousands of cells.
		required = max(required, len(lanes)*laneSize)
	}
	return required
}

func assignPortRequirements(nodes []*workingNode, edges []*workingEdge) {
	for _, node := range nodes {
		node.incomingPorts, node.outgoingPorts = 0, 0
	}
	for _, edge := range edges {
		if !edge.reversed && !edge.selfLoop {
			edge.from.outgoingPorts++
			edge.to.incomingPorts++
		}
	}
}

func measureNodeSizes(nodes []*workingNode, direction diagram.Direction, options Options) {
	for _, node := range nodes {
		if node.dummy {
			node.width, node.height = 1, 1
			continue
		}
		lines := textwidth.Wrap(node.label, node.textWrap)
		labelWidth := 0
		for _, line := range lines {
			labelWidth = max(labelWidth, textwidth.String(line))
		}
		if len(node.cells) > 0 {
			cellWidth, cellHeight := recordSize(node.cells)
			labelWidth, lines = cellWidth, make([]string, cellHeight)
		}
		if node.shape == diagram.ShapePoint {
			node.width, node.height = 3, 1
			continue
		}
		node.width = max(options.NodeWidth, labelWidth+4)
		node.height = max(options.NodeHeight, len(lines)+2)
		ports := max(node.incomingPorts, node.outgoingPorts)
		if direction.Vertical() {
			node.width = max(node.width, ports+2)
		} else {
			node.height = max(node.height, ports+2)
		}
	}
}

func recordSize(rows [][]string) (int, int) {
	widths := recordColumnWidths(rows)
	width := 1
	for _, value := range widths {
		width += value + 1
	}
	return width - 4, len(rows)*2 - 1
}

func measureEdgeLabels(edges []*workingEdge, direction diagram.Direction) (laneSize, margin int) {
	for _, edge := range edges {
		if edge.label == "" || edge.lineStyle == diagram.LineInvisible {
			continue
		}
		lines, width := textwidth.Lines(edge.label)
		if direction.Vertical() {
			laneSize = max(laneSize, len(lines)+2)
			margin = max(margin, max(0, (width-1)/2-2))
		} else {
			laneSize = max(laneSize, width+2)
			margin = max(margin, max(0, (len(lines)-1)/2-1))
		}
	}
	return laneSize, margin
}

func maxSelfLoopsPerNode(loops []*workingEdge) int {
	counts := make(map[*workingNode]int)
	maximum := 0
	for _, edge := range loops {
		counts[edge.from]++
		maximum = max(maximum, counts[edge.from])
	}
	return maximum
}

func reserveSpecialRouteSpace(rects map[*workingNode]Rect, loops []*workingEdge, direction diagram.Direction, reversedEdges, layerSpacing, width, height int, outerLaneStart *int) (int, int) {
	loopCounts := make(map[*workingNode]int)
	loopExtra := make(map[*workingNode]int)
	for _, edge := range loops {
		loopCounts[edge.from]++
		if edge.label == "" {
			continue
		}
		lines, labelWidth := textwidth.Lines(edge.label)
		labelSize := labelWidth
		if !direction.Vertical() {
			labelSize = len(lines)
		}
		loopExtra[edge.from] = max(loopExtra[edge.from], labelSize+1)
	}
	for node, count := range loopCounts {
		rect := rects[node]
		extra := count + loopExtra[node]
		if direction.Vertical() {
			width = max(width, rect.X+rect.Width+extra)
		} else {
			height = max(height, rect.Y+rect.Height+extra)
		}
	}
	if reversedEdges == 0 {
		return width, height
	}
	if direction.Vertical() {
		height += layerSpacing
		*outerLaneStart = width + 1
		width = *outerLaneStart + reversedEdges
	} else {
		width += layerSpacing
		*outerLaneStart = height + 1
		height = *outerLaneStart + reversedEdges
	}
	return width, height
}
