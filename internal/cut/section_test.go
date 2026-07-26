package cut

import (
	"math"
	"testing"

	"stl-cutter/internal/fixtures"
	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

func planeAt(origin, normal geom.Vec3) Plane {
	n := normal.Unit()
	return Plane{N: n, D: n.Dot(origin)}
}

// A cube halved crosses in one contour whose area is a whole face.
func TestSectionScoreOnASimpleSolid(t *testing.T) {
	m := fixtures.Cube(10)
	s := SectionScore(m, planeAt(geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, 1}), m.Epsilon())

	if s.Loops != 1 {
		t.Errorf("Loops = %d, want 1", s.Loops)
	}
	if math.Abs(s.Area-100) > 1e-6 {
		t.Errorf("Area = %v, want 100", s.Area)
	}
}

// A tube's section is an annulus, so the hole has to subtract itself.
func TestSectionScoreSubtractsAHole(t *testing.T) {
	m := fixtures.Tube(5, 3, 20, 96)
	s := SectionScore(m, planeAt(geom.Vec3{0, 0, 10}, geom.Vec3{0, 0, 1}), m.Epsilon())

	if s.Loops != 2 {
		t.Errorf("Loops = %d, want 2 — the outside and the bore", s.Loops)
	}
	want := math.Pi * (25 - 9)
	if math.Abs(s.Area-want) > 0.02*want {
		t.Errorf("Area = %v, want about %v", s.Area, want)
	}
}

// The point of the whole thing: a plane through both arms of a U cuts in two places, and
// one through the base cuts in one. That is the difference between splitting a foot in
// half and severing an ankle.
func TestSectionScoreCountsSeparateCuttingLines(t *testing.T) {
	m := fixtures.UShape(10)

	// y=25 is above the base, so it crosses both arms.
	arms := SectionScore(m, planeAt(geom.Vec3{15, 25, 5}, geom.Vec3{0, 1, 0}), m.Epsilon())
	if arms.Loops != 2 {
		t.Errorf("across the arms: Loops = %d, want 2", arms.Loops)
	}

	// y=5 is through the base, one solid slab.
	base := SectionScore(m, planeAt(geom.Vec3{15, 5, 5}, geom.Vec3{0, 1, 0}), m.Epsilon())
	if base.Loops != 1 {
		t.Errorf("through the base: Loops = %d, want 1", base.Loops)
	}
	if !(base.Area > arms.Area) {
		t.Errorf("the base section (%v) should be larger than the two arms (%v)", base.Area, arms.Area)
	}
}

// A narrow waist must score smaller than a wide part, which is what makes the search
// prefer the ankle over the foot.
func TestSectionScoreFindsTheNarrowerPlace(t *testing.T) {
	// A step: 40 wide for the lower half, 10 wide for the upper.
	wide := fixtures.Box(geom.Vec3{0, 0, 0}, geom.Vec3{40, 40, 10})
	narrow := fixtures.Box(geom.Vec3{15, 15, 10}, geom.Vec3{25, 25, 30})
	m := &stl.Mesh{Tris: append(append([]stl.Tri{}, wide.Tris...), narrow.Tris...)}

	low := SectionScore(m, planeAt(geom.Vec3{20, 20, 5}, geom.Vec3{0, 0, 1}), m.Epsilon())
	high := SectionScore(m, planeAt(geom.Vec3{20, 20, 20}, geom.Vec3{0, 0, 1}), m.Epsilon())

	if math.Abs(low.Area-1600) > 1 {
		t.Errorf("through the wide part: Area = %v, want 1600", low.Area)
	}
	if math.Abs(high.Area-100) > 1 {
		t.Errorf("through the narrow part: Area = %v, want 100", high.Area)
	}
	if !(high.Area < low.Area) {
		t.Error("the narrow place must score smaller, or the search cannot prefer it")
	}
}

// A plane that misses the model entirely cuts nothing.
func TestSectionScoreOfAPlaneThatMisses(t *testing.T) {
	m := fixtures.Cube(10)
	s := SectionScore(m, planeAt(geom.Vec3{0, 0, 500}, geom.Vec3{0, 0, 1}), m.Epsilon())
	if s.Loops != 0 || s.Area != 0 {
		t.Errorf("got %+v, want nothing", s)
	}
}

// The whole feature: given a barbell — two heavy ends joined by a thin bar — the cut must
// land on the bar, not through an end. Halving the bounding box puts it in the middle,
// which here happens to be the bar; so the ends are made unequal, which moves the box's
// midpoint into the fat end and only a search that looks at the model still finds the bar.
func TestBestCutPrefersTheNarrowPlace(t *testing.T) {
	// A 60-long fat block, a 40-long thin bar, then a 60-long fat block: 160 total.
	a := fixtures.Box(geom.Vec3{0, 0, 0}, geom.Vec3{60, 50, 50})
	bar := fixtures.Box(geom.Vec3{60, 22, 22}, geom.Vec3{100, 28, 28})
	b := fixtures.Box(geom.Vec3{100, 0, 0}, geom.Vec3{160, 50, 50})
	m := &stl.Mesh{Tris: append(append(append([]stl.Tri{}, a.Tris...), bar.Tris...), b.Tris...)}

	spec, sec, ok := BestCut(m, Bed{X: 110, Y: 100, Z: 100})
	if !ok {
		t.Fatal("no cut was chosen for a part that does not fit")
	}
	if sec.Loops != 1 {
		t.Errorf("Loops = %d, want 1 — a single cutting line", sec.Loops)
	}
	// The bar spans x 60..100 and is 6x6. Anything through an end would be 2500.
	if spec.Origin[0] < 60 || spec.Origin[0] > 100 {
		t.Errorf("the cut is at x=%v, outside the bar at 60..100", spec.Origin[0])
	}
	if sec.Area > 100 {
		t.Errorf("Area = %v; that is a cut through a fat end, not the bar", sec.Area)
	}
}

func TestBestCutDeclinesWhenThePartAlreadyFits(t *testing.T) {
	m := fixtures.Cube(10)
	if _, _, ok := BestCut(m, Bed{X: 100, Y: 100, Z: 100}); ok {
		t.Error("a part that already fits needs no cut")
	}
}

// A part more than twice the bed cannot be fixed by one cut; it must still make progress
// rather than refusing, and the next pass divides again.
func TestBestCutStillDividesAVeryLargePart(t *testing.T) {
	m := fixtures.Cube(300)
	spec, _, ok := BestCut(m, Bed{X: 100, Y: 100, Z: 100})
	if !ok {
		t.Fatal("a part far too large still needs dividing")
	}
	if spec.Origin[0] < 100 || spec.Origin[0] > 200 {
		t.Errorf("the cut is at x=%v; a 300mm cube should be divided near its middle", spec.Origin[0])
	}
}
