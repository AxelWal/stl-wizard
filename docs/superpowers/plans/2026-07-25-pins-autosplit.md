# STL Cutter — Pins, Auto-Split and CI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Alignment pins that let printed pieces click together, auto-splitting to fit a printer's build volume, and signed-off builds for Linux, macOS and Windows.

**Architecture:** Pins are a post-pass over a finished cut, not a change to the cutter. The cut face is planar, so a pin is a local edit of that face's triangulation — no boolean operation is needed anywhere.

**Tech Stack:** Go 1.26, Wails v2.13.0, vendored three.js r185. Standard library only in `internal/`.

**Spec:** `docs/superpowers/specs/2026-07-25-stl-cutter-design.md`
**Builds on:** `2026-07-25-geometry-core.md` and `2026-07-25-wails-app.md`, both complete.

## Global Constraints

- Module path `stl-cutter`. Standard library only inside `internal/`. No CGO. No npm, no bundler.
- **`Split` and `SplitProgress` must not change.** They took a BLOCKED report and four review rounds to get correct, and are covered by a randomized-cut regression test. Pins attach afterwards; auto-split calls `Split` repeatedly. If you find yourself editing `split.go`, stop and report.
- Every `wails` command carries `-tags webkit2_41`. A plain build fails on this machine looking for webkit2gtk 4.0 while the system has 4.1, and `wails doctor` reports a **false negative** about it.
- Bound-method changes require regenerating `frontend/wailsjs/` before committing — those files sit outside `go test`'s reach and drift there is invisible until the window breaks.
- `gofmt -l .` must print nothing. `go vet ./...` clean. `go test ./... -race` green.
- Deliberate simplifications get a `// ponytail:` comment naming the ceiling and the upgrade path.

## Why pins are a post-pass

The spec's original pipeline read `clip → assemble cut loops → plan pins → triangulate caps`, assuming one capping stage. **That is no longer how the cutter works.** Plan 1's implementation caps plane by plane: the cut plane's cap is built first, then the four rectangle-side planes trim it. Pins planned on the intermediate cap could sit in material the side planes later remove.

So pins operate on the **finished** parts:

```
Split(mesh, spec)                     unchanged
  → recover the final cut face from part 2's triangles lying on the cut plane
  → plan pin positions on that face
  → rebuild the face with pin circles as extra hole loops
  → emit peg cylinders into one part, socket cavities into the other
```

Only `Planes()[0]` — the plane the user positioned — is ever pinned. The four rectangle sides are structural, not mating surfaces.

## Why no boolean operation is needed

This is what keeps the whole feature small. A cut face is planar, so a pin is a local edit of a 2D polygon:

- **Peg:** punch a circle of radius `r` into the cap triangulation as an extra hole loop, then attach a cylinder wall standing proud of the face and a disc closing its end.
- **Socket:** punch a circle of radius `r + clearance`, then attach a cylinder wall running *into* the material and a disc closing its floor.

Same construction, extrusion direction flipped. The existing `groupLoops` / `bridgeHoles` / `earClip` already triangulate a polygon with holes, so the circles cost nothing new.

Carving the socket is only legal because the wall check proved there is material there — which is why the check is not optional.

## The 1mm guard applies twice

`MinWall` defaults to 1.0mm and is enforced in two independent directions. Both must pass or the pin is skipped and reported.

- **Laterally**, in the plane of the cut face: a pin's centre must be at least `r + clearance + minWall` from the face's boundary, including the boundaries of any holes in it. Otherwise the socket breaks out through the side.
- **Axially**, into the material: from the socket's footprint, there must be at least `Length + clearance + minWall` of material before the nearest surface. Otherwise the socket breaks out through the back.

A skipped pin is reported with its position and the clearance actually measured — never dropped silently.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/cut/raycast.go` | Uniform-grid accelerator and ray–triangle intersection, for the axial wall check. |
| `internal/cut/cutface.go` | Recovering the finished cut face of a `Result` as 2D loops. |
| `internal/cut/pins.go` | `PinSpec`, placement, both wall checks, peg and socket geometry, `ApplyPins`. |
| `internal/cut/autosplit.go` | Recursive bounding-box fitting to a build volume. |
| `app.go` | `PinSpec` on `Cut`; a new `AutoSplit` bound method. |
| `frontend/pins.js` | The pin settings panel and the bed-size panel. |
| `.github/workflows/build.yml` | Native `wails build` on ubuntu, macos and windows runners. |

---

### Task 1: Ray casting

The axial wall check asks "how much material is behind this point?", which is a ray cast against the part's own triangles. A model can have millions of triangles and a cut face can carry tens of pins, so a bare loop over every triangle per ray is too slow — hence a uniform grid.

**Files:**
- Create: `internal/cut/raycast.go`
- Test: `internal/cut/raycast_test.go`

**Interfaces:**
- Consumes: `stl.Mesh`, `stl.Tri`, `geom.Vec3`.
- Produces: `cut.newRayGrid(m *stl.Mesh) *rayGrid`; `(*rayGrid).nearestHit(origin, dir geom.Vec3) (dist float64, ok bool)`; `cut.rayTriangle(origin, dir geom.Vec3, t stl.Tri) (float64, bool)`.

- [ ] **Step 1: Write the failing test**

Create `internal/cut/raycast_test.go`:

```go
package cut

import (
	"math"
	"testing"

	"stl-cutter/internal/fixtures"
	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

func TestRayTriangleHitsAndMisses(t *testing.T) {
	tri := stl.Tri{
		A: geom.Vec3{0, 0, 0},
		B: geom.Vec3{2, 0, 0},
		C: geom.Vec3{0, 2, 0},
	}

	// Straight down onto the middle of the triangle.
	d, ok := rayTriangle(geom.Vec3{0.5, 0.5, 5}, geom.Vec3{0, 0, -1}, tri)
	if !ok {
		t.Fatal("expected a hit")
	}
	if math.Abs(d-5) > 1e-9 {
		t.Errorf("distance = %v, want 5", d)
	}

	// Beside the triangle.
	if _, ok := rayTriangle(geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, -1}, tri); ok {
		t.Error("expected a miss beside the triangle")
	}

	// Pointing away from it.
	if _, ok := rayTriangle(geom.Vec3{0.5, 0.5, 5}, geom.Vec3{0, 0, 1}, tri); ok {
		t.Error("expected a miss pointing away")
	}

	// Parallel to its plane.
	if _, ok := rayTriangle(geom.Vec3{0.5, 0.5, 5}, geom.Vec3{1, 0, 0}, tri); ok {
		t.Error("expected a miss for a ray parallel to the plane")
	}
}

// A hit exactly at the origin of the ray is a zero-distance hit, not a miss —
// the wall check relies on that to notice a surface it is already touching.
func TestRayTriangleCountsAZeroDistanceHit(t *testing.T) {
	tri := stl.Tri{
		A: geom.Vec3{0, 0, 0},
		B: geom.Vec3{2, 0, 0},
		C: geom.Vec3{0, 2, 0},
	}
	d, ok := rayTriangle(geom.Vec3{0.5, 0.5, 0}, geom.Vec3{0, 0, -1}, tri)
	if !ok {
		t.Fatal("expected a hit at zero distance")
	}
	if math.Abs(d) > 1e-9 {
		t.Errorf("distance = %v, want 0", d)
	}
}

func TestRayGridFindsTheNearestSurface(t *testing.T) {
	// A 10-cube spans 0..10. From inside, looking down, the floor is 5 away.
	g := newRayGrid(fixtures.Cube(10))

	d, ok := g.nearestHit(geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, -1})
	if !ok {
		t.Fatal("expected to hit the cube's floor")
	}
	if math.Abs(d-5) > 1e-9 {
		t.Errorf("distance = %v, want 5", d)
	}

	// Looking up, the ceiling is also 5 away.
	d, ok = g.nearestHit(geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, 1})
	if !ok {
		t.Fatal("expected to hit the cube's ceiling")
	}
	if math.Abs(d-5) > 1e-9 {
		t.Errorf("distance = %v, want 5", d)
	}
}

// The grid must return the NEAREST hit, not merely some hit. A hollow box has
// two surfaces along the same ray, and the wall check is only sound if the
// closer one wins.
func TestRayGridReturnsTheNearestOfSeveralHits(t *testing.T) {
	// Outer 0..20, wall 2, so the inner void spans 2..18.
	g := newRayGrid(fixtures.HollowBox(geom.Vec3{20, 20, 20}, 2))

	// From well above, looking down: the outer top face at z=20 comes first.
	d, ok := g.nearestHit(geom.Vec3{10, 10, 30}, geom.Vec3{0, 0, -1})
	if !ok {
		t.Fatal("expected a hit")
	}
	if math.Abs(d-10) > 1e-9 {
		t.Errorf("distance = %v, want 10 — the outer surface, not the inner one", d)
	}
}

func TestRayGridMissesEntirely(t *testing.T) {
	g := newRayGrid(fixtures.Cube(10))
	if _, ok := g.nearestHit(geom.Vec3{50, 50, 50}, geom.Vec3{1, 0, 0}); ok {
		t.Error("expected a miss from outside, pointing away")
	}
}

