package solution

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/xshoji/agents-workspace/internal/diagram"
	"github.com/xshoji/agents-workspace/internal/geom"
)

// QualityResult is the complete result of a route quality evaluation.
type QualityResult struct {
	Metrics RouteMetrics
	Quality Quality
}

type unitSegment struct{ A, B geom.Point }
type pointOwners struct{ horizontal, vertical map[string]struct{} }
type edgeContribution struct {
	metrics   RouteMetrics
	alignment int
}
type edgePair struct{ Left, Right string }
type pairContribution struct{ crossings, sharedUnits int }
type spatialKind byte

const (
	spatialNode spatialKind = iota
	spatialGroup
	spatialLabel
)

type spatialRef struct {
	Kind  spatialKind
	ID    string
	Rect  geom.Rect
	Dummy bool
}

// Index is immutable derived state tied to exactly one Snapshot revision.
// Its fields deliberately remain private: callers may cache it but cannot
// mutate the ownership sets used by incremental evaluation.
type Index struct {
	owner          *Snapshot
	revision       uint64
	base           QualityResult
	segments       map[unitSegment]map[string]struct{}
	points         map[geom.Point]pointOwners
	boundaries     map[unitSegment]map[string]struct{}
	labels         map[string]geom.Rect
	edges          map[string]edgeContribution
	edgeSegments   map[string]map[unitSegment]struct{}
	edgePoints     map[string]map[geom.Point]byte
	pairs          map[edgePair]pairContribution
	routes         map[string]RoutedEdge
	order          map[string]int
	rankExtent     map[int]geom.Rect
	rects          spatialIndex
	labelRects     spatialIndex
	corridorExtent map[int]geom.Rect
	labelNode      map[string]int
	labelPair      map[string]bool
	labelEdge      map[string]bool
}

type spatialIndex struct {
	rows    map[int]map[spatialRef]struct{}
	columns map[int]map[spatialRef]struct{}
}

func (x *spatialIndex) add(ref spatialRef) {
	if x.rows == nil {
		x.rows = map[int]map[spatialRef]struct{}{}
		x.columns = map[int]map[spatialRef]struct{}{}
	}
	for y := ref.Rect.Y; y < ref.Rect.Y+ref.Rect.Height; y++ {
		if x.rows[y] == nil {
			x.rows[y] = map[spatialRef]struct{}{}
		}
		x.rows[y][ref] = struct{}{}
	}
	for column := ref.Rect.X; column < ref.Rect.X+ref.Rect.Width; column++ {
		if x.columns[column] == nil {
			x.columns[column] = map[spatialRef]struct{}{}
		}
		x.columns[column][ref] = struct{}{}
	}
}

func (x spatialIndex) query(rect geom.Rect) []spatialRef {
	rows := map[spatialRef]struct{}{}
	for y := rect.Y; y < rect.Y+rect.Height; y++ {
		for ref := range x.rows[y] {
			rows[ref] = struct{}{}
		}
	}
	hits := map[spatialRef]struct{}{}
	for column := rect.X; column < rect.X+rect.Width; column++ {
		for ref := range x.columns[column] {
			if _, ok := rows[ref]; ok && geom.Overlaps(rect, ref.Rect) {
				hits[ref] = struct{}{}
			}
		}
	}
	result := make([]spatialRef, 0, len(hits))
	for ref := range hits {
		result = append(result, ref)
	}
	sort.Slice(result, func(a, b int) bool {
		return result[a].Kind < result[b].Kind || result[a].Kind == result[b].Kind && result[a].ID < result[b].ID
	})
	return result
}

