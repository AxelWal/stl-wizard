# Manual verification

Most of this list is now automated. `e2e/gui.test.mjs` drives the real page in
a real browser against the dev server, pointer input included:

    wails dev -tags webkit2_41 &
    node e2e/gui.test.mjs

Lines marked **[auto]** are covered there and do not need doing by hand — fix
the test instead if one of them regresses. Run the suite before any release
build and after any change to `frontend/`.

**The unmarked lines are what is left for a person**, each with the reason
it cannot be automated: a native dialog the browser cannot answer, a slicer, or
a model far larger than any fixture. Treat them as the outstanding work.

The suite generates the fixtures it needs into `testdata/`. To make one by hand:

    go run ./cmd/genfixture -name u -out testdata/u.stl
    # u, cube, sphere, tube, hollowbox, openbox (a fillable hole),
    # cubewithflap (zero-volume debris), touchingcubes (two real bodies, unfixable)

Drop `-tags webkit2_41` on a system with webkit2gtk 4.0 instead of 4.1.

## Viewer

- [x] **[auto]** Open `u.stl`. The model appears, lit and shaded, filling the viewport.
- [x] **[auto]** Left-drag orbits. Wheel zooms. Right-drag pans.
- [x] **[auto]** Open a second model afterwards. The first is gone and its mesh is
      disposed; the viewport shows only the new model, not both.

## Gizmo

- [x] **[auto]** The plane appears across the model with a visible outline and a normal arrow.
- [x] **[auto]** Move mode drags the plane; the model stays still.
- [x] **[auto]** Rotate mode tilts the plane about its own origin.
- [x] **[auto]** The camera does not orbit while a gizmo handle is being dragged.
- [x] **[auto]** Dragging a corner resizes the rectangle, and the Width/Height fields follow.
- [x] **[auto]** Typing in Width resizes the rectangle.
- [x] **[auto]** Clear the Width field entirely (select all, delete) while typing. The
      rectangle does not vanish or corrupt; typing a new value recovers it cleanly.
- [x] **[auto]** After rotating the plane, corner dragging still tracks the cursor.
- [x] **[auto]** Start dragging a gizmo corner, then trigger a model reload before
      releasing the mouse button. Afterwards the camera still orbits normally — not
      academic, a dead camera after a drag interrupted mid-gesture was a real bug
      fixed twice, via two different code paths.

## Feedback while it works

- [x] **[auto]** Loading shows a spinner naming the file and clears it afterwards, with
      the controls locked meanwhile.
- [x] **[auto]** A command that fails clears the indicator too.
- [x] **[auto]** A cut turns the spinner into a percentage bar once it reports progress.
- [x] **[auto]** A stray progress event outside a command draws nothing.
- [x] **Seen by hand** on a real 24MB load: the spinner turns, the label reads
      "Loading gedreht_660_prozent_wholea.stl", and Add plane is disabled.
- [ ] Watch it during a cut long enough to see the bar actually advance rather than jump
      from 0 to 100. *(Needs a model of a million triangles or more.)*

## Scale

- [x] **[auto]** A percentage scales the model and the reported size and volume follow.
- [x] **[auto]** Applying the same percentage twice changes nothing the second time.
- [x] **[auto]** Keep proportions makes one field drive all three.
- [x] **[auto]** Unticked, each axis scales on its own and the model stays closed.
- [x] **[auto]** Millimetre mode reaches the size asked for, even after an earlier scale.
- [x] **[auto]** Reset returns to exactly the file's own size and volume.
- [x] **[auto]** A zero or negative factor is refused with a readable message and changes
      nothing.
- [x] **[auto]** Scaling clears the cut plan and says so.
- [x] **[auto]** A scaled model cuts into pieces that sum to the scaled whole.
- [ ] Scale something to a measured size, print it, and check it with calipers.

## The cut plan

- [x] **[auto]** Add plane cuts nothing; the list gains a numbered entry; Cut now cuts.
- [x] **[auto]** Entries are listed in the order they will run.
- [x] **[auto]** Renaming sticks, including across executing the plan.
- [x] **[auto]** Deleting an entry removes it from the plan and from the result.
- [x] **[auto]** Unticking an entry leaves it out without losing it.
- [x] **[auto]** Clicking an entry brings its plane back under the gizmo; moving it and
      pressing Save plane changes what Cut now produces.
- [x] **[auto]** Plan fit to printer proposes and cuts nothing until Cut now.
- [x] **[auto]** Separating bodies is recorded as an entry and survives Cut now.
- [x] **[auto]** Clearing the plan, and loading a model, both empty the list.
- [x] **[auto]** Dragging an entry reorders the plan, every entry survives the move, and
      the new order changes what the plan produces.
