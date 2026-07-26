package cut

import (
	"math"
	"strings"
	"testing"

	"stl-wizard/internal/fixtures"
	"stl-wizard/internal/geom"
	"stl-wizard/internal/meshcheck"
	"stl-wizard/internal/stl"
)

// assertInvariants checks the three properties from the spec that must hold for
// every cut: volume is conserved, both parts are watertight, and geometry
// outside the cutter is untouched.
func assertInvariants(t *testing.T, in *stl.Mesh, r *Result) {
	t.Helper()
	eps := in.Epsilon()

	sum := r.Part1.Volume() + r.Part2.Volume()
	want := in.Volume()
	// Scale the tolerance to the model: a 100mm part's volume is 1e6 mm³, so an
	// absolute epsilon would be meaninglessly strict.
	tol := math.Max(1e-6, math.Abs(want)*1e-9)
	if math.Abs(sum-want) > tol {
		t.Errorf("volume not conserved: part1=%v + part2=%v = %v, want %v (diff %v)",
			r.Part1.Volume(), r.Part2.Volume(), sum, want, sum-want)
	}

	if rep := meshcheck.Check(r.Part1, eps); !rep.OK() {
		t.Errorf("part1 %s", rep)
	}
	if rep := meshcheck.Check(r.Part2, eps); !rep.OK() {
		t.Errorf("part2 %s", rep)
	}
}

// assertOutsideIdentity is the property that proves the cut really is bounded:
// a triangle wholly outside the cutter must reappear in part1 bit for bit.
func assertOutsideIdentity(t *testing.T, in *stl.Mesh, s Spec, r *Result) {
	t.Helper()
	eps := in.Epsilon()
	planes := s.Planes()

	have := make(map[stl.Tri]int, len(r.Part1.Tris))
	for _, tr := range r.Part1.Tris {
		have[tr]++
	}

	checked := 0
	for _, tr := range in.Tris {
		outside := false
		for _, p := range planes {
			if p.Classify(tr.A, eps) == Outside &&
				p.Classify(tr.B, eps) == Outside &&
				p.Classify(tr.C, eps) == Outside {
				outside = true
				break
			}
		}
		if !outside {
			continue
		}
		checked++
		if have[tr] == 0 {
			t.Fatalf("triangle %v lies entirely outside the cutter but is not in part1 unchanged", tr)
		}
	}
	if checked == 0 {
		t.Fatal("no triangle was wholly outside the cutter; this fixture cannot test the identity property")
	}
}

func TestSplitCubeInHalf(t *testing.T) {
	m := fixtures.Cube(10)
	// Rectangle large enough to span the whole cross-section, so this behaves as
	// an unbounded plane.
	s := SpecFromNormal(geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, 1}, 100, 100)

	r, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	assertInvariants(t, m, r)

	if got := r.Part2.Volume(); math.Abs(got-500) > 1e-6 {
		t.Errorf("part2 volume = %v, want 500", got)
	}
	if got := r.Part1.Volume(); math.Abs(got-500) > 1e-6 {
		t.Errorf("part1 volume = %v, want 500", got)
	}
}

func TestSplitCubeOnAnObliquePlane(t *testing.T) {
	m := fixtures.Cube(10)
	s := SpecFromNormal(geom.Vec3{5, 5, 5}, geom.Vec3{1, 1, 1}, 100, 100)

	r, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	assertInvariants(t, m, r)
	// A plane through the centre of a cube halves it whatever its orientation.
	if got := r.Part2.Volume(); math.Abs(got-500) > 1e-3 {
		t.Errorf("part2 volume = %v, want 500", got)
	}
}

func TestSplitSphere(t *testing.T) {
	m := fixtures.UVSphere(10, 48, 24)
	s := SpecFromNormal(geom.Vec3{}, geom.Vec3{0, 0, 1}, 100, 100)

	r, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	assertInvariants(t, m, r)
	if math.Abs(r.Part1.Volume()-r.Part2.Volume()) > 1e-3 {
		t.Errorf("halves differ: %v vs %v", r.Part1.Volume(), r.Part2.Volume())
	}
}

// A tube cut across its axis produces annular caps, so this exercises hole
// handling inside the full pipeline.
func TestSplitTubeProducesAnnularCaps(t *testing.T) {
	m := fixtures.Tube(5, 3, 20, 48)
	s := SpecFromNormal(geom.Vec3{0, 0, 10}, geom.Vec3{0, 0, 1}, 100, 100)

	r, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	assertInvariants(t, m, r)
}

