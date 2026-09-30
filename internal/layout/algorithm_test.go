package layout

import (
	"fmt"
	"testing"

	"github.com/xshoji/go-text-diagram/internal/diagram"
)

func TestStronglyConnectedComponentsPartition(t *testing.T) {
	t.Parallel()

	a := &workingNode{id: "A", inputOrder: 0}
	b := &workingNode{id: "B", inputOrder: 1}
	c := &workingNode{id: "C", inputOrder: 2}
	components := stronglyConnectedComponents(
		[]*workingNode{a, b, c},
		[]*workingEdge{{from: a, to: b}, {from: b, to: a}},
	)
	if components[a] != components[b] {
		t.Fatal("cycle members must share a component")
	}
	if components[a] == components[c] {
		t.Fatal("isolated node must be a separate component")
	}
}

func TestFeedbackOrderReducesBackwardEdges(t *testing.T) {
	t.Parallel()

	a := &workingNode{id: "A", inputOrder: 0}
	b := &workingNode{id: "B", inputOrder: 1}
	c := &workingNode{id: "C", inputOrder: 2}
	d := &workingNode{id: "D", inputOrder: 3}
	nodes := []*workingNode{a, b, c, d}
	edges := []*workingEdge{{from: d, to: c}, {from: c, to: b}, {from: b, to: a}, {from: a, to: d}}
	order := feedbackOrder(nodes, edges)
	if got, want := backwardEdgeCount(order, edges), 1; got != want {
		t.Fatalf("backward edges = %d, want %d", got, want)
	}
	if baseline := backwardEdgeCount(nodes, edges); baseline != 3 {
		t.Fatalf("baseline backward edges = %d, want 3", baseline)
	}
}

func TestPackComponentsWrapsAtMaximumWidth(t *testing.T) {
	t.Parallel()

	a := &workingNode{id: "A", inputOrder: 0}
	b := &workingNode{id: "B", inputOrder: 1}
	rects := map[*workingNode]Rect{
		a: {X: 10, Y: 5, Width: 5, Height: 3},
		b: {X: 20, Y: 5, Width: 5, Height: 3},
	}
	width, height := packComponents([]*workingNode{a, b}, nil, rects, 2, 8)
	if width != 5 || height != 8 || rects[a].Y != 0 || rects[b].Y != 5 {
		t.Fatalf("extent=%dx%d rects=%+v", width, height, rects)
	}
}

func TestStraightenCoordinatesAlignsUnambiguousChain(t *testing.T) {
	t.Parallel()

	a := &workingNode{id: "A"}
	b := &workingNode{id: "B"}
	c := &workingNode{id: "C"}
	rects := map[*workingNode]Rect{
		a: {X: 4, Y: 0, Width: 3, Height: 3},
		b: {X: 0, Y: 5, Width: 3, Height: 3},
		c: {X: 4, Y: 10, Width: 3, Height: 3},
	}
	straightenCoordinates([][]*workingNode{{a}, {b}, {c}}, []*workingEdge{{from: a, to: b}, {from: b, to: c}}, rects, true)
	if rects[b].X != rects[a].X || rects[b].X != rects[c].X {
		t.Fatalf("chain rects = %+v", rects)
	}
}

func TestRequiredLayerSpacingDoesNotMultiplyLabelExtentByLaneCount(t *testing.T) {
	t.Parallel()

	from := &workingNode{id: "from", rank: 0}
	rects := map[*workingNode]Rect{from: {X: 0, Y: 0, Width: 5, Height: 30}}
	var edges []*workingEdge
	for index := 0; index < 10; index++ {
		to := &workingNode{id: fmt.Sprintf("to-%d", index), rank: 1}
		rects[to] = Rect{X: 40, Y: index * 3, Width: 5, Height: 3}
		edges = append(edges, &workingEdge{from: from, to: to})
	}
	if got := requiredLayerSpacing(edges, rects, diagram.DirectionRight, 20, 1); got != 20 {
		t.Fatalf("layer spacing = %d, want label extent 20", got)
	}
}

