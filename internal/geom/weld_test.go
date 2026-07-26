package geom

import "testing"

func TestWelderReusesIndexForNearCoincidentPoints(t *testing.T) {
	w := NewWelder(1e-6)
	a := w.ID(Vec3{1, 2, 3})
	b := w.ID(Vec3{1 + 1e-9, 2, 3})
	if a != b {
		t.Fatalf("ids %d and %d differ; points within eps must weld", a, b)
	}
	if w.Len() != 1 {
		t.Fatalf("Len = %d, want 1", w.Len())
	}
}

func TestWelderSeparatesDistinctPoints(t *testing.T) {
	w := NewWelder(1e-6)
	a := w.ID(Vec3{0, 0, 0})
	b := w.ID(Vec3{1, 0, 0})
	if a == b {
		t.Fatal("distinct points must not weld")
	}
	if w.Len() != 2 {
		t.Fatalf("Len = %d, want 2", w.Len())
	}
}

// The failure mode this guards against: quantising to a grid of side eps puts
// two points closer than eps into different cells whenever they straddle a
// cell boundary. Probing neighbouring cells is what fixes it.
func TestWelderWeldsAcrossACellBoundary(t *testing.T) {
	const eps = 1e-6
	w := NewWelder(eps)
	// Sits exactly on a cell boundary; the partner is eps/4 the other side.
	a := w.ID(Vec3{eps * 3, 0, 0})
	b := w.ID(Vec3{eps*3 - eps/4, 0, 0})
	if a != b {
		t.Fatalf("ids %d and %d differ; points straddling a cell boundary must weld", a, b)
	}
}

func TestWelderPointsReturnsInsertionOrder(t *testing.T) {
	w := NewWelder(1e-6)
	w.ID(Vec3{5, 5, 5})
	w.ID(Vec3{1, 1, 1})
	pts := w.Points()
	if len(pts) != 2 || pts[0] != (Vec3{5, 5, 5}) || pts[1] != (Vec3{1, 1, 1}) {
		t.Fatalf("Points = %v, want insertion order", pts)
	}
}

// Welding must not depend on where a point falls inside its grid cell. The probe covers a
// fixed block of cells around the point, and if that block is chosen wrongly — too few
// cells, or the neighbour on the wrong side — then two points within eps land in cells the
// probe never looks at and come back as two distinct ids.
//
// The sweep walks a pair of points across a whole cell in each axis, including exactly on
// the boundary, which is where a wrong block shows up.
func TestWelderWeldsWithinEpsWhereverTheCellBoundaryFalls(t *testing.T) {
	const eps = 0.01
	// Offsets covering more than one cell width for any plausible cell size, at a step
	// fine enough to land on and either side of a boundary.
	var offsets []float64
	for k := 0; k <= 60; k++ {
		offsets = append(offsets, float64(k)*eps/4)
	}

	for axis := 0; axis < 3; axis++ {
		for _, off := range offsets {
			// Separations at and just inside eps must weld; the separation is placed on
			// each axis in turn so a probe that is right on one axis and wrong on another
			// cannot hide.
			for _, sep := range []float64{0, eps * 0.25, eps * 0.9, eps * 0.999} {
				var a, b Vec3
				a[axis] = off
				b[axis] = off + sep
				w := NewWelder(eps)
				if ia, ib := w.ID(a), w.ID(b); ia != ib {
					t.Fatalf("axis %d offset %g separation %g: got ids %d and %d, want one point",
						axis, off, sep, ia, ib)
				}
				// And the same pair in the other order, since insertion decides which
				// cell holds the stored point.
				w2 := NewWelder(eps)
				if ib, ia := w2.ID(b), w2.ID(a); ia != ib {
					t.Fatalf("axis %d offset %g separation %g reversed: got ids %d and %d, want one point",
						axis, off, sep, ib, ia)
				}
			}
		}
	}
}

// The other half of the contract: a separation comfortably beyond eps must stay two points,
// wherever it falls. A probe that grew too greedy would pass the test above and fail this.
func TestWelderKeepsPointsBeyondEpsApartWhereverTheCellBoundaryFalls(t *testing.T) {
	const eps = 0.01
	for axis := 0; axis < 3; axis++ {
		for k := 0; k <= 60; k++ {
			off := float64(k) * eps / 4
			for _, sep := range []float64{eps * 1.5, eps * 2.5, eps * 10} {
				var a, b Vec3
				a[axis] = off
				b[axis] = off + sep
				w := NewWelder(eps)
				if ia, ib := w.ID(a), w.ID(b); ia == ib {
					t.Fatalf("axis %d offset %g separation %g: both got id %d, want two points",
						axis, off, sep, ia)
				}
			}
		}
	}
}

// A diagonal separation is the case a per-axis argument can get wrong: each axis is within
// eps but the distance is not, and vice versa.
func TestWelderMeasuresDistanceNotPerAxisDifference(t *testing.T) {
	const eps = 0.01
	w := NewWelder(eps)
	// Each axis differs by 0.9*eps, so every axis is "close", but the distance is
	// 0.9*eps*sqrt(3) = 1.56*eps — beyond the tolerance.
	a := Vec3{0, 0, 0}
	b := Vec3{eps * 0.9, eps * 0.9, eps * 0.9}
	if w.ID(a) == w.ID(b) {
		t.Errorf("points %v and %v are %g apart, beyond eps %g, but welded", a, b, 0.9*eps*1.7320508, eps)
	}
}

func BenchmarkWelderID(b *testing.B) {
	// A grid of points with each one repeated, which is what real geometry looks like:
	// most vertices are shared between triangles and arrive more than once.
	const n = 60
	var pts []Vec3
	for x := 0; x < n; x++ {
		for y := 0; y < n; y++ {
			for z := 0; z < 8; z++ {
				p := Vec3{float64(x) * 0.5, float64(y) * 0.5, float64(z) * 0.5}
				pts = append(pts, p, p, p) // 3 arrivals, as a shared vertex has
			}
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := NewWelder(1e-6)
		for _, p := range pts {
			w.ID(p)
		}
	}
	b.ReportMetric(float64(len(pts)), "points")
}
