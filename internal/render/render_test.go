package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xshoji/go-text-diagram/internal/canvas"
	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/layout"
	"github.com/xshoji/go-text-diagram/internal/plantuml"
	"github.com/xshoji/go-text-diagram/internal/route"
	"github.com/xshoji/go-text-diagram/internal/solution"
	"github.com/xshoji/go-text-diagram/internal/textwidth"
)

func TestDiagramChain(t *testing.T) {
	t.Parallel()

	snapshot := renderInput(t, "A -> B\n")
	want := "┌───┐\n│ A │\n└─┬─┘\n  │\n  │\n┌─▼─┐\n│ B │\n└───┘\n"
	if got := Diagram(snapshot, Unicode); got != want {
		t.Fatalf("diagram:\n%s\nwant:\n%s", got, want)
	}
}

func TestDiagramASCIIAndRightDirection(t *testing.T) {
	t.Parallel()

	snapshot := renderInput(t, "left to right direction\nA --> B\n")
	want := "+---+  +---+\n| A +--> B |\n+---+  +---+\n"
	if got := Diagram(snapshot, ASCII); got != want {
		t.Fatalf("diagram:\n%s\nwant:\n%s", got, want)
	}
}

func TestASCIIRendersOnlyDiagramShapesAsASCII(t *testing.T) {
	t.Parallel()

	c := canvas.New(6, 4)
	c.Connect(route.Point{X: 0, Y: 1}, route.Point{X: 4, Y: 1}, "horizontal")
	c.Connect(route.Point{X: 2, Y: 0}, route.Point{X: 2, Y: 2}, "vertical")
	drawArrowAt(c, route.Point{X: 3, Y: 1}, route.Point{X: 4, Y: 1}, ASCII, diagram.ArrowDefault)
	c.WriteText(route.Point{X: 0, Y: 3}, "日本語")

	want := "  |\n--+->\n  |\n日本語\n"
	if got := c.String(canvas.ASCII); got != want {
		t.Fatalf("ASCII shapes with Unicode label:\n%s\nwant:\n%s", got, want)
	}
}

func TestDiagramRendersEdgeKinds(t *testing.T) {
	t.Parallel()
	snapshot := renderInput(t, "A -- B\nB <--> C\n")
	output := Diagram(snapshot, Unicode)
	if strings.Count(output, "▶")+strings.Count(output, "▼") < 1 ||
		strings.Count(output, "◀")+strings.Count(output, "▲") < 1 {
		t.Fatalf("bidirectional arrows are missing:\n%s", output)
	}
}

func TestDiagramRendersStylesGroupsAndRecords(t *testing.T) {
	t.Parallel()
	input := `allowmixing
class Record {
A
B
C
D
}
package Container {
component Target
}
Record -[bold]-> Target
`
	snapshot := renderInput(t, input)
	output := Diagram(snapshot, Unicode)
	for _, value := range []string{" Container ", "A", "B", "C", "D", "┃", "▼"} {
		if !strings.Contains(output, value) {
			t.Fatalf("missing %q:\n%s", value, output)
		}
	}
}

func TestArrowStylesAreDistinctWithoutChangingDefault(t *testing.T) {
	t.Parallel()
	c := canvas.New(3, 4)
	drawArrowAt(c, route.Point{X: 0, Y: 0}, route.Point{X: 1, Y: 0}, Unicode, diagram.ArrowDefault)
	drawArrowAt(c, route.Point{X: 0, Y: 1}, route.Point{X: 1, Y: 1}, Unicode, diagram.ArrowClosed)
	drawArrowAt(c, route.Point{X: 0, Y: 2}, route.Point{X: 1, Y: 2}, Unicode, diagram.ArrowOpen)
	drawArrowAt(c, route.Point{X: 0, Y: 3}, route.Point{X: 1, Y: 3}, Unicode, diagram.ArrowFilled)
	output := c.String(canvas.Unicode)
	for _, arrow := range []string{"▶", "▷", "→"} {
		if !strings.Contains(output, arrow) {
			t.Fatalf("missing %q:\n%s", arrow, output)
		}
	}
	if strings.Count(output, "▶") != 2 {
		t.Fatalf("default and filled arrows changed:\n%s", output)
	}
}

func TestAnonymousGroupDoesNotRenderInternalID(t *testing.T) {
	t.Parallel()
	snapshot := renderInput(t, "package Group {\ncomponent A\n}\n")
	output := Diagram(snapshot, Unicode)
	if strings.Contains(output, "__group_") {
		t.Fatalf("internal group ID leaked:\n%s", output)
	}
}

