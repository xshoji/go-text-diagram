package route

import (
	"sort"

	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/layout"
)

func portFor(node *layout.Node, specification diagram.PortHint, incoming bool, direction diagram.Direction, index, count int) Point {
	centerX := node.Rect.X + (node.Rect.Width-1)/2
	centerY := node.Rect.Y + (node.Rect.Height-1)/2
	if node.Dummy {
		return Point{X: centerX, Y: centerY}
	}
	if specification.Side != diagram.SideAuto {
		specification.Side = resolvedSide(specification.Side, incoming, direction)
		return specifiedPort(node, specification)
	}
	if direction.Vertical() {
		centerX = distributedPosition(node.Rect.X, node.Rect.Width, index, count)
		if incoming == (direction == diagram.DirectionDown) {
			return Point{X: centerX, Y: node.Rect.Y}
		}
		return Point{X: centerX, Y: node.Rect.Y + node.Rect.Height - 1}
	}
	centerY = distributedPosition(node.Rect.Y, node.Rect.Height, index, count)
	if incoming == (direction == diagram.DirectionRight) {
		return Point{X: node.Rect.X, Y: centerY}
	}
	return Point{X: node.Rect.X + node.Rect.Width - 1, Y: centerY}
}

func specifiedPort(node *layout.Node, specification diagram.PortHint) Point {
	position := func(start, size int) int {
		if specification.Offset == nil {
			return start + (size-1)/2
		}
		offset := *specification.Offset
		if offset < 0 {
			offset = size + offset
		}
		return start + max(0, min(size-1, offset))
	}
	switch specification.Side {
	case diagram.SideNorth:
		return Point{X: position(node.Rect.X, node.Rect.Width), Y: node.Rect.Y}
	case diagram.SideEast:
		return Point{X: node.Rect.X + node.Rect.Width - 1, Y: position(node.Rect.Y, node.Rect.Height)}
	case diagram.SideSouth:
		return Point{X: position(node.Rect.X, node.Rect.Width), Y: node.Rect.Y + node.Rect.Height - 1}
	case diagram.SideWest:
		return Point{X: node.Rect.X, Y: position(node.Rect.Y, node.Rect.Height)}
	default:
		return Point{X: node.Rect.X + (node.Rect.Width-1)/2, Y: node.Rect.Y + (node.Rect.Height-1)/2}
	}
}

func distributedPosition(start, size, index, count int) int {
	available := size - 2
	if available <= 1 || count <= 1 {
		return start + (size-1)/2
	}
	if count <= available {
		return start + 1 + index*(available-1)/(count-1)
	}
	return start + 1 + index%available
}

func assignPorts(edges []*layout.Edge, nodes map[string]*layout.Node, direction diagram.Direction) (map[*layout.Edge]Point, map[*layout.Edge]Point) {
	outgoing := make(map[string][]*layout.Edge)
	incoming := make(map[string][]*layout.Edge)
	for _, edge := range edges {
		outgoing[edge.From] = append(outgoing[edge.From], edge)
		incoming[edge.To] = append(incoming[edge.To], edge)
	}
	starts := make(map[*layout.Edge]Point, len(edges))
	ends := make(map[*layout.Edge]Point, len(edges))
	for nodeID, group := range outgoing {
		sort.SliceStable(group, func(i, j int) bool {
			left, right := nodes[group[i].To], nodes[group[j].To]
			leftPosition, rightPosition := crossCenter(left, direction), crossCenter(right, direction)
			if leftPosition != rightPosition {
				return leftPosition < rightPosition
			}
			return group[i].ID < group[j].ID
		})
		for index, edge := range group {
			starts[edge] = endpointPort(nodes[nodeID], edge.Source, edge.Start, false, direction, index, len(group))
		}
	}
	for nodeID, group := range incoming {
		sort.SliceStable(group, func(i, j int) bool {
			left, right := nodes[group[i].From], nodes[group[j].From]
			leftPosition, rightPosition := crossCenter(left, direction), crossCenter(right, direction)
			if leftPosition != rightPosition {
				return leftPosition < rightPosition
			}
			return group[i].ID < group[j].ID
		})
		for index, edge := range group {
			ends[edge] = endpointPort(nodes[nodeID], edge.Target, edge.End, true, direction, index, len(group))
		}
	}
	return starts, ends
}

