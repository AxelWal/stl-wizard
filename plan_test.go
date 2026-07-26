package main

import (
	"context"
	"math"
	"strings"
	"testing"

	"stl-wizard/internal/cut"
	"stl-wizard/internal/fixtures"
	"stl-wizard/internal/geom"
	"stl-wizard/internal/stl"
)

// acrossTheArms is the plane that cuts the U at y=25 spanning everything.
func acrossTheArms() PlaneInput {
	return PlaneInput{
		Origin: [3]float64{15, 25, 5},
		Normal: [3]float64{0, 1, 0},
		Width:  200,
		Height: 200,
	}
}

// leftArmOnly is the bounded rectangle covering only the U's left arm.
func leftArmOnly() PlaneInput {
	return PlaneInput{
		Origin: [3]float64{7.5, 25, 5},
		Normal: [3]float64{0, 1, 0},
		Width:  15,
		Height: 20,
	}
}

func loadU(t *testing.T) *App {
	t.Helper()
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "u.stl", fixtures.UShape(10)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	return app
}

// The whole point: adding a plane must not cut anything.
func TestAddPlaneCutsNothing(t *testing.T) {
	app := loadU(t)

	pv, err := app.AddPlane(leftArmOnly(), cut.PinSpec{})
	if err != nil {
		t.Fatalf("AddPlane: %v", err)
	}
	if len(pv.Cuts) != 1 {
		t.Fatalf("got %d planned cuts, want 1", len(pv.Cuts))
	}

	tree := app.view()
	if !tree.Root.IsLeaf() {
		t.Error("the model was cut; adding a plane must only plan")
	}
	if tree.CanUndo {
		t.Error("nothing was cut, so there should be nothing to undo")
	}
}

func TestPlannedCutsGetNumberedDefaultNames(t *testing.T) {
	app := loadU(t)
	for i := 0; i < 3; i++ {
		if _, err := app.AddPlane(leftArmOnly(), cut.PinSpec{}); err != nil {
			t.Fatalf("AddPlane %d: %v", i, err)
		}
	}
	pv := app.Plan()
	for i, want := range []string{"Cut 1", "Cut 2", "Cut 3"} {
		if pv.Cuts[i].Name != want {
			t.Errorf("cut %d is named %q, want %q", i, pv.Cuts[i].Name, want)
		}
	}
}

// Numbering must not be reused, or two entries would carry the same label and be
// indistinguishable in the list.
func TestDeletingAPlannedCutDoesNotRecycleItsNumber(t *testing.T) {
	app := loadU(t)
	for i := 0; i < 2; i++ {
		if _, err := app.AddPlane(leftArmOnly(), cut.PinSpec{}); err != nil {
			t.Fatalf("AddPlane: %v", err)
		}
	}
	pv := app.Plan()
	if _, err := app.DeletePlane(pv.Cuts[0].ID); err != nil {
		t.Fatalf("DeletePlane: %v", err)
	}
	pv, err := app.AddPlane(leftArmOnly(), cut.PinSpec{})
	if err != nil {
		t.Fatalf("AddPlane: %v", err)
	}
	names := []string{}
	for _, c := range pv.Cuts {
		names = append(names, c.Name)
	}
	if got := strings.Join(names, ","); got != "Cut 2,Cut 3" {
		t.Errorf("names are %q, want \"Cut 2,Cut 3\" — a number must not be handed out twice", got)
	}
}

// An empty plan must be an empty list, not a null. A nil slice marshals as JSON null,
// and the frontend reading .some() off that killed the whole module on load.
func TestAnEmptyPlanIsAnEmptyListNotNull(t *testing.T) {
	app := loadU(t)
	if got := app.Plan().Cuts; got == nil {
		t.Error("Plan().Cuts is nil; it must be an empty slice so it marshals as []")
	}
	if _, err := app.AddPlane(leftArmOnly(), cut.PinSpec{}); err != nil {
		t.Fatalf("AddPlane: %v", err)
	}
	pv, err := app.ClearPlan()
	if err != nil {
		t.Fatalf("ClearPlan: %v", err)
	}
	if pv.Cuts == nil {
		t.Error("Cuts is nil after clearing; it must be an empty slice")
	}
}

