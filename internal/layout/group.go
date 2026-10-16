package layout

import (
	"fmt"
	"sort"

	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/geom"
	"github.com/xshoji/go-text-diagram/internal/textwidth"
)

type groupScopeUnit struct {
	groupID string
	nodes   map[*workingNode]bool
	order   int
	rect    Rect
}

// packGroupScopes moves scopes only when their group bounds collide.
func packGroupScopes(groups []diagram.Group, nodes []*workingNode, edges []*workingEdge, nodeRects map[*workingNode]Rect, vertical bool, spacing int) error {
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
			if parent != "" {
				unit := &groupScopeUnit{nodes: make(map[*workingNode]bool)}
				for id := range directIDs {
					node := workingByID[id]
					if node == nil {
						return fmt.Errorf("%w: group %q references unknown node %q", ErrInvalidGraph, parent, id)
					}
					unit.nodes[node] = true
				}
				addInternalDummies(unit, func(endpoint diagram.Endpoint) bool {
					return endpoint.Kind != diagram.EndpointGroup && directIDs[endpoint.ID()]
				})
				if err := compactScopeUnit(unit, nodeRects, vertical); err != nil {
					return err
				}
			}
			unitsByRank := make(map[int]*groupScopeUnit)
			for id := range directIDs {
				node := workingByID[id]
				if node == nil {
					return fmt.Errorf("%w: group %q references unknown node %q", ErrInvalidGraph, parent, id)
				}
				unit := unitsByRank[node.rank]
				if unit == nil {
					unit = &groupScopeUnit{nodes: make(map[*workingNode]bool), order: node.inputOrder}
					unitsByRank[node.rank] = unit
				}
				unit.nodes[node] = true
				unit.order = min(unit.order, node.inputOrder)
			}
			for _, node := range nodes {
				if !node.dummy {
					continue
				}
				edge := edgeByID[node.ownerEdgeID]
				if edge != nil && directIDs[edge.source.ID()] && directIDs[edge.target.ID()] {
					if unit := unitsByRank[edge.from.rank]; unit != nil {
						unit.nodes[node] = true
					}
				}
			}
			ranks := make([]int, 0, len(unitsByRank))
			for rank := range unitsByRank {
				ranks = append(ranks, rank)
			}
			sort.Ints(ranks)
			for _, rank := range ranks {
				unit := unitsByRank[rank]
				unit.rect = boundsOfNodeSet(unit.nodes, nodeRects)
				units = append(units, *unit)
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
		separateOverlappingScopeUnits(units, nodeRects, vertical, max(1, spacing))
		return nil
	}
	return pack("")
}

// alignIsolatedGroupMembers packs disconnected members beside their group peers.
func alignIsolatedGroupMembers(groups []diagram.Group, nodes []*workingNode, edges []*workingEdge, rects map[*workingNode]Rect, vertical bool) {
	byID := make(map[string]diagram.Group, len(groups))
	children := make(map[string][]string)
	for _, group := range groups {
		byID[group.ID] = group
		children[group.Parent] = append(children[group.Parent], group.ID)
	}
	if len(byID) == 0 {
		return
	}

	nodesByID := make(map[string]*workingNode, len(nodes))
	for _, node := range nodes {
		if !node.dummy {
			nodesByID[node.id] = node
		}
	}
	connected := make(map[string]bool)
	for _, edge := range edges {
		connected[edge.originalFrom] = true
		connected[edge.originalTo] = true
	}

	var descendants func(string, map[string]bool)
	descendants = func(groupID string, members map[string]bool) {
		group := byID[groupID]
		for _, member := range group.Members {
			members[member] = true
		}
		for _, child := range children[groupID] {
			descendants(child, members)
		}
	}

	orderedNodes := append([]*workingNode(nil), nodes...)
	sortWorkingNodes(orderedNodes)
	for _, group := range groups {
		members := make(map[string]bool)
		descendants(group.ID, members)
		var centers []int
		for id := range members {
			if !connected[id] {
				continue
			}
			node := nodesByID[id]
			if node == nil {
				continue
			}
			rect := rects[node]
			center := rect.X + rect.Width/2
			if vertical {
				center = rect.Y + rect.Height/2
			}
			centers = append(centers, center)
		}
		if len(centers) == 0 {
			continue
		}
		sort.Ints(centers)
		center := centers[len(centers)/2]
		for _, node := range orderedNodes {
			if !members[node.id] || connected[node.id] || node.dummy {
				continue
			}
			candidate := rects[node]
			if vertical {
				candidate.Y = center - candidate.Height/2
			} else {
				candidate.X = center - candidate.Width/2
			}
			blocked := false
			for _, other := range nodes {
				if other != node && geom.Overlaps(candidate, rects[other]) {
					blocked = true
					break
				}
			}
			if !blocked {
				rects[node] = candidate
			}
		}
	}
}

func separateOverlappingScopeUnits(units []groupScopeUnit, rects map[*workingNode]Rect, vertical bool, spacing int) {
	// Flow-separated scopes need one gutter, not cross-axis node spacing.
	xSpacing, ySpacing := 1, spacing
	if vertical {
		xSpacing, ySpacing = spacing, 1
	}
	for index := range units {
		unit := &units[index]
		for {
			delta := 0
			for previous := 0; previous < index; previous++ {
				if units[previous].groupID == "" && unit.groupID == "" {
					if scopeRectsOverlap(units[previous].rect, unit.rect, spacing, spacing) {
						delta = max(delta, overlappingNodeShift(units[previous], *unit, rects, vertical, spacing))
					}
					continue
				}
				if scopeRectsOverlap(units[previous].rect, unit.rect, xSpacing, ySpacing) {
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

func overlappingNodeShift(previous, current groupScopeUnit, rects map[*workingNode]Rect, vertical bool, spacing int) int {
	delta := 0
	for left := range previous.nodes {
		if left.dummy {
			continue
		}
		for right := range current.nodes {
			if right.dummy {
				continue
			}
			leftRect, rightRect := rects[left], rects[right]
			if geom.Overlaps(leftRect, rightRect) {
				delta = max(delta, crossEnd(leftRect, vertical)+spacing-crossStart(rightRect, vertical))
			}
		}
	}
	return delta
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

func scopeRectsOverlap(left, right Rect, xSpacing, ySpacing int) bool {
	return left.X < right.X+right.Width+xSpacing && right.X < left.X+left.Width+xSpacing &&
		left.Y < right.Y+right.Height+ySpacing && right.Y < left.Y+left.Height+ySpacing
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
