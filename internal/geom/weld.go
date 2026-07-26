package geom

import (
	"math"
	"runtime"
	"sync"
)

// Welder maps near-coincident points onto a single canonical index. Cut-edge
// loop assembly relies on it: STL stores vertices per triangle, and real files
// routinely differ in the low bits for what is meant to be one shared vertex,
// so chaining edges by exact equality would leave loops open.
//
// This is the hot spot of the whole application. meshcheck, shells, cut, repair
// and threemf all weld a whole mesh, and on a 492k-triangle model a profile of
// meshcheck.Check spent 81% of its time in runtime.mapaccess1 with 75% inside
// ID. The two structures below exist to keep the number of lookups down; see ID.
type Welder struct {
	eps float64
	// side is the grid cell's edge length, deliberately 2*eps rather than eps.
	// See ID for why that is what makes eight cells enough.
	side float64
	// exact answers a point that has been seen bit-for-bit before, which on real
	// geometry is most of them: 83% of the vertices in a 492k-triangle export are
	// exact repeats, and on that file exact matching alone finds every weld the
	// eps probe does. It is a fast path, never the only path — generated meshes do
	// differ in the low bits, so a miss still falls through to the probe.
	exact map[Vec3]int
	grid  map[[3]int64][]int
	pts   []Vec3
}

func NewWelder(eps float64) *Welder {
	return &Welder{
		eps:   eps,
		side:  2 * eps,
		exact: make(map[Vec3]int),
		grid:  make(map[[3]int64][]int),
	}
}

func (w *Welder) cell(v Vec3) [3]int64 {
	return [3]int64{
		int64(math.Floor(v[0] / w.side)),
		int64(math.Floor(v[1] / w.side)),
		int64(math.Floor(v[2] / w.side)),
	}
}

// ID returns the canonical index for v, inserting it if no existing point lies
// within eps.
//
// Cells are 2*eps on a side, and that is the whole trick. A point sits at some
// offset p in [0, 2*eps) within its cell, so v-eps reaches the cell below only
// when p < eps, and v+eps reaches the cell above only when p >= eps — exactly
// one neighbour per axis, never both. Probing the point's own cell and that one
// neighbour on each axis therefore covers the entire eps ball in **eight**
// lookups and cannot miss a candidate.
//
// With cells of eps all three cells on every axis are always in range, which is
// why the earlier version had to probe 27 and why shrinking that number is not a
// matter of accepting a little risk: 8 is exact, and anything smaller is not.
func (w *Welder) ID(v Vec3) int {
	// A nil exact map means the caller has already deduplicated by bits, so remembering
	// them again would be a map insert per point that can never be read. WeldAll does
	// exactly that, and it is a third of this function's cost on a large mesh.
	if w.exact != nil {
		if i, ok := w.exact[v]; ok {
			return i
		}
	}

	c := w.cell(v)
	// The one neighbour that the eps ball reaches on each axis.
	var near [3]int64
	for a := range 3 {
		if v[a]-float64(c[a])*w.side < w.eps {
			near[a] = c[a] - 1
		} else {
			near[a] = c[a] + 1
		}
	}

	eps2 := w.eps * w.eps
	for _, x := range [2]int64{c[0], near[0]} {
		for _, y := range [2]int64{c[1], near[1]} {
			for _, z := range [2]int64{c[2], near[2]} {
				for _, i := range w.grid[[3]int64{x, y, z}] {
					d := w.pts[i].Sub(v)
					if d.Dot(d) <= eps2 {
						// Remember the exact bits too, so a repeat of this
						// spelling of the point skips the probe next time.
						if w.exact != nil {
							w.exact[v] = i
						}
						return i
					}
				}
			}
		}
	}

	id := len(w.pts)
	w.pts = append(w.pts, v)
	w.grid[c] = append(w.grid[c], id)
	if w.exact != nil {
		w.exact[v] = id
	}
	return id
}

func (w *Welder) Points() []Vec3 { return w.pts }

func (w *Welder) Len() int { return len(w.pts) }

