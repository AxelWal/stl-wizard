# Making the slow parts fast: welding, duplicated checks, idle cores

Loading a 24.6MB model takes about 3.3 seconds in Go. Nineteen milliseconds of that is
reading the file. The rest is one function.

## What was measured

`AMD Ryzen AI MAX+ 395`, 32 cores, on the real 492,032-triangle export
`gedreht_660_prozent_wholea.stl`:

| Stage | Time | Welds? |
| --- | --- | --- |
| `stl.Read` | 19 ms | no |
| `Mesh.Volume` | 12 ms | no |
| `stl.Write` | 47 ms | no |
| `meshcheck.Check` | 2.49 s | yes |
| `shells.Surfaces` | 2.67 s | yes |
| `cut.Split` | 2.86 s | yes |

A CPU profile of `meshcheck.Check` says where it goes:

```
4.71s 47.34%  internal/runtime/maps.ctrlGroup.matchH2
1.12s 11.26%  runtime.mapaccess1                      (cum 8.07s, 81.11%)
0.97s  9.75%  aeshashbody
0.54s  5.43%  geom.(*Welder).ID                       (cum 7.49s, 75.28%)
```

**81% of the run is `runtime.mapaccess1`, and 75% of it is inside `geom.Welder.ID`.**
Parsing is not the problem. Welding is the problem, and everything slow depends on it —
`meshcheck`, `shells`, `cut`, `repair` and `threemf` each build a `Welder`.

The cause is at `internal/geom/weld.go:30`. Cells are `eps` on a side, so a point within
`eps` may lie in any of the 26 neighbours, and `ID` probes all 27 cells:

```go
for dx := -1; dx <= 1; dx++ { for dy := ...; { for dz := ...; {
    key := [3]int64{c[0] + dx, c[1] + dy, c[2] + dz}
    for _, i := range w.grid[key] { ... }
```

That is 27 lookups on a `map[[3]int64][]int` per vertex — a 24-byte key hashed 27 times.
For 492k triangles, 1,476,096 vertices, **about 40 million map lookups**.

Two further measurements decide the approach:

- **83.4% of vertices are bit-identical duplicates.** On the real file, exact matching
  alone yields precisely the same result as eps-welding: 245,238 distinct points either
  way. On the generated `sphere.stl` they differ (1176 exact against 1106 welded), so
  eps-welding is doing real work there and cannot simply be dropped.
- **One cut welds the same geometry three to five times.** `cut.Split` checks both parts
  (`split.go:310`, `:314`); `ApplyPins` re-checks both (`pins.go:578`); `Tree` re-checks
  every leaf when it measures (`tree.go:199`); `repairCutParts` checks again
  (`app.go:1075`).

## A. Make welding cheap

Two independent changes inside `weld.go`. Nothing outside the file changes, and `ID` keeps
its contract: *return the canonical index for any point within `eps` of a known one*.

**An exact-match fast path.** Keep a `map[Vec3]int` and consult it first. A `Vec3` is a
`[3]float64`, comparable, and hashes as 24 bytes — one lookup instead of 27. It answers
83% of calls on real geometry. A miss falls through to the eps probe exactly as now, so
the tolerance still holds; this only removes work from the common case.

**Eight cells instead of twenty-seven.** Make cells `2*eps` on a side. A point sits at
offset `p ∈ [0, 2*eps)` within its cell, so `v[i] - eps` reaches the lower neighbour only
when `p < eps`, and `v[i] + eps` reaches the upper neighbour only when `p >= eps` —
exactly one neighbour per axis, never both. Probing the point's own cell and that one
neighbour on each axis covers the whole `eps` ball with **8 lookups, and never misses a
candidate**. With `eps`-sized cells all three cells per axis are always in range, which is
why the current code cannot do better than 27.

Expected together: `0.834 × 1 + 0.166 × 8 ≈ 2.2` lookups per vertex against 27 today.

**Why not a flat open-addressing table.** `matchH2`, `aeshashbody` and `memeqbody` are
about 57% of the profile, so replacing Go's map would win a large constant factor too. It
is deliberately not in this spec: it is a new data structure to get wrong, and it only
pays after the lookup *count* is down. Revisit if the numbers below are not met.

