# STL Cutter — Design

**Date:** 2026-07-25
**Status:** Approved

## Overview

A cross-platform desktop application for splitting STL models into printable
parts. The user opens an STL, rotates it with the mouse, places a **bounded
cutting plane**, and splits the model. Splits are sequential: each cut produces
two parts, and either part can be cut again. Optional alignment pins are added
to the cut faces so the printed pieces fit together.

Go performs all geometry work. The frontend is a webview running three.js,
hosted by Wails v2.

## Goals

- Load and display binary and ASCII STL models; rotate, pan, and zoom with the mouse.
- Place a cutting plane whose **rectangular extent bounds which part of the model is cut**.
- Split sequentially, building a tree of parts, with undo.
- Generate alignment pins (peg and socket) on cut faces, with configurable
  dimensions and a minimum wall-thickness guard.
- Auto-split a model to fit a given printer build volume.
- Report each part's bounding box, triangle count, and volume.
- Export all leaf parts as binary STL files to a chosen folder.
- Ship as a single binary for Linux, macOS, and Windows.

## Non-Goals

- Mesh repair. Non-manifold input is detected and reported, not fixed.
- General CSG. The cutter is always a convex region; no arbitrary mesh booleans.
- Rotating parts to fit a build volume diagonally. Auto-split is axis-aligned.
- Formats other than STL.
- Multi-user or hosted operation. This is a local desktop tool.
- Code signing and notarization. Builds are unsigned; install steps are documented.

## Decisions

These were settled during brainstorming and are not open for reinterpretation
during implementation.

| Decision | Choice | Consequence |
|---|---|---|
| Cut engine | Go backend | Frontend is a viewer and gizmo only; it never modifies geometry. |
| Plane semantics | **Bounded rectangle** | The rectangle limits *which region* is cut, so a plane can cut one arm of a U without touching the other. |
| Cutter depth | Infinite half-space | The rectangle sweeps forever along its normal. No finite-depth pocket carving. |
| Cut workflow | Sequential part tree | One active plane at a time; select a part, cut it, repeat. |
| Geometry approach | Hand-rolled exact clipping | No CGO, no geometry dependency, no voxel remeshing. Original triangles preserved. |
| Shell | Wails v2.13.0 | Desktop app, not a web server. Removes all session and upload plumbing. |
| Builds | GitHub Actions matrix | Native `wails build` on ubuntu/macos/windows runners. |

### The bounded-plane requirement

This is the defining feature and the reason a plain infinite-plane splitter is
insufficient. Given a model shaped like the letter U, the user wants to cut off
the top of the **left** arm only. A horizontal infinite plane at that height
would also cut the right arm. Restricting the plane to a rectangle that spans
only the left arm excludes the right arm from the cut.

```
   ┌──┐      ┌──┐                    ┌──┐      ┌──┐
   │  │      │  │                    │▓▓│ ◄── part 2
 ┈┈┼┈┈┼┈┈    │  │      split         └──┘      │  │
   │  │      │  │      ────►         ┌──┐      │  │
   │  └──────┘  │                    │  └──────┘  │
   └────────────┘                    └────────────┘
   ▲ rectangle spans the             right arm untouched — the
     left arm only                   rectangle never reached it
```

Because the rectangle's edges pass through empty space in this case, both
results are watertight solids. When a rectangle edge *does* pass through
material, the cut simply continues along that edge — the result is still
watertight, just with additional cap faces on the rectangle's side planes. Both
situations are handled by the same algorithm; neither is a special case.

## Architecture

A single Wails v2 application. One process, one user, no network.

