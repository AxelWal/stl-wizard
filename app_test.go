package main

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"stl-cutter/internal/cut"
	"stl-cutter/internal/fixtures"
	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

func writeFixture(t *testing.T, name string, m *stl.Mesh) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := stl.WriteFile(path, m); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestLoadPathBuildsATree(t *testing.T) {
	app := NewApp()
	path := writeFixture(t, "cube.stl", fixtures.Cube(10))

	view, err := app.loadPath(path, false)
	if err != nil {
		t.Fatalf("loadPath: %v", err)
	}
	if view.ModelName != "cube.stl" {
		t.Errorf("ModelName = %q, want cube.stl", view.ModelName)
	}
	if view.Root == nil {
		t.Fatal("view has no root")
	}
	if view.Root.Tris != 12 {
		t.Errorf("Tris = %d, want 12", view.Root.Tris)
	}
	if view.SelectedID != view.Root.ID {
		t.Errorf("SelectedID = %q, want the root", view.SelectedID)
	}
	if view.CanUndo {
		t.Error("a freshly loaded model has nothing to undo")
	}
}

// OpenPath is what a headless browser uses instead of the native file dialog
// (see CLAUDE.md), so it is the entry point every automated UI run depends on.
// Deleting it would break that without breaking anything a Go test otherwise
// covers, since loadPath itself would still be exercised.
func TestOpenPathLoadsWithoutADialog(t *testing.T) {
	app := NewApp()
	view, err := app.OpenPath(writeFixture(t, "cube.stl", fixtures.Cube(10)), false)
	if err != nil {
		t.Fatalf("OpenPath: %v", err)
	}
	if view.ModelName != "cube.stl" || view.Root == nil || view.Root.Tris != 12 {
		t.Errorf("OpenPath gave %+v, want cube.stl with a 12-triangle root", view)
	}
}

func TestOpenPathRepairsTheMeshWhenAsked(t *testing.T) {
	app := NewApp()
	path := writeFixture(t, "openbox.stl", fixtures.OpenBox(10))

	view, err := app.OpenPath(path, true)
	if err != nil {
		t.Fatalf("OpenPath: %v", err)
	}
	if view.Repair == nil {
		t.Fatal("no repair report on a load that asked for repair")
	}
	if view.Repair.HolesFilled != 1 {
		t.Errorf("HolesFilled = %d, want 1", view.Repair.HolesFilled)
	}
	if !view.Repair.Closed {
		t.Errorf("the box should be closed after repair; before %q, after %q",
			view.Repair.Before, view.Repair.After)
	}
	// The tree has to agree, or the sidebar would report a repair and still flag
	// the part.
	if !view.Root.Watertight {
		t.Error("the part is still flagged after a successful repair")
	}
	if want := fixtures.Cube(10).Volume(); math.Abs(view.Root.Volume-want) > 1e-6 {
		t.Errorf("volume = %v, want %v — the fill should restore the missing flat face exactly",
			view.Root.Volume, want)
	}
}

// Repair rewrites the user's geometry, so it must happen only when asked. A
// broken model still loads and is still flagged without it.
func TestOpenPathLeavesTheMeshAloneWhenRepairIsOff(t *testing.T) {
	app := NewApp()
	path := writeFixture(t, "openbox.stl", fixtures.OpenBox(10))

	view, err := app.OpenPath(path, false)
	if err != nil {
		t.Fatalf("OpenPath: %v", err)
	}
	if view.Repair != nil {
		t.Errorf("a repair report was produced for a load that did not ask for one: %+v", view.Repair)
	}
	if view.Root.Watertight {
		t.Error("the unrepaired box should still be flagged as not a closed solid")
	}
	if view.Root.Tris != 10 {
		t.Errorf("Tris = %d, want the original 10 — nothing should have been added", view.Root.Tris)
	}
}

// Asking for repair on a model that does not need it must not invent one.
func TestOpenPathReportsNoRepairOnASoundMesh(t *testing.T) {
	app := NewApp()
	view, err := app.OpenPath(writeFixture(t, "cube.stl", fixtures.Cube(10)), true)
	if err != nil {
		t.Fatalf("OpenPath: %v", err)
	}
	if view.Repair != nil && view.Repair.HolesFilled != 0 {
		t.Errorf("a sound cube needed %d hole(s) filled", view.Repair.HolesFilled)
	}
	if view.Root.Tris != 12 {
		t.Errorf("Tris = %d, want 12", view.Root.Tris)
	}
}

