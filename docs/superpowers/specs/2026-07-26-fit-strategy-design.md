# A fit-to-printer strategy that looks at the model

Reported with a screenshot: auto-split put a plane through the middle of a foot, when the
ankle a few centimetres up is a fraction of the cross-section. The foot would have fitted
the plate anyway.

The cause is that `cut.PlanAutoSplit` halves **bounding boxes**. It never looks at the
geometry, so it cannot know whether a plane passes through an ankle or a foot, and on a
lattice or an organic shape that is close to worst case.

## Done: the measurement

`cut.SectionScore(mesh, plane, eps) Section` reports how many separate closed contours a
plane cuts, and their total area.

One contour is a single cutting line — which is both what severs a part into two watertight
bodies and what a person would choose by eye. Area then distinguishes an ankle from a
thigh. That one measurement covers all three requirements from the report.

The area comes straight off the crossing segments by the shoelace formula, oriented from
each triangle's winding, so holes subtract themselves and only the contour count needs the
segments joined up.

`cut.BestCut(mesh, bed) (Spec, Section, bool)` uses it: for the axis overflowing by the
most, it searches the positions where both halves would fit — `p ∈ [L−B, B]` — and takes
the fewest contours, breaking ties on the smallest area. The midpoint is not privileged, so
a waist a third of the way along wins over a fat middle.

Both are tested: a U crosses in two places across its arms and one through its base; a
tube's annulus subtracts its own bore; and a barbell with deliberately unequal ends — so
the bounding box's midpoint falls inside a fat end — is still cut on the thin bar, area 36
against 2500.

## Not done: the planner, and exactly why

Two attempts, both reverted rather than shipped. The blocker is not the scoring.

**A plan is replayed by applying each entry to every leaf its rectangle crosses.**
Auto-split entries carry `Target: ""` for that reason, and it works for `PlanAutoSplit`
because every step's rectangle is bounded to the box that step was planned from.

Planning with `BestCut` has to cut as it goes, to score real fragments. That produces a
sequence of cuts each belonging to *one specific fragment* — and replaying them against
every leaf is not the same computation:

- **Rectangle generous** (what `BestCut` returns, so a cut cannot graze an edge and shed a
  sliver): on replay a plane strikes siblings it was never meant for. Measured: a 90mm
  hollow shell on a 35mm bed came back with pieces 45mm across.
- **Rectangle bounded tightly to the fragment** (a 2% margin): siblings are safe, but cuts
  graze their own edges. Measured: pieces 0.05mm and 0.1mm thick — slivers.

There is no margin that satisfies both, because the two requirements pull opposite ways.

**The fix is architectural, not a margin.** The simulation knows which fragment each cut
belongs to, and part naming is deterministic — `whole`, `wholea`, `wholeb`, and so on — so
each planned entry can record its fragment's **name** in `Target`, exactly as a manual cut
does. Replay then applies each cut to the one part it was chosen for, matching the
simulation exactly, and the rectangle may be as generous as it likes.

That means `planToFit` returning `[]struct{ spec cut.Spec; target string }` rather than
bare specs, with the target derived by tracking names through the simulated splits.

## Acceptance

The test to write first, which both attempts failed:

- For several shapes and beds, plan then execute, and assert **every** resulting piece is
  within the bed and none is a sliver.
- Fragments nothing can divide are counted and reported, never dropped. `BestCut` returns
  false both for a part that already fits and for one it could not place a cut on, and
  conflating those is what dropped fragments the first time.
- The barbell is cut on its bar, through the app rather than only through `cut.BestCut`.
- A plan whose entries name their fragments gives the same tree twice running.

## Cost, measured

Scoring is a pass over the fragment per candidate position, twelve positions per cut. A
90mm hollow shell onto a 35mm bed took about four minutes to plan. That is the price of
looking at the model, and it belongs behind the busy indicator; it is also why the sweep
above should stay modest in the tests.