// BuildIndex constructs the spatial and contribution index for s.
func BuildIndex(s *Snapshot) (*Index, error) {
	if s == nil {
		return nil, fmt.Errorf("snapshot is nil")
	}
	i := &Index{owner: s, revision: s.revision, segments: map[unitSegment]map[string]struct{}{}, points: map[geom.Point]pointOwners{}, boundaries: map[unitSegment]map[string]struct{}{}, labels: map[string]geom.Rect{}, edges: map[string]edgeContribution{}, edgeSegments: map[string]map[unitSegment]struct{}{}, edgePoints: map[string]map[geom.Point]byte{}, pairs: map[edgePair]pairContribution{}, routes: map[string]RoutedEdge{}, order: map[string]int{}, rankExtent: map[int]geom.Rect{}, corridorExtent: map[int]geom.Rect{}, labelNode: map[string]int{}, labelPair: map[string]bool{}, labelEdge: map[string]bool{}}
	for _, n := range s.nodes {
		ref := spatialRef{Kind: spatialNode, ID: n.ID, Rect: n.Rect, Dummy: n.Dummy}
		i.rects.add(ref)
		r, ok := i.rankExtent[n.Rank]
		if !ok {
			i.rankExtent[n.Rank] = n.Rect
		} else {
			i.rankExtent[n.Rank] = unionRect(r, n.Rect)
		}
	}
	for _, g := range s.groups {
		ref := spatialRef{Kind: spatialGroup, ID: g.ID, Rect: g.Rect}
		i.rects.add(ref)
		addBoundary(i.boundaries, g)
	}
	for n, e := range s.routes {
		if _, ok := i.routes[e.ID]; ok {
			return nil, fmt.Errorf("duplicate route %q", e.ID)
		}
		i.routes[e.ID] = cloneRoutedEdge(e)
		i.order[e.ID] = n
		if e.LabelPlaced {
			i.labels[e.ID] = e.LabelRect
			ref := spatialRef{Kind: spatialLabel, ID: e.ID, Rect: e.LabelRect}
			i.labelRects.add(ref)
		}
		c, segs, pts := contribution(s, e, i.boundaries)
		i.edges[e.ID] = c
		i.edgeSegments[e.ID] = segs
		i.edgePoints[e.ID] = pts
		addOwners(i.segments, i.points, e.ID, segs, pts)
	}
	for seg, owners := range i.segments {
		_ = seg
		for a := range owners {
			for b := range owners {
				if a < b {
					p := edgePair{a, b}
					c := i.pairs[p]
					c.sharedUnits++
					i.pairs[p] = c
				}
			}
		}
	}
	for _, owners := range i.points {
		for a := range owners.horizontal {
			for b := range owners.vertical {
				if a != b {
					p := orderedPair(a, b)
					c := i.pairs[p]
					c.crossings++
					i.pairs[p] = c
				}
			}
		}
	}
	ranks := make([]int, 0, len(i.rankExtent))
	for rank := range i.rankExtent {
		ranks = append(ranks, rank)
	}
	sort.Ints(ranks)
	for position, rank := range ranks {
		extent := i.rankExtent[rank]
		if position+1 < len(ranks) {
			extent = unionRect(extent, i.rankExtent[ranks[position+1]])
		}
		i.corridorExtent[rank] = extent
	}
	buildLabelContributions(i)
	i.base = FullQuality(s)
	return i, nil
}

