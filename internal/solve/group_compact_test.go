package solve

import (
	"os"
	"strings"
	"testing"

	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/geom"
	"github.com/xshoji/go-text-diagram/internal/layout"
	"github.com/xshoji/go-text-diagram/internal/plantuml"
	"github.com/xshoji/go-text-diagram/internal/route"
	"github.com/xshoji/go-text-diagram/internal/solution"
)

func TestCompactGroupFlowShrinksRankGap(t *testing.T) {
	data, err := os.ReadFile("../../testdata/regression/iot-edge-fleet.puml")
	if err != nil {
		t.Fatal(err)
	}
	problem, err := plantuml.ParseProblem(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	placed, err := layout.Compute(problem, layout.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	routes, metrics, _ := route.ComputeBaselineWithDiagnostics(placed)
	placed, routes, metrics, _ = prepareCandidate(placed, routes, metrics, nil)
	baseline, err := solution.FromLegacy(problem, placed, routes, metrics)
	if err != nil {
		t.Fatal(err)
	}
	before := nodeX(t, placed, "edge_gateway")
	budget := maximumGroupFlowAStarExpansions
	var diagnostics SolveDiagnostics
	compacted, snapshot, changed := compactGroupFlow(problem, placed, baseline, &diagnostics, &budget)
	if !changed {
		t.Fatal("expected group flow compaction")
	}
	if after := nodeX(t, compacted, "edge_gateway"); after >= before {
		t.Fatalf("edge_gateway did not move toward its preceding rank: before=%d after=%d", before, after)
	}
	const minimumWhitespaceReduction = 7
	beforeWhitespace := groupFlowWhitespace(problem, placed)
	if after := groupFlowWhitespace(problem, compacted); beforeWhitespace-after < minimumWhitespaceReduction {
		t.Fatalf("group whitespace reduction = %d, want at least %d", beforeWhitespace-after, minimumWhitespaceReduction)
	}
	if !groupFlowQualityNoWorse(snapshot, baseline) {
		t.Fatalf("compaction worsened route quality: before=%+v after=%+v", baseline.Quality(), snapshot.Quality())
	}
	if len(diagnostics.GroupFlow) == 0 {
		t.Fatal("expected group-flow diagnostics")
	}
}

func TestGroupRankBandsAreIndependentOfMemberOrder(t *testing.T) {
	nodes := map[string]*layout.Node{
		"right": {ID: "right", Rank: 2, Rect: geom.Rect{X: 20, Width: 3}},
		"left":  {ID: "left", Rank: 2, Rect: geom.Rect{X: 10, Width: 5}},
	}
	for range 20 {
		band := groupRankBands(map[string]bool{"right": true, "left": true}, nodes, diagram.DirectionRight)[2]
		if band != (geom.Rect{X: 10, Width: 13}) {
			t.Fatalf("unexpected rank band: %+v", band)
		}
	}
}

func TestGroupFlowCandidateDoesNotReplaceBetterCandidate(t *testing.T) {
	current := solution.Quality{Length: 100}
	best := solution.Quality{Length: 90}

	if groupFlowCandidateBetter(solution.Quality{Length: 95}, current, &best, 10, 5) {
		t.Fatal("a worse candidate replaced the current best")
	}
	if !groupFlowCandidateBetter(solution.Quality{Length: 90}, current, &best, 4, 5) {
		t.Fatal("equal-quality candidate with less group whitespace was rejected")
	}
	if !groupFlowCandidateBetter(solution.Quality{Length: 85}, current, &best, 10, 5) {
		t.Fatal("better-quality candidate was rejected")
	}
}

func nodeX(t *testing.T, placed *layout.Layout, id string) int {
	t.Helper()
	for _, node := range placed.Nodes {
		if node.ID == id {
			return node.Rect.X
		}
	}
	t.Fatalf("node %q not found", id)
	return 0
}