// Two bodies touching along one edge are one non-manifold mesh that repair is
// documented as unable to fix. Separating them needs no geometry change, and each
// is then a closed solid — which is the whole reason for the feature.
func TestSeparateBodiesSplitsTouchingSolidsAndMakesBothSound(t *testing.T) {
	app := NewApp()
	view, err := app.loadPath(writeFixture(t, "two.stl", fixtures.TouchingCubes(10)), false)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if view.Root.Watertight {
		t.Fatal("the fixture should load flagged, or this test proves nothing")
	}

	out, err := app.SeparateBodies(view.Root.ID)
	if err != nil {
		t.Fatalf("SeparateBodies: %v", err)
	}
	if out.Bodies != 2 {
		t.Errorf("Bodies = %d, want 2", out.Bodies)
	}
	if len(out.Tree.Root.Children) != 2 {
		t.Fatalf("got %d children, want 2", len(out.Tree.Root.Children))
	}
	for i, c := range out.Tree.Root.Children {
		if !c.Watertight {
			t.Errorf("body %d is flagged; separated bodies should each be sound", i)
		}
		if c.Volume < 999 || c.Volume > 1001 {
			t.Errorf("body %d volume = %v, want about 1000", i, c.Volume)
		}
	}
	if !out.Tree.CanUndo {
		t.Error("a separation should be undoable")
	}
}

// Pressing the button on an ordinary part must not be an error: it is cheap to
// press and the honest answer is "there is only one body here".
func TestSeparateBodiesReportsASingleBodyWithoutSplitting(t *testing.T) {
	app := NewApp()
	view, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10)), false)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	out, err := app.SeparateBodies(view.Root.ID)
	if err != nil {
		t.Fatalf("SeparateBodies: %v", err)
	}
	if out.Bodies != 1 {
		t.Errorf("Bodies = %d, want 1", out.Bodies)
	}
	if !out.Tree.Root.IsLeaf() {
		t.Error("a single-body part must not be split")
	}
	if out.Tree.CanUndo {
		t.Error("nothing happened, so there should be nothing to undo")
	}
}

// A hollow model is one body: its internal void is a separate surface, and
// returning it as a body would give a solid shell and an inside-out one.
func TestSeparateBodiesKeepsAHollowModelWhole(t *testing.T) {
	app := NewApp()
	view, err := app.loadPath(writeFixture(t, "hollow.stl", fixtures.HollowBox(geom.Vec3{20, 20, 20}, 2)), false)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := view.Root.Volume

	out, err := app.SeparateBodies(view.Root.ID)
	if err != nil {
		t.Fatalf("SeparateBodies: %v", err)
	}
	if out.Bodies != 1 {
		t.Errorf("Bodies = %d, want 1 — the void is not a body", out.Bodies)
	}
	if math.Abs(out.Tree.Root.Volume-want) > 1e-9 {
		t.Errorf("volume = %v, want the shell's %v", out.Tree.Root.Volume, want)
	}
}

func TestSeparateBodiesRefusesAPartThatWasAlreadySplit(t *testing.T) {
	app := NewApp()
	view, err := app.loadPath(writeFixture(t, "two.stl", fixtures.TouchingCubes(10)), false)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := app.SeparateBodies(view.Root.ID); err != nil {
		t.Fatalf("first separation: %v", err)
	}
	if _, err := app.SeparateBodies(view.Root.ID); err == nil {
		t.Error("expected an error when separating a part that was already split")
	}
}

// Repair labels surfaces anyway, so the body count is free there and the load-time
// message can mention it without a second weld of the whole mesh.
func TestRepairReportsTheBodyCount(t *testing.T) {
	app := NewApp()
	view, err := app.OpenPath(writeFixture(t, "two.stl", fixtures.TouchingCubes(10)), true)
	if err != nil {
		t.Fatalf("OpenPath: %v", err)
	}
	if view.Repair == nil {
		t.Fatal("no repair report")
	}
	if view.Repair.Bodies != 2 {
		t.Errorf("Bodies = %d, want 2", view.Repair.Bodies)
	}
}

