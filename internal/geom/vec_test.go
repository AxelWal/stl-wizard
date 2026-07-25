package geom

import (
	"math"
	"testing"
)

func TestCrossIsRightHanded(t *testing.T) {
	x := Vec3{1, 0, 0}
	y := Vec3{0, 1, 0}
	got := x.Cross(y)
	want := Vec3{0, 0, 1}
	if got != want {
		t.Fatalf("x cross y = %v, want %v", got, want)
	}
}

func TestDotAndLen(t *testing.T) {
	a := Vec3{3, 4, 0}
	if got := a.Len(); got != 5 {
		t.Fatalf("Len = %v, want 5", got)
	}
	if got := a.Dot(Vec3{1, 0, 0}); got != 3 {
		t.Fatalf("Dot = %v, want 3", got)
	}
}

func TestUnitOfZeroVectorDoesNotProduceNaN(t *testing.T) {
	got := Vec3{0, 0, 0}.Unit()
	for i, c := range got {
		if math.IsNaN(c) {
			t.Fatalf("component %d is NaN, want 0", i)
		}
	}
}

// Less must be a strict total order — this is what makes edge intersection
// canonical, so two triangles sharing an edge compute the identical point.
func TestLessIsLexicographicAndAntisymmetric(t *testing.T) {
	a := Vec3{1, 2, 3}
	b := Vec3{1, 2, 4}
	if !a.Less(b) {
		t.Fatal("expected a < b on the third component")
	}
	if b.Less(a) {
		t.Fatal("Less is not antisymmetric")
	}
	if a.Less(a) {
		t.Fatal("Less must be strict — a < a is false")
	}
}