// WeldAll welds a whole batch of points at once and returns the canonical index of each,
// together with the canonical points in index order. It is exactly what feeding the same
// slice through a Welder one point at a time produces — same numbering, same
// representatives — and TestWeldAllAgreesWithTheSerialWelder holds it to that.
//
// It exists to use the cores. Welding is this application's dominant cost, and the eps
// probe cannot simply be split across goroutines: two points within eps may fall in cells
// that different shards own, so a shard would have to consult its neighbours and the merge
// is where the bugs would live.
//
// What makes this safe is that the *exact* pass needs no neighbours at all. Points with
// identical bits are identical wherever they are in space, so grouping them shards
// perfectly — and on real geometry that is 83% of the vertices. The eps probe then runs
// over the distinct points only, six times fewer on a real export, and stays serial where
// it is easy to be sure of.
//
// The representative is preserved by visiting distinct points in order of first appearance,
// which is the point the serial welder would have kept. Without that, welded output such as
// a 3MF's vertex order would depend on the number of cores.
func WeldAll(pts []Vec3, eps float64) ([]int, []Vec3) {
	ids := make([]int, len(pts))
	if len(pts) == 0 {
		return ids, nil
	}

	// first[i] is the index of the earliest point holding exactly these bits.
	first := exactGroups(pts)

	// The distinct points, in order of first appearance, welded serially. The welder's own
	// exact map is switched off: these points are already distinct by bits, so it could
	// never answer anything.
	w := NewWelder(eps)
	w.exact = nil
	canonOf := make(map[int]int, len(pts)/4)
	for i := range pts {
		if first[i] == i {
			canonOf[i] = w.ID(pts[i])
		}
	}

	parallelChunks(len(pts), func(lo, hi int) {
		for i := lo; i < hi; i++ {
			ids[i] = canonOf[first[i]]
		}
	})
	return ids, w.Points()
}

// exactGroups maps each point to the index of the first point with identical bits.
//
// Sharded on a hash of the point, so each shard owns a disjoint set of exact values and
// two goroutines can never look at the same one. Indices are bucketed per shard first, in
// input order, so a shard sees its own points in the order they appeared and the earliest
// always wins.
func exactGroups(pts []Vec3) []int {
	first := make([]int, len(pts))
	shards := shardCount(len(pts))
	if shards == 1 {
		seen := make(map[Vec3]int, len(pts)/4)
		for i, p := range pts {
			if j, ok := seen[p]; ok {
				first[i] = j
			} else {
				seen[p] = i
				first[i] = i
			}
		}
		return first
	}

	// Bucket indices by shard, keeping input order within each.
	buckets := make([][]int, shards)
	chunks := shards
	perChunk := make([][][]int, chunks)
	parallelChunks(len(pts), func(lo, hi int) {
		local := make([][]int, shards)
		for i := lo; i < hi; i++ {
			s := int(hashVec(pts[i]) % uint64(shards))
			local[s] = append(local[s], i)
		}
		perChunk[chunkIndex(lo, len(pts), chunks)] = local
	})
	for s := range shards {
		for c := range chunks {
			if perChunk[c] != nil {
				buckets[s] = append(buckets[s], perChunk[c][s]...)
			}
		}
	}

	var wg sync.WaitGroup
	for s := range shards {
		wg.Add(1)
		go func(idx []int) {
			defer wg.Done()
			seen := make(map[Vec3]int, len(idx)/2+1)
			for _, i := range idx {
				if j, ok := seen[pts[i]]; ok {
					first[i] = j
				} else {
					seen[pts[i]] = i
					first[i] = i
				}
			}
		}(buckets[s])
	}
	wg.Wait()
	return first
}

// hashVec hashes a point by its bits. FNV-1a over the three float64s: cheap, and it only
// has to spread values across shards.
func hashVec(v Vec3) uint64 {
	h := uint64(14695981039346656037)
	for a := range 3 {
		b := math.Float64bits(v[a])
		for k := 0; k < 8; k++ {
			h ^= (b >> (8 * k)) & 0xff
			h *= 1099511628211
		}
	}
	return h
}

// shardCount keeps small batches on one goroutine, where the coordination costs more than
// the work saved.
func shardCount(n int) int {
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

// parallelChunks runs fn over contiguous ranges of [0, n) concurrently, in as many chunks
// as shardCount asks for.
func parallelChunks(n int, fn func(lo, hi int)) {
	chunks := shardCount(n)
	if chunks == 1 {
		fn(0, n)
		return
	}
	var wg sync.WaitGroup
	for c := range chunks {
		lo, hi := chunkBounds(c, n, chunks)
		if lo >= hi {
			continue
		}
		wg.Add(1)
		go func(lo, hi int) {
			defer wg.Done()
			fn(lo, hi)
		}(lo, hi)
	}
	wg.Wait()
}

func chunkBounds(c, n, chunks int) (int, int) {
	per := (n + chunks - 1) / chunks
	lo := c * per
	hi := lo + per
	if lo > n {
		lo = n
	}
	if hi > n {
		hi = n
	}
	return lo, hi
}

func chunkIndex(lo, n, chunks int) int {
	per := (n + chunks - 1) / chunks
	if per == 0 {
		return 0
	}
	return lo / per
}
