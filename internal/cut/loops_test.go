package cut

import (
	"testing"

	"stl-cutter/internal/geom"
)

func TestBoundaryEdgesFindsTheEdgeLyingOnThePlane(t *testing.T) {
	// A triangle with edge A-B on z=0 and C above it.
	polys := []Polygon{{{0, 0, 0}, {1, 0, 0}, {0, 0, 1}}}
	w := geom.NewWelder(1e-9)
	edges := boundaryEdges(polys, zPlane, w, testEps)
	if len(edges) != 1 {
		t.Fatalf("got %d edges, want 1", len(edges))
	}
}

// A triangle lying wholly in the cut plane would otherwise contribute all three
// of its edges and corrupt the loop graph.
func TestBoundaryEdgesIgnoresCoplanarPolygons(t *testing.T) {
	polys := []Polygon{{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}}
	w := geom.NewWelder(1e-9)
	if edges := boundaryEdges(polys, zPlane, w, testEps); len(edges) != 0 {
		t.Fatalf("got %d edges, want 0 for a coplanar polygon", len(edges))
	}
}

// Two polygons sharing an on-plane edge means that edge is interior to a
// coplanar region, not part of the cap boundary. Such pairs must cancel.
func TestBoundaryEdgesCancelsDuplicatedEdges(t *testing.T) {
	shared := []Polygon{
		{{0, 0, 0}, {1, 0, 0}, {0, 0, 1}},
		{{1, 0, 0}, {0, 0, 0}, {0, 0, -1}},
	}
	w := geom.NewWelder(1e-9)
	if edges := boundaryEdges(shared, zPlane, w, testEps); len(edges) != 0 {
		t.Fatalf("got %d edges, want 0 — a doubled edge is interior", len(edges))
	}
}

func TestAssembleLoopsChainsASquare(t *testing.T) {
	w := geom.NewWelder(1e-9)
	ids := []int{
		w.ID(geom.Vec3{0, 0, 0}),
		w.ID(geom.Vec3{1, 0, 0}),
		w.ID(geom.Vec3{1, 1, 0}),
		w.ID(geom.Vec3{0, 1, 0}),
	}
	edges := [][2]int{{ids[0], ids[1]}, {ids[1], ids[2]}, {ids[2], ids[3]}, {ids[3], ids[0]}}

	loops, open := assembleLoops(edges, w)
	if open != 0 {
		t.Fatalf("open = %d, want 0", open)
	}
	if len(loops) != 1 || len(loops[0]) != 4 {
		t.Fatalf("got %d loops with lengths %v, want one loop of 4", len(loops), loopLens(loops))
	}
}

// Edges are given in scrambled order and with inconsistent direction, because
// that is what the clipper actually produces.
func TestAssembleLoopsIgnoresEdgeOrderAndDirection(t *testing.T) {
	w := geom.NewWelder(1e-9)
	p := []geom.Vec3{{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0}}
	id := func(i int) int { return w.ID(p[i]) }
	edges := [][2]int{{id(2), id(1)}, {id(3), id(0)}, {id(0), id(1)}, {id(2), id(3)}}

	loops, open := assembleLoops(edges, w)
	if open != 0 || len(loops) != 1 || len(loops[0]) != 4 {
		t.Fatalf("open=%d loops=%v, want one closed loop of 4", open, loopLens(loops))
	}
}

func TestAssembleLoopsSeparatesDisjointLoops(t *testing.T) {
	w := geom.NewWelder(1e-9)
	var edges [][2]int
	// Two separate triangles, far apart.
	for _, off := range []float64{0, 100} {
		a := w.ID(geom.Vec3{off, 0, 0})
		b := w.ID(geom.Vec3{off + 1, 0, 0})
		c := w.ID(geom.Vec3{off, 1, 0})
		edges = append(edges, [2]int{a, b}, [2]int{b, c}, [2]int{c, a})
	}
	loops, open := assembleLoops(edges, w)
	if open != 0 {
		t.Fatalf("open = %d, want 0", open)
	}
	if len(loops) != 2 {
		t.Fatalf("got %d loops, want 2", len(loops))
	}
}

// This is the non-manifold detection path: a missing edge means the loop cannot
// close, and the caller must be told rather than shipping a broken part.
func TestAssembleLoopsReportsAnUnclosableChain(t *testing.T) {
	w := geom.NewWelder(1e-9)
	p := []geom.Vec3{{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0}}
	id := func(i int) int { return w.ID(p[i]) }
	// Square with one side missing.
	edges := [][2]int{{id(0), id(1)}, {id(1), id(2)}, {id(2), id(3)}}

	loops, open := assembleLoops(edges, w)
	if open == 0 {
		t.Fatal("open = 0, want a report of the unclosed chain")
	}
	if len(loops) != 0 {
		t.Fatalf("got %d loops, want none — the chain never closed", len(loops))
	}
}

