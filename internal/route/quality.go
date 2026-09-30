package route

import (
	"fmt"

	"github.com/xshoji/go-text-diagram/internal/diagram"
	"github.com/xshoji/go-text-diagram/internal/geom"
	"github.com/xshoji/go-text-diagram/internal/layout"
)

type Metrics struct {
	NodeOverlaps          int
	EdgeNodeCollisions    int
	Crossings             int
	Overlaps              int
	TotalLength           int
	ExcessLength          int
	Bends                 int
	ReverseMoves          int
	AStarFallbacks        int
	UnroutedEdges         int
	LabelNodeCollisions   int
	LabelLabelCollisions  int
	LabelEdgeCollisions   int
	UnplacedLabels        int
	GroupBoundaryOverlaps int
}

// Quality is the shared lexicographic quality contract used when selecting
// between complete routed layouts. Fields earlier in CompareQuality are never
// traded for improvements in later fields.
type Quality struct {
	Unrouted       int
	Structural     int
	Labels         int
	Intersections  int
	Reversed       int
	Directionality int
	Excess         int
	CrossAxis      int
	Alignment      int
	Area           int
	Length         int
}

func MeasureQuality(result *layout.Layout, metrics Metrics, routes []*Edge) Quality {
	if result == nil {
		return Quality{Unrouted: metrics.UnroutedEdges}
	}
	crossAxis := result.Metrics.Height
	if result.Direction.Vertical() {
		crossAxis = result.Metrics.Width
	}
	return Quality{
		Unrouted:       metrics.UnroutedEdges,
		Structural:     metrics.NodeOverlaps + metrics.EdgeNodeCollisions + metrics.GroupBoundaryOverlaps,
		Labels:         metrics.LabelNodeCollisions + metrics.LabelLabelCollisions + metrics.LabelEdgeCollisions + metrics.UnplacedLabels,
		Intersections:  metrics.Overlaps + metrics.Crossings,
		Reversed:       result.Metrics.ReversedEdges,
		Directionality: metrics.Bends + metrics.ReverseMoves,
		Excess:         metrics.ExcessLength,
		CrossAxis:      crossAxis,
		Alignment:      routeAlignmentDrift(result, routes),
		Area:           result.Metrics.Width * result.Metrics.Height,
		Length:         metrics.TotalLength,
	}
}

func CompareQuality(left, right Quality) int {
	leftValues := [...]int{left.Unrouted, left.Structural, left.Labels, left.Intersections, left.Reversed, left.Directionality, left.Excess, left.CrossAxis, left.Alignment, left.Area, left.Length}
	rightValues := [...]int{right.Unrouted, right.Structural, right.Labels, right.Intersections, right.Reversed, right.Directionality, right.Excess, right.CrossAxis, right.Alignment, right.Area, right.Length}
	for index := range leftValues {
		if leftValues[index] < rightValues[index] {
			return -1
		}
		if leftValues[index] > rightValues[index] {
			return 1
		}
	}
	return 0
}

func routeAlignmentDrift(result *layout.Layout, routes []*Edge) int {
	drift := 0
	for _, edge := range routes {
		if edge == nil || edge.SelfLoop || edge.LineStyle == diagram.LineInvisible || len(edge.Points) < 2 {
			continue
		}
		first, last := edge.Points[0], edge.Points[len(edge.Points)-1]
		if result.Direction.Vertical() {
			drift += abs(last.X - first.X)
		} else {
			drift += abs(last.Y - first.Y)
		}
	}
	return drift
}

func (m Metrics) MajorCollisions() int {
	return m.NodeOverlaps + m.EdgeNodeCollisions + m.GroupBoundaryOverlaps + m.UnroutedEdges +
		m.LabelNodeCollisions + m.LabelLabelCollisions + m.LabelEdgeCollisions + m.UnplacedLabels
}

type pointDirections struct {
	horizontal map[string]bool
	vertical   map[string]bool
}

// Analyze measures structural route quality without combining unrelated
// problems into a single score.
func Analyze(result *layout.Layout, routes []*Edge) Metrics {
	var metrics Metrics
	if result == nil {
		return metrics
	}
	return analyzeWithGroupBoundaries(result, routes, newGroupBoundaryIndex(result.Groups))
}

