// Package meshcheck provides the mesh invariants the geometry tests assert on.
// It lives outside internal/cut so that cut's tests and any future tooling can
// share one definition of "watertight".
package meshcheck

import (
	"fmt"
	"strings"

	"stl-wizard/internal/geom"
	"stl-wizard/internal/stl"
)

// Report summarises how far a mesh is from being a closed, consistently wound
// solid.
type Report struct {
	// OpenEdges counts undirected edges not shared by exactly two triangles.
	OpenEdges int
	// Misoriented counts edges shared by two triangles that traverse them in
	// the same direction, meaning one of the two is wound backwards.
	Misoriented int
	// Degenerate counts triangles with a repeated vertex. They have zero area and
	// no valid boundary, so none of their edges are tallied: a degenerate
	// triangle's two remaining edges are the same edge traversed both ways, which
	// would otherwise self-cancel into a convincing but fictional shared edge and
	// hide a genuinely open one.
	Degenerate int
}

func (r Report) OK() bool { return r.OpenEdges == 0 && r.Misoriented == 0 && r.Degenerate == 0 }

func (r Report) String() string {
	if r.OK() {
		return "watertight"
	}
	var parts []string
	if r.OpenEdges != 0 {
		parts = append(parts, fmt.Sprintf("%d open edges", r.OpenEdges))
	}
	if r.Misoriented != 0 {
		parts = append(parts, fmt.Sprintf("%d misoriented edges", r.Misoriented))
	}
	if r.Degenerate != 0 {
		parts = append(parts, fmt.Sprintf("%d degenerate triangles", r.Degenerate))
	}
	return "not watertight: " + strings.Join(parts, ", ")
}

// Check welds vertices within eps and then tallies edge usage. Welding is
// required: STL stores vertices per triangle, so exact comparison would report
// every edge of a perfectly good mesh as open.
func Check(m *stl.Mesh, eps float64) Report {
	w := geom.NewWelder(eps)

	// For each undirected edge, count traversals in each direction.
	type tally struct{ forward, backward int }
	edges := make(map[[2]int]*tally, len(m.Tris)*3)

	var rep Report
	for _, t := range m.Tris {
		ids := [3]int{w.ID(t.A), w.ID(t.B), w.ID(t.C)}
		if ids[0] == ids[1] || ids[1] == ids[2] || ids[2] == ids[0] {
			rep.Degenerate++
			continue
		}
		for i := 0; i < 3; i++ {
			a, b := ids[i], ids[(i+1)%3]
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
