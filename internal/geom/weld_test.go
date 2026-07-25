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
