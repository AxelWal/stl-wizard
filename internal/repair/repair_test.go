package repair

import (
	"math"
	"testing"

	"stl-cutter/internal/fixtures"
	"stl-cutter/internal/geom"
	"stl-cutter/internal/meshcheck"
	"stl-cutter/internal/stl"
)

func TestRepairClosesAFlatHole(t *testing.T) {
	m := fixtures.OpenBox(10)
	eps := m.Epsilon()
	want := fixtures.Cube(10).Volume()

	res := Repair(m, eps)

	if res.Before.OpenEdges != 4 {
		t.Errorf("Before.OpenEdges = %d, want 4", res.Before.OpenEdges)
	}
	if !res.After.OK() {
		t.Errorf("the box should be closed after repair, got %s", res.After)
	}
	if res.HolesFilled != 1 {
		t.Errorf("HolesFilled = %d, want 1", res.HolesFilled)
	}
	if res.TrianglesAdded == 0 {
		t.Error("no triangles were added, so nothing was filled")
	}

	// The missing face was flat, so a correct fill restores the exact volume. A
	// fan that wandered off the plane, or wound its triangles inconsistently,
	// would not — this is the assertion about fill quality rather than closure.
	if got := m.Volume(); math.Abs(got-want) > 1e-6 {
		t.Errorf("volume = %v, want %v", got, want)
	}
}

// The fill has to face the same way as the surface around it. A hole closed with
// inward-facing triangles is watertight and still prints as a void, and the
// signed volume is the only thing that notices — see the note on translation
// invariance in internal/fixtures.
func TestRepairFillFacesOutward(t *testing.T) {
	m := fixtures.OpenBox(10)
	Repair(m, m.Epsilon())

	off := geom.Vec3{123.5, -71.25, 44.75}
	shifted := &stl.Mesh{Tris: make([]stl.Tri, len(m.Tris))}
	for i, tr := range m.Tris {
		shifted.Tris[i] = stl.Tri{A: tr.A.Add(off), B: tr.B.Add(off), C: tr.C.Add(off)}
	}
	if got, want := shifted.Volume(), m.Volume(); math.Abs(got-want) > 1e-6 {
		t.Errorf("volume changed with translation (%v against %v), so a fill triangle is wound backwards", got, want)
	}
}

func TestRepairLeavesAClosedMeshAlone(t *testing.T) {
	m := fixtures.Cube(10)
	before := len(m.Tris)

	res := Repair(m, m.Epsilon())

	if res.HolesFilled != 0 || res.TrianglesAdded != 0 {
		t.Errorf("a closed mesh should need nothing: filled %d, added %d", res.HolesFilled, res.TrianglesAdded)
	}
	if len(m.Tris) != before {
		t.Errorf("triangle count changed from %d to %d", before, len(m.Tris))
	}
}

func TestRepairDropsDegenerateTriangles(t *testing.T) {
	m := fixtures.Cube(10)
	// A triangle with a repeated vertex: zero area, no valid boundary.
	p := geom.Vec3{1, 2, 3}
	m.Tris = append(m.Tris, stl.Tri{A: p, B: p, C: geom.Vec3{4, 5, 6}})

	res := Repair(m, m.Epsilon())

	if res.DegenerateRemoved != 1 {
		t.Errorf("DegenerateRemoved = %d, want 1", res.DegenerateRemoved)
	}
	if !res.After.OK() {
		t.Errorf("dropping the degenerate should leave the cube sound, got %s", res.After)
	}
}

// A hole with several boundary loops has to be closed loop by loop. Filling them
// as one ring would join two unrelated rims with a sheet of triangles.
func TestRepairClosesEveryLoopSeparately(t *testing.T) {
	m := fixtures.OpenBox(10)
	// A second, unconnected open box makes two independent four-edge holes.
	far := fixtures.OpenBox(10)
	off := geom.Vec3{100, 0, 0}
	for _, tr := range far.Tris {
		m.Tris = append(m.Tris, stl.Tri{A: tr.A.Add(off), B: tr.B.Add(off), C: tr.C.Add(off)})
	}

	res := Repair(m, m.Epsilon())

	if res.HolesFilled != 2 {
		t.Errorf("HolesFilled = %d, want 2", res.HolesFilled)
	}
	if !res.After.OK() {
		t.Errorf("both boxes should be closed, got %s", res.After)
	}
	if got, want := m.Volume(), 2*fixtures.Cube(10).Volume(); math.Abs(got-want) > 1e-6 {
		t.Errorf("volume = %v, want %v", got, want)
	}
}

