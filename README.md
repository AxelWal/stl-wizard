# STL Cutter

Splits STL models into printable parts with a **bounded** cutting plane: the
plane is restricted to a rectangle, so one cut can take the top off one arm of a
U-shaped model without touching the other.

- Alignment pins, with configurable diameter, length, clearance and a
  minimum wall guard, so printed pieces peg together instead of just
  matching at the cut.
- Fit-to-printer auto-splitting, which repeatedly cuts an oversized model
  down until every piece fits a given build volume.

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

Exits non-zero if either resulting part is not a closed solid.

## Layout

    main.go app.go tree.go session.go assets.go   the desktop application
    internal/stl        STL reading and writing
    internal/cut         the bounded-plane cutter
    internal/geom        vectors and point welding
    internal/meshcheck   watertightness invariants
    internal/fixtures    procedurally generated test meshes
    cmd/cutdemo          cut a file from the command line
    cmd/genfixture       write a fixture to a file
    frontend/            the window: three.js viewer and the plane gizmo

`internal/stl` and `internal/cut` import nothing from Wails and know nothing
about the UI, so the geometry is testable with `go test` alone.

## Pins

Enabling pins adds a peg-and-socket pair to the cut face on every alignment
pin the guard allows: one part gets raised pegs, the other matching bored
sockets, so the two pieces locate against each other instead of just
matching at the cut.

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

## Known limitations

All of these are reported at runtime — a part that is not a closed solid is
returned flagged, never silently.

- A cutting plane placed exactly flush with a flat face of the model can leave
  T-junctions at reflex corners. Nudge the plane slightly.
- Roughly 1% of bounded cuts hit a limit in cap triangulation and come back
  flagged.
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
- **The frontend has not been visually verified.** Every window-facing task
  in this project was implemented, built, and covered as far as static
  checks go, but no one has yet opened the window and driven it end to end.
  The Go side is covered by `go test ./...`; the window is not. Work through
  `docs/manual-verification.md` before relying on this application, and
  before any release build.

## Testing

    go test ./...        # the Go side
    go test ./... -race  # the session locking

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
