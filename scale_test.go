package main

import (
	"math"
	"testing"

	"stl-cutter/internal/cut"
	"stl-cutter/internal/fixtures"
)

func loadCube(t *testing.T, size float64) *App {
	t.Helper()
	app := NewApp()
	if _, err := app.loadPath(writeFixture(t, "cube.stl", fixtures.Cube(size)), false); err != nil {
		t.Fatalf("load: %v", err)
	}
	return app
}

func TestScaleModelChangesTheReportedSizeAndVolume(t *testing.T) {
	app := loadCube(t, 10)

	v, err := app.ScaleModel([3]float64{2, 3, 4})
	if err != nil {
		t.Fatalf("ScaleModel: %v", err)
	}
	if want := [3]float64{20, 30, 40}; v.Root.Size != want {
		t.Errorf("Size = %v, want %v", v.Root.Size, want)
	}
	if want := 1000.0 * 24; math.Abs(v.Root.Volume-want) > 1e-6 {
		t.Errorf("Volume = %v, want %v", v.Root.Volume, want)
	}
	if v.Scale != [3]float64{2, 3, 4} {
		t.Errorf("Scale = %v, want the factors that were asked for", v.Scale)
	}
	if want := [3]float64{10, 10, 10}; v.OriginalSize != want {
		t.Errorf("OriginalSize = %v, want the file's own %v", v.OriginalSize, want)
	}
	if !v.Root.Watertight {
		t.Error("a scaled cube should still be a closed solid")
	}
}

// Absolute, not relative. Typing 200% twice must give 200%, or the field disagrees with
// the model the moment you press Apply again.
func TestScaleModelIsIdempotent(t *testing.T) {
	app := loadCube(t, 10)
	first, err := app.ScaleModel([3]float64{2, 2, 2})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := app.ScaleModel([3]float64{2, 2, 2})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.Root.Size != second.Root.Size {
		t.Errorf("size went from %v to %v on applying the same factors again",
			first.Root.Size, second.Root.Size)
	}
	if math.Abs(first.Root.Volume-second.Root.Volume) > 1e-9 {
		t.Errorf("volume went from %v to %v", first.Root.Volume, second.Root.Volume)
	}
}

// Returning to 1 has to be exact, which is only true because scaling works from the mesh
// as loaded rather than dividing what it last produced.
func TestScaleModelReturnsExactlyToTheOriginal(t *testing.T) {
	app := loadCube(t, 10)
	want := app.view().Root.Volume

	if _, err := app.ScaleModel([3]float64{3, 7, 0.1}); err != nil {
		t.Fatalf("scale: %v", err)
	}
	v, err := app.ScaleModel([3]float64{1, 1, 1})
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if v.Root.Volume != want {
		t.Errorf("volume came back as %v, want exactly %v", v.Root.Volume, want)
	}
	if v.Root.Size != [3]float64{10, 10, 10} {
		t.Errorf("size came back as %v, want exactly 10x10x10", v.Root.Size)
	}
}

// A negative factor mirrors the model: consistently wound, negative volume, reads as
// sound and prints as nothing. Zero flattens it. Neither may be obliged.
func TestScaleModelRefusesNonsenseFactors(t *testing.T) {
	for _, f := range [][3]float64{
		{0, 1, 1},
		{-1, 1, 1},
		{1, 1, -2},
		{math.NaN(), 1, 1},
		{1, math.Inf(1), 1},
	} {
		app := loadCube(t, 10)
		before := app.view().Root.Volume
		if _, err := app.ScaleModel(f); err == nil {
			t.Errorf("scaling by %v should be refused", f)
		}
		if got := app.view().Root.Volume; got != before {
			t.Errorf("scaling by %v was refused but changed the volume from %v to %v", f, before, got)
		}
	}
}

// A plan aimed at coordinates that no longer describe the model is cleared, the same rule
// as loading a model.
func TestScaleModelClearsThePlan(t *testing.T) {
	app := loadU(t)
	if _, err := app.AddPlane(leftArmOnly(), cut.PinSpec{}); err != nil {
		t.Fatalf("AddPlane: %v", err)
	}
	if _, err := app.ScaleModel([3]float64{2, 2, 2}); err != nil {
		t.Fatalf("ScaleModel: %v", err)
	}
	if got := len(app.Plan().Cuts); got != 0 {
		t.Errorf("the plan still holds %d cut(s) aimed at the old size", got)
	}
}

func TestScaleModelResetsTheTree(t *testing.T) {
	app := loadU(t)
	if _, err := app.AddPlane(leftArmOnly(), cut.PinSpec{}); err != nil {
		t.Fatalf("AddPlane: %v", err)
	}
	if _, err := app.ExecutePlan(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if app.view().Root.IsLeaf() {
		t.Fatal("the model should be cut at this point")
	}

	v, err := app.ScaleModel([3]float64{2, 2, 2})
	if err != nil {
		t.Fatalf("ScaleModel: %v", err)
	}
	if !v.Root.IsLeaf() {
		t.Error("scaling should reset the tree to a single part")
	}
	if v.CanUndo {
		t.Error("a reset tree has nothing to undo")
	}
}

// Cutting a scaled model has to work on the scaled geometry, not the file's.
func TestCuttingAScaledModelUsesTheScaledGeometry(t *testing.T) {
	app := loadCube(t, 10)
	if _, err := app.ScaleModel([3]float64{2, 2, 2}); err != nil {
		t.Fatalf("ScaleModel: %v", err)
	}
	whole := app.view().Root.Volume // 8000

	out, err := app.Cut(app.view().Root.ID, PlaneInput{
		Origin: [3]float64{10, 10, 10}, Normal: [3]float64{0, 0, 1}, Width: 100, Height: 100,
	}, cut.PinSpec{})
	if err != nil {
		t.Fatalf("Cut: %v", err)
	}
	var sum float64
	for _, c := range out.Tree.Root.Children {
		sum += c.Volume
	}
	if math.Abs(sum-whole) > 1e-6 {
		t.Errorf("the pieces sum to %v, want the scaled whole %v", sum, whole)
	}
	if len(out.Tree.Root.Children) != 2 || !out.Watertight {
		t.Errorf("a scaled cube should halve cleanly; warnings %v", out.Warnings)
	}
}

func TestScaleModelWithNoModelOpen(t *testing.T) {
	app := NewApp()
	if _, err := app.ScaleModel([3]float64{2, 2, 2}); err == nil {
		t.Error("scaling with nothing open should be an error")
	}
}

// A freshly loaded model reports no scaling, so the fields start at 100%.
func TestAFreshModelReportsUnitScale(t *testing.T) {
	v := loadCube(t, 10).view()
	if v.Scale != [3]float64{1, 1, 1} {
		t.Errorf("Scale = %v, want 1,1,1", v.Scale)
	}
	if v.OriginalSize != [3]float64{10, 10, 10} {
		t.Errorf("OriginalSize = %v, want 10x10x10", v.OriginalSize)
	}
}
