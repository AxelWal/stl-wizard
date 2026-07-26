package geom

import "math"

// Welder maps near-coincident points onto a single canonical index. Cut-edge
// loop assembly relies on it: STL stores vertices per triangle, and real files
// routinely differ in the low bits for what is meant to be one shared vertex,
// so chaining edges by exact equality would leave loops open.
//
// This is the hot spot of the whole application. meshcheck, shells, cut, repair
// and threemf all weld a whole mesh, and on a 492k-triangle model a profile of
// meshcheck.Check spent 81% of its time in runtime.mapaccess1 with 75% inside
// ID. The two structures below exist to keep the number of lookups down; see ID.
type Welder struct {
	eps float64
	// side is the grid cell's edge length, deliberately 2*eps rather than eps.
	// See ID for why that is what makes eight cells enough.
	side float64
	// exact answers a point that has been seen bit-for-bit before, which on real
	// geometry is most of them: 83% of the vertices in a 492k-triangle export are
	// exact repeats, and on that file exact matching alone finds every weld the
	// eps probe does. It is a fast path, never the only path — generated meshes do
	// differ in the low bits, so a miss still falls through to the probe.
	exact map[Vec3]int
	grid  map[[3]int64][]int
	pts   []Vec3
}

func NewWelder(eps float64) *Welder {
	return &Welder{
		eps:   eps,
		side:  2 * eps,
		exact: make(map[Vec3]int),
		grid:  make(map[[3]int64][]int),
	}
}

func (w *Welder) cell(v Vec3) [3]int64 {
	return [3]int64{
		int64(math.Floor(v[0] / w.side)),
		int64(math.Floor(v[1] / w.side)),
		int64(math.Floor(v[2] / w.side)),
	}
}

// ID returns the canonical index for v, inserting it if no existing point lies
// within eps.
//
// Cells are 2*eps on a side, and that is the whole trick. A point sits at some
// offset p in [0, 2*eps) within its cell, so v-eps reaches the cell below only
// when p < eps, and v+eps reaches the cell above only when p >= eps — exactly
// one neighbour per axis, never both. Probing the point's own cell and that one
// neighbour on each axis therefore covers the entire eps ball in **eight**
// lookups and cannot miss a candidate.
//
// With cells of eps all three cells on every axis are always in range, which is
// why the earlier version had to probe 27 and why shrinking that number is not a
// matter of accepting a little risk: 8 is exact, and anything smaller is not.
func (w *Welder) ID(v Vec3) int {
	if i, ok := w.exact[v]; ok {
		return i
	}

	c := w.cell(v)
	// The one neighbour that the eps ball reaches on each axis.
	var near [3]int64
	for a := range 3 {
		if v[a]-float64(c[a])*w.side < w.eps {
			near[a] = c[a] - 1
		} else {
			near[a] = c[a] + 1
		}
	}

	eps2 := w.eps * w.eps
	for _, x := range [2]int64{c[0], near[0]} {
		for _, y := range [2]int64{c[1], near[1]} {
			for _, z := range [2]int64{c[2], near[2]} {
				for _, i := range w.grid[[3]int64{x, y, z}] {
					d := w.pts[i].Sub(v)
					if d.Dot(d) <= eps2 {
						// Remember the exact bits too, so a repeat of this
						// spelling of the point skips the probe next time.
						w.exact[v] = i
						return i
					}
				}
			}
		}
	}

	id := len(w.pts)
	w.pts = append(w.pts, v)
	w.grid[c] = append(w.grid[c], id)
	w.exact[v] = id
	return id
}

func (w *Welder) Points() []Vec3 { return w.pts }

func (w *Welder) Len() int { return len(w.pts) }