// Loop order must not depend on Go's randomised map iteration, or test output
// and exported geometry would vary run to run.
func TestAssembleLoopsIsDeterministic(t *testing.T) {
	build := func() ([]Polygon, int) {
		w := geom.NewWelder(1e-9)
		var edges [][2]int
		for _, off := range []float64{0, 50, 100} {
			a := w.ID(geom.Vec3{off, 0, 0})
			b := w.ID(geom.Vec3{off + 1, 0, 0})
			c := w.ID(geom.Vec3{off, 1, 0})
			edges = append(edges, [2]int{a, b}, [2]int{b, c}, [2]int{c, a})
		}
		return assembleLoops(edges, w)
	}
	first, _ := build()
	for i := 0; i < 5; i++ {
		got, _ := build()
		if len(got) != len(first) {
			t.Fatalf("run %d produced %d loops, first run produced %d", i, len(got), len(first))
		}
		for j := range got {
			if len(got[j]) != len(first[j]) || got[j][0] != first[j][0] {
				t.Fatalf("run %d loop %d differs from the first run", i, j)
			}
		}
	}
}

// Two triangles meeting at a single pinch vertex are two rings, not one. Walked
// from a leaf vertex, the old greedy walk emitted a single six-point
// self-intersecting polygon and reported open = 0, giving no signal that the cap
// it fed was malformed.
func TestAssembleLoopsSplitsAPinchVertexIntoTwoRings(t *testing.T) {
	w := geom.NewWelder(1e-9)
	a := w.ID(geom.Vec3{1, 0, 0})
	s := w.ID(geom.Vec3{0, 0, 0}) // the pinch
	c := w.ID(geom.Vec3{-1, 0, 0})
	d := w.ID(geom.Vec3{0, -1, 0})
	b := w.ID(geom.Vec3{0, 1, 0})
	edges := [][2]int{{a, s}, {s, c}, {c, d}, {d, s}, {s, b}, {b, a}}

	loops, open := assembleLoops(edges, w)
	if open != 0 {
		t.Errorf("open = %d, want 0", open)
	}
	if len(loops) != 2 {
		t.Fatalf("got %d loops with lengths %v, want two rings of 3", len(loops), loopLens(loops))
	}
	for i, l := range loops {
		if len(l) != 3 {
			t.Errorf("loop %d has %d vertices, want 3", i, len(l))
		}
		seen := map[geom.Vec3]bool{}
		for _, v := range l {
			if seen[v] {
				t.Errorf("loop %d revisits %v — the polygon self-intersects", i, v)
			}
			seen[v] = true
		}
	}
}

// A dangling whisker attached to a pinch vertex must not consume the valid ring
// it passes through. The old walk chased the whisker, failed to close, and
// discarded the ring entirely.
func TestAssembleLoopsKeepsAValidRingDespiteAWhisker(t *testing.T) {
	w := geom.NewWelder(1e-9)
	l := w.ID(geom.Vec3{-2, 0, 0})
	m := w.ID(geom.Vec3{-1, 0, 0})
	v := w.ID(geom.Vec3{0, 0, 0}) // pinch: whisker meets the triangle here
	x := w.ID(geom.Vec3{1, 0, 0})
	y := w.ID(geom.Vec3{0, 1, 0})
	edges := [][2]int{{l, m}, {m, v}, {v, x}, {x, y}, {y, v}}

	loops, open := assembleLoops(edges, w)
	if len(loops) != 1 || (len(loops) == 1 && len(loops[0]) != 3) {
		t.Fatalf("got %d loops with lengths %v, want the one triangle v-x-y", len(loops), loopLens(loops))
	}
	if open != 1 {
		t.Errorf("open = %d, want 1 for the whisker", open)
	}
}

// One open path is one hole. Starting from an interior vertex of the path used to
// walk it outward in both directions and report two.
func TestAssembleLoopsCountsOneOpenPathOnce(t *testing.T) {
	w := geom.NewWelder(1e-9)
	// Register the midpoint first so it takes the lowest id, which is what made
	// the old walk start there.
	mid := w.ID(geom.Vec3{0, 0, 0})
	p0 := w.ID(geom.Vec3{-2, 0, 0})
	p1 := w.ID(geom.Vec3{-1, 0, 0})
	p3 := w.ID(geom.Vec3{1, 0, 0})
	p4 := w.ID(geom.Vec3{2, 0, 0})
	edges := [][2]int{{p0, p1}, {p1, mid}, {mid, p3}, {p3, p4}}

	loops, open := assembleLoops(edges, w)
	if len(loops) != 0 {
		t.Errorf("got %d loops, want none — nothing closes", len(loops))
	}
	if open != 1 {
		t.Errorf("open = %d, want 1 for a single open path", open)
	}
}

func loopLens(loops []Polygon) []int {
	out := make([]int, len(loops))
	for i, l := range loops {
		out[i] = len(l)
	}
	return out
}
