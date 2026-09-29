package exchange

import (
	"strings"
	"testing"

	"github.com/xshoji/agents-workspace/internal/diagram"
)

func TestParseDOTProblemPreservesNestedContainmentAndSources(t *testing.T) {
	t.Parallel()
	input := `digraph {
  subgraph cluster_outer {
    subgraph cluster_inner { A; }
    A:e -> B:w;
  }
}`
	problem, err := ParseDOTProblem(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	inner, ok := problem.Group("cluster_inner")
	if !ok || inner.Parent != "cluster_outer" || inner.Source.Format != "dot" || inner.Source.Start != (diagram.Position{Line: 3, Column: 5}) {
		t.Fatalf("inner = %+v, ok=%t", inner, ok)
	}
	if parent, ok := problem.ParentOf(diagram.ElementRef{Kind: diagram.ElementNode, Node: "A"}); !ok || parent != inner.ID {
		t.Fatalf("A parent = %q, %t", parent, ok)
	}
	outer, _ := problem.Group("cluster_outer")
	if len(outer.Members) != 1 || outer.Members[0] != "B" {
		t.Fatalf("outer direct members = %v", outer.Members)
	}
	edges := problem.Edges()
	if len(edges) != 1 || edges[0].Source.PortHint.Side != diagram.SideEast || edges[0].Target.PortHint.Side != diagram.SideWest || edges[0].SourceSpan.Start != (diagram.Position{Line: 4, Column: 5}) {
		t.Fatalf("edge = %+v", edges)
	}
}

func TestParseDOTProblemRejectsDuplicateClusterBeforeContainmentResolution(t *testing.T) {
	t.Parallel()
	_, err := ParseDOTProblem(strings.NewReader(`digraph {
  subgraph cluster_a {
    subgraph cluster_b {
      subgraph cluster_a { X; }
      X;
    }
  }
  subgraph cluster_c { X; }
}`))
	if err == nil || !strings.Contains(err.Error(), `duplicate group ID "cluster_a"`) {
		t.Fatalf("error = %v", err)
	}
}
