package cut

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"stl-cutter/internal/geom"
	"stl-cutter/internal/meshcheck"
	"stl-cutter/internal/stl"
)

// Result holds the two halves of a cut and anything the caller needs to know
// about how well it went.
type Result struct {
	// Part1 is the material outside the cutter, Part2 the material inside it —
	// the piece the plane's normal points toward.
	Part1, Part2 *stl.Mesh

	// OpenLoops counts cap boundaries that could not be closed, which means the
	// input mesh was not manifold. It is a defect count, never a hole count: a
	// single tangled boundary can raise it more than once.
	OpenLoops int
	// CapIncomplete counts cut faces whose cap could not be fully triangulated,
	// leaving a hole in the cut surface shared by both parts. Kept apart from
	// LeftoverIncomplete so a caller can tell which part is holed.
	CapIncomplete int
	// LeftoverIncomplete counts original triangles whose portion outside the
	// cutter could not be fully triangulated. Only part 1 is affected.
	LeftoverIncomplete int

	// Part1Check and Part2Check are meshcheck's verdict on the finished parts.
	// The zero value is a clean report, so an unset field means "verified good".
	Part1Check, Part2Check meshcheck.Report

	Warnings []string
}

// Watertight reports whether both parts closed cleanly. When false the parts are
// still returned, flagged, so the caller can decide — silently shipping a part
// with a hole in it would produce a model that looks right and fails to print.
func (r *Result) Watertight() bool {
	return r.OpenLoops == 0 && r.CapIncomplete == 0 && r.LeftoverIncomplete == 0 &&
		r.Part1Check.OK() && r.Part2Check.OK()
}

type triClass int

const (
	triStraddles triClass = iota
	triInside
	triOutside
)

// classifyTri is the fast path. On a large model nearly every triangle is wholly
// on one side, and recognising that avoids allocating polygon fragments for it.
func classifyTri(t stl.Tri, planes []Plane, eps float64) triClass {
	allInside := true
	for _, p := range planes {
		a := p.Classify(t.A, eps)
		b := p.Classify(t.B, eps)
		c := p.Classify(t.C, eps)
		// Outside any single half-space means outside the convex cutter.
		if a == Outside && b == Outside && c == Outside {
			return triOutside
		}
		if a == Outside || b == Outside || c == Outside {
			allInside = false
		}
	}
	if allInside {
		return triInside
	}
	return triStraddles
}

// taggedPoly is one polygon of the part-2 surface under construction, marked
// with where it came from.
//
// Fragments stay polygons all the way through and are fan-triangulated only at
// the end. Fanning between stages and clipping the triangles instead would leave
// each stage's fan diagonals crossing the next stage's cut, littering the
// fragment boundaries with vertices that exist for no geometric reason — and
// part 1 has to reproduce those boundaries exactly.
//
// isCap tells cap geometry from original surface. It cannot be recognised by
// position: an original face lying exactly on a cutter plane looks identical to
// a cap and must not be treated as one.
type taggedPoly struct {
	poly  Polygon
	isCap bool
}

