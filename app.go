package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"stl-cutter/internal/cut"
	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

// App is the bound service. Every exported method on it is callable from the
// frontend, and together they are the application's entire command API.
type App struct {
	ctx     context.Context
	session *Session
}

func NewApp() *App { return &App{session: &Session{}} }

// startup stores the context Wails hands us. Every runtime call — dialogs,
// events — needs it.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// emit sends an event to the frontend, or does nothing when there is no frontend
// — which is the case in unit tests, where a.ctx is never set.
func (a *App) emit(name string, data ...interface{}) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, name, data...)
}

// TreeView is the tree as the frontend sees it. It is a separate type from Tree
// because the frontend needs CanUndo, which is derived from history the tree
// keeps private, and because Part.Mesh must never be serialised.
type TreeView struct {
	ModelName  string `json:"modelName"`
	Root       *Part  `json:"root"`
	SelectedID string `json:"selectedId"`
	CanUndo    bool   `json:"canUndo"`
}

// view snapshots the current tree for the frontend. Returns nil when nothing is
// open, which the frontend renders as the empty state.
//
// Every field is read inside WithTree. Session.Tree() would hand back a raw
// pointer and drop the lock, leaving these reads racing any concurrent Cut or
// Undo — the tree is deliberately lock-free so it stays testable, and this is
// where that has to be paid for.
func (a *App) view() *TreeView {
	var v *TreeView
	_ = a.session.WithTree(func(tr *Tree) error {
		v = &TreeView{
			ModelName:  tr.ModelName,
			Root:       clonePart(tr.Root),
			SelectedID: tr.SelectedID,
			CanUndo:    tr.CanUndo(),
		}
		return nil
	})
	return v
}

// clonePart deep-copies a part subtree, so what the frontend receives shares no
// memory with the live tree.
//
// Wails marshals a bound method's return value after the method has returned and
// the lock has been dropped, on its own goroutine. Tree.Split mutates parts in
// place — it sets Children and nils Mesh — so without this copy a concurrent cut
// could tear a slice header the JSON encoder is walking, panicking in a goroutine
// nothing recovers and taking the window with it. go test -race cannot see this:
// the marshalling happens inside Wails, never in a test.
//
// The tree holds tens of nodes and the copy runs under a lock already held.
func clonePart(p *Part) *Part {
	if p == nil {
		return nil
	}
	c := *p
	c.Mesh = nil // never serialised; the frontend fetches geometry by id
	c.Children = nil
	for _, ch := range p.Children {
		c.Children = append(c.Children, clonePart(ch))
	}
	return &c
}