func TestRenameAndDeleteAndDisable(t *testing.T) {
	app := loadU(t)
	if _, err := app.AddPlane(leftArmOnly(), cut.PinSpec{}); err != nil {
		t.Fatalf("AddPlane: %v", err)
	}
	id := app.Plan().Cuts[0].ID

	if _, err := app.RenamePlane(id, "left arm"); err != nil {
		t.Fatalf("RenamePlane: %v", err)
	}
	if got := app.Plan().Cuts[0].Name; got != "left arm" {
		t.Errorf("name = %q, want \"left arm\"", got)
	}
	if _, err := app.RenamePlane(id, "  "); err == nil {
		t.Error("an all-whitespace name should be refused")
	}

	if _, err := app.SetPlaneEnabled(id, false); err != nil {
		t.Fatalf("SetPlaneEnabled: %v", err)
	}
	if app.Plan().Cuts[0].Enabled {
		t.Error("the cut is still enabled")
	}

	if _, err := app.DeletePlane(id); err != nil {
		t.Fatalf("DeletePlane: %v", err)
	}
	if len(app.Plan().Cuts) != 0 {
		t.Error("the cut was not removed")
	}
	if _, err := app.DeletePlane(id); err == nil {
		t.Error("deleting an unknown id should be an error")
	}
}

// One entry must give exactly what Cut gives for the same plane, or the plan is a
// second implementation of cutting rather than a queue for the one that exists.
func TestExecutingOneEntryMatchesAnImmediateCut(t *testing.T) {
	direct := loadU(t)
	if _, err := direct.Cut(direct.view().Root.ID, leftArmOnly(), cut.PinSpec{}); err != nil {
		t.Fatalf("Cut: %v", err)
	}

	planned := loadU(t)
	if _, err := planned.AddPlane(leftArmOnly(), cut.PinSpec{}); err != nil {
		t.Fatalf("AddPlane: %v", err)
	}
	out, err := planned.ExecutePlan()
	if err != nil {
		t.Fatalf("ExecutePlan: %v", err)
	}
	if out.CutsMade != 1 {
		t.Errorf("CutsMade = %d, want 1", out.CutsMade)
	}
	if !out.Watertight {
		t.Errorf("the plan's cut is not watertight; warnings %v", out.Warnings)
	}

	want := volumes(direct.view())
	got := volumes(out.Tree)
	if len(got) != len(want) {
		t.Fatalf("got %d parts, want %d", len(got), len(want))
	}
	for i := range want {
		if math.Abs(got[i]-want[i]) > 1e-6 {
			t.Errorf("part %d volume = %v, want %v", i, got[i], want[i])
		}
	}
}

// The plan is the source of truth, so executing it twice has to give the same tree.
// Anything else means the second run built on the first instead of replacing it.
func TestExecutingTwiceGivesTheSameTree(t *testing.T) {
	app := loadU(t)
	if _, err := app.AddPlane(leftArmOnly(), cut.PinSpec{}); err != nil {
		t.Fatalf("AddPlane: %v", err)
	}
	first, err := app.ExecutePlan()
	if err != nil {
		t.Fatalf("first execute: %v", err)
	}
	second, err := app.ExecutePlan()
	if err != nil {
		t.Fatalf("second execute: %v", err)
	}

	if second.CutsMade != first.CutsMade {
		t.Errorf("CutsMade went from %d to %d", first.CutsMade, second.CutsMade)
	}
	a, b := volumes(first.Tree), volumes(second.Tree)
	if len(a) != len(b) {
		t.Fatalf("part counts differ: %d then %d", len(a), len(b))
	}
	for i := range a {
		if math.Abs(a[i]-b[i]) > 1e-6 {
			t.Errorf("part %d volume went from %v to %v", i, a[i], b[i])
		}
	}
}

