package stl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	"stl-cutter/internal/geom"
)

func float32bits(v float64) uint32 { return math.Float32bits(float32(v)) }

// Read parses an STL from r. Binary and ASCII are distinguished by arithmetic,
// not by the leading token: a binary file's size is exactly 84 + 50*count, and
// binary files that happen to begin with "solid" are common enough that
// sniffing the token misclassifies them.
func Read(r io.ReaderAt, size int64) (*Mesh, error) {
	if size >= 84 {
		var countBuf [4]byte
		if _, err := r.ReadAt(countBuf[:], 80); err == nil {
			count := int64(binary.LittleEndian.Uint32(countBuf[:]))
			if 84+binaryTriSize*count == size {
				return readBinary(r, size)
			}
		}
	}

	// The size did not match a binary file. Decide what this is by looking at the
	// content rather than at the triangle count: those four bytes at offset 80 are
	// meaningless in a file that is not binary STL, and a diagnosis derived from
	// them would be confident and wrong.
	if looksLikeText(r, size) {
		return readASCII(r, size)
	}
	if size >= 84 {
		return nil, fmt.Errorf("not a recognised STL file: %d bytes of binary data whose declared triangle count does not match the file length", size)
	}
	return nil, fmt.Errorf("not a recognised STL file: only %d bytes, too short for a binary header and not ASCII text", size)
}

// looksLikeText reports whether the start of r is printable ASCII, which is what
// separates an ASCII STL from binary data. Only the first 512 bytes are examined;
// that is ample to classify a file whose second line is already "facet normal".
func looksLikeText(r io.ReaderAt, size int64) bool {
	n := size
	if n > 512 {
		n = 512
	}
	if n == 0 {
		return false
	}
	buf := make([]byte, n)
	if _, err := r.ReadAt(buf, 0); err != nil && err != io.EOF {
		return false
	}
	for _, b := range buf {
		if b == '\t' || b == '\n' || b == '\r' {
			continue
		}
		if b < 0x20 || b > 0x7e {
			return false
		}
	}
	return true
}

func ReadFile(path string) (*Mesh, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	m, err := Read(f, info.Size())
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return m, nil
}

func readBinary(r io.ReaderAt, size int64) (*Mesh, error) {
	var countBuf [4]byte
	if _, err := r.ReadAt(countBuf[:], 80); err != nil {
		return nil, fmt.Errorf("read triangle count: %w", err)
	}
	count := int(binary.LittleEndian.Uint32(countBuf[:]))
	if count == 0 {
		return nil, errors.New("STL contains no triangles")
	}
	if want := int64(84) + binaryTriSize*int64(count); want != size {
		return nil, fmt.Errorf("file is %d bytes, but its header declares %d triangles (%d bytes) — file is truncated or corrupt", size, count, want)
	}

	raw := make([]byte, binaryTriSize*count)
	if _, err := r.ReadAt(raw, 84); err != nil {
		return nil, fmt.Errorf("read triangle data: %w", err)
	}

	m := &Mesh{Tris: make([]Tri, count)}
	for i := 0; i < count; i++ {
		// Skip the stored normal at offset 0; it is advisory and frequently
		// wrong in files produced by other tools. Normal() recomputes it.
		base := i * binaryTriSize
		var v [9]float64
		for j := 0; j < 9; j++ {
			bits := binary.LittleEndian.Uint32(raw[base+12+j*4:])
			v[j] = float64(math.Float32frombits(bits))
		}
		m.Tris[i] = Tri{
			A: geom.Vec3{v[0], v[1], v[2]},
			B: geom.Vec3{v[3], v[4], v[5]},
			C: geom.Vec3{v[6], v[7], v[8]},
		}
	}
	return m, nil
}

// readASCII is implemented in Task 5.
func readASCII(r io.ReaderAt, size int64) (*Mesh, error) {
	return nil, errors.New("ASCII STL reading not implemented yet")
}