- [ ] Run a fifty-entry plan on a large model and watch the progress bar. *(Cut now
      re-cuts from scratch, so this is minutes; no fixture is large enough to make the
      wait realistic.)*

## The bounded cut — the feature this application exists for

- [x] **[auto]** On `u.stl`, place the plane across the arms and shrink the
      rectangle to cover only the left arm.
- [x] **[auto]** Cut. Two parts appear.
- [x] **[auto]** **The right arm is still attached to the base, at full height.**
      Measured: 7500 mm³ over the full 30 × 40 × 10 envelope, against 1500 mm³ at
      10 × 15 × 10 for the piece taken off.
- [x] **[auto]** Widen the rectangle to span both arms and cut again. Now both arms
      are cut: 6000 / 3000, and the remainder is no longer 40 mm tall.

## Parts and measurements

- [x] **[auto]** The parts list shows the tree, with split parts greyed and leaves clickable.
- [x] **[auto]** Clicking a leaf selects it; the viewport highlights it and dims the rest.
- [x] **[auto]** Measurements match what `cutdemo` reports for the same geometry.

## Honest reporting

- [x] **[auto]** A part that is not a closed solid carries a ⚠ in the list and a
      warning in the sidebar, and the flag matches the Closed reading exactly.
- [x] **[auto]** Cutting a part that already carries pins is flagged when it breaks,
      never silently broken. See the README's Known limitations for how often.
- [ ] Place the plane exactly flush with a flat face of the model and cut. The
      result is either refused or flagged — never silently reported as good.
      *(Not automated: which faces are exactly flush depends on the fixture, and a
      test that does not actually hit the degenerate case would prove nothing.)*
- [ ] Export a flagged part; the export message names the suspect file.
      *(Needs the native folder dialog.)*

## Long cuts

- [x] **Done once, by hand**, on a real 492k-triangle model: the cut took 6.5s, the
      window stayed responsive, and the progress bar was hidden again afterwards.
      Not automated — no fixture is that large.
- [ ] Repeat with a million triangles or more, and watch that the bar actually
      advances rather than only appearing and vanishing. *(Cutting costs about 5µs
      per triangle.)*

## Alignment pins

- [x] **[auto]** Enable pins, cut, and confirm the message reports how many were
      placed. The socket removes strictly more than the peg adds — that difference
      is the clearance — and both pieces stay closed.
- [x] **[auto]** Ask for more pins than fit. The message says "of N" and gives the
      reason, rather than quietly placing fewer.
- [x] **[auto]** Set the pin diameter larger than the cut face and cut. Every pin is
      skipped and the reason names the clearance that was available.
- [x] **[auto]** Pins are skipped for want of material behind the face rather than
      broken through it: the 10 mm cube halved leaves 5 mm, which refuses an 8 mm pin.
- [x] **[auto]** Relaxing Min wall from 2 to 0.1 changes the outcome for the same
      geometry. That proves the guard is doing the work rather than the geometry
      refusing anyway.
- [x] **[auto]** Choose "holes both sides" and cut. Both pieces lose material and
      neither gains any, and the message gives the bore, the depth, and how much
      stock to cut.
- [x] **[auto]** Dowel mode refuses a hole that would break out of the far side of
      *either* piece, including the side peg mode does not measure.
- [ ] Export both pieces and open them in a slicer: one has raised pegs on the cut
      face, the other matching sockets, and both are watertight.
      *(Needs a slicer. The volume arithmetic is checked automatically, but only a
      slicer shows whether the pieces actually seat together.)*
- [ ] Print a dowel joint and check a piece of your own stock actually slides into
      both holes and seats. *(The clearance arithmetic is checked automatically;
      only a print shows whether the fit is right in practice.)*
- [ ] The pegs line up with the sockets — same count, same positions.
      *(Same reason. Go tests check the positions; this checks the print.)*

## Repair on load

- [x] **[auto]** The checkbox starts unticked.
- [x] **[auto]** Load `openbox.stl` with it unticked: the part is flagged ⚠ and
      reports `Closed: no`, and nothing claims to have repaired anything.
- [x] **[auto]** Tick it and load again: the message says how many holes were
      filled, the part reports `Closed: yes`, and the volume is the full 8000 mm³
      of the cube the fixture came from.
- [x] **[auto]** A repaired model then cuts into two closed halves — which is the
      entire point of the feature.
- [x] **[auto]** Load `cubewithflap.stl` with it ticked. The stray zero-volume
      shell is dropped, the cube comes back closed, and the volume is unchanged.
      This is the defect real cut output actually has.
