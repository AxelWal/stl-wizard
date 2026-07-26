package cut

import (
	"stl-wizard/internal/geom"
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
//
// A polygon lying wholly on p goes entirely to in, and out is nil. That is not
// an arbitrary tie-break, and it must not be "tidied" into splitting it or
// dropping it. triangulateFace recovers each cap boundary as the edges of part
// 2's polygons with both endpoints on p, and a model face flush with p needs no
// cap precisely because it is present there and its edges cancel against the
// plane's own. Send the coplanar face anywhere else and the cancellation stops
// happening, and a spurious cap is laid over the flush face — a Critical that
// has already been fixed once.
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
