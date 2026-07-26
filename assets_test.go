package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"stl-wizard/internal/fixtures"
	"stl-wizard/internal/stl"
)

func TestPartHandlerServesBinarySTL(t *testing.T) {
	var s Session
	if err := s.Load("cube.stl", fixtures.Cube(10)); err != nil {
		t.Fatalf("Load: %v", err)
	}
	id := s.Tree().Root.ID

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/part/"+id+".stl", nil)
	newPartHandler(&s).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "model/stl" {
		t.Errorf("Content-Type = %q, want model/stl", got)
	}

	// The bytes must be a real STL the loader can parse, not just non-empty.
	body := rec.Body.Bytes()
	m, err := stl.Read(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("served bytes do not parse as STL: %v", err)
	}
	if len(m.Tris) != 12 {
		t.Errorf("got %d triangles, want 12", len(m.Tris))
	}
}

func TestPartHandlerRejectsUnknownPart(t *testing.T) {
	var s Session
	if err := s.Load("cube.stl", fixtures.Cube(10)); err != nil {
		t.Fatalf("Load: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/part/nope.stl", nil)
	newPartHandler(&s).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestPartHandlerIgnoresOtherPaths(t *testing.T) {
	var s Session
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	newPartHandler(&s).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for a path this handler does not own", rec.Code)
	}
}

func TestPartHandlerRejectsNonGET(t *testing.T) {
	var s Session
	if err := s.Load("cube.stl", fixtures.Cube(10)); err != nil {
		t.Fatalf("Load: %v", err)
	}
	id := s.Tree().Root.ID

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/part/"+id+".stl", nil)
	newPartHandler(&s).ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

// A part that has been split has no mesh of its own, so asking for it is a 404
// rather than an empty file.
func TestPartHandlerRejectsASplitPart(t *testing.T) {
	var s Session
	if err := s.Load("cube.stl", fixtures.Cube(10)); err != nil {
		t.Fatalf("Load: %v", err)
	}
	rootID := s.Tree().Root.ID
	err := s.WithTree(func(tr *Tree) error {
		_, _, err := tr.Split(rootID, fixtures.Cube(5), fixtures.Cube(4), true, true)
		return err
	})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/part/"+rootID+".stl", nil)
	newPartHandler(&s).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for a part that has been split", rec.Code)
	}
}
