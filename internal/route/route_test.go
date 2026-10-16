package route

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/geom"
	"github.com/xshoji/go-text-diagram/internal/layout"
	"github.com/xshoji/go-text-diagram/internal/plantuml"
)

func TestComputeJoinsLongEdge(t *testing.T) {
	t.Parallel()

	result := layoutDiagram(t, "A --> B\nB --> C\nC --> D\nA --> D\n")
	routes := testRoutes(result)
	if len(routes) != 4 {
		t.Fatalf("routes = %d, want 4", len(routes))
	}
	long := routes[3]
	if long.From != "A" || long.To != "D" {
		t.Fatalf("long route = %q -> %q", long.From, long.To)
	}
	assertOrthogonal(t, long.Points)
	for _, point := range long.Points[1 : len(long.Points)-1] {
		for _, node := range result.Nodes {
			if node.Dummy {
				continue
			}
			if point.X > node.Rect.X && point.X < node.Rect.X+node.Rect.Width-1 &&
				point.Y > node.Rect.Y && point.Y < node.Rect.Y+node.Rect.Height-1 {
				t.Fatalf("route point %+v is inside node %q", point, node.ID)
			}
		}
	}
}

func TestAutomaticPortsStayInsideNodeCorners(t *testing.T) {
	const start, size, count = 20, 12, 4
	for index := 0; index < count; index++ {
		position := distributedPortPosition(start, size, index, count, portCornerClearance)
		if position < start+portCornerClearance || position > start+size-1-portCornerClearance {
			t.Fatalf("port %d at %d is too close to boundary corner", index, position)
		}
	}

	rect := layout.Rect{X: 10, Y: 20, Width: 12, Height: 12}
	for _, side := range []diagram.Side{diagram.SideNorth, diagram.SideEast, diagram.SideSouth, diagram.SideWest} {
		for _, point := range adaptiveSideSlots(rect, side) {
			if side == diagram.SideNorth || side == diagram.SideSouth {
				if point.X < rect.X+portCornerClearance || point.X > rect.X+rect.Width-1-portCornerClearance {
					t.Fatalf("%s adaptive port %v is too close to a corner", side, point)
				}
				continue
			}
			if point.Y < rect.Y+portCornerClearance || point.Y > rect.Y+rect.Height-1-portCornerClearance {
				t.Fatalf("%s adaptive port %v is too close to a corner", side, point)
			}
		}
	}
}

func TestTwoOutgoingPortsFitCompactNode(t *testing.T) {
	const start, size = 0, 4
	clearance := automaticPortClearance(2)
	first := distributedPortPosition(start, size, 0, 2, clearance)
	second := distributedPortPosition(start, size, 1, 2, clearance)
	if first == second || first <= start || second >= start+size-1 {
		t.Fatalf("two ports overlap or touch a corner: %d, %d", first, second)
	}
}

func TestAssignIncomingPortsUsesOriginalSourceForLongEdges(t *testing.T) {
	t.Parallel()

	for _, direction := range []diagram.Direction{diagram.DirectionRight, diagram.DirectionLeft, diagram.DirectionDown, diagram.DirectionUp} {
		direction := direction
		t.Run(direction.String(), func(t *testing.T) {
			var curated, lineage, dummy, metadata layout.Node
			if direction.Vertical() {
				curated = layout.Node{ID: "curated", Rect: layout.Rect{X: 2, Y: 10, Width: 3, Height: 3}}
				lineage = layout.Node{ID: "lineage", Rect: layout.Rect{X: 12, Y: 40, Width: 3, Height: 3}}
				dummy = layout.Node{ID: "dummy", Dummy: true, Rect: layout.Rect{X: 10, Y: 25, Width: 1, Height: 1}}
				metadata = layout.Node{ID: "metadata", Rect: layout.Rect{X: 30, Y: 20, Width: 8, Height: 3}}
			} else {
				curated = layout.Node{ID: "curated", Rect: layout.Rect{X: 10, Y: 2, Width: 3, Height: 3}}
				lineage = layout.Node{ID: "lineage", Rect: layout.Rect{X: 40, Y: 12, Width: 3, Height: 3}}
				dummy = layout.Node{ID: "dummy", Dummy: true, Rect: layout.Rect{X: 25, Y: 10, Width: 1, Height: 1}}
				metadata = layout.Node{ID: "metadata", Rect: layout.Rect{X: 20, Y: 30, Width: 3, Height: 8}}
			}
			nodes := map[string]*layout.Node{
				curated.ID: &curated, lineage.ID: &lineage, dummy.ID: &dummy, metadata.ID: &metadata,
			}
			direct := &layout.Edge{
				ID: "e13", From: "curated", To: "metadata", OriginalFrom: "curated", OriginalTo: "metadata",
				Source: diagram.NodeEndpoint("curated"), Target: diagram.NodeEndpoint("metadata"),
			}
			long := &layout.Edge{
				ID: "e20:1", From: "dummy", To: "metadata", OriginalFrom: "lineage", OriginalTo: "metadata",
				Source: diagram.NodeEndpoint("lineage"), Target: diagram.NodeEndpoint("metadata"),
			}
			_, ends := assignPorts([]*layout.Edge{direct, long}, nodes, direction)
			if directEnd, longEnd := crossAxisPosition(ends[direct], direction), crossAxisPosition(ends[long], direction); directEnd >= longEnd {
				t.Fatalf("incoming ports = %d,%d, want curated before lineage", directEnd, longEnd)
			}
		})
	}
}

