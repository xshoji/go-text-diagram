package layout

import (
	"testing"

	"github.com/xshoji/go-text-diagram/internal/diagram"
)

func TestGroupBoundsExcludeOthers(t *testing.T) {
	for _, test := range []struct {
		name, label string
		padding     int
	}{
		{name: "long label", label: "012345678901234567890123456789", padding: 1},
		{name: "padding", label: "G", padding: 12},
	} {
		t.Run(test.name, func(t *testing.T) {
			builder := diagram.NewBuilder("test")
			builder.AddNode("a", "A", diagram.SourceSpan{})
			builder.AddNode("b", "B", diagram.SourceSpan{})
			builder.AddGroup("g", []string{"a"}, test.label, "", test.padding, diagram.LineSolid, false, diagram.SourceSpan{})
			result, err := Compute(mustBuild(t, builder), DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			if group, node := result.Groups[0].Rect, layoutNodesByID(result)["b"].Rect; rectanglesOverlap(group, node) {
				t.Fatalf("group %v overlaps external node %v", group, node)
			}
		})
	}
}

func TestGroupKeepsFlowGap(t *testing.T) {
	for _, direction := range []diagram.Direction{diagram.DirectionDown, diagram.DirectionRight, diagram.DirectionUp, diagram.DirectionLeft} {
		t.Run(direction.String(), func(t *testing.T) {
			builder := diagram.NewBuilder("test")
			builder.SetDirection(direction)
			builder.AddNode("a", "A", diagram.SourceSpan{})
			builder.AddNode("b", "B", diagram.SourceSpan{})
			builder.AddGroup("g", []string{"b"}, "G", "", 1, diagram.LineSolid, false, diagram.SourceSpan{})
			builder.AddEdge(diagram.NodeEndpoint("a"), diagram.NodeEndpoint("b"), "", diagram.Directed, diagram.SourceSpan{})
			result, err := Compute(mustBuild(t, builder), DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			nodes := layoutNodesByID(result)
			a, b := nodes["a"].Rect, nodes["b"].Rect
			if crossStart(a, direction.Vertical())+crossSize(a, direction.Vertical())/2 != crossStart(b, direction.Vertical())+crossSize(b, direction.Vertical())/2 {
				t.Fatalf("flow-separated group moved off axis: a=%v b=%v", a, b)
			}
			if rectanglesOverlap(result.Groups[0].Rect, a) {
				t.Fatal("group overlaps external node")
			}
		})
	}
}

func TestGroupsDoNotChangeNodeRanks(t *testing.T) {
	builder := diagram.NewBuilder("test")
	builder.AddNode("a", "A", diagram.SourceSpan{})
	builder.AddNode("b", "B", diagram.SourceSpan{})
	builder.AddGroup("first", []string{"a"}, "First", "", 12, diagram.LineSolid, false, diagram.SourceSpan{})
	builder.AddGroup("second", []string{"b"}, "Second", "", 12, diagram.LineSolid, false, diagram.SourceSpan{})
	builder.AddEdge(diagram.NodeEndpoint("a"), diagram.NodeEndpoint("b"), "", diagram.Directed, diagram.SourceSpan{})

	result, err := Compute(mustBuild(t, builder), DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	nodes := layoutNodesByID(result)
	if nodes["a"].Rank != 0 || nodes["b"].Rank != 1 {
		t.Fatalf("group membership changed node ranks: a=%d b=%d", nodes["a"].Rank, nodes["b"].Rank)
	}
	for _, member := range []struct{ group, node string }{{"first", "a"}, {"second", "b"}} {
		var group *Group
		for _, candidate := range result.Groups {
			if candidate.ID == member.group {
				group = candidate
				break
			}
		}
		node := nodes[member.node]
		if group.Rect.X > node.Rect.X || group.Rect.Y > node.Rect.Y ||
			group.Rect.X+group.Rect.Width < node.Rect.X+node.Rect.Width ||
			group.Rect.Y+group.Rect.Height < node.Rect.Y+node.Rect.Height {
			t.Errorf("group %q bounds %v do not contain member %v", group.ID, group.Rect, node.Rect)
		}
	}
}

func TestGroupPackingKeepsDirectNodesDisjointAcrossRanks(t *testing.T) {
	builder := diagram.NewBuilder("test")
	builder.SetDirection(diagram.DirectionRight)
	builder.AddNode("k", "K", diagram.SourceSpan{})
	builder.AddNode("b", "B", diagram.SourceSpan{})
	rank := 1
	builder.AddNode("a", "A", diagram.SourceSpan{}).Rank.Fixed = &rank
	builder.AddGroup("g", []string{"k"}, "G", "", 9, diagram.LineSolid, false, diagram.SourceSpan{})
	options := DefaultOptions()
	options.MaxWidth = 80

	result, err := Compute(mustBuild(t, builder), options)
	if err != nil {
		t.Fatal(err)
	}
	nodes := layoutNodesByID(result)
	if rectanglesOverlap(nodes["a"].Rect, nodes["b"].Rect) {
		t.Fatalf("rank 0 node b at %v overlaps rank 1 node a at %v", nodes["b"].Rect, nodes["a"].Rect)
	}
}

func TestDirectRankNodesStaySeparatedOutsideGroupBounds(t *testing.T) {
	const input = `left to right direction
component ui
component act
component c1
component c2
component internal
package package1 {
  component tool1
  component publisher1
  component subscriber1
}
ui --> tool1
act --> tool1
c1 --> tool1
c2 --> tool1
internal --> tool1
`
	result := layoutInput(t, input)
	nodes := layoutNodesByID(result)
	var group *Group
	for _, candidate := range result.Groups {
		if candidate.ID == "package1" {
			group = candidate
			break
		}
	}
	if group == nil {
		t.Fatal("package1 group is missing")
	}
	directIDs := []string{"ui", "act", "c1", "c2", "internal"}
	for _, id := range directIDs {
		node := nodes[id]
		if node == nil {
			t.Errorf("direct node %q is missing", id)
			continue
		}
		if node.Rank != 0 {
			t.Errorf("node %q rank = %d, want 0", id, node.Rank)
		}
		if rectanglesOverlap(node.Rect, group.Rect) {
			t.Errorf("direct node %q at %+v overlaps group at %+v", id, node.Rect, group.Rect)
		}
	}
	for i, left := range directIDs {
		for _, right := range directIDs[i+1:] {
			leftNode, rightNode := nodes[left], nodes[right]
			if leftNode != nil && rightNode != nil && rectanglesOverlap(leftNode.Rect, rightNode.Rect) {
				t.Errorf("direct nodes %q and %q overlap", left, right)
			}
		}
	}
}

func TestIsolatedGroupMembersPackBesideConnectedMembers(t *testing.T) {
	builder := diagram.NewBuilder("test")
	builder.SetDirection(diagram.DirectionRight)
	for _, id := range []string{"source1", "source2", "source3", "tool", "publisher", "subscriber"} {
		builder.AddNode(id, id, diagram.SourceSpan{})
	}
	builder.AddGroup("package", []string{"tool", "publisher", "subscriber"}, "package", "", 1, diagram.LineSolid, false, diagram.SourceSpan{})
	for _, source := range []string{"source1", "source2", "source3"} {
		builder.AddEdge(diagram.NodeEndpoint(source), diagram.NodeEndpoint("tool"), "", diagram.Directed, diagram.SourceSpan{})
	}

	result, err := Compute(mustBuild(t, builder), DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	nodes := layoutNodesByID(result)
	toolCenter := nodes["tool"].Rect.X + nodes["tool"].Rect.Width/2
	if publisherCenter := nodes["publisher"].Rect.X + nodes["publisher"].Rect.Width/2; publisherCenter != toolCenter {
		t.Fatalf("publisher center = %d, want tool center %d", publisherCenter, toolCenter)
	}
	if subscriberCenter := nodes["subscriber"].Rect.X + nodes["subscriber"].Rect.Width/2; subscriberCenter != toolCenter {
		t.Fatalf("subscriber center = %d, want tool center %d", subscriberCenter, toolCenter)
	}
	if nodes["publisher"].Component == nodes["tool"].Component || nodes["subscriber"].Component == nodes["tool"].Component {
		t.Fatal("isolated group members share the tool's route component")
	}
	if len(result.Groups) != 1 || result.Groups[0].Rect.Width > 18 {
		t.Fatalf("group bounds remain wide: group=%+v tool=%+v", result.Groups, nodes["tool"].Rect)
	}
}

func mustBuild(t testing.TB, builder *diagram.Builder) *diagram.Problem {
	t.Helper()
	problem, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	return problem
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
