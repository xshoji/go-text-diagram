package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/exchange"
	"github.com/xshoji/go-text-diagram/internal/layout"
	"github.com/xshoji/go-text-diagram/internal/plantuml"
	"github.com/xshoji/go-text-diagram/internal/render"
	"github.com/xshoji/go-text-diagram/internal/solution"
	"github.com/xshoji/go-text-diagram/internal/solve"
)

func main() {
	if err := runWithStderr(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "go-text-diagram:", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	return runWithStderr(args, stdin, stdout, io.Discard)
}

func runWithStderr(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("go-text-diagram", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	direction := flags.String("direction", "", "layout direction: down, right, up, or left")
	inputFormat := flags.String("input-format", "plantuml", "input format: plantuml or dot")
	outputFormat := flags.String("output-format", "diagram", "output format: diagram, dot, or graphml")
	maxWidth := flags.Int("max-width", 0, "maximum rendered line width; connected diagrams are split into viewport panels; 0 disables wrapping")
	ascii := flags.Bool("ascii", false, "render diagram shapes with ASCII characters")
	unicode := flags.Bool("unicode", false, "render with Unicode box-drawing characters (default)")
	debugLayout := flags.Bool("debug-layout", false, "print layout and route details instead of a diagram")
	strict := flags.Bool("strict", false, "reject supported PlantUML statements that have no ASCII rendering effect")
	flags.Usage = func() {
		fmt.Fprintln(stdout, "Usage: go-text-diagram [options] [input-file]")
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Render a PlantUML component diagram as Unicode or ASCII.")
		fmt.Fprintln(stdout, "If input-file is omitted or is -, input is read from standard input.")
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Options:")
		flags.SetOutput(stdout)
		flags.PrintDefaults()
		flags.SetOutput(io.Discard)
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if *ascii && *unicode {
		return fmt.Errorf("--ascii and --unicode cannot be used together")
	}
	if *maxWidth < 0 || *maxWidth == 1 {
		return fmt.Errorf("--max-width must be zero or at least 2")
	}
	if *debugLayout && *outputFormat != "diagram" {
		return fmt.Errorf("--debug-layout requires --output-format=diagram")
	}
	if *outputFormat != "diagram" && (*ascii || *unicode || *maxWidth != 0) {
		return fmt.Errorf("--ascii, --unicode, and --max-width require --output-format=diagram")
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("accepts at most one input file")
	}

	input := stdin
	if flags.NArg() == 1 && flags.Arg(0) != "-" {
		file, err := os.Open(flags.Arg(0))
		if err != nil {
			return fmt.Errorf("open input: %w", err)
		}
		defer file.Close()
		input = file
	}

	var problem *diagram.Problem
	var err error
	switch *inputFormat {
	case "plantuml":
		problem, err = plantuml.ParseProblemWithOptions(input, plantuml.Options{Strict: *strict})
	case "dot":
		if *strict {
			return fmt.Errorf("--strict requires --input-format=plantuml")
		}
		problem, err = exchange.ParseDOTProblem(input)
	default:
		return fmt.Errorf("unknown input format %q; want plantuml or dot", *inputFormat)
	}
	if err != nil {
		return err
	}
	if *direction != "" {
		var requested diagram.Direction
		switch *direction {
		case "down":
			requested = diagram.DirectionDown
		case "right":
			requested = diagram.DirectionRight
		case "up":
			requested = diagram.DirectionUp
		case "left":
			requested = diagram.DirectionLeft
		default:
			return fmt.Errorf("unknown direction %q; want down, right, up, or left", *direction)
		}
		problem = problem.WithDirection(requested)
	}
	switch *outputFormat {
	case "dot":
		return exchange.WriteDOTProblem(stdout, problem)
	case "graphml":
		return exchange.WriteGraphMLProblem(stdout, problem)
	case "diagram":
	default:
		return fmt.Errorf("unknown output format %q; want diagram, dot, or graphml", *outputFormat)
	}

	options := layout.DefaultOptions()
	options.MaxWidth = *maxWidth
	optimized, err := solve.ComputeProblem(problem, options)
	if err != nil {
		return err
	}
	snapshot := optimized.Snapshot
	if !*debugLayout {
		writeQualityWarnings(stderr, inputName(flags.Args(), problem.Format()), problem, snapshot)
	}
	output := ""
	if *debugLayout {
		output = "optimization: " + optimized.Method + "\n" + snapshot.DebugString()
	} else {
		style := render.Unicode
		if *ascii {
			style = render.ASCII
		}
		output = render.Diagram(snapshot, style)
		if *maxWidth > 0 {
			output = render.ConstrainWidth(output, *maxWidth)
		}
	}
	_, err = io.WriteString(stdout, output)
	return err
}

func inputName(arguments []string, format string) string {
	if len(arguments) == 1 && arguments[0] != "-" {
		return arguments[0]
	}
	if format != "" {
		return format
	}
	return "input"
}

func writeQualityWarnings(output io.Writer, name string, problem *diagram.Problem, snapshot *solution.Snapshot) {
	for _, issue := range snapshot.LabelIssues() {
		edge, ok := problem.Edge(diagram.EdgeID(issue.EdgeID))
		if !ok {
			continue
		}
		location := name
		if edge.SourceSpan.Start.Line > 0 {
			location = fmt.Sprintf("%s:%d:%d", name, edge.SourceSpan.Start.Line, edge.SourceSpan.Start.Column)
		}
		reason := "could not be placed"
		switch {
		case issue.NodeCollision:
			reason = "overlaps a node"
		case issue.LabelCollision:
			reason = "overlaps another label"
		case issue.EdgeCollision:
			reason = "overlaps another route"
		}
		fmt.Fprintf(output, "go-text-diagram: warning: %s: edge %s -> %s label %q %s\n", location, edge.Source.ID(), edge.Target.ID(), edge.Label, reason)
	}
}
