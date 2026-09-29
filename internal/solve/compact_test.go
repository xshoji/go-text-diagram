package solve

import (
	"testing"

	"github.com/xshoji/agents-workspace/internal/layout"
	"github.com/xshoji/agents-workspace/internal/route"
)

func TestCompactCollapsesRepeatedGroupAndStraightRouteRows(t *testing.T) {
	result := &layout.Layout{
		Nodes: []*layout.Node{
			{ID: "top", Rect: layout.Rect{X: 2, Y: 2, Width: 5, Height: 3}},
			{ID: "bottom", Rect: layout.Rect{X: 2, Y: 14, Width: 5, Height: 3}},
		},
		Groups:  []*layout.Group{{ID: "stores", Label: "Data Stores", Rect: layout.Rect{X: 0, Y: 0, Width: 20, Height: 20}}},
		Metrics: layout.Metrics{Width: 20, Height: 20},
	}
	edges := []*route.Edge{{ID: "edge", Points: []route.Point{{X: 4, Y: 4}, {X: 4, Y: 14}}}}

	if !compact(result, edges) {
		t.Fatal("compact reported no change")
	}
	if result.Metrics.Height >= 20 || result.Groups[0].Rect.Height >= 20 {
		t.Fatalf("repeated group rows were not collapsed: bounds=%+v group=%+v", result.Metrics, result.Groups[0].Rect)
	}
	if result.Groups[0].Rect.Width < len("Data Stores")+4 {
		t.Fatalf("group label no longer fits: group=%+v", result.Groups[0].Rect)
	}
	if got := edges[0].Points; len(got) != 2 || got[0].X != got[1].X || got[1].Y-got[0].Y >= 10 {
		t.Fatalf("straight route was not compacted orthogonally: %v", got)
	}
}

func TestCompactMapKeepsOneGutterBetweenDifferentStates(t *testing.T) {
	coordinateMap, size := compactMap([]bool{true, false, false, true})
	if size != 3 || coordinateMap[3] != 2 {
		t.Fatalf("map=%v size=%d, want one retained gutter", coordinateMap, size)
	}
}
