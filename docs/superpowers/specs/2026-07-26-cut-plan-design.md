# A cut plan you can edit before anything is cut

Today both cut paths act immediately. Manual cutting splits the selected part the
moment the button is pressed; auto-split loops, cutting until everything fits. Neither
gives you a chance to look at what is about to happen.

This makes both of them *propose* cuts into a named, editable list. Nothing is split
until **Cut now**.

## The plan is the source of truth

The part tree is **derived** from the plan, not edited alongside it. Cut now discards
the tree and rebuilds it from the originally loaded mesh, applying every enabled entry
in order.

That is the decision everything else follows from:

- Editing an entry and pressing Cut now again gives the result of the edited plan, not
  a mixture of the old cuts and the new ones.
- The list and the tree cannot disagree, because one produces the other.
- Undo keeps working on the tree exactly as now; the plan is separate state.

The cost is that Cut now re-cuts from scratch every time. At roughly 5µs per triangle
a 50-entry plan on a large model is minutes, and that is the honest trade: the plan is
reviewed and edited cheaply, then executed once.

## An entry

```go
type PlannedCut struct {
    ID      string      `json:"id"`
    Name    string      `json:"name"`     // "Cut 1" by default, renameable
    Plane   PlaneInput  `json:"plane"`
    Pins    cut.PinSpec `json:"pins"`
    Target  string      `json:"target"`   // a part NAME, or "" for every leaf it hits
    Enabled bool        `json:"enabled"`
}
```

**`Target` is a part name, not an id.** Rebuilding is deterministic, so names — `whole`,
`wholea`, `wholeb`, `whole1` — are reproducible across executions. An id would be
freshly minted on every rebuild and match nothing.

A manual cut records whichever part was selected. Auto-split leaves `Target` empty,
meaning *every leaf this bounded rectangle actually intersects*. That is the right
semantics for a slab plan and it is safe: `cut.PlanAutoSplit` already bounds each
step's rectangle to that step's own box, so a step cannot strike an unrelated sibling.

If an entry is deleted, a later entry naming one of its products has a target that no
longer exists. That entry is **skipped and named in the report**, never silently
dropped.

## Bound methods

    AddPlane(plane, pins)        append an entry aimed at the selected part
    PlanFitToPrinter(bed)        append one entry per cut.PlanAutoSplit step, Target ""
    RenamePlane(id, name)
    DeletePlane(id)
    SetPlaneEnabled(id, on)
    UpdatePlane(id, plane, pins) re-position an entry from the gizmo
    ClearPlan()
    ExecutePlan()                rebuild the tree from the plan

`Cut` stays exactly as it is. `ExecutePlan` calls the same `cutPart` path, so pins,
watertightness reporting and the part tree behave identically, and the Go tests that
cover cutting do not have to change. What changes is which method the button calls.

`ExecutePlan` brackets the whole run in one `cut:start`/`cut:done` pair, as `AutoSplit`
already does — fifty entries must not produce fifty event pairs.

## Session state

`Session` gains the plan and the originally loaded mesh. Cut now needs the original to
rebuild from: the tree's root releases its mesh into the undo history the moment it is
split, so the tree cannot be the source.

Loading a model clears the plan. A plan aimed at part names from a different model
would target things that do not exist.

## UI

A **Cut plan** panel listing entries in order, each with:

- its name, editable in place
- a tick to enable or disable it
- a delete button

Clicking an entry **selects** it: its plane loads into the gizmo and is shown in the
viewport, so the plane can be moved and then saved back over that entry with **Save
plane**. Only the selected entry's plane is drawn — a fifty-entry plan drawn all at
once hides the model.

The Cut button becomes **Add plane**. **Cut now** executes. **Split to fit** becomes
**Plan fit to printer**, appending entries rather than cutting.

## Testing

Go:

- Adding a plane changes nothing about the tree.
- Default names are numbered, and stay unique after deletions.
- Executing an empty plan is refused rather than producing an empty tree.
- Executing one entry gives exactly what `Cut` gives for the same plane — the same
  volumes, the same watertightness.
- Executing twice from the same plan gives the same tree, which is what makes the plan
  the source of truth.
- Editing an entry and re-executing gives the edited result, not the old one.
- Disabling an entry omits its cut.
- Deleting an entry whose product a later entry targets: that later entry is reported
  as skipped, and the rest of the plan still runs.
- An entry with `Target` "" cuts every leaf its rectangle hits and no others.
- `PlanFitToPrinter` appends entries and cuts nothing until executed.
- Loading a model clears the plan.
- One `cut:start`/`cut:done` pair for a whole execution.

Playwright: the list appears and is ordered; a default name is numbered; renaming
sticks; deleting removes an entry; disabling omits the cut; clicking an entry shows
its plane and loads it into the gizmo; moving the gizmo and saving updates the entry;
Cut now produces the parts and nothing before it does.

The harness's `cut()` helper becomes add-plane-then-execute, so the 61 existing tests
keep testing what they were written to test rather than being rewritten around the new
flow.

Each test written to fail first, then the production code broken on purpose to confirm
the test notices. See CLAUDE.md's standing rules.
