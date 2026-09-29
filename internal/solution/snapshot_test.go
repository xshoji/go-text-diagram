package solution

import (
	"strings"
	"testing"

	"github.com/xshoji/agents-workspace/internal/diagram"
	"github.com/xshoji/agents-workspace/internal/geom"
	"github.com/xshoji/agents-workspace/internal/layout"
	"github.com/xshoji/agents-workspace/internal/route"
)

func TestFromLegacyPreservesDebugOutputAndOwnsCopies(t *testing.T) {
	t.Parallel()
	builder := diagram.NewBuilder("test")
	builder.AddNode("A", "A", diagram.Span("test", 1, 1))
	builder.AddNode("B", "B", diagram.Span("test", 2, 1))
	builder.AddEdge(diagram.NodeEndpoint("A"), diagram.NodeEndpoint("B"), "label", diagram.Directed, diagram.Span("test", 3, 1))
	problem, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	graph := problem
	result, err := layout.Compute(graph, layout.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	routes, metrics, _ := route.ComputeBaselineWithDiagnostics(result)
	snapshot, err := FromLegacy(problem, result, routes, metrics)
	if err != nil {
		t.Fatal(err)
	}
	wantDebug := result.DebugString() + route.DebugString(routes) + metrics.DebugString()
	if got := snapshot.DebugString(); got != wantDebug {
		t.Fatalf("debug output changed\ngot:\n%s\nwant:\n%s", got, wantDebug)
	}
	wantLabel := snapshot.Nodes()[0].Label
	wantPoint := snapshot.Routes()[0].Points[0]
	result.Nodes[0].Label = "mutated"
	routes[0].Points[0] = geom.Point{X: 99, Y: 99}
	nodes := snapshot.Nodes()
	nodes[0].Label = "caller mutation"
	routed := snapshot.Routes()
	routed[0].Points[0] = geom.Point{X: 88, Y: 88}
	if snapshot.Nodes()[0].Label != wantLabel || snapshot.Routes()[0].Points[0] != wantPoint {
		t.Fatal("legacy source or getter mutated Snapshot")
	}
}

func TestFromLegacyCandidateRejectsDiagonalBeforeQualityEvaluation(t *testing.T) {
	t.Parallel()
	builder := diagram.NewBuilder("test")
	builder.AddNode("A", "A", diagram.SourceSpan{})
	builder.AddNode("B", "B", diagram.SourceSpan{})
	builder.AddEdge(diagram.NodeEndpoint("A"), diagram.NodeEndpoint("B"), "", diagram.Directed, diagram.SourceSpan{})
	problem, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	result, err := layout.Compute(problem, layout.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	routes, metrics, _ := route.ComputeBaselineWithDiagnostics(result)
	routes[0].Points = []geom.Point{{X: 0, Y: 0}, {X: 2, Y: 1}}
	if _, err := FromLegacyCandidate(problem, result, routes, metrics); err == nil || !strings.Contains(err.Error(), "non-orthogonal") {
		t.Fatalf("error = %v, want non-orthogonal route rejection", err)
	}
}

func TestSnapshotValidatorRejectsInvalidGeometry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		edit func(*Snapshot)
		want string
	}{
		{name: "diagonal route", edit: func(s *Snapshot) { s.routes[0].Points = []geom.Point{{X: 3, Y: 2}, {X: 5, Y: 3}, {X: 10, Y: 2}} }, want: "non-orthogonal"},
		{name: "point outside bounds", edit: func(s *Snapshot) { s.routes[0].Points = []geom.Point{{X: 3, Y: 2}, {X: 20, Y: 2}, {X: 10, Y: 2}} }, want: "outside bounds"},
		{name: "invalid endpoint portal", edit: func(s *Snapshot) {
			s.routes[0].Points[0] = geom.Point{X: 4, Y: 2}
			s.routes[0].Start.Point = s.routes[0].Points[0]
		}, want: "not on endpoint"},
		{name: "route through node", edit: func(s *Snapshot) {
			s.nodes = append(s.nodes, Node{ID: "C", OriginalNodeID: "C", Rect: geom.Rect{X: 6, Y: 1, Width: 3, Height: 3}})
			s.manifest.nodes["C"] = "C"
		}, want: "crosses node"},
		{name: "group endpoint through same ID node", edit: func(s *Snapshot) {
			source := diagram.Endpoint{Kind: diagram.EndpointGroup, Group: "A"}
			s.groups = []Group{{ID: "A", Rect: geom.Rect{X: 0, Y: 0, Width: 7, Height: 7}}}
			s.manifest.groups = map[string]string{"A": ""}
			s.layoutEdges[0].Source = source
			s.routes[0].Source = source
			s.routes[0].Points = []geom.Point{{X: 0, Y: 2}, {X: 10, Y: 2}}
			s.routes[0].StartSide = diagram.SideWest
			s.routes[0].Start = ResolvedEndpoint{Semantic: source, Point: s.routes[0].Points[0], Side: diagram.SideWest}
			s.manifest.edges["e0"] = semanticEdge{source: source, target: s.routes[0].Target}
		}, want: "crosses node"},
		{name: "mismatched endpoint", edit: func(s *Snapshot) { s.routes[0].Target.Node = "A" }, want: "endpoints differ"},
		{name: "missing route", edit: func(s *Snapshot) { s.routes = nil }, want: "has no route"},
		{name: "missing semantic node placement", edit: func(s *Snapshot) { s.manifest.nodes["isolated"] = "isolated" }, want: "node count"},
		{name: "missing semantic group placement", edit: func(s *Snapshot) { s.manifest.groups["missing"] = "" }, want: "group count"},
		{name: "missing semantic layout edge", edit: func(s *Snapshot) { s.layoutEdges = nil }, want: "has no layout edge"},
		{name: "empty semantic edge ID", edit: func(s *Snapshot) { s.layoutEdges[0].OriginalEdgeID = "" }, want: "empty semantic edge ID"},
		{name: "inconsistent virtual segment endpoint", edit: func(s *Snapshot) {
			segment := s.layoutEdges[0]
			segment.ID = "e0:1"
			segment.Target.Node = "A"
			s.layoutEdges = append(s.layoutEdges, segment)
		}, want: "endpoints differ"},
		{name: "label outside bounds", edit: func(s *Snapshot) {
			s.routes[0].LabelPlaced = true
			s.routes[0].LabelRect = geom.Rect{X: 19, Y: 1, Width: 2, Height: 1}
		}, want: "label is outside"},
		{name: "resolved side mismatch", edit: func(s *Snapshot) {
			s.routes[0].StartSide = diagram.SideNorth
			s.routes[0].Start.Side = diagram.SideNorth
		}, want: "does not match resolved side"},
		{name: "cell boundary not exposed on node perimeter", edit: func(s *Snapshot) {
			source := diagram.Endpoint{Kind: diagram.EndpointNode, Node: "A", Cell: "hidden"}
			s.nodes[0].Rect = geom.Rect{X: 1, Y: 1, Width: 5, Height: 5}
			s.nodes[0].CellRects = map[string]geom.Rect{"hidden": {X: 3, Y: 2, Width: 1, Height: 1}}
			s.layoutEdges[0].Source = source
			s.routes[0].Source = source
			s.routes[0].Points = []geom.Point{{X: 1, Y: 2}, {X: 10, Y: 2}}
			s.routes[0].StartSide = diagram.SideWest
			s.routes[0].Start = ResolvedEndpoint{Semantic: source, Point: s.routes[0].Points[0], Side: diagram.SideWest}
			s.manifest.edges["e0"] = semanticEdge{source: source, target: s.routes[0].Target}
		}, want: "does not use exposed boundary"},
		{name: "bounds metrics mismatch", edit: func(s *Snapshot) { s.bounds.Width-- }, want: "differ from layout metrics"},
		{name: "invalid containment", edit: func(s *Snapshot) {
			s.groups = []Group{
				{ID: "outer", Rect: geom.Rect{X: 1, Y: 1, Width: 5, Height: 5}},
				{ID: "inner", Parent: "outer", Rect: geom.Rect{X: 10, Y: 1, Width: 3, Height: 3}},
			}
			s.manifest.groups = map[string]string{"outer": "", "inner": "outer"}
		}, want: "outside parent"},
		{name: "containment cycle", edit: func(s *Snapshot) {
			s.groups = []Group{
				{ID: "left", Parent: "right", Rect: geom.Rect{X: 1, Y: 1, Width: 5, Height: 5}},
				{ID: "right", Parent: "left", Rect: geom.Rect{X: 1, Y: 1, Width: 5, Height: 5}},
			}
			s.manifest.groups = map[string]string{"left": "right", "right": "left"}
		}, want: "containment cycle"},
		{name: "group endpoint inside instead of portal", edit: func(s *Snapshot) {
			s.groups = []Group{{ID: "group", Rect: geom.Rect{X: 4, Y: 1, Width: 5, Height: 5}}}
			s.layoutEdges[0].Source = diagram.Endpoint{Kind: diagram.EndpointGroup, Group: "group"}
			s.routes[0].Source = diagram.Endpoint{Kind: diagram.EndpointGroup, Group: "group"}
			s.routes[0].Points = []geom.Point{{X: 6, Y: 3}, {X: 10, Y: 3}}
			s.routes[0].Start = ResolvedEndpoint{Semantic: s.routes[0].Source, Point: s.routes[0].Points[0], Side: diagram.SideEast}
			s.routes[0].End.Point = s.routes[0].Points[1]
			s.manifest.groups = map[string]string{"group": ""}
			s.manifest.edges["e0"] = semanticEdge{source: s.routes[0].Source, target: s.routes[0].Target}
		}, want: "not on endpoint"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			snapshot := validSnapshot()
			test.edit(snapshot)
			if err := snapshot.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLabelIssuesReportsRenderedRouteOverlap(t *testing.T) {
	snapshot := validSnapshot()
	snapshot.routes[0].Label = "request"
	snapshot.routes[0].LabelPlaced = true
	snapshot.routes[0].LabelRect = geom.Rect{X: 6, Y: 2, Width: 2, Height: 1}
	blocker := snapshot.routes[0]
	blocker.ID = "blocker"
	blocker.Label = ""
	blocker.LabelPlaced = false
	blocker.LabelRect = geom.Rect{}
	blocker.Points = []geom.Point{{X: 7, Y: 0}, {X: 7, Y: 4}}
	snapshot.routes = append(snapshot.routes, blocker)

	issues := snapshot.LabelIssues()
	if len(issues) != 1 || issues[0].EdgeID != "e0" || !issues[0].EdgeCollision || issues[0].Unplaced {
		t.Fatalf("label issues = %+v", issues)
	}
}

func TestValidateKeepsNodeAndGroupNamespacesSeparate(t *testing.T) {
	t.Parallel()
	snapshot := validSnapshot()
	snapshot.groups = []Group{{ID: "A", Rect: geom.Rect{X: 0, Y: 0, Width: 7, Height: 7}}}
	snapshot.manifest.groups = map[string]string{"A": ""}
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateGroupBoundaryOverlapContract(t *testing.T) {
	t.Parallel()
	group := Group{ID: "group", Rect: geom.Rect{X: 4, Y: 1, Width: 5, Height: 5}}

	t.Run("rejects unrelated group boundary", func(t *testing.T) {
		snapshot := validSnapshot()
		snapshot.groups = []Group{group}
		snapshot.manifest.groups = map[string]string{"group": ""}
		snapshot.routes[0].Points = []geom.Point{{X: 3, Y: 2}, {X: 4, Y: 2}, {X: 4, Y: 5}, {X: 10, Y: 5}, {X: 10, Y: 2}}
		snapshot.routes[0].Start.Point = snapshot.routes[0].Points[0]
		snapshot.routes[0].End.Point = snapshot.routes[0].Points[len(snapshot.routes[0].Points)-1]
		if err := snapshot.Validate(); err == nil || !strings.Contains(err.Error(), "overlaps group") {
			t.Fatalf("Validate() error = %v, want group boundary overlap", err)
		}
	})

	t.Run("allows endpoint group boundary", func(t *testing.T) {
		snapshot := validSnapshot()
		source := diagram.Endpoint{Kind: diagram.EndpointGroup, Group: group.ID, NamedPort: "out"}
		snapshot.groups = []Group{group}
		snapshot.manifest.groups = map[string]string{"group": ""}
		snapshot.layoutEdges[0].Source = source
		snapshot.routes[0].Source = source
		snapshot.routes[0].Points = []geom.Point{{X: 4, Y: 2}, {X: 4, Y: 5}, {X: 3, Y: 5}, {X: 3, Y: 7}, {X: 10, Y: 7}, {X: 10, Y: 2}}
		snapshot.routes[0].StartSide = diagram.SideWest
		snapshot.routes[0].Start = ResolvedEndpoint{Semantic: source, Point: snapshot.routes[0].Points[0], Side: diagram.SideWest}
		snapshot.routes[0].End.Point = snapshot.routes[0].Points[len(snapshot.routes[0].Points)-1]
		snapshot.manifest.edges["e0"] = semanticEdge{source: source, target: snapshot.routes[0].Target}
		if err := snapshot.Validate(); err != nil {
			t.Fatalf("Validate() error = %v", err)
		}
	})

	t.Run("rejects nonterminal overlap with endpoint group", func(t *testing.T) {
		snapshot := validSnapshot()
		source := diagram.Endpoint{Kind: diagram.EndpointGroup, Group: group.ID}
		snapshot.groups = []Group{group}
		snapshot.manifest.groups = map[string]string{"group": ""}
		snapshot.layoutEdges[0].Source = source
		snapshot.routes[0].Source = source
		snapshot.routes[0].Points = []geom.Point{{X: 4, Y: 2}, {X: 3, Y: 2}, {X: 3, Y: 5}, {X: 8, Y: 5}, {X: 10, Y: 5}, {X: 10, Y: 2}}
		snapshot.routes[0].StartSide = diagram.SideWest
		snapshot.routes[0].Start = ResolvedEndpoint{Semantic: source, Point: snapshot.routes[0].Points[0], Side: diagram.SideWest}
		snapshot.routes[0].End.Point = snapshot.routes[0].Points[len(snapshot.routes[0].Points)-1]
		snapshot.manifest.edges["e0"] = semanticEdge{source: source, target: snapshot.routes[0].Target}
		if err := snapshot.Validate(); err == nil || !strings.Contains(err.Error(), "overlaps group") {
			t.Fatalf("Validate() error = %v, want nonterminal group boundary overlap", err)
		}
	})
}

func validSnapshot() *Snapshot {
	source := diagram.Endpoint{Kind: diagram.EndpointNode, Node: "A"}
	target := diagram.Endpoint{Kind: diagram.EndpointNode, Node: "B"}
	return &Snapshot{
		bounds:        geom.Rect{Width: 20, Height: 10},
		layoutMetrics: LayoutMetrics{Width: 20, Height: 10},
		nodes: []Node{
			{ID: "A", OriginalNodeID: "A", Rect: geom.Rect{X: 1, Y: 1, Width: 3, Height: 3}},
			{ID: "B", OriginalNodeID: "B", Rect: geom.Rect{X: 10, Y: 1, Width: 3, Height: 3}},
		},
		layoutEdges: []LayoutEdge{{ID: "e0", OriginalEdgeID: "e0", From: "A", To: "B", Source: source, Target: target}},
		routes: []RoutedEdge{{
			ID: "e0", From: "A", To: "B", Source: source, Target: target,
			Points: []geom.Point{{X: 3, Y: 2}, {X: 10, Y: 2}}, StartSide: diagram.SideEast, EndSide: diagram.SideWest,
			Start: ResolvedEndpoint{Semantic: source, Point: geom.Point{X: 3, Y: 2}, Side: diagram.SideEast},
			End:   ResolvedEndpoint{Semantic: target, Point: geom.Point{X: 10, Y: 2}, Side: diagram.SideWest},
		}},
		manifest: semanticManifest{
			nodes: map[string]string{"A": "A", "B": "B"}, groups: map[string]string{},
			edges: map[string]semanticEdge{"e0": {source: source, target: target}}, nodeParent: map[string]string{},
		},
	}
}