// The grid is an optimisation, so it must agree exactly with the brute-force
// answer. If it ever disagrees, the acceleration is wrong, not the maths.
func TestRayGridAgreesWithBruteForce(t *testing.T) {
	m := fixtures.UVSphere(10, 24, 12)
	g := newRayGrid(m)

	dirs := []geom.Vec3{
		{0, 0, -1}, {0, 0, 1}, {1, 0, 0}, {-1, 0, 0},
		{1, 1, 1}, {-1, 0.5, -0.25}, {0.3, -1, 0.7},
	}
	origins := []geom.Vec3{
		{0, 0, 0}, {5, 0, 0}, {0, 4, 2}, {-3, -3, 1}, {0, 0, 30},
	}

	for _, o := range origins {
		for _, d := range dirs {
			gotDist, gotOK := g.nearestHit(o, d)

			var wantDist float64 = math.Inf(1)
			wantOK := false
			for _, tri := range m.Tris {
				if dist, ok := rayTriangle(o, d, tri); ok && dist < wantDist {
					wantDist, wantOK = dist, true
				}
			}

			if gotOK != wantOK {
				t.Errorf("origin %v dir %v: grid ok=%v, brute force ok=%v", o, d, gotOK, wantOK)
				continue
			}
			if wantOK && math.Abs(gotDist-wantDist) > 1e-9 {
				t.Errorf("origin %v dir %v: grid %v, brute force %v", o, d, gotDist, wantDist)
			}
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cut/ -run 'RayTriangle|RayGrid' -v`
Expected: FAIL — `undefined: rayTriangle`.

- [ ] **Step 3: Write the implementation**

Create `internal/cut/raycast.go`:

```go
package cut

import (
	"math"

	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

// rayTriangle returns the distance along dir at which the ray from origin meets
// t, using the Möller–Trumbore test. Hits behind the origin do not count; a hit
// exactly at the origin does, because the wall check needs to notice a surface
// it is already touching.
func rayTriangle(origin, dir geom.Vec3, t stl.Tri) (float64, bool) {
	const eps = 1e-12

	e1 := t.B.Sub(t.A)
	e2 := t.C.Sub(t.A)
	h := dir.Cross(e2)
	a := e1.Dot(h)
	if math.Abs(a) < eps {
		return 0, false // parallel to the triangle's plane
	}

	f := 1 / a
	s := origin.Sub(t.A)
	u := f * s.Dot(h)
	if u < 0 || u > 1 {
		return 0, false
	}

	q := s.Cross(e1)
	v := f * dir.Dot(q)
	if v < 0 || u+v > 1 {
		return 0, false
	}

	dist := f * e2.Dot(q)
	if dist < 0 {
		return 0, false // behind the origin
	}
	return dist, true
}

// rayGrid is a uniform spatial grid over a mesh's triangles.
//
// ponytail: a uniform grid rather than a BVH. It is a few dozen lines against a
// few hundred, and a cut face carries tens of pins, not thousands of rays. The
// upgrade path if pin placement ever dominates a cut: swap this type's internals
// for a BVH — nothing outside it depends on the representation.
type rayGrid struct {
	tris  []stl.Tri
	min   geom.Vec3
	cell  geom.Vec3
	dims  [3]int
	cells map[[3]int][]int32
}

func newRayGrid(m *stl.Mesh) *rayGrid {
	b := m.BBox()
	size := b.Size()

	// Aim for roughly one triangle per cell, clamped so a huge mesh does not
	// produce an unreasonable number of cells.
	n := len(m.Tris)
	if n == 0 {
		return &rayGrid{cells: map[[3]int][]int32{}}
	}
	target := math.Cbrt(float64(n))
	if target < 1 {
		target = 1
	}
	if target > 128 {
		target = 128
	}

	g := &rayGrid{tris: m.Tris, min: b.Min, cells: make(map[[3]int][]int32, n)}
	for i := 0; i < 3; i++ {
		d := int(target)
		if d < 1 {
			d = 1
		}
		g.dims[i] = d
		// A zero-extent axis would divide by zero; give it one cell of any size.
		if size[i] <= 0 {
			g.cell[i] = 1
		} else {
			g.cell[i] = size[i] / float64(d)
		}
	}

	for i, t := range m.Tris {
		lo, hi := triCellRange(g, t)
		for x := lo[0]; x <= hi[0]; x++ {
			for y := lo[1]; y <= hi[1]; y++ {
				for z := lo[2]; z <= hi[2]; z++ {
					k := [3]int{x, y, z}
					g.cells[k] = append(g.cells[k], int32(i))
				}
			}
		}
	}
	return g
}

func (g *rayGrid) cellOf(v geom.Vec3) [3]int {
	var c [3]int
	for i := 0; i < 3; i++ {
		idx := int((v[i] - g.min[i]) / g.cell[i])
		if idx < 0 {
			idx = 0
		}
		if idx >= g.dims[i] {
			idx = g.dims[i] - 1
		}
		c[i] = idx
	}
	return c
}

func triCellRange(g *rayGrid, t stl.Tri) (lo, hi [3]int) {
	a, b, c := g.cellOf(t.A), g.cellOf(t.B), g.cellOf(t.C)
	for i := 0; i < 3; i++ {
		lo[i] = min3(a[i], b[i], c[i])
		hi[i] = max3(a[i], b[i], c[i])
	}
	return lo, hi
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

func max3(a, b, c int) int {
	if b > a {
		a = b
	}
	if c > a {
		a = c
	}
	return a
}

// nearestHit returns the distance to the closest surface along dir.
//
// ponytail: this tests every triangle in every cell the ray's bounding box
// touches, rather than walking the grid cell by cell in ray order. It is exact —
// it just does more work than a DDA traversal would. Upgrade path if it shows up
// in a profile: an Amanatides–Woo walk, returning as soon as a cell yields a hit
// closer than the cell's own far boundary.
func (g *rayGrid) nearestHit(origin, dir geom.Vec3) (float64, bool) {
	if len(g.tris) == 0 {
		return 0, false
	}
	d := dir.Unit()

	best := math.Inf(1)
	found := false
	seen := make(map[int32]bool)

	for _, idx := range g.candidates(origin, d) {
		if seen[idx] {
			continue
		}
		seen[idx] = true
		if dist, ok := rayTriangle(origin, d, g.tris[idx]); ok && dist < best {
			best, found = dist, true
		}
	}
	return best, found
}

// candidates returns the triangles worth testing: those in any cell the ray
// passes through. A ray that starts outside the grid is clamped into it, which
// can over-include but never under-includes.
func (g *rayGrid) candidates(origin, dir geom.Vec3) []int32 {
	// The far end of the ray, taken as the grid's diagonal beyond the origin —
	// far enough that anything the ray could hit lies within it.
	var span float64
	for i := 0; i < 3; i++ {
		span += g.cell[i] * float64(g.dims[i])
	}
	end := origin.Add(dir.Scale(span * 2))

	a, b := g.cellOf(origin), g.cellOf(end)
	var out []int32
	for x := min2(a[0], b[0]); x <= max2(a[0], b[0]); x++ {
		for y := min2(a[1], b[1]); y <= max2(a[1], b[1]); y++ {
			for z := min2(a[2], b[2]); z <= max2(a[2], b[2]); z++ {
				out = append(out, g.cells[[3]int{x, y, z}]...)
			}
		}
	}
	return out
}

func min2(a, b int) int {
	if b < a {
		return b
	}
	return a
}

func max2(a, b int) int {
	if b > a {
		return b
	}
	return a
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/cut/ -v`
Expected: PASS, including every pre-existing cut test unchanged.

`TestRayGridAgreesWithBruteForce` is the one that matters: the grid is an optimisation, and the moment it disagrees with the exhaustive answer it is wrong.

- [ ] **Step 5: Commit**

```bash
git add internal/cut/raycast.go internal/cut/raycast_test.go
git commit -m "feat: add ray casting with a uniform grid for wall-thickness queries"
```

---

### Task 2: Recovering the finished cut face

Pins go on the cut face **after** the rectangle's side planes have trimmed it. This task recovers that final face from a completed `Result`.

**Files:**
- Create: `internal/cut/cutface.go`
- Test: `internal/cut/cutface_test.go`

**Interfaces:**
- Consumes: `Result`, `Spec`, `Plane`, `boundaryEdges`, `assembleLoops`, `projectLoop`, `groupLoops`, `planeBasis`, `geom.Welder`.
- Produces: `cut.cutFace(part *stl.Mesh, p Plane, eps float64) (tris []int, groups []faceGroup, origin, u, v geom.Vec3, ok bool)`.

- [ ] **Step 1: Write the failing test**

Create `internal/cut/cutface_test.go`:

```go
package cut

import (
	"math"
	"testing"

	"stl-cutter/internal/fixtures"
	"stl-cutter/internal/geom"
)

func TestCutFaceOfAHalvedCube(t *testing.T) {
	m := fixtures.Cube(10)
	s := SpecFromNormal(geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, 1}, 100, 100)
	res, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	idx, groups, _, _, _, ok := cutFace(res.Part2, s.Planes()[0], m.Epsilon())
	if !ok {
		t.Fatal("no cut face recovered")
	}
	if len(idx) == 0 {
		t.Fatal("no triangles identified as cut face")
	}
	if len(groups) != 1 {
		t.Fatalf("got %d face regions, want 1", len(groups))
	}
	if len(groups[0].Holes) != 0 {
		t.Errorf("got %d holes, want 0 for a solid cube's cross-section", len(groups[0].Holes))
	}

	// The face is the cube's full 10x10 cross-section.
	if got := math.Abs(signedArea2(groups[0].Outer)) / 2; math.Abs(got-100) > 1e-6 {
		t.Errorf("face area = %v, want 100", got)
	}
}

// A tube's cross-section is an annulus, so the recovered face must carry a hole.
func TestCutFaceOfATubeHasAHole(t *testing.T) {
	m := fixtures.Tube(5, 3, 20, 48)
	s := SpecFromNormal(geom.Vec3{0, 0, 10}, geom.Vec3{0, 0, 1}, 100, 100)
	res, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	_, groups, _, _, _, ok := cutFace(res.Part2, s.Planes()[0], m.Epsilon())
	if !ok {
		t.Fatal("no cut face recovered")
	}
	if len(groups) != 1 {
		t.Fatalf("got %d face regions, want 1", len(groups))
	}
	if len(groups[0].Holes) != 1 {
		t.Fatalf("got %d holes, want 1 — the tube's bore", len(groups[0].Holes))
	}

	outer := math.Abs(signedArea2(groups[0].Outer)) / 2
	hole := math.Abs(signedArea2(groups[0].Holes[0])) / 2
	want := math.Pi * (25 - 9)
	if got := outer - hole; math.Abs(got-want)/want > 0.02 {
		t.Errorf("annulus area = %v, want about %v", got, want)
	}
}

// The face must be the TRIMMED one. A bounded rectangle carving a plug out of a
// cube gives a 4x4 face, not the cube's full 10x10 cross-section — this is the
// property that makes pins land in material that survives the cut.
func TestCutFaceIsTrimmedByTheRectangle(t *testing.T) {
	m := fixtures.Cube(10)
	s := SpecFromNormal(geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, 1}, 4, 4)
	res, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	_, groups, _, _, _, ok := cutFace(res.Part2, s.Planes()[0], m.Epsilon())
	if !ok {
		t.Fatal("no cut face recovered")
	}
	if len(groups) != 1 {
		t.Fatalf("got %d face regions, want 1", len(groups))
	}
	if got := math.Abs(signedArea2(groups[0].Outer)) / 2; math.Abs(got-16) > 1e-6 {
		t.Errorf("face area = %v, want 16 — the rectangle's 4x4, not the cube's 100", got)
	}
}

// Cutting the U's left arm gives one face, on that arm alone.
func TestCutFaceOfTheUShape(t *testing.T) {
	m := fixtures.UShape(10)
	s := SpecFromNormal(geom.Vec3{7.5, 25, 5}, geom.Vec3{0, 1, 0}, 15, 20)
	res, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	_, groups, _, _, _, ok := cutFace(res.Part2, s.Planes()[0], m.Epsilon())
	if !ok {
		t.Fatal("no cut face recovered")
	}
	if len(groups) != 1 {
		t.Fatalf("got %d face regions, want 1 — only the left arm was cut", len(groups))
	}
	// The left arm is 10 wide and 10 deep.
	if got := math.Abs(signedArea2(groups[0].Outer)) / 2; math.Abs(got-100) > 1e-6 {
		t.Errorf("face area = %v, want 100", got)
	}
}

func TestCutFaceReportsNothingWhenThePlaneMissesThePart(t *testing.T) {
	m := fixtures.Cube(10)
	s := SpecFromNormal(geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, 1}, 100, 100)
	res, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	far := Plane{N: geom.Vec3{0, 0, 1}, D: 1000}
	if _, _, _, _, _, ok := cutFace(res.Part2, far, m.Epsilon()); ok {
		t.Error("expected no face for a plane nowhere near the part")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cut/ -run CutFace -v`
Expected: FAIL — `undefined: cutFace`.

- [ ] **Step 3: Write the implementation**

Create `internal/cut/cutface.go`:

```go
package cut

import (
	"math"

	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

// cutFace recovers a finished part's cut face: the triangles lying on plane p,
// and that region expressed as 2D loops with their holes.
//
// This runs on a COMPLETED part rather than during the cut. Split caps plane by
// plane, so the cut plane's cap is built before the four rectangle-side planes
// trim it — planning pins on the intermediate cap would put them in material
// those side planes later remove. By the time a part is finished, its triangles
// on p are exactly the face that survived.
//
// idx lists the positions in part.Tris that make up the face, so a caller can
// replace them wholesale after adding pin holes.
func cutFace(part *stl.Mesh, p Plane, eps float64) (idx []int, groups []faceGroup, origin, u, v geom.Vec3, ok bool) {
	origin, u, v = planeBasis(p)

	var polys []Polygon
	for i, t := range part.Tris {
		if math.Abs(p.Dist(t.A)) > eps || math.Abs(p.Dist(t.B)) > eps || math.Abs(p.Dist(t.C)) > eps {
			continue
		}
		idx = append(idx, i)
		polys = append(polys, Polygon{t.A, t.B, t.C})
	}
	if len(polys) == 0 {
		return nil, nil, origin, u, v, false
	}

	// The face's boundary is the set of edges used by exactly one of its
	// triangles. boundaryEdges already computes that, and it cancels the shared
	// interior edges for us.
	w := geom.NewWelder(eps)
	edges := boundaryEdges(polys, p, w, eps)
	if len(edges) == 0 {
		return nil, nil, origin, u, v, false
	}

	loops, open := assembleLoops(edges, w)
	if open > 0 || len(loops) == 0 {
		// A boundary that will not close cannot be pinned safely — the region's
		// true extent is unknown, so every clearance measurement would be a guess.
		return nil, nil, origin, u, v, false
	}

	projected := make([]faceLoop, 0, len(loops))
	for _, l := range loops {
		projected = append(projected, projectLoop(l, origin, u, v))
	}
	groups = groupLoops(projected)
	if len(groups) == 0 {
		return nil, nil, origin, u, v, false
	}
	return idx, groups, origin, u, v, true
}
```

Note `boundaryEdges` skips polygons lying wholly on the plane — and every triangle here does. **Check that before assuming this works**: read `boundaryEdges` in `loops.go` and confirm whether the wholly-on-plane guard was removed during Plan 1's fixes. It was: coplanar polygons now contribute their edges so they cancel against neighbours, which is exactly the behaviour needed here. If your reading disagrees, stop and report rather than editing `loops.go`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cut/ -v`
Expected: PASS, all cut tests including the pre-existing ones.

If `TestCutFaceIsTrimmedByTheRectangle` reports 100 instead of 16, the face being recovered is the intermediate cap rather than the trimmed one — which would mean the triangles on `p` in the finished part are not what this assumes. Report that rather than adjusting the expected number.

- [ ] **Step 5: Commit**

```bash
git add internal/cut/cutface.go internal/cut/cutface_test.go
git commit -m "feat: recover a finished part's trimmed cut face as 2D loops"
```

---

### Task 3: Pin placement

Where the pins go, in the plane of the cut face. The axial check comes next; this task is the lateral one.

**Files:**
- Create: `internal/cut/pins.go`
- Test: `internal/cut/pins_test.go`

**Interfaces:**
- Consumes: `faceGroup`, `faceLoop`, `pt2`, `pointInLoop`, `signedArea2`.
- Produces: `cut.PinSpec`; `(PinSpec).Validate() error`; `(PinSpec).withDefaults() PinSpec`; `cut.distanceToBoundary(p pt2, g faceGroup) float64`; `cut.placePins(g faceGroup, ps PinSpec) []pt2`.

- [ ] **Step 1: Write the failing test**

Create `internal/cut/pins_test.go`:

```go
package cut

import (
	"math"
	"testing"

	"stl-cutter/internal/geom"
)

// squareFace builds a faceGroup covering a square centred on the origin.
func squareFace(half float64) faceGroup {
	return faceGroup{Outer: projectZ(square(0, 0, half))}
}

func TestPinSpecDefaults(t *testing.T) {
	got := PinSpec{Enabled: true, Count: 4, Diameter: 4, Length: 8}.withDefaults()
	if got.Clearance != 0.15 {
		t.Errorf("Clearance = %v, want the 0.15 default", got.Clearance)
	}
	if got.MinWall != 1.0 {
		t.Errorf("MinWall = %v, want the 1.0 default", got.MinWall)
	}
	if got.PegOnPart != 2 {
		t.Errorf("PegOnPart = %v, want 2", got.PegOnPart)
	}

	// An explicit value is not overwritten.
	explicit := PinSpec{Enabled: true, Count: 1, Diameter: 4, Length: 8, Clearance: 0.3, MinWall: 2, PegOnPart: 1}.withDefaults()
	if explicit.Clearance != 0.3 || explicit.MinWall != 2 || explicit.PegOnPart != 1 {
		t.Errorf("defaults overwrote explicit values: %+v", explicit)
	}
}

func TestPinSpecValidate(t *testing.T) {
	good := PinSpec{Enabled: true, Count: 2, Diameter: 4, Length: 8}.withDefaults()
	if err := good.Validate(); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}

	for name, bad := range map[string]PinSpec{
		"zero diameter":   {Enabled: true, Count: 1, Diameter: 0, Length: 8},
		"zero length":     {Enabled: true, Count: 1, Diameter: 4, Length: 0},
		"zero count":      {Enabled: true, Count: 0, Diameter: 4, Length: 8},
		"negative wall":   {Enabled: true, Count: 1, Diameter: 4, Length: 8, MinWall: -1},
		"bad peg side":    {Enabled: true, Count: 1, Diameter: 4, Length: 8, PegOnPart: 3},
		"nan diameter":    {Enabled: true, Count: 1, Diameter: math.NaN(), Length: 8},
		"infinite length": {Enabled: true, Count: 1, Diameter: 4, Length: math.Inf(1)},
	} {
		if err := bad.withDefaults().Validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestDistanceToBoundaryOnASquare(t *testing.T) {
	g := squareFace(5) // 10x10 centred on the origin

	if got := distanceToBoundary(pt2{0, 0}, g); math.Abs(got-5) > 1e-9 {
		t.Errorf("centre distance = %v, want 5", got)
	}
	if got := distanceToBoundary(pt2{4, 0}, g); math.Abs(got-1) > 1e-9 {
		t.Errorf("near-edge distance = %v, want 1", got)
	}
	// Outside the face is not a candidate at all.
	if got := distanceToBoundary(pt2{9, 0}, g); got > 0 {
		t.Errorf("distance outside the face = %v, want 0 or less", got)
	}
}

// A hole in the face constrains placement just as its outer boundary does — a
// socket must not break out into the bore of a tube either.
func TestDistanceToBoundaryRespectsHoles(t *testing.T) {
	g := faceGroup{
		Outer: projectZ(square(0, 0, 10)),
		Holes: []faceLoop{reverseLoop(projectZ(square(0, 0, 2)))},
	}
	// A point 3 from the origin is 1 from the hole's edge, not 7 from the outer.
	if got := distanceToBoundary(pt2{3, 0}, g); math.Abs(got-1) > 1e-9 {
		t.Errorf("distance = %v, want 1 — the hole is nearer than the outer edge", got)
	}
	// A point inside the hole is not on the face at all.
	if got := distanceToBoundary(pt2{0, 0}, g); got > 0 {
		t.Errorf("distance inside the hole = %v, want 0 or less", got)
	}
}

func TestPlacePinsFindsRoomOnALargeFace(t *testing.T) {
	g := squareFace(20) // 40x40
	ps := PinSpec{Enabled: true, Count: 4, Diameter: 4, Length: 8}.withDefaults()

	pins := placePins(g, ps)
	if len(pins) != 4 {
		t.Fatalf("got %d pins, want 4", len(pins))
	}

	need := ps.Diameter/2 + ps.Clearance + ps.MinWall
	for i, p := range pins {
		if d := distanceToBoundary(p, g); d < need {
			t.Errorf("pin %d at %v is %v from the boundary, need %v", i, p, d, need)
		}
	}
}

// Pins must spread out rather than cluster, or they do nothing to stop the
// printed pieces rotating relative to each other.
func TestPlacePinsSpreadsThemOut(t *testing.T) {
	g := squareFace(20)
	ps := PinSpec{Enabled: true, Count: 4, Diameter: 4, Length: 8}.withDefaults()

	pins := placePins(g, ps)
	if len(pins) < 2 {
		t.Fatalf("got %d pins, want at least 2", len(pins))
	}
	closest := math.Inf(1)
	for i := range pins {
		for j := i + 1; j < len(pins); j++ {
			dx, dy := pins[i].X-pins[j].X, pins[i].Y-pins[j].Y
			if d := math.Hypot(dx, dy); d < closest {
				closest = d
			}
		}
	}
	// On a 40x40 face, four pins clustered within a pin diameter of each other
	// would be useless.
	if closest < ps.Diameter*2 {
		t.Errorf("closest pair is %v apart, want them spread across the face", closest)
	}
}

// A face too small for even one pin yields none — reported, not forced in.
func TestPlacePinsRefusesAFaceWithNoRoom(t *testing.T) {
	g := squareFace(1) // 2x2, far too small for a 4mm pin plus 1mm walls
	ps := PinSpec{Enabled: true, Count: 4, Diameter: 4, Length: 8}.withDefaults()

	if pins := placePins(g, ps); len(pins) != 0 {
		t.Errorf("got %d pins on a face with no room, want 0", len(pins))
	}
}

func TestPlacePinsReturnsFewerWhenRoomIsLimited(t *testing.T) {
	g := squareFace(4.5) // 9x9: room for one pin, not four
	ps := PinSpec{Enabled: true, Count: 4, Diameter: 4, Length: 8}.withDefaults()

	pins := placePins(g, ps)
	if len(pins) == 0 || len(pins) > 4 {
		t.Fatalf("got %d pins, want between 1 and 4", len(pins))
	}
	need := ps.Diameter/2 + ps.Clearance + ps.MinWall
	for i, p := range pins {
		if d := distanceToBoundary(p, g); d < need {
			t.Errorf("pin %d is %v from the boundary, need %v", i, d, need)
		}
	}
}
```

Note this file does **not** import `geom` yet — nothing in these tests needs it. Task 4 adds tests that do, and adds the import then.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cut/ -run 'PinSpec|DistanceToBoundary|PlacePins' -v`
Expected: FAIL — `undefined: PinSpec`.

- [ ] **Step 3: Write the implementation**

Create `internal/cut/pins.go`:

```go
package cut

import (
	"errors"
	"fmt"
	"math"
)

// PinSpec describes the alignment pins to place on a cut face. All lengths are
// millimetres.
type PinSpec struct {
	Enabled  bool    `json:"enabled"`
	Count    int     `json:"count"`    // target; fewer are placed if there is no room
	Diameter float64 `json:"diameter"`
	Length   float64 `json:"length"` // how far the peg stands proud of the face

	// Clearance is how much wider the socket is than the peg, so the printed
	// pieces actually fit together.
	Clearance float64 `json:"clearance"`
	// MinWall is the least material that must remain around and beyond a socket.
	// It is enforced both in the plane of the face and into the material behind
	// it; a pin failing either is skipped and reported.
	MinWall float64 `json:"minWall"`
	// PegOnPart is 1 or 2 — which side receives the pegs and which the sockets.
	PegOnPart int `json:"pegOnPart"`
}

func (ps PinSpec) withDefaults() PinSpec {
	if ps.Clearance == 0 {
		ps.Clearance = 0.15
	}
	if ps.MinWall == 0 {
		ps.MinWall = 1.0
	}
	if ps.PegOnPart == 0 {
		ps.PegOnPart = 2
	}
	return ps
}

func (ps PinSpec) Validate() error {
	finite := func(name string, v float64) error {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("pin %s must be a finite number, got %v", name, v)
		}
		return nil
	}
	for name, v := range map[string]float64{
		"diameter": ps.Diameter, "length": ps.Length,
		"clearance": ps.Clearance, "minimum wall": ps.MinWall,
	} {
		if err := finite(name, v); err != nil {
			return err
		}
	}
	if ps.Diameter <= 0 {
		return errors.New("pin diameter must be greater than zero")
	}
	if ps.Length <= 0 {
		return errors.New("pin length must be greater than zero")
	}
	if ps.Count <= 0 {
		return errors.New("pin count must be at least one")
	}
	if ps.Clearance < 0 {
		return errors.New("pin clearance cannot be negative")
	}
	if ps.MinWall < 0 {
		return errors.New("minimum wall cannot be negative")
	}
	if ps.PegOnPart != 1 && ps.PegOnPart != 2 {
		return fmt.Errorf("pegs must go on part 1 or part 2, got %d", ps.PegOnPart)
	}
	return nil
}

// lateralNeed is how far a pin's centre must sit from any boundary of the face:
// its own radius, the socket's clearance, and the wall that must survive around
// it.
func (ps PinSpec) lateralNeed() float64 {
	return ps.Diameter/2 + ps.Clearance + ps.MinWall
}

// distanceToBoundary returns how far p is from the nearest edge of the face,
// counting the boundaries of holes as well as the outer one. A point outside the
// face, or inside one of its holes, gets a non-positive result.
func distanceToBoundary(p pt2, g faceGroup) float64 {
	if !pointInLoop(p, g.Outer) {
		return -1
	}
	for _, h := range g.Holes {
		if pointInLoop(p, h) {
			return -1
		}
	}

	best := distanceToLoop(p, g.Outer)
	for _, h := range g.Holes {
		if d := distanceToLoop(p, h); d < best {
			best = d
		}
	}
	return best
}

func distanceToLoop(p pt2, l faceLoop) float64 {
	best := math.Inf(1)
	for i := range l {
		j := (i + 1) % len(l)
		if d := distanceToSegment(p, l[i].P2, l[j].P2); d < best {
			best = d
		}
	}
	return best
}

func distanceToSegment(p, a, b pt2) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return math.Hypot(p.X-a.X, p.Y-a.Y)
	}
	t := ((p.X-a.X)*dx + (p.Y-a.Y)*dy) / l2
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(p.X-(a.X+t*dx), p.Y-(a.Y+t*dy))
}

// placePins chooses where pins go on a face, in that face's 2D coordinates.
//
// Candidates are sampled on a grid and kept only where there is room for the pin
// plus its clearance plus the wall that must survive around it. They are then
// selected by farthest-point sampling, so pins spread across the face rather
// than clustering — a cluster does nothing to stop the printed pieces rotating
// against each other.
//
// ponytail: a grid sample rather than a medial-axis or distance-transform
// solution. A cut face is a simple polygon of tens of vertices and a handful of
// pins, so the grid is exact enough and a fraction of the code. Upgrade path if
// placement quality ever matters: compute the face's medial axis and place along
// it.
func placePins(g faceGroup, ps PinSpec) []pt2 {
	ps = ps.withDefaults()
	need := ps.lateralNeed()

	lo, hi := loopBounds(g.Outer)
	// Sample fine enough that a viable spot cannot hide between samples.
	step := need / 2
	if step <= 0 {
		return nil
	}

	var candidates []pt2
	for x := lo.X; x <= hi.X; x += step {
		for y := lo.Y; y <= hi.Y; y += step {
			c := pt2{X: x, Y: y}
			if distanceToBoundary(c, g) >= need {
				candidates = append(candidates, c)
			}
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	// Start from the most interior point, then repeatedly take whichever
	// remaining candidate is farthest from everything chosen so far.
	chosen := []pt2{deepest(candidates, g)}
	for len(chosen) < ps.Count {
		best, bestDist := pt2{}, -1.0
		for _, c := range candidates {
			d := math.Inf(1)
			for _, s := range chosen {
				if dd := math.Hypot(c.X-s.X, c.Y-s.Y); dd < d {
					d = dd
				}
			}
			if d > bestDist {
				best, bestDist = c, d
			}
		}
		// Nothing left that is meaningfully apart from what we already have.
		if bestDist < ps.Diameter+ps.Clearance {
			break
		}
		chosen = append(chosen, best)
	}
	return chosen
}

func deepest(candidates []pt2, g faceGroup) pt2 {
	best, bestD := candidates[0], -1.0
	for _, c := range candidates {
		if d := distanceToBoundary(c, g); d > bestD {
			best, bestD = c, d
		}
	}
	return best
}

func loopBounds(l faceLoop) (lo, hi pt2) {
	lo = pt2{X: math.Inf(1), Y: math.Inf(1)}
	hi = pt2{X: math.Inf(-1), Y: math.Inf(-1)}
	for _, v := range l {
		lo.X = math.Min(lo.X, v.P2.X)
		lo.Y = math.Min(lo.Y, v.P2.Y)
		hi.X = math.Max(hi.X, v.P2.X)
		hi.Y = math.Max(hi.Y, v.P2.Y)
	}
	return lo, hi
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cut/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cut/pins.go internal/cut/pins_test.go
git commit -m "feat: place alignment pins on a cut face with a lateral wall guard"
```

---

### Task 4: The axial wall check

The lateral check stops a socket breaking out through the *side* of a part. This one stops it breaking out through the *back*.

**Files:**
- Modify: `internal/cut/pins.go`
- Test: `internal/cut/pins_test.go`

**Interfaces:**
- Consumes: `rayGrid`, `geom.Vec3`.
- Produces: `cut.axialClearance(grid *rayGrid, centre, dir geom.Vec3, radius float64, u, v geom.Vec3, eps float64) float64`.

- [ ] **Step 1: Write the failing test**

Append to `internal/cut/pins_test.go`:

```go
// A 10-cube cut in half at z=5: below the cut face there is 5mm of material.
func TestAxialClearanceMeasuresMaterialBehindTheFace(t *testing.T) {
	m := fixtures.Cube(10)
	grid := newRayGrid(m)
	eps := m.Epsilon()

	got := axialClearance(grid, geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, -1}, 2,
		geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0}, eps)
	if math.Abs(got-5) > 1e-6 {
		t.Errorf("clearance = %v, want 5", got)
	}
}

// The whole footprint is sampled, not just the centre: a socket is a cylinder,
// and it breaks out if ANY of it does.
func TestAxialClearanceUsesTheWholeFootprint(t *testing.T) {
	// A box 20 wide, 20 deep, but only 3 tall over half its span: place the pin
	// so its centre is over deep material while its rim overhangs the shallow part.
	deep := fixtures.Box(geom.Vec3{0, 0, 0}, geom.Vec3{10, 20, 20})
	shallow := fixtures.Box(geom.Vec3{10, 0, 17}, geom.Vec3{20, 20, 20})
	m := &stl.Mesh{Tris: append(append([]stl.Tri{}, deep.Tris...), shallow.Tris...)}

	grid := newRayGrid(m)
	eps := m.Epsilon()

	// Centred at x=8, radius 4, so the footprint reaches x=12 — over the shallow
	// region, where only 3mm remains.
	got := axialClearance(grid, geom.Vec3{8, 10, 20}, geom.Vec3{0, 0, -1}, 4,
		geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0}, eps)
	if got > 4 {
		t.Errorf("clearance = %v; the footprint overhangs 3mm-thick material, so it must report about 3", got)
	}
}

// A ray that escapes without hitting anything means there is nothing behind the
// point at all — the least safe answer, not the most.
func TestAxialClearanceIsZeroWhereThereIsNoMaterial(t *testing.T) {
	m := fixtures.Cube(10)
	grid := newRayGrid(m)
	eps := m.Epsilon()

	// Standing on the top face looking up: nothing above it.
	got := axialClearance(grid, geom.Vec3{5, 5, 10}, geom.Vec3{0, 0, 1}, 2,
		geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0}, eps)
	if got != 0 {
		t.Errorf("clearance = %v, want 0 where there is no material", got)
	}
}

// The face the ray starts on must not count as the first thing it hits, or every
// measurement would be zero.
func TestAxialClearanceIgnoresTheFaceItStartsOn(t *testing.T) {
	m := fixtures.Cube(10)
	grid := newRayGrid(m)
	eps := m.Epsilon()

	got := axialClearance(grid, geom.Vec3{5, 5, 0}, geom.Vec3{0, 0, 1}, 2,
		geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0}, eps)
	if math.Abs(got-10) > 1e-6 {
		t.Errorf("clearance = %v, want 10 — the floor it starts on must not count", got)
	}
}
```

Add `"stl-cutter/internal/stl"` to the test file's imports if it is not already there.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cut/ -run AxialClearance -v`
Expected: FAIL — `undefined: axialClearance`.

- [ ] **Step 3: Write the implementation**

Append to `internal/cut/pins.go`:

```go
// axialClearance measures how much material lies behind a point, in the
// direction a socket would be bored.
//
// The whole footprint is sampled — the centre plus a ring at the socket's radius
// — because a socket is a cylinder and it breaks out if any part of it does.
// The smallest of those measurements wins.
//
// The ray starts a hair inside the material so the face it is standing on does
// not register as a zero-distance hit; that offset is on the order of the mesh's
// own tolerance and is negligible against a millimetre wall.
func axialClearance(grid *rayGrid, centre, dir geom.Vec3, radius float64, u, v geom.Vec3, eps float64) float64 {
	d := dir.Unit()
	start := centre.Add(d.Scale(eps * 4))

	worst := math.Inf(1)
	samples := 8

	probe := func(from geom.Vec3) {
		dist, ok := grid.nearestHit(from, d)
		if !ok {
			// Nothing ahead at all: there is no material to bore into. That is
			// the least safe answer, not an absent one.
			worst = 0
			return
		}
		if dist < worst {
			worst = dist
		}
	}

	probe(start)
	for i := 0; i < samples; i++ {
		a := 2 * math.Pi * float64(i) / float64(samples)
		off := u.Scale(radius * math.Cos(a)).Add(v.Scale(radius * math.Sin(a)))
		probe(start.Add(off))
	}

	if math.IsInf(worst, 1) {
		return 0
	}
	return worst
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cut/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cut/pins.go internal/cut/pins_test.go
git commit -m "feat: measure material behind a pin so a socket cannot break out"
```

---

### Task 5: Peg and socket geometry

The construction that needs no boolean. Both are the same shape with the extrusion direction flipped.

**Files:**
- Modify: `internal/cut/pins.go`
- Test: `internal/cut/pins_test.go`

**Interfaces:**
- Produces: `cut.circleLoop(centre pt2, r float64, segments int, origin, u, v geom.Vec3) faceLoop`; `cut.pinCylinder(base geom.Vec3, dir, u, v geom.Vec3, r, length float64, segments int, cavity bool) []stl.Tri`; the constant `pinSegments`.

- [ ] **Step 1: Write the failing test**

Append to `internal/cut/pins_test.go`:

```go
func TestCircleLoopIsClosedAndCorrectlySized(t *testing.T) {
	l := circleLoop(pt2{0, 0}, 3, 32, geom.Vec3{}, geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0})
	if len(l) != 32 {
		t.Fatalf("got %d vertices, want 32", len(l))
	}
	for i, v := range l {
		if got := math.Hypot(v.P2.X, v.P2.Y); math.Abs(got-3) > 1e-9 {
			t.Errorf("vertex %d is %v from the centre, want 3", i, got)
		}
	}
	// A hole must be wound clockwise so groupLoops treats it as one.
	if signedArea2(l) >= 0 {
		t.Error("a pin circle must be clockwise, so it reads as a hole")
	}
}

func TestCircleLoopCarriesMatching3DPoints(t *testing.T) {
	origin := geom.Vec3{0, 0, 5}
	u := geom.Vec3{1, 0, 0}
	v := geom.Vec3{0, 1, 0}
	l := circleLoop(pt2{2, 0}, 1, 8, origin, u, v)

	for i, fv := range l {
		want := origin.Add(u.Scale(fv.P2.X)).Add(v.Scale(fv.P2.Y))
		if fv.P3.Sub(want).Len() > 1e-9 {
			t.Errorf("vertex %d: 3D point %v does not match its 2D position", i, fv.P3)
		}
	}
}

// A peg is a closed solid on its own: its wall plus its end disc plus the hole
// it leaves in the face.
func TestPinCylinderVolumeMatchesTheIdealPeg(t *testing.T) {
	tris := pinCylinder(geom.Vec3{0, 0, 0}, geom.Vec3{0, 0, 1},
		geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0}, 2, 5, 64, false)

	// Close it with a disc at the base so the volume is measurable.
	base := discAt(geom.Vec3{0, 0, 0}, geom.Vec3{0, 0, -1}, geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0}, 2, 64)
	m := &stl.Mesh{Tris: append(append([]stl.Tri{}, tris...), base...)}

	want := math.Pi * 4 * 5
	if got := m.Volume(); math.Abs(got-want)/want > 0.01 {
		t.Errorf("peg volume = %v, want about %v", got, want)
	}
	if rep := meshcheck.Check(m, 1e-9); !rep.OK() {
		t.Errorf("a capped peg should be a closed solid: %s", rep)
	}
}

// A socket is the same shape wound the other way, so it subtracts rather than adds.
func TestPinCylinderCavityHasNegativeVolume(t *testing.T) {
	tris := pinCylinder(geom.Vec3{0, 0, 0}, geom.Vec3{0, 0, 1},
		geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0}, 2, 5, 64, true)
	base := discAt(geom.Vec3{0, 0, 0}, geom.Vec3{0, 0, 1}, geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0}, 2, 64)
	m := &stl.Mesh{Tris: append(append([]stl.Tri{}, tris...), base...)}

	want := -math.Pi * 4 * 5
	if got := m.Volume(); math.Abs(got-want)/math.Abs(want) > 0.01 {
		t.Errorf("cavity volume = %v, want about %v", got, want)
	}
}
```

Add `"stl-cutter/internal/meshcheck"` to the test file's imports.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cut/ -run 'CircleLoop|PinCylinder' -v`
Expected: FAIL — `undefined: circleLoop`.

- [ ] **Step 3: Write the implementation**

Append to `internal/cut/pins.go`:

```go
// pinSegments is how finely a pin's circle is tessellated. 32 keeps a 4mm pin's
// facets well under a typical printer's resolution.
const pinSegments = 32

// circleLoop returns a pin's circle in a face's 2D coordinates, wound clockwise
// so groupLoops reads it as a hole. The matching 3D points are carried, not
// reconstructed, for the same reason the rest of the cap machinery carries them:
// rebuilding coordinates from 2D rounds, and the rounding shows up as hairline
// cracks.
func circleLoop(centre pt2, r float64, segments int, origin, u, v geom.Vec3) faceLoop {
	l := make(faceLoop, 0, segments)
	for i := 0; i < segments; i++ {
		// Negative angle, so the loop comes out clockwise.
		a := -2 * math.Pi * float64(i) / float64(segments)
		p := pt2{X: centre.X + r*math.Cos(a), Y: centre.Y + r*math.Sin(a)}
		l = append(l, faceVert{
			P2: p,
			P3: origin.Add(u.Scale(p.X)).Add(v.Scale(p.Y)),
		})
	}
	return l
}

// pinCylinder returns the side wall and closing disc of a pin.
//
// With cavity false it is a peg standing proud of the face: the wall faces away
// from the axis and the disc closes its free end. With cavity true it is a
// socket bored into the material: the same surface wound the other way, so it
// faces into the bore and subtracts rather than adds.
//
// base is the centre of the circle where the pin meets the face; dir is the
// direction it extends.
func pinCylinder(base, dir, u, v geom.Vec3, r, length float64, segments int, cavity bool) []stl.Tri {
	d := dir.Unit()
	tip := base.Add(d.Scale(length))

	ring := func(centre geom.Vec3, i int) geom.Vec3 {
		a := 2 * math.Pi * float64(i) / float64(segments)
		return centre.Add(u.Scale(r * math.Cos(a))).Add(v.Scale(r * math.Sin(a)))
	}

	out := make([]stl.Tri, 0, segments*4)
	for i := 0; i < segments; i++ {
		j := (i + 1) % segments
		b0, b1 := ring(base, i), ring(base, j)
		t0, t1 := ring(tip, i), ring(tip, j)

		wall := []stl.Tri{
			{A: b0, B: b1, C: t1},
			{A: b0, B: t1, C: t0},
		}
		// The end disc, fanned from the tip's centre.
		cap := []stl.Tri{{A: tip, B: t1, C: t0}}

		for _, tr := range append(wall, cap...) {
			if cavity {
				tr = tr.Reversed()
			}
			out = append(out, tr)
		}
	}
	return out
}

// discAt returns a flat disc facing along normal. Used to close a pin for testing
// and to floor a socket.
func discAt(centre, normal, u, v geom.Vec3, r float64, segments int) []stl.Tri {
	out := make([]stl.Tri, 0, segments)
	ring := func(i int) geom.Vec3 {
		a := 2 * math.Pi * float64(i) / float64(segments)
		return centre.Add(u.Scale(r * math.Cos(a))).Add(v.Scale(r * math.Sin(a)))
	}
	for i := 0; i < segments; i++ {
		j := (i + 1) % segments
		tr := stl.Tri{A: centre, B: ring(i), C: ring(j)}
		if tr.Normal().Dot(normal) < 0 {
			tr = tr.Reversed()
		}
		out = append(out, tr)
	}
	return out
}
```

Add `"stl-cutter/internal/geom"` and `"stl-cutter/internal/stl"` to `pins.go`'s imports.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cut/ -v`
Expected: PASS.

If the peg's volume comes out negative, the wall winding is inverted — fix the winding, not the expected sign.

- [ ] **Step 5: Commit**

```bash
git add internal/cut/pins.go internal/cut/pins_test.go
git commit -m "feat: build peg and socket geometry without a boolean operation"
```

---

### Task 6: Applying pins to a cut

Everything so far, wired into one call over a finished `Result`.

**Files:**
- Modify: `internal/cut/pins.go`
- Test: `internal/cut/pins_test.go`

**Interfaces:**
- Produces: `cut.SkippedPin{X, Y, Z float64; Reason string; Measured, Required float64}`; `cut.PinResult{Placed int; Skipped []SkippedPin; Warnings []string}`; `cut.ApplyPins(res *Result, s Spec, ps PinSpec) (*PinResult, error)`.

- [ ] **Step 1: Write the failing test**

Append to `internal/cut/pins_test.go`:

```go
func cutCube(t *testing.T) (*Result, Spec, float64) {
	t.Helper()
	m := fixtures.Cube(40)
	s := SpecFromNormal(geom.Vec3{20, 20, 20}, geom.Vec3{0, 0, 1}, 200, 200)
	res, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	return res, s, m.Epsilon()
}

func TestApplyPinsAddsMaterialToOnePartAndRemovesItFromTheOther(t *testing.T) {
	res, s, _ := cutCube(t)
	before1, before2 := res.Part1.Volume(), res.Part2.Volume()

	ps := PinSpec{Enabled: true, Count: 4, Diameter: 4, Length: 6}.withDefaults()
	out, err := ApplyPins(res, s, ps)
	if err != nil {
		t.Fatalf("ApplyPins: %v", err)
	}
	if out.Placed == 0 {
		t.Fatalf("no pins placed on a 40mm face; skipped: %+v", out.Skipped)
	}

	// Pegs go on part 2 by default, sockets on part 1.
	if res.Part2.Volume() <= before2 {
		t.Errorf("part 2 volume %v did not grow from %v — pegs add material",
			res.Part2.Volume(), before2)
	}
	if res.Part1.Volume() >= before1 {
		t.Errorf("part 1 volume %v did not shrink from %v — sockets remove material",
			res.Part1.Volume(), before1)
	}

	// Roughly the right amount: N pegs of the given size.
	wantAdded := float64(out.Placed) * math.Pi * 4 * 6
	if got := res.Part2.Volume() - before2; math.Abs(got-wantAdded)/wantAdded > 0.05 {
		t.Errorf("part 2 grew by %v, want about %v", got, wantAdded)
	}
}

// This is the property that makes pins usable: both parts must still be closed
// solids afterwards, or neither will print.
func TestApplyPinsLeavesBothPartsWatertight(t *testing.T) {
	res, s, eps := cutCube(t)

	ps := PinSpec{Enabled: true, Count: 3, Diameter: 5, Length: 6}.withDefaults()
	if _, err := ApplyPins(res, s, ps); err != nil {
		t.Fatalf("ApplyPins: %v", err)
	}

	if rep := meshcheck.Check(res.Part1, eps); !rep.OK() {
		t.Errorf("part 1 after pinning: %s", rep)
	}
	if rep := meshcheck.Check(res.Part2, eps); !rep.OK() {
		t.Errorf("part 2 after pinning: %s", rep)
	}
}

func TestApplyPinsRespectsThePegSide(t *testing.T) {
	res, s, _ := cutCube(t)
	before1 := res.Part1.Volume()

	ps := PinSpec{Enabled: true, Count: 2, Diameter: 4, Length: 6, PegOnPart: 1}.withDefaults()
	if _, err := ApplyPins(res, s, ps); err != nil {
		t.Fatalf("ApplyPins: %v", err)
	}
	if res.Part1.Volume() <= before1 {
		t.Error("with PegOnPart 1, part 1 should gain material")
	}
}

// The 1mm wall guard, laterally. A face barely wider than the pin has no room.
func TestApplyPinsSkipsWhenTheFaceIsTooNarrow(t *testing.T) {
	m := fixtures.Cube(10)
	// A 5x5 rectangle: a 4mm pin needs 4/2 + 0.15 + 1 = 3.15mm from every edge,
	// which a 5mm-wide face cannot give.
	s := SpecFromNormal(geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, 1}, 5, 5)
	res, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	ps := PinSpec{Enabled: true, Count: 2, Diameter: 4, Length: 3}.withDefaults()
	out, err := ApplyPins(res, s, ps)
	if err != nil {
		t.Fatalf("ApplyPins: %v", err)
	}
	if out.Placed != 0 {
		t.Errorf("placed %d pins on a face with no room", out.Placed)
	}
	if len(out.Warnings) == 0 {
		t.Error("a face with no room must be reported, not silently left bare")
	}
}

// The 1mm wall guard, axially. A thin shell has nothing behind the face.
func TestApplyPinsSkipsWhenThereIsNoMaterialBehind(t *testing.T) {
	// A hollow box with 1.5mm walls: a 6mm-deep socket cannot fit behind the face.
	m := fixtures.HollowBox(geom.Vec3{40, 40, 40}, 1.5)
	s := SpecFromNormal(geom.Vec3{20, 20, 20}, geom.Vec3{0, 0, 1}, 200, 200)
	res, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	ps := PinSpec{Enabled: true, Count: 4, Diameter: 4, Length: 6}.withDefaults()
	out, err := ApplyPins(res, s, ps)
	if err != nil {
		t.Fatalf("ApplyPins: %v", err)
	}
	if out.Placed != 0 {
		t.Errorf("placed %d pins into a 1.5mm shell; a 6mm socket cannot fit", out.Placed)
	}
	for _, sk := range out.Skipped {
		if sk.Measured >= sk.Required {
			t.Errorf("skipped pin reports measured %v >= required %v, which is not a reason to skip",
				sk.Measured, sk.Required)
		}
	}
}

func TestApplyPinsIsANoOpWhenDisabled(t *testing.T) {
	res, s, _ := cutCube(t)
	before1, before2 := res.Part1.Volume(), res.Part2.Volume()

	out, err := ApplyPins(res, s, PinSpec{Enabled: false})
	if err != nil {
		t.Fatalf("ApplyPins: %v", err)
	}
	if out.Placed != 0 {
		t.Errorf("placed %d pins while disabled", out.Placed)
	}
	if res.Part1.Volume() != before1 || res.Part2.Volume() != before2 {
		t.Error("a disabled pin spec must not change the geometry")
	}
}

func TestApplyPinsRejectsAnInvalidSpec(t *testing.T) {
	res, s, _ := cutCube(t)
	if _, err := ApplyPins(res, s, PinSpec{Enabled: true, Count: 1, Diameter: -4, Length: 6}); err == nil {
		t.Error("expected an error for a negative diameter")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cut/ -run ApplyPins -v`
Expected: FAIL — `undefined: ApplyPins`.

- [ ] **Step 3: Write the implementation**

Append to `internal/cut/pins.go`:

```go
// SkippedPin records a pin that could not be placed, and why. A skipped pin is
// always reported: silently leaving one out would let a user print two pieces
// that do not locate against each other.
type SkippedPin struct {
	X, Y, Z  float64 `json:"x"`
	Reason   string  `json:"reason"`
	Measured float64 `json:"measured"`
	Required float64 `json:"required"`
}

type PinResult struct {
	Placed   int          `json:"placed"`
	Skipped  []SkippedPin `json:"skipped"`
	Warnings []string     `json:"warnings"`
}

// ApplyPins adds alignment pins to a finished cut, modifying both parts in place.
//
// It runs after the cut rather than during it because Split caps plane by plane:
// the cut plane's cap is built before the rectangle's side planes trim it, so
// pins planned mid-cut could sit in material those planes later remove. Only the
// plane the user positioned is pinned — the rectangle's sides are structural,
// not mating surfaces.
//
// Both parts stay closed solids: a pin is a hole punched in the cut face plus a
// cylinder closing it, so no boolean operation is involved anywhere.
func ApplyPins(res *Result, s Spec, ps PinSpec) (*PinResult, error) {
	out := &PinResult{}
	if !ps.Enabled {
		return out, nil
	}
	ps = ps.withDefaults()
	if err := ps.Validate(); err != nil {
		return nil, err
	}

	cutPlane := s.Planes()[0]
	eps := res.Part2.Epsilon()

	// The peg extends out of the peg-bearing part, and the socket is bored the
	// same way into the other one — so the peg exactly fills what the socket
	// removes. The cut plane's normal points into part 2, so part 2's face looks
	// the other way.
	pegPart, socketPart := res.Part2, res.Part1
	pegDir := cutPlane.N.Unit().Scale(-1)
	if ps.PegOnPart == 1 {
		pegPart, socketPart = res.Part1, res.Part2
		pegDir = cutPlane.N.Unit()
	}

	idxPeg, groups, origin, u, v, ok := cutFace(pegPart, cutPlane, eps)
	if !ok {
		out.Warnings = append(out.Warnings,
			"could not read the cut face, so no pins were placed")
		return out, nil
	}
	idxSocket, _, _, _, _, okS := cutFace(socketPart, cutPlane, eps)
	if !okS {
		out.Warnings = append(out.Warnings,
			"could not read the matching face on the other part, so no pins were placed")
		return out, nil
	}

	socketGrid := newRayGrid(socketPart)
	r := ps.Diameter / 2
	socketR := r + ps.Clearance
	socketDepth := ps.Length + ps.Clearance
	axialNeed := socketDepth + ps.MinWall

	// Circles to punch into each part's face, and the cylinders to close them.
	pegHoles := map[int][]faceLoop{}
	socketHoles := map[int][]faceLoop{}
	var pegTris, socketTris []stl.Tri

	for gi, g := range groups {
		positions := placePins(g, ps)
		if len(positions) == 0 {
			out.Warnings = append(out.Warnings, fmt.Sprintf(
				"no room for a %gmm pin on one cut face: it needs %gmm of clearance from every edge",
				ps.Diameter, ps.lateralNeed()))
			continue
		}

		for _, p := range positions {
			centre3 := origin.Add(u.Scale(p.X)).Add(v.Scale(p.Y))

			have := axialClearance(socketGrid, centre3, pegDir, socketR, u, v, eps)
			if have < axialNeed {
				out.Skipped = append(out.Skipped, SkippedPin{
					X: centre3[0], Y: centre3[1], Z: centre3[2],
					Reason:   "not enough material behind the face for the socket",
					Measured: have, Required: axialNeed,
				})
				continue
			}

			pegHoles[gi] = append(pegHoles[gi], circleLoop(p, r, pinSegments, origin, u, v))
			socketHoles[gi] = append(socketHoles[gi], circleLoop(p, socketR, pinSegments, origin, u, v))

			pegTris = append(pegTris, pinCylinder(centre3, pegDir, u, v, r, ps.Length, pinSegments, false)...)
			socketTris = append(socketTris, pinCylinder(centre3, pegDir, u, v, socketR, socketDepth, pinSegments, true)...)
			// The socket needs a floor, and the peg's own end disc is already in
			// pinCylinder.
			floor := centre3.Add(pegDir.Scale(socketDepth))
			socketTris = append(socketTris, discAt(floor, pegDir.Scale(-1), u, v, socketR, pinSegments)...)

			out.Placed++
		}
	}

	if out.Placed == 0 {
		return out, nil
	}

	if err := repaveFace(pegPart, idxPeg, groups, pegHoles, cutPlane, origin, u, v, eps, false); err != nil {
		return nil, err
	}
	if err := repaveFace(socketPart, idxSocket, groups, socketHoles, cutPlane, origin, u, v, eps, true); err != nil {
		return nil, err
	}

	pegPart.Tris = append(pegPart.Tris, pegTris...)
	socketPart.Tris = append(socketPart.Tris, socketTris...)
	return out, nil
}

// repaveFace replaces a part's cut-face triangles with a fresh triangulation that
// has the pin circles as extra holes.
//
// flip is set for the part whose face looks the other way, so both parts keep
// their own outward orientation.
func repaveFace(part *stl.Mesh, idx []int, groups []faceGroup, holes map[int][]faceLoop,
	p Plane, origin, u, v geom.Vec3, eps float64, flip bool) error {

	keep := make([]stl.Tri, 0, len(part.Tris))
	drop := make(map[int]bool, len(idx))
	for _, i := range idx {
		drop[i] = true
	}
	for i, t := range part.Tris {
		if !drop[i] {
			keep = append(keep, t)
		}
	}

	for gi, g := range groups {
		merged := bridgeHoles(g.Outer, append(append([]faceLoop{}, g.Holes...), holes[gi]...))
		tris, ok := earClip(merged)
		if !ok {
			return fmt.Errorf("could not re-triangulate a cut face around its pins")
		}
		for _, tr := range tris {
			t := stl.Tri{A: merged[tr[0]].P3, B: merged[tr[1]].P3, C: merged[tr[2]].P3}
			// triangulateFace emits faces along +p.N; each part needs its own
			// outward direction.
			if flip {
				t = t.Reversed()
			}
			keep = append(keep, t)
		}
	}

	part.Tris = keep
	return nil
}
```

Add `"fmt"` to `pins.go`'s imports.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cut/ -v`
Expected: PASS.

**If the watertightness test fails, do not weaken it.** The likely causes, in order: the re-paved face is wound the wrong way for one of the parts (check the `flip` argument), the peg's circle and the socket's circle differ in radius so the two faces no longer meet (they are meant to — that is what `Clearance` is), or `earClip` refused the face with its pin holes, which `repaveFace` should be reporting as an error rather than silently dropping.

- [ ] **Step 5: Commit**

```bash
git add internal/cut/pins.go internal/cut/pins_test.go
git commit -m "feat: apply alignment pins to a finished cut, guarded on both axes"
```

---

### Task 7: Auto-split to a build volume

Recursively halve a model until every piece fits the printer.

**Files:**
- Create: `internal/cut/autosplit.go`
- Test: `internal/cut/autosplit_test.go`

**Interfaces:**
- Consumes: `Split`, `Spec`, `SpecFromNormal`, `stl.Mesh`.
- Produces: `cut.Bed{X, Y, Z float64}` with `Fits(b stl.BBox) bool`; `cut.AutoSplitStep{Spec Spec; Depth int}`; `cut.PlanAutoSplit(m *stl.Mesh, bed Bed) ([]AutoSplitStep, error)`; the constant `maxAutoSplitDepth`.

- [ ] **Step 1: Write the failing test**

Create `internal/cut/autosplit_test.go`:

```go
package cut

import (
	"testing"

	"stl-cutter/internal/fixtures"
	"stl-cutter/internal/stl"
)

func TestBedFits(t *testing.T) {
	bed := Bed{X: 220, Y: 220, Z: 250}
	if !bed.Fits(fixtures.Cube(100).BBox()) {
		t.Error("a 100mm cube fits a 220x220x250 bed")
	}
	if bed.Fits(fixtures.Cube(300).BBox()) {
		t.Error("a 300mm cube does not fit")
	}
	// Exactly the bed size counts as fitting.
	if !bed.Fits(fixtures.Cube(220).BBox()) {
		t.Error("a part exactly the bed's size should fit")
	}
}

func TestPlanAutoSplitLeavesASmallModelAlone(t *testing.T) {
	steps, err := PlanAutoSplit(fixtures.Cube(50), Bed{X: 220, Y: 220, Z: 250})
	if err != nil {
		t.Fatalf("PlanAutoSplit: %v", err)
	}
	if len(steps) != 0 {
		t.Errorf("got %d cuts for a model that already fits, want none", len(steps))
	}
}

func TestPlanAutoSplitHalvesAnOversizedModel(t *testing.T) {
	// 300mm cube on a 220mm bed: one cut per axis that overflows.
	steps, err := PlanAutoSplit(fixtures.Cube(300), Bed{X: 220, Y: 220, Z: 250})
	if err != nil {
		t.Fatalf("PlanAutoSplit: %v", err)
	}
	if len(steps) == 0 {
		t.Fatal("got no cuts for a model three times the bed")
	}
	// Every planned cut must be a valid spec the cutter will accept.
	for i, s := range steps {
		if err := s.Spec.Validate(); err != nil {
			t.Errorf("step %d produced an invalid spec: %v", i, err)
		}
	}
}

// The real test: apply the plan and confirm every resulting piece fits.
func TestPlanAutoSplitProducesPiecesThatFit(t *testing.T) {
	bed := Bed{X: 120, Y: 120, Z: 120}
	m := fixtures.Cube(300)

	steps, err := PlanAutoSplit(m, bed)
	if err != nil {
		t.Fatalf("PlanAutoSplit: %v", err)
	}

	// Apply the plan breadth-first, exactly as the app will.
	pieces := []*stl.Mesh{m}
	for _, step := range steps {
		var next []*stl.Mesh
		for _, p := range pieces {
			if bed.Fits(p.BBox()) {
				next = append(next, p)
				continue
			}
			res, err := Split(p, step.Spec)
			if err != nil {
				// A step that does not apply to this piece is not a failure of
				// the plan; another step will reach it.
				next = append(next, p)
				continue
			}
			next = append(next, res.Part1, res.Part2)
		}
		pieces = next
	}

	for i, p := range pieces {
		if !bed.Fits(p.BBox()) {
			t.Errorf("piece %d is %v, which does not fit %v", i, p.BBox().Size(), bed)
		}
	}
}

func TestPlanAutoSplitRejectsANonsenseBed(t *testing.T) {
	for name, bed := range map[string]Bed{
		"zero":     {X: 0, Y: 100, Z: 100},
		"negative": {X: 100, Y: -1, Z: 100},
	} {
		if _, err := PlanAutoSplit(fixtures.Cube(50), bed); err == nil {
			t.Errorf("%s bed: expected an error", name)
		}
	}
}

func TestPlanAutoSplitStopsAtTheDepthCap(t *testing.T) {
	// A bed so small that halving could recurse forever.
	steps, err := PlanAutoSplit(fixtures.Cube(1000), Bed{X: 0.5, Y: 0.5, Z: 0.5})
	if err != nil {
		t.Fatalf("PlanAutoSplit: %v", err)
	}
	for _, s := range steps {
		if s.Depth > maxAutoSplitDepth {
			t.Fatalf("step at depth %d exceeds the cap of %d", s.Depth, maxAutoSplitDepth)
		}
	}
}
```

This file imports `stl-cutter/internal/stl` for `*stl.Mesh`, and `stl-cutter/internal/geom` is not needed.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cut/ -run 'Bed|PlanAutoSplit' -v`
Expected: FAIL — `undefined: Bed`.

- [ ] **Step 3: Write the implementation**

Create `internal/cut/autosplit.go`:

```go
package cut

import (
	"fmt"
	"math"

	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

// maxAutoSplitDepth bounds the recursion. A model needing more than this many
// halvings on one axis is either enormous or the bed is unusable, and either way
// the user should hear about it rather than wait.
const maxAutoSplitDepth = 12

// Bed is a printer's build volume in millimetres.
type Bed struct {
	X, Y, Z float64 `json:"x"`
}

func (b Bed) valid() error {
	for name, v := range map[string]float64{"width": b.X, "depth": b.Y, "height": b.Z} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("bed %s must be a finite number, got %v", name, v)
		}
		if v <= 0 {
			return fmt.Errorf("bed %s must be greater than zero, got %v", name, v)
		}
	}
	return nil
}

