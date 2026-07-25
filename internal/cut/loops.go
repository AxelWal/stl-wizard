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
