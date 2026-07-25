package cut

import (
	"math"
	"strings"
	"testing"

	"stl-cutter/internal/fixtures"
	"stl-cutter/internal/geom"
	"stl-cutter/internal/meshcheck"
	"stl-cutter/internal/stl"
)

// squareFace builds a faceGroup covering a square centred on the origin.
func squareFace(half float64) faceGroup {
	return faceGroup{Outer: projectZ(square(0, 0, half))}
}

func TestPinSpecDefaults(t *testing.T) {
	got := PinSpec{Enabled: true, Count: 4, Diameter: 4, Length: 8}.withDefaults()
	if got.Clearance != 0.15 {
		t.Errorf("Clearance = %v, want the 0.15 default", got.Clearance)
	}
	if got.MinWall != 1.0 {
		t.Errorf("MinWall = %v, want the 1.0 default", got.MinWall)
	}
	if got.PegOnPart != 2 {
		t.Errorf("PegOnPart = %v, want 2", got.PegOnPart)
	}

	// An explicit value is not overwritten.
	explicit := PinSpec{Enabled: true, Count: 1, Diameter: 4, Length: 8, Clearance: 0.3, MinWall: 2, PegOnPart: 1}.withDefaults()
	if explicit.Clearance != 0.3 || explicit.MinWall != 2 || explicit.PegOnPart != 1 {
		t.Errorf("defaults overwrote explicit values: %+v", explicit)
	}
}

// Zero doubles as "unset" for these two fields, so an explicit zero becomes the
// default. That is a real limitation of the representation, not an accident —
// pin it down so a change to it is deliberate.
func TestPinSpecTreatsAnExplicitZeroAsUnset(t *testing.T) {
	got := PinSpec{Enabled: true, Count: 1, Diameter: 4, Length: 8, Clearance: 0, MinWall: 0}.withDefaults()
	if got.Clearance != 0.15 {
		t.Errorf("Clearance = %v, want the 0.15 default", got.Clearance)
	}
	if got.MinWall != 1.0 {
		t.Errorf("MinWall = %v, want the 1.0 default", got.MinWall)
	}

	// A small positive value is how a caller asks for effectively no clearance.
	tiny := PinSpec{Enabled: true, Count: 1, Diameter: 4, Length: 8, Clearance: 1e-9, MinWall: 1e-9}.withDefaults()
	if tiny.Clearance != 1e-9 || tiny.MinWall != 1e-9 {
		t.Errorf("a small positive value was overwritten: %+v", tiny)
	}
}

// Applying defaults twice must not change anything.
func TestPinSpecDefaultsAreIdempotent(t *testing.T) {
	once := PinSpec{Enabled: true, Count: 2, Diameter: 4, Length: 8}.withDefaults()
	twice := once.withDefaults()
	if once != twice {
		t.Errorf("withDefaults is not idempotent: %+v then %+v", once, twice)
	}
}

