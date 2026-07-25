package cut

import (
	"errors"
	"fmt"
	"math"
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
