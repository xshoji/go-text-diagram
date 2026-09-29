package route

import (
	"sort"

	"github.com/xshoji/agents-workspace/internal/geom"
	"github.com/xshoji/agents-workspace/internal/layout"
	"github.com/xshoji/agents-workspace/internal/textwidth"
)

type labelCandidate struct {
	rect          layout.Rect
	segment       int
	segmentLength int
	positionOrder int
}

// LabelContext caches route-local candidate enumeration for one immutable
// adaptive round. Candidate ordering and collision checks remain unchanged.
type LabelContext struct {
	candidates    map[*Edge][]labelCandidate
	rows, columns map[int][]labelRouteSpan
}

type labelRouteSpan struct {
	from, to int
	edgeID   string
}

func NewLabelContext(routes []*Edge) *LabelContext {
	context := &LabelContext{candidates: make(map[*Edge][]labelCandidate, len(routes)), rows: make(map[int][]labelRouteSpan), columns: make(map[int][]labelRouteSpan)}
	for _, edge := range routes {
		if edge == nil || !visibleEdge(edge) {
			continue
		}
		if edge != nil && edge.Label != "" && visibleEdge(edge) {
			context.candidates[edge] = labelCandidates(edge.Label, edge.Points)
		}
		for index := 1; index < len(edge.Points); index++ {
			from, to := edge.Points[index-1], edge.Points[index]
			if from.Y == to.Y {
				context.rows[from.Y] = append(context.rows[from.Y], labelRouteSpan{from: min(from.X, to.X), to: max(from.X, to.X), edgeID: edge.ID})
			} else {
				context.columns[from.X] = append(context.columns[from.X], labelRouteSpan{from: min(from.Y, to.Y), to: max(from.Y, to.Y), edgeID: edge.ID})
			}
		}
	}
	return context
}

// FirstAffected returns the earliest label whose deterministic candidate set
// intersects old or replacement geometry. It is exact for PlaceLabelsFrom's
// ordered greedy placement because labels before this index see no changed
// obstacle.
func (context *LabelContext) FirstAffected(routes, replacements []*Edge) int {
	if context == nil {
		return 0
	}
	changedIDs := make(map[string]bool, len(replacements))
	changed := make([]*Edge, 0, len(replacements)*2)
	for _, replacement := range replacements {
		if replacement != nil {
			changedIDs[replacement.ID] = true
			changed = append(changed, replacement)
		}
	}
	for _, edge := range routes {
		if edge != nil && changedIDs[edge.ID] {
			changed = append(changed, edge)
		}
	}
	for index, edge := range routes {
		if edge == nil || edge.Label == "" || !visibleEdge(edge) {
			continue
		}
		if changedIDs[edge.ID] {
			return index
		}
		for _, candidate := range context.candidates[edge] {
			if rectHitsOtherRoute(candidate.rect, edge.ID, changed) {
				return index
			}
		}
	}
	return len(routes)
}

// placeLabels reserves a collision-free rectangle on the longest usable
// straight segment. Input order is the deterministic tie-break between labels.
func placeLabels(result *layout.Layout, routes []*Edge) {
	var occupied []layout.Rect
	for _, edge := range routes {
		if edge.Label == "" || !visibleEdge(edge) {
			continue
		}
		candidates := labelCandidates(edge.Label, edge.Points)
		for _, allowRouteOverlap := range []bool{false, true} {
			for _, candidate := range candidates {
				if !rectWithin(candidate.rect, result.Metrics.Width, result.Metrics.Height) ||
					rectHitsNode(candidate.rect, result.Nodes) ||
					rectHitsAny(candidate.rect, occupied) ||
					!allowRouteOverlap && rectHitsOtherRoute(candidate.rect, edge.ID, routes) {
					continue
				}
				edge.LabelRect = candidate.rect
				edge.LabelPlaced = true
				occupied = append(occupied, candidate.rect)
				break
			}
			if edge.LabelPlaced {
				break
			}
		}
	}
}