```
main.go            wails.Run — options, Bind, AssetServer with custom Handler
app.go             the bound service; its exported methods are the entire API
state.go           part tree, active selection, mutex
assets.go          http.Handler serving /part/{id}.stl
stl/
  read.go          binary + ASCII STL parsing, format detection
  write.go         binary STL writing
  mesh.go          Mesh and Tri types, bbox, volume, triangle count
cut/
  plane.go         PlaneSpec, cutter construction (5 half-spaces)
  clip.go          polygon splitting against a half-space
  cap.go           cut-edge loop assembly, hole nesting, ear clipping
  split.go         orchestration: mesh + cutter → two meshes
  pins.go          pin placement, wall checks, peg/socket geometry
  raycast.go       uniform-grid accelerator for wall-thickness queries
  autosplit.go     recursive build-volume fitting
frontend/
  index.html
  viewer.js        three.js scene, orbit controls, plane gizmo
  ui.js            parts tree, settings panels, progress
  style.css
  vendor/three/    three.module.js, OrbitControls, TransformControls, STLLoader
.github/workflows/build.yml
```

### Boundary rule

**JS owns the canvas. Go owns the geometry. The seam is narrow and typed.**

`stl/` and `cut/` import nothing from Wails and know nothing about HTTP or the
UI. They are pure functions over meshes. This is what makes them unit-testable
and what made the earlier htmx→Wails pivot cost nothing in those packages.

### Bound API

```go
func (a *App) OpenModel() (*Tree, error)
func (a *App) Cut(partID string, p PlaneSpec, pins PinSpec) (*CutResult, error)
func (a *App) Undo() (*Tree, error)
func (a *App) AutoSplit(bedX, bedY, bedZ float64, pins PinSpec) (*Tree, error)
func (a *App) ExportAll() (string, error)
```

```go
type PlaneSpec struct {
    Origin Vec3    // a point on the plane
    Normal Vec3    // unit; the half-space it points into becomes part 2
    U, V   Vec3    // unit in-plane basis vectors, orthogonal to Normal
    Width  float64 // extent along U, centred on Origin
    Height float64 // extent along V, centred on Origin
}

type PinSpec struct {
    Enabled   bool
    Count     int     // target number; fewer may be placed
    Diameter  float64 // mm
    Length    float64 // mm, protrusion of the peg
    Clearance float64 // mm, socket oversize (default 0.15)
    MinWall   float64 // mm, minimum material around and beyond a socket (default 1.0)
    PegOnPart int     // 1 or 2; which child receives the pegs (default 2)
}

type Tree struct {
    ModelName string
    Root      *Part
    SelectedID string
    CanUndo   bool
}

type Part struct {
    ID       string
    Name     string
    Tris     int
    BBox     [2]Vec3
    Volume   float64  // mm³
    Children []*Part  // empty for leaves; only leaves are exported
    Watertight bool
}

type CutResult struct {
    Tree        *Tree
    PinsPlaced  int
    PinsSkipped []SkippedPin  // each with position and measured clearance
    Warnings    []string
}
```

`OpenModel` and `ExportAll` use `runtime.OpenFileDialog` and
`runtime.OpenDirectoryDialog`. Files are read from and written to real paths;
there is no upload or download step.

### Three architectural details

- **Mesh bytes never pass through a bound method.** `assetserver.Options.Handler`
  serves `GET /part/{id}.stl`, and three.js `STLLoader` fetches it. A bound
  method returning megabytes of JSON-encoded floats would be far slower.
- **The mutex is required.** Each JS→Go call runs in its own goroutine, so
  concurrent `Cut` and `Undo` are possible. `state.go` guards the tree.
- **Long operations emit progress.** A multi-million-triangle cut takes seconds.
  `runtime.EventsEmit(ctx, "progress", pct)` drives a progress bar. Bound calls
  already run off the UI thread, so the webview stays responsive.

### Part tree and undo

Each node stores its own mesh. Undo discards the two children of the most
recent cut and re-selects the parent — no operation replay is needed. Only leaf
nodes are exported. Export writes `<model>_part1.stl`, `<model>_part2.stl`, …

## Geometry Engine

### Mesh representation

STL is a triangle soup with no shared vertices, and this design **keeps it that
way**. There is no global vertex-weld pass.

```go
type Tri struct { A, B, C Vec3 }   // Vec3 is [3]float64
type Mesh struct { Tris []Tri }
```

Clipping is per-triangle and independent. The only step that needs topology is
chaining cut edges into loops, and that is handled by a coordinate hash
(below) rather than by welding the whole mesh.

