package cut

import (
	"math"
	"testing"
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
func TestPlacePinsKeepsSocketsApart(t *testing.T) {
	g := squareFace(20)
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
