# Render snapshots

`render.json` fixes the rendered bytes, debug bytes, selected optimization,
bounds, and route quality metrics for every E2E fixture.

Do not update a hash merely to make a test pass.

For an intentional solver change, record why the output changed and compare the
structural contract and quality fields before updating the affected entry.

The snapshot contract requires route geometry for every semantic edge,
including invisible edges.

Edge labels are a soft-quality objective: the solver places them when it can do
so without a higher-priority collision and reports `unplaced_labels` otherwise.

The August 2026 routing update adds adjacent-segment label candidates, preserves
labels as a warned overlap when no collision-free position exists, and compares
an expanded A* route budget against the previous budget. Snapshots 02 and 11
changed label geometry without changing quality metrics. Snapshot 13 now places
its previously missing label and reduces crossings from 61 to 55; its additional
fallbacks, one bend, and three cells of route length are lower-priority costs.

The healthcare FHIR regression widens the local search budget for crowded
routes and lets labels slide along a segment when its adjacent row is blocked.
Its route crossings fall from 42 to 32; bounds remain 291x111, while two shared
route units and one label-route collision remain. Labels now avoid group border
cells. Snapshots 11 and 13 change geometry with their recorded quality metrics
unchanged.

The arrow-terminal update keeps at least one visible line cell between the last
bend and a node arrowhead. Snapshots 11 and 13 changed only route geometry.
Snapshot 06 grows by one column and two route cells to satisfy the same terminal
spacing; its structural, label, and intersection metrics do not regress.

Constrained routes now account for group-boundary segments when selecting an
orthogonal path. Cell-port routes are re-routed only when their initial path
would follow a group border. Snapshots 09, 12, and 13 change geometry; all keep
zero structural and label collisions. Snapshot 09 drops from one crossing to
zero and shortens by seven cells. Snapshot 12 shortens by eleven cells and
reduces bends from eight to five. Snapshot 13 drops from 48 to 44 crossings,
from six to five overlaps, and shortens by 34 cells.

Adaptive fan-out routing now visits targets in descending cross-axis order.
This lets nearer targets use inner lanes without crossing already routed links.
The digital-bank regression falls from 27 to 24 route crossings.

Unambiguous automatic fan-ins now assign target ports by each edge's original
source position, rather than an intermediate dummy node. The lakehouse
regression no longer crosses its direct and long incoming edges; snapshot 13
keeps the same crossings and bounds while route length falls by four cells.

The group-interior compaction update first compacts each package's direct
members independently on the cross axis, then collapses repeated empty rows and
columns while retaining one gutter and enough top-border width for the group
label. Snapshots 07 and 08 shrink by one row. Snapshot 11 shrinks by one column.
Snapshot 13 shrinks from 201 to 132 rows and reduces crossings, route length,
excess length, and A* fallbacks; structural and label metrics remain at zero.

The node-only ranking update removes group-driven rank changes while retaining
cross-axis compaction of each group's direct members. Direct rank units move
only when their actual node rectangles overlap, avoiding both reserved scope
bands and rank-crossing collisions. Snapshot 13 remains 430x132; relative to
main, crossings fall from 51 to 48, route length from 3,447 to 3,436, and excess
length from 154 to 152. Snapshot 11 shrinks from 38x46 to 37x46 and route length
falls from 167 to 165. Structural, label, and unrouted metrics remain zero.

Route lanes now use weak-component bounds even when width packing is disabled.
Snapshot 02 changes lane positions; bounds and route-quality metrics are
unchanged. This also prevents isolated nodes moved near a group from widening
an unrelated fan-in's routing channels.

Final compaction also shortens exterior straight runs that previously pinned
group interiors. Against the working-tree baseline, snapshot 13 shrinks from
430x139 to 401x136 and route length falls from 3,483 to 3,375. Crossings (42),
overlaps (3), and bends (94) are unchanged; structural and label metrics remain
zero. Snapshots 01–12 are unchanged by this pass.

