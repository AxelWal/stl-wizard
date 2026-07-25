package stl

import (
	"math"
	"testing"

	"stl-cutter/internal/geom"
)

// unitCube returns a 1x1x1 cube at the origin with outward-facing normals,
// written out explicitly so this test does not depend on the fixtures package.
func unitCube() *Mesh {
	v := [8]geom.Vec3{
		{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0},
		{0, 0, 1}, {1, 0, 1}, {1, 1, 1}, {0, 1, 1},
	}
	return &Mesh{Tris: []Tri{
		{v[0], v[2], v[1]}, {v[0], v[3], v[2]}, // bottom, -Z
		{v[4], v[5], v[6]}, {v[4], v[6], v[7]}, // top, +Z
		{v[0], v[1], v[5]}, {v[0], v[5], v[4]}, // front, -Y
		{v[3], v[6], v[2]}, {v[3], v[7], v[6]}, // back, +Y
		{v[0], v[7], v[3]}, {v[0], v[4], v[7]}, // left, -X
		{v[1], v[2], v[6]}, {v[1], v[6], v[5]}, // right, +X
	}}
}

func TestTriNormalAndArea(t *testing.T) {
	tr := Tri{geom.Vec3{0, 0, 0}, geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0}}
	if got := tr.Normal(); got != (geom.Vec3{0, 0, 1}) {
		t.Fatalf("Normal = %v, want +Z", got)
	}
	if got := tr.Area(); math.Abs(got-0.5) > 1e-12 {
		t.Fatalf("Area = %v, want 0.5", got)
	}
}

func TestTriReversedFlipsNormal(t *testing.T) {
	tr := Tri{geom.Vec3{0, 0, 0}, geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0}}
	if got := tr.Reversed().Normal(); got != (geom.Vec3{0, 0, -1}) {
		t.Fatalf("reversed Normal = %v, want -Z", got)
	}
}

func TestBBoxOfUnitCube(t *testing.T) {
	b := unitCube().BBox()
	if b.Min != (geom.Vec3{0, 0, 0}) || b.Max != (geom.Vec3{1, 1, 1}) {
		t.Fatalf("BBox = %v..%v, want 0,0,0..1,1,1", b.Min, b.Max)
	}
	if got := b.Diagonal(); math.Abs(got-math.Sqrt(3)) > 1e-12 {
		t.Fatalf("Diagonal = %v, want sqrt(3)", got)
	}
}

// Volume is the primary invariant every cut test leans on, so its sign
// convention must be pinned down here: outward normals give a positive volume.
func TestVolumeOfUnitCubeIsPositiveOne(t *testing.T) {
	if got := unitCube().Volume(); math.Abs(got-1.0) > 1e-12 {
		t.Fatalf("Volume = %v, want 1.0", got)
	}
}

func TestEpsilonScalesWithModelSize(t *testing.T) {
	small := unitCube()
	big := &Mesh{Tris: make([]Tri, len(small.Tris))}
	for i, tr := range small.Tris {
		big.Tris[i] = Tri{tr.A.Scale(1000), tr.B.Scale(1000), tr.C.Scale(1000)}
	}
	if big.Epsilon() <= small.Epsilon() {
		t.Fatalf("epsilon did not scale: small=%v big=%v", small.Epsilon(), big.Epsilon())
	}
}

func TestEpsilonHasAFloorForDegenerateMeshes(t *testing.T) {
	empty := &Mesh{}
	if got := empty.Epsilon(); got < 1e-9 {
		t.Fatalf("Epsilon = %v, want at least the 1e-9 floor", got)
	}
}
