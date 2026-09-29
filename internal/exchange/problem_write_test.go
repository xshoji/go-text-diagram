package exchange

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/xshoji/agents-workspace/internal/diagram"
	"github.com/xshoji/agents-workspace/internal/plantuml"
)

func TestProblemDOTRoundTripPreservesSupportedSemantics(t *testing.T) {
	t.Parallel()
	want := richExchangeProblem(t)
	var output bytes.Buffer
	if err := WriteDOTProblem(&output, want); err != nil {
		t.Fatal(err)
	}
	got, err := ParseDOTProblem(strings.NewReader(output.String()))
	if err != nil {
		t.Fatalf("parse generated DOT: %v\n%s", err, output.String())
	}
	if !reflect.DeepEqual(problemSemantics(want), problemSemantics(got)) {
		t.Fatalf("semantic round trip differs\nDOT:\n%s\nwant: %#v\ngot:  %#v", output.String(), problemSemantics(want), problemSemantics(got))
	}
}

func TestProblemGraphMLUsesTypedSafeEndpointsAndSemanticData(t *testing.T) {
	t.Parallel()
	problem := richExchangeProblem(t)
	var output bytes.Buffer
	if err := WriteGraphMLProblem(&output, problem); err != nil {
		t.Fatal(err)
	}
	var document graphML
	if err := xml.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatalf("invalid GraphML XML: %v\n%s", err, output.String())
	}
	if len(document.Graph.Nodes) != 4 || len(document.Graph.Edges) != 2 {
		t.Fatalf("GraphML structure = %+v", document.Graph)
	}
	keys := make(map[string]bool, len(document.Keys))
	for _, key := range document.Keys {
		if key.ID == "" || keys[key.ID] {
			t.Fatalf("empty or duplicate GraphML key %q", key.ID)
		}
		keys[key.ID] = true
	}
	nodeIDs := make(map[string]bool, len(document.Graph.Nodes))
	for _, node := range document.Graph.Nodes {
		if node.ID == "" || nodeIDs[node.ID] {
			t.Fatalf("empty or duplicate GraphML node ID %q", node.ID)
		}
		nodeIDs[node.ID] = true
		assertGraphMLDataKeys(t, keys, node.Data)
	}
	edgeIDs := make(map[string]bool, len(document.Graph.Edges))
	for _, edge := range document.Graph.Edges {
		if edge.ID == "" || edgeIDs[edge.ID] || !nodeIDs[edge.Source] || !nodeIDs[edge.Target] {
			t.Fatalf("unresolved GraphML endpoint: %+v", edge)
		}
		edgeIDs[edge.ID] = true
		assertGraphMLDataKeys(t, keys, edge.Data)
	}
	assertGraphMLDataKeys(t, keys, document.Graph.Data)
	if !strings.Contains(output.String(), `<data key="parent">outer</data>`) ||
		!strings.Contains(output.String(), `<data key="source-endpoint">`) ||
		!strings.Contains(output.String(), `<data key="group-semantics">`) {
		t.Fatalf("typed semantic data missing:\n%s", output.String())
	}
}

func assertGraphMLDataKeys(t *testing.T, keys map[string]bool, data []graphMLData) {
	t.Helper()
	for _, entry := range data {
		if !keys[entry.Key] {
			t.Fatalf("GraphML data references undeclared key %q", entry.Key)
		}
	}
}

func TestProblemWritersAreDeterministicAndRejectNil(t *testing.T) {
	t.Parallel()
	problem := richExchangeProblem(t)
	for name, write := range map[string]func(*bytes.Buffer, *diagram.Problem) error{
		"dot": func(output *bytes.Buffer, problem *diagram.Problem) error { return WriteDOTProblem(output, problem) },
		"graphml": func(output *bytes.Buffer, problem *diagram.Problem) error {
			return WriteGraphMLProblem(output, problem)
		},
	} {
		name, write := name, write
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var first, second bytes.Buffer
			if err := write(&first, problem); err != nil {
				t.Fatal(err)
			}
			if err := write(&second, problem); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(first.Bytes(), second.Bytes()) {
				t.Fatal("successive outputs differ")
			}
			if err := write(&bytes.Buffer{}, nil); err == nil {
				t.Fatal("nil Problem was accepted")
			}
		})
	}
}

