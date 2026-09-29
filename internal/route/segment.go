package route

import "github.com/xshoji/agents-workspace/internal/diagram"

type unitSegment struct {
	from Point
	to   Point
}

type segmentCountIndex map[unitSegment]int

func canonicalSegment(from, to Point) unitSegment {
	if from.X > to.X || (from.X == to.X && from.Y > to.Y) {
		from, to = to, from
	}
	return unitSegment{from: from, to: to}
}

func walkSegment(from, to Point, visit func(Point, Point)) {
	dx, dy := axisSign(to.X-from.X), axisSign(to.Y-from.Y)
	current := from
	for current != to {
		next := Point{X: current.X + dx, Y: current.Y + dy}
		visit(current, next)
		current = next
	}
}

func axisSign(value int) int {
	if value < 0 {
		return -1
	}
	if value > 0 {
		return 1
	}
	return 0
}

func walkPoints(from, to Point, visit func(Point)) {
	visit(from)
	walkSegment(from, to, func(_ Point, next Point) { visit(next) })
}

func (segments segmentCountIndex) addPoints(points []Point) {
	for index := 1; index < len(points); index++ {
		walkSegment(points[index-1], points[index], func(from, to Point) {
			segments[canonicalSegment(from, to)]++
		})
	}
}

func (segments segmentCountIndex) addEdge(edge *Edge) {
	if visibleEdge(edge) {
		segments.addPoints(edge.Points)
	}
}

func (segments segmentCountIndex) interactions(points []Point) (int, int) {
	crossings, overlaps := 0, 0
	for index := 1; index < len(points); index++ {
		walkSegment(points[index-1], points[index], func(from, to Point) {
			candidate := canonicalSegment(from, to)
			overlaps += segments[candidate]
			for _, crossing := range perpendicularUnitSegments(candidate) {
				crossings += segments[crossing]
			}
		})
	}
	return crossings, overlaps
}

func perpendicularUnitSegments(segment unitSegment) [4]unitSegment {
	if segment.from.Y == segment.to.Y {
		return [4]unitSegment{
			canonicalSegment(Point{X: segment.from.X, Y: segment.from.Y - 1}, segment.from),
			canonicalSegment(segment.from, Point{X: segment.from.X, Y: segment.from.Y + 1}),
			canonicalSegment(Point{X: segment.to.X, Y: segment.to.Y - 1}, segment.to),
			canonicalSegment(segment.to, Point{X: segment.to.X, Y: segment.to.Y + 1}),
		}
	}
	return [4]unitSegment{
		canonicalSegment(Point{X: segment.from.X - 1, Y: segment.from.Y}, segment.from),
		canonicalSegment(segment.from, Point{X: segment.from.X + 1, Y: segment.from.Y}),
		canonicalSegment(Point{X: segment.to.X - 1, Y: segment.to.Y}, segment.to),
		canonicalSegment(segment.to, Point{X: segment.to.X + 1, Y: segment.to.Y}),
	}
}

func segmentPoints(start, end Point, lane int, direction diagram.Direction) []Point {
	if direction.Vertical() {
		return []Point{start, {X: start.X, Y: lane}, {X: end.X, Y: lane}, end}
	}
	return []Point{start, {X: lane, Y: start.Y}, {X: lane, Y: end.Y}, end}
}

func normalize(points []Point) []Point {
	result := make([]Point, 0, len(points))
	for _, point := range points {
		if len(result) > 0 && point == result[len(result)-1] {
			continue
		}
		result = append(result, point)
		for len(result) >= 3 {
			a, b, c := result[len(result)-3], result[len(result)-2], result[len(result)-1]
			if (a.X == b.X && b.X == c.X) || (a.Y == b.Y && b.Y == c.Y) {
				result[len(result)-2] = c
				result = result[:len(result)-1]
				continue
			}
			break
		}
	}
	return result
}
