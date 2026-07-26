package main

import (
	"math"
	"testing"

	"stl-wizard/internal/fixtures"
	"stl-wizard/internal/stl"
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

// A model that is not a closed solid must be flagged the moment it is loaded,
// not only after it is cut.
func TestNewTreeFlagsANonWatertightModel(t *testing.T) {
	if tr := NewTree("holed.stl", fixtures.NonManifold()); tr.Root.Watertight {
		t.Error("a mesh with a hole was reported as a closed solid on load")
	}
	if tr := NewTree("cube.stl", fixtures.Cube(10)); !tr.Root.Watertight {
		t.Error("a closed cube was reported as not watertight")
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

func TestSplitManyReplacesALeafWithOneChildPerMesh(t *testing.T) {
	tr := NewTree("bodies.stl", fixtures.Cube(10))
	rootID := tr.Root.ID

	kids, err := tr.SplitMany(rootID,
		[]*stl.Mesh{fixtures.Cube(5), fixtures.Cube(4), fixtures.Cube(3)},
		[]bool{true, true, false})
	if err != nil {
		t.Fatalf("SplitMany: %v", err)
	}
	if len(kids) != 3 {
		t.Fatalf("got %d children, want 3", len(kids))
	}
	if len(tr.Leaves()) != 3 {
		t.Errorf("got %d leaves, want 3", len(tr.Leaves()))
	}
	// Numbered, so a name says which operation produced the part: cuts use a/b.
	for i, want := range []string{"whole1", "whole2", "whole3"} {
		if kids[i].Name != want {
			t.Errorf("child %d is named %q, want %q", i, kids[i].Name, want)
		}
	}
	seen := map[string]bool{}
	for _, k := range kids {
		if seen[k.ID] {
			t.Errorf("duplicate id %q", k.ID)
		}
		seen[k.ID] = true
	}
	if kids[2].Watertight {
		t.Error("the third mesh was passed as not watertight")
	}
	if tr.Root.Mesh != nil {
		t.Error("a split parent should release its mesh")
	}
	if tr.SelectedID != kids[0].ID {
		t.Errorf("SelectedID = %q, want the first child", tr.SelectedID)
	}
}

// Undo records only the parent and its mesh, so it never knew how many children a
// split made. One undo must put back a three-way separation just as it does a cut.
func TestUndoRestoresAManyWaySplit(t *testing.T) {
	tr := NewTree("bodies.stl", fixtures.Cube(10))
	rootID := tr.Root.ID
	if _, err := tr.SplitMany(rootID,
		[]*stl.Mesh{fixtures.Cube(5), fixtures.Cube(4), fixtures.Cube(3)},
		[]bool{true, true, true}); err != nil {
		t.Fatalf("SplitMany: %v", err)
	}

	if err := tr.Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if !tr.Root.IsLeaf() {
		t.Error("the root should be a leaf again")
	}
	if tr.Root.Mesh == nil {
		t.Fatal("the root's mesh was not restored")
	}
	if got := len(tr.Root.Mesh.Tris); got != 12 {
		t.Errorf("restored mesh has %d triangles, want the original 12", got)
	}
	if tr.CanUndo() {
		t.Error("one separation should leave nothing more to undo")
	}
}

func TestSplitManyRefusesFewerThanTwoMeshes(t *testing.T) {
	tr := NewTree("cube.stl", fixtures.Cube(10))
	if _, err := tr.SplitMany(tr.Root.ID, []*stl.Mesh{fixtures.Cube(5)}, []bool{true}); err == nil {
		t.Error("splitting a part into one piece should be refused, not recorded as a split")
	}
	if !tr.Root.IsLeaf() {
		t.Error("a refused split must leave the tree alone")
	}
	if tr.CanUndo() {
		t.Error("a refused split must not push undo history")
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
	wantTris := len(tr.Root.Mesh.Tris)
	wantVolume := tr.Root.Mesh.Volume()

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
		t.Fatal("undo must restore the parent's mesh")
	}
	// Not just "some mesh" — the original one. A mutant restoring an empty mesh
	// would satisfy a nil check while losing the geometry undo exists to bring back.
	if got := len(tr.Root.Mesh.Tris); got != wantTris {
		t.Errorf("restored mesh has %d triangles, want the original %d", got, wantTris)
	}
	if got := tr.Root.Mesh.Volume(); math.Abs(got-wantVolume) > 1e-9 {
		t.Errorf("restored mesh volume = %v, want the original %v", got, wantVolume)
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
