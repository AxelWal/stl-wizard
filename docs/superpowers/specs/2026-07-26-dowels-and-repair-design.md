# Dowel holes and mesh repair — design

Two independent features, sharing only the sidebar.

1. **Dowel holes.** A cut can bore a matching hole into *both* pieces instead of
   raising a peg on one, so the user joins them with their own round stock — a
   piece of 1.75mm filament, a 3mm rod, a cocktail stick.
2. **Repair on load.** An optional pass that closes holes in a model as it is
   opened, so a slightly broken download can be cut without every piece coming
   back flagged.

## 1. Dowel holes

### Why it is small

`ApplyPins` already bores a socket into one part and raises a peg on the other.
Dowel mode bores a socket into **both** and raises no peg. Everything that makes
the existing peg/socket pair sound carries over unchanged: both faces are
re-paved from one shared set of hole loops, which is what guarantees they match
vertex for vertex however each part's own loop assembly ordered things.

### Interface

`PinSpec` gains one field:

```go
// Dowel bores a matching hole into both parts instead of raising a peg on one,
// so the two pieces are joined with the user's own round stock. PegOnPart is
// ignored when it is set.
Dowel bool `json:"dowel"`
```

The sidebar's `Peg side` select gains a third option, `Holes on both sides
(dowel)`, which sets `dowel: true`.

### Geometry

The cut plane's normal `N` points into part 2. At the cut face part 2's material
lies along `+N` and part 1's along `-N`. In peg mode both cylinders run the same
way, because the peg fills the socket. In dowel mode each part bores into its own
material, so the two run **opposite** ways:

| | bore direction | basis | re-paved face |
|---|---|---|---|
| part 1 | `-N` | `v` mirrored, since `dir · (u × v) < 0` | not flipped |
| part 2 | `+N` | `u, v` as `planeBasis` gives them | flipped |

The face flips are exactly what they already are — `triangulateFace` emits along
`+N`, so part 2's face is the one that needs reversing — and are unchanged from
peg mode.

Both holes use the socket radius, so a hole is `Diameter + 2 × Clearance` wide.
`Diameter` therefore means **the stock the user will insert**, not the hole.

### Field meanings

- `Diameter` — the round stock. The hole is that plus twice the clearance.
- `Length` — **the depth of each hole**, matching what it already means in peg
  mode, so switching modes does not silently change the geometry. The dowel the
  user cuts is about `2 × Length` long, and the message says so.
- `Clearance` — how much wider and deeper than the stock, so it slides in.
- `MinWall` — unchanged, but see below.

### The wall guard has to check both sides

Today `axialClearance` is measured once, on the socket part only. That is correct
for a peg: only one part is bored. A dowel hole can break out of the far side of
**either** piece, so dowel mode measures both and skips the pin if either fails.
The reported measurement is the worse of the two, and names which side.

This is the one place dowel mode is not simply "the existing socket path twice",
and it is the part most likely to be got wrong by assuming symmetry.

### Reporting

`Placed 2 dowel hole pair(s), 2.05mm wide and 8.15mm deep each side — use about
16mm of 1.75mm stock.` The existing `Placed N of M` shortfall wording and the
per-skip measurements apply unchanged.

## 2. Repair on load

### Algorithm

New package `internal/repair`.

1. Weld vertices within `eps` and tally edge usage, as `meshcheck` does.
2. Drop degenerate triangles — zero area, no valid boundary, trivially removable.
3. Collect edges used exactly once. These are the hole boundaries.
4. Assemble them into closed loops, splitting at any vertex revisited mid-loop so
   a pinch does not produce a self-intersecting ring.
5. Fan each loop from its own centroid, winding each new triangle to oppose the
   boundary edge's direction so the fill faces outward like its neighbours.

**Why a centroid fan and not a proper planar triangulation:** every boundary edge
receives exactly one new triangle, so every boundary edge becomes a shared edge.
The result is watertight *by construction*, not by hope. A non-planar hole gets a
geometrically approximate fill — the fan is not flat — but it is closed, and
closed is what makes the model printable and cuttable. A planar triangulation
would look better on a flat hole and would fail outright on a non-planar one.

`ponytail:` fan from the centroid. Upgrade path if fill quality on large
non-planar holes ever matters: fit a plane per loop, project, and ear-clip with
the existing `cut.earClip` — at the cost of having to handle projection failures.

### What it does not do

**Backwards-wound triangles are not repaired.** Flipping one requires propagating
a consistent orientation across the whole connected surface, which is a different
algorithm with its own failure modes. `Misoriented` is reported before and after,
and a mesh whose only defect is winding comes back reported and unchanged rather
than silently mangled.

### Interface

```go
type Result struct {
    HolesFilled       int
    TrianglesAdded    int
    DegenerateRemoved int
    Before, After     meshcheck.Report
}

func Repair(m *stl.Mesh, eps float64) Result
```

`Repair` rewrites `m.Tris`. It is a no-op returning a zero `HolesFilled` when the
mesh is already closed.

### Wiring

`OpenModel` and `OpenPath` take a `repair bool`. Stateless: the frontend reads
the checkbox and passes it, rather than the App holding a mode.

`TreeView` gains `Repair *RepairView`, non-nil only when repair was asked for,
carrying the counts and the before/after verdicts as strings. The frontend turns
it into a message.

### UI

A checkbox `#repair-on-load`, **unticked by default**, beside `Open STL…` rather
than in a panel — the other panels are hidden until a model is loaded, and this
one has to be reachable before that.

Repair rewrites the user's geometry, so it is asked for, not assumed. A broken
model still loads and is still flagged without it, exactly as now.

### Fixture

`fixtures.OpenBox(size)` — a cube with one face's two triangles removed, giving
four open edges in one loop. Exposed through `cmd/genfixture` as `openbox`.
Nothing in the repository is currently broken in a way repair can be tested
against.

## Testing

Go, per feature:

- Repair closes `OpenBox`: `OpenEdges` goes from 4 to 0, and the volume is
  unchanged to within `eps` because the fill is coplanar with the missing face.
- Repair leaves a closed mesh untouched: zero triangles added.
- Repair drops degenerate triangles.
- Repair reports a misoriented mesh honestly and does not claim to have fixed it.
- Dowel mode bores both parts, adds no peg, and leaves both closed.
- Dowel holes are the same width on both parts.
- The dowel wall guard refuses a hole that would break out of *either* side,
  including the side peg mode would not have measured.

Playwright:

- Repair ticked, load `openbox` → a filled-holes message and `Closed: yes`.
- Repair unticked, load `openbox` → `Closed: no` and a ⚠ in the parts list.
- Dowel mode → cut → both parts lose volume, neither gains any, and the message
  names the stock length to cut.

Every test written to fail first, then the production code broken on purpose
afterwards to confirm the test notices. See CLAUDE.md's standing rules.