func FullQuality(s *Snapshot) QualityResult {
	if s == nil {
		return QualityResult{}
	}
	var m RouteMetrics
	m.NodeOverlaps = countNodeOverlaps(s.nodes)
	boundaries := map[unitSegment]map[string]struct{}{}
	for _, g := range s.groups {
		addBoundary(boundaries, g)
	}
	segments := map[unitSegment]map[string]struct{}{}
	points := map[geom.Point]pointOwners{}
	routed := map[string]bool{}
	alignment := 0
	for _, e := range s.routes {
		routed[e.ID] = true
		c, segs, pts := contribution(s, e, boundaries)
		addMetrics(&m, c.metrics)
		alignment += c.alignment
		addOwners(segments, points, e.ID, segs, pts)
	}
	for _, owners := range segments {
		m.Overlaps += overlap(owners)
	}
	for _, owners := range points {
		m.Crossings += crossings(owners)
	}
	expected := map[string]bool{}
	for _, e := range s.layoutEdges {
		expected[e.OriginalEdgeID] = true
	}
	for id := range expected {
		if !routed[id] {
			m.UnroutedEdges++
		}
	}
	tmp := &Index{routes: map[string]RoutedEdge{}, points: points, labelNode: map[string]int{}, labelPair: map[string]bool{}, labelEdge: map[string]bool{}}
	for _, node := range s.nodes {
		ref := spatialRef{Kind: spatialNode, ID: node.ID, Rect: node.Rect, Dummy: node.Dummy}
		tmp.rects.add(ref)
	}
	for _, e := range s.routes {
		tmp.routes[e.ID] = e
		if e.LabelPlaced {
			ref := spatialRef{Kind: spatialLabel, ID: e.ID, Rect: e.LabelRect}
			tmp.labelRects.add(ref)
		}
	}
	buildLabelContributions(tmp)
	for _, n := range tmp.labelNode {
		m.LabelNodeCollisions += n
	}
	for _, hit := range tmp.labelPair {
		if hit {
			m.LabelLabelCollisions++
		}
	}
	for _, hit := range tmp.labelEdge {
		if hit {
			m.LabelEdgeCollisions++
		}
	}
	return QualityResult{m, qualityFromMetrics(s, m, alignment)}
}

// IncrementalQuality evaluates one or more route replacements without a full
// route scan. Replacements are matched by ID and do not mutate the Snapshot.
func IncrementalQuality(s *Snapshot, index *Index, replacements []RoutedEdge) (QualityResult, error) {
	if err := checkIndex(s, index); err != nil {
		return QualityResult{}, err
	}
	changed := map[string]RoutedEdge{}
	for _, e := range replacements {
		base, ok := index.routes[e.ID]
		if !ok {
			return QualityResult{}, fmt.Errorf("unknown route %q", e.ID)
		}
		if err := validateIncrementalReplacement(s, base, e); err != nil {
			return QualityResult{}, err
		}
		changed[e.ID] = cloneRoutedEdge(e)
	}
	base := fullQualityFromIndex(index)
	m := base.Metrics
	alignment := base.Quality.Alignment
	touchedSeg := map[unitSegment]struct{}{}
	touchedPoint := map[geom.Point]struct{}{}
	newSeg := map[string]map[unitSegment]struct{}{}
	newPts := map[string]map[geom.Point]byte{}
	for id, e := range changed {
		old := index.edges[id]
		subtractMetrics(&m, old.metrics)
		alignment -= old.alignment
		c, segs, pts := contribution(s, e, index.boundaries)
		addMetrics(&m, c.metrics)
		alignment += c.alignment
		newSeg[id] = segs
		newPts[id] = pts
		for seg := range index.edgeSegments[id] {
			touchedSeg[seg] = struct{}{}
		}
		for seg := range segs {
			touchedSeg[seg] = struct{}{}
		}
		for point := range index.edgePoints[id] {
			touchedPoint[point] = struct{}{}
		}
		for p := range pts {
			touchedPoint[p] = struct{}{}
		}
	}
	for seg := range touchedSeg {
		before := overlap(index.segments[seg])
		ownerCount := len(index.segments[seg])
		for id := range changed {
			_, had := index.segments[seg][id]
			_, has := newSeg[id][seg]
			if had && !has {
				ownerCount--
			} else if !had && has {
				ownerCount++
			}
		}
		m.Overlaps += overlapCount(ownerCount) - before
	}
	for p := range touchedPoint {
		before := crossings(index.points[p])
		m.Crossings += crossingsAfter(index.points[p], changed, newPts, p) - before
	}
	updateLabels(index, changed, newPts, &m)
	return QualityResult{m, qualityFromMetrics(s, m, alignment)}, nil
}

