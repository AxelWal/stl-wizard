package meshcheck

import (
	"testing"

	"stl-cutter/internal/fixtures"
	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

func TestClosedSolidsAreWatertight(t *testing.T) {
	for name, m := range map[string]*stl.Mesh{
		"cube":      fixtures.Cube(10),
		"sphere":    fixtures.UVSphere(5, 24, 12),
		"tube":      fixtures.Tube(4, 2, 8, 32),
		"hollowbox": fixtures.HollowBox(geom.Vec3{10, 10, 10}, 1),
		"ushape":    fixtures.UShape(10),
	} {
		if rep := Check(m, m.Epsilon()); !rep.OK() {
			t.Errorf("%s: %s", name, rep)
		}
	}
}

func TestNonManifoldIsDetected(t *testing.T) {
	m := fixtures.NonManifold()
	rep := Check(m, m.Epsilon())
	if rep.OK() {
		t.Fatal("expected the holed cube to fail the watertightness check")
	}
	if rep.OpenEdges == 0 {
		t.Fatal("OpenEdges = 0, want the three edges of the missing triangle")
	}
}

func TestMisorientedTriangleIsDetected(t *testing.T) {
	m := fixtures.Cube(10)
	m.Tris[0] = m.Tris[0].Reversed()
	rep := Check(m, m.Epsilon())
	if rep.OK() {
		t.Fatal("expected a flipped triangle to fail the check")
	}
	if rep.Misoriented == 0 {
		t.Fatal("Misoriented = 0, want the flipped triangle's edges")
	}
}
