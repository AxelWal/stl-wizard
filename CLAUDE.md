# stl-cutter — working notes for Claude

## Two standing rules

**1. Use the superpowers skills. Every task.** Invoke the relevant skill before
acting — including before asking a clarifying question or reading the code.
`brainstorming` before designing anything, `test-driven-development` before
writing any feature or fix, `systematic-debugging` before proposing a cause,
`writing-plans` for anything multi-step, `verification-before-completion`
before claiming something works. If a skill might apply, it does.

**2. Every feature and every bugfix gets a Playwright test.** Anything that
changes what the window does is not finished until `e2e/gui.test.mjs` covers
it, and Go-side changes still want their `go test` as well. Write the test
first, watch it fail, then fix — a test written afterwards passes immediately
and proves nothing. For a bugfix the test must reproduce the bug, so run it
against the unfixed code and see it fail before you touch anything.

Both rules apply to small changes too. This project has been bitten repeatedly
by tests that passed against broken behaviour: a plain `wails build` stays green
while the window renders blank, and a volume-sum assertion holds for any
partition of a model. **After writing a test, break the production code on
purpose and confirm the test fails.** If it still passes, the test is decoration.

## Every wails command needs `-tags webkit2_41`

This machine has webkit2gtk **4.1**, not 4.0. A plain `wails build` or
`wails dev` fails with `Package 'webkit2gtk-4.0' not found`. `wails doctor`
also reports `Required dependencies missing: libwebkit` here — a false
negative; check `pkg-config --modversion webkit2gtk-4.1` before believing it.

## Driving the UI

`wails dev` serves the frontend over HTTP as well as into the native window,
so the whole application can be driven headlessly:

    wails dev -tags webkit2_41 > /tmp/wails-dev.log 2>&1 &
    timeout 120 bash -c 'until curl -sf http://localhost:34115 >/dev/null; do sleep 2; done'

Port 34115 is the Wails v2 default and `wails.json` sets no `devServer` key.
Stop it with `lsof -ti:34115 -sTCP:LISTEN | xargs -r kill`. First start takes
30–60s: it runs `go mod tidy`, regenerates bindings, and compiles.

For anything repeatable, write it into `e2e/gui.test.mjs` (see below) rather
than driving by hand. `playwright-cli` (global npm `@playwright/cli`) is for
ad-hoc probing — screenshots you want to look at, or working out why a test
fails. Sessions are named with `-s`; artefacts land in `.playwright-cli/`
(gitignored):

    playwright-cli -s=stl open http://localhost:34115
    playwright-cli -s=stl screenshot        # then Read the .png — look at it
    playwright-cli -s=stl snapshot          # element refs for click/fill/check
    playwright-cli -s=stl console error

`click`/`fill`/`check` take a `ref=eNN` from the latest snapshot, not a CSS
selector. **Refs are renumbered on every re-render**, so a cut or a selection
invalidates them all — re-snapshot, or drive buttons through `eval` with
`document.getElementById('do-cut').click()`, which fires the same listener and
survives re-renders. `run-code` rejects multi-statement snippets; use one
`await` per call or put the logic in `eval`.

### `window.app` — the headless handle

**The file dialogs cannot be answered from a browser tab.** `OpenModel`
(`app.go:101`) and `ExportAll` call `runtime.OpenFileDialog` against the Wails
context, so clicking `#open` in a tab makes a GTK dialog appear over the
*native window* and leaves the tab's promise pending forever. From Playwright
that looks like a dead button. Without a way round it an automated run is
stuck on an empty sidebar and can never reach the cut.

`OpenPath` (`app.go:119`) is the way round it, and `ui.js` exposes:

    window.app.openPath(absolutePath, repair?)  // full open sequence: render + frame + gizmo
                                        // repair defaults to the checkbox
    window.app.gizmoGroup()             // the THREE.Group carrying the plane
    window.app.setExtent(w, h)
    window.app.planeInput()             // exactly what Cut receives
    window.app.mode()                   // "translate" or "rotate"
    window.app.three                    // the THREE namespace, for projections
    window.app.camera() .controls() .canvas()