func TestPinSpecValidate(t *testing.T) {
	good := PinSpec{Enabled: true, Count: 2, Diameter: 4, Length: 8}.withDefaults()
	if err := good.Validate(); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}

	for name, bad := range map[string]PinSpec{
		"zero diameter":   {Enabled: true, Count: 1, Diameter: 0, Length: 8},
		"zero length":     {Enabled: true, Count: 1, Diameter: 4, Length: 0},
		"zero count":      {Enabled: true, Count: 0, Diameter: 4, Length: 8},
		"negative wall":   {Enabled: true, Count: 1, Diameter: 4, Length: 8, MinWall: -1},
		"bad peg side":    {Enabled: true, Count: 1, Diameter: 4, Length: 8, PegOnPart: 3},
		"nan diameter":    {Enabled: true, Count: 1, Diameter: math.NaN(), Length: 8},
		"infinite length": {Enabled: true, Count: 1, Diameter: 4, Length: math.Inf(1)},
	} {
		if err := bad.withDefaults().Validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestDistanceToBoundaryOnASquare(t *testing.T) {
	g := squareFace(5) // 10x10 centred on the origin

	if got := distanceToBoundary(pt2{0, 0}, g); math.Abs(got-5) > 1e-9 {
		t.Errorf("centre distance = %v, want 5", got)
	}
	if got := distanceToBoundary(pt2{4, 0}, g); math.Abs(got-1) > 1e-9 {
		t.Errorf("near-edge distance = %v, want 1", got)
	}
	// Outside the face is not a candidate at all.
	if got := distanceToBoundary(pt2{9, 0}, g); got > 0 {
		t.Errorf("distance outside the face = %v, want 0 or less", got)
	}
}

// A hole in the face constrains placement just as its outer boundary does — a
// socket must not break out into the bore of a tube either.
func TestDistanceToBoundaryRespectsHoles(t *testing.T) {
	g := faceGroup{
		Outer: projectZ(square(0, 0, 10)),
		Holes: []faceLoop{reverseLoop(projectZ(square(0, 0, 2)))},
	}
	// A point 3 from the origin is 1 from the hole's edge, not 7 from the outer.
	if got := distanceToBoundary(pt2{3, 0}, g); math.Abs(got-1) > 1e-9 {
		t.Errorf("distance = %v, want 1 — the hole is nearer than the outer edge", got)
	}
	// A point inside the hole is not on the face at all.
	if got := distanceToBoundary(pt2{0, 0}, g); got > 0 {
		t.Errorf("distance inside the hole = %v, want 0 or less", got)
	}
}

func TestPlacePinsFindsRoomOnALargeFace(t *testing.T) {
	g := squareFace(20) // 40x40
	ps := PinSpec{Enabled: true, Count: 4, Diameter: 4, Length: 8}.withDefaults()

	pins := placePins(g, ps)
	if len(pins) != 4 {
		t.Fatalf("got %d pins, want 4", len(pins))
	}

	need := ps.Diameter/2 + ps.Clearance + ps.MinWall
	for i, p := range pins {
		if d := distanceToBoundary(p, g); d < need {
			t.Errorf("pin %d at %v is %v from the boundary, need %v", i, p, d, need)
		}
	}
}

// Pins must spread out rather than cluster, or they do nothing to stop the
// printed pieces rotating relative to each other.
func TestPlacePinsSpreadsThemOut(t *testing.T) {
	g := squareFace(20)
	ps := PinSpec{Enabled: true, Count: 4, Diameter: 4, Length: 8}.withDefaults()

	pins := placePins(g, ps)
	if len(pins) < 2 {
		t.Fatalf("got %d pins, want at least 2", len(pins))
	}
	closest := math.Inf(1)
	for i := range pins {
		for j := i + 1; j < len(pins); j++ {
			dx, dy := pins[i].X-pins[j].X, pins[i].Y-pins[j].Y
			if d := math.Hypot(dx, dy); d < closest {
				closest = d
			}
		}
	}
	// On a 40x40 face, four pins clustered within a pin diameter of each other
	// would be useless.
	if closest < ps.Diameter*2 {
		t.Errorf("closest pair is %v apart, want them spread across the face", closest)
	}
}

// A face too small for even one pin yields none — reported, not forced in.
func TestPlacePinsRefusesAFaceWithNoRoom(t *testing.T) {
	g := squareFace(1) // 2x2, far too small for a 4mm pin plus 1mm walls
	ps := PinSpec{Enabled: true, Count: 4, Diameter: 4, Length: 8}.withDefaults()

	if pins := placePins(g, ps); len(pins) != 0 {
		t.Errorf("got %d pins on a face with no room, want 0", len(pins))
	}
}

// A face with real but modest room must yield a pin. A single grid pass at
// step = need/2 can miss a viable spot by up to step/sqrt(2), and this sweep
// lands squarely in that band: at half = 3.6 the centre has 0.45mm of slack and
// the old single-pass search returned nothing.
func TestPlacePinsFindsRoomThatASingleGridPassMisses(t *testing.T) {
	ps := PinSpec{Enabled: true, Count: 1, Diameter: 4, Length: 8}.withDefaults()
	need := ps.lateralNeed()

	for _, half := range []float64{3.4, 3.6, 3.9, 4.2} {
		g := squareFace(half)
		if distanceToBoundary(pt2{0, 0}, g) < need {
			continue // this face genuinely has no room; not a case for this test
		}
		pins := placePins(g, ps)
		if len(pins) == 0 {
			t.Errorf("half = %v: the centre has %v of clearance (need %v) but no pin was placed",
				half, distanceToBoundary(pt2{0, 0}, g), need)
		}
	}
}

// Sockets must not intersect each other any more than they may break out through
// the face's edge.
//
// The face has to be cramped enough that pinSeparation actually constrains the
// result. On the original 40x40 face, farthest-point sampling spaced 6 pins
// about 15.75mm apart — far above both the old, broken threshold
// (Diameter+Clearance = 4.15) and the correct one (pinSeparation = 5.3) — so
// the assertion below never bound and the test passed even against the old
// threshold. An 18x18 face asking for 6 pins is cramped enough that the room
// available forces pins toward whatever the stopping threshold allows:
// verified by mutation, the old threshold packs 6 pins in with a closest pair
// ~4.98mm apart (between 4.15 and 5.3, so this test fails against it) while
// the correct threshold places 5 with a closest pair ~6.68mm apart (passes).
func TestPlacePinsKeepsSocketsApart(t *testing.T) {
	g := squareFace(9) // 18x18
	ps := PinSpec{Enabled: true, Count: 6, Diameter: 4, Length: 8}.withDefaults()

	pins := placePins(g, ps)
	if len(pins) < 2 {
		t.Fatalf("got %d pins, want several", len(pins))
	}
	need := ps.pinSeparation()
	for i := range pins {
		for j := i + 1; j < len(pins); j++ {
			d := math.Hypot(pins[i].X-pins[j].X, pins[i].Y-pins[j].Y)
			if d < need {
				t.Errorf("pins %d and %d are %v apart, need %v", i, j, d, need)
			}
		}
	}
}

func TestPlacePinsReturnsFewerWhenRoomIsLimited(t *testing.T) {
	g := squareFace(4.5) // 9x9: room for one pin, not four
	ps := PinSpec{Enabled: true, Count: 4, Diameter: 4, Length: 8}.withDefaults()

	pins := placePins(g, ps)
	if len(pins) == 0 || len(pins) > 4 {
		t.Fatalf("got %d pins, want between 1 and 4", len(pins))
	}
	need := ps.Diameter/2 + ps.Clearance + ps.MinWall
	for i, p := range pins {
		if d := distanceToBoundary(p, g); d < need {
			t.Errorf("pin %d is %v from the boundary, need %v", i, d, need)
		}
	}
}

// A 10-cube cut in half at z=5: below the cut face there is 5mm of material.
func TestAxialClearanceMeasuresMaterialBehindTheFace(t *testing.T) {
	m := fixtures.Cube(10)
	grid := newRayGrid(m)
	eps := m.Epsilon()

	got := axialClearance(grid, geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, -1}, 2,
		geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0}, eps)
	if math.Abs(got-5) > 1e-6 {
		t.Errorf("clearance = %v, want 5", got)
	}
}

