package main

import (
	"fmt"
	"sync/atomic"

	"stl-cutter/internal/meshcheck"
	"stl-cutter/internal/stl"
)

// treeSeq hands each Tree a distinct id prefix, so part ids from a model that has
// been closed can never collide with those of the model that replaced it. It is
// consulted once per tree rather than once per part: Session serialises every
// path that creates parts, so nothing finer is needed.
var treeSeq atomic.Uint64

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

	seq     uint64 // unique per tree; makes this tree's part ids globally distinct
	nextID  int
	history []undoStep
}

func (t *Tree) CanUndo() bool { return len(t.history) > 0 }

// MarshalJSON is not needed; CanUndo is exposed through the App layer instead,
// which is where the frontend's view of the tree is assembled.

func (t *Tree) newPart(name string, m *stl.Mesh, watertight bool) *Part {
	t.nextID++

	b := m.BBox()
	size := b.Size()
	return &Part{
		ID:         fmt.Sprintf("t%dp%d", t.seq, t.nextID),
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
	t := &Tree{ModelName: name, seq: treeSeq.Add(1)}
	// Check the loaded mesh rather than assuming it is sound. A model that is
	// already not a closed solid must say so before it is ever cut — reporting
	// "closed: yes" for it is exactly the silent-bad-part outcome this app is
	// meant to rule out.
	watertight := meshcheck.Check(root, root.Epsilon()).OK()
	t.Root = t.newPart("whole", root, watertight)
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
//
// A cut's children are suffixed a and b; a separation's are numbered. The suffix
// therefore says which operation produced a part.
func (t *Tree) Split(id string, a, b *stl.Mesh, aWatertight, bWatertight bool) (*Part, *Part, error) {
	kids, err := t.replaceLeaf(id, []*stl.Mesh{a, b}, []bool{aWatertight, bWatertight},
		func(name string, i int) string { return name + string(rune('a'+i)) })
	if err != nil {
		return nil, nil, err
	}
	return kids[0], kids[1], nil
}

// SplitMany replaces the leaf id with one child per mesh, for separating a part
// into its disconnected bodies.
func (t *Tree) SplitMany(id string, meshes []*stl.Mesh, watertight []bool) ([]*Part, error) {
	return t.replaceLeaf(id, meshes, watertight,
		func(name string, i int) string { return fmt.Sprintf("%s%d", name, i+1) })
}

// replaceLeaf is the one path by which a leaf becomes a parent, so cuts and
// separations cannot drift apart over id assignment, undo history or selection.
//
// It validates everything before mutating anything: a refused split must leave the
// tree exactly as it was, with no half-built children and no undo entry that would
// put back a mesh nothing had taken away.
func (t *Tree) replaceLeaf(id string, meshes []*stl.Mesh, watertight []bool, name func(string, int) string) ([]*Part, error) {
	if len(meshes) < 2 {
		return nil, fmt.Errorf("a split needs at least two pieces, got %d", len(meshes))
	}
	if len(watertight) != len(meshes) {
		return nil, fmt.Errorf("got %d meshes but %d watertight flags", len(meshes), len(watertight))
	}
	p := t.Find(id)
	if p == nil {
		return nil, fmt.Errorf("no part with id %q", id)
	}
	if !p.IsLeaf() {
		return nil, fmt.Errorf("part %q has already been split; select one of its pieces", p.Name)
	}

	kids := make([]*Part, 0, len(meshes))
	for i, m := range meshes {
		kids = append(kids, t.newPart(name(p.Name, i), m, watertight[i]))
	}
	p.Children = kids

	// The parent's mesh moves into the history: it is what Undo puts back, and
	// keeping it on the node as well would double the memory for every cut.
	t.history = append(t.history, undoStep{parent: p, mesh: p.Mesh})
	p.Mesh = nil

	t.SelectedID = kids[0].ID
	return kids, nil
}

// RefreshLeaves recomputes every leaf's recorded measurements from its mesh.
//
// Needed when a mesh is changed after the part was created — a cut that left a gap and
// had it closed is not the geometry the part first recorded, and the sidebar must show
// what will actually be exported.
func (t *Tree) RefreshLeaves() {
	for _, p := range t.Leaves() {
		if p.Mesh == nil {
			continue
		}
		b := p.Mesh.BBox()
		size := b.Size()
		p.Tris = len(p.Mesh.Tris)
		p.Volume = p.Mesh.Volume()
		p.Min = [3]float64{b.Min[0], b.Min[1], b.Min[2]}
		p.Size = [3]float64{size[0], size[1], size[2]}
		p.Watertight = meshcheck.Check(p.Mesh, p.Mesh.Epsilon()).OK()
	}
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
