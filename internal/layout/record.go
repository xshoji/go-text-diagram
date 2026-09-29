package layout

import "github.com/xshoji/agents-workspace/internal/textwidth"

func (n *Node) CellRect(id string) (Rect, bool) {
	if n == nil || id == "" {
		return Rect{}, false
	}
	widths := recordColumnWidths(n.Cells)
	for row, ids := range n.CellIDs {
		x := n.Rect.X + 1
		for column := range widths {
			if column < len(ids) && ids[column] == id {
				return Rect{X: x, Y: n.Rect.Y + 1 + row*2, Width: widths[column], Height: 1}, true
			}
			x += widths[column] + 1
		}
	}
	return Rect{}, false
}

func recordColumnWidths(rows [][]string) []int {
	columns := 0
	for _, row := range rows {
		columns = max(columns, len(row))
	}
	widths := make([]int, columns)
	for _, row := range rows {
		for column, value := range row {
			widths[column] = max(widths[column], textwidth.String(value)+2)
		}
	}
	for column := range widths {
		widths[column] = max(3, widths[column])
	}
	return widths
}