// OpenModel shows a native file dialog and loads the chosen STL.
func (a *App) OpenModel() (*TreeView, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Open STL",
		Filters: []runtime.FileFilter{
			{DisplayName: "STL models (*.stl)", Pattern: "*.stl"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("could not open the file dialog: %w", err)
	}
	if path == "" {
		// The user cancelled. Not an error; the frontend leaves things as they are.
		return a.view(), nil
	}
	return a.loadPath(path)
}

// loadPath is everything OpenModel does once a path is known, split out so it
// can be tested without a dialog.
func (a *App) loadPath(path string) (*TreeView, error) {
	mesh, err := stl.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := a.session.Load(filepath.Base(path), mesh); err != nil {
		return nil, err
	}
	return a.view(), nil
}

// PlaneInput is the gizmo's state as the frontend reports it. Arrays rather than
// geom.Vec3 because that is what serialises cleanly to and from JavaScript.
type PlaneInput struct {
	Origin [3]float64 `json:"origin"`
	Normal [3]float64 `json:"normal"`
	U      [3]float64 `json:"u"`
	V      [3]float64 `json:"v"`
	Width  float64    `json:"width"`
	Height float64    `json:"height"`
}

// CutOutcome is what the frontend needs after a cut: the new tree, and an honest
// account of how well it went.
type CutOutcome struct {
	Tree       *TreeView `json:"tree"`
	Watertight bool      `json:"watertight"`
	Warnings   []string  `json:"warnings"`
}

// Cut splits the given part with a bounded plane.
//
// A part that comes back not watertight is still added to the tree, flagged,
// and Warnings explains why. Hiding that would produce a model that looks right
// on screen and fails to print, which is the one outcome this application must
// never produce.
func (a *App) Cut(partID string, p PlaneInput) (*CutOutcome, error) {
	spec := cut.Spec{
		Origin: geom.Vec3{p.Origin[0], p.Origin[1], p.Origin[2]},
		Normal: geom.Vec3{p.Normal[0], p.Normal[1], p.Normal[2]},
		U:      geom.Vec3{p.U[0], p.U[1], p.U[2]},
		V:      geom.Vec3{p.V[0], p.V[1], p.V[2]},
		Width:  p.Width,
		Height: p.Height,
	}
	// The gizmo may not send a basis; derive one when it is absent.
	if spec.U == (geom.Vec3{}) || spec.V == (geom.Vec3{}) {
		spec = cut.SpecFromNormal(spec.Origin, spec.Normal, spec.Width, spec.Height)
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}

	a.emit("cut:start")
	defer a.emit("cut:done")

	var res *cut.Result
	err := a.session.WithTree(func(tr *Tree) error {
		part := tr.Find(partID)
		if part == nil {
			return fmt.Errorf("no part with id %q", partID)
		}
		if !part.IsLeaf() {
			return fmt.Errorf("%q has already been split; select one of its pieces", part.Name)
		}

		var err error
		res, err = cut.SplitProgress(part.Mesh, spec, func(f float64) {
			a.emit("cut:progress", f)
		})
		if err != nil {
			return err
		}
		_, _, err = tr.Split(partID, res.Part1, res.Part2,
			res.Part1Check.OK(), res.Part2Check.OK())
		return err
	})
	if err != nil {
		return nil, err
	}

	return &CutOutcome{
		Tree:       a.view(),
		Watertight: res.Watertight(),
		Warnings:   res.Warnings,
	}, nil
}

// Undo reverses the most recent cut and re-selects the part it restored.
func (a *App) Undo() (*TreeView, error) {
	err := a.session.WithTree(func(tr *Tree) error { return tr.Undo() })
	if err != nil {
		return nil, err
	}
	return a.view(), nil
}

// Select changes which part the next cut applies to. Only leaves can be
// selected: a part that has been split has no geometry of its own.
func (a *App) Select(partID string) (*TreeView, error) {
	err := a.session.WithTree(func(tr *Tree) error {
		p := tr.Find(partID)
		if p == nil {
			return fmt.Errorf("no part with id %q", partID)
		}
		if !p.IsLeaf() {
			return fmt.Errorf("%q has been split; select one of its pieces", p.Name)
		}
		tr.SelectedID = partID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return a.view(), nil
}

// ExportOutcome tells the frontend where the files went and which of them the
// user should look at twice.
type ExportOutcome struct {
	Dir           string   `json:"dir"`
	Files         []string `json:"files"`
	NotWatertight []string `json:"notWatertight"`
	// Cancelled reports that the user dismissed the folder dialog. The frontend
	// gets a real object either way, so it never has to guard against null.
	Cancelled bool `json:"cancelled"`
}

// ExportAll writes every leaf part as a binary STL into a folder the user picks.
func (a *App) ExportAll() (*ExportOutcome, error) {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose a folder for the parts",
	})
	if err != nil {
		return nil, fmt.Errorf("could not open the folder dialog: %w", err)
	}
	if dir == "" {
		return &ExportOutcome{Cancelled: true}, nil
	}
	return a.exportTo(dir)
}

// exportTo is ExportAll once a folder is known, split out so it can be tested
// without a dialog.
func (a *App) exportTo(dir string) (*ExportOutcome, error) {
	// What to write is decided under the lock; the writing itself happens after
	// it is released. Holding the session mutex across the disk I/O would block
	// Cut, Undo, Select and the viewer's redraw for the whole export. A Part's
	// mesh is only ever replaced, never mutated in place, so a snapshotted
	// pointer stays valid for as long as this needs it.
	type pending struct {
		name       string
		mesh       *stl.Mesh
		watertight bool
	}
	var todo []pending

	err := a.session.WithTree(func(tr *Tree) error {
		base := strings.TrimSuffix(tr.ModelName, filepath.Ext(tr.ModelName))
		seen := make(map[string]bool)
		for i, leaf := range tr.Leaves() {
			name := fmt.Sprintf("%s_%s.stl", base, leaf.Name)
			// Leaf names are root-to-leaf a/b paths and so are already unique
			// within a tree; this only guards against that changing.
			if seen[name] {
				name = fmt.Sprintf("%s_part%d.stl", base, i+1)
			}
			seen[name] = true
			todo = append(todo, pending{name: name, mesh: leaf.Mesh, watertight: leaf.Watertight})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	out := &ExportOutcome{Dir: dir}
	for _, p := range todo {
		if err := stl.WriteFile(filepath.Join(dir, p.name), p.mesh); err != nil {
			// Return what did get written. Those files are on disk whatever
			// happens next, and the user needs to know which ones.
			return out, err
		}
		out.Files = append(out.Files, p.name)
		if !p.watertight {
			out.NotWatertight = append(out.NotWatertight, p.name)
		}
	}
	return out, nil
}
