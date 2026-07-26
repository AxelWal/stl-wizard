package meshcheck

import (
	"testing"

	"stl-wizard/internal/fixtures"
	"stl-wizard/internal/geom"
	"stl-wizard/internal/stl"
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
	if rep.OpenEdges != 3 {
		t.Fatalf("OpenEdges = %d, want 3 (the edges of the missing triangle)", rep.OpenEdges)
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

// A lone triangle with a repeated vertex must not be able to pass as a closed
// surface. Its degenerate edge used to be skipped while its two remaining
// iterations addressed the same real edge in both directions, self-cancelling
// into a fictional properly-shared edge — so Check returned OK for a mesh that is
// nothing but a sliver.
func TestDegenerateTriangleCannotPassAsClosed(t *testing.T) {
	m := &stl.Mesh{Tris: []stl.Tri{{
		A: geom.Vec3{0, 0, 0},
		B: geom.Vec3{0, 0, 0}, // repeated vertex: zero area
		C: geom.Vec3{1, 0, 0},
	}}}

	rep := Check(m, m.Epsilon())
	if rep.OK() {
		t.Fatal("a single degenerate triangle reported as watertight")
	}
	if rep.Degenerate != 1 {
		t.Errorf("Degenerate = %d, want 1", rep.Degenerate)
	}
}

// A degenerate triangle alongside real geometry must be reported without
// disturbing the edge tallies of the surrounding mesh.
func TestDegenerateTriangleIsReportedAlongsideAClosedSolid(t *testing.T) {
	m := fixtures.Cube(10)
	m.Tris = append(m.Tris, stl.Tri{
		A: geom.Vec3{2, 2, 2},
		B: geom.Vec3{3, 3, 3},
		C: geom.Vec3{3, 3, 3},
	})

	rep := Check(m, m.Epsilon())
	if rep.Degenerate != 1 {
		t.Errorf("Degenerate = %d, want 1", rep.Degenerate)
	}
	if rep.OpenEdges != 0 || rep.Misoriented != 0 {
		t.Errorf("the cube's own edges were disturbed: %s", rep)
	}
}
