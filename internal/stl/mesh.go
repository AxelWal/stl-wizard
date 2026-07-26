// Package stl reads and writes STL files and provides the triangle-soup mesh
// type the cutting package operates on.
package stl

import (
	"math"

	"stl-wizard/internal/geom"
)

// Tri is a single triangle. Winding is counter-clockwise seen from outside the
// solid, so Normal points outward.
type Tri struct {
	A, B, C geom.Vec3
}

func (t Tri) Normal() geom.Vec3 {
	return t.B.Sub(t.A).Cross(t.C.Sub(t.A)).Unit()
}

func (t Tri) Area() float64 {
	return t.B.Sub(t.A).Cross(t.C.Sub(t.A)).Len() / 2
}

// Reversed flips the winding, and therefore the normal. Caps use this: the same
// cap geometry belongs to both output parts, facing opposite ways.
func (t Tri) Reversed() Tri { return Tri{t.A, t.C, t.B} }

// Mesh is a triangle soup, exactly as STL stores it.
//
// ponytail: soup costs roughly 3x indexed storage, so a 2M-triangle model runs
// about 150MB per part-tree node. Upgrade path if memory becomes the binding
// constraint: an indexed mesh with a weld pass on load, changing this type and
// its iteration sites only.
type Mesh struct {
	Tris []Tri
}

type BBox struct {
	Min, Max geom.Vec3
}

func (b BBox) Size() geom.Vec3 { return b.Max.Sub(b.Min) }

func (b BBox) Diagonal() float64 { return b.Size().Len() }

func (m *Mesh) BBox() BBox {
	if len(m.Tris) == 0 {
		return BBox{}
	}
	lo := geom.Vec3{math.Inf(1), math.Inf(1), math.Inf(1)}
	hi := geom.Vec3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, t := range m.Tris {
		for _, v := range [3]geom.Vec3{t.A, t.B, t.C} {
			for i := 0; i < 3; i++ {
				lo[i] = math.Min(lo[i], v[i])
				hi[i] = math.Max(hi[i], v[i])
			}
		}
	}
	return BBox{Min: lo, Max: hi}
}

// Volume is the signed tetrahedron sum, which works directly on a soup and is
// positive for outward-wound triangles. Cut tests use it as their primary
// invariant: it catches inverted winding, missing caps and double-counted
// triangles all at once.
func (m *Mesh) Volume() float64 {
	var v float64
	for _, t := range m.Tris {
		v += t.A.Dot(t.B.Cross(t.C))
	}
	return v / 6
}

// Scaled returns a copy of m with every vertex multiplied componentwise by f.
//
// A copy, not in place: the session keeps the mesh exactly as it was loaded and scales
// from that every time, so applying the same factors twice is idempotent and returning
// to 1 is exact rather than a division that never quite arrives.
//
// Positive factors preserve winding, so a closed solid stays closed and its volume comes
// out multiplied by f[0]*f[1]*f[2]. Negative factors would mirror it — consistently wound
// and enclosing a negative volume, which reads as sound and prints as nothing — so
// callers reject them rather than this silently obliging.
func (m *Mesh) Scaled(f [3]float64) *Mesh {
	out := &Mesh{Tris: make([]Tri, len(m.Tris))}
	scale := func(v geom.Vec3) geom.Vec3 {
		return geom.Vec3{v[0] * f[0], v[1] * f[1], v[2] * f[2]}
	}
	for i, t := range m.Tris {
		out.Tris[i] = Tri{A: scale(t.A), B: scale(t.B), C: scale(t.C)}
	}
	return out
}

// Epsilon is the single tolerance every geometric predicate uses, scaled to the
// model so that a 1mm part and a 1m part behave the same way.
func (m *Mesh) Epsilon() float64 {
	return math.Max(1e-9, 1e-7*m.BBox().Diagonal())
}
