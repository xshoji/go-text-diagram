package layout

import (
	"fmt"
	"sort"

	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/textwidth"
)

type groupScopeUnit struct {
	groupID string
	nodes   map[*workingNode]bool
	order   int
	rect    Rect
}

// packGroupScopes separates every child group from the parent's direct node
// contents. Vertical layouts reserve disjoint bands because long top-to-bottom
// groups commonly span the same rows. Horizontal layouts move only colliding
// scopes to avoid excessive diagram height. Groups are otherwise only bounding
// boxes around their members, so without this constraint a wide group can
// accidentally enclose nodes that belong to a sibling or ancestor scope.
func packGroupScopes(groups []diagram.Group, nodes []*workingNode, edges []*workingEdge, nodeRects map[*workingNode]Rect, vertical, flowFirst bool, spacing int) error {
	if len(groups) == 0 {
		return nil
	}
	byID := make(map[string]*diagram.Group, len(groups))
	children := make(map[string][]*diagram.Group)
	directMembers := make(map[string]map[string]bool)
	owned := make(map[string]bool)
	for i := range groups {
		group := &groups[i]
		if group == nil || group.ID == "" || byID[group.ID] != nil {
			return fmt.Errorf("%w: group ID is empty or duplicated", ErrInvalidGraph)
		}
		byID[group.ID] = group
		children[group.Parent] = append(children[group.Parent], group)
		if directMembers[group.ID] == nil {
			directMembers[group.ID] = make(map[string]bool)
		}
		for _, member := range group.Members {
			directMembers[group.ID][member] = true
			owned[member] = true
		}
	}
	for _, group := range groups {
		if group.Parent != "" && byID[group.Parent] == nil {
			return fmt.Errorf("%w: group %q has unknown parent %q", ErrInvalidGraph, group.ID, group.Parent)
		}
	}

	descendantMembers := make(map[string]map[string]bool, len(groups))
	visiting := make(map[string]bool)
	var membersOf func(string) (map[string]bool, error)
	membersOf = func(id string) (map[string]bool, error) {
		if members := descendantMembers[id]; members != nil {
			return members, nil
		}
		if visiting[id] {
			return nil, fmt.Errorf("%w: group parent cycle at %q", ErrInvalidGraph, id)
		}
		visiting[id] = true
		members := make(map[string]bool)
		for member := range directMembers[id] {
			members[member] = true
		}
		for _, child := range children[id] {
			childMembers, err := membersOf(child.ID)
			if err != nil {
				return nil, err
			}
			for member := range childMembers {
				members[member] = true
			}
		}
		visiting[id] = false
		descendantMembers[id] = members
		return members, nil
	}
	for _, group := range groups {
		if _, err := membersOf(group.ID); err != nil {
			return err
		}
	}

	workingByID := make(map[string]*workingNode, len(nodes))
	for _, node := range nodes {
		workingByID[node.id] = node
	}
	edgeByID := make(map[string]*workingEdge)
	for _, edge := range edges {
		if edgeByID[edge.originalEdgeID] == nil {
			edgeByID[edge.originalEdgeID] = edge
		}
	}
	groupWithin := func(candidate, ancestor string) bool {
		for candidate != "" {
			if candidate == ancestor {
				return true
			}
			group := byID[candidate]
			if group == nil {
				return false
			}
			candidate = group.Parent
		}
		return false
	}
	endpointWithin := func(endpoint diagram.Endpoint, groupID string) bool {
		if endpoint.Kind == diagram.EndpointGroup {
			return groupWithin(endpoint.ID(), groupID)
		}
		return descendantMembers[groupID][endpoint.ID()]
	}
	addInternalDummies := func(unit *groupScopeUnit, containsEndpoint func(diagram.Endpoint) bool) {
		for _, node := range nodes {
			if !node.dummy {
				continue
			}
			edge := edgeByID[node.ownerEdgeID]
			if edge != nil && containsEndpoint(edge.source) && containsEndpoint(edge.target) {
				unit.nodes[node] = true
			}
		}
	}

	var pack func(string) error
	pack = func(parent string) error {
		for _, child := range children[parent] {
			if err := pack(child.ID); err != nil {
				return err
			}
		}
		groupRects, _, err := buildGroupRects(groups, nodeRects, workingByID)
		if err != nil {
			return err
		}
		units := make([]groupScopeUnit, 0, len(children[parent])+1)
		for _, child := range children[parent] {
			unit := groupScopeUnit{groupID: child.ID, nodes: make(map[*workingNode]bool), order: child.InputOrder, rect: groupRects[child.ID]}
			for member := range descendantMembers[child.ID] {
				unit.nodes[workingByID[member]] = true
			}
			addInternalDummies(&unit, func(endpoint diagram.Endpoint) bool { return endpointWithin(endpoint, child.ID) })
			units = append(units, unit)
		}
		directIDs := directMembers[parent]
		if parent == "" {
			directIDs = make(map[string]bool)
			for _, node := range nodes {
				if !node.dummy && !owned[node.id] {
					directIDs[node.id] = true
				}
			}
		}
		if len(directIDs) > 0 {
			unit := groupScopeUnit{nodes: make(map[*workingNode]bool), order: len(nodes) + len(groups)}
			for id := range directIDs {
				node := workingByID[id]
				if node == nil {
					return fmt.Errorf("%w: group %q references unknown node %q", ErrInvalidGraph, parent, id)
				}
				unit.nodes[node] = true
				unit.order = min(unit.order, node.inputOrder)
			}
			addInternalDummies(&unit, func(endpoint diagram.Endpoint) bool {
				return endpoint.Kind != diagram.EndpointGroup && directIDs[endpoint.ID()]
			})
			unit.rect = boundsOfNodeSet(unit.nodes, nodeRects)
			units = append(units, unit)
		}
		if parent != "" {
			for index := range units {
				// Child groups compact their own direct members recursively. Only
				// compact this group's direct-member unit here so nested padding and
				// sibling group bounds remain intact.
				if units[index].groupID == "" {
					if err := compactScopeUnit(&units[index], nodeRects, vertical); err != nil {
						return err
					}
				}
			}
		}
		if len(units) < 2 {
			return nil
		}
		sort.SliceStable(units, func(i, j int) bool {
			if units[i].order != units[j].order {
				return units[i].order < units[j].order
			}
			return units[i].groupID < units[j].groupID
		})
		if flowFirst && vertical {
			placeFlowScopeUnits(units, nodeRects, max(1, spacing))
			return nil
		}
		if !vertical {
			separateOverlappingScopeUnits(units, nodeRects, vertical, max(1, spacing))
			return nil
		}
		cursor := crossStart(units[0].rect, vertical)
		for index := range units {
			unit := &units[index]
			delta := cursor - crossStart(unit.rect, vertical)
			moveScopeUnit(unit, nodeRects, vertical, delta)
			cursor += crossSize(unit.rect, vertical) + max(1, spacing)
		}
		return nil
	}
	return pack("")
}