type ShadowResult struct {
	Incremental QualityResult
	Full        QualityResult
	Match       bool
	Adoptable   bool
}

// ShadowQuality compares incremental evaluation with the full oracle.
func ShadowQuality(s *Snapshot, index *Index, replacements []RoutedEdge, applied *Snapshot) (result ShadowResult, err error) {
	return shadowQuality(s, index, replacements, applied, nil)
}

// ShadowQualityWithFull uses a caller-supplied full oracle result, avoiding a
// duplicate full evaluation when materialization already computed it.
func ShadowQualityWithFull(s *Snapshot, index *Index, replacements []RoutedEdge, applied *Snapshot, full QualityResult) (result ShadowResult, err error) {
	return shadowQuality(s, index, replacements, applied, &full)
}

func shadowQuality(s *Snapshot, index *Index, replacements []RoutedEdge, applied *Snapshot, cached *QualityResult) (result ShadowResult, err error) {
	if applied == nil {
		return result, fmt.Errorf("applied snapshot is nil")
	}
	if s == nil || applied.revision != s.revision+1 {
		return result, fmt.Errorf("applied snapshot has wrong revision")
	}
	expected := s.Routes()
	seen := map[string]bool{}
	for _, replacement := range replacements {
		if seen[replacement.ID] {
			return result, fmt.Errorf("duplicate replacement %q", replacement.ID)
		}
		seen[replacement.ID] = true
		found := false
		for n := range expected {
			if expected[n].ID == replacement.ID {
				expected[n], found = cloneRoutedEdge(replacement), true
				break
			}
		}
		if !found {
			return result, fmt.Errorf("unknown route %q", replacement.ID)
		}
	}
	if !reflect.DeepEqual(expected, applied.routes) {
		return result, fmt.Errorf("applied routes do not match replacements")
	}
	baseContext, appliedContext := snapshotContext(s), snapshotContext(applied)
	if !reflect.DeepEqual(baseContext, appliedContext) {
		return result, fmt.Errorf("applied snapshot changes non-route context")
	}
	result.Incremental, err = IncrementalQuality(s, index, replacements)
	if err != nil {
		return result, err
	}
	if cached == nil {
		result.Full = FullQuality(applied)
	} else {
		result.Full = *cached
	}
	result.Match = result.Incremental == result.Full
	result.Adoptable = result.Match
	return result, nil
}

func snapshotContext(s *Snapshot) Snapshot {
	context := *s
	context.routes = nil
	context.revision = 0
	context.routeMetrics = RouteMetrics{}
	context.quality = Quality{}
	return context
}

// FirstAffectedLabel returns the earliest input-order label whose placement
// may be affected by the changed routes, or -1 when no label is affected.
func FirstAffectedLabel(s *Snapshot, index *Index, replacements []RoutedEdge) (int, error) {
	if err := checkIndex(s, index); err != nil {
		return -1, err
	}
	first := len(s.routes)
	geometryChanged := false
	for _, e := range replacements {
		old, ok := index.routes[e.ID]
		if !ok {
			return -1, fmt.Errorf("unknown route %q", e.ID)
		}
		if old.LineStyle != e.LineStyle || !reflect.DeepEqual(old.Points, e.Points) {
			geometryChanged = true
		}
		if old.Label != e.Label || old.LabelPlaced != e.LabelPlaced || old.LabelRect != e.LabelRect {
			if index.order[e.ID] < first {
				first = index.order[e.ID]
			}
		}
	}
	if geometryChanged {
		for n, route := range s.routes {
			if route.LineStyle != diagram.LineInvisible && route.Label != "" {
				return n, nil
			}
		}
	}
	if first == len(s.routes) {
		return -1, nil
	}
	return first, nil
}

func fullQualityFromIndex(i *Index) QualityResult { return i.base }
func checkIndex(s *Snapshot, i *Index) error {
	if s == nil || i == nil {
		return fmt.Errorf("snapshot or index is nil")
	}
	if i.owner != s || i.revision != s.revision {
		return fmt.Errorf("index belongs to a different snapshot revision")
	}
	return nil
}

