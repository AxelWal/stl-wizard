package cut

import (
	"math"
	"testing"

	"stl-cutter/internal/fixtures"
	"stl-cutter/internal/geom"
)

func TestCutFaceOfAHalvedCube(t *testing.T) {
	m := fixtures.Cube(10)
	s := SpecFromNormal(geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, 1}, 100, 100)
	res, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	idx, groups, _, _, _, ok := cutFace(res.Part2, s.Planes()[0], m.Epsilon())
	if !ok {
		t.Fatal("no cut face recovered")
	}
	if len(idx) == 0 {
		t.Fatal("no triangles identified as cut face")
	}
	if len(groups) != 1 {
		t.Fatalf("got %d face regions, want 1", len(groups))
	}
	if len(groups[0].Holes) != 0 {
		t.Errorf("got %d holes, want 0 for a solid cube's cross-section", len(groups[0].Holes))
	}

	// The face is the cube's full 10x10 cross-section.
	if got := math.Abs(signedArea2(groups[0].Outer)) / 2; math.Abs(got-100) > 1e-6 {
		t.Errorf("face area = %v, want 100", got)
	}
}

// A tube's cross-section is an annulus, so the recovered face must carry a hole.
func TestCutFaceOfATubeHasAHole(t *testing.T) {
	m := fixtures.Tube(5, 3, 20, 48)
	s := SpecFromNormal(geom.Vec3{0, 0, 10}, geom.Vec3{0, 0, 1}, 100, 100)
	res, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	_, groups, _, _, _, ok := cutFace(res.Part2, s.Planes()[0], m.Epsilon())
	if !ok {
		t.Fatal("no cut face recovered")
	}
	if len(groups) != 1 {
		t.Fatalf("got %d face regions, want 1", len(groups))
	}
	if len(groups[0].Holes) != 1 {
		t.Fatalf("got %d holes, want 1 — the tube's bore", len(groups[0].Holes))
	}

	outer := math.Abs(signedArea2(groups[0].Outer)) / 2
	hole := math.Abs(signedArea2(groups[0].Holes[0])) / 2
	want := math.Pi * (25 - 9)
	if got := outer - hole; math.Abs(got-want)/want > 0.02 {
		t.Errorf("annulus area = %v, want about %v", got, want)
	}
}

// The face must be the TRIMMED one. A bounded rectangle carving a plug out of a
// cube gives a 4x4 face, not the cube's full 10x10 cross-section — this is the
// property that makes pins land in material that survives the cut.
func TestCutFaceIsTrimmedByTheRectangle(t *testing.T) {
	m := fixtures.Cube(10)
	s := SpecFromNormal(geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, 1}, 4, 4)
	res, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	_, groups, _, _, _, ok := cutFace(res.Part2, s.Planes()[0], m.Epsilon())
	if !ok {
		t.Fatal("no cut face recovered")
	}
	if len(groups) != 1 {
		t.Fatalf("got %d face regions, want 1", len(groups))
	}
	if got := math.Abs(signedArea2(groups[0].Outer)) / 2; math.Abs(got-16) > 1e-6 {
		t.Errorf("face area = %v, want 16 — the rectangle's 4x4, not the cube's 100", got)
	}
}

// Cutting the U's left arm gives one face, on that arm alone.
func TestCutFaceOfTheUShape(t *testing.T) {
	m := fixtures.UShape(10)
	s := SpecFromNormal(geom.Vec3{7.5, 25, 5}, geom.Vec3{0, 1, 0}, 15, 20)
	res, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	_, groups, _, _, _, ok := cutFace(res.Part2, s.Planes()[0], m.Epsilon())
	if !ok {
		t.Fatal("no cut face recovered")
	}
	if len(groups) != 1 {
		t.Fatalf("got %d face regions, want 1 — only the left arm was cut", len(groups))
	}
	// The left arm is 10 wide and 10 deep.
	if got := math.Abs(signedArea2(groups[0].Outer)) / 2; math.Abs(got-100) > 1e-6 {
		t.Errorf("face area = %v, want 100", got)
	}
}

func TestCutFaceReportsNothingWhenThePlaneMissesThePart(t *testing.T) {
	m := fixtures.Cube(10)
	s := SpecFromNormal(geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, 1}, 100, 100)
	res, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	far := Plane{N: geom.Vec3{0, 0, 1}, D: 1000}
	if _, _, _, _, _, ok := cutFace(res.Part2, far, m.Epsilon()); ok {
		t.Error("expected no face for a plane nowhere near the part")
	}
}
