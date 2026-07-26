package main

import (
	"bytes"
	"net/http"
	"strings"

	"stl-wizard/internal/stl"
)

// newPartHandler serves a part's geometry as binary STL.
//
// Mesh bytes travel this way rather than through a bound method because a bound
// method would JSON-encode every coordinate as a decimal string — for a million
// triangles that is both far slower and far larger than the binary form, which
// three.js STLLoader can parse directly.
//
// Wails calls this handler when a request does not match an embedded asset, so
// it only ever sees paths the frontend does not otherwise own.
func newPartHandler(s *Session) http.Handler {
	const prefix = "/part/"
	const suffix = ".stl"

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, prefix) || !strings.HasSuffix(r.URL.Path, suffix) {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "only GET is supported", http.StatusMethodNotAllowed)
			return
		}

		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix), suffix)
		mesh, ok := s.MeshFor(id)
		if !ok {
			http.NotFound(w, r)
			return
		}

		// Buffer so a write failure becomes a 500 rather than a truncated body
		// the loader would report as a corrupt file.
		var buf bytes.Buffer
		if err := stl.Write(&buf, mesh); err != nil {
			http.Error(w, "could not encode the part", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "model/stl")
		// A given id always names the same geometry — ids are unique per tree and
		// undo restores the very same mesh. What changes is whether the id resolves
		// at all: a part flips between 200 and 404 as it is split and un-split, and
		// caching either answer would go stale across that transition.
		w.Header().Set("Cache-Control", "no-store")
		w.Write(buf.Bytes())
	})
}