`openPath` shares one `load()` body with the button's click handler, so
driving it exercises the real path rather than an imitation of it. `three` is
handed over whole instead of growing a helper per assertion — the pointer tests
need it to project a handle's world position to a screen coordinate.

Aim the plane by writing to the group. A pointer can drag it (the suite does
exactly that) but cannot be aimed at a coordinate:

    const g = window.app.gizmoGroup();
    g.rotation.set(-Math.PI/2, 0, 0);   // local +Z is the cut normal; this aims it at +Y
    g.position.set(7.5, 25, 5);
    g.updateMatrixWorld();

### The regression worth running

This drives the feature the application exists for — a plane bounded to one
arm of the U — and every number below has been observed:

1. `window.app.openPath('/home/axel/source/stl-cutter/testdata/u.stl')`
   → status `u.stl — 1 part(s)`, info `30.0 × 40.0 × 10.0 mm`, 9000 mm³, 28 triangles, Closed yes
2. Aim the plane at +Y at `(7.5, 25, 5)` as above
3. `page.fill('#plane-width', '15')` and `'#plane-height', '20'`
4. `document.getElementById('do-cut').click()`
5. → `Cut complete. Both pieces are closed solids.`
   - `whole-a` **7500 mm³, 36 triangles, 30.0 × 40.0 × 10.0 mm** — the full
     envelope, which is only possible with **the right arm still at full
     height**. This is the assertion that matters; a plain unbounded cut
     would take the top off both arms and leave 6375/2625.
   - `whole-b` **1500 mm³, 20 triangles, 10.0 × 15.0 × 10.0 mm**
   - Both match `cmd/cutdemo` on the same geometry.

### Expected noise in a browser tab

- `TypeError: Cannot read properties of null (reading 'nodes')` from
  `wails/ipc.js`, once per page load. Wails' own runtime bootstrap reaching
  for something only the native webview provides. Bindings work regardless —
  ignore it, and do not chase it as a frontend bug.
- `favicon.ico` 404.
- WebGL `GPU stall due to ReadPixels` warnings. These are proof the canvas is
  really rendering, not a problem.

Anything else in the console is ours. There was nothing else during the run
that produced the figures above.

## Selectors

    #open              Open STL…          #do-cut          run the cut
    #status            state line         #do-undo         undo one cut
    #tree              parts list         #do-export       export parts…
    #part-info         measurements       #do-autosplit    split to fit
    #messages          results, warnings  #viewport        three.js canvas
    #progress          long-cut bar       #progress-bar    its fill

    #repair-on-load                       repair holes as the model loads
    #do-separate                          split a part into its bodies
    #do-plates                            export a multi-plate 3MF
    #plane-width #plane-height            rectangle extent
    #mode-translate #mode-rotate          gizmo mode
    #pins-enabled #pins-count #pins-diameter #pins-length
    #pins-clearance #pins-minwall
    #pins-pegside                         "2" | "1" | "dowel"
    #bed-x #bed-y #bed-z
    #printer                              a model slug, or "custom"; bed in data-bed

With no model loaded, `#tree-panel`, `#plane-controls`, `#pin-controls`,
`#bed-controls` and `#actions` all carry `hidden`. A sidebar showing only
`#open` and `#status` is the correct initial state, not a failure.

Part geometry is served as binary STL from `/part/{id}.stl` (`assets.go`), not
through a binding — JSON-encoding a million triangles as decimal strings
would be far slower and larger.

## The Playwright suite

`e2e/gui.test.mjs` covers every GUI feature, pointer input included. It needs
the dev server running, and it drives the same page a user gets:

    wails dev -tags webkit2_41 &
    timeout 120 bash -c 'until curl -sf http://localhost:34115 >/dev/null; do sleep 2; done'
    node e2e/gui.test.mjs              # all 61
    node e2e/gui.test.mjs pointer      # one group, matched by substring

`e2e/harness.mjs` holds the runner and the vocabulary — `app.open("u")`,
`app.cut()`, `app.parts()`, `app.dragMouse()`. Add a test by calling `test()`
inside a `group()`; the runner reloads the page before each one, and any
unexpected console error fails the test on its own.

Three things the harness knows that are easy to get wrong:

- **`app.cameraStill()` before projecting anything to a screen coordinate.**
  OrbitControls has damping, so the camera keeps easing for frames after
  `frameAll()`. A corner handle is about 5px across and the drift is about 25px,
  so a mid-drift projection aims the mouse at where the handle *was* and the
  drag silently does nothing. `handleScreenPos` and `gizmoCentre` wait for you.
- **A left-drag at the viewport centre translates the plane, it does not orbit.**
  TransformControls sits on the plane's origin and latches the press. Use
  `app.emptySpot()` to orbit and `app.gizmoCentre()` to translate.
- **Do not drive UI state off `cut:start` / `cut:done` ordering.** Go emits them in
  order, but on an error path they leave microseconds apart and the bridge
  delivered them *reversed* about one run in four — `cut:start` ran last and left
  the progress bar on screen for good. Visibility now brackets the awaited call in
  `ui.js`; `cut:progress` only moves the width, where a stale message is harmless.

The suite launches the system Chrome, because the globally installed
playwright's bundled-browser revision does not match what is downloaded under
`~/.cache/ms-playwright`. It falls back to the bundled build if there is no
system Chrome.

## Testing without the window

    go test ./...
    go test ./... -race    # the session locking

    go run ./cmd/genfixture -name u -out testdata/u.stl
    go run ./cmd/cutdemo -in testdata/u.stl -out /tmp/u \
        -origin 7.5,25,5 -normal 0,1,0 -width 15 -height 20

`cutdemo` exits non-zero if either part is not a closed solid, and those are
the same arguments as the UI regression above — the fastest way to prove the
engine still works.

`frontend_test.go` is the only automated check on `frontend/`: a static walk
verifying that relative imports carry `.js` and resolve, that import-map
targets exist, that bare specifiers are covered, and that vendored three.js
imports resolve. It caught a vendored three.js missing `three.core.js` that
rendered the window blank while every other test stayed green. It says
nothing about behaviour — that is what the browser run above is for.

## Before changing frontend/

- Bindings in `frontend/wailsjs/` are regenerated by `wails dev`/`wails build`.
  Commit them; drift is invisible to `go test` and surfaces as a runtime error.
- three.js is vendored whole under `frontend/vendor/three/` — copy the entire
  `build/` directory, not just `three.module.js`, which is a shim.
- Native ES modules with an import map. No bundler, no npm, no `node_modules`.
- `transform.visible` is inert in r185. Toggle
  `TransformControls.getHelper()` instead; see `frontend/gizmo.js:179`.
- `wails dev` reloads the page on every save, so a half-finished multi-file
  edit will throw in the console. Re-check after the last edit lands.

## 3MF export

`internal/threemf` writes plain core 3MF — a single `3D/3dmodel.model` with inline
meshes — plus `Metadata/model_settings.config` carrying the Bambu/Orca plate
extension. Bambu's own export uses the production extension (external object files,
`p:UUID` everywhere, `requiredextensions="p"`); none of it is needed.

**Two traps, both with tests that only exist because of them:**

- **3MF transforms a point as a row vector**, `p' = p·M`, so the stored 3x3 is the
  transpose of the usual column-vector rotation. `app.go`'s `exportPlatesTo`
  transposes `orient`'s row-major matrix on the way out. Every axis-aligned fixture
  picks a rotation like `diag(1,-1,-1)` which is its own transpose, so the error is
  invisible to them — `TestExportPlatesWritesTheTransformTheWayThreeMFReadsIt`
  tilts a box by two odd angles specifically to catch it. That test was added after
  a transpose mutation broke nothing.
- **`orient` must exclude triangles resting on the plate** from the overhang score,
  or a flat bottom is the worst possible score and the search avoids resting parts
  flat.

Each `#printer` option's value is a model slug and its bed size is in `data-bed`. The
size cannot be the value: nine of the fourteen machines share a bed with another, five
of them at 256x256x250, so a size-valued select cannot say which printer is chosen — it
read H2C back as A2L, and a test selecting by value was silently exercising A2L while
claiming to test H2C. Match options by the exact model name before the em dash, never
by prefix: "A1" is a prefix of "A1 mini".

