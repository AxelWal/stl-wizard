package main

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"stl-cutter/internal/fixtures"
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

	view, err := app.loadPath(path)
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

func TestLoadPathReportsAnUnreadableFile(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath("/nonexistent/nope.stl"); err == nil {
		t.Error("expected an error for a missing file")
	}
}

func TestLoadPathReportsAFileThatIsNotSTL(t *testing.T) {
	app := NewApp()
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := writeBytes(path, []byte("this is not an STL file at all")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := app.loadPath(path); err == nil {
		t.Error("expected an error for a non-STL file")
	}
}

// Loading a second model must not leave the first one's parts reachable.
func TestLoadPathReplacesTheOpenModel(t *testing.T) {
	app := NewApp()
	first, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10)))
	if err != nil {
		t.Fatalf("first load: %v", err)
	}

	if _, err := app.loadPath(writeFixture(t, "tube.stl", fixtures.Tube(4, 2, 8, 24))); err != nil {
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
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10))); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID

	out, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{5, 5, 5},
		Normal: [3]float64{0, 0, 1},
		Width:  100,
		Height: 100,
	})
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

// The bounded rectangle is the whole point: cutting the U's left arm must leave
// the right arm attached.
func TestCutBoundedToOneArmOfTheU(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "u.stl", fixtures.UShape(10))); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID

	out, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{7.5, 25, 5},
		Normal: [3]float64{0, 1, 0},
		Width:  15,
		Height: 20,
	})
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
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10))); err != nil {
		t.Fatalf("load: %v", err)
	}
	_, err := app.Cut("nope", PlaneInput{
		Origin: [3]float64{5, 5, 5}, Normal: [3]float64{0, 0, 1}, Width: 100, Height: 100,
	})
	if err == nil {
		t.Error("expected an error for an unknown part id")
	}
}

func TestCutRejectsAPlaneThatMissesTheModel(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10))); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID

	_, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{5, 5, 500}, Normal: [3]float64{0, 0, 1}, Width: 100, Height: 100,
	})
	if err == nil {
		t.Error("expected an error when the cutter encloses nothing")
	}
}

func TestCutRejectsANonFinitePlane(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10))); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID

	_, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{5, 5, 5}, Normal: [3]float64{0, 0, 1},
		Width:  math.NaN(),
		Height: 100,
	})
	if err == nil {
		t.Error("expected an error for a non-finite extent")
	}
}

// Cutting a part that has already been split must return an error, not panic.
// A split part has no mesh of its own, so without the guard the geometry library
// would be handed a nil mesh.
func TestCutRefusesAPartThatWasAlreadySplit(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10))); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID

	plane := PlaneInput{
		Origin: [3]float64{5, 5, 5}, Normal: [3]float64{0, 0, 1}, Width: 100, Height: 100,
	}
	if _, err := app.Cut(id, plane); err != nil {
		t.Fatalf("first Cut: %v", err)
	}
	if _, err := app.Cut(id, plane); err == nil {
		t.Error("expected an error cutting a part that has already been split")
	}
}

func TestCutWithNoModelOpen(t *testing.T) {
	app := NewApp()
	_, err := app.Cut("p1", PlaneInput{
		Origin: [3]float64{0, 0, 0}, Normal: [3]float64{0, 0, 1}, Width: 10, Height: 10,
	})
	if err == nil {
		t.Error("expected an error when no model is open")
	}
}

// The frontend's copy must share no memory with the live tree, because Wails
// marshals it after the lock is released while another call may be mutating.
func TestViewReturnsADeepCopy(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10))); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID

	before := app.view()
	if _, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{5, 5, 5}, Normal: [3]float64{0, 0, 1}, Width: 100, Height: 100,
	}); err != nil {
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
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10))); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID
	if _, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{5, 5, 5}, Normal: [3]float64{0, 0, 1}, Width: 100, Height: 100,
	}); err != nil {
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
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10))); err != nil {
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
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10))); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID
	out, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{5, 5, 5}, Normal: [3]float64{0, 0, 1}, Width: 100, Height: 100,
	})
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
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10))); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID
	if _, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{5, 5, 5}, Normal: [3]float64{0, 0, 1}, Width: 100, Height: 100,
	}); err != nil {
		t.Fatalf("Cut: %v", err)
	}

	if _, err := app.Select(id); err == nil {
		t.Error("expected an error selecting a part that has been split")
	}
}

func TestSelectRefusesAnUnknownPart(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10))); err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := app.Select("nope"); err == nil {
		t.Error("expected an error for an unknown part id")
	}
}

func TestExportToWritesEveryLeaf(t *testing.T) {
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10))); err != nil {
		t.Fatalf("load: %v", err)
	}
	id := app.session.Tree().Root.ID
	if _, err := app.Cut(id, PlaneInput{
		Origin: [3]float64{5, 5, 5}, Normal: [3]float64{0, 0, 1}, Width: 100, Height: 100,
	}); err != nil {
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
	if _, err := app.loadPath(writeFixture(t, "bracket.stl", fixtures.Cube(10))); err != nil {
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
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10))); err != nil {
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
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(10))); err != nil {
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
