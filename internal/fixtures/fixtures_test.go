package fixtures

import (
	"math"
	"testing"

	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

func TestCubeVolumeAndTriangleCount(t *testing.T) {
	m := Cube(2)
	if len(m.Tris) != 12 {
		t.Fatalf("got %d triangles, want 12", len(m.Tris))
	}
	if got := m.Volume(); math.Abs(got-8) > 1e-9 {
		t.Fatalf("Volume = %v, want 8", got)
	}
}

func TestBoxRespectsBounds(t *testing.T) {
	m := Box(geom.Vec3{-1, -2, -3}, geom.Vec3{1, 2, 3})
	b := m.BBox()
	if b.Min != (geom.Vec3{-1, -2, -3}) || b.Max != (geom.Vec3{1, 2, 3}) {
		t.Fatalf("BBox = %v..%v, want -1,-2,-3..1,2,3", b.Min, b.Max)
	}
	if got := m.Volume(); math.Abs(got-48) > 1e-9 {
		t.Fatalf("Volume = %v, want 48", got)
	}
}

// A tessellated sphere under-approximates the ideal one, so this checks the
// order of magnitude rather than the exact figure. Cut tests compare against
// the mesh's own volume, so tessellation error never matters there.
func TestUVSphereVolumeIsCloseToIdeal(t *testing.T) {
	m := UVSphere(1, 32, 16)
	ideal := 4.0 / 3.0 * math.Pi
	got := m.Volume()
	if got <= 0 {
		t.Fatalf("Volume = %v, want positive (winding is inverted)", got)
	}
	if math.Abs(got-ideal)/ideal > 0.02 {
		t.Fatalf("Volume = %v, want within 2%% of %v", got, ideal)
	}
}

func TestTubeVolumeMatchesAnnulus(t *testing.T) {
	m := Tube(2, 1, 5, 64)
	ideal := math.Pi * (4 - 1) * 5
	got := m.Volume()
	if got <= 0 {
		t.Fatalf("Volume = %v, want positive", got)
	}
	if math.Abs(got-ideal)/ideal > 0.02 {
		t.Fatalf("Volume = %v, want within 2%% of %v", got, ideal)
	}
}

func TestHollowBoxVolumeIsShellOnly(t *testing.T) {
	m := HollowBox(geom.Vec3{10, 10, 10}, 1)
	want := 1000.0 - 512.0 // 10^3 minus 8^3
	if got := m.Volume(); math.Abs(got-want) > 1e-9 {
		t.Fatalf("Volume = %v, want %v", got, want)
	}
}

func TestUShapeVolumeIsProfileTimesDepth(t *testing.T) {
	m := UShape(10)
	want := UShapeProfileArea * 10
	if got := m.Volume(); math.Abs(got-want) > 1e-9 {
		t.Fatalf("Volume = %v, want %v", got, want)
	}
}

// The U is the motivating case for the whole project: a bounded plane must be
// able to cut one arm without touching the other. Pin down its geometry so the
// cut tests can rely on exact coordinates.
func TestUShapeGeometryIsAsDocumented(t *testing.T) {
	b := UShape(10).BBox()
	if b.Min != (geom.Vec3{0, 0, 0}) || b.Max != (geom.Vec3{30, 40, 10}) {
		t.Fatalf("BBox = %v..%v, want 0,0,0..30,40,10", b.Min, b.Max)
	}
}

func TestNonManifoldHasAHole(t *testing.T) {
	if got := len(NonManifold().Tris); got != 11 {
		t.Fatalf("got %d triangles, want 11 (a cube with one face triangle removed)", got)
	}
}

// translate returns a copy of m shifted by off.
func translate(m *stl.Mesh, off geom.Vec3) *stl.Mesh {
	out := &stl.Mesh{Tris: make([]stl.Tri, len(m.Tris))}
	for i, t := range m.Tris {
		out.Tris[i] = stl.Tri{A: t.A.Add(off), B: t.B.Add(off), C: t.C.Add(off)}
	}
	return out
}

// Volume() is a signed sum taken about the origin, so a triangle whose plane
// passes through the origin contributes exactly zero and its winding cannot be
// seen by any assertion about volume. Every fixture here is built from the
// origin, which leaves large parts of each one unchecked — for Tube it is the
// whole bottom annulus.
//
// Translating away from the origin makes every face contribute. For a closed,
// consistently wound mesh the signed volume is translation-invariant, because the
// area-weighted normals sum to zero; flip a single face and that sum is non-zero,
// so the computed volume starts drifting with the offset. This test is therefore
// the one that actually pins down the winding of every triangle.
func TestFixtureVolumesAreTranslationInvariant(t *testing.T) {
	// Deliberately far from the model and on no axis, so no face stays coplanar
	// with the origin.
	off := geom.Vec3{123.5, -456.25, 789.125}

	for name, m := range map[string]*stl.Mesh{
		"cube":      Cube(10),
		"box":       Box(geom.Vec3{-1, -2, -3}, geom.Vec3{1, 2, 3}),
		"sphere":    UVSphere(5, 24, 12),
		"tube":      Tube(4, 2, 8, 32),
		"hollowbox": HollowBox(geom.Vec3{10, 10, 10}, 1),
		"ushape":    UShape(10),
	} {
		before := m.Volume()
		after := translate(m, off).Volume()
		tol := math.Max(1e-9, math.Abs(before)*1e-9)
		if math.Abs(after-before) > tol {
			t.Errorf("%s: volume %v about the origin but %v after translation (drift %v) — a face is wound backwards",
				name, before, after, after-before)
		}
	}
}
