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

	// dir is passed to rayTriangle unmodified: Möller–Trumbore's u/v are
	// scale-invariant in exact arithmetic, but not bit-for-bit identical
	// under floating point, so normalising here could flip a grazing hit
	// that a caller testing with the same raw dir would still see. Only the
	// candidate search — which just needs a well-scaled far point — uses a
	// unit vector.
	best := math.Inf(1)
	found := false
	seen := make(map[int32]bool)

	for _, idx := range g.candidates(origin, dir.Unit()) {
		if seen[idx] {
			continue
		}
		seen[idx] = true
		if dist, ok := rayTriangle(origin, dir, g.tris[idx]); ok && dist < best {
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
