# STL Cutter

Splits STL models into printable parts with a **bounded** cutting plane: the
plane is restricted to a rectangle, so one cut can take the top off one arm of a
U-shaped model without touching the other.

- Alignment pins, with configurable diameter, length, clearance and a
  minimum wall guard, so printed pieces peg together instead of just
  matching at the cut. Either a printed peg on one side, or a matching hole
  in both for a dowel of your own.
- Fit-to-printer auto-splitting, which repeatedly cuts an oversized model
  down until every piece fits a given build volume.
- Optional repair as a model is loaded: closes holes and deletes the stray
  zero-volume debris a cut leaves behind, so a slightly broken file can be cut
  without every piece coming back flagged.
- Separating a file's disconnected bodies into their own parts, so several solids
  in one STL can be cut, measured and exported individually.

## Running

Requires Go 1.26 and the Wails v2 CLI:

    go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
    wails doctor      # on Arch-family systems this reports libwebkit missing;
                      # check pkg-config --modversion webkit2gtk-4.1 before believing it
    wails dev -tags webkit2_41

On Linux you need webkit2gtk development headers. This machine has
webkit2gtk 4.1, not 4.0, so **every** `wails` command needs
`-tags webkit2_41` — a plain `wails build` or `wails dev` fails with
`Package 'webkit2gtk-4.0' not found`. `wails doctor` also reports
`Required dependencies missing: libwebkit` on this machine; that is a false
negative, since the library is present as 4.1. If your system has
webkit2gtk 4.0 instead, drop the tag.

## Building

    wails build -tags webkit2_41   # binary in build/bin/

## Command line

The geometry library is usable without the window:

    go run ./cmd/genfixture -name u -out testdata/u.stl
    go run ./cmd/cutdemo -in testdata/u.stl -out /tmp/u \
        -origin 7.5,25,5 -normal 0,1,0 -width 15 -height 20
    go run ./cmd/meshrepair -in model.stl

Exits non-zero if either resulting part is not a closed solid.

## Layout

    main.go app.go tree.go session.go assets.go   the desktop application
    internal/stl        STL reading and writing
    internal/cut         the bounded-plane cutter
    internal/geom        vectors and point welding
    internal/meshcheck   watertightness invariants
    internal/fixtures    procedurally generated test meshes
    internal/repair      closing holes and clearing debris
    internal/shells      separating a mesh into its disconnected bodies
    cmd/cutdemo          cut a file from the command line
    cmd/meshrepair       report on and repair a file from the command line
    cmd/genfixture       write a fixture to a file
    frontend/            the window: three.js viewer and the plane gizmo

`internal/stl` and `internal/cut` import nothing from Wails and know nothing
about the UI, so the geometry is testable with `go test` alone.

## Pins

Enabling pins adds a peg-and-socket pair to the cut face on every alignment
pin the guard allows: one part gets raised pegs, the other matching bored
sockets, so the two pieces locate against each other instead of just
matching at the cut.

**Holes both sides** is the alternative, chosen with the Style select. Neither
piece gets a peg; both are bored the same, and the two are joined with round
stock of your own — a piece of filament, a rod, a cocktail stick. `Diameter`
then means that stock rather than the hole, so the bore comes out
`Diameter + 2 × Clearance` and it actually slides in. `Length` is the depth of
each hole in both modes, so the piece to cut is about twice it; the message
after the cut gives the figure.

Dowel mode checks the wall on **both** sides. A printed peg is only bored into
one piece, so peg mode measures the material behind one face; a dowel hole can
break out of the far side of either, and the thinner side governs.

The guard is a wall check, not a suggestion. A pin is skipped when there is
not at least the minimum wall of material around it — in the plane of the
cut face, clear of every edge and every other pin — or behind it, for the
socket to be bored into without breaking through the far side. Every skip is
reported with the measurement that caused it: what was actually there and
what was required, so a run that places fewer pins than asked for always
says why.

## Releases

Builds are produced by pushing a tag matching `v*`, which runs
`.github/workflows/build.yml` on Linux, macOS and Windows runners and
uploads a binary for each. macOS builds are unsigned; Gatekeeper will warn
on first launch, and the user has to right-click and choose Open (or clear
the quarantine attribute) to get past it.

**The workflow itself has never been run.** It has been written and
reviewed, but no tag has yet been pushed, so it has not executed on a real
runner. The first tag push is what proves it actually works end to end.

## Repair

**Repair holes on load** is off by default, because it rewrites the geometry you
handed over. Ticked, it closes every hole in the model as it is opened and says
what it did. A model left unrepaired still loads and is still flagged, exactly as
before.

Each hole's rim is fanned from its own centroid, so every boundary edge gains
exactly one triangle and becomes a shared edge — closed by construction rather
than by hope. A rim that is not flat gets a geometrically approximate fill, but it
does get closed.

It also **deletes stray surfaces that enclose no volume**, and on real files that
is the repair that matters. Two parts exported from this application, of 492k and
1.75M triangles, reported 6 and 16 open edges and had **no holes at all**: every
bad edge came from a two-triangle zero-volume flap fused to a real body — some
0.01mm across — left behind by a cut. Dropping them took all 22 edges with them:

    before: not watertight: 6 open edges
    repair: dropped 7 empty shell(s) totalling 14 triangle(s)
    after:  watertight
    volume: 1.15061e+06 -> 1.15061e+06 (+0)

