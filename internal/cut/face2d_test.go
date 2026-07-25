package cut

import (
	"math"
	"testing"

	"stl-cutter/internal/geom"
)

// square returns a loop in the z=0 plane, counter-clockwise seen from +Z.
func square(cx, cy, half float64) Polygon {
	return Polygon{
		{cx - half, cy - half, 0},
		{cx + half, cy - half, 0},
		{cx + half, cy + half, 0},
		{cx - half, cy + half, 0},
	}
}

func projectZ(loop Polygon) faceLoop {
	return projectLoop(loop, geom.Vec3{}, geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0})
}

func TestProjectLoopFlattensAndKeepsThe3DPoint(t *testing.T) {
	loop := Polygon{{1, 2, 7}, {3, 4, 7}}
	got := projectLoop(loop, geom.Vec3{0, 0, 7}, geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0})
	if got[0].P2 != (pt2{1, 2}) || got[1].P2 != (pt2{3, 4}) {
		t.Fatalf("2D coords = %v, %v, want {1,2} and {3,4}", got[0].P2, got[1].P2)
	}
	if got[0].P3 != (geom.Vec3{1, 2, 7}) {
		t.Fatalf("3D point = %v, want the original vertex preserved exactly", got[0].P3)
	}
}

func TestSignedAreaSignIndicatesWinding(t *testing.T) {
	ccw := projectZ(square(0, 0, 1))
	if got := signedArea2(ccw); got <= 0 {
		t.Fatalf("counter-clockwise area = %v, want positive", got)
	}
	if got := signedArea2(reverseLoop(ccw)); got >= 0 {
		t.Fatalf("clockwise area = %v, want negative", got)
	}
	if got := math.Abs(signedArea2(ccw)); math.Abs(got-8) > 1e-12 {
		t.Fatalf("|signedArea2| = %v, want 8 (twice the area of a 2x2 square)", got)
	}
}

func TestPointInLoop(t *testing.T) {
	l := projectZ(square(0, 0, 1))
	if !pointInLoop(pt2{0, 0}, l) {
		t.Error("centre should be inside")
	}
	if pointInLoop(pt2{5, 0}, l) {
		t.Error("distant point should be outside")
	}
	if pointInLoop(pt2{0, 5}, l) {
		t.Error("distant point above should be outside")
	}
}

// The annulus a tube cut produces: one outer loop with one hole inside it.
func TestGroupLoopsPairsAHoleWithItsOuter(t *testing.T) {
	loops := []faceLoop{projectZ(square(0, 0, 5)), projectZ(square(0, 0, 2))}
	groups := groupLoops(loops)

	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(groups))
	}
	if len(groups[0].Holes) != 1 {
		t.Fatalf("got %d holes, want 1", len(groups[0].Holes))
	}
	if signedArea2(groups[0].Outer) <= 0 {
		t.Error("outer loop must be normalised counter-clockwise")
	}
	if signedArea2(groups[0].Holes[0]) >= 0 {
		t.Error("hole must be normalised clockwise")
	}
}

// Input winding is arbitrary, so grouping must normalise it rather than trust it.
func TestGroupLoopsNormalisesArbitraryInputWinding(t *testing.T) {
	loops := []faceLoop{
		reverseLoop(projectZ(square(0, 0, 5))), // outer, given clockwise
		projectZ(square(0, 0, 2)),              // hole, given counter-clockwise
	}
	groups := groupLoops(loops)
	if len(groups) != 1 || len(groups[0].Holes) != 1 {
		t.Fatalf("got %d groups, want 1 with 1 hole", len(groups))
	}
	if signedArea2(groups[0].Outer) <= 0 || signedArea2(groups[0].Holes[0]) >= 0 {
		t.Error("winding was not normalised")
	}
}

func TestGroupLoopsKeepsDisjointLoopsSeparate(t *testing.T) {
	loops := []faceLoop{projectZ(square(0, 0, 1)), projectZ(square(10, 0, 1))}
	groups := groupLoops(loops)
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}
	for i, g := range groups {
		if len(g.Holes) != 0 {
			t.Errorf("group %d has %d holes, want 0", i, len(g.Holes))
		}
	}
}

// Three levels of nesting: an island inside a hole is a solid region again, so
// it becomes its own group rather than a hole of the outermost loop.
func TestGroupLoopsTreatsAnIslandInsideAHoleAsItsOwnGroup(t *testing.T) {
	loops := []faceLoop{
		projectZ(square(0, 0, 10)), // outer
		projectZ(square(0, 0, 6)),  // hole
		projectZ(square(0, 0, 2)),  // island inside the hole
	}
	groups := groupLoops(loops)
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2 (outer-with-hole, and the island)", len(groups))
	}

	var withHole, island *faceGroup
	for i := range groups {
		if len(groups[i].Holes) == 1 {
			withHole = &groups[i]
		} else {
			island = &groups[i]
		}
	}
	if withHole == nil || island == nil {
		t.Fatalf("expected one group with a hole and one without, got %v", groupHoleCounts(groups))
	}
	if math.Abs(signedArea2(island.Outer)) >= math.Abs(signedArea2(withHole.Outer)) {
		t.Error("the island should be the smaller of the two outers")
	}
}

func groupHoleCounts(gs []faceGroup) []int {
	out := make([]int, len(gs))
	for i, g := range gs {
		out[i] = len(g.Holes)
	}
	return out
}
