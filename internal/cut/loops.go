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

// assembleLoops chains undirected edges into closed loops.
//
// open counts chains that could not be closed, which is how non-manifold input is
// detected. It is a defect count, not a hole count — a caller must not report it
// as "N holes". The two coincide for disjoint simple chains, but a tangled
// boundary can raise it more than once for a single region.
//
// A vertex where four or more boundary edges meet is a pinch point, where the
// cross-section touches itself. That is two rings meeting at a point, and the walk
// splits it accordingly: on reaching a vertex it has already passed, the sub-path
// from that earlier visit is emitted as its own ring and the walk carries on from
// there. Without the split the walk would return a single self-intersecting
// polygon and report success, and the malformed ring would not surface until it
// had already corrupted a cap.
//
// The split also rescues a ring that a whisker attached at one vertex would
// otherwise have consumed. A chain that enters a ring at one vertex and leaves at
// another can still swallow it; such input is non-manifold and open is always
// non-zero when it happens, so the defect is reported even though the ring is lost.
func assembleLoops(edges [][2]int, w *geom.Welder) (loops []Polygon, open int) {
	type link struct{ to, edge int }

	adj := make(map[int][]link, len(edges)*2)
	for ei, e := range edges {
		adj[e[0]] = append(adj[e[0]], link{to: e[1], edge: ei})
		adj[e[1]] = append(adj[e[1]], link{to: e[0], edge: ei})
	}

	// Map iteration order is randomised, so walk vertices in a fixed order to keep
	// the output stable between runs. Odd-degree vertices go first: those are the
	// true endpoints of an open chain, and starting there walks such a chain once
	// end to end rather than twice outward from some interior vertex, which would
	// report one hole as two.
	var odd, even []int
	for v, ls := range adj {
		if len(ls)%2 == 1 {
			odd = append(odd, v)
		} else {
			even = append(even, v)
		}
	}
	sort.Ints(odd)
	sort.Ints(even)
	starts := append(append([]int(nil), odd...), even...)

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

	toPolygon := func(ids []int) Polygon {
		poly := make(Polygon, len(ids))
		for i, id := range ids {
			poly[i] = pts[id]
		}
		return poly
	}

	emit := func(ids []int) {
		if len(ids) >= 3 {
			loops = append(loops, toPolygon(ids))
		} else {
			// Fewer than three distinct vertices cannot bound any area.
			open++
		}
	}

	for _, start := range starts {
		for {
			if _, ok := nextUnused(start); !ok {
				break
			}

			path := []int{start}
			posOf := map[int]int{start: 0}
			cur := start

			for {
				step, ok := nextUnused(cur)
				if !ok {
					// Dead end: what remains of path is an unclosed chain.
					open++
					break
				}
				used[step.edge] = true
				cur = step.to

				if cur == start {
					emit(path)
					break
				}
				if at, seen := posOf[cur]; seen {
					// Pinch point. Emit the ring that closes here, then carry on
					// from this vertex with the rest of the path intact.
					emit(path[at:])
					for _, v := range path[at+1:] {
						delete(posOf, v)
					}
					path = path[:at+1]
					continue
				}
				posOf[cur] = len(path)
				path = append(path, cur)
			}
		}
	}
	return loops, open
}
