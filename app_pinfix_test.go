package main

import (
	"testing"

	"stl-cutter/internal/cut"
	"stl-cutter/internal/fixtures"
)

// The project's biggest known defect: cutting through a pin's cylinder leaves a rim of
// about three edges the cap triangulator does not pair, and auto-splitting with pins on
// every cut flagged roughly half of all pieces.
//
// The triangulation weakness is still there. What is fixed is the outcome: a cut that
// leaves a piece open now gets one repair attempt, and a three-edge rim is exactly what
// repair closes. Measured before this existed: 32 of 65 pieces flagged.
func TestAutoSplitWithPinsProducesPrintablePieces(t *testing.T) {
	count := func(pins bool) (flagged, total, gaps int) {
		app := NewApp()
		if _, err := app.loadPath(writeFixture(t, "big.stl", fixtures.Cube(300)), false); err != nil {
			t.Fatalf("load: %v", err)
		}
		ps := cut.PinSpec{Enabled: pins, Count: 4, Diameter: 4, Length: 8, Clearance: 0.15, MinWall: 1, PegOnPart: 2}
		out, err := app.AutoSplit(cut.Bed{X: 120, Y: 120, Z: 120}, ps)
		if err != nil {
			t.Fatalf("AutoSplit: %v", err)
		}
		var walk func(*Part)
		walk = func(p *Part) {
			if p == nil {
				return
			}
			if len(p.Children) == 0 {
				total++
				if !p.Watertight {
					flagged++
				}
				return
			}
			for _, c := range p.Children {
				walk(c)
			}
		}
		walk(out.Tree.Root)
		return flagged, total, out.GapsClosed
	}

	noPins, totalNoPins, _ := count(false)
	if noPins != 0 {
		t.Errorf("without pins %d of %d pieces are flagged; that path was sound and must stay so",
			noPins, totalNoPins)
	}

	withPins, totalWithPins, gaps := count(true)
	if gaps == 0 {
		t.Fatal("no gaps were closed, so this test is not exercising the repair path at all")
	}
	// Was 32 of 65. Anything close to that means the repair is not being applied.
	if withPins > totalWithPins/10 {
		t.Errorf("with pins %d of %d pieces are still flagged, and %d gap(s) were closed; "+
			"this used to be about half and should now be nearly none",
			withPins, totalWithPins, gaps)
	}
	t.Logf("with pins: %d of %d flagged, %d gap(s) closed", withPins, totalWithPins, gaps)
}

// A closed gap must be reported. The piece is not the one the cut produced, and a user
// wondering why a volume moved has to be able to find out.
func TestAClosedGapIsReported(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "big.stl", fixtures.Cube(300)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	ps := cut.PinSpec{Enabled: true, Count: 4, Diameter: 4, Length: 8, Clearance: 0.15, MinWall: 1, PegOnPart: 2}
	out, err := app.AutoSplit(cut.Bed{X: 200, Y: 200, Z: 200}, ps)
	if err != nil {
		t.Fatalf("AutoSplit: %v", err)
	}
	if out.GapsClosed == 0 {
		t.Skip("this bed did not provoke a gap; nothing to assert about reporting")
	}
	found := false
	for _, w := range out.Warnings {
		if len(w) > 0 && (containsAll(w, "gap", "closed") || containsAll(w, "gap", "triangle")) {
			found = true
		}
	}
	if !found {
		t.Errorf("%d gap(s) were closed but no warning says so: %v", out.GapsClosed, out.Warnings)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
