package layout

import (
	"testing"

	"github.com/xshoji/go-text-diagram/internal/diagram"
)

func TestGroupFlowPlacesDependentSiblingScopesInLaterRankBands(t *testing.T) {
	graph := diagram.NewBuilder("test")
	graph.AddNode("a", "A", diagram.SourceSpan{})
	graph.AddNode("b", "B", diagram.SourceSpan{})
	graph.AddGroup("first", []string{"a"}, "First", "", 1, diagram.LineSolid, false, diagram.SourceSpan{})
	graph.AddGroup("second", []string{"b"}, "Second", "", 1, diagram.LineSolid, false, diagram.SourceSpan{})
	graph.AddEdge(diagram.NodeEndpoint("a"), diagram.NodeEndpoint("b"), "", diagram.Directed, diagram.SourceSpan{})

	options := DefaultOptions()
	options.FlowGroups = true
	result, err := Compute(mustBuild(t, graph), options)
	if err != nil {
		t.Fatal(err)
	}
	if !result.FlowApplied {
		t.Fatal("dependent sibling scopes did not apply flow layout")
	}
	nodes := layoutNodesByID(result)
	if nodes["b"].Rank != nodes["a"].Rank+1 || nodes["b"].Rect.Y <= nodes["a"].Rect.Y {
		t.Fatalf("second scope was not placed below the first: a=%+v b=%+v", nodes["a"], nodes["b"])
	}
}

func TestGroupFlowReservesSiblingPaddingBetweenRankBands(t *testing.T) {
	graph := diagram.NewBuilder("test")
	graph.AddNode("a", "A", diagram.SourceSpan{})
	graph.AddNode("b", "B", diagram.SourceSpan{})
	graph.AddGroup("first", []string{"a"}, "First", "", 2, diagram.LineSolid, false, diagram.SourceSpan{})
	graph.AddGroup("second", []string{"b"}, "Second", "", 2, diagram.LineSolid, false, diagram.SourceSpan{})
	graph.AddEdge(diagram.NodeEndpoint("a"), diagram.NodeEndpoint("b"), "", diagram.Directed, diagram.SourceSpan{})

	options := DefaultOptions()
	options.FlowGroups = true
	result, err := Compute(mustBuild(t, graph), options)
	if err != nil {
		t.Fatal(err)
	}
	groups := make(map[string]Rect)
	for _, group := range result.Groups {
		groups[group.ID] = group.Rect
	}
	first, second := groups["first"], groups["second"]
	if first.Y+first.Height > second.Y {
		t.Fatalf("group padding overlaps vertically: first=%+v second=%+v", first, second)
	}
	if first.X != second.X {
		t.Fatalf("dependent groups escaped laterally: first=%+v second=%+v", first, second)
	}
}

func TestGroupFlowUsesNestedScopeDependencyAtCommonAncestor(t *testing.T) {
	graph := diagram.NewBuilder("test")
	graph.AddNode("a", "A", diagram.SourceSpan{})
	graph.AddNode("b", "B", diagram.SourceSpan{})
	graph.AddGroup("left", nil, "Left", "", 1, diagram.LineSolid, false, diagram.SourceSpan{})
	graph.AddGroup("left-child", []string{"a"}, "Left child", "left", 1, diagram.LineSolid, false, diagram.SourceSpan{})
	graph.AddGroup("right", nil, "Right", "", 1, diagram.LineSolid, false, diagram.SourceSpan{})
	graph.AddGroup("right-child", []string{"b"}, "Right child", "right", 1, diagram.LineSolid, false, diagram.SourceSpan{})
	graph.AddEdge(diagram.NodeEndpoint("a"), diagram.NodeEndpoint("b"), "", diagram.Directed, diagram.SourceSpan{})

	options := DefaultOptions()
	options.FlowGroups = true
	result, err := Compute(mustBuild(t, graph), options)
	if err != nil {
		t.Fatal(err)
	}
	if !result.FlowApplied {
		t.Fatal("nested dependency did not apply flow layout")
	}
	nodes := layoutNodesByID(result)
	if nodes["b"].Rank <= nodes["a"].Rank {
		t.Fatalf("nested target rank=%d, want below source rank=%d", nodes["b"].Rank, nodes["a"].Rank)
	}
}

