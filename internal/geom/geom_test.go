package geom

import "testing"

func TestRectangleContainmentSemantics(t *testing.T) {
	t.Parallel()

	rect := Rect{X: 1, Y: 1, Width: 3, Height: 3}
	if !Contains(rect, Point{X: 1, Y: 1}) {
		t.Fatal("Contains must include the boundary")
	}
	if ContainsInterior(rect, Point{X: 1, Y: 1}) {
		t.Fatal("ContainsInterior must exclude the boundary")
	}
	if !ContainsInterior(rect, Point{X: 2, Y: 2}) {
		t.Fatal("ContainsInterior must include the center")
	}
}

func TestFirstAvailableLaneUsesClosedIntervals(t *testing.T) {
	t.Parallel()

	lanes := [][]Interval{{{Start: 1, End: 3}}}
	if got := FirstAvailableLane(lanes, Interval{Start: 3, End: 5}); got != 1 {
		t.Fatalf("shared endpoint lane = %d, want 1", got)
	}
	if got := FirstAvailableLane(lanes, Interval{Start: 4, End: 5}); got != 0 {
		t.Fatalf("disjoint interval lane = %d, want 0", got)
	}
}