func endpointPort(node *layout.Node, endpoint diagram.Endpoint, specification diagram.PortHint, incoming bool, direction diagram.Direction, index, count int) Point {
	cell, ok := node.CellRect(endpoint.Cell)
	if !ok {
		return portFor(node, specification, incoming, direction, index, count)
	}
	preferred := resolvedSide(specification.Side, incoming, direction)
	side := closestExposedCellSide(node.Rect, cell, preferred)
	x, y := cell.X+(cell.Width-1)/2, cell.Y
	switch side {
	case diagram.SideNorth:
		return Point{X: x, Y: node.Rect.Y}
	case diagram.SideEast:
		return Point{X: node.Rect.X + node.Rect.Width - 1, Y: y}
	case diagram.SideSouth:
		return Point{X: x, Y: node.Rect.Y + node.Rect.Height - 1}
	default:
		return Point{X: node.Rect.X, Y: y}
	}
}

func closestExposedCellSide(node, cell layout.Rect, preferred diagram.Side) diagram.Side {
	exposed := map[diagram.Side]bool{
		diagram.SideNorth: cell.Y == node.Y+1,
		diagram.SideEast:  cell.X+cell.Width == node.X+node.Width-1,
		diagram.SideSouth: cell.Y+cell.Height == node.Y+node.Height-1,
		diagram.SideWest:  cell.X == node.X+1,
	}
	if exposed[preferred] {
		return preferred
	}
	best, bestDistance := diagram.SideNorth, 5
	for _, candidate := range []diagram.Side{diagram.SideNorth, diagram.SideEast, diagram.SideSouth, diagram.SideWest} {
		if !exposed[candidate] {
			continue
		}
		distance := sideDistance(preferred, candidate)
		if distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	if bestDistance < 5 {
		return best
	}
	return preferred
}

func sideDistance(left, right diagram.Side) int {
	distance := abs(int(left) - int(right))
	return min(distance, 4-distance)
}

func crossCenter(node *layout.Node, direction diagram.Direction) int {
	if !direction.Vertical() {
		return node.Rect.Y + (node.Rect.Height-1)/2
	}
	return node.Rect.X + (node.Rect.Width-1)/2
}

func resolvedSide(side diagram.Side, incoming bool, direction diagram.Direction) diagram.Side {
	if side >= diagram.SideNorth && side <= diagram.SideWest {
		return side
	}
	if side != diagram.SideAuto {
		front := diagram.SideSouth
		switch direction {
		case diagram.DirectionUp:
			front = diagram.SideNorth
		case diagram.DirectionRight:
			front = diagram.SideEast
		case diagram.DirectionLeft:
			front = diagram.SideWest
		}
		if side == diagram.SideFront {
			return front
		}
		if side == diagram.SideBack {
			return oppositeSide(front)
		}
		if side == diagram.SideLeft {
			return turnLeft(front)
		}
		return oppositeSide(turnLeft(front))
	}
	switch direction {
	case diagram.DirectionDown:
		if incoming {
			return diagram.SideNorth
		}
		return diagram.SideSouth
	case diagram.DirectionUp:
		if incoming {
			return diagram.SideSouth
		}
		return diagram.SideNorth
	case diagram.DirectionRight:
		if incoming {
			return diagram.SideWest
		}
		return diagram.SideEast
	default:
		if incoming {
			return diagram.SideEast
		}
		return diagram.SideWest
	}
}

func oppositeSide(side diagram.Side) diagram.Side {
	switch side {
	case diagram.SideNorth:
		return diagram.SideSouth
	case diagram.SideEast:
		return diagram.SideWest
	case diagram.SideSouth:
		return diagram.SideNorth
	default:
		return diagram.SideEast
	}
}

func turnLeft(side diagram.Side) diagram.Side {
	switch side {
	case diagram.SideNorth:
		return diagram.SideWest
	case diagram.SideEast:
		return diagram.SideNorth
	case diagram.SideSouth:
		return diagram.SideEast
	default:
		return diagram.SideSouth
	}
}

func firstNonAutoStart(edges []*layout.Edge, direction diagram.Direction) diagram.Side {
	for _, edge := range edges {
		if edge.Start.Side != diagram.SideAuto {
			return resolvedSide(edge.Start.Side, false, direction)
		}
	}
	return resolvedSide(diagram.SideAuto, false, direction)
}

func firstNonAutoEnd(edges []*layout.Edge, direction diagram.Direction) diagram.Side {
	for index := len(edges) - 1; index >= 0; index-- {
		if edges[index].End.Side != diagram.SideAuto {
			return resolvedSide(edges[index].End.Side, true, direction)
		}
	}
	return resolvedSide(diagram.SideAuto, true, direction)
}
