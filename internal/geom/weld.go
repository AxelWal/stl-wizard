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
