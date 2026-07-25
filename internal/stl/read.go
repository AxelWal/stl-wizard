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
			// The size does not match a binary file. Try ASCII, but if that fails
			// too, a truncated or corrupt binary file is the likelier explanation
			// and saying so is far more useful than relaying a parse complaint
			// about a file that was never ASCII.
			m, asciiErr := readASCII(r, size)
			if asciiErr == nil {
				return m, nil
			}
			return nil, fmt.Errorf("file is %d bytes and is not valid ASCII STL (%v); read as binary STL its header declares %d triangles, which would require %d bytes — the file is truncated or corrupt",
				size, asciiErr, count, 84+binaryTriSize*count)
		}
	}
	return readASCII(r, size)
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