// Winding is a different problem: flipping one triangle means propagating a
// consistent orientation across the whole surface. Repair must report that
// rather than claim a fix it has not made.
func TestRepairReportsMisorientationWithoutClaimingToFixIt(t *testing.T) {
	m := fixtures.Cube(10)
	m.Tris[0] = m.Tris[0].Reversed()
	before := meshcheck.Check(m, m.Epsilon())
	if before.Misoriented == 0 {
		t.Fatal("the fixture is not misoriented, so this test proves nothing")
	}

	res := Repair(m, m.Epsilon())

	if res.After.Misoriented != before.Misoriented {
		t.Errorf("After.Misoriented = %d, want it left at %d", res.After.Misoriented, before.Misoriented)
	}
	if res.After.OK() {
		t.Error("a misoriented mesh must not be reported as sound")
	}
}

// One hand-picked hole pattern is an anecdote. This sweeps a range of deletion
// patterns across every fixture: whatever holes fall out, repair has to close
// them, and it has to keep the fill facing outward.
//
// Closure is meant to hold by construction — every boundary edge gets exactly one
// new triangle — so a single failure here is a real defect in the loop walk
// rather than a tolerance to be widened. It caught one: loops that revisited a
// vertex used a fan spoke four times, which meshcheck counts as open, so filling
// one hole opened two more.
func TestRepairClosesHolesAcrossManyDeletionPatterns(t *testing.T) {
	solids := map[string]func() *stl.Mesh{
		"cube":      func() *stl.Mesh { return fixtures.Cube(10) },
		"sphere":    func() *stl.Mesh { return fixtures.UVSphere(10, 16, 8) },
		"tube":      func() *stl.Mesh { return fixtures.Tube(5, 3, 20, 24) },
		"u":         func() *stl.Mesh { return fixtures.UShape(10) },
		"hollowbox": func() *stl.Mesh { return fixtures.HollowBox(geom.Vec3{20, 20, 20}, 2) },
	}

	cases, failures := 0, 0
	for name, build := range solids {
		for _, every := range []int{2, 3, 5, 7, 11, 13, 17, 23} {
			for _, offset := range []int{0, 1, 2} {
				whole := build()
				m := build()
				kept := make([]stl.Tri, 0, len(m.Tris))
				for i, tr := range m.Tris {
					if i%every == offset {
						continue
					}
					kept = append(kept, tr)
				}
				m.Tris = kept
				eps := whole.Epsilon()
				if len(m.Tris) == 0 || meshcheck.Check(m, eps).OpenEdges == 0 {
					continue // nothing was opened, so there is nothing to prove
				}

				cases++
				res := Repair(m, eps)
				if res.After.OpenEdges != 0 {
					failures++
					t.Errorf("%s every %d offset %d: %d open edges remain (%s)",
						name, every, offset, res.After.OpenEdges, res.After)
					continue
				}

				// Translation invariance is the only check that sees a backwards
				// fill triangle; a closed mesh's volume must not move with it.
				off := geom.Vec3{321.5, -87.25, 55.75}
				shifted := &stl.Mesh{Tris: make([]stl.Tri, len(m.Tris))}
				for i, tr := range m.Tris {
					shifted.Tris[i] = stl.Tri{A: tr.A.Add(off), B: tr.B.Add(off), C: tr.C.Add(off)}
				}
				if got, want := shifted.Volume(), m.Volume(); math.Abs(got-want) > 1e-6*math.Max(1, math.Abs(want)) {
					failures++
					t.Errorf("%s every %d offset %d: volume moved with translation (%v against %v), so a fill triangle is backwards",
						name, every, offset, got, want)
				}
			}
		}
	}

	if cases < 50 {
		t.Fatalf("only %d cases actually opened a hole; the sweep is not exercising anything", cases)
	}
	t.Logf("closed %d of %d holed meshes", cases-failures, cases)
}

// A hole whose rim is not flat still has to be closed. The fill will not be
// geometrically ideal, but every boundary edge must end up shared.
func TestRepairClosesANonPlanarHole(t *testing.T) {
	m := fixtures.UVSphere(10, 16, 8)
	// Remove a band of triangles to open a ragged, distinctly non-planar hole.
	kept := make([]stl.Tri, 0, len(m.Tris))
	for i, tr := range m.Tris {
		if i%17 == 0 {
			continue
		}
		kept = append(kept, tr)
	}
	m.Tris = kept
	if meshcheck.Check(m, m.Epsilon()).OpenEdges == 0 {
		t.Fatal("the fixture is not open, so this test proves nothing")
	}

	res := Repair(m, m.Epsilon())

	if res.After.OpenEdges != 0 {
		t.Errorf("OpenEdges = %d after repair, want 0; %s", res.After.OpenEdges, res.After)
	}
}
