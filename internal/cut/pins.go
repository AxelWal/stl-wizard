package cut

import (
	"errors"
	"fmt"
	"math"

	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

// PinSpec describes the alignment pins to place on a cut face. All lengths are
// millimetres.
type PinSpec struct {
	Enabled  bool    `json:"enabled"`
	Count    int     `json:"count"` // target; fewer are placed if there is no room
	Diameter float64 `json:"diameter"`
	Length   float64 `json:"length"` // how far the peg stands proud of the face

	// Clearance is how much wider the socket is than the peg, so the printed
	// pieces actually fit together.
	Clearance float64 `json:"clearance"`
	// MinWall is the least material that must remain around and beyond a socket.
	// It is enforced both in the plane of the face and into the material behind
	// it; a pin failing either is skipped and reported.
	MinWall float64 `json:"minWall"`
	// PegOnPart is 1 or 2 — which side receives the pegs and which the sockets.
	PegOnPart int `json:"pegOnPart"`
}

// withDefaults fills in the values the UI leaves out.
//
// Zero means "unset" for Clearance and MinWall, so an explicit zero cannot be
// distinguished from an absent one and becomes the default. A press fit or a
// disabled wall guard therefore has to be asked for with a small positive value
// rather than exactly zero. The UI always sends explicit values, so this only
// affects Go callers constructing a PinSpec by hand.
func (ps PinSpec) withDefaults() PinSpec {
	if ps.Clearance == 0 {
		ps.Clearance = 0.15
	}
	if ps.MinWall == 0 {
		ps.MinWall = 1.0
	}
	if ps.PegOnPart == 0 {
		ps.PegOnPart = 2
	}
	return ps
}

func (ps PinSpec) Validate() error {
	finite := func(name string, v float64) error {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("pin %s must be a finite number, got %v", name, v)
		}
		return nil
	}
	for name, v := range map[string]float64{
		"diameter": ps.Diameter, "length": ps.Length,
		"clearance": ps.Clearance, "minimum wall": ps.MinWall,
	} {
		if err := finite(name, v); err != nil {
			return err
		}
	}
	if ps.Diameter <= 0 {
		return errors.New("pin diameter must be greater than zero")
	}
	if ps.Length <= 0 {
		return errors.New("pin length must be greater than zero")
	}
	if ps.Count <= 0 {
		return errors.New("pin count must be at least one")
	}
	if ps.Clearance < 0 {
		return errors.New("pin clearance cannot be negative")
	}
	if ps.MinWall < 0 {
		return errors.New("minimum wall cannot be negative")
	}
	if ps.PegOnPart != 1 && ps.PegOnPart != 2 {
		return fmt.Errorf("pegs must go on part 1 or part 2, got %d", ps.PegOnPart)
	}
	return nil
}

// lateralNeed is how far a pin's centre must sit from any boundary of the face:
// its own radius, the socket's clearance, and the wall that must survive around
// it.
func (ps PinSpec) lateralNeed() float64 {
	return ps.Diameter/2 + ps.Clearance + ps.MinWall
}

// pinSeparation is how far apart two pin centres must be. Two sockets each of
// radius Diameter/2 + Clearance would otherwise be free to intersect, and the
// same wall that must survive at the face's edge should survive between them.
func (ps PinSpec) pinSeparation() float64 {
	return ps.Diameter + 2*ps.Clearance + ps.MinWall
}

// distanceToBoundary returns how far p is from the nearest edge of the face,
// counting the boundaries of holes as well as the outer one. A point outside the
// face, or inside one of its holes, gets a non-positive result.
func distanceToBoundary(p pt2, g faceGroup) float64 {
	if !pointInLoop(p, g.Outer) {
		return -1
	}
	for _, h := range g.Holes {
		if pointInLoop(p, h) {
			return -1
		}
	}

	best := distanceToLoop(p, g.Outer)
	for _, h := range g.Holes {
		if d := distanceToLoop(p, h); d < best {
			best = d
		}
	}
	return best
}

func distanceToLoop(p pt2, l faceLoop) float64 {
	best := math.Inf(1)
	for i := range l {
		j := (i + 1) % len(l)
		if d := distanceToSegment(p, l[i].P2, l[j].P2); d < best {
			best = d
		}
	}
	return best
}

