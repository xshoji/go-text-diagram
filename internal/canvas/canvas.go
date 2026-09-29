package canvas

import (
	"strings"

	"github.com/xshoji/agents-workspace/internal/geom"
	"github.com/xshoji/agents-workspace/internal/textwidth"
)

type Direction uint8

const (
	North Direction = 1 << iota
	East
	South
	West
)

type CellKind uint8

const (
	CellEmpty CellKind = iota
	CellLine
	CellText
)

type Cell struct {
	Connections  Direction
	EdgeIDs      []string
	Text         string
	Kind         CellKind
	LineStyle    LineStyle
	continuation bool
}

type Style uint8

const (
	Unicode Style = iota
	ASCII
)

type LineStyle uint8

const (
	Solid LineStyle = iota
	Dotted
	Dashed
	Double
	Bold
	Invisible
)

type Canvas struct {
	width  int
	height int
	cells  [][]Cell
}

func New(width, height int) *Canvas {
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	cells := make([][]Cell, height)
	for y := range cells {
		cells[y] = make([]Cell, width)
	}
	return &Canvas{width: width, height: height, cells: cells}
}

func (c *Canvas) Connect(from, to geom.Point, edgeID string) {
	c.ConnectStyled(from, to, edgeID, Solid)
}

func (c *Canvas) ConnectStyled(from, to geom.Point, edgeID string, lineStyle LineStyle) {
	if lineStyle == Invisible {
		return
	}
	if from.X != to.X && from.Y != to.Y {
		return
	}
	dx, dy := sign(to.X-from.X), sign(to.Y-from.Y)
	current := from
	for current != to {
		next := geom.Point{X: current.X + dx, Y: current.Y + dy}
		outgoing, incoming := connection(dx, dy)
		c.addConnection(current, outgoing, edgeID, lineStyle)
		c.addConnection(next, incoming, edgeID, lineStyle)
		current = next
	}
}

func (c *Canvas) SetText(point geom.Point, text string) {
	if !c.contains(point) {
		return
	}
	cell := &c.cells[point.Y][point.X]
	cell.Text = text
	cell.Kind = CellText
}

func (c *Canvas) WriteText(point geom.Point, text string) {
	x := point.X
	previousX := -1
	for _, cluster := range textwidth.Clusters(text) {
		width := cluster.Width
		if width == 0 {
			if previousX >= 0 && c.contains(geom.Point{X: previousX, Y: point.Y}) {
				c.cells[point.Y][previousX].Text += cluster.Text
			}
			continue
		}
		current := geom.Point{X: x, Y: point.Y}
		if !c.contains(current) {
			return
		}
		c.SetText(current, cluster.Text)
		previousX = x
		for offset := 1; offset < width; offset++ {
			continuation := geom.Point{X: x + offset, Y: point.Y}
			if c.contains(continuation) {
				c.cells[continuation.Y][continuation.X].continuation = true
				c.cells[continuation.Y][continuation.X].Kind = CellText
			}
		}
		x += width
	}
}

func (c *Canvas) Clear(x, y, width, height int) {
	for row := max(0, y); row < min(c.height, y+height); row++ {
		for column := max(0, x); column < min(c.width, x+width); column++ {
			c.cells[row][column] = Cell{}
		}
	}
}

func (c *Canvas) String(style Style) string {
	if c.width == 0 || c.height == 0 {
		return ""
	}
	var out strings.Builder
	for y, row := range c.cells {
		var line strings.Builder
		lastContent := -1
		for x, cell := range row {
			if cell.continuation {
				continue
			}
			value := " "
			if cell.Text != "" {
				value = cell.Text
			} else if cell.Connections != 0 {
				value = lineCharacter(cell.Connections, style, cell.LineStyle)
			}
			line.WriteString(value)
			if value != " " {
				lastContent = x
			}
		}
		value := line.String()
		if lastContent < 0 {
			value = ""
		} else {
			value = strings.TrimRight(value, " ")
		}
		out.WriteString(value)
		if y < c.height-1 {
			out.WriteByte('\n')
		}
	}
	return strings.TrimRight(out.String(), "\n") + "\n"
}

