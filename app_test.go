package main

import (
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