// Fits reports whether a bounding box sits inside the bed, axis for axis.
//
// ponytail: axis-aligned only — a part that would fit if turned diagonally is
// split anyway. Rotating to fit is a packing problem, and this is a splitter.
// Upgrade path if it matters: try the six axis permutations before splitting.
func (b Bed) Fits(box stl.BBox) bool {
	s := box.Size()
	return s[0] <= b.X && s[1] <= b.Y && s[2] <= b.Z
}

// AutoSplitStep is one planned cut. Depth records how many halvings deep it is,
// so a caller can show progress or stop early.
type AutoSplitStep struct {
	Spec  Spec `json:"spec"`
	Depth int  `json:"depth"`
}

// PlanAutoSplit works out the cuts needed to bring a model within a build volume.
//
// It plans rather than cuts, so the caller keeps control: the app applies each
// step through the same Cut path a manual cut uses, which means the part tree,
// the undo history and the watertightness reporting all behave identically.
//
// Each step halves the longest overflowing axis with a rectangle spanning the
// whole cross-section, so the bounded plane behaves as an unbounded one.
func PlanAutoSplit(m *stl.Mesh, bed Bed) ([]AutoSplitStep, error) {
	if err := bed.valid(); err != nil {
		return nil, err
	}
	if len(m.Tris) == 0 {
		return nil, fmt.Errorf("mesh has no triangles")
	}

	var steps []AutoSplitStep
	var plan func(box stl.BBox, depth int)

	plan = func(box stl.BBox, depth int) {
		if bed.Fits(box) || depth >= maxAutoSplitDepth {
			return
		}

		// Halve whichever axis overflows by the most.
		size := box.Size()
		limits := [3]float64{bed.X, bed.Y, bed.Z}
		axis, worst := -1, 0.0
		for i := 0; i < 3; i++ {
			if over := size[i] - limits[i]; over > worst {
				axis, worst = i, over
			}
		}
		if axis < 0 {
			return
		}

		var normal geom.Vec3
		normal[axis] = 1
		centre := box.Min.Add(size.Scale(0.5))

		// A rectangle spanning the whole cross-section, so the bound never bites.
		span := size.Len() * 2
		steps = append(steps, AutoSplitStep{
			Spec:  SpecFromNormal(centre, normal, span, span),
			Depth: depth,
		})

		// Both halves have the same bounding box bar the halved axis.
		lower, upper := box, box
		mid := centre[axis]
		lower.Max[axis] = mid
		upper.Min[axis] = mid
		plan(lower, depth+1)
		plan(upper, depth+1)
	}

	plan(m.BBox(), 0)
	return steps, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cut/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cut/autosplit.go internal/cut/autosplit_test.go
git commit -m "feat: plan the cuts needed to fit a model on a printer bed"
```

---

### Task 8: Pins in the app layer

**Files:**
- Modify: `app.go`
- Test: `app_test.go`

**Interfaces:**
- Produces: `Cut` gains a `pins cut.PinSpec` parameter; `CutOutcome` gains `PinsPlaced int` and `PinsSkipped []cut.SkippedPin`.

- [ ] **Step 1: Write the failing test**

Append to `app_test.go`:

```go
func TestCutWithPinsAddsThemToBothParts(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(40))); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID

	out, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{20, 20, 20}, Normal: [3]float64{0, 0, 1}, Width: 200, Height: 200,
	}, cut.PinSpec{Enabled: true, Count: 4, Diameter: 4, Length: 6})
	if err != nil {
		t.Fatalf("Cut: %v", err)
	}
	if out.PinsPlaced == 0 {
		t.Fatalf("no pins placed on a 40mm face; skipped: %+v", out.PinsSkipped)
	}
	if !out.Watertight {
		t.Errorf("pinned parts should still be closed solids; warnings: %v", out.Warnings)
	}
}

