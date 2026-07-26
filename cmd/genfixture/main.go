// Command genfixture writes a named test fixture to an STL file, so fixtures can
// be opened in a slicer or fed to cutdemo without going through a Go test.
//
//	genfixture -name u -out testdata/u.stl
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"stl-wizard/internal/fixtures"
	"stl-wizard/internal/geom"
	"stl-wizard/internal/stl"
)

func build(name string) (*stl.Mesh, error) {
	switch name {
	case "u":
		return fixtures.UShape(10), nil
	case "cube":
		return fixtures.Cube(10), nil
	case "sphere":
		return fixtures.UVSphere(10, 48, 24), nil
	case "tube":
		return fixtures.Tube(5, 3, 20, 48), nil
	case "hollowbox":
		return fixtures.HollowBox(geom.Vec3{20, 20, 20}, 1), nil
	case "openbox":
		// Deliberately broken: a cube missing its top face, for exercising repair.
		return fixtures.OpenBox(20), nil
	case "cubewithflap":
		// Deliberately broken the way real cut output is: a sound cube with a
		// zero-volume two-triangle flap fused to one edge.
		return fixtures.CubeWithFlap(20), nil
	case "touchingcubes":
		// Deliberately broken the way real exports are: two sound cubes sharing one
		// edge, which is non-manifold rather than holed, so repair cannot fix it.
		return fixtures.TouchingCubes(20), nil
	default:
		return nil, fmt.Errorf("unknown fixture %q; want u, cube, sphere, tube, hollowbox, "+
			"openbox, touchingcubes or cubewithflap", name)
	}
}

func main() {
	name := flag.String("name", "", "fixture to write: u, cube, sphere, tube, hollowbox")
	out := flag.String("out", "", "output STL path")
	flag.Parse()

	err := func() error {
		if *name == "" || *out == "" {
			return errors.New("-name and -out are both required")
		}
		m, err := build(*name)
		if err != nil {
			return err
		}
		return stl.WriteFile(*out, m)
	}()
	if err != nil {
		fmt.Fprintln(os.Stderr, "genfixture:", err)
		os.Exit(1)
	}
}