func qualityFromMetrics(s *Snapshot, m RouteMetrics, alignment int) Quality {
	cross := s.layoutMetrics.Height
	if s.direction.Vertical() {
		cross = s.layoutMetrics.Width
	}
	return Quality{m.UnroutedEdges, m.NodeOverlaps + m.EdgeNodeCollisions + m.GroupBoundaryOverlaps, m.LabelNodeCollisions + m.LabelLabelCollisions + m.LabelEdgeCollisions + m.UnplacedLabels, m.Overlaps + m.Crossings, s.layoutMetrics.ReversedEdges, m.Bends + m.ReverseMoves, m.ExcessLength, cross, alignment, s.layoutMetrics.Width * s.layoutMetrics.Height, m.TotalLength}
}

func contribution(s *Snapshot, e RoutedEdge, boundaries map[unitSegment]map[string]struct{}) (edgeContribution, map[unitSegment]struct{}, map[geom.Point]byte) {
	var c edgeContribution
	segs := map[unitSegment]struct{}{}
	pts := map[geom.Point]byte{}
	if e.LineStyle == diagram.LineInvisible {
		return c, segs, pts
	}
	if e.Label != "" && !e.LabelPlaced {
		c.metrics.UnplacedLabels++
	}
	if len(e.Points) < 2 {
		c.metrics.UnroutedEdges++
		return c, segs, pts
	}
	if e.Fallback {
		c.metrics.AStarFallbacks++
	}
	c.metrics.Bends = max(0, len(e.Points)-2)
	collided := map[string]bool{}
	length := 0
	for n := 1; n < len(e.Points); n++ {
		a, b := e.Points[n-1], e.Points[n]
		d := magnitude(a.X-b.X) + magnitude(a.Y-b.Y)
		length += d
		c.metrics.TotalLength += d
		if reverseMove(a, b, s.direction) {
			c.metrics.ReverseMoves++
		}
		walk(a, b, func(x, y geom.Point) {
			seg := canonical(x, y)
			segs[seg] = struct{}{}
			bit := byte(2)
			if x.Y == y.Y {
				bit = 1
			}
			pts[x] |= bit
			pts[y] |= bit
			if boundaryApplies(s, e, boundaries[seg]) {
				c.metrics.GroupBoundaryOverlaps++
			}
		})
		walkPoints(a, b, func(p geom.Point) {
			for _, node := range s.nodes {
				if !node.Dummy && node.ID != e.From && node.ID != e.To && !collided[node.ID] && geom.ContainsInterior(node.Rect, p) {
					collided[node.ID] = true
					c.metrics.EdgeNodeCollisions++
				}
			}
		})
	}
	first, last := e.Points[0], e.Points[len(e.Points)-1]
	c.metrics.ExcessLength = max(0, length-magnitude(last.X-first.X)-magnitude(last.Y-first.Y))
	if !e.SelfLoop {
		if s.direction.Vertical() {
			c.alignment = magnitude(last.X - first.X)
		} else {
			c.alignment = magnitude(last.Y - first.Y)
		}
	}
	return c, segs, pts
}

