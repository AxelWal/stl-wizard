package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"stl-cutter/internal/cut"
	"stl-cutter/internal/geom"
	"stl-cutter/internal/repair"
	"stl-cutter/internal/stl"
)

// App is the bound service. Every exported method on it is callable from the
// frontend, and together they are the application's entire command API.
type App struct {
	ctx     context.Context
	session *Session

	// emitFunc is the low-level event sink. It defaults to the real Wails
	// runtime call; tests override it to count events without a frontend.
	emitFunc func(ctx context.Context, name string, data ...interface{})
}

func NewApp() *App {
	return &App{session: &Session{}, emitFunc: runtime.EventsEmit}
}

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
	a.emitFunc(a.ctx, name, data...)
}

// TreeView is the tree as the frontend sees it. It is a separate type from Tree
// because the frontend needs CanUndo, which is derived from history the tree
// keeps private, and because Part.Mesh must never be serialised.
type TreeView struct {
	ModelName  string `json:"modelName"`
	Root       *Part  `json:"root"`
	SelectedID string `json:"selectedId"`
	CanUndo    bool   `json:"canUndo"`
	// Repair is non-nil only on a load that asked for repair and found something
	// to do. Cuts and undos leave it nil, so the message appears once, on the load
	// that caused it.
	Repair *RepairView `json:"repair,omitempty"`
}