// compactScopeUnit collapses repeated cross-axis whitespace inside one group
// while retaining a single gutter between occupied node bands.
func compactScopeUnit(unit *groupScopeUnit, rects map[*workingNode]Rect, vertical bool) error {
	if unit == nil || len(unit.nodes) == 0 {
		return nil
	}
	var start, end int64
	set := false
	for node := range unit.nodes {
		rect := rects[node]
		coordinate, size := rect.Y, rect.Height
		if vertical {
			coordinate, size = rect.X, rect.Width
		}
		coordinate64, size64 := int64(coordinate), int64(size)
		if size64 <= 0 || coordinate64 > int64(^uint(0)>>1)-size64 {
			return fmt.Errorf("%w: invalid group compaction bounds", ErrInvalidGraph)
		}
		nodeEnd := coordinate64 + size64
		if !set {
			start, end, set = coordinate64, nodeEnd, true
		} else {
			start, end = min(start, coordinate64), max(end, nodeEnd)
		}
	}
	span := uint64(end) - uint64(start)
	if end < start || span > uint64(maximumLayoutCells) {
		return fmt.Errorf("%w: group compaction span exceeds %d cells", ErrInvalidGraph, maximumLayoutCells)
	}
	occupied := make([]bool, int(span))
	for node := range unit.nodes {
		rect := rects[node]
		coordinate, size := rect.Y, rect.Height
		if vertical {
			coordinate, size = rect.X, rect.Width
		}
		first := int(int64(coordinate) - start)
		for offset := range size {
			occupied[first+offset] = true
		}
	}
	coordinateMap := compactScopeMap(occupied)
	startPosition := int(start)
	for node := range unit.nodes {
		rect := rects[node]
		if vertical {
			rect.X = startPosition + coordinateMap[rect.X-startPosition]
		} else {
			rect.Y = startPosition + coordinateMap[rect.Y-startPosition]
		}
		rects[node] = rect
	}
	unit.rect = boundsOfNodeSet(unit.nodes, rects)
	return nil
}

func compactScopeMap(occupied []bool) []int {
	coordinateMap := make([]int, len(occupied))
	next := 0
	seen := false
	for coordinate, used := range occupied {
		coordinateMap[coordinate] = next
		gutter := !used && seen && coordinate+1 < len(occupied) && occupied[coordinate+1]
		if used || gutter {
			next++
		}
		seen = seen || used
	}
	return coordinateMap
}