// Editing an entry and executing again must give the edited result, not the old one.
func TestEditingAnEntryChangesWhatIsCut(t *testing.T) {
	app := loadU(t)
	if _, err := app.AddPlane(leftArmOnly(), cut.PinSpec{}); err != nil {
		t.Fatalf("AddPlane: %v", err)
	}
	out, err := app.ExecutePlan()
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// The bounded cut takes only the left arm's top: 1500.
	if !hasVolume(out.Tree, 1500) {
		t.Fatalf("expected a 1500mm³ piece, got %v", volumes(out.Tree))
	}

	id := app.Plan().Cuts[0].ID
	if _, err := app.UpdatePlane(id, acrossTheArms(), cut.PinSpec{}); err != nil {
		t.Fatalf("UpdatePlane: %v", err)
	}
	out, err = app.ExecutePlan()
	if err != nil {
		t.Fatalf("re-execute: %v", err)
	}
	// Unbounded across x, the same height takes both arms' tops: 3000.
	if !hasVolume(out.Tree, 3000) {
		t.Errorf("expected a 3000mm³ piece after widening the plane, got %v", volumes(out.Tree))
	}
	if hasVolume(out.Tree, 1500) {
		t.Error("the old bounded result survived; the tree was not rebuilt from the plan")
	}
}

func TestADisabledEntryIsNotCut(t *testing.T) {
	app := loadU(t)
	if _, err := app.AddPlane(leftArmOnly(), cut.PinSpec{}); err != nil {
		t.Fatalf("AddPlane: %v", err)
	}
	if _, err := app.AddPlane(acrossTheArms(), cut.PinSpec{}); err != nil {
		t.Fatalf("AddPlane: %v", err)
	}
	id := app.Plan().Cuts[1].ID
	if _, err := app.SetPlaneEnabled(id, false); err != nil {
		t.Fatalf("SetPlaneEnabled: %v", err)
	}

	out, err := app.ExecutePlan()
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out.CutsMade != 1 {
		t.Errorf("CutsMade = %d, want 1 — the disabled entry should not run", out.CutsMade)
	}
}

