package cut

import (
	"errors"
	"fmt"
	"math"

	"stl-cutter/internal/geom"
	"stl-cutter/internal/meshcheck"
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

// SkippedPin records a pin that could not be placed, and why. A skipped pin is
// always reported: silently leaving one out would let a user print two pieces
// that do not locate against each other.
type SkippedPin struct {
	// One tag each: a shared `json:"x"` across all three, which go vet rejects,
	// would serialise the same field three times and lose Y and Z entirely.
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Z        float64 `json:"z"`
	Reason   string  `json:"reason"`
	Measured float64 `json:"measured"`
	Required float64 `json:"required"`
}

type PinResult struct {
	Placed   int          `json:"placed"`
	Skipped  []SkippedPin `json:"skipped"`
	Warnings []string     `json:"warnings"`
}

// ApplyPins adds alignment pins to a finished cut, modifying both parts in place.
//
// It runs after the cut rather than during it because Split caps plane by plane:
// the cut plane's cap is built before the rectangle's side planes trim it, so
// pins planned mid-cut could sit in material those planes later remove. Only the
// plane the user positioned is pinned — the rectangle's sides are structural,
// not mating surfaces.
//
// Both parts stay closed solids: a pin is a hole punched in the cut face plus a
// cylinder closing it, so no boolean operation is involved anywhere.
func ApplyPins(res *Result, s Spec, ps PinSpec) (*PinResult, error) {
	out := &PinResult{}
	if !ps.Enabled {
		return out, nil
	}
	ps = ps.withDefaults()
	if err := ps.Validate(); err != nil {
		return nil, err
	}

	cutPlane := s.Planes()[0]
	// The looser of the two, so the tolerance is no tighter than the one the cut
	// was made with and the same value is fair to both parts.
	eps := math.Max(res.Part1.Epsilon(), res.Part2.Epsilon())

	// The peg extends out of the peg-bearing part, and the socket is bored the
	// same way into the other one — so the peg exactly fills what the socket
	// removes.
	//
	// The cut plane's normal points into part 2, so at the cut face part 2's
	// material lies along +N and part 1's along -N. An outward normal points away
	// from its own material: part 1's face looks along +N, part 2's along -N.
	// triangulateFace and repaveFace both emit along +N, so it is part 2 — whether
	// it carries the peg or the socket — whose re-paved face has to be flipped.
	pegPart, socketPart := res.Part2, res.Part1
	pegDir := cutPlane.N.Unit().Scale(-1)
	pegFlip, socketFlip := true, false
	if ps.PegOnPart == 1 {
		pegPart, socketPart = res.Part1, res.Part2
		pegDir = cutPlane.N.Unit()
		pegFlip, socketFlip = false, true
	}

	idxPeg, groups, origin, u, v, ok := cutFace(pegPart, cutPlane, eps)
	if !ok {
		out.Warnings = append(out.Warnings,
			"could not read the cut face on the part carrying the pegs, so no pins were placed")
		return out, nil
	}
	// The two faces are the same region — part 1's is part 2's cap reversed — so
	// the peg part's loops are used to re-pave both. That is not a shortcut: it is
	// what guarantees the two re-paved faces share their boundary vertex for
	// vertex, however each part's own loop assembly happened to order things.
	idxSocket, socketGroups, _, _, _, okS := cutFace(socketPart, cutPlane, eps)
	if !okS {
		out.Warnings = append(out.Warnings,
			"could not read the cut face on the part carrying the sockets, so no pins were placed")
		return out, nil
	}

	// Both parts are re-paved from the peg part's groups, so a face region present
	// on one part and not the other would be dropped and never replaced. That
	// happens with multi-body models where an unrelated shell's face lies exactly
	// in the cut plane.
	if len(groups) != len(socketGroups) {
		out.Warnings = append(out.Warnings, fmt.Sprintf(
			"the two cut faces do not match (%d region(s) against %d), so no pins were placed; "+
				"this usually means another part of the model lies exactly in the cutting plane",
			len(groups), len(socketGroups)))
		return out, nil
	}

	// pinCylinder's cavity flag assumes a basis right-handed about the direction
	// the pin runs: it winds its wall from u x v, so with dir opposed to u x v the
	// flag would mean the opposite of what it says. planeBasis gives u x v == +N,
	// so a pin running along -N gets a mirrored basis. Either way the cylinder's
	// ring lands on exactly the same points as circleLoop's, so the two meet along
	// the same edges. Which index carries which point is not the same in both
	// cases — with the pegs on part 1 the two run the circle in opposite orders —
	// and it does not need to be: the edges coincide either way, and what has to
	// agree is the orientation the wall and the hole give them.
	pu, pv := u, v
	if pegDir.Dot(u.Cross(v)) < 0 {
		pv = v.Scale(-1)
	}

	socketGrid := newRayGrid(socketPart)
	r := ps.Diameter / 2
	socketR := r + ps.Clearance
	socketDepth := ps.Length + ps.Clearance
	axialNeed := socketDepth + ps.MinWall

	// Circles to punch into each part's face, and the cylinders to close them.
	pegHoles := map[int][]faceLoop{}
	socketHoles := map[int][]faceLoop{}
	var pegTris, socketTris []stl.Tri

	for gi, g := range groups {
		positions := placePins(g, ps)
		if len(positions) == 0 {
			out.Warnings = append(out.Warnings, fmt.Sprintf(
				"no room for a %gmm pin on one cut face: it needs %gmm of clearance from every edge",
				ps.Diameter, ps.lateralNeed()))
			continue
		}

		for _, p := range positions {
			centre3 := origin.Add(u.Scale(p.X)).Add(v.Scale(p.Y))

			have := axialClearance(socketGrid, centre3, pegDir, socketR, u, v, eps)
			if have < axialNeed {
				out.Skipped = append(out.Skipped, SkippedPin{
					X: centre3[0], Y: centre3[1], Z: centre3[2],
					Reason:   "not enough material behind the face for the socket",
					Measured: have, Required: axialNeed,
				})
				continue
			}

			pegHoles[gi] = append(pegHoles[gi], circleLoop(p, r, pinSegments, origin, u, v))
			socketHoles[gi] = append(socketHoles[gi], circleLoop(p, socketR, pinSegments, origin, u, v))

			// Each cylinder is closed at its far end by pinCylinder's own disc and
			// at the face by the hole it stands in, so neither needs a further cap.
			pegTris = append(pegTris, pinCylinder(centre3, pegDir, pu, pv, r, ps.Length, pinSegments, false)...)
			socketTris = append(socketTris, pinCylinder(centre3, pegDir, pu, pv, socketR, socketDepth, pinSegments, true)...)

			out.Placed++
		}
	}

	if out.Placed == 0 {
		return out, nil
	}

	pegPaved, err := repaveFace(pegPart, idxPeg, groups, pegHoles, pegFlip)
	if err != nil {
		return nil, err
	}
	socketPaved, err := repaveFace(socketPart, idxSocket, groups, socketHoles, socketFlip)
	if err != nil {
		return nil, err
	}
	// Keep what pinning is about to replace, and the verdict that described it.
	// repaveFace builds fresh slices, so these stay valid however the pinned
	// parts are appended to.
	pegBefore, socketBefore := pegPart.Tris, socketPart.Tris
	before1, before2 := res.Part1Check, res.Part2Check

	pegPart.Tris = append(pegPaved, pegTris...)
	socketPart.Tris = append(socketPaved, socketTris...)

	// Pinning rewrites both cut faces, so the cut's own verdict no longer
	// describes what is being handed back. Re-check the finished parts and
	// replace it. This is also the net that catches a face which could not be
	// re-paved cleanly — a hole filled solid, or a triangle dropped and never
	// replaced — none of which the placement logic can see for itself.
	res.Part1Check = meshcheck.Check(res.Part1, eps)
	res.Part2Check = meshcheck.Check(res.Part2, eps)

	// A sound cut that pinning has holed is worse than the same cut with no pins
	// in it: unpinned pieces still print and still mate, an open part does
	// neither. Put the bare cut back rather than leaving Undo as the only way out.
	//
	// ponytail: "worse" means a part that was sound is not any more. A part the
	// cut already broke is left as pinned — there is nothing sound left to
	// protect, and rolling back would not make it printable either.
	if (before1.OK() && !res.Part1Check.OK()) || (before2.OK() && !res.Part2Check.OK()) {
		pegPart.Tris, socketPart.Tris = pegBefore, socketBefore
		res.Part1Check, res.Part2Check = before1, before2
		out.Placed, out.Skipped = 0, nil
		out.Warnings = append(out.Warnings,
			"adding pins would have left a part open, so the cut was kept without them")
		return out, nil
	}

	if !res.Part1Check.OK() {
		out.Warnings = append(out.Warnings, fmt.Sprintf(
			"after adding pins, the remaining part is not a closed solid: %s", res.Part1Check))
	}
	if !res.Part2Check.OK() {
		out.Warnings = append(out.Warnings, fmt.Sprintf(
			"after adding pins, the cut-off part is not a closed solid: %s", res.Part2Check))
	}
	return out, nil
}

// repaveFace returns a part's triangles with its cut face replaced by a fresh
// triangulation carrying the pin circles as extra holes. It assigns nothing, so
// a caller can build both parts' replacements before committing either — an
// error partway through must not leave one part punched and the other whole.
//
// flip is set for the part whose face looks the other way, so both parts keep
// their own outward orientation.
func repaveFace(part *stl.Mesh, idx []int, groups []faceGroup, holes map[int][]faceLoop, flip bool) ([]stl.Tri, error) {
	drop := make(map[int]bool, len(idx))
	for _, i := range idx {
		drop[i] = true
	}
	keep := make([]stl.Tri, 0, len(part.Tris))
	for i, t := range part.Tris {
		if !drop[i] {
			keep = append(keep, t)
		}
	}

	for gi, g := range groups {
		merged := bridgeHoles(g.Outer, append(append([]faceLoop{}, g.Holes...), holes[gi]...))
		tris, ok := earClip(merged)
		if !ok {
			return nil, fmt.Errorf("could not re-triangulate a cut face around its pins")
		}
		for _, tr := range tris {
			t := stl.Tri{A: merged[tr[0]].P3, B: merged[tr[1]].P3, C: merged[tr[2]].P3}
			// earClip emits faces along +p.N; each part needs its own outward
			// direction.
			if flip {
				t = t.Reversed()
			}
			keep = append(keep, t)
		}
	}

	return keep, nil
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
