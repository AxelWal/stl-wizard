package main

import (
	"fmt"
	"sync"

	"stl-cutter/internal/stl"
)

var globalIDLock sync.Mutex
var globalNextID int

// Part is one node of the cut tree. Only leaves carry a mesh: a part that has
// been split is just a grouping, and holding its mesh would multiply memory for
// nothing. Undo restores it from the history instead.
type Part struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Tris       int        `json:"tris"`
	Volume     float64    `json:"volume"` // mm³
	Min        [3]float64 `json:"min"`
	Size       [3]float64 `json:"size"`
	Watertight bool       `json:"watertight"`
	Children   []*Part    `json:"children"`

	// Mesh is nil for a part that has been split. Not serialised — the frontend
	// fetches geometry from /part/{id}.stl instead.
	Mesh *stl.Mesh `json:"-"`
}

func (p *Part) IsLeaf() bool { return len(p.Children) == 0 }

// undoStep records what a split replaced, so it can be put back exactly.
type undoStep struct {
	parent *Part
	mesh   *stl.Mesh
}

type Tree struct {
	ModelName  string `json:"modelName"`
	Root       *Part  `json:"root"`
	SelectedID string `json:"selectedId"`

	history []undoStep
}

func (t *Tree) CanUndo() bool { return len(t.history) > 0 }

// MarshalJSON is not needed; CanUndo is exposed through the App layer instead,
// which is where the frontend's view of the tree is assembled.

func (t *Tree) newPart(name string, m *stl.Mesh, watertight bool) *Part {
	globalIDLock.Lock()
	globalNextID++
	id := globalNextID
	globalIDLock.Unlock()

	b := m.BBox()
	size := b.Size()
	return &Part{
		ID:         fmt.Sprintf("p%d", id),
		Name:       name,
		Tris:       len(m.Tris),
		Volume:     m.Volume(),
		Min:        [3]float64{b.Min[0], b.Min[1], b.Min[2]},
		Size:       [3]float64{size[0], size[1], size[2]},
		Watertight: watertight,
		Mesh:       m,
	}
}

func NewTree(name string, root *stl.Mesh) *Tree {
	t := &Tree{ModelName: name}
	t.Root = t.newPart("whole", root, true)
	t.SelectedID = t.Root.ID
	return t
}

// Find returns the part with the given id, or nil.
func (t *Tree) Find(id string) *Part {
	var walk func(p *Part) *Part
	walk = func(p *Part) *Part {
		if p == nil {
			return nil
		}
		if p.ID == id {
			return p
		}
		for _, c := range p.Children {
			if found := walk(c); found != nil {
				return found
			}
		}
		return nil
	}
	return walk(t.Root)
}

// Leaves returns the parts that carry geometry. Only these are exported.
func (t *Tree) Leaves() []*Part {
	var out []*Part
	var walk func(p *Part)
	walk = func(p *Part) {
		if p == nil {
			return
		}
		if p.IsLeaf() {
			out = append(out, p)
			return
		}
		for _, c := range p.Children {
			walk(c)
		}
	}
	walk(t.Root)
	return out
}

// Split replaces the leaf id with two children carrying the given meshes.
func (t *Tree) Split(id string, a, b *stl.Mesh, aWatertight, bWatertight bool) (*Part, *Part, error) {
	p := t.Find(id)
	if p == nil {
		return nil, nil, fmt.Errorf("no part with id %q", id)
	}
	if !p.IsLeaf() {
		return nil, nil, fmt.Errorf("part %q has already been split; select one of its pieces", p.Name)
	}

	ca := t.newPart(p.Name+"a", a, aWatertight)
	cb := t.newPart(p.Name+"b", b, bWatertight)
	p.Children = []*Part{ca, cb}

	// The parent's mesh moves into the history: it is what Undo puts back, and
	// keeping it on the node as well would double the memory for every cut.
	t.history = append(t.history, undoStep{parent: p, mesh: p.Mesh})
	p.Mesh = nil

	t.SelectedID = ca.ID
	return ca, cb, nil
}

// Undo reverses the most recent split.
func (t *Tree) Undo() error {
	if len(t.history) == 0 {
		return fmt.Errorf("nothing to undo")
	}
	step := t.history[len(t.history)-1]
	t.history = t.history[:len(t.history)-1]

	step.parent.Children = nil
	step.parent.Mesh = step.mesh
	t.SelectedID = step.parent.ID
	return nil
}
