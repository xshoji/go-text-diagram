package solve

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/geom"
	"github.com/xshoji/go-text-diagram/internal/layout"
	"github.com/xshoji/go-text-diagram/internal/plantuml"
	"github.com/xshoji/go-text-diagram/internal/route"
	"github.com/xshoji/go-text-diagram/internal/solution"
)

func transactionSnapshot(t *testing.T) *solution.Snapshot {
	t.Helper()
	problem, err := plantuml.ParseProblem(strings.NewReader("@startuml\nA --> B\n@enduml\n"))
	if err != nil {
		t.Fatal(err)
	}
	placed, err := layout.Compute(problem, layout.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	routes, metrics, _ := route.ComputeBaselineWithDiagnostics(placed)
	snapshot, err := solution.FromLegacy(problem, placed, routes, metrics)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestRejectedRouteCandidateLeavesBaseImmutable(t *testing.T) {
	base := transactionSnapshot(t)
	wantNodes, wantRoutes, wantRevision := base.Nodes(), base.Routes(), base.Revision()
	tx, err := NewTransaction(base)
	if err != nil {
		t.Fatal(err)
	}
	edge := base.Routes()[0]
	edge.Points = []geom.Point{{X: -1, Y: -1}, {X: 0, Y: 0}}
	edge.Start.Point, edge.End.Point = edge.Points[0], edge.Points[1]
	tx.ReplaceRoute(edge)
	if candidate, err := tx.CommitRoute(); err == nil || candidate != nil {
		t.Fatal("invalid route candidate was accepted")
	}
	if base.Revision() != wantRevision || !reflect.DeepEqual(base.Nodes(), wantNodes) || !reflect.DeepEqual(base.Routes(), wantRoutes) {
		t.Fatal("rejected candidate mutated base")
	}
}

func TestRejectedDiagonalRouteReturnsWithoutEvaluatingQuality(t *testing.T) {
	base := transactionSnapshot(t)
	want := base.Routes()
	tx, err := NewTransaction(base)
	if err != nil {
		t.Fatal(err)
	}
	edge := base.Routes()[0]
	edge.Points = []geom.Point{{X: 2, Y: 2}, {X: 4, Y: 3}}
	edge.Start.Point, edge.End.Point = edge.Points[0], edge.Points[1]
	tx.ReplaceRoute(edge)
	if candidate, err := tx.CommitRoute(); err == nil || candidate != nil {
		t.Fatal("diagonal route candidate was accepted")
	}
	if !reflect.DeepEqual(base.Routes(), want) || tx.Diagnostics().FullEvaluations != 0 {
		t.Fatal("invalid candidate mutated base or reached full quality evaluation")
	}
}

func TestRouteTransactionRejectsSemanticAttributeChanges(t *testing.T) {
	base := transactionSnapshot(t)
	tests := []struct {
		name string
		edit func(*solution.RoutedEdge)
	}{
		{name: "label", edit: func(edge *solution.RoutedEdge) { edge.Label = "changed" }},
		{name: "kind", edit: func(edge *solution.RoutedEdge) { edge.Kind = diagram.Undirected }},
		{name: "line style", edit: func(edge *solution.RoutedEdge) { edge.LineStyle = diagram.LineInvisible }},
		{name: "arrow style", edit: func(edge *solution.RoutedEdge) { edge.ArrowStyle = diagram.ArrowNone }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx, err := NewTransaction(base)
			if err != nil {
				t.Fatal(err)
			}
			edge := base.Routes()[0]
			test.edit(&edge)
			tx.ReplaceRoute(edge)
			if candidate, err := tx.CommitRoute(); err == nil || candidate != nil {
				t.Fatal("semantic attribute change was accepted")
			}
		})
	}
}

func TestDigitalBankFanoutReroutesWithFewerCrossings(t *testing.T) {
	input, err := os.ReadFile("../../testdata/regression/digital-bank-payments.puml")
	if err != nil {
		t.Fatal(err)
	}
	problem, err := plantuml.ParseProblem(strings.NewReader(string(input)))
	if err != nil {
		t.Fatal(err)
	}
	result, err := ComputeProblem(problem.WithDirection(diagram.DirectionDown), layout.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if crossings := result.Snapshot.RouteMetrics().Crossings; crossings >= 27 {
		t.Fatalf("route crossings = %d, want fewer than the previous 27", crossings)
	}
}

func TestDigitalBankSourcesKeepCornerClearanceAndLead(t *testing.T) {
	input, err := os.ReadFile("../../testdata/regression/digital-bank-payments.puml")
	if err != nil {
		t.Fatal(err)
	}
	problem, err := plantuml.ParseProblem(strings.NewReader(string(input)))
	if err != nil {
		t.Fatal(err)
	}
	result, err := ComputeProblem(problem.WithDirection(diagram.DirectionDown), layout.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Snapshot.Validate(); err != nil {
		t.Fatal(err)
	}

	nodes := make(map[string]solution.Node)
	for _, node := range result.Snapshot.Nodes() {
		nodes[node.ID] = node
	}
	routes := make(map[string]solution.RoutedEdge)
	for _, edge := range result.Snapshot.Routes() {
		routes[edge.ID] = edge
	}
	for _, id := range []string{"e9", "e20"} {
		edge, ok := routes[id]
		if !ok {
			t.Fatalf("route %q is missing", id)
		}
		node := nodes[edge.From]
		point := edge.Start.Point
		clearance, sideSize := 0, 0
		switch edge.Start.Side {
		case diagram.SideNorth, diagram.SideSouth:
			clearance = min(point.X-node.Rect.X, node.Rect.X+node.Rect.Width-1-point.X)
			sideSize = node.Rect.Width
		case diagram.SideEast, diagram.SideWest:
			clearance = min(point.Y-node.Rect.Y, node.Rect.Y+node.Rect.Height-1-point.Y)
			sideSize = node.Rect.Height
		default:
			t.Fatalf("route %q has unresolved source side %s", id, edge.Start.Side)
		}
		if required := min(4, (sideSize-1)/2); clearance < required {
			t.Fatalf("route %q source point %v is only %d cells from a corner; want %d", id, point, clearance, required)
		}
		if len(edge.Points) < 2 || geom.Manhattan(edge.Points[0], edge.Points[1]) < 1 {
			t.Fatalf("route %q has no source lead: %v", id, edge.Points)
		}
		distance := geom.Manhattan(edge.Points[0], edge.Points[1])
		got := geom.Point{X: edge.Points[1].X - edge.Points[0].X, Y: edge.Points[1].Y - edge.Points[0].Y}
		var want geom.Point
		switch edge.Start.Side {
		case diagram.SideNorth:
			want.Y = -distance
		case diagram.SideEast:
			want.X = distance
		case diagram.SideSouth:
			want.Y = distance
		case diagram.SideWest:
			want.X = -distance
		}
		if got != want {
			t.Fatalf("route %q starts in direction %v, want %v (auto=%t adaptive=%t side=%s): %v", id, got, want, edge.StartAuto, edge.AdaptiveStart, edge.StartSide, edge.Points)
		}
	}
}

func TestLakehouseFanInKeepsDirectEdgeAboveLongEdge(t *testing.T) {
	input, err := os.ReadFile("../../testdata/regression/data-lakehouse-ml-platform.puml")
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
	routes := result.Snapshot.Routes()
	byID := make(map[string]solution.RoutedEdge, len(routes))
	for _, edge := range routes {
		byID[edge.ID] = edge
	}
	direct, directOK := byID["e13"]
	long, longOK := byID["e20"]
	if !directOK || !longOK {
		t.Fatalf("routes missing e13/e20: %v", byID)
	}
	if direct.End.Point.Y >= long.End.Point.Y {
		t.Fatalf("metadata ports: direct=%+v long=%+v", direct.End.Point, long.End.Point)
	}
	metrics := result.Snapshot.RouteMetrics()
	if metrics.Crossings >= 23 || metrics.GroupBoundaryOverlaps != 0 {
		t.Fatalf("route metrics = %+v, want fewer than 23 crossings and no group-boundary overlaps", metrics)
	}
}

func TestHealthcareFanIn(t *testing.T) {
	input, err := os.ReadFile("../../testdata/regression/healthcare-fhir-platform.puml")
	if err != nil {
		t.Fatal(err)
	}
	problem, err := plantuml.ParseProblem(strings.NewReader(string(input)))
	if err != nil {
		t.Fatal(err)
	}
	for _, direction := range []diagram.Direction{diagram.DirectionRight, diagram.DirectionLeft, diagram.DirectionDown, diagram.DirectionUp} {
		t.Run(direction.String(), func(t *testing.T) {
			result, err := ComputeProblem(problem.WithDirection(direction), layout.DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			for _, sources := range [][2]string{{"patient", "hl7"}, {"lab", "pharmacy"}} {
				var incoming []*route.Edge
				for _, edge := range result.Snapshot.Routes() {
					if edge.To == "fhir" && (edge.From == sources[0] || edge.From == sources[1]) {
						incoming = append(incoming, &route.Edge{ID: edge.ID, Points: edge.Points})
					}
				}
				if len(incoming) != 2 {
					t.Fatalf("missing fan-in routes for %v", sources)
				}
				metrics := route.Analyze(&layout.Layout{}, incoming)
				if metrics.Crossings != 0 || metrics.Overlaps != 0 {
					t.Fatalf("%v fan-in crosses: %+v\n%s", sources, metrics, route.DebugString(incoming))
				}
			}
		})
	}
}

func TestFailedLayoutCandidateKeepsValidBaseline(t *testing.T) {
	builder := diagram.NewBuilder("test")
	ranks := []int{0, 1, 2}
	for index, id := range []string{"A", "B", "C"} {
		builder.AddNode(id, id, diagram.SourceSpan{}).Rank.Fixed = &ranks[index]
	}
	for _, endpoints := range [][2]string{{"A", "B"}, {"B", "C"}, {"C", "A"}, {"B", "A"}} {
		builder.AddEdge(diagram.NodeEndpoint(endpoints[0]), diagram.NodeEndpoint(endpoints[1]), "", diagram.Directed, diagram.SourceSpan{})
	}
	problem, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	result, diagnostics, err := ComputeProblemWithDiagnostics(problem, layout.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !strings.HasPrefix(result.Method, "baseline") || diagnostics.Layouts >= diagnostics.Candidates {
		t.Fatalf("result = %+v, diagnostics = %+v", result, diagnostics)
	}
}

func TestRouteCommitRevisionAndDeepCopy(t *testing.T) {
	base := transactionSnapshot(t)
	tx, _ := NewTransaction(base)
	edge := base.Routes()[0]
	tx.ReplaceRoute(edge)
	committed, err := tx.CommitRoute()
	if err != nil {
		t.Fatal(err)
	}
	if committed.Revision() != base.Revision()+1 {
		t.Fatalf("revision = %d", committed.Revision())
	}
	edge.Points[0].X++
	got := committed.Routes()[0]
	if reflect.DeepEqual(got.Points, edge.Points) {
		t.Fatal("committed route aliases overlay")
	}
}

func TestComputeProblemMatchesLegacyOptimizerAndBoundsAdaptiveWork(t *testing.T) {
	input, err := os.Open(filepath.Join("..", "..", "testdata", "e2e", "13-large-system.puml"))
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	problem, err := plantuml.ParseProblem(input)
	if err != nil {
		t.Fatal(err)
	}
	options := layout.DefaultOptions()
	_, diagnostics, err := ComputeProblemWithDiagnostics(problem, options)
	if err != nil {
		t.Fatal(err)
	}
	if diagnostics.Route.AdaptiveCandidates == 0 {
		t.Fatal("fixture produced no adaptive candidates")
	}
	if diagnostics.Transaction.FullEvaluations > diagnostics.Layouts {
		t.Fatalf("transaction full evaluations %d scale beyond layouts %d", diagnostics.Transaction.FullEvaluations, diagnostics.Layouts)
	}
	edges := len(problem.Edges())
	clonedRoutes := diagnostics.Route.CandidateClonedRoutes + diagnostics.ConversionClonedRoutes
	if clonedRoutes >= diagnostics.Route.AdaptiveCandidates*edges {
		t.Fatalf("candidate clones scale as full route sets: clones=%d candidates=%d edges=%d", clonedRoutes, diagnostics.Route.AdaptiveCandidates, edges)
	}
}

func TestAnnotateProblemEdgeIncludesSourceAndSemanticRelation(t *testing.T) {
	builder := diagram.NewBuilder("plantuml")
	builder.AddNode("source", "Source", diagram.Span("plantuml", 1, 1))
	builder.AddNode("target", "Target", diagram.Span("plantuml", 2, 1))
	builder.AddEdge(diagram.NodeEndpoint("source"), diagram.NodeEndpoint("target"), "request", diagram.Directed, diagram.Span("plantuml", 7, 3))
	problem, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	annotated := annotateProblemEdge(problem, fmt.Errorf(`route "e0" crosses node "blocker" interior`)).Error()
	for _, expected := range []string{"plantuml:7:3", "edge source -> target", `label "request"`, `route "e0"`} {
		if !strings.Contains(annotated, expected) {
			t.Fatalf("annotated error %q is missing %q", annotated, expected)
		}
	}
}

func TestCandidateBudgetStopsOnlyAfterBaseline(t *testing.T) {
	diagnostics := route.Diagnostics{AStarExpansions: maximumSolveAStarExpansions}
	if candidateBudgetExhausted(false, diagnostics) {
		t.Fatal("budget stopped candidates before baseline")
	}
	if !candidateBudgetExhausted(true, diagnostics) {
		t.Fatal("budget did not stop optional candidates after baseline")
	}
}

func TestCandidateOptionsDoNotHaveSizeCliff(t *testing.T) {
	builder := diagram.NewBuilder("test")
	for index := range 151 {
		id := fmt.Sprintf("node-%d", index)
		builder.AddNode(id, id, diagram.SourceSpan{})
	}
	_, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	if got := len(candidateOptionsFor()); got != 3 {
		t.Fatalf("large graph candidates = %d, want 3 budget-controlled candidates", got)
	}
}

func BenchmarkAdaptiveEvaluatorE2E(b *testing.B) {
	encoded, err := os.ReadFile(filepath.Join("..", "..", "testdata", "e2e", "13-large-system.puml"))
	if err != nil {
		b.Fatal(err)
	}
	problem, err := plantuml.ParseProblem(strings.NewReader(string(encoded)))
	if err != nil {
		b.Fatal(err)
	}
	options := layout.DefaultOptions()
	b.Run("transaction-cow", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, diagnostics, err := ComputeProblemWithDiagnostics(problem, options)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportMetric(float64(diagnostics.Route.AdaptiveCandidates), "adaptive-candidates/op")
			b.ReportMetric(float64(diagnostics.Route.CandidateClonedRoutes+diagnostics.ConversionClonedRoutes), "candidate-cloned-routes/op")
			b.ReportMetric(float64(diagnostics.Transaction.FullEvaluations), "transaction-full-evaluations/op")
		}
	})
}
