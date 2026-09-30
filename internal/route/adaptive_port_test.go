package route

import (
	"reflect"
	"testing"

	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/layout"
)

func TestAllocateAdaptiveStartPortsReservesFixedSlot(t *testing.T) {
	t.Parallel()
	source := &layout.Node{ID: "source", Rect: layout.Rect{X: 2, Y: 2, Width: 7, Height: 3}}
	a := &Edge{ID: "a", From: "source", To: "left"}
	b := &Edge{ID: "b", From: "source", To: "right"}
	fixed := &Edge{ID: "fixed", From: "source", Points: []Point{{X: 5, Y: 4}, {X: 5, Y: 5}}, Constrained: true}
	assignment := map[string]diagram.Side{"a": diagram.SideSouth, "b": diagram.SideSouth}
	nodes := map[string]*layout.Node{
		"left":  {ID: "left", Rect: layout.Rect{X: 0, Y: 8, Width: 3, Height: 3}},
		"right": {ID: "right", Rect: layout.Rect{X: 12, Y: 8, Width: 3, Height: 3}},
	}
	ports := allocateAdaptiveStartPorts(source, []*Edge{a, b}, []*Edge{a, b, fixed}, assignment, nodes)
	if ports["a"] == fixed.Points[0] || ports["b"] == fixed.Points[0] || ports["a"] == ports["b"] {
		t.Fatalf("ports were not distributed around reserved slot: %v", ports)
	}
}

func TestAdaptiveGeometrySnapshotPreservesSharedSegmentCounts(t *testing.T) {
	t.Parallel()
	a := &Edge{ID: "a", Points: []Point{{X: 0, Y: 2}, {X: 4, Y: 2}}}
	b := &Edge{ID: "b", Points: []Point{{X: 1, Y: 2}, {X: 4, Y: 2}}}
	c := &Edge{ID: "c", Points: []Point{{X: 2, Y: 0}, {X: 2, Y: 4}}}
	snapshot := newAdaptiveGeometrySnapshot([]*Edge{a, b, c})
	gotCrossings, gotOverlaps := (adaptiveGeometryView{base: snapshot}).interactions(a.ID, a.Points)
	wantCrossings, wantOverlaps := edgeInteractions(a, []*Edge{b, c})
	if gotCrossings != wantCrossings || gotOverlaps != wantOverlaps {
		t.Fatalf("snapshot interactions = (%d, %d), want (%d, %d)", gotCrossings, gotOverlaps, wantCrossings, wantOverlaps)
	}
	if snapshot.occupancy[canonicalSegment(Point{X: 2, Y: 2}, Point{X: 3, Y: 2})] != 2 {
		t.Fatal("shared segment occupancy did not retain both edge references")
	}
}

func TestOptimizeAutoStartSidesCOWRejectsWithoutMutatingBase(t *testing.T) {
	t.Parallel()
	result, routes := adaptiveCOWFixture()
	want := cloneRoutes(routes)
	var diagnostics Diagnostics
	got, _ := OptimizeAutoStartSidesCOW(result, routes, Metrics{}, func(base []*Edge, candidate AdaptiveCandidate) AdaptiveEvaluation {
		if !reflect.DeepEqual(base, want) {
			t.Fatal("callback base was mutated")
		}
		if len(candidate.Replacements) != 1 {
			t.Fatalf("replacement count = %d, want bundle size 1", len(candidate.Replacements))
		}
		return AdaptiveEvaluation{}
	}, &diagnostics)
	if !reflect.DeepEqual(routes, want) || !reflect.DeepEqual(got, routes) {
		t.Fatal("rejected candidates changed the base")
	}
	if diagnostics.RouteCloneCalls != 0 || diagnostics.CandidateClonedRoutes != diagnostics.AdaptiveCandidates {
		t.Fatalf("unexpected clone diagnostics: %+v", diagnostics)
	}
}

func TestAdaptiveGeometryViewMatchesAcceptedOccupancy(t *testing.T) {
	base := []*Edge{
		{ID: "a", Points: []Point{{X: 0, Y: 1}, {X: 6, Y: 1}}},
		{ID: "b", Points: []Point{{X: 2, Y: 0}, {X: 2, Y: 4}}},
		{ID: "c", Points: []Point{{X: 0, Y: 3}, {X: 6, Y: 3}}},
	}
	replacement := &Edge{ID: "b", Points: []Point{{X: 4, Y: 0}, {X: 4, Y: 4}}}
	added := newAdaptiveGeometrySnapshot([]*Edge{replacement})
	view := adaptiveGeometryView{base: newAdaptiveGeometrySnapshot(base), excluded: map[string]bool{"b": true, "c": true}, added: added}
	want := buildOccupancy([]*Edge{base[0], replacement})
	for y := 0; y < 5; y++ {
		for x := 0; x < 7; x++ {
			point := Point{X: x, Y: y}
			var directions uint8
			for _, neighbor := range []Point{{X: x - 1, Y: y}, {X: x + 1, Y: y}} {
				if view.count(canonicalSegment(point, neighbor), "candidate") > 0 {
					directions |= 1
				}
			}
			for _, neighbor := range []Point{{X: x, Y: y - 1}, {X: x, Y: y + 1}} {
				if view.count(canonicalSegment(point, neighbor), "candidate") > 0 {
					directions |= 2
				}
			}
			if directions != want.points[point] {
				t.Fatalf("point %v directions=%d, want %d", point, directions, want.points[point])
			}
		}
	}
}

func TestOptimizeAutoStartSidesCOWCommitsReplacement(t *testing.T) {
	t.Parallel()
	result, routes := adaptiveCOWFixture()
	want := cloneRoutes(routes)
	accepted := false
	got, metrics := OptimizeAutoStartSidesCOW(result, routes, Metrics{}, func(_ []*Edge, candidate AdaptiveCandidate) AdaptiveEvaluation {
		accept := !accepted && candidate.Side == diagram.SideEast
		if accept {
			accepted = true
		}
		return AdaptiveEvaluation{Quality: Quality{}, Metrics: Metrics{Crossings: 7}, Accept: accept}
	}, nil)
	if !accepted || got[0] == routes[0] || got[0].StartSide != diagram.SideEast || metrics.Crossings != 7 {
		t.Fatalf("candidate was not committed: edge=%+v metrics=%+v", got[0], metrics)
	}
	if got[1] != routes[1] || !reflect.DeepEqual(routes, want) {
		t.Fatal("commit changed a non-bundle edge or the base")
	}
}

func adaptiveCOWFixture() (*layout.Layout, []*Edge) {
	result := &layout.Layout{Direction: diagram.DirectionDown, Nodes: []*layout.Node{
		{ID: "source", Rect: layout.Rect{X: 2, Y: 2, Width: 5, Height: 3}},
		{ID: "target", Rect: layout.Rect{X: 14, Y: 7, Width: 5, Height: 3}},
	}, Metrics: layout.Metrics{Width: 20, Height: 12}}
	return result, []*Edge{
		{ID: "adaptive", From: "source", To: "target", StartAuto: true, StartSide: diagram.SideSouth, EndSide: diagram.SideNorth,
			Source: diagram.Endpoint{Node: "source"}, Target: diagram.Endpoint{Node: "target"}, Points: []Point{{X: 4, Y: 4}, {X: 4, Y: 5}, {X: 16, Y: 5}, {X: 16, Y: 7}}},
		{ID: "blocker", From: "x", To: "y", Constrained: true, Points: []Point{{X: 5, Y: 4}, {X: 5, Y: 10}}},
	}
}
