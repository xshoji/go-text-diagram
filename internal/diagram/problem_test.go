package diagram

import (
	"strings"
	"testing"
)

func TestProblemIsImmutableAndIndexesContainment(t *testing.T) {
	t.Parallel()
	builder := NewBuilder("test")
	outer := builder.AddGroup("outer", nil, "Outer", "", 2, LineSolid, false, Span("test", 1, 1))
	builder.AddGroup("inner", nil, "Inner", outer.ID, 2, LineSolid, false, Span("test", 2, 1))
	node := builder.AddNode("node", "Node", Span("test", 3, 1))
	node.Cells = [][]Cell{{{ID: "name", Label: "Name"}}}
	node.Ports = []NamedPort{{ID: "input", Direction: PortInput}}
	inner, _ := builder.Group("inner")
	inner.Members = []NodeID{node.ID}
	builder.AddEdge(
		Endpoint{Kind: EndpointNode, Node: node.ID, Cell: "name"},
		GroupEndpoint(outer.ID), "relation", Directed, Span("test", 4, 1),
	)

	problem, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	if parent, ok := problem.ParentOf(ElementRef{Kind: ElementNode, Node: node.ID}); !ok || parent != inner.ID {
		t.Fatalf("node parent = %q, %t", parent, ok)
	}
	if parent, ok := problem.ParentOf(ElementRef{Kind: ElementGroup, Group: inner.ID}); !ok || parent != outer.ID {
		t.Fatalf("group parent = %q, %t", parent, ok)
	}

	nodes := problem.Nodes()
	nodes[0].Label = "mutated"
	nodes[0].Cells[0][0].Label = "mutated"
	nodes[0].Ports[0].ID = "mutated"
	got, _ := problem.Node(node.ID)
	if got.Label != "Node" || got.Cells[0][0].Label != "Name" || got.Ports[0].ID != "input" {
		t.Fatalf("caller mutated Problem: %+v", got)
	}
}

func TestProblemKeepsNodeAndGroupIDNamespacesDistinct(t *testing.T) {
	t.Parallel()
	builder := NewBuilder("test")
	builder.AddNode("same", "Node", Span("test", 1, 1))
	builder.AddGroup("same", nil, "Group", "", 1, LineSolid, false, Span("test", 2, 1))
	problem, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := problem.Node("same"); !ok {
		t.Fatal("node namespace lookup failed")
	}
	if _, ok := problem.Group("same"); !ok {
		t.Fatal("group namespace lookup failed")
	}
}

func TestProblemRejectsInvalidSemanticReferences(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		make func(*Builder)
		want string
	}{
		{
			name: "unknown endpoint",
			make: func(builder *Builder) {
				builder.AddEdge(NodeEndpoint("missing"), NodeEndpoint("also-missing"), "", Directed, SourceSpan{})
			},
			want: "unknown node",
		},
		{
			name: "duplicate node ID",
			make: func(builder *Builder) {
				builder.AddNode("node", "First", SourceSpan{})
				builder.AddNode("node", "Second", SourceSpan{})
			},
			want: "duplicate node ID",
		},
		{
			name: "duplicate direct membership",
			make: func(builder *Builder) {
				builder.AddNode("node", "Node", SourceSpan{})
				builder.AddGroup("left", []NodeID{"node"}, "Left", "", 1, LineSolid, false, SourceSpan{})
				builder.AddGroup("right", []NodeID{"node"}, "Right", "", 1, LineSolid, false, SourceSpan{})
			},
			want: "belongs to both groups",
		},
		{
			name: "duplicate group ID",
			make: func(builder *Builder) {
				builder.AddGroup("group", nil, "First", "", 1, LineSolid, false, SourceSpan{})
				builder.AddGroup("group", nil, "Second", "", 1, LineSolid, false, SourceSpan{})
			},
			want: "duplicate group ID",
		},
		{
			name: "non-canonical endpoint",
			make: func(builder *Builder) {
				builder.AddNode("node", "Node", SourceSpan{})
				builder.AddEdge(Endpoint{Kind: EndpointNode, Node: "node", Group: "ghost"}, NodeEndpoint("node"), "", Directed, SourceSpan{})
			},
			want: "must set only Node",
		},
		{
			name: "cell and port",
			make: func(builder *Builder) {
				node := builder.AddNode("node", "Node", SourceSpan{})
				node.Cells = [][]Cell{{{ID: "cell", Label: "Cell"}}}
				node.Ports = []NamedPort{{ID: "port"}}
				builder.AddEdge(Endpoint{Kind: EndpointNode, Node: "node", Cell: "cell", NamedPort: "port"}, NodeEndpoint("node"), "", Directed, SourceSpan{})
			},
			want: "cannot select both",
		},
		{
			name: "duplicate edge ID",
			make: func(builder *Builder) {
				builder.AddNode("node", "Node", SourceSpan{})
				builder.AddEdge(NodeEndpoint("node"), NodeEndpoint("node"), "", Directed, SourceSpan{})
				second := builder.AddEdge(NodeEndpoint("node"), NodeEndpoint("node"), "", Directed, SourceSpan{})
				second.ID = "e0"
			},
			want: "duplicate edge ID",
		},
		{
			name: "negative fixed rank",
			make: func(builder *Builder) {
				rank := -1
				builder.AddNode("node", "Node", SourceSpan{}).Rank.Fixed = &rank
			},
			want: "fixed rank -1 is outside",
		},
		{
			name: "excessive minimum length",
			make: func(builder *Builder) {
				builder.AddNode("source", "Source", SourceSpan{})
				builder.AddNode("target", "Target", SourceSpan{})
				edge := builder.AddEdge(NodeEndpoint("source"), NodeEndpoint("target"), "", Directed, SourceSpan{})
				edge.MinLength = MaximumMinLength + 1
			},
			want: "minimum length 10001 is outside",
		},
		{
			name: "group self-loop",
			make: func(builder *Builder) {
				builder.AddNode("member", "Member", SourceSpan{})
				builder.AddGroup("group", []NodeID{"member"}, "Group", "", 1, LineSolid, false, SourceSpan{})
				builder.AddEdge(GroupEndpoint("group"), GroupEndpoint("group"), "", Directed, SourceSpan{})
			},
			want: "group self-loops are not supported",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			builder := NewBuilder("test")
			test.make(builder)
			_, err := builder.Build()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}