func TestProblemDOTReportsWriteFailure(t *testing.T) {
	t.Parallel()
	want := errors.New("write failed")
	writer := &failOnceWriter{err: want}
	if err := WriteDOTProblem(writer, richExchangeProblem(t)); !errors.Is(err, want) {
		t.Fatalf("WriteDOTProblem error = %v, want %v", err, want)
	}
}

type failOnceWriter struct {
	err    error
	failed bool
}

func (w *failOnceWriter) Write(payload []byte) (int, error) {
	if !w.failed {
		w.failed = true
		return 0, w.err
	}
	return len(payload), nil
}

func TestProblemDOTEmitsAndRestoresStandardSameRankConstraint(t *testing.T) {
	t.Parallel()
	builder := diagram.NewBuilder("test")
	first := builder.AddNode("first", "First", diagram.Span("test", 1, 1))
	second := builder.AddNode("second", "Second", diagram.Span("test", 2, 1))
	first.Rank.Same, second.Rank.Same = "peers", "peers"
	problem, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := WriteDOTProblem(&output, problem); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "rank=same") {
		t.Fatalf("standard rank constraint missing:\n%s", output.String())
	}
	got, err := ParseDOTProblem(strings.NewReader(output.String()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(problemSemantics(problem), problemSemantics(got)) {
		t.Fatalf("same-rank round trip differs\nwant: %#v\ngot:  %#v", problemSemantics(problem), problemSemantics(got))
	}
}

func TestProblemDOTUsesNewRankForRankedNodesInGroups(t *testing.T) {
	t.Parallel()
	builder := diagram.NewBuilder("test")
	group := builder.AddGroup("group", nil, "Group", "", 2, diagram.LineSolid, false, diagram.Span("test", 1, 1))
	first := builder.AddNode("first", "First", diagram.Span("test", 2, 1))
	second := builder.AddNode("second", "Second", diagram.Span("test", 3, 1))
	first.Rank.Same, second.Rank.Same = "peers", "peers"
	group.Members = []diagram.NodeID{first.ID, second.ID}
	builder.AddEdge(diagram.NodeEndpoint(first.ID), diagram.GroupEndpoint(group.ID), "", diagram.Directed, diagram.Span("test", 4, 1))
	problem, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := WriteDOTProblem(&output, problem); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "newrank=true") {
		t.Fatalf("grouped rank constraint is missing newrank=true:\n%s", output.String())
	}
	got, err := ParseDOTProblem(strings.NewReader(output.String()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(problemSemantics(problem), problemSemantics(got)) {
		t.Fatalf("grouped same-rank round trip differs\nwant: %#v\ngot:  %#v", problemSemantics(problem), problemSemantics(got))
	}
}

func TestParseDOTProblemAcceptsCanonicalGraphGroupAttributes(t *testing.T) {
	t.Parallel()
	outer := diagram.Group{ID: "outer", Label: "Outer", InputOrder: 0, Padding: 2, Source: diagram.Span("plantuml", 1, 1)}
	inner := diagram.Group{ID: "inner", Label: "Inner", Parent: outer.ID, InputOrder: 1, Padding: 3, Source: diagram.Span("plantuml", 2, 1)}
	input := fmt.Sprintf(`digraph {
  subgraph cluster_diagram_0 {
    graph [diagram_group_id=%s, diagram_group_semantics=%s, label=%s];
    subgraph cluster_diagram_1 {
      graph [diagram_group_id=%s, diagram_group_semantics=%s, label=%s];
      A;
    }
  }
  proxy [diagram_group_endpoint=%s];
  A -> proxy;
}`,
		dotQuote(string(outer.ID)), dotQuote(mustJSON(outer)), dotQuote(outer.Label),
		dotQuote(string(inner.ID)), dotQuote(mustJSON(inner)), dotQuote(inner.Label), dotQuote(string(outer.ID)))
	problem, err := ParseDOTProblem(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	gotOuter, outerOK := problem.Group(outer.ID)
	gotInner, innerOK := problem.Group(inner.ID)
	if !outerOK || !innerOK || gotInner.Parent != outer.ID || gotOuter.Source != outer.Source || gotInner.Source != inner.Source {
		t.Fatalf("canonical graph attributes lost groups: outer=%+v (%t), inner=%+v (%t)", gotOuter, outerOK, gotInner, innerOK)
	}
	edges := problem.Edges()
	if len(edges) != 1 || edges[0].Target.Kind != diagram.EndpointGroup || edges[0].Target.Group != outer.ID {
		t.Fatalf("group proxy was not resolved: %+v", edges)
	}
	if _, proxyExists := problem.Node("proxy"); proxyExists {
		t.Fatal("group proxy remains in Problem")
	}
}

func TestParseDOTProblemAcceptsGraphvizQuotedStrings(t *testing.T) {
	t.Parallel()
	input := `digraph { node [label="\N"]; A [diagram_node_semantics="{\"ID\":\"A\",\` + "\n" + `\"Label\":\"A\"}"]; }`
	problem, err := ParseDOTProblem(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	node, ok := problem.Node("A")
	if !ok || node.Label != "A" {
		t.Fatalf("node = %+v, ok=%t", node, ok)
	}
}

func TestParseDOTProblemRejectsMalformedSemanticPayload(t *testing.T) {
	t.Parallel()
	tests := map[string][]string{
		"node":  {`digraph { A [diagram_node_semantics="{"]; }`, `digraph { A [diagram_node_semantics=""]; }`},
		"edge":  {`digraph { A -> B [diagram_edge_semantics="{"]; }`, `digraph { A -> B [diagram_edge_semantics=""]; }`},
		"group": {`digraph { subgraph cluster_a { graph [diagram_group_id="a", diagram_group_semantics="{"]; A; } }`, `digraph { subgraph cluster_a { graph [diagram_group_id="a", diagram_group_semantics=""]; A; } }`},
	}
	for name, inputs := range tests {
		for index, input := range inputs {
			name, index, input := name, index, input
			t.Run(fmt.Sprintf("%s/%d", name, index), func(t *testing.T) {
				t.Parallel()
				_, err := ParseDOTProblem(strings.NewReader(input))
				if err == nil || !strings.Contains(err.Error(), "invalid diagram_"+name+"_semantics") {
					t.Fatalf("error = %v", err)
				}
			})
		}
	}
}

func TestProblemGraphMLKeepsNodeAndGroupNamespacesDistinct(t *testing.T) {
	t.Parallel()
	builder := diagram.NewBuilder("test")
	node := builder.AddNode("same", "Node", diagram.Span("test", 1, 1))
	group := builder.AddGroup("same", nil, "Group", "", 1, diagram.LineSolid, false, diagram.Span("test", 2, 1))
	target := builder.AddNode("target", "Target", diagram.Span("test", 3, 1))
	builder.AddEdge(diagram.GroupEndpoint(group.ID), diagram.NodeEndpoint(target.ID), "", diagram.Directed, diagram.Span("test", 4, 1))
	problem, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	if node.ID != diagram.NodeID(group.ID) {
		t.Fatal("test does not exercise a typed ID collision")
	}
	var output bytes.Buffer
	if err := WriteGraphMLProblem(&output, problem); err != nil {
		t.Fatal(err)
	}
	var document graphML
	if err := xml.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Graph.Nodes) != 3 || len(document.Graph.Edges) != 1 || document.Graph.Edges[0].Source != "g0" {
		t.Fatalf("typed namespace collision was not preserved: %+v", document.Graph)
	}
}

func TestPlantUMLProblemDOTRoundTripPreservesSemantics(t *testing.T) {
	t.Parallel()
	want, err := plantuml.ParseProblem(strings.NewReader(`@startuml
allowmixing
left to right direction
package Outer {
  package Inner {
    class User {
      id : INTEGER
    }
  }
  component Backend {
    portin request
  }
}
User::id --> request
Backend --> Outer
@enduml`))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := WriteDOTProblem(&output, want); err != nil {
		t.Fatal(err)
	}
	got, err := ParseDOTProblem(strings.NewReader(output.String()))
	if err != nil {
		t.Fatalf("parse generated DOT: %v", err)
	}
	if !reflect.DeepEqual(problemSemantics(want), problemSemantics(got)) {
		t.Fatalf("PlantUML semantic round trip differs\nwant: %#v\ngot:  %#v", problemSemantics(want), problemSemantics(got))
	}
}

func richExchangeProblem(t *testing.T) *diagram.Problem {
	t.Helper()
	builder := diagram.NewBuilder("test")
	builder.SetDirection(diagram.DirectionRight)
	outer := builder.AddGroup("outer", nil, "Outer", "", 3, diagram.LineDouble, false, diagram.Span("test", 1, 1))
	inner := builder.AddGroup("inner", nil, "Inner", outer.ID, 2, diagram.LineDashed, false, diagram.Span("test", 2, 1))
	inner.Ports = []diagram.NamedPort{{ID: "out", Direction: diagram.PortOutput, Source: inner.Source}}
	fixed := 2
	record := builder.AddNode("record node", "Record", diagram.Span("test", 3, 1))
	record.Shape = diagram.ShapeRounded
	record.Align = diagram.AlignRight
	record.TextWrap = 12
	record.Rank.Fixed = &fixed
	record.Cells = [][]diagram.Cell{{{Label: "Record"}}, {{ID: "id", Label: "id : INTEGER"}}}
	record.Ports = []diagram.NamedPort{{ID: "input", Direction: diagram.PortInput, Source: record.Source}}
	sink := builder.AddNode("sink", "Sink", diagram.Span("test", 4, 1))
	sink.Ports = []diagram.NamedPort{{ID: "in", Direction: diagram.PortInput, Source: sink.Source}}
	inner.Members = []diagram.NodeID{record.ID}
	outer.Members = []diagram.NodeID{sink.ID}
	first := builder.AddEdge(
		diagram.Endpoint{Kind: diagram.EndpointNode, Node: record.ID, Cell: "id"},
		diagram.GroupEndpoint(outer.ID), "lookup", diagram.Bidirectional, diagram.Span("test", 5, 1),
	)
	first.MinLength = 2
	first.Start.Side, first.End.Side = diagram.SideEast, diagram.SideWest
	first.Source.PortHint, first.Target.PortHint = first.Start, first.End
	first.LineStyle = diagram.LineDotted
	first.ArrowStyle = diagram.ArrowOpen
	first.LayoutReverse = true
	second := builder.AddEdge(
		diagram.Endpoint{Kind: diagram.EndpointGroup, Group: inner.ID, NamedPort: "out", PortHint: diagram.PortHint{Side: diagram.SideEast}},
		diagram.Endpoint{Kind: diagram.EndpointNode, Node: sink.ID, NamedPort: "in", PortHint: diagram.PortHint{Side: diagram.SideWest}},
		"deliver", diagram.Undirected, diagram.Span("test", 6, 1),
	)
	second.Start, second.End = second.Source.PortHint, second.Target.PortHint
	problem, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	return problem
}

type semanticProblem struct {
	Direction diagram.Direction
	Nodes     []diagram.Node
	Groups    []diagram.Group
	Edges     []diagram.Edge
}

func problemSemantics(problem *diagram.Problem) semanticProblem {
	nodes := problem.Nodes()
	groups := problem.Groups()
	edges := problem.Edges()
	return semanticProblem{Direction: problem.Direction(), Nodes: nodes, Groups: groups, Edges: edges}
}