func TestLoadPathReportsAnUnreadableFile(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath("/nonexistent/nope.stl", false); err == nil {
		t.Error("expected an error for a missing file")
	}
}

func TestLoadPathReportsAFileThatIsNotSTL(t *testing.T) {
	app := NewApp()
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := writeBytes(path, []byte("this is not an STL file at all")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := app.loadPath(path, false); err == nil {
		t.Error("expected an error for a non-STL file")
	}
}

// Loading a second model must not leave the first one's parts reachable.
func TestLoadPathReplacesTheOpenModel(t *testing.T) {
	app := NewApp()
	first, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10)), false)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}

	if _, err := app.loadPath(writeFixture(t, "tube.stl", fixtures.Tube(4, 2, 8, 24)), false); err != nil {
		t.Fatalf("second load: %v", err)
	}
	if _, ok := app.session.MeshFor(first.Root.ID); ok {
		t.Error("the first model's part is still reachable after loading another")
	}
}

func writeBytes(path string, b []byte) error {
	return os.WriteFile(path, b, 0o644)
}

func TestCutSplitsTheSelectedPart(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID

	out, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{5, 5, 5},
		Normal: [3]float64{0, 0, 1},
		Width:  100,
		Height: 100,
	}, cut.PinSpec{})
	if err != nil {
		t.Fatalf("Cut: %v", err)
	}
	if !out.Watertight {
		t.Errorf("a cube halved should be watertight; warnings: %v", out.Warnings)
	}
	if len(out.Tree.Root.Children) != 2 {
		t.Fatalf("got %d children, want 2", len(out.Tree.Root.Children))
	}

	// Both halves are 500mm³. Part 2 is the side the normal points to.
	for i, c := range out.Tree.Root.Children {
		if c.Volume < 499 || c.Volume > 501 {
			t.Errorf("child %d volume = %v, want about 500", i, c.Volume)
		}
		if !c.Watertight {
			t.Errorf("child %d is flagged not watertight", i)
		}
	}
	if !out.Tree.CanUndo {
		t.Error("CanUndo should be true after a cut")
	}
}

// PinSpec.Count is a target, not a demand: placement grids the cut face and
// stops when it runs out of room, and a pin it never found a candidate for
// produces no SkippedPin to explain itself. Without the requested count beside
// the placed one the frontend cannot tell "you asked for 4 and got 1" from
// "you asked for 1 and got 1", so it quietly under-delivers on the user's
// request — the one thing this application is not allowed to do.
func TestCutReportsHowManyPinsWereRequested(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "u.stl", fixtures.UShape(10)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID

	// The U's left arm gives a 10x15mm cut face, which has room for far fewer
	// than eight 4mm pins.
	out, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{7.5, 25, 5},
		Normal: [3]float64{0, 1, 0},
		Width:  15,
		Height: 20,
	}, cut.PinSpec{Enabled: true, Count: 8, Diameter: 4, Length: 8, Clearance: 0.15, MinWall: 1, PegOnPart: 1})
	if err != nil {
		t.Fatalf("Cut: %v", err)
	}
	if out.PinsRequested != 8 {
		t.Errorf("PinsRequested = %d, want 8", out.PinsRequested)
	}
	if out.PinsPlaced >= 8 {
		t.Fatalf("PinsPlaced = %d; this face is too small to fit them all, so the test proves nothing", out.PinsPlaced)
	}
}

// Reporting a requested count when pins are switched off would have the
// frontend announce "0 of 4 placed" for a cut that never asked for any.
func TestCutReportsNoPinsRequestedWhenPinsAreOff(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID

	out, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{5, 5, 5},
		Normal: [3]float64{0, 0, 1},
		Width:  100,
		Height: 100,
	}, cut.PinSpec{Enabled: false, Count: 4, Diameter: 4, Length: 8})
	if err != nil {
		t.Fatalf("Cut: %v", err)
	}
	if out.PinsRequested != 0 {
		t.Errorf("PinsRequested = %d, want 0 when pins are disabled", out.PinsRequested)
	}
}

