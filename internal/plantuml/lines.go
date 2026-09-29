package plantuml

import (
	"bufio"
	"fmt"
	"io"
)

type sourceLine struct {
	Number int
	Text   string
}

func readLines(r io.Reader) ([]sourceLine, error) {
	scanner := bufio.NewScanner(r)
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, 1024*1024)
	var lines []sourceLine
	for scanner.Scan() {
		lines = append(lines, sourceLine{Number: len(lines) + 1, Text: scanner.Text()})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read PlantUML: %w", err)
	}
	return lines, nil
}
