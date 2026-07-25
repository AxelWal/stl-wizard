// Package cut splits a mesh with a bounded cutting plane.
//
// The cutter is the convex intersection of five half-spaces: the cut plane
// itself, plus the four sides of a bounded rectangle. Bounding the plane is
// what lets a single cut take the top off one arm of a U-shaped model without
// touching the other arm, which an unbounded plane at the same height would
// also slice through.
package cut

import (
	"errors"
	"fmt"
	"math"

	"stl-cutter/internal/geom"
)

// Plane is a half-space. Points with N·p - D >= 0 are inside it.
type Plane struct {
	N geom.Vec3
	D float64
}

func (p Plane) Dist(v geom.Vec3) float64 { return p.N.Dot(v) - p.D }

// Side is the position of a point relative to a plane, with a tolerance band.
type Side int

const (
	Outside Side = -1
	On      Side = 0
	Inside  Side = 1
)

// Classify treats points within eps of the plane as exactly on it. That band is
// what stops clipping from emitting sliver triangles.
func (p Plane) Classify(v geom.Vec3, eps float64) Side {
	d := p.Dist(v)
	switch {
	case d > eps:
		return Inside
	case d < -eps:
		return Outside
	default:
		return On
	}
}

// Spec is a bounded cutting plane as the UI describes it: a plane through
// Origin with the given Normal, restricted to a Width x Height rectangle
// centred on Origin and spanned by U and V.
//
// The half-space the Normal points into is the region that becomes part 2.
type Spec struct {
	Origin geom.Vec3
	Normal geom.Vec3
	U, V   geom.Vec3
	Width  float64
	Height float64
}

// SpecFromNormal builds a Spec with an arbitrary but right-handed in-plane
// basis. Used by tests and by auto-split, where the in-plane orientation is
// irrelevant because the rectangle spans the whole cross-section anyway.
func SpecFromNormal(origin, normal geom.Vec3, width, height float64) Spec {
	n := normal.Unit()
	// Pick the least-aligned cardinal axis so the cross product stays well
	// conditioned.
	seed := geom.Vec3{1, 0, 0}
	if math.Abs(n[0]) > math.Abs(n[1]) {
		seed = geom.Vec3{0, 1, 0}
	}
	u := seed.Sub(n.Scale(seed.Dot(n))).Unit()
	v := n.Cross(u)
	return Spec{Origin: origin, Normal: n, U: u, V: v, Width: width, Height: height}
}

// Basis returns an orthonormal right-handed in-plane basis with u x v == Normal.
// Cap triangulation depends on that identity: a counter-clockwise 2D triangle in
// (u, v) then maps to a triangle whose normal is +Normal.
func (s Spec) Basis() (u, v geom.Vec3) {
	n := s.Normal.Unit()
	u = s.U.Sub(n.Scale(s.U.Dot(n))).Unit()
	return u, n.Cross(u)
}

func (s Spec) Validate() error {
	if s.Normal.Len() == 0 {
		return errors.New("cutting plane has a zero normal")
	}
	if s.Width <= 0 || s.Height <= 0 {
		return fmt.Errorf("cutting rectangle must have positive extent, got %gx%g", s.Width, s.Height)
	}
	if s.U.Len() == 0 || s.V.Len() == 0 {
		return errors.New("cutting plane has a degenerate in-plane basis")
	}
	// U must not be parallel to Normal. Basis re-orthogonalises U against Normal,
	// so a parallel U collapses to the zero vector, and a zero basis makes all four
	// rectangle-side planes no-ops — the cutter silently loses its bound and cuts
	// the whole model instead of the region the user marked out.
	if math.Abs(s.U.Unit().Dot(s.Normal.Unit())) > 1-1e-6 {
		return errors.New("cutting plane basis vector U is parallel to the plane normal, which would leave the cut unbounded")
	}
	// U and V must span the plane. Requiring them merely non-parallel is enough;
	// Basis re-orthogonalises.
	if s.U.Unit().Cross(s.V.Unit()).Len() < 1e-6 {
		return errors.New("cutting plane basis vectors are parallel")
	}
	return nil
}

// Planes returns the five inward-facing half-spaces whose intersection is the
// cutter. Order is the cut plane first, then the four rectangle sides; nothing
// depends on that order, because cap recovery happens after all splitting.
func (s Spec) Planes() []Plane {
	n := s.Normal.Unit()
	u, v := s.Basis()
	cu, cv := u.Dot(s.Origin), v.Dot(s.Origin)
	hw, hh := s.Width/2, s.Height/2

	return []Plane{
		{N: n, D: n.Dot(s.Origin)},           // keep the side the normal points into
		{N: u, D: cu - hw},                   // u >= centre - half width
		{N: u.Scale(-1), D: -(cu + hw)},      // u <= centre + half width
		{N: v, D: cv - hh},                   // v >= centre - half height
		{N: v.Scale(-1), D: -(cv + hh)},      // v <= centre + half height
	}
}
