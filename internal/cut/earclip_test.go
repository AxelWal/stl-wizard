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
	i := visibleVertex(outer, pt2{0, 0}, nil, nil)
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
	outer := projectZ(square(0, 0, 5))             // 10x10, area 100
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
	outer := projectZ(square(0, 0, 10))           // 20x20, area 400
	h1 := reverseLoop(projectZ(square(-5, 0, 2))) // area 16
	h2 := reverseLoop(projectZ(square(5, 0, 2)))  // area 16
	merged := bridgeHoles(outer, []faceLoop{h1, h2})

	tris, ok := earClip(merged)
	if !ok {
		t.Fatalf("ok = false, want a complete triangulation")
	}
	if got := area2D(merged, tris); math.Abs(got-368) > 1e-9 {
		t.Fatalf("area = %v, want 368 (400 minus two 16 holes)", got)
	}
}

// clipHalfPlane clips a convex polygon to the half-plane left of a->b
// (Sutherland-Hodgman). The subject is always convex here — a triangle, then
// what is left of it — so one output ring is enough.
func clipHalfPlane(subject []pt2, a, b pt2) []pt2 {
	out := make([]pt2, 0, len(subject)+1)
	for i := range subject {
		c := subject[i]
		d := subject[(i+1)%len(subject)]
		sc, sd := cross2(a, b, c), cross2(a, b, d)
		if sc >= 0 {
			out = append(out, c)
		}
		if (sc > 0 && sd < 0) || (sc < 0 && sd > 0) {
			f := sc / (sc - sd)
			out = append(out, pt2{X: c.X + f*(d.X-c.X), Y: c.Y + f*(d.Y-c.Y)})
		}
	}
	return out
}

// overlapArea returns the area shared by two counter-clockwise triangles. Two
// triangles that only abut along an edge or meet at a vertex clip to a
// degenerate ring and so score zero.
func overlapArea(p, q [3]pt2) float64 {
	poly := []pt2{p[0], p[1], p[2]}
	for i := 0; i < 3; i++ {
		poly = clipHalfPlane(poly, q[i], q[(i+1)%3])
		if len(poly) < 3 {
			return 0
		}
	}
	var s float64
	for i := range poly {
		j := (i + 1) % len(poly)
		s += poly[i].X*poly[j].Y - poly[j].X*poly[i].Y
	}
	return math.Abs(s) / 2
}

