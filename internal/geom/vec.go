// Package geom provides the vector primitives and point-welding used by the
// mesh and cutting packages.
package geom

import "math"

// Vec3 is a point or direction in 3D. Geometry uses float64 throughout;
// float32 loses too much precision in the repeated plane-distance arithmetic
// that clipping performs.
type Vec3 [3]float64

func (a Vec3) Add(b Vec3) Vec3 { return Vec3{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }
func (a Vec3) Sub(b Vec3) Vec3 { return Vec3{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }

func (a Vec3) Scale(s float64) Vec3 { return Vec3{a[0] * s, a[1] * s, a[2] * s} }

func (a Vec3) Dot(b Vec3) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }

func (a Vec3) Cross(b Vec3) Vec3 {
	return Vec3{
		a[1]*b[2] - a[2]*b[1],
		a[2]*b[0] - a[0]*b[2],
		a[0]*b[1] - a[1]*b[0],
	}
}

func (a Vec3) Len() float64 { return math.Sqrt(a.Dot(a)) }

// Unit returns a normalised copy. A zero vector is returned unchanged rather
// than becoming NaN, so a degenerate triangle cannot poison downstream maths.
func (a Vec3) Unit() Vec3 {
	l := a.Len()
	if l == 0 {
		return a
	}
	return a.Scale(1 / l)
}

// Less orders points lexicographically. Its only job is to give an edge a
// canonical direction: both triangles sharing an edge order its endpoints the
// same way, so both compute a bit-identical plane intersection point.
func (a Vec3) Less(b Vec3) bool {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
