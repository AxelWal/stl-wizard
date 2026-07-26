# stl-cutter — working notes for Claude

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

`playwright-cli` (global npm `@playwright/cli`) drives it. Sessions are named
with `-s`; artefacts land in `.playwright-cli/` (gitignored):

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

    window.app.openPath(absolutePath)   // full open sequence: render + frame + gizmo
    window.app.gizmoGroup()             // the THREE.Group carrying the plane
    window.app.setExtent(w, h)
    window.app.planeInput()             // exactly what Cut receives

`openPath` shares one `load()` body with the button's click handler, so
driving it exercises the real path rather than an imitation of it.

Place the plane by writing to the group — the corner handles and
TransformControls need a real mouse and cannot be driven precisely:

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

    #plane-width #plane-height            rectangle extent
    #mode-translate #mode-rotate          gizmo mode
    #pins-enabled #pins-count #pins-diameter #pins-length
    #pins-clearance #pins-minwall #pins-pegside
    #bed-x #bed-y #bed-z

With no model loaded, `#tree-panel`, `#plane-controls`, `#pin-controls`,
`#bed-controls` and `#actions` all carry `hidden`. A sidebar showing only
`#open` and `#status` is the correct initial state, not a failure.

Part geometry is served as binary STL from `/part/{id}.stl` (`assets.go`), not
through a binding — JSON-encoding a million triangles as decimal strings
would be far slower and larger.

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

## Read before trusting results

`README.md` "Known limitations" carries measured failure rates — most
importantly that cutting a part which **already has pins** often yields a
piece that is not a closed solid (37 of 64 with pins, 0 of 64 without).
Everything affected is flagged at runtime, never silent.

`PinSpec.Count` is a **target, not a demand** (`internal/cut/pins.go:17`):
placement grids the face and stops when it runs out of room, and pins it never
had a candidate for produce no skip message. Asking for 4 on the U's 10×15mm
cut face places 1 and reports only `Placed 1 alignment pin(s)`. Skip
messages appear only for candidates the wall guard actually rejected.

`docs/manual-verification.md` is the human checklist — pointer devices,
orbiting, mid-drag interruptions, and slicer inspection of exported pins, none
of which a headless browser can reach.
