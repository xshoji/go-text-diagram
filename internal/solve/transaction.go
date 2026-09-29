// Package solve provides immutable candidate transactions over Solution snapshots.
package solve

import (
	"fmt"
	"reflect"
	"regexp"

	"github.com/xshoji/agents-workspace/internal/diagram"
	"github.com/xshoji/agents-workspace/internal/geom"
	"github.com/xshoji/agents-workspace/internal/layout"
	"github.com/xshoji/agents-workspace/internal/route"
	"github.com/xshoji/agents-workspace/internal/solution"
)

type Result struct {
	Snapshot *solution.Snapshot
	Method   string
}

// ComputeProblem lays out and routes an immutable semantic problem.
func ComputeProblem(problem *diagram.Problem, options layout.Options) (*Result, error) {
	result, _, err := ComputeProblemWithDiagnostics(problem, options)
	return result, err
}

func ComputeProblemWithDiagnostics(problem *diagram.Problem, options layout.Options) (*Result, SolveDiagnostics, error) {
	if problem == nil {
		return nil, SolveDiagnostics{}, fmt.Errorf("problem is nil")
	}
	var diagnostics SolveDiagnostics
	var best *Result
	var rejected []error
	var seenLayouts []*layout.Layout
	baselineTried := false
	for _, candidate := range candidateOptionsFor(problem) {
		if candidateBudgetExhausted(baselineTried, diagnostics.Route) {
			break
		}
		if candidate.name == "baseline" {
			baselineTried = true
		}
		diagnostics.Candidates++
		candidateOptions := options
		candidateOptions.FeedbackArc, candidateOptions.Straighten, candidateOptions.FlowGroups = candidate.feedbackArc, candidate.straighten, candidate.flowGroups
		candidateOptions.RankCompactionSteps = candidate.rankCompactionSteps
		placed, err := layout.Compute(problem, candidateOptions)
		if err != nil {
			rejected = append(rejected, fmt.Errorf("layout candidate %s: %w", candidate.name, err))
			continue
		}
		diagnostics.Layouts++
		if candidate.flowGroups && !placed.FlowApplied {
			continue
		}
		duplicate := false
		for _, previous := range seenLayouts {
			if equivalentLayout(previous, placed) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		seenLayouts = append(seenLayouts, placed)
		routes, metrics, rd := route.ComputeBaselineWithDiagnostics(placed)
		diagnostics.Route.Add(rd)
		basePlaced, baseRoutes, baseMetrics := placed, routes, metrics
		var od compactionDiagnostics
		placed, routes, metrics, compacted := prepareCandidate(placed, routes, metrics, &od)
		diagnostics.Compactions += od.Compactions
		diagnostics.CompactionClones += od.LayoutCloneCalls
		diagnostics.CompactionObjects += od.ClonedNodes + od.ClonedEdges + od.ClonedGroups + od.Route.ClonedRoutes
		diagnostics.Route.Add(od.Route)
		method := candidate.name
		if compacted {
			method += "+compact"
		}
		buildSnapshot := func() (*solution.Snapshot, error) {
			snapshot, err := solution.FromLegacyCandidate(problem, placed, routes, metrics)
			if err == nil {
				err = snapshot.Validate()
			}
			return snapshot, err
		}
		snapshot, err := buildSnapshot()
		if err != nil && compacted {
			diagnostics.Transaction.RejectedContracts++
			placed, routes, metrics, compacted, method = basePlaced, baseRoutes, baseMetrics, false, candidate.name
			snapshot, err = buildSnapshot()
		}
		if err != nil {
			diagnostics.Transaction.RejectedContracts++
			rejected = append(rejected, fmt.Errorf("candidate %s: %w", candidate.name, annotateProblemEdge(problem, err)))
			continue
		}
		index, err := solution.BuildIndex(snapshot)
		if err != nil {
			return nil, diagnostics, fmt.Errorf("index candidate %s: %w", candidate.name, err)
		}
		initialRoutes := snapshot.Routes()
		adaptive, _ := route.OptimizeAutoStartSidesCOW(placed, routes, metrics, adaptiveEvaluator(snapshot, index, placed, &diagnostics), &diagnostics.Route)
		route.EnsureArrowLeadSegments(placed, adaptive, &diagnostics.Route)
		finalRoutes := legacyReplacements(initialRoutes, adaptive, &diagnostics)
		if len(finalRoutes) > 0 {
			tx, txErr := NewTransaction(snapshot)
			if txErr != nil {
				return nil, diagnostics, txErr
			}
			for _, replacement := range finalRoutes {
				tx.ReplaceRoute(replacement)
			}
			adaptiveSnapshot, commitErr := tx.CommitRoute()
			diagnostics.Transaction.Add(tx.Diagnostics())
			// Adaptive routing is an optional improvement. A replacement that
			// violates the snapshot contract must not discard the valid routed
			// baseline for this layout candidate.
			if commitErr == nil {
				snapshot = adaptiveSnapshot
			}
		}
		current := &Result{Snapshot: snapshot, Method: method}
		if best == nil || solution.CompareQuality(snapshot.Quality(), best.Snapshot.Quality()) < 0 {
			best = current
		}
	}
	if best == nil {
		if len(rejected) > 0 {
			return nil, diagnostics, fmt.Errorf("no viable layout candidate: %w", rejected[len(rejected)-1])
		}
		return nil, diagnostics, fmt.Errorf("no viable layout candidate")
	}
	return best, diagnostics, nil
}

var routeIDPattern = regexp.MustCompile(`route "([^"]+)"`)

func annotateProblemEdge(problem *diagram.Problem, err error) error {
	if problem == nil || err == nil {
		return err
	}
	match := routeIDPattern.FindStringSubmatch(err.Error())
	if len(match) != 2 {
		return err
	}
	edge, ok := problem.Edge(diagram.EdgeID(match[1]))
	if !ok {
		return err
	}
	location := edge.SourceSpan.Format
	if edge.SourceSpan.Start.Line > 0 {
		location = fmt.Sprintf("%s:%d:%d", location, edge.SourceSpan.Start.Line, edge.SourceSpan.Start.Column)
	}
	description := fmt.Sprintf("edge %s -> %s", edge.Source.ID(), edge.Target.ID())
	if edge.Label != "" {
		description += fmt.Sprintf(" label %q", edge.Label)
	}
	if location == "" {
		return fmt.Errorf("%s: %w", description, err)
	}
	return fmt.Errorf("%s: %s: %w", location, description, err)
}

const maximumSolveAStarExpansions = 2_000_000

func candidateBudgetExhausted(baselineTried bool, diagnostics route.Diagnostics) bool {
	return baselineTried && diagnostics.AStarExpansions >= maximumSolveAStarExpansions
}

type candidateOptions struct {
	name                                string
	feedbackArc, straighten, flowGroups bool
	rankCompactionSteps                 int
}

func candidateOptionsFor(g *diagram.Problem) []candidateOptions {
	c := []candidateOptions{}
	if g != nil && len(g.Groups()) > 0 && g.Direction().Vertical() {
		c = append(c, candidateOptions{name: "flow-groups", flowGroups: true})
	}
	c = append(c, candidateOptions{name: "baseline"}, candidateOptions{name: "feedback-arc", feedbackArc: true}, candidateOptions{name: "straightened", straighten: true})
	if g != nil && len(g.Groups()) > 0 && !g.Direction().Vertical() {
		for steps := 1; steps <= 3; steps++ {
			c = append(c, candidateOptions{name: fmt.Sprintf("rank-compact-%d", steps), rankCompactionSteps: steps})
		}
	}
	return c
}

func equivalentLayout(left, right *layout.Layout) bool {
	return left != nil && right != nil && left.Direction == right.Direction &&
		reflect.DeepEqual(left.Nodes, right.Nodes) && reflect.DeepEqual(left.Edges, right.Edges) &&
		reflect.DeepEqual(left.Groups, right.Groups) && left.Metrics == right.Metrics &&
		left.OuterLaneStart == right.OuterLaneStart && left.LabelLaneSize == right.LabelLaneSize
}

type SolveDiagnostics struct {
	Candidates, Layouts, Compactions    int
	CompactionClones, CompactionObjects int
	ConversionClonedRoutes              int
	ConversionClonedPoints              int
	Route                               route.Diagnostics
	Transaction                         Diagnostics
}

type Diagnostics struct {
	IncrementalEvaluations, FullEvaluations int
	RejectedContracts                       int
}

func (d *Diagnostics) Add(other Diagnostics) {
	d.IncrementalEvaluations += other.IncrementalEvaluations
	d.FullEvaluations += other.FullEvaluations
	d.RejectedContracts += other.RejectedContracts
}

type Transaction struct {
	base         *solution.Snapshot
	index        *solution.Index
	replacements map[string]solution.RoutedEdge
	diagnostics  Diagnostics
}

func NewTransaction(base *solution.Snapshot) (*Transaction, error) {
	if base == nil {
		return nil, fmt.Errorf("base snapshot is nil")
	}
	if err := base.Validate(); err != nil {
		return nil, fmt.Errorf("invalid base snapshot: %w", err)
	}
	index, err := solution.BuildIndex(base)
	if err != nil {
		return nil, err
	}
	return &Transaction{base: base, index: index, replacements: map[string]solution.RoutedEdge{}}, nil
}

func (t *Transaction) ReplaceRoute(edge solution.RoutedEdge) { t.replacements[edge.ID] = edge }
func (t *Transaction) Diagnostics() Diagnostics              { return t.diagnostics }

func (t *Transaction) changedRoutes() []solution.RoutedEdge {
	base := t.base.Routes()
	result := make([]solution.RoutedEdge, 0, len(t.replacements))
	for _, edge := range base {
		if replacement, ok := t.replacements[edge.ID]; ok {
			result = append(result, replacement)
		}
	}
	return result
}

// CommitRoute validates and shadow-checks a route-only candidate. Invalid or
// mismatched candidates are rejected without changing base.
func (t *Transaction) CommitRoute() (*solution.Snapshot, error) {
	candidate, err := solution.Materialize(t.base, t.replacements)
	full := solution.QualityResult{}
	if err == nil {
		err = candidate.Validate()
	}
	if err == nil {
		candidate, err = solution.Finalize(candidate)
	}
	if err == nil {
		full = solution.QualityResult{Metrics: candidate.RouteMetrics(), Quality: candidate.Quality()}
	}
	if err != nil {
		t.diagnostics.RejectedContracts++
		return nil, err
	}
	t.diagnostics.FullEvaluations++
	t.diagnostics.IncrementalEvaluations++
	shadow, err := solution.ShadowQualityWithFull(t.base, t.index, t.changedRoutes(), candidate, full)
	if err != nil || !shadow.Adoptable {
		t.diagnostics.RejectedContracts++
		if err == nil {
			err = fmt.Errorf("incremental/full quality mismatch")
		}
		return nil, err
	}
	if solution.CompareQuality(candidate.Quality(), t.base.Quality()) > 0 {
		return nil, fmt.Errorf("candidate quality is worse than base")
	}
	return candidate, nil
}

func adaptiveEvaluator(initial *solution.Snapshot, index *solution.Index, placed *layout.Layout, diagnostics *SolveDiagnostics) route.AdaptiveEvaluator {
	initialRoutes := initial.Routes()
	var cachedCurrent **route.Edge
	cacheValid := false
	var cachedChanged []solution.RoutedEdge
	var cachedQuality solution.QualityResult
	var labelContext *route.LabelContext
	return func(current []*route.Edge, candidate route.AdaptiveCandidate) route.AdaptiveEvaluation {
		var currentKey **route.Edge
		if len(current) > 0 {
			currentKey = &current[0]
		}
		if !cacheValid || currentKey != cachedCurrent {
			cachedCurrent = currentKey
			cacheValid = true
			cachedChanged = legacyReplacements(initialRoutes, current, diagnostics)
			labelContext = route.NewLabelContext(current)
			var err error
			cachedQuality, err = solution.IncrementalQuality(initial, index, cachedChanged)
			diagnostics.Transaction.IncrementalEvaluations++
			if err != nil {
				diagnostics.Transaction.RejectedContracts++
				return route.AdaptiveEvaluation{}
			}
		}
		currentChanged, currentQuality := cachedChanged, cachedQuality
		first := labelContext.FirstAffected(current, candidate.Replacements)
		labels := route.PlaceLabelsFromContext(placed, current, candidate.Replacements, first, &diagnostics.Route, labelContext)
		commit := mergeRouteEdges(candidate.Replacements, labels)
		allChanged := mergeLegacy(currentChanged, commit)
		quality, err := solution.IncrementalQuality(initial, index, allChanged)
		diagnostics.Transaction.IncrementalEvaluations++
		if err != nil {
			diagnostics.Transaction.RejectedContracts++
			return route.AdaptiveEvaluation{}
		}
		if quality.Metrics.UnroutedEdges > currentQuality.Metrics.UnroutedEdges ||
			quality.Quality.Structural > currentQuality.Quality.Structural ||
			quality.Metrics.ReverseMoves > currentQuality.Metrics.ReverseMoves ||
			quality.Metrics.Crossings+quality.Metrics.Overlaps > currentQuality.Metrics.Crossings+currentQuality.Metrics.Overlaps ||
			solution.CompareQuality(quality.Quality, currentQuality.Quality) >= 0 {
			return route.AdaptiveEvaluation{}
		}
		return route.AdaptiveEvaluation{Quality: routeQuality(quality.Quality), Metrics: routeMetrics(quality.Metrics), Accept: true, CommitReplacements: commit}
	}
}

func legacyReplacements(initial []solution.RoutedEdge, routes []*route.Edge, diagnostics *SolveDiagnostics) []solution.RoutedEdge {
	old := make(map[string]solution.RoutedEdge, len(initial))
	for _, edge := range initial {
		old[edge.ID] = edge
	}
	result := make([]solution.RoutedEdge, 0)
	for _, edge := range routes {
		converted := routedEdgeView(edge)
		previous, ok := old[converted.ID]
		if ok && len(previous.Points) > 0 && len(converted.Points) > 0 && previous.Points[0] == converted.Points[0] {
			converted.StartSide = previous.StartSide
			converted.Start = previous.Start
		}
		if ok && len(previous.Points) > 0 && len(converted.Points) > 0 && previous.Points[len(previous.Points)-1] == converted.Points[len(converted.Points)-1] {
			converted.EndSide = previous.EndSide
			converted.End = previous.End
		}
		if ok && !reflect.DeepEqual(previous, converted) {
			converted.Points = append([]geom.Point(nil), converted.Points...)
			if diagnostics != nil {
				diagnostics.ConversionClonedRoutes++
				diagnostics.ConversionClonedPoints += len(converted.Points)
			}
			result = append(result, converted)
		}
	}
	return result
}

func mergeLegacy(base []solution.RoutedEdge, replacements []*route.Edge) []solution.RoutedEdge {
	byID := make(map[string]solution.RoutedEdge, len(base)+len(replacements))
	for _, edge := range base {
		byID[edge.ID] = edge
	}
	for _, edge := range replacements {
		converted := routedEdgeView(edge)
		byID[converted.ID] = converted
	}
	result := make([]solution.RoutedEdge, 0, len(byID))
	for _, edge := range base {
		if replacement, ok := byID[edge.ID]; ok {
			result = append(result, replacement)
			delete(byID, edge.ID)
		}
	}
	for _, edge := range replacements {
		if replacement, ok := byID[edge.ID]; ok {
			result = append(result, replacement)
			delete(byID, edge.ID)
		}
	}
	return result
}

func mergeRouteEdges(base, replacements []*route.Edge) []*route.Edge {
	byID := make(map[string]*route.Edge, len(base)+len(replacements))
	order := make([]string, 0, len(base)+len(replacements))
	for _, edge := range base {
		if _, ok := byID[edge.ID]; !ok {
			order = append(order, edge.ID)
		}
		byID[edge.ID] = edge
	}
	for _, edge := range replacements {
		if _, ok := byID[edge.ID]; !ok {
			order = append(order, edge.ID)
		}
		byID[edge.ID] = edge
	}
	result := make([]*route.Edge, 0, len(order))
	for _, id := range order {
		result = append(result, byID[id])
	}
	return result
}

func routedEdgeView(e *route.Edge) solution.RoutedEdge {
	r := solution.RoutedEdge{ID: e.ID, From: e.From, To: e.To, Label: e.Label, LabelRect: e.LabelRect, LabelPlaced: e.LabelPlaced, Points: e.Points, Lane: e.Lane, Reversed: e.Reversed, SelfLoop: e.SelfLoop, Long: e.Long, Optimized: e.Optimized, Fallback: e.Fallback, GroupDetour: e.GroupDetour, Kind: e.Kind, StartSide: e.StartSide, EndSide: e.EndSide, StartAuto: e.StartAuto, AdaptiveStart: e.AdaptiveStart, Constrained: e.Constrained, LineStyle: e.LineStyle, ArrowStyle: e.ArrowStyle, Source: e.Source, Target: e.Target}
	if len(r.Points) > 0 {
		if len(r.Points) > 1 {
			r.StartSide = physicalSide(r.Points[0], r.Points[1], r.StartSide)
			r.EndSide = physicalSide(r.Points[len(r.Points)-1], r.Points[len(r.Points)-2], r.EndSide)
		}
		r.Start = solution.ResolvedEndpoint{Semantic: r.Source, Point: r.Points[0], Side: r.StartSide}
		r.End = solution.ResolvedEndpoint{Semantic: r.Target, Point: r.Points[len(r.Points)-1], Side: r.EndSide}
	}
	return r
}

func physicalSide(point, outside geom.Point, fallback diagram.Side) diagram.Side {
	switch {
	case outside.X > point.X:
		return diagram.SideEast
	case outside.X < point.X:
		return diagram.SideWest
	case outside.Y > point.Y:
		return diagram.SideSouth
	case outside.Y < point.Y:
		return diagram.SideNorth
	default:
		return fallback
	}
}

func routeQuality(q solution.Quality) route.Quality {
	return route.Quality{Unrouted: q.Unrouted, Structural: q.Structural, Labels: q.Labels, Intersections: q.Intersections, Reversed: q.Reversed, Directionality: q.Directionality, Excess: q.Excess, CrossAxis: q.CrossAxis, Alignment: q.Alignment, Area: q.Area, Length: q.Length}
}
func routeMetrics(m solution.RouteMetrics) route.Metrics {
	return route.Metrics{NodeOverlaps: m.NodeOverlaps, EdgeNodeCollisions: m.EdgeNodeCollisions, Crossings: m.Crossings, Overlaps: m.Overlaps, TotalLength: m.TotalLength, ExcessLength: m.ExcessLength, Bends: m.Bends, ReverseMoves: m.ReverseMoves, AStarFallbacks: m.AStarFallbacks, UnroutedEdges: m.UnroutedEdges, LabelNodeCollisions: m.LabelNodeCollisions, LabelLabelCollisions: m.LabelLabelCollisions, LabelEdgeCollisions: m.LabelEdgeCollisions, UnplacedLabels: m.UnplacedLabels, GroupBoundaryOverlaps: m.GroupBoundaryOverlaps}
}