func TestGroupFlowAlignsSparseDirectScopeAcrossGroups(t *testing.T) {
	graph := diagram.NewBuilder("test")
	for _, id := range []string{"top", "a", "middle", "b", "bottom"} {
		graph.AddNode(diagram.NodeID(id), id, diagram.SourceSpan{})
	}
	graph.AddGroup("first", []string{"a"}, "First", "", 1, diagram.LineSolid, false, diagram.SourceSpan{})
	graph.AddGroup("second", []string{"b"}, "Second", "", 1, diagram.LineSolid, false, diagram.SourceSpan{})
	for _, edge := range [][2]string{{"top", "a"}, {"a", "middle"}, {"middle", "b"}, {"b", "bottom"}} {
		graph.AddEdge(diagram.NodeEndpoint(diagram.NodeID(edge[0])), diagram.NodeEndpoint(diagram.NodeID(edge[1])), "", diagram.Directed, diagram.SourceSpan{})
	}

	options := DefaultOptions()
	options.FlowGroups = true
	result, err := Compute(mustBuild(t, graph), options)
	if err != nil {
		t.Fatal(err)
	}
	if !result.FlowApplied {
		t.Fatal("interleaved scopes did not apply flow layout")
	}
	nodes := layoutNodesByID(result)
	wantCenter := nodes["top"].Rect.X + (nodes["top"].Rect.Width-1)/2
	for _, id := range []string{"a", "middle", "b", "bottom"} {
		center := nodes[id].Rect.X + (nodes[id].Rect.Width-1)/2
		if absInt(center-wantCenter) > 1 {
			t.Fatalf("node %q escaped laterally: top=%+v node=%+v", id, nodes["top"], nodes[id])
		}
	}
}

