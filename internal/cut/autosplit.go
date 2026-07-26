package cut

import (
	"fmt"
	"math"

	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

// maxAutoSplitDepth bounds the recursion. A model needing more than this many
// halvings on one axis is either enormous or the bed is unusable, and either way
// the user should hear about it rather than wait.
const maxAutoSplitDepth = 12

// Bed is a printer's build volume in millimetres.
type Bed struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// Valid reports whether the bed is usable. Exported so the app layer can reject a
// nonsense bed before doing any work, rather than duplicating the rules.
func (b Bed) Valid() error {
	for name, v := range map[string]float64{"width": b.X, "depth": b.Y, "height": b.Z} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("bed %s must be a finite number, got %v", name, v)
		}
		if v <= 0 {
			return fmt.Errorf("bed %s must be greater than zero, got %v", name, v)
		}
	}
	return nil
}

// Fits reports whether a bounding box sits inside the bed, axis for axis.
//
// ponytail: axis-aligned only — a part that would fit if turned diagonally is
// split anyway. Rotating to fit is a packing problem, and this is a splitter.
// Upgrade path if it matters: try the six axis permutations before splitting.
func (b Bed) Fits(box stl.BBox) bool {
	s := box.Size()
	return s[0] <= b.X && s[1] <= b.Y && s[2] <= b.Z
}

// AutoSplitStep is one planned cut. Depth records how many halvings deep it is,
// so a caller can show progress or stop early.
type AutoSplitStep struct {
	Spec  Spec `json:"spec"`
	Depth int  `json:"depth"`
}

// PlanAutoSplit works out the cuts needed to bring a model within a build volume.
//
// It plans rather than cuts, so the caller keeps control: the app applies each
// step through the same Split path a manual cut uses, which means the part tree,
// the undo history and the watertightness reporting all behave identically.
//
// Each step halves the longest overflowing axis with a rectangle bounded to
// that box's own cross-section, so it cannot also strike an unrelated sibling
// piece that happens to share the same footprint on the other two axes.
//
// Do not replay the whole plan across a tree of real meshes. Every step after
// the first is derived from a *predicted* bounding box, and a shape that does
// not fill its box — a U, an L, a hollow shell — makes those predictions
// over-estimates, which was found to let one branch's cut slice a sibling
// piece. Apply steps[0] to the mesh it was planned from, then replan from the
// real fragments; that is what App.AutoSplit does.
func PlanAutoSplit(m *stl.Mesh, bed Bed) ([]AutoSplitStep, error) {
	if err := bed.Valid(); err != nil {
		return nil, err
	}
	if len(m.Tris) == 0 {
		return nil, fmt.Errorf("mesh has no triangles")
	}

	var steps []AutoSplitStep
	var plan func(box stl.BBox, depth int)

	plan = func(box stl.BBox, depth int) {
		if bed.Fits(box) || depth >= maxAutoSplitDepth {
			return
		}

		// Halve whichever axis overflows by the most.
		size := box.Size()
		limits := [3]float64{bed.X, bed.Y, bed.Z}
		axis, worst := -1, 0.0
		for i := 0; i < 3; i++ {
			if over := size[i] - limits[i]; over > worst {
				axis, worst = i, over
			}
		}
		if axis < 0 {
			return
		}

		var normal geom.Vec3
		normal[axis] = 1
		centre := box.Min.Add(size.Scale(0.5))

		// Bound the rectangle to this box's own cross-section — the other two
		// axes — rather than the whole model. A step is planned for one specific
		// branch of the recursion; a sibling branch can share this box's exact
		// footprint on those two axes (it only differs on the axis being cut
		// here), and an unbounded rectangle would let this step also slice that
		// sibling, off-centre, at a position meant for a different box entirely.
		// Splitting only ever shrinks a mesh, so a real fragment's bbox is
		// always inside the box it was predicted from, meaning this bound can
		// never clip real geometry — it only ever excludes the wrong piece.
		var width, height float64
		switch axis {
		case 0:
			width, height = size[1], size[2] // normal +X: rectangle spans Y, Z
		case 1:
			width, height = size[0], size[2] // normal +Y: rectangle spans X, Z
		default:
			width, height = size[0], size[1] // normal +Z: rectangle spans X, Y
		}
		steps = append(steps, AutoSplitStep{
			Spec:  SpecFromNormal(centre, normal, width, height),
			Depth: depth,
		})

		// Both halves have the same bounding box bar the halved axis.
		lower, upper := box, box
		mid := centre[axis]
		lower.Max[axis] = mid
		upper.Min[axis] = mid
		plan(lower, depth+1)
		plan(upper, depth+1)
	}

	plan(m.BBox(), 0)
	return steps, nil
}
