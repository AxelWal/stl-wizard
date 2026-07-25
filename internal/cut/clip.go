package cut

import (
	"stl-cutter/internal/geom"
)

// Polygon is a planar convex polygon. Splitting a convex polygon by a plane
// yields two convex polygons, so every fragment produced during clipping stays
// convex and can be fan-triangulated safely.
type Polygon []geom.Vec3

// intersect returns the point where segment ab crosses p.
//
// The endpoints are ordered canonically first. Without that, the two triangles
// sharing an edge traverse it in opposite directions and the floating-point
// result can differ in the last bits — enough to leave cap loops unable to
// chain, and the resulting parts not watertight.
func intersect(a, b geom.Vec3, p Plane) geom.Vec3 {
	if b.Less(a) {
		a, b = b, a
	}
	da, db := p.Dist(a), p.Dist(b)
	t := da / (da - db)
	return a.Add(b.Sub(a).Scale(t))
}

// splitPolygon divides poly along p, returning the part inside the half-space
// and the part outside it. Either result may be nil. Vertices lying on the
// plane go into both, so the two pieces share an exact boundary.
func splitPolygon(poly Polygon, p Plane, eps float64) (in, out Polygon) {
	n := len(poly)
	if n < 3 {
		return nil, nil
	}

	sides := make([]Side, n)
	anyInside, anyOutside := false, false
	for i, v := range poly {
		sides[i] = p.Classify(v, eps)
		switch sides[i] {
		case Inside:
			anyInside = true
		case Outside:
			anyOutside = true
		}
	}

	// Fast paths. These matter for performance, not just tidiness: on a large
	// model almost every triangle takes one of them, and returning the input
	// slice avoids allocating anything at all.
	if !anyOutside {
		return poly, nil
	}
	if !anyInside {
		return nil, poly
	}

	for i := 0; i < n; i++ {
		j := (i + 1) % n
		ci, si := poly[i], sides[i]
		sj := sides[j]

		if si != Outside {
			in = append(in, ci)
		}
		if si != Inside {
			out = append(out, ci)
		}
		// Only a strict sign change creates a new vertex. An On vertex has
		// already been added to both sides above.
		if (si == Inside && sj == Outside) || (si == Outside && sj == Inside) {
			x := intersect(ci, poly[j], p)
			in = append(in, x)
			out = append(out, x)
		}
	}

	if len(in) < 3 {
		in = nil
	}
	if len(out) < 3 {
		out = nil
	}
	return in, out
}
