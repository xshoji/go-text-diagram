package route

import (
	"container/heap"

	"github.com/xshoji/agents-workspace/internal/diagram"
	"github.com/xshoji/agents-workspace/internal/geom"
	"github.com/xshoji/agents-workspace/internal/layout"
)

const maximumPathSearchExpansions = 100_000

type moveDirection uint8

const (
	moveNone moveDirection = iota
	moveNorth
	moveEast
	moveSouth
	moveWest
)

type routeState struct {
	point     Point
	direction moveDirection
}

type queueItem struct {
	state    routeState
	cost     int
	priority int
	sequence int
	index    int
}

type priorityQueue []*queueItem

func (q priorityQueue) Len() int { return len(q) }
func (q priorityQueue) Less(i, j int) bool {
	if q[i].priority != q[j].priority {
		return q[i].priority < q[j].priority
	}
	return q[i].sequence < q[j].sequence
}
func (q priorityQueue) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].index, q[j].index = i, j
}
func (q *priorityQueue) Push(value any) {
	item := value.(*queueItem)
	item.index = len(*q)
	*q = append(*q, item)
}
func (q *priorityQueue) Pop() any {
	old := *q
	item := old[len(old)-1]
	*q = old[:len(old)-1]
	return item
}

type routeOccupancy struct {
	segments map[unitSegment]bool
	points   map[Point]uint8
}

type pathSearchResult struct {
	points         []Point
	gridExpansions int
	found          bool
}

type searchGrid struct {
	width             int
	height            int
	direction         diagram.Direction
	start             Point
	goal              Point
	blocked           map[Point]bool
	obstacles         []geom.Rect
	blockedSegments   map[unitSegment]bool
	occupied          routeOccupancy
	maximumExpansions int
	expansions        *int
}

func findPath(result *layout.Layout, edge *Edge, accepted []*Edge) pathSearchResult {
	if result == nil || edge == nil || len(edge.Points) < 2 || result.Metrics.Width <= 0 || result.Metrics.Height <= 0 {
		return pathSearchResult{}
	}
	start := edge.Points[0]
	goal := edge.Points[len(edge.Points)-1]
	return searchOrthogonalPath(searchGrid{
		width:             result.Metrics.Width,
		height:            result.Metrics.Height,
		direction:         result.Direction,
		start:             start,
		goal:              goal,
		obstacles:         obstacleRects(result.Nodes),
		occupied:          buildOccupancy(accepted),
		maximumExpansions: pathSearchBudget(result.Metrics.Width, result.Metrics.Height),
	})
}

func pathSearchBudget(width, height int) int {
	return max(64, min(maximumPathSearchExpansions, cappedExpansionBudget(int64(width)*int64(height)*4)))
}

func searchOrthogonalPath(grid searchGrid) pathSearchResult {
	if grid.blocked == nil {
		grid.blocked = blockedRectCells(grid.obstacles, grid.start, grid.goal)
	}
	expansions := 0
	grid.expansions = &expansions
	points, found := searchGridPath(grid)
	return pathSearchResult{points: points, gridExpansions: expansions, found: found}
}

func searchGridPath(grid searchGrid) ([]Point, bool) {
	startState := routeState{point: grid.start, direction: moveNone}
	costs := map[routeState]int{startState: 0}
	previous := make(map[routeState]routeState)
	queue := &priorityQueue{}
	heap.Init(queue)
	sequence := 0
	heap.Push(queue, &queueItem{state: startState, priority: geom.Manhattan(grid.start, grid.goal), sequence: sequence})
	expansions := 0
	recordExpansions := func() {
		if grid.expansions != nil {
			*grid.expansions = expansions
		}
	}

	for queue.Len() > 0 && expansions < grid.maximumExpansions {
		current := heap.Pop(queue).(*queueItem)
		bestCost, exists := costs[current.state]
		if !exists || current.cost != bestCost {
			continue
		}
		expansions++
		if current.state.point == grid.goal {
			recordExpansions()
			return reconstructPath(current.state, previous), true
		}
		for _, direction := range preferredDirections(grid.direction) {
			nextPoint := move(current.state.point, direction)
			if nextPoint.X < 0 || nextPoint.X >= grid.width || nextPoint.Y < 0 || nextPoint.Y >= grid.height {
				continue
			}
			if grid.blocked[nextPoint] && nextPoint != grid.goal {
				continue
			}
			segment := canonicalSegment(current.state.point, nextPoint)
			if grid.blockedSegments[segment] {
				continue
			}
			next := routeState{point: nextPoint, direction: direction}
			stepCost := 1
			if current.state.direction != moveNone && current.state.direction != direction {
				stepCost += 5
			}
			if grid.occupied.segments[segment] {
				stepCost += 30
			}
			if crossesDirection(grid.occupied.points[nextPoint], direction) {
				stepCost += 20
			}
			if reverseDirection(direction, grid.direction) {
				stepCost += 8
			}
			candidateCost := current.cost + stepCost
			if knownCost, ok := costs[next]; ok && knownCost <= candidateCost {
				continue
			}
			costs[next] = candidateCost
			previous[next] = current.state
			sequence++
			heap.Push(queue, &queueItem{
				state:    next,
				cost:     candidateCost,
				priority: candidateCost + geom.Manhattan(nextPoint, grid.goal),
				sequence: sequence,
			})
		}
	}
	recordExpansions()
	return nil, false
}