// Pins change the geometry, so the tree's recorded measurements must reflect the
// pinned parts, not the bare ones.
func TestCutWithPinsRecordsThePinnedVolumes(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(40))); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID

	bare, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{20, 20, 20}, Normal: [3]float64{0, 0, 1}, Width: 200, Height: 200,
	}, cut.PinSpec{})
	if err != nil {
		t.Fatalf("bare Cut: %v", err)
	}
	bareVolume := bare.Tree.Root.Children[1].Volume

	if _, err := app.Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}

	pinned, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{20, 20, 20}, Normal: [3]float64{0, 0, 1}, Width: 200, Height: 200,
	}, cut.PinSpec{Enabled: true, Count: 4, Diameter: 4, Length: 6})
	if err != nil {
		t.Fatalf("pinned Cut: %v", err)
	}
	if got := pinned.Tree.Root.Children[1].Volume; got <= bareVolume {
		t.Errorf("pinned part volume %v is not greater than the bare %v; the tree recorded the wrong mesh",
			got, bareVolume)
	}
}

func TestCutWithPinsReportsSkips(t *testing.T) {
	app := NewApp()
	// A thin shell has no material behind the cut face for a socket.
	if _, err := app.loadPath(writeFixture(t, "shell.stl", fixtures.HollowBox(geom.Vec3{40, 40, 40}, 1.5))); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID

	out, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{20, 20, 20}, Normal: [3]float64{0, 0, 1}, Width: 200, Height: 200,
	}, cut.PinSpec{Enabled: true, Count: 4, Diameter: 4, Length: 6})
	if err != nil {
		t.Fatalf("Cut: %v", err)
	}
	if out.PinsPlaced != 0 {
		t.Errorf("placed %d pins into a 1.5mm shell", out.PinsPlaced)
	}
	if len(out.PinsSkipped) == 0 && len(out.Warnings) == 0 {
		t.Error("skipped pins must be reported, not silently dropped")
	}
}

