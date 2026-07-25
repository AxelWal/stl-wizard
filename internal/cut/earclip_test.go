package cut

import (
	"math"
	"testing"
)

// area2D totals the area of index triples over a loop, so a triangulation can
// be checked against the area it is supposed to cover.
func area2D(l faceLoop, tris [][3]int) float64 {
	var total float64
	for _, tr := range tris {
		total += math.Abs(cross2(l[tr[0]].P2, l[tr[1]].P2, l[tr[2]].P2)) / 2
	}
	return total
}

func TestCross2SignIndicatesTurnDirection(t *testing.T) {
	if got := cross2(pt2{0, 0}, pt2{1, 0}, pt2{0, 1}); got <= 0 {
		t.Fatalf("left turn = %v, want positive", got)
	}
	if got := cross2(pt2{0, 0}, pt2{1, 0}, pt2{0, -1}); got >= 0 {
		t.Fatalf("right turn = %v, want negative", got)
	}
	if got := cross2(pt2{0, 0}, pt2{1, 0}, pt2{2, 0}); got != 0 {
		t.Fatalf("collinear = %v, want 0", got)
	}
}

func TestSegmentsProperlyCross(t *testing.T) {
	if !segmentsProperlyCross(pt2{0, 0}, pt2{2, 2}, pt2{0, 2}, pt2{2, 0}) {
		t.Error("crossing diagonals should report a crossing")
	}
	if segmentsProperlyCross(pt2{0, 0}, pt2{1, 0}, pt2{2, 0}, pt2{3, 0}) {
		t.Error("disjoint collinear segments should not report a crossing")
	}
	// Segments meeting at a shared endpoint must not count: bridges legitimately
	// touch the polygon at the vertex they connect to.
	if segmentsProperlyCross(pt2{0, 0}, pt2{1, 1}, pt2{1, 1}, pt2{2, 0}) {
		t.Error("segments sharing an endpoint should not report a crossing")
	}
}

func TestEarClipASquare(t *testing.T) {
	l := projectZ(square(0, 0, 1))
	tris, ok := earClip(l)
	if !ok {
		t.Fatal("ok = false, want a complete triangulation")
	}
	if len(tris) != 2 {
		t.Fatalf("got %d triangles, want 2", len(tris))
	}
	if got := area2D(l, tris); math.Abs(got-4) > 1e-12 {
		t.Fatalf("area = %v, want 4", got)
	}
}

// A non-convex loop is where a naive fan triangulation would fail and ear
// clipping must not.
func TestEarClipAnLShape(t *testing.T) {
	l := projectZ(Polygon{
		{0, 0, 0}, {3, 0, 0}, {3, 1, 0}, {1, 1, 0}, {1, 3, 0}, {0, 3, 0},
	})
	tris, ok := earClip(l)
	if !ok {
		t.Fatal("ok = false, want a complete triangulation")
	}
	if len(tris) != 4 {
		t.Fatalf("got %d triangles, want 4 (n-2 for 6 vertices)", len(tris))
	}
	if got := area2D(l, tris); math.Abs(got-5) > 1e-12 {
		t.Fatalf("area = %v, want 5", got)
	}
}

func TestEarClipReportsFailureOnADegenerateLoop(t *testing.T) {
	l := projectZ(Polygon{{0, 0, 0}, {1, 0, 0}, {2, 0, 0}, {3, 0, 0}})
	if _, ok := earClip(l); ok {
		t.Fatal("ok = true, want false for a loop with no area")
	}
}

func TestRightmostIdx(t *testing.T) {
	l := projectZ(Polygon{{0, 0, 0}, {5, 1, 0}, {2, 2, 0}})
	if got := rightmostIdx(l); got != 1 {
		t.Fatalf("rightmostIdx = %d, want 1", got)
	}
}

