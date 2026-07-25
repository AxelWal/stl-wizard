package cut

import (
	"math"
	"testing"

	"stl-cutter/internal/geom"
)

var zPlane = Plane{N: geom.Vec3{0, 0, 1}, D: 0}

const testEps = 1e-9

// This is the property the whole capping stage rests on: the two triangles that
// share an edge traverse it in opposite directions, and both must land on the
// same intersection point down to the last bit.
func TestIntersectIsIndependentOfEdgeDirection(t *testing.T) {
	a := geom.Vec3{0.1, 0.2, -0.3}
	b := geom.Vec3{0.7, -0.4, 0.9}
	ab := intersect(a, b, zPlane)
	ba := intersect(b, a, zPlane)
	if ab != ba {
		t.Fatalf("intersect(a,b) = %v but intersect(b,a) = %v; must be bit-identical", ab, ba)
	}
}

func TestIntersectLandsOnThePlane(t *testing.T) {
	got := intersect(geom.Vec3{0, 0, -2}, geom.Vec3{0, 0, 6}, zPlane)
	if math.Abs(zPlane.Dist(got)) > 1e-12 {
		t.Fatalf("intersection %v is not on the plane", got)
	}
	if got != (geom.Vec3{0, 0, 0}) {
		t.Fatalf("got %v, want the origin", got)
	}
}

func TestSplitPolygonPassesThroughWhollyInsidePolygons(t *testing.T) {
	poly := Polygon{{0, 0, 1}, {1, 0, 1}, {0, 1, 1}}
	in, out := splitPolygon(poly, zPlane, testEps)
	if len(in) != 3 || out != nil {
		t.Fatalf("in=%v out=%v, want the polygon unchanged inside and nothing outside", in, out)
	}
}

func TestSplitPolygonPassesThroughWhollyOutsidePolygons(t *testing.T) {
	poly := Polygon{{0, 0, -1}, {1, 0, -1}, {0, 1, -1}}
	in, out := splitPolygon(poly, zPlane, testEps)
	if in != nil || len(out) != 3 {
		t.Fatalf("in=%v out=%v, want nothing inside and the polygon unchanged outside", in, out)
	}
}

func TestSplitPolygonSplitsAStraddlingTriangle(t *testing.T) {
	// One vertex above the plane, two below.
	poly := Polygon{{0, 0, 2}, {0, 0, -2}, {2, 0, -2}}
	in, out := splitPolygon(poly, zPlane, testEps)
	if len(in) != 3 {
		t.Fatalf("inside piece has %d vertices, want 3", len(in))
	}
	if len(out) != 4 {
		t.Fatalf("outside piece has %d vertices, want 4", len(out))
	}
	// Every generated vertex must sit on or beyond the plane on its own side.
	for _, v := range in {
		if zPlane.Dist(v) < -testEps {
			t.Errorf("inside vertex %v is below the plane", v)
		}
	}
	for _, v := range out {
		if zPlane.Dist(v) > testEps {
			t.Errorf("outside vertex %v is above the plane", v)
		}
	}
}

// Vertices lying on the plane belong to both sides — that is what keeps the two
// pieces sharing an exact boundary instead of leaving a gap.
func TestSplitPolygonPutsOnPlaneVerticesInBothPieces(t *testing.T) {
	poly := Polygon{{0, 0, 0}, {2, 0, 0}, {1, 0, 2}, {1, 0, -2}}
	in, out := splitPolygon(poly, zPlane, testEps)
	countOnPlane := func(p Polygon) int {
		n := 0
		for _, v := range p {
			if math.Abs(zPlane.Dist(v)) <= testEps {
				n++
			}
		}
		return n
	}
	if countOnPlane(in) < 2 || countOnPlane(out) < 2 {
		t.Fatalf("in has %d on-plane vertices, out has %d; both need the shared boundary",
			countOnPlane(in), countOnPlane(out))
	}
}

func TestSplitPolygonDiscardsSubTriangularFragments(t *testing.T) {
	// A polygon touching the plane at a single vertex yields no real inside area.
	poly := Polygon{{0, 0, 0}, {1, 0, -1}, {-1, 0, -1}}
	in, _ := splitPolygon(poly, zPlane, testEps)
	if in != nil {
		t.Fatalf("in = %v, want nil for a fragment with fewer than three vertices", in)
	}
}

func TestFanTrianglesCoversPolygonArea(t *testing.T) {
	// A unit square in the z=0 plane.
	poly := Polygon{{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0}}
	tris := fanTriangles(poly, 0)
	if len(tris) != 2 {
		t.Fatalf("got %d triangles, want 2", len(tris))
	}
	var area float64
	for _, tr := range tris {
		area += tr.Area()
	}
	if math.Abs(area-1) > 1e-12 {
		t.Fatalf("total area = %v, want 1", area)
	}
}

func TestFanTrianglesDropsDegenerateSlivers(t *testing.T) {
	// Three collinear points have no area.
	poly := Polygon{{0, 0, 0}, {1, 0, 0}, {2, 0, 0}}
	if got := fanTriangles(poly, 1e-18); len(got) != 0 {
		t.Fatalf("got %d triangles, want 0", len(got))
	}
}