func placeFlowScopeUnits(units []groupScopeUnit, rects map[*workingNode]Rect, spacing int) {
	centers := make([]int, len(units))
	for index, unit := range units {
		centers[index] = unit.rect.X + (unit.rect.Width-1)/2
	}
	sort.Ints(centers)
	targetCenter := centers[len(centers)/2]
	for index := range units {
		unit := &units[index]
		currentCenter := unit.rect.X + (unit.rect.Width-1)/2
		moveScopeUnit(unit, rects, true, targetCenter-currentCenter)
		if unit.groupID == "" {
			centerIsolatedFlowNodes(unit, rects, targetCenter)
		}
	}

	placed := make([][]Rect, len(units))
	for index := range units {
		unit := &units[index]
		current := flowScopeFootprints(unit, rects)
		selected := nearestFlowScopeOffset(placed[:index], current, spacing)
		moveScopeUnit(unit, rects, true, selected)
		for footprint := range current {
			current[footprint].X += selected
		}
		placed[index] = current
	}
}

func centerIsolatedFlowNodes(unit *groupScopeUnit, rects map[*workingNode]Rect, targetCenter int) {
	nodes := make([]*workingNode, 0, len(unit.nodes))
	for node := range unit.nodes {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool {
		left, right := rects[nodes[i]], rects[nodes[j]]
		if left.Y != right.Y {
			return left.Y < right.Y
		}
		if left.X != right.X {
			return left.X < right.X
		}
		return nodes[i].id < nodes[j].id
	})
	priorEnd := 0
	for index, node := range nodes {
		rect := rects[node]
		isolatedBefore := index == 0 || priorEnd <= rect.Y
		isolatedAfter := index+1 == len(nodes) || rect.Y+rect.Height <= rects[nodes[index+1]].Y
		if isolatedBefore && isolatedAfter {
			rect.X += targetCenter - (rect.X + (rect.Width-1)/2)
			rects[node] = rect
		}
		priorEnd = max(priorEnd, rect.Y+rect.Height)
	}
	unit.rect = boundsOfNodeSet(unit.nodes, rects)
}

func flowScopeFootprints(unit *groupScopeUnit, rects map[*workingNode]Rect) []Rect {
	if unit.groupID != "" {
		return []Rect{unit.rect}
	}
	result := make([]Rect, 0, len(unit.nodes))
	for node := range unit.nodes {
		result = append(result, rects[node])
	}
	return result
}

type flowForbiddenInterval struct {
	start int
	end   int
}

func nearestFlowScopeOffset(placed [][]Rect, current []Rect, spacing int) int {
	var forbidden []flowForbiddenInterval
	for _, unit := range placed {
		for _, other := range unit {
			for _, footprint := range current {
				if !flowRangesOverlap(other.Y, other.Height, footprint.Y, footprint.Height) {
					continue
				}
				forbidden = append(forbidden, flowForbiddenInterval{
					start: other.X - spacing - (footprint.X + footprint.Width) + 1,
					end:   other.X + other.Width + spacing - footprint.X - 1,
				})
			}
		}
	}
	if len(forbidden) == 0 {
		return 0
	}
	sort.Slice(forbidden, func(i, j int) bool {
		if forbidden[i].start != forbidden[j].start {
			return forbidden[i].start < forbidden[j].start
		}
		return forbidden[i].end < forbidden[j].end
	})
	merged := forbidden[:1]
	for _, interval := range forbidden[1:] {
		last := &merged[len(merged)-1]
		if interval.start <= last.end+1 {
			last.end = max(last.end, interval.end)
			continue
		}
		merged = append(merged, interval)
	}
	for _, interval := range merged {
		if interval.start <= 0 && interval.end >= 0 {
			left, right := interval.start-1, interval.end+1
			if absInt(left) < absInt(right) {
				return left
			}
			return right
		}
	}
	return 0
}

