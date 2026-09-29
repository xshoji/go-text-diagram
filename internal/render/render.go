package render

import (
	"strings"
	"unicode"

	"github.com/xshoji/agents-workspace/internal/canvas"
	"github.com/xshoji/agents-workspace/internal/diagram"
	"github.com/xshoji/agents-workspace/internal/geom"
	"github.com/xshoji/agents-workspace/internal/solution"
	"github.com/xshoji/agents-workspace/internal/textwidth"
)

type Style = canvas.Style

const (
	Unicode = canvas.Unicode
	ASCII   = canvas.ASCII
)

func Diagram(snapshot *solution.Snapshot, style Style) string {
	if snapshot == nil {
		return ""
	}
	nodes := snapshot.Nodes()
	if len(nodes) == 0 {
		return ""
	}
	groups := snapshot.Groups()
	routes := snapshot.Routes()
	bounds := snapshot.Bounds()
	c := canvas.New(bounds.Width, bounds.Height)

	for index := range groups {
		drawGroup(c, &groups[index])
	}
	for _, edge := range routes {
		for i := 1; i < len(edge.Points); i++ {
			c.ConnectStyled(edge.Points[i-1], edge.Points[i], edge.ID, canvasLineStyle(edge.LineStyle))
		}
	}
	for index := range nodes {
		node := &nodes[index]
		if !node.Dummy {
			drawNode(c, node, style)
		}
	}
	for _, edge := range routes {
		drawArrow(c, edge, style)
	}
	for _, edge := range routes {
		drawEdgeLabel(c, edge)
	}
	return c.String(style)
}

// ConstrainWidth splits a rendered diagram into vertically stacked viewport
// panels. Boundaries are moved left when necessary so a wide grapheme cluster
// is never split or discarded.
func ConstrainWidth(diagram string, maximum int) string {
	if maximum <= 0 || diagram == "" {
		return diagram
	}
	lines := strings.Split(strings.TrimSuffix(diagram, "\n"), "\n")
	total := 0
	for _, line := range lines {
		total = max(total, textwidth.String(line))
	}
	if total <= maximum {
		return diagram
	}
	hardForbidden := make(map[int]bool)
	softForbidden := make(map[int]bool)
	for _, line := range lines {
		column := 0
		previousText := false
		for _, cluster := range textwidth.Clusters(line) {
			currentText := textCluster(cluster.Text)
			if previousText && currentText {
				softForbidden[column] = true
			}
			for boundary := column + 1; boundary < column+cluster.Width; boundary++ {
				hardForbidden[boundary] = true
			}
			column += cluster.Width
			previousText = currentText
		}
	}
	var output strings.Builder
	for start := 0; start < total; {
		end := min(total, start+maximum)
		for end > start && (hardForbidden[end] || softForbidden[end]) {
			end--
		}
		if end == start {
			end = min(total, start+maximum)
			for end > start && hardForbidden[end] {
				end--
			}
		}
		if end == start {
			// No panel of this width can contain the grapheme at start.
			// Keep it intact even though this one panel must exceed maximum.
			end++
			for hardForbidden[end] {
				end++
			}
		}
		if start > 0 {
			output.WriteByte('\n')
		}
		for _, line := range lines {
			output.WriteString(sliceColumns(line, start, end))
			output.WriteByte('\n')
		}
		start = end
	}
	return output.String()
}

func sliceColumns(line string, start, end int) string {
	var output strings.Builder
	column := 0
	for _, cluster := range textwidth.Clusters(line) {
		next := column + cluster.Width
		if column >= end {
			break
		}
		if next > start && column >= start {
			output.WriteString(cluster.Text)
		}
		column = next
	}
	return strings.TrimRight(output.String(), " ")
}

func textCluster(cluster string) bool {
	for _, character := range cluster {
		if unicode.IsLetter(character) || unicode.IsNumber(character) || unicode.IsMark(character) {
			return true
		}
	}
	return false
}

func drawNode(c *canvas.Canvas, node *solution.Node, style Style) {
	if node.Shape == diagram.ShapeInvisible {
		return
	}
	left, top := node.Rect.X, node.Rect.Y
	right := left + node.Rect.Width - 1
	bottom := top + node.Rect.Height - 1
	if node.Shape == diagram.ShapePoint {
		point := "●"
		if style == ASCII {
			point = "*"
		}
		c.SetText(geom.Point{X: left + (node.Rect.Width-1)/2, Y: top}, point)
		return
	}
	if node.Shape != diagram.ShapeNone {
		c.Connect(geom.Point{X: left, Y: top}, geom.Point{X: right, Y: top}, "")
		c.Connect(geom.Point{X: right, Y: top}, geom.Point{X: right, Y: bottom}, "")
		c.Connect(geom.Point{X: right, Y: bottom}, geom.Point{X: left, Y: bottom}, "")
		c.Connect(geom.Point{X: left, Y: bottom}, geom.Point{X: left, Y: top}, "")
		if node.Shape == diagram.ShapeRounded && style == Unicode {
			c.SetText(geom.Point{X: left, Y: top}, "╭")
			c.SetText(geom.Point{X: right, Y: top}, "╮")
			c.SetText(geom.Point{X: left, Y: bottom}, "╰")
			c.SetText(geom.Point{X: right, Y: bottom}, "╯")
		}
	}
	if len(node.Cells) > 0 {
		drawRecord(c, node)
		return
	}

	lines := textwidth.Wrap(node.Label, node.TextWrap)
	firstY := top + 1 + (node.Rect.Height-2-len(lines))/2
	for index, line := range lines {
		lineWidth := textwidth.String(line)
		firstX := left + 1 + (node.Rect.Width-2-lineWidth)/2
		if node.Align == diagram.AlignLeft {
			firstX = left + 1
		}
		if node.Align == diagram.AlignRight {
			firstX = right - lineWidth
		}
		c.WriteText(geom.Point{X: firstX, Y: firstY + index}, line)
	}
}