// The whole footprint is sampled, not just the centre: a socket is a cylinder,
// and it breaks out if ANY of it does.
func TestAxialClearanceUsesTheWholeFootprint(t *testing.T) {
	// A box 20 wide, 20 deep, but only 3 tall over half its span: place the pin
	// so its centre is over deep material while its rim overhangs the shallow part.
	deep := fixtures.Box(geom.Vec3{0, 0, 0}, geom.Vec3{10, 20, 20})
	shallow := fixtures.Box(geom.Vec3{10, 0, 17}, geom.Vec3{20, 20, 20})
	m := &stl.Mesh{Tris: append(append([]stl.Tri{}, deep.Tris...), shallow.Tris...)}

	grid := newRayGrid(m)
	eps := m.Epsilon()

	// Centred at x=8, radius 4, so the footprint reaches x=12 — over the shallow
	// region, where only 3mm remains.
	got := axialClearance(grid, geom.Vec3{8, 10, 20}, geom.Vec3{0, 0, -1}, 4,
		geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0}, eps)
	if got > 4 {
		t.Errorf("clearance = %v; the footprint overhangs 3mm-thick material, so it must report about 3", got)
	}
}

// A ray that escapes without hitting anything means there is nothing behind the
// point at all — the least safe answer, not the most.
func TestAxialClearanceIsZeroWhereThereIsNoMaterial(t *testing.T) {
	m := fixtures.Cube(10)
	grid := newRayGrid(m)
	eps := m.Epsilon()

	// Standing on the top face looking up: nothing above it.
	got := axialClearance(grid, geom.Vec3{5, 5, 10}, geom.Vec3{0, 0, 1}, 2,
		geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0}, eps)
	if got != 0 {
		t.Errorf("clearance = %v, want 0 where there is no material", got)
	}
}