// The motivating case. A plane at y=25 bounded to x in [0,15] must take the top
// off the left arm and leave the right arm attached to the base.
func TestSplitUShapeCutsOneArmOnly(t *testing.T) {
	m := fixtures.UShape(10)
	s := SpecFromNormal(geom.Vec3{7.5, 25, 5}, geom.Vec3{0, 1, 0}, 15, 20)

	r, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	assertInvariants(t, m, r)
	assertOutsideIdentity(t, m, s, r)

	// The severed piece is the left arm above y=25: 10 wide, 15 tall, 10 deep.
	if got := r.Part2.Volume(); math.Abs(got-1500) > 1e-6 {
		t.Errorf("part2 volume = %v, want 1500", got)
	}
	if got := r.Part1.Volume(); math.Abs(got-7500) > 1e-6 {
		t.Errorf("part1 volume = %v, want 7500", got)
	}

	// part2 must be the left arm alone.
	if b := r.Part2.BBox(); b.Min[0] < -1e-9 || b.Max[0] > 10+1e-9 {
		t.Errorf("part2 spans x %v..%v, want it confined to the left arm 0..10", b.Min[0], b.Max[0])
	}

	// The decisive check: the right arm is still there, still above the cut
	// height, still part of part1.
	rightArmHigh := false
	for _, tr := range r.Part1.Tris {
		for _, v := range [3]geom.Vec3{tr.A, tr.B, tr.C} {
			if v[0] > 20-1e-9 && v[1] > 39-1e-9 {
				rightArmHigh = true
			}
		}
	}
	if !rightArmHigh {
		t.Error("the right arm's top is missing from part1; the bounded plane cut it too")
	}
}

// Same U, but the rectangle now reaches across both arms, so both get cut. This
// is the control that proves the bound in the previous test is what did the work.
func TestSplitUShapeWithAWideRectangleCutsBothArms(t *testing.T) {
	m := fixtures.UShape(10)
	s := SpecFromNormal(geom.Vec3{15, 25, 5}, geom.Vec3{0, 1, 0}, 100, 20)

	r, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	assertInvariants(t, m, r)

	// Both arms above y=25: 2 x 10 x 15 x 10.
	if got := r.Part2.Volume(); math.Abs(got-3000) > 1e-6 {
		t.Errorf("part2 volume = %v, want 3000", got)
	}
}

// A rectangle whose edges pass through material still yields watertight parts —
// the cut simply continues along the rectangle's side planes.
func TestSplitWithRectangleEdgesInsideMaterial(t *testing.T) {
	m := fixtures.Cube(10)
	s := SpecFromNormal(geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, 1}, 4, 4)

	r, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	assertInvariants(t, m, r)

	// part2 is the 4x4 column above z=5: 4 * 4 * 5.
	if got := r.Part2.Volume(); math.Abs(got-80) > 1e-6 {
		t.Errorf("part2 volume = %v, want 80", got)
	}
}

func TestSplitRejectsAPlaneThatMissesTheModel(t *testing.T) {
	m := fixtures.Cube(10)
	s := SpecFromNormal(geom.Vec3{5, 5, 500}, geom.Vec3{0, 0, 1}, 100, 100)
	if _, err := Split(m, s); err == nil {
		t.Fatal("expected an error when the cutter contains no material")
	}
}

func TestSplitRejectsARectangleThatMissesTheModel(t *testing.T) {
	m := fixtures.Cube(10)
	s := SpecFromNormal(geom.Vec3{500, 500, 5}, geom.Vec3{0, 0, 1}, 4, 4)
	if _, err := Split(m, s); err == nil {
		t.Fatal("expected an error when the rectangle sits beside the model")
	}
}

func TestSplitRejectsACutterSwallowingTheWholeModel(t *testing.T) {
	m := fixtures.Cube(10)
	s := SpecFromNormal(geom.Vec3{5, 5, -50}, geom.Vec3{0, 0, 1}, 100, 100)
	if _, err := Split(m, s); err == nil {
		t.Fatal("expected an error when nothing is left behind")
	}
}

func TestSplitRejectsAnInvalidSpec(t *testing.T) {
	if _, err := Split(fixtures.Cube(10), Spec{}); err == nil {
		t.Fatal("expected an error for a zero-value spec")
	}
}

func TestSplitRejectsAnEmptyMesh(t *testing.T) {
	s := SpecFromNormal(geom.Vec3{}, geom.Vec3{0, 0, 1}, 10, 10)
	if _, err := Split(&stl.Mesh{}, s); err == nil {
		t.Fatal("expected an error for a mesh with no triangles")
	}
}

// Non-manifold input is reported, not rejected and not silently accepted. The
// caller decides whether to keep a part that is flagged as not watertight.
func TestSplitReportsNonManifoldInputWithoutFailing(t *testing.T) {
	m := fixtures.NonManifold()
	s := SpecFromNormal(geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, 1}, 100, 100)

	r, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split should report, not fail: %v", err)
	}
	if r.OpenLoops == 0 {
		t.Error("OpenLoops = 0, want the unclosed boundary reported")
	}
	if r.Watertight() {
		t.Error("Watertight() = true, want false")
	}
	if len(r.Warnings) == 0 {
		t.Error("Warnings is empty, want an explanation of what went wrong")
	}
}