// PlaceLabelsFrom places the suffix labels against an overlay of base and
// replacements. It never mutates either input and returns only changed edges.
func PlaceLabelsFrom(result *layout.Layout, base []*Edge, replacements []*Edge, first int, diagnostics *Diagnostics) []*Edge {
	return PlaceLabelsFromContext(result, base, replacements, first, diagnostics, nil)
}

func PlaceLabelsFromContext(result *layout.Layout, base []*Edge, replacements []*Edge, first int, diagnostics *Diagnostics, context *LabelContext) []*Edge {
	view := overlayRoutes(base, replacements)
	replacementIDs := make(map[string]bool, len(replacements))
	for _, edge := range replacements {
		if edge != nil {
			replacementIDs[edge.ID] = true
		}
	}
	if first < 0 {
		first = 0
	}
	if first > len(view) {
		first = len(view)
	}
	var occupied []layout.Rect
	for _, edge := range view[:first] {
		if edge != nil && edge.LabelPlaced {
			occupied = append(occupied, edge.LabelRect)
		}
	}
	changed := make([]*Edge, 0)
	for index := first; index < len(view); index++ {
		edge := view[index]
		if edge == nil || edge.Label == "" || !visibleEdge(edge) {
			continue
		}
		clone := *edge
		clone.LabelPlaced = false
		clone.LabelRect = layout.Rect{}
		view[index] = &clone
		candidates := contextCandidates(context, edge)
		if candidates == nil {
			candidates = labelCandidates(clone.Label, clone.Points)
		}
		for _, allowRouteOverlap := range []bool{false, true} {
			for _, candidate := range candidates {
				if !rectWithin(candidate.rect, result.Metrics.Width, result.Metrics.Height) ||
					rectHitsNode(candidate.rect, result.Nodes) || rectHitsAny(candidate.rect, occupied) ||
					!allowRouteOverlap && labelRectHitsRoute(candidate.rect, clone.ID, view, replacements, replacementIDs, context) {
					continue
				}
				clone.LabelRect, clone.LabelPlaced = candidate.rect, true
				occupied = append(occupied, candidate.rect)
				break
			}
			if clone.LabelPlaced {
				break
			}
		}
		if clone.LabelPlaced != edge.LabelPlaced || clone.LabelRect != edge.LabelRect {
			changed = append(changed, &clone)
		}
	}
	if diagnostics != nil {
		diagnostics.LabelPasses++
	}
	return changed
}

func labelRectHitsRoute(rect layout.Rect, edgeID string, view, replacements []*Edge, replacementIDs map[string]bool, context *LabelContext) bool {
	if context == nil {
		return rectHitsOtherRoute(rect, edgeID, view)
	}
	for y := rect.Y; y < rect.Y+rect.Height; y++ {
		for _, span := range context.rows[y] {
			if span.edgeID != edgeID && !replacementIDs[span.edgeID] && span.to >= rect.X && span.from < rect.X+rect.Width {
				return true
			}
		}
	}
	for x := rect.X; x < rect.X+rect.Width; x++ {
		for _, span := range context.columns[x] {
			if span.edgeID != edgeID && !replacementIDs[span.edgeID] && span.to >= rect.Y && span.from < rect.Y+rect.Height {
				return true
			}
		}
	}
	return rectHitsOtherRoute(rect, edgeID, replacements)
}

func contextCandidates(context *LabelContext, edge *Edge) []labelCandidate {
	if context == nil || edge == nil {
		return nil
	}
	return context.candidates[edge]
}

func overlayRoutes(base, replacements []*Edge) []*Edge {
	byID := make(map[string]*Edge, len(replacements))
	for _, edge := range replacements {
		if edge != nil {
			byID[edge.ID] = edge
		}
	}
	view := make([]*Edge, len(base))
	for index, edge := range base {
		view[index] = edge
		if edge != nil && byID[edge.ID] != nil {
			view[index] = byID[edge.ID]
		}
	}
	return view
}

