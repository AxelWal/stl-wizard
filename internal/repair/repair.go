// Package repair closes holes in a mesh so a slightly broken model can be cut
// without every piece coming back flagged.
//
// It fixes what can be fixed and reports the rest. It fills holes, drops
// degenerate triangles, and deletes stray surfaces that enclose no volume.
//
// That last one is what repairs real files. Two parts exported from this
// application, of 492k and 1.75M triangles, reported 6 and 16 open edges and had
// no holes at all: every bad edge came from a two-triangle zero-volume flap fused
// to a real body, some 0.01mm across, left behind by a cut. Dropping those took
// every one of the 22 edges with them and left both files watertight with their
// volume unchanged.
//
// It does not correct winding. Flipping one backwards triangle means propagating a
// consistent orientation across the whole connected surface, which is a different
// algorithm with its own failure modes. Nor does it separate two genuinely solid
// bodies that touch along an edge. A mesh with either fault comes back reported
// and otherwise unchanged rather than silently mangled.
package repair

import (
	"math"

	"stl-cutter/internal/geom"
	"stl-cutter/internal/meshcheck"
	"stl-cutter/internal/stl"
)

// Result records what repair did, and what it could not do.
type Result struct {
	HolesFilled       int
	TrianglesAdded    int
	DegenerateRemoved int

	// ShellsDropped and TrianglesRemoved count connected surfaces deleted for
	// enclosing nothing — cut debris. Two parts exported from this application had
	// 7 and 11 two-triangle shells of exactly zero volume, some 0.01mm across, each
	// fused to a real body along one edge; that fusion is what made 21 of their 22
	// edges non-manifold. Deleting such a shell removes nothing that prints and
	// takes the bad edge with it.
	ShellsDropped    int
	TrianglesRemoved int

	// BoundaryEdges and NonManifoldEdges split the open-edge count meshcheck
	// reports, both measured before any work.
	//
	// An edge used once is a hole rim, and filling closes it. An edge used three
	// or more times has more than two triangles meeting along it — two surfaces
	// touching, a self-intersection — and filling does nothing whatever for it.
	// Without the split a caller can only say "filled 0 holes", which reads as a
	// repair that did not bother rather than a defect of a kind repair cannot
	// address. Real exported models turn out to be almost entirely the second
	// kind: two parts from this application, of 492k and 1.75M triangles, had 6
	// and 16 open edges and not one of them was a hole.
	BoundaryEdges    int
	NonManifoldEdges int

	// Before and After are the full verdicts either side of the work, so a caller
	// can report an improvement honestly rather than asserting one.
	Before, After meshcheck.Report
}

// Changed reports whether the mesh was actually modified.
func (r Result) Changed() bool {
	return r.HolesFilled > 0 || r.DegenerateRemoved > 0 || r.ShellsDropped > 0
}

// Unfixable reports that the mesh is still open and repair has nothing left to
// try: every remaining open edge is non-manifold rather than a hole rim. The
// caller should say so instead of implying the model merely needs another pass.
func (r Result) Unfixable() bool {
	return !r.After.OK() && r.After.OpenEdges > 0 && r.BoundaryEdges == 0 && r.NonManifoldEdges > 0
}

