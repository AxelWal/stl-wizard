// Command cutdemo cuts an STL with a bounded plane and writes the two parts.
// It exists to exercise the geometry library on real files, ahead of any UI:
// load its output in a slicer and confirm both parts come up watertight.
//
//	cutdemo -in model.stl -out part \
//	        -origin 7.5,25,5 -normal 0,1,0 -width 15 -height 20
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"stl-cutter/internal/cut"
	"stl-cutter/internal/geom"
	"stl-cutter/internal/meshcheck"
	"stl-cutter/internal/stl"
)

// errFlagsReported marks an error the flag package has already reported itself.
var errFlagsReported = errors.New("flag parsing failed")

func parseVec3(s string) (geom.Vec3, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 3 {
		return geom.Vec3{}, fmt.Errorf("want three comma-separated numbers, got %q", s)
	}
	var v geom.Vec3
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return geom.Vec3{}, fmt.Errorf("component %d of %q is not a number", i+1, s)
		}
		// ParseFloat accepts "NaN" and "Inf". A non-finite coordinate defeats the
		// geometry's own validation downstream: NaN <= 0 is false, so a NaN extent
		// passes Spec.Validate, and NaN plane distances classify every point as
		// lying on the plane.
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return geom.Vec3{}, fmt.Errorf("component %d of %q is not a finite number", i+1, s)
		}
		v[i] = f
	}
	return v, nil
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("cutdemo", flag.ContinueOnError)
	fs.SetOutput(out)

	inPath := fs.String("in", "", "input STL file")
	outPrefix := fs.String("out", "", "output path prefix; writes <prefix>_part1.stl and <prefix>_part2.stl")
	originStr := fs.String("origin", "0,0,0", "point on the cutting plane, as x,y,z")
	normalStr := fs.String("normal", "0,0,1", "plane normal, as x,y,z; part2 is the side it points to")
	width := fs.Float64("width", 1e9, "rectangle extent across the plane; the default spans any model")
	height := fs.Float64("height", 1e9, "rectangle extent along the plane; the default spans any model")

	if err := fs.Parse(args); err != nil {
		// fs has already written both the complaint and the usage text to out.
		// Returning err verbatim would have main print the same complaint again on
		// stderr, so it is wrapped in a marker main knows to stay quiet about.
		return fmt.Errorf("%w: %v", errFlagsReported, err)
	}
	if *inPath == "" {
		return errors.New("-in is required")
	}
	if *outPrefix == "" {
		return errors.New("-out is required")
	}

	origin, err := parseVec3(*originStr)
	if err != nil {
		return fmt.Errorf("-origin: %w", err)
	}
	normal, err := parseVec3(*normalStr)
	if err != nil {
		return fmt.Errorf("-normal: %w", err)
	}

	// The extents are not checked here. cut.Spec.Validate rejects a non-finite or
	// non-positive extent for every caller, and duplicating that guard in one
	// front end only teaches the next one that it does not need its own.
	mesh, err := stl.ReadFile(*inPath)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "loaded %d triangles, volume %.4g, %s\n",
		len(mesh.Tris), mesh.Volume(), meshcheck.Check(mesh, mesh.Epsilon()))

	res, err := cut.Split(mesh, cut.SpecFromNormal(origin, normal, *width, *height))
	if err != nil {
		return err
	}

	for _, w := range res.Warnings {
		fmt.Fprintf(out, "warning: %s\n", w)
	}

	for i, part := range []*stl.Mesh{res.Part1, res.Part2} {
		path := fmt.Sprintf("%s_part%d.stl", *outPrefix, i+1)
		if err := stl.WriteFile(path, part); err != nil {
			return err
		}
		b := part.BBox()
		fmt.Fprintf(out, "%s: %d triangles, volume %.4g, size %.4gx%.4gx%.4g, %s\n",
			path, len(part.Tris), part.Volume(),
			b.Size()[0], b.Size()[1], b.Size()[2],
			meshcheck.Check(part, mesh.Epsilon()))
	}

	// Reported after the files are written, never instead of them: the library's
	// contract is that a flagged part is returned, not withheld, and the whole
	// point of this command is to load the output in a slicer and look at it. But
	// a warning on stdout is invisible to a script, so a flagged part has to reach
	// the exit code too.
	if !res.Watertight() {
		return errors.New("the cut did not produce two closed solids; the files above were written anyway")
	}
	return nil
}

func main() {
	err := run(os.Args[1:], os.Stdout)
	switch {
	case err == nil:
		return
	case errors.Is(err, errFlagsReported):
		// The flag package already printed the problem and the usage text.
	default:
		fmt.Fprintln(os.Stderr, "cutdemo:", err)
	}
	os.Exit(1)
}