- [x] **[auto]** Load `touchingcubes.stl` with it ticked. Both bodies enclose
      volume so neither is debris; the message names the defect and says repairing
      again will not help, rather than reporting "filled 0 holes" and stopping.
- [x] **Done once, by hand, on a real 24MB 492k-triangle file** (not automated —
      the file is the user's, not a fixture): ticked, it dropped 7 stray shells,
      reported `Closed: yes`, and the same cut then gave two closed solids where
      unticked it gave two open ones — 4 and 2 edges — at identical volumes. Load
      took 12.6s, the cut 6.5s.
- [ ] Repeat on a download from elsewhere, export, and open it in a slicer. *(No
      fixture stands in for every way real exporters break meshes.
      `go run ./cmd/meshrepair -in model.stl` gives the verdict without the GUI,
      which is the practical route for a file of tens of megabytes.)*
- [ ] Load a model whose only fault is backwards-facing triangles. The message says
      it is still not a closed solid and names winding as the reason, rather than
      implying a clean bill of health.
      *(Not automated: no fixture currently has that as its only fault.)*

## Separate bodies

- [x] **[auto]** Load `touchingcubes.stl` and press Separate bodies: two parts, each
      `Closed: yes` where the pair was flagged, volumes still adding up. No geometry
      moved to achieve that.
- [x] **[auto]** Press it on an ordinary cube: it says the part is a single body and
      leaves the tree alone.
- [x] **[auto]** Press it on `hollowbox.stl`: one body, not a shell and a void.
- [x] **[auto]** Undo after separating restores the single part.
- [x] **[auto]** A separated body then cuts like any other part.
- [ ] Separate a real multi-body download, export the bodies, and check in a slicer
      that each is the solid you expected and none is inside out.
      *(Bounding-box nesting is a heuristic: two interlocking parts whose boxes
      contain one another could be grouped wrongly. No fixture reproduces that.)*

## Fit to printer

- [x] **[auto]** Enter a bed smaller than the model and click Split to fit. Every
      piece reports dimensions within the bed and the volumes still sum to the
      original. On `u.stl` onto 20 × 20 × 20: 4 pieces, 2 × 2000 and 2 × 2500 mm³.
- [x] **[auto]** Enter a bed larger than the model. Nothing happens and the message says so.
- [x] **[auto]** Undo after an auto-split removes one cut, not the whole run.
- [x] **[auto]** Enter a bed of 0. A readable error appears rather than a hang.

## Printer sizes

- [x] **[auto]** Choosing a printer fills the three bed fields, and the figures match
      Bambu Studio's own profiles — checked by `scripts/verify-printers.sh`, which was
      run and passes for all fourteen models.
- [x] **[auto]** Typing a bed size switches the select to Custom and keeps what was
      typed.
- [x] **[auto]** The chosen printer actually reaches the cutter: a bed change alters
      what Split to fit does.
- [ ] Check a plate against the physical printer once. *(The purge zone on the P1/X1
      machines is not modelled; a part filling the plate may clash with it.)*

## 3MF plates

- [x] **[auto]** Export with two parts: two plates, one part each, the file a real zip
      holding `3D/3dmodel.model` and `Metadata/model_settings.config`.
- [x] **[auto]** A tall part is laid on its largest face rather than stood up, and the
      reported height reflects it.
- [x] **[auto]** A part too large for the bed is still exported and named in the
      warnings.
- [x] **Done once, by hand, against the real slicer.** `scripts/verify-3mf.sh` on a
      two-plate export: Bambu Studio read both objects as manifold with volumes 6000
      and 3000, and after a `--arrange 0` round trip both plates came back with one
      part each.
- [ ] Open an export in the Bambu Studio GUI and look at it: two plates in the plate
      list, each holding one part, each part lying the way the message described.
      *(The CLI proves the file parses and the plates survive; only the GUI shows
      whether it looks right.)*
- [ ] Slice a plate and print it. *(Nothing short of a print shows whether the chosen
      orientation was actually a good idea.)*

## Errors

- [x] **[auto]** Opening a non-STL file shows a readable error, not a stack trace.
- [x] **[auto]** Placing the plane entirely off the model and cutting shows a
      readable error, leaves the tree alone, and hides the progress bar again.
- [x] **[auto]** Undo with nothing to undo is not reachable: the button disables
      itself, including right after an undo that empties the history.
- [ ] Cancelling the open dialog changes nothing and reports nothing.
      *(Needs the native file dialog.)*
- [ ] Start an export, then cancel the folder-choice dialog. Nothing happens
      and no error appears. *(Needs the native folder dialog.)*
