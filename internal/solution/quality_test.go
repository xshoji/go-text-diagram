package solution

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/xshoji/agents-workspace/internal/geom"
)

func TestIncrementalQualityMatchesFull(t *testing.T) {
	s := validSnapshot()
	index, err := BuildIndex(s)
	if err != nil {
		t.Fatal(err)
	}
	changed := s.Routes()[0]
	changed.Points = []geom.Point{{X: 3, Y: 2}, {X: 3, Y: 6}, {X: 10, Y: 6}, {X: 10, Y: 2}}
	got, err := IncrementalQuality(s, index, []RoutedEdge{changed})
	if err != nil {
		t.Fatal(err)
	}
	applied := replaceRoutesForTest(s, changed)
	if want := FullQuality(applied); got != want {
		t.Fatalf("incremental = %+v, full = %+v", got, want)
	}
	if first, err := FirstAffectedLabel(s, index, []RoutedEdge{changed}); err != nil || first != -1 {
		t.Fatalf("FirstAffectedLabel = %d, %v", first, err)
	}
}

func TestIndexRejectsAnotherSnapshotAndRevision(t *testing.T) {
	s := validSnapshot()
	index, _ := BuildIndex(s)
	other := validSnapshot()
	if _, err := IncrementalQuality(other, index, nil); err == nil {
		t.Fatal("accepted index owned by another Snapshot")
	}
	s.revision++
	if _, err := IncrementalQuality(s, index, nil); err == nil {
		t.Fatal("accepted index for another revision")
	}
}

func TestDeriveDeepCopiesResolvedEndpointOffsets(t *testing.T) {
	snapshot := validSnapshot()
	offset := 1
	source := snapshot.routes[0].Source
	source.PortHint.Offset = &offset
	snapshot.routes[0].Source = source
	snapshot.routes[0].Start.Semantic = cloneEndpoint(source)
	snapshot.layoutEdges[0].Source = cloneEndpoint(source)
	snapshot.manifest.edges["e0"] = semanticEdge{source: cloneEndpoint(source), target: snapshot.routes[0].Target}
	replacement := snapshot.Routes()[0]
	derived, err := Materialize(snapshot, map[string]RoutedEdge{"e0": replacement})
	if err != nil {
		t.Fatal(err)
	}
	*replacement.Start.Semantic.PortHint.Offset = 9
	if got := *derived.Routes()[0].Start.Semantic.PortHint.Offset; got != 1 {
		t.Fatalf("derived offset changed through overlay alias: %d", got)
	}
	if err := derived.Validate(); err != nil {
		t.Fatalf("derived Snapshot became invalid: %v", err)
	}
}

func TestIndexStoresPairAndTypedSpatialContributions(t *testing.T) {
	s := syntheticQualitySnapshot(3)
	s.routes[1].Points = append([]geom.Point(nil), s.routes[0].Points...)
	index, err := BuildIndex(s)
	if err != nil {
		t.Fatal(err)
	}
	if index.pairs[orderedPair(s.routes[0].ID, s.routes[1].ID)].sharedUnits == 0 {
		t.Fatal("shared-unit edge-pair contribution was not indexed")
	}

	s = validSnapshot()
	s.groups = []Group{{ID: s.nodes[0].ID, Rect: s.nodes[0].Rect}}
	index, _ = BuildIndex(s)
	refs := index.rects.query(s.nodes[0].Rect)
	kinds := map[spatialKind]bool{}
	for _, ref := range refs {
		if ref.ID == s.nodes[0].ID {
			kinds[ref.Kind] = true
		}
	}
	if !kinds[spatialNode] || !kinds[spatialGroup] {
		t.Fatalf("typed spatial query lost colliding namespaces: %+v", refs)
	}
}

