package cut

import (
	"math"
	"testing"

	"stl-cutter/internal/geom"
)

func TestPlaneClassifyRespectsEpsilon(t *testing.T) {
	p := Plane{N: geom.Vec3{0, 0, 1}, D: 0}
	const eps = 1e-6

	if got := p.Classify(geom.Vec3{0, 0, 1}, eps); got != Inside {
		t.Errorf("above plane: got %v, want Inside", got)
	}
	if got := p.Classify(geom.Vec3{0, 0, -1}, eps); got != Outside {
		t.Errorf("below plane: got %v, want Outside", got)
	}
	if got := p.Classify(geom.Vec3{0, 0, eps / 2}, eps); got != On {
		t.Errorf("within eps: got %v, want On", got)
	}
}

// The five planes must all point inward, so that "inside every one of them"
// means "inside the cutter".
func TestPlanesAllPointInwardAtTheOrigin(t *testing.T) {
	s := SpecFromNormal(geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, 1}, 4, 4)
	// A point just above the cut plane and centred in the rectangle is inside
	// all five half-spaces.
	probe := geom.Vec3{5, 5, 5.5}
	for i, p := range s.Planes() {
		if p.Dist(probe) < 0 {
			t.Errorf("plane %d: probe is outside; planes must point inward", i)
		}
	}
}

func TestPlanesBoundTheRectangle(t *testing.T) {
	s := SpecFromNormal(geom.Vec3{0, 0, 0}, geom.Vec3{0, 0, 1}, 10, 10)
	planes := s.Planes()

	inside := func(v geom.Vec3) bool {
		for _, p := range planes {
			if p.Dist(v) < 0 {
				return false
			}
		}
		return true
	}

	if !inside(geom.Vec3{4, 4, 1}) {
		t.Error("point within the rectangle and above the plane should be inside")
	}
	if inside(geom.Vec3{6, 0, 1}) {
		t.Error("point beyond the rectangle's half-width should be outside")
	}
	if inside(geom.Vec3{0, 6, 1}) {
		t.Error("point beyond the rectangle's half-height should be outside")
	}
	if inside(geom.Vec3{0, 0, -1}) {
		t.Error("point below the cut plane should be outside")
	}
}

func TestValidateRejectsBadSpecs(t *testing.T) {
	good := SpecFromNormal(geom.Vec3{}, geom.Vec3{0, 0, 1}, 10, 10)
	if err := good.Validate(); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}

	cases := map[string]Spec{
		"zero normal":    {Normal: geom.Vec3{}, U: geom.Vec3{1, 0, 0}, V: geom.Vec3{0, 1, 0}, Width: 1, Height: 1},
		"zero width":     SpecFromNormal(geom.Vec3{}, geom.Vec3{0, 0, 1}, 0, 10),
		"negative height": SpecFromNormal(geom.Vec3{}, geom.Vec3{0, 0, 1}, 10, -1),
		"non-orthogonal basis": {
			Normal: geom.Vec3{0, 0, 1}, U: geom.Vec3{1, 0, 0}, V: geom.Vec3{1, 0, 0},
			Width: 1, Height: 1,
		},
	}
	for name, s := range cases {
		if err := s.Validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// Basis must be right-handed with u x v == Normal, because cap triangulation
// relies on a counter-clockwise 2D winding mapping to a +Normal facing triangle.
func TestBasisIsRightHanded(t *testing.T) {
	for _, n := range []geom.Vec3{
		{0, 0, 1}, {1, 0, 0}, {0, 1, 0}, {1, 1, 1},
	} {
		s := SpecFromNormal(geom.Vec3{}, n, 1, 1)
		u, v := s.Basis()
		want := n.Unit()
		if got := u.Cross(v); got.Sub(want).Len() > 1e-12 {
			t.Errorf("normal %v: u cross v = %v, want %v", n, got, want)
		}
		if math.Abs(u.Len()-1) > 1e-12 || math.Abs(v.Len()-1) > 1e-12 {
			t.Errorf("normal %v: basis vectors must be unit length", n)
		}
	}
}
