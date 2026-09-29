package plantuml

import (
	"errors"
	"strings"
	"testing"

	"github.com/xshoji/agents-workspace/internal/diagram"
)

func TestParseComponentsAliasesAndRelations(t *testing.T) {
	t.Parallel()
	graph := parseTest(t, `@startuml
' architecture
left to right direction
component "Web Client" as client
[API] as api
component [Primary\nDatabase] as db
client --> api : request
api ..> db : "write\nthrough"
@enduml`)
	if graph.Direction() != diagram.DirectionRight || len(graph.Nodes()) != 3 || len(graph.Edges()) != 2 {
		t.Fatalf("graph = %+v", graph)
	}
	if graph.Nodes()[0].ID != "client" || graph.Nodes()[0].Label != "Web Client" || graph.Nodes()[2].Label != "Primary\nDatabase" {
		t.Fatalf("nodes = %+v", graph.Nodes())
	}
	if graph.Edges()[1].LineStyle != diagram.LineDotted || graph.Edges()[1].Label != "write\nthrough" {
		t.Fatalf("edge = %+v", graph.Edges()[1])
	}
}

func TestParseRelationKindsStylesAndLength(t *testing.T) {
	t.Parallel()
	graph := parseTest(t, `@startuml
A -- B
B <--> C
C -[bold]-> D
D -[dashed]-> E
E -[hidden]-> F
F ----> G
@enduml`)
	if len(graph.Edges()) != 6 || graph.Edges()[0].Kind != diagram.Undirected || graph.Edges()[1].Kind != diagram.Bidirectional {
		t.Fatalf("edges = %+v", graph.Edges())
	}
	styles := []diagram.LineStyle{diagram.LineSolid, diagram.LineSolid, diagram.LineBold, diagram.LineDashed, diagram.LineInvisible, diagram.LineSolid}
	for index, want := range styles {
		if graph.Edges()[index].LineStyle != want {
			t.Fatalf("edge %d style = %v, want %v", index, graph.Edges()[index].LineStyle, want)
		}
	}
	if graph.Edges()[5].MinLength != 3 {
		t.Fatalf("min length = %d, want 3", graph.Edges()[5].MinLength)
	}
}

func TestParsePackagesClassesAndStructuredEndpoints(t *testing.T) {
	t.Parallel()
	graph := parseTest(t, `@startuml
allowmixing
package System {
  component Web as web
  package Backend {
    class "User Record" as user {
      id : INTEGER
      ..
      name : TEXT
    }
  }
}
user::id --> web
Backend --> web
@enduml`)
	if len(graph.Groups()) != 2 || graph.Groups()[1].Parent != "System" {
		t.Fatalf("groups = %+v", graph.Groups())
	}
	user, ok := graph.Node("user")
	if !ok || len(user.Cells) != 3 || user.Cells[2][0].ID != "name" {
		t.Fatalf("user = %+v", user)
	}
	if graph.Edges()[0].Source.Cell != "id" || graph.Edges()[1].Source.Kind != diagram.EndpointGroup {
		t.Fatalf("edges = %+v", graph.Edges())
	}
}

func TestParseComponentPorts(t *testing.T) {
	t.Parallel()
	graph := parseTest(t, `@startuml
component Backend {
  portin request
  portout result
  component Worker as worker
  request --> worker
  worker --> result
}
[Client] --> request
result --> [Database]
@enduml`)
	backend, ok := graph.Group("Backend")
	if !ok || len(backend.Ports) != 2 || backend.Ports[0].Direction != diagram.PortInput || backend.Ports[1].Direction != diagram.PortOutput {
		t.Fatalf("backend = %+v", backend)
	}
	if len(graph.Edges()) != 4 || graph.Edges()[0].Source.NamedPort != "request" || graph.Edges()[1].Target.NamedPort != "result" {
		t.Fatalf("edges = %+v", graph.Edges())
	}
}

func TestParseDirectionHintSetsAttachmentSides(t *testing.T) {
	t.Parallel()
	graph := parseTest(t, "@startuml\nA -left-> B\nC -down-> D\nE -up-> F\n@enduml")
	if graph.Edges()[0].Start.Side != diagram.SideWest || graph.Edges()[0].End.Side != diagram.SideEast ||
		graph.Edges()[1].Start.Side != diagram.SideSouth || graph.Edges()[1].End.Side != diagram.SideNorth {
		t.Fatalf("edges = %+v", graph.Edges())
	}
	if !graph.Edges()[2].LayoutReverse {
		t.Fatalf("upward direction was not retained as a reverse layout constraint: %+v", graph.Edges()[2])
	}
}

func TestParseLeftArrowKeepsTextualDirectionHint(t *testing.T) {
	t.Parallel()
	graph := parseTest(t, "@startuml\nA <-up- B\nC <-right- D\nE <-- F\n@enduml")
	up, right, unhinted := graph.Edges()[0], graph.Edges()[1], graph.Edges()[2]
	if up.Source.ID() != "B" || up.Target.ID() != "A" || up.LayoutReverse ||
		up.Start.Side != diagram.SideSouth || up.End.Side != diagram.SideNorth {
		t.Fatalf("left/up edge = %+v", up)
	}
	if right.Source.ID() != "D" || right.Target.ID() != "C" || !right.LayoutReverse ||
		right.Start.Side != diagram.SideWest || right.End.Side != diagram.SideEast {
		t.Fatalf("left/right edge = %+v", right)
	}
	if unhinted.Source.ID() != "F" || unhinted.Target.ID() != "E" || unhinted.LayoutReverse {
		t.Fatalf("unhinted left edge = %+v", unhinted)
	}
}