func TestShadowQualityMatchAndCorruptIndexFallback(t *testing.T) {
	s := validSnapshot()
	index, _ := BuildIndex(s)
	applied := replaceRoutesForTest(s)
	if result, err := ShadowQuality(s, index, nil, applied); err != nil || !result.Match || !result.Adoptable {
		t.Fatalf("unchanged shadow = %+v, error %v", result, err)
	}
	index.base.Metrics.TotalLength++
	got, err := ShadowQuality(s, index, nil, applied)
	if err != nil || got.Match || got.Adoptable || got.Full != FullQuality(applied) {
		t.Fatalf("corrupt shadow did not preserve full oracle: %+v, error %v", got, err)
	}
	if _, err := ShadowQuality(s, index, nil, nil); err == nil {
		t.Fatal("accepted nil applied snapshot")
	}
	wrongRevision := replaceRoutesForTest(s)
	wrongRevision.revision++
	if _, err := ShadowQuality(s, index, nil, wrongRevision); err == nil {
		t.Fatal("accepted wrong revision")
	}
	changedNode := replaceRoutesForTest(s)
	changedNode.nodes = append([]Node(nil), changedNode.nodes...)
	changedNode.nodes[0].Label += " changed"
	if _, err := ShadowQuality(s, index, nil, changedNode); err == nil {
		t.Fatal("accepted changed node label")
	}
	changedBounds := replaceRoutesForTest(s)
	changedBounds.bounds.Width++
	if _, err := ShadowQuality(s, index, nil, changedBounds); err == nil {
		t.Fatal("accepted changed bounds")
	}
	changed := s.Routes()[0]
	changed.Label = "different"
	if _, err := ShadowQuality(s, index, nil, replaceRoutesForTest(s, changed)); err == nil {
		t.Fatal("accepted unapplied replacement")
	}
}

func TestIncrementalQualityMetricContributions(t *testing.T) {
	tests := []struct {
		name   string
		base   func() *Snapshot
		change func(*Snapshot) []RoutedEdge
	}{
		{name: "length bends reverse fallback alignment", base: func() *Snapshot { return syntheticQualitySnapshot(3) }, change: func(s *Snapshot) []RoutedEdge {
			e := s.Routes()[0]
			e.Points = []geom.Point{{X: 0, Y: 0}, {X: 8, Y: 0}, {X: 8, Y: 7}, {X: 3, Y: 7}}
			e.Fallback = true
			return []RoutedEdge{e}
		}},
		{name: "unplaced label", base: func() *Snapshot { return syntheticQualitySnapshot(2) }, change: func(s *Snapshot) []RoutedEdge {
			e := s.Routes()[0]
			e.Label = "missing"
			e.LabelPlaced = false
			return []RoutedEdge{e}
		}},
		{name: "nonendpoint node collision", base: func() *Snapshot {
			s := syntheticQualitySnapshot(2)
			s.nodes = []Node{{ID: "obstacle", Rect: geom.Rect{X: 10, Y: 4, Width: 5, Height: 5}}}
			return s
		}, change: func(s *Snapshot) []RoutedEdge {
			e := s.Routes()[0]
			e.Points = []geom.Point{{X: 0, Y: 0}, {X: 12, Y: 0}, {X: 12, Y: 7}}
			return []RoutedEdge{e}
		}},
		{name: "group boundary overlap", base: func() *Snapshot {
			s := syntheticQualitySnapshot(2)
			s.groups = []Group{{ID: "group", Rect: geom.Rect{X: 5, Y: 2, Width: 10, Height: 5}}}
			return s
		}, change: func(s *Snapshot) []RoutedEdge {
			e := s.Routes()[0]
			e.Points = []geom.Point{{X: 0, Y: 0}, {X: 5, Y: 0}, {X: 5, Y: 7}}
			return []RoutedEdge{e}
		}},
		{name: "crossing and three edge shared units", base: func() *Snapshot {
			s := syntheticQualitySnapshot(4)
			s.routes[0].Points = []geom.Point{{X: 0, Y: 3}, {X: 30, Y: 3}}
			s.routes[1].Points = []geom.Point{{X: 0, Y: 3}, {X: 30, Y: 3}}
			s.routes[2].Points = []geom.Point{{X: 0, Y: 3}, {X: 30, Y: 3}}
			s.routes[3].Points = []geom.Point{{X: 15, Y: 0}, {X: 15, Y: 10}}
			return s
		}, change: func(s *Snapshot) []RoutedEdge {
			e := s.Routes()[1]
			e.Points = []geom.Point{{X: 0, Y: 6}, {X: 30, Y: 6}}
			return []RoutedEdge{e}
		}},
		{name: "label node label and edge collisions", base: labeledQualitySnapshot, change: func(s *Snapshot) []RoutedEdge {
			routes := s.Routes()
			routes[0].LabelRect = geom.Rect{X: 9, Y: 4, Width: 5, Height: 2}
			routes[1].Points = []geom.Point{{X: 0, Y: 5}, {X: 30, Y: 5}}
			return []RoutedEdge{routes[0], routes[1]}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := test.base()
			assertIncrementalMatchesFull(t, s, test.change(s)...)
		})
	}
}

