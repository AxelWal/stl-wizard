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
