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

	// plan is the cuts asked for but not yet made, and origin is the mesh as it was
	// loaded. Cut now rebuilds the tree from origin by replaying the plan, so the
	// tree cannot be the source: the root releases its mesh into the undo history
	// the moment it is split.
	plan     Plan
	origin   *stl.Mesh
	originNm string

	// scale is applied to origin whenever a tree is built, and is relative to the file
	// as loaded. Kept as a factor rather than baked into origin so that applying the
	// same factors twice is idempotent and returning to 1 is exact.
	scale [3]float64
}

// working returns the mesh a tree should be built from: the file as loaded, scaled.
func (s *Session) working() *stl.Mesh {
	if s.scale == [3]float64{1, 1, 1} {
		return s.origin
	}
	return s.origin.Scaled(s.scale)
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
	s.origin, s.originNm = m, name
	s.scale = [3]float64{1, 1, 1}
	s.tree = NewTree(name, m)
	// A plan names parts of the model it was built against, so keeping it across a
	// load would leave every entry targeting something that does not exist. Reset
	// rather than Clear: a new model starts counting at "Cut 1".
	s.plan.Reset()
	return nil
}

// WithPlan runs fn with the plan under the lock, and reports whether a model is open —
// a plan without a model is meaningless, since every entry targets one of its parts.
func (s *Session) WithPlan(fn func(*Plan) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tree == nil {
		return errors.New("no model is open")
	}
	return fn(&s.plan)
}

// WithTreeAndPlan runs fn with both under one lock. AddPlane needs it: which part is
// selected and appending the entry that names it have to be one atomic step, or a cut
// arriving between them would leave the entry targeting a part that has just been split.
func (s *Session) WithTreeAndPlan(fn func(*Tree, *Plan) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tree == nil {
		return errors.New("no model is open")
	}
	return fn(s.tree, &s.plan)
}

// Replay rebuilds the tree from the mesh as loaded and hands it, with the plan, to fn.
// Everything happens under one lock: a half-rebuilt tree must never be visible.
func (s *Session) Replay(fn func(*Tree, *Plan) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.origin == nil {
		return errors.New("no model is open")
	}
	s.tree = NewTree(s.originNm, s.working())
	return fn(s.tree, &s.plan)
}

// Scale reports the factors in force, relative to the file as loaded.
func (s *Session) Scale() [3]float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scale == ([3]float64{}) {
		return [3]float64{1, 1, 1} // nothing loaded yet
	}
	return s.scale
}

// OriginalSize is the size of the file as loaded, which is what a target size in
// millimetres has to be divided by to get a factor.
func (s *Session) OriginalSize() [3]float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.origin == nil {
		return [3]float64{}
	}
	sz := s.origin.BBox().Size()
	return [3]float64{sz[0], sz[1], sz[2]}
}

// Rescale sets the scale and rebuilds the tree from the file as loaded.
//
// The plan is cleared: its planes name coordinates that no longer describe the model, and
// for a non-uniform scale a bounded rectangle does not even stay a rectangle unless it
// happens to align with the scale axes.
func (s *Session) Rescale(factors [3]float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.origin == nil {
		return errors.New("no model is open")
	}
	s.scale = factors
	s.tree = NewTree(s.originNm, s.working())
	s.plan.Reset()
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
