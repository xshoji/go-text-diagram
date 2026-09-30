# E2E scenarios

These PlantUML fixtures exercise the complete CLI path: subset parsing, graph modeling,
layered layout, routing, quality-gated optimization, and terminal rendering.

| Fixture | Scenario | Expected result |
|---|---|---|
| `01` | Basic directed chain | Three boxes and two downward arrows |
| `02` | Branch and merge with labels | Both labels are placed and all four edges are routed |
| `03` | Rightward CJK and multiline labels | Terminal widths and multiline edge labels remain aligned |
| `04` | Cycle, self-loop, and parallel edges | Every original edge is restored and rendered |
| `05` | Undirected/bidirectional edges and line styles | Dotted, dashed, bold, and bidirectional glyphs are visible |
| `06` | Long relation and direction hint | Minimum length and requested connection sides are honored |
| `07` | Class members, point shape, and package | Composite cells and package boundary are visible |
| `08` | Nested groups | Parent containment and both group labels are preserved |
| `09` | Named-cell and group endpoints | Routes start at their named cells and terminate on the group boundary |
| `10` | Disconnected component packing | Four components wrap into two deterministic shelves at width 21 |
| `11` | Combined optimized graph | Labels, cycle, records, groups, and styled routes survive optimization |
| `12` | Component ports | Port-owned relations reach the component boundary and internal worker |
| `13` | Large labeled system with six top-level areas | Width remains bounded and every one of the 49 relations is routed |

Every scenario must be deterministic, retain route geometry for every semantic
edge including invisible edges, and avoid node overlap and edge-node collision.
Edge labels are a soft-quality objective: the solver places them when it can do
so without a higher-priority collision and reports `unplaced_labels` otherwise.
Crossings and shared segments are allowed only where the input intentionally
contains cycles, parallel edges, or reciprocal group endpoints.

Invisible edges constrain rank and layout and remain present in debug route
output and routed/unrouted metrics, but the renderer emits no line, arrow, or
label for them.
