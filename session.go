package main

import (
	"errors"
	"fmt"
	"sync"

	"stl-cutter/internal/stl"
)

// maxTriangles caps what will be loaded. A triangle costs 72 bytes as a soup,
// and a cut tree holds a mesh per leaf, so a few million triangles is already
// hundreds of megabytes.
//
// ponytail: a flat cap rather than a running total of the whole tree. Upgrade
// path if it bites: sum triangles across leaves in Session.WithTree and refuse a
// cut that would cross the budget.
const maxTriangles = 3_000_000

// Session holds the one model the window is working on. Wails runs every
// JS-to-Go call in its own goroutine, so two commands can genuinely overlap —
// double-clicking Cut, or hitting Undo mid-cut. Everything goes through the
// mutex.
type Session struct {
	mu   sync.Mutex
	tree *Tree
}

func (s *Session) Loaded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tree != nil
}

func (s *Session) Load(name string, m *stl.Mesh) error {
	if len(m.Tris) > maxTriangles {
		return fmt.Errorf("model has %d triangles, above the %d limit this build can hold",
			len(m.Tris), maxTriangles)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tree = NewTree(name, m)
	return nil
}

// Tree returns the live tree. Callers must not mutate it; use WithTree for that.
func (s *Session) Tree() *Tree {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tree
}

// WithTree runs fn with the tree under the lock. Every mutation goes through
// here, which is what keeps overlapping commands from interleaving.
func (s *Session) WithTree(fn func(*Tree) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tree == nil {
		return errors.New("no model is open")
	}
	return fn(s.tree)
}

// MeshFor returns the mesh of a leaf part. Parts that have been split have no
// mesh of their own.
func (s *Session) MeshFor(id string) (*stl.Mesh, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tree == nil {
		return nil, false
	}
	p := s.tree.Find(id)
	if p == nil || p.Mesh == nil {
		return nil, false
	}
	return p.Mesh, true
}
