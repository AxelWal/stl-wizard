package cut

import (
	"math"

	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

// cutFace recovers a finished part's cut face: the triangles lying on plane p,
// and that region expressed as 2D loops with their holes.
//
// This runs on a COMPLETED part rather than during the cut. Split caps plane by
// plane, so the cut plane's cap is built before the four rectangle-side planes
// trim it — planning pins on the intermediate cap would put them in material
// those side planes later remove. By the time a part is finished, its triangles
// on p are exactly the face that survived.
//
// idx lists the positions in part.Tris that make up the face, so a caller can
// replace them wholesale after adding pin holes.
func cutFace(part *stl.Mesh, p Plane, eps float64) (idx []int, groups []faceGroup, origin, u, v geom.Vec3, ok bool) {
	origin, u, v = planeBasis(p)

	var polys []Polygon
	for i, t := range part.Tris {
		if math.Abs(p.Dist(t.A)) > eps || math.Abs(p.Dist(t.B)) > eps || math.Abs(p.Dist(t.C)) > eps {
			continue
		}
		idx = append(idx, i)
		polys = append(polys, Polygon{t.A, t.B, t.C})
	}
	if len(polys) == 0 {
		return nil, nil, origin, u, v, false
	}

	// The face's boundary is the set of edges used by exactly one of its
	// triangles. boundaryEdges already computes that, and it cancels the shared
	// interior edges for us.
	w := geom.NewWelder(eps)
	edges := boundaryEdges(polys, p, w, eps)
	if len(edges) == 0 {
		return nil, nil, origin, u, v, false
	}

	loops, open := assembleLoops(edges, w)
	if open > 0 || len(loops) == 0 {
		// A boundary that will not close cannot be pinned safely — the region's
		// true extent is unknown, so every clearance measurement would be a guess.
		return nil, nil, origin, u, v, false
	}

	projected := make([]faceLoop, 0, len(loops))
	for _, l := range loops {
		projected = append(projected, projectLoop(l, origin, u, v))
	}
	groups = groupLoops(projected)
	if len(groups) == 0 {
		return nil, nil, origin, u, v, false
	}
	return idx, groups, origin, u, v, true
}
