package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"stl-wizard/internal/cut"
	"stl-wizard/internal/geom"
	"stl-wizard/internal/meshcheck"
	"stl-wizard/internal/orient"
	"stl-wizard/internal/repair"
	"stl-wizard/internal/shells"
	"stl-wizard/internal/stl"
	"stl-wizard/internal/threemf"
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
	// Scale is the factors in force relative to the file as loaded, and OriginalSize
	// is the file's own size. The sidebar needs both: one to say what the current
	// scaling is, the other to work out the factor a target size in millimetres needs.
	Scale        [3]float64 `json:"scale"`
	OriginalSize [3]float64 `json:"originalSize"`
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
	// Bodies is how many disconnected solids the model holds. Free here because
	// repair labels surfaces anyway; counting it on every load would cost a second
	// full weld, about 20s on 1.75M triangles.
	Bodies int `json:"bodies"`
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
		v = viewOf(tr)
		return nil
	})
	return a.withScale(v)
}

// viewOf snapshots a tree the caller already holds the lock on. Calling view() from
// inside WithTree would deadlock, which is exactly what happened when cutPart's tail
// was moved under the lock.
func viewOf(tr *Tree) *TreeView {
	return &TreeView{
		ModelName:  tr.ModelName,
		Root:       clonePart(tr.Root),
		SelectedID: tr.SelectedID,
		CanUndo:    tr.CanUndo(),
	}
}