func TestCutWithoutPinsIsUnchanged(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10))); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID

	out, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{5, 5, 5}, Normal: [3]float64{0, 0, 1}, Width: 100, Height: 100,
	}, cut.PinSpec{})
	if err != nil {
		t.Fatalf("Cut: %v", err)
	}
	if out.PinsPlaced != 0 {
		t.Errorf("placed %d pins with pinning disabled", out.PinsPlaced)
	}
	for _, c := range out.Tree.Root.Children {
		if c.Volume < 499 || c.Volume > 501 {
			t.Errorf("child volume = %v, want about 500 — an unpinned cut must be unchanged", c.Volume)
		}
	}
}
```

Add `"stl-cutter/internal/cut"` to `app_test.go`'s imports. **Every existing `app.Cut(...)` call in the file now needs a third argument** — pass `cut.PinSpec{}` to leave them behaving exactly as before, and say in your report how many you updated.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test . -run CutWithPins -v`
Expected: FAIL — too many arguments to `app.Cut`.

- [ ] **Step 3: Write the implementation**

In `app.go`, extend `CutOutcome`:

```go
type CutOutcome struct {
	Tree        *TreeView        `json:"tree"`
	Watertight  bool             `json:"watertight"`
	Warnings    []string         `json:"warnings"`
	PinsPlaced  int              `json:"pinsPlaced"`
	PinsSkipped []cut.SkippedPin `json:"pinsSkipped"`
}
```

