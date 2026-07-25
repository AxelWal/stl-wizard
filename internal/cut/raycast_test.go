package cut

import (
	"math"
	"testing"

	"stl-cutter/internal/fixtures"
	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

func TestRayTriangleHitsAndMisses(t *testing.T) {
	tri := stl.Tri{
		A: geom.Vec3{0, 0, 0},
		B: geom.Vec3{2, 0, 0},
		C: geom.Vec3{0, 2, 0},
	}

	// Straight down onto the middle of the triangle.
	d, ok := rayTriangle(geom.Vec3{0.5, 0.5, 5}, geom.Vec3{0, 0, -1}, tri)
	if !ok {
		t.Fatal("expected a hit")
	}
	if math.Abs(d-5) > 1e-9 {
		t.Errorf("distance = %v, want 5", d)
	}

	// Beside the triangle.
	if _, ok := rayTriangle(geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, -1}, tri); ok {
		t.Error("expected a miss beside the triangle")
	}

	// Pointing away from it.
	if _, ok := rayTriangle(geom.Vec3{0.5, 0.5, 5}, geom.Vec3{0, 0, 1}, tri); ok {
		t.Error("expected a miss pointing away")
	}

	// Parallel to its plane.
	if _, ok := rayTriangle(geom.Vec3{0.5, 0.5, 5}, geom.Vec3{1, 0, 0}, tri); ok {
		t.Error("expected a miss for a ray parallel to the plane")
	}
}

// A hit exactly at the origin of the ray is a zero-distance hit, not a miss —
// the wall check relies on that to notice a surface it is already touching.
func TestRayTriangleCountsAZeroDistanceHit(t *testing.T) {
	tri := stl.Tri{
		A: geom.Vec3{0, 0, 0},
		B: geom.Vec3{2, 0, 0},
		C: geom.Vec3{0, 2, 0},
	}
	d, ok := rayTriangle(geom.Vec3{0.5, 0.5, 0}, geom.Vec3{0, 0, -1}, tri)
	if !ok {
		t.Fatal("expected a hit at zero distance")
	}
	if math.Abs(d) > 1e-9 {
		t.Errorf("distance = %v, want 0", d)
	}
}

func TestRayGridFindsTheNearestSurface(t *testing.T) {
	// A 10-cube spans 0..10. From inside, looking down, the floor is 5 away.
	g := newRayGrid(fixtures.Cube(10))

	d, ok := g.nearestHit(geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, -1})
	if !ok {
		t.Fatal("expected to hit the cube's floor")
	}
	if math.Abs(d-5) > 1e-9 {
		t.Errorf("distance = %v, want 5", d)
	}

	// Looking up, the ceiling is also 5 away.
	d, ok = g.nearestHit(geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, 1})
	if !ok {
		t.Fatal("expected to hit the cube's ceiling")
	}
	if math.Abs(d-5) > 1e-9 {
		t.Errorf("distance = %v, want 5", d)
	}
}

// The grid must return the NEAREST hit, not merely some hit. A hollow box has
// two surfaces along the same ray, and the wall check is only sound if the
// closer one wins.
func TestRayGridReturnsTheNearestOfSeveralHits(t *testing.T) {
	// Outer 0..20, wall 2, so the inner void spans 2..18.
	g := newRayGrid(fixtures.HollowBox(geom.Vec3{20, 20, 20}, 2))

	// From well above, looking down: the outer top face at z=20 comes first.
	d, ok := g.nearestHit(geom.Vec3{10, 10, 30}, geom.Vec3{0, 0, -1})
	if !ok {
		t.Fatal("expected a hit")
	}
	if math.Abs(d-10) > 1e-9 {
		t.Errorf("distance = %v, want 10 — the outer surface, not the inner one", d)
	}
}

func TestRayGridMissesEntirely(t *testing.T) {
	g := newRayGrid(fixtures.Cube(10))
	if _, ok := g.nearestHit(geom.Vec3{50, 50, 50}, geom.Vec3{1, 0, 0}); ok {
		t.Error("expected a miss from outside, pointing away")
	}
}

// The grid is an optimisation, so it must agree exactly with the brute-force
// answer. If it ever disagrees, the acceleration is wrong, not the maths.
func TestRayGridAgreesWithBruteForce(t *testing.T) {
	m := fixtures.UVSphere(10, 24, 12)
	g := newRayGrid(m)

	dirs := []geom.Vec3{
		{0, 0, -1}, {0, 0, 1}, {1, 0, 0}, {-1, 0, 0},
		{1, 1, 1}, {-1, 0.5, -0.25}, {0.3, -1, 0.7},
	}
	origins := []geom.Vec3{
		{0, 0, 0}, {5, 0, 0}, {0, 4, 2}, {-3, -3, 1}, {0, 0, 30},
	}

	for _, o := range origins {
		for _, d := range dirs {
			gotDist, gotOK := g.nearestHit(o, d)

			var wantDist float64 = math.Inf(1)
			wantOK := false
			for _, tri := range m.Tris {
				if dist, ok := rayTriangle(o, d, tri); ok && dist < wantDist {
					wantDist, wantOK = dist, true
				}
			}

			if gotOK != wantOK {
				t.Errorf("origin %v dir %v: grid ok=%v, brute force ok=%v", o, d, gotOK, wantOK)
				continue
			}
			if wantOK && math.Abs(gotDist-wantDist) > 1e-9 {
				t.Errorf("origin %v dir %v: grid %v, brute force %v", o, d, gotDist, wantDist)
			}
		}
	}
}