func (c *Canvas) addConnection(point geom.Point, direction Direction, edgeID string, lineStyle LineStyle) {
	if !c.contains(point) {
		return
	}
	cell := &c.cells[point.Y][point.X]
	cell.Connections |= direction
	cell.Kind = CellLine
	if lineStyle > cell.LineStyle {
		cell.LineStyle = lineStyle
	}
	if edgeID == "" {
		return
	}
	for _, id := range cell.EdgeIDs {
		if id == edgeID {
			return
		}
	}
	cell.EdgeIDs = append(cell.EdgeIDs, edgeID)
}

func (c *Canvas) contains(point geom.Point) bool {
	return point.X >= 0 && point.X < c.width && point.Y >= 0 && point.Y < c.height
}

func sign(value int) int {
	switch {
	case value < 0:
		return -1
	case value > 0:
		return 1
	default:
		return 0
	}
}

func connection(dx, dy int) (Direction, Direction) {
	switch {
	case dx > 0:
		return East, West
	case dx < 0:
		return West, East
	case dy > 0:
		return South, North
	default:
		return North, South
	}
}

func lineCharacter(mask Direction, style Style, lineStyle LineStyle) string {
	if style == ASCII {
		if lineStyle == Dotted {
			if mask == North|South || mask == North || mask == South {
				return ":"
			}
			if mask == East|West || mask == East || mask == West {
				return "."
			}
		}
		if lineStyle == Dashed {
			if mask == North|South || mask == North || mask == South {
				return ":"
			}
			if mask == East|West || mask == East || mask == West {
				return "~"
			}
		}
		if lineStyle == Double {
			if mask == East|West || mask == East || mask == West {
				return "="
			}
			return "#"
		}
		if lineStyle == Bold {
			return "#"
		}
		if mask == North|South || mask == North || mask == South {
			return "|"
		}
		if mask == East|West || mask == East || mask == West {
			return "-"
		}
		return "+"
	}
	if lineStyle == Double {
		switch mask {
		case North, South, North | South:
			return "║"
		case East, West, East | West:
			return "═"
		case East | South:
			return "╔"
		case West | South:
			return "╗"
		case East | North:
			return "╚"
		case West | North:
			return "╝"
		case East | West | South:
			return "╦"
		case East | West | North:
			return "╩"
		case North | South | East:
			return "╠"
		case North | South | West:
			return "╣"
		default:
			return "╬"
		}
	}
	if lineStyle == Bold {
		switch mask {
		case North, South, North | South:
			return "┃"
		case East, West, East | West:
			return "━"
		case East | South:
			return "┏"
		case West | South:
			return "┓"
		case East | North:
			return "┗"
		case West | North:
			return "┛"
		case East | West | South:
			return "┳"
		case East | West | North:
			return "┻"
		case North | South | East:
			return "┣"
		case North | South | West:
			return "┫"
		default:
			return "╋"
		}
	}
	if lineStyle == Dotted || lineStyle == Dashed {
		if mask == North || mask == South || mask == North|South {
			if lineStyle == Dotted {
				return "┆"
			}
			return "╎"
		}
		if mask == East || mask == West || mask == East|West {
			if lineStyle == Dotted {
				return "┄"
			}
			return "╌"
		}
	}
	switch mask {
	case North, South, North | South:
		return "│"
	case East, West, East | West:
		return "─"
	case East | South:
		return "┌"
	case West | South:
		return "┐"
	case East | North:
		return "└"
	case West | North:
		return "┘"
	case East | West | South:
		return "┬"
	case East | West | North:
		return "┴"
	case North | South | East:
		return "├"
	case North | South | West:
		return "┤"
	default:
		return "┼"
	}
}
