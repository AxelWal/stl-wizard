// Package fixtures generates the test meshes used across the geometry tests.
// Meshes are built procedurally rather than committed as binary STL files so
// that they stay readable and reviewable in diffs.
package fixtures

import (
	"math"

	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

// prism extrudes a counter-clockwise 2D profile along +Z from z0 to z1,
// producing a closed solid with outward normals. capTris must triangulate the
// profile counter-clockwise, indexing into profile.
func prism(profile [][2]float64, capTris [][3]int, z0, z1 float64) *stl.Mesh {
	at := func(i int, z float64) geom.Vec3 {
		return geom.Vec3{profile[i][0], profile[i][1], z}
	}
	m := &stl.Mesh{}

	for _, tr := range capTris {
		// Top cap faces +Z with the profile's own winding; the bottom cap is
		// the same triangle reversed so it faces -Z.
		top := stl.Tri{A: at(tr[0], z1), B: at(tr[1], z1), C: at(tr[2], z1)}
		m.Tris = append(m.Tris, top, stl.Tri{A: at(tr[0], z0), B: at(tr[2], z0), C: at(tr[1], z0)})
	}

	// Side walls. For a counter-clockwise profile the outward normal of edge
	// i->j is (dy, -dx, 0), which this winding produces.
	for i := range profile {
		j := (i + 1) % len(profile)
		a0, b0 := at(i, z0), at(j, z0)
		a1, b1 := at(i, z1), at(j, z1)
		m.Tris = append(m.Tris,
			stl.Tri{A: a0, B: b0, C: b1},
			stl.Tri{A: a0, B: b1, C: a1},
		)
	}
	return m
}

// reversed flips every triangle, turning a solid into a cavity. HollowBox uses
// it for the inner void.
func reversed(m *stl.Mesh) *stl.Mesh {
	out := &stl.Mesh{Tris: make([]stl.Tri, len(m.Tris))}
	for i, t := range m.Tris {
		out.Tris[i] = t.Reversed()
	}
	return out
}

func Box(min, max geom.Vec3) *stl.Mesh {
	profile := [][2]float64{
		{min[0], min[1]}, {max[0], min[1]}, {max[0], max[1]}, {min[0], max[1]},
	}
	return prism(profile, [][3]int{{0, 1, 2}, {0, 2, 3}}, min[2], max[2])
}

func Cube(size float64) *stl.Mesh {
	return Box(geom.Vec3{}, geom.Vec3{size, size, size})
}

// UVSphere is a latitude/longitude tessellated sphere centred on the origin.
// It is the curved fixture: a cut through it produces many straddling
// triangles and a curved cap boundary.
func UVSphere(radius float64, segments, rings int) *stl.Mesh {
	at := func(seg, ring int) geom.Vec3 {
		phi := math.Pi * float64(ring) / float64(rings)         // 0 at +Z pole
		theta := 2 * math.Pi * float64(seg) / float64(segments) // around Z
		s := math.Sin(phi)
		return geom.Vec3{
			radius * s * math.Cos(theta),
			radius * s * math.Sin(theta),
			radius * math.Cos(phi),
		}
	}
	m := &stl.Mesh{}
	for ring := 0; ring < rings; ring++ {
		for seg := 0; seg < segments; seg++ {
			a := at(seg, ring)
			b := at(seg+1, ring)
			c := at(seg+1, ring+1)
			d := at(seg, ring+1)
			switch {
			case ring == 0: // +Z pole: a and b coincide, emit one triangle
				m.Tris = append(m.Tris, stl.Tri{A: d, B: c, C: a})
			case ring == rings-1: // -Z pole: c and d coincide
				m.Tris = append(m.Tris, stl.Tri{A: c, B: b, C: a})
			default:
				m.Tris = append(m.Tris,
					stl.Tri{A: c, B: b, C: a},
					stl.Tri{A: d, B: c, C: a},
				)
			}
		}
	}
	return m
}

// Tube is a hollow cylinder along Z, sitting on z=0. Cutting it produces an
// annular cap, which is what exercises hole nesting in the triangulator.
func Tube(outerR, innerR, height float64, segments int) *stl.Mesh {
	ring := func(r float64, i int, z float64) geom.Vec3 {
		th := 2 * math.Pi * float64(i) / float64(segments)
		return geom.Vec3{r * math.Cos(th), r * math.Sin(th), z}
	}
	m := &stl.Mesh{}
	for i := 0; i < segments; i++ {
		j := i + 1
		o0, o1 := ring(outerR, i, 0), ring(outerR, j, 0)
		o2, o3 := ring(outerR, j, height), ring(outerR, i, height)
		i0, i1 := ring(innerR, i, 0), ring(innerR, j, 0)
		i2, i3 := ring(innerR, j, height), ring(innerR, i, height)

		// Outer wall faces away from the axis.
		m.Tris = append(m.Tris, stl.Tri{A: o0, B: o1, C: o2}, stl.Tri{A: o0, B: o2, C: o3})
		// Inner wall faces toward the axis, so its winding is the reverse.
		m.Tris = append(m.Tris, stl.Tri{A: i0, B: i2, C: i1}, stl.Tri{A: i0, B: i3, C: i2})
		// Bottom annulus faces -Z, top annulus faces +Z.
		m.Tris = append(m.Tris, stl.Tri{A: i0, B: i1, C: o0}, stl.Tri{A: i1, B: o1, C: o0})
		m.Tris = append(m.Tris, stl.Tri{A: i2, B: i3, C: o3}, stl.Tri{A: o2, B: i2, C: o3})
	}
	return m
}

// HollowBox is a closed shell: an outer box with an inner void. It is the
// fixture for the pin wall-thickness check, where a socket must be refused for
// want of material.
func HollowBox(outer geom.Vec3, wall float64) *stl.Mesh {
	out := Box(geom.Vec3{}, outer)
	in := reversed(Box(
		geom.Vec3{wall, wall, wall},
		geom.Vec3{outer[0] - wall, outer[1] - wall, outer[2] - wall},
	))
	out.Tris = append(out.Tris, in.Tris...)
	return out
}

// UShapeProfileArea is the area of the U cross-section: a 30x40 rectangle less
// the 10x30 notch between the arms. Typed float64 so tests can multiply it by a
// depth without converting.
const UShapeProfileArea float64 = 30*40 - 10*30

// UShape is the letter U extruded along Z, and the motivating fixture for the
// whole project. A plane at y=25 bounded to x in [0,15] must cut the left arm
// and leave the right arm untouched and still attached to the base.
//
//	y=40  ┌──┐      ┌──┐
//	      │  │      │  │
//	y=10  │  └──────┘  │
//	 y=0  └────────────┘
//	     x=0  10    20  30
func UShape(depth float64) *stl.Mesh {
	profile := [][2]float64{
		{0, 0}, {30, 0}, {30, 40}, {20, 40}, {20, 10}, {10, 10}, {10, 40}, {0, 40},
	}
	// Hand-decomposed as two fans, because the U is star-shaped from neither
	// vertex alone: a fan from vertex 0 over the chain 1-4-5-6-7, and a fan from
	// vertex 1 over the chain 2-3-4. The two fans meet along edge 1-4 and their
	// areas sum to UShapeProfileArea, which the fixture test verifies.
	capTris := [][3]int{
		{0, 1, 4}, {0, 4, 5}, {0, 5, 6}, {0, 6, 7}, // fan from vertex 0
		{1, 2, 3}, {1, 3, 4}, // fan from vertex 1
	}
	return prism(profile, capTris, 0, depth)
}

// NonManifold is a cube with one triangle removed, so one cap loop cannot
// close. It drives the open-loop error path.
func NonManifold() *stl.Mesh {
	m := Cube(10)
	m.Tris = m.Tris[:len(m.Tris)-1]
	return m
}