func TestVisibleVertexPicksAReachableVertexToTheRight(t *testing.T) {
	outer := projectZ(square(0, 0, 5))
	i := visibleVertex(outer, pt2{0, 0})
	if outer[i].P2.X < 0 {
		t.Fatalf("chose vertex %v, want one at or right of the probe", outer[i].P2)
	}
	// The chosen vertex must actually be joinable without crossing an edge.
	for k := range outer {
		k2 := (k + 1) % len(outer)
		if k == i || k2 == i {
			continue
		}
		if segmentsProperlyCross(pt2{0, 0}, outer[i].P2, outer[k].P2, outer[k2].P2) {
			t.Fatalf("bridge to vertex %d crosses edge %d-%d", i, k, k2)
		}
	}
}

func TestBridgeHolesProducesOneLoopWithTheChannelVertices(t *testing.T) {
	outer := projectZ(square(0, 0, 5))
	hole := reverseLoop(projectZ(square(0, 0, 2))) // holes arrive clockwise
	got := bridgeHoles(outer, []faceLoop{hole})

	// 4 outer + 4 hole + 2 duplicated vertices forming the zero-width channel.
	if len(got) != 10 {
		t.Fatalf("got %d vertices, want 10", len(got))
	}
}

// The end-to-end property that matters: an annular cap triangulates to the
// annulus area, not the full disc.
func TestBridgedAnnulusTriangulatesToTheAnnulusArea(t *testing.T) {
	outer := projectZ(square(0, 0, 5))       // 10x10, area 100
	hole := reverseLoop(projectZ(square(0, 0, 2))) // 4x4, area 16
	merged := bridgeHoles(outer, []faceLoop{hole})

	tris, ok := earClip(merged)
	if !ok {
		t.Fatalf("ok = false, want a complete triangulation of the bridged loop")
	}
	if got := area2D(merged, tris); math.Abs(got-84) > 1e-9 {
		t.Fatalf("area = %v, want 84 (100 minus the 16 hole)", got)
	}
}

func TestBridgeHolesHandlesTwoHoles(t *testing.T) {
	outer := projectZ(square(0, 0, 10))            // 20x20, area 400
	h1 := reverseLoop(projectZ(square(-5, 0, 2)))  // area 16
	h2 := reverseLoop(projectZ(square(5, 0, 2)))   // area 16
	merged := bridgeHoles(outer, []faceLoop{h1, h2})

	tris, ok := earClip(merged)
	if !ok {
		t.Fatalf("ok = false, want a complete triangulation")
	}
	if got := area2D(merged, tris); math.Abs(got-368) > 1e-9 {
		t.Fatalf("area = %v, want 368 (400 minus two 16 holes)", got)
	}
}

// assertTriangulationSound checks the properties a cap must have: every triangle
// counter-clockwise, and the total area exactly the region's area. Summing signed
// areas rather than absolute ones is what catches an inverted triangle — the
// existing area tests sum |area| and would score an inverted triangle positively.
func assertTriangulationSound(t *testing.T, l faceLoop, tris [][3]int, wantArea float64) {
	t.Helper()

	var signed float64
	for i, tr := range tris {
		c := cross2(l[tr[0]].P2, l[tr[1]].P2, l[tr[2]].P2)
		if c <= 0 {
			t.Errorf("triangle %d is inverted or degenerate: cross2 = %v", i, c)
		}
		signed += c / 2
	}
	if math.Abs(signed-wantArea) > 1e-9 {
		t.Errorf("signed area = %v, want %v", signed, wantArea)
	}
}

// An off-centre hole is the case the old strict point-in-triangle ear test got
// wrong. Every hole in the original suite was centred on its outer, and centred
// positions happen to miss the degeneracy.
func TestEarClipOffCentreHole(t *testing.T) {
	outer := projectZ(square(0, 0, 5))             // 10x10, area 100
	hole := reverseLoop(projectZ(square(2, 0, 1))) // 2x2 at x=2, area 4
	merged := bridgeHoles(outer, []faceLoop{hole})

	tris, ok := earClip(merged)
	if !ok {
		t.Fatalf("ok = false, want a complete triangulation")
	}
	assertTriangulationSound(t, merged, tris, 96)
}

