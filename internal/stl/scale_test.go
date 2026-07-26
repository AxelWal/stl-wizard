// An external test package, so it can use fixtures and meshcheck: both import stl, so
// an internal test importing them would be a cycle.
package stl_test

import (
	"math"
	"testing"

	"stl-wizard/internal/fixtures"
	"stl-wizard/internal/geom"
	"stl-wizard/internal/meshcheck"
	"stl-wizard/internal/stl"
)

// The volume identity is the strong check: it holds only if every vertex was
// transformed and none was missed or transformed twice.
func TestScaledMultipliesVolumeByTheProductOfTheFactors(t *testing.T) {
	for _, f := range [][3]float64{
		{1, 1, 1},
		{2, 2, 2},
		{2, 3, 5},
		{0.5, 1, 4},
		{1e-3, 1e3, 1},
	} {
		m := fixtures.Cube(10)
		want := m.Volume() * f[0] * f[1] * f[2]
		got := m.Scaled(f).Volume()
		if math.Abs(got-want) > 1e-6*math.Max(1, math.Abs(want)) {
			t.Errorf("scaling by %v gave volume %v, want %v", f, got, want)
		}
	}
}

// Positive factors preserve winding, so a scaled solid stays closed. A non-uniform
// scale is the case that would catch a normal being transformed as if it were a point.
func TestScaledStaysAClosedSolid(t *testing.T) {
	for _, f := range [][3]float64{{2, 2, 2}, {1, 5, 0.2}, {7, 1, 1}} {
		for name, m := range map[string]*stl.Mesh{
			"cube":   fixtures.Cube(10),
			"sphere": fixtures.UVSphere(10, 24, 12),
			"tube":   fixtures.Tube(5, 3, 20, 24),
			"u":      fixtures.UShape(10),
		} {
			s := m.Scaled(f)
			if rep := meshcheck.Check(s, s.Epsilon()); !rep.OK() {
				t.Errorf("%s scaled by %v: %s", name, f, rep)
			}
			// And it must not have turned inside out: a mirrored solid is consistently
			// wound and has a negative volume, which no other check would notice.
			if s.Volume() <= 0 {
				t.Errorf("%s scaled by %v has volume %v; it is inside out", name, f, s.Volume())
			}
		}
	}
}

func TestScaledScalesTheBoundingBox(t *testing.T) {
	m := fixtures.Box(geom.Vec3{1, 2, 3}, geom.Vec3{5, 8, 11})
	s := m.Scaled([3]float64{2, 3, 4})
	b := s.BBox()
	for k, want := range [3]float64{2, 6, 12} {
		if math.Abs(b.Min[k]-want) > 1e-9 {
			t.Errorf("min[%d] = %v, want %v", k, b.Min[k], want)
		}
	}
	for k, want := range [3]float64{10, 24, 44} {
		if math.Abs(b.Max[k]-want) > 1e-9 {
			t.Errorf("max[%d] = %v, want %v", k, b.Max[k], want)
		}
	}
}

// The session keeps the mesh as loaded and scales a copy, so Scaled must not touch it.
func TestScaledLeavesTheOriginalAlone(t *testing.T) {
	m := fixtures.Cube(10)
	before := m.Volume()
	beforeTris := len(m.Tris)

	_ = m.Scaled([3]float64{3, 3, 3})

	if got := m.Volume(); math.Abs(got-before) > 1e-9 {
		t.Errorf("the original's volume changed from %v to %v", before, got)
	}
	if len(m.Tris) != beforeTris {
		t.Errorf("the original's triangle count changed")
	}
}