func flowRangesOverlap(leftStart, leftSize, rightStart, rightSize int) bool {
	return leftStart < rightStart+rightSize && rightStart < leftStart+leftSize
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func separateOverlappingScopeUnits(units []groupScopeUnit, rects map[*workingNode]Rect, vertical bool, spacing int) {
	for index := range units {
		unit := &units[index]
		for {
			delta := 0
			for previous := 0; previous < index; previous++ {
				if scopeRectsOverlap(units[previous].rect, unit.rect, spacing) {
					delta = max(delta, crossEnd(units[previous].rect, vertical)+spacing-crossStart(unit.rect, vertical))
				}
			}
			if delta <= 0 {
				break
			}
			moveScopeUnit(unit, rects, vertical, delta)
		}
	}
}

func moveScopeUnit(unit *groupScopeUnit, rects map[*workingNode]Rect, vertical bool, delta int) {
	for node := range unit.nodes {
		rect := rects[node]
		if vertical {
			rect.X += delta
		} else {
			rect.Y += delta
		}
		rects[node] = rect
	}
	if vertical {
		unit.rect.X += delta
	} else {
		unit.rect.Y += delta
	}
}

func boundsOfNodeSet(nodes map[*workingNode]bool, rects map[*workingNode]Rect) Rect {
	var bounds Rect
	set := false
	for node := range nodes {
		rect := rects[node]
		if !set {
			bounds, set = rect, true
			continue
		}
		minX, minY := min(bounds.X, rect.X), min(bounds.Y, rect.Y)
		maxX, maxY := max(bounds.X+bounds.Width, rect.X+rect.Width), max(bounds.Y+bounds.Height, rect.Y+rect.Height)
		bounds = Rect{X: minX, Y: minY, Width: maxX - minX, Height: maxY - minY}
	}
	return bounds
}

func crossStart(rect Rect, vertical bool) int {
	if vertical {
		return rect.X
	}
	return rect.Y
}

func crossSize(rect Rect, vertical bool) int {
	if vertical {
		return rect.Width
	}
	return rect.Height
}

func crossEnd(rect Rect, vertical bool) int {
	return crossStart(rect, vertical) + crossSize(rect, vertical)
}

func scopeRectsOverlap(left, right Rect, spacing int) bool {
	return left.X < right.X+right.Width+spacing && right.X < left.X+left.Width+spacing &&
		left.Y < right.Y+right.Height+spacing && right.Y < left.Y+left.Height+spacing
}

func buildGroupRects(groups []diagram.Group, nodeRects map[*workingNode]Rect, nodes map[string]*workingNode) (map[string]Rect, int, error) {
	result := make(map[string]Rect, len(groups))
	byID := make(map[string]*diagram.Group, len(groups))
	children := make(map[string][]string)
	for i := range groups {
		group := &groups[i]
		if group == nil || group.ID == "" || byID[group.ID] != nil {
			return nil, 0, fmt.Errorf("%w: group ID is empty or duplicated", ErrInvalidGraph)
		}
		byID[group.ID] = group
		if group.Parent != "" {
			children[group.Parent] = append(children[group.Parent], group.ID)
		}
	}
	for _, group := range groups {
		if group.Parent != "" && byID[group.Parent] == nil {
			return nil, 0, fmt.Errorf("%w: group %q has unknown parent %q", ErrInvalidGraph, group.ID, group.Parent)
		}
	}
	visiting := make(map[string]bool)
	var visit func(string) (Rect, error)
	visit = func(id string) (Rect, error) {
		if rect, ok := result[id]; ok {
			return rect, nil
		}
		if visiting[id] {
			return Rect{}, fmt.Errorf("%w: group parent cycle at %q", ErrInvalidGraph, id)
		}
		visiting[id] = true
		group := byID[id]
		set, minX, minY, maxX, maxY := false, 0, 0, 0, 0
		include := func(rect Rect) {
			if !set {
				minX, minY, maxX, maxY, set = rect.X, rect.Y, rect.X+rect.Width, rect.Y+rect.Height, true
				return
			}
			minX, minY = min(minX, rect.X), min(minY, rect.Y)
			maxX, maxY = max(maxX, rect.X+rect.Width), max(maxY, rect.Y+rect.Height)
		}
		for _, member := range group.Members {
			node := nodes[member]
			if node == nil {
				return Rect{}, fmt.Errorf("%w: group %q references unknown node %q", ErrInvalidGraph, id, member)
			}
			include(nodeRects[node])
		}
		for _, child := range children[id] {
			rect, err := visit(child)
			if err != nil {
				return Rect{}, err
			}
			include(rect)
		}
		if !set {
			return Rect{}, fmt.Errorf("%w: group %q has no content", ErrInvalidGraph, id)
		}
		padding := max(1, group.Padding)
		labelWidth := textwidth.String(group.Label)
		result[id] = Rect{
			X: minX - padding, Y: minY - padding,
			Width: max(maxX-minX+padding*2, labelWidth+4), Height: maxY - minY + padding*2,
		}
		visiting[id] = false
		return result[id], nil
	}
	shift := 0
	for _, group := range groups {
		rect, err := visit(group.ID)
		if err != nil {
			return nil, 0, err
		}
		shift = max(shift, max(-rect.X, -rect.Y))
	}
	return result, shift, nil
}