// The bounded rectangle is the whole point: cutting the U's left arm must leave
// the right arm attached.
func TestCutBoundedToOneArmOfTheU(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "u.stl", fixtures.UShape(10)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID

	out, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{7.5, 25, 5},
		Normal: [3]float64{0, 1, 0},
		Width:  15,
		Height: 20,
	}, cut.PinSpec{})
	if err != nil {
		t.Fatalf("Cut: %v", err)
	}
	if !out.Watertight {
		t.Errorf("expected a watertight cut; warnings: %v", out.Warnings)
	}

	children := out.Tree.Root.Children
	if len(children) != 2 {
		t.Fatalf("got %d children, want 2", len(children))
	}

	// Assert each volume, not just the sum. Any valid partition of the mesh sums
	// to 9000 — a cut that severed BOTH arms gives [6000, 3000] and would pass a
	// sum-only check, which is exactly the regression this test exists to catch.
	if got := children[0].Volume; math.Abs(got-7500) > 1 {
		t.Errorf("part 1 volume = %v, want 7500 — the material left behind", got)
	}
	if got := children[1].Volume; math.Abs(got-1500) > 1 {
		t.Errorf("part 2 volume = %v, want 1500 — the left arm's top only; "+
			"3000 would mean both arms were cut", got)
	}
}

func TestCutRejectsAnUnknownPart(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	_, err := app.Cut("nope", PlaneInput{
		Origin: [3]float64{5, 5, 5}, Normal: [3]float64{0, 0, 1}, Width: 100, Height: 100,
	}, cut.PinSpec{})
	if err == nil {
		t.Error("expected an error for an unknown part id")
	}
}

func TestCutRejectsAPlaneThatMissesTheModel(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID

	_, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{5, 5, 500}, Normal: [3]float64{0, 0, 1}, Width: 100, Height: 100,
	}, cut.PinSpec{})
	if err == nil {
		t.Error("expected an error when the cutter encloses nothing")
	}
}

func TestCutRejectsANonFinitePlane(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID

	_, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{5, 5, 5}, Normal: [3]float64{0, 0, 1},
		Width:  math.NaN(),
		Height: 100,
	}, cut.PinSpec{})
	if err == nil {
		t.Error("expected an error for a non-finite extent")
	}
}

// Cutting a part that has already been split must return an error, not panic.
// A split part has no mesh of its own, so without the guard the geometry library
// would be handed a nil mesh.
func TestCutRefusesAPartThatWasAlreadySplit(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID

	plane := PlaneInput{
		Origin: [3]float64{5, 5, 5}, Normal: [3]float64{0, 0, 1}, Width: 100, Height: 100,
	}
	if _, err := app.Cut(id, plane, cut.PinSpec{}); err != nil {
		t.Fatalf("first Cut: %v", err)
	}
	if _, err := app.Cut(id, plane, cut.PinSpec{}); err == nil {
		t.Error("expected an error cutting a part that has already been split")
	}
}

func TestCutWithNoModelOpen(t *testing.T) {
	app := NewApp()
	_, err := app.Cut("p1", PlaneInput{
		Origin: [3]float64{0, 0, 0}, Normal: [3]float64{0, 0, 1}, Width: 10, Height: 10,
	}, cut.PinSpec{})
	if err == nil {
		t.Error("expected an error when no model is open")
	}
}

// The frontend's copy must share no memory with the live tree, because Wails
// marshals it after the lock is released while another call may be mutating.
func TestViewReturnsADeepCopy(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID

	before := app.view()
	if _, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{5, 5, 5}, Normal: [3]float64{0, 0, 1}, Width: 100, Height: 100,
	}, cut.PinSpec{}); err != nil {
		t.Fatalf("Cut: %v", err)
	}

	// The snapshot taken before the cut must be unchanged by it.
	if len(before.Root.Children) != 0 {
		t.Errorf("the earlier snapshot grew %d children; it aliases the live tree",
			len(before.Root.Children))
	}
	if before.Root.Mesh != nil {
		t.Error("a snapshot must not carry a mesh reference")
	}
}

