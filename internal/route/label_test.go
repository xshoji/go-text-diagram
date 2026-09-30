package route

import (
	"reflect"
	"testing"

	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/geom"
	"github.com/xshoji/go-text-diagram/internal/layout"
)

func TestPlaceLabelsFromFirstZeroMatchesFullPlacement(t *testing.T) {
	t.Parallel()
	result := &layout.Layout{Metrics: layout.Metrics{Width: 20, Height: 12}}
	base := []*Edge{
		{ID: "one", Label: "one", Points: []Point{{X: 1, Y: 2}, {X: 18, Y: 2}}},
		{ID: "two", Label: "two", Points: []Point{{X: 1, Y: 8}, {X: 18, Y: 8}}},
	}
	want := cloneRoutes(base)
	resetRouteLabels(want)
	placeLabels(result, want)
	replacements := PlaceLabelsFrom(result, base, nil, 0, nil)
	got := overlayRoutes(base, replacements)
	for index := range want {
		if got[index].LabelPlaced != want[index].LabelPlaced || got[index].LabelRect != want[index].LabelRect {
			t.Fatalf("edge %d label = (%t,%+v), want (%t,%+v)", index, got[index].LabelPlaced, got[index].LabelRect, want[index].LabelPlaced, want[index].LabelRect)
		}
	}
	if !reflect.DeepEqual(base, []*Edge{
		{ID: "one", Label: "one", Points: []Point{{X: 1, Y: 2}, {X: 18, Y: 2}}},
		{ID: "two", Label: "two", Points: []Point{{X: 1, Y: 8}, {X: 18, Y: 8}}},
	}) {
		t.Fatal("base labels were mutated")
	}
}

func TestPlaceLabelsUsesLongestCollisionFreeSegment(t *testing.T) {
	t.Parallel()

	result := &layout.Layout{Metrics: layout.Metrics{Width: 20, Height: 10}}
	routes := []*Edge{
		{ID: "labeled", Label: "status", Points: []Point{{X: 1, Y: 1}, {X: 1, Y: 7}, {X: 18, Y: 7}}},
		{ID: "blocker", Points: []Point{{X: 10, Y: 5}, {X: 10, Y: 8}}},
	}
	placeLabels(result, routes)
	if !routes[0].LabelPlaced {
		t.Fatal("label was not placed")
	}
	if geom.Contains(routes[0].LabelRect, Point{X: 10, Y: 7}) {
		t.Fatalf("label intersects blocker: %+v", routes[0].LabelRect)
	}
}

func TestLabelCandidatesUseDeterministicSegmentTieBreak(t *testing.T) {
	t.Parallel()

	candidates := labelCandidates("x", []Point{{X: 0, Y: 0}, {X: 0, Y: 6}, {X: 6, Y: 6}})
	if len(candidates) == 0 || candidates[0].segment != 0 || candidates[0].positionOrder != 0 {
		t.Fatalf("first candidate = %+v", candidates)
	}
}

func TestPlaceLabelsFallsBackBesideShortSegment(t *testing.T) {
	t.Parallel()

	result := &layout.Layout{Metrics: layout.Metrics{Width: 20, Height: 8}}
	routes := []*Edge{{
		ID: "short", Label: "long label",
		Points: []Point{{X: 5, Y: 4}, {X: 10, Y: 4}},
	}}
	placeLabels(result, routes)
	if !routes[0].LabelPlaced {
		t.Fatal("label was not placed beside the short segment")
	}
	if routes[0].LabelRect.Y == 4 {
		t.Fatalf("label was placed on a segment that is too short: %+v", routes[0].LabelRect)
	}
}

func TestPlaceLabelsSeparatesMultipleLabels(t *testing.T) {
	t.Parallel()

	result := &layout.Layout{Metrics: layout.Metrics{Width: 12, Height: 16}}
	routes := []*Edge{
		{ID: "one", Label: "one", Points: []Point{{X: 4, Y: 1}, {X: 4, Y: 14}}},
		{ID: "two", Label: "two", Points: []Point{{X: 6, Y: 1}, {X: 6, Y: 14}}},
	}
	placeLabels(result, routes)
	metrics := Analyze(result, routes)
	if !routes[0].LabelPlaced || !routes[1].LabelPlaced || metrics.LabelLabelCollisions != 0 || metrics.LabelEdgeCollisions != 0 {
		t.Fatalf("routes = %+v, metrics = %+v", routes, metrics)
	}
}

func TestLabeledSelfLoopReservesSpace(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"A --> A : loop\n", "left to right direction\nA --> A : loop\n"} {
		result := layoutDiagram(t, input)
		routes, metrics := testRoutesMetrics(result)
		if len(routes) != 1 || !routes[0].LabelPlaced || metrics.MajorCollisions() != 0 {
			t.Fatalf("input %q: routes = %+v, metrics = %+v", input, routes, metrics)
		}
	}
}

func TestInvisibleEdgeDoesNotBlockVisibleLabelOrRouting(t *testing.T) {
	t.Parallel()
	result := &layout.Layout{Metrics: layout.Metrics{Width: 9, Height: 3}}
	visible := &Edge{ID: "visible", Label: "abcde", Points: []Point{{X: 0, Y: 1}, {X: 8, Y: 1}}}
	invisible := &Edge{
		ID: "invisible", LineStyle: diagram.LineInvisible,
		Points: []Point{{X: 4, Y: 0}, {X: 4, Y: 2}},
	}
	routes := []*Edge{visible, invisible}
	placeLabels(result, routes)
	if !visible.LabelPlaced {
		t.Fatal("invisible edge blocked visible label")
	}
	if crossings, overlaps := edgeInteractions(visible, []*Edge{invisible}); crossings != 0 || overlaps != 0 {
		t.Fatalf("invisible interactions = crossings:%d overlaps:%d", crossings, overlaps)
	}
	occupancy := buildOccupancy([]*Edge{invisible})
	if len(occupancy.segments) != 0 || len(occupancy.points) != 0 {
		t.Fatalf("invisible occupancy = %+v", occupancy)
	}
}