**Deliberate ceiling:** soup storage costs roughly 3× indexed storage. A
2M-triangle model occupies ~150MB as `float64` per tree node, and a deep part
tree multiplies that. If memory becomes a real problem, the upgrade path is an
indexed mesh with a weld pass on load — a change confined to `stl/mesh.go` and
the iteration sites. This is marked with a `ponytail:` comment in the code and
not built up front.

### Tolerance

A single relative epsilon derived from the model's bounding-box diagonal:

```
eps = max(1e-9, 1e-7 * bboxDiagonal)
```

Signed distance `d` to a plane classifies a point as `Inside` when `d > eps`,
`Outside` when `d < -eps`, and `On` when `|d| <= eps`. Treating near-plane
points as exactly on the plane is what prevents sliver triangles.

### Cutter

The cutter is the convex intersection of five half-spaces:

- `H0` — the cut plane itself, keeping the side the normal points into.
- `H1..H4` — the rectangle's four side planes, each pointing inward.

Part 2 is `mesh ∩ cutter`; part 1 is `mesh \ cutter`. Both are closed with caps.

### Split-clip

Sutherland–Hodgman returns only the inside piece, and the outside remainder of
a triangle clipped by a convex region is not convex. So each plane **splits**
rather than clips, and outside fragments are accumulated as they fall out:

```go
insides  := []Polygon{polyFromTri(t)}
var outsides []Polygon

for _, P := range cutter.Planes {          // H0 plus the four sides
    var next []Polygon
    for _, poly := range insides {
        in, out := splitPolygon(poly, P)
        if in != nil  { next = append(next, in) }
        if out != nil { outsides = append(outsides, out) }
    }
    insides = next
}
// insides → part 2, outsides → part 1
```

Every fragment stays convex, because the intersection of a convex polygon with
a half-space is convex. Fan triangulation is therefore always valid for both
sides.

Fast paths, checked before splitting: a triangle entirely inside all five
half-spaces is copied to part 2 unchanged; a triangle entirely outside **any
single** half-space is copied to part 1 unchanged. Only straddling triangles
enter the loop above, so cost scales with the cut's cross-section, not with
model size.

### Canonical edge intersection

When an edge `(p, q)` crosses a plane, the intersection point must come out
**bit-identical** for both triangles sharing that edge, or loop assembly breaks.
The two triangles traverse the edge in opposite directions, so the endpoints are
first ordered canonically — lexicographically by `(x, y, z)` — and the
interpolation is computed from that fixed order. The result is a deterministic
function of the unordered pair.

### Capping

Cap boundaries are recovered **after** splitting, not during it. This keeps the
result independent of the order in which planes are processed.

For each cutter plane `P`, the cap boundary is the set of edges of part 2's
polygons whose **both endpoints lie on `P`**. Those edges are chained into
closed loops using a hash keyed on coordinates quantized to `eps`. The
quantization is the safety net for STL files that store slightly different
float values for what should be the same shared vertex; canonical intersection
keeps the required tolerance small.

Each loop is then triangulated:

1. Project to 2D in `P`'s basis.
2. Signed area gives orientation, distinguishing outer loops from holes. A cut
   through a tube produces an annulus, so holes are expected, not exceptional.
3. Assign each hole to its containing outer loop by point-in-polygon test.
4. Bridge holes into their outer loop, then ear-clip.

Each cap is emitted into **both** parts with opposite winding. That is what
makes both results watertight.

Output triangles with area below `eps²` are discarded.

### Pins

**No boolean operation is required.** Because a cut face is planar, a pin is a
local edit of a 2D polygon triangulation. Peg and socket are the same
construction with the extrusion direction flipped:

- Punch a circle into the cap triangulation as an additional hole loop.
- Emit a cylinder wall — outward for a peg, inward for a socket — plus a
  closing disc.

This forces the pipeline order, since pin circles must be present before the
cap is triangulated:

```
clip
  → assemble cut loops on H0
  → plan pins on those loops
  → add pin circles as hole loops
  → triangulate H0 caps
  → emit pin cylinders and discs
  → assemble and triangulate H1..H4 caps (never pinned)
```