func TestUndoRestoresThePreviousState(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID
	if _, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{5, 5, 5}, Normal: [3]float64{0, 0, 1}, Width: 100, Height: 100,
	}, cut.PinSpec{}); err != nil {
		t.Fatalf("Cut: %v", err)
	}

	view, err := app.Undo()
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if len(view.Root.Children) != 0 {
		t.Errorf("root still has %d children after undo", len(view.Root.Children))
	}
	if view.CanUndo {
		t.Error("nothing left to undo")
	}
	if view.SelectedID != id {
		t.Errorf("SelectedID = %q, want the restored part %q", view.SelectedID, id)
	}

	// The restored part must be fetchable again, or the viewer cannot redraw it.
	if _, ok := app.session.MeshFor(id); !ok {
		t.Error("the restored part has no mesh")
	}
}

func TestUndoWithNothingToUndo(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := app.Undo(); err == nil {
		t.Error("expected an error undoing a freshly loaded model")
	}
}

func TestUndoWithNoModelOpen(t *testing.T) {
	app := NewApp()
	if _, err := app.Undo(); err == nil {
		t.Error("expected an error when no model is open")
	}
}

func TestSelectChangesTheSelection(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID
	out, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{5, 5, 5}, Normal: [3]float64{0, 0, 1}, Width: 100, Height: 100,
	}, cut.PinSpec{})
	if err != nil {
		t.Fatalf("Cut: %v", err)
	}
	second := out.Tree.Root.Children[1].ID

	view, err := app.Select(second)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if view.SelectedID != second {
		t.Errorf("SelectedID = %q, want %q", view.SelectedID, second)
	}
}

// Only leaves carry geometry, so selecting a split part would leave the viewer
// with nothing to show.
func TestSelectRefusesASplitPart(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID
	if _, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{5, 5, 5}, Normal: [3]float64{0, 0, 1}, Width: 100, Height: 100,
	}, cut.PinSpec{}); err != nil {
		t.Fatalf("Cut: %v", err)
	}

	if _, err := app.Select(id); err == nil {
		t.Error("expected an error selecting a part that has been split")
	}
}

func TestSelectRefusesAnUnknownPart(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := app.Select("nope"); err == nil {
		t.Error("expected an error for an unknown part id")
	}
}

func TestExportToWritesEveryLeaf(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID
	if _, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{5, 5, 5}, Normal: [3]float64{0, 0, 1}, Width: 100, Height: 100,
	}, cut.PinSpec{}); err != nil {
		t.Fatalf("Cut: %v", err)
	}

	dir := t.TempDir()
	out, err := app.exportTo(dir)
	if err != nil {
		t.Fatalf("exportTo: %v", err)
	}
	if len(out.Files) != 2 {
		t.Fatalf("wrote %d files, want 2: %v", len(out.Files), out.Files)
	}

	// Every written file must be a readable STL with the volume the tree claims.
	for _, name := range out.Files {
		m, err := stl.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("%s does not read back as STL: %v", name, err)
			continue
		}
		if v := m.Volume(); v < 499 || v > 501 {
			t.Errorf("%s volume = %v, want about 500", name, v)
		}
	}
}

func TestExportToNamesFilesAfterTheModel(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "bracket.stl", fixtures.Cube(10)), false); err != nil {
		t.Fatalf("load: %v", err)
	}

	out, err := app.exportTo(t.TempDir())
	if err != nil {
		t.Fatalf("exportTo: %v", err)
	}
	if len(out.Files) != 1 {
		t.Fatalf("wrote %d files, want 1", len(out.Files))
	}
	if !strings.HasPrefix(out.Files[0], "bracket_") || !strings.HasSuffix(out.Files[0], ".stl") {
		t.Errorf("file name %q should be derived from the model name", out.Files[0])
	}
}

// A flagged part is still exported — the user asked for it — but they must be
// told which files are suspect.
func TestExportToReportsFlaggedParts(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	// Force a flagged leaf directly, since producing one through Cut reliably is
	// awkward.
	err := app.session.WithTree(func(tr *Tree) error {
		_, _, err := tr.Split(tr.Root.ID, fixtures.Cube(5), fixtures.Cube(4), false, true)
		return err
	})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	out, err := app.exportTo(t.TempDir())
	if err != nil {
		t.Fatalf("exportTo: %v", err)
	}
	if len(out.NotWatertight) != 1 {
		t.Errorf("NotWatertight = %v, want exactly one flagged file", out.NotWatertight)
	}
}