// assertTriangulationSound checks the properties a cap must have: every triangle
// counter-clockwise, no two triangles overlapping, and the total area exactly the
// region's area. Summing signed areas rather than absolute ones is what catches an
// inverted triangle — the existing area tests sum |area| and would score an
// inverted triangle positively.
//
// The pairwise overlap check is not implied by the other two. A triangulation can
// hit the exact area with every triangle wound correctly and still have one
// triangle lying 32% on top of another, paid for by a third that misses area
// elsewhere; that is a visibly wrong cap that the area test alone waves through.
//
// ponytail: O(t^2) pairs, exact clip per pair. Caps run to a few hundred
// triangles, so this is a test-only cost of a few hundred thousand clips.
func assertTriangulationSound(t *testing.T, l faceLoop, tris [][3]int, wantArea float64) {
	t.Helper()

	var signed float64
	pts := make([][3]pt2, len(tris))
	for i, tr := range tris {
		c := cross2(l[tr[0]].P2, l[tr[1]].P2, l[tr[2]].P2)
		if c <= 0 {
			t.Errorf("triangle %d is inverted or degenerate: cross2 = %v", i, c)
		}
		signed += c / 2
		pts[i] = [3]pt2{l[tr[0]].P2, l[tr[1]].P2, l[tr[2]].P2}
	}
	if math.Abs(signed-wantArea) > 1e-9 {
		t.Errorf("signed area = %v, want %v", signed, wantArea)
	}
	for i := range pts {
		for j := i + 1; j < len(pts); j++ {
			if a := overlapArea(pts[i], pts[j]); a > 1e-9 {
				t.Errorf("triangles %d and %d overlap by %v", i, j, a)
			}
		}
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

// Several holes arranged so that the second and third bridges would naturally
// land on the outer vertex the first bridge already used. bridgeHoles duplicates
// its landing vertex, so a second channel through the same point makes the merged
// loop non-simple — the position appears three times — and ear clipping cannot
// complete. Both configurations were found by fuzzing: they yield ok = false
// before visibleVertex learned to skip already-duplicated vertices.
func TestBridgeHolesDoesNotStackChannelsOnOneVertex(t *testing.T) {
	cases := []struct {
		name     string
		centres  [][2]float64
		wantArea float64
	}{
		// Two holes stacked in the left column: both see the same outer corner as
		// their nearest reachable vertex to the right.
		{"two holes, one column", [][2]float64{{-3, -3}, {-3, 3}}, 100 - 2*4},
		{"three holes", [][2]float64{{-3, -3}, {-3, 0}, {0, 3}}, 100 - 3*4},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outer := projectZ(square(0, 0, 5)) // 10x10, area 100
			holes := make([]faceLoop, 0, len(tc.centres))
			for _, c := range tc.centres {
				holes = append(holes, reverseLoop(projectZ(square(c[0], c[1], 1)))) // 2x2, area 4
			}
			merged := bridgeHoles(outer, holes)

			seen := map[pt2]int{}
			for _, v := range merged {
				if seen[v.P2]++; seen[v.P2] > 2 {
					t.Fatalf("position %v appears %d times; a channel was stacked on a duplicated vertex",
						v.P2, seen[v.P2])
				}
			}

			tris, ok := earClip(merged)
			if !ok {
				t.Fatalf("ok = false, want a complete triangulation")
			}
			assertTriangulationSound(t, merged, tris, tc.wantArea)
		})
	}
}

// Two holes crowded against the outer's left edge. Nothing at or right of the
// lower hole's rightmost vertex is reachable once the upper hole's channel is in
// place, so its bridge falls back to a landing on the left — and that join runs
// back through the lower hole's own boundary, leaving a merged loop that crosses
// itself and cannot be triangulated. Found by fuzzing; fails before visibleVertex
// was shown the hole it is splicing in.
func TestBridgeHolesDoesNotCutThroughTheHoleItIsSplicing(t *testing.T) {
	outer := projectZ(square(0, 0, 10)) // 20x20, area 400
	holes := []faceLoop{
		reverseLoop(projectZ(square(-8, 3, 1))), // 2x2, area 4
		reverseLoop(projectZ(square(-7, 0, 1))), // 2x2, area 4
	}
	merged := bridgeHoles(outer, holes)

	tris, ok := earClip(merged)
	if !ok {
		t.Fatalf("ok = false, want a complete triangulation")
	}
	assertTriangulationSound(t, merged, tris, 392)
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
		return
	}
	// Failing closed is acceptable, silently emitting bad geometry is not — so the
	// failure branch has to assert the contract rather than only log it.
	if tris != nil {
		t.Fatalf("ok = false but %d triangles were returned; a failed triangulation must return none",
			len(tris))
	}
	t.Logf("hole touching the outer boundary yields ok = false and no triangles")
}

// A plate with holes on integer positions is what this tool is for, and it is
// where a bridge grazes rather than crosses. Holes at (-8,-8) and (-8,-5): the
// first bridges from (-7,-7) to the corner (10,-10); the second enters at (-7,-4)
// with nothing reachable rightward, so its nearest candidate is (-7,-9) — a join
// that runs along the second hole's own edge, straight through the first hole's
// duplicated entry at (-7,-7), then along the first hole's edge. Not one proper
// crossing anywhere, so a clearance test that only looks for crossings passes it
// and the merged loop is degenerate.
func TestEarClipGridPlateWithTwoHoles(t *testing.T) {
	outer := projectZ(square(0, 0, 10)) // 20x20, area 400
	holes := []faceLoop{
		reverseLoop(projectZ(square(-8, -8, 1))), // 2x2, area 4
		reverseLoop(projectZ(square(-8, -5, 1))), // 2x2, area 4
	}
	merged := bridgeHoles(outer, holes)

	tris, ok := earClip(merged)
	if !ok {
		t.Fatalf("ok = false, want a complete triangulation")
	}
	assertTriangulationSound(t, merged, tris, 392)
}

