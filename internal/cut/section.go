package cut

import (
	"math"

	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

// Section describes where a plane crosses a mesh.
//
// Loops is how many separate closed contours the plane cuts — the "cutting lines" a
// user sees. One means the plane severs the part in a single place, which is both what
// makes two watertight bodies and what a person would choose by eye. Two or more means
// it is passing through several limbs at once.
//
// Area is the total cross-section. Between two planes that both make a single loop, the
// smaller area is the narrower waist, which is the easier cut and the stronger join.
type Section struct {
	Loops int
	Area  float64
}

// SectionScore measures where a plane crosses a mesh.
//
// This is what makes auto-splitting look at the model rather than only at its bounding
// box. Halving a box knows nothing about whether the plane passes through an ankle or
// through the widest part of a foot; this does.
//
// The area is accumulated straight from the crossing segments by the shoelace formula,
// so it needs no loop assembly and holes subtract themselves. Only the loop count needs
// the segments joined up.
func SectionScore(m *stl.Mesh, p Plane, eps float64) Section {
	origin, u, v := planeBasis(p)
	n := p.N.Unit()

	w := geom.NewWelder(eps)
	var segs [][2]int
	var twiceArea float64

	for _, t := range m.Tris {
		verts := [3]geom.Vec3{t.A, t.B, t.C}
		var d [3]float64
		pos, neg := 0, 0
		for i, x := range verts {
			d[i] = p.Dist(x)
			if d[i] > eps {
				pos++
			} else if d[i] < -eps {
				neg++
			}
		}
		if pos == 0 || neg == 0 {
			continue // wholly on one side, or lying in the plane
		}

		// The two points where the plane crosses this triangle's edges.
		var hits []geom.Vec3
		for i := 0; i < 3; i++ {
			j := (i + 1) % 3
			if (d[i] > eps && d[j] < -eps) || (d[i] < -eps && d[j] > eps) {
				f := d[i] / (d[i] - d[j])
				hits = append(hits, verts[i].Add(verts[j].Sub(verts[i]).Scale(f)))
			}
		}
		if len(hits) != 2 {
			continue // a vertex sits on the plane; the neighbouring triangles carry it
		}

		// Orient the segment so every one runs the same way round the contour: the
		// material is consistently on one side. Without this the shoelace terms cancel
		// against each other and the area comes out near zero.
		dir := n.Cross(t.Normal())
		if hits[1].Sub(hits[0]).Dot(dir) < 0 {
			hits[0], hits[1] = hits[1], hits[0]
		}

		ax, ay := hits[0].Sub(origin).Dot(u), hits[0].Sub(origin).Dot(v)
		bx, by := hits[1].Sub(origin).Dot(u), hits[1].Sub(origin).Dot(v)
		twiceArea += ax*by - bx*ay

		segs = append(segs, [2]int{w.ID(hits[0]), w.ID(hits[1])})
	}

	return Section{Loops: countLoops(segs, w.Len()), Area: math.Abs(twiceArea) / 2}
}

// countLoops counts the connected components of the crossing segments. On a closed mesh
// each component is a closed contour, so this is how many separate places the plane cuts.
func countLoops(segs [][2]int, points int) int {
	if len(segs) == 0 {
		return 0
	}
	parent := make([]int, points)
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
	seen := make(map[int]bool, points)
	for _, s := range segs {
		seen[s[0]], seen[s[1]] = true, true
		if ra, rb := find(s[0]), find(s[1]); ra != rb {
			parent[ra] = rb
		}
	}
	roots := make(map[int]bool, 8)
	for id := range seen {
		roots[find(id)] = true
	}
	return len(roots)
}

// bestCutSamples is how many positions are tried along the axis being divided. Twelve
// is enough to find a waist without making planning cost more than the cuts.
const bestCutSamples = 12

// BestCut chooses where to divide a part so it fits the bed.
//
// It picks the axis that overflows by the most, then searches the positions at which
// both halves would fit — for an axis of length L on a bed of B that is p in [L-B, B] —
// and takes the one crossing the model in the fewest places, breaking ties on the
// smallest cross-section.
//
// That is the difference between arithmetic and looking at the model. Halving a bounding
// box knows nothing about whether the plane passes through an ankle or through the widest
// part of a foot; the fewest-loops rule prefers a plane that severs the part in one place,
// and the smallest-area rule then prefers the narrowest such place. Neither privileges the
// midpoint, so a waist a third of the way along beats a fat middle.
//
// ok is false when the part already fits or no position would leave both halves fitting.
//
// The returned rectangle is generous rather than bounded to the fragment, so a cut cannot
// graze an edge and shed a sliver. That makes it safe to apply to the fragment it was
// chosen for, and unsafe to replay against every leaf a plane crosses: it would strike
// siblings. A caller must therefore record which fragment each cut belongs to, which is
// what planToFit does by naming it — see
// docs/superpowers/specs/2026-07-26-fit-strategy-design.md.
func BestCut(m *stl.Mesh, bed Bed) (spec Spec, s Section, ok bool) {
	if err := bed.Valid(); err != nil || len(m.Tris) == 0 {
		return Spec{}, Section{}, false
	}
	box := m.BBox()
	if bed.Fits(box) {
		return Spec{}, Section{}, false
	}

	size := box.Size()
	limits := [3]float64{bed.X, bed.Y, bed.Z}
	axis, worst := -1, 0.0
	for i := 0; i < 3; i++ {
		if over := size[i] - limits[i]; over > worst {
			axis, worst = i, over
		}
	}
	if axis < 0 {
		return Spec{}, Section{}, false
	}

	// Positions where both halves fit along this axis. When the part is more than twice
	// the bed no single cut can manage it, so fall back to the midpoint and let the next
	// pass divide again.
	lo := box.Min[axis] + (size[axis] - limits[axis])
	hi := box.Min[axis] + limits[axis]
	if lo > hi {
		mid := box.Min[axis] + size[axis]/2
		lo, hi = mid, mid
	}

	normal := geom.Vec3{}
	normal[axis] = 1
	w, h := sectionExtent(size, axis)

	eps := m.Epsilon()
	best := Section{Loops: math.MaxInt}
	bestAt := lo
	for i := 0; i < bestCutSamples; i++ {
		at := lo
		if bestCutSamples > 1 && hi > lo {
			at = lo + (hi-lo)*float64(i)/float64(bestCutSamples-1)
		}
		centre := boxCentre(box)
		centre[axis] = at
		sc := SectionScore(m, planeThrough(centre, normal), eps)
		if sc.Loops == 0 {
			continue // the plane misses the geometry entirely
		}
		if sc.Loops < best.Loops || (sc.Loops == best.Loops && sc.Area < best.Area) {
			best, bestAt = sc, at
		}
		if hi == lo {
			break
		}
	}
	if best.Loops == math.MaxInt {
		return Spec{}, Section{}, false
	}

	centre := boxCentre(box)
	centre[axis] = bestAt
	return SpecFromNormal(centre, normal, w, h), best, true
}

// sectionExtent is the rectangle a cut spans, comfortably clear of this fragment's own
// cross-section so the cut cannot fall short of an edge and shed a sliver.
//
// Shrinking it below the cross-section stops the cut severing the fragment at all, and the
// part comes back whole and still above the bed. Only the caller knowing which fragment the
// cut belongs to makes room to be generous here; see the note on BestCut.
func sectionExtent(size geom.Vec3, axis int) (w, h float64) {
	var other []float64
	for i := 0; i < 3; i++ {
		if i != axis {
			other = append(other, size[i])
		}
	}
	// Safe for a single cut applied to a known fragment; NOT safe for replaying a plan by
	// applying each plane to every leaf it crosses, because it would strike siblings.
	return other[0] * 2, other[1] * 2
}

func planeThrough(origin, normal geom.Vec3) Plane {
	n := normal.Unit()
	return Plane{N: n, D: n.Dot(origin)}
}

func boxCentre(b stl.BBox) geom.Vec3 {
	return geom.Vec3{
		(b.Min[0] + b.Max[0]) / 2,
		(b.Min[1] + b.Max[1]) / 2,
		(b.Min[2] + b.Max[2]) / 2,
	}
}