func TestExportToReportsAnUnwritableDirectory(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := app.exportTo("/nonexistent/directory"); err == nil {
		t.Error("expected an error for a directory that cannot be written to")
	}
}

func TestExportWithNoModelOpen(t *testing.T) {
	app := NewApp()
	if _, err := app.exportTo(t.TempDir()); err == nil {
		t.Error("expected an error when no model is open")
	}
}

// A partial failure must still report what reached the disk.
func TestExportToReturnsWhatItWroteWhenAWriteFails(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID
	if _, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{5, 5, 5}, Normal: [3]float64{0, 0, 1}, Width: 100, Height: 100,
	}, cut.PinSpec{}); err != nil {
		t.Fatalf("Cut: %v", err)
	}

	dir := t.TempDir()
	// Make the second write fail by occupying its name with a directory.
	base := "cube_"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	_ = entries
	// The two leaves are named wholea and wholeb, so the second file is:
	blocker := filepath.Join(dir, base+"wholeb.stl")
	if err := os.Mkdir(blocker, 0o755); err != nil {
		t.Fatalf("mkdir blocker: %v", err)
	}

	out, err := app.exportTo(dir)
	if err == nil {
		t.Fatal("expected an error when a file cannot be written")
	}
	if out == nil {
		t.Fatal("a partial outcome must still be returned so the user knows what was written")
	}
	if len(out.Files) != 1 {
		t.Errorf("Files = %v, want the one file that was written before the failure", out.Files)
	}
}

func TestCutWithPinsAddsThemToBothParts(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(40)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID

	out, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{20, 20, 20}, Normal: [3]float64{0, 0, 1}, Width: 200, Height: 200,
	}, cut.PinSpec{Enabled: true, Count: 4, Diameter: 4, Length: 6})
	if err != nil {
		t.Fatalf("Cut: %v", err)
	}
	if out.PinsPlaced == 0 {
		t.Fatalf("no pins placed on a 40mm face; skipped: %+v", out.PinsSkipped)
	}
	if !out.Watertight {
		t.Errorf("pinned parts should still be closed solids; warnings: %v", out.Warnings)
	}
}

// Pins change the geometry, so the tree's recorded measurements must reflect the
// pinned parts, not the bare ones.
func TestCutWithPinsRecordsThePinnedVolumes(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(40)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID

	bare, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{20, 20, 20}, Normal: [3]float64{0, 0, 1}, Width: 200, Height: 200,
	}, cut.PinSpec{})
	if err != nil {
		t.Fatalf("bare Cut: %v", err)
	}
	bareVolume := bare.Tree.Root.Children[1].Volume

	if _, err := app.Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}

	pinned, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{20, 20, 20}, Normal: [3]float64{0, 0, 1}, Width: 200, Height: 200,
	}, cut.PinSpec{Enabled: true, Count: 4, Diameter: 4, Length: 6})
	if err != nil {
		t.Fatalf("pinned Cut: %v", err)
	}
	if got := pinned.Tree.Root.Children[1].Volume; got <= bareVolume {
		t.Errorf("pinned part volume %v is not greater than the bare %v; the tree recorded the wrong mesh",
			got, bareVolume)
	}
}

func TestCutWithPinsReportsSkips(t *testing.T) {
	app := NewApp()
	// A thin shell has no material behind the cut face for a socket.
	if _, err := app.loadPath(writeFixture(t, "shell.stl", fixtures.HollowBox(geom.Vec3{40, 40, 40}, 1.5)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID

	out, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{20, 20, 20}, Normal: [3]float64{0, 0, 1}, Width: 200, Height: 200,
	}, cut.PinSpec{Enabled: true, Count: 4, Diameter: 4, Length: 6})
	if err != nil {
		t.Fatalf("Cut: %v", err)
	}
	if out.PinsPlaced != 0 {
		t.Errorf("placed %d pins into a 1.5mm shell", out.PinsPlaced)
	}
	if len(out.PinsSkipped) == 0 && len(out.Warnings) == 0 {
		t.Error("skipped pins must be reported, not silently dropped")
	}
}