func TestCellRectSupportsRaggedRowsAndWideText(t *testing.T) {
	t.Parallel()
	node := &Node{
		Rect:    Rect{X: 3, Y: 4, Width: 16, Height: 5},
		Cells:   [][]string{{"識別子", "Name"}, {"値"}},
		CellIDs: [][]string{{"id", "name"}, {"value"}},
	}
	id, ok := node.CellRect("id")
	if !ok || id.X != 4 || id.Y != 5 || id.Width < 8 {
		t.Fatalf("wide cell rect = %+v, ok=%t", id, ok)
	}
	value, ok := node.CellRect("value")
	if !ok || value.X != id.X || value.Y != 7 {
		t.Fatalf("ragged cell rect = %+v, ok=%t", value, ok)
	}
	if _, ok := node.CellRect("missing"); ok {
		t.Fatal("unknown cell unexpectedly resolved")
	}
}

func TestTopologicalOrderUsesStableTieBreak(t *testing.T) {
	t.Parallel()

	lateID := &workingNode{id: "B", inputOrder: 0}
	earlyID := &workingNode{id: "A", inputOrder: 0}
	laterInput := &workingNode{id: "0", inputOrder: 1}
	order, err := topologicalOrder([]*workingNode{laterInput, lateID, earlyID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if order[0] != earlyID || order[1] != lateID || order[2] != laterInput {
		t.Fatalf("order = %q, %q, %q", order[0].id, order[1].id, order[2].id)
	}
}

func TestAssignRanksUsesLongestIncomingPath(t *testing.T) {
	t.Parallel()

	a := &workingNode{id: "A"}
	b := &workingNode{id: "B"}
	c := &workingNode{id: "C"}
	edges := []*workingEdge{{from: a, to: b}, {from: a, to: c}, {from: b, to: c}}
	assignRanks([]*workingNode{a, b, c}, edges)
	if a.rank != 0 || b.rank != 1 || c.rank != 2 {
		t.Fatalf("ranks = A:%d B:%d C:%d", a.rank, b.rank, c.rank)
	}
}

func TestCompactGroupRanksMovesGroupedSourceIntoAvailableSlack(t *testing.T) {
	source := &workingNode{id: "source", inputOrder: 0, rank: 0}
	anchor := &workingNode{id: "anchor", inputOrder: 1, rank: 5}
	edges := []*workingEdge{{from: source, to: anchor, minLength: 1}}
	groups := []diagram.Group{{ID: "group", Members: []string{"source"}}}

	compactGroupRanks(groups, []*workingNode{source, anchor}, edges, 3)

	if source.rank != 3 {
		t.Fatalf("source rank = %d, want bounded rank 3", source.rank)
	}
}

func TestCompactGroupRanksPreservesMinimumLengthAndFixedRanks(t *testing.T) {
	fixed := 0
	source := &workingNode{id: "source", inputOrder: 0, rank: 0, rankConstraint: diagram.RankConstraint{Fixed: &fixed}}
	target := &workingNode{id: "target", inputOrder: 1, rank: 3}
	edges := []*workingEdge{{from: source, to: target, minLength: 2}}
	groups := []diagram.Group{{ID: "group", Members: []string{"source", "target"}}}

	compactGroupRanks(groups, []*workingNode{source, target}, edges, 10)

	if source.rank != 0 || target.rank != 2 {
		t.Fatalf("ranks = source:%d target:%d, want 0 and 2", source.rank, target.rank)
	}
}

func TestDummyInsertionPreservesEdgeMetadataAndAvoidsIDCollision(t *testing.T) {
	t.Parallel()

	from := &workingNode{id: "A", rank: 0}
	to := &workingNode{id: "D", rank: 3}
	existing := &workingNode{id: "__dummy_long_edge_1", rank: 1}
	edge := &workingEdge{
		id: "long_edge", originalEdgeID: "long_edge", from: from, to: to, label: "label",
		originalFrom: "D", originalTo: "A", reversed: true,
	}
	nodes, edges, longEdges := insertDummyNodes(
		[]*workingNode{from, to, existing},
		map[string]*workingNode{from.id: from, to.id: to, existing.id: existing},
		[]*workingEdge{edge},
	)
	if longEdges != 1 || len(nodes) != 5 || len(edges) != 3 {
		t.Fatalf("nodes=%d edges=%d long=%d", len(nodes), len(edges), longEdges)
	}
	if nodes[3].id != "__dummy_long_edge_1_1" || nodes[3].ownerEdgeID != "long_edge" {
		t.Fatalf("colliding dummy ID = %q", nodes[3].id)
	}
	for _, segment := range edges {
		if segment.label != "label" || !segment.reversed || segment.originalFrom != "D" || segment.originalTo != "A" {
			t.Fatalf("metadata was not preserved: %+v", segment)
		}
	}
}
