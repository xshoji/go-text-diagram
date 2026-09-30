package route

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/layout"
)

func TestAStarAvoidsNodeObstacle(t *testing.T) {
	t.Parallel()

	result := &layout.Layout{
		Direction: diagram.DirectionRight,
		Nodes: []*layout.Node{
			{ID: "obstacle", Rect: layout.Rect{X: 4, Y: 1, Width: 3, Height: 5}},
		},
		Metrics: layout.Metrics{Width: 11, Height: 7},
	}
	edge := &Edge{ID: "edge", From: "source", To: "target", Points: []Point{{X: 1, Y: 3}, {X: 9, Y: 3}}}
	search := findPath(result, edge, nil)
	if !search.found {
		t.Fatal("A* did not find a path")
	}
	points := search.points
	if points[0] != edge.Points[0] || points[len(points)-1] != edge.Points[1] {
		t.Fatalf("path endpoints = %+v", points)
	}
	for _, point := range points {
		if point.X >= 4 && point.X <= 6 && point.Y >= 1 && point.Y <= 5 {
			t.Fatalf("path enters obstacle at %+v: %+v", point, points)
		}
	}
	assertOrthogonal(t, points)
}

func TestPathSearchBudgetIsBounded(t *testing.T) {
	t.Parallel()
	if got := pathSearchBudget(10, 10); got != 400 {
		t.Fatalf("small-grid budget = %d, want 400", got)
	}
	if got := pathSearchBudget(10_000, 10_000); got != maximumPathSearchExpansions {
		t.Fatalf("large-grid budget = %d, want %d", got, maximumPathSearchExpansions)
	}
}

func TestAStarAvoidsOccupiedRoute(t *testing.T) {
	t.Parallel()

	result := &layout.Layout{
		Direction: diagram.DirectionRight,
		Metrics:   layout.Metrics{Width: 7, Height: 5},
	}
	occupied := &Edge{ID: "occupied", Points: []Point{{X: 0, Y: 2}, {X: 6, Y: 2}}}
	edge := &Edge{ID: "candidate", Points: []Point{{X: 0, Y: 2}, {X: 6, Y: 2}}}
	search := findPath(result, edge, []*Edge{occupied})
	if !search.found {
		t.Fatal("A* did not find an alternate path")
	}
	points := search.points
	segments := make(segmentCountIndex)
	segments.addEdge(occupied)
	_, overlaps := segments.interactions(points)
	if overlaps != 0 {
		t.Fatalf("alternate path shares %d segments: %+v", overlaps, points)
	}
}

func TestSearchGridPathIsDeterministicAndReportsUnreachable(t *testing.T) {
	t.Parallel()

	grid := searchGrid{
		width: 5, height: 3, direction: diagram.DirectionRight,
		start: Point{X: 0, Y: 1}, goal: Point{X: 4, Y: 1},
		blocked:           map[Point]bool{{X: 2, Y: 1}: true},
		maximumExpansions: 64,
	}
	first, found := searchGridPath(grid)
	if !found {
		t.Fatal("expected a path around the obstacle")
	}
	second, found := searchGridPath(grid)
	if !found || !reflect.DeepEqual(first, second) {
		t.Fatalf("non-deterministic paths: %+v and %+v", first, second)
	}
	grid.blocked[Point{X: 2, Y: 0}] = true
	grid.blocked[Point{X: 2, Y: 2}] = true
	if _, found := searchGridPath(grid); found {
		t.Fatal("expected the blocked grid to be unreachable")
	}
}

