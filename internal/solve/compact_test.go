package solve

import (
	"os"
	"strings"
	"testing"

	"github.com/xshoji/go-text-diagram/internal/layout"
	"github.com/xshoji/go-text-diagram/internal/plantuml"
	"github.com/xshoji/go-text-diagram/internal/route"
)

func TestIoTFinalCompaction(t *testing.T) {
	input, err := os.ReadFile("../../testdata/regression/iot-edge-fleet.puml")
	if err != nil {
		t.Fatal(err)
	}
	problem, err := plantuml.ParseProblem(strings.NewReader(string(input)))
	if err != nil {
		t.Fatal(err)
	}
	result, err := ComputeProblem(problem, layout.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	// Previously 421x72: exterior straight runs pinned the group interiors.
	// Preserve the area reduction without trimming group padding to meet a height cap.
	bounds := result.Snapshot.Bounds()
	if bounds.Width > 360 || bounds.Width*bounds.Height >= 421*72 {
		t.Fatalf("group whitespace remains: bounds=%+v", bounds)
	}
	// Global bounds alone missed the 76-column gap inside Factory Site.
	var sensorRight, gatewayLeft int
	for _, node := range result.Snapshot.Nodes() {
		switch node.ID {
		case "temperature":
			sensorRight = node.Rect.X + node.Rect.Width
		case "edge_gateway":
			gatewayLeft = node.Rect.X
		}
	}
	if gap := gatewayLeft - sensorRight; gap < 0 || gap > 30 {
		t.Fatalf("Factory Site rank gap = %d, want at most 30 columns", gap)
	}
	metrics := result.Snapshot.RouteMetrics()
	if metrics.Crossings > 7 || metrics.Overlaps > 1 || metrics.Bends > 56 || metrics.ReverseMoves > 2 {
		t.Fatalf("compaction increased route complexity: %+v", metrics)
	}
	if metrics.UnroutedEdges+metrics.NodeOverlaps+metrics.EdgeNodeCollisions+metrics.GroupBoundaryOverlaps+
		metrics.LabelNodeCollisions+metrics.LabelLabelCollisions+metrics.LabelEdgeCollisions+metrics.UnplacedLabels != 0 {
		t.Fatalf("compaction introduced collisions or lost routes/labels: %+v", metrics)
	}
}

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

	if !compact(result, edges, compactInterior) {
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
