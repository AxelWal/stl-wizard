package cut

import (
	"math"
	"math/rand"
	"testing"

	"stl-cutter/internal/fixtures"
	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

// Seed is fixed so a failure is reproducible; change it only to widen coverage,
// never to make a red run go green.
const randomCutSeed = 20260725

// randomCutFailureLimit is deliberately loose. Ear clipping still gives up on
// genuinely tangled remainders, and a plane exactly flush with a model face
// leaves T-junctions that no amount of capping closes, so a few percent of
// random cuts come back flagged and that is the honest answer rather than a
// bug. The limit is set well above the measured rate and far below the 14-29%
// that the ear-clip stall used to produce, so it catches a regression of that
// class without failing on float noise.
const randomCutFailureLimit = 0.08

// TestRandomBoundedCutsStayWatertight is the committed guard for the ear-clip
// stall fix.
//
// Before that fix, a cap loop ending in a near-collinear chain stalled the clip;
// a stalled clip returned no triangles at all, so an unclippable sliver of area
// ~1e-15 threw away an entire cap, and the missing cap left open edges that made
// later cutter planes fail too. Bounded rectangles produce those chains
// constantly, by grazing along model edges — which is why the rate was low for a
// rectangle spanning the whole model and high for the bounded rectangles that
// are the point of this package. The rates this test measures were what
// diagnosed it, and nothing guarded against regression until this test existed.
func TestRandomBoundedCutsStayWatertight(t *testing.T) {
	models := []struct {
		name string
		mesh *stl.Mesh
	}{
		{"cube", fixtures.Cube(10)},
		{"sphere", fixtures.UVSphere(10, 16, 12)},
		{"tube", fixtures.Tube(10, 6, 20, 24)},
		{"u", fixtures.UShape(10)},
	}

	const cutsPerModel = 120
	rng := rand.New(rand.NewSource(randomCutSeed))

	totalCuts, totalFlagged := 0, 0
	for _, m := range models {
		b := m.mesh.BBox()
		size := b.Size()
		span := math.Max(size[0], math.Max(size[1], size[2]))

		cuts, flagged := 0, 0
		for i := 0; i < cutsPerModel; i++ {
			origin := geom.Vec3{
				b.Min[0] + rng.Float64()*size[0],
				b.Min[1] + rng.Float64()*size[1],
				b.Min[2] + rng.Float64()*size[2],
			}
			// Half the cuts are axis-aligned and half oblique. Axis-aligned planes
			// graze whole model faces at once; oblique ones exercise the projected
			// bases where a legitimate sliver is easiest to mistake for nothing.
			var normal geom.Vec3
			if i%2 == 0 {
				normal[rng.Intn(3)] = 1
			} else {
				for {
					normal = geom.Vec3{rng.NormFloat64(), rng.NormFloat64(), rng.NormFloat64()}
					if normal.Len() > 1e-6 {
						break
					}
				}
				normal = normal.Unit()
			}
			// Bounded: the rectangle covers a fraction of the model, so its side
			// planes cut through material rather than sailing past it.
			w := (0.2 + 0.6*rng.Float64()) * span
			h := (0.2 + 0.6*rng.Float64()) * span

			res, err := Split(m.mesh, SpecFromNormal(origin, normal, w, h))
			if err != nil {
				// The cutter missed the model, grazed it, or swallowed it whole.
				// Split refuses those by contract; they are not cuts.
				continue
			}
			cuts++
			if !res.Watertight() {
				flagged++
			}
		}

		if cuts == 0 {
			t.Fatalf("%s: every cut was refused, so this model tested nothing", m.name)
		}
		rate := float64(flagged) / float64(cuts)
		t.Logf("%s: %d/%d cuts not watertight (%.1f%%)", m.name, flagged, cuts, rate*100)
		if rate > randomCutFailureLimit {
			t.Errorf("%s: %.1f%% of %d cuts came back not watertight, limit %.1f%%",
				m.name, rate*100, cuts, randomCutFailureLimit*100)
		}
		totalCuts += cuts
		totalFlagged += flagged
	}
	t.Logf("overall: %d/%d cuts not watertight (%.1f%%)",
		totalFlagged, totalCuts, float64(totalFlagged)/float64(totalCuts)*100)
}
