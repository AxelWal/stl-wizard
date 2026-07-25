# STL Cutter — Geometry Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A tested pure-Go library that reads an STL, splits it with a bounded cutting plane into two watertight parts, and writes them back out.

**Architecture:** Triangle-soup meshes. The cutter is the convex intersection of five half-spaces (the cut plane plus the four sides of a bounded rectangle). Each triangle is split against those half-spaces one at a time, accumulating inside fragments for one part and outside fragments for the other. Cut faces are then recovered by chaining the polygon edges that lie on each cutter plane into closed loops and triangulating them with holes.

**Tech Stack:** Go 1.26, standard library only. No CGO, no geometry dependencies, no Wails in this plan.

**Spec:** `docs/superpowers/specs/2026-07-25-stl-cutter-design.md`

## Global Constraints

- Module path is `stl-cutter`. Go 1.26.
- **Standard library only.** No third-party imports anywhere in this plan. No CGO.
- `internal/stl/` and `internal/cut/` must not import Wails, `net/http`, or anything UI-related. They are pure functions over meshes.
- All coordinates are `float64`. `Vec3` is `[3]float64`.
- Tolerance is always derived, never hardcoded at a call site: `eps = max(1e-9, 1e-7 * bboxDiagonal)`, from `(*Mesh).Epsilon()`.
- STL output is always binary. The 80-byte header must **not** begin with `solid`.
- Deliberate simplifications get a `// ponytail:` comment naming the ceiling and the upgrade path. The plan says where.
- Every task ends with a commit. Use `feat:`, `test:`, or `refactor:` prefixes.

**One deviation from the spec:** the spec names an *icosphere* as the curved
fixture; this plan uses a latitude/longitude `UVSphere` instead. It needs no
subdivision code, and tessellation quality is irrelevant here because the cut
invariants compare against the fixture mesh's own volume rather than an ideal
sphere's. Substitute an icosphere later if a fixture with uniform triangle sizes
turns out to matter.

---

## File Structure

| File | Responsibility |
|---|---|
| `go.mod` | Module declaration. No requires. |
| `internal/geom/vec.go` | `Vec3` and its arithmetic. Lexicographic `Less` for canonical edge ordering. |
| `internal/geom/weld.go` | `Welder` — maps near-coincident points to a canonical index via a spatial hash. Used by loop assembly and by watertightness checks. |
| `internal/stl/mesh.go` | `Tri`, `Mesh`, `BBox`, volume, epsilon. |
| `internal/stl/write.go` | Binary STL writing. |
| `internal/stl/read.go` | Binary and ASCII STL reading, format detection. |
| `internal/fixtures/fixtures.go` | Procedurally generated test meshes. No committed binaries. |
| `internal/meshcheck/meshcheck.go` | Invariant helpers: watertightness, edge balance. Used by every geometry test. |
| `internal/cut/plane.go` | `Plane`, `Side`, `Spec`, and the five-half-space `Cutter()`. |
| `internal/cut/clip.go` | `Polygon`, `splitPolygon`, canonical `intersect`, fan triangulation. |
| `internal/cut/loops.go` | Recovering cap boundary edges and chaining them into closed loops. |
| `internal/cut/face2d.go` | Plane basis projection, loop orientation, hole nesting and grouping. |
| `internal/cut/earclip.go` | Hole bridging and ear clipping. |
| `internal/cut/cap.go` | `triangulateFace` — wires face2d and earclip into 3D cap triangles. |
| `internal/cut/split.go` | `Split` — the whole bounded cut. |
| `cmd/cutdemo/main.go` | CLI proving the library end to end on a real file. |
| `cmd/genfixture/main.go` | Writes a fixture to an STL, so fixtures can be opened in a slicer. |

Split by responsibility, not layer: everything about 2D face handling lives in `face2d.go` and `earclip.go` and is never touched by `split.go`, which only knows about meshes and planes.

---

### Task 1: Module and vector arithmetic

**Files:**
- Create: `go.mod`
- Create: `internal/geom/vec.go`
- Test: `internal/geom/vec_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `geom.Vec3 [3]float64` with methods `Add(Vec3) Vec3`, `Sub(Vec3) Vec3`, `Scale(float64) Vec3`, `Dot(Vec3) float64`, `Cross(Vec3) Vec3`, `Len() float64`, `Unit() Vec3`, `Less(Vec3) bool`.

- [ ] **Step 1: Create the module**

```bash
cd /home/axel/source/stl-cutter
go mod init stl-cutter
```

Expect `go.mod` containing `module stl-cutter` and a `go 1.26` line.

- [ ] **Step 2: Write the failing test**

Create `internal/geom/vec_test.go`:

```go
package geom

import (
	"math"
	"testing"
)

func TestCrossIsRightHanded(t *testing.T) {
	x := Vec3{1, 0, 0}
	y := Vec3{0, 1, 0}
	got := x.Cross(y)
	want := Vec3{0, 0, 1}
	if got != want {
		t.Fatalf("x cross y = %v, want %v", got, want)
	}
}

func TestDotAndLen(t *testing.T) {
	a := Vec3{3, 4, 0}
	if got := a.Len(); got != 5 {
		t.Fatalf("Len = %v, want 5", got)
	}
	if got := a.Dot(Vec3{1, 0, 0}); got != 3 {
		t.Fatalf("Dot = %v, want 3", got)
	}
}

func TestUnitOfZeroVectorDoesNotProduceNaN(t *testing.T) {
	got := Vec3{0, 0, 0}.Unit()
	for i, c := range got {
		if math.IsNaN(c) {
			t.Fatalf("component %d is NaN, want 0", i)
		}
	}
}