// The face the ray starts on must not count as the first thing it hits, or every
// measurement would be zero.
func TestAxialClearanceIgnoresTheFaceItStartsOn(t *testing.T) {
	m := fixtures.Cube(10)
	grid := newRayGrid(m)
	eps := m.Epsilon()

	got := axialClearance(grid, geom.Vec3{5, 5, 0}, geom.Vec3{0, 0, 1}, 2,
		geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0}, eps)
	if math.Abs(got-10) > 1e-6 {
		t.Errorf("clearance = %v, want 10 — the floor it starts on must not count", got)
	}
}

func TestCircleLoopIsClosedAndCorrectlySized(t *testing.T) {
	l := circleLoop(pt2{0, 0}, 3, 32, geom.Vec3{}, geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0})
	if len(l) != 32 {
		t.Fatalf("got %d vertices, want 32", len(l))
	}
	for i, v := range l {
		if got := math.Hypot(v.P2.X, v.P2.Y); math.Abs(got-3) > 1e-9 {
			t.Errorf("vertex %d is %v from the centre, want 3", i, got)
		}
	}
	// A hole must be wound clockwise so groupLoops treats it as one.
	if signedArea2(l) >= 0 {
		t.Error("a pin circle must be clockwise, so it reads as a hole")
	}
}

func TestCircleLoopCarriesMatching3DPoints(t *testing.T) {
	origin := geom.Vec3{0, 0, 5}
	u := geom.Vec3{1, 0, 0}
	v := geom.Vec3{0, 1, 0}
	l := circleLoop(pt2{2, 0}, 1, 8, origin, u, v)

	for i, fv := range l {
		want := origin.Add(u.Scale(fv.P2.X)).Add(v.Scale(fv.P2.Y))
		if fv.P3.Sub(want).Len() > 1e-9 {
			t.Errorf("vertex %d: 3D point %v does not match its 2D position", i, fv.P3)
		}
	}
}

// A peg is a closed solid on its own: its wall plus its end disc plus the hole
// it leaves in the face.
func TestPinCylinderVolumeMatchesTheIdealPeg(t *testing.T) {
	tris := pinCylinder(geom.Vec3{0, 0, 0}, geom.Vec3{0, 0, 1},
		geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0}, 2, 5, 64, false)

	// Close it with a disc at the base so the volume is measurable.
	base := discAt(geom.Vec3{0, 0, 0}, geom.Vec3{0, 0, -1}, geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0}, 2, 64)
	m := &stl.Mesh{Tris: append(append([]stl.Tri{}, tris...), base...)}

	want := math.Pi * 4 * 5
	if got := m.Volume(); math.Abs(got-want)/want > 0.01 {
		t.Errorf("peg volume = %v, want about %v", got, want)
	}
	if rep := meshcheck.Check(m, 1e-9); !rep.OK() {
		t.Errorf("a capped peg should be a closed solid: %s", rep)
	}
}

