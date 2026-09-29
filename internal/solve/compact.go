package solve

import (
	"github.com/xshoji/agents-workspace/internal/layout"
	"github.com/xshoji/agents-workspace/internal/route"
	"github.com/xshoji/agents-workspace/internal/textwidth"
)

// compact removes globally redundant rows and columns from a fully routed
// result. Straight lines and group interiors may shrink because repeating the
// same state does not add information. Nodes, routes, groups, and labels share
// the same coordinate maps, preserving containment and connectivity.
func compact(result *layout.Layout, routes []*route.Edge) bool {
	if result == nil || result.Metrics.Width <= 0 || result.Metrics.Height <= 0 {
		return false
	}
	occupiedX := make([]bool, result.Metrics.Width)
	occupiedY := make([]bool, result.Metrics.Height)
	occupyRect := func(rect layout.Rect) {
		for x := max(0, rect.X); x < min(len(occupiedX), rect.X+rect.Width); x++ {
			occupiedX[x] = true
		}
		for y := max(0, rect.Y); y < min(len(occupiedY), rect.Y+rect.Height); y++ {
			occupiedY[y] = true
		}
	}
	for _, node := range result.Nodes {
		if !node.Dummy {
			occupyRect(node.Rect)
		}
	}
	for _, group := range result.Groups {
		left, top := group.Rect.X, group.Rect.Y
		right, bottom := left+group.Rect.Width-1, top+group.Rect.Height-1
		occupyPoint(occupiedX, left)
		occupyPoint(occupiedX, right)
		occupyPoint(occupiedY, top)
		occupyPoint(occupiedY, bottom)
		// Keep enough of the top border for the rendered " label " and both
		// corners. The rest of a group interior is safely compressible.
		for x := left; x <= min(right, left+textwidth.String(group.Label)+3); x++ {
			occupyPoint(occupiedX, x)
		}
	}
	for _, edge := range routes {
		for _, point := range edge.Points {
			occupyPoint(occupiedX, point.X)
			occupyPoint(occupiedY, point.Y)
		}
		for index := 1; index < len(edge.Points); index++ {
			from, to := edge.Points[index-1], edge.Points[index]
			if from.X == to.X {
				occupyPoint(occupiedX, from.X)
				for y := max(0, min(from.Y, to.Y)); y <= min(len(occupiedY)-1, max(from.Y, to.Y)); y++ {
					if !insideGroupInterior(result.Groups, route.Point{X: from.X, Y: y}) {
						occupiedY[y] = true
					}
				}
			} else if from.Y == to.Y {
				occupyPoint(occupiedY, from.Y)
				for x := max(0, min(from.X, to.X)); x <= min(len(occupiedX)-1, max(from.X, to.X)); x++ {
					if !insideGroupInterior(result.Groups, route.Point{X: x, Y: from.Y}) {
						occupiedX[x] = true
					}
				}
			}
		}
		if edge.LabelPlaced {
			occupyRect(edge.LabelRect)
		}
	}
	xMap, width := compactMap(occupiedX)
	yMap, height := compactMap(occupiedY)
	if width == result.Metrics.Width && height == result.Metrics.Height {
		return false
	}
	mapRect := func(rect layout.Rect) layout.Rect {
		if rect.Width <= 0 || rect.Height <= 0 {
			return rect
		}
		x1, x2 := mapped(xMap, rect.X), mapped(xMap, rect.X+rect.Width-1)
		y1, y2 := mapped(yMap, rect.Y), mapped(yMap, rect.Y+rect.Height-1)
		return layout.Rect{X: x1, Y: y1, Width: x2 - x1 + 1, Height: y2 - y1 + 1}
	}
	for _, node := range result.Nodes {
		node.Rect = mapRect(node.Rect)
	}
	for _, group := range result.Groups {
		group.Rect = mapRect(group.Rect)
	}
	for _, edge := range routes {
		for index, point := range edge.Points {
			edge.Points[index].X = mapped(xMap, point.X)
			edge.Points[index].Y = mapped(yMap, point.Y)
		}
		edge.Points = normalizePoints(edge.Points)
		if edge.LabelPlaced {
			edge.LabelRect = mapRect(edge.LabelRect)
		}
	}
	result.Metrics.Width, result.Metrics.Height = width, height
	if result.OuterLaneStart > 0 {
		if result.Direction.Vertical() {
			result.OuterLaneStart = mapped(xMap, result.OuterLaneStart)
		} else {
			result.OuterLaneStart = mapped(yMap, result.OuterLaneStart)
		}
	}
	return true
}

func occupyPoint(occupied []bool, coordinate int) {
	if coordinate >= 0 && coordinate < len(occupied) {
		occupied[coordinate] = true
	}
}

func insideGroupInterior(groups []*layout.Group, point route.Point) bool {
	for _, group := range groups {
		rect := group.Rect
		if point.X > rect.X && point.X < rect.X+rect.Width-1 &&
			point.Y > rect.Y && point.Y < rect.Y+rect.Height-1 {
			return true
		}
	}
	return false
}

func normalizePoints(points []route.Point) []route.Point {
	result := make([]route.Point, 0, len(points))
	for _, point := range points {
		if len(result) > 0 && result[len(result)-1] == point {
			continue
		}
		result = append(result, point)
		for len(result) >= 3 {
			last := len(result) - 1
			if result[last-2].X != result[last-1].X || result[last-1].X != result[last].X {
				if result[last-2].Y != result[last-1].Y || result[last-1].Y != result[last].Y {
					break
				}
			}
			result[last-1] = result[last]
			result = result[:last]
		}
	}
	return result
}

func compactMap(occupied []bool) ([]int, int) {
	coordinateMap := make([]int, len(occupied))
	usedAfter := make([]bool, len(occupied))
	seen := false
	for coordinate := len(occupied) - 1; coordinate >= 0; coordinate-- {
		usedAfter[coordinate] = seen
		seen = seen || occupied[coordinate]
	}
	next := 0
	seen = false
	for coordinate, used := range occupied {
		coordinateMap[coordinate] = next
		// Keep one gutter cell between separate occupied bands. Removing all
		// whitespace would make unrelated box borders visually merge.
		gutter := !used && seen && usedAfter[coordinate] && coordinate > 0 && occupied[coordinate-1]
		if used || gutter {
			next++
		}
		seen = seen || used
	}
	return coordinateMap, next
}

func mapped(coordinateMap []int, coordinate int) int {
	if len(coordinateMap) == 0 || coordinate <= 0 {
		return 0
	}
	if coordinate >= len(coordinateMap) {
		return coordinateMap[len(coordinateMap)-1]
	}
	return coordinateMap[coordinate]
}
