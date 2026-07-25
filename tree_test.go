package main

import (
	"testing"

	"stl-cutter/internal/fixtures"
)

func TestNewTreeHasASingleSelectableRoot(t *testing.T) {
	tr := NewTree("cube.stl", fixtures.Cube(10))

	if tr.Root == nil {
		t.Fatal("root is nil")
	}
	if !tr.Root.IsLeaf() {
		t.Error("a fresh root should be a leaf")
	}
	if tr.SelectedID != tr.Root.ID {
		t.Errorf("SelectedID = %q, want the root %q", tr.SelectedID, tr.Root.ID)
	}
	if tr.CanUndo() {
		t.Error("a fresh tree has nothing to undo")
	}
	if got := len(tr.Leaves()); got != 1 {
		t.Errorf("got %d leaves, want 1", got)
	}
}

func TestRootRecordsMeasurements(t *testing.T) {
	tr := NewTree("cube.stl", fixtures.Cube(10))

	if got := tr.Root.Tris; got != 12 {
		t.Errorf("Tris = %d, want 12", got)
	}
	if got := tr.Root.Volume; got < 999 || got > 1001 {
		t.Errorf("Volume = %v, want about 1000", got)
	}
	if got := tr.Root.Size; got != [3]float64{10, 10, 10} {
		t.Errorf("Size = %v, want 10x10x10", got)
	}
}

func TestSplitReplacesALeafWithTwoChildren(t *testing.T) {
	tr := NewTree("cube.stl", fixtures.Cube(10))
	rootID := tr.Root.ID

	a, b, err := tr.Split(rootID, fixtures.Cube(5), fixtures.Cube(4), true, true)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if tr.Root.IsLeaf() {
		t.Error("the root should no longer be a leaf")
	}
	if len(tr.Leaves()) != 2 {
		t.Errorf("got %d leaves, want 2", len(tr.Leaves()))
	}
	if a.ID == b.ID {
		t.Error("children must have distinct ids")
	}
	// Only leaves are exported, so the parent's mesh is no longer needed.
	if tr.Root.Mesh != nil {
		t.Error("a split parent should release its mesh")
	}
	if tr.SelectedID != a.ID {
		t.Errorf("SelectedID = %q, want the first child %q", tr.SelectedID, a.ID)
	}
}

func TestSplitRefusesANonLeaf(t *testing.T) {
	tr := NewTree("cube.stl", fixtures.Cube(10))
	rootID := tr.Root.ID
	if _, _, err := tr.Split(rootID, fixtures.Cube(5), fixtures.Cube(4), true, true); err != nil {
		t.Fatalf("first split: %v", err)
	}
	if _, _, err := tr.Split(rootID, fixtures.Cube(3), fixtures.Cube(2), true, true); err == nil {
		t.Error("expected an error when splitting a part that was already split")
	}
}

func TestSplitRefusesAnUnknownID(t *testing.T) {
	tr := NewTree("cube.stl", fixtures.Cube(10))
	if _, _, err := tr.Split("nope", fixtures.Cube(5), fixtures.Cube(4), true, true); err == nil {
		t.Error("expected an error for an unknown part id")
	}
}

// Undo restores the parent's mesh, which is why Split keeps it rather than
// discarding it outright.
func TestUndoRestoresTheParentAndItsMesh(t *testing.T) {
	tr := NewTree("cube.stl", fixtures.Cube(10))
	rootID := tr.Root.ID
	if _, _, err := tr.Split(rootID, fixtures.Cube(5), fixtures.Cube(4), true, true); err != nil {
		t.Fatalf("Split: %v", err)
	}

	if err := tr.Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if !tr.Root.IsLeaf() {
		t.Error("after undo the root should be a leaf again")
	}
	if tr.Root.Mesh == nil {
		t.Error("undo must restore the parent's mesh")
	}
	if tr.SelectedID != rootID {
		t.Errorf("SelectedID = %q, want the restored parent %q", tr.SelectedID, rootID)
	}
	if tr.CanUndo() {
		t.Error("nothing left to undo")
	}
}

func TestUndoUnwindsInReverseOrder(t *testing.T) {
	tr := NewTree("cube.stl", fixtures.Cube(10))
	a, _, err := tr.Split(tr.Root.ID, fixtures.Cube(5), fixtures.Cube(4), true, true)
	if err != nil {
		t.Fatalf("first split: %v", err)
	}
	if _, _, err := tr.Split(a.ID, fixtures.Cube(3), fixtures.Cube(2), true, true); err != nil {
		t.Fatalf("second split: %v", err)
	}
	if got := len(tr.Leaves()); got != 3 {
		t.Fatalf("got %d leaves, want 3", got)
	}

	if err := tr.Undo(); err != nil {
		t.Fatalf("first undo: %v", err)
	}
	if got := len(tr.Leaves()); got != 2 {
		t.Errorf("after one undo got %d leaves, want 2", got)
	}
	if err := tr.Undo(); err != nil {
		t.Fatalf("second undo: %v", err)
	}
	if got := len(tr.Leaves()); got != 1 {
		t.Errorf("after two undos got %d leaves, want 1", got)
	}
}

func TestUndoOnAFreshTreeIsAnError(t *testing.T) {
	tr := NewTree("cube.stl", fixtures.Cube(10))
	if err := tr.Undo(); err == nil {
		t.Error("expected an error undoing with no history")
	}
}

// A flagged part must stay flagged in the tree, because the UI reads this to
// warn the user.
func TestSplitCarriesTheWatertightFlag(t *testing.T) {
	tr := NewTree("cube.stl", fixtures.Cube(10))
	a, b, err := tr.Split(tr.Root.ID, fixtures.Cube(5), fixtures.Cube(4), false, true)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if a.Watertight {
		t.Error("part a was reported not watertight and must stay flagged")
	}
	if !b.Watertight {
		t.Error("part b was watertight and should not be flagged")
	}
}

func TestLeavesReturnsOnlyLeaves(t *testing.T) {
	tr := NewTree("cube.stl", fixtures.Cube(10))
	a, _, err := tr.Split(tr.Root.ID, fixtures.Cube(5), fixtures.Cube(4), true, true)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if _, _, err := tr.Split(a.ID, fixtures.Cube(3), fixtures.Cube(2), true, true); err != nil {
		t.Fatalf("Split: %v", err)
	}
	for _, l := range tr.Leaves() {
		if !l.IsLeaf() {
			t.Errorf("part %s is not a leaf", l.ID)
		}
		if l.Mesh == nil {
			t.Errorf("leaf %s has no mesh; only leaves are exported", l.ID)
		}
	}
}