// A sweep over hole positions. The failures were all at exact collinearity with
// lines through the outer's corners, which a handful of hand-picked positions
// misses but which axis-aligned models hit routinely.
func TestEarClipHolePositionSweep(t *testing.T) {
	const half = 5.0
	outer := projectZ(square(0, 0, half))

	for i := -8; i <= 8; i++ {
		for j := -8; j <= 8; j++ {
			cx, cy := float64(i)*0.5, float64(j)*0.5
			// Keep the hole strictly inside the outer.
			if math.Abs(cx)+1 >= half || math.Abs(cy)+1 >= half {
				continue
			}
			hole := reverseLoop(projectZ(square(cx, cy, 1)))
			merged := bridgeHoles(outer, []faceLoop{hole})

			tris, ok := earClip(merged)
			if !ok {
				t.Errorf("hole at (%v,%v): ok = false", cx, cy)
				continue
			}
			var signed float64
			inverted := 0
			for _, tr := range tris {
				c := cross2(merged[tr[0]].P2, merged[tr[1]].P2, merged[tr[2]].P2)
				if c <= 0 {
					inverted++
				}
				signed += c / 2
			}
			if inverted > 0 {
				t.Errorf("hole at (%v,%v): %d inverted triangles", cx, cy, inverted)
			}
			if math.Abs(signed-96) > 1e-9 {
				t.Errorf("hole at (%v,%v): signed area = %v, want 96", cx, cy, signed)
			}
		}
	}
}

// The predicate must be symmetric under reflection. It previously reported a
// shared endpoint as a crossing for a left turn but not for a right turn.
func TestSegmentsProperlyCrossIsOrientationSymmetric(t *testing.T) {
	cases := []struct {
		name       string
		a, b, c, d pt2
	}{
		{"shared endpoint, left turn", pt2{0, 0}, pt2{1, 0}, pt2{1, 0}, pt2{1, 1}},
		{"shared endpoint, right turn", pt2{0, 0}, pt2{1, 1}, pt2{1, 1}, pt2{2, 0}},
		{"T-junction from above", pt2{0, 0}, pt2{4, 0}, pt2{2, 0}, pt2{2, 3}},
		{"T-junction from below", pt2{0, 0}, pt2{4, 0}, pt2{2, 0}, pt2{2, -3}},
	}
	for _, tc := range cases {
		if segmentsProperlyCross(tc.a, tc.b, tc.c, tc.d) {
			t.Errorf("%s: reported a crossing; touching is not a proper crossing", tc.name)
		}
	}

	// A genuine crossing must still be reported.
	if !segmentsProperlyCross(pt2{0, 0}, pt2{2, 2}, pt2{0, 2}, pt2{2, 0}) {
		t.Error("crossing diagonals should report a crossing")
	}
}

// A hole touching the outer boundary at a vertex is legitimate input — groupLoops
// classifies it as a hole. Record whatever the triangulator does with it so a
// change in behaviour is visible; Task 13 needs a defined outcome for ok = false.
func TestEarClipHoleTouchingTheOuterBoundary(t *testing.T) {
	outer := projectZ(square(0, 0, 5))
	hole := reverseLoop(projectZ(Polygon{{5, 0, 0}, {3, 1, 0}, {3, -1, 0}}))
	merged := bridgeHoles(outer, []faceLoop{hole})

	tris, ok := earClip(merged)
	if ok {
		assertTriangulationSound(t, merged, tris, 98)
	} else {
		t.Logf("hole touching the outer boundary yields ok = false with %d triangles; "+
			"failing closed is acceptable, silently emitting bad geometry is not", len(tris))
	}
}
