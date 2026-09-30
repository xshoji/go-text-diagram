package layout

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/plantuml"
)

func TestLayoutLongEdgeInsertsDummyNodes(t *testing.T) {
	t.Parallel()

	result := layoutInput(t, "A --> B\nB --> C\nC --> D\nA --> D\n")
	if got, want := result.Metrics.DummyNodes, 2; got != want {
		t.Fatalf("dummy nodes = %d, want %d", got, want)
	}
	if got, want := result.Metrics.LongEdges, 1; got != want {
		t.Fatalf("long edges = %d, want %d", got, want)
	}

	segments := 0
	for _, edge := range result.Edges {
		if edge.OriginalEdgeID == "e3" {
			segments++
		}
	}
	if segments != 3 {
		t.Fatalf("long edge segments = %d, want 3", segments)
	}
}

func TestBarycenterRemovesCrossing(t *testing.T) {
	t.Parallel()

	result := layoutInput(t, "component A\ncomponent B\ncomponent C\ncomponent D\nA -> D\nB -> C\n")
	if result.Metrics.Crossings != 0 {
		t.Fatalf("crossings = %d, want 0", result.Metrics.Crossings)
	}

	var rankOne []string
	for _, node := range result.Nodes {
		if node.Rank == 1 {
			rankOne = append(rankOne, node.ID)
		}
	}
	if got, want := strings.Join(rankOne, ","), "D,C"; got != want {
		t.Fatalf("rank 1 order = %s, want %s", got, want)
	}
}

func TestMedianPositionAndTranspose(t *testing.T) {
	t.Parallel()

	if got, want := medianPosition([]int{4, 0, 2}), 4; got != want {
		t.Fatalf("median position = %d, want %d", got, want)
	}
	if got, want := medianPosition([]int{4, 0}), 4; got != want {
		t.Fatalf("even median position = %d, want %d", got, want)
	}

	a := &workingNode{id: "A", rank: 0, order: 0}
	b := &workingNode{id: "B", rank: 0, order: 1}
	c := &workingNode{id: "C", rank: 1, order: 0}
	d := &workingNode{id: "D", rank: 1, order: 1}
	layers := [][]*workingNode{{a, b}, {c, d}}
	edges := []*workingEdge{{from: a, to: d}, {from: b, to: c}}
	if crossings := countCrossings(layers, edges); crossings != 1 {
		t.Fatalf("initial crossings = %d, want 1", crossings)
	}
	transposeLayers(layers, edges)
	if crossings := countCrossings(layers, edges); crossings != 0 {
		t.Fatalf("transposed crossings = %d, want 0", crossings)
	}
}