func TestIncomingSourceOrderRequiresAutomaticUniqueEndpoints(t *testing.T) {
	nodes := map[string]*layout.Node{
		"curated": {ID: "curated"},
		"lineage": {ID: "lineage"},
		"dummy":   {ID: "dummy", Dummy: true},
		"target":  {ID: "target"},
	}
	newEdges := func() []*layout.Edge {
		return []*layout.Edge{
			{ID: "direct", From: "curated", To: "target", OriginalFrom: "curated", OriginalTo: "target", Source: diagram.NodeEndpoint("curated"), Target: diagram.NodeEndpoint("target")},
			{ID: "long", From: "dummy", To: "target", OriginalFrom: "lineage", OriginalTo: "target", Source: diagram.NodeEndpoint("lineage"), Target: diagram.NodeEndpoint("target")},
		}
	}

	tests := []struct {
		name string
		edit func([]*layout.Edge)
	}{
		{name: "parallel source", edit: func(edges []*layout.Edge) { edges[1].OriginalFrom = "curated" }},
		{name: "explicit source side", edit: func(edges []*layout.Edge) { edges[1].Source.PortHint.Side = diagram.SideNorth }},
		{name: "cell target", edit: func(edges []*layout.Edge) { edges[1].Target.Cell = "id" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			edges := newEdges()
			test.edit(edges)
			if incomingPortsUseOriginalSource(edges, nodes) {
				t.Fatal("ambiguous or constrained incoming bundle used semantic-source order")
			}
		})
	}
}

func TestLakehouseDirectAndLongFanInDoNotCross(t *testing.T) {
	t.Parallel()
	input, err := os.ReadFile("../../testdata/regression/data-lakehouse-ml-platform.puml")
	if err != nil {
		t.Fatal(err)
	}
	routes, _ := testBaselineMetrics(layoutDiagram(t, string(input)))
	byID := make(map[string]*Edge, len(routes))
	for _, edge := range routes {
		byID[edge.ID] = edge
	}
	direct, long := byID["e13"], byID["e20"]
	if direct == nil || long == nil {
		t.Fatalf("routes missing e13/e20: %s", DebugString(routes))
	}
	if crossings, overlaps := edgeInteractions(direct, []*Edge{long}); crossings != 0 || overlaps != 0 {
		t.Fatalf("direct/long fan-in has %d crossings and %d overlaps:\n%s", crossings, overlaps, DebugString([]*Edge{direct, long}))
	}
}

