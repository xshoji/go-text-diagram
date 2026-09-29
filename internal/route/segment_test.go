package route

import (
	"math/rand"
	"testing"

	"github.com/xshoji/agents-workspace/internal/diagram"
)

func TestSegmentCountIndexMatchesPairwiseReference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		candidate []Point
		prior     []*Edge
	}{
		{name: "center crossing", candidate: []Point{{X: 0, Y: 2}, {X: 4, Y: 2}}, prior: []*Edge{{Points: []Point{{X: 2, Y: 0}, {X: 2, Y: 4}}}}},
		{name: "endpoint crossing", candidate: []Point{{X: 0, Y: 2}, {X: 2, Y: 2}}, prior: []*Edge{{Points: []Point{{X: 2, Y: 2}, {X: 2, Y: 4}}}}},
		{name: "complete overlap", candidate: []Point{{X: 0, Y: 2}, {X: 4, Y: 2}}, prior: []*Edge{{Points: []Point{{X: 0, Y: 2}, {X: 4, Y: 2}}}}},
		{name: "parallel separate", candidate: []Point{{X: 0, Y: 2}, {X: 4, Y: 2}}, prior: []*Edge{{Points: []Point{{X: 0, Y: 3}, {X: 4, Y: 3}}}}},
		{name: "repeated prior segment", candidate: []Point{{X: 0, Y: 2}, {X: 3, Y: 2}}, prior: []*Edge{{Points: []Point{{X: 0, Y: 2}, {X: 3, Y: 2}, {X: 0, Y: 2}}}}},
		{name: "shared segment across routes", candidate: []Point{{X: 1, Y: 2}, {X: 3, Y: 2}}, prior: []*Edge{{Points: []Point{{X: 0, Y: 2}, {X: 4, Y: 2}}}, {Points: []Point{{X: 1, Y: 2}, {X: 3, Y: 2}}}}},
		{name: "repeated candidate segment", candidate: []Point{{X: 0, Y: 2}, {X: 3, Y: 2}, {X: 0, Y: 2}}, prior: []*Edge{{Points: []Point{{X: 1, Y: 2}, {X: 2, Y: 2}}}}},
		{name: "invisible prior", candidate: []Point{{X: 0, Y: 2}, {X: 4, Y: 2}}, prior: []*Edge{{LineStyle: diagram.LineInvisible, Points: []Point{{X: 2, Y: 0}, {X: 2, Y: 4}}}}},
		{name: "asymmetric multibend", candidate: []Point{{X: 0, Y: 0}, {X: 7, Y: 0}, {X: 7, Y: 3}, {X: 2, Y: 3}}, prior: []*Edge{{Points: []Point{{X: 1, Y: -1}, {X: 1, Y: 4}, {X: 5, Y: 4}, {X: 5, Y: 0}}}, {Points: []Point{{X: 3, Y: 3}, {X: 9, Y: 3}}}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertSegmentInteractions(t, test.candidate, test.prior)
		})
	}

	random := rand.New(rand.NewSource(7))
	for range 200 {
		candidate := randomOrthogonalPath(random)
		prior := make([]*Edge, 1+random.Intn(4))
		for index := range prior {
			prior[index] = &Edge{Points: randomOrthogonalPath(random)}
			if random.Intn(5) == 0 {
				prior[index].LineStyle = diagram.LineInvisible
			}
		}
		assertSegmentInteractions(t, candidate, prior)
	}
}

func assertSegmentInteractions(t *testing.T, candidate []Point, prior []*Edge) {
	t.Helper()
	segments := make(segmentCountIndex)
	for _, edge := range prior {
		segments.addEdge(edge)
	}
	gotCrossings, gotOverlaps := segments.interactions(candidate)
	wantCrossings, wantOverlaps := referenceInteractions(candidate, prior)
	if gotCrossings != wantCrossings || gotOverlaps != wantOverlaps {
		t.Fatalf("interactions = (%d, %d), want (%d, %d); candidate=%v prior=%v", gotCrossings, gotOverlaps, wantCrossings, wantOverlaps, candidate, prior)
	}
}

func referenceInteractions(candidate []Point, prior []*Edge) (int, int) {
	candidateSegments := referenceSegments(candidate)
	crossings, overlaps := 0, 0
	for _, edge := range prior {
		if edge == nil || edge.LineStyle == diagram.LineInvisible {
			continue
		}
		for _, left := range candidateSegments {
			for _, right := range referenceSegments(edge.Points) {
				if left == right {
					overlaps++
				} else if referenceSegmentsCross(left, right) {
					crossings++
				}
			}
		}
	}
	return crossings, overlaps
}

func referenceSegments(points []Point) []unitSegment {
	var segments []unitSegment
	for index := 1; index < len(points); index++ {
		from, to := points[index-1], points[index]
		dx, dy := axisSign(to.X-from.X), axisSign(to.Y-from.Y)
		for from != to {
			next := Point{X: from.X + dx, Y: from.Y + dy}
			segments = append(segments, canonicalSegment(from, next))
			from = next
		}
	}
	return segments
}

func referenceSegmentsCross(left, right unitSegment) bool {
	leftHorizontal := left.from.Y == left.to.Y
	rightHorizontal := right.from.Y == right.to.Y
	if leftHorizontal == rightHorizontal {
		return false
	}
	if !leftHorizontal {
		left, right = right, left
	}
	return right.from.X >= left.from.X && right.from.X <= left.to.X &&
		left.from.Y >= right.from.Y && left.from.Y <= right.to.Y
}

func randomOrthogonalPath(random *rand.Rand) []Point {
	points := []Point{{X: random.Intn(9) - 4, Y: random.Intn(9) - 4}}
	for range 1 + random.Intn(6) {
		next := points[len(points)-1]
		distance := 1 + random.Intn(5)
		if random.Intn(2) == 0 {
			next.X += distance * (random.Intn(2)*2 - 1)
		} else {
			next.Y += distance * (random.Intn(2)*2 - 1)
		}
		points = append(points, next)
	}
	return points
}
