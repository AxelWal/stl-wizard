package cut

import (
	"testing"

	"stl-cutter/internal/fixtures"
	"stl-cutter/internal/stl"
)

func TestBedFits(t *testing.T) {
	bed := Bed{X: 220, Y: 220, Z: 250}
	if !bed.Fits(fixtures.Cube(100).BBox()) {
		t.Error("a 100mm cube fits a 220x220x250 bed")
	}
	if bed.Fits(fixtures.Cube(300).BBox()) {
		t.Error("a 300mm cube does not fit")
	}
	// Exactly the bed size counts as fitting.
	if !bed.Fits(fixtures.Cube(220).BBox()) {
		t.Error("a part exactly the bed's size should fit")
	}
}

func TestPlanAutoSplitLeavesASmallModelAlone(t *testing.T) {
	steps, err := PlanAutoSplit(fixtures.Cube(50), Bed{X: 220, Y: 220, Z: 250})
	if err != nil {
		t.Fatalf("PlanAutoSplit: %v", err)
	}
	if len(steps) != 0 {
		t.Errorf("got %d cuts for a model that already fits, want none", len(steps))
	}
}

func TestPlanAutoSplitHalvesAnOversizedModel(t *testing.T) {
	// 300mm cube on a 220mm bed: one cut per axis that overflows.
	steps, err := PlanAutoSplit(fixtures.Cube(300), Bed{X: 220, Y: 220, Z: 250})
	if err != nil {
		t.Fatalf("PlanAutoSplit: %v", err)
	}
	if len(steps) == 0 {
		t.Fatal("got no cuts for a model three times the bed")
	}
	// Every planned cut must be a valid spec the cutter will accept.
	for i, s := range steps {
		if err := s.Spec.Validate(); err != nil {
			t.Errorf("step %d produced an invalid spec: %v", i, err)
		}
	}
}

// The real test: apply the plan and confirm every resulting piece fits.
func TestPlanAutoSplitProducesPiecesThatFit(t *testing.T) {
	bed := Bed{X: 120, Y: 120, Z: 120}
	m := fixtures.Cube(300)

	steps, err := PlanAutoSplit(m, bed)
	if err != nil {
		t.Fatalf("PlanAutoSplit: %v", err)
	}

	// Apply the plan breadth-first, exactly as the app will.
	pieces := []*stl.Mesh{m}
	for _, step := range steps {
		var next []*stl.Mesh
		for _, p := range pieces {
			if bed.Fits(p.BBox()) {
				next = append(next, p)
				continue
			}
			res, err := Split(p, step.Spec)
			if err != nil {
				// A step that does not apply to this piece is not a failure of
				// the plan; another step will reach it.
				next = append(next, p)
				continue
			}
			next = append(next, res.Part1, res.Part2)
		}
		pieces = next
	}

	for i, p := range pieces {
		if !bed.Fits(p.BBox()) {
			t.Errorf("piece %d is %v, which does not fit %v", i, p.BBox().Size(), bed)
		}
	}
}

func TestPlanAutoSplitRejectsANonsenseBed(t *testing.T) {
	for name, bed := range map[string]Bed{
		"zero":     {X: 0, Y: 100, Z: 100},
		"negative": {X: 100, Y: -1, Z: 100},
	} {
		if _, err := PlanAutoSplit(fixtures.Cube(50), bed); err == nil {
			t.Errorf("%s bed: expected an error", name)
		}
	}
}

func TestPlanAutoSplitStopsAtTheDepthCap(t *testing.T) {
	// A bed so small that halving could recurse forever.
	steps, err := PlanAutoSplit(fixtures.Cube(1000), Bed{X: 0.5, Y: 0.5, Z: 0.5})
	if err != nil {
		t.Fatalf("PlanAutoSplit: %v", err)
	}
	for _, s := range steps {
		if s.Depth > maxAutoSplitDepth {
			t.Fatalf("step at depth %d exceeds the cap of %d", s.Depth, maxAutoSplitDepth)
		}
	}
}
