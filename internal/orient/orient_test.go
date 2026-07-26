package orient

import (
	"math"
	"testing"

	"stl-cutter/internal/fixtures"
	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

// rotate applies a Result's rotation the way the caller will.
func rotate(r Result, v geom.Vec3) geom.Vec3 {
	return geom.Vec3{
		r.Rotation[0]*v[0] + r.Rotation[1]*v[1] + r.Rotation[2]*v[2],
		r.Rotation[3]*v[0] + r.Rotation[4]*v[1] + r.Rotation[5]*v[2],
		r.Rotation[6]*v[0] + r.Rotation[7]*v[1] + r.Rotation[8]*v[2],
	}
}

// The rotation has to actually put the chosen direction at the bottom. Everything
// downstream — the height, the plate placement, the transform — assumes it.
func TestRotationSendsTheChosenDirectionStraightDown(t *testing.T) {
	for name, m := range map[string]*stl.Mesh{
		"box":    fixtures.Box(geom.Vec3{}, geom.Vec3{10, 10, 40}),
		"cube":   fixtures.Cube(10),
		"u":      fixtures.UShape(10),
		"sphere": fixtures.UVSphere(10, 24, 12),
		"tube":   fixtures.Tube(5, 3, 20, 24),
	} {
		res := Best(m, Spec{})
		got := rotate(res, res.Down)
		if math.Abs(got[0]) > 1e-9 || math.Abs(got[1]) > 1e-9 || math.Abs(got[2]+1) > 1e-9 {
			t.Errorf("%s: rotating Down %v gives %v, want (0,0,-1)", name, res.Down, got)
		}
	}
}

// A flat-bottomed box has no overhangs in any axis-aligned orientation. If the base
// were counted as overhang this would be non-zero — and the search would then avoid
// resting parts flat, which is the exact opposite of what it is for.
func TestABoxHasNoOverhangAtAll(t *testing.T) {
	res := Best(fixtures.Box(geom.Vec3{}, geom.Vec3{10, 20, 30}), Spec{})
	if res.OverhangArea > 1e-9 {
		t.Errorf("OverhangArea = %v, want 0 — a box resting on a face overhangs nothing", res.OverhangArea)
	}
	if res.BaseArea <= 0 {
		t.Errorf("BaseArea = %v, want a whole face", res.BaseArea)
	}
}

// The tie-break: every orientation of a tall box is overhang-free, so the biggest
// flat base wins and the box is laid down rather than stood up.
func TestATallBoxIsLaidDownNotStoodUp(t *testing.T) {
	res := Best(fixtures.Box(geom.Vec3{}, geom.Vec3{10, 10, 40}), Spec{})

	if res.OverhangArea > 1e-9 {
		t.Fatalf("OverhangArea = %v, want 0", res.OverhangArea)
	}
	// Resting on a 10x40 side gives a base of 400 and a height of 10. Standing on
	// the 10x10 end gives a base of 100 and a height of 40.
	if math.Abs(res.BaseArea-400) > 1e-6 {
		t.Errorf("BaseArea = %v, want 400 — the largest face", res.BaseArea)
	}
	if math.Abs(res.Height-10) > 1e-6 {
		t.Errorf("Height = %v, want 10 — lying down", res.Height)
	}
}

// The point of the whole thing: an orientation with a big overhang must lose to one
// without. A wedge stood on its point overhangs; laid on its flat face it does not.
func TestAWedgeRestsOnItsFlatFaceRatherThanItsPoint(t *testing.T) {
	// A triangular prism: flat 40x20 base, sloping up to an apex.
	profile := [][2]float64{{0, 0}, {40, 0}, {20, 30}}
	m := prism(profile, [][3]int{{0, 1, 2}}, 0, 20)

	res := Best(m, Spec{})

	down := rotate(res, geom.Vec3{0, 0, 0}) // unused, keeps the helper honest
	_ = down
	if res.OverhangArea > 1e-6 {
		t.Errorf("OverhangArea = %v, want 0 — the flat base can face down", res.OverhangArea)
	}
	// The flat face is 40 wide by 20 deep = 800, the largest in the shape.
	if math.Abs(res.BaseArea-800) > 1e-6 {
		t.Errorf("BaseArea = %v, want 800 (the 40x20 face)", res.BaseArea)
	}
}

// A steeper threshold must find more overhang, or the setting does nothing.
func TestASteeperThresholdFindsMoreOverhang(t *testing.T) {
	m := fixtures.UVSphere(10, 32, 16)

	loose := Best(m, Spec{OverhangDegrees: 20})
	strict := Best(m, Spec{OverhangDegrees: 80})

	if !(strict.OverhangArea > loose.OverhangArea) {
		t.Errorf("80° found %v of overhang and 20° found %v; the threshold is being ignored",
			strict.OverhangArea, loose.OverhangArea)
	}
}

func TestEveryCandidateIsConsidered(t *testing.T) {
	res := Best(fixtures.Cube(10), Spec{})
	if res.Considered < 6 {
		t.Errorf("Considered = %d, want at least the 6 axes", res.Considered)
	}
	if l := res.Down.Len(); math.Abs(l-1) > 1e-9 {
		t.Errorf("Down has length %v, want a unit vector", l)
	}
}

func TestBestHandlesAnEmptyMesh(t *testing.T) {
	res := Best(&stl.Mesh{}, Spec{})
	// Identity, so a caller that applies it regardless cannot corrupt anything.
	want := [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
	if res.Rotation != want {
		t.Errorf("Rotation = %v, want identity for an empty mesh", res.Rotation)
	}
}

// Sampling large meshes must not change which orientation wins. The score is an
// area integral, so a sample estimates it — but the winner has to be the same.
func TestSamplingDoesNotChangeTheWinner(t *testing.T) {
	m := fixtures.Box(geom.Vec3{}, geom.Vec3{10, 10, 40})
	exact := Best(m, Spec{SampleAbove: 1 << 30})
	sampled := Best(m, Spec{SampleAbove: 4})

	if math.Abs(sampled.Height-exact.Height) > 1e-6 {
		t.Errorf("sampled chose height %v, exact chose %v", sampled.Height, exact.Height)
	}
}

// prism builds a closed solid from a counter-clockwise profile, mirroring what
// internal/fixtures does. Duplicated rather than exported: this is the only test
// here that needs a shape fixtures does not already provide.
func prism(profile [][2]float64, capTris [][3]int, z0, z1 float64) *stl.Mesh {
	m := &stl.Mesh{}
	at := func(i int, z float64) geom.Vec3 {
		return geom.Vec3{profile[i][0], profile[i][1], z}
	}
	for _, t := range capTris {
		m.Tris = append(m.Tris,
			stl.Tri{A: at(t[0], z1), B: at(t[1], z1), C: at(t[2], z1)},
			stl.Tri{A: at(t[2], z0), B: at(t[1], z0), C: at(t[0], z0)})
	}
	n := len(profile)
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		m.Tris = append(m.Tris,
			stl.Tri{A: at(i, z0), B: at(j, z0), C: at(j, z1)},
			stl.Tri{A: at(i, z0), B: at(j, z1), C: at(i, z1)})
	}
	return m
}
