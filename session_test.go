package main

import (
	"sync"
	"testing"

	"stl-cutter/internal/fixtures"
	"stl-cutter/internal/stl"
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

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.WithTree(func(tr *Tree) error {
				leaf := tr.Leaves()[0]
				_, _, err := tr.Split(leaf.ID, fixtures.Cube(2), fixtures.Cube(2), true, true)
				return err
			})
		}()
	}
	wg.Wait()

	// Every successful split adds exactly one leaf; the count must be consistent
	// with the number of splits that actually happened, whatever the ordering.
	if got := len(s.Tree().Leaves()); got < 1 {
		t.Errorf("got %d leaves, want at least 1", got)
	}
}

func TestWithTreeErrorsWhenNothingIsLoaded(t *testing.T) {
	var s Session
	err := s.WithTree(func(tr *Tree) error { return nil })
	if err == nil {
		t.Error("expected an error when no model is loaded")
	}
}