// Placing the plane exactly on a face is ambiguous, and the user must be told
// rather than handed a silently wrong volume.
//
// The U at y=10 is the case that makes this reachable: the notch floor is a face
// lying exactly in the plane, and material remains on both sides, so Split
// proceeds rather than refusing for want of a part.
func TestSplitWarnsWhenThePlaneIsCoplanarWithFaces(t *testing.T) {
	m := fixtures.UShape(10)
	s := SpecFromNormal(geom.Vec3{15, 10, 5}, geom.Vec3{0, 1, 0}, 100, 100)

	r, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(r.Warnings) == 0 {
		t.Fatal("expected a warning that the plane is coplanar with model faces")
	}

	found := false
	for _, w := range r.Warnings {
		if strings.Contains(w, "flat against") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings do not mention the coplanar faces: %v", r.Warnings)
	}
}

// A cut on a large model takes seconds, so callers need to be able to show
// progress. The contract is deliberately loose — coarse, monotonic, ending at 1 —
// because tying it to internal loop structure would freeze that structure.
func TestSplitProgressReportsMonotonicFractions(t *testing.T) {
	m := fixtures.UVSphere(10, 48, 24)
	s := SpecFromNormal(geom.Vec3{}, geom.Vec3{0, 0, 1}, 100, 100)

	var seen []float64
	res, err := SplitProgress(m, s, func(f float64) {
		seen = append(seen, f)
	})
	if err != nil {
		t.Fatalf("SplitProgress: %v", err)
	}
	if res == nil {
		t.Fatal("no result")
	}
	if len(seen) < 2 {
		t.Fatalf("got %d progress reports, want several", len(seen))
	}
	for i, f := range seen {
		if f < 0 || f > 1 {
			t.Errorf("report %d is %v, want a fraction in [0,1]", i, f)
		}
		if i > 0 && f < seen[i-1] {
			t.Errorf("progress went backwards: %v then %v", seen[i-1], f)
		}
	}
	if last := seen[len(seen)-1]; last != 1 {
		t.Errorf("final report is %v, want exactly 1", last)
	}
}

// The plain Split must behave identically, so no caller has to care.
func TestSplitMatchesSplitProgressWithNoCallback(t *testing.T) {
	m := fixtures.Cube(10)
	s := SpecFromNormal(geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, 1}, 100, 100)

	a, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	b, err := SplitProgress(m, s, nil)
	if err != nil {
		t.Fatalf("SplitProgress: %v", err)
	}
	if a.Part1.Volume() != b.Part1.Volume() || a.Part2.Volume() != b.Part2.Volume() {
		t.Error("Split and SplitProgress produced different geometry")
	}
}

// The periodic reports during the part-1 pass fire every 4096 triangles, and both
// other progress tests use meshes far below that, so this is the only test that
// exercises that arithmetic at all.
func TestSplitProgressReportsDuringTheLongPass(t *testing.T) {
	m := fixtures.UVSphere(10, 120, 100)
	if len(m.Tris) <= 4096 {
		t.Fatalf("fixture has %d triangles; this test needs more than the 4096 reporting interval", len(m.Tris))
	}
	s := SpecFromNormal(geom.Vec3{}, geom.Vec3{0, 0, 1}, 100, 100)

	var seen []float64
	if _, err := SplitProgress(m, s, func(f float64) { seen = append(seen, f) }); err != nil {
		t.Fatalf("SplitProgress: %v", err)
	}

	// Five plane reports plus at least one periodic report plus the final 1.
	if len(seen) < 7 {
		t.Errorf("got %d reports %v, want at least 7 — the periodic path did not fire", len(seen), seen)
	}
	// At least one report must land strictly between the plane phase and the end,
	// which is what the periodic path produces.
	periodic := 0
	for _, f := range seen {
		if f > 0.5 && f < 1 {
			periodic++
		}
	}
	if periodic == 0 {
		t.Errorf("no report fell between 0.5 and 1 in %v; the periodic path did not fire", seen)
	}
	for i, f := range seen {
		if f < 0 || f > 1 {
			t.Errorf("report %d is %v, want a fraction in [0,1]", i, f)
		}
		if i > 0 && f < seen[i-1] {
			t.Errorf("progress went backwards: %v then %v", seen[i-1], f)
		}
	}
	if last := seen[len(seen)-1]; last != 1 {
		t.Errorf("final report is %v, want exactly 1", last)
	}
}
