// Package meshcheck provides the mesh invariants the geometry tests assert on.
// It lives outside internal/cut so that cut's tests and any future tooling can
// share one definition of "watertight".
package meshcheck

import (
	"fmt"

	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

// Report summarises how far a mesh is from being a closed, consistently wound
// solid.
type Report struct {
	// OpenEdges counts undirected edges not shared by exactly two triangles.
	OpenEdges int
	// Misoriented counts edges shared by two triangles that traverse them in
	// the same direction, meaning one of the two is wound backwards.
	Misoriented int
}

func (r Report) OK() bool { return r.OpenEdges == 0 && r.Misoriented == 0 }

func (r Report) String() string {
	if r.OK() {
		return "watertight"
	}
	return fmt.Sprintf("not watertight: %d open edges, %d misoriented edges", r.OpenEdges, r.Misoriented)
}

// Check welds vertices within eps and then tallies edge usage. Welding is
// required: STL stores vertices per triangle, so exact comparison would report
// every edge of a perfectly good mesh as open.
func Check(m *stl.Mesh, eps float64) Report {
	w := geom.NewWelder(eps)

	// For each undirected edge, count traversals in each direction.
	type tally struct{ forward, backward int }
	edges := make(map[[2]int]*tally, len(m.Tris)*3)

	for _, t := range m.Tris {
		ids := [3]int{w.ID(t.A), w.ID(t.B), w.ID(t.C)}
		for i := 0; i < 3; i++ {
			a, b := ids[i], ids[(i+1)%3]
			if a == b {
				continue // degenerate edge of a zero-area triangle
			}
			key := [2]int{a, b}
			forward := true
			if a > b {
				key = [2]int{b, a}
				forward = false
			}
			e := edges[key]
			if e == nil {
				e = &tally{}
				edges[key] = e
			}
			if forward {
				e.forward++
			} else {
				e.backward++
			}
		}
	}

	var rep Report
	for _, e := range edges {
		total := e.forward + e.backward
		switch {
		case total != 2:
			rep.OpenEdges++
		case e.forward != 1 || e.backward != 1:
			rep.Misoriented++
		}
	}
	return rep
}

func IsWatertight(m *stl.Mesh, eps float64) bool { return Check(m, eps).OK() }
