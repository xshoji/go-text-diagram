package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xshoji/go-text-diagram/internal/textwidth"
)

func TestRun(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	err := run([]string{"--direction=right"}, pumlReader("A --> B\nB --> C"), &output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "▶") || !strings.Contains(output.String(), "C │") {
		t.Fatalf("unexpected output:\n%s", output.String())
	}
}

func TestRunASCII(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	if err := run([]string{"--ascii"}, pumlReader("A --> B"), &output); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(output.String(), "┌─┐│└┘▶▼") || !strings.Contains(output.String(), "v") {
		t.Fatalf("unexpected ASCII output:\n%s", output.String())
	}
}

func TestRunDebugLayout(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	if err := run([]string{"--debug-layout"}, pumlReader("A --> B"), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "direction: down") || !strings.Contains(output.String(), "routes:") {
		t.Fatalf("unexpected debug output:\n%s", output.String())
	}
}

func TestRunHelp(t *testing.T) {
	t.Parallel()

	for _, argument := range []string{"-h", "--help"} {
		argument := argument
		t.Run(argument, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer
			if err := run([]string{argument}, strings.NewReader(""), &output); err != nil {
				t.Fatal(err)
			}
			help := output.String()
			for _, expected := range []string{"Usage: go-text-diagram [options] [input-file]", "standard input", "-direction", "-input-format", "-output-format"} {
				if !strings.Contains(help, expected) {
					t.Errorf("help output does not contain %q:\n%s", expected, help)
				}
			}
		})
	}
}

func TestRunRejectsConflictingStyles(t *testing.T) {
	t.Parallel()

	err := run([]string{"--ascii", "--unicode"}, pumlReader("component A"), &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected conflicting style error")
	}
}

func TestRunDOTInputAndExchangeOutputs(t *testing.T) {
	t.Parallel()

	input := `digraph G { graph [rankdir=LR]; A [label="API"]; A -> B [label="request"]; }`
	var diagram bytes.Buffer
	if err := run([]string{"--input-format=dot"}, strings.NewReader(input), &diagram); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diagram.String(), "API") || !strings.Contains(diagram.String(), "request") {
		t.Fatalf("diagram output:\n%s", diagram.String())
	}

	var graphml bytes.Buffer
	if err := run([]string{"--input-format=dot", "--output-format=graphml"}, strings.NewReader(input), &graphml); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(graphml.String(), `<graphml`) || !strings.Contains(graphml.String(), `source="n0"`) {
		t.Fatalf("GraphML output:\n%s", graphml.String())
	}
}

func TestRunNestedDOTThroughSolutionSnapshot(t *testing.T) {
	t.Parallel()
	input := `digraph {
  subgraph cluster_outer {
    subgraph cluster_inner {
      label="inner label much wider than its parent";
      A;
    }
  }
}`
	var output bytes.Buffer
	if err := run([]string{"--input-format=dot"}, strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	if diagram := output.String(); !strings.Contains(diagram, "A") || !strings.Contains(diagram, "inner label much wider than its parent") {
		t.Fatalf("nested DOT diagram output:\n%s", diagram)
	}
}

func TestRunMaxWidthWrapsDisconnectedComponents(t *testing.T) {
	t.Parallel()

	graph := "@startuml\n[A]\n[B]\n@enduml\n"
	var output bytes.Buffer
	if err := run([]string{"--max-width=5", "--debug-layout"}, strings.NewReader(graph), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `node "B" order=1 rect=(0,4`) {
		t.Fatalf("components were not wrapped:\n%s", output.String())
	}
}

func TestRunMaxWidthSplitsConnectedDiagramIntoViewportPanels(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	if err := run([]string{"--direction=right", "--max-width=20"}, pumlReader("LongSourceNode --> LongTargetNode : request"), &output); err != nil {
		t.Fatal(err)
	}
	for number, line := range strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n") {
		if width := textwidth.String(line); width > 20 {
			t.Fatalf("line %d width = %d, want at most 20: %q", number+1, width, line)
		}
	}
	if !strings.Contains(output.String(), "LongSourceNode") || !strings.Contains(output.String(), "LongTargetNode") {
		t.Fatalf("viewport output lost nodes:\n%s", output.String())
	}
}

