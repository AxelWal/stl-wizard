package cut

import (
	"sort"

	"stl-cutter/internal/geom"
)

// pt2 is a point in a cut plane's 2D basis.
type pt2 struct{ X, Y float64 }

// faceVert carries a cap vertex in both representations. The 3D point is kept
// rather than reconstructed from the 2D one after triangulation: reconstruction
// rounds, and the rounding shows up as hairline cracks between the cap and the
// clipped triangles it closes.
type faceVert struct {
	P2 pt2
	P3 geom.Vec3
}

type faceLoop []faceVert

// projectLoop flattens a 3D cap loop into the (u, v) basis of its plane.
func projectLoop(loop Polygon, origin, u, v geom.Vec3) faceLoop {
	out := make(faceLoop, len(loop))
	for i, p := range loop {
		d := p.Sub(origin)
		out[i] = faceVert{P2: pt2{X: d.Dot(u), Y: d.Dot(v)}, P3: p}
	}
	return out
}

// signedArea2 returns twice the signed area. Positive means counter-clockwise.
func signedArea2(l faceLoop) float64 {
	var s float64
	for i := range l {
		j := (i + 1) % len(l)
		s += l[i].P2.X*l[j].P2.Y - l[j].P2.X*l[i].P2.Y
	}
	return s
}

func reverseLoop(l faceLoop) faceLoop {
	out := make(faceLoop, len(l))
	for i := range l {
		out[i] = l[len(l)-1-i]
	}
	return out
}

// pointInLoop is a standard crossing-number test.
func pointInLoop(p pt2, l faceLoop) bool {
	in := false
	for i, j := 0, len(l)-1; i < len(l); j, i = i, i+1 {
		pi, pj := l[i].P2, l[j].P2
		if (pi.Y > p.Y) != (pj.Y > p.Y) {
			x := pi.X + (p.Y-pi.Y)/(pj.Y-pi.Y)*(pj.X-pi.X)
			if p.X < x {
				in = !in
			}
		}
	}
	return in
}

// faceGroup is one solid region of a cap: an outer boundary, normalised
// counter-clockwise, and the holes inside it, normalised clockwise.
type faceGroup struct {
	Outer faceLoop
	Holes []faceLoop
}

// groupLoops sorts cap loops into solid regions by nesting depth. A loop nested
// an even number of times bounds material; an odd number bounds a void. So an
// island inside a hole is a solid region in its own right, not a hole of the
// outermost loop.
//
// ponytail: nesting is tested with one vertex per loop, which assumes loops do
// not touch. Cross-sections of a solid satisfy that. Upgrade path if
// self-touching cross-sections turn up: test an interior point derived from the
// loop instead of one of its vertices.
func groupLoops(loops []faceLoop) []faceGroup {
	n := len(loops)
	if n == 0 {
		return nil
	}

	depth := make([]int, n)
	for i := range loops {
		if len(loops[i]) == 0 {
			continue
		}
		probe := loops[i][0].P2
		for j := range loops {
			if i != j && pointInLoop(probe, loops[j]) {
				depth[i]++
			}
		}
	}

	// Largest first, so that when a hole is assigned to its innermost containing
	// outer that outer already exists.
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return depth[idx[a]] < depth[idx[b]] })

	groups := make([]faceGroup, 0, n)
	outerAt := make(map[int]int) // loop index -> index into groups

	for _, i := range idx {
		l := loops[i]
		if len(l) < 3 {
			continue
		}
		if depth[i]%2 == 0 {
			if signedArea2(l) < 0 {
				l = reverseLoop(l)
			}
			outerAt[i] = len(groups)
			groups = append(groups, faceGroup{Outer: l})
			continue
		}

		// A hole belongs to the containing loop one level shallower.
		if signedArea2(l) > 0 {
			l = reverseLoop(l)
		}
		probe := loops[i][0].P2
		host := -1
		for j := range loops {
			if j == i || depth[j] != depth[i]-1 {
				continue
			}
			if pointInLoop(probe, loops[j]) {
				host = j
				break
			}
		}
		if g, ok := outerAt[host]; ok {
			groups[g].Holes = append(groups[g].Holes, l)
		}
		// A hole with no host cannot happen for a closed cross-section; if it
		// somehow does, dropping it loses a void rather than corrupting a solid.
	}
	return groups
}
