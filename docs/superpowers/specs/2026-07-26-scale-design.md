# Scaling a model

Scale the loaded model, by percentage or to a target size, uniformly or per axis.

## Factors are absolute

The session keeps the mesh **as it arrived** plus a scale, and the working mesh is the
product. The alternative — multiplying the mesh each time — makes typing 200% twice give
400%, makes "back to 100%" a division that never quite returns, and accumulates float
error over a few adjustments.

So `Session` gains `scale [3]float64`, defaulting to `{1,1,1}`, and every tree is built
from `origin` scaled by it. `ExecutePlan`'s `Replay` already rebuilds from `origin`, so
it picks the scale up for free.

```go
// ScaleModel scales the loaded model. Factors are relative to the file as loaded, so
// applying the same factors twice is idempotent.
func (a *App) ScaleModel(factors [3]float64) (*TreeView, error)
```

Validation in Go: each factor finite and **strictly positive**. A negative factor mirrors
the model and inverts every normal, turning a solid inside out — `meshcheck` would report
it as consistently wound and the volume would come out negative, which is exactly the
silent-bad-part outcome this application exists to rule out.

`TreeView` gains `Scale` and `OriginalSize` so the sidebar can say what the current scale
is and work out the factor a target size in millimetres needs.

## Geometry

```go
// Scaled returns m with every vertex multiplied componentwise.
func (m *Mesh) Scaled(f [3]float64) *Mesh
```

A copy rather than in place: `origin` must stay the mesh as loaded.

Positive factors preserve winding, so a scaled closed solid stays closed and its volume
is the original times `fx*fy*fz` exactly. Both are worth asserting — the volume identity
is a strong check that every vertex was transformed and none was missed, and
watertightness after a *non-uniform* scale is the one that would catch a normal being
transformed as if it were a point.

## Scaling clears the cut plan

A plane at y=25 means something different once the model is twice the size, and for a
non-uniform scale a bounded rectangle does not stay a rectangle unless it happens to
align with the scale axes — a plane's normal transforms by the inverse transpose, not by
the factors.

So scaling clears the plan and says so, the same rule as loading a model. That is also
arguably right rather than merely simple: a fit-to-printer plan for a 300mm model is
meaningless for a 600mm one, so it would want re-planning anyway.

Scaling resets the tree to a single part, so there is nothing left to undo.

## UI

A **Scale** panel below Parts:

- a mode select: `Percent` or `Millimetres`
- a `Keep proportions` tick, on by default
- three fields, X Y Z, showing either the current percentages or the current size
- **Apply** and **Reset to 100%**
- a line stating the size the model will end up

With proportions kept, editing any one field sets all three factors from that axis, so a
target width of 100mm on a 30x40x10 model gives 333% on every axis and a final size of
100 x 133.3 x 33.3.

The percentage and millimetre arithmetic lives in the frontend, which is where the mode
and the fields are; Playwright covers it.

## Testing

Go, `stl`:

- Volume after scaling is the original times the product of the factors, exactly.
- A scaled cube is still a closed solid, including under a non-uniform scale.
- The bounding box scales componentwise.
- The original mesh is untouched.

Go, app:

- Scaling changes the reported size and volume.
- Applying the same factors twice is idempotent — the second call changes nothing.
- Factors of 1 restore the original size after a scale.
- A zero, negative, NaN or infinite factor is refused, and refusing changes nothing.
- Scaling clears the plan.
- Scaling resets the tree, so a cut made before it is gone and there is nothing to undo.
- Cutting a scaled model gives pieces whose volumes sum to the scaled whole.
- `TreeView.Scale` and `OriginalSize` report what was asked for.

Playwright: the panel appears with a model; entering 200% and applying doubles the
reported size; keep-proportions makes one field drive all three; millimetre mode reaches
a stated target size; Reset returns to the original; a nonsense factor is refused with a
readable message; scaling clears a plan and says so.

Each test written to fail first, then the production code broken on purpose to confirm
the test notices. See CLAUDE.md's standing rules.