// withScale fills in the scaling fields, which live on the session rather than the tree.
func (a *App) withScale(v *TreeView) *TreeView {
	if v != nil {
		v.Scale = a.session.Scale()
		v.OriginalSize = a.session.OriginalSize()
	}
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
				Bodies:            res.Bodies,
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

// SeparateOutcome reports what separating a part into bodies found.
type SeparateOutcome struct {
	Tree   *TreeView `json:"tree"`
	Bodies int       `json:"bodies"`
	// Flagged names the bodies that are still not closed solids on their own, so a
	// body broken in its own right is not hidden by the pieces around it improving.
	Flagged  []string `json:"flagged"`
	Warnings []string `json:"warnings"`
}

// SeparateBodies replaces a leaf with one part per disconnected body.
//
// An STL has no notion of a body, so a file holding several solids arrives as one
// part and cuts and exports as one. This is the operation that fixes two solid
// bodies touching along an edge, which repair cannot: separating them changes no
// geometry at all, and each is then a closed solid.
//
// Finding one body is not an error. The button is cheap to press and the honest
// answer is that there is nothing to do, so the tree is left alone and Bodies says
// 1 — returning an error would make the common case look like a failure.
func (a *App) SeparateBodies(partID string) (*SeparateOutcome, error) {
	out := &SeparateOutcome{}
	err := a.session.WithTreeAndPlan(func(tr *Tree, pl *Plan) error {
		part := tr.Find(partID)
		if part == nil {
			return fmt.Errorf("no part with id %q", partID)
		}
		if !part.IsLeaf() {
			return fmt.Errorf("%q has already been split; select one of its pieces", part.Name)
		}

		eps := part.Mesh.Epsilon()
		bodies := shells.Split(part.Mesh, eps)
		out.Bodies = len(bodies)
		if len(bodies) < 2 {
			return nil
		}

		// Each body is checked in its own right. One that was flagged only because it
		// touched its neighbour comes back sound; one broken on its own stays flagged,
		// and has to keep saying so.
		sound := make([]bool, len(bodies))
		for i, b := range bodies {
			sound[i] = meshcheck.Check(b, eps).OK()
		}

		kids, err := tr.SplitMany(partID, bodies, sound)
		if err != nil {
			return err
		}
		for i, k := range kids {
			if !sound[i] {
				out.Flagged = append(out.Flagged, k.Name)
			}
		}
		// Recorded so Cut now reproduces it. Executing rebuilds from the mesh as
		// loaded, so a separation the plan did not know about would vanish.
		pl.Add(PlannedCut{Name: "Separate " + part.Name, Separate: true, Target: part.Name})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(out.Flagged) > 0 {
		out.Warnings = append(out.Warnings, fmt.Sprintf(
			"%d of the %d bodies are not closed solids on their own: %s",
			len(out.Flagged), out.Bodies, strings.Join(out.Flagged, ", ")))
	}
	out.Tree = a.view()
	return out, nil
}

// PlanView is the cut plan as the frontend sees it.
type PlanView struct {
	Cuts []PlannedCut `json:"cuts"`
	// Crowded counts planned cuts crossing the model in more than one place, and
	// StillTooBig counts fragments nothing could divide. Set only by PlanFitToPrinter.
	Crowded     int `json:"crowded"`
	StillTooBig int `json:"stillTooBig"`
}

// planView snapshots the plan. Copied rather than aliased for the same reason
// clonePart exists: Wails marshals after the lock has been dropped.
func (a *App) planView() *PlanView {
	v := &PlanView{}
	_ = a.session.WithPlan(func(p *Plan) error {
		v.Cuts = p.clone()
		return nil
	})
	return v
}

// AddPlane records a cut to make later, aimed at the selected part.
//
// Nothing is cut here. That is the point: both cut paths now propose, and only
// ExecutePlan acts.
func (a *App) AddPlane(p PlaneInput, pins cut.PinSpec) (*PlanView, error) {
	spec, err := specFrom(p)
	if err != nil {
		return nil, err
	}
	_ = spec // validated now rather than at execution, so a bad plane is refused where it is made

	err = a.session.WithTreeAndPlan(func(tr *Tree, pl *Plan) error {
		part := tr.Find(tr.SelectedID)
		if part == nil {
			return fmt.Errorf("no part is selected")
		}
		if !part.IsLeaf() {
			return fmt.Errorf("%q has already been split; select one of its pieces", part.Name)
		}
		pl.Add(PlannedCut{Plane: p, Pins: pins, Target: part.Name})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return a.planView(), nil
}

// PlanFitToPrinter appends one entry per cut needed to bring the model within the bed.
//
// The entries leave Target empty, meaning every leaf the bounded rectangle intersects.
// That is what a slab plan wants, and it is safe: cut.PlanAutoSplit bounds each step's
// rectangle to that step's own box, so a step cannot strike an unrelated sibling.
func (a *App) PlanFitToPrinter(bed cut.Bed) (*PlanView, error) {
	if err := bed.Valid(); err != nil {
		return nil, err
	}
	var plan fitPlan
	err := a.session.WithTree(func(tr *Tree) error {
		if tr.Root.Mesh == nil {
			return errors.New("the model has already been cut; undo first, or clear the plan and start again")
		}
		plan = planToFit(tr.Root.Mesh, tr.Root.Name, bed)
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = a.session.WithPlan(func(p *Plan) error {
		for _, s := range plan.cuts {
			p.Add(PlannedCut{Plane: planeFromSpec(s.spec), Target: s.target})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	v := a.planView()
	v.Crowded, v.StillTooBig = plan.crowded, plan.stillTooBig
	return v, nil
}

// plannedFit is one cut and the fragment it belongs to, by name.
type plannedFit struct {
	spec   cut.Spec
	target string
}

type fitPlan struct {
	cuts []plannedFit
	// crowded counts cuts crossing the model in more than one place. Still planned — a
	// branched model may have nowhere clean to cut — but worth looking at before printing.
	crowded int
	// stillTooBig counts fragments nothing could divide.
	stillTooBig int
}

// planToFit works out the cuts needed to bring a model within the bed, choosing each one
// by looking at the geometry rather than by halving a bounding box.
//
// It cuts as it plans, because cut.BestCut scores where a plane actually crosses the model
// and so needs the real fragment rather than a predicted box. That is the whole point:
// halving a box put a plane through the middle of a foot when the ankle a little further up
// is a fraction of the cross-section. The cutting done here is thrown away; only the specs
// and the names survive.
//
// Every cut records the NAME of the fragment it belongs to, and this is the part that took
// two attempts to get right. A plan is replayed by applying each entry to every leaf its
// rectangle crosses, so with no target a generous rectangle strikes siblings it was never
// meant for — a 90mm shell on a 35mm bed came back with 45mm pieces — while a rectangle
// bounded tightly enough to spare them grazes its own edges into slivers. No margin
// satisfies both. Naming the fragment sidesteps it: replay cuts the one part the cut was
// chosen for, so the rectangle may be as generous as it needs to be.
//
// The names mirror Tree.Split exactly — first piece "a", second "b" — so a name computed
// here identifies the same part when the plan runs. Breadth first, so a fragment's cut is
// always recorded after the cut that creates it.
func planToFit(root *stl.Mesh, rootName string, bed cut.Bed) fitPlan {
	type frag struct {
		mesh *stl.Mesh
		name string
	}
	var out fitPlan
	pending := []frag{{root, rootName}}

	for len(pending) > 0 && len(out.cuts) < maxAutoSplitCuts {
		f := pending[0]
		pending = pending[1:]
		if bed.Fits(f.mesh.BBox()) {
			continue // this fragment is done
		}

		spec, sec, ok := cut.BestCut(f.mesh, bed)
		if !ok {
			// Nothing scored, so fall back to halving the worst axis. BestCut returns
			// false both for a part that already fits and for one it could not place a cut
			// on; the Fits check above tells them apart, so reaching here means a fragment
			// that does not fit and must still be divided rather than dropped.
			steps, err := cut.PlanAutoSplit(f.mesh, bed)
			if err != nil || len(steps) == 0 {
				out.stillTooBig++
				continue
			}
			spec, sec = steps[0].Spec, cut.Section{Loops: 1}
		}

		res, err := cut.Split(f.mesh, spec)
		if err != nil {
			out.stillTooBig++
			continue
		}
		out.cuts = append(out.cuts, plannedFit{spec: spec, target: f.name})
		if sec.Loops > 1 {
			out.crowded++
		}
		pending = append(pending,
			frag{res.Part1, f.name + "a"},
			frag{res.Part2, f.name + "b"})
	}
	// Anything still queued when the cap was reached does not fit.
	out.stillTooBig += len(pending)
	return out
}

func (a *App) RenamePlane(id, name string) (*PlanView, error) {
	if err := a.session.WithPlan(func(p *Plan) error { return p.Rename(id, name) }); err != nil {
		return nil, err
	}
	return a.planView(), nil
}

func (a *App) DeletePlane(id string) (*PlanView, error) {
	if err := a.session.WithPlan(func(p *Plan) error { return p.Delete(id) }); err != nil {
		return nil, err
	}
	return a.planView(), nil
}

func (a *App) SetPlaneEnabled(id string, on bool) (*PlanView, error) {
	if err := a.session.WithPlan(func(p *Plan) error { return p.SetEnabled(id, on) }); err != nil {
		return nil, err
	}
	return a.planView(), nil
}

// UpdatePlane re-positions an entry from the gizmo.
func (a *App) UpdatePlane(id string, p PlaneInput, pins cut.PinSpec) (*PlanView, error) {
	if _, err := specFrom(p); err != nil {
		return nil, err
	}
	if err := a.session.WithPlan(func(pl *Plan) error { return pl.Update(id, p, pins) }); err != nil {
		return nil, err
	}
	return a.planView(), nil
}

// ReorderPlan rearranges the plan. Order decides what each entry has to cut, so this
// changes results and not merely the display.
func (a *App) ReorderPlan(ids []string) (*PlanView, error) {
	if err := a.session.WithPlan(func(p *Plan) error { return p.Reorder(ids) }); err != nil {
		return nil, err
	}
	return a.planView(), nil
}

func (a *App) ClearPlan() (*PlanView, error) {
	if err := a.session.WithPlan(func(p *Plan) error { p.Clear(); return nil }); err != nil {
		return nil, err
	}
	return a.planView(), nil
}

// Plan returns the current plan, for the frontend to render on load.
func (a *App) Plan() *PlanView { return a.planView() }

// ExecuteOutcome is what Cut now produced.
type ExecuteOutcome struct {
	Tree       *TreeView `json:"tree"`
	Plan       *PlanView `json:"plan"`
	CutsMade   int       `json:"cutsMade"`
	Watertight bool      `json:"watertight"`
	Warnings   []string  `json:"warnings"`
	// Skipped names entries that could not run, with the reason. An entry whose
	// target was produced by an entry since deleted has nothing to cut, and saying so
	// is the difference between a plan the user can fix and one that quietly does less
	// than it says.
	Skipped []string `json:"skipped"`
	// GapsClosed counts pieces a cut left open that repair then closed.
	GapsClosed int `json:"gapsClosed"`
	// Made is one report per entry that cut something, carrying that entry's own pin
	// spec. Pins are per entry, so the count placed and the wording — peg or dowel —
	// have to be reported against the entry that asked for them rather than summed
	// into one number that describes none of them.
	Made []PlanCutReport `json:"made"`
}

// PlanCutReport is what one entry of the plan did.
type PlanCutReport struct {
	Name          string           `json:"name"`
	Cuts          int              `json:"cuts"`
	Pins          cut.PinSpec      `json:"pins"`
	PinsPlaced    int              `json:"pinsPlaced"`
	PinsRequested int              `json:"pinsRequested"`
	PinsSkipped   []cut.SkippedPin `json:"pinsSkipped"`
	// GapsClosed counts pieces the cut left open that repair then closed. Cutting
	// through a pin's cylinder leaves a small rim the cap triangulator does not pair.
	GapsClosed int `json:"gapsClosed"`
}

// ExecutePlan rebuilds the part tree from the plan.
//
// The tree is discarded and rebuilt from the mesh as loaded, so the result is the
// plan's result and not a mixture of it with whatever was cut before. That is what
// makes editing an entry and pressing Cut now again do what it looks like it does.
func (a *App) ExecutePlan() (*ExecuteOutcome, error) {
	out := &ExecuteOutcome{Watertight: true}

	// One event pair for the whole run, however many entries it holds. Fifty entries
	// must not produce fifty pairs — AutoSplit already learnt that.
	a.emit("cut:start")
	defer a.emit("cut:done")

	err := a.session.Replay(func(tr *Tree, p *Plan) error {
		enabled := p.Enabled()
		if len(enabled) == 0 {
			return errors.New("the cut plan is empty; add a plane first")
		}

		for _, entry := range enabled {
			if entry.Separate {
				n, err := separateLocked(tr, entry.Target)
				if err != nil {
					out.Skipped = append(out.Skipped, fmt.Sprintf("%s: %v", entry.Name, err))
					continue
				}
				out.CutsMade += n
				out.Made = append(out.Made, PlanCutReport{Name: entry.Name, Cuts: n})
				continue
			}

			spec, err := specFrom(entry.Plane)
			if err != nil {
				out.Skipped = append(out.Skipped, fmt.Sprintf("%s: %v", entry.Name, err))
				continue
			}

			targets := planTargets(tr, entry.Target)
			if len(targets) == 0 {
				out.Skipped = append(out.Skipped, fmt.Sprintf(
					"%s: no part called %q is left to cut — an earlier planned cut that produced it "+
						"may have been deleted or disabled", entry.Name, entry.Target))
				continue
			}

			report := PlanCutReport{Name: entry.Name, Pins: entry.Pins}
			made := 0
			for _, id := range targets {
				res, err := a.cutPartLocked(tr, id, spec, entry.Pins)
				if err != nil {
					// A wildcard entry is expected to miss most leaves; only a named
					// target failing is worth reporting.
					if entry.Target != "" {
						out.Skipped = append(out.Skipped, fmt.Sprintf("%s: %v", entry.Name, err))
					}
					continue
				}
				made++
				out.CutsMade++
				if !res.Watertight {
					out.Watertight = false
				}
				out.Warnings = append(out.Warnings, res.Warnings...)
				out.GapsClosed += res.GapsClosed
				report.PinsPlaced += res.PinsPlaced
				report.PinsRequested += res.PinsRequested
				report.PinsSkipped = append(report.PinsSkipped, res.PinsSkipped...)
			}
			if made > 0 {
				report.Cuts = made
				out.Made = append(out.Made, report)
			}
			if made == 0 && entry.Target == "" {
				out.Skipped = append(out.Skipped, fmt.Sprintf(
					"%s: its rectangle does not cross any piece", entry.Name))
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	out.Tree = a.view()
	out.Plan = a.planView()
	return out, nil
}

// separateLocked splits a named part into its bodies, with the lock already held.
// Shared by SeparateBodies and by the plan, so both produce the same tree.
func separateLocked(tr *Tree, name string) (int, error) {
	var part *Part
	for _, p := range tr.Leaves() {
		if p.Name == name {
			part = p
		}
	}
	if part == nil {
		return 0, fmt.Errorf("no part called %q is left to separate", name)
	}
	eps := part.Mesh.Epsilon()
	bodies := shells.Split(part.Mesh, eps)
	if len(bodies) < 2 {
		return 0, nil // one body: nothing to do, and not an error
	}
	sound := make([]bool, len(bodies))
	for i, b := range bodies {
		sound[i] = meshcheck.Check(b, eps).OK()
	}
	if _, err := tr.SplitMany(part.ID, bodies, sound); err != nil {
		return 0, err
	}
	return 1, nil
}

// planTargets returns the ids of the leaves an entry applies to. An empty name means
// every leaf, and Split itself decides which of them the rectangle actually crosses.
func planTargets(tr *Tree, name string) []string {
	var out []string
	for _, p := range tr.Leaves() {
		if name == "" || p.Name == name {
			out = append(out, p.ID)
		}
	}
	return out
}

// PlateOutcome reports what the 3MF export produced.
type PlateOutcome struct {
	Cancelled bool   `json:"cancelled"`
	Path      string `json:"path"`
	Plates    int    `json:"plates"`
	// Oriented describes what the orientation search did to each part, so the user
	// can see whether it found anything and does not have to trust that it did.
	Oriented []PlateReport `json:"oriented"`
	Warnings []string      `json:"warnings"`
}

// PlateReport is one part's placement.
type PlateReport struct {
	Name         string  `json:"name"`
	Plate        int     `json:"plate"`
	OverhangArea float64 `json:"overhangArea"`
	BaseArea     float64 `json:"baseArea"`
	Height       float64 `json:"height"`
	Rotated      bool    `json:"rotated"`
	FitsPlate    bool    `json:"fitsPlate"`
}

// plateGap separates plates in world coordinates.
//
// 40mm because that is what Bambu Studio itself uses: a reference project it exported
// placed plate 1's items around x=95 and plate 2's around x=335 on a 200mm bed, a
// stride of 240. Matching it keeps the drawn layout aligned with where the slicer
// expects its plates to be.
//
// The stride only affects appearance. What actually assigns a part to a plate is the
// model_instance entry in model_settings.config, which was verified to survive a
// round trip through Bambu Studio — see docs/manual-verification.md. Note that
// Bambu's *command line* re-arranges on import unless given --arrange 0, and that
// repacks everything onto plate 1; the GUI honours the stored layout.
const plateGap = 40.0

// ExportPlates writes every part to its own build plate in one 3MF, each part
// oriented to need as little support as it can.
func (a *App) ExportPlates(bed cut.Bed) (*PlateOutcome, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save 3MF project",
		DefaultFilename: "plates.3mf",
		Filters:         []runtime.FileFilter{{DisplayName: "3MF projects (*.3mf)", Pattern: "*.3mf"}},
	})
	if err != nil {
		return nil, fmt.Errorf("could not open the save dialog: %w", err)
	}
	if path == "" {
		return &PlateOutcome{Cancelled: true}, nil
	}
	return a.exportPlatesTo(path, bed)
}

// ExportPlatesTo is ExportPlates once a path is known.
//
// Bound, not merely unexported, for the same reason as OpenPath: the save dialog
// belongs to the Wails window and cannot be answered from a browser tab, so without
// this the export could not be driven by a test at all.
func (a *App) ExportPlatesTo(path string, bed cut.Bed) (*PlateOutcome, error) {
	return a.exportPlatesTo(path, bed)
}

func (a *App) exportPlatesTo(path string, bed cut.Bed) (*PlateOutcome, error) {
	if err := bed.Valid(); err != nil {
		return nil, err
	}

	// Snapshot under the lock, orient and write outside it. Orientation is seconds
	// of arithmetic on a large part; holding the session mutex through it would
	// freeze cutting, undo and the viewer for the duration.
	type pending struct {
		name string
		mesh *stl.Mesh
	}
	var todo []pending
	err := a.session.WithTree(func(tr *Tree) error {
		for _, p := range tr.Leaves() {
			todo = append(todo, pending{name: p.Name, mesh: p.Mesh})
		}
		if len(todo) == 0 {
			return errors.New("there is nothing to export")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	out := &PlateOutcome{Path: path, Plates: len(todo)}
	plates := make([]threemf.Plate, 0, len(todo))

	for i, p := range todo {
		res := orient.Best(p.mesh, orient.Spec{})

		// The rotated extent decides both where the part sits and whether it fits.
		lo, hi := rotatedBounds(p.mesh, res.Rotation)
		size := hi.Sub(lo)

		// Plates are laid out in a row along X, each part centred on its own.
		centreX := float64(i)*(bed.X+plateGap) + bed.X/2
		centreY := bed.Y / 2
		tx := centreX - (lo[0]+hi[0])/2
		ty := centreY - (lo[1]+hi[1])/2
		tz := -lo[2] // sit on the plate

		fits := size[0] <= bed.X && size[1] <= bed.Y && size[2] <= bed.Z
		out.Oriented = append(out.Oriented, PlateReport{
			Name: p.name, Plate: i + 1,
			OverhangArea: res.OverhangArea, BaseArea: res.BaseArea, Height: res.Height,
			Rotated:   res.Rotation != [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1},
			FitsPlate: fits,
		})
		if !fits {
			out.Warnings = append(out.Warnings, fmt.Sprintf(
				"%s measures %.1f x %.1f x %.1f mm once oriented and does not fit a %.0f x %.0f x %.0f bed; "+
					"it is on its plate anyway",
				p.name, size[0], size[1], size[2], bed.X, bed.Y, bed.Z))
		}

		// 3MF applies a transform to a row vector, p' = p·M, so the stored 3x3 is the
		// transpose of orient's row-major rotation.
		r := res.Rotation
		plates = append(plates, threemf.Plate{
			Name: p.name,
			Mesh: p.mesh,
			Transform: [12]float64{
				r[0], r[3], r[6],
				r[1], r[4], r[7],
				r[2], r[5], r[8],
				tx, ty, tz,
			},
		})
	}

	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	// The bed goes into the file, so the slicer lays the project out on the printer the
	// user chose rather than substituting its own default.
	//
	// One filament for now: the export layer takes a table and a per-part assignment, so
	// multi-colour is a matter of filling those in from the UI.
	proj := threemf.Project{
		Bed:       [3]float64{bed.X, bed.Y, bed.Z},
		Filaments: []threemf.Filament{{Type: "PLA"}},
	}
	if err := threemf.Write(f, plates, proj); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return out, nil
}

// rotatedBounds returns the extent of m after applying a row-major rotation.
func rotatedBounds(m *stl.Mesh, r [9]float64) (lo, hi geom.Vec3) {
	lo = geom.Vec3{math.Inf(1), math.Inf(1), math.Inf(1)}
	hi = geom.Vec3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, t := range m.Tris {
		for _, v := range [3]geom.Vec3{t.A, t.B, t.C} {
			q := geom.Vec3{
				r[0]*v[0] + r[1]*v[1] + r[2]*v[2],
				r[3]*v[0] + r[4]*v[1] + r[5]*v[2],
				r[6]*v[0] + r[7]*v[1] + r[8]*v[2],
			}
			for k := 0; k < 3; k++ {
				lo[k] = math.Min(lo[k], q[k])
				hi[k] = math.Max(hi[k], q[k])
			}
		}
	}
	return lo, hi
}

// ScaleModel scales the loaded model.
//
// Factors are relative to the file as loaded, so applying the same ones twice is
// idempotent and 1,1,1 returns exactly to the original. The percentage and millimetre
// arithmetic is the frontend's: it has the mode, the fields, and both Scale and
// OriginalSize to work from.
//
// The tree is reset and the plan cleared, because both describe the model at its previous
// size.
func (a *App) ScaleModel(factors [3]float64) (*TreeView, error) {
	for i, f := range factors {
		axis := string(rune('X' + i))
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("the %s scale must be a finite number, got %v", axis, f)
		}
		// Zero flattens the model; a negative factor mirrors it, which leaves a mesh
		// that is consistently wound and encloses a negative volume — it reads as sound
		// and prints as nothing. Neither is obliged.
		if f <= 0 {
			return nil, fmt.Errorf("the %s scale must be greater than zero, got %v", axis, f)
		}
	}
	if err := a.session.Rescale(factors); err != nil {
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

// specFrom turns the gizmo's report into a validated cut spec. Shared by Cut and by
// the plan, so a plane refused in one is refused in the other.
func specFrom(p PlaneInput) (cut.Spec, error) {
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
		return cut.Spec{}, err
	}
	return spec, nil
}

// planeFromSpec is the reverse, for turning a planned auto-split step into an entry the
// gizmo can also load and show.
func planeFromSpec(s cut.Spec) PlaneInput {
	return PlaneInput{
		Origin: [3]float64{s.Origin[0], s.Origin[1], s.Origin[2]},
		Normal: [3]float64{s.Normal[0], s.Normal[1], s.Normal[2]},
		U:      [3]float64{s.U[0], s.U[1], s.U[2]},
		V:      [3]float64{s.V[0], s.V[1], s.V[2]},
		Width:  s.Width,
		Height: s.Height,
	}
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
	// GapsClosed counts pieces the cut left open that repair then closed. Cutting
	// through a pin's cylinder leaves a small rim the cap triangulator does not pair.
	GapsClosed int `json:"gapsClosed"`
}

// Cut splits the given part with a bounded plane, then applies the given
// alignment pins to the cut face.
//
// A part that comes back not watertight is still added to the tree, flagged,
// and Warnings explains why. Hiding that would produce a model that looks right
// on screen and fails to print, which is the one outcome this application must
// never produce.
func (a *App) Cut(partID string, p PlaneInput, pins cut.PinSpec) (*CutOutcome, error) {
	spec, err := specFrom(p)
	if err != nil {
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
	var out *CutOutcome
	err := a.session.WithTree(func(tr *Tree) error {
		var err error
		out, err = a.cutPartLocked(tr, partID, spec, pins)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// cutPartLocked is cutPart with the session lock already held, so ExecutePlan can make
// every cut of a plan inside one lock — a half-rebuilt tree must never be visible.
func (a *App) cutPartLocked(tr *Tree, partID string, spec cut.Spec, pins cut.PinSpec) (*CutOutcome, error) {
	var res *cut.Result
	var pinRes *cut.PinResult
	err := func(tr *Tree) error {
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
	}(tr)
	if err != nil {
		return nil, err
	}

	requested := 0
	if pins.Enabled {
		requested = pins.Count
	}
	warnings := append(res.Warnings, pinRes.Warnings...)

	// A cut that leaves a piece open gets one repair attempt.
	//
	// Cutting through a pin's cylinder leaves a rim of about three edges that the cap
	// triangulator does not pair — the defect measured at roughly half of all pieces
	// when auto-splitting with pins on every cut. That is a real weakness in the
	// triangulation and it is still there; what this does is close the gap afterwards,
	// which turns an unprintable piece into a printable one for the price of three
	// triangles and a volume change around a ten-millionth.
	//
	// Reported, never silent: the piece is not the one the cut produced, and a user
	// checking why a volume moved has to be able to find out. A piece repair cannot
	// close stays flagged exactly as before.
	repairs := repairCutParts(tr, res, &warnings)

	return &CutOutcome{
		Tree:          viewOf(tr),
		Watertight:    res.Watertight(),
		Warnings:      warnings,
		PinsPlaced:    pinRes.Placed,
		PinsRequested: requested,
		PinsSkipped:   pinRes.Skipped,
		GapsClosed:    repairs,
	}, nil
}

// repairCutParts tries to close any piece the cut left open, and returns how many it
// managed. The tree's record of each part is refreshed, so what the sidebar shows is
// the geometry that will actually be exported.
func repairCutParts(tr *Tree, res *cut.Result, warnings *[]string) int {
	closed := 0
	for i, part := range []*stl.Mesh{res.Part1, res.Part2} {
		chk := res.Part1Check
		if i == 1 {
			chk = res.Part2Check
		}
		if chk.OK() {
			continue
		}
		eps := part.Epsilon()
		before := part.Volume()
		r := repair.Repair(part, eps)
		after := meshcheck.Check(part, eps)
		if !after.OK() {
			continue // left as it was; the existing warning already says so
		}
		closed++
		if i == 0 {
			res.Part1Check = after
		} else {
			res.Part2Check = after
		}
		*warnings = append(*warnings, fmt.Sprintf(
			"the cut left one piece open by %d edge(s); the gap was closed with %d triangle(s), "+
				"changing that piece's volume by %.4gmm³ out of %.6g",
			chk.OpenEdges, r.TrianglesAdded, part.Volume()-before, before))
	}
	if closed > 0 {
		// Only the pieces repair actually rewrote; the rest of the tree is unchanged.
		tr.RefreshLeaves(res.Part1, res.Part2)
	}
	return closed
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
	// GapsClosed counts pieces a cut left open that repair then closed. Pins are what
	// provoke it: a cut through a pin's cylinder leaves a rim the cap triangulator does
	// not pair.
	GapsClosed int `json:"gapsClosed"`
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
		out.GapsClosed += res.GapsClosed

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