Automatic fan-in ports are now reconsidered after source-side optimization.
Swapping target slots removes crossings introduced by a changed approach order
without relaxing explicit port constraints. Snapshot 13 retains 401x136 bounds;
crossings fall from 42 to 38, bends from 94 to 92, and route length from 3,375 to
3,371. Structural and label metrics remain zero; snapshots 01–12 are unchanged.

Group packing now retains label width and padding instead of testing only member
node bounds. Against the pre-review working tree, snapshots 07 and 12 change
only debug/optimization metadata; 08 grows by one column and one route cell;
11 grows from 24 to 37 columns without changing intersections or bends;
13 grows from 136 to 145 rows, with crossings (38), overlaps (3), and bends (92)
unchanged. Snapshot 09 moves the group below the record: height grows from 7 to
15, crossings and shared route units each rise from zero to one, and length
grows from 18 to 32. All retain zero structural and label collisions. The
affected entries also incorporate their pre-existing snapshot differences;
unaffected entries are left alone.

Flow-axis separation now requires one empty cell rather than cross-axis node
spacing, while packing still uses full group bounds. Automatic cell endpoints
also consider their other exposed sides and preserve the final approach
direction. Snapshot 09 returns to 30x7 with zero crossings and overlaps; length
falls from 32 to 4, and its return edge no longer crosses the record interior.
Its E2E contract no longer allows crossings or overlaps. Snapshot 11 returns
from 37 to 24 columns and length 165 to 146, with intersections and bends
unchanged. Other E2E snapshots are unchanged by this correction.

Invisible edges participate in rank and layout constraints. Their route
geometry remains in debug output and routed/unrouted metrics, while the renderer
excludes their line, arrow, and label. Changes must preserve that distinction
unless the snapshot contract is changed explicitly.

## Historical performance snapshot

The following one-iteration measurements were recorded on Go 1.26.0,
darwin/amd64. They are comparison baselines, not portable service-level
objectives.

| Benchmark | Time | Bytes | Allocations | Deterministic work counts |
|---|---:|---:|---:|---|
| Optimize 50 nodes / 100 edges | 9.09 s | 3.44 GB | 11,032,686 | 3 candidates, 4,686,975 A* expansions, 177 clone calls, 117 full evaluations |
| Route initial | 31.5 ms | 3.38 MB | 11,850 | n/a |
| Route baseline | 1.02 s | 403.6 MB | 855,557 | 521,758 A* expansions |
| Label placement | 19.1 ms | 1.04 MB | 1,680 | one pass |
| Full quality evaluation | 16.8 ms | 6.31 MB | 43,292 | one full evaluation |
| Adaptive ports | 5.14 s | 1.15 GB | 5,593,736 | 859,394 A* expansions, 14,500 cloned routes, 103 full evaluations |

The fixed-size optimization boundary at the time of measurement is also part
of the historical baseline. The current solver replaces this node/edge-count
cliff with a deterministic A* expansion budget.

Quality tuples use the `CompareQuality` order: `(unrouted, structural, labels,
intersections, reversed, directionality, excess, cross-axis, alignment, area,
length)`.

| Boundary | Candidates | Selected method | Quality tuple | A* fallbacks |
|---|---:|---|---|---:|
| 149 nodes / 299 edges | 3 | `baseline` | `(0, 0, 0, 14678, 0, 976, 29082, 1122, 60012, 380358, 129740)` | 4 |
| 150 nodes / 300 edges | 3 | `baseline` | `(0, 0, 0, 7411, 0, 5364, 14378, 762, 25751, 941070, 120981)` | 4 |
| 151 nodes / 301 edges | 1 | `baseline` | `(0, 0, 0, 5856, 0, 2613, 9472, 627, 23234, 409431, 71807)` | 4 |

Use `-benchtime=1x -count=1 -benchmem` when comparing these expensive
benchmarks. Run longer benchmark series only after the relevant stage becomes
fast enough for stable repetition.
