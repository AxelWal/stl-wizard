package stl

import (
	"bytes"
	"math"
	"strings"
	"testing"
)

const asciiTetra = `solid tetra
  facet normal 0 0 -1
    outer loop
      vertex 0 0 0
      vertex 0 1 0
      vertex 1 0 0
    endloop
  endfacet
  facet normal 0 -1 0
    outer loop
      vertex 0 0 0
      vertex 1 0 0
      vertex 0 0 1
    endloop
  endfacet
  facet normal -1 0 0
    outer loop
      vertex 0 0 0
      vertex 0 0 1
      vertex 0 1 0
    endloop
  endfacet
  facet normal 0.577 0.577 0.577
    outer loop
      vertex 1 0 0
      vertex 0 1 0
      vertex 0 0 1
    endloop
  endfacet
endsolid tetra
`

func TestReadASCIITetrahedron(t *testing.T) {
	r := strings.NewReader(asciiTetra)
	m, err := Read(r, int64(len(asciiTetra)))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(m.Tris) != 4 {
		t.Fatalf("got %d triangles, want 4", len(m.Tris))
	}
	// A corner tetrahedron of edge 1 has volume 1/6.
	if got := m.Volume(); math.Abs(got-1.0/6.0) > 1e-9 {
		t.Fatalf("Volume = %v, want %v", got, 1.0/6.0)
	}
}

func TestReadASCIIAcceptsScientificNotationAndTabs(t *testing.T) {
	src := "solid s\n\tfacet normal 0 0 1\n\t\touter loop\n" +
		"\t\t\tvertex 0 0 0\n\t\t\tvertex 1e0 0 0\n\t\t\tvertex 0 1.0E+00 0\n" +
		"\t\tendloop\n\tendfacet\nendsolid s\n"
	m, err := Read(strings.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(m.Tris) != 1 {
		t.Fatalf("got %d triangles, want 1", len(m.Tris))
	}
}

func TestReadASCIIRejectsVertexCountNotMultipleOfThree(t *testing.T) {
	src := "solid s\nfacet normal 0 0 1\nouter loop\nvertex 0 0 0\nvertex 1 0 0\nendloop\nendfacet\nendsolid s\n"
	if _, err := Read(strings.NewReader(src), int64(len(src))); err == nil {
		t.Fatal("expected an error for a facet with two vertices")
	}
}

func TestReadASCIIRejectsEmptySolid(t *testing.T) {
	src := "solid empty\nendsolid empty\n"
	if _, err := Read(strings.NewReader(src), int64(len(src))); err == nil {
		t.Fatal("expected an error for a solid with no facets")
	}
}

func TestReadASCIIRejectsMalformedVertex(t *testing.T) {
	src := "solid s\nvertex 0 0 zero\nendsolid s\n"
	if _, err := Read(strings.NewReader(src), int64(len(src))); err == nil {
		t.Fatal("expected an error for an unparseable coordinate")
	}
}

func TestReadRejectsCompleteGarbage(t *testing.T) {
	raw := []byte("this is not an STL file at all")
	if _, err := Read(bytes.NewReader(raw), int64(len(raw))); err == nil {
		t.Fatal("expected an error for non-STL input")
	}
}
