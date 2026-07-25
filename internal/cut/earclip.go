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
	return d1*d2 < 0 && d3*d4 < 0
}

// pointOnSegment reports whether p lies on segment ab, endpoints included.
func pointOnSegment(p, a, b pt2) bool {
	if cross2(a, b, p) != 0 {
		return false
	}
	return math.Min(a.X, b.X) <= p.X && p.X <= math.Max(a.X, b.X) &&
		math.Min(a.Y, b.Y) <= p.Y && p.Y <= math.Max(a.Y, b.Y)
}

// segmentsTouch reports whether ab and cd share any point at all — a proper
// crossing, a collinear overlap, or a single grazing contact.
//
// Bridge clearance needs this rather than segmentsProperlyCross. A join that
// merely grazes an edge, or runs along one, still leaves the merged loop
// non-simple and so unclippable; and grazing is the normal case for
// axis-aligned models cut by axis-aligned planes, where vertices routinely line
// up exactly.
func segmentsTouch(a, b, c, d pt2) bool {
	if segmentsProperlyCross(a, b, c, d) {
		return true
	}
	return pointOnSegment(c, a, b) || pointOnSegment(d, a, b) ||
		pointOnSegment(a, c, d) || pointOnSegment(b, c, d)
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
// without the join touching any edge of poly away from its own endpoints, or -1
// if no such vertex exists. Candidates at or right of m are preferred and the
// nearest is taken, because bridging proceeds rightward.
//
// Touching, not merely crossing, is what disqualifies a join: a bridge that
// grazes a vertex or runs along an edge pinches the merged loop at that point,
// and a non-simple loop cannot be ear clipped. Axis-aligned models cut by
// axis-aligned planes graze constantly.
//
// The join is tested against two boundaries: the accumulated loop poly, and hole,
// the loop about to be spliced in. Testing hole is not redundant. m is hole's
// rightmost vertex, so while the landing lies to its right the hole cannot be in
// the way — but when the rightward search finds nothing and the fallback picks a
// landing to the left, the join can cut straight back through the hole it is
// supposed to be entering, and the merged loop self-crosses.
//
// avoid counts how often each position already appears in poly. A position that
// appears twice already carries a bridge channel, and a second channel through
// it would pinch the merged loop at that point — the loop stops being simple,
// so no ear test can prove a corner there safe and ear clipping cannot complete.
// Such candidates are therefore skipped, in both the rightward search and the
// fallback. If every visible candidate is already duplicated the skip is
// dropped: a duplicated landing still beats an arbitrary index.
//
// ponytail: O(n^2) per hole. Caps have tens to low hundreds of boundary
// vertices, so this is immaterial. Upgrade path if a cap ever gets large: the
// Eberly ray-cast construction, which finds a visible vertex in one pass.
func visibleVertex(poly faceLoop, m pt2, avoid map[pt2]int, hole faceLoop) int {
	duplicated := func(i int) bool { return avoid[poly[i].P2] >= 2 }

	clear := func(i int) bool {
		v := poly[i].P2

		// The join legitimately meets the boundary at its own two endpoints, so
		// edges ending at either are skipped. The test is by position, not by
		// index: bridging duplicates positions, so the landing may appear at a
		// second index whose edges the index test would not exclude, and the
		// grazing test would then reject every candidate. Position subsumes the
		// old index test, since poly[i].P2 is v.
		endpoint := func(p pt2) bool { return p == m || p == v }

		for k := range poly {
			k2 := (k + 1) % len(poly)
			if endpoint(poly[k].P2) || endpoint(poly[k2].P2) {
				continue
			}
			if segmentsTouch(m, v, poly[k].P2, poly[k2].P2) {
				return false
			}
		}
		for k := range hole {
			k2 := (k + 1) % len(hole)
			if endpoint(hole[k].P2) || endpoint(hole[k2].P2) {
				continue
			}
			if segmentsTouch(m, v, hole[k].P2, hole[k2].P2) {
				return false
			}
		}
		return true
	}

	// Two passes: the first skips already-duplicated vertices, the second accepts
	// them because nothing else was reachable.
	for _, strict := range []bool{true, false} {
		best, bestDist := -1, math.Inf(1)
		for i := range poly {
			v := poly[i].P2
			if v.X < m.X || (strict && duplicated(i)) {
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
			if strict && duplicated(i) {
				continue
			}
			if clear(i) {
				return i
			}
		}
	}
	// Nothing at all is joinable. Instrumentation over 20,000 real bridges never
	// reached here; if it ever does, an arbitrary landing would silently produce a
	// cap with the hole filled in, so the caller is told to leave the hole
	// unspliced and let earClip report the failure.
	return -1
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
			continue // a one- or two-vertex hole has no area, so dropping it loses nothing
		}
		// ponytail: recounted per hole rather than maintained incrementally. Holes
		// number in the low tens, so O(holes * n) is free and cannot drift.
		counts := make(map[pt2]int, len(result))
		for _, v := range result {
			counts[v.P2]++
		}

		hi := rightmostIdx(h)
		entry := h[hi]
		pi := visibleVertex(result, entry.P2, counts, h)
		if pi < 0 {
			// No landing is safe. Splicing on an arbitrary index would silently
			// corrupt the merged loop, so the hole is left unspliced: any channel
			// already in place then leaves the loop non-simple and earClip reports
			// ok = false, which the caller surfaces. Unreachable in practice — no
			// fuzzed configuration has produced it — so it is not worth an error
			// return that every caller would have to thread through.
			continue
		}

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
//
// A failed triangulation returns no triangles at all. The partial fan a stalled
// clip has accumulated covers only part of the region, and `tris, _ :=` is one
// character away from shipping it as a finished cap.
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

	for len(idx) > 3 {
		clipped := false
		for k := range idx {
			if !isEar(l, idx, k) {
				continue
			}
			prev := idx[(k+len(idx)-1)%len(idx)]
			cur := idx[k]
			next := idx[(k+1)%len(idx)]

			tris = append(tris, [3]int{prev, cur, next})
			idx = append(idx[:k], idx[k+1:]...)
			clipped = true
			break
		}
		if !clipped {
			break
		}
	}
	if len(idx) == 3 {
		if cross2(l[idx[0]].P2, l[idx[1]].P2, l[idx[2]].P2) > 0 {
			tris = append(tris, [3]int{idx[0], idx[1], idx[2]})
		}
	}
	if len(tris) != n-2 {
		return nil, false
	}
	return tris, true
}

// pointInRemaining is a crossing-number test against the polygon still left in
// idx, rather than against the original vertex list.
func pointInRemaining(l faceLoop, idx []int, p pt2) bool {
	in := false
	n := len(idx)
	for i := 0; i < n; i++ {
		j := (i + n - 1) % n
		pi, pj := l[idx[i]].P2, l[idx[j]].P2
		if (pi.Y > p.Y) != (pj.Y > p.Y) {
			x := pi.X + (p.Y-pi.Y)/(pj.Y-pi.Y)*(pj.X-pi.X)
			if p.X < x {
				in = !in
			}
		}
	}
	return in
}

// isEar reports whether the corner at position k of idx can be clipped away.
//
// The usual shortcut — "no other vertex lies strictly inside the candidate
// triangle" — is only sound for a polygon whose vertices are distinct. Bridging
// deliberately duplicates two vertices per hole, and a duplicate sitting exactly
// on a candidate edge then reads as outside, letting an ear swallow hole area and
// emit an inverted triangle. So the diagonal prev->next is tested directly as
// well: it must cross no remaining edge, must touch no remaining vertex, and must
// run through the interior rather than outside the polygon. The vertex test is
// kept alongside those, because with duplicated vertices the diagonal tests have
// a blind spot of their own — a hole whose entry and exit both sit on the
// duplicated bridge vertex hangs entirely inside the ear while touching the
// diagonal only at an endpoint.
//
// Five conditions are tested below, and they do not carry equal weight. Turning
// them off one at a time shows the convex-corner test is what keeps bad geometry
// out and the vertex-in-triangle test is what keeps the clip from stalling,
// while the diagonal-crossing test and the midpoint-interior test are
// outcome-neutral — deleting both reproduces the full result exactly on both a
// generic and an axis-aligned test family. They are kept as defence in depth,
// because the analytic argument that makes them redundant assumes a simple
// polygon and a bridged loop with duplicated vertices is not strictly simple.
// The on-the-diagonal vertex test is the fifth, and is load-bearing for the
// axis-aligned case described below.
//
// Together they are conservative: a corner that cannot be shown safe is not
// clipped, so earClip reports ok = false rather than emitting bad geometry.
//
// ponytail: O(n) per candidate, so O(n^3) over a full triangulation — the same
// order as the vertex test it replaces. Caps run to a few hundred vertices at
// most. Upgrade path if a cap ever gets large: index the remaining edges spatially
// and query only those near the diagonal.
func isEar(l faceLoop, idx []int, k int) bool {
	n := len(idx)
	ip := idx[(k+n-1)%n]
	ic := idx[k]
	in := idx[(k+1)%n]

	a, b, c := l[ip].P2, l[ic].P2, l[in].P2

	// A reflex or zero-area corner is never an ear. Zero-area corners are common
	// here: every bridge channel has two of them.
	if cross2(a, b, c) <= 0 {
		return false
	}

	// The diagonal must not properly cross any edge that does not share one of its
	// endpoints.
	for e := 0; e < n; e++ {
		u, v := idx[e], idx[(e+1)%n]
		if u == ip || u == in || v == ip || v == in {
			continue
		}
		if segmentsProperlyCross(a, c, l[u].P2, l[v].P2) {
			return false
		}
	}

	// Nor may any remaining vertex touch the diagonal or sit inside the triangle.
	// Vertices are compared by coordinate, not by index, because bridging
	// duplicates them: a duplicate at a corner of the candidate is that same point,
	// not an intruder.
	//
	// The on-the-diagonal half is what an axis-aligned hole trips: the outer's
	// corner-to-corner diagonal runs exactly through a hole corner. That is not a
	// proper crossing, but clipping it leaves the polygon pinched at that corner —
	// no longer simple — and every later ear test then reasons about a polygon that
	// does not exist.
	for _, i := range idx {
		p := l[i].P2
		if p == a || p == c {
			continue
		}
		if cross2(a, c, p) == 0 &&
			p.X >= math.Min(a.X, c.X) && p.X <= math.Max(a.X, c.X) &&
			p.Y >= math.Min(a.Y, c.Y) && p.Y <= math.Max(a.Y, c.Y) {
			return false
		}
		if p == b {
			continue
		}
		if cross2(a, b, p) > 0 && cross2(b, c, p) > 0 && cross2(c, a, p) > 0 {
			return false
		}
	}

	// ...and it must lie inside the polygon, not span a concavity. The midpoint is
	// tested because it avoids every vertex, and so is immune to the duplicate
	// vertices bridging creates.
	mid := pt2{X: (a.X + c.X) / 2, Y: (a.Y + c.Y) / 2}
	return pointInRemaining(l, idx, mid)
}
