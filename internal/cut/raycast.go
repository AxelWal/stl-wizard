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
// baryEps is how far outside a triangle a hit may fall and still count.
//
// Barycentric coordinates are dimensionless and run 0..1, so this needs no scaling to the
// model, and 1e-9 of one is a fraction of a nanometre on any real part.
//
// It is not cosmetic. Two triangles sharing an edge both put a hit on that edge at exactly
// a coordinate boundary, and the tests here are `< 0` and `> 1`, so a hit that lands exactly
// on the boundary is accepted by both — but one rounded an ulp outward is rejected by both,
// and the surface is porous: the ray passes through solid material and reports nothing.
// Whether it rounds outward depends on the platform, because arm64 may fuse a multiply-add
// where amd64 may not. A cube's top face is split along its diagonal and axialClearance
// samples its footprint at 45 degrees, landing exactly on it, so this cost two pin tests on
// macos-arm64 while both amd64 runners stayed green.
const baryEps = 1e-9

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
	if u < -baryEps || u > 1+baryEps {
		return 0, false
	}

	q := s.Cross(e1)
	v := f * dir.Dot(q)
	if v < -baryEps || u+v > 1+baryEps {
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

// nearestHit returns the distance to the closest surface along dir. dir need
// not be a unit vector — the result is always a true distance in model units,
// whatever scale the caller's direction happens to have.
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

	// Normalise once, so the result is a true distance in model units whatever
	// scale the caller's direction happened to have. Möller–Trumbore returns a
	// parameter along the direction it is given, so an unnormalised direction
	// would silently yield a number that is not millimetres.
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

// candidates returns the triangles worth testing: those in any cell the ray's
// path through the grid could touch.
//
// The ray is clipped to the grid's own bounding box before its cells are taken.
// Without that, an origin far outside the grid clamps to a boundary cell and the
// far end clamps to the same one, collapsing the search box to a single cell and
// silently missing every cell the ray really passes through — a reported miss for
// a ray that genuinely hits.
func (g *rayGrid) candidates(origin, dir geom.Vec3) []int32 {
	t0, t1, ok := g.clipRay(origin, dir)
	if !ok {
		return nil // the ray never enters the grid
	}

	a := g.cellOf(origin.Add(dir.Scale(t0)))
	b := g.cellOf(origin.Add(dir.Scale(t1)))

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

// clipRay returns the parameter range over which the ray lies inside the grid's
// bounding box, by the standard slab method. ok is false when the ray misses the
// box entirely.
func (g *rayGrid) clipRay(origin, dir geom.Vec3) (t0, t1 float64, ok bool) {
	t0, t1 = 0, math.Inf(1)

	for i := 0; i < 3; i++ {
		lo := g.min[i]
		hi := g.min[i] + g.cell[i]*float64(g.dims[i])

		if math.Abs(dir[i]) < 1e-15 {
			// Parallel to this slab: either inside it for the whole ray, or never.
			if origin[i] < lo || origin[i] > hi {
				return 0, 0, false
			}
			continue
		}

		inv := 1 / dir[i]
		near := (lo - origin[i]) * inv
		far := (hi - origin[i]) * inv
		if near > far {
			near, far = far, near
		}
		if near > t0 {
			t0 = near
		}
		if far < t1 {
			t1 = far
		}
		if t0 > t1 {
			return 0, 0, false
		}
	}
	return t0, t1, true
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