// A socket is the same shape wound the other way, so it subtracts rather than adds.
func TestPinCylinderCavityHasNegativeVolume(t *testing.T) {
	tris := pinCylinder(geom.Vec3{0, 0, 0}, geom.Vec3{0, 0, 1},
		geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0}, 2, 5, 64, true)
	base := discAt(geom.Vec3{0, 0, 0}, geom.Vec3{0, 0, 1}, geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0}, 2, 64)
	m := &stl.Mesh{Tris: append(append([]stl.Tri{}, tris...), base...)}

	want := -math.Pi * 4 * 5
	if got := m.Volume(); math.Abs(got-want)/math.Abs(want) > 0.01 {
		t.Errorf("cavity volume = %v, want about %v", got, want)
	}
	// Volume alone would pass with a compensating pair of winding errors, which
	// is exactly the defect that makes a part unprintable.
	if rep := meshcheck.Check(m, 1e-9); !rep.OK() {
		t.Errorf("a capped cavity should still be a closed solid: %s", rep)
	}
}

// The two tests above pin 64 segments, but production uses pinSegments. Exercise
// the constant itself, so lowering it past the point where a pin stops being
// round enough is caught here rather than on a printer.
func TestPinCylinderVolumeAtTheProductionTessellation(t *testing.T) {
	tris := pinCylinder(geom.Vec3{0, 0, 0}, geom.Vec3{0, 0, 1},
		geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0}, 2, 5, pinSegments, false)
	base := discAt(geom.Vec3{0, 0, 0}, geom.Vec3{0, 0, -1}, geom.Vec3{1, 0, 0}, geom.Vec3{0, 1, 0}, 2, pinSegments)
	m := &stl.Mesh{Tris: append(append([]stl.Tri{}, tris...), base...)}

	want := math.Pi * 4 * 5
	if got := m.Volume(); math.Abs(got-want)/want > 0.01 {
		t.Errorf("peg volume at %d segments = %v, want about %v", pinSegments, got, want)
	}
	if rep := meshcheck.Check(m, 1e-9); !rep.OK() {
		t.Errorf("a capped peg at %d segments should be a closed solid: %s", pinSegments, rep)
	}
}

func cutCube(t *testing.T) (*Result, Spec, float64) {
	t.Helper()
	m := fixtures.Cube(40)
	s := SpecFromNormal(geom.Vec3{20, 20, 20}, geom.Vec3{0, 0, 1}, 200, 200)
	res, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	return res, s, m.Epsilon()
}

func TestApplyPinsAddsMaterialToOnePartAndRemovesItFromTheOther(t *testing.T) {
	res, s, _ := cutCube(t)
	before1, before2 := res.Part1.Volume(), res.Part2.Volume()

	ps := PinSpec{Enabled: true, Count: 4, Diameter: 4, Length: 6}.withDefaults()
	out, err := ApplyPins(res, s, ps)
	if err != nil {
		t.Fatalf("ApplyPins: %v", err)
	}
	if out.Placed == 0 {
		t.Fatalf("no pins placed on a 40mm face; skipped: %+v", out.Skipped)
	}

	// Pegs go on part 2 by default, sockets on part 1.
	if res.Part2.Volume() <= before2 {
		t.Errorf("part 2 volume %v did not grow from %v — pegs add material",
			res.Part2.Volume(), before2)
	}
	if res.Part1.Volume() >= before1 {
		t.Errorf("part 1 volume %v did not shrink from %v — sockets remove material",
			res.Part1.Volume(), before1)
	}

	// Roughly the right amount: N pegs of the given size.
	wantAdded := float64(out.Placed) * math.Pi * 4 * 6
	if got := res.Part2.Volume() - before2; math.Abs(got-wantAdded)/wantAdded > 0.05 {
		t.Errorf("part 2 grew by %v, want about %v", got, wantAdded)
	}
}

// This is the property that makes pins usable: both parts must still be closed
// solids afterwards, or neither will print.
func TestApplyPinsLeavesBothPartsWatertight(t *testing.T) {
	res, s, eps := cutCube(t)

	ps := PinSpec{Enabled: true, Count: 3, Diameter: 5, Length: 6}.withDefaults()
	if _, err := ApplyPins(res, s, ps); err != nil {
		t.Fatalf("ApplyPins: %v", err)
	}

	if rep := meshcheck.Check(res.Part1, eps); !rep.OK() {
		t.Errorf("part 1 after pinning: %s", rep)
	}
	if rep := meshcheck.Check(res.Part2, eps); !rep.OK() {
		t.Errorf("part 2 after pinning: %s", rep)
	}
}

