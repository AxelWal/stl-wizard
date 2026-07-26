// Package orient chooses which way up to print a part so that as little of it as
// possible needs support.
package orient

import (
	"math"
	"sort"

	"stl-wizard/internal/geom"
	"stl-wizard/internal/stl"
)

// Spec configures the search. The zero value is the intended default.
type Spec struct {
	// OverhangDegrees is how far a face may lean from vertical before it needs
	// support. 0 means 45, which is what slicers assume.
	OverhangDegrees float64
	// MaxCandidates caps how many of the model's own face directions are tried,
	// on top of the six axes. 0 means 64.
	MaxCandidates int
	// SampleAbove is the triangle count past which scoring uses an area-weighted
	// sample rather than every triangle. 0 means 100000.
	SampleAbove int
}

func (s Spec) withDefaults() Spec {
	if s.OverhangDegrees <= 0 {
		s.OverhangDegrees = 45
	}
	if s.MaxCandidates <= 0 {
		s.MaxCandidates = 64
	}
	if s.SampleAbove <= 0 {
		s.SampleAbove = 100000
	}
	return s
}

// Result is the chosen orientation.
type Result struct {
	// Down is the direction, in the model's own coordinates, that ends up pointing
	// at the build plate.
	Down geom.Vec3
	// Rotation is row-major and maps model coordinates to printed coordinates. It
	// sends Down to (0,0,-1).
	Rotation [9]float64

	// OverhangArea is how much surface needs support, excluding whatever rests on
	// the plate. BaseArea is what rests on it.
	OverhangArea float64
	BaseArea     float64
	Height       float64
	Considered   int
}

type face struct {
	n    geom.Vec3 // unit outward normal
	area float64
}