func boundaryApplies(_ *Snapshot, e RoutedEdge, owners map[string]struct{}) bool {
	for id := range owners {
		if !(e.Source.Kind == diagram.EndpointGroup && e.Source.ID() == id) && !(e.Target.Kind == diagram.EndpointGroup && e.Target.ID() == id) {
			return true
		}
	}
	return false
}
func addBoundary(dst map[unitSegment]map[string]struct{}, g Group) {
	if g.Rect.Width <= 0 || g.Rect.Height <= 0 {
		return
	}
	l, t := g.Rect.X, g.Rect.Y
	r, b := l+g.Rect.Width-1, t+g.Rect.Height-1
	for _, side := range [][2]geom.Point{
		{{X: l, Y: t}, {X: r, Y: t}},
		{{X: r, Y: t}, {X: r, Y: b}},
		{{X: r, Y: b}, {X: l, Y: b}},
		{{X: l, Y: b}, {X: l, Y: t}},
	} {
		walk(side[0], side[1], func(a, z geom.Point) {
			k := canonical(a, z)
			if dst[k] == nil {
				dst[k] = map[string]struct{}{}
			}
			dst[k][g.ID] = struct{}{}
		})
	}
}
func addOwners(sm map[unitSegment]map[string]struct{}, pm map[geom.Point]pointOwners, id string, segs map[unitSegment]struct{}, pts map[geom.Point]byte) {
	for s := range segs {
		if sm[s] == nil {
			sm[s] = map[string]struct{}{}
		}
		sm[s][id] = struct{}{}
	}
	for p, b := range pts {
		o := pm[p]
		if o.horizontal == nil {
			o.horizontal = map[string]struct{}{}
			o.vertical = map[string]struct{}{}
		}
		if b&1 != 0 {
			o.horizontal[id] = struct{}{}
		}
		if b&2 != 0 {
			o.vertical[id] = struct{}{}
		}
		pm[p] = o
	}
}
func canonical(a, b geom.Point) unitSegment {
	if a.X > b.X || a.X == b.X && a.Y > b.Y {
		a, b = b, a
	}
	return unitSegment{a, b}
}
func walk(a, b geom.Point, fn func(geom.Point, geom.Point)) {
	dx, dy := sign(b.X-a.X), sign(b.Y-a.Y)
	for a != b {
		n := geom.Point{X: a.X + dx, Y: a.Y + dy}
		fn(a, n)
		a = n
	}
}
func walkPoints(a, b geom.Point, fn func(geom.Point)) {
	dx, dy := sign(b.X-a.X), sign(b.Y-a.Y)
	for {
		fn(a)
		if a == b {
			return
		}
		a = geom.Point{X: a.X + dx, Y: a.Y + dy}
	}
}
func magnitude(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
func reverseMove(a, b geom.Point, d diagram.Direction) bool {
	if d.Vertical() {
		return (b.Y-a.Y)*d.ForwardSign() < 0
	}
	return (b.X-a.X)*d.ForwardSign() < 0
}
func overlap(s map[string]struct{}) int {
	return overlapCount(len(s))
}
func overlapCount(count int) int { return max(0, count-1) }
func crossings(o pointOwners) int {
	n := 0
	for h := range o.horizontal {
		for v := range o.vertical {
			if h != v {
				n++
			}
		}
	}
	return n
}
func crossingsAfter(o pointOwners, changed map[string]RoutedEdge, points map[string]map[geom.Point]byte, point geom.Point) int {
	horizontal, vertical, common := len(o.horizontal), len(o.vertical), 0
	for id := range o.horizontal {
		if _, ok := o.vertical[id]; ok {
			common++
		}
	}
	for id := range changed {
		_, oldHorizontal := o.horizontal[id]
		_, oldVertical := o.vertical[id]
		bits := points[id][point]
		newHorizontal, newVertical := bits&1 != 0, bits&2 != 0
		if oldHorizontal != newHorizontal {
			if newHorizontal {
				horizontal++
			} else {
				horizontal--
			}
		}
		if oldVertical != newVertical {
			if newVertical {
				vertical++
			} else {
				vertical--
			}
		}
		if oldHorizontal && oldVertical {
			common--
		}
		if newHorizontal && newVertical {
			common++
		}
	}
	return horizontal*vertical - common
}
func addMetrics(a *RouteMetrics, b RouteMetrics) {
	a.EdgeNodeCollisions += b.EdgeNodeCollisions
	a.GroupBoundaryOverlaps += b.GroupBoundaryOverlaps
	a.TotalLength += b.TotalLength
	a.ExcessLength += b.ExcessLength
	a.Bends += b.Bends
	a.ReverseMoves += b.ReverseMoves
	a.AStarFallbacks += b.AStarFallbacks
	a.UnroutedEdges += b.UnroutedEdges
	a.UnplacedLabels += b.UnplacedLabels
}
func subtractMetrics(a *RouteMetrics, b RouteMetrics) {
	b.EdgeNodeCollisions *= -1
	b.GroupBoundaryOverlaps *= -1
	b.TotalLength *= -1
	b.ExcessLength *= -1
	b.Bends *= -1
	b.ReverseMoves *= -1
	b.AStarFallbacks *= -1
	b.UnroutedEdges *= -1
	b.UnplacedLabels *= -1
	addMetrics(a, b)
}
func unionRect(a, b geom.Rect) geom.Rect {
	x := min(a.X, b.X)
	y := min(a.Y, b.Y)
	r := max(a.X+a.Width, b.X+b.Width)
	d := max(a.Y+a.Height, b.Y+b.Height)
	return geom.Rect{X: x, Y: y, Width: r - x, Height: d - y}
}
func cloneRoutedEdge(e RoutedEdge) RoutedEdge {
	e.Points = append([]geom.Point(nil), e.Points...)
	e.Source = cloneEndpoint(e.Source)
	e.Target = cloneEndpoint(e.Target)
	e.Start.Semantic = cloneEndpoint(e.Start.Semantic)
	e.End.Semantic = cloneEndpoint(e.End.Semantic)
	return e
}

func validateIncrementalReplacement(s *Snapshot, base, edge RoutedEdge) error {
	if !sameEndpoint(base.Source, edge.Source) || !sameEndpoint(base.Target, edge.Target) {
		return fmt.Errorf("route %q changes semantic endpoints", edge.ID)
	}
	if len(edge.Points) < 2 {
		return fmt.Errorf("route %q has fewer than two points", edge.ID)
	}
	for index, point := range edge.Points {
		if !geom.Contains(s.bounds, point) {
			return fmt.Errorf("route %q point %+v is outside bounds", edge.ID, point)
		}
		if index > 0 {
			previous := edge.Points[index-1]
			if previous == point || previous.X != point.X && previous.Y != point.Y {
				return fmt.Errorf("route %q has invalid segment %+v -> %+v", edge.ID, previous, point)
			}
		}
	}
	if !sameEndpoint(edge.Start.Semantic, edge.Source) || edge.Start.Point != edge.Points[0] ||
		!sameEndpoint(edge.End.Semantic, edge.Target) || edge.End.Point != edge.Points[len(edge.Points)-1] {
		return fmt.Errorf("route %q has inconsistent resolved endpoints", edge.ID)
	}
	return nil
}

func pairKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "\x00" + b
}
func orderedPair(a, b string) edgePair {
	if a > b {
		a, b = b, a
	}
	return edgePair{a, b}
}

