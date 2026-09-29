package solution

import "fmt"

// Materialize applies route replacements without evaluating quality. The returned
// candidate must pass Validate before FullQuality or publication.
func Materialize(base *Snapshot, replacements map[string]RoutedEdge) (*Snapshot, error) {
	if base == nil {
		return nil, fmt.Errorf("base snapshot is nil")
	}
	next := *base
	next.revision = base.revision + 1
	next.nodes = base.Nodes()
	next.groups = base.Groups()
	next.layoutEdges = append([]LayoutEdge(nil), base.layoutEdges...)
	next.routes = base.Routes()
	next.manifest = cloneManifest(base.manifest)
	seenRoutes := make(map[string]bool, len(replacements))
	for i := range next.routes {
		if edge, ok := replacements[next.routes[i].ID]; ok {
			next.routes[i], seenRoutes[edge.ID] = cloneRoutedEdge(edge), true
		}
	}
	for id := range replacements {
		if !seenRoutes[id] {
			return nil, fmt.Errorf("unknown route %q", id)
		}
	}
	return &next, nil
}

// Finalize validates a materialized candidate before evaluating its quality.
func Finalize(candidate *Snapshot) (*Snapshot, error) {
	if err := candidate.Validate(); err != nil {
		return nil, err
	}
	next := *candidate
	evaluated := FullQuality(&next)
	next.routeMetrics, next.quality = evaluated.Metrics, evaluated.Quality
	return &next, nil
}

func cloneManifest(source semanticManifest) semanticManifest {
	result := semanticManifest{nodes: map[string]string{}, groups: map[string]string{}, edges: map[string]semanticEdge{}, nodeParent: map[string]string{}}
	for k, v := range source.nodes {
		result.nodes[k] = v
	}
	for k, v := range source.groups {
		result.groups[k] = v
	}
	for k, v := range source.edges {
		v.source, v.target = cloneEndpoint(v.source), cloneEndpoint(v.target)
		result.edges[k] = v
	}
	for k, v := range source.nodeParent {
		result.nodeParent[k] = v
	}
	return result
}

// CompareQuality implements the solver's documented lexicographic contract.
func CompareQuality(left, right Quality) int {
	a := [...]int{left.Unrouted, left.Structural, left.Labels, left.Intersections, left.Reversed, left.Directionality, left.Excess, left.CrossAxis, left.Alignment, left.Area, left.Length}
	b := [...]int{right.Unrouted, right.Structural, right.Labels, right.Intersections, right.Reversed, right.Directionality, right.Excess, right.CrossAxis, right.Alignment, right.Area, right.Length}
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}
