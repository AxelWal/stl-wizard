// Package meshcheck provides the mesh invariants the geometry tests assert on.
// It lives outside internal/cut so that cut's tests and any future tooling can
// share one definition of "watertight".
package meshcheck

import (
	"fmt"
	"runtime"
	"strings"
	"sync"

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
	// Weld every vertex in one batch rather than one at a time, so the exact-match pass
	// can use the cores. The result is identical to feeding them through a Welder in
	// order; see geom.WeldAll.
	corners := make([]geom.Vec3, 0, len(m.Tris)*3)
	for _, t := range m.Tris {
		corners = append(corners, t.A, t.B, t.C)
	}
	welded, _ := geom.WeldAll(corners, eps)

	// Every edge, as a record, so the tally can be shared out across the cores. A
	// degenerate triangle contributes none and is marked with a == -1.
	recs := make([]edgeRec, len(m.Tris)*3)
	degenerate := make([]int, shardsFor(len(m.Tris)))
	forEachChunk(len(m.Tris), func(c, lo, hi int) {
		for ti := lo; ti < hi; ti++ {
			ids := [3]int32{int32(welded[ti*3]), int32(welded[ti*3+1]), int32(welded[ti*3+2])}
			if ids[0] == ids[1] || ids[1] == ids[2] || ids[2] == ids[0] {
				degenerate[c]++
				recs[ti*3], recs[ti*3+1], recs[ti*3+2] = deadEdge, deadEdge, deadEdge
				continue
			}
			for i := range 3 {
				a, b := ids[i], ids[(i+1)%3]
				forward := true
				if a > b {
					a, b, forward = b, a, false
				}
				recs[ti*3+i] = edgeRec{a: a, b: b, forward: forward}
			}
		}
	})

	var rep Report
	for _, d := range degenerate {
		rep.Degenerate += d
	}

	// Count traversals of each undirected edge, in each direction. Sharded on a hash of
	// the edge, so every traversal of one edge lands in the same shard and no two
	// goroutines ever touch the same map. Each shard reads the whole record slice and
	// skips what is not its own: linear scanning is cheap, and it buys a map phase
	// divided as many ways as there are shards without a lock or a merge step.
	shards := shardsFor(len(recs))
	open := make([]int, shards)
	misoriented := make([]int, shards)
	var wg sync.WaitGroup
	for s := range shards {
		wg.Add(1)
		go func(s int) {
			defer wg.Done()
			type tally struct{ forward, backward int32 }
			// A value in the map rather than a pointer to one: a pointer means an
			// allocation per distinct edge, 1.5 million of them on a large model.
			edges := make(map[[2]int32]tally, len(recs)/shards+1)
			for _, r := range recs {
				if r.a < 0 || int(hashEdge(r.a, r.b)%uint64(shards)) != s {
					continue
				}
				key := [2]int32{r.a, r.b}
				e := edges[key]
				if r.forward {
					e.forward++
				} else {
					e.backward++
				}
				edges[key] = e
			}
			for _, e := range edges {
				switch total := e.forward + e.backward; {
				case total != 2:
					open[s]++
				case e.forward != 1 || e.backward != 1:
					misoriented[s]++
				}
			}
		}(s)
	}
	wg.Wait()
	for s := range shards {
		rep.OpenEdges += open[s]
		rep.Misoriented += misoriented[s]
	}
	return rep
}

// edgeRec is one traversal of one undirected edge: its two welded endpoints in ascending
// order, and whether the triangle walked them that way round.
type edgeRec struct {
	a, b    int32
	forward bool
}

// deadEdge marks a slot contributed by a degenerate triangle, which has no edges worth
// counting. A negative endpoint cannot arise from a welded index.
var deadEdge = edgeRec{a: -1}

// hashEdge spreads an edge across shards. A multiply-xor mix, which is enough for that and
// costs a few instructions.
func hashEdge(a, b int32) uint64 {
	h := uint64(uint32(a))<<32 | uint64(uint32(b))
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	return h
}

// shardsFor keeps small meshes on one goroutine, where coordinating costs more than the
// work it saves.
func shardsFor(n int) int {
	if n < 8192 {
		return 1
	}
	s := runtime.GOMAXPROCS(0)
	if s > 16 {
		s = 16
	}
	if s < 1 {
		s = 1
	}
	return s
}

// forEachChunk runs fn over contiguous ranges of [0, n), concurrently, passing each its
// chunk number so it can accumulate into a slot of its own without synchronising.
func forEachChunk(n int, fn func(chunk, lo, hi int)) {
	chunks := shardsFor(n)
	if chunks == 1 {
		fn(0, 0, n)
		return
	}
	per := (n + chunks - 1) / chunks
	var wg sync.WaitGroup
	for c := range chunks {
		lo := c * per
		hi := min(lo+per, n)
		if lo >= hi {
			continue
		}
		wg.Add(1)
		go func(c, lo, hi int) {
			defer wg.Done()
			fn(c, lo, hi)
		}(c, lo, hi)
	}
	wg.Wait()
}