// Split cuts m with the bounded plane s, returning the material outside the
// cutter as Part1 and the material inside it as Part2.
func Split(m *stl.Mesh, s Spec) (*Result, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if len(m.Tris) == 0 {
		return nil, errors.New("mesh has no triangles")
	}

	eps := m.Epsilon()
	planes := s.Planes()
	cutPlane := planes[0]
	res := &Result{}

	coplanar := 0
	for _, t := range m.Tris {
		if classifyTri(t, planes, eps) == triInside &&
			cutPlane.Classify(t.A, eps) == On &&
			cutPlane.Classify(t.B, eps) == On &&
			cutPlane.Classify(t.C, eps) == On {
			coplanar++
		}
	}

	// Build part 2 by clipping to one half-space at a time and capping as we go.
	// Capping after each plane is what makes the cutter's own edges exist: a cap
	// joins the surface that the next plane cuts, so where a cutter edge runs
	// through solid material its boundary is found as an ordinary on-plane edge.
	// Capping all five planes against the final fragment set finds only the edges
	// where a plane met the original surface, and misses every cutter edge that
	// lies strictly inside the model.
	current := make([]taggedPoly, 0, len(m.Tris))
	for _, t := range m.Tris {
		current = append(current, taggedPoly{poly: Polygon{t.A, t.B, t.C}})
	}

	for _, p := range planes {
		var insidePolys []Polygon
		var next []taggedPoly

		for _, cp := range current {
			// out is discarded here; part 1 is built separately, below.
			in, _ := splitPolygon(cp.poly, p, eps)
			if in != nil {
				next = append(next, taggedPoly{poly: in, isCap: cp.isCap})
				insidePolys = append(insidePolys, in)
			}
		}

		if len(next) == 0 {
			return nil, errors.New("the cutting plane and rectangle enclose no part of the model — nothing to cut")
		}

		// Every plane is capped, with no test for whether it removed anything. A
		// plane flush against a model face needs no cap, and gets none: its
		// on-plane edges cancel against the coplanar face's own in boundaryEdges,
		// leaving no loop to cap. Guarding on "this plane removed something" was
		// a global test over the whole mesh, so a plane that cut in one place and
		// lay flush in another passed it and got a spurious cap laid over the
		// flush face.
		tris, open, incomplete := triangulateFace(insidePolys, p, eps)
		res.OpenLoops += open
		res.CapIncomplete += incomplete
		for _, t := range tris {
			// t faces +p.N, which points into the region being kept, so the
			// kept side takes the reverse.
			r := t.Reversed()
			next = append(next, taggedPoly{poly: Polygon{r.A, r.B, r.C}, isCap: true})
		}
		current = next
	}

	part2 := &stl.Mesh{}
	for _, cp := range current {
		part2.Tris = append(part2.Tris, fanPolygon(cp.poly, eps)...)
	}

	// part 1 is the original surface outside the cutter, closed off by part 2's
	// cap surface facing the other way.
	//
	// The outside surface is taken a triangle at a time as t minus its own
	// clipped fragment, rather than by collecting the pieces each stage discards.
	// Discarded pieces are cut along whole planes, and a cutter plane goes on
	// cutting well past the bounded rectangle: the cube's top face would be split
	// along x = 3 for its full width when the cutter only reaches y in [3, 7].
	// Those cuts are invisible to the untouched neighbouring face across the
	// edge, and every one of them is an open edge.
	part1 := &stl.Mesh{}
	for _, t := range m.Tris {
		frag := Polygon{t.A, t.B, t.C}
		for _, p := range planes {
			frag, _ = splitPolygon(frag, p, eps)
			if frag == nil {
				break
			}
		}
		if frag == nil {
			// Nothing of this triangle is inside the cutter, so it crosses over
			// unchanged. Bit for bit: that identity is what makes the cut bounded,
			// and clipping a triangle against planes it never meets would still
			// perturb its vertices.
			part1.Tris = append(part1.Tris, t)
			continue
		}
		tris, ok := subtractFragment(t, frag, eps)
		if !ok {
			res.LeftoverIncomplete++
		}
		part1.Tris = append(part1.Tris, tris...)
	}
	if len(part1.Tris) == 0 {
		return nil, errors.New("the cutting rectangle encloses the entire model — nothing would be left behind")
	}

	// Reversed, part 2's cap surface is exactly part 1's boundary against the
	// cutter. It is taken from part 2's own triangles so the two agree edge for
	// edge.
	for _, cp := range current {
		if !cp.isCap {
			continue
		}
		for _, t := range fanPolygon(cp.poly, eps) {
			part1.Tris = append(part1.Tris, t.Reversed())
		}
	}

	// A plane flush with a model face can leave a "part" that is a sheet of
	// surface with no interior — the cube's top face cut at z = 10 gives two
	// triangles and a nominal volume from the tetrahedra to the origin. Refuse it
	// outright rather than hand back something that is not a solid at all. This
	// comes before the watertightness check so the clearer message wins.
	whole := m.Volume()
	v1, v2 := part1.Volume(), part2.Volume()
	if v1 <= 0 || v2 <= 0 || math.Abs(v1) <= math.Abs(whole)*1e-9 || math.Abs(v2) <= math.Abs(whole)*1e-9 {
		return nil, errors.New("the cutting plane only grazes the model's surface and removes nothing — move it into the material")
	}

	res.Part1, res.Part2 = part1, part2

	if res.OpenLoops > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"%d cut boundary %s could not be closed — the model is not watertight where it was cut, so the parts may have holes",
			res.OpenLoops, plural(res.OpenLoops, "loop", "loops")))
	}
	if res.CapIncomplete > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"%d cut %s could not be fully triangulated, so both parts are holed where they meet",
			res.CapIncomplete, plural(res.CapIncomplete, "face", "faces")))
	}
	if res.LeftoverIncomplete > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"%d original %s could not be fully rebuilt around the cut, so the remaining part is holed",
			res.LeftoverIncomplete, plural(res.LeftoverIncomplete, "triangle", "triangles")))
	}
	if coplanar > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"the cutting plane lies flat against %d model %s; move it slightly to get a predictable result",
			coplanar, plural(coplanar, "face", "faces")))
	}

	// Volume conservation is cheap to verify and catches whole classes of
	// winding and capping bugs, so it is checked in production too, not only in
	// tests.
	if got := v1 + v2; math.Abs(got-whole) > math.Max(1e-6, math.Abs(whole)*1e-6) {
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"the parts total %.4g in volume but the original was %.4g — the cut may be flawed", got, whole))
	}

	// Verify what we are about to return. Every earlier signal is indirect —
	// OpenLoops sees only loop assembly, CapIncomplete only ear clipping, and
	// volume conservation is blind to a spurious cap because it cancels between
	// the two parts. Checking the finished meshes is the one test that cannot be
	// fooled by a defect it was not designed to anticipate.
	if rep := meshcheck.Check(part1, eps); !rep.OK() {
		res.Part1Check = rep
		res.Warnings = append(res.Warnings, fmt.Sprintf("the remaining part is not a closed solid: %s", rep))
	}
	if rep := meshcheck.Check(part2, eps); !rep.OK() {
		res.Part2Check = rep
		res.Warnings = append(res.Warnings, fmt.Sprintf("the cut-off part is not a closed solid: %s", rep))
	}

	return res, nil
}

