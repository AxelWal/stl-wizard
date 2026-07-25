# Manual verification

The Go side is covered by `go test ./...`, including guard tests in
`frontend_test.go` that catch broken imports (see the README's Testing
section). The window itself is not covered by any automated test. Run this
list before any release build, and after any change to `frontend/`.

**As of this writing, no line in this checklist has been run.** Every
frontend task in this project was implemented and built but never opened in
a window. Treat this document as the outstanding work, not as a record of
what already passed.

Prepare fixtures:

    go run ./cmd/genfixture -name u -out testdata/u.stl
    go run ./cmd/genfixture -name tube -out testdata/tube.stl
    go run ./cmd/genfixture -name sphere -out testdata/sphere.stl

Build and run with `wails dev -tags webkit2_41` (see README; drop the tag on
a system with webkit2gtk 4.0 instead of 4.1).

## Viewer

- [ ] Open `u.stl`. The model appears, lit and shaded, filling the viewport.
- [ ] Left-drag orbits. Wheel zooms. Right-drag pans.
- [ ] Open `sphere.stl` afterwards. The U is gone and the sphere is framed;
      the viewport shows only the new model, not both.

## Gizmo

- [ ] The plane appears across the model with a visible outline and a normal arrow.
- [ ] Move mode drags the plane; the model stays still.
- [ ] Rotate mode tilts the plane.
- [ ] The camera does not orbit while a gizmo handle is being dragged.
- [ ] Dragging a corner resizes the rectangle, and the Width/Height fields follow.
- [ ] Typing in Width resizes the rectangle.
- [ ] Clear the Width field entirely (select all, delete) while typing. The
      rectangle does not vanish or corrupt; typing a new value recovers it
      cleanly.
- [ ] After rotating the plane, corner dragging still tracks the cursor.
- [ ] Start dragging a gizmo corner, then hide the gizmo (or trigger a model
      reload) before releasing the mouse button. Afterwards the camera still
      orbits normally — this is not academic, a dead camera after a drag
      interrupted mid-gesture was a real bug fixed twice, via two different
      code paths, during implementation.

## The bounded cut — the feature this application exists for

- [ ] On `u.stl`, place the plane across the arms and shrink the rectangle to
      cover only the left arm.
- [ ] Cut. Two parts appear.
- [ ] **The right arm is still attached to the base, at full height.**
- [ ] Widen the rectangle to span both arms and cut again. Now both arms are cut.

## Parts and measurements

- [ ] The parts list shows the tree, with split parts greyed and leaves clickable.
- [ ] Clicking a leaf selects it; the viewport highlights it and dims the rest.
- [ ] Measurements match what `cutdemo` reports for the same geometry.

## Honest reporting

- [ ] Place the plane exactly flush with a flat face of the model and cut. The
      result is either refused or flagged — it is never silently reported as good.
- [ ] A flagged part shows a ⚠ in the list and a warning in the sidebar.
- [ ] Export a flagged part; the export message names the suspect file.

## Long cuts

- [ ] Open a model with hundreds of thousands of triangles and cut it. A progress
      bar appears and advances; the window stays responsive.

## Alignment pins

- [ ] Enable pins, cut a large model, and confirm the message reports how many were placed.
- [ ] Export both pieces and open them in a slicer: one has raised pegs on the cut
      face, the other matching sockets, and both are watertight.
- [ ] The pegs line up with the sockets — same count, same positions.
- [ ] Set the pin diameter larger than the cut face and cut. Every pin is skipped
      and the reason names the clearance that was available.
- [ ] Cut a thin-walled model with pins enabled. Pins are skipped for want of
      material behind the face, not placed and broken through.
- [ ] Set Min wall to 0.1 and repeat: more pins are now placed. That proves the
      guard is doing the work rather than the geometry refusing anyway.

## Fit to printer

- [ ] Enter a bed smaller than the model and click Split to fit. The parts list
      grows and every piece reports dimensions within the bed.
- [ ] Enter a bed larger than the model. Nothing happens and the message says so.
- [ ] Undo repeatedly after an auto-split. Every cut it made undoes one at a time.
- [ ] Enter a bed of 0. A readable error appears rather than a hang.

## Errors

- [ ] Cancelling the open dialog changes nothing and reports nothing.
- [ ] Opening a non-STL file shows a readable error, not a stack trace.
- [ ] Placing the plane entirely off the model and cutting shows a readable error.
- [ ] Undo with nothing to undo shows a readable error.
- [ ] Start an export, then cancel the folder-choice dialog. Nothing happens
      and no error appears.