**Lateral placement.** Sample the cap polygon's interior on a grid of spacing
`(r + clearance + minWall) / 2`. Keep sample points whose distance to the
polygon boundary — including hole boundaries — is at least
`r + clearance + minWall`. Select `Count` of them by farthest-point sampling so
pins spread across the face rather than clustering. Both parts share the same
cut face, so one lateral check covers peg and socket.

**Axial placement.** Only the socket needs this check; a peg adds material. The
socket is bored **into** the socket-side part, so its direction is the cut
plane's normal negated for part 2, and the normal itself for part 1 — in both
cases pointing away from the cut face into that part's own material. Cast rays
from the socket footprint — the centre plus eight samples on the circle of
radius `r + clearance` — along that direction, and require the nearest
triangle hit at a distance of at least `Length + clearance + minWall`. The
uniform grid in `raycast.go` accelerates this. Passing this check is precisely
what makes carving the socket safe without a general boolean.

Pins that fail either check are **skipped and reported** with their measured
clearance, never silently dropped.

### Auto-split

Recursive, bounding-box based:

1. If the part fits the bed on all three axes, stop.
2. Otherwise pick the axis with the largest overflow.
3. Cut at the midpoint of that axis, with a rectangle spanning the full
   cross-section plus a small margin, so it behaves as an unbounded plane.
4. Recurse on both children, applying the same `PinSpec`.
5. Stop at a recursion depth of 12 and report any part that still does not fit.

**Known simplification:** parts are never rotated, so a piece that would fit
diagonally is split anyway.

### Volume

Signed tetrahedron sum, which works directly on a soup:

```
V = (1/6) · Σ (a · (b × c))
```

Used for the parts list and, more importantly, as the primary test invariant.

### STL format detection

A binary STL may begin with the ASCII token `solid`, so the token alone is not a
reliable discriminator. Detection reads the triangle count at offset 80 and
checks whether `84 + 50*count` equals the file size. If it does, the file is
binary; otherwise it is parsed as ASCII. Output is always binary.

## Frontend

three.js is vendored under `frontend/vendor/three/` and embedded in the binary —
a desktop app must not depend on a CDN.

**Viewer.** `OrbitControls` for rotate, pan, and zoom. Parts render with
distinct materials; the selected part is highlighted and the others dimmed.

**Plane gizmo.** A translucent quad sized to `Width × Height`, with:

- `TransformControls` in translate and rotate modes for positioning.
- Four corner handles that resize the rectangle. Dragging raycasts onto the
  plane and updates `Width` and `Height`.
- An `ArrowHelper` along the normal, so which side becomes part 2 is never
  ambiguous.

All gizmo interaction is local to the frontend. **No cut is computed while
dragging**; the geometry call happens only when the user commits.

**Panels.** Parts tree with per-part triangle count, bounding box, and volume;
pin settings; bed dimensions for auto-split; buttons for Cut, Undo, and Export;
a progress bar bound to the `progress` event.

State for the tree and panels lives in `ui.js` — about 150–250 lines of vanilla
JS. No frontend framework.

## Error Handling

Every bound method returns `error` as its final value, which Wails surfaces as a
rejected Promise. That gives the UI exactly one place to present failures.

| Case | Policy |
|---|---|
| Unreadable file — not STL, truncated, zero triangles | Refuse at load; name the specific problem. |
| Non-manifold input — cap loops fail to close | **Never silently emit a broken part.** Report the failing plane and the number of open loops. Offer to proceed with the part flagged `Watertight: false`. |
| Degenerate cut — plane misses the part, or rectangle does not intersect it | Refuse with a message; do not create an empty part. |
| Pins skipped | Not an error. Returned in `CutResult.PinsSkipped` with reasons and measured clearances. |
| Auto-split cannot converge | Report which parts still exceed the bed. |
| Export I/O — permission denied, disk full | Surface the OS error verbatim. |

**`recover()` in the bound-method wrapper.** Hand-rolled geometry can panic on
adversarial input, and in a desktop app a panic destroys the window along with
the user's entire part tree. Converting a panic into an error is cheap
insurance and is required, not optional.

