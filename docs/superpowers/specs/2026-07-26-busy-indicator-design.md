# A busy indicator for anything slow

The progress bar only covers cutting. Loading is the gap that matters: an 87MB file takes
71 seconds with nothing on screen at all, which reads as a hung window.

## One indicator, two states

- **Indeterminate** — a labelled spinner, for work that reports no progress: loading,
  repair, separating bodies, scaling, the orientation search, exporting.
- **Determinate** — the existing bar with a percentage, from the moment `cut:progress`
  starts arriving. A cut is the one operation that knows how far along it is, and a
  two-minute cut with no sense of that is nearly as bad as no indicator.

It starts indeterminate and becomes determinate when the first progress event lands.
Nothing goes the other way within one operation.

## The label is not decoration

A spinner alone at 70 seconds leaves the user guessing which of several slow things is
happening — and whether it is the one they asked for. Every operation names itself:
"Loading gedreht_660_prozent_wholeb.stl", "Separating bodies", "Cutting", "Scaling",
"Working out orientations".

## A CSS animation, deliberately

CSS animations run on the compositor thread. Loading a large model blocks the main thread
inside `STLLoader.parse` for seconds at a time, and a JS-driven spinner would freeze
exactly when it is most needed — the moment it stops moving is the moment the user
concludes the application has crashed. A `@keyframes` rotation keeps turning.

## One wrapper, so nothing can leave it stuck

```js
async function withBusy(label, fn) { ... }
```

Every command goes through it: set the label, show, disable the controls, `await fn()`,
and clear in a `finally`. The repeated `busy(true)` / `try` / `finally` in each handler
collapses into it.

That is the point rather than tidiness. A stuck indicator is this feature's failure mode,
and it has already happened once in this project: the progress bar was driven by
`cut:start` and `cut:done` events whose delivery order is not guaranteed, and when they
arrived reversed the bar stayed on screen with nothing able to clear it. Visibility
belongs to the awaited call, and one wrapper means one place that can get it wrong.

## Locking

`busy(true)` already disables Cut, Undo and Export during a cut. `withBusy` extends that
to every slow command. Firing a second command at a busy backend blocks on the session
mutex, which looks like a freeze rather than a queue.

## Testing without a timing window

The label is set **synchronously before the first await**, so a test can:

1. start an operation without awaiting it,
2. assert the indicator is visible and names the operation,
3. await it,
4. assert the indicator is hidden and the controls are enabled again.

No polling and no race. Specifically:

- Loading shows the spinner, names the file, and hides it afterwards.
- A failed command clears the indicator too — the `finally`, tested by loading a
  non-STL.
- A cut switches from spinner to bar when progress arrives, and the bar reaches 100%.
- Controls are disabled while busy and enabled afterwards, including after a failure.
- Two indicators cannot stack: a command started while another runs does not leave the
  indicator on when the first finishes.

Each test written to fail first, then the production code broken on purpose to confirm
the test notices. See CLAUDE.md's standing rules.
