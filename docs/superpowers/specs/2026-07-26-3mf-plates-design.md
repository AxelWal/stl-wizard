# Exporting a multi-plate 3MF — design

Export every part to its own build plate in one 3MF, each oriented to minimise
overhangs.

## The format is known, not guessed

Multi-plate 3MF is not in the core specification. It is a Bambu Studio / OrcaSlicer
extension, and PrusaSlicer and Cura have no plate concept at all, so this targets
Bambu and Orca.

Bambu Studio is installed here, so rather than infer the schema it was asked to
write one: six 120mm cubes exported with `--arrange 1 --export-3mf` produced six
plates with one object each. `Metadata/model_settings.config`:

```xml
<config>
  <object id="2">
    <metadata key="name" value="cube.stl"/>
    <metadata key="extruder" value="1"/>
    <part id="1" subtype="normal_part">
      <metadata key="name" value="cube.stl"/>
    </part>
  </object>
  <plate>
    <metadata key="plater_id" value="1"/>
    <metadata key="plater_name" value=""/>
    <metadata key="locked" value="false"/>
    <metadata key="gcode_file" value=""/>
    <model_instance>
      <metadata key="object_id" value="2"/>
      <metadata key="instance_id" value="0"/>
    </model_instance>
  </plate>
</config>
```

Bambu's own file uses the production extension — external `3D/Objects/object_N.model`
files, `p:UUID` on everything, `requiredextensions="p"`. None of that is needed. A
single `3D/3dmodel.model` with an inline `<mesh>` per object is plain core 3MF, and
that is what gets written.

Also observed: plates are laid out side by side in world coordinates, plate 1's items
near (95, 95) and plate 2's near (335, 95). The world position and the
`model_instance` assignment have to agree, or a part is assigned to one plate and
drawn on another.

## Verification is a round trip, not an assertion

Whether a slicer accepts this file is not something to assert in a comment. Bambu
Studio can load a 3MF and re-export it, so:

1. write the file
2. `flatpak run com.bambulab.BambuStudio out.3mf --export-3mf check.3mf`
3. read `check.3mf`'s `model_settings.config` and confirm the plates survived, one
   object each

That is an end-to-end check against the real consumer. It cannot run in CI — the
slicer is not there — so it lives in a script and in the manual checklist, run here
before this is called done.

## `internal/orient`

```go
type Spec struct {
    OverhangDegrees float64 // 0 means 45
    MaxCandidates   int     // 0 means 64 flat-face directions plus the 6 axes
}

type Result struct {
    Down         geom.Vec3  // the model-space direction that ends up pointing down
    Rotation     [9]float64 // row-major, maps model space to printed space
    OverhangArea float64
    BaseArea     float64
    Height       float64
    Considered   int
}

func Best(m *stl.Mesh, s Spec) Result
```

**The score.** For a candidate direction `d` — the direction that will point down —
a triangle with unit outward normal `n` and area `A` is an overhang when
`n·d > cos(45°)`. That is the whole test: one dot product per triangle per candidate.

**Triangles resting on the plate are excluded, and this is the detail the feature
lives or dies on.** After rotating `d` to point down, height is proportional to
`-(v·d)`, so the lowest plane is at `max(v·d)`. A triangle whose vertices all sit
within a tolerance of that maximum is resting on the plate and is supported by it.
Counting the base as overhang would make a flat bottom the worst possible score, and
the search would then actively avoid resting a part flat — the exact opposite of what
is wanted.

**Candidates: the 6 axes, plus the directions the model's own area points in.** A
printed part usually rests on a face it already has, so the outward normal of a large
face is a good guess at a down direction. Normals are bucketed on a coarse direction
grid with their areas accumulated, and the heaviest buckets win, each contributing
its area-weighted mean normal.

This is a deliberately narrow search, chosen over a Fibonacci sphere. It is fast and
right for mechanical parts. On an organic or lattice model it may find nothing better
than axis-aligned, and the honest thing is to measure that rather than claim
otherwise — the figures go in the README. `ponytail:` widening it later is one
function: generate more candidate directions and hand them to the same scorer.

**Tie-break: largest base area.** Equal overhang goes to whichever orientation rests
on more flat material — best adhesion, least likely to peel. Without a tie-break the
result would be an accident of candidate ordering, unstable across runs and
untestable.

**Cost.** Score on an area-weighted sample of triangles above 100k, then re-score the
best few exactly. The score is an area integral, so a sample estimates it well, and
this keeps a 1.75M-triangle part under a second.

## `internal/threemf`

```go
type Plate struct {
    Name string
    Mesh *stl.Mesh
    // Transform is the 4x3 the build item carries: rotation then translation.
    Transform [12]float64
}

func Write(w io.Writer, plates []Plate) error
```

`archive/zip` from the standard library. Files written:

    [Content_Types].xml
    _rels/.rels
    3D/3dmodel.model                 one object per plate, inline mesh, one build item each
    Metadata/model_settings.config   one <plate> per plate

**The transform convention is a trap.** 3MF stores a 4x3 matrix and transforms a
point as a *row vector* — `p' = p·M` — so the stored 3x3 is the transpose of the
usual column-vector rotation. Getting it backwards yields a part rotated the wrong
way or mirrored, and it would look plausible. A test therefore applies the transform
exactly as 3MF defines it and asserts the part lands sitting on z=0 inside its plate.

## App and UI

`ExportPlates()` opens the native save dialog; `ExportPlatesTo(path)` takes a path so
the suite can drive it, the same split as `OpenModel`/`OpenPath`.

Plate size comes from the bed fields that already exist. A part too large for the
plate is exported anyway and named in the warnings — refusing would withhold work the
user can still slice by hand, and silence would be worse than either.

Every leaf part gets a plate, in tree order. Plates are placed on a grid in world
coordinates, stride `bed + 20mm`, so the assignment and the drawn position agree.

Button: `Export 3MF plates…`.

## Testing

Go, `orient`:

- A box 10x10x40 is laid down, not stood up: the best down direction is a large face,
  and the resulting height is 10 rather than 40.
- A cube scores zero overhang whichever way up, and the tie-break picks a full face.
- A shape with one obvious flat face and one spiky side rests on the flat face.
- The base is excluded: a flat-bottomed part must not score its own bottom as
  overhang. Asserted by checking a plain box scores exactly zero.
- `Rotation` really maps `Down` to `(0,0,-1)`.
- Every candidate is a unit vector, and `Considered` is at least 6.

Go, `threemf`:

- The archive contains exactly the four expected entries and the XML parses.
- One `<plate>` per part, each with one `model_instance` naming a distinct object.
- Applying the build item transform as 3MF defines it puts every part on z=0 and
  inside its own plate's footprint.
- Vertex and triangle counts survive the round trip.
- A part flagged not watertight is still written, and named in the warnings.

Playwright: export to a temp path, then assert the file exists, is a ZIP, and holds
one plate per part. The native dialog is out of reach, hence `ExportPlatesTo`.

Bambu Studio round trip, by hand and scripted: as above.

Each test written to fail first, then the production code broken on purpose to
confirm the test notices. See CLAUDE.md's standing rules.