func blockedRectCells(rects []geom.Rect, start, goal Point) map[Point]bool {
	blocked := make(map[Point]bool)
	for _, rect := range rects {
		for y := rect.Y; y < rect.Y+rect.Height; y++ {
			for x := rect.X; x < rect.X+rect.Width; x++ {
				point := Point{X: x, Y: y}
				if point != start && point != goal {
					blocked[point] = true
				}
			}
		}
	}
	return blocked
}

func obstacleRects(nodes []*layout.Node) []geom.Rect {
	result := make([]geom.Rect, 0, len(nodes))
	for _, node := range nodes {
		if !node.Dummy {
			result = append(result, node.Rect)
		}
	}
	return result
}

func buildOccupancy(routes []*Edge) routeOccupancy {
	result := routeOccupancy{segments: make(map[unitSegment]bool), points: make(map[Point]uint8)}
	for _, edge := range routes {
		addOccupancy(&result, edge)
	}
	return result
}

func resetOccupancy(result *routeOccupancy, routes []*Edge) routeOccupancy {
	if result == nil {
		return buildOccupancy(routes)
	}
	if result.segments == nil {
		result.segments = make(map[unitSegment]bool)
	} else {
		clear(result.segments)
	}
	if result.points == nil {
		result.points = make(map[Point]uint8)
	} else {
		clear(result.points)
	}
	for _, edge := range routes {
		addOccupancy(result, edge)
	}
	return *result
}

func addOccupancy(result *routeOccupancy, edge *Edge) {
	if result == nil || !visibleEdge(edge) {
		return
	}
	for index := 1; index < len(edge.Points); index++ {
		from, to := edge.Points[index-1], edge.Points[index]
		walkSegment(from, to, func(current, next Point) {
			result.segments[canonicalSegment(current, next)] = true
			bit := uint8(1)
			if current.X == next.X {
				bit = 2
			}
			result.points[current] |= bit
			result.points[next] |= bit
		})
	}
}

func preferredDirections(direction diagram.Direction) []moveDirection {
	if direction == diagram.DirectionRight {
		return []moveDirection{moveEast, moveSouth, moveNorth, moveWest}
	}
	if direction == diagram.DirectionLeft {
		return []moveDirection{moveWest, moveSouth, moveNorth, moveEast}
	}
	if direction == diagram.DirectionUp {
		return []moveDirection{moveNorth, moveEast, moveWest, moveSouth}
	}
	return []moveDirection{moveSouth, moveEast, moveWest, moveNorth}
}

func move(point Point, direction moveDirection) Point {
	switch direction {
	case moveNorth:
		point.Y--
	case moveEast:
		point.X++
	case moveSouth:
		point.Y++
	case moveWest:
		point.X--
	}
	return point
}

func reverseDirection(direction moveDirection, layoutDirection diagram.Direction) bool {
	if layoutDirection == diagram.DirectionRight {
		return direction == moveWest
	}
	if layoutDirection == diagram.DirectionLeft {
		return direction == moveEast
	}
	if layoutDirection == diagram.DirectionUp {
		return direction == moveSouth
	}
	return direction == moveNorth
}

func crossesDirection(occupied uint8, direction moveDirection) bool {
	if direction == moveEast || direction == moveWest {
		return occupied&2 != 0
	}
	return occupied&1 != 0
}

func reconstructPath(goal routeState, previous map[routeState]routeState) []Point {
	points := []Point{goal.point}
	current := goal
	for {
		prior, exists := previous[current]
		if !exists {
			break
		}
		points = append(points, prior.point)
		current = prior
	}
	for left, right := 0, len(points)-1; left < right; left, right = left+1, right-1 {
		points[left], points[right] = points[right], points[left]
	}
	return normalize(points)
}
