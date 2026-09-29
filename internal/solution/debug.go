package solution

import (
	"fmt"
	"strings"
)

func (s *Snapshot) DebugString() string {
	if s == nil {
		return ""
	}
	var out strings.Builder
	fmt.Fprintf(&out, "direction: %s\n", s.direction)
	fmt.Fprintf(&out, "bounds: %dx%d\n", s.layoutMetrics.Width, s.layoutMetrics.Height)
	fmt.Fprintf(&out, "metrics: layers=%d crossings=%d dummies=%d long_edges=%d sweeps=%d\n",
		s.layoutMetrics.LayerCount, s.layoutMetrics.Crossings, s.layoutMetrics.DummyNodes,
		s.layoutMetrics.LongEdges, s.layoutMetrics.SweepRounds)
	if s.layoutMetrics.ReversedEdges > 0 || s.layoutMetrics.SelfLoops > 0 {
		fmt.Fprintf(&out, "cycles: reversed_edges=%d self_loops=%d\n", s.layoutMetrics.ReversedEdges, s.layoutMetrics.SelfLoops)
	}
	lastRank := -1
	for _, node := range s.nodes {
		if node.Rank != lastRank {
			fmt.Fprintf(&out, "rank %d:\n", node.Rank)
			lastRank = node.Rank
		}
		kind := "node"
		if node.Dummy {
			kind = "dummy"
		}
		fmt.Fprintf(&out, "  %s %q order=%d rect=(%d,%d %dx%d)\n", kind, node.ID, node.Order, node.Rect.X, node.Rect.Y, node.Rect.Width, node.Rect.Height)
	}
	if len(s.layoutEdges) > 0 {
		out.WriteString("edges:\n")
		for _, edge := range s.layoutEdges {
			fmt.Fprintf(&out, "  %s %q -> %q original=%s segment=%d\n", edge.ID, edge.From, edge.To, edge.OriginalEdgeID, edge.Segment)
		}
	}
	if len(s.routes) > 0 {
		out.WriteString("routes:\n")
		for _, edge := range s.routes {
			fmt.Fprintf(&out, "  %s %q -> %q lane=%d reversed=%t loop=%t long=%t optimized=%t fallback=%t label=%q label_placed=%t label_rect=(%d,%d,%d,%d) points=",
				edge.ID, edge.From, edge.To, edge.Lane, edge.Reversed, edge.SelfLoop, edge.Long, edge.Optimized, edge.Fallback,
				edge.Label, edge.LabelPlaced, edge.LabelRect.X, edge.LabelRect.Y, edge.LabelRect.Width, edge.LabelRect.Height)
			for index, point := range edge.Points {
				if index > 0 {
					out.WriteString(" -> ")
				}
				fmt.Fprintf(&out, "(%d,%d)", point.X, point.Y)
			}
			out.WriteByte('\n')
		}
	}
	fmt.Fprintf(&out, "route_metrics: node_overlaps=%d edge_node_collisions=%d group_boundary_overlaps=%d crossings=%d overlaps=%d length=%d excess_length=%d bends=%d reverse_moves=%d astar_fallbacks=%d unrouted=%d label_node_collisions=%d label_label_collisions=%d label_edge_collisions=%d unplaced_labels=%d\n",
		s.routeMetrics.NodeOverlaps, s.routeMetrics.EdgeNodeCollisions, s.routeMetrics.GroupBoundaryOverlaps,
		s.routeMetrics.Crossings, s.routeMetrics.Overlaps, s.routeMetrics.TotalLength, s.routeMetrics.ExcessLength,
		s.routeMetrics.Bends, s.routeMetrics.ReverseMoves, s.routeMetrics.AStarFallbacks, s.routeMetrics.UnroutedEdges,
		s.routeMetrics.LabelNodeCollisions, s.routeMetrics.LabelLabelCollisions, s.routeMetrics.LabelEdgeCollisions, s.routeMetrics.UnplacedLabels)
	return out.String()
}
