package cut

import (
	"math"
	"testing"

	"stl-cutter/internal/geom"
)

// wellPolys returns the four vertical quads of a square shaft whose top edges
// all lie on z=0 and which extends downward. Their on-plane edges form a closed
// square, which is exactly the shape a cut produces.
func wellPolys(half, depth float64) []Polygon {
	corners := [4][2]float64{{-half, -half}, {half, -half}, {half, half}, {-half, half}}
	out := make([]Polygon, 0, 4)
	for i := range corners {
		j := (i + 1) % 4
		a := geom.Vec3{corners[i][0], corners[i][1], 0}
		b := geom.Vec3{corners[j][0], corners[j][1], 0}
		out = append(out, Polygon{a, b,
			{b[0], b[1], -depth},
			{a[0], a[1], -depth},
		})
	}
	return out
}

func TestPlaneBasisIsRightHanded(t *testing.T) {
	for _, n := range []geom.Vec3{{0, 0, 1}, {1, 0, 0}, {0, -1, 0}, {1, 2, 3}} {
		p := Plane{N: n.Unit(), D: 1.5}
		origin, u, v := planeBasis(p)
		if math.Abs(p.Dist(origin)) > 1e-12 {
			t.Errorf("normal %v: basis origin is not on the plane", n)
		}
		if got := u.Cross(v); got.Sub(p.N).Len() > 1e-12 {
			t.Errorf("normal %v: u cross v = %v, want %v", n, got, p.N)
		}
	}
}

func TestTriangulateFaceCapsASquareOpening(t *testing.T) {
	tris, open, incomplete := triangulateFace(wellPolys(5, 3), zPlane, 1e-9)
	if open != 0 || incomplete != 0 {
		t.Fatalf("open=%d incomplete=%d, want 0 and 0", open, incomplete)
	}
	if len(tris) != 2 {
		t.Fatalf("got %d triangles, want 2", len(tris))
	}
	var area float64
	for _, tr := range tris {
		area += tr.Area()
	}
	if math.Abs(area-100) > 1e-9 {
		t.Fatalf("cap area = %v, want 100", area)
	}
}

// The cap must face +p.N. Split relies on that to hand the same geometry to both
// output parts with opposite winding.
func TestTriangulateFaceOrientsCapAlongPlaneNormal(t *testing.T) {
	tris, _, _ := triangulateFace(wellPolys(5, 3), zPlane, 1e-9)
	for i, tr := range tris {
		if got := tr.Normal(); got.Sub(zPlane.N).Len() > 1e-9 {
			t.Errorf("triangle %d normal = %v, want %v", i, got, zPlane.N)
		}
	}
}

// A tube's cross-section is an annulus, so this is the case that exercises the
// whole hole path end to end.
func TestTriangulateFaceCapsAnAnnulus(t *testing.T) {
	polys := append(wellPolys(5, 3), wellPolys(2, 3)...)
	tris, open, incomplete := triangulateFace(polys, zPlane, 1e-9)
	if open != 0 || incomplete != 0 {
		t.Fatalf("open=%d incomplete=%d, want 0 and 0", open, incomplete)
	}
	var area float64
	for _, tr := range tris {
		area += tr.Area()
	}
	if math.Abs(area-84) > 1e-9 {
		t.Fatalf("cap area = %v, want 84 (100 minus the 16 hole)", area)
	}
}

// A missing wall leaves the cross-section boundary unable to close, which is how
// non-manifold input is detected.
func TestTriangulateFaceReportsAnOpenBoundary(t *testing.T) {
	polys := wellPolys(5, 3)[:3]
	tris, open, _ := triangulateFace(polys, zPlane, 1e-9)
	if open == 0 {
		t.Fatal("open = 0, want a report of the unclosed boundary")
	}
	if len(tris) != 0 {
		t.Fatalf("got %d triangles, want none for an unclosed boundary", len(tris))
	}
}

func TestTriangulateFaceReturnsNothingWhenThePlaneMissesEverything(t *testing.T) {
	far := Plane{N: geom.Vec3{0, 0, 1}, D: 1000}
	tris, open, incomplete := triangulateFace(wellPolys(5, 3), far, 1e-9)
	if len(tris) != 0 || open != 0 || incomplete != 0 {
		t.Fatalf("got %d tris, open=%d, incomplete=%d; want all zero", len(tris), open, incomplete)
	}
}