Change `Cut`'s signature to `func (a *App) Cut(partID string, p PlaneInput, pins cut.PinSpec) (*CutOutcome, error)`.

Inside, after `cut.SplitProgress` succeeds and **before** `tr.Split` records the parts in the tree, apply the pins:

```go
		// Pins must be applied before the parts go into the tree, or the tree
		// would record the volumes and triangle counts of the bare parts.
		pinRes, err := cut.ApplyPins(res, spec, pins)
		if err != nil {
			return err
		}
		out.PinsPlaced = pinRes.Placed
		out.PinsSkipped = pinRes.Skipped
		pinWarnings = pinRes.Warnings
```

Carry `pinWarnings` out of the closure and append it to the outcome's warnings alongside `res.Warnings`.

**Re-verify watertightness after pinning.** `res.Watertight()` reflects the cut, not the pinned result, so recompute:

```go
	// Pinning rewrites the cut face, so the cutter's own verdict is now stale.
	// Check what is actually about to be handed over.
	p1 := meshcheck.Check(res.Part1, res.Part1.Epsilon())
	p2 := meshcheck.Check(res.Part2, res.Part2.Epsilon())
	watertight := res.Watertight() && p1.OK() && p2.OK()
```

and use `p1.OK()`/`p2.OK()` for the two children's flags. Add a warning naming the part when either fails.

