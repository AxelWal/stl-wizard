# Separating disconnected bodies — design

A loaded STL can contain several entirely separate solids. Today they arrive as one
part, so they cut and export as one. This makes them separate parts on request.

Not hypothetical: `gedreht_660_prozent_wholea.stl` holds two bodies of 578,016 and
572,593 mm³, presented as a single 1,150,609 mm³ part.

## `internal/shells`

The connected-component labelling this needs already exists, privately, inside
`repair.dropEmptyShells`. Extract it so there is one definition of "a connected
surface" rather than two:

```go
package shells

// Split returns one mesh per body, largest by triangle count first. Surfaces
// nested inside another are grouped with their container rather than returned
// separately.
func Split(m *stl.Mesh, eps float64) []*stl.Mesh
```

Triangles are joined into a surface across edges shared by **exactly two** of them.
A non-manifold edge deliberately does not join, which is what leaves two bodies
touching along an edge as two surfaces — and is what makes this feature able to fix
that case.

`repair` then calls `shells.Split` and weighs each result, replacing its own inline
union-find.

## Nesting: a void is not a body

A hollow model's internal void is a separate surface with negative volume —
`fixtures.HollowBox` is precisely that. Returning it as a body would hand the user a
solid box and an inside-out box.

So a surface whose bounding box lies inside another's is grouped with that
container, smallest containing box winning. Only surfaces contained by nothing are
returned as bodies, each carrying whatever nests inside it.

`ponytail:` bounding-box containment, not true geometric containment. It is correct
for hollow shells, for two bodies side by side, and for debris sitting inside a
body's box. Two interlocking parts whose boxes contain one another could be
misgrouped, which puts a surface in the wrong body rather than corrupting either.
Upgrade path if that ever matters: one point-in-mesh ray test per nested surface
against each candidate container, with `cut`'s existing ray grid.

## Tree: N children instead of two

`Tree.Split` hardcodes two children, but `undoStep` records only `{parent, mesh}` —
it restores the parent's mesh and clears its children, which is already independent
of how many there were. So the change is additive:

```go
// SplitMany replaces the leaf id with one child per mesh.
func (t *Tree) SplitMany(id string, meshes []*stl.Mesh, watertight []bool) ([]*Part, error)
```

`Split` becomes a two-element call into the same guts, so cuts and separations share
one code path for id assignment, history and selection.

Children are named `whole1`, `whole2`, … Cuts already use `wholea`/`wholeb`, so the
suffix says which operation produced a part.

## `App.SeparateBodies`

```go
func (a *App) SeparateBodies(partID string) (*SeparateOutcome, error)
```

`SeparateOutcome` carries the new tree, how many bodies were found, and per-body
watertightness. Separating one body is not an error — it reports "this part is a
single body" and changes nothing, because refusing with an error would make the
button feel broken on the common case.

Each new part is checked with `meshcheck` in its own right. A body that was
non-manifold only because it touched its neighbour comes back sound, and that is the
point; a body broken on its own stays flagged.

## Counting is lazy

Labelling 1.75M triangles costs a full weld, about 20 seconds. Doing that on every
load to populate a number the user usually does not need would double load time, so
the count is computed when the button is pressed.

Repair already labels, though, so `repair.Result` gains `Bodies int` at no cost, and
the load-time message says "contains 2 separate bodies" whenever repair ran.

## This fixes what repair cannot

Two solid bodies touching along one edge are reported today as beyond repair: both
enclose volume so neither is debris, and separating them means changing geometry.
Separating them into two meshes needs no geometry change at all, and each is then a
closed solid.

So the message for that case stops being "repairing again will not help" and starts
naming the button that does help. `fixtures.TouchingCubes` becomes a fixture with a
fix.

## UI

A `Separate bodies` button in the parts panel, acting on the selected leaf. Enabled
whenever a leaf is selected — it is cheap to press and it answers honestly when
there is nothing to do.

## Testing

Go:

- `shells.Split` returns one mesh per body for two disjoint cubes, and their
  triangles sum to the original.
- A hollow box returns **one** body, not two, and its volume is the shell's.
- Two cubes touching along an edge return two bodies, and each is watertight on its
  own though the pair was not.
- A single solid returns one mesh, unchanged.
- Debris nested in a body's box rides with it rather than becoming a body.
- `Tree.SplitMany` creates N children, and one `Undo` restores the parent.
- `SeparateBodies` on a single-body part reports 1 and leaves the tree alone.

Playwright:

- Loading `touchingcubes`, then Separate bodies: two parts, both `Closed: yes`,
  volumes summing to the original.
- Separate bodies on a single-body cube says so and leaves one part.
- Undo after separating restores one part.
- The sidebar names the body count after a repair that found more than one.

Each written to fail first, then the production code broken on purpose to confirm
the test notices. See CLAUDE.md's standing rules.