// collinearWellPolys returns three quads whose on-plane edges form a closed but
// completely degenerate loop: three collinear points enclosing no area. earClip
// cannot triangulate it, which is what drives the incomplete counter.
func collinearWellPolys() []Polygon {
	pts := [3][2]float64{{20, 0}, {22, 0}, {24, 0}}
	out := make([]Polygon, 0, 3)
	for i := range pts {
		j := (i + 1) % len(pts)
		a := geom.Vec3{pts[i][0], pts[i][1], 0}
		b := geom.Vec3{pts[j][0], pts[j][1], 0}
		out = append(out, Polygon{a, b,
			{b[0], b[1], -1},
			{a[0], a[1], -1},
		})
	}
	return out
}

// One region failing to triangulate must not cost the others. The failure is
// reported through incomplete rather than by dropping the whole cap or by
// emitting partial geometry for the region that failed.
func TestTriangulateFaceReportsOneFailedRegionAndKeepsTheRest(t *testing.T) {
	polys := append(wellPolys(5, 3), collinearWellPolys()...)

	tris, open, incomplete := triangulateFace(polys, zPlane, 1e-9)

	if open != 0 {
		t.Errorf("open = %d, want 0 — both boundaries close", open)
	}
	if incomplete != 1 {
		t.Errorf("incomplete = %d, want 1 for the degenerate region", incomplete)
	}

	// The good square still produces its full cap, and the degenerate region
	// contributes nothing at all rather than a sliver.
	var area float64
	for _, tr := range tris {
		area += tr.Area()
	}
	if math.Abs(area-100) > 1e-9 {
		t.Errorf("cap area = %v, want exactly 100 — the square's cap, and nothing from the degenerate region", area)
	}
}

// tiltedWellPolys is wellPolys built directly from a plane's own (origin, u, v)
// basis, so its top-edge vertices land exactly on p no matter how p is tilted.
//
// zPlane will not do for TestTriangulateFaceReusesTheInputVerticesExactly: its
// normal is axis-aligned, so planeBasis hands back the trivial basis u=(1,0,0),
// v=(0,1,0), origin=(0,0,0), and reconstructing a point from that basis is bit
// exact — it would pass even if triangulateFace rebuilt vertices instead of
// carrying them. A tilted plane makes u and v components that are not exactly
// representable, so going through Dot then Scale/Add actually rounds.
func tiltedWellPolys(p Plane, half, depth float64) []Polygon {
	origin, u, v := planeBasis(p)
	corners := [4][2]float64{{-half, -half}, {half, -half}, {half, half}, {-half, half}}
	top := make([]geom.Vec3, 4)
	for i, c := range corners {
		top[i] = origin.Add(u.Scale(c[0])).Add(v.Scale(c[1]))
	}
	out := make([]Polygon, 0, 4)
	for i := range top {
		j := (i + 1) % 4
		a, b := top[i], top[j]
		out = append(out, Polygon{a, b, b.Sub(p.N.Scale(depth)), a.Sub(p.N.Scale(depth))})
	}
	return out
}

// The cap must reuse the very vertices the loops arrived with. Reconstructing
// them from the plane basis would round, and the cap would no longer meet the
// clipped triangles it is supposed to close.
func TestTriangulateFaceReusesTheInputVerticesExactly(t *testing.T) {
	p := Plane{N: geom.Vec3{1, 2, 3}.Unit(), D: 1.5}
	polys := tiltedWellPolys(p, 5, 3)

	// Every vertex that lies on the cut plane is a candidate cap vertex.
	onPlane := map[geom.Vec3]bool{}
	for _, poly := range polys {
		for _, v := range poly {
			if math.Abs(p.Dist(v)) <= 1e-9 {
				onPlane[v] = true
			}
		}
	}

	tris, _, _ := triangulateFace(polys, p, 1e-9)
	if len(tris) == 0 {
		t.Fatal("no cap was produced")
	}
	for i, tr := range tris {
		for j, v := range [3]geom.Vec3{tr.A, tr.B, tr.C} {
			if !onPlane[v] {
				t.Errorf("triangle %d vertex %d is %v, which is not one of the input vertices — it was reconstructed, not carried", i, j, v)
			}
		}
	}
}
