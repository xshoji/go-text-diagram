package main

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

func BenchmarkOptimizeE2E(b *testing.B) {
	for _, test := range []struct {
		name  string
		nodes int
		edges int
	}{
		{name: "50_nodes_100_edges", nodes: 50, edges: 100},
		{name: "100_nodes_300_edges", nodes: 100, edges: 300},
	} {
		b.Run(test.name, func(b *testing.B) {
			input := benchmarkPlantUML(test.nodes, test.edges)
			b.ReportAllocs()
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				if err := run(nil, strings.NewReader(input), io.Discard); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func benchmarkPlantUML(nodeCount, edgeCount int) string {
	var input strings.Builder
	input.WriteString("@startuml\n")
	for index := 0; index < nodeCount; index++ {
		fmt.Fprintf(&input, "component \"Node %03d\" as n%d\n", index, index)
	}
	for index := 0; index < edgeCount; index++ {
		from := (index*37 + index/nodeCount) % (nodeCount - 1)
		to := from + 1 + (index*17)%(nodeCount-from-1)
		fmt.Fprintf(&input, "n%d --> n%d : relation %03d\n", from, to, index)
	}
	input.WriteString("@enduml\n")
	return input.String()
}