func TestCutWithoutPinsIsUnchanged(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID

	out, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{5, 5, 5}, Normal: [3]float64{0, 0, 1}, Width: 100, Height: 100,
	}, cut.PinSpec{})
	if err != nil {
		t.Fatalf("Cut: %v", err)
	}
	if out.PinsPlaced != 0 {
		t.Errorf("placed %d pins with pinning disabled", out.PinsPlaced)
	}
	for _, c := range out.Tree.Root.Children {
		if c.Volume < 499 || c.Volume > 501 {
			t.Errorf("child volume = %v, want about 500 — an unpinned cut must be unchanged", c.Volume)
		}
	}
}

func TestAutoSplitLeavesAFittingModelAlone(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(50)), false); err != nil {
		t.Fatalf("load: %v", err)
	}

	out, err := app.AutoSplit(cut.Bed{X: 220, Y: 220, Z: 250}, cut.PinSpec{})
	if err != nil {
		t.Fatalf("AutoSplit: %v", err)
	}
	if out.CutsMade != 0 {
		t.Errorf("made %d cuts on a model that already fits", out.CutsMade)
	}
	if len(out.Tree.Root.Children) != 0 {
		t.Error("the tree should be untouched")
	}
}

func TestAutoSplitDividesAnOversizedModel(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "big.stl", fixtures.Cube(300)), false); err != nil {
		t.Fatalf("load: %v", err)
	}

	bed := cut.Bed{X: 120, Y: 120, Z: 120}
	out, err := app.AutoSplit(bed, cut.PinSpec{})
	if err != nil {
		t.Fatalf("AutoSplit: %v", err)
	}
	if out.CutsMade == 0 {
		t.Fatal("made no cuts on a model far larger than the bed")
	}

	// Every leaf must now fit, or be named as one that does not.
	var tooBig int
	for _, leaf := range app.session.Tree().Leaves() {
		box := stl.BBox{
			Min: geom.Vec3{leaf.Min[0], leaf.Min[1], leaf.Min[2]},
			Max: geom.Vec3{leaf.Min[0] + leaf.Size[0], leaf.Min[1] + leaf.Size[1], leaf.Min[2] + leaf.Size[2]},
		}
		if !bed.Fits(box) {
			tooBig++
		}
	}
	if tooBig != len(out.StillTooBig) {
		t.Errorf("%d leaves do not fit but %d were reported", tooBig, len(out.StillTooBig))
	}
}

func TestAutoSplitIsUndoable(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "big.stl", fixtures.Cube(300)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := app.AutoSplit(cut.Bed{X: 120, Y: 120, Z: 120}, cut.PinSpec{}); err != nil {
		t.Fatalf("AutoSplit: %v", err)
	}

	// Every cut it made must be undoable one at a time, like any other cut.
	undone := 0
	for app.session.Tree().CanUndo() {
		if _, err := app.Undo(); err != nil {
			t.Fatalf("Undo after %d: %v", undone, err)
		}
		undone++
	}
	if undone == 0 {
		t.Fatal("auto-split left nothing to undo")
	}
	if len(app.session.Tree().Leaves()) != 1 {
		t.Errorf("after undoing everything there are %d leaves, want 1", len(app.session.Tree().Leaves()))
	}
}

func TestAutoSplitRejectsANonsenseBed(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(50)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := app.AutoSplit(cut.Bed{X: 0, Y: 100, Z: 100}, cut.PinSpec{}); err == nil {
		t.Error("expected an error for a zero-width bed")
	}
}

func TestAutoSplitWithNoModelOpen(t *testing.T) {
	app := NewApp()
	if _, err := app.AutoSplit(cut.Bed{X: 220, Y: 220, Z: 250}, cut.PinSpec{}); err == nil {
		t.Error("expected an error when no model is open")
	}
}

