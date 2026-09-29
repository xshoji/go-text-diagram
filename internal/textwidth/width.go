package textwidth

import (
	"strings"

	"github.com/clipperhouse/uax29/v2/graphemes"
	"github.com/mattn/go-runewidth"
)

var terminalWidth = &runewidth.Condition{
	EastAsianWidth:     false,
	StrictEmojiNeutral: true,
}

type Cluster struct {
	Text  string
	Width int
}

// Lines returns the label lines and the greatest terminal-cell width.
func Lines(text string) ([]string, int) {
	lines := strings.Split(text, "\n")
	maxWidth := 0
	for _, line := range lines {
		if width := String(line); width > maxWidth {
			maxWidth = width
		}
	}
	return lines, maxWidth
}

// String reports the number of terminal cells occupied by text.
func String(text string) int {
	return terminalWidth.StringWidth(text)
}

// Clusters splits text at Unicode grapheme boundaries and reports each width.
func Clusters(text string) []Cluster {
	iterator := graphemes.FromString(text)
	var result []Cluster
	for iterator.Next() {
		value := iterator.Value()
		result = append(result, Cluster{Text: value, Width: String(value)})
	}
	return result
}

// Wrap breaks text at grapheme boundaries without exceeding width terminal cells.
func Wrap(text string, width int) []string {
	if width <= 0 {
		lines, _ := Lines(text)
		return lines
	}
	var result []string
	for _, source := range strings.Split(text, "\n") {
		var line strings.Builder
		lineWidth := 0
		for _, cluster := range Clusters(source) {
			if lineWidth > 0 && lineWidth+cluster.Width > width {
				result = append(result, line.String())
				line.Reset()
				lineWidth = 0
			}
			line.WriteString(cluster.Text)
			lineWidth += cluster.Width
		}
		result = append(result, line.String())
	}
	return result
}