func TestLayoutReversesCycleEdge(t *testing.T) {
	t.Parallel()

	graph, err := plantuml.ParseProblem(plantUMLReader("A --> B\nB --> C\nC --> A\n"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Compute(graph, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if result.Metrics.ReversedEdges != 1 {
		t.Fatalf("reversed edges = %d, want 1", result.Metrics.ReversedEdges)
	}
	for _, edge := range result.Edges {
		if edge.OriginalEdgeID == "e3" && !edge.Reversed {
			t.Fatalf("cycle edge is not marked reversed: %+v", edge)
		}
	}
}

func TestLayoutSeparatesSelfLoops(t *testing.T) {
	t.Parallel()

	result := layoutInput(t, "A -> A\nA -> A\n")
	if result.Metrics.SelfLoops != 2 {
		t.Fatalf("self loops = %d, want 2", result.Metrics.SelfLoops)
	}
	for _, edge := range result.Edges {
		if !edge.SelfLoop || edge.From != "A" || edge.To != "A" {
			t.Fatalf("unexpected loop edge: %+v", edge)
		}
	}
}

func TestLayoutBidirectionalEdgeReversesOneDirection(t *testing.T) {
	t.Parallel()

	result := layoutInput(t, "A -> B\nB -> A\n")
	if result.Metrics.ReversedEdges != 1 {
		t.Fatalf("reversed edges = %d, want 1", result.Metrics.ReversedEdges)
	}
}

func TestLayoutRejectsInvalidGraph(t *testing.T) {
	t.Parallel()

	graph := diagram.NewBuilder("test")
	graph.AddNode("A", "A", diagram.SourceSpan{})
	graph.SetDirection(diagram.Direction(99))
	_, err := Compute(mustBuild(t, graph), DefaultOptions())
	if !errors.Is(err, ErrInvalidGraph) {
		t.Fatalf("error = %v, want ErrInvalidGraph", err)
	}
}

func TestLayoutHonorsRankConstraints(t *testing.T) {
	t.Parallel()
	graph := diagram.NewBuilder("test")
	a, b := graph.AddNode("A", "A", diagram.SourceSpan{}), graph.AddNode("B", "B", diagram.SourceSpan{})
	a.Rank.Same, b.Rank.Same = "pair", "pair"
	graph.AddNode("C", "C", diagram.SourceSpan{})
	graph.AddNode("D", "D", diagram.SourceSpan{})
	graph.AddEdge(diagram.NodeEndpoint("A"), diagram.NodeEndpoint("C"), "", diagram.Directed, diagram.SourceSpan{}).MinLength = 2
	graph.AddEdge(diagram.NodeEndpoint("B"), diagram.NodeEndpoint("D"), "", diagram.Directed, diagram.SourceSpan{})
	result, err := Compute(mustBuild(t, graph), DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	ranks := make(map[string]int)
	for _, node := range result.Nodes {
		if !node.Dummy {
			ranks[node.ID] = node.Rank
		}
	}
	if ranks["A"] != ranks["B"] || ranks["C"]-ranks["A"] < 2 || ranks["D"] <= ranks["B"] {
		t.Fatalf("ranks = %v", ranks)
	}
}

func TestLayoutRejectsConflictingRankConstraints(t *testing.T) {
	t.Parallel()
	graph := diagram.NewBuilder("test")
	rankA, rankB := 2, 1
	graph.AddNode("A", "A", diagram.SourceSpan{}).Rank.Fixed = &rankA
	graph.AddNode("B", "B", diagram.SourceSpan{}).Rank.Fixed = &rankB
	graph.AddEdge(diagram.NodeEndpoint("A"), diagram.NodeEndpoint("B"), "", diagram.Directed, diagram.SourceSpan{})
	if _, err := Compute(mustBuild(t, graph), DefaultOptions()); !errors.Is(err, ErrInvalidGraph) {
		t.Fatalf("error = %v, want ErrInvalidGraph", err)
	}
}

func TestLayoutRejectsResourceExpansions(t *testing.T) {
	t.Parallel()
	t.Run("combined rank exceeds limit", func(t *testing.T) {
		graph := diagram.NewBuilder("test")
		graph.AddNode("A", "A", diagram.SourceSpan{})
		graph.AddNode("B", "B", diagram.SourceSpan{})
		graph.AddNode("C", "C", diagram.SourceSpan{})
		graph.AddEdge(diagram.NodeEndpoint("A"), diagram.NodeEndpoint("B"), "", diagram.Directed, diagram.SourceSpan{}).MinLength = diagram.MaximumMinLength
		graph.AddEdge(diagram.NodeEndpoint("B"), diagram.NodeEndpoint("C"), "", diagram.Directed, diagram.SourceSpan{}).MinLength = 1
		if _, err := Compute(mustBuild(t, graph), DefaultOptions()); err == nil || !strings.Contains(err.Error(), "above limit") {
			t.Fatalf("error = %v, want rank limit", err)
		}
	})

	t.Run("total dummy nodes exceed limit", func(t *testing.T) {
		graph := diagram.NewBuilder("test")
		graph.AddNode("A", "A", diagram.SourceSpan{})
		graph.AddNode("B", "B", diagram.SourceSpan{})
		for range 2 {
			graph.AddEdge(diagram.NodeEndpoint("A"), diagram.NodeEndpoint("B"), "", diagram.Directed, diagram.SourceSpan{}).MinLength = maximumDummyNodes/2 + 2
		}
		if _, err := Compute(mustBuild(t, graph), DefaultOptions()); err == nil || !strings.Contains(err.Error(), "dummy nodes") {
			t.Fatalf("error = %v, want dummy-node limit", err)
		}
	})

	t.Run("canvas exceeds cell limit", func(t *testing.T) {
		graph := diagram.NewBuilder("test")
		graph.AddNode("A", "A", diagram.SourceSpan{})
		options := DefaultOptions()
		options.NodeWidth = int(maximumLayoutCells) + 1
		if _, err := Compute(mustBuild(t, graph), options); err == nil || !strings.Contains(err.Error(), "exceed") {
			t.Fatalf("error = %v, want canvas limit", err)
		}
	})

	t.Run("oversized group node width", func(t *testing.T) {
		graph := diagram.NewBuilder("test")
		graph.AddNode("A", "A", diagram.SourceSpan{})
		graph.AddGroup("G", []diagram.NodeID{"A"}, "G", "", 1, diagram.LineSolid, false, diagram.SourceSpan{})
		options := DefaultOptions()
		options.NodeWidth = int(^uint(0) >> 1)
		if _, err := Compute(mustBuild(t, graph), options); err == nil || !strings.Contains(err.Error(), "node width") {
			t.Fatalf("error = %v, want node-width limit", err)
		}
	})

	t.Run("oversized horizontal group node height", func(t *testing.T) {
		graph := diagram.NewBuilder("test")
		graph.SetDirection(diagram.DirectionRight)
		graph.AddNode("A", "A", diagram.SourceSpan{})
		graph.AddGroup("G", []diagram.NodeID{"A"}, "G", "", 1, diagram.LineSolid, false, diagram.SourceSpan{})
		options := DefaultOptions()
		options.NodeHeight = int(maximumLayoutCells) + 1
		if _, err := Compute(mustBuild(t, graph), options); err == nil || !strings.Contains(err.Error(), "node height") {
			t.Fatalf("error = %v, want node-height limit", err)
		}
	})

	t.Run("oversized group node spacing", func(t *testing.T) {
		graph := diagram.NewBuilder("test")
		graph.AddNode("A", "A", diagram.SourceSpan{})
		graph.AddNode("B", "B", diagram.SourceSpan{})
		graph.AddGroup("G", []diagram.NodeID{"A", "B"}, "G", "", 1, diagram.LineSolid, false, diagram.SourceSpan{})
		options := DefaultOptions()
		options.NodeSpacing = int(maximumLayoutCells) + 1
		if _, err := Compute(mustBuild(t, graph), options); err == nil || !strings.Contains(err.Error(), "node spacing") {
			t.Fatalf("error = %v, want node-spacing limit", err)
		}
	})
}

func TestLayoutSupportsReverseDirections(t *testing.T) {
	t.Parallel()
	for _, direction := range []string{"up", "left"} {
		graph := diagram.NewBuilder("test")
		graph.AddNode("A", "A", diagram.SourceSpan{})
		graph.AddNode("B", "B", diagram.SourceSpan{})
		graph.AddEdge(diagram.NodeEndpoint("A"), diagram.NodeEndpoint("B"), "", diagram.Directed, diagram.SourceSpan{})
		if direction == "up" {
			graph.SetDirection(diagram.DirectionUp)
		} else {
			graph.SetDirection(diagram.DirectionLeft)
		}
		result, err := Compute(mustBuild(t, graph), DefaultOptions())
		if err != nil {
			t.Fatal(err)
		}
		var a, b *Node
		for _, node := range result.Nodes {
			if node.ID == "A" {
				a = node
			}
			if node.ID == "B" {
				b = node
			}
		}
		if direction == "up" && !(b.Rect.Y < a.Rect.Y) {
			t.Fatalf("up rects: A=%+v B=%+v", a.Rect, b.Rect)
		}
		if direction == "left" && !(b.Rect.X < a.Rect.X) {
			t.Fatalf("left rects: A=%+v B=%+v", a.Rect, b.Rect)
		}
	}
}

func TestLayoutHonorsOppositePlantUMLDirectionHint(t *testing.T) {
	t.Parallel()
	result := layoutInput(t, "A -up-> B\n")
	var a, b *Node
	for _, node := range result.Nodes {
		if node.ID == "A" {
			a = node
		} else if node.ID == "B" {
			b = node
		}
	}
	if a == nil || b == nil || b.Rect.Y >= a.Rect.Y {
		t.Fatalf("up hint was not reflected in layout: A=%+v B=%+v", a, b)
	}
}

func TestLayoutHonorsLeftArrowWithOppositeDirectionHint(t *testing.T) {
	t.Parallel()
	result := layoutInput(t, "A <-up- B\n")
	var a, b *Node
	for _, node := range result.Nodes {
		if node.ID == "A" {
			a = node
		} else if node.ID == "B" {
			b = node
		}
	}
	if a == nil || b == nil || b.Rect.Y >= a.Rect.Y {
		t.Fatalf("up hint was not retained independently of arrow direction: A=%+v B=%+v", a, b)
	}
}

func TestLayoutBuildsNestedGroupBounds(t *testing.T) {
	t.Parallel()
	result := layoutInput(t, "package outer {\ncomponent B\npackage inner {\ncomponent A\n}\nA --> B\n}\n")
	if len(result.Groups) != 2 {
		t.Fatalf("groups = %d", len(result.Groups))
	}
	byID := make(map[string]*Group)
	for _, group := range result.Groups {
		byID[group.ID] = group
	}
	inner, outer := byID["inner"].Rect, byID["outer"].Rect
	if !(outer.X <= inner.X && outer.Y <= inner.Y && outer.X+outer.Width >= inner.X+inner.Width && outer.Y+outer.Height >= inner.Y+inner.Height) {
		t.Fatalf("outer %+v does not contain inner %+v", outer, inner)
	}
}

func TestCompactScopeUnitCollapsesLocalWhitespace(t *testing.T) {
	t.Parallel()
	top := &workingNode{id: "top"}
	bottom := &workingNode{id: "bottom"}
	rects := map[*workingNode]Rect{
		top:    {X: 4, Y: 2, Width: 5, Height: 3},
		bottom: {X: 4, Y: 20, Width: 5, Height: 3},
	}
	unit := groupScopeUnit{nodes: map[*workingNode]bool{top: true, bottom: true}}

	if err := compactScopeUnit(&unit, rects, false); err != nil {
		t.Fatal(err)
	}

	if gap := rects[bottom].Y - (rects[top].Y + rects[top].Height); gap != 1 {
		t.Fatalf("local vertical gap = %d, want 1: top=%+v bottom=%+v", gap, rects[top], rects[bottom])
	}
}

func TestCompactScopeUnitRejectsOversizedSpan(t *testing.T) {
	t.Parallel()
	left := &workingNode{id: "left"}
	right := &workingNode{id: "right"}
	rects := map[*workingNode]Rect{
		left:  {Width: 1, Height: 1},
		right: {X: int(maximumLayoutCells), Width: 1, Height: 1},
	}
	unit := groupScopeUnit{nodes: map[*workingNode]bool{left: true, right: true}}

	if err := compactScopeUnit(&unit, rects, true); !errors.Is(err, ErrInvalidGraph) {
		t.Fatalf("error = %v, want ErrInvalidGraph", err)
	}
}

func TestLayoutKeepsForeignNodesOutsideGroupScopes(t *testing.T) {
	t.Parallel()
	const input = `component "End User" as user
package Edge {
  component DNS as dns
}
package VPC {
  package Public {
    component ALB as alb
  }
  package App {
    component ECS as ecs
    component "Auto Scaling Group" as asg
  }
  package Data {
    component Database as db
  }
}
user --> dns
dns --> alb
alb --> ecs
asg -[hidden]- ecs
ecs --> db
	`
	for _, testDirection := range []struct {
		name   string
		prefix string
	}{
		{name: "down"},
		{name: "right", prefix: "left to right direction\n"},
	} {
		t.Run(testDirection.name, func(t *testing.T) {
			result := layoutInput(t, testDirection.prefix+input)
			nodes := make(map[string]Rect)
			for _, node := range result.Nodes {
				if !node.Dummy {
					nodes[node.ID] = node.Rect
				}
			}
			groups := make(map[string]Rect)
			for _, group := range result.Groups {
				groups[group.ID] = group.Rect
			}
			for _, test := range []struct {
				foreign string
				group   string
			}{
				{foreign: "user", group: "VPC"},
				{foreign: "user", group: "App"},
				{foreign: "dns", group: "VPC"},
				{foreign: "alb", group: "App"},
			} {
				if rectanglesOverlap(nodes[test.foreign], groups[test.group]) {
					t.Errorf("foreign node %q at %+v overlaps group %q at %+v", test.foreign, nodes[test.foreign], test.group, groups[test.group])
				}
			}
			for _, pair := range [][2]string{{"Edge", "VPC"}, {"Public", "App"}, {"App", "Data"}} {
				if rectanglesOverlap(groups[pair[0]], groups[pair[1]]) {
					t.Errorf("sibling groups %q at %+v and %q at %+v overlap", pair[0], groups[pair[0]], pair[1], groups[pair[1]])
				}
			}
		})
	}
}

func TestLayoutIsDeterministic(t *testing.T) {
	t.Parallel()

	const input = "A -> B\nA -> C\nB -> D\nC -> D\nA -> D\n"
	want := layoutInput(t, input).DebugString()
	for i := 0; i < 20; i++ {
		if got := layoutInput(t, input).DebugString(); got != want {
			t.Fatalf("run %d changed output\nwant:\n%s\ngot:\n%s", i, want, got)
		}
	}
}

func TestCyclicLayoutIsDeterministic(t *testing.T) {
	t.Parallel()

	const input = "A -> B\nB -> C\nC -> A\nB -> B\nA -> B\nB -> A\n"
	want := layoutInput(t, input).DebugString()
	for i := 0; i < 20; i++ {
		if got := layoutInput(t, input).DebugString(); got != want {
			t.Fatalf("run %d changed cyclic output\nwant:\n%s\ngot:\n%s", i, want, got)
		}
	}
}

func TestCorpus(t *testing.T) {
	files, err := filepath.Glob("testdata/corpus/*.puml")
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
			input, err := os.Open(file)
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()

			graph, err := plantuml.ParseProblem(input)
			if err != nil {
				t.Fatal(err)
			}
			result, err := Compute(graph, DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			assertLayoutInvariants(t, result)
		})
	}
}

func TestGolden(t *testing.T) {
	files, err := filepath.Glob("testdata/golden/*.puml")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no golden inputs")
	}

	for _, file := range files {
		file := file
		t.Run(filepath.Base(file), func(t *testing.T) {
			t.Parallel()
			input, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(strings.TrimSuffix(file, ".puml") + ".golden")
			if err != nil {
				t.Fatal(err)
			}
			got := layoutInput(t, string(input)).DebugString()
			if got != string(want) {
				t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", want, got)
			}
		})
	}
}

func layoutInput(t *testing.T, input string) *Layout {
	t.Helper()
	graph, err := plantuml.ParseProblem(plantUMLReader(input))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Compute(graph, DefaultOptions())
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

func assertLayoutInvariants(t *testing.T, result *Layout) {
	t.Helper()
	seen := make(map[string]bool, len(result.Nodes))
	for _, node := range result.Nodes {
		if seen[node.ID] {
			t.Fatalf("duplicate layout node %q", node.ID)
		}
		seen[node.ID] = true
		if node.Rect.Width <= 0 || node.Rect.Height <= 0 {
			t.Fatalf("node %q has invalid rect: %+v", node.ID, node.Rect)
		}
	}
	for i, left := range result.Nodes {
		for _, right := range result.Nodes[i+1:] {
			if rectanglesOverlap(left.Rect, right.Rect) {
				t.Fatalf("nodes %q and %q overlap", left.ID, right.ID)
			}
		}
	}
}

func rectanglesOverlap(a, b Rect) bool {
	return a.X < b.X+b.Width && b.X < a.X+a.Width &&
		a.Y < b.Y+b.Height && b.Y < a.Y+a.Height
}

func BenchmarkLayout100Nodes(b *testing.B) {
	graph := diagram.NewBuilder("test")
	for i := 0; i < 100; i++ {
		graph.AddNode(fmt.Sprintf("n%d", i), fmt.Sprintf("n%d", i), diagram.SourceSpan{})
	}
	for i := 0; i < 99; i++ {
		graph.AddEdge(diagram.NodeEndpoint(fmt.Sprintf("n%d", i)), diagram.NodeEndpoint(fmt.Sprintf("n%d", i+1)), "", diagram.Directed, diagram.SourceSpan{})
		for jump := 2; jump <= 4 && i+jump < 100; jump++ {
			graph.AddEdge(diagram.NodeEndpoint(fmt.Sprintf("n%d", i)), diagram.NodeEndpoint(fmt.Sprintf("n%d", i+jump)), "", diagram.Directed, diagram.SourceSpan{})
		}
	}
	problem := mustBuild(b, graph)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Compute(problem, DefaultOptions()); err != nil {
			b.Fatal(err)
		}
	}
}