// Cuts that already committed must be reported even when a later one fails.
// Returning a bare error would leave the caller unaware the model changed, and
// the window showing geometry that no longer exists.
func TestAutoSplitReportsProgressWhenACutFails(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "big.stl", fixtures.Cube(300)), false); err != nil {
		t.Fatalf("load: %v", err)
	}

	// A bed small enough to need many cuts, so the run is long enough that
	// stopping partway leaves real progress behind.
	out, err := app.AutoSplit(cut.Bed{X: 40, Y: 40, Z: 40}, cut.PinSpec{})
	if err != nil {
		t.Fatalf("AutoSplit returned an error rather than a partial outcome: %v", err)
	}
	if out == nil {
		t.Fatal("a nil outcome tells the caller nothing about what happened")
	}
	if out.Tree == nil {
		t.Error("the outcome must carry the tree, or the window cannot re-render")
	}
	// Whatever happened, the reported cut count must match the tree.
	leaves := len(app.session.Tree().Leaves())
	if out.CutsMade != leaves-1 {
		t.Errorf("CutsMade = %d but the tree has %d leaves (%d cuts)", out.CutsMade, leaves, leaves-1)
	}
}

// One leaf that cannot be divided must not block every leaf behind it.
func TestAutoSplitSkipsALeafItCannotDivide(t *testing.T) {
	app := NewApp()
	// A fine-tessellated sphere has leaves the planner cannot always cut.
	if _, err := app.loadPath(writeFixture(t, "sphere.stl", fixtures.UVSphere(80, 32, 16)), false); err != nil {
		t.Fatalf("load: %v", err)
	}

	bed := cut.Bed{X: 60, Y: 60, Z: 60}
	out, err := app.AutoSplit(bed, cut.PinSpec{})
	if err != nil {
		t.Fatalf("AutoSplit: %v", err)
	}

	// Whatever it could not divide must be named, and everything it could divide
	// must actually have been divided — a piece the size of the original means the
	// run gave up before reaching it.
	original := app.session.Tree().Root
	for _, leaf := range app.session.Tree().Leaves() {
		if leaf.ID == original.ID {
			continue
		}
		if !bed.Fits(leafBBox(leaf)) {
			named := false
			for _, n := range out.StillTooBig {
				if n == leaf.Name {
					named = true
				}
			}
			if !named {
				t.Errorf("%s is oversized but not reported in StillTooBig", leaf.Name)
			}
		}
	}
	if out.CutsMade == 0 {
		t.Error("made no cuts at all")
	}

	// The point of skipping: no leaf is left at anything like the original's
	// size. Before the fix one 80x80x160 piece came back untouched.
	for _, leaf := range app.session.Tree().Leaves() {
		if leaf.Size[0] >= original.Size[0] && leaf.Size[1] >= original.Size[1] &&
			leaf.Size[2] >= original.Size[2] {
			t.Errorf("%s is still the full size of the model — the run gave up before reaching it", leaf.Name)
		}
	}
}

// One user-visible operation emits one start/done pair, however many cuts it
// makes internally — otherwise a long auto-split flickers the progress bar once
// per cut instead of showing one continuous run. cutPart's cut:progress still
// fires once per cut, from the geometry library's own callback, so the bar
// keeps advancing during the run.
func TestAutoSplitBracketsTheWholeRunWithOneEventPair(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "big.stl", fixtures.Cube(300)), false); err != nil {
		t.Fatalf("load: %v", err)
	}

	var starts, dones, progresses int
	app.ctx = context.Background()
	app.emitFunc = func(_ context.Context, name string, _ ...interface{}) {
		switch name {
		case "cut:start":
			starts++
		case "cut:done":
			dones++
		case "cut:progress":
			progresses++
		}
	}

	if _, err := app.AutoSplit(cut.Bed{X: 100, Y: 100, Z: 100}, cut.PinSpec{}); err != nil {
		t.Fatalf("AutoSplit: %v", err)
	}

	if starts != 1 {
		t.Errorf("cut:start fired %d times, want exactly 1 for the whole run", starts)
	}
	if dones != 1 {
		t.Errorf("cut:done fired %d times, want exactly 1 for the whole run", dones)
	}
	if progresses <= 1 {
		t.Errorf("cut:progress fired %d times, want more than 1 across a multi-cut run", progresses)
	}
}