func TestComputeKeepsVirtualAndSemanticEdgeIDsSeparate(t *testing.T) {
	t.Parallel()
	builder := diagram.NewBuilder("test")
	rank0, rank1, rank2 := 0, 1, 2
	a := builder.AddNode("A", "A", diagram.SourceSpan{})
	x := builder.AddNode("X", "X", diagram.SourceSpan{})
	b := builder.AddNode("B", "B", diagram.SourceSpan{})
	c := builder.AddNode("C", "C", diagram.SourceSpan{})
	a.Rank.Fixed, x.Rank.Fixed, b.Rank.Fixed, c.Rank.Fixed = &rank0, &rank0, &rank1, &rank2
	long := builder.AddEdge(diagram.NodeEndpoint("A"), diagram.NodeEndpoint("C"), "", diagram.Directed, diagram.SourceSpan{})
	long.ID, long.MinLength = "e0", 2
	semantic := builder.AddEdge(diagram.NodeEndpoint("X"), diagram.NodeEndpoint("B"), "", diagram.Directed, diagram.SourceSpan{})
	semantic.ID = "e0:0"
	problem, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	placed, err := layout.Compute(problem, layout.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	routes := computeInitial(placed, nil)
	var longRoute *Edge
	for _, edge := range routes {
		if edge.ID == "e0" {
			longRoute = edge
		}
	}
	var sourceRect layout.Rect
	for _, node := range placed.Nodes {
		if node.ID == "A" {
			sourceRect = node.Rect
		}
	}
	if longRoute == nil || len(longRoute.Points) < 2 || !onRectBoundary(sourceRect, longRoute.Points[0]) {
		t.Fatalf("long route used colliding semantic segment: %+v", longRoute)
	}
}

func TestComputeUsesSeparateLanesForOverlappingIntervals(t *testing.T) {
	t.Parallel()

	lanes := [][]interval{{{Start: 2, End: 5}}}
	if got := geom.FirstAvailableLane(lanes, interval{Start: 4, End: 7}); got != 1 {
		t.Fatalf("overlapping interval lane = %d, want 1", got)
	}
	if got := geom.FirstAvailableLane(lanes, interval{Start: 6, End: 8}); got != 0 {
		t.Fatalf("disjoint interval lane = %d, want 0", got)
	}
}

func TestComputeAvoidsCrossingsForConvergingEdges(t *testing.T) {
	t.Parallel()

	result := &layout.Layout{
		Direction: diagram.DirectionRight,
		Nodes: []*layout.Node{
			{ID: "app", Rank: 0, Rect: layout.Rect{X: 2, Y: 0, Width: 22, Height: 3}},
			{ID: "merchant", Rank: 0, Rect: layout.Rect{X: 2, Y: 6, Width: 12, Height: 3}},
			{ID: "open_banking", Rank: 0, Rect: layout.Rect{X: 2, Y: 12, Width: 25, Height: 3}},
			{ID: "gateway", Rank: 1, Rect: layout.Rect{X: 48, Y: 42, Width: 15, Height: 8}},
		},
		Edges: []*layout.Edge{
			{ID: "e0", OriginalEdgeID: "e0", From: "app", To: "gateway", OriginalFrom: "app", OriginalTo: "gateway"},
			{ID: "e1", OriginalEdgeID: "e1", From: "merchant", To: "gateway", OriginalFrom: "merchant", OriginalTo: "gateway"},
			{ID: "e2", OriginalEdgeID: "e2", From: "open_banking", To: "gateway", OriginalFrom: "open_banking", OriginalTo: "gateway"},
		},
		Metrics:       layout.Metrics{Width: 65, Height: 51},
		LabelLaneSize: 1,
	}

	routes, metrics := testBaselineMetrics(result)
	if metrics.Crossings != 0 {
		t.Fatalf("converging routes have %d crossings, want 0:\n%s", metrics.Crossings, DebugString(routes))
	}
	lanes := make(map[string]int, len(routes))
	for _, edge := range routes {
		lanes[edge.ID] = edge.Lane
	}
	if lanes["e0"] != 2 || lanes["e1"] != 1 || lanes["e2"] != 0 {
		t.Fatalf("converging lanes = %v, want e0=2 e1=1 e2=0", lanes)
	}
}

func TestComputeKeepsDenseGroupFanInCrossingFree(t *testing.T) {
	const input = `left to right direction
component "ui" as ui
component "act" as act
component "component1" as c1
component "component2" as c2
component "component3" as c3
component "api" as api
component "mq <<mq>>" as mq
component "internal-tool" as internal
package "package1" {
  component "tool1" as tool1
  component "publisher1" as publisher1
  component "subscriber1" as subscriber1
}
ui --> tool1 : [ read / write ]\n/api/SampleService/aaa,bbb
act --> tool1 : [ write ]\n/api/SampleService/aaa
c1 --> tool1 : [ read ]\n/api/SampleService/aaa\n...etc
c2 --> tool1 : [ read ]\n/api/SampleService/aaa\n...etc
c3 --> tool1 : [ read ]\n/api/SampleService/aaa
internal --> tool1 : [ read / write ]\n/api/SampleService/aaa
`
	result := layoutDiagram(t, input)
	routes, metrics := testBaselineMetrics(result)
	if metrics.Crossings != 0 || metrics.LabelEdgeCollisions != 0 || metrics.AStarFallbacks != 0 {
		t.Fatalf("fan-in route quality = %+v; routes:\n%s", metrics, DebugString(routes))
	}
	nodes := make(map[string]*layout.Node, len(result.Nodes))
	for _, node := range result.Nodes {
		nodes[node.ID] = node
	}
	if nodes["internal"].Rect.Y >= nodes["api"].Rect.Y || nodes["internal"].Rect.Y >= nodes["mq"].Rect.Y {
		t.Fatalf("internal-tool position = %v; api = %v; mq = %v", nodes["internal"].Rect, nodes["api"].Rect, nodes["mq"].Rect)
	}
}

func TestOrderConvergingEdgesChoosesNestingForEveryDirection(t *testing.T) {
	t.Parallel()

	for _, direction := range []diagram.Direction{diagram.DirectionRight, diagram.DirectionLeft, diagram.DirectionDown, diagram.DirectionUp} {
		direction := direction
		for _, test := range []struct {
			name   string
			starts []int
			ends   []int
		}{
			{name: "targets-after-sources", starts: []int{1, 7, 13}, ends: []int{43, 45, 48}},
			{name: "targets-before-sources", starts: []int{49, 43, 37}, ends: []int{6, 3, 1}},
		} {
			test := test
			t.Run(fmt.Sprintf("%s/%s", direction, test.name), func(t *testing.T) {
				edges := []*layout.Edge{
					{ID: "e0", From: "a", To: "gateway", OriginalFrom: "a"},
					{ID: "e1", From: "b", To: "gateway", OriginalFrom: "b"},
					{ID: "e2", From: "c", To: "gateway", OriginalFrom: "c"},
				}
				starts, ends := make(map[*layout.Edge]Point), make(map[*layout.Edge]Point)
				for index, edge := range edges {
					if direction.Vertical() {
						starts[edge], ends[edge] = Point{X: test.starts[index]}, Point{X: test.ends[index]}
					} else {
						starts[edge], ends[edge] = Point{Y: test.starts[index]}, Point{Y: test.ends[index]}
					}
				}

				orderConvergingEdges(edges, starts, ends, direction)
				if edges[0].ID != "e2" || edges[1].ID != "e1" || edges[2].ID != "e0" {
					t.Fatalf("edge order = %s,%s,%s, want e2,e1,e0", edges[0].ID, edges[1].ID, edges[2].ID)
				}
			})
		}
	}
}

func TestOrderConvergingEdgesPreservesLongParallelEdges(t *testing.T) {
	t.Parallel()

	edges := []*layout.Edge{
		{ID: "e0:1", From: "dummy0", To: "target", OriginalFrom: "source"},
		{ID: "e1:1", From: "dummy1", To: "target", OriginalFrom: "source"},
	}
	starts := map[*layout.Edge]Point{edges[0]: {Y: 1}, edges[1]: {Y: 7}}
	ends := map[*layout.Edge]Point{edges[0]: {Y: 43}, edges[1]: {Y: 45}}

	orderConvergingEdges(edges, starts, ends, diagram.DirectionRight)
	if edges[0].ID != "e0:1" || edges[1].ID != "e1:1" {
		t.Fatalf("parallel edge order changed to %s,%s", edges[0].ID, edges[1].ID)
	}
}

func TestOrderConvergingEdgesPreservesLongEdgeWithExplicitSourcePort(t *testing.T) {
	t.Parallel()

	edges := []*layout.Edge{
		{ID: "e0:1", From: "dummy0", To: "target", OriginalFrom: "source0", Source: diagram.Endpoint{PortHint: diagram.PortHint{Side: diagram.SideNorth}}},
		{ID: "e1:1", From: "dummy1", To: "target", OriginalFrom: "source1"},
	}
	starts := map[*layout.Edge]Point{edges[0]: {Y: 1}, edges[1]: {Y: 7}}
	ends := map[*layout.Edge]Point{edges[0]: {Y: 43}, edges[1]: {Y: 45}}

	orderConvergingEdges(edges, starts, ends, diagram.DirectionRight)
	if edges[0].ID != "e0:1" || edges[1].ID != "e1:1" {
		t.Fatalf("explicit-port edge order changed to %s,%s", edges[0].ID, edges[1].ID)
	}
}

func TestComputeRestoresCycleDirectionAndRoutesLoops(t *testing.T) {
	t.Parallel()

	result := layoutDiagram(t, "A --> B\nB --> C\nC --> A\nB --> B\n")
	routes := testRoutes(result)
	if len(routes) != 4 {
		t.Fatalf("routes = %d, want 4", len(routes))
	}
	cycle, loop := routes[2], routes[3]
	if cycle.From != "C" || cycle.To != "A" || !cycle.Reversed {
		t.Fatalf("cycle route = %+v", cycle)
	}
	if loop.From != "B" || loop.To != "B" || !loop.SelfLoop {
		t.Fatalf("loop route = %+v", loop)
	}
	assertOrthogonal(t, cycle.Points)
	assertOrthogonal(t, loop.Points)
}

func TestComputeSeparatesParallelEdgesAndBidirectionalRoutes(t *testing.T) {
	t.Parallel()

	parallelResult := layoutDiagram(t, "A -> B\nA -> B\nA -> B\nA -> B\n")
	parallel := testRoutes(parallelResult)
	if len(parallel) != 4 {
		t.Fatalf("parallel routes = %d, want 4", len(parallel))
	}
	starts := make(map[Point]bool)
	ends := make(map[Point]bool)
	for _, edge := range parallel {
		starts[edge.Points[0]] = true
		ends[edge.Points[len(edge.Points)-1]] = true
	}
	if len(starts) != 4 || len(ends) != 4 {
		t.Fatalf("parallel ports: starts=%v ends=%v", starts, ends)
	}

	bidirectionalResult := layoutDiagram(t, "A -> B\nB -> A\n")
	bidirectional := testRoutes(bidirectionalResult)
	if len(bidirectional) != 2 || bidirectional[0].Reversed || !bidirectional[1].Reversed {
		t.Fatalf("bidirectional routes = %+v", bidirectional)
	}
	if bidirectional[1].From != "B" || bidirectional[1].To != "A" {
		t.Fatalf("backward route direction = %q -> %q", bidirectional[1].From, bidirectional[1].To)
	}
}

func TestComputeRightwardRoutesAreOrthogonal(t *testing.T) {
	t.Parallel()

	result := layoutDiagram(t, "left to right direction\nA --> B\nA --> C\n")
	for _, edge := range testRoutes(result) {
		assertOrthogonal(t, edge.Points)
	}
}

func TestComputeHonorsExplicitPorts(t *testing.T) {
	t.Parallel()
	graph := diagram.NewBuilder("test")
	graph.AddNode("A", "A", diagram.SourceSpan{})
	graph.AddNode("B", "B", diagram.SourceSpan{})
	edgeModel := graph.AddEdge(diagram.NodeEndpoint("A"), diagram.NodeEndpoint("B"), "", diagram.Directed, diagram.SourceSpan{})
	edgeModel.Start.Side, edgeModel.End.Side = diagram.SideNorth, diagram.SideEast
	edgeModel.Source.PortHint, edgeModel.Target.PortHint = edgeModel.Start, edgeModel.End
	result, err := layout.Compute(mustBuildProblem(t, graph), layout.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	routes := testRoutes(result)
	if len(routes) != 1 {
		t.Fatalf("routes = %d", len(routes))
	}
	edge := routes[0]
	if edge.StartSide != diagram.SideNorth || edge.EndSide != diagram.SideEast {
		t.Fatalf("sides = %s -> %s", edge.StartSide, edge.EndSide)
	}
	if len(edge.Points) < 4 || !(edge.Points[1].Y < edge.Points[0].Y) ||
		!(edge.Points[len(edge.Points)-2].X > edge.Points[len(edge.Points)-1].X) {
		t.Fatalf("points do not honor north/east ports: %v", edge.Points)
	}
	assertRoutes(t, result, routes)
}

func TestComputeResolvesFlowRelativePorts(t *testing.T) {
	t.Parallel()
	graph := diagram.NewBuilder("test")
	graph.SetDirection(diagram.DirectionLeft)
	graph.AddNode("A", "A", diagram.SourceSpan{})
	graph.AddNode("B", "B", diagram.SourceSpan{})
	edgeModel := graph.AddEdge(diagram.NodeEndpoint("A"), diagram.NodeEndpoint("B"), "", diagram.Directed, diagram.SourceSpan{})
	edgeModel.Start.Side, edgeModel.End.Side = diagram.SideFront, diagram.SideRight
	edgeModel.Source.PortHint, edgeModel.Target.PortHint = edgeModel.Start, edgeModel.End
	result, err := layout.Compute(mustBuildProblem(t, graph), layout.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	edge := testRoutes(result)[0]
	if edge.StartSide != diagram.SideWest || edge.EndSide != diagram.SideNorth {
		t.Fatalf("relative sides = %s -> %s, want west -> north", edge.StartSide, edge.EndSide)
	}
}

func TestComputeRoutesNamedCellsThroughTheirOwnExposedBoundary(t *testing.T) {
	t.Parallel()
	result := layoutDiagram(t, "allowmixing\nleft to right direction\nclass record {\nid : 識別子\nname : Name\n}\ncomponent target\nrecord::id --> target\ntarget --> record::name\n")
	routes := testRoutes(result)
	if len(routes) != 2 {
		t.Fatalf("routes = %d", len(routes))
	}
	var record *layout.Node
	for _, node := range result.Nodes {
		if node.ID == "record" {
			record = node
		}
	}
	id, idOK := record.CellRect("id")
	name, nameOK := record.CellRect("name")
	if !idOK || !nameOK {
		t.Fatalf("cell rectangles: id=%+v/%t name=%+v/%t", id, idOK, name, nameOK)
	}
	start := routes[0].Points[0]
	end := routes[1].Points[len(routes[1].Points)-1]
	idBoundary := start.Y == record.Rect.Y && start.X >= id.X && start.X < id.X+id.Width ||
		(start.X == record.Rect.X || start.X == record.Rect.X+record.Rect.Width-1) && start.Y == id.Y
	if !idBoundary {
		t.Fatalf("id route starts outside its exposed boundary: cell=%+v point=%+v", id, start)
	}
	nameBoundary := end.Y == record.Rect.Y && end.X >= name.X && end.X < name.X+name.Width ||
		(end.X == record.Rect.X || end.X == record.Rect.X+record.Rect.Width-1) && end.Y == name.Y
	if !nameBoundary {
		t.Fatalf("name route ends outside its exposed boundary: cell=%+v point=%+v", name, end)
	}
	for _, edge := range routes {
		for i := 1; i < len(edge.Points); i++ {
			walkPoints(edge.Points[i-1], edge.Points[i], func(point Point) {
				if point != edge.Points[0] && point != edge.Points[len(edge.Points)-1] && geom.Contains(record.Rect, point) {
					t.Errorf("cell route %q touches record outside its port at %v: %v", edge.ID, point, edge.Points)
				}
			})
		}
	}
}

func TestComputeClipsNestedGroupEndpointsToGroupBoundaries(t *testing.T) {
	t.Parallel()
	result := layoutDiagram(t, "package outer {\ncomponent outside\npackage inner {\ncomponent inside\n}\n}\nouter --> outside\noutside --> inner\n")
	routes := testRoutes(result)
	groups := make(map[string]layout.Rect)
	for _, group := range result.Groups {
		groups[group.ID] = group.Rect
	}
	if len(routes) != 2 || !onRectBoundary(groups["outer"], routes[0].Points[0]) ||
		!onRectBoundary(groups["inner"], routes[1].Points[len(routes[1].Points)-1]) {
		t.Fatalf("group routes were not clipped: groups=%+v\n%s", groups, DebugString(routes))
	}
	if routes[0].From != "group:outer" || routes[1].To != "group:inner" {
		t.Fatalf("structured endpoint identities were lost: %+v", routes)
	}
}

func TestCorpusRoutesStayInsideBoundsAndOutsideNodes(t *testing.T) {
	files, err := filepath.Glob("../layout/testdata/corpus/*.puml")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 20 {
		t.Fatalf("corpus has %d inputs, want at least 20", len(files))
	}

	for _, file := range files {
		file := file
		t.Run(filepath.Base(file), func(t *testing.T) {
			t.Parallel()
			input, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			result := layoutDiagram(t, string(input))
			routes := testRoutes(result)
			assertRoutes(t, result, routes)
		})
	}
}

func TestRandomDirectedGraphsPreserveRouteInvariants(t *testing.T) {
	random := rand.New(rand.NewSource(42))
	for graphIndex := 0; graphIndex < 50; graphIndex++ {
		graph := diagram.NewBuilder("test")
		if graphIndex%2 == 1 {
			graph.SetDirection(diagram.DirectionRight)
		}
		for node := 0; node < 8; node++ {
			id := fmt.Sprintf("n%d", node)
			graph.AddNode(id, id, diagram.SourceSpan{})
		}
		for edge := 0; edge < 20; edge++ {
			from := fmt.Sprintf("n%d", random.Intn(8))
			to := fmt.Sprintf("n%d", random.Intn(8))
			graph.AddEdge(diagram.NodeEndpoint(from), diagram.NodeEndpoint(to), "", diagram.Directed, diagram.SourceSpan{})
		}
		result, err := layout.Compute(mustBuildProblem(t, graph), layout.DefaultOptions())
		if err != nil {
			t.Fatalf("graph %d: %v", graphIndex, err)
		}
		routes := testRoutes(result)
		if len(routes) != len(graph.Edges()) {
			t.Fatalf("graph %d: routes = %d, want %d", graphIndex, len(routes), len(graph.Edges()))
		}
		assertRoutes(t, result, routes)
	}
}

func TestPackedComponentsRouteWithinTheirOwnRanks(t *testing.T) {
	t.Parallel()

	graph, err := plantuml.ParseProblem(plantUMLReader("A --> B\nA --> B\nA --> B\ncomponent C\n"))
	if err != nil {
		t.Fatal(err)
	}
	options := layout.DefaultOptions()
	options.MaxWidth = 10
	result, err := layout.Compute(graph, options)
	if err != nil {
		t.Fatal(err)
	}
	routes := testRoutes(result)
	assertRoutes(t, result, routes)
	if metrics := Analyze(result, routes); metrics.EdgeNodeCollisions != 0 {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestLongRoutesDoNotFollowDisplacedGroupDummyNodes(t *testing.T) {
	input, err := os.ReadFile("testdata/aws_webapp_component.puml")
	if err != nil {
		t.Fatal(err)
	}
	result := layoutDiagram(t, string(input))
	routes, metrics := testRoutesMetrics(result)

	wantOptimized := map[string]bool{"e2": true, "e8": true, "e11": true, "e16": true}
	for _, edge := range routes {
		if wantOptimized[edge.ID] && (!edge.Long || !edge.Optimized) {
			t.Errorf("edge %s was not optimized: %+v", edge.ID, edge)
		}
	}
	if metrics.EdgeNodeCollisions != 0 || metrics.GroupBoundaryOverlaps != 0 || metrics.UnroutedEdges != 0 {
		t.Fatalf("long-route optimization introduced a structural regression: %+v", metrics)
	}
	if metrics.Crossings > 26 || metrics.Overlaps > 10 {
		t.Fatalf("long-route optimization increased route interactions: %+v", metrics)
	}
	if metrics.ExcessLength > 10 {
		t.Fatalf("excess route length = %d, want at most 10\n%s", metrics.ExcessLength, DebugString(routes))
	}
}

func TestLongRouteRetryAvoidsStaleDummyPath(t *testing.T) {
	input, err := os.ReadFile("../../testdata/regression/iot-edge-fleet.puml")
	if err != nil {
		t.Fatal(err)
	}
	result := layoutDiagram(t, string(input))
	routes, metrics := testRoutesMetrics(result)
	for _, edge := range routes {
		if edge.ID != "e1" {
			continue
		}
		if !edge.Optimized || len(edge.Points) > 4 {
			t.Fatalf("vibration-to-gateway route = %+v; want an optimized route with at most 4 points", edge)
		}
		if metrics.EdgeNodeCollisions != 0 || metrics.GroupBoundaryOverlaps != 0 || metrics.UnroutedEdges != 0 {
			t.Fatalf("route metrics = %+v", metrics)
		}
		return
	}
	t.Fatal("route e1 not found")
}

func BenchmarkRoute50Nodes100Edges(b *testing.B) {
	random := rand.New(rand.NewSource(42))
	graph := diagram.NewBuilder("test")
	for node := 0; node < 50; node++ {
		id := fmt.Sprintf("n%d", node)
		graph.AddNode(id, id, diagram.SourceSpan{})
	}
	for edge := 0; edge < 100; edge++ {
		from := random.Intn(49)
		to := from + 1 + random.Intn(50-from-1)
		graph.AddEdge(diagram.NodeEndpoint(fmt.Sprintf("n%d", from)), diagram.NodeEndpoint(fmt.Sprintf("n%d", to)), "", diagram.Directed, diagram.SourceSpan{})
	}
	result, err := layout.Compute(mustBuildProblem(b, graph), layout.DefaultOptions())
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		routes, metrics := testRoutesMetrics(result)
		if len(routes) != len(graph.Edges()) || metrics.UnroutedEdges != 0 {
			b.Fatalf("routes=%d metrics=%+v", len(routes), metrics)
		}
	}
}

func layoutDiagram(t *testing.T, input string) *layout.Layout {
	t.Helper()
	graph, err := plantuml.ParseProblem(plantUMLReader(input))
	if err != nil {
		t.Fatal(err)
	}
	result, err := layout.Compute(graph, layout.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func plantUMLReader(input string) *strings.Reader {
	if strings.HasPrefix(strings.TrimSpace(input), "@startuml") {
		return strings.NewReader(input)
	}
	return strings.NewReader("@startuml\n" + input + "@enduml\n")
}

func assertOrthogonal(t *testing.T, points []Point) {
	t.Helper()
	if len(points) < 2 {
		t.Fatalf("route has %d points", len(points))
	}
	for i := 1; i < len(points); i++ {
		if points[i-1].X != points[i].X && points[i-1].Y != points[i].Y {
			t.Fatalf("segment %d is diagonal: %+v -> %+v", i, points[i-1], points[i])
		}
	}
}

func assertRoutes(t *testing.T, result *layout.Layout, routes []*Edge) {
	t.Helper()
	for _, edge := range routes {
		assertOrthogonal(t, edge.Points)
		for index := 1; index < len(edge.Points); index++ {
			visitSegment(edge.Points[index-1], edge.Points[index], func(point Point) {
				if point.X < 0 || point.X >= result.Metrics.Width || point.Y < 0 || point.Y >= result.Metrics.Height {
					t.Fatalf("edge %q leaves bounds at %+v", edge.ID, point)
				}
				for _, node := range result.Nodes {
					if node.Dummy || node.ID == edge.From || node.ID == edge.To {
						continue
					}
					if point.X > node.Rect.X && point.X < node.Rect.X+node.Rect.Width-1 &&
						point.Y > node.Rect.Y && point.Y < node.Rect.Y+node.Rect.Height-1 {
						t.Fatalf("edge %q enters node %q at %+v", edge.ID, node.ID, point)
					}
				}
			})
		}
	}
}

func visitSegment(from, to Point, visit func(Point)) {
	dx, dy := routeSign(to.X-from.X), routeSign(to.Y-from.Y)
	point := from
	for {
		visit(point)
		if point == to {
			return
		}
		point.X += dx
		point.Y += dy
	}
}

func routeSign(value int) int {
	if value < 0 {
		return -1
	}
	if value > 0 {
		return 1
	}
	return 0
}

func mustBuildProblem(t testing.TB, builder *diagram.Builder) *diagram.Problem {
	t.Helper()
	problem, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	return problem
}

func testRoutes(result *layout.Layout) []*Edge {
	routes, _, _ := ComputeBaselineWithDiagnostics(result)
	return routes
}

func testRoutesMetrics(result *layout.Layout) ([]*Edge, Metrics) {
	routes, metrics, _ := ComputeBaselineWithDiagnostics(result)
	return routes, metrics
}

func testBaselineMetrics(result *layout.Layout) ([]*Edge, Metrics) {
	routes, metrics, _ := ComputeBaselineWithDiagnostics(result)
	return routes, metrics
}
