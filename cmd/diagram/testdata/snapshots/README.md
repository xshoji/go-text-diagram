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

The arrow-terminal update keeps at least one visible line cell between the last
bend and a node arrowhead. Snapshots 11 and 13 changed only route geometry.
Snapshot 06 grows by one column and two route cells to satisfy the same terminal
spacing; its structural, label, and intersection metrics do not regress.

The group-interior compaction update first compacts each package's direct
members independently on the cross axis, then collapses repeated empty rows and
columns while retaining one gutter and enough top-border width for the group
label. Snapshots 07 and 08 shrink by one row. Snapshot 11 shrinks by one column.
Snapshot 13 shrinks from 201 to 132 rows and reduces crossings, route length,
excess length, and A* fallbacks; structural and label metrics remain at zero.

The sparse flow-scope packing update treats a direct scope's nodes and internal
dummy nodes as separate collision footprints instead of reserving their entire
bounding rectangle. Snapshot 12 shrinks from 29 to 14 columns and route length
falls from 54 to 32; all structural, label, and intersection metrics remain at
zero.

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