The fourteen printer sizes in `index.html` come from Bambu Studio's machine profiles,
resolved through their `inherits` chains — not from spec sheets, which disagree: the X1
Carbon is sold as 256 tall and the slicer accepts 250. `scripts/verify-printers.sh`
diffs the UI against the installed slicer in both directions. Run it if you touch that
select.

Verify a real export with `scripts/verify-3mf.sh` — it round-trips through the
installed Bambu Studio flatpak. **Pass `--arrange 0`** as that script does: Bambu's
CLI re-arranges on import by default and repacks everything onto plate 1, which is
indistinguishable from our file being wrong. The GUI does not.

Bambu's default bed here is 200x200x100, not the app's 220x220x250 default, so a part
placed by our stride can land off Bambu's bed. The plate assignment in
`model_settings.config` is what actually decides the plate, and that was verified to
survive; the stride only affects where things are drawn.

## Read before trusting results

`README.md` "Known limitations" carries measured failure rates — most
importantly that cutting a part which **already has pins** often yields a
piece that is not a closed solid (37 of 64 with pins, 0 of 64 without).
Everything affected is flagged at runtime, never silent.

`PinSpec.Count` is a **target, not a demand** (`internal/cut/pins.go:17`):
placement grids the face and stops when it runs out of room, and a pin it never
found a candidate for produces no `SkippedPin` to explain itself. The sidebar
says `Placed 1 of 8 alignment pin(s) — the cut face had room for no more`;
`CutOutcome.PinsRequested` is what makes that possible. Individual skip
messages, with the measurement that caused them, appear only for candidates the
wall guard actually rejected.

A pin needs its length plus the minimum wall *behind* the cut face, so the 10mm
cube fixture halved leaves 5mm and refuses anything longer than about 3mm. That
is correct, and two tests depend on it.

**Dowel mode** (`#pins-pegside` set to `dowel`) bores both pieces instead of
raising a peg, and measures the wall on **both** sides — peg mode only ever
measures the socket side, which is correct there and would let a dowel break out
of the other one. `Diameter` means the user's stock, so the bore is
`Diameter + 2 × Clearance`.

**Repair** (`internal/repair`) fans each boundary loop from its centroid, so every
boundary edge gains exactly one triangle and closure holds by construction. Off by
default, because it rewrites the user's geometry.

`meshcheck.OpenEdges` lumps together two unrelated defects, and repair only
addresses one. `Result.BoundaryEdges` counts edges used once — rims, which filling
closes. `Result.NonManifoldEdges` counts edges used three or more times — two
surfaces touching, which filling cannot touch at all. **Real files are almost
entirely the second kind:** two parts exported from this application, 492k and
1.75M triangles, had 6 and 16 open edges and not one was a hole. `Result.Unfixable()`
is the "do not bother trying again" signal, and both the sidebar and
`cmd/meshrepair` must say which kind it is — "filled 0 holes" alone reads as a
repair that could not be bothered.

`dropEmptyShells` is what actually repairs real files, and it runs **after** hole
filling: a legitimately open shell has no enclosed volume until its holes close,
and weighing it first deletes exactly the model repair exists to rescue. The rule
is "encloses nothing" (|volume| <= eps × own area), never "is small" — a hollow
model's inner void is a separate inside-out surface and dropping inverted surfaces
would fill every hollow model solid.

Fixtures: `openbox` a fillable rim, `cubewithflap` a fused zero-volume flap (what
real cut output has), `touchingcubes` two real bodies sharing an edge (what repair
cannot fix but Separate bodies can). Use `cmd/meshrepair` for anything large;
pushing an 87MB file through the browser to find out whether it is even broken is
the slow way round.

**`internal/shells`** labels connected surfaces, joining only across edges shared by
**exactly two** triangles — a non-manifold edge deliberately does not join, which is
what makes two touching bodies separable. Nested surfaces are grouped with their
container by bounding box, because a hollow model's void is its own surface and
returning it as a body gives a solid shell and an inside-out one. `Tree.SplitMany`
is the N-child version of `Split`; both go through `replaceLeaf`, and `undoStep`
records only `{parent, mesh}` so undo never knew how many children there were.

`docs/manual-verification.md` is what is left for a human: exported pins in a
slicer, and a real model of a few hundred thousand triangles. Everything the
browser can reach is in `e2e/gui.test.mjs` now, pointer input included.
