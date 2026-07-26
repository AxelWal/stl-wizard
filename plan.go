package main

import (
	"fmt"
	"strings"

	"stl-wizard/internal/cut"
)

// PlannedCut is one cut the user has asked for but not yet made.
type PlannedCut struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Plane and Pins are exactly what Cut would have been given.
	Plane PlaneInput  `json:"plane"`
	Pins  cut.PinSpec `json:"pins"`
	// Target is the NAME of the part to cut, or "" for every leaf whose geometry the
	// bounded rectangle actually intersects.
	//
	// A name rather than an id because the tree is rebuilt from scratch on every
	// execution: naming is deterministic, so "wholea" means the same piece each time,
	// where an id would be freshly minted and match nothing. Auto-split's entries
	// leave it empty, which is both what a slab plan wants and safe — every step's
	// rectangle is already bounded to its own box.
	Target string `json:"target"`
	// Separate makes this entry split its target into its disconnected bodies rather
	// than cut it with a plane.
	//
	// It belongs in the plan because the plan is the source of truth: executing rebuilds
	// the tree from the mesh as loaded, so a separation made outside the plan would be
	// silently discarded the next time Cut now ran. Anything that shapes the tree has to
	// be an entry, or the two features fight and the user loses work without being told.
	Separate bool `json:"separate"`
	// Enabled lets an entry be parked without losing where its plane was.
	Enabled bool `json:"enabled"`
}

// Plan is the ordered list of cuts to make when Cut now is pressed.
//
// The plan is the source of truth and the part tree is derived from it, so editing an
// entry and executing again gives the edited plan's result rather than a mixture of
// the old cuts and the new ones.
type Plan struct {
	Cuts []PlannedCut `json:"cuts"`

	nextID  int
	nextNum int
}

// Add appends an entry, giving it a numbered default name.
func (p *Plan) Add(c PlannedCut) *PlannedCut {
	p.nextID++
	p.nextNum++
	c.ID = fmt.Sprintf("c%d", p.nextID)
	if strings.TrimSpace(c.Name) == "" {
		c.Name = fmt.Sprintf("Cut %d", p.nextNum)
	}
	c.Enabled = true
	p.Cuts = append(p.Cuts, c)
	return &p.Cuts[len(p.Cuts)-1]
}

// Find returns the entry with the given id, or nil.
func (p *Plan) Find(id string) *PlannedCut {
	for i := range p.Cuts {
		if p.Cuts[i].ID == id {
			return &p.Cuts[i]
		}
	}
	return nil
}

// Delete removes an entry. Numbering is never reused: two entries called "Cut 3" would
// be indistinguishable in the list, and the number is a label rather than a position.
func (p *Plan) Delete(id string) error {
	for i := range p.Cuts {
		if p.Cuts[i].ID == id {
			p.Cuts = append(p.Cuts[:i], p.Cuts[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("no planned cut with id %q", id)
}

func (p *Plan) Rename(id, name string) error {
	c := p.Find(id)
	if c == nil {
		return fmt.Errorf("no planned cut with id %q", id)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("a planned cut needs a name")
	}
	c.Name = name
	return nil
}

func (p *Plan) SetEnabled(id string, on bool) error {
	c := p.Find(id)
	if c == nil {
		return fmt.Errorf("no planned cut with id %q", id)
	}
	c.Enabled = on
	return nil
}

// Update replaces an entry's geometry, for re-positioning it from the gizmo. The name
// and the enabled flag are deliberately left alone: moving a plane is not renaming it.
func (p *Plan) Update(id string, plane PlaneInput, pins cut.PinSpec) error {
	c := p.Find(id)
	if c == nil {
		return fmt.Errorf("no planned cut with id %q", id)
	}
	c.Plane = plane
	c.Pins = pins
	return nil
}

// Reorder rearranges the plan to the given order of ids.
//
// The whole order is sent rather than "move this one to index n" because order is what
// the caller is editing: a drag produces a new arrangement, and validating that against
// the set it started from catches a stale list — one built before a delete landed —
// instead of silently reordering into something the user never saw.
func (p *Plan) Reorder(ids []string) error {
	if len(ids) != len(p.Cuts) {
		return fmt.Errorf("got %d ids for %d planned cuts; the list is out of date", len(ids), len(p.Cuts))
	}
	byID := make(map[string]PlannedCut, len(p.Cuts))
	for _, c := range p.Cuts {
		byID[c.ID] = c
	}
	out := make([]PlannedCut, 0, len(ids))
	for _, id := range ids {
		c, ok := byID[id]
		if !ok {
			return fmt.Errorf("no planned cut with id %q", id)
		}
		delete(byID, id) // a repeated id would otherwise duplicate an entry and lose another
		out = append(out, c)
	}
	p.Cuts = out
	return nil
}

func (p *Plan) Clear() {
	p.Cuts = nil
	// nextNum is not reset: a plan cleared and refilled against the same model should
	// not hand out a name the user has already seen used for something else.
}

// Reset empties the plan and starts numbering again, for a newly loaded model. A fresh
// model is a fresh start, so its first cut is "Cut 1" rather than continuing a count
// from whatever was open before.
func (p *Plan) Reset() {
	p.Cuts = nil
	p.nextNum = 0
}

// Enabled returns the entries that will actually run, in order.
func (p *Plan) Enabled() []PlannedCut {
	var out []PlannedCut
	for _, c := range p.Cuts {
		if c.Enabled {
			out = append(out, c)
		}
	}
	return out
}

// clone deep-copies the plan for handing to the frontend, for the same reason
// clonePart exists: Wails marshals after the lock has been dropped.
func (p *Plan) clone() []PlannedCut {
	// Never nil. A nil slice marshals as JSON null, and the frontend then reads
	// plan.cuts.some(...) off null and the whole module dies on load — an empty plan
	// is an empty list, not an absent one.
	out := make([]PlannedCut, len(p.Cuts))
	copy(out, p.Cuts)
	return out
}