func TestRunRoutesReversedEdgeAroundUnrelatedGroupNode(t *testing.T) {
	input, err := os.ReadFile(filepath.Join("testdata", "regression", "iot-edge-fleet.puml"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run([]string{"--debug-layout"}, bytes.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	debug := output.String()
	for _, expected := range []string{`e4 "local_rules" -> "plc"`, `fallback=true label="control command" label_placed=true`, "edge_node_collisions=0", "unrouted=0", "unplaced_labels=0"} {
		if !strings.Contains(debug, expected) {
			t.Fatalf("debug output is missing %q:\n%s", expected, debug)
		}
	}
}

func TestRunLeavesLineCellBeforeNodeArrow(t *testing.T) {
	input, err := os.ReadFile(filepath.Join("testdata", "regression", "digital-bank-payments.puml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "right"},
		{name: "down", args: []string{"--direction=down"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := run(test.args, bytes.NewReader(input), &output); err != nil {
				t.Fatal(err)
			}
			if output.Len() == 0 {
				t.Fatal("rendered output is empty")
			}
			if test.name == "right" && !strings.Contains(output.String(), "─▶ OAuth Server") {
				t.Fatalf("OAuth arrow has no line cell after its final bend:\n%s", output.String())
			}
		})
	}
}

func TestRunRejectsRenderingOptionsForExchangeOutput(t *testing.T) {
	t.Parallel()

	err := run([]string{"--output-format=dot", "--max-width=40"}, pumlReader("component A"), &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected rendering option error")
	}
}

func TestRunRejectsRemovedDiagramInputFormatAndLegacySyntax(t *testing.T) {
	t.Parallel()

	if err := run([]string{"--input-format=diagram"}, pumlReader("A --> B"), &bytes.Buffer{}); err == nil {
		t.Fatal("expected removed diagram input format to be rejected")
	}
	if err := run(nil, strings.NewReader("graph { flow: east; }\n[A] -> [B]\n"), &bytes.Buffer{}); err == nil {
		t.Fatal("expected legacy DSL to be rejected")
	}
}

func TestE2E(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		args           []string
		edges          int
		maxWidth       int
		maxHeight      int
		expected       []string
		allowedQuality []string
	}{
		{name: "01-basic-chain", edges: 2, expected: []string{"│ A │", "│ B │", "│ C │", "▼"}},
		{name: "02-branch-and-labels", edges: 4, expected: []string{"left", "right", "│ D │"}},
		{name: "03-right-cjk-multiline", edges: 2, expected: []string{"利用者", "Client", "要求", "200 ✅", "Database"}},
		{name: "04-cycles-loops-parallel", edges: 6, expected: []string{"│ A │", "│ B ", "│ C │", "▲", "▼"}, allowedQuality: []string{"crossings", "overlaps", "reverse_moves"}},
		{name: "05-edge-kinds-and-styles", edges: 4, expected: []string{"┄", "╌", "━", "◀", "▶"}},
		{name: "06-rank-minlen-ports", edges: 4, expected: []string{"root", "left", "right", "sink"}, allowedQuality: []string{"crossings", "overlaps"}},
		{name: "07-shapes-record-group", edges: 2, expected: []string{"Name", "Type", "id", "int", "Service"}},
		{name: "08-nested-groups", edges: 2, expected: []string{"System", "Backend", "web", "api", "db"}},
		{name: "09-cell-and-group-endpoints", edges: 2, expected: []string{"識別子", "Name", "Services", "target"}, allowedQuality: []string{"crossings", "overlaps", "reverse_moves"}},
		{name: "10-disconnected-packing", args: []string{"--max-width=21"}, edges: 0, expected: []string{"Alpha │ │ Beta", "Gamma │ │ Delta"}},
		{name: "11-complex-optimized", edges: 7, expected: []string{"Client", "Gateway", "ID", "Name", "Role", "orders", "cache", "database", "request", "lookup", "create", "retry"}, allowedQuality: []string{"crossings", "reverse_moves"}},
		{name: "12-component-ports", edges: 4, expected: []string{"Backend", "Worker", "Client", "Database"}, allowedQuality: []string{"crossings", "reverse_moves"}},
		{name: "13-large-system", edges: 49, maxWidth: 450, maxHeight: 220, expected: []string{"Web Browser", "API Platform", "Application Services", "Data Platform", "Event Platform", "Operations", "Primary Database"}, allowedQuality: []string{"crossings", "overlaps", "reverse_moves", "unplaced_labels"}},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input, err := os.ReadFile(filepath.Join("testdata", "e2e", test.name+".puml"))
			if err != nil {
				t.Fatal(err)
			}
			output := runE2E(t, test.args, input)
			if output == "" || strings.ContainsRune(output, '\ufffd') {
				t.Fatalf("invalid rendered output:\n%s", output)
			}
			for _, expected := range test.expected {
				if !strings.Contains(output, expected) {
					t.Fatalf("missing %q:\n%s", expected, output)
				}
			}
			for iteration := 0; iteration < 3; iteration++ {
				if repeated := runE2E(t, test.args, input); repeated != output {
					t.Fatalf("render changed on iteration %d\nfirst:\n%s\nrepeated:\n%s", iteration, output, repeated)
				}
			}

			debugArgs := append(append([]string(nil), test.args...), "--debug-layout")
			debug := runE2E(t, debugArgs, input)
			if !strings.Contains(debug, "optimization:") || routeCount(debug) != test.edges {
				t.Fatalf("unexpected routed edge count, want %d:\n%s", test.edges, debug)
			}
			var width, height int
			if _, err := fmt.Sscanf(debug[strings.Index(debug, "bounds:"):], "bounds: %dx%d", &width, &height); err != nil {
				t.Fatalf("invalid layout bounds: %v\n%s", err, debug)
			}
			if test.maxWidth > 0 && width > test.maxWidth || test.maxHeight > 0 && height > test.maxHeight {
				t.Fatalf("layout bounds %dx%d exceed limit %dx%d", width, height, test.maxWidth, test.maxHeight)
			}
			assertE2EQuality(t, debug, test.allowedQuality)
		})
	}
}

func pumlReader(body string) *strings.Reader {
	return strings.NewReader("@startuml\n" + body + "\n@enduml\n")
}

func runE2E(t *testing.T, args []string, input []byte) string {
	t.Helper()
	var output bytes.Buffer
	if err := run(args, bytes.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func routeCount(debug string) int {
	start := strings.Index(debug, "routes:\n")
	end := strings.Index(debug, "route_metrics:")
	if start < 0 {
		return 0
	}
	if end < start {
		end = len(debug)
	}
	return strings.Count(debug[start:end], "\n  e")
}

func assertE2EQuality(t *testing.T, debug string, allowed []string) {
	t.Helper()
	allowedSet := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = true
	}
	metricsStart := strings.Index(debug, "route_metrics:")
	if metricsStart < 0 {
		t.Fatalf("route metrics are missing:\n%s", debug)
	}
	for _, field := range strings.Fields(debug[metricsStart:])[1:] {
		name, value, ok := strings.Cut(field, "=")
		if !ok || allowedSet[name] {
			continue
		}
		if value != "0" && name != "length" && name != "excess_length" && name != "bends" && name != "astar_fallbacks" {
			t.Fatalf("unexpected quality regression %s:\n%s", field, debug)
		}
	}
}