Add `"stl-cutter/internal/meshcheck"` to `app.go`'s imports.

- [ ] **Step 4: Update the frontend call**

`frontend/ui.js` calls `Cut(currentTree.selectedId, planeInput())`. It now needs a third argument. For this task pass a disabled spec so nothing changes on screen yet; Task 10 adds the panel:

```js
    const outcome = await Cut(currentTree.selectedId, planeInput(), { enabled: false });
```

- [ ] **Step 5: Verify**

Run: `gofmt -l . && go vet ./... && go test ./... -race -count=1`, then `wails build -tags webkit2_41` to regenerate bindings. Commit the `frontend/wailsjs/` changes — `Cut`'s signature changed, so they will differ.

- [ ] **Step 6: Commit**

```bash
git add app.go app_test.go frontend/
git commit -m "feat: apply alignment pins as part of a cut"
```

---

### Task 9: AutoSplit in the app layer

**Files:**
- Modify: `app.go`
- Test: `app_test.go`

**Interfaces:**
- Produces: `(*App).AutoSplit(bed cut.Bed, pins cut.PinSpec) (*AutoSplitOutcome, error)`; `AutoSplitOutcome`.

- [ ] **Step 1: Write the failing test**

Append to `app_test.go`:

```go
func TestAutoSplitLeavesAFittingModelAlone(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(50))); err != nil {
		t.Fatalf("load: %v", err)
	}

	out, err := app.AutoSplit(cut.Bed{X: 220, Y: 220, Z: 250}, cut.PinSpec{})
	if err != nil {
		t.Fatalf("AutoSplit: %v", err)
	}
	if out.CutsMade != 0 {
		t.Errorf("made %d cuts on a model that already fits", out.CutsMade)
	}
	if len(out.Tree.Root.Children) != 0 {
		t.Error("the tree should be untouched")
	}
}

func TestAutoSplitDividesAnOversizedModel(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "big.stl", fixtures.Cube(300))); err != nil {
		t.Fatalf("load: %v", err)
	}

	bed := cut.Bed{X: 120, Y: 120, Z: 120}
	out, err := app.AutoSplit(bed, cut.PinSpec{})
	if err != nil {
		t.Fatalf("AutoSplit: %v", err)
	}
	if out.CutsMade == 0 {
		t.Fatal("made no cuts on a model far larger than the bed")
	}

	// Every leaf must now fit, or be named as one that does not.
	var tooBig int
	for _, leaf := range app.session.Tree().Leaves() {
		box := stl.BBox{
			Min: geom.Vec3{leaf.Min[0], leaf.Min[1], leaf.Min[2]},
			Max: geom.Vec3{leaf.Min[0] + leaf.Size[0], leaf.Min[1] + leaf.Size[1], leaf.Min[2] + leaf.Size[2]},
		}
		if !bed.Fits(box) {
			tooBig++
		}
	}
	if tooBig != len(out.StillTooBig) {
		t.Errorf("%d leaves do not fit but %d were reported", tooBig, len(out.StillTooBig))
	}
}

func TestAutoSplitIsUndoable(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "big.stl", fixtures.Cube(300))); err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := app.AutoSplit(cut.Bed{X: 120, Y: 120, Z: 120}, cut.PinSpec{}); err != nil {
		t.Fatalf("AutoSplit: %v", err)
	}

	// Every cut it made must be undoable one at a time, like any other cut.
	undone := 0
	for app.session.Tree().CanUndo() {
		if _, err := app.Undo(); err != nil {
			t.Fatalf("Undo after %d: %v", undone, err)
		}
		undone++
	}
	if undone == 0 {
		t.Fatal("auto-split left nothing to undo")
	}
	if len(app.session.Tree().Leaves()) != 1 {
		t.Errorf("after undoing everything there are %d leaves, want 1", len(app.session.Tree().Leaves()))
	}
}

func TestAutoSplitRejectsANonsenseBed(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(50))); err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := app.AutoSplit(cut.Bed{X: 0, Y: 100, Z: 100}, cut.PinSpec{}); err == nil {
		t.Error("expected an error for a zero-width bed")
	}
}

func TestAutoSplitWithNoModelOpen(t *testing.T) {
	app := NewApp()
	if _, err := app.AutoSplit(cut.Bed{X: 220, Y: 220, Z: 250}, cut.PinSpec{}); err == nil {
		t.Error("expected an error when no model is open")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test . -run AutoSplit -v`
Expected: FAIL — `app.AutoSplit undefined`.

- [ ] **Step 3: Write the implementation**

Add to `app.go`:

```go
// AutoSplitOutcome reports what auto-splitting did and what it could not manage.
type AutoSplitOutcome struct {
	Tree        *TreeView `json:"tree"`
	CutsMade    int       `json:"cutsMade"`
	StillTooBig []string  `json:"stillTooBig"`
	Warnings    []string  `json:"warnings"`
}

// AutoSplit divides the open model until every piece fits the given build volume.
//
// It reuses the ordinary cut path rather than a separate one, so the part tree,
// the undo history and the watertightness reporting all behave exactly as they
// do for a manual cut — including that every cut it makes can be undone one at
// a time.
func (a *App) AutoSplit(bed cut.Bed, pins cut.PinSpec) (*AutoSplitOutcome, error) {
	out := &AutoSplitOutcome{}
	a.emit("cut:start")
	defer a.emit("cut:done")

	for {
		// Find a leaf that does not fit. One cut per pass, so each is recorded
		// as its own undoable step.
		var target string
		err := a.session.WithTree(func(tr *Tree) error {
			for _, leaf := range tr.Leaves() {
				box := stl.BBox{
					Min: geom.Vec3{leaf.Min[0], leaf.Min[1], leaf.Min[2]},
					Max: geom.Vec3{
						leaf.Min[0] + leaf.Size[0],
						leaf.Min[1] + leaf.Size[1],
						leaf.Min[2] + leaf.Size[2],
					},
				}
				if !bed.Fits(box) {
					target = leaf.ID
					return nil
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		if target == "" {
			break // everything fits
		}

		mesh, ok := a.session.MeshFor(target)
		if !ok {
			return nil, fmt.Errorf("part %q has no geometry", target)
		}

		steps, err := cut.PlanAutoSplit(mesh, bed)
		if err != nil {
			return nil, err
		}
		if len(steps) == 0 {
			break
		}

		res, err := a.cutPart(target, steps[0].Spec, pins, out)
		if err != nil {
			out.Warnings = append(out.Warnings, err.Error())
			break
		}
		if res == 0 {
			break // the cut changed nothing; stop rather than loop
		}
		out.CutsMade++

		if out.CutsMade > 512 {
			out.Warnings = append(out.Warnings,
				"stopped after 512 cuts; the bed may be too small for this model")
			break
		}
	}

	// Name anything still too large, rather than leaving the user to notice.
	_ = a.session.WithTree(func(tr *Tree) error {
		for _, leaf := range tr.Leaves() {
			box := stl.BBox{
				Min: geom.Vec3{leaf.Min[0], leaf.Min[1], leaf.Min[2]},
				Max: geom.Vec3{
					leaf.Min[0] + leaf.Size[0],
					leaf.Min[1] + leaf.Size[1],
					leaf.Min[2] + leaf.Size[2],
				},
			}
			if !bed.Fits(box) {
				out.StillTooBig = append(out.StillTooBig, leaf.Name)
			}
		}
		return nil
	})

	out.Tree = a.view()
	return out, nil
}
```

You will need a small `cutPart` helper that performs one cut on a named part with a given `Spec` and `PinSpec`, updating the tree — factor it out of `Cut` so both use the same path rather than duplicating it. Report how you structured that.

Add `"stl-cutter/internal/stl"` to `app.go`'s imports.

- [ ] **Step 4: Verify and regenerate bindings**

Run: `gofmt -l . && go vet ./... && go test ./... -race -count=1`, then `wails build -tags webkit2_41`. Commit the `frontend/wailsjs/` changes.

- [ ] **Step 5: Commit**

```bash
git add app.go app_test.go frontend/
git commit -m "feat: add AutoSplit, reusing the ordinary cut path"
```

---

### Task 10: The pin and bed panels

**Files:**
- Create: `frontend/pins.js`
- Modify: `frontend/index.html`, `frontend/style.css`, `frontend/ui.js`

**Interfaces:**
- Produces: `pins.js` exporting `initPins()`, `pinSpec()`, `bedSpec()`, `reportPins(outcome)`.

- [ ] **Step 1: Add the markup**

In `frontend/index.html`, inside `#sidebar` after `#plane-controls`:

