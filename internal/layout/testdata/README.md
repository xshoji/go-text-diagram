# Evaluation corpus

The corpus records graph shapes that the layered layout and renderer must keep readable and deterministic.

The corpus covers ranks, ordering, dummy-node insertion, variable box dimensions, orthogonal routes, terminal-width-aware labels, cycles, route-quality metrics, crossing-aware lanes, A* fallback, and multiline rendering. The route and render packages reuse it to check bounds, collisions, determinism, and the 80% major-collision-free target.

| Inputs | Expected property |
|---|---|
| `01`–`04` | Empty, singleton, edge, and chain inputs remain stable. |
| `05`–`07` | Fan-out, fan-in, and diamond structures preserve their hierarchy. |
| `08`, `14`, `22` | Edges spanning ranks receive one dummy node per skipped rank. |
| `09`, `20` | Disconnected components retain every node without overlap. |
| `10`, `11`, `21` | Multiple roots and sinks receive valid longest-path ranks. |
| `12` | Barycenter sweeps remove the avoidable crossing. |
| `13`, `18`, `19` | Wider and denser DAGs remain deterministic. |
| `15` | Rightward layout transposes the rank and cross axes. |
| `16`, `17` | Quoted and Japanese labels survive parsing unchanged. |
| `23` | A directed cycle is made acyclic deterministically and restored while routing. |
| `24`–`26` | Self-loops, parallel edges, and bidirectional edges retain every input edge. |
| `27` | Single-line and multiline edge labels reserve collision-free route regions. |

The `golden` directory locks the complete layout representation for a chain, a diamond, a long edge, and a rightward layout. Render tests keep separate expected diagrams so layout and rasterization regressions remain distinguishable.