func countNodeOverlaps(nodes []Node) int {
	count := 0
	for n, left := range nodes {
		if left.Dummy {
			continue
		}
		for _, right := range nodes[n+1:] {
			if !right.Dummy && geom.Overlaps(left.Rect, right.Rect) {
				count++
			}
		}
	}
	return count
}
func buildLabelContributions(i *Index) {
	for id, e := range i.routes {
		if !e.LabelPlaced {
			continue
		}
		for _, ref := range i.rects.query(e.LabelRect) {
			if ref.Kind == spatialNode && !ref.Dummy && geom.Overlaps(e.LabelRect, ref.Rect) {
				i.labelNode[id]++
			}
		}
		for _, ref := range i.labelRects.query(e.LabelRect) {
			if ref.ID > id && geom.Overlaps(e.LabelRect, ref.Rect) {
				i.labelPair[pairKey(id, ref.ID)] = true
			}
		}
		i.labelEdge[id] = labelHitsPointOwners(e.LabelRect, id, i, nil, nil)
	}
}
func updateLabels(i *Index, ch map[string]RoutedEdge, newPts map[string]map[geom.Point]byte, m *RouteMetrics) {
	affectedLabels := map[string]struct{}{}
	for id := range ch {
		affectedLabels[id] = struct{}{}
		for seg := range i.edgeSegments[id] {
			for _, ref := range i.labelRects.query(segmentRect(seg)) {
				affectedLabels[ref.ID] = struct{}{}
			}
		}
		for seg := range segmentsFromPoints(ch[id].Points) {
			for _, ref := range i.labelRects.query(segmentRect(seg)) {
				affectedLabels[ref.ID] = struct{}{}
			}
		}
	}
	for id := range affectedLabels {
		m.LabelNodeCollisions -= i.labelNode[id]
		old := i.labelEdge[id]
		if old {
			m.LabelEdgeCollisions--
		}
		e := currentRoute(i, ch, id)
		if e.LabelPlaced {
			for _, ref := range i.rects.query(e.LabelRect) {
				if ref.Kind == spatialNode && !ref.Dummy && geom.Overlaps(e.LabelRect, ref.Rect) {
					m.LabelNodeCollisions++
				}
			}
			if labelHitsPointOwners(e.LabelRect, id, i, ch, newPts) {
				m.LabelEdgeCollisions++
			}
		}
	}
	pairs := map[string][2]string{}
	for id := range ch {
		old := i.routes[id]
		for _, rect := range []geom.Rect{old.LabelRect, ch[id].LabelRect} {
			for _, ref := range i.labelRects.query(rect) {
				if ref.ID != id {
					pairs[pairKey(id, ref.ID)] = [2]string{id, ref.ID}
				}
			}
		}
		for oid := range ch {
			if id != oid {
				pairs[pairKey(id, oid)] = [2]string{id, oid}
			}
		}
	}
	for k, ids := range pairs {
		if i.labelPair[k] {
			m.LabelLabelCollisions--
		}
		left, right := currentRoute(i, ch, ids[0]), currentRoute(i, ch, ids[1])
		if left.LabelPlaced && right.LabelPlaced && geom.Overlaps(left.LabelRect, right.LabelRect) {
			m.LabelLabelCollisions++
		}
	}
}