```html
      <div id="pin-controls" hidden>
        <h2>Alignment pins</h2>
        <label><input id="pins-enabled" type="checkbox" /> Add pins to the cut</label>
        <label>Count <input id="pins-count" type="number" min="1" step="1" value="4" /></label>
        <label>Diameter mm <input id="pins-diameter" type="number" min="0.1" step="0.1" value="4" /></label>
        <label>Length mm <input id="pins-length" type="number" min="0.1" step="0.1" value="8" /></label>
        <label>Clearance mm <input id="pins-clearance" type="number" min="0" step="0.05" value="0.15" /></label>
        <label>Min wall mm <input id="pins-minwall" type="number" min="0" step="0.1" value="1" /></label>
        <label>Pegs on
          <select id="pins-pegside">
            <option value="2" selected>the cut-off piece</option>
            <option value="1">the piece left behind</option>
          </select>
        </label>
      </div>

      <div id="bed-controls" hidden>
        <h2>Fit to printer</h2>
        <label>Bed X mm <input id="bed-x" type="number" min="1" step="1" value="220" /></label>
        <label>Bed Y mm <input id="bed-y" type="number" min="1" step="1" value="220" /></label>
        <label>Bed Z mm <input id="bed-z" type="number" min="1" step="1" value="250" /></label>
        <button id="do-autosplit">Split to fit</button>
      </div>
```

Add to `style.css`:

```css
#sidebar select { width: 100%; padding: 4px; }
#sidebar label input[type="checkbox"] { width: auto; margin-right: 6px; }
```

- [ ] **Step 2: Write the panel module**

Create `frontend/pins.js`:

```js
// Reads the pin and bed panels. Kept apart from ui.js so the controller stays
// about wiring commands rather than parsing form fields.

let els = {};

export function initPins() {
  const id = (name) => document.getElementById(name);
  els = {
    enabled: id("pins-enabled"),
    count: id("pins-count"),
    diameter: id("pins-diameter"),
    length: id("pins-length"),
    clearance: id("pins-clearance"),
    minwall: id("pins-minwall"),
    pegside: id("pins-pegside"),
    bedX: id("bed-x"),
    bedY: id("bed-y"),
    bedZ: id("bed-z"),
    panel: id("pin-controls"),
    bedPanel: id("bed-controls"),
  };
}

// num reads a field, falling back to a default when it has been emptied mid-edit
// rather than sending NaN to Go, where it would be rejected with a less useful
// message than the field itself already conveys.
function num(el, fallback) {
  const v = parseFloat(el.value);
  return Number.isFinite(v) ? v : fallback;
}

export function pinSpec() {
  return {
    enabled: els.enabled.checked,
    count: Math.max(1, Math.round(num(els.count, 4))),
    diameter: num(els.diameter, 4),
    length: num(els.length, 8),
    clearance: num(els.clearance, 0.15),
    minWall: num(els.minwall, 1),
    pegOnPart: parseInt(els.pegside.value, 10) === 1 ? 1 : 2,
  };
}

export function bedSpec() {
  return {
    x: num(els.bedX, 220),
    y: num(els.bedY, 220),
    z: num(els.bedZ, 250),
  };
}

export function showPanels() {
  els.panel.hidden = false;
  els.bedPanel.hidden = false;
}

// reportPins turns a cut's pin outcome into messages. A skipped pin is always
// surfaced with the clearance actually measured: the user needs to know their
// pieces will not locate against each other, and why.
export function reportPins(outcome, message) {
  if (!outcome) return;
  if (outcome.pinsPlaced > 0) {
    message(`Placed ${outcome.pinsPlaced} alignment pin(s).`, "ok");
  }
  for (const s of outcome.pinsSkipped || []) {
    message(
      `Skipped a pin at (${s.x.toFixed(1)}, ${s.y.toFixed(1)}, ${s.z.toFixed(1)}): ` +
        `${s.reason} — ${s.measured.toFixed(2)}mm available, ${s.required.toFixed(2)}mm needed.`,
      "warn"
    );
  }
}
```

- [ ] **Step 3: Wire it into ui.js**

Import `initPins`, `pinSpec`, `bedSpec`, `showPanels`, `reportPins` from `./pins.js` and `AutoSplit` from `./wailsjs/go/main/App.js` — **with the `.js` extension**, or the module graph fails to load and the window goes blank.

Call `initPins()` alongside `initGizmo()`, and `showPanels()` where the other panels are revealed.

Pass the pin spec to `Cut`:

```js
    const outcome = await Cut(currentTree.selectedId, planeInput(), pinSpec());
    ...
    reportPins(outcome, message);
```

Wire the auto-split button:

```js
document.getElementById("do-autosplit").addEventListener("click", async () => {
  if (!currentTree) return;
  clearMessages();
  busy(true);
  try {
    const out = await AutoSplit(bedSpec(), pinSpec());
    currentTree = out.tree;
    await render(currentTree);

    if (out.cutsMade === 0) {
      message("Every piece already fits the bed. Nothing to do.", "ok");
    } else {
      message(`Split into ${out.cutsMade + 1} pieces.`, "ok");
    }
    for (const w of out.warnings || []) message(w, "warn");
    for (const name of out.stillTooBig || []) {
      message(`${name} still does not fit the bed.`, "warn");
    }
  } catch (err) {
    message(String(err), "err");
  } finally {
    busy(false);
  }
});
```

- [ ] **Step 4: Verify what can be verified**

You cannot open the window. Do not claim otherwise.

Run: `go vet ./... && go test ./... -count=1` — the four frontend guard tests check every relative import carries a `.js` extension and resolves, every import-map target exists, every bare specifier is covered, every vendored relative import resolves, every `getElementById` has a matching element, and every named import has a matching export. **A failure there is a real bug**, not a formality: two of those tests were added after bugs that blanked the entire window.

Then `node --check frontend/pins.js frontend/ui.js` and `wails build -tags webkit2_41`.

State in your report that the panels' appearance and behaviour are unverified and remain for a human.

- [ ] **Step 5: Commit**

```bash
git add frontend/
git commit -m "feat: add the pin settings and fit-to-printer panels"
```

---

### Task 11: The build matrix

**Files:**
- Create: `.github/workflows/build.yml`

- [ ] **Step 1: Write the workflow**

Wails v2 cannot cross-compile, so each platform builds on its own runner.

```yaml
name: build

on:
  push:
    tags: ["v*"]
  workflow_dispatch:

jobs:
  build:
    strategy:
      fail-fast: false
      matrix:
        include:
          - os: ubuntu-latest
            name: linux-amd64
            tags: webkit2_41
          - os: macos-latest
            name: macos-arm64
            tags: ""
          - os: windows-latest
            name: windows-amd64
            tags: ""

    runs-on: ${{ matrix.os }}

    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: "1.26"

      - name: Install Linux dependencies
        if: matrix.os == 'ubuntu-latest'
        run: |
          sudo apt-get update
          sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.1-dev

      - name: Install the Wails CLI
        run: go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0

      - name: Test
        run: go test ./... -race

      - name: Build
        run: wails build -tags "${{ matrix.tags }}" -o stl-cutter-${{ matrix.name }}

      - uses: actions/upload-artifact@v4
        with:
          name: stl-cutter-${{ matrix.name }}
          path: build/bin/*
```

**Two things to check rather than assume:**

- Ubuntu runners have carried `libwebkit2gtk-4.0-dev` historically and moved to 4.1. If `apt-get` cannot find `-4.1-dev`, fall back to `-4.0-dev` and drop the tag for that row. Whichever you settle on, say which the runner actually provided.
- Confirm `wails build -o` names the output as expected, and that an empty `-tags ""` is accepted rather than erroring. If it is not, split the build step per platform instead of parameterising the tag.

- [ ] **Step 2: Validate the workflow file**

You cannot run GitHub Actions from here. Do what you can:

```bash
python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/build.yml')); print('yaml ok')"
```

If `actionlint` is available, run it. Report which checks you managed.

State plainly that the workflow is **unexecuted** — it has never run on a real runner, and the first tag push is what will prove it.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/build.yml
git commit -m "ci: build for linux, macos and windows on tag"
```

---

### Task 12: Documentation and final verification

**Files:**
- Modify: `README.md`, `docs/manual-verification.md`

- [ ] **Step 1: Update the README**

Add to the feature list: alignment pins with configurable diameter, length, clearance and a minimum wall guard; and fit-to-printer auto-splitting.

Add a **Pins** section explaining the guard in plain terms — a pin is skipped when there is not at least the minimum wall of material around it in the plane of the cut, or behind it for the socket, and every skip is reported with the measurement that caused it.

Add the **Releases** section: builds are produced by pushing a `v*` tag, on Linux, macOS and Windows runners; macOS builds are unsigned and Gatekeeper will warn on first launch.

Update Known limitations with anything the pin work turned up.

- [ ] **Step 2: Extend the verification checklist**

Add a Pins section to `docs/manual-verification.md`:

```markdown
## Alignment pins

- [ ] Enable pins, cut a large model, and confirm the message reports how many were placed.
- [ ] Export both pieces and open them in a slicer: one has raised pegs on the cut
      face, the other matching sockets, and both are watertight.
- [ ] The pegs line up with the sockets — same count, same positions.
- [ ] Set the pin diameter larger than the cut face and cut. Every pin is skipped
      and the reason names the clearance that was available.
- [ ] Cut a thin-walled model with pins enabled. Pins are skipped for want of
      material behind the face, not placed and broken through.
- [ ] Set Min wall to 0.1 and repeat: more pins are now placed. That proves the
      guard is doing the work rather than the geometry refusing anyway.

## Fit to printer

- [ ] Enter a bed smaller than the model and click Split to fit. The parts list
      grows and every piece reports dimensions within the bed.
- [ ] Enter a bed larger than the model. Nothing happens and the message says so.
- [ ] Undo repeatedly after an auto-split. Every cut it made undoes one at a time.
- [ ] Enter a bed of 0. A readable error appears rather than a hang.
```

- [ ] **Step 3: Full verification**

```bash
gofmt -l . && go vet ./... && go test ./... -race -count=1
wails build -tags webkit2_41
go run ./cmd/genfixture -name u -out testdata/u.stl
go run ./cmd/cutdemo -in testdata/u.stl -out /tmp/u -origin 7.5,25,5 -normal 0,1,0 -width 15 -height 20
```

Expected: everything green, a binary in `build/bin/`, and the U still cutting into 7500 and 1500 with both parts watertight. **That last check is the regression net for the whole project** — if the geometry library drifted, it shows up here.

- [ ] **Step 4: Commit**

```bash
git add README.md docs/manual-verification.md
git commit -m "docs: document pins, auto-split and releases"
```

---

## Done When

- `gofmt -l .` silent, `go vet ./...` clean, `go test ./... -race` green.
- `wails build -tags webkit2_41` produces a binary.
- The U still cuts into 7500 and 1500, both watertight.
- A pinned cut leaves both parts watertight, with pegs on one and sockets on the other.
- Every skipped pin is reported with the clearance that caused it.
- Auto-splitting a model larger than the bed produces pieces that all fit, each undoable.

## What This Plan Does Not Cover

| Deferred | Why |
|---|---|
| Rotating parts to fit a bed diagonally | Auto-split is axis-aligned by design; rotating is a packing problem. |
| Cancelling a cut in progress | Progress is reported; cancellation needs a context through the cutter. |
| Signing and notarising macOS builds | Needs an Apple Developer account; out of scope. |
| Pins on the rectangle's side faces | Only the plane the user positioned is a mating surface. |
| Non-cylindrical pins | A cylinder is what a printer handles well and what the spec asks for. |

## Standing Lessons From Plans 1 and 2

Carried forward because they cost real time to learn:

- **A signed volume taken about the origin cannot see the winding of any face whose plane contains the origin.** Assert translation invariance, or check edge orientation with `meshcheck`, whenever winding matters.
- **`meshcheck.Check` is the load-bearing invariant.** Cutting and capping produce zero-area slivers, so its `Degenerate` counter is not a formality.
- **The frontend has no bundler.** Every relative import needs its `.js`; every bare specifier needs an import-map entry. Both classes blank the window while the Go build stays green, which is why `frontend_test.go` exists.
- **A test that passes against the broken behaviour proves nothing.** Every fix in Plans 1 and 2 was verified by deliberately reintroducing the bug and watching the test fail. Several tests that looked fine turned out not to discriminate at all.

---

## Post-Implementation Corrections

Defects in this plan's own code, found during execution. The committed code and
its tests are the artifact of record.

| Task | Finding | Resolution |
|---|---|---|
| 1 | `candidates()` clamped both ends of a ray into the same boundary cell for an origin far outside the grid, collapsing the search to one cell. A ray from `{-1000,8,0}` reported a miss where brute force found a hit at 994. The doc comment claimed it "never under-includes" — false. | Clip the ray to the grid's bounding box by the slab method before taking cells. `4fb45a8` |
| 1 | `nearestHit` returned the Möller–Trumbore parameter rather than a distance whenever the caller's direction was not unit length, silently yielding numbers that are not millimetres. | Normalise once inside `nearestHit`; fix the test's brute-force reference to match. `4fb45a8` |