func TestApplyPinsRespectsThePegSide(t *testing.T) {
	res, s, eps := cutCube(t)
	before1 := res.Part1.Volume()

	ps := PinSpec{Enabled: true, Count: 2, Diameter: 4, Length: 6, PegOnPart: 1}.withDefaults()
	if _, err := ApplyPins(res, s, ps); err != nil {
		t.Fatalf("ApplyPins: %v", err)
	}
	if res.Part1.Volume() <= before1 {
		t.Error("with PegOnPart 1, part 1 should gain material")
	}
	// Volume alone would pass with the face re-paved the wrong way round. This
	// path swaps which part is flipped, so it has to be defended in its own right.
	if rep := meshcheck.Check(res.Part1, eps); !rep.OK() {
		t.Errorf("part 1 after pinning with PegOnPart 1: %s", rep)
	}
	if rep := meshcheck.Check(res.Part2, eps); !rep.OK() {
		t.Errorf("part 2 after pinning with PegOnPart 1: %s", rep)
	}
}

// Split's verdict describes the cut, not the pinned result. ApplyPins rewrites
// both cut faces, so it has to re-check what it is actually handing back.
func TestApplyPinsRefreshesTheWatertightnessVerdict(t *testing.T) {
	res, s, _ := cutCube(t)

	// A clean cut is clean before and after, so agreement alone would hold even
	// if the verdict were never recomputed. Seed both fields with a report that is
	// false about the pinned parts: only an ApplyPins that re-checks can put them
	// right.
	res.Part1Check = meshcheck.Report{OpenEdges: 7}
	res.Part2Check = meshcheck.Report{OpenEdges: 7}

	ps := PinSpec{Enabled: true, Count: 4, Diameter: 4, Length: 6}.withDefaults()
	if _, err := ApplyPins(res, s, ps); err != nil {
		t.Fatalf("ApplyPins: %v", err)
	}

	eps := math.Max(res.Part1.Epsilon(), res.Part2.Epsilon())
	if got, want := res.Part1Check.OK(), meshcheck.Check(res.Part1, eps).OK(); got != want {
		t.Errorf("Part1Check says OK=%v but the mesh says %v", got, want)
	}
	if got, want := res.Part2Check.OK(), meshcheck.Check(res.Part2, eps).OK(); got != want {
		t.Errorf("Part2Check says OK=%v but the mesh says %v", got, want)
	}
	if !res.Watertight() {
		t.Errorf("a clean pinned cut should report watertight; warnings: %v", res.Warnings)
	}
}

// A model with a second body whose face lies exactly in the cutting plane used to
// have that face silently deleted: both parts are re-paved from one part's face
// regions, so a region present on only one side was dropped and never replaced.
// The result was an unprintable part with no warning at all.
func TestApplyPinsRefusesWhenTheTwoFacesDoNotMatch(t *testing.T) {
	big := fixtures.Cube(40)
	stray := fixtures.Box(geom.Vec3{60, 0, 20}, geom.Vec3{70, 10, 30})
	m := &stl.Mesh{Tris: append(append([]stl.Tri{}, big.Tris...), stray.Tris...)}

	s := SpecFromNormal(geom.Vec3{20, 20, 20}, geom.Vec3{0, 0, 1}, 30, 30)
	res, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	out, err := ApplyPins(res, s, PinSpec{Enabled: true, Count: 2, Diameter: 4, Length: 6}.withDefaults())
	if err != nil {
		t.Fatalf("ApplyPins: %v", err)
	}
	if len(out.Warnings) == 0 {
		t.Error("a mismatched pair of cut faces must be reported, not silently mangled")
	}

	// Whatever it decides to do, it must not hand back a broken part quietly.
	eps := math.Max(res.Part1.Epsilon(), res.Part2.Epsilon())
	if rep := meshcheck.Check(res.Part1, eps); !rep.OK() && res.Part1Check.OK() {
		t.Errorf("part 1 is %s but Part1Check reports it clean", rep)
	}
	if rep := meshcheck.Check(res.Part2, eps); !rep.OK() && res.Part2Check.OK() {
		t.Errorf("part 2 is %s but Part2Check reports it clean", rep)
	}
}

