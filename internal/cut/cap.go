package cut

import (
	"math"

	"stl-wizard/internal/geom"
	"stl-wizard/internal/stl"
)

// planeBasis returns a point on p together with a right-handed orthonormal
// in-plane basis satisfying u x v == p.N. That identity is what makes a
// counter-clockwise 2D triangle come back out facing +p.N.
func planeBasis(p Plane) (origin, u, v geom.Vec3) {
	n := p.N.Unit()
	origin = n.Scale(p.D)

	// Seed with the axis least aligned to n so the cross product stays well
	// conditioned.
	seed := geom.Vec3{1, 0, 0}
	if math.Abs(n[0]) > math.Abs(n[1]) {
		seed = geom.Vec3{0, 1, 0}
	}
	u = seed.Sub(n.Scale(seed.Dot(n))).Unit()
	return origin, u, n.Cross(u)
}

// triangulateFace builds the cap that closes polys on plane p. Returned
// triangles face +p.N.
//
// open counts boundary chains that could not be closed, which happens when the
// input mesh is not manifold. incomplete counts regions ear clipping could not
// fully triangulate. Both are reported rather than swallowed: a cap with a hole
// in it produces a part that looks fine on screen and fails to print.
func triangulateFace(polys []Polygon, p Plane, eps float64) (tris []stl.Tri, open, incomplete int) {
	w := geom.NewWelder(eps)
	edges := boundaryEdges(polys, p, w, eps)
	if len(edges) == 0 {
		return nil, 0, 0
	}

	loops, open := assembleLoops(edges, w)
	if len(loops) == 0 {
		return nil, open, 0
	}

	origin, u, v := planeBasis(p)
	projected := make([]faceLoop, 0, len(loops))
	for _, l := range loops {
		projected = append(projected, projectLoop(l, origin, u, v))
	}

	for _, g := range groupLoops(projected) {
		merged := bridgeHoles(g.Outer, g.Holes)
		idxTris, ok := earClip(merged)
		if !ok {
			incomplete++
		}
		// Every triangle earClip returns is emitted, however small. earClip
		// already guarantees no degenerate output — isEar rejects a corner unless
		// cross2 > 0 and the final triangle is guarded the same way — and it
		// returns a triangulation that is complete or nothing at all. Culling by
		// area afterwards can therefore only remove a legitimate member of a
		// complete triangulation, and doing so drops that triangle's two boundary
		// edges from the cap while the clipped surface still carries them: the
		// part silently stops being watertight. Under an oblique plane basis a
		// perfectly legitimate sliver falls below eps*eps, which is exactly how
		// that used to happen.
		for _, tr := range idxTris {
			tris = append(tris, stl.Tri{A: merged[tr[0]].P3, B: merged[tr[1]].P3, C: merged[tr[2]].P3})
		}
	}
	return tris, open, incomplete
}