// Repair closes m's holes in place.
//
// Degenerate triangles go first: they have zero area and no valid boundary, and
// leaving them in would let a triangle's two remaining edges self-cancel into a
// convincing but fictional shared edge, hiding a genuinely open one.
//
// Each hole is then filled by fanning its boundary loop from that loop's own
// centroid. Every boundary edge receives exactly one new triangle, so every
// boundary edge becomes shared: the result is watertight by construction rather
// than by hope. A non-planar rim gets a geometrically approximate fill — the fan
// is not flat — but it does get closed, and closed is what makes a model
// printable and cuttable.
//
// ponytail: a centroid fan, not a planar triangulation. Upgrade path if fill
// quality on large non-planar holes ever matters: fit a plane per loop, project,
// and ear-clip with cut.earClip — at the cost of handling projection failures on
// exactly the rims a fan handles without complaint.
func Repair(m *stl.Mesh, eps float64) Result {
	res := Result{Before: meshcheck.Check(m, eps)}

	// One welder for the whole pass, so an id means the same point throughout.
	w := geom.NewWelder(eps)
	ids := make([][3]int, 0, len(m.Tris))
	kept := make([]stl.Tri, 0, len(m.Tris))
	for _, t := range m.Tris {
		id := [3]int{w.ID(t.A), w.ID(t.B), w.ID(t.C)}
		if id[0] == id[1] || id[1] == id[2] || id[2] == id[0] {
			res.DegenerateRemoved++
			continue
		}
		ids = append(ids, id)
		kept = append(kept, t)
	}
	m.Tris = kept

	// Directed edge use. A boundary edge is one traversed exactly once overall:
	// its opposite traversal is missing, which is what leaves the mesh open.
	type edge struct{ a, b int }
	use := make(map[edge]int, len(ids)*3)
	for _, id := range ids {
		for i := 0; i < 3; i++ {
			use[edge{id[i], id[(i+1)%3]}]++
		}
	}

	// next maps each boundary edge's start to its end, which is what walks a loop.
	// The rim of a hole runs opposite to the surface around it, so following these
	// directed edges traverses the rim consistently and a fill triangle built from
	// the reverse of each edge comes out facing the same way as its neighbours.
	next := make(map[int][]int)
	var starts []int
	counted := make(map[edge]bool, len(use))
	for e, n := range use {
		// Tally each undirected edge once, the way meshcheck does, so the two
		// counts add up to its OpenEdges rather than double-counting.
		key := e
		if key.a > key.b {
			key = edge{e.b, e.a}
		}
		if !counted[key] {
			counted[key] = true
			switch total := use[edge{key.a, key.b}] + use[edge{key.b, key.a}]; {
			case total == 1:
				res.BoundaryEdges++
			case total > 2:
				res.NonManifoldEdges++
			}
		}

		if n != 1 || use[edge{e.b, e.a}] != 0 {
			continue
		}
		if len(next[e.a]) == 0 {
			starts = append(starts, e.a)
		}
		next[e.a] = append(next[e.a], e.b)
	}
	// No early return when there is nothing to fill: a mesh whose only defect is
	// debris fused to it has no boundary edges at all, and returning here would
	// skip the one thing that would have repaired it.
	pos := w.Points()
	for _, loop := range boundaryLoops(next, starts) {
		if len(loop) < 3 {
			// Cannot be fanned. The edges stay open and After reports them, which
			// is better than emitting triangles that do not close anything.
			continue
		}
		m.Tris = append(m.Tris, fanLoop(loop, pos)...)
		res.TrianglesAdded += len(loop)
		res.HolesFilled++
	}

	dropEmptyShells(m, eps, &res)

	res.After = meshcheck.Check(m, eps)
	return res
}

