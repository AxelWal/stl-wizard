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
		"zero normal":     {Normal: geom.Vec3{}, U: geom.Vec3{1, 0, 0}, V: geom.Vec3{0, 1, 0}, Width: 1, Height: 1},
		"zero width":      SpecFromNormal(geom.Vec3{}, geom.Vec3{0, 0, 1}, 0, 10),
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

// A non-finite value defeats every other check in Validate, because NaN fails
// all comparisons: NaN <= 0 is false, so a NaN extent used to pass, make every
// Classify return On, and leave the cut silently unbounded but reported as good.
// The guard lives in Validate rather than in a caller so that every caller
// inherits it.
func TestValidateRejectsNonFiniteValues(t *testing.T) {
	nanNormal := SpecFromNormal(geom.Vec3{}, geom.Vec3{0, 0, 1}, 10, 10)
	nanNormal.Normal[1] = math.NaN()

	cases := map[string]Spec{
		"NaN width":  SpecFromNormal(geom.Vec3{}, geom.Vec3{0, 0, 1}, math.NaN(), 10),
		"Inf height": SpecFromNormal(geom.Vec3{}, geom.Vec3{0, 0, 1}, 10, math.Inf(1)),
		"NaN normal": nanNormal,
		"Inf origin": SpecFromNormal(geom.Vec3{math.Inf(-1), 0, 0}, geom.Vec3{0, 0, 1}, 10, 10),
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

// A U parallel to Normal used to pass validation and then collapse Basis to zero
// vectors, which turned all four rectangle-side planes into no-ops and silently
// unbounded the cutter.
func TestValidateRejectsUParallelToNormal(t *testing.T) {
	s := Spec{
		Normal: geom.Vec3{0, 0, 1},
		U:      geom.Vec3{0, 0, 1},
		V:      geom.Vec3{1, 0, 0},
		Width:  1,
		Height: 1,
	}
	if err := s.Validate(); err == nil {
		t.Fatal("expected an error for a U parallel to the normal")
	}
}

// Basis re-orthogonalises U against Normal. Only SpecFromNormal was exercising
// Basis, and it always supplies an already-perpendicular U, so this branch was
// never actually tested.
func TestBasisReorthogonalisesASkewedU(t *testing.T) {
	s := Spec{
		Normal: geom.Vec3{0, 0, 1},
		U:      geom.Vec3{1, 0, 0.5}, // not perpendicular to Normal
		V:      geom.Vec3{0, 1, 0},
		Width:  1,
		Height: 1,
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("skewed but usable U was rejected: %v", err)
	}

	u, v := s.Basis()
	if math.Abs(u.Dot(s.Normal)) > 1e-12 {
		t.Errorf("u is not perpendicular to the normal: u.n = %v", u.Dot(s.Normal))
	}
	// Assert the actual value, not just that u is some valid perpendicular.
	// Projecting U = {1, 0, 0.5} onto the plane normal to {0, 0, 1} and normalising
	// gives exactly {1, 0, 0}; a Basis that ignored U could still satisfy every
	// generic invariant below.
	if want := (geom.Vec3{1, 0, 0}); u.Sub(want).Len() > 1e-12 {
		t.Errorf("u = %v, want %v — Basis is not deriving u from U", u, want)
	}
	if math.Abs(u.Len()-1) > 1e-12 || math.Abs(v.Len()-1) > 1e-12 {
		t.Errorf("basis vectors are not unit length: |u|=%v |v|=%v", u.Len(), v.Len())
	}
	if got := u.Cross(v); got.Sub(s.Normal.Unit()).Len() > 1e-12 {
		t.Errorf("u cross v = %v, want %v", got, s.Normal.Unit())
	}
}

// Later tasks index Planes() by position: element 0 is the plane the user placed,
// and pin placement operates on that plane's caps alone.
func TestPlanesElementZeroIsTheCutPlane(t *testing.T) {
	s := SpecFromNormal(geom.Vec3{1, 2, 3}, geom.Vec3{0, 1, 0}, 10, 10)
	p := s.Planes()[0]
	if p.N.Sub(s.Normal.Unit()).Len() > 1e-12 {
		t.Errorf("Planes()[0].N = %v, want the spec normal %v", p.N, s.Normal.Unit())
	}
	if math.Abs(p.Dist(s.Origin)) > 1e-12 {
		t.Errorf("the spec origin should lie on Planes()[0], got distance %v", p.Dist(s.Origin))
	}
}
