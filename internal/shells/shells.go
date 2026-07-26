// Package shells separates a mesh into its disconnected bodies.
//
// An STL is a bag of triangles with no notion of a body, so a file holding several
// solids arrives as one mesh and cuts and exports as one. One real example: a part
// exported from this application held two solids of 578,016 and 572,593 mm³
// presented as a single 1,150,609 mm³ part.
package shells

import (
	"sort"

	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

// Split returns one mesh per body, largest by triangle count first.
//
// Triangles join into a surface across edges shared by exactly two of them. A
// non-manifold edge deliberately does not join, which is what leaves two bodies
// touching along an edge as two surfaces — and is what lets separating them make
// each one sound without moving any geometry.
//
// A surface nested inside another is grouped with its container rather than
// returned on its own, because a hollow model's internal void is exactly such a
// surface and returning it separately would give a solid shell and an inside-out
// one.
func Split(m *stl.Mesh, eps float64) []*stl.Mesh {
	return Nest(Surfaces(m, eps))
}

// Surfaces returns one mesh per connected surface, without folding nested ones
// into their containers.
//
// This is the raw labelling, and it is what a caller weighing surfaces
// individually needs. Repair wants it: a zero-volume flap sitting inside a body's
// bounding box is debris to be deleted, and handing it back already merged into
// that body would hide it behind the body's own volume. Bodies and surfaces are
// different questions, and conflating them silently stopped repair from fixing the
// files it was written for.
func Surfaces(m *stl.Mesh, eps float64) []*stl.Mesh {
	if len(m.Tris) == 0 {
		return nil
	}

	roots := label(m, eps)

	// Group by root, in first-appearance order so the result is deterministic
	// whatever order the edge map happened to iterate in.
	order := make([]int, 0, 8)
	groups := make(map[int][]stl.Tri)
	for i, t := range m.Tris {
		r := roots[i]
		if _, seen := groups[r]; !seen {
			order = append(order, r)
		}
		groups[r] = append(groups[r], t)
	}

	surfaces := make([]*stl.Mesh, 0, len(order))
	for _, r := range order {
		surfaces = append(surfaces, &stl.Mesh{Tris: groups[r]})
	}

	sort.SliceStable(surfaces, func(i, j int) bool {
		return len(surfaces[i].Tris) > len(surfaces[j].Tris)
	})
	return surfaces
}

// label assigns every triangle the id of the surface it belongs to.
func label(m *stl.Mesh, eps float64) []int {
	w := geom.NewWelder(eps)
	type edge struct{ a, b int }
	shared := make(map[edge][]int, len(m.Tris)*3)
	for i, t := range m.Tris {
		id := [3]int{w.ID(t.A), w.ID(t.B), w.ID(t.C)}
		for k := 0; k < 3; k++ {
			a, b := id[k], id[(k+1)%3]
			if a > b {
				a, b = b, a
			}
			shared[edge{a, b}] = append(shared[edge{a, b}], i)
		}
	}

	parent := make([]int, len(m.Tris))
	for i := range parent {
		parent[i] = i
	}
	find := func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	for _, ts := range shared {
		if len(ts) != 2 {
			continue
		}
		if ra, rb := find(ts[0]), find(ts[1]); ra != rb {
			parent[ra] = rb
		}
	}

	roots := make([]int, len(m.Tris))
	for i := range roots {
		roots[i] = find(i)
	}
	return roots
}

// Nest folds every surface contained by another into its container, and returns
// only those contained by nothing — which is what makes a body a body rather than a
// surface. Exported so a caller that has already dropped some surfaces can ask what
// bodies the remainder makes up, without labelling the mesh a second time.
//
// ponytail: bounding-box containment, not true geometric containment. It is right
// for a hollow shell's void, for two solids side by side, and for debris sitting
// inside a body's box, and it fails safe — a misjudged surface lands in the wrong
// body rather than corrupting either. Upgrade path if two interlocking parts ever
// need to be told apart: one point-in-mesh ray test per nested surface against each
// candidate container, using cut's existing ray grid.
func Nest(surfaces []*stl.Mesh) []*stl.Mesh {
	if len(surfaces) < 2 {
		return surfaces
	}

	boxes := make([]stl.BBox, len(surfaces))
	for i, s := range surfaces {
		boxes[i] = s.BBox()
	}

	// container[i] is the smallest surface whose box holds i's box, or -1.
	container := make([]int, len(surfaces))
	for i := range container {
		container[i] = -1
		for j := range surfaces {
			if i == j || !contains(boxes[j], boxes[i]) {
				continue
			}
			// Two surfaces with identical boxes would otherwise each claim the other.
			// Break that by index, so exactly one of the pair ends up nested.
			if contains(boxes[i], boxes[j]) && j > i {
				continue
			}
			if container[i] == -1 || volumeOf(boxes[j]) < volumeOf(boxes[container[i]]) {
				container[i] = j
			}
		}
	}

	// Walk to the outermost container, so a void inside a body inside nothing ends
	// up on the body rather than orphaned.
	outermost := func(i int) int {
		for depth := 0; container[i] != -1 && depth <= len(surfaces); depth++ {
			i = container[i]
		}
		return i
	}

	merged := make(map[int][]stl.Tri)
	var order []int
	for i := range surfaces {
		o := outermost(i)
		if _, seen := merged[o]; !seen {
			order = append(order, o)
		}
		merged[o] = append(merged[o], surfaces[i].Tris...)
	}

	out := make([]*stl.Mesh, 0, len(order))
	for _, o := range order {
		out = append(out, &stl.Mesh{Tris: merged[o]})
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i].Tris) > len(out[j].Tris) })
	return out
}

// contains reports whether outer holds inner, allowing exact coincidence so a
// surface is not judged outside a box it lies exactly on.
func contains(outer, inner stl.BBox) bool {
	for k := 0; k < 3; k++ {
		if inner.Min[k] < outer.Min[k] || inner.Max[k] > outer.Max[k] {
			return false
		}
	}
	return true
}

func volumeOf(b stl.BBox) float64 {
	s := b.Size()
	return s[0] * s[1] * s[2]
}