func currentRoute(i *Index, changed map[string]RoutedEdge, id string) RoutedEdge {
	if edge, ok := changed[id]; ok {
		return edge
	}
	return i.routes[id]
}
func segmentRect(seg unitSegment) geom.Rect {
	return geom.Rect{X: min(seg.A.X, seg.B.X), Y: min(seg.A.Y, seg.B.Y), Width: magnitude(seg.A.X-seg.B.X) + 1, Height: magnitude(seg.A.Y-seg.B.Y) + 1}
}
func segmentsFromPoints(points []geom.Point) map[unitSegment]struct{} {
	result := map[unitSegment]struct{}{}
	for n := 1; n < len(points); n++ {
		walk(points[n-1], points[n], func(a, b geom.Point) { result[canonical(a, b)] = struct{}{} })
	}
	return result
}
func labelHitsPointOwners(rect geom.Rect, self string, i *Index, changed map[string]RoutedEdge, newPts map[string]map[geom.Point]byte) bool {
	for y := rect.Y; y < rect.Y+rect.Height; y++ {
		for x := rect.X; x < rect.X+rect.Width; x++ {
			p := geom.Point{X: x, Y: y}
			o := i.points[p]
			for id := range o.horizontal {
				if id != self {
					if _, replaced := changed[id]; !replaced || newPts[id][p]&1 != 0 {
						return true
					}
				}
			}
			for id := range o.vertical {
				if id != self {
					if _, replaced := changed[id]; !replaced || newPts[id][p]&2 != 0 {
						return true
					}
				}
			}
			for id := range changed {
				if id != self && newPts[id][p] != 0 {
					return true
				}
			}
		}
	}
	return false
}