// The 1mm wall guard, laterally. A face barely wider than the pin has no room.
func TestApplyPinsSkipsWhenTheFaceIsTooNarrow(t *testing.T) {
	m := fixtures.Cube(10)
	// A 5x5 rectangle: a 4mm pin needs 4/2 + 0.15 + 1 = 3.15mm from every edge,
	// which a 5mm-wide face cannot give.
	s := SpecFromNormal(geom.Vec3{5, 5, 5}, geom.Vec3{0, 0, 1}, 5, 5)
	res, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	ps := PinSpec{Enabled: true, Count: 2, Diameter: 4, Length: 3}.withDefaults()
	out, err := ApplyPins(res, s, ps)
	if err != nil {
		t.Fatalf("ApplyPins: %v", err)
	}
	if out.Placed != 0 {
		t.Errorf("placed %d pins on a face with no room", out.Placed)
	}
	if len(out.Warnings) == 0 {
		t.Error("a face with no room must be reported, not silently left bare")
	}
}

// The 1mm wall guard, axially — the guard that measures material BEHIND the
// face, not room across it.
//
// The fixture has to pass the lateral guard to reach the axial one. A hollow
// shell does not: its cut face is a wall-thin ring, so placePins finds nowhere
// to stand and the run ends at the lateral warning without a single skipped pin
// to show. A wide, thin plate separates the two: 40x40 of face is ample room
// laterally, while the 4mm of plate behind it is nowhere near the 7.15mm a
// 6mm peg's socket needs (6 + 0.15 clearance + 1 wall).
func TestApplyPinsSkipsWhenThereIsNoMaterialBehind(t *testing.T) {
	m := fixtures.Box(geom.Vec3{0, 0, 0}, geom.Vec3{40, 40, 8})
	s := SpecFromNormal(geom.Vec3{20, 20, 4}, geom.Vec3{0, 0, 1}, 200, 200)
	res, err := Split(m, s)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	ps := PinSpec{Enabled: true, Count: 4, Diameter: 4, Length: 6}.withDefaults()
	out, err := ApplyPins(res, s, ps)
	if err != nil {
		t.Fatalf("ApplyPins: %v", err)
	}
	if out.Placed != 0 {
		t.Errorf("placed %d pins into a 4mm plate; a 6mm socket cannot fit", out.Placed)
	}
	// The point of the fixture: the axial guard is what refuses these, so it must
	// actually report them.
	if len(out.Skipped) == 0 {
		t.Fatalf("nothing was skipped, so the axial guard was never reached; warnings: %v", out.Warnings)
	}
	for _, sk := range out.Skipped {
		if !strings.Contains(sk.Reason, "material behind") {
			t.Errorf("skipped for %q, want the axial guard's reason", sk.Reason)
		}
		if sk.Measured >= sk.Required {
			t.Errorf("skipped pin reports measured %v >= required %v, which is not a reason to skip",
				sk.Measured, sk.Required)
		}
	}
}

func TestApplyPinsIsANoOpWhenDisabled(t *testing.T) {
	res, s, _ := cutCube(t)
	before1, before2 := res.Part1.Volume(), res.Part2.Volume()

	out, err := ApplyPins(res, s, PinSpec{Enabled: false})
	if err != nil {
		t.Fatalf("ApplyPins: %v", err)
	}
	if out.Placed != 0 {
		t.Errorf("placed %d pins while disabled", out.Placed)
	}
	if res.Part1.Volume() != before1 || res.Part2.Volume() != before2 {
		t.Error("a disabled pin spec must not change the geometry")
	}
}

func TestApplyPinsRejectsAnInvalidSpec(t *testing.T) {
	res, s, _ := cutCube(t)
	if _, err := ApplyPins(res, s, PinSpec{Enabled: true, Count: 1, Diameter: -4, Length: 6}); err == nil {
		t.Error("expected an error for a negative diameter")
	}
}
