# A 3MF Bambu Studio treats as a project, and colours per part

## First: what is already right

The reported list of likely faults was checked against `internal/threemf/threemf.go` line
by line, and against a reference export from the **installed** Bambu Studio 2.7.1.62
(`flatpak run com.bambulab.BambuStudio --export-3mf`). Most of it we already handle:

| Reported trap | Actual state |
| --- | --- |
| `[Content_Types].xml` / `_rels/.rels` missing or malformed | Both written. Root relationship is `http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel` targeting `/3D/3dmodel.model` — correct |
| Object id mismatch between model and settings | Ids come from one array used in both places; a plate cannot reference a missing object |
| Transform is a 12-value row-major 4×3, not a 16-value 4×4 | `Plate.Transform` is `[12]float64`, and `exportPlatesTo` transposes because 3MF multiplies a *row* vector. `TestExportPlatesWritesTheTransformTheWayThreeMFReadsIt` tilts a box by two odd angles to catch a missing transpose |
| Missing `unit="millimeter"` | Present on `<model>` |
| XML entity encoding of attribute values | `escape()` runs every name through `xml.EscapeText` |
| Degenerate triangles rejected by the importer | `writeMesh` drops any triangle whose corners welded together |

Two claims from the secondary sources are **wrong for this slicer version**, and the
reference export is the evidence. Do not "fix" the code to match them:

- `<plate>` carries `<metadata key="plater_id" value="1"/>` **children**, not
  `plater_id="1"` attributes.
- The part subtype is `subtype="normal_part"`, not `model_part`.

## What is actually missing

**No `Metadata/project_settings.config`.** This is the real defect and it has a measured
consequence. The reference export contains:

```
printable_area   = ['0x0', '200x0', '200x200', '0x200']
printable_height = 100
```

Those are Bambu's defaults. Because we ship no settings file, Bambu applies them to our
project — so a part laid out for the 256×256×256 bed the user selected in the app can open
sitting off the bed. CLAUDE.md already records the symptom ("Bambu's default bed here is
200x200x100, not the app's 220x220x250 default, so a part placed by our stride can land
off Bambu's bed") without naming the cause. The cause is this file.

Also absent, in descending order of mattering:

- `<metadata key="matrix">` on `<part>` — the reference carries a **16-value 4×4** here,
  distinct from the build item's 12-value 4×3. Ours omits it; Bambu then treats the part
  as unrotated within its object, which is right only because we put the rotation on the
  build item.
- `identify_id` on `<model_instance>` — the reference has it; ours does not.
- `<metadata name="BambuStudio:3mfVersion">1</metadata>` on the model element.
- Part ids sharing the object id. The reference uses `<object id="2">` with
  `<part id="1">` — separate counters. Ours reuses one number for both, which has not been
  observed to fail but does not match.
- `Metadata/slice_info.config`, thumbnails, `plate_1.json`, `filament_sequence.json`,
  `cut_information.xml` — all generated at slice time. Not needed and deliberately not
  written.

## Scope: two phases, and why colour splits in two

**Phase 1 — the project file, and one colour per part.** This is what the application can
express today. `stl-wizard` cuts a model into named parts and puts each on a plate; the
natural colour model is *this part prints in filament N*. That is `<metadata
key="extruder" value="N"/>` on the object in `model_settings.config`, plus the filament
arrays in `project_settings.config` that say what filament N actually is.

**Phase 2 — per-triangle painting.** `paint_color` on the `<triangle>` element in
`3dmodel.model` (`MMU_SEGMENTATION_ATTR` in OrcaSlicer's `bbs_3mf.cpp`; the sibling
attributes are `paint_supports`, `paint_seam`, `paint_fuzzy_skin`). Its value is a
compressed encoding of a per-facet segmentation tree, and I do not yet know that encoding
well enough to specify it. **Phase 2 is explicitly out of this spec** and needs its own
investigation against `bbs_3mf.cpp`, because writing a malformed `paint_color` is worse
than writing none: the importer rejects the file rather than ignoring the attribute.

Nothing in Phase 1 forecloses Phase 2 — a painted triangle is an extra attribute on
geometry Phase 1 already writes correctly.

## Phase 1 design

### `Metadata/project_settings.config`

A JSON object. The reference has 545 keys, and writing 545 keys we do not understand is how
this goes wrong — most are print-profile defaults the slicer supplies for itself. Write the
minimum that changes behaviour, and let Bambu fill the rest:

- `printable_area` and `printable_height` from the bed the user selected in the app, in the
  reference's own format: `['0x0', '256x0', '256x256', '0x256']` and `250`.
- `filament_colour`, `filament_type` — one entry per filament in use.
- `nozzle_diameter`, `version`, `from: "project"`.

**The index bases differ and this is the trap.** `filament_colour` and `filament_type` are
JSON arrays, **0-based**. The `extruder` value in `model_settings.config` is **1-based**.
So part → `extruder = k` means `filament_colour[k-1]`. One test asserts exactly this
correspondence, with at least three filaments so an off-by-one cannot pass by symmetry —
with two, a swap is a plausible-looking pass.

### The colour API

`threemf.Plate` gains `Filament int` — 1-based, matching what goes in the file, so nothing
has to remember to convert at the boundary. Zero means unset and is written as 1.

`threemf.Write` gains the filament table: colour and type per filament, 0-based as the JSON
is. Passing a `Plate.Filament` with no matching table entry is an error at the API, not a
file the slicer rejects later.

Where the colours come from in the UI is a separate question and not in this spec; the
export layer takes a table and an assignment per part.

### Testing

The existing threemf tests already unzip and assert structure, which is the pattern to
follow. New assertions:

- The archive contains `Metadata/project_settings.config` and it parses as JSON.
- `printable_area` and `printable_height` match the bed passed in — an actual number the
  test chooses, not a default, and not 200/100.
- Part with `Filament: 3` produces `extruder = "3"` and `filament_colour[2]` is that
  filament's colour. Mutating the code to `filament_colour[k]` must fail this.
- A name containing `&` and `"` round-trips through `escape` and the file still parses.
- `Write` rejects a plate whose `Filament` exceeds the table.

`scripts/verify-3mf.sh` remains the only check on whether the slicer *accepts* the result,
and it must be run by hand for this change, with `--arrange 0`. Add a second script step,
or extend that one, to assert the imported project's bed matches what we wrote — that is
the whole point of the change and a Go test cannot see it.

## Risks

- **545 keys of unknowns.** A partial `project_settings.config` may make Bambu apply
  defaults inconsistently, or refuse the file. Mitigation: `verify-3mf.sh` before and
  after, and be ready to grow the key set from the reference rather than from guesses.
- **Bambu version drift.** Everything here is read off 2.7.1.62. Another version may write
  different keys. The reference-export command is recorded above so it can be regenerated.
- **`printable_area` for a non-rectangular bed** is a polygon; every printer in the app's
  list is rectangular, so four corners suffice. Note it, do not generalise it.
