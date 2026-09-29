package route

import (
	"github.com/xshoji/agents-workspace/internal/diagram"
	"github.com/xshoji/agents-workspace/internal/layout"
	"github.com/xshoji/agents-workspace/internal/textwidth"
)

func selfLoopPoints(node *layout.Node, lane, count int, direction diagram.Direction, label string) []Point {
	if node == nil {
		return nil
	}
	left, top := node.Rect.X, node.Rect.Y
	right := left + node.Rect.Width - 1
	bottom := top + node.Rect.Height - 1
	labelWidth, labelHeight := 0, 0
	if label != "" {
		lines, width := textwidth.Lines(label)
		labelWidth, labelHeight = width, len(lines)
	}
	if direction.Vertical() {
		centerY := top + (node.Rect.Height-1)/2
		loopX := right + 1 + lane
		if label != "" {
			loopX += labelWidth + 1
		}
		targetX := distributedPosition(left, node.Rect.Width, lane, count)
		return normalize([]Point{{X: right, Y: centerY}, {X: loopX, Y: centerY}, {X: loopX, Y: top}, {X: targetX, Y: top}})
	}
	centerX := left + (node.Rect.Width-1)/2
	loopY := bottom + 1 + lane
	if label != "" {
		loopY += labelHeight + 1
	}
	targetY := distributedPosition(top, node.Rect.Height, lane, count)
	return normalize([]Point{{X: centerX, Y: bottom}, {X: centerX, Y: loopY}, {X: left, Y: loopY}, {X: left, Y: targetY}})
}

func reversedPoints(result *layout.Layout, from, to *layout.Node, lane int, normalLanes map[rankKey]int) []Point {
	if from == nil || to == nil {
		return nil
	}
	if result.Direction.Vertical() {
		y := func(node *layout.Node) int { return node.Rect.Y + node.Rect.Height - 1 }
		start := Point{X: from.Rect.X + (from.Rect.Width-1)/2, Y: y(from)}
		end := Point{X: to.Rect.X + (to.Rect.Width-1)/2, Y: y(to)}
		sourceGap := y(from) + 1 + normalLanes[rankKey{component: from.Component, rank: from.Rank}] + lane
		targetGap := y(to) + 1 + normalLanes[rankKey{component: to.Component, rank: to.Rank}] + lane
		outer := result.OuterLaneStart + lane
		return normalize([]Point{start, {X: start.X, Y: sourceGap}, {X: outer, Y: sourceGap}, {X: outer, Y: targetGap}, {X: end.X, Y: targetGap}, end})
	}
	x := func(node *layout.Node) int { return node.Rect.X + node.Rect.Width - 1 }
	start := Point{X: x(from), Y: from.Rect.Y + (from.Rect.Height-1)/2}
	end := Point{X: x(to), Y: to.Rect.Y + (to.Rect.Height-1)/2}
	sourceGap := x(from) + 1 + normalLanes[rankKey{component: from.Component, rank: from.Rank}] + lane
	targetGap := x(to) + 1 + normalLanes[rankKey{component: to.Component, rank: to.Rank}] + lane
	outer := result.OuterLaneStart + lane
	return normalize([]Point{start, {X: sourceGap, Y: start.Y}, {X: sourceGap, Y: outer}, {X: targetGap, Y: outer}, {X: targetGap, Y: end.Y}, end})
}