func TestGroupFlowSkipsUnsupportedFixedRankWithoutChangingRanks(t *testing.T) {
	graph := diagram.NewBuilder("test")
	fixedRank := 0
	a := graph.AddNode("a", "A", diagram.SourceSpan{})
	a.Rank.Fixed = &fixedRank
	graph.AddNode("b", "B", diagram.SourceSpan{})
	graph.AddGroup("first", []string{"a"}, "First", "", 1, diagram.LineSolid, false, diagram.SourceSpan{})
	graph.AddGroup("second", []string{"b"}, "Second", "", 1, diagram.LineSolid, false, diagram.SourceSpan{})
	graph.AddEdge(diagram.NodeEndpoint("a"), diagram.NodeEndpoint("b"), "", diagram.Directed, diagram.SourceSpan{})

	nodes, _, edges, err := buildWorkingGraph(mustBuild(t, graph))
	if err != nil {
		t.Fatal(err)
	}
	edges, _, _ = makeAcyclic(nodes, edges, false)
	order, err := topologicalOrder(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	if err := assignRanks(order, edges); err != nil {
		t.Fatal(err)
	}
	before := make(map[*workingNode]int, len(nodes))
	for _, node := range nodes {
		before[node] = node.rank
	}
	if applyGroupFlowRanks(mustBuild(t, graph).Groups(), nodes, edges, DefaultOptions().LayerSpacing) {
		t.Fatal("fixed-rank graph unexpectedly applied flow layout")
	}
	for _, node := range nodes {
		if node.rank != before[node] {
			t.Fatalf("rank changed after skipped flow layout: node=%s before=%d after=%d", node.id, before[node], node.rank)
		}
	}
}

func TestArrangeFlowUnitsKeepsIndependentAndCyclicScopesInSameBand(t *testing.T) {
	a := &workingNode{id: "a", rank: 2}
	b := &workingNode{id: "b", rank: 2}
	independent := &workingNode{id: "independent", rank: 2}
	units := []flowScopeUnit{
		{nodes: map[*workingNode]bool{a: true}, order: 0},
		{nodes: map[*workingNode]bool{b: true}, order: 1},
		{nodes: map[*workingNode]bool{independent: true}, order: 2},
	}
	edges := []*workingEdge{{from: a, to: b}, {from: b, to: a}}
	if !arrangeFlowUnits(units, edges, DefaultOptions().LayerSpacing) {
		t.Fatal("scope SCC could not be arranged")
	}
	if a.rank != 2 || b.rank != 2 || independent.rank != 2 {
		t.Fatalf("SCC or independent scope was unnecessarily shifted: a=%d b=%d independent=%d", a.rank, b.rank, independent.rank)
	}
}

func TestAlignFlowLayerPreservesOrderAndSpacing(t *testing.T) {
	left := &workingNode{id: "left"}
	right := &workingNode{id: "right"}
	target := &workingNode{id: "target"}
	rects := map[*workingNode]Rect{
		left:   {X: 12, Width: 5, Height: 3},
		right:  {X: 0, Width: 3, Height: 3},
		target: {X: 0, Width: 3, Height: 3},
	}
	alignFlowLayer([]*workingNode{left, right}, map[*workingNode][]*workingNode{
		left:  {target},
		right: {target},
	}, rects, 2)
	if rects[right].X < rects[left].X+rects[left].Width+2 {
		t.Fatalf("projection violated order or spacing: left=%+v right=%+v", rects[left], rects[right])
	}
}

func TestPlaceFlowScopeUnitsOnlyUsesLateralEscapeForOverlappingRows(t *testing.T) {
	first := &workingNode{id: "first"}
	below := &workingNode{id: "below"}
	overlap := &workingNode{id: "overlap"}
	rects := map[*workingNode]Rect{
		first:   {X: 10, Y: 0, Width: 5, Height: 3},
		below:   {X: 10, Y: 6, Width: 5, Height: 3},
		overlap: {X: 10, Y: 1, Width: 5, Height: 3},
	}
	units := []groupScopeUnit{
		{nodes: map[*workingNode]bool{first: true}, rect: rects[first]},
		{nodes: map[*workingNode]bool{below: true}, rect: rects[below]},
		{nodes: map[*workingNode]bool{overlap: true}, rect: rects[overlap]},
	}
	placeFlowScopeUnits(units, rects, 2)
	if rects[below].X != rects[first].X {
		t.Fatalf("non-overlapping row moved laterally: first=%+v below=%+v", rects[first], rects[below])
	}
	if rects[overlap].X == rects[first].X {
		t.Fatalf("overlapping row did not use lateral escape: first=%+v overlap=%+v", rects[first], rects[overlap])
	}
}

func TestPlaceFlowScopeUnitsIgnoresEmptyRowsInDirectScopeBounds(t *testing.T) {
	for _, directFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "group-first", true: "direct-first"}[directFirst], func(t *testing.T) {
			top := &workingNode{id: "top"}
			bottom := &workingNode{id: "bottom"}
			member := &workingNode{id: "member"}
			rects := map[*workingNode]Rect{
				top:    {X: 10, Y: 0, Width: 5, Height: 3},
				bottom: {X: 10, Y: 8, Width: 5, Height: 3},
				member: {X: 10, Y: 4, Width: 5, Height: 3},
			}
			direct := groupScopeUnit{
				nodes: map[*workingNode]bool{top: true, bottom: true},
				rect:  boundsOfNodeSet(map[*workingNode]bool{top: true, bottom: true}, rects),
			}
			group := groupScopeUnit{groupID: "group", nodes: map[*workingNode]bool{member: true}, rect: rects[member]}
			units := []groupScopeUnit{group, direct}
			if directFirst {
				units[0], units[1] = units[1], units[0]
			}

			placeFlowScopeUnits(units, rects, 2)

			if rects[top].X != rects[member].X || rects[bottom].X != rects[member].X {
				t.Fatalf("empty rows caused lateral escape: top=%+v member=%+v bottom=%+v", rects[top], rects[member], rects[bottom])
			}
		})
	}
}