func labelCandidates(label string, points []Point) []labelCandidate {
	lines, width := textwidth.Lines(label)
	width = max(1, width)
	height := max(1, len(lines))
	var candidates []labelCandidate
	for index := 1; index < len(points); index++ {
		from, to := points[index-1], points[index]
		length := geom.Manhattan(from, to)
		centerX, centerY := (from.X+to.X)/2, (from.Y+to.Y)/2
		if from.Y == to.Y && length >= width+2 || from.X == to.X && length >= height+2 {
			for order, offset := range centeredOffsets(length + 1) {
				x, y := centerX, centerY
				if from.Y == to.Y {
					x += offset
				} else {
					y += offset
				}
				rect := layout.Rect{X: x - (width-1)/2, Y: y - (height-1)/2, Width: width, Height: height}
				if from.Y == to.Y && (rect.X <= min(from.X, to.X) || rect.X+rect.Width-1 >= max(from.X, to.X)) ||
					from.X == to.X && (rect.Y <= min(from.Y, to.Y) || rect.Y+rect.Height-1 >= max(from.Y, to.Y)) {
					continue
				}
				candidates = append(candidates, labelCandidate{
					rect:          rect,
					segment:       index - 1,
					segmentLength: length,
					positionOrder: order,
				})
			}
		}
		if length == 0 {
			continue
		}
		if from.Y == to.Y {
			for order, y := range []int{from.Y - height, from.Y + 1} {
				candidates = append(candidates, labelCandidate{
					rect:    layout.Rect{X: centerX - (width-1)/2, Y: y, Width: width, Height: height},
					segment: index - 1, segmentLength: length, positionOrder: length + 1 + order,
				})
			}
		} else {
			for order, x := range []int{from.X - width, from.X + 1} {
				candidates = append(candidates, labelCandidate{
					rect:    layout.Rect{X: x, Y: centerY - (height-1)/2, Width: width, Height: height},
					segment: index - 1, segmentLength: length, positionOrder: length + 1 + order,
				})
			}
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].segmentLength != candidates[j].segmentLength {
			return candidates[i].segmentLength > candidates[j].segmentLength
		}
		if candidates[i].segment != candidates[j].segment {
			return candidates[i].segment < candidates[j].segment
		}
		return candidates[i].positionOrder < candidates[j].positionOrder
	})
	return candidates
}

func centeredOffsets(count int) []int {
	result := make([]int, 0, count)
	result = append(result, 0)
	for distance := 1; len(result) < count; distance++ {
		result = append(result, -distance)
		if len(result) < count {
			result = append(result, distance)
		}
	}
	return result
}

func rectWithin(rect layout.Rect, width, height int) bool {
	return rect.X >= 0 && rect.Y >= 0 && rect.X+rect.Width <= width && rect.Y+rect.Height <= height
}

func rectHitsNode(rect layout.Rect, nodes []*layout.Node) bool {
	for _, node := range nodes {
		if !node.Dummy && geom.Overlaps(rect, node.Rect) {
			return true
		}
	}
	return false
}

func rectHitsAny(rect layout.Rect, occupied []layout.Rect) bool {
	for _, current := range occupied {
		if geom.Overlaps(rect, current) {
			return true
		}
	}
	return false
}

func rectHitsOtherRoute(rect layout.Rect, edgeID string, routes []*Edge) bool {
	for _, edge := range routes {
		if edge.ID == edgeID || !visibleEdge(edge) {
			continue
		}
		for index := 1; index < len(edge.Points); index++ {
			hit := false
			walkPoints(edge.Points[index-1], edge.Points[index], func(point Point) {
				if geom.Contains(rect, point) {
					hit = true
				}
			})
			if hit {
				return true
			}
		}
	}
	return false
}