// An entry naming a part an earlier entry was to have produced has nothing to cut once
// that earlier entry is gone. It has to be reported, not quietly dropped.
func TestAnEntryWhoseTargetIsGoneIsReported(t *testing.T) {
	app := loadU(t)
	if _, err := app.AddPlane(leftArmOnly(), cut.PinSpec{}); err != nil {
		t.Fatalf("first AddPlane: %v", err)
	}
	if _, err := app.ExecutePlan(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	// Select a piece the first cut produced and plan a cut on it.
	tv := app.view()
	child := tv.Root.Children[0]
	if _, err := app.Select(child.ID); err != nil {
		t.Fatalf("Select: %v", err)
	}
	if _, err := app.AddPlane(acrossTheArms(), cut.PinSpec{}); err != nil {
		t.Fatalf("second AddPlane: %v", err)
	}
	if out, err := app.ExecutePlan(); err != nil {
		t.Fatalf("execute both: %v", err)
	} else if out.CutsMade != 2 {
		t.Fatalf("CutsMade = %d, want 2 with both entries", out.CutsMade)
	}

	// Now remove the first entry. The second targets a part nothing produces.
	if _, err := app.DeletePlane(app.Plan().Cuts[0].ID); err != nil {
		t.Fatalf("DeletePlane: %v", err)
	}
	out, err := app.ExecutePlan()
	if err != nil {
		t.Fatalf("execute after delete: %v", err)
	}
	if len(out.Skipped) != 1 {
		t.Fatalf("got %d skipped entries, want 1: %v", len(out.Skipped), out.Skipped)
	}
	if !strings.Contains(out.Skipped[0], child.Name) {
		t.Errorf("the skip message %q should name the missing part %q", out.Skipped[0], child.Name)
	}
	if out.CutsMade != 0 {
		t.Errorf("CutsMade = %d; the surviving entry had nothing to cut", out.CutsMade)
	}
}

func TestReorderRearrangesThePlan(t *testing.T) {
	app := loadU(t)
	for i := 0; i < 3; i++ {
		if _, err := app.AddPlane(leftArmOnly(), cut.PinSpec{}); err != nil {
			t.Fatalf("AddPlane: %v", err)
		}
	}
	ids := []string{}
	for _, c := range app.Plan().Cuts {
		ids = append(ids, c.ID)
	}

	pv, err := app.ReorderPlan([]string{ids[2], ids[0], ids[1]})
	if err != nil {
		t.Fatalf("ReorderPlan: %v", err)
	}
	got := []string{}
	for _, c := range pv.Cuts {
		got = append(got, c.Name)
	}
	if strings.Join(got, ",") != "Cut 3,Cut 1,Cut 2" {
		t.Errorf("order is %v, want Cut 3,Cut 1,Cut 2", got)
	}
}

// Order decides what each entry has to cut, so reordering changes results rather than
// only the display. Cut 2 aims at a piece Cut 1 produces; run first, it has nothing.
func TestReorderChangesWhatGetsCut(t *testing.T) {
	app := loadU(t)
	if _, err := app.AddPlane(leftArmOnly(), cut.PinSpec{}); err != nil {
		t.Fatalf("first AddPlane: %v", err)
	}
	if _, err := app.ExecutePlan(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	child := app.view().Root.Children[0]
	if _, err := app.Select(child.ID); err != nil {
		t.Fatalf("Select: %v", err)
	}
	if _, err := app.AddPlane(acrossTheArms(), cut.PinSpec{}); err != nil {
		t.Fatalf("second AddPlane: %v", err)
	}

	out, err := app.ExecutePlan()
	if err != nil {
		t.Fatalf("execute in order: %v", err)
	}
	if out.CutsMade != 2 || len(out.Skipped) != 0 {
		t.Fatalf("in order: %d cuts, skipped %v; want 2 and none", out.CutsMade, out.Skipped)
	}

	ids := []string{app.Plan().Cuts[0].ID, app.Plan().Cuts[1].ID}
	if _, err := app.ReorderPlan([]string{ids[1], ids[0]}); err != nil {
		t.Fatalf("ReorderPlan: %v", err)
	}
	out, err = app.ExecutePlan()
	if err != nil {
		t.Fatalf("execute reversed: %v", err)
	}
	if len(out.Skipped) != 1 {
		t.Errorf("reversed: skipped %v, want the entry whose target does not exist yet", out.Skipped)
	}
	if out.CutsMade != 1 {
		t.Errorf("reversed: %d cuts made, want 1", out.CutsMade)
	}
}

// A list built before a delete landed would reorder into something never seen.
func TestReorderRefusesAStaleList(t *testing.T) {
	app := loadU(t)
	for i := 0; i < 2; i++ {
		if _, err := app.AddPlane(leftArmOnly(), cut.PinSpec{}); err != nil {
			t.Fatalf("AddPlane: %v", err)
		}
	}
	ids := []string{app.Plan().Cuts[0].ID, app.Plan().Cuts[1].ID}

	if _, err := app.ReorderPlan([]string{ids[0]}); err == nil {
		t.Error("too few ids should be refused")
	}
	if _, err := app.ReorderPlan([]string{ids[0], ids[0]}); err == nil {
		t.Error("a repeated id should be refused; it would duplicate one entry and lose another")
	}
	if _, err := app.ReorderPlan([]string{ids[0], "nope"}); err == nil {
		t.Error("an unknown id should be refused")
	}
	// And a refused reorder must leave the plan alone.
	if got := len(app.Plan().Cuts); got != 2 {
		t.Errorf("the plan holds %d cuts after refused reorders, want 2", got)
	}
}

func TestExecutingAnEmptyPlanIsRefused(t *testing.T) {
	app := loadU(t)
	if _, err := app.ExecutePlan(); err == nil {
		t.Error("executing an empty plan should be refused, not produce an uncut tree silently")
	}
	if !app.view().Root.IsLeaf() {
		t.Error("the tree should be untouched")
	}
}

// Auto-split proposes rather than cuts, and its entries apply to every piece their
// bounded rectangle crosses.
func TestPlanFitToPrinterProposesWithoutCutting(t *testing.T) {
	app := loadU(t)
	pv, err := app.PlanFitToPrinter(cut.Bed{X: 20, Y: 20, Z: 20})
	if err != nil {
		t.Fatalf("PlanFitToPrinter: %v", err)
	}
	if len(pv.Cuts) == 0 {
		t.Fatal("no cuts were planned for a 30x40x10 model on a 20mm bed")
	}
	if !app.view().Root.IsLeaf() {
		t.Fatal("the model was cut; planning must only plan")
	}

	out, err := app.ExecutePlan()
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out.CutsMade == 0 {
		t.Error("executing the plan cut nothing")
	}
	for _, v := range sizes(out.Tree) {
		for _, d := range v {
			if d > 20.001 {
				t.Errorf("a piece measures %v, above the 20mm bed", v)
				break
			}
		}
	}
}

// A plan names parts of the model it was built against, so keeping it across a load
// would leave every entry aimed at something that does not exist.
func TestLoadingAModelClearsThePlan(t *testing.T) {
	app := loadU(t)
	if _, err := app.AddPlane(leftArmOnly(), cut.PinSpec{}); err != nil {
		t.Fatalf("AddPlane: %v", err)
	}
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10)), false); err != nil {
		t.Fatalf("second load: %v", err)
	}
	if got := len(app.Plan().Cuts); got != 0 {
		t.Errorf("the plan still holds %d cut(s) from the previous model", got)
	}
}