func TestPlaceFlowScopeUnitsMovesWholeDirectScopeForDummyCollision(t *testing.T) {
	top := &workingNode{id: "top"}
	bottom := &workingNode{id: "bottom"}
	dummy := &workingNode{id: "dummy", dummy: true}
	member := &workingNode{id: "member"}
	rects := map[*workingNode]Rect{
		top:    {X: 10, Y: 0, Width: 5, Height: 3},
		bottom: {X: 10, Y: 8, Width: 5, Height: 3},
		dummy:  {X: 12, Y: 4, Width: 1, Height: 1},
		member: {X: 10, Y: 4, Width: 5, Height: 3},
	}
	directNodes := map[*workingNode]bool{top: true, bottom: true, dummy: true}
	units := []groupScopeUnit{
		{groupID: "group", nodes: map[*workingNode]bool{member: true}, rect: rects[member]},
		{nodes: directNodes, rect: boundsOfNodeSet(directNodes, rects)},
	}

	placeFlowScopeUnits(units, rects, 2)

	delta := rects[top].X - 10
	if delta != 5 || rects[bottom].X-10 != delta || rects[dummy].X-12 != delta {
		t.Fatalf("direct scope did not move together: top=%+v bottom=%+v dummy=%+v", rects[top], rects[bottom], rects[dummy])
	}
}

func TestPlaceFlowScopeUnitsUsesMovedFootprintsForFollowingScopes(t *testing.T) {
	first := &workingNode{id: "first"}
	second := &workingNode{id: "second"}
	third := &workingNode{id: "third"}
	rects := map[*workingNode]Rect{
		first:  {X: 10, Y: 0, Width: 5, Height: 3},
		second: {X: 10, Y: 0, Width: 5, Height: 3},
		third:  {X: 10, Y: 0, Width: 5, Height: 3},
	}
	units := []groupScopeUnit{
		{nodes: map[*workingNode]bool{first: true}, rect: rects[first]},
		{nodes: map[*workingNode]bool{second: true}, rect: rects[second]},
		{nodes: map[*workingNode]bool{third: true}, rect: rects[third]},
	}

	placeFlowScopeUnits(units, rects, 2)

	if rects[first].X != 10 || rects[second].X != 17 || rects[third].X != 3 {
		t.Fatalf("scope positions = first=%+v second=%+v third=%+v", rects[first], rects[second], rects[third])
	}
}

func TestCenterIsolatedFlowNodesPreservesSameRankSiblings(t *testing.T) {
	top := &workingNode{id: "top"}
	left := &workingNode{id: "left"}
	right := &workingNode{id: "right"}
	rects := map[*workingNode]Rect{
		top:   {X: 2, Y: 0, Width: 5, Height: 3},
		left:  {X: 0, Y: 4, Width: 5, Height: 3},
		right: {X: 8, Y: 4, Width: 5, Height: 3},
	}
	nodes := map[*workingNode]bool{top: true, left: true, right: true}
	unit := groupScopeUnit{nodes: nodes, rect: boundsOfNodeSet(nodes, rects)}

	centerIsolatedFlowNodes(&unit, rects, 10)

	if got := rects[top].X + (rects[top].Width-1)/2; got != 10 {
		t.Fatalf("isolated node center = %d, want 10", got)
	}
	if rects[left].X != 0 || rects[right].X != 8 {
		t.Fatalf("same-rank siblings moved: left=%+v right=%+v", rects[left], rects[right])
	}
}

func BenchmarkNearestFlowScopeOffsetManyFootprints(b *testing.B) {
	const footprints = 10_000
	placed := make([]Rect, footprints)
	for index := range placed {
		placed[index] = Rect{X: index * 8, Width: 5, Height: 3}
	}
	current := []Rect{{X: footprints * 4, Width: 5, Height: 3}}

	b.ResetTimer()
	for range b.N {
		nearestFlowScopeOffset([][]Rect{placed}, current, 2)
	}
}

func layoutNodesByID(result *Layout) map[string]*Node {
	nodes := make(map[string]*Node)
	for _, node := range result.Nodes {
		if !node.Dummy {
			nodes[node.ID] = node
		}
	}
	return nodes
}

func mustBuild(t testing.TB, builder *diagram.Builder) *diagram.Problem {
	t.Helper()
	problem, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	return problem
}
