// Command meshrepair reports what is wrong with an STL and optionally repairs it.
//
// It exists because the interesting files are large — tens of megabytes, millions
// of triangles — and pushing one of those through the window to find out whether
// it is even broken is the slow way round.
//
//	meshrepair -in model.stl                      # just the verdict
//	meshrepair -in model.stl -out fixed.stl       # repair and write
//
// Exits non-zero when the mesh is not a closed solid, after repair if one was
// asked for, so it can gate a script. The file is still written either way: a
// partly repaired mesh is more use than none, and withholding it would hide what
// happened.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"stl-cutter/internal/meshcheck"
	"stl-cutter/internal/repair"
	"stl-cutter/internal/stl"
)

var errFlagsReported = errors.New("flag parsing failed")

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("meshrepair", flag.ContinueOnError)
	fs.SetOutput(out)

	inPath := fs.String("in", "", "input STL file")
	outPath := fs.String("out", "", "write the repaired mesh here; omit to only report")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errFlagsReported, err)
	}
	if *inPath == "" {
		return errors.New("-in is required")
	}

	start := time.Now()
	mesh, err := stl.ReadFile(*inPath)
	if err != nil {
		return err
	}
	eps := mesh.Epsilon()
	read := time.Since(start)

	b := mesh.BBox().Size()
	fmt.Fprintf(out, "%s\n", *inPath)
	fmt.Fprintf(out, "  %d triangles, size %.4g x %.4g x %.4g, volume %.6g  (read in %s)\n",
		len(mesh.Tris), b[0], b[1], b[2], mesh.Volume(), read.Round(time.Millisecond))

	before := meshcheck.Check(mesh, eps)
	fmt.Fprintf(out, "  before: %s\n", before)

	if *outPath == "" {
		if !before.OK() {
			// Say whether repair could even help, rather than sending the user off to
			// run it and get "filled 0 holes" with no explanation.
			probe := repair.Repair(&stl.Mesh{Tris: append([]stl.Tri(nil), mesh.Tris...)}, eps)
			explainKinds(out, probe)
			if probe.BoundaryEdges == 0 && probe.DegenerateRemoved == 0 && probe.ShellsDropped == 0 {
				return errors.New("the mesh is not a closed solid, and repair cannot help with this kind of defect")
			}
			return errors.New("the mesh is not a closed solid; pass -out to repair it")
		}
		return nil
	}

	volBefore := mesh.Volume()
	start = time.Now()
	res := repair.Repair(mesh, eps)
	took := time.Since(start)

	fmt.Fprintf(out, "  repair: filled %d hole(s) with %d triangle(s), removed %d degenerate,\n"+
		"          dropped %d empty shell(s) totalling %d triangle(s)  (in %s)\n",
		res.HolesFilled, res.TrianglesAdded, res.DegenerateRemoved,
		res.ShellsDropped, res.TrianglesRemoved, took.Round(time.Millisecond))
	fmt.Fprintf(out, "  after:  %s\n", res.After)
	fmt.Fprintf(out, "  volume: %.6g -> %.6g (%+.4g)\n", volBefore, mesh.Volume(), mesh.Volume()-volBefore)

	if err := stl.WriteFile(*outPath, mesh); err != nil {
		return err
	}
	fmt.Fprintf(out, "  wrote %s (%d triangles)\n", *outPath, len(mesh.Tris))

	if !res.After.OK() {
		explainKinds(out, res)
		return errors.New("the repaired mesh is still not a closed solid; the file above was written anyway")
	}
	return nil
}

// explainKinds says which defects repair can address and which it cannot, so
// "filled 0 holes" is never the whole story a user gets.
func explainKinds(out io.Writer, res repair.Result) {
	if res.NonManifoldEdges > 0 {
		fmt.Fprintf(out, "  note: %d edge(s) have more than two triangles meeting along them.\n"+
			"        That is two surfaces touching, not a hole — filling cannot fix it.\n"+
			"        Most slicers still print such a model; a mesh tool can separate the\n"+
			"        surfaces if yours refuses.\n", res.NonManifoldEdges)
	}
	if res.After.Misoriented > 0 {
		fmt.Fprintf(out, "  note: %d edge(s) have triangles wound the wrong way. Repair closes holes\n"+
			"        and drops degenerate triangles; it does not turn triangles around.\n",
			res.After.Misoriented)
	}
	if res.BoundaryEdges > 0 && res.After.OpenEdges > 0 && res.NonManifoldEdges == 0 {
		fmt.Fprintf(out, "  note: %d hole edge(s) were found but %d remain open, so some rim did not\n"+
			"        close. That is a defect in repair, not in your model.\n",
			res.BoundaryEdges, res.After.OpenEdges)
	}
}

func main() {
	err := run(os.Args[1:], os.Stdout)
	switch {
	case err == nil:
		return
	case errors.Is(err, errFlagsReported):
	default:
		fmt.Fprintln(os.Stderr, "meshrepair:", err)
	}
	os.Exit(1)
}
