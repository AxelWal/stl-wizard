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
	"os"
	"strconv"
	"strings"

	"stl-cutter/internal/cut"
	"stl-cutter/internal/geom"
	"stl-cutter/internal/meshcheck"
	"stl-cutter/internal/stl"
)

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
		return err
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
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "cutdemo:", err)
		os.Exit(1)
	}
}