func TestParseRejectsPortAndElementAliasCollisions(t *testing.T) {
	t.Parallel()
	inputs := []string{
		"@startuml\ncomponent Backend {\n portin api\n}\ncomponent api\n@enduml",
		"@startuml\ncomponent api\ncomponent Backend {\n portin api\n}\n@enduml",
	}
	for _, input := range inputs {
		if _, err := ParseProblem(strings.NewReader(input)); !errors.Is(err, ErrInvalidSyntax) {
			t.Fatalf("error = %v, want ErrInvalidSyntax for %q", err, input)
		}
	}
}

func TestParseRequiresAllowMixingForImplicitComponent(t *testing.T) {
	t.Parallel()
	input := "@startuml\nclass User {\n id : INTEGER\n}\nUser --> Validator\n@enduml"
	if _, err := ParseProblem(strings.NewReader(input)); !errors.Is(err, ErrInvalidSyntax) {
		t.Fatalf("error = %v, want ErrInvalidSyntax", err)
	}
	if _, err := ParseProblem(strings.NewReader(strings.Replace(input, "@startuml", "@startuml\nallowmixing", 1))); err != nil {
		t.Fatalf("allowmixing input: %v", err)
	}
	if _, err := ParseProblem(strings.NewReader("@startuml\nclass User\nclass Validator\nUser --> Validator\n@enduml")); err != nil {
		t.Fatalf("class-only relation: %v", err)
	}
}

func TestParseRelationsWithoutSpacesAndInterleavedPorts(t *testing.T) {
	t.Parallel()
	graph := parseTest(t, `@startuml
component C {
  component worker
  portin request
  request-->worker
  portout result
  worker-->result
}
[Client]-->request
result-->[Database]
@enduml`)
	group, ok := graph.Group("C")
	if !ok || len(group.Ports) != 2 || len(graph.Edges()) != 4 {
		t.Fatalf("group = %+v, edges = %+v", group, graph.Edges())
	}
}

func TestParseProblemPreservesTypedEndpointsContainmentAndSources(t *testing.T) {
	t.Parallel()
	problem, err := ParseProblem(strings.NewReader(`@startuml
allowmixing
package Outer {
  package Inner {
    class User {
      id : INTEGER
    }
  }
  component Backend {
    portin request
    component Worker
  }
}
User::id --> request
Implicit --> Outer
@enduml`))
	if err != nil {
		t.Fatal(err)
	}
	user, ok := problem.Node("User")
	if !ok || user.Source.Format != "plantuml" || user.Source.Start.Line != 5 {
		t.Fatalf("user source = %+v, ok=%t", user.Source, ok)
	}
	if len(user.Cells) != 2 || user.Cells[1][0].ID != "id" || user.Cells[1][0].Source.Start.Line != 6 {
		t.Fatalf("member cell = %+v", user.Cells)
	}
	inner, ok := problem.Group("Inner")
	if !ok || inner.Parent != "Outer" {
		t.Fatalf("inner = %+v, ok=%t", inner, ok)
	}
	if parent, ok := problem.ParentOf(diagram.ElementRef{Kind: diagram.ElementNode, Node: "User"}); !ok || parent != "Inner" {
		t.Fatalf("User parent = %q, %t", parent, ok)
	}
	edges := problem.Edges()
	if len(edges) != 2 || edges[0].Source.Node != "User" || edges[0].Source.Cell != "id" ||
		edges[0].Target.Group != "Backend" || edges[0].Target.NamedPort != "request" || edges[0].SourceSpan.Start.Line != 14 {
		t.Fatalf("typed edge = %+v", edges[0])
	}
	if edges[1].Source.Node != "Implicit" || edges[1].Target.Kind != diagram.EndpointGroup || edges[1].Target.Group != "Outer" {
		t.Fatalf("implicit/group edge = %+v", edges[1])
	}
}

func TestParseRejectsUnsupportedOrInvalidInput(t *testing.T) {
	t.Parallel()
	tests := []string{
		"A --> B",
		"@startuml\nA --> B",
		"@startuml\nA --> B --> C\n@enduml",
		"@startuml\nskinparam ArrowColor red\n@enduml",
		"@startuml\nclass A {\n id\n id\n}\n@enduml",
		"@startuml\nclass A\ncomponent B\n@enduml",
		"@startuml\nA -[rainbow]-> B\n@enduml",
	}
	for _, input := range tests {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			_, err := ParseProblem(strings.NewReader(input))
			if !errors.Is(err, ErrInvalidSyntax) {
				t.Fatalf("error = %v, want ErrInvalidSyntax", err)
			}
		})
	}
}

func TestStrictModeRejectsIgnoredRenderingStatement(t *testing.T) {
	t.Parallel()
	_, err := ParseProblemWithOptions(strings.NewReader("@startuml\nskinparam componentStyle rectangle\nA --> B\n@enduml"), Options{Strict: true})
	if !errors.Is(err, ErrInvalidSyntax) {
		t.Fatalf("error = %v, want ErrInvalidSyntax", err)
	}
}

func FuzzParseDocument(f *testing.F) {
	f.Add("@startuml\nA --> B\n@enduml")
	f.Add("@startuml\nclass A {\nid : INTEGER\n}\n@enduml")
	f.Fuzz(func(t *testing.T, input string) {
		_, _ = ParseDocument(strings.NewReader(input))
	})
}

func parseTest(t *testing.T, input string) *diagram.Problem {
	t.Helper()
	graph, err := ParseProblem(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	return graph
}