// fanPolygon triangulates a convex polygon from its first vertex. It first drops
// vertices that repeat their neighbour within eps, then drops any fan triangle a
// surviving repeat still makes degenerate.
//
// It applies no minimum area. These are the finished
// fragments of the two parts, and their fans tile the surface exactly: culling
// any one of them punches a hole. A sliver here is the honest shape of a cut
// that grazed a vertex, and shipping it beats shipping a part that is not
// closed. A repeated vertex is different — the triangle has no boundary at all,
// and its two remaining edges are one edge traversed both ways, which cancel.
func fanPolygon(poly Polygon, eps float64) []stl.Tri {
	poly = dedupe(poly, eps)
	if len(poly) < 3 {
		return nil
	}
	out := make([]stl.Tri, 0, len(poly)-2)
	for i := 1; i+1 < len(poly); i++ {
		t := stl.Tri{A: poly[0], B: poly[i], C: poly[i+1]}
		if t.A == t.B || t.B == t.C || t.C == t.A {
			continue
		}
		out = append(out, t)
	}
	return out
}

// paramOnSegment reports whether p lies on segment ab within eps, and where.
func paramOnSegment(p, a, b geom.Vec3, eps float64) (float64, bool) {
	ab := b.Sub(a)
	l2 := ab.Dot(ab)
	// A segment shorter than eps has no direction worth projecting onto, so treat
	// it as the single point a.
	if l2 <= eps*eps {
		return 0, p.Sub(a).Len() <= eps
	}
	s := p.Sub(a).Dot(ab) / l2
	if s < 0 {
		s = 0
	} else if s > 1 {
		s = 1
	}
	return s, p.Sub(a.Add(ab.Scale(s))).Len() <= eps
}

// boundaryPos locates p on the triangle's boundary as a single parameter in
// [0, 3): edge index plus the fraction along it. A corner reports as the exact
// integer position of the edge it starts, so the same corner never gets two
// different parameters depending on which edge found it.
func boundaryPos(p geom.Vec3, corners [3]geom.Vec3, eps float64) (float64, bool) {
	for k := 0; k < 3; k++ {
		s, ok := paramOnSegment(p, corners[k], corners[(k+1)%3], eps)
		if !ok {
			continue
		}
		if s >= 1 {
			return float64((k + 1) % 3), true
		}
		return float64(k) + s, true
	}
	return 0, false
}

// cornersBetween returns the triangle corners lying strictly between boundary
// positions from and to, walking forward in the triangle's own winding.
func cornersBetween(corners [3]geom.Vec3, from, to float64) []geom.Vec3 {
	span := math.Mod(to-from+3, 3)
	if span <= 0 {
		// The arc leaves and re-enters at the same point, so the walk is the whole
		// boundary.
		span = 3
	}
	type step struct {
		off float64
		v   geom.Vec3
	}
	var steps []step
	for k := 0; k < 3; k++ {
		off := math.Mod(float64(k)-from+3, 3)
		if off > 1e-12 && off < span-1e-12 {
			steps = append(steps, step{off, corners[k]})
		}
	}
	sort.Slice(steps, func(i, j int) bool { return steps[i].off < steps[j].off })
	out := make([]geom.Vec3, len(steps))
	for i, s := range steps {
		out[i] = s.v
	}
	return out
}

