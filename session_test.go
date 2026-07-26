package main

import (
	"sync"
	"testing"

	"stl-wizard/internal/cut"
	"stl-wizard/internal/fixtures"
	"stl-wizard/internal/stl"
)

func TestSessionStartsEmpty(t *testing.T) {
	var s Session
	if s.Loaded() {
		t.Error("a fresh session has no model")
	}
	if _, ok := s.MeshFor("p1"); ok {
		t.Error("no meshes should be available before loading")
	}
}

func TestSessionLoadReplacesTheTree(t *testing.T) {
	var s Session
	if err := s.Load("cube.stl", fixtures.Cube(10)); err != nil {
		t.Fatalf("Load: %v", err)
	}
	first := s.Tree().Root.ID

	if err := s.Load("sphere.stl", fixtures.UVSphere(5, 16, 8)); err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if s.Tree().ModelName != "sphere.stl" {
		t.Errorf("ModelName = %q, want sphere.stl", s.Tree().ModelName)
	}
	if _, ok := s.MeshFor(first); ok {
		t.Error("the previous model's parts should be gone after loading a new one")
	}
}

func TestSessionRefusesAnOversizedModel(t *testing.T) {
	var s Session
	big := &stl.Mesh{Tris: make([]stl.Tri, maxTriangles+1)}
	if err := s.Load("huge.stl", big); err == nil {
		t.Error("expected an error for a model above the triangle cap")
	}
}

func TestMeshForFindsALeaf(t *testing.T) {
	var s Session
	if err := s.Load("cube.stl", fixtures.Cube(10)); err != nil {
		t.Fatalf("Load: %v", err)
	}
	id := s.Tree().Root.ID

	m, ok := s.MeshFor(id)
	if !ok {
		t.Fatal("the root's mesh should be available")
	}
	if len(m.Tris) != 12 {
		t.Errorf("got %d triangles, want 12", len(m.Tris))
	}
}

// WithTree is the only way to touch the tree, so that every mutation is under
// the lock. This is what makes overlapping Cut and Undo calls safe.
func TestWithTreeSerialisesConcurrentMutations(t *testing.T) {
	var s Session
	if err := s.Load("cube.stl", fixtures.Cube(10)); err != nil {
		t.Fatalf("Load: %v", err)
	}

	errs := make([]error, 50)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = s.WithTree(func(tr *Tree) error {
				leaf := tr.Leaves()[0]
				_, _, err := tr.Split(leaf.ID, fixtures.Cube(2), fixtures.Cube(2), true, true)
				return err
			})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("split %d: %v", i, err)
		}
	}

	// Each of the 50 splits turns one leaf into two, so the count is deterministic
	// regardless of ordering. Asserting a range instead would pass against an
	// unlocked implementation: a reviewer removed the lock and saw counts of 7, 8,
	// 10, 16 and 19 while a ">= 1" assertion stayed green every run.
	if got := len(s.Tree().Leaves()); got != 51 {
		t.Errorf("got %d leaves, want 51 — a lost update means the lock is not holding", got)
	}
}

func TestWithTreeErrorsWhenNothingIsLoaded(t *testing.T) {
	var s Session
	err := s.WithTree(func(tr *Tree) error { return nil })
	if err == nil {
		t.Error("expected an error when no model is loaded")
	}
}

// Executing a plan rebuilds the tree from the mesh as loaded, and the root's verdict is
// carried across rather than recomputed — the mesh has not changed, and on a 492k-triangle
// model checking it again costs about a second on every single execute.
//
// Carrying a verdict is only safe if it is the real one. This loads a model that is NOT a
// closed solid and asserts the rebuilt tree still says so: a carried constant, or a
// zero-valued Report standing in for "not checked", would claim the model is closed and
// that is the silent-bad-part outcome the application exists to prevent.
func TestReplayKeepsTheRootsRealVerdict(t *testing.T) {
	for _, c := range []struct {
		name       string
		mesh       *stl.Mesh
		watertight bool
	}{
		{"openbox", fixtures.OpenBox(10), false},
		{"cube", fixtures.Cube(10), true},
	} {
		app := NewApp()
		if _, err := app.loadPath(writeFixture(t, c.name+".stl", c.mesh), false); err != nil {
			t.Fatalf("%s: load: %v", c.name, err)
		}
		if got := app.view().Root.Watertight; got != c.watertight {
			t.Fatalf("%s: on load Watertight = %v, want %v", c.name, got, c.watertight)
		}

		// A plan whose execution rebuilds the tree from the original mesh.
		r := app.view().Root
		if _, err := app.AddPlane(PlaneInput{
			Origin: [3]float64{r.Min[0] + r.Size[0]/2, r.Min[1] + r.Size[1]/2, r.Min[2] + r.Size[2]/2},
			Normal: [3]float64{0, 0, 1},
			U:      [3]float64{1, 0, 0},
			V:      [3]float64{0, 1, 0},
			Width:  r.Size[0] * 3,
			Height: r.Size[1] * 3,
		}, cut.PinSpec{}); err != nil {
			t.Fatalf("%s: AddPlane: %v", c.name, err)
		}
		out, err := app.ExecutePlan()
		if err != nil {
			t.Fatalf("%s: execute: %v", c.name, err)
		}
		if got := out.Tree.Root.Watertight; got != c.watertight {
			t.Errorf("%s: after replay the root says Watertight = %v, want %v",
				c.name, got, c.watertight)
		}
	}
}