func TestSearchGridPathAvoidsBlockedSegmentButAllowsPerpendicularCrossing(t *testing.T) {
	t.Parallel()

	blocked := canonicalSegment(Point{X: 2, Y: 1}, Point{X: 3, Y: 1})
	grid := searchGrid{
		width: 5, height: 3, direction: diagram.DirectionRight,
		start: Point{X: 0, Y: 1}, goal: Point{X: 4, Y: 1},
		blockedSegments:   map[unitSegment]bool{blocked: true},
		maximumExpansions: 64,
	}
	points, found := searchGridPath(grid)
	if !found {
		t.Fatal("expected a path around the blocked segment")
	}
	usedBlocked := false
	for index := 1; index < len(points); index++ {
		walkSegment(points[index-1], points[index], func(from, to Point) {
			usedBlocked = usedBlocked || canonicalSegment(from, to) == blocked
		})
	}
	if usedBlocked {
		t.Fatalf("path uses blocked segment: %v", points)
	}
	// Blocking a horizontal boundary segment must not block a vertical move
	// through one of its endpoints.
	verticalGrid := searchGrid{
		width: 5, height: 3, direction: diagram.DirectionDown,
		start: Point{X: 2, Y: 0}, goal: Point{X: 2, Y: 2},
		blockedSegments:   map[unitSegment]bool{blocked: true},
		maximumExpansions: 64,
	}
	if vertical, found := searchGridPath(verticalGrid); !found || len(vertical) != 2 {
		t.Fatalf("perpendicular crossing was blocked: %v, found=%t", vertical, found)
	}
}

func TestImproveRoutesUsesAStarForNodeCollision(t *testing.T) {
	t.Parallel()

	result := &layout.Layout{
		Direction: diagram.DirectionRight,
		Nodes: []*layout.Node{
			{ID: "obstacle", Rect: layout.Rect{X: 4, Y: 1, Width: 3, Height: 5}},
		},
		Metrics: layout.Metrics{Width: 11, Height: 7},
	}
	edge := &Edge{ID: "edge", From: "source", To: "target", Points: []Point{{X: 1, Y: 3}, {X: 9, Y: 3}}}
	improved := improveRoutes(result, []*Edge{edge}, nil, newGroupBoundaryIndex(result.Groups))
	if len(improved) != 1 || !improved[0].Fallback {
		t.Fatalf("improved routes = %+v", improved)
	}
	if metrics := Analyze(result, improved); metrics.EdgeNodeCollisions != 0 || metrics.AStarFallbacks != 1 {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestAnalyzeReportsIndependentQualityMetrics(t *testing.T) {
	t.Parallel()

	result := &layout.Layout{
		Direction: diagram.DirectionRight,
		Nodes: []*layout.Node{
			{ID: "obstacle", Rect: layout.Rect{X: 2, Y: 1, Width: 3, Height: 3}},
		},
		Edges: []*layout.Edge{
			{OriginalEdgeID: "horizontal"},
			{OriginalEdgeID: "vertical"},
		},
		Metrics: layout.Metrics{Width: 7, Height: 5},
	}
	routes := []*Edge{
		{ID: "horizontal", From: "A", To: "B", Points: []Point{{X: 0, Y: 2}, {X: 6, Y: 2}}},
		{ID: "vertical", From: "C", To: "D", Points: []Point{{X: 3, Y: 0}, {X: 3, Y: 4}}},
	}
	metrics := Analyze(result, routes)
	if metrics.EdgeNodeCollisions != 2 || metrics.Crossings == 0 || metrics.TotalLength != 10 {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestEvaluationCorpusMeetsMajorCollisionTarget(t *testing.T) {
	files, err := filepath.Glob("../layout/testdata/corpus/*.puml")
	if err != nil {
		t.Fatal(err)
	}
	clean := 0
	for _, file := range files {
		input, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		result := layoutDiagram(t, string(input))
		_, metrics := testRoutesMetrics(result)
		if metrics.MajorCollisions() == 0 {
			clean++
		}
	}
	t.Logf("major-collision-free corpus: %d/%d (%.1f%%)", clean, len(files), float64(clean)*100/float64(len(files)))
	if clean*100 < len(files)*80 {
		t.Fatalf("major-collision-free corpus = %d/%d, want at least 80%%", clean, len(files))
	}
}

func TestCrossingAwareLanePrefersLowerInteractionCost(t *testing.T) {
	t.Parallel()

	lanes := [][]interval{nil, nil}
	prior := make(segmentCountIndex)
	prior.addPoints([]Point{{X: 0, Y: 4}, {X: 8, Y: 4}})
	// Lane 0 is y=4 and overlaps the existing route; lane 1 is y=5.
	lane := crossingAwareLane(
		lanes,
		interval{Start: 2, End: 6},
		Point{X: 2, Y: 3},
		Point{X: 6, Y: 6},
		3,
		2,
		1,
		diagram.DirectionDown,
		prior,
	)
	if lane != 1 {
		t.Fatalf("lane = %d, want 1", lane)
	}
}
