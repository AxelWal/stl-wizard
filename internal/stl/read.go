package stl

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"stl-wizard/internal/geom"
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

// readASCII parses the ASCII STL grammar loosely: it collects every "vertex x y z"
// it finds and groups them in threes. Being permissive matters more than strict
// grammar checking here, because ASCII STL is written by a long tail of tools
// that disagree about whitespace and about which keywords are optional.
func readASCII(r io.ReaderAt, size int64) (*Mesh, error) {
	sr := io.NewSectionReader(r, 0, size)
	sc := bufio.NewScanner(sr)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var verts []geom.Vec3
	sawSolid := false
	line := 0

	for sc.Scan() {
		line++
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "solid":
			sawSolid = true
		case "vertex":
			if len(fields) < 4 {
				return nil, fmt.Errorf("line %d: vertex needs three coordinates, got %d", line, len(fields)-1)
			}
			var v geom.Vec3
			for i := 0; i < 3; i++ {
				f, err := strconv.ParseFloat(fields[i+1], 64)
				if err != nil {
					return nil, fmt.Errorf("line %d: coordinate %q is not a number", line, fields[i+1])
				}
				v[i] = f
			}
			verts = append(verts, v)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	if !sawSolid {
		return nil, errors.New("not an STL file: no binary header match and no \"solid\" keyword")
	}
	if len(verts) == 0 {
		return nil, errors.New("STL contains no triangles")
	}
	if len(verts)%3 != 0 {
		return nil, fmt.Errorf("STL has %d vertices, which is not a multiple of three", len(verts))
	}

	m := &Mesh{Tris: make([]Tri, 0, len(verts)/3)}
	for i := 0; i+2 < len(verts); i += 3 {
		m.Tris = append(m.Tris, Tri{A: verts[i], B: verts[i+1], C: verts[i+2]})
	}
	return m, nil
}
