package stl

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"stl-cutter/internal/geom"
)

func TestBinaryRoundTripPreservesTriangles(t *testing.T) {
	in := unitCube()
	var buf bytes.Buffer
	if err := Write(&buf, in); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out, err := Read(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(out.Tris) != len(in.Tris) {
		t.Fatalf("got %d triangles, want %d", len(out.Tris), len(in.Tris))
	}
	// STL stores float32, so compare with a tolerance rather than for equality.
	for i := range in.Tris {
		for j, pair := range [3][2]geom.Vec3{
			{in.Tris[i].A, out.Tris[i].A},
			{in.Tris[i].B, out.Tris[i].B},
			{in.Tris[i].C, out.Tris[i].C},
		} {
			if pair[0].Sub(pair[1]).Len() > 1e-5 {
				t.Fatalf("tri %d vertex %d: got %v, want %v", i, j, pair[1], pair[0])
			}
		}
	}
}

// A header beginning with "solid" would make naive parsers treat our binary
// output as ASCII.
func TestWrittenHeaderIsNotMistakableForAscii(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, unitCube()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if strings.HasPrefix(string(buf.Bytes()[:5]), "solid") {
		t.Fatal("binary header must not start with \"solid\"")
	}
}

// The real-world trap: plenty of binary STLs in the wild start with the word
// "solid". Detection must use the size arithmetic, not the leading token.
func TestDetectsBinaryEvenWhenHeaderSaysSolid(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, unitCube()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	raw := buf.Bytes()
	copy(raw[:5], "solid")

	out, err := Read(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(out.Tris) != 12 {
		t.Fatalf("got %d triangles, want 12", len(out.Tris))
	}
}

func TestReadRejectsEmptyTriangleCount(t *testing.T) {
	raw := make([]byte, 84)
	binary.LittleEndian.PutUint32(raw[80:], 0)
	if _, err := Read(bytes.NewReader(raw), int64(len(raw))); err == nil {
		t.Fatal("expected an error for a zero-triangle file")
	}
}

func TestReadRejectsTruncatedFile(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, unitCube()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	raw := buf.Bytes()[:buf.Len()-20] // lop off part of the last triangle
	if _, err := Read(bytes.NewReader(raw), int64(len(raw))); err == nil {
		t.Fatal("expected an error for a truncated file")
	}
}

func TestWrittenNormalsMatchWinding(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, unitCube()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	raw := buf.Bytes()
	// First triangle's normal sits at offset 84.
	var n [3]float32
	for i := 0; i < 3; i++ {
		bits := binary.LittleEndian.Uint32(raw[84+i*4:])
		n[i] = math.Float32frombits(bits)
	}
	want := unitCube().Tris[0].Normal()
	got := geom.Vec3{float64(n[0]), float64(n[1]), float64(n[2])}
	if got.Sub(want).Len() > 1e-5 {
		t.Fatalf("stored normal %v, want %v", got, want)
	}
}