func analyzeWithGroupBoundaries(result *layout.Layout, routes []*Edge, groupBoundaries groupBoundaryIndex) Metrics {
	var metrics Metrics
	metrics.NodeOverlaps = countNodeOverlaps(result.Nodes)
	expectedEdges := make(map[string]bool)
	for _, edge := range result.Edges {
		expectedEdges[edge.OriginalEdgeID] = true
	}
	routedEdges := make(map[string]bool)
	segments := make(map[unitSegment]map[string]bool)
	points := make(map[Point]*pointDirections)
	for _, edge := range routes {
		routedEdges[edge.ID] = true
		if edge.LineStyle == diagram.LineInvisible {
			continue
		}
		metrics.GroupBoundaryOverlaps += groupBoundaries.overlapCount(edge, edge.Points)
		if edge.Label != "" && !edge.LabelPlaced {
			metrics.UnplacedLabels++
		}
		if len(edge.Points) < 2 {
			metrics.UnroutedEdges++
			continue
		}
		if edge.Fallback {
			metrics.AStarFallbacks++
		}
		edgeLength := 0
		metrics.Bends += max(0, len(edge.Points)-2)
		collidedNodes := make(map[string]bool)
		for index := 1; index < len(edge.Points); index++ {
			from, to := edge.Points[index-1], edge.Points[index]
			segmentLength := abs(to.X-from.X) + abs(to.Y-from.Y)
			edgeLength += segmentLength
			metrics.TotalLength += segmentLength
			if isReverseMove(from, to, result.Direction) {
				metrics.ReverseMoves++
			}
			walkSegment(from, to, func(current, next Point) {
				key := canonicalSegment(current, next)
				if segments[key] == nil {
					segments[key] = make(map[string]bool)
				}
				segments[key][edge.ID] = true
				for _, point := range []Point{current, next} {
					directions := points[point]
					if directions == nil {
						directions = &pointDirections{horizontal: make(map[string]bool), vertical: make(map[string]bool)}
						points[point] = directions
					}
					if current.Y == next.Y {
						directions.horizontal[edge.ID] = true
					} else {
						directions.vertical[edge.ID] = true
					}
				}
			})
			walkPoints(from, to, func(point Point) {
				for _, node := range result.Nodes {
					if node.Dummy || node.ID == edge.From || node.ID == edge.To || collidedNodes[node.ID] {
						continue
					}
					if geom.ContainsInterior(node.Rect, point) {
						collidedNodes[node.ID] = true
						metrics.EdgeNodeCollisions++
					}
				}
			})
		}
		first, last := edge.Points[0], edge.Points[len(edge.Points)-1]
		metrics.ExcessLength += max(0, edgeLength-abs(last.X-first.X)-abs(last.Y-first.Y))
	}
	metrics.LabelNodeCollisions, metrics.LabelLabelCollisions, metrics.LabelEdgeCollisions = analyzeLabelCollisions(result, routes)
	for edgeID := range expectedEdges {
		if !routedEdges[edgeID] {
			metrics.UnroutedEdges++
		}
	}
	for _, edgeIDs := range segments {
		if len(edgeIDs) > 1 {
			metrics.Overlaps += len(edgeIDs) - 1
		}
	}
	for _, directions := range points {
		for horizontalID := range directions.horizontal {
			for verticalID := range directions.vertical {
				if horizontalID != verticalID {
					metrics.Crossings++
				}
			}
		}
	}
	return metrics
}

func (m Metrics) DebugString() string {
	return fmt.Sprintf(
		"route_metrics: node_overlaps=%d edge_node_collisions=%d group_boundary_overlaps=%d crossings=%d overlaps=%d length=%d excess_length=%d bends=%d reverse_moves=%d astar_fallbacks=%d unrouted=%d label_node_collisions=%d label_label_collisions=%d label_edge_collisions=%d unplaced_labels=%d\n",
		m.NodeOverlaps,
		m.EdgeNodeCollisions,
		m.GroupBoundaryOverlaps,
		m.Crossings,
		m.Overlaps,
		m.TotalLength,
		m.ExcessLength,
		m.Bends,
		m.ReverseMoves,
		m.AStarFallbacks,
		m.UnroutedEdges,
		m.LabelNodeCollisions,
		m.LabelLabelCollisions,
		m.LabelEdgeCollisions,
		m.UnplacedLabels,
	)
}

func analyzeLabelCollisions(result *layout.Layout, routes []*Edge) (nodeCollisions, labelCollisions, edgeCollisions int) {
	for index, labeled := range routes {
		if !labeled.LabelPlaced {
			continue
		}
		for _, node := range result.Nodes {
			if !node.Dummy && geom.Overlaps(labeled.LabelRect, node.Rect) {
				nodeCollisions++
			}
		}
		for _, other := range routes[index+1:] {
			if other.LabelPlaced && geom.Overlaps(labeled.LabelRect, other.LabelRect) {
				labelCollisions++
			}
		}
		if rectHitsOtherRoute(labeled.LabelRect, labeled.ID, routes) {
			edgeCollisions++
		}
	}
	return nodeCollisions, labelCollisions, edgeCollisions
}

func countNodeOverlaps(nodes []*layout.Node) int {
	count := 0
	for index, left := range nodes {
		if left.Dummy {
			continue
		}
		for _, right := range nodes[index+1:] {
			if right.Dummy {
				continue
			}
			if left.Rect.X < right.Rect.X+right.Rect.Width && right.Rect.X < left.Rect.X+left.Rect.Width &&
				left.Rect.Y < right.Rect.Y+right.Rect.Height && right.Rect.Y < left.Rect.Y+left.Rect.Height {
				count++
			}
		}
	}
	return count
}

func isReverseMove(from, to Point, direction diagram.Direction) bool {
	if direction.Vertical() {
		return (to.Y-from.Y)*direction.ForwardSign() < 0
	}
	return (to.X-from.X)*direction.ForwardSign() < 0
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