// The position sweep with two holes on integer positions. Exact collinearity is
// the norm on a grid, not an accident of a few positions: one hole never fails,
// two holes failed 4-5% of the time before bridge clearance rejected grazing.
func TestEarClipGridPositionSweep(t *testing.T) {
	outer := projectZ(square(0, 0, 10))

	for ax := -8; ax <= 8; ax += 4 {
		for ay := -8; ay <= 8; ay += 4 {
			for bx := -8; bx <= 8; bx += 3 {
				for by := -8; by <= 8; by += 3 {
					// Holes span +-1, so anything closer than 3 in both axes touches
					// or overlaps and is not two holes at all.
					if abs(ax-bx) < 3 && abs(ay-by) < 3 {
						continue
					}
					holes := []faceLoop{
						reverseLoop(projectZ(square(float64(ax), float64(ay), 1))),
						reverseLoop(projectZ(square(float64(bx), float64(by), 1))),
					}
					merged := bridgeHoles(outer, holes)
					tris, ok := earClip(merged)
					if !ok {
						t.Errorf("holes at (%d,%d) and (%d,%d): ok = false", ax, ay, bx, by)
						continue
					}
					assertTriangulationSound(t, merged, tris, 392)
				}
			}
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// earClip's contract on input it cannot triangulate: no triangles, ok = false.
// A clockwise loop has no counter-clockwise ear anywhere, and a bowtie is not a
// simple polygon in either winding. All four were reported as untested.
func TestEarClipRejectsLoopsItCannotTriangulate(t *testing.T) {
	bowtie := Polygon{{0, 0, 0}, {2, 2, 0}, {2, 0, 0}, {0, 2, 0}}
	cases := []struct {
		name string
		loop Polygon
	}{
		{"clockwise triangle", Polygon{{0, 0, 0}, {0, 1, 0}, {1, 0, 0}}},
		{"clockwise square", Polygon{{0, 0, 0}, {0, 1, 0}, {1, 1, 0}, {1, 0, 0}}},
		{"bowtie", bowtie},
		{"bowtie reversed", Polygon{bowtie[3], bowtie[2], bowtie[1], bowtie[0]}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tris, ok := earClip(projectZ(tc.loop))
			if ok {
				t.Fatalf("ok = true with %d triangles, want false", len(tris))
			}
			if tris != nil {
				t.Fatalf("got %d triangles alongside ok = false, want none", len(tris))
			}
		})
	}
}

func TestSegmentsTouchCatchesWhatProperCrossingMisses(t *testing.T) {
	cases := []struct {
		name       string
		a, b, c, d pt2
		want       bool
	}{
		{"proper crossing", pt2{0, 0}, pt2{2, 2}, pt2{0, 2}, pt2{2, 0}, true},
		{"collinear overlap", pt2{0, 0}, pt2{0, 4}, pt2{0, 1}, pt2{0, 3}, true},
		{"grazing a vertex", pt2{0, -2}, pt2{0, 2}, pt2{0, 0}, pt2{3, 1}, true},
		{"shared endpoint", pt2{0, 0}, pt2{1, 1}, pt2{1, 1}, pt2{2, 0}, true},
		{"disjoint collinear", pt2{0, 0}, pt2{1, 0}, pt2{2, 0}, pt2{3, 0}, false},
		{"apart", pt2{0, 0}, pt2{1, 0}, pt2{0, 1}, pt2{1, 1}, false},
	}
	for _, tc := range cases {
		if got := segmentsTouch(tc.a, tc.b, tc.c, tc.d); got != tc.want {
			t.Errorf("%s: segmentsTouch = %v, want %v", tc.name, got, tc.want)
		}
	}
}