// subtractFragment triangulates t with frag removed, where frag is t's own
// convex fragment inside the cutter. Returned triangles face t's normal, and
// ok reports whether every region was triangulated completely.
//
// The point of doing it this way rather than by clipping is that no vertex is
// introduced on t's boundary except where frag itself meets it. A neighbouring
// triangle's fragment meets their shared edge along the same segment, so both
// sides of the edge come out subdivided identically and the surface stays
// closed.
func subtractFragment(t stl.Tri, frag Polygon, eps float64) (tris []stl.Tri, ok bool) {
	corners := [3]geom.Vec3{t.A, t.B, t.C}
	n := t.Normal()
	origin := t.A
	u := t.B.Sub(t.A).Unit()
	v := n.Cross(u)

	pos := make([]float64, len(frag))
	onEdge := make([]bool, len(frag))
	anyOn := false
	for i, p := range frag {
		pos[i], onEdge[i] = boundaryPos(p, corners, eps)
		anyOn = anyOn || onEdge[i]
	}

	// frag sits strictly inside t, so what is left is an annulus: t with one
	// hole. That is the shape the cap machinery already handles.
	if !anyOn {
		groups := groupLoops([]faceLoop{
			projectLoop(Polygon{t.A, t.B, t.C}, origin, u, v),
			projectLoop(frag, origin, u, v),
		})
		ok = true
		for _, g := range groups {
			out, complete := clipLoop(bridgeHoles(g.Outer, g.Holes))
			tris = append(tris, out...)
			ok = ok && complete
		}
		return tris, ok
	}

	// Otherwise frag touches t's boundary, possibly several times, and each
	// stretch of it that runs through t's interior cuts off one region of what is
	// left. Walking the fragment backwards and then forward along the triangle's
	// own boundary traces that region.
	ok = true
	for st := range frag {
		if !onEdge[st] {
			continue
		}
		end := st + 1
		for ; end < st+len(frag); end++ {
			if onEdge[end%len(frag)] {
				break
			}
		}
		end %= len(frag)
		if (end-st+len(frag))%len(frag) == 1 {
			// A single span between two boundary points. It is part of the
			// triangle's own edge, and bounds nothing, if its midpoint is on the
			// boundary too.
			mid := frag[st].Add(frag[end]).Scale(0.5)
			if _, onB := boundaryPos(mid, corners, eps); onB {
				continue
			}
		}

		loop := make(Polygon, 0, len(frag)+3)
		for i := end; ; i = (i - 1 + len(frag)) % len(frag) {
			loop = append(loop, frag[i])
			if i == st {
				break
			}
		}
		loop = append(loop, cornersBetween(corners, pos[st], pos[end])...)
		loop = dedupe(loop, eps)
		if len(loop) < 3 {
			continue
		}
		out, complete := clipLoop(projectLoop(loop, origin, u, v))
		tris = append(tris, out...)
		ok = ok && complete
	}
	// No interior stretch at all means frag covers t, so nothing is left over.
	return tris, ok
}

// dedupe drops vertices that repeat their predecessor, cyclically, within eps.
// A triangle corner can land within rounding of the point where the fragment
// leaves the boundary, and the repeat would become a triangle with no boundary
// of its own.
func dedupe(poly Polygon, eps float64) Polygon {
	out := make(Polygon, 0, len(poly))
	for i, p := range poly {
		if len(out) > 0 && p.Sub(out[len(out)-1]).Len() <= eps {
			continue
		}
		if i == len(poly)-1 && len(out) > 0 && p.Sub(out[0]).Len() <= eps {
			continue
		}
		out = append(out, p)
	}
	return out
}

// clipLoop ear clips a single loop, normalising it counter-clockwise first so
// the triangles come out facing the plane normal.
func clipLoop(l faceLoop) (tris []stl.Tri, ok bool) {
	if len(l) < 3 {
		return nil, false
	}
	if signedArea2(l) < 0 {
		l = reverseLoop(l)
	}
	idxTris, ok := earClip(l)
	for _, tr := range idxTris {
		tris = append(tris, stl.Tri{A: l[tr[0]].P3, B: l[tr[1]].P3, C: l[tr[2]].P3})
	}
	return tris, ok
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