// Less must be a strict total order — this is what makes edge intersection
// canonical, so two triangles sharing an edge compute the identical point.
func TestLessIsLexicographicAndAntisymmetric(t *testing.T) {
	a := Vec3{1, 2, 3}
	b := Vec3{1, 2, 4}
	if !a.Less(b) {
		t.Fatal("expected a < b on the third component")
	}
	if b.Less(a) {
		t.Fatal("Less is not antisymmetric")
	}
	if a.Less(a) {
		t.Fatal("Less must be strict — a < a is false")
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/geom/ -v`
Expected: FAIL — build error, `undefined: Vec3`.

- [ ] **Step 4: Write the implementation**

Create `internal/geom/vec.go`:

```go
// Package geom provides the vector primitives and point-welding used by the
// mesh and cutting packages.
package geom

import "math"

// Vec3 is a point or direction in 3D. Geometry uses float64 throughout;
// float32 loses too much precision in the repeated plane-distance arithmetic
// that clipping performs.
type Vec3 [3]float64

func (a Vec3) Add(b Vec3) Vec3 { return Vec3{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }
func (a Vec3) Sub(b Vec3) Vec3 { return Vec3{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }

func (a Vec3) Scale(s float64) Vec3 { return Vec3{a[0] * s, a[1] * s, a[2] * s} }

func (a Vec3) Dot(b Vec3) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }

func (a Vec3) Cross(b Vec3) Vec3 {
	return Vec3{
		a[1]*b[2] - a[2]*b[1],
		a[2]*b[0] - a[0]*b[2],
		a[0]*b[1] - a[1]*b[0],
	}
}

func (a Vec3) Len() float64 { return math.Sqrt(a.Dot(a)) }

// Unit returns a normalised copy. A zero vector is returned unchanged rather
// than becoming NaN, so a degenerate triangle cannot poison downstream maths.
func (a Vec3) Unit() Vec3 {
	l := a.Len()
	if l == 0 {
		return a
	}
	return a.Scale(1 / l)
}

// Less orders points lexicographically. Its only job is to give an edge a
// canonical direction: both triangles sharing an edge order its endpoints the
// same way, so both compute a bit-identical plane intersection point.
func (a Vec3) Less(b Vec3) bool {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/geom/ -v`
Expected: PASS, four tests.

- [ ] **Step 6: Commit**

```bash
git add go.mod internal/geom/vec.go internal/geom/vec_test.go
git commit -m "feat: add Vec3 arithmetic with canonical lexicographic ordering"
```

---

### Task 2: Point welding

Chaining cut edges into loops needs to know when two points are the same point. STL stores vertices per-triangle and real files often differ in the last bits for what should be one shared vertex, so equality has to be tolerance-based. A spatial hash over cells of side `eps`, probing the 27-cell neighbourhood, makes that exact and cheap.

**Files:**
- Create: `internal/geom/weld.go`
- Test: `internal/geom/weld_test.go`

**Interfaces:**
- Consumes: `geom.Vec3` from Task 1.
- Produces: `geom.NewWelder(eps float64) *Welder`; `(*Welder).ID(v Vec3) int` returning a canonical index, inserting on first sight; `(*Welder).Points() []Vec3`; `(*Welder).Len() int`.

- [ ] **Step 1: Write the failing test**

Create `internal/geom/weld_test.go`:

```go
package geom

import "testing"

func TestWelderReusesIndexForNearCoincidentPoints(t *testing.T) {
	w := NewWelder(1e-6)
	a := w.ID(Vec3{1, 2, 3})
	b := w.ID(Vec3{1 + 1e-9, 2, 3})
	if a != b {
		t.Fatalf("ids %d and %d differ; points within eps must weld", a, b)
	}
	if w.Len() != 1 {
		t.Fatalf("Len = %d, want 1", w.Len())
	}
}

func TestWelderSeparatesDistinctPoints(t *testing.T) {
	w := NewWelder(1e-6)
	a := w.ID(Vec3{0, 0, 0})
	b := w.ID(Vec3{1, 0, 0})
	if a == b {
		t.Fatal("distinct points must not weld")
	}
	if w.Len() != 2 {
		t.Fatalf("Len = %d, want 2", w.Len())
	}
}

// The failure mode this guards against: quantising to a grid of side eps puts
// two points closer than eps into different cells whenever they straddle a
// cell boundary. Probing neighbouring cells is what fixes it.
func TestWelderWeldsAcrossACellBoundary(t *testing.T) {
	const eps = 1e-6
	w := NewWelder(eps)
	// Sits exactly on a cell boundary; the partner is eps/4 the other side.
	a := w.ID(Vec3{eps * 3, 0, 0})
	b := w.ID(Vec3{eps*3 - eps/4, 0, 0})
	if a != b {
		t.Fatalf("ids %d and %d differ; points straddling a cell boundary must weld", a, b)
	}
}

func TestWelderPointsReturnsInsertionOrder(t *testing.T) {
	w := NewWelder(1e-6)
	w.ID(Vec3{5, 5, 5})
	w.ID(Vec3{1, 1, 1})
	pts := w.Points()
	if len(pts) != 2 || pts[0] != (Vec3{5, 5, 5}) || pts[1] != (Vec3{1, 1, 1}) {
		t.Fatalf("Points = %v, want insertion order", pts)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/geom/ -run TestWelder -v`
Expected: FAIL — `undefined: NewWelder`.

- [ ] **Step 3: Write the implementation**

Create `internal/geom/weld.go`:

```go
package geom

import "math"

// Welder maps near-coincident points onto a single canonical index. Cut-edge
// loop assembly relies on it: STL stores vertices per triangle, and real files
// routinely differ in the low bits for what is meant to be one shared vertex,
// so chaining edges by exact equality would leave loops open.
type Welder struct {
	eps  float64
	grid map[[3]int64][]int
	pts  []Vec3
}

func NewWelder(eps float64) *Welder {
	return &Welder{eps: eps, grid: make(map[[3]int64][]int)}
}

func (w *Welder) cell(v Vec3) [3]int64 {
	return [3]int64{
		int64(math.Floor(v[0] / w.eps)),
		int64(math.Floor(v[1] / w.eps)),
		int64(math.Floor(v[2] / w.eps)),
	}
}

// ID returns the canonical index for v, inserting it if no existing point lies
// within eps. Cells are eps on a side, so any point within eps of v must live
// in v's own cell or one of the 26 neighbours — hence the 3x3x3 probe.
func (w *Welder) ID(v Vec3) int {
	c := w.cell(v)
	eps2 := w.eps * w.eps
	for dx := int64(-1); dx <= 1; dx++ {
		for dy := int64(-1); dy <= 1; dy++ {
			for dz := int64(-1); dz <= 1; dz++ {
				key := [3]int64{c[0] + dx, c[1] + dy, c[2] + dz}
				for _, i := range w.grid[key] {
					d := w.pts[i].Sub(v)
					if d.Dot(d) <= eps2 {
						return i
					}
				}
			}
		}
	}
	id := len(w.pts)
	w.pts = append(w.pts, v)
	w.grid[c] = append(w.grid[c], id)
	return id
}

func (w *Welder) Points() []Vec3 { return w.pts }

func (w *Welder) Len() int { return len(w.pts) }
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/geom/ -v`
Expected: PASS, eight tests.

- [ ] **Step 5: Commit**

```bash
git add internal/geom/weld.go internal/geom/weld_test.go
git commit -m "feat: add tolerance-based point welder for loop assembly"
```

---

### Task 3: Mesh, bounding box, volume, epsilon

**Files:**
- Create: `internal/stl/mesh.go`
- Test: `internal/stl/mesh_test.go`

**Interfaces:**
- Consumes: `geom.Vec3`.
- Produces: `stl.Tri{A, B, C geom.Vec3}` with `Normal() geom.Vec3`, `Area() float64`, `Reversed() Tri`; `stl.Mesh{Tris []Tri}` with `BBox() BBox`, `Volume() float64`, `Epsilon() float64`; `stl.BBox{Min, Max geom.Vec3}` with `Size() geom.Vec3` and `Diagonal() float64`.

- [ ] **Step 1: Write the failing test**

Create `internal/stl/mesh_test.go`:

```go
package stl

import (
	"math"
	"testing"

	"stl-cutter/internal/geom"
)

// unitCube returns a 1x1x1 cube at the origin with outward-facing normals,
// written out explicitly so this test does not depend on the fixtures package.
func unitCube() *Mesh {
	v := [8]geom.Vec3{
		{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0},
		{0, 0, 1}, {1, 0, 1}, {1, 1, 1}, {0, 1, 1},
	}
	return &Mesh{Tris: []Tri{
		{v[0], v[2], v[1]}, {v[0], v[3], v[2]}, // bottom, -Z
		{v[4], v[5], v[6]}, {v[4], v[6], v[7]}, // top, +Z
		{v[0], v[1], v[5]}, {v[0], v[5], v[4]}, // front, -Y
		{v[3], v[6], v[2]}, {v[3], v[7], v[6]}, // back, +Y
		{v[0], v[7], v[3]}, {v[0], v[4], v[7]}, // left, -X
		{v[1], v[2], v[6]}, {v[1], v[6], v[5]}, // right, +X
	}}
}

func TestTriNormalAndArea(t *testing.T) {
	tr := Tri{geom.Vec3{0, 0, 0}, geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0}}
	if got := tr.Normal(); got != (geom.Vec3{0, 0, 1}) {
		t.Fatalf("Normal = %v, want +Z", got)
	}
	if got := tr.Area(); math.Abs(got-0.5) > 1e-12 {
		t.Fatalf("Area = %v, want 0.5", got)
	}
}

func TestTriReversedFlipsNormal(t *testing.T) {
	tr := Tri{geom.Vec3{0, 0, 0}, geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0}}
	if got := tr.Reversed().Normal(); got != (geom.Vec3{0, 0, -1}) {
		t.Fatalf("reversed Normal = %v, want -Z", got)
	}
}

func TestBBoxOfUnitCube(t *testing.T) {
	b := unitCube().BBox()
	if b.Min != (geom.Vec3{0, 0, 0}) || b.Max != (geom.Vec3{1, 1, 1}) {
		t.Fatalf("BBox = %v..%v, want 0,0,0..1,1,1", b.Min, b.Max)
	}
	if got := b.Diagonal(); math.Abs(got-math.Sqrt(3)) > 1e-12 {
		t.Fatalf("Diagonal = %v, want sqrt(3)", got)
	}
}

// Volume is the primary invariant every cut test leans on, so its sign
// convention must be pinned down here: outward normals give a positive volume.
func TestVolumeOfUnitCubeIsPositiveOne(t *testing.T) {
	if got := unitCube().Volume(); math.Abs(got-1.0) > 1e-12 {
		t.Fatalf("Volume = %v, want 1.0", got)
	}
}

func TestEpsilonScalesWithModelSize(t *testing.T) {
	small := unitCube()
	big := &Mesh{Tris: make([]Tri, len(small.Tris))}
	for i, tr := range small.Tris {
		big.Tris[i] = Tri{tr.A.Scale(1000), tr.B.Scale(1000), tr.C.Scale(1000)}
	}
	if big.Epsilon() <= small.Epsilon() {
		t.Fatalf("epsilon did not scale: small=%v big=%v", small.Epsilon(), big.Epsilon())
	}
}

func TestEpsilonHasAFloorForDegenerateMeshes(t *testing.T) {
	empty := &Mesh{}
	if got := empty.Epsilon(); got < 1e-9 {
		t.Fatalf("Epsilon = %v, want at least the 1e-9 floor", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/stl/ -v`
Expected: FAIL — `undefined: Mesh`, `undefined: Tri`.

- [ ] **Step 3: Write the implementation**

Create `internal/stl/mesh.go`:

```go
// Package stl reads and writes STL files and provides the triangle-soup mesh
// type the cutting package operates on.
package stl

import (
	"math"

	"stl-cutter/internal/geom"
)

// Tri is a single triangle. Winding is counter-clockwise seen from outside the
// solid, so Normal points outward.
type Tri struct {
	A, B, C geom.Vec3
}

func (t Tri) Normal() geom.Vec3 {
	return t.B.Sub(t.A).Cross(t.C.Sub(t.A)).Unit()
}

func (t Tri) Area() float64 {
	return t.B.Sub(t.A).Cross(t.C.Sub(t.A)).Len() / 2
}

// Reversed flips the winding, and therefore the normal. Caps use this: the same
// cap geometry belongs to both output parts, facing opposite ways.
func (t Tri) Reversed() Tri { return Tri{t.A, t.C, t.B} }

// Mesh is a triangle soup, exactly as STL stores it.
//
// ponytail: soup costs roughly 3x indexed storage, so a 2M-triangle model runs
// about 150MB per part-tree node. Upgrade path if memory becomes the binding
// constraint: an indexed mesh with a weld pass on load, changing this type and
// its iteration sites only.
type Mesh struct {
	Tris []Tri
}

type BBox struct {
	Min, Max geom.Vec3
}

func (b BBox) Size() geom.Vec3 { return b.Max.Sub(b.Min) }

func (b BBox) Diagonal() float64 { return b.Size().Len() }

func (m *Mesh) BBox() BBox {
	if len(m.Tris) == 0 {
		return BBox{}
	}
	lo := geom.Vec3{math.Inf(1), math.Inf(1), math.Inf(1)}
	hi := geom.Vec3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, t := range m.Tris {
		for _, v := range [3]geom.Vec3{t.A, t.B, t.C} {
			for i := 0; i < 3; i++ {
				lo[i] = math.Min(lo[i], v[i])
				hi[i] = math.Max(hi[i], v[i])
			}
		}
	}
	return BBox{Min: lo, Max: hi}
}

// Volume is the signed tetrahedron sum, which works directly on a soup and is
// positive for outward-wound triangles. Cut tests use it as their primary
// invariant: it catches inverted winding, missing caps and double-counted
// triangles all at once.
func (m *Mesh) Volume() float64 {
	var v float64
	for _, t := range m.Tris {
		v += t.A.Dot(t.B.Cross(t.C))
	}
	return v / 6
}

// Epsilon is the single tolerance every geometric predicate uses, scaled to the
// model so that a 1mm part and a 1m part behave the same way.
func (m *Mesh) Epsilon() float64 {
	return math.Max(1e-9, 1e-7*m.BBox().Diagonal())
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/stl/ -v`
Expected: PASS, six tests.

- [ ] **Step 5: Commit**

```bash
git add internal/stl/mesh.go internal/stl/mesh_test.go
git commit -m "feat: add mesh, bbox, volume and derived epsilon"
```

---

### Task 4: Binary STL write and read

Write comes first so the reader has something to round-trip against.

**Files:**
- Create: `internal/stl/write.go`
- Create: `internal/stl/read.go`
- Test: `internal/stl/binary_test.go`

**Interfaces:**
- Consumes: `stl.Mesh`, `stl.Tri`.
- Produces: `stl.Write(w io.Writer, m *Mesh) error`; `stl.WriteFile(path string, m *Mesh) error`; `stl.Read(r io.ReaderAt, size int64) (*Mesh, error)`; `stl.ReadFile(path string) (*Mesh, error)`; `stl.readBinary(r io.ReaderAt, size int64) (*Mesh, error)`.

- [ ] **Step 1: Write the failing test**

Create `internal/stl/binary_test.go`:

```go
package stl

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"stl-cutter/internal/geom"
)

func TestBinaryRoundTripPreservesTriangles(t *testing.T) {
	in := unitCube()
	var buf bytes.Buffer
	if err := Write(&buf, in); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out, err := Read(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(out.Tris) != len(in.Tris) {
		t.Fatalf("got %d triangles, want %d", len(out.Tris), len(in.Tris))
	}
	// STL stores float32, so compare with a tolerance rather than for equality.
	for i := range in.Tris {
		for j, pair := range [3][2]geom.Vec3{
			{in.Tris[i].A, out.Tris[i].A},
			{in.Tris[i].B, out.Tris[i].B},
			{in.Tris[i].C, out.Tris[i].C},
		} {
			if pair[0].Sub(pair[1]).Len() > 1e-5 {
				t.Fatalf("tri %d vertex %d: got %v, want %v", i, j, pair[1], pair[0])
			}
		}
	}
}

// A header beginning with "solid" would make naive parsers treat our binary
// output as ASCII.
func TestWrittenHeaderIsNotMistakableForAscii(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, unitCube()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if strings.HasPrefix(string(buf.Bytes()[:5]), "solid") {
		t.Fatal("binary header must not start with \"solid\"")
	}
}

// The real-world trap: plenty of binary STLs in the wild start with the word
// "solid". Detection must use the size arithmetic, not the leading token.
func TestDetectsBinaryEvenWhenHeaderSaysSolid(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, unitCube()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	raw := buf.Bytes()
	copy(raw[:5], "solid")

	out, err := Read(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(out.Tris) != 12 {
		t.Fatalf("got %d triangles, want 12", len(out.Tris))
	}
}

func TestReadRejectsEmptyTriangleCount(t *testing.T) {
	raw := make([]byte, 84)
	binary.LittleEndian.PutUint32(raw[80:], 0)
	if _, err := Read(bytes.NewReader(raw), int64(len(raw))); err == nil {
		t.Fatal("expected an error for a zero-triangle file")
	}
}

func TestReadRejectsTruncatedFile(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, unitCube()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	raw := buf.Bytes()[:buf.Len()-20] // lop off part of the last triangle
	if _, err := Read(bytes.NewReader(raw), int64(len(raw))); err == nil {
		t.Fatal("expected an error for a truncated file")
	}
}

func TestWrittenNormalsMatchWinding(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, unitCube()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	raw := buf.Bytes()
	// First triangle's normal sits at offset 84.
	var n [3]float32
	for i := 0; i < 3; i++ {
		bits := binary.LittleEndian.Uint32(raw[84+i*4:])
		n[i] = math.Float32frombits(bits)
	}
	want := unitCube().Tris[0].Normal()
	got := geom.Vec3{float64(n[0]), float64(n[1]), float64(n[2])}
	if got.Sub(want).Len() > 1e-5 {
		t.Fatalf("stored normal %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/stl/ -run 'Binary|Written|Detects|Read' -v`
Expected: FAIL — `undefined: Write`, `undefined: Read`.

- [ ] **Step 3: Write the writer**

Create `internal/stl/write.go`:

```go
package stl

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// binaryTriSize is the on-disk size of one binary STL triangle: a normal and
// three vertices as float32, plus a 2-byte attribute field.
const binaryTriSize = 50

// headerText deliberately avoids the leading token "solid" so that parsers
// which sniff the first five bytes do not misread our output as ASCII.
const headerText = "Binary STL generated by stl-cutter"

// Write emits m as a binary STL. Output is always binary; ASCII is read-only.
func Write(w io.Writer, m *Mesh) error {
	bw := bufio.NewWriter(w)

	var header [80]byte
	copy(header[:], headerText)
	if _, err := bw.Write(header[:]); err != nil {
		return err
	}
	if err := binary.Write(bw, binary.LittleEndian, uint32(len(m.Tris))); err != nil {
		return err
	}

	buf := make([]byte, binaryTriSize)
	for _, t := range m.Tris {
		n := t.Normal()
		vals := [12]float64{
			n[0], n[1], n[2],
			t.A[0], t.A[1], t.A[2],
			t.B[0], t.B[1], t.B[2],
			t.C[0], t.C[1], t.C[2],
		}
		for i, v := range vals {
			binary.LittleEndian.PutUint32(buf[i*4:], float32bits(v))
		}
		buf[48], buf[49] = 0, 0
		if _, err := bw.Write(buf); err != nil {
			return err
		}
	}
	return bw.Flush()
}

func WriteFile(path string, m *Mesh) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if err := Write(f, m); err != nil {
		f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	return f.Close()
}
```

- [ ] **Step 4: Write the reader**

Create `internal/stl/read.go`:

```go
package stl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	"stl-cutter/internal/geom"
)

func float32bits(v float64) uint32 { return math.Float32bits(float32(v)) }

// Read parses an STL from r. Binary and ASCII are distinguished by arithmetic,
// not by the leading token: a binary file's size is exactly 84 + 50*count, and
// binary files that happen to begin with "solid" are common enough that
// sniffing the token misclassifies them.
func Read(r io.ReaderAt, size int64) (*Mesh, error) {
	if size >= 84 {
		var countBuf [4]byte
		if _, err := r.ReadAt(countBuf[:], 80); err == nil {
			count := int64(binary.LittleEndian.Uint32(countBuf[:]))
			if 84+binaryTriSize*count == size {
				return readBinary(r, size)
			}
		}
	}
	return readASCII(r, size)
}

func ReadFile(path string) (*Mesh, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	m, err := Read(f, info.Size())
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return m, nil
}

func readBinary(r io.ReaderAt, size int64) (*Mesh, error) {
	var countBuf [4]byte
	if _, err := r.ReadAt(countBuf[:], 80); err != nil {
		return nil, fmt.Errorf("read triangle count: %w", err)
	}
	count := int(binary.LittleEndian.Uint32(countBuf[:]))
	if count == 0 {
		return nil, errors.New("STL contains no triangles")
	}
	if want := int64(84) + binaryTriSize*int64(count); want != size {
		return nil, fmt.Errorf("file is %d bytes, but its header declares %d triangles (%d bytes) — file is truncated or corrupt", size, count, want)
	}

	raw := make([]byte, binaryTriSize*count)
	if _, err := r.ReadAt(raw, 84); err != nil {
		return nil, fmt.Errorf("read triangle data: %w", err)
	}

	m := &Mesh{Tris: make([]Tri, count)}
	for i := 0; i < count; i++ {
		// Skip the stored normal at offset 0; it is advisory and frequently
		// wrong in files produced by other tools. Normal() recomputes it.
		base := i * binaryTriSize
		var v [9]float64
		for j := 0; j < 9; j++ {
			bits := binary.LittleEndian.Uint32(raw[base+12+j*4:])
			v[j] = float64(math.Float32frombits(bits))
		}
		m.Tris[i] = Tri{
			A: geom.Vec3{v[0], v[1], v[2]},
			B: geom.Vec3{v[3], v[4], v[5]},
			C: geom.Vec3{v[6], v[7], v[8]},
		}
	}
	return m, nil
}
```

- [ ] **Step 5: Add a temporary ASCII stub so the package compiles**

Append to `internal/stl/read.go`:

```go
// readASCII is implemented in Task 5.
func readASCII(r io.ReaderAt, size int64) (*Mesh, error) {
	return nil, errors.New("ASCII STL reading not implemented yet")
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/stl/ -v`
Expected: PASS. All binary tests green; nothing exercises `readASCII` yet.

- [ ] **Step 7: Commit**

```bash
git add internal/stl/write.go internal/stl/read.go internal/stl/binary_test.go
git commit -m "feat: add binary STL write, read and size-based format detection"
```

---

### Task 5: ASCII STL read

**Files:**
- Modify: `internal/stl/read.go` — replace the `readASCII` stub
- Test: `internal/stl/ascii_test.go`

**Interfaces:**
- Consumes: `stl.Mesh`, `stl.Tri`.
- Produces: a working `readASCII(r io.ReaderAt, size int64) (*Mesh, error)`.

- [ ] **Step 1: Write the failing test**

Create `internal/stl/ascii_test.go`:

```go
package stl

import (
	"bytes"
	"math"
	"strings"
	"testing"
)

const asciiTetra = `solid tetra
  facet normal 0 0 -1
    outer loop
      vertex 0 0 0
      vertex 0 1 0
      vertex 1 0 0
    endloop
  endfacet
  facet normal 0 -1 0
    outer loop
      vertex 0 0 0
      vertex 1 0 0
      vertex 0 0 1
    endloop
  endfacet
  facet normal -1 0 0
    outer loop
      vertex 0 0 0
      vertex 0 0 1
      vertex 0 1 0
    endloop
  endfacet
  facet normal 0.577 0.577 0.577
    outer loop
      vertex 1 0 0
      vertex 0 1 0
      vertex 0 0 1
    endloop
  endfacet
endsolid tetra
`

func TestReadASCIITetrahedron(t *testing.T) {
	r := strings.NewReader(asciiTetra)
	m, err := Read(r, int64(len(asciiTetra)))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(m.Tris) != 4 {
		t.Fatalf("got %d triangles, want 4", len(m.Tris))
	}
	// A corner tetrahedron of edge 1 has volume 1/6.
	if got := m.Volume(); math.Abs(got-1.0/6.0) > 1e-9 {
		t.Fatalf("Volume = %v, want %v", got, 1.0/6.0)
	}
}

func TestReadASCIIAcceptsScientificNotationAndTabs(t *testing.T) {
	src := "solid s\n\tfacet normal 0 0 1\n\t\touter loop\n" +
		"\t\t\tvertex 0 0 0\n\t\t\tvertex 1e0 0 0\n\t\t\tvertex 0 1.0E+00 0\n" +
		"\t\tendloop\n\tendfacet\nendsolid s\n"
	m, err := Read(strings.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(m.Tris) != 1 {
		t.Fatalf("got %d triangles, want 1", len(m.Tris))
	}
}

func TestReadASCIIRejectsVertexCountNotMultipleOfThree(t *testing.T) {
	src := "solid s\nfacet normal 0 0 1\nouter loop\nvertex 0 0 0\nvertex 1 0 0\nendloop\nendfacet\nendsolid s\n"
	if _, err := Read(strings.NewReader(src), int64(len(src))); err == nil {
		t.Fatal("expected an error for a facet with two vertices")
	}
}

func TestReadASCIIRejectsEmptySolid(t *testing.T) {
	src := "solid empty\nendsolid empty\n"
	if _, err := Read(strings.NewReader(src), int64(len(src))); err == nil {
		t.Fatal("expected an error for a solid with no facets")
	}
}

func TestReadASCIIRejectsMalformedVertex(t *testing.T) {
	src := "solid s\nvertex 0 0 zero\nendsolid s\n"
	if _, err := Read(strings.NewReader(src), int64(len(src))); err == nil {
		t.Fatal("expected an error for an unparseable coordinate")
	}
}

func TestReadRejectsCompleteGarbage(t *testing.T) {
	raw := []byte("this is not an STL file at all")
	if _, err := Read(bytes.NewReader(raw), int64(len(raw))); err == nil {
		t.Fatal("expected an error for non-STL input")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/stl/ -run ASCII -v`
Expected: FAIL — "ASCII STL reading not implemented yet".

- [ ] **Step 3: Replace the stub**

In `internal/stl/read.go`, delete the `readASCII` stub and add this in its place. Also add `"bufio"`, `"strconv"` and `"strings"` to the import block.

```go
// readASCII parses the ASCII STL grammar loosely: it collects every "vertex x y z"
// it finds and groups them in threes. Being permissive matters more than strict
// grammar checking here, because ASCII STL is written by a long tail of tools
// that disagree about whitespace and about which keywords are optional.
func readASCII(r io.ReaderAt, size int64) (*Mesh, error) {
	sr := io.NewSectionReader(r, 0, size)
	sc := bufio.NewScanner(sr)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var verts []geom.Vec3
	sawSolid := false
	line := 0

	for sc.Scan() {
		line++
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "solid":
			sawSolid = true
		case "vertex":
			if len(fields) < 4 {
				return nil, fmt.Errorf("line %d: vertex needs three coordinates, got %d", line, len(fields)-1)
			}
			var v geom.Vec3
			for i := 0; i < 3; i++ {
				f, err := strconv.ParseFloat(fields[i+1], 64)
				if err != nil {
					return nil, fmt.Errorf("line %d: coordinate %q is not a number", line, fields[i+1])
				}
				v[i] = f
			}
			verts = append(verts, v)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	if !sawSolid {
		return nil, errors.New("not an STL file: no binary header match and no \"solid\" keyword")
	}
	if len(verts) == 0 {
		return nil, errors.New("STL contains no triangles")
	}
	if len(verts)%3 != 0 {
		return nil, fmt.Errorf("STL has %d vertices, which is not a multiple of three", len(verts))
	}

	m := &Mesh{Tris: make([]Tri, 0, len(verts)/3)}
	for i := 0; i+2 < len(verts); i += 3 {
		m.Tris = append(m.Tris, Tri{A: verts[i], B: verts[i+1], C: verts[i+2]})
	}
	return m, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/stl/ -v`
Expected: PASS, all binary and ASCII tests.

- [ ] **Step 5: Commit**

```bash
git add internal/stl/read.go internal/stl/ascii_test.go
git commit -m "feat: add ASCII STL reading"
```

---

### Task 6: Test fixtures

Fixtures are generated in Go rather than committed as binary files, so they stay readable and reviewable. Every later geometry test draws on this package.

**Files:**
- Create: `internal/fixtures/fixtures.go`
- Test: `internal/fixtures/fixtures_test.go`

**Interfaces:**
- Consumes: `stl.Mesh`, `stl.Tri`, `geom.Vec3`.
- Produces: `fixtures.Box(min, max geom.Vec3) *stl.Mesh`; `fixtures.Cube(size float64) *stl.Mesh`; `fixtures.UVSphere(radius float64, segments, rings int) *stl.Mesh`; `fixtures.Tube(outerR, innerR, height float64, segments int) *stl.Mesh`; `fixtures.HollowBox(outer geom.Vec3, wall float64) *stl.Mesh`; `fixtures.UShape(depth float64) *stl.Mesh`; `fixtures.NonManifold() *stl.Mesh`; `fixtures.UShapeProfileArea` constant.

- [ ] **Step 1: Write the failing test**

Create `internal/fixtures/fixtures_test.go`:

```go
package fixtures

import (
	"math"
	"testing"

	"stl-cutter/internal/geom"
)

func TestCubeVolumeAndTriangleCount(t *testing.T) {
	m := Cube(2)
	if len(m.Tris) != 12 {
		t.Fatalf("got %d triangles, want 12", len(m.Tris))
	}
	if got := m.Volume(); math.Abs(got-8) > 1e-9 {
		t.Fatalf("Volume = %v, want 8", got)
	}
}

func TestBoxRespectsBounds(t *testing.T) {
	m := Box(geom.Vec3{-1, -2, -3}, geom.Vec3{1, 2, 3})
	b := m.BBox()
	if b.Min != (geom.Vec3{-1, -2, -3}) || b.Max != (geom.Vec3{1, 2, 3}) {
		t.Fatalf("BBox = %v..%v, want -1,-2,-3..1,2,3", b.Min, b.Max)
	}
	if got := m.Volume(); math.Abs(got-48) > 1e-9 {
		t.Fatalf("Volume = %v, want 48", got)
	}
}

// A tessellated sphere under-approximates the ideal one, so this checks the
// order of magnitude rather than the exact figure. Cut tests compare against
// the mesh's own volume, so tessellation error never matters there.
func TestUVSphereVolumeIsCloseToIdeal(t *testing.T) {
	m := UVSphere(1, 32, 16)
	ideal := 4.0 / 3.0 * math.Pi
	got := m.Volume()
	if got <= 0 {
		t.Fatalf("Volume = %v, want positive (winding is inverted)", got)
	}
	if math.Abs(got-ideal)/ideal > 0.02 {
		t.Fatalf("Volume = %v, want within 2%% of %v", got, ideal)
	}
}

func TestTubeVolumeMatchesAnnulus(t *testing.T) {
	m := Tube(2, 1, 5, 64)
	ideal := math.Pi * (4 - 1) * 5
	got := m.Volume()
	if got <= 0 {
		t.Fatalf("Volume = %v, want positive", got)
	}
	if math.Abs(got-ideal)/ideal > 0.02 {
		t.Fatalf("Volume = %v, want within 2%% of %v", got, ideal)
	}
}

func TestHollowBoxVolumeIsShellOnly(t *testing.T) {
	m := HollowBox(geom.Vec3{10, 10, 10}, 1)
	want := 1000.0 - 512.0 // 10^3 minus 8^3
	if got := m.Volume(); math.Abs(got-want) > 1e-9 {
		t.Fatalf("Volume = %v, want %v", got, want)
	}
}

func TestUShapeVolumeIsProfileTimesDepth(t *testing.T) {
	m := UShape(10)
	want := UShapeProfileArea * 10
	if got := m.Volume(); math.Abs(got-want) > 1e-9 {
		t.Fatalf("Volume = %v, want %v", got, want)
	}
}

// The U is the motivating case for the whole project: a bounded plane must be
// able to cut one arm without touching the other. Pin down its geometry so the
// cut tests can rely on exact coordinates.
func TestUShapeGeometryIsAsDocumented(t *testing.T) {
	b := UShape(10).BBox()
	if b.Min != (geom.Vec3{0, 0, 0}) || b.Max != (geom.Vec3{30, 40, 10}) {
		t.Fatalf("BBox = %v..%v, want 0,0,0..30,40,10", b.Min, b.Max)
	}
}

func TestNonManifoldHasAHole(t *testing.T) {
	if got := len(NonManifold().Tris); got != 11 {
		t.Fatalf("got %d triangles, want 11 (a cube with one face triangle removed)", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/fixtures/ -v`
Expected: FAIL — `undefined: Cube`.

- [ ] **Step 3: Write the implementation**

Create `internal/fixtures/fixtures.go`:

```go
// Package fixtures generates the test meshes used across the geometry tests.
// Meshes are built procedurally rather than committed as binary STL files so
// that they stay readable and reviewable in diffs.
package fixtures

import (
	"math"

	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

// prism extrudes a counter-clockwise 2D profile along +Z from z0 to z1,
// producing a closed solid with outward normals. capTris must triangulate the
// profile counter-clockwise, indexing into profile.
func prism(profile [][2]float64, capTris [][3]int, z0, z1 float64) *stl.Mesh {
	at := func(i int, z float64) geom.Vec3 {
		return geom.Vec3{profile[i][0], profile[i][1], z}
	}
	m := &stl.Mesh{}

	for _, tr := range capTris {
		// Top cap faces +Z with the profile's own winding; the bottom cap is
		// the same triangle reversed so it faces -Z.
		top := stl.Tri{A: at(tr[0], z1), B: at(tr[1], z1), C: at(tr[2], z1)}
		m.Tris = append(m.Tris, top, stl.Tri{A: at(tr[0], z0), B: at(tr[2], z0), C: at(tr[1], z0)})
	}

	// Side walls. For a counter-clockwise profile the outward normal of edge
	// i->j is (dy, -dx, 0), which this winding produces.
	for i := range profile {
		j := (i + 1) % len(profile)
		a0, b0 := at(i, z0), at(j, z0)
		a1, b1 := at(i, z1), at(j, z1)
		m.Tris = append(m.Tris,
			stl.Tri{A: a0, B: b0, C: b1},
			stl.Tri{A: a0, B: b1, C: a1},
		)
	}
	return m
}

// reversed flips every triangle, turning a solid into a cavity. HollowBox uses
// it for the inner void.
func reversed(m *stl.Mesh) *stl.Mesh {
	out := &stl.Mesh{Tris: make([]stl.Tri, len(m.Tris))}
	for i, t := range m.Tris {
		out.Tris[i] = t.Reversed()
	}
	return out
}

func Box(min, max geom.Vec3) *stl.Mesh {
	profile := [][2]float64{
		{min[0], min[1]}, {max[0], min[1]}, {max[0], max[1]}, {min[0], max[1]},
	}
	return prism(profile, [][3]int{{0, 1, 2}, {0, 2, 3}}, min[2], max[2])
}

func Cube(size float64) *stl.Mesh {
	return Box(geom.Vec3{}, geom.Vec3{size, size, size})
}

// UVSphere is a latitude/longitude tessellated sphere centred on the origin.
// It is the curved fixture: a cut through it produces many straddling
// triangles and a curved cap boundary.
func UVSphere(radius float64, segments, rings int) *stl.Mesh {
	at := func(seg, ring int) geom.Vec3 {
		phi := math.Pi * float64(ring) / float64(rings)         // 0 at +Z pole
		theta := 2 * math.Pi * float64(seg) / float64(segments) // around Z
		s := math.Sin(phi)
		return geom.Vec3{
			radius * s * math.Cos(theta),
			radius * s * math.Sin(theta),
			radius * math.Cos(phi),
		}
	}
	m := &stl.Mesh{}
	for ring := 0; ring < rings; ring++ {
		for seg := 0; seg < segments; seg++ {
			a := at(seg, ring)
			b := at(seg+1, ring)
			c := at(seg+1, ring+1)
			d := at(seg, ring+1)
			switch {
			case ring == 0: // +Z pole: a and b coincide, emit one triangle
				m.Tris = append(m.Tris, stl.Tri{A: a, B: c, C: d})
			case ring == rings-1: // -Z pole: c and d coincide
				m.Tris = append(m.Tris, stl.Tri{A: a, B: b, C: c})
			default:
				m.Tris = append(m.Tris,
					stl.Tri{A: a, B: b, C: c},
					stl.Tri{A: a, B: c, C: d},
				)
			}
		}
	}
	return m
}

// Tube is a hollow cylinder along Z, sitting on z=0. Cutting it produces an
// annular cap, which is what exercises hole nesting in the triangulator.
func Tube(outerR, innerR, height float64, segments int) *stl.Mesh {
	ring := func(r float64, i int, z float64) geom.Vec3 {
		th := 2 * math.Pi * float64(i) / float64(segments)
		return geom.Vec3{r * math.Cos(th), r * math.Sin(th), z}
	}
	m := &stl.Mesh{}
	for i := 0; i < segments; i++ {
		j := i + 1
		o0, o1 := ring(outerR, i, 0), ring(outerR, j, 0)
		o2, o3 := ring(outerR, j, height), ring(outerR, i, height)
		i0, i1 := ring(innerR, i, 0), ring(innerR, j, 0)
		i2, i3 := ring(innerR, j, height), ring(innerR, i, height)

		// Outer wall faces away from the axis.
		m.Tris = append(m.Tris, stl.Tri{A: o0, B: o1, C: o2}, stl.Tri{A: o0, B: o2, C: o3})
		// Inner wall faces toward the axis, so its winding is the reverse.
		m.Tris = append(m.Tris, stl.Tri{A: i0, B: i2, C: i1}, stl.Tri{A: i0, B: i3, C: i2})
		// Bottom annulus faces -Z, top annulus faces +Z.
		m.Tris = append(m.Tris, stl.Tri{A: o0, B: i1, C: i0}, stl.Tri{A: o0, B: o1, C: i1})
		m.Tris = append(m.Tris, stl.Tri{A: o3, B: i3, C: i2}, stl.Tri{A: o3, B: i2, C: o2})
	}
	return m
}

// HollowBox is a closed shell: an outer box with an inner void. It is the
// fixture for the pin wall-thickness check, where a socket must be refused for
// want of material.
func HollowBox(outer geom.Vec3, wall float64) *stl.Mesh {
	out := Box(geom.Vec3{}, outer)
	in := reversed(Box(
		geom.Vec3{wall, wall, wall},
		geom.Vec3{outer[0] - wall, outer[1] - wall, outer[2] - wall},
	))
	out.Tris = append(out.Tris, in.Tris...)
	return out
}

// UShapeProfileArea is the area of the U cross-section: a 30x40 rectangle less
// the 10x30 notch between the arms. Typed float64 so tests can multiply it by a
// depth without converting.
const UShapeProfileArea float64 = 30*40 - 10*30

// UShape is the letter U extruded along Z, and the motivating fixture for the
// whole project. A plane at y=25 bounded to x in [0,15] must cut the left arm
// and leave the right arm untouched and still attached to the base.
//
//	     y=40  ┌──┐      ┌──┐
//	           │  │      │  │
//	     y=10  │  └──────┘  │
//	      y=0  └────────────┘
//	          x=0  10    20  30
func UShape(depth float64) *stl.Mesh {
	profile := [][2]float64{
		{0, 0}, {30, 0}, {30, 40}, {20, 40}, {20, 10}, {10, 10}, {10, 40}, {0, 40},
	}
	// Hand-decomposed as two fans, because the U is star-shaped from neither
	// vertex alone: a fan from vertex 0 over the chain 1-4-5-6-7, and a fan from
	// vertex 1 over the chain 2-3-4. The two fans meet along edge 1-4 and their
	// areas sum to UShapeProfileArea, which the fixture test verifies.
	capTris := [][3]int{
		{0, 1, 4}, {0, 4, 5}, {0, 5, 6}, {0, 6, 7}, // fan from vertex 0
		{1, 2, 3}, {1, 3, 4},                       // fan from vertex 1
	}
	return prism(profile, capTris, 0, depth)
}

// NonManifold is a cube with one triangle removed, so one cap loop cannot
// close. It drives the open-loop error path.
func NonManifold() *stl.Mesh {
	m := Cube(10)
	m.Tris = m.Tris[:len(m.Tris)-1]
	return m
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/fixtures/ -v`
Expected: PASS, eight tests.

If `TestUShapeVolumeIsProfileTimesDepth` fails with a negative or wrong volume, the `capTris` decomposition is not counter-clockwise — check the triangle index order against the profile listing rather than adjusting the tolerance.

- [ ] **Step 5: Commit**

```bash
git add internal/fixtures/
git commit -m "test: add procedurally generated mesh fixtures"
```

---

### Task 7: Invariant helpers

The three properties from the spec need to be checkable in one line from any test. Watertightness is the fiddly one: it means every undirected edge is shared by exactly two triangles **with opposite orientation**, which needs welded vertices to detect reliably.

**Files:**
- Create: `internal/meshcheck/meshcheck.go`
- Test: `internal/meshcheck/meshcheck_test.go`

**Interfaces:**
- Consumes: `stl.Mesh`, `geom.Welder`.
- Produces: `meshcheck.Report{OpenEdges, Misoriented int}` with `OK() bool` and `String() string`; `meshcheck.Check(m *stl.Mesh, eps float64) Report`; `meshcheck.IsWatertight(m *stl.Mesh, eps float64) bool`.

- [ ] **Step 1: Write the failing test**

Create `internal/meshcheck/meshcheck_test.go`:

```go
package meshcheck

import (
	"testing"

	"stl-cutter/internal/fixtures"
	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

func TestClosedSolidsAreWatertight(t *testing.T) {
	for name, m := range map[string]*stl.Mesh{
		"cube":      fixtures.Cube(10),
		"sphere":    fixtures.UVSphere(5, 24, 12),
		"tube":      fixtures.Tube(4, 2, 8, 32),
		"hollowbox": fixtures.HollowBox(geom.Vec3{10, 10, 10}, 1),
		"ushape":    fixtures.UShape(10),
	} {
		if rep := Check(m, m.Epsilon()); !rep.OK() {
			t.Errorf("%s: %s", name, rep)
		}
	}
}

func TestNonManifoldIsDetected(t *testing.T) {
	m := fixtures.NonManifold()
	rep := Check(m, m.Epsilon())
	if rep.OK() {
		t.Fatal("expected the holed cube to fail the watertightness check")
	}
	if rep.OpenEdges == 0 {
		t.Fatal("OpenEdges = 0, want the three edges of the missing triangle")
	}
}

func TestMisorientedTriangleIsDetected(t *testing.T) {
	m := fixtures.Cube(10)
	m.Tris[0] = m.Tris[0].Reversed()
	rep := Check(m, m.Epsilon())
	if rep.OK() {
		t.Fatal("expected a flipped triangle to fail the check")
	}
	if rep.Misoriented == 0 {
		t.Fatal("Misoriented = 0, want the flipped triangle's edges")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/meshcheck/ -v`
Expected: FAIL — `undefined: Check`.

- [ ] **Step 3: Write the implementation**

Create `internal/meshcheck/meshcheck.go`:

```go
// Package meshcheck provides the mesh invariants the geometry tests assert on.
// It lives outside internal/cut so that cut's tests and any future tooling can
// share one definition of "watertight".
package meshcheck

import (
	"fmt"

	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

// Report summarises how far a mesh is from being a closed, consistently wound
// solid.
type Report struct {
	// OpenEdges counts undirected edges not shared by exactly two triangles.
	OpenEdges int
	// Misoriented counts edges shared by two triangles that traverse them in
	// the same direction, meaning one of the two is wound backwards.
	Misoriented int
}

func (r Report) OK() bool { return r.OpenEdges == 0 && r.Misoriented == 0 }

func (r Report) String() string {
	if r.OK() {
		return "watertight"
	}
	return fmt.Sprintf("not watertight: %d open edges, %d misoriented edges", r.OpenEdges, r.Misoriented)
}

// Check welds vertices within eps and then tallies edge usage. Welding is
// required: STL stores vertices per triangle, so exact comparison would report
// every edge of a perfectly good mesh as open.
func Check(m *stl.Mesh, eps float64) Report {
	w := geom.NewWelder(eps)

	// For each undirected edge, count traversals in each direction.
	type tally struct{ forward, backward int }
	edges := make(map[[2]int]*tally, len(m.Tris)*3)

	for _, t := range m.Tris {
		ids := [3]int{w.ID(t.A), w.ID(t.B), w.ID(t.C)}
		for i := 0; i < 3; i++ {
			a, b := ids[i], ids[(i+1)%3]
			if a == b {
				continue // degenerate edge of a zero-area triangle
			}
			key := [2]int{a, b}
			forward := true
			if a > b {
				key = [2]int{b, a}
				forward = false
			}
			e := edges[key]
			if e == nil {
				e = &tally{}
				edges[key] = e
			}
			if forward {
				e.forward++
			} else {
				e.backward++
			}
		}
	}

	var rep Report
	for _, e := range edges {
		total := e.forward + e.backward
		switch {
		case total != 2:
			rep.OpenEdges++
		case e.forward != 1 || e.backward != 1:
			rep.Misoriented++
		}
	}
	return rep
}

func IsWatertight(m *stl.Mesh, eps float64) bool { return Check(m, eps).OK() }
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/meshcheck/ -v`
Expected: PASS, three tests.

If `TestClosedSolidsAreWatertight` reports open edges on `sphere`, the pole triangles in `UVSphere` are degenerate — confirm the `ring == 0` and `ring == rings-1` branches in Task 6 emit a single triangle each rather than a quad.

- [ ] **Step 5: Commit**

```bash
git add internal/meshcheck/
git commit -m "test: add watertightness and edge-orientation invariant helpers"
```

---

### Task 8: Planes and the five-half-space cutter

This is where the bounded-plane requirement becomes concrete: a `Spec` from the UI turns into five half-spaces whose intersection is the region that gets cut away.

**Files:**
- Create: `internal/cut/plane.go`
- Test: `internal/cut/plane_test.go`

**Interfaces:**
- Consumes: `geom.Vec3`.
- Produces: `cut.Plane{N geom.Vec3; D float64}` with `Dist(geom.Vec3) float64` and `Classify(geom.Vec3, float64) Side`; `cut.Side` with constants `Outside`, `On`, `Inside`; `cut.Spec{Origin, Normal, U, V geom.Vec3; Width, Height float64}` with `Validate() error`, `Planes() []Plane` (element 0 is the user's cut plane), `Basis() (u, v geom.Vec3)`; `cut.SpecFromNormal(origin, normal geom.Vec3, width, height float64) Spec`.

- [ ] **Step 1: Write the failing test**

Create `internal/cut/plane_test.go`:

```go
package cut

import (
	"math"
	"testing"

	"stl-cutter/internal/geom"
)

func TestPlaneClassifyRespectsEpsilon(t *testing.T) {
	p := Plane{N: geom.Vec3{0, 0, 1}, D: 0}
	const eps = 1e-6

	if got := p.Classify(geom.Vec3{0, 0, 1}, eps); got != Inside {
		t.Errorf("above plane: got %v, want Inside", got)
	}
	if got := p.Classify(geom.Vec3{0, 0, -1}, eps); got != Outside {
		t.Errorf("below plane: got %v, want Outside", got)
	}
	if got := p.Classify(geom.Vec3{0, 0, eps / 2}, eps); got != On {
		t.Errorf("within eps: got %v, want On", got)
	}
}

// The five planes must all point inward, so that "inside every one of them"
// means "inside the cutter".
func TestPlanesAllPointInwardAtTheOrigin(t *testing.T) {
	s := SpecFromNormal(geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, 1}, 4, 4)
	// A point just above the cut plane and centred in the rectangle is inside
	// all five half-spaces.
	probe := geom.Vec3{5, 5, 5.5}
	for i, p := range s.Planes() {
		if p.Dist(probe) < 0 {
			t.Errorf("plane %d: probe is outside; planes must point inward", i)
		}
	}
}

func TestPlanesBoundTheRectangle(t *testing.T) {
	s := SpecFromNormal(geom.Vec3{0, 0, 0}, geom.Vec3{0, 0, 1}, 10, 10)
	planes := s.Planes()

	inside := func(v geom.Vec3) bool {
		for _, p := range planes {
			if p.Dist(v) < 0 {
				return false
			}
		}
		return true
	}

	if !inside(geom.Vec3{4, 4, 1}) {
		t.Error("point within the rectangle and above the plane should be inside")
	}
	if inside(geom.Vec3{6, 0, 1}) {
		t.Error("point beyond the rectangle's half-width should be outside")
	}
	if inside(geom.Vec3{0, 6, 1}) {
		t.Error("point beyond the rectangle's half-height should be outside")
	}
	if inside(geom.Vec3{0, 0, -1}) {
		t.Error("point below the cut plane should be outside")
	}
}

func TestValidateRejectsBadSpecs(t *testing.T) {
	good := SpecFromNormal(geom.Vec3{}, geom.Vec3{0, 0, 1}, 10, 10)
	if err := good.Validate(); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}

	cases := map[string]Spec{
		"zero normal":    {Normal: geom.Vec3{}, U: geom.Vec3{1, 0, 0}, V: geom.Vec3{0, 1, 0}, Width: 1, Height: 1},
		"zero width":     SpecFromNormal(geom.Vec3{}, geom.Vec3{0, 0, 1}, 0, 10),
		"negative height": SpecFromNormal(geom.Vec3{}, geom.Vec3{0, 0, 1}, 10, -1),
		"non-orthogonal basis": {
			Normal: geom.Vec3{0, 0, 1}, U: geom.Vec3{1, 0, 0}, V: geom.Vec3{1, 0, 0},
			Width: 1, Height: 1,
		},
	}
	for name, s := range cases {
		if err := s.Validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// Basis must be right-handed with u x v == Normal, because cap triangulation
// relies on a counter-clockwise 2D winding mapping to a +Normal facing triangle.
func TestBasisIsRightHanded(t *testing.T) {
	for _, n := range []geom.Vec3{
		{0, 0, 1}, {1, 0, 0}, {0, 1, 0}, {1, 1, 1},
	} {
		s := SpecFromNormal(geom.Vec3{}, n, 1, 1)
		u, v := s.Basis()
		want := n.Unit()
		if got := u.Cross(v); got.Sub(want).Len() > 1e-12 {
			t.Errorf("normal %v: u cross v = %v, want %v", n, got, want)
		}
		if math.Abs(u.Len()-1) > 1e-12 || math.Abs(v.Len()-1) > 1e-12 {
			t.Errorf("normal %v: basis vectors must be unit length", n)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cut/ -v`
Expected: FAIL — `undefined: Plane`.

- [ ] **Step 3: Write the implementation**

Create `internal/cut/plane.go`:

```go
// Package cut splits a mesh with a bounded cutting plane.
//
// The cutter is the convex intersection of five half-spaces: the cut plane
// itself, plus the four sides of a bounded rectangle. Bounding the plane is
// what lets a single cut take the top off one arm of a U-shaped model without
// touching the other arm, which an unbounded plane at the same height would
// also slice through.
package cut

import (
	"errors"
	"fmt"
	"math"

	"stl-cutter/internal/geom"
)

// Plane is a half-space. Points with N·p - D >= 0 are inside it.
type Plane struct {
	N geom.Vec3
	D float64
}

func (p Plane) Dist(v geom.Vec3) float64 { return p.N.Dot(v) - p.D }

// Side is the position of a point relative to a plane, with a tolerance band.
type Side int

const (
	Outside Side = -1
	On      Side = 0
	Inside  Side = 1
)

// Classify treats points within eps of the plane as exactly on it. That band is
// what stops clipping from emitting sliver triangles.
func (p Plane) Classify(v geom.Vec3, eps float64) Side {
	d := p.Dist(v)
	switch {
	case d > eps:
		return Inside
	case d < -eps:
		return Outside
	default:
		return On
	}
}

// Spec is a bounded cutting plane as the UI describes it: a plane through
// Origin with the given Normal, restricted to a Width x Height rectangle
// centred on Origin and spanned by U and V.
//
// The half-space the Normal points into is the region that becomes part 2.
type Spec struct {
	Origin geom.Vec3
	Normal geom.Vec3
	U, V   geom.Vec3
	Width  float64
	Height float64
}

// SpecFromNormal builds a Spec with an arbitrary but right-handed in-plane
// basis. Used by tests and by auto-split, where the in-plane orientation is
// irrelevant because the rectangle spans the whole cross-section anyway.
func SpecFromNormal(origin, normal geom.Vec3, width, height float64) Spec {
	n := normal.Unit()
	// Pick the least-aligned cardinal axis so the cross product stays well
	// conditioned.
	seed := geom.Vec3{1, 0, 0}
	if math.Abs(n[0]) > math.Abs(n[1]) {
		seed = geom.Vec3{0, 1, 0}
	}
	u := seed.Sub(n.Scale(seed.Dot(n))).Unit()
	v := n.Cross(u)
	return Spec{Origin: origin, Normal: n, U: u, V: v, Width: width, Height: height}
}

// Basis returns an orthonormal right-handed in-plane basis with u x v == Normal.
// Cap triangulation depends on that identity: a counter-clockwise 2D triangle in
// (u, v) then maps to a triangle whose normal is +Normal.
func (s Spec) Basis() (u, v geom.Vec3) {
	n := s.Normal.Unit()
	u = s.U.Sub(n.Scale(s.U.Dot(n))).Unit()
	return u, n.Cross(u)
}

func (s Spec) Validate() error {
	if s.Normal.Len() == 0 {
		return errors.New("cutting plane has a zero normal")
	}
	if s.Width <= 0 || s.Height <= 0 {
		return fmt.Errorf("cutting rectangle must have positive extent, got %gx%g", s.Width, s.Height)
	}
	if s.U.Len() == 0 || s.V.Len() == 0 {
		return errors.New("cutting plane has a degenerate in-plane basis")
	}
	// U and V must span the plane. Requiring them merely non-parallel is enough;
	// Basis re-orthogonalises.
	if s.U.Unit().Cross(s.V.Unit()).Len() < 1e-6 {
		return errors.New("cutting plane basis vectors are parallel")
	}
	return nil
}

// Planes returns the five inward-facing half-spaces whose intersection is the
// cutter. Order is the cut plane first, then the four rectangle sides; nothing
// depends on that order, because cap recovery happens after all splitting.
func (s Spec) Planes() []Plane {
	n := s.Normal.Unit()
	u, v := s.Basis()
	cu, cv := u.Dot(s.Origin), v.Dot(s.Origin)
	hw, hh := s.Width/2, s.Height/2

	return []Plane{
		{N: n, D: n.Dot(s.Origin)},           // keep the side the normal points into
		{N: u, D: cu - hw},                   // u >= centre - half width
		{N: u.Scale(-1), D: -(cu + hw)},      // u <= centre + half width
		{N: v, D: cv - hh},                   // v >= centre - half height
		{N: v.Scale(-1), D: -(cv + hh)},      // v <= centre + half height
	}
}
```

`Planes()[0]` is the plane the user positioned. Plan 3's pin placement needs that
distinction — pins go on the cut plane's caps only, never on the four rectangle
sides — so the ordering is worth relying on even though nothing in Plan 1 does.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/cut/ -v`
Expected: PASS, five tests.

- [ ] **Step 5: Commit**

```bash
git add internal/cut/plane.go internal/cut/plane_test.go
git commit -m "feat: add planes and the five-half-space bounded cutter"
```

---

### Task 9: Polygon splitting

Plain Sutherland–Hodgman returns only the inside piece, but both sides are needed — one for each output part. So the primitive here **splits** rather than clips. The other essential property is that the intersection point is computed canonically, so two triangles sharing an edge produce a bit-identical point and cap loops chain up cleanly.

**Files:**
- Create: `internal/cut/clip.go`
- Test: `internal/cut/clip_test.go`

**Interfaces:**
- Consumes: `cut.Plane`, `cut.Side`, `geom.Vec3`, `stl.Tri`.
- Produces: `cut.Polygon []geom.Vec3`; `cut.splitPolygon(poly Polygon, p Plane, eps float64) (in, out Polygon)`; `cut.intersect(a, b geom.Vec3, p Plane) geom.Vec3`; `cut.fanTriangles(poly Polygon, minArea float64) []stl.Tri`.

- [ ] **Step 1: Write the failing test**

Create `internal/cut/clip_test.go`:

```go
package cut

import (
	"math"
	"testing"

	"stl-cutter/internal/geom"
)

var zPlane = Plane{N: geom.Vec3{0, 0, 1}, D: 0}

const testEps = 1e-9

// This is the property the whole capping stage rests on: the two triangles that
// share an edge traverse it in opposite directions, and both must land on the
// same intersection point down to the last bit.
func TestIntersectIsIndependentOfEdgeDirection(t *testing.T) {
	a := geom.Vec3{0.1, 0.2, -0.3}
	b := geom.Vec3{0.7, -0.4, 0.9}
	ab := intersect(a, b, zPlane)
	ba := intersect(b, a, zPlane)
	if ab != ba {
		t.Fatalf("intersect(a,b) = %v but intersect(b,a) = %v; must be bit-identical", ab, ba)
	}
}

func TestIntersectLandsOnThePlane(t *testing.T) {
	got := intersect(geom.Vec3{0, 0, -2}, geom.Vec3{0, 0, 6}, zPlane)
	if math.Abs(zPlane.Dist(got)) > 1e-12 {
		t.Fatalf("intersection %v is not on the plane", got)
	}
	if got != (geom.Vec3{0, 0, 0}) {
		t.Fatalf("got %v, want the origin", got)
	}
}

func TestSplitPolygonPassesThroughWhollyInsidePolygons(t *testing.T) {
	poly := Polygon{{0, 0, 1}, {1, 0, 1}, {0, 1, 1}}
	in, out := splitPolygon(poly, zPlane, testEps)
	if len(in) != 3 || out != nil {
		t.Fatalf("in=%v out=%v, want the polygon unchanged inside and nothing outside", in, out)
	}
}

func TestSplitPolygonPassesThroughWhollyOutsidePolygons(t *testing.T) {
	poly := Polygon{{0, 0, -1}, {1, 0, -1}, {0, 1, -1}}
	in, out := splitPolygon(poly, zPlane, testEps)
	if in != nil || len(out) != 3 {
		t.Fatalf("in=%v out=%v, want nothing inside and the polygon unchanged outside", in, out)
	}
}

func TestSplitPolygonSplitsAStraddlingTriangle(t *testing.T) {
	// One vertex above the plane, two below.
	poly := Polygon{{0, 0, 2}, {0, 0, -2}, {2, 0, -2}}
	in, out := splitPolygon(poly, zPlane, testEps)
	if len(in) != 3 {
		t.Fatalf("inside piece has %d vertices, want 3", len(in))
	}
	if len(out) != 4 {
		t.Fatalf("outside piece has %d vertices, want 4", len(out))
	}
	// Every generated vertex must sit on or beyond the plane on its own side.
	for _, v := range in {
		if zPlane.Dist(v) < -testEps {
			t.Errorf("inside vertex %v is below the plane", v)
		}
	}
	for _, v := range out {
		if zPlane.Dist(v) > testEps {
			t.Errorf("outside vertex %v is above the plane", v)
		}
	}
}

// Vertices lying on the plane belong to both sides — that is what keeps the two
// pieces sharing an exact boundary instead of leaving a gap.
func TestSplitPolygonPutsOnPlaneVerticesInBothPieces(t *testing.T) {
	poly := Polygon{{0, 0, 0}, {2, 0, 0}, {1, 0, 2}, {1, 0, -2}}
	in, out := splitPolygon(poly, zPlane, testEps)
	countOnPlane := func(p Polygon) int {
		n := 0
		for _, v := range p {
			if math.Abs(zPlane.Dist(v)) <= testEps {
				n++
			}
		}
		return n
	}
	if countOnPlane(in) < 2 || countOnPlane(out) < 2 {
		t.Fatalf("in has %d on-plane vertices, out has %d; both need the shared boundary",
			countOnPlane(in), countOnPlane(out))
	}
}

func TestSplitPolygonDiscardsSubTriangularFragments(t *testing.T) {
	// A polygon touching the plane at a single vertex yields no real inside area.
	poly := Polygon{{0, 0, 0}, {1, 0, -1}, {-1, 0, -1}}
	in, _ := splitPolygon(poly, zPlane, testEps)
	if in != nil {
		t.Fatalf("in = %v, want nil for a fragment with fewer than three vertices", in)
	}
}

func TestFanTrianglesCoversPolygonArea(t *testing.T) {
	// A unit square in the z=0 plane.
	poly := Polygon{{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0}}
	tris := fanTriangles(poly, 0)
	if len(tris) != 2 {
		t.Fatalf("got %d triangles, want 2", len(tris))
	}
	var area float64
	for _, tr := range tris {
		area += tr.Area()
	}
	if math.Abs(area-1) > 1e-12 {
		t.Fatalf("total area = %v, want 1", area)
	}
}

func TestFanTrianglesDropsDegenerateSlivers(t *testing.T) {
	// Three collinear points have no area.
	poly := Polygon{{0, 0, 0}, {1, 0, 0}, {2, 0, 0}}
	if got := fanTriangles(poly, 1e-18); len(got) != 0 {
		t.Fatalf("got %d triangles, want 0", len(got))
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cut/ -run 'Intersect|SplitPolygon|FanTriangles' -v`
Expected: FAIL — `undefined: intersect`, `undefined: Polygon`.

- [ ] **Step 3: Write the implementation**

Create `internal/cut/clip.go`:

```go
package cut

import (
	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

// Polygon is a planar convex polygon. Splitting a convex polygon by a plane
// yields two convex polygons, so every fragment produced during clipping stays
// convex and can be fan-triangulated safely.
type Polygon []geom.Vec3

// intersect returns the point where segment ab crosses p.
//
// The endpoints are ordered canonically first. Without that, the two triangles
// sharing an edge traverse it in opposite directions and the floating-point
// result can differ in the last bits — enough to leave cap loops unable to
// chain, and the resulting parts not watertight.
func intersect(a, b geom.Vec3, p Plane) geom.Vec3 {
	if b.Less(a) {
		a, b = b, a
	}
	da, db := p.Dist(a), p.Dist(b)
	t := da / (da - db)
	return a.Add(b.Sub(a).Scale(t))
}

// splitPolygon divides poly along p, returning the part inside the half-space
// and the part outside it. Either result may be nil. Vertices lying on the
// plane go into both, so the two pieces share an exact boundary.
func splitPolygon(poly Polygon, p Plane, eps float64) (in, out Polygon) {
	n := len(poly)
	if n < 3 {
		return nil, nil
	}

	sides := make([]Side, n)
	anyInside, anyOutside := false, false
	for i, v := range poly {
		sides[i] = p.Classify(v, eps)
		switch sides[i] {
		case Inside:
			anyInside = true
		case Outside:
			anyOutside = true
		}
	}

	// Fast paths. These matter for performance, not just tidiness: on a large
	// model almost every triangle takes one of them, and returning the input
	// slice avoids allocating anything at all.
	if !anyOutside {
		return poly, nil
	}
	if !anyInside {
		return nil, poly
	}

	for i := 0; i < n; i++ {
		j := (i + 1) % n
		ci, si := poly[i], sides[i]
		sj := sides[j]

		if si != Outside {
			in = append(in, ci)
		}
		if si != Inside {
			out = append(out, ci)
		}
		// Only a strict sign change creates a new vertex. An On vertex has
		// already been added to both sides above.
		if (si == Inside && sj == Outside) || (si == Outside && sj == Inside) {
			x := intersect(ci, poly[j], p)
			in = append(in, x)
			out = append(out, x)
		}
	}

	if len(in) < 3 {
		in = nil
	}
	if len(out) < 3 {
		out = nil
	}
	return in, out
}

// fanTriangles triangulates a convex polygon from its first vertex. Triangles
// with an area at or below minArea are dropped, which removes the slivers that
// clipping near a vertex produces.
func fanTriangles(poly Polygon, minArea float64) []stl.Tri {
	if len(poly) < 3 {
		return nil
	}
	out := make([]stl.Tri, 0, len(poly)-2)
	for i := 1; i+1 < len(poly); i++ {
		t := stl.Tri{A: poly[0], B: poly[i], C: poly[i+1]}
		if t.Area() > minArea {
			out = append(out, t)
		}
	}
	return out
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/cut/ -v`
Expected: PASS, all plane and clip tests.

- [ ] **Step 5: Commit**

```bash
git add internal/cut/clip.go internal/cut/clip_test.go
git commit -m "feat: add convex polygon splitting with canonical edge intersection"
```

---

### Task 10: Cap boundary recovery

Cap boundaries are recovered **after** all splitting, not collected during it. That makes the result independent of the order the five planes are processed in, which is the property that keeps `split.go` simple. For a given plane, the boundary is exactly the set of polygon edges lying on that plane.

Edge direction is deliberately ignored here. Loop orientation gets decided in Task 11 from nesting depth, which is more reliable than inheriting winding from whichever triangle happened to produce the edge.

**Files:**
- Create: `internal/cut/loops.go`
- Test: `internal/cut/loops_test.go`

**Interfaces:**
- Consumes: `cut.Polygon`, `cut.Plane`, `geom.Welder`.
- Produces: `cut.boundaryEdges(polys []Polygon, p Plane, w *geom.Welder, eps float64) [][2]int`; `cut.assembleLoops(edges [][2]int, w *geom.Welder) (loops []Polygon, open int)`.

- [ ] **Step 1: Write the failing test**

Create `internal/cut/loops_test.go`:

```go
package cut

import (
	"testing"

	"stl-cutter/internal/geom"
)

func TestBoundaryEdgesFindsTheEdgeLyingOnThePlane(t *testing.T) {
	// A triangle with edge A-B on z=0 and C above it.
	polys := []Polygon{{{0, 0, 0}, {1, 0, 0}, {0, 0, 1}}}
	w := geom.NewWelder(1e-9)
	edges := boundaryEdges(polys, zPlane, w, testEps)
	if len(edges) != 1 {
		t.Fatalf("got %d edges, want 1", len(edges))
	}
}

// A triangle lying wholly in the cut plane would otherwise contribute all three
// of its edges and corrupt the loop graph.
func TestBoundaryEdgesIgnoresCoplanarPolygons(t *testing.T) {
	polys := []Polygon{{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}}
	w := geom.NewWelder(1e-9)
	if edges := boundaryEdges(polys, zPlane, w, testEps); len(edges) != 0 {
		t.Fatalf("got %d edges, want 0 for a coplanar polygon", len(edges))
	}
}

// Two polygons sharing an on-plane edge means that edge is interior to a
// coplanar region, not part of the cap boundary. Such pairs must cancel.
func TestBoundaryEdgesCancelsDuplicatedEdges(t *testing.T) {
	shared := []Polygon{
		{{0, 0, 0}, {1, 0, 0}, {0, 0, 1}},
		{{1, 0, 0}, {0, 0, 0}, {0, 0, -1}},
	}
	w := geom.NewWelder(1e-9)
	if edges := boundaryEdges(shared, zPlane, w, testEps); len(edges) != 0 {
		t.Fatalf("got %d edges, want 0 — a doubled edge is interior", len(edges))
	}
}

func TestAssembleLoopsChainsASquare(t *testing.T) {
	w := geom.NewWelder(1e-9)
	ids := []int{
		w.ID(geom.Vec3{0, 0, 0}),
		w.ID(geom.Vec3{1, 0, 0}),
		w.ID(geom.Vec3{1, 1, 0}),
		w.ID(geom.Vec3{0, 1, 0}),
	}
	edges := [][2]int{{ids[0], ids[1]}, {ids[1], ids[2]}, {ids[2], ids[3]}, {ids[3], ids[0]}}

	loops, open := assembleLoops(edges, w)
	if open != 0 {
		t.Fatalf("open = %d, want 0", open)
	}
	if len(loops) != 1 || len(loops[0]) != 4 {
		t.Fatalf("got %d loops with lengths %v, want one loop of 4", len(loops), loopLens(loops))
	}
}

// Edges are given in scrambled order and with inconsistent direction, because
// that is what the clipper actually produces.
func TestAssembleLoopsIgnoresEdgeOrderAndDirection(t *testing.T) {
	w := geom.NewWelder(1e-9)
	p := []geom.Vec3{{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0}}
	id := func(i int) int { return w.ID(p[i]) }
	edges := [][2]int{{id(2), id(1)}, {id(3), id(0)}, {id(0), id(1)}, {id(2), id(3)}}

	loops, open := assembleLoops(edges, w)
	if open != 0 || len(loops) != 1 || len(loops[0]) != 4 {
		t.Fatalf("open=%d loops=%v, want one closed loop of 4", open, loopLens(loops))
	}
}

func TestAssembleLoopsSeparatesDisjointLoops(t *testing.T) {
	w := geom.NewWelder(1e-9)
	var edges [][2]int
	// Two separate triangles, far apart.
	for _, off := range []float64{0, 100} {
		a := w.ID(geom.Vec3{off, 0, 0})
		b := w.ID(geom.Vec3{off + 1, 0, 0})
		c := w.ID(geom.Vec3{off, 1, 0})
		edges = append(edges, [2]int{a, b}, [2]int{b, c}, [2]int{c, a})
	}
	loops, open := assembleLoops(edges, w)
	if open != 0 {
		t.Fatalf("open = %d, want 0", open)
	}
	if len(loops) != 2 {
		t.Fatalf("got %d loops, want 2", len(loops))
	}
}

// This is the non-manifold detection path: a missing edge means the loop cannot
// close, and the caller must be told rather than shipping a broken part.
func TestAssembleLoopsReportsAnUnclosableChain(t *testing.T) {
	w := geom.NewWelder(1e-9)
	p := []geom.Vec3{{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0}}
	id := func(i int) int { return w.ID(p[i]) }
	// Square with one side missing.
	edges := [][2]int{{id(0), id(1)}, {id(1), id(2)}, {id(2), id(3)}}

	loops, open := assembleLoops(edges, w)
	if open == 0 {
		t.Fatal("open = 0, want a report of the unclosed chain")
	}
	if len(loops) != 0 {
		t.Fatalf("got %d loops, want none — the chain never closed", len(loops))
	}
}

// Loop order must not depend on Go's randomised map iteration, or test output
// and exported geometry would vary run to run.
func TestAssembleLoopsIsDeterministic(t *testing.T) {
	build := func() ([]Polygon, int) {
		w := geom.NewWelder(1e-9)
		var edges [][2]int
		for _, off := range []float64{0, 50, 100} {
			a := w.ID(geom.Vec3{off, 0, 0})
			b := w.ID(geom.Vec3{off + 1, 0, 0})
			c := w.ID(geom.Vec3{off, 1, 0})
			edges = append(edges, [2]int{a, b}, [2]int{b, c}, [2]int{c, a})
		}
		return assembleLoops(edges, w)
	}
	first, _ := build()
	for i := 0; i < 5; i++ {
		got, _ := build()
		if len(got) != len(first) {
			t.Fatalf("run %d produced %d loops, first run produced %d", i, len(got), len(first))
		}
		for j := range got {
			if len(got[j]) != len(first[j]) || got[j][0] != first[j][0] {
				t.Fatalf("run %d loop %d differs from the first run", i, j)
			}
		}
	}
}

func loopLens(loops []Polygon) []int {
	out := make([]int, len(loops))
	for i, l := range loops {
		out[i] = len(l)
	}
	return out
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cut/ -run 'BoundaryEdges|AssembleLoops' -v`
Expected: FAIL — `undefined: boundaryEdges`.

- [ ] **Step 3: Write the implementation**

Create `internal/cut/loops.go`:

```go
package cut

import (
	"math"
	"sort"

	"stl-cutter/internal/geom"
)

// boundaryEdges returns the cap boundary on plane p: the edges of polys whose
// both endpoints lie on p, as welded index pairs.
//
// Edges are returned undirected. Loop orientation is decided later from nesting
// depth, which is more dependable than inheriting the winding of whichever
// triangle happened to produce a given edge.
func boundaryEdges(polys []Polygon, p Plane, w *geom.Welder, eps float64) [][2]int {
	// An edge shared by two polygons is interior to a coplanar region rather
	// than part of the boundary, so occurrences are counted and only the odd
	// ones survive.
	counts := make(map[[2]int]int)
	order := make([][2]int, 0, 16)

	for _, poly := range polys {
		onPlane := make([]bool, len(poly))
		allOn := true
		for i, v := range poly {
			onPlane[i] = math.Abs(p.Dist(v)) <= eps
			if !onPlane[i] {
				allOn = false
			}
		}
		// A polygon lying wholly in the plane has no boundary of its own; every
		// one of its edges would otherwise be injected into the loop graph.
		if allOn {
			continue
		}
		for i := range poly {
			j := (i + 1) % len(poly)
			if !onPlane[i] || !onPlane[j] {
				continue
			}
			a, b := w.ID(poly[i]), w.ID(poly[j])
			if a == b {
				continue
			}
			key := [2]int{a, b}
			if a > b {
				key = [2]int{b, a}
			}
			if counts[key] == 0 {
				order = append(order, key)
			}
			counts[key]++
		}
	}

	edges := make([][2]int, 0, len(order))
	for _, key := range order {
		if counts[key]%2 == 1 {
			edges = append(edges, key)
		}
	}
	return edges
}

// assembleLoops chains undirected edges into closed loops. open counts chains
// that could not be closed, which is how non-manifold input is detected: a mesh
// with a hole produces a cross-section boundary that does not close.
//
// ponytail: a vertex where four or more boundary edges meet — a pinch point
// where the cross-section touches itself — is resolved by taking whichever
// unused edge comes first, which may pair the loops differently than a human
// would. Upgrade path if that shows up in practice: order the edges at such a
// vertex by angle in the cut plane and pair them by turn direction.
func assembleLoops(edges [][2]int, w *geom.Welder) (loops []Polygon, open int) {
	type link struct{ to, edge int }

	adj := make(map[int][]link, len(edges)*2)
	for ei, e := range edges {
		adj[e[0]] = append(adj[e[0]], link{to: e[1], edge: ei})
		adj[e[1]] = append(adj[e[1]], link{to: e[0], edge: ei})
	}

	// Map iteration order is randomised, so walk vertices in index order to keep
	// the output stable between runs.
	starts := make([]int, 0, len(adj))
	for v := range adj {
		starts = append(starts, v)
	}
	sort.Ints(starts)

	used := make([]bool, len(edges))
	pts := w.Points()

	nextUnused := func(v int) (link, bool) {
		for _, l := range adj[v] {
			if !used[l.edge] {
				return l, true
			}
		}
		return link{}, false
	}

	for _, start := range starts {
		for {
			if _, ok := nextUnused(start); !ok {
				break
			}
			loop := Polygon{pts[start]}
			cur, closed := start, false
			for {
				step, ok := nextUnused(cur)
				if !ok {
					break
				}
				used[step.edge] = true
				cur = step.to
				if cur == start {
					closed = true
					break
				}
				loop = append(loop, pts[cur])
			}
			if closed && len(loop) >= 3 {
				loops = append(loops, loop)
			} else {
				open++
			}
		}
	}
	return loops, open
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/cut/ -v`
Expected: PASS, all plane, clip and loop tests.

- [ ] **Step 5: Commit**

```bash
git add internal/cut/loops.go internal/cut/loops_test.go
git commit -m "feat: recover cap boundary edges and chain them into closed loops"
```

---

### Task 11: Face projection and hole grouping

Cap loops arrive as unordered 3D rings with arbitrary winding. Before they can be triangulated they need to be flattened into the plane's 2D basis, sorted into outer loops and holes, and given consistent orientation. Cutting a tube produces an annulus, so holes are the normal case rather than an exotic one.

The 3D point is carried alongside the 2D one throughout. Reconstructing 3D from 2D afterwards would introduce rounding that leaves hairline cracks between the cap and the clipped triangles it is supposed to close.

**Files:**
- Create: `internal/cut/face2d.go`
- Test: `internal/cut/face2d_test.go`

**Interfaces:**
- Consumes: `cut.Polygon`, `geom.Vec3`.
- Produces: `cut.pt2{X, Y float64}`; `cut.faceVert{P2 pt2; P3 geom.Vec3}`; `cut.faceLoop []faceVert`; `cut.projectLoop(loop Polygon, origin, u, v geom.Vec3) faceLoop`; `cut.signedArea2(l faceLoop) float64`; `cut.pointInLoop(p pt2, l faceLoop) bool`; `cut.reverseLoop(l faceLoop) faceLoop`; `cut.faceGroup{Outer faceLoop; Holes []faceLoop}`; `cut.groupLoops(loops []faceLoop) []faceGroup`.

- [ ] **Step 1: Write the failing test**

Create `internal/cut/face2d_test.go`:

```go
package cut

import (
	"math"
	"testing"

	"stl-cutter/internal/geom"
)

// square returns a loop in the z=0 plane, counter-clockwise seen from +Z.
func square(cx, cy, half float64) Polygon {
	return Polygon{
		{cx - half, cy - half, 0},
		{cx + half, cy - half, 0},
		{cx + half, cy + half, 0},
		{cx - half, cy + half, 0},
	}
}

func projectZ(loop Polygon) faceLoop {
	return projectLoop(loop, geom.Vec3{}, geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0})
}

func TestProjectLoopFlattensAndKeepsThe3DPoint(t *testing.T) {
	loop := Polygon{{1, 2, 7}, {3, 4, 7}}
	got := projectLoop(loop, geom.Vec3{0, 0, 7}, geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0})
	if got[0].P2 != (pt2{1, 2}) || got[1].P2 != (pt2{3, 4}) {
		t.Fatalf("2D coords = %v, %v, want {1,2} and {3,4}", got[0].P2, got[1].P2)
	}
	if got[0].P3 != (geom.Vec3{1, 2, 7}) {
		t.Fatalf("3D point = %v, want the original vertex preserved exactly", got[0].P3)
	}
}

func TestSignedAreaSignIndicatesWinding(t *testing.T) {
	ccw := projectZ(square(0, 0, 1))
	if got := signedArea2(ccw); got <= 0 {
		t.Fatalf("counter-clockwise area = %v, want positive", got)
	}
	if got := signedArea2(reverseLoop(ccw)); got >= 0 {
		t.Fatalf("clockwise area = %v, want negative", got)
	}
	if got := math.Abs(signedArea2(ccw)); math.Abs(got-8) > 1e-12 {
		t.Fatalf("|signedArea2| = %v, want 8 (twice the area of a 2x2 square)", got)
	}
}

func TestPointInLoop(t *testing.T) {
	l := projectZ(square(0, 0, 1))
	if !pointInLoop(pt2{0, 0}, l) {
		t.Error("centre should be inside")
	}
	if pointInLoop(pt2{5, 0}, l) {
		t.Error("distant point should be outside")
	}
	if pointInLoop(pt2{0, 5}, l) {
		t.Error("distant point above should be outside")
	}
}

// The annulus a tube cut produces: one outer loop with one hole inside it.
func TestGroupLoopsPairsAHoleWithItsOuter(t *testing.T) {
	loops := []faceLoop{projectZ(square(0, 0, 5)), projectZ(square(0, 0, 2))}
	groups := groupLoops(loops)

	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(groups))
	}
	if len(groups[0].Holes) != 1 {
		t.Fatalf("got %d holes, want 1", len(groups[0].Holes))
	}
	if signedArea2(groups[0].Outer) <= 0 {
		t.Error("outer loop must be normalised counter-clockwise")
	}
	if signedArea2(groups[0].Holes[0]) >= 0 {
		t.Error("hole must be normalised clockwise")
	}
}

// Input winding is arbitrary, so grouping must normalise it rather than trust it.
func TestGroupLoopsNormalisesArbitraryInputWinding(t *testing.T) {
	loops := []faceLoop{
		reverseLoop(projectZ(square(0, 0, 5))), // outer, given clockwise
		projectZ(square(0, 0, 2)),              // hole, given counter-clockwise
	}
	groups := groupLoops(loops)
	if len(groups) != 1 || len(groups[0].Holes) != 1 {
		t.Fatalf("got %d groups, want 1 with 1 hole", len(groups))
	}
	if signedArea2(groups[0].Outer) <= 0 || signedArea2(groups[0].Holes[0]) >= 0 {
		t.Error("winding was not normalised")
	}
}

func TestGroupLoopsKeepsDisjointLoopsSeparate(t *testing.T) {
	loops := []faceLoop{projectZ(square(0, 0, 1)), projectZ(square(10, 0, 1))}
	groups := groupLoops(loops)
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}
	for i, g := range groups {
		if len(g.Holes) != 0 {
			t.Errorf("group %d has %d holes, want 0", i, len(g.Holes))
		}
	}
}

// Three levels of nesting: an island inside a hole is a solid region again, so
// it becomes its own group rather than a hole of the outermost loop.
func TestGroupLoopsTreatsAnIslandInsideAHoleAsItsOwnGroup(t *testing.T) {
	loops := []faceLoop{
		projectZ(square(0, 0, 10)), // outer
		projectZ(square(0, 0, 6)),  // hole
		projectZ(square(0, 0, 2)),  // island inside the hole
	}
	groups := groupLoops(loops)
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2 (outer-with-hole, and the island)", len(groups))
	}

	var withHole, island *faceGroup
	for i := range groups {
		if len(groups[i].Holes) == 1 {
			withHole = &groups[i]
		} else {
			island = &groups[i]
		}
	}
	if withHole == nil || island == nil {
		t.Fatalf("expected one group with a hole and one without, got %v", groupHoleCounts(groups))
	}
	if math.Abs(signedArea2(island.Outer)) >= math.Abs(signedArea2(withHole.Outer)) {
		t.Error("the island should be the smaller of the two outers")
	}
}

func groupHoleCounts(gs []faceGroup) []int {
	out := make([]int, len(gs))
	for i, g := range gs {
		out[i] = len(g.Holes)
	}
	return out
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cut/ -run 'ProjectLoop|SignedArea|PointInLoop|GroupLoops' -v`
Expected: FAIL — `undefined: projectLoop`.

- [ ] **Step 3: Write the implementation**

Create `internal/cut/face2d.go`:

```go
package cut

import (
	"sort"

	"stl-cutter/internal/geom"
)

// pt2 is a point in a cut plane's 2D basis.
type pt2 struct{ X, Y float64 }

// faceVert carries a cap vertex in both representations. The 3D point is kept
// rather than reconstructed from the 2D one after triangulation: reconstruction
// rounds, and the rounding shows up as hairline cracks between the cap and the
// clipped triangles it closes.
type faceVert struct {
	P2 pt2
	P3 geom.Vec3
}

type faceLoop []faceVert

// projectLoop flattens a 3D cap loop into the (u, v) basis of its plane.
func projectLoop(loop Polygon, origin, u, v geom.Vec3) faceLoop {
	out := make(faceLoop, len(loop))
	for i, p := range loop {
		d := p.Sub(origin)
		out[i] = faceVert{P2: pt2{X: d.Dot(u), Y: d.Dot(v)}, P3: p}
	}
	return out
}

// signedArea2 returns twice the signed area. Positive means counter-clockwise.
func signedArea2(l faceLoop) float64 {
	var s float64
	for i := range l {
		j := (i + 1) % len(l)
		s += l[i].P2.X*l[j].P2.Y - l[j].P2.X*l[i].P2.Y
	}
	return s
}

func reverseLoop(l faceLoop) faceLoop {
	out := make(faceLoop, len(l))
	for i := range l {
		out[i] = l[len(l)-1-i]
	}
	return out
}

// pointInLoop is a standard crossing-number test.
func pointInLoop(p pt2, l faceLoop) bool {
	in := false
	for i, j := 0, len(l)-1; i < len(l); j, i = i, i+1 {
		pi, pj := l[i].P2, l[j].P2
		if (pi.Y > p.Y) != (pj.Y > p.Y) {
			x := pi.X + (p.Y-pi.Y)/(pj.Y-pi.Y)*(pj.X-pi.X)
			if p.X < x {
				in = !in
			}
		}
	}
	return in
}

// faceGroup is one solid region of a cap: an outer boundary, normalised
// counter-clockwise, and the holes inside it, normalised clockwise.
type faceGroup struct {
	Outer faceLoop
	Holes []faceLoop
}

// groupLoops sorts cap loops into solid regions by nesting depth. A loop nested
// an even number of times bounds material; an odd number bounds a void. So an
// island inside a hole is a solid region in its own right, not a hole of the
// outermost loop.
//
// ponytail: nesting is tested with one vertex per loop, which assumes loops do
// not touch. Cross-sections of a solid satisfy that. Upgrade path if
// self-touching cross-sections turn up: test an interior point derived from the
// loop instead of one of its vertices.
func groupLoops(loops []faceLoop) []faceGroup {
	n := len(loops)
	if n == 0 {
		return nil
	}

	depth := make([]int, n)
	for i := range loops {
		if len(loops[i]) == 0 {
			continue
		}
		probe := loops[i][0].P2
		for j := range loops {
			if i != j && pointInLoop(probe, loops[j]) {
				depth[i]++
			}
		}
	}

	// Largest first, so that when a hole is assigned to its innermost containing
	// outer that outer already exists.
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return depth[idx[a]] < depth[idx[b]] })

	groups := make([]faceGroup, 0, n)
	outerAt := make(map[int]int) // loop index -> index into groups

	for _, i := range idx {
		l := loops[i]
		if len(l) < 3 {
			continue
		}
		if depth[i]%2 == 0 {
			if signedArea2(l) < 0 {
				l = reverseLoop(l)
			}
			outerAt[i] = len(groups)
			groups = append(groups, faceGroup{Outer: l})
			continue
		}

		// A hole belongs to the containing loop one level shallower.
		if signedArea2(l) > 0 {
			l = reverseLoop(l)
		}
		probe := loops[i][0].P2
		host := -1
		for j := range loops {
			if j == i || depth[j] != depth[i]-1 {
				continue
			}
			if pointInLoop(probe, loops[j]) {
				host = j
				break
			}
		}
		if g, ok := outerAt[host]; ok {
			groups[g].Holes = append(groups[g].Holes, l)
		}
		// A hole with no host cannot happen for a closed cross-section; if it
		// somehow does, dropping it loses a void rather than corrupting a solid.
	}
	return groups
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/cut/ -v`
Expected: PASS, all tests so far.

- [ ] **Step 5: Commit**

```bash
git add internal/cut/face2d.go internal/cut/face2d_test.go
git commit -m "feat: project cap loops to 2D and group them into outers and holes"
```

---

### Task 12: Hole bridging and ear clipping

Ear clipping handles a single simple polygon. A cap with holes is turned into one by *bridging*: joining each hole to the outer loop with a zero-width channel, which duplicates two vertices and leaves a polygon that is simple in the sense ear clipping needs.

**Files:**
- Create: `internal/cut/earclip.go`
- Test: `internal/cut/earclip_test.go`

**Interfaces:**
- Consumes: `cut.faceLoop`, `cut.faceVert`, `cut.pt2`.
- Produces: `cut.cross2(a, b, c pt2) float64`; `cut.segmentsProperlyCross(a, b, c, d pt2) bool`; `cut.rightmostIdx(l faceLoop) int`; `cut.visibleVertex(poly faceLoop, m pt2) int`; `cut.bridgeHoles(outer faceLoop, holes []faceLoop) faceLoop`; `cut.earClip(l faceLoop) (tris [][3]int, ok bool)`.

- [ ] **Step 1: Write the failing test**

Create `internal/cut/earclip_test.go`:

```go
package cut

import (
	"math"
	"testing"
)

// area2D totals the area of index triples over a loop, so a triangulation can
// be checked against the area it is supposed to cover.
func area2D(l faceLoop, tris [][3]int) float64 {
	var total float64
	for _, tr := range tris {
		total += math.Abs(cross2(l[tr[0]].P2, l[tr[1]].P2, l[tr[2]].P2)) / 2
	}
	return total
}

func TestCross2SignIndicatesTurnDirection(t *testing.T) {
	if got := cross2(pt2{0, 0}, pt2{1, 0}, pt2{0, 1}); got <= 0 {
		t.Fatalf("left turn = %v, want positive", got)
	}
	if got := cross2(pt2{0, 0}, pt2{1, 0}, pt2{0, -1}); got >= 0 {
		t.Fatalf("right turn = %v, want negative", got)
	}
	if got := cross2(pt2{0, 0}, pt2{1, 0}, pt2{2, 0}); got != 0 {
		t.Fatalf("collinear = %v, want 0", got)
	}
}

func TestSegmentsProperlyCross(t *testing.T) {
	if !segmentsProperlyCross(pt2{0, 0}, pt2{2, 2}, pt2{0, 2}, pt2{2, 0}) {
		t.Error("crossing diagonals should report a crossing")
	}
	if segmentsProperlyCross(pt2{0, 0}, pt2{1, 0}, pt2{2, 0}, pt2{3, 0}) {
		t.Error("disjoint collinear segments should not report a crossing")
	}
	// Segments meeting at a shared endpoint must not count: bridges legitimately
	// touch the polygon at the vertex they connect to.
	if segmentsProperlyCross(pt2{0, 0}, pt2{1, 1}, pt2{1, 1}, pt2{2, 0}) {
		t.Error("segments sharing an endpoint should not report a crossing")
	}
}

func TestEarClipASquare(t *testing.T) {
	l := projectZ(square(0, 0, 1))
	tris, ok := earClip(l)
	if !ok {
		t.Fatal("ok = false, want a complete triangulation")
	}
	if len(tris) != 2 {
		t.Fatalf("got %d triangles, want 2", len(tris))
	}
	if got := area2D(l, tris); math.Abs(got-4) > 1e-12 {
		t.Fatalf("area = %v, want 4", got)
	}
}

// A non-convex loop is where a naive fan triangulation would fail and ear
// clipping must not.
func TestEarClipAnLShape(t *testing.T) {
	l := projectZ(Polygon{
		{0, 0, 0}, {3, 0, 0}, {3, 1, 0}, {1, 1, 0}, {1, 3, 0}, {0, 3, 0},
	})
	tris, ok := earClip(l)
	if !ok {
		t.Fatal("ok = false, want a complete triangulation")
	}
	if len(tris) != 4 {
		t.Fatalf("got %d triangles, want 4 (n-2 for 6 vertices)", len(tris))
	}
	if got := area2D(l, tris); math.Abs(got-5) > 1e-12 {
		t.Fatalf("area = %v, want 5", got)
	}
}

func TestEarClipReportsFailureOnADegenerateLoop(t *testing.T) {
	l := projectZ(Polygon{{0, 0, 0}, {1, 0, 0}, {2, 0, 0}, {3, 0, 0}})
	if _, ok := earClip(l); ok {
		t.Fatal("ok = true, want false for a loop with no area")
	}
}

func TestRightmostIdx(t *testing.T) {
	l := projectZ(Polygon{{0, 0, 0}, {5, 1, 0}, {2, 2, 0}})
	if got := rightmostIdx(l); got != 1 {
		t.Fatalf("rightmostIdx = %d, want 1", got)
	}
}

func TestVisibleVertexPicksAReachableVertexToTheRight(t *testing.T) {
	outer := projectZ(square(0, 0, 5))
	i := visibleVertex(outer, pt2{0, 0})
	if outer[i].P2.X < 0 {
		t.Fatalf("chose vertex %v, want one at or right of the probe", outer[i].P2)
	}
	// The chosen vertex must actually be joinable without crossing an edge.
	for k := range outer {
		k2 := (k + 1) % len(outer)
		if k == i || k2 == i {
			continue
		}
		if segmentsProperlyCross(pt2{0, 0}, outer[i].P2, outer[k].P2, outer[k2].P2) {
			t.Fatalf("bridge to vertex %d crosses edge %d-%d", i, k, k2)
		}
	}
}

func TestBridgeHolesProducesOneLoopWithTheChannelVertices(t *testing.T) {
	outer := projectZ(square(0, 0, 5))
	hole := reverseLoop(projectZ(square(0, 0, 2))) // holes arrive clockwise
	got := bridgeHoles(outer, []faceLoop{hole})

	// 4 outer + 4 hole + 2 duplicated vertices forming the zero-width channel.
	if len(got) != 10 {
		t.Fatalf("got %d vertices, want 10", len(got))
	}
}

// The end-to-end property that matters: an annular cap triangulates to the
// annulus area, not the full disc.
func TestBridgedAnnulusTriangulatesToTheAnnulusArea(t *testing.T) {
	outer := projectZ(square(0, 0, 5))       // 10x10, area 100
	hole := reverseLoop(projectZ(square(0, 0, 2))) // 4x4, area 16
	merged := bridgeHoles(outer, []faceLoop{hole})

	tris, ok := earClip(merged)
	if !ok {
		t.Fatalf("ok = false, want a complete triangulation of the bridged loop")
	}
	if got := area2D(merged, tris); math.Abs(got-84) > 1e-9 {
		t.Fatalf("area = %v, want 84 (100 minus the 16 hole)", got)
	}
}

func TestBridgeHolesHandlesTwoHoles(t *testing.T) {
	outer := projectZ(square(0, 0, 10))            // 20x20, area 400
	h1 := reverseLoop(projectZ(square(-5, 0, 2)))  // area 16
	h2 := reverseLoop(projectZ(square(5, 0, 2)))   // area 16
	merged := bridgeHoles(outer, []faceLoop{h1, h2})

	tris, ok := earClip(merged)
	if !ok {
		t.Fatalf("ok = false, want a complete triangulation")
	}
	if got := area2D(merged, tris); math.Abs(got-368) > 1e-9 {
		t.Fatalf("area = %v, want 368 (400 minus two 16 holes)", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cut/ -run 'Cross2|SegmentsProperly|EarClip|Rightmost|VisibleVertex|BridgeHoles|BridgedAnnulus' -v`
Expected: FAIL — `undefined: cross2`.

- [ ] **Step 3: Write the implementation**

Create `internal/cut/earclip.go`:

```go
package cut

import (
	"math"
	"sort"
)

// cross2 is the z component of (b-a) x (c-a). Positive means a->b->c turns left.
func cross2(a, b, c pt2) float64 {
	return (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X)
}

// segmentsProperlyCross reports whether ab and cd cross at interior points of
// both. Shared endpoints and collinear touching deliberately do not count: a
// bridge always touches the polygon at the vertex it connects to, and treating
// that as a crossing would reject every valid bridge.
func segmentsProperlyCross(a, b, c, d pt2) bool {
	d1 := cross2(c, d, a)
	d2 := cross2(c, d, b)
	d3 := cross2(a, b, c)
	d4 := cross2(a, b, d)
	return (d1 > 0) != (d2 > 0) && (d3 > 0) != (d4 > 0)
}

func rightmostIdx(l faceLoop) int {
	best := 0
	for i := range l {
		if l[i].P2.X > l[best].P2.X {
			best = i
		}
	}
	return best
}

// visibleVertex returns the index of a vertex of poly that m can be joined to
// without the join crossing any edge of poly. Candidates at or right of m are
// preferred and the nearest is taken, because bridging proceeds rightward.
//
// ponytail: O(n^2) per hole. Caps have tens to low hundreds of boundary
// vertices, so this is immaterial. Upgrade path if a cap ever gets large: the
// Eberly ray-cast construction, which finds a visible vertex in one pass.
func visibleVertex(poly faceLoop, m pt2) int {
	clear := func(i int) bool {
		v := poly[i].P2
		for k := range poly {
			k2 := (k + 1) % len(poly)
			if k == i || k2 == i {
				continue // edges incident to the candidate always touch it
			}
			if segmentsProperlyCross(m, v, poly[k].P2, poly[k2].P2) {
				return false
			}
		}
		return true
	}

	best, bestDist := -1, math.Inf(1)
	for i := range poly {
		v := poly[i].P2
		if v.X < m.X {
			continue
		}
		dx, dy := v.X-m.X, v.Y-m.Y
		d := dx*dx + dy*dy
		if d >= bestDist || !clear(i) {
			continue
		}
		best, bestDist = i, d
	}
	if best >= 0 {
		return best
	}
	// Nothing to the right was reachable — accept any visible vertex.
	for i := range poly {
		if clear(i) {
			return i
		}
	}
	return 0
}

// bridgeHoles splices each hole into outer through a zero-width channel,
// returning a single loop that ear clipping can handle. outer must be
// counter-clockwise and each hole clockwise, as groupLoops guarantees.
//
// The channel duplicates two vertices: the outer vertex the bridge lands on and
// the hole vertex it leaves from. That is intended — the duplicates give the
// traversal a way in and back out of the hole.
func bridgeHoles(outer faceLoop, holes []faceLoop) faceLoop {
	result := append(faceLoop(nil), outer...)

	// Rightmost hole first. visibleVertex searches rightward, so handling holes
	// right to left keeps already-spliced channels out of later searches.
	ordered := append([]faceLoop(nil), holes...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i][rightmostIdx(ordered[i])].P2.X > ordered[j][rightmostIdx(ordered[j])].P2.X
	})

	for _, h := range ordered {
		if len(h) < 3 {
			continue
		}
		hi := rightmostIdx(h)
		entry := h[hi]
		pi := visibleVertex(result, entry.P2)

		spliced := make(faceLoop, 0, len(result)+len(h)+2)
		spliced = append(spliced, result[:pi+1]...)
		for k := 0; k < len(h); k++ {
			spliced = append(spliced, h[(hi+k)%len(h)])
		}
		spliced = append(spliced, entry)          // close the hole traversal
		spliced = append(spliced, result[pi:]...) // and rejoin the outer loop
		result = spliced
	}
	return result
}

// earClip triangulates a simple counter-clockwise loop, returning index triples
// into l. ok reports whether the triangulation is complete: a simple polygon of
// n vertices must yield exactly n-2 triangles, and anything less means the
// input was degenerate or self-intersecting. Callers surface that rather than
// shipping a cap with a hole in it.
func earClip(l faceLoop) (tris [][3]int, ok bool) {
	n := len(l)
	if n < 3 {
		return nil, false
	}

	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}

	tris = make([][3]int, 0, n-2)
	// Each pass either removes a vertex or gives up, so n attempts per removal
	// bounds the work; the guard exists so malformed input cannot spin forever.
	guard, maxGuard := 0, n*n+16

	for len(idx) > 3 {
		clipped := false
		for k := range idx {
			prev := idx[(k+len(idx)-1)%len(idx)]
			cur := idx[k]
			next := idx[(k+1)%len(idx)]

			// A reflex or zero-area corner is not an ear. Zero-area corners are
			// common here: every bridge channel has two of them.
			if cross2(l[prev].P2, l[cur].P2, l[next].P2) <= 0 {
				continue
			}
			if containsOther(l, idx, prev, cur, next) {
				continue
			}
			tris = append(tris, [3]int{prev, cur, next})
			idx = append(idx[:k], idx[k+1:]...)
			clipped = true
			break
		}
		guard++
		if !clipped || guard > maxGuard {
			break
		}
	}
	if len(idx) == 3 {
		tris = append(tris, [3]int{idx[0], idx[1], idx[2]})
	}
	return tris, len(tris) == n-2
}

// containsOther reports whether a remaining vertex lies strictly inside the
// candidate ear. Comparison is by coordinate rather than index because bridging
// duplicates vertices: a duplicate sitting on a corner is that same point, not
// an intruder blocking the ear.
func containsOther(l faceLoop, idx []int, ia, ib, ic int) bool {
	a, b, c := l[ia].P2, l[ib].P2, l[ic].P2
	for _, i := range idx {
		if i == ia || i == ib || i == ic {
			continue
		}
		p := l[i].P2
		if p == a || p == b || p == c {
			continue
		}
		if cross2(a, b, p) > 0 && cross2(b, c, p) > 0 && cross2(c, a, p) > 0 {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/cut/ -v`
Expected: PASS, all tests so far.

If `TestBridgedAnnulusTriangulatesToTheAnnulusArea` reports 100 instead of 84, the hole was passed counter-clockwise — `groupLoops` normalises holes to clockwise, and `bridgeHoles` depends on that. If it reports `ok = false`, the bridge landed on a vertex it cannot see; check `segmentsProperlyCross` uses strict sign comparison so touching segments do not count as crossings.

- [ ] **Step 5: Commit**

```bash
git add internal/cut/earclip.go internal/cut/earclip_test.go
git commit -m "feat: add hole bridging and ear clipping for cap triangulation"
```

---

### Task 13: Cap assembly

Wires Tasks 10–12 into one call: boundary edges → loops → 2D groups → bridged and ear-clipped → 3D triangles facing `+p.N`.

**Files:**
- Create: `internal/cut/cap.go`
- Test: `internal/cut/cap_test.go`

**Interfaces:**
- Consumes: `boundaryEdges`, `assembleLoops`, `projectLoop`, `groupLoops`, `bridgeHoles`, `earClip`, `cut.Plane`, `stl.Tri`.
- Produces: `cut.planeBasis(p Plane) (origin, u, v geom.Vec3)`; `cut.triangulateFace(polys []Polygon, p Plane, eps float64) (tris []stl.Tri, open, incomplete int)`.

- [ ] **Step 1: Write the failing test**

Create `internal/cut/cap_test.go`:

```go
package cut

import (
	"math"
	"testing"

	"stl-cutter/internal/geom"
)

// wellPolys returns the four vertical quads of a square shaft whose top edges
// all lie on z=0 and which extends downward. Their on-plane edges form a closed
// square, which is exactly the shape a cut produces.
func wellPolys(half, depth float64) []Polygon {
	corners := [4][2]float64{{-half, -half}, {half, -half}, {half, half}, {-half, half}}
	out := make([]Polygon, 0, 4)
	for i := range corners {
		j := (i + 1) % 4
		a := geom.Vec3{corners[i][0], corners[i][1], 0}
		b := geom.Vec3{corners[j][0], corners[j][1], 0}
		out = append(out, Polygon{a, b,
			{b[0], b[1], -depth},
			{a[0], a[1], -depth},
		})
	}
	return out
}

func TestPlaneBasisIsRightHanded(t *testing.T) {
	for _, n := range []geom.Vec3{{0, 0, 1}, {1, 0, 0}, {0, -1, 0}, {1, 2, 3}} {
		p := Plane{N: n.Unit(), D: 1.5}
		origin, u, v := planeBasis(p)
		if math.Abs(p.Dist(origin)) > 1e-12 {
			t.Errorf("normal %v: basis origin is not on the plane", n)
		}
		if got := u.Cross(v); got.Sub(p.N).Len() > 1e-12 {
			t.Errorf("normal %v: u cross v = %v, want %v", n, got, p.N)
		}
	}
}

func TestTriangulateFaceCapsASquareOpening(t *testing.T) {
	tris, open, incomplete := triangulateFace(wellPolys(5, 3), zPlane, 1e-9)
	if open != 0 || incomplete != 0 {
		t.Fatalf("open=%d incomplete=%d, want 0 and 0", open, incomplete)
	}
	if len(tris) != 2 {
		t.Fatalf("got %d triangles, want 2", len(tris))
	}
	var area float64
	for _, tr := range tris {
		area += tr.Area()
	}
	if math.Abs(area-100) > 1e-9 {
		t.Fatalf("cap area = %v, want 100", area)
	}
}

// The cap must face +p.N. Split relies on that to hand the same geometry to both
// output parts with opposite winding.
func TestTriangulateFaceOrientsCapAlongPlaneNormal(t *testing.T) {
	tris, _, _ := triangulateFace(wellPolys(5, 3), zPlane, 1e-9)
	for i, tr := range tris {
		if got := tr.Normal(); got.Sub(zPlane.N).Len() > 1e-9 {
			t.Errorf("triangle %d normal = %v, want %v", i, got, zPlane.N)
		}
	}
}

// A tube's cross-section is an annulus, so this is the case that exercises the
// whole hole path end to end.
func TestTriangulateFaceCapsAnAnnulus(t *testing.T) {
	polys := append(wellPolys(5, 3), wellPolys(2, 3)...)
	tris, open, incomplete := triangulateFace(polys, zPlane, 1e-9)
	if open != 0 || incomplete != 0 {
		t.Fatalf("open=%d incomplete=%d, want 0 and 0", open, incomplete)
	}
	var area float64
	for _, tr := range tris {
		area += tr.Area()
	}
	if math.Abs(area-84) > 1e-9 {
		t.Fatalf("cap area = %v, want 84 (100 minus the 16 hole)", area)
	}
}

// A missing wall leaves the cross-section boundary unable to close, which is how
// non-manifold input is detected.
func TestTriangulateFaceReportsAnOpenBoundary(t *testing.T) {
	polys := wellPolys(5, 3)[:3]
	tris, open, _ := triangulateFace(polys, zPlane, 1e-9)
	if open == 0 {
		t.Fatal("open = 0, want a report of the unclosed boundary")
	}
	if len(tris) != 0 {
		t.Fatalf("got %d triangles, want none for an unclosed boundary", len(tris))
	}
}

func TestTriangulateFaceReturnsNothingWhenThePlaneMissesEverything(t *testing.T) {
	far := Plane{N: geom.Vec3{0, 0, 1}, D: 1000}
	tris, open, incomplete := triangulateFace(wellPolys(5, 3), far, 1e-9)
	if len(tris) != 0 || open != 0 || incomplete != 0 {
		t.Fatalf("got %d tris, open=%d, incomplete=%d; want all zero", len(tris), open, incomplete)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cut/ -run 'PlaneBasis|TriangulateFace' -v`
Expected: FAIL — `undefined: planeBasis`.

- [ ] **Step 3: Write the implementation**

Create `internal/cut/cap.go`:

```go
package cut

import (
	"math"

	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

// planeBasis returns a point on p together with a right-handed orthonormal
// in-plane basis satisfying u x v == p.N. That identity is what makes a
// counter-clockwise 2D triangle come back out facing +p.N.
func planeBasis(p Plane) (origin, u, v geom.Vec3) {
	n := p.N.Unit()
	origin = n.Scale(p.D)

	// Seed with the axis least aligned to n so the cross product stays well
	// conditioned.
	seed := geom.Vec3{1, 0, 0}
	if math.Abs(n[0]) > math.Abs(n[1]) {
		seed = geom.Vec3{0, 1, 0}
	}
	u = seed.Sub(n.Scale(seed.Dot(n))).Unit()
	return origin, u, n.Cross(u)
}

// triangulateFace builds the cap that closes polys on plane p. Returned
// triangles face +p.N.
//
// open counts boundary chains that could not be closed, which happens when the
// input mesh is not manifold. incomplete counts regions ear clipping could not
// fully triangulate. Both are reported rather than swallowed: a cap with a hole
// in it produces a part that looks fine on screen and fails to print.
func triangulateFace(polys []Polygon, p Plane, eps float64) (tris []stl.Tri, open, incomplete int) {
	w := geom.NewWelder(eps)
	edges := boundaryEdges(polys, p, w, eps)
	if len(edges) == 0 {
		return nil, 0, 0
	}

	loops, open := assembleLoops(edges, w)
	if len(loops) == 0 {
		return nil, open, 0
	}

	origin, u, v := planeBasis(p)
	projected := make([]faceLoop, 0, len(loops))
	for _, l := range loops {
		projected = append(projected, projectLoop(l, origin, u, v))
	}

	minArea := eps * eps
	for _, g := range groupLoops(projected) {
		merged := bridgeHoles(g.Outer, g.Holes)
		idxTris, ok := earClip(merged)
		if !ok {
			incomplete++
		}
		for _, tr := range idxTris {
			t := stl.Tri{A: merged[tr[0]].P3, B: merged[tr[1]].P3, C: merged[tr[2]].P3}
			if t.Area() > minArea {
				tris = append(tris, t)
			}
		}
	}
	return tris, open, incomplete
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/cut/ -v`
Expected: PASS, all tests so far.

If the cap normals come out inverted, `groupLoops` is returning the outer loop clockwise — check `signedArea2`'s sign convention rather than flipping the triangles here, because `Split` depends on `+p.N` exactly.

- [ ] **Step 5: Commit**

```bash
git add internal/cut/cap.go internal/cut/cap_test.go
git commit -m "feat: assemble cap triangles from boundary loops"
```

---

### Task 14: The bounded cut

Everything comes together. This is also where the three spec invariants get asserted for real, on every fixture — including the U, which is the case the whole project exists for.

**Files:**
- Create: `internal/cut/split.go`
- Test: `internal/cut/split_test.go`

**Interfaces:**
- Consumes: `Spec`, `splitPolygon`, `fanTriangles`, `triangulateFace`, `stl.Mesh`.
- Produces: `cut.Result{Part1, Part2 *stl.Mesh; OpenLoops, Incomplete int; Warnings []string}` with `Watertight() bool`; `cut.Split(m *stl.Mesh, s Spec) (*Result, error)`.

- [ ] **Step 1: Write the failing test**

Create `internal/cut/split_test.go`:

```go
package cut

import (
	"math"
	"strings"
	"testing"

	"stl-cutter/internal/fixtures"
	"stl-cutter/internal/geom"
	"stl-cutter/internal/meshcheck"
	"stl-cutter/internal/stl"
)

// assertInvariants checks the three properties from the spec that must hold for
// every cut: volume is conserved, both parts are watertight, and geometry
// outside the cutter is untouched.
func assertInvariants(t *testing.T, in *stl.Mesh, r *Result) {
	t.Helper()
	eps := in.Epsilon()

	sum := r.Part1.Volume() + r.Part2.Volume()
	want := in.Volume()
	// Scale the tolerance to the model: a 100mm part's volume is 1e6 mm³, so an
	// absolute epsilon would be meaninglessly strict.
	tol := math.Max(1e-6, math.Abs(want)*1e-9)
	if math.Abs(sum-want) > tol {
		t.Errorf("volume not conserved: part1=%v + part2=%v = %v, want %v (diff %v)",
			r.Part1.Volume(), r.Part2.Volume(), sum, want, sum-want)
	}

	if rep := meshcheck.Check(r.Part1, eps); !rep.OK() {
		t.Errorf("part1 %s", rep)
	}
	if rep := meshcheck.Check(r.Part2, eps); !rep.OK() {
		t.Errorf("part2 %s", rep)
	}
}

// assertOutsideIdentity is the property that proves the cut really is bounded:
// a triangle wholly outside the cutter must reappear in part1 bit for bit.
func assertOutsideIdentity(t *testing.T, in *stl.Mesh, s Spec, r *Result) {
	t.Helper()
	eps := in.Epsilon()
	planes := s.Planes()

	have := make(map[stl.Tri]int, len(r.Part1.Tris))
	for _, tr := range r.Part1.Tris {
		have[tr]++
	}

	checked := 0
	for _, tr := range in.Tris {
		outside := false
		for _, p := range planes {
			if p.Classify(tr.A, eps) == Outside &&
				p.Classify(tr.B, eps) == Outside &&
				p.Classify(tr.C, eps) == Outside {
				outside = true
				break
			}
		}
		if !outside {
			continue
		}
		checked++
		if have[tr] == 0 {
			t.Fatalf("triangle %v lies entirely outside the cutter but is not in part1 unchanged", tr)
		}
	}
	if checked == 0 {
		t.Fatal("no triangle was wholly outside the cutter; this fixture cannot test the identity property")
	}
}

func TestSplitCubeInHalf(t *testing.T) {
	m := fixtures.Cube(10)
	// Rectangle large enough to span the whole cross-section, so this behaves as
	// an unbounded plane.
	s := SpecFromNormal(geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, 1}, 100, 100)

	r, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	assertInvariants(t, m, r)

	if got := r.Part2.Volume(); math.Abs(got-500) > 1e-6 {
		t.Errorf("part2 volume = %v, want 500", got)
	}
	if got := r.Part1.Volume(); math.Abs(got-500) > 1e-6 {
		t.Errorf("part1 volume = %v, want 500", got)
	}
}

func TestSplitCubeOnAnObliquePlane(t *testing.T) {
	m := fixtures.Cube(10)
	s := SpecFromNormal(geom.Vec3{5, 5, 5}, geom.Vec3{1, 1, 1}, 100, 100)

	r, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	assertInvariants(t, m, r)
	// A plane through the centre of a cube halves it whatever its orientation.
	if got := r.Part2.Volume(); math.Abs(got-500) > 1e-3 {
		t.Errorf("part2 volume = %v, want 500", got)
	}
}

func TestSplitSphere(t *testing.T) {
	m := fixtures.UVSphere(10, 48, 24)
	s := SpecFromNormal(geom.Vec3{}, geom.Vec3{0, 0, 1}, 100, 100)

	r, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	assertInvariants(t, m, r)
	if math.Abs(r.Part1.Volume()-r.Part2.Volume()) > 1e-3 {
		t.Errorf("halves differ: %v vs %v", r.Part1.Volume(), r.Part2.Volume())
	}
}

// A tube cut across its axis produces annular caps, so this exercises hole
// handling inside the full pipeline.
func TestSplitTubeProducesAnnularCaps(t *testing.T) {
	m := fixtures.Tube(5, 3, 20, 48)
	s := SpecFromNormal(geom.Vec3{0, 0, 10}, geom.Vec3{0, 0, 1}, 100, 100)

	r, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	assertInvariants(t, m, r)
}

// The motivating case. A plane at y=25 bounded to x in [0,15] must take the top
// off the left arm and leave the right arm attached to the base.
func TestSplitUShapeCutsOneArmOnly(t *testing.T) {
	m := fixtures.UShape(10)
	s := SpecFromNormal(geom.Vec3{7.5, 25, 5}, geom.Vec3{0, 1, 0}, 15, 20)

	r, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	assertInvariants(t, m, r)
	assertOutsideIdentity(t, m, s, r)

	// The severed piece is the left arm above y=25: 10 wide, 15 tall, 10 deep.
	if got := r.Part2.Volume(); math.Abs(got-1500) > 1e-6 {
		t.Errorf("part2 volume = %v, want 1500", got)
	}
	if got := r.Part1.Volume(); math.Abs(got-7500) > 1e-6 {
		t.Errorf("part1 volume = %v, want 7500", got)
	}

	// part2 must be the left arm alone.
	if b := r.Part2.BBox(); b.Min[0] < -1e-9 || b.Max[0] > 10+1e-9 {
		t.Errorf("part2 spans x %v..%v, want it confined to the left arm 0..10", b.Min[0], b.Max[0])
	}

	// The decisive check: the right arm is still there, still above the cut
	// height, still part of part1.
	rightArmHigh := false
	for _, tr := range r.Part1.Tris {
		for _, v := range [3]geom.Vec3{tr.A, tr.B, tr.C} {
			if v[0] > 20-1e-9 && v[1] > 39-1e-9 {
				rightArmHigh = true
			}
		}
	}
	if !rightArmHigh {
		t.Error("the right arm's top is missing from part1; the bounded plane cut it too")
	}
}

// Same U, but the rectangle now reaches across both arms, so both get cut. This
// is the control that proves the bound in the previous test is what did the work.
func TestSplitUShapeWithAWideRectangleCutsBothArms(t *testing.T) {
	m := fixtures.UShape(10)
	s := SpecFromNormal(geom.Vec3{15, 25, 5}, geom.Vec3{0, 1, 0}, 100, 20)

	r, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	assertInvariants(t, m, r)

	// Both arms above y=25: 2 x 10 x 15 x 10.
	if got := r.Part2.Volume(); math.Abs(got-3000) > 1e-6 {
		t.Errorf("part2 volume = %v, want 3000", got)
	}
}

// A rectangle whose edges pass through material still yields watertight parts —
// the cut simply continues along the rectangle's side planes.
func TestSplitWithRectangleEdgesInsideMaterial(t *testing.T) {
	m := fixtures.Cube(10)
	s := SpecFromNormal(geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, 1}, 4, 4)

	r, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	assertInvariants(t, m, r)

	// part2 is the 4x4 column above z=5: 4 * 4 * 5.
	if got := r.Part2.Volume(); math.Abs(got-80) > 1e-6 {
		t.Errorf("part2 volume = %v, want 80", got)
	}
}

func TestSplitRejectsAPlaneThatMissesTheModel(t *testing.T) {
	m := fixtures.Cube(10)
	s := SpecFromNormal(geom.Vec3{5, 5, 500}, geom.Vec3{0, 0, 1}, 100, 100)
	if _, err := Split(m, s); err == nil {
		t.Fatal("expected an error when the cutter contains no material")
	}
}

func TestSplitRejectsARectangleThatMissesTheModel(t *testing.T) {
	m := fixtures.Cube(10)
	s := SpecFromNormal(geom.Vec3{500, 500, 5}, geom.Vec3{0, 0, 1}, 4, 4)
	if _, err := Split(m, s); err == nil {
		t.Fatal("expected an error when the rectangle sits beside the model")
	}
}

func TestSplitRejectsACutterSwallowingTheWholeModel(t *testing.T) {
	m := fixtures.Cube(10)
	s := SpecFromNormal(geom.Vec3{5, 5, -50}, geom.Vec3{0, 0, 1}, 100, 100)
	if _, err := Split(m, s); err == nil {
		t.Fatal("expected an error when nothing is left behind")
	}
}

func TestSplitRejectsAnInvalidSpec(t *testing.T) {
	if _, err := Split(fixtures.Cube(10), Spec{}); err == nil {
		t.Fatal("expected an error for a zero-value spec")
	}
}

func TestSplitRejectsAnEmptyMesh(t *testing.T) {
	s := SpecFromNormal(geom.Vec3{}, geom.Vec3{0, 0, 1}, 10, 10)
	if _, err := Split(&stl.Mesh{}, s); err == nil {
		t.Fatal("expected an error for a mesh with no triangles")
	}
}

// Non-manifold input is reported, not rejected and not silently accepted. The
// caller decides whether to keep a part that is flagged as not watertight.
func TestSplitReportsNonManifoldInputWithoutFailing(t *testing.T) {
	m := fixtures.NonManifold()
	s := SpecFromNormal(geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, 1}, 100, 100)

	r, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split should report, not fail: %v", err)
	}
	if r.OpenLoops == 0 {
		t.Error("OpenLoops = 0, want the unclosed boundary reported")
	}
	if r.Watertight() {
		t.Error("Watertight() = true, want false")
	}
	if len(r.Warnings) == 0 {
		t.Error("Warnings is empty, want an explanation of what went wrong")
	}
}

// Placing the plane exactly on a face is ambiguous, and the user must be told
// rather than handed a silently wrong volume.
//
// The U at y=10 is the case that makes this reachable: the notch floor is a face
// lying exactly in the plane, and material remains on both sides, so Split
// proceeds rather than refusing for want of a part.
func TestSplitWarnsWhenThePlaneIsCoplanarWithFaces(t *testing.T) {
	m := fixtures.UShape(10)
	s := SpecFromNormal(geom.Vec3{15, 10, 5}, geom.Vec3{0, 1, 0}, 100, 100)

	r, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(r.Warnings) == 0 {
		t.Fatal("expected a warning that the plane is coplanar with model faces")
	}

	found := false
	for _, w := range r.Warnings {
		if strings.Contains(w, "flat against") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings do not mention the coplanar faces: %v", r.Warnings)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cut/ -run TestSplit -v`
Expected: FAIL — `undefined: Split`.

- [ ] **Step 3: Write the implementation**

Create `internal/cut/split.go`:

```go
package cut

import (
	"errors"
	"fmt"
	"math"

	"stl-cutter/internal/stl"
)

// Result holds the two halves of a cut and anything the caller needs to know
// about how well it went.
type Result struct {
	// Part1 is the material outside the cutter, Part2 the material inside it —
	// the piece the plane's normal points toward.
	Part1, Part2 *stl.Mesh

	// OpenLoops counts cap boundaries that could not be closed, which means the
	// input mesh was not manifold.
	OpenLoops int
	// Incomplete counts cap regions that could not be fully triangulated.
	Incomplete int

	Warnings []string
}

// Watertight reports whether both parts closed cleanly. When false the parts are
// still returned, flagged, so the caller can decide — silently shipping a part
// with a hole in it would produce a model that looks right and fails to print.
func (r *Result) Watertight() bool { return r.OpenLoops == 0 && r.Incomplete == 0 }

type triClass int

const (
	triStraddles triClass = iota
	triInside
	triOutside
)

// classifyTri is the fast path. On a large model nearly every triangle is wholly
// on one side, and recognising that avoids allocating polygon fragments for it.
func classifyTri(t stl.Tri, planes []Plane, eps float64) triClass {
	allInside := true
	for _, p := range planes {
		a := p.Classify(t.A, eps)
		b := p.Classify(t.B, eps)
		c := p.Classify(t.C, eps)
		// Outside any single half-space means outside the convex cutter.
		if a == Outside && b == Outside && c == Outside {
			return triOutside
		}
		if a == Outside || b == Outside || c == Outside {
			allInside = false
		}
	}
	if allInside {
		return triInside
	}
	return triStraddles
}

// Split cuts m with the bounded plane s, returning the material outside the
// cutter as Part1 and the material inside it as Part2.
func Split(m *stl.Mesh, s Spec) (*Result, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if len(m.Tris) == 0 {
		return nil, errors.New("mesh has no triangles")
	}

	eps := m.Epsilon()
	minArea := eps * eps
	planes := s.Planes()
	cutPlane := planes[0]

	part1 := &stl.Mesh{}
	part2 := &stl.Mesh{}
	// insidePolys keeps the polygon fragments of part2, because cap boundaries
	// are recovered from polygon edges rather than collected during clipping.
	// Doing it afterwards is what makes the result independent of plane order.
	var insidePolys []Polygon
	coplanar := 0

	for _, t := range m.Tris {
		switch classifyTri(t, planes, eps) {
		case triOutside:
			part1.Tris = append(part1.Tris, t)
			continue
		case triInside:
			part2.Tris = append(part2.Tris, t)
			insidePolys = append(insidePolys, Polygon{t.A, t.B, t.C})
			if cutPlane.Classify(t.A, eps) == On &&
				cutPlane.Classify(t.B, eps) == On &&
				cutPlane.Classify(t.C, eps) == On {
				coplanar++
			}
			continue
		}

		fragments := []Polygon{{t.A, t.B, t.C}}
		for _, p := range planes {
			var next []Polygon
			for _, poly := range fragments {
				in, out := splitPolygon(poly, p, eps)
				if in != nil {
					next = append(next, in)
				}
				if out != nil {
					part1.Tris = append(part1.Tris, fanTriangles(out, minArea)...)
				}
			}
			fragments = next
			if len(fragments) == 0 {
				break
			}
		}
		for _, poly := range fragments {
			part2.Tris = append(part2.Tris, fanTriangles(poly, minArea)...)
			insidePolys = append(insidePolys, poly)
		}
	}

	if len(part2.Tris) == 0 {
		return nil, errors.New("the cutting plane and rectangle enclose no part of the model — nothing to cut")
	}
	if len(part1.Tris) == 0 {
		return nil, errors.New("the cutting rectangle encloses the entire model — nothing would be left behind")
	}

	res := &Result{Part1: part1, Part2: part2}

	// Cap every cutter plane, not just the cut plane: when a rectangle edge
	// passes through material, the cut continues along that side plane and needs
	// closing too.
	for _, p := range planes {
		tris, open, incomplete := triangulateFace(insidePolys, p, eps)
		res.OpenLoops += open
		res.Incomplete += incomplete
		for _, t := range tris {
			// t faces +p.N, which points into the cutter: outward for part1,
			// inward for part2, so part2 takes the reverse.
			part1.Tris = append(part1.Tris, t)
			part2.Tris = append(part2.Tris, t.Reversed())
		}
	}

	if res.OpenLoops > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"%d cut boundary %s could not be closed — the model is not watertight where it was cut, so the parts may have holes",
			res.OpenLoops, plural(res.OpenLoops, "loop", "loops")))
	}
	if res.Incomplete > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"%d cut %s could not be fully triangulated",
			res.Incomplete, plural(res.Incomplete, "face", "faces")))
	}
	if coplanar > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"the cutting plane lies flat against %d model %s; move it slightly to get a predictable result",
			coplanar, plural(coplanar, "face", "faces")))
	}

	// Volume conservation is cheap to verify and catches whole classes of
	// winding and capping bugs, so it is checked in production too, not only in
	// tests.
	if want, got := m.Volume(), part1.Volume()+part2.Volume(); math.Abs(got-want) > math.Max(1e-6, math.Abs(want)*1e-6) {
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"the parts total %.4g in volume but the original was %.4g — the cut may be flawed", got, want))
	}

	return res, nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/cut/ -v`
Expected: PASS, every test in the package.

Diagnosis if something fails:
- **Volume off by exactly the cap area's contribution** — cap winding is inverted. Check the `t` / `t.Reversed()` assignment in the capping loop; `+p.N` points *into* the cutter.
- **`part1` not watertight but `part2` fine** — the four side planes are not being capped. Confirm the loop iterates all of `planes`, not just `planes[0]`.
- **U test cuts both arms** — `SpecFromNormal`'s basis is not what the test assumes. Print `s.Basis()` and check the rectangle's x extent really is 0..15.

- [ ] **Step 5: Commit**

```bash
git add internal/cut/split.go internal/cut/split_test.go
git commit -m "feat: add bounded plane mesh splitting with capped watertight parts"
```

---

### Task 15: End-to-end CLI

Proves the library works on real files, not just on generated fixtures. This is also the tool used to sanity-check a cut in a slicer before any UI exists — load the output in PrusaSlicer or Bambu Studio and confirm it reports a watertight, printable body.

`run` is separated from `main` so the whole flow is testable without spawning a process.

**Files:**
- Create: `cmd/cutdemo/main.go`
- Test: `cmd/cutdemo/main_test.go`

**Interfaces:**
- Consumes: `stl.ReadFile`, `stl.WriteFile`, `cut.Split`, `cut.SpecFromNormal`.
- Produces: `main.parseVec3(s string) (geom.Vec3, error)`; `main.run(args []string, out io.Writer) error`.

- [ ] **Step 1: Write the failing test**

Create `cmd/cutdemo/main_test.go`:

```go
package main

import (
	"bytes"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"stl-cutter/internal/fixtures"
	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

func TestParseVec3(t *testing.T) {
	got, err := parseVec3("1.5,-2,3e0")
	if err != nil {
		t.Fatalf("parseVec3: %v", err)
	}
	if got != (geom.Vec3{1.5, -2, 3}) {
		t.Fatalf("got %v, want {1.5 -2 3}", got)
	}
}

func TestParseVec3RejectsBadInput(t *testing.T) {
	for _, s := range []string{"", "1,2", "1,2,3,4", "1,2,x"} {
		if _, err := parseVec3(s); err == nil {
			t.Errorf("parseVec3(%q): expected an error", s)
		}
	}
}

func TestRunCutsAFileAndWritesBothParts(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "cube.stl")
	if err := stl.WriteFile(in, fixtures.Cube(10)); err != nil {
		t.Fatalf("write input: %v", err)
	}
	prefix := filepath.Join(dir, "out")

	var log bytes.Buffer
	err := run([]string{
		"-in", in, "-out", prefix,
		"-origin", "5,5,5", "-normal", "0,0,1",
		"-width", "100", "-height", "100",
	}, &log)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, log.String())
	}

	for _, name := range []string{"out_part1.stl", "out_part2.stl"} {
		m, err := stl.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if got := m.Volume(); math.Abs(got-500) > 1 {
			t.Errorf("%s volume = %v, want about 500", name, got)
		}
	}

	if !strings.Contains(log.String(), "watertight") {
		t.Errorf("output did not report watertightness:\n%s", log.String())
	}
}

func TestRunReportsAMissingInputFile(t *testing.T) {
	var log bytes.Buffer
	err := run([]string{"-in", "/nonexistent/nope.stl", "-out", "x"}, &log)
	if err == nil {
		t.Fatal("expected an error for a missing input file")
	}
}

func TestRunRequiresInputAndOutput(t *testing.T) {
	var log bytes.Buffer
	if err := run([]string{"-out", "x"}, &log); err == nil {
		t.Error("expected an error when -in is missing")
	}
	if err := run([]string{"-in", "x.stl"}, &log); err == nil {
		t.Error("expected an error when -out is missing")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/cutdemo/ -v`
Expected: FAIL — `undefined: parseVec3`, `undefined: run`.

- [ ] **Step 3: Write the implementation**

Create `cmd/cutdemo/main.go`:

```go
// Command cutdemo cuts an STL with a bounded plane and writes the two parts.
// It exists to exercise the geometry library on real files, ahead of any UI:
// load its output in a slicer and confirm both parts come up watertight.
//
//	cutdemo -in model.stl -out part \
//	        -origin 7.5,25,5 -normal 0,1,0 -width 15 -height 20
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"stl-cutter/internal/cut"
	"stl-cutter/internal/geom"
	"stl-cutter/internal/meshcheck"
	"stl-cutter/internal/stl"
)

func parseVec3(s string) (geom.Vec3, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 3 {
		return geom.Vec3{}, fmt.Errorf("want three comma-separated numbers, got %q", s)
	}
	var v geom.Vec3
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return geom.Vec3{}, fmt.Errorf("component %d of %q is not a number", i+1, s)
		}
		v[i] = f
	}
	return v, nil
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("cutdemo", flag.ContinueOnError)
	fs.SetOutput(out)

	inPath := fs.String("in", "", "input STL file")
	outPrefix := fs.String("out", "", "output path prefix; writes <prefix>_part1.stl and <prefix>_part2.stl")
	originStr := fs.String("origin", "0,0,0", "point on the cutting plane, as x,y,z")
	normalStr := fs.String("normal", "0,0,1", "plane normal, as x,y,z; part2 is the side it points to")
	width := fs.Float64("width", 1e9, "rectangle extent across the plane; the default spans any model")
	height := fs.Float64("height", 1e9, "rectangle extent along the plane; the default spans any model")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *inPath == "" {
		return errors.New("-in is required")
	}
	if *outPrefix == "" {
		return errors.New("-out is required")
	}

	origin, err := parseVec3(*originStr)
	if err != nil {
		return fmt.Errorf("-origin: %w", err)
	}
	normal, err := parseVec3(*normalStr)
	if err != nil {
		return fmt.Errorf("-normal: %w", err)
	}

	mesh, err := stl.ReadFile(*inPath)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "loaded %d triangles, volume %.4g, %s\n",
		len(mesh.Tris), mesh.Volume(), meshcheck.Check(mesh, mesh.Epsilon()))

	res, err := cut.Split(mesh, cut.SpecFromNormal(origin, normal, *width, *height))
	if err != nil {
		return err
	}

	for _, w := range res.Warnings {
		fmt.Fprintf(out, "warning: %s\n", w)
	}

	for i, part := range []*stl.Mesh{res.Part1, res.Part2} {
		path := fmt.Sprintf("%s_part%d.stl", *outPrefix, i+1)
		if err := stl.WriteFile(path, part); err != nil {
			return err
		}
		b := part.BBox()
		fmt.Fprintf(out, "%s: %d triangles, volume %.4g, size %.4gx%.4gx%.4g, %s\n",
			path, len(part.Tris), part.Volume(),
			b.Size()[0], b.Size()[1], b.Size()[2],
			meshcheck.Check(part, mesh.Epsilon()))
	}
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "cutdemo:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/cutdemo/ -v`
Expected: PASS, five tests.

- [ ] **Step 5: Verify the whole module is green**

Run: `go vet ./... && go test ./... -v`
Expected: no vet findings; every package passes.

- [ ] **Step 6: Add a fixture writer so fixtures can be inspected outside tests**

`cutdemo` only reads STLs, so something has to put a fixture on disk for the manual check. Create `cmd/genfixture/main.go`:

```go
// Command genfixture writes a named test fixture to an STL file, so fixtures can
// be opened in a slicer or fed to cutdemo without going through a Go test.
//
//	genfixture -name u -out testdata/u.stl
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"stl-cutter/internal/fixtures"
	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

func build(name string) (*stl.Mesh, error) {
	switch name {
	case "u":
		return fixtures.UShape(10), nil
	case "cube":
		return fixtures.Cube(10), nil
	case "sphere":
		return fixtures.UVSphere(10, 48, 24), nil
	case "tube":
		return fixtures.Tube(5, 3, 20, 48), nil
	case "hollowbox":
		return fixtures.HollowBox(geom.Vec3{20, 20, 20}, 1), nil
	default:
		return nil, fmt.Errorf("unknown fixture %q; want u, cube, sphere, tube or hollowbox", name)
	}
}

func main() {
	name := flag.String("name", "", "fixture to write: u, cube, sphere, tube, hollowbox")
	out := flag.String("out", "", "output STL path")
	flag.Parse()

	err := func() error {
		if *name == "" || *out == "" {
			return errors.New("-name and -out are both required")
		}
		m, err := build(*name)
		if err != nil {
			return err
		}
		return stl.WriteFile(*out, m)
	}()
	if err != nil {
		fmt.Fprintln(os.Stderr, "genfixture:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 7: Cut the U by hand and confirm it in a slicer**

This is the check the automated tests cannot make: that the output is genuinely printable.

```bash
mkdir -p testdata
go run ./cmd/genfixture -name u -out testdata/u.stl
go run ./cmd/cutdemo -in testdata/u.stl -out /tmp/u \
    -origin 7.5,25,5 -normal 0,1,0 -width 15 -height 20
```

Expected output: both parts reported `watertight`, `/tmp/u_part1.stl` volume about 7500 and `/tmp/u_part2.stl` about 1500. Open both in a slicer and confirm neither reports errors needing repair, and that `part1` still has both arms of the U while `part2` is a single rectangular block.

- [ ] **Step 8: Commit**

```bash
git add cmd/cutdemo/ cmd/genfixture/ testdata/
git commit -m "feat: add cutdemo CLI proving the geometry library end to end"
```

---

## Done When

- `go vet ./... && go test ./...` is clean.
- `cutdemo` cuts the U fixture into a 7500 and a 1500 part, both reported watertight.
- Both parts load into a slicer without repair warnings.

## What Plan 1 Does Not Cover

Deferred to the next two plans, and listed here so nothing is assumed complete:

| Deferred | Plan |
|---|---|
| Wails shell, three.js viewer, plane gizmo, `Cut`/`Undo`/`ExportAll` | Plan 2 |
| Part tree and selection state | Plan 2 |
| Alignment pins, ray-cast wall checks | Plan 3 |
| Auto-split to build volume | Plan 3 |
| Per-part measurements surfaced in the UI | Plan 3 |
| GitHub Actions build matrix | Plan 3 |

The pipeline order pins one constraint Plan 3 must respect: **pin planning runs between loop assembly and cap triangulation**, because pin circles enter the cap as additional hole loops. `triangulateFace` will therefore gain an optional set of extra hole loops rather than being called as-is.