func drawArrow(c *canvas.Canvas, edge solution.RoutedEdge, style Style) {
	if len(edge.Points) < 2 || edge.Kind == diagram.Undirected || edge.ArrowStyle == diagram.ArrowNone || edge.LineStyle == diagram.LineInvisible {
		return
	}
	drawArrowAt(c, edge.Points[len(edge.Points)-2], edge.Points[len(edge.Points)-1], style, edge.ArrowStyle)
	if edge.Kind == diagram.Bidirectional {
		drawArrowAt(c, edge.Points[1], edge.Points[0], style, edge.ArrowStyle)
	}
}

func drawArrowAt(c *canvas.Canvas, previous, last geom.Point, style Style, arrowStyle diagram.ArrowStyle) {
	arrow := "▼"
	if style == ASCII {
		arrow = "v"
	}
	switch {
	case last.X > previous.X:
		arrow = "▶"
		if style == ASCII {
			arrow = ">"
		}
	case last.X < previous.X:
		arrow = "◀"
		if style == ASCII {
			arrow = "<"
		}
	case last.Y < previous.Y:
		arrow = "▲"
		if style == ASCII {
			arrow = "^"
		}
	}
	if style == Unicode && arrowStyle == diagram.ArrowOpen {
		switch {
		case last.X > previous.X:
			arrow = "→"
		case last.X < previous.X:
			arrow = "←"
		case last.Y < previous.Y:
			arrow = "↑"
		default:
			arrow = "↓"
		}
	}
	if style == Unicode && arrowStyle == diagram.ArrowClosed {
		switch {
		case last.X > previous.X:
			arrow = "▷"
		case last.X < previous.X:
			arrow = "◁"
		case last.Y < previous.Y:
			arrow = "△"
		default:
			arrow = "▽"
		}
	}
	c.SetText(last, arrow)
}

func canvasLineStyle(style diagram.LineStyle) canvas.LineStyle {
	switch style {
	case diagram.LineDotted:
		return canvas.Dotted
	case diagram.LineDashed:
		return canvas.Dashed
	case diagram.LineDouble:
		return canvas.Double
	case diagram.LineBold:
		return canvas.Bold
	case diagram.LineInvisible:
		return canvas.Invisible
	default:
		return canvas.Solid
	}
}

func drawGroup(c *canvas.Canvas, group *solution.Group) {
	if group.LineStyle == diagram.LineInvisible {
		return
	}
	left, top := group.Rect.X, group.Rect.Y
	right, bottom := left+group.Rect.Width-1, top+group.Rect.Height-1
	lineStyle := canvasLineStyle(group.LineStyle)
	c.ConnectStyled(geom.Point{X: left, Y: top}, geom.Point{X: right, Y: top}, "", lineStyle)
	c.ConnectStyled(geom.Point{X: right, Y: top}, geom.Point{X: right, Y: bottom}, "", lineStyle)
	c.ConnectStyled(geom.Point{X: right, Y: bottom}, geom.Point{X: left, Y: bottom}, "", lineStyle)
	c.ConnectStyled(geom.Point{X: left, Y: bottom}, geom.Point{X: left, Y: top}, "", lineStyle)
	if group.Label != "" {
		c.WriteText(geom.Point{X: left + 2, Y: top}, " "+group.Label+" ")
	}
}

func drawRecord(c *canvas.Canvas, node *solution.Node) {
	columns := 0
	for _, row := range node.Cells {
		columns = max(columns, len(row))
	}
	widths := make([]int, columns)
	for _, row := range node.Cells {
		for column, value := range row {
			widths[column] = max(widths[column], textwidth.String(value)+2)
		}
	}
	for column := range widths {
		widths[column] = max(3, widths[column])
	}
	left, top := node.Rect.X, node.Rect.Y
	for rowIndex, row := range node.Cells {
		y := top + 1 + rowIndex*2
		x := left + 1
		for column := 0; column < columns; column++ {
			value := ""
			if column < len(row) {
				value = row[column]
			}
			valueX := x + (widths[column]-textwidth.String(value))/2
			c.WriteText(geom.Point{X: valueX, Y: y}, value)
			x += widths[column]
			if column+1 < columns {
				c.Connect(geom.Point{X: x, Y: top}, geom.Point{X: x, Y: top + node.Rect.Height - 1}, "")
				x++
			}
		}
		if rowIndex+1 < len(node.Cells) {
			c.Connect(geom.Point{X: left, Y: y + 1}, geom.Point{X: left + node.Rect.Width - 1, Y: y + 1}, "")
		}
	}
}

func drawEdgeLabel(c *canvas.Canvas, edge solution.RoutedEdge) {
	if !edge.LabelPlaced || edge.LineStyle == diagram.LineInvisible {
		return
	}
	c.Clear(edge.LabelRect.X, edge.LabelRect.Y, edge.LabelRect.Width, edge.LabelRect.Height)
	lines, _ := textwidth.Lines(edge.Label)
	for index, line := range lines {
		x := edge.LabelRect.X + (edge.LabelRect.Width-textwidth.String(line))/2
		c.WriteText(geom.Point{X: x, Y: edge.LabelRect.Y + index}, line)
	}
}