The rule is "encloses nothing", not "is small". A hollow model's internal void is a
separate inside-out surface with a large negative volume, and deleting surfaces for
being inverted would fill every hollow model solid. Two real bodies in one file are
both real. Only a surface whose signed volume cancels to nothing goes, and removing
one cannot change what prints — the volume above is unchanged to the last digit.

**What it buys you**, measured on that 492k-triangle file: the same plane, the same
cut, the checkbox the only difference.

| | repair off | repair on |
|---|---|---|
| on load | not a closed solid | closed solid |
| after the cut | both pieces open, 4 and 2 edges | **both pieces closed solids** |
| piece volumes | 613424 + 537185 mm³ | 613424 + 537185 mm³ |

The volumes are identical to the digit. Repair changed nothing about the geometry
that matters and turned two unprintable pieces into two printable ones, because the
debris it removed was sitting on the cut path.

One fault it does **not** fix: **backwards-wound triangles.** Turning one around
means propagating a consistent orientation across the whole surface, which is a
different algorithm. The model comes back reported and otherwise untouched.

**Two solid bodies touching along an edge** used to be in that list. It is now
handled by Separate bodies, below — no geometry has to move at all.

## Separate bodies

An STL is a bag of triangles with no notion of a body, so a file holding several
solids arrives as one part and cuts and exports as one. **Separate bodies** gives
each solid its own part. One real example: an exported part held two solids of
578,016 and 572,593 mm³ presented as a single 1,150,609 mm³ part.

It is also the fix for two bodies touching along an edge. Such a pair is one
non-manifold mesh that repair cannot help with — both halves enclose volume, so
neither is debris. Split apart, each is a closed solid, and nothing has been moved
to achieve it.

A hollow model stays one body. Its internal void is a separate inside-out surface,
and handing that back as a body would give you a solid shell and an inside-out one,
so a surface nested inside another is grouped with its container.

Counting bodies costs a full weld of the mesh — about 20 seconds at 1.75M triangles
— so it happens when you press the button rather than on every load. Repair labels
surfaces anyway, so with repair on the load message names the count for free. `cmd/meshrepair`
gives the same account from the command line, which is the practical way to check a
large file:

    go run ./cmd/meshrepair -in model.stl                 # just the verdict
    go run ./cmd/meshrepair -in model.stl -out fixed.stl  # repair and write

## Known limitations

All of these are reported at runtime — a part that is not a closed solid is
returned flagged, never silently.

- A cutting plane placed exactly flush with a flat face of the model can leave
  T-junctions at reflex corners. Nudge the plane slightly.
- Cutting a part that **already carries pins** often produces a piece that is not
  a closed solid. Pins put several circular holes in the cut face, and bridging
  them can emit slivers thin enough that a later cut cannot pair their edges.
  Measured: auto-splitting a 300mm cube onto a 120mm bed flags 37 of 64 pieces
  when pins are enabled, and 0 of 64 when they are not. Every affected piece is
  reported — never silently — so the practical advice is to make all the cuts
  first and enable pins on the last one, or to check the parts list before
  exporting.
- The same weakness affects heavily-curved models even without pins: auto-splitting
  a fine-tessellated sphere (`UVSphere(80, 32, 16)`) onto a 100mm bed takes 7 cuts
  and flags 3 of the 8 pieces. Cubes and tubes survive it.
- Cutting costs about 5µs per triangle, so a two-million-triangle model takes
  around ten seconds per cut.
- Pin placement samples the cut face on a grid rather than computing its
  medial axis, and the behind-the-face wall check samples nine points
  (the pin's centre and eight around its circumference) rather than
  sweeping the whole footprint. Both are exact enough for the simple faces
  a bounded cut produces; a far surface with a gap narrower than the sample
  spacing could in principle slip through the wall check and be
  overestimated.
- Auto-split is axis-aligned only: a part that would fit the bed turned
  diagonally is split anyway, because rotating to fit is a packing problem
  and this is a splitter.
- **The frontend is covered by an automated browser suite, not by hand.**
  `e2e/gui.test.mjs` drives the real page against the dev server — the viewer,
  the gizmo, the bounded cut, selection, measurements, undo, pins, auto-split,
  the error paths, and pointer input including orbit, pan, zoom and corner-handle
  drags. What is left for a human is in `docs/manual-verification.md`: opening
  exported pinned pieces in a slicer, and a model of a few hundred thousand
  triangles for the progress bar. Do those before a release build.

## Testing

    go test ./...        # the Go side
    go test ./... -race  # the session locking

The window has its own suite, which drives a real browser against the dev
server and needs it running:

    wails dev -tags webkit2_41 &
    node e2e/gui.test.mjs

53 tests over every GUI feature, pointer input included. See CLAUDE.md.

`go test ./...` also runs `frontend_test.go`, which is the only automated
check on the frontend: it walks `frontend/` and verifies that every relative
import in our own JS carries a `.js` extension and resolves to a real file,
that every target the import map declares exists, that every bare specifier
our JS imports is covered by the import map, and that every relative import
inside the vendored three.js resolves. It is a static check, not a
behavioural one, but it has already caught a real bug: a vendored three.js
tree that was missing a file and rendered the entire window blank while the
Go build and every other Go test stayed green.

The window has no other automated tests, by design; see
`docs/manual-verification.md`.