// dropEmptyShells deletes connected surfaces that enclose no volume.
//
// It runs after hole filling, deliberately: a legitimately open shell has no
// enclosed volume until its holes are closed, and weighing it first would delete
// exactly the model repair exists to rescue.
//
// The rule is "encloses nothing", not "is small". A hollow model's internal void is
// a separate inside-out shell with a large negative volume, and deleting shells for
// being inverted would fill every hollow model solid. Two real bodies in one file
// are likewise both real. Only a shell whose signed volume cancels to nothing is
// debris, and removing one cannot change what prints.
func dropEmptyShells(m *stl.Mesh, eps float64, res *Result) {
	if len(m.Tris) == 0 {
		return
	}

	// Label surfaces, joining triangles only across edges shared by exactly two.
	// A non-manifold edge deliberately does not join: that is what leaves debris
	// fused to a body as its own surface rather than part of it.
	w := geom.NewWelder(eps)
	ids := make([][3]int, len(m.Tris))
	type edge struct{ a, b int }
	shared := make(map[edge][]int, len(m.Tris)*3)
	for i, t := range m.Tris {
		ids[i] = [3]int{w.ID(t.A), w.ID(t.B), w.ID(t.C)}
		for k := 0; k < 3; k++ {
			a, b := ids[i][k], ids[i][(k+1)%3]
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

	// Signed volume and surface area per shell. Area sets the tolerance: a shell
	// enclosing nothing has a volume of pure rounding error, which is bounded by
	// its own area times the welding tolerance. A real solid's volume exceeds that
	// by orders of magnitude, since eps is nanometres against a printable wall.
	vol := make(map[int]float64)
	area := make(map[int]float64)
	count := make(map[int]int)
	for i, t := range m.Tris {
		r := find(i)
		vol[r] += t.A.Cross(t.B).Dot(t.C) / 6
		area[r] += t.B.Sub(t.A).Cross(t.C.Sub(t.A)).Len() / 2
		count[r]++
	}

	empty := make(map[int]bool)
	for r, v := range vol {
		if math.Abs(v) <= eps*area[r] {
			empty[r] = true
			res.ShellsDropped++
			res.TrianglesRemoved += count[r]
		}
	}
	if len(empty) == 0 {
		return
	}
	// Never strip the mesh to nothing. A model that is entirely flat sheets is not
	// debris, it is a model this cannot help with, and deleting all of it would turn
	// a reported problem into an empty file.
	if res.TrianglesRemoved == len(m.Tris) {
		res.ShellsDropped, res.TrianglesRemoved = 0, 0
		return
	}

	kept := make([]stl.Tri, 0, len(m.Tris)-res.TrianglesRemoved)
	for i, t := range m.Tris {
		if !empty[find(i)] {
			kept = append(kept, t)
		}
	}
	m.Tris = kept
}

// boundaryLoops walks the directed boundary edges into simple closed loops,
// consuming every edge. next is modified.
//
// Every boundary vertex has equal in- and out-degree, because a vertex's boundary
// edges pair up around the arcs of its link, so every walk closes and every edge
// is reachable. Where several rims meet at one vertex it has several outgoing
// edges, and the walk must split there.
//
// That split is not cosmetic. A loop that visits a vertex twice, fanned from a
// centroid, uses the spoke to that vertex four times — and meshcheck counts any
// edge not used exactly twice as open. Emitting one self-touching ring therefore
// closes the hole it was given and opens two new ones in its place. Keeping a
// stack of the current walk and cutting a cycle out whenever it returns to a
// vertex already on it is what guarantees no loop repeats a vertex.
func boundaryLoops(next map[int][]int, starts []int) [][]int {
	var loops [][]int
	for _, s := range starts {
		for len(next[s]) > 0 {
			stack := []int{s}
			at := map[int]int{s: 0}
			cur := s
			for len(next[cur]) > 0 {
				outs := next[cur]
				nxt := outs[len(outs)-1]
				next[cur] = outs[:len(outs)-1]

				if i, seen := at[nxt]; seen {
					// The walk has come back to nxt, so stack[i:] plus the edge just
					// taken is a closed loop with no repeated vertex.
					loops = append(loops, append([]int(nil), stack[i:]...))
					for _, v := range stack[i:] {
						delete(at, v)
					}
					stack = stack[:i]
				}
				stack = append(stack, nxt)
				at[nxt] = len(stack) - 1
				cur = nxt
			}
		}
	}
	return loops
}

// fanLoop closes a boundary loop with a fan from its centroid.
//
// Each triangle is built from a boundary edge reversed, so it traverses that edge
// opposite to the way the rim does — which is exactly how the surface's existing
// triangles relate to their shared edges, and so gives the fill the same outward
// facing as its neighbours.
func fanLoop(loop []int, pos []geom.Vec3) []stl.Tri {
	var centre geom.Vec3
	for _, id := range loop {
		centre = centre.Add(pos[id])
	}
	centre = centre.Scale(1 / float64(len(loop)))

	out := make([]stl.Tri, 0, len(loop))
	for i := range loop {
		a := pos[loop[i]]
		b := pos[loop[(i+1)%len(loop)]]
		out = append(out, stl.Tri{A: centre, B: b, C: a})
	}
	return out
}