func distanceToSegment(p, a, b pt2) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return math.Hypot(p.X-a.X, p.Y-a.Y)
	}
	t := ((p.X-a.X)*dx + (p.Y-a.Y)*dy) / l2
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(p.X-(a.X+t*dx), p.Y-(a.Y+t*dy))
}

// placePins chooses where pins go on a face, in that face's 2D coordinates.
//
// Candidates are sampled on a grid and kept only where there is room for the pin
// plus its clearance plus the wall that must survive around it. They are then
// selected by farthest-point sampling, so pins spread across the face rather
// than clustering — a cluster does nothing to stop the printed pieces rotating
// against each other.
//
// ponytail: a grid sample rather than a medial-axis or distance-transform
// solution. A cut face is a simple polygon of tens of vertices and a handful of
// pins, so the grid is exact enough and a fraction of the code. Upgrade path if
// placement quality ever matters: compute the face's medial axis and place along
// it.
func placePins(g faceGroup, ps PinSpec) []pt2 {
	ps = ps.withDefaults()
	need := ps.lateralNeed()

	// A single grid can miss a viable spot between its samples — the worst case
	// is step/sqrt(2). Rather than claim a step is fine enough, retry finer:
	// each pass quarters the area a spot could hide in, and a face with real room
	// yields one within a couple of passes. Only a face with genuinely no room
	// survives all of them.
	var candidates []pt2
	for step := need / 2; step >= need/32; step /= 2 {
		candidates = sampleCandidates(g, need, step)
		if len(candidates) > 0 {
			break
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	// Start from the most interior point, then repeatedly take whichever
	// remaining candidate is farthest from everything chosen so far.
	chosen := []pt2{deepest(candidates, g)}
	for len(chosen) < ps.Count {
		best, bestDist := pt2{}, -1.0
		for _, c := range candidates {
			d := math.Inf(1)
			for _, s := range chosen {
				if dd := math.Hypot(c.X-s.X, c.Y-s.Y); dd < d {
					d = dd
				}
			}
			if d > bestDist {
				best, bestDist = c, d
			}
		}
		// Nothing left that is meaningfully apart from what we already have.
		if bestDist < ps.pinSeparation() {
			break
		}
		chosen = append(chosen, best)
	}
	return chosen
}

func sampleCandidates(g faceGroup, need, step float64) []pt2 {
	if step <= 0 {
		return nil
	}
	lo, hi := loopBounds(g.Outer)
	var out []pt2
	for x := lo.X; x <= hi.X; x += step {
		for y := lo.Y; y <= hi.Y; y += step {
			c := pt2{X: x, Y: y}
			if distanceToBoundary(c, g) >= need {
				out = append(out, c)
			}
		}
	}
	return out
}

func deepest(candidates []pt2, g faceGroup) pt2 {
	best, bestD := candidates[0], -1.0
	for _, c := range candidates {
		if d := distanceToBoundary(c, g); d > bestD {
			best, bestD = c, d
		}
	}
	return best
}

// axialClearance measures how much material lies behind a point, in the
// direction a socket would be bored.
//
// The footprint is sampled at nine points — the centre and eight on the boundary
// circle — and the smallest measurement wins, because a socket is a cylinder and
// it breaks out if any part of it does.
//
// ponytail: nine rays rather than a dense sweep. A far surface with a slot or
// hole narrower than the gap between adjacent samples could slip through and
// leave the clearance overestimated. Upgrade path if that ever shows up: sample
// on a spiral scaled to the socket radius, or march the whole footprint.
func axialClearance(grid *rayGrid, centre, dir geom.Vec3, radius float64, u, v geom.Vec3, eps float64) float64 {
	d := dir.Unit()
	offset := eps * 4
	start := centre.Add(d.Scale(offset))

	worst := math.Inf(1)
	samples := 8

	probe := func(from geom.Vec3) {
		dist, ok := grid.nearestHit(from, d)
		if !ok {
			// Nothing ahead at all: there is no material to bore into. That is
			// the least safe answer, not an absent one.
			worst = 0
			return
		}
		// The ray started a hair inside the material so the face it stands on
		// would not register as a zero-distance hit. Add that back, so the result
		// is measured from the face itself.
		dist += offset
		if dist < worst {
			worst = dist
		}
	}

	probe(start)
	for i := 0; i < samples; i++ {
		a := 2 * math.Pi * float64(i) / float64(samples)
		off := u.Scale(radius * math.Cos(a)).Add(v.Scale(radius * math.Sin(a)))
		probe(start.Add(off))
	}

	if math.IsInf(worst, 1) {
		return 0
	}
	return worst
}

// pinSegments is how finely a pin's circle is tessellated. 32 keeps a 4mm pin's
// facets well under a typical printer's resolution.
const pinSegments = 32

// circleLoop returns a pin's circle in a face's 2D coordinates, wound clockwise
// so groupLoops reads it as a hole. The matching 3D points are carried, not
// reconstructed, for the same reason the rest of the cap machinery carries them:
// rebuilding coordinates from 2D rounds, and the rounding shows up as hairline
// cracks.
func circleLoop(centre pt2, r float64, segments int, origin, u, v geom.Vec3) faceLoop {
	l := make(faceLoop, 0, segments)
	for i := 0; i < segments; i++ {
		// Negative angle, so the loop comes out clockwise.
		a := -2 * math.Pi * float64(i) / float64(segments)
		p := pt2{X: centre.X + r*math.Cos(a), Y: centre.Y + r*math.Sin(a)}
		l = append(l, faceVert{
			P2: p,
			P3: origin.Add(u.Scale(p.X)).Add(v.Scale(p.Y)),
		})
	}
	return l
}

// pinCylinder returns the side wall and closing disc of a pin.
//
// With cavity false it is a peg standing proud of the face: the wall faces away
// from the axis and the disc closes its free end. With cavity true it is a
// socket bored into the material: the same surface wound the other way, so it
// faces into the bore and subtracts rather than adds.
//
// base is the centre of the circle where the pin meets the face; dir is the
// direction it extends.
func pinCylinder(base, dir, u, v geom.Vec3, r, length float64, segments int, cavity bool) []stl.Tri {
	d := dir.Unit()
	tip := base.Add(d.Scale(length))

	ring := func(centre geom.Vec3, i int) geom.Vec3 {
		a := 2 * math.Pi * float64(i) / float64(segments)
		return centre.Add(u.Scale(r * math.Cos(a))).Add(v.Scale(r * math.Sin(a)))
	}

	out := make([]stl.Tri, 0, segments*4)
	for i := 0; i < segments; i++ {
		j := (i + 1) % segments
		b0, b1 := ring(base, i), ring(base, j)
		t0, t1 := ring(tip, i), ring(tip, j)

		wall := []stl.Tri{
			{A: b0, B: b1, C: t1},
			{A: b0, B: t1, C: t0},
		}
		// The end disc, fanned from the tip's centre. B and C are t0, t1 (not
		// t1, t0): the wall triangles above wind outward from the axis with
		// that same b0->b1->t1->t0 vertex order, and matching the cap's fan
		// order to it is what makes the cap face along +dir instead of into
		// the pin.
		cap := []stl.Tri{{A: tip, B: t0, C: t1}}

		for _, tr := range append(wall, cap...) {
			if cavity {
				tr = tr.Reversed()
			}
			out = append(out, tr)
		}
	}
	return out
}

// discAt returns a flat disc facing along normal. Used to close a pin for testing
// and to floor a socket.
func discAt(centre, normal, u, v geom.Vec3, r float64, segments int) []stl.Tri {
	out := make([]stl.Tri, 0, segments)
	ring := func(i int) geom.Vec3 {
		a := 2 * math.Pi * float64(i) / float64(segments)
		return centre.Add(u.Scale(r * math.Cos(a))).Add(v.Scale(r * math.Sin(a)))
	}
	for i := 0; i < segments; i++ {
		j := (i + 1) % segments
		tr := stl.Tri{A: centre, B: ring(i), C: ring(j)}
		if tr.Normal().Dot(normal) < 0 {
			tr = tr.Reversed()
		}
		out = append(out, tr)
	}
	return out
}

func loopBounds(l faceLoop) (lo, hi pt2) {
	lo = pt2{X: math.Inf(1), Y: math.Inf(1)}
	hi = pt2{X: math.Inf(-1), Y: math.Inf(-1)}
	for _, v := range l {
		lo.X = math.Min(lo.X, v.P2.X)
		lo.Y = math.Min(lo.Y, v.P2.Y)
		hi.X = math.Max(hi.X, v.P2.X)
		hi.Y = math.Max(hi.Y, v.P2.Y)
	}
	return lo, hi
}
