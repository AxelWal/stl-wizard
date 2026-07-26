package shells

import (
	"math"
	"testing"

	"stl-wizard/internal/fixtures"
	"stl-wizard/internal/geom"
	"stl-wizard/internal/meshcheck"
	"stl-wizard/internal/stl"
)

func TestSplitReturnsOneMeshForASingleSolid(t *testing.T) {
	m := fixtures.Cube(10)
	got := Split(m, m.Epsilon())

	if len(got) != 1 {
		t.Fatalf("got %d bodies, want 1", len(got))
	}
	if len(got[0].Tris) != 12 {
		t.Errorf("got %d triangles, want all 12", len(got[0].Tris))
	}
	if v, want := got[0].Volume(), m.Volume(); math.Abs(v-want) > 1e-9 {
		t.Errorf("volume = %v, want %v", v, want)
	}
}

func TestSplitSeparatesTwoDisjointSolids(t *testing.T) {
	m := fixtures.Cube(10)
	far := fixtures.Cube(6)
	for _, tr := range far.Tris {
		off := geom.Vec3{100, 0, 0}
		m.Tris = append(m.Tris, stl.Tri{A: tr.A.Add(off), B: tr.B.Add(off), C: tr.C.Add(off)})
	}

	got := Split(m, m.Epsilon())

	if len(got) != 2 {
		t.Fatalf("got %d bodies, want 2", len(got))
	}
	// Largest first, so the 10mm cube leads.
	if v, want := got[0].Volume(), 1000.0; math.Abs(v-want) > 1e-9 {
		t.Errorf("first body volume = %v, want %v", v, want)
	}
	if v, want := got[1].Volume(), 216.0; math.Abs(v-want) > 1e-9 {
		t.Errorf("second body volume = %v, want %v", v, want)
	}
	if n := len(got[0].Tris) + len(got[1].Tris); n != len(m.Tris) {
		t.Errorf("bodies hold %d triangles between them, want all %d", n, len(m.Tris))
	}
}

// The point of the whole feature: two bodies touching along one edge are one
// non-manifold mesh, and separating them needs no geometry change to make each a
// closed solid. This is the case repair is documented as unable to fix.
func TestSplitMakesTouchingBodiesSoundOnTheirOwn(t *testing.T) {
	m := fixtures.TouchingCubes(10)
	eps := m.Epsilon()
	if meshcheck.Check(m, eps).OK() {
		t.Fatal("the fixture is already sound, so this test proves nothing")
	}

	got := Split(m, eps)

	if len(got) != 2 {
		t.Fatalf("got %d bodies, want 2", len(got))
	}
	for i, b := range got {
		if rep := meshcheck.Check(b, eps); !rep.OK() {
			t.Errorf("body %d on its own: %s", i, rep)
		}
		if v, want := b.Volume(), 1000.0; math.Abs(v-want) > 1e-9 {
			t.Errorf("body %d volume = %v, want %v", i, v, want)
		}
	}
}

// A hollow model's internal void is its own surface with negative volume.
// Returning it as a body would hand the caller a solid box and an inside-out box.
func TestSplitKeepsAHollowModelWhole(t *testing.T) {
	m := fixtures.HollowBox(geom.Vec3{20, 20, 20}, 2)
	want := m.Volume()

	got := Split(m, m.Epsilon())

	if len(got) != 1 {
		t.Fatalf("got %d bodies, want 1 — the void is not a body", len(got))
	}
	if len(got[0].Tris) != len(m.Tris) {
		t.Errorf("got %d triangles, want all %d", len(got[0].Tris), len(m.Tris))
	}
	if v := got[0].Volume(); math.Abs(v-want) > 1e-9 {
		t.Errorf("volume = %v, want the shell's %v", v, want)
	}
}

// Debris sitting inside a body's bounding box belongs to that body, not beside it.
func TestSplitGroupsNestedDebrisWithItsContainer(t *testing.T) {
	m := fixtures.Cube(20)
	// A tiny flap well inside the cube.
	p := geom.Vec3{9, 9, 9}
	q := geom.Vec3{11, 9, 9}
	r := geom.Vec3{10, 11, 9}
	flap := stl.Tri{A: p, B: q, C: r}
	m.Tris = append(m.Tris, flap, flap.Reversed())

	got := Split(m, m.Epsilon())

	if len(got) != 1 {
		t.Fatalf("got %d bodies, want 1 — the flap is inside the cube", len(got))
	}
	if len(got[0].Tris) != 14 {
		t.Errorf("got %d triangles, want all 14", len(got[0].Tris))
	}
}

func TestSplitHandlesAnEmptyMesh(t *testing.T) {
	if got := Split(&stl.Mesh{}, 1e-6); len(got) != 0 {
		t.Errorf("got %d bodies for an empty mesh, want 0", len(got))
	}
}

// Every triangle has to end up in exactly one body, whatever the shape. Losing or
// duplicating one would change what prints.
func TestSplitPartitionsEveryTriangle(t *testing.T) {
	cases := map[string]*stl.Mesh{
		"cube":          fixtures.Cube(10),
		"sphere":        fixtures.UVSphere(10, 16, 8),
		"tube":          fixtures.Tube(5, 3, 20, 24),
		"u":             fixtures.UShape(10),
		"hollowbox":     fixtures.HollowBox(geom.Vec3{20, 20, 20}, 2),
		"touchingcubes": fixtures.TouchingCubes(10),
		"cubewithflap":  fixtures.CubeWithFlap(10),
		"openbox":       fixtures.OpenBox(10),
	}
	for name, m := range cases {
		total := 0
		var vol float64
		for _, b := range Split(m, m.Epsilon()) {
			total += len(b.Tris)
			vol += b.Volume()
		}
		if total != len(m.Tris) {
			t.Errorf("%s: bodies hold %d triangles, want all %d", name, total, len(m.Tris))
		}
		if want := m.Volume(); math.Abs(vol-want) > 1e-6*math.Max(1, math.Abs(want)) {
			t.Errorf("%s: body volumes sum to %v, want %v", name, vol, want)
		}
	}
}
