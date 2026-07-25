package main

import (
	"math"
	"os"
	"path/filepath"
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
