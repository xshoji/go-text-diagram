package layout

import (
	"sort"

	"github.com/xshoji/go-text-diagram/internal/diagram"
)

type rankCompactionScore struct {
	edgeSlack int
	groupSpan int
}

// compactGroupRanks uses rank slack to shorten long edges and package spans
// before dummy nodes make either cost expensive. Every accepted move preserves
// explicit rank constraints and edge minimum lengths.
func compactGroupRanks(groups []diagram.Group, nodes []*workingNode, edges []*workingEdge, maximumSteps int) {
	if len(groups) == 0 || len(nodes) == 0 {
		return
	}
	grouped := make(map[string]bool)
	for _, group := range groups {
		for _, member := range group.Members {
			grouped[member] = true
		}
	}
	sameMembers, sameGrouped := make(map[string]int), make(map[string]int)
	for _, node := range nodes {
		if name := node.rankConstraint.Same; name != "" {
			sameMembers[name]++
			if grouped[node.id] {
				sameGrouped[name]++
			}
		}
	}

	unitsByKey := make(map[string][]*workingNode)
	for _, node := range nodes {
		if !grouped[node.id] {
			continue
		}
		key := "node:" + node.id
		if node.rankConstraint.Same != "" {
			if sameGrouped[node.rankConstraint.Same] != sameMembers[node.rankConstraint.Same] {
				continue
			}
			key = "same:" + node.rankConstraint.Same
		}
		unitsByKey[key] = append(unitsByKey[key], node)
	}
	units := make([][]*workingNode, 0, len(unitsByKey))
	for _, unit := range unitsByKey {
		sortWorkingNodes(unit)
		units = append(units, unit)
	}
	sort.SliceStable(units, func(i, j int) bool { return workingNodeLess(units[i][0], units[j][0]) })

	score := measureRankCompaction(groups, nodes, edges)
	limit := min(maximumSteps, max(1, len(nodes)*len(nodes)))
	for iteration := 0; iteration < limit; iteration++ {
		changed := false
		for _, unit := range units {
			if fixedRankUnit(unit) {
				continue
			}
			best, bestDelta := score, 0
			for _, delta := range []int{-1, 1} {
				if !canMoveRankUnit(unit, edges, delta) {
					continue
				}
				moveRankUnit(unit, delta)
				candidate := measureRankCompaction(groups, nodes, edges)
				moveRankUnit(unit, -delta)
				if rankCompactionLess(candidate, best) {
					best, bestDelta = candidate, delta
				}
			}
			if bestDelta != 0 {
				moveRankUnit(unit, bestDelta)
				score = best
				changed = true
			}
		}
		if !changed {
			return
		}
	}
}

func fixedRankUnit(unit []*workingNode) bool {
	for _, node := range unit {
		if node.rankConstraint.Root || node.rankConstraint.Fixed != nil {
			return true
		}
	}
	return false
}

func canMoveRankUnit(unit []*workingNode, edges []*workingEdge, delta int) bool {
	moving := make(map[*workingNode]bool, len(unit))
	for _, node := range unit {
		if node.rank+delta < 0 {
			return false
		}
		moving[node] = true
	}
	for _, edge := range edges {
		from, to := edge.from.rank, edge.to.rank
		if moving[edge.from] {
			from += delta
		}
		if moving[edge.to] {
			to += delta
		}
		if to-from < max(1, edge.minLength) {
			return false
		}
	}
	return true
}

func moveRankUnit(unit []*workingNode, delta int) {
	for _, node := range unit {
		node.rank += delta
	}
}

func measureRankCompaction(groups []diagram.Group, nodes []*workingNode, edges []*workingEdge) rankCompactionScore {
	score := rankCompactionScore{}
	for _, edge := range edges {
		score.edgeSlack += edge.to.rank - edge.from.rank - max(1, edge.minLength)
	}
	byID := make(map[string]*workingNode, len(nodes))
	for _, node := range nodes {
		byID[node.id] = node
	}
	for _, group := range groups {
		minimum, maximum, set := 0, 0, false
		for _, member := range group.Members {
			node := byID[member]
			if node == nil {
				continue
			}
			if !set {
				minimum, maximum, set = node.rank, node.rank, true
			} else {
				minimum, maximum = min(minimum, node.rank), max(maximum, node.rank)
			}
		}
		if set {
			score.groupSpan += maximum - minimum
		}
	}
	return score
}

func rankCompactionLess(left, right rankCompactionScore) bool {
	return left.edgeSlack < right.edgeSlack ||
		left.edgeSlack == right.edgeSlack && left.groupSpan < right.groupSpan
}