func TestIncrementalQualityMatchesFullForDeterministicRandomChanges(t *testing.T) {
	random := rand.New(rand.NewSource(42))
	snapshot := syntheticQualitySnapshot(12)
	for step := 0; step < 150; step++ {
		index, err := BuildIndex(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		routeIndex := random.Intn(len(snapshot.routes))
		edge := snapshot.Routes()[routeIndex]
		startY := routeIndex * 3
		laneY := random.Intn(snapshot.layoutMetrics.Height)
		for laneY == startY {
			laneY = random.Intn(snapshot.layoutMetrics.Height)
		}
		turnX := 1 + random.Intn(29)
		edge.Points = []geom.Point{{X: 0, Y: startY}, {X: turnX, Y: startY}, {X: turnX, Y: laneY}, {X: 30, Y: laneY}}
		edge.Start.Point, edge.End.Point = edge.Points[0], edge.Points[len(edge.Points)-1]
		edge.Fallback = random.Intn(5) == 0
		if random.Intn(4) == 0 {
			edge.Label = "label"
			edge.LabelPlaced = random.Intn(3) != 0
			edge.LabelRect = geom.Rect{X: random.Intn(25), Y: random.Intn(snapshot.layoutMetrics.Height - 1), Width: 4, Height: 1}
		}
		got, err := IncrementalQuality(snapshot, index, []RoutedEdge{edge})
		if err != nil {
			t.Fatal(err)
		}
		applied := replaceRoutesForTest(snapshot, edge)
		if want := FullQuality(applied); got != want {
			t.Fatalf("step %d incremental = %+v, full = %+v", step, got, want)
		}
		snapshot = applied
	}
}

func TestFirstAffectedLabelTracksRouteAndLabelChanges(t *testing.T) {
	snapshot := labeledQualitySnapshot()
	index, err := BuildIndex(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	changedRoute := snapshot.Routes()[0]
	changedRoute.Points = []geom.Point{{X: 0, Y: 0}, {X: 11, Y: 0}, {X: 11, Y: 6}, {X: 30, Y: 6}}
	if got, err := FirstAffectedLabel(snapshot, index, []RoutedEdge{changedRoute}); err != nil || got != 0 {
		t.Fatalf("route impact = %d, %v; want 0", got, err)
	}
	changedLabel := snapshot.Routes()[0]
	changedLabel.LabelRect.X++
	if got, err := FirstAffectedLabel(snapshot, index, []RoutedEdge{changedLabel}); err != nil || got != 0 {
		t.Fatalf("label impact = %d, %v; want 0", got, err)
	}
}

func TestFirstAffectedLabelConservativelyReleasesAnyLabelCandidate(t *testing.T) {
	tests := []struct {
		name   string
		placed bool
		rect   geom.Rect
	}{
		{name: "unplaced label", placed: false},
		{name: "label placed on another candidate", placed: true, rect: geom.Rect{X: 25, Y: 20, Width: 4, Height: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := syntheticQualitySnapshot(3)
			s.routes[0].Label, s.routes[0].LabelPlaced, s.routes[0].LabelRect = "first", test.placed, test.rect
			index, err := BuildIndex(s)
			if err != nil {
				t.Fatal(err)
			}
			changed := s.Routes()[2]
			changed.Points = []geom.Point{{X: 0, Y: 6}, {X: 30, Y: 7}}
			if got, err := FirstAffectedLabel(s, index, []RoutedEdge{changed}); err != nil || got != 0 {
				t.Fatalf("FirstAffectedLabel = %d, %v; want 0", got, err)
			}
		})
	}
}

func assertIncrementalMatchesFull(t *testing.T, snapshot *Snapshot, replacements ...RoutedEdge) {
	t.Helper()
	for index := range replacements {
		if len(replacements[index].Points) > 0 {
			replacements[index].Start.Semantic = cloneEndpoint(replacements[index].Source)
			replacements[index].Start.Point = replacements[index].Points[0]
			replacements[index].End.Semantic = cloneEndpoint(replacements[index].Target)
			replacements[index].End.Point = replacements[index].Points[len(replacements[index].Points)-1]
		}
	}
	index, err := BuildIndex(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	got, err := IncrementalQuality(snapshot, index, replacements)
	if err != nil {
		t.Fatal(err)
	}
	if want := FullQuality(replaceRoutesForTest(snapshot, replacements...)); got != want {
		t.Fatalf("incremental = %+v, full = %+v", got, want)
	}
}

func labeledQualitySnapshot() *Snapshot {
	s := syntheticQualitySnapshot(3)
	s.nodes = []Node{{ID: "label-obstacle", Rect: geom.Rect{X: 10, Y: 4, Width: 5, Height: 4}}}
	s.routes[0].Label, s.routes[0].LabelPlaced = "first", true
	s.routes[0].LabelRect = geom.Rect{X: 2, Y: 1, Width: 5, Height: 1}
	s.routes[1].Label, s.routes[1].LabelPlaced = "second", true
	s.routes[1].LabelRect = geom.Rect{X: 10, Y: 6, Width: 6, Height: 1}
	return s
}

func replaceRoutesForTest(source *Snapshot, replacements ...RoutedEdge) *Snapshot {
	result := *source
	result.routes = source.Routes()
	for _, replacement := range replacements {
		for n := range result.routes {
			if result.routes[n].ID == replacement.ID {
				result.routes[n] = cloneRoutedEdge(replacement)
			}
		}
	}
	result.revision++
	return &result
}

func BenchmarkQuality(b *testing.B) {
	s := syntheticQualitySnapshot(80)
	index, _ := BuildIndex(s)
	changed := s.Routes()[0]
	changed.Points = []geom.Point{{X: 0, Y: 0}, {X: 0, Y: 4}, {X: 30, Y: 4}}
	b.Run("full", func(b *testing.B) {
		for range b.N {
			_ = FullQuality(s)
		}
	})
	b.Run("one-edge-incremental", func(b *testing.B) {
		for range b.N {
			_, _ = IncrementalQuality(s, index, []RoutedEdge{changed})
		}
	})
}

func BenchmarkDenseLabelQuality(b *testing.B) {
	for _, count := range []int{80, 320} {
		s := syntheticQualitySnapshot(count)
		for n := range s.routes {
			s.routes[n].Label, s.routes[n].LabelPlaced = "label", true
			s.routes[n].LabelRect = geom.Rect{X: 10, Y: n * 3, Width: 5, Height: 1}
		}
		index, _ := BuildIndex(s)
		changed := s.Routes()[count-1]
		y := (count - 1) * 3
		changed.Points = []geom.Point{{X: 0, Y: y}, {X: 0, Y: y + 2}, {X: 30, Y: y + 2}}
		b.Run(fmt.Sprintf("labels-%d/full", count), func(b *testing.B) {
			for range b.N {
				_ = FullQuality(s)
			}
		})
		b.Run(fmt.Sprintf("labels-%d/last-edge-incremental", count), func(b *testing.B) {
			for range b.N {
				_, _ = IncrementalQuality(s, index, []RoutedEdge{changed})
			}
		})
	}
}

func syntheticQualitySnapshot(edgeCount int) *Snapshot {
	s := validSnapshot()
	s.nodes, s.groups, s.routes, s.layoutEdges = nil, nil, nil, nil
	s.layoutMetrics.Width, s.layoutMetrics.Height = 40, edgeCount*3+2
	s.bounds = geom.Rect{Width: s.layoutMetrics.Width, Height: s.layoutMetrics.Height}
	for n := 0; n < edgeCount; n++ {
		id := fmt.Sprintf("edge-%03d", n)
		y := n * 3
		points := []geom.Point{{X: 0, Y: y}, {X: 30, Y: y}}
		s.routes = append(s.routes, RoutedEdge{ID: id, Points: points, Start: ResolvedEndpoint{Point: points[0]}, End: ResolvedEndpoint{Point: points[1]}})
		s.layoutEdges = append(s.layoutEdges, LayoutEdge{ID: id, OriginalEdgeID: id})
	}
	return s
}