## Testing

`stl/` and `cut/` import neither Wails nor `net/http`, so they are testable with
`go test` alone. Tests are written before implementation for each geometry unit.

### Invariants

Three properties must hold for every cut, on every fixture:

1. **Volume conservation** — `vol(part1) + vol(part2) ≈ vol(original)` within
   tolerance. The strongest single signal: it catches inverted winding, missing
   caps, and double-counted triangles simultaneously.
2. **Watertightness** — every edge appears exactly twice, with opposite
   orientation, in each output part.
3. **Outside-the-rectangle identity** — triangles entirely outside the cutter
   footprint are bit-identical to the input. This is the property that proves the
   bounded cut is genuinely bounded.

### Fixtures

Generated procedurally in Go, not committed as binary files:

| Fixture | Exercises |
|---|---|
| Cube | The trivial case; exact expected volumes. |
| Icosphere | Many straddling triangles; curved cut boundary. |
| Tube | Annular cap — proves hole nesting and bridging. |
| Thin-walled box | Pin wall-check rejection. |
| **U shape** | The motivating case: a bounded rectangle cutting one arm while the other stays intact and connected. |
| Deliberately non-manifold mesh | The open-loop error path. |

### Additional coverage

- Round-trip: binary write → read → identical triangles; ASCII read; binary
  detection for a binary file whose header begins with `solid`.
- Pins: socket volume removed matches peg volume added, within tolerance; the
  thin-walled fixture yields skips with correct measured clearances.
- Auto-split: every resulting leaf fits the bed, or is reported.

**Deliberate omission:** no browser automation for the frontend. Verification is
a manual checklist. Visual bugs in a 3D viewer are visible; a Playwright setup
for a single-window desktop tool is not worth its maintenance. This can be
overruled.

## Build and Distribution

- Wails v2.13.0, module path `stl-cutter`.
- Cross-compilation is not supported in v2, so `.github/workflows/build.yml`
  runs a matrix over `ubuntu-latest`, `macos-latest`, and `windows-latest`, each
  executing `wails build` natively and uploading its artifact on tag push.
- Linux build requires `webkit2gtk` development headers. Because `webkit2gtk`
  versions differ across distributions, a plain binary is not reliably portable;
  AppImage is the documented distribution route.
- Windows uses WebView2, present on current Windows 10 and 11 and bundleable if
  needed.
- macOS builds are unsigned. Gatekeeper will warn on first launch; install
  instructions cover it. Notarization requires an Apple Developer account and is
  out of scope.

## Deliberate Simplifications

Recorded so they are tracked rather than forgotten. Each will carry a
`ponytail:` comment at its site in the code.

| Simplification | Add when |
|---|---|
| Triangle soup instead of an indexed mesh | Memory measurably limits usable model size. |
| Uniform grid instead of a BVH for ray casts | Wall-check time becomes noticeable. |
| Infinite cutter depth, no finite-depth pocket | A real need for notches and slots appears. |
| Axis-aligned auto-split, no rotation | Parts fail to fit that would fit rotated. |
| No frontend test automation | The frontend grows beyond a single window's worth of state. |
| Unsigned macOS builds | Distributing to users who will not accept a Gatekeeper warning. |

## Implementation Order

The geometry engine carries all the risk and none of the UI dependencies, so it
comes first and is validated by tests before any window opens.

1. `stl/` — parse, write, mesh primitives, volume, bbox.
2. `cut/clip.go` — split-clip against half-spaces; volume and identity invariants.
3. `cut/cap.go` — loop assembly and ear clipping with holes; watertightness invariant.
4. `cut/split.go` — full bounded cut; the U fixture must pass.
5. Wails shell — window, `OpenModel`, asset handler, three.js viewer, orbit controls.
6. Plane gizmo, then `Cut` wired end to end, then `Undo`.
7. `cut/pins.go` and `cut/raycast.go`.
8. `cut/autosplit.go`, parts list measurements, `ExportAll`.
9. CI matrix workflow.