func TestDiagramMultilineAndJapaneseLabel(t *testing.T) {
	t.Parallel()

	snapshot := renderInput(t, `component "利用者\nClient" as client`+"\n")
	want := "┌────────┐\n│ 利用者 │\n│ Client │\n└────────┘\n"
	if got := Diagram(snapshot, Unicode); got != want {
		t.Fatalf("diagram:\n%s\nwant:\n%s", got, want)
	}
}

func TestConstrainWidthPreservesWideGraphemeClusters(t *testing.T) {
	t.Parallel()

	output := ConstrainWidth("x日本語\n", 2)
	for number, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		if width := textwidth.String(line); width > 2 {
			t.Fatalf("line %d width = %d, want at most 2: %q", number+1, width, line)
		}
	}
	for _, cluster := range []string{"日", "本", "語"} {
		if strings.Count(output, cluster) != 1 {
			t.Fatalf("wide cluster %q was split or lost:\n%s", cluster, output)
		}
	}
}

func TestDiagramMultilineEdgeLabel(t *testing.T) {
	t.Parallel()

	snapshot := renderInput(t, "left to right direction\nA --> B: \"成功\\n200 ✅\"\n")
	want := "┌───┐        ┌───┐\n│ A ├─ 成功 ─▶ B │\n└───┘ 200 ✅ └───┘\n"
	if got := Diagram(snapshot, Unicode); got != want {
		t.Fatalf("edge label diagram:\n%s\nwant:\n%s", got, want)
	}
	if routes := snapshot.Routes(); len(routes) != 1 || !routes[0].LabelPlaced {
		t.Fatal("edge label was not placed")
	}
}

func TestDiagramCycle(t *testing.T) {
	t.Parallel()

	snapshot := renderInput(t, "A --> B\nB --> C\nC --> A\n")
	want := "  ┌───┐\n  │ A │\n  └─▲─┘\n  ┌─┤\n  │ └─────┐\n  │       │\n┌─▼─┐     │\n│ B │     │\n└─┬─┘     │\n  └─┐     │\n    │     │\n    │     │\n  ┌─▼─┐   │\n  │ C │   │\n  └─┬─┘   │\n    └─────┘\n"
	if got := Diagram(snapshot, Unicode); got != want {
		t.Fatalf("cycle diagram:\n%s\nwant:\n%s", got, want)
	}
}

func TestDiagramMultipleSelfLoops(t *testing.T) {
	t.Parallel()

	snapshot := renderInput(t, "A -> A\nA -> A\nA -> A\n")
	want := "┌◀◀◀┬┬┬┐\n│ A ├┴┴┘\n└───┘\n"
	if got := Diagram(snapshot, Unicode); got != want {
		t.Fatalf("loop diagram:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderedCorpusIsDeterministic(t *testing.T) {
	t.Parallel()

	const input = "A -> B: left\nA -> C: right\nB -> D\nC -> D: merge\nA -> D: long\n"
	snapshot := renderInput(t, input)
	want := Diagram(snapshot, Unicode)
	for i := 0; i < 10; i++ {
		snapshot = renderInput(t, input)
		if got := Diagram(snapshot, Unicode); got != want {
			t.Fatalf("render changed on run %d:\n%s", i, got)
		}
	}
}

func TestRenderedCorpusFitsLayoutBounds(t *testing.T) {
	files, err := filepath.Glob("../layout/testdata/corpus/*.puml")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		file := file
		t.Run(filepath.Base(file), func(t *testing.T) {
			t.Parallel()
			input, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			snapshot := renderInput(t, string(input))
			output := Diagram(snapshot, Unicode)
			nodes := snapshot.Nodes()
			bounds := snapshot.Bounds()
			if len(nodes) == 0 {
				if output != "" {
					t.Fatalf("empty graph rendered %q", output)
				}
				return
			}
			if output == "" {
				t.Fatal("non-empty graph rendered no output")
			}
			lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
			if len(lines) > bounds.Height {
				t.Fatalf("rendered height %d exceeds %d", len(lines), bounds.Height)
			}
			for lineNumber, line := range lines {
				if width := textwidth.String(line); width > bounds.Width {
					t.Fatalf("line %d width %d exceeds %d: %q", lineNumber+1, width, bounds.Width, line)
				}
			}
		})
	}
}

func renderInput(t *testing.T, input string) *solution.Snapshot {
	t.Helper()
	problem, err := plantuml.ParseProblem(plantUMLReader(input))
	if err != nil {
		t.Fatal(err)
	}
	graph := problem
	result, err := layout.Compute(graph, layout.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	routes, metrics, _ := route.ComputeBaselineWithDiagnostics(result)
	snapshot, err := solution.FromLegacy(problem, result, routes, metrics)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func plantUMLReader(input string) *strings.Reader {
	if strings.HasPrefix(strings.TrimSpace(input), "@startuml") {
		return strings.NewReader(input)
	}
	return strings.NewReader("@startuml\n" + input + "@enduml\n")
}