// Best searches for the orientation with the least overhang, breaking ties on the
// largest flat base.
//
// A triangle with unit outward normal n is an overhang for a down direction d when
// n·d exceeds cos(threshold): rotating d to point down maps a normal's height
// component to -(n·d), so a large positive n·d is a steeply downward face.
//
// Triangles resting on the plate are excluded, and that exclusion is the whole
// feature. Height is proportional to -(v·d), so the lowest plane sits at max(v·d),
// and a triangle whose vertices all reach it is held up by the plate. Counting the
// base as overhang would make a flat bottom the worst possible score, and the search
// would then work to avoid resting a part flat.
//
// ponytail: candidates are the six axes plus the directions the model's own area
// points in, bucketed on a coarse grid. A part usually rests on a face it already
// has, so this is fast and right for mechanical shapes; on an organic or lattice
// model it may find nothing better than axis-aligned. Widening it is one function —
// generate more directions and hand them to the same scorer.
func Best(m *stl.Mesh, s Spec) Result {
	s = s.withDefaults()
	identity := [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
	if len(m.Tris) == 0 {
		return Result{Down: geom.Vec3{0, 0, -1}, Rotation: identity}
	}

	faces := make([]face, 0, len(m.Tris))
	for _, t := range m.Tris {
		cross := t.B.Sub(t.A).Cross(t.C.Sub(t.A))
		l := cross.Len()
		if l == 0 {
			continue // degenerate: no normal, no area, no opinion
		}
		faces = append(faces, face{n: cross.Scale(1 / l), area: l / 2})
	}
	if len(faces) == 0 {
		return Result{Down: geom.Vec3{0, 0, -1}, Rotation: identity}
	}

	// Scoring reads faces and their vertices together, so the sample has to keep
	// them in step: sample whole triangles, not normals and vertices separately.
	scored := faces
	tris := m.Tris
	if len(faces) > s.SampleAbove {
		scored, tris = sampleTriangles(m.Tris, faces, s.SampleAbove)
	}

	cosLimit := math.Cos(s.OverhangDegrees * math.Pi / 180)
	cands := candidates(faces, s.MaxCandidates)

	type scoreT struct {
		d              geom.Vec3
		overhang, base float64
	}
	best := scoreT{}
	haveBest := false
	for _, d := range cands {
		o, b, _ := score(tris, scored, d, cosLimit)
		if !haveBest || better(o, b, best.overhang, best.base) {
			best = scoreT{d: d, overhang: o, base: b}
			haveBest = true
		}
	}

	// Re-score the winner against every triangle, so the numbers reported are the
	// real ones even when the search itself used a sample.
	o, b, h := score(m.Tris, faces, best.d, cosLimit)

	return Result{
		Down:         best.d,
		Rotation:     rotationTo(best.d),
		OverhangArea: o,
		BaseArea:     b,
		Height:       h,
		Considered:   len(cands),
	}
}

// better reports whether (o1,b1) beats (o2,b2): less overhang wins, and equal
// overhang goes to the larger base. Without the tie-break the winner would be an
// accident of candidate ordering, unstable between runs.
func better(o1, b1, o2, b2 float64) bool {
	const tol = 1e-9
	if math.Abs(o1-o2) > tol {
		return o1 < o2
	}
	return b1 > b2+tol
}

// score returns the overhang area, the resting area and the height for one down
// direction. faces[i] must describe tris[i].
func score(tris []stl.Tri, faces []face, d geom.Vec3, cosLimit float64) (overhang, base, height float64) {
	lowest := math.Inf(-1)
	highest := math.Inf(1)
	for _, t := range tris {
		for _, v := range [3]geom.Vec3{t.A, t.B, t.C} {
			p := v.Dot(d)
			if p > lowest {
				lowest = p
			}
			if p < highest {
				highest = p
			}
		}
	}
	height = lowest - highest

	// A tolerance proportional to the part, so "resting on the plate" means the same
	// thing on a 5mm part and a 500mm one.
	tol := 1e-7 * math.Max(1, math.Abs(height))

	for i, t := range tris {
		f := faces[i]
		onPlate := t.A.Dot(d) >= lowest-tol && t.B.Dot(d) >= lowest-tol && t.C.Dot(d) >= lowest-tol
		if onPlate {
			base += f.area
			continue
		}
		if f.n.Dot(d) > cosLimit {
			overhang += f.area
		}
	}
	return overhang, base, height
}

// candidates returns the directions worth trying: the six axes, plus the directions
// the model's own surface area points in.
//
// Normals are bucketed on a coarse grid with their areas summed, so a curved region
// contributes one direction rather than thousands, and the heaviest buckets — the
// flattest, largest parts of the surface — win. A part resting on a face has that
// face's outward normal pointing down, so a large face's normal is directly a
// candidate for Down.
func candidates(faces []face, max int) []geom.Vec3 {
	out := []geom.Vec3{
		{1, 0, 0}, {-1, 0, 0},
		{0, 1, 0}, {0, -1, 0},
		{0, 0, 1}, {0, 0, -1},
	}

	const grid = 16 // buckets per unit of each component
	type bucket struct {
		sum  geom.Vec3
		area float64
	}
	buckets := make(map[[3]int]*bucket, 256)
	for _, f := range faces {
		key := [3]int{
			int(math.Round(f.n[0] * grid)),
			int(math.Round(f.n[1] * grid)),
			int(math.Round(f.n[2] * grid)),
		}
		b := buckets[key]
		if b == nil {
			b = &bucket{}
			buckets[key] = b
		}
		b.sum = b.sum.Add(f.n.Scale(f.area))
		b.area += f.area
	}

	keys := make([][3]int, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	// By area, then by key, so the result does not depend on map iteration order.
	sort.Slice(keys, func(i, j int) bool {
		ai, aj := buckets[keys[i]].area, buckets[keys[j]].area
		if ai != aj {
			return ai > aj
		}
		return less(keys[i], keys[j])
	})

	for _, k := range keys {
		if len(out) >= max+6 {
			break
		}
		n := buckets[k].sum
		l := n.Len()
		if l == 0 {
			continue
		}
		out = append(out, n.Scale(1/l))
	}
	return out
}

func less(a, b [3]int) bool {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// rotationTo builds the shortest rotation sending d to (0,0,-1), row-major.
func rotationTo(d geom.Vec3) [9]float64 {
	identity := [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
	l := d.Len()
	if l == 0 {
		return identity
	}
	a := d.Scale(1 / l)
	target := geom.Vec3{0, 0, -1}

	axis := a.Cross(target)
	s := axis.Len()
	c := a.Dot(target)
	if s < 1e-12 {
		if c > 0 {
			return identity // already pointing down
		}
		// Exactly opposite: a half turn about any perpendicular axis. X will do.
		return [9]float64{1, 0, 0, 0, -1, 0, 0, 0, -1}
	}
	k := axis.Scale(1 / s)

	// Rodrigues: R = I + sin(t)K + (1-cos(t))K², with K the cross-product matrix.
	kx, ky, kz := k[0], k[1], k[2]
	oc := 1 - c
	return [9]float64{
		c + kx*kx*oc, kx*ky*oc - kz*s, kx*kz*oc + ky*s,
		ky*kx*oc + kz*s, c + ky*ky*oc, ky*kz*oc - kx*s,
		kz*kx*oc - ky*s, kz*ky*oc + kx*s, c + kz*kz*oc,
	}
}

// sampleTriangles keeps every nth triangle, scaling the kept areas so the totals
// still estimate the whole surface. Deterministic: a fixed stride rather than a
// random draw, so the same mesh always yields the same orientation.
func sampleTriangles(tris []stl.Tri, faces []face, want int) ([]face, []stl.Tri) {
	stride := len(faces)/want + 1
	outF := make([]face, 0, want+1)
	outT := make([]stl.Tri, 0, want+1)
	for i := 0; i < len(faces); i += stride {
		f := faces[i]
		f.area *= float64(stride)
		outF = append(outF, f)
		outT = append(outT, tris[i])
	}
	return outF, outT
}