// RepairView is what load-time repair did, for the sidebar to report.
//
// Before and After are the full verdicts as text rather than a "fixed" flag,
// because repair closes holes and drops degenerate triangles but deliberately
// does not correct winding — so a mesh can come back genuinely improved and still
// not be a closed solid, and saying so is the whole point.
type RepairView struct {
	HolesFilled       int    `json:"holesFilled"`
	TrianglesAdded    int    `json:"trianglesAdded"`
	DegenerateRemoved int    `json:"degenerateRemoved"`
	Before            string `json:"before"`
	After             string `json:"after"`
	Closed            bool   `json:"closed"`
	// NonManifoldEdges is how many open edges had more than two triangles meeting
	// along them — two surfaces touching rather than a hole. Filling does nothing
	// for those, and real exported models turn out to be mostly this: two parts
	// from this application had 6 and 16 open edges and not one was a hole. Without
	// this the sidebar could only say "filled 0 holes", which reads as a repair
	// that did not bother.
	NonManifoldEdges int `json:"nonManifoldEdges"`
	// ShellsDropped counts surfaces deleted for enclosing nothing — cut debris.
	// This is what actually repairs real exported files: two parts from this
	// application had 7 and 14 such shells, and deleting them took every one of
	// their 22 non-manifold edges with them.
	ShellsDropped    int `json:"shellsDropped"`
	TrianglesRemoved int `json:"trianglesRemoved"`
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
//
// repair asks for load-time hole filling. It is a parameter rather than App state
// so that what a load did is a property of that call, and two loads cannot
// disagree about a mode set between them.
func (a *App) OpenModel(repair bool) (*TreeView, error) {
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
	return a.loadPath(path, repair)
}

// OpenPath loads an STL from a path, bypassing the native dialog.
//
// This exists so the UI can be driven without a human at the keyboard.
// OpenModel's dialog belongs to the Wails window, so a browser tab pointed at
// the dev server (see CLAUDE.md) makes it appear over there and waits forever
// for an answer it cannot give. Everything past loading is identical, so this
// one method is the difference between a headless browser reaching the cut
// path and being stuck on an empty sidebar.
func (a *App) OpenPath(path string, repair bool) (*TreeView, error) {
	return a.loadPath(path, repair)
}

// loadPath is everything OpenModel does once a path is known, split out so it
// can be tested without a dialog.
//
// Repair runs before the mesh reaches the session, so the tree records the
// repaired geometry's measurements and watertightness rather than the file's. A
// tree that still flagged a part the sidebar had just announced as repaired would
// be the worst of both.
func (a *App) loadPath(path string, doRepair bool) (*TreeView, error) {
	mesh, err := stl.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var rv *RepairView
	if doRepair {
		res := repair.Repair(mesh, mesh.Epsilon())
		// Reported when repair did something, and also when it could not: a model
		// whose every open edge is non-manifold has to be told it will not improve
		// by trying again, rather than left with no message at all.
		if res.Changed() || res.Unfixable() {
			rv = &RepairView{
				HolesFilled:       res.HolesFilled,
				TrianglesAdded:    res.TrianglesAdded,
				DegenerateRemoved: res.DegenerateRemoved,
				Before:            res.Before.String(),
				After:             res.After.String(),
				Closed:            res.After.OK(),
				NonManifoldEdges:  res.NonManifoldEdges,
				ShellsDropped:     res.ShellsDropped,
				TrianglesRemoved:  res.TrianglesRemoved,
			}
		}
	}

	if err := a.session.Load(filepath.Base(path), mesh); err != nil {
		return nil, err
	}
	v := a.view()
	if v != nil {
		v.Repair = rv
	}
	return v, nil
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
	PinsPlaced int       `json:"pinsPlaced"`
	// PinsRequested is what the user asked for, which PinsPlaced can fall short
	// of without any SkippedPin saying so — cut.PinSpec.Count is a target and
	// placement simply stops when the face runs out of room. The frontend needs
	// both numbers to admit it placed fewer than were asked for.
	PinsRequested int              `json:"pinsRequested"`
	PinsSkipped   []cut.SkippedPin `json:"pinsSkipped"`
}

// Cut splits the given part with a bounded plane, then applies the given
// alignment pins to the cut face.
//
// A part that comes back not watertight is still added to the tree, flagged,
// and Warnings explains why. Hiding that would produce a model that looks right
// on screen and fails to print, which is the one outcome this application must
// never produce.
func (a *App) Cut(partID string, p PlaneInput, pins cut.PinSpec) (*CutOutcome, error) {
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
	return a.cutPart(partID, spec, pins)
}

// cutPart performs one cut on a leaf and applies pins to the cut face, then
// folds the result into the tree. Cut and AutoSplit both go through here —
// the only difference between a manual cut and an automatic one is where the
// Spec comes from — so the part tree, the undo history, pin placement and the
// watertightness reporting behave identically for both.
func (a *App) cutPart(partID string, spec cut.Spec, pins cut.PinSpec) (*CutOutcome, error) {
	var res *cut.Result
	var pinRes *cut.PinResult
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

		// Pins must be applied before the parts go into the tree, or the tree
		// would record the volumes and triangle counts of the bare parts.
		// ApplyPins re-checks both parts with meshcheck and rewrites
		// res.Part1Check/Part2Check itself, so res.Watertight() below already
		// reflects the pinned geometry, not the bare cut.
		pinRes, err = cut.ApplyPins(res, spec, pins)
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

	requested := 0
	if pins.Enabled {
		requested = pins.Count
	}
	return &CutOutcome{
		Tree:          a.view(),
		Watertight:    res.Watertight(),
		Warnings:      append(res.Warnings, pinRes.Warnings...),
		PinsPlaced:    pinRes.Placed,
		PinsRequested: requested,
		PinsSkipped:   pinRes.Skipped,
	}, nil
}

// maxAutoSplitCuts caps the AutoSplit loop so a pathological bed/model
// combination is reported rather than left to run indefinitely.
const maxAutoSplitCuts = 512

// AutoSplitOutcome reports what auto-splitting did and what it could not manage.
type AutoSplitOutcome struct {
	Tree        *TreeView `json:"tree"`
	CutsMade    int       `json:"cutsMade"`
	StillTooBig []string  `json:"stillTooBig"`
	Warnings    []string  `json:"warnings"`
}

// AutoSplit divides the open model until every piece fits the given build
// volume.
//
// Each pass finds one leaf that does not fit, plans cuts from that leaf's own
// mesh with cut.PlanAutoSplit, and applies only the first of those steps
// through cutPart — the same path Cut uses for a manual cut. It then loops
// and re-scans, rather than computing one plan up front and replaying every
// step of it.
//
// That matters because PlanAutoSplit predicts from bounding boxes. A shape
// that does not fill its box — a U, an L, a hollow shell — makes those
// predictions over-estimates, so a plan computed once and replayed across a
// tree of real meshes can let one branch's cut slice a sibling piece that
// only shares a predicted footprint. Replanning from each real leaf, every
// iteration, sidesteps that: every plan is derived from geometry that
// actually exists. It also means every cut AutoSplit makes lands as its own
// undoable step, exactly like a manual one.
//
// A leaf the planner or the cutter cannot handle is skipped and the loop moves
// on. The failure is per-leaf, so the recovery is too: abandoning the run would
// leave every piece behind that one in the traversal untouched, oversized and
// unexplained.
func (a *App) AutoSplit(bed cut.Bed, pins cut.PinSpec) (*AutoSplitOutcome, error) {
	out := &AutoSplitOutcome{}

	a.emit("cut:start")
	defer a.emit("cut:done")

	// Leaves this run has given up on. A leaf the planner or the cutter cannot
	// handle is a fact about that one leaf, not about the run: without this,
	// one awkward piece near the front of the traversal blocks every piece
	// behind it in the queue. Anything skipped still fails the final oversized
	// scan below, so it is named in StillTooBig either way.
	skip := make(map[string]bool)

	for {
		target, name, err := a.firstOversizedLeaf(bed, skip)
		if err != nil {
			return nil, err
		}
		if target == "" {
			break // everything fits, or everything left has already been given up on
		}

		mesh, ok := a.session.MeshFor(target)
		if !ok {
			return nil, fmt.Errorf("part %q has no geometry", target)
		}
		steps, err := cut.PlanAutoSplit(mesh, bed)
		if err != nil {
			return nil, err
		}
		if len(steps) == 0 {
			// The planner sees this leaf's real mesh; the scan above sees the
			// Min/Size pair the tree carries for the frontend, which round-trips
			// through floating point. They can disagree at the margin. Either way
			// there is no cut to make here.
			skip[target] = true
			continue
		}

		res, err := a.cutPart(target, steps[0].Spec, pins)
		if err != nil {
			// This leaf cannot be divided, but the others still can. Give up on
			// it alone rather than abandoning every piece behind it in the queue.
			// Earlier cuts have already committed to the tree, and the caller
			// needs to know the model changed even when a piece defeats us.
			out.Warnings = append(out.Warnings, fmt.Sprintf(
				"could not divide %s: %v", name, err))
			skip[target] = true
			continue
		}
		out.CutsMade++
		out.Warnings = append(out.Warnings, res.Warnings...)

		if out.CutsMade >= maxAutoSplitCuts {
			out.Warnings = append(out.Warnings, fmt.Sprintf(
				"stopped after %d cuts; the bed may be too small for this model", maxAutoSplitCuts))
			break
		}
	}

	tooBig, err := a.oversizedLeaves(bed)
	if err != nil {
		return nil, err
	}
	out.StillTooBig = tooBig
	out.Tree = a.view()
	return out, nil
}

// firstOversizedLeaf returns the id and name of the first leaf that does not fit
// bed and is not in skip, or "" once there is no such leaf.
func (a *App) firstOversizedLeaf(bed cut.Bed, skip map[string]bool) (string, string, error) {
	var id, name string
	err := a.session.WithTree(func(tr *Tree) error {
		for _, leaf := range tr.Leaves() {
			if !skip[leaf.ID] && !bed.Fits(leafBBox(leaf)) {
				id, name = leaf.ID, leaf.Name
				return nil
			}
		}
		return nil
	})
	return id, name, err
}

// oversizedLeaves names every leaf that does not fit bed.
func (a *App) oversizedLeaves(bed cut.Bed) ([]string, error) {
	var names []string
	err := a.session.WithTree(func(tr *Tree) error {
		for _, leaf := range tr.Leaves() {
			if !bed.Fits(leafBBox(leaf)) {
				names = append(names, leaf.Name)
			}
		}
		return nil
	})
	return names, err
}

// leafBBox reconstructs a part's bounding box from the Min/Size pair it
// carries for the frontend.
func leafBBox(p *Part) stl.BBox {
	return stl.BBox{
		Min: geom.Vec3{p.Min[0], p.Min[1], p.Min[2]},
		Max: geom.Vec3{p.Min[0] + p.Size[0], p.Min[1] + p.Size[1], p.Min[2] + p.Size[2]},
	}
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