### Acceptance

- `meshcheck.Check` on the 492k-triangle file at or under **0.9 s** (from 2.49 s).
- Every existing test in `internal/geom`, `internal/cut`, `internal/meshcheck`,
  `internal/shells`, `internal/repair` and `internal/threemf` still passes unchanged —
  they are the correctness argument, and welding behaviour must not move.
- A new test asserting the property the 8-cell probe relies on: **two points within `eps`
  weld no matter where they fall relative to a cell boundary.** Sweep offsets across a
  cell in each axis, including exactly on the boundary, and assert one id. This must fail
  against a deliberately-wrong probe (7 cells, or the wrong neighbour side).
- A benchmark committed alongside, so the next change to this file can be measured rather
  than argued about.

## B. Stop welding the same mesh repeatedly

Pure duplicated work: the same mesh is checked, then checked again by the next layer up.

- `ApplyPins` re-checks both parts because pinning rewrote the cut faces. That is correct
  and must stay — the geometry genuinely changed.
- `Tree.measure` re-checks a leaf whose mesh `cut.Split` has *already* checked and whose
  verdict is in `Result.Part1Check` / `Part2Check`. That result should be passed in rather
  than recomputed.
- `repairCutParts` checks a part it was handed a verdict for.

The change is to thread the known verdict through, not to cache on the mesh. A cache keyed
on a `*stl.Mesh` is a correctness trap: `ApplyPins` mutates `Tris` in place, so a cache
would answer for geometry that no longer exists. Passing a value the caller already holds
cannot go stale.

`Tree.RefreshLeaves` exists precisely because a mesh can change after its leaf was made,
so it must keep re-checking. Only the paths with a verdict in hand skip the work.

### Acceptance

- A cut of the 492k-triangle model performs **at most one** `meshcheck.Check` per
  resulting part, plus one more per part only when pins or repair actually modify it.
- Counted, not assumed: a test that injects a counting check, or a benchmark showing the
  cut's time falling by roughly one `Check` per part.
- `TestPinnedCutStaysClosed`-style tests and the pin-repair warnings still report the same
  edge counts, because those come from the verdict being threaded through.

## C. Use the idle cores

Thirty-two cores, everything single-threaded.

Ordered by ratio of win to risk:

1. **`meshcheck.Check` and `shells.Surfaces`** weld a whole mesh up front. Shard the
   welder by a hash of the cell, run one goroutine per shard over the triangle list, then
   renumber. Each shard owns disjoint cells, so no locking on the hot path.
2. **`cut.Split`'s triangle classification** — `classifyTri` per triangle is independent.
   The cap triangulation that follows is not, and must stay serial.
3. **`orient.Best`** scores independent candidate rotations. Trivially parallel, and it is
   already behind a busy indicator with no progress reporting.

The risk in 1 is that sharding changes which point becomes canonical when two candidates
are both within `eps`. Ids are internal, but *which* representative is kept is observable
through welded output such as `threemf.writeMesh`. So: the shard's boundary must be
decided by the cell, never by arrival order, and the existing threemf tests are the guard.

`cut.Split` reports progress through `SplitProgress`; parallelising it must keep progress
monotonic, which a shared counter gives.

### Acceptance

- `meshcheck.Check` under **0.25 s** on the 492k file with A and C together.
- Every package still green under `go test ./... -race` — this is the change most likely
  to introduce a data race, and `-race` is already in CI.
- Welded output byte-identical to the serial version for the fixtures: same vertex order,
  same triangle indices. A test that writes a 3MF twice, once forced serial, once
  parallel, and diffs.

## Order, and what would make me stop

A, then B, then C, measuring after each. A is one file and speeds every slow operation at
once. B is removing work that should never have run. C is the only one that adds
concurrency to code that has none, and if A and B alone bring a cut of a real model under
a second, C is not worth its risk.

## What is not in scope

The browser side. A rough measurement put fetching 24.6MB from `/part/{id}.stl` at about
2.6 s, but it was inconsistent with the total for the same load and the handler looks
sound — buffered, and `stl.Write` is 47 ms. That needs measuring properly before anything
is claimed about it, and it is its own piece of work.
