package geom

type Point struct {
	X int
	Y int
}

type Rect struct {
	X      int
	Y      int
	Width  int
	Height int
}

func Manhattan(left, right Point) int {
	return abs(right.X-left.X) + abs(right.Y-left.Y)
}

// Contains reports whether point is in rect, including its boundary.
func Contains(rect Rect, point Point) bool {
	return point.X >= rect.X && point.X < rect.X+rect.Width &&
		point.Y >= rect.Y && point.Y < rect.Y+rect.Height
}

// ContainsInterior reports whether point is strictly inside rect.
func ContainsInterior(rect Rect, point Point) bool {
	return point.X > rect.X && point.X < rect.X+rect.Width-1 &&
		point.Y > rect.Y && point.Y < rect.Y+rect.Height-1
}

// Overlaps treats rectangles as half-open cell ranges.
func Overlaps(left, right Rect) bool {
	return left.X < right.X+right.Width && right.X < left.X+left.Width &&
		left.Y < right.Y+right.Height && right.Y < left.Y+left.Height
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
