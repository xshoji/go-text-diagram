package geom

type Interval struct {
	Start int
	End   int
}

// OverlapsInterval treats both endpoints as occupied.
func OverlapsInterval(left, right Interval) bool {
	return left.Start <= right.End && right.Start <= left.End
}

func FirstAvailableLane(lanes [][]Interval, candidate Interval) int {
	for lane, occupied := range lanes {
		available := true
		for _, current := range occupied {
			if OverlapsInterval(candidate, current) {
				available = false
				break
			}
		}
		if available {
			return lane
		}
	}
	return len(lanes)
}