// However many entries a plan holds, the progress bar is shown and hidden once.
func TestExecutePlanEmitsOneEventPair(t *testing.T) {
	app := loadU(t)
	for i := 0; i < 3; i++ {
		if _, err := app.AddPlane(leftArmOnly(), cut.PinSpec{}); err != nil {
			t.Fatalf("AddPlane: %v", err)
		}
	}
	starts, dones := countCutEvents(app)
	if _, err := app.ExecutePlan(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if *starts != 1 || *dones != 1 {
		t.Errorf("got %d start and %d done events, want 1 of each", *starts, *dones)
	}
}

func TestPlanWithNoModelOpen(t *testing.T) {
	app := NewApp()
	if _, err := app.AddPlane(leftArmOnly(), cut.PinSpec{}); err == nil {
		t.Error("adding a plane with nothing open should be an error")
	}
	if _, err := app.ExecutePlan(); err == nil {
		t.Error("executing with nothing open should be an error")
	}
}

// --- helpers ---

// countCutEvents redirects the app's event sink and returns counters for the pair that
// brackets a run, so a caller can assert one pair however many cuts happen inside.
func countCutEvents(app *App) (starts, dones *int) {
	starts, dones = new(int), new(int)
	app.ctx = context.Background()
	app.emitFunc = func(_ context.Context, name string, _ ...interface{}) {
		switch name {
		case "cut:start":
			*starts++
		case "cut:done":
			*dones++
		}
	}
	return starts, dones
}

func volumes(tv *TreeView) []float64 {
	var out []float64
	var walk func(*Part)
	walk = func(p *Part) {
		if p == nil {
			return
		}
		if len(p.Children) == 0 {
			out = append(out, p.Volume)
			return
		}
		for _, c := range p.Children {
			walk(c)
		}
	}
	walk(tv.Root)
	return out
}

func sizes(tv *TreeView) [][3]float64 {
	var out [][3]float64
	var walk func(*Part)
	walk = func(p *Part) {
		if p == nil {
			return
		}
		if len(p.Children) == 0 {
			out = append(out, p.Size)
			return
		}
		for _, c := range p.Children {
			walk(c)
		}
	}
	walk(tv.Root)
	return out
}

func hasVolume(tv *TreeView, want float64) bool {
	for _, v := range volumes(tv) {
		if math.Abs(v-want) < 0.5 {
			return true
		}
	}
	return false
}

// The acceptance test both earlier attempts failed. Every piece must end up within the bed
// and none may be a sliver.
//
// The first attempt dropped fragments BestCut declined, because it returns false both for a
// part that already fits and for one it could not cut. The second bounded every rectangle
// tightly enough to spare siblings during replay, and cuts then grazed their own edges into
// 0.05mm slivers. Naming each cut's fragment is what makes a generous rectangle safe.
func TestPlanFitToPrinterBringsEveryFragmentWithinTheBed(t *testing.T) {
	cases := map[string]*stl.Mesh{
		"u":         fixtures.UShape(10),
		"cube":      fixtures.Cube(100),
		"sphere":    fixtures.UVSphere(40, 24, 12),
		"tube":      fixtures.Tube(30, 18, 90, 32),
		"hollowbox": fixtures.HollowBox(geom.Vec3{90, 90, 90}, 5),
	}
	for name, m := range cases {
		for _, bed := range []float64{60, 35} {
			app := NewApp()
			if _, err := app.loadPath(writeFixture(t, name+".stl", m), false); err != nil {
				t.Fatalf("%s: load: %v", name, err)
			}
			pv, err := app.PlanFitToPrinter(cut.Bed{X: bed, Y: bed, Z: bed})
			if err != nil {
				t.Fatalf("%s bed %v: %v", name, bed, err)
			}
			if pv.StillTooBig > 0 {
				t.Errorf("%s bed %v: %d fragment(s) reported as still too big", name, bed, pv.StillTooBig)
			}
			if len(pv.Cuts) > 0 {
				if _, err := app.ExecutePlan(); err != nil {
					t.Fatalf("%s bed %v: execute: %v", name, bed, err)
				}
			}

			for _, size := range sizes(app.view()) {
				biggest := 0.0
				for _, d := range size {
					if d > bed+0.001 {
						t.Errorf("%s bed %v: a piece measures %v, above the bed", name, bed, size)
					}
					if d > biggest {
						biggest = d
					}
				}
				// A sliver: something with an extent thousands of times smaller than its
				// longest. The tightly-bounded attempt produced 0.05mm pieces of a 90mm
				// shell, which is what this catches.
				for _, d := range size {
					if biggest > 0 && d > 0 && d < biggest/500 {
						t.Errorf("%s bed %v: a piece measures %v, which is a sliver", name, bed, size)
					}
				}
			}
		}
	}
}

// Each planned cut must name the fragment it belongs to, which is what makes replay match
// the simulation. The first entry names the root; later ones name pieces earlier cuts make.
func TestPlanFitToPrinterNamesTheFragmentEachCutBelongsTo(t *testing.T) {
	app := loadU(t)
	if _, err := app.ScaleModel([3]float64{20, 20, 20}); err != nil {
		t.Fatalf("ScaleModel: %v", err)
	}
	pv, err := app.PlanFitToPrinter(cut.Bed{X: 200, Y: 200, Z: 200})
	if err != nil {
		t.Fatalf("PlanFitToPrinter: %v", err)
	}
	if len(pv.Cuts) < 2 {
		t.Fatalf("got %d cuts, want at least 2", len(pv.Cuts))
	}
	if pv.Cuts[0].Target != "whole" {
		t.Errorf("the first cut targets %q, want the root %q", pv.Cuts[0].Target, "whole")
	}
	// Every target must be the root or a name an earlier cut produces, or replay cannot
	// find it.
	available := map[string]bool{"whole": true}
	for i, c := range pv.Cuts {
		if !available[c.Target] {
			t.Errorf("cut %d targets %q, which no earlier cut produces", i, c.Target)
		}
		available[c.Target+"a"] = true
		available[c.Target+"b"] = true
	}
}

// The reported problem, through the app: a plane through a wide part when a narrow one is
// available. The ends are unequal so the bounding box midpoint falls inside a fat end.
func TestPlanFitToPrinterCutsTheNarrowPlace(t *testing.T) {
	a := fixtures.Box(geom.Vec3{0, 0, 0}, geom.Vec3{60, 50, 50})
	bar := fixtures.Box(geom.Vec3{60, 22, 22}, geom.Vec3{100, 28, 28})
	b := fixtures.Box(geom.Vec3{100, 0, 0}, geom.Vec3{160, 50, 50})
	m := &stl.Mesh{Tris: append(append(append([]stl.Tri{}, a.Tris...), bar.Tris...), b.Tris...)}

	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "barbell.stl", m), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	pv, err := app.PlanFitToPrinter(cut.Bed{X: 110, Y: 100, Z: 100})
	if err != nil {
		t.Fatalf("PlanFitToPrinter: %v", err)
	}
	if len(pv.Cuts) != 1 {
		t.Fatalf("got %d cuts, want 1", len(pv.Cuts))
	}
	if x := pv.Cuts[0].Plane.Origin[0]; x < 60 || x > 100 {
		t.Errorf("the cut is at x=%v, outside the thin bar at 60..100", x)
	}
	if pv.Crowded != 0 {
		t.Errorf("Crowded = %d; cutting the bar crosses the model in one place", pv.Crowded)
	}
}
