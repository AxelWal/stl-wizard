package main

import (
	"bytes"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"stl-wizard/internal/fixtures"
	"stl-wizard/internal/geom"
	"stl-wizard/internal/stl"
)

func TestParseVec3(t *testing.T) {
	got, err := parseVec3("1.5,-2,3e0")
	if err != nil {
		t.Fatalf("parseVec3: %v", err)
	}
	if got != (geom.Vec3{1.5, -2, 3}) {
		t.Fatalf("got %v, want {1.5 -2 3}", got)
	}
}

func TestParseVec3RejectsBadInput(t *testing.T) {
	for _, s := range []string{"", "1,2", "1,2,3,4", "1,2,x"} {
		if _, err := parseVec3(s); err == nil {
			t.Errorf("parseVec3(%q): expected an error", s)
		}
	}
}

func TestRunCutsAFileAndWritesBothParts(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "cube.stl")
	if err := stl.WriteFile(in, fixtures.Cube(10)); err != nil {
		t.Fatalf("write input: %v", err)
	}
	prefix := filepath.Join(dir, "out")

	var log bytes.Buffer
	err := run([]string{
		"-in", in, "-out", prefix,
		"-origin", "5,5,5", "-normal", "0,0,1",
		"-width", "100", "-height", "100",
	}, &log)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, log.String())
	}

	for _, name := range []string{"out_part1.stl", "out_part2.stl"} {
		m, err := stl.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if got := m.Volume(); math.Abs(got-500) > 1 {
			t.Errorf("%s volume = %v, want about 500", name, got)
		}
	}

	// "not watertight: ..." contains "watertight", so the substring alone proves
	// nothing about the verdict. Assert the failure text is absent as well.
	if strings.Contains(log.String(), "not watertight") {
		t.Errorf("a part came back not watertight:\n%s", log.String())
	}
	if !strings.Contains(log.String(), "watertight") {
		t.Errorf("output did not report watertightness:\n%s", log.String())
	}
}

func TestRunReportsAMissingInputFile(t *testing.T) {
	var log bytes.Buffer
	err := run([]string{"-in", "/nonexistent/nope.stl", "-out", "x"}, &log)
	if err == nil {
		t.Fatal("expected an error for a missing input file")
	}
}

func TestRunRequiresInputAndOutput(t *testing.T) {
	var log bytes.Buffer
	if err := run([]string{"-out", "x"}, &log); err == nil {
		t.Error("expected an error when -in is missing")
	}
	if err := run([]string{"-in", "x.stl"}, &log); err == nil {
		t.Error("expected an error when -out is missing")
	}
}

func TestParseVec3RejectsNonFiniteValues(t *testing.T) {
	for _, s := range []string{"1,2,NaN", "Inf,0,0", "1,-Inf,3", "1,2,+Inf"} {
		if _, err := parseVec3(s); err == nil {
			t.Errorf("parseVec3(%q): expected an error for a non-finite component", s)
		}
	}
}

// NaN used to slip past Spec.Validate, because NaN <= 0 is false, and the cut
// then came back silently unbounded and reported as good. Validate rejects it
// now, so the command inherits the refusal rather than carrying its own copy —
// this test still passes, via the library's error instead of the command's.
func TestRunRejectsNonFiniteExtents(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "cube.stl")
	if err := stl.WriteFile(in, fixtures.Cube(10)); err != nil {
		t.Fatalf("write input: %v", err)
	}

	for _, bad := range []string{"NaN", "Inf", "0", "-5"} {
		var log bytes.Buffer
		err := run([]string{
			"-in", in, "-out", filepath.Join(dir, "out"),
			"-origin", "5,5,5", "-normal", "0,0,1",
			"-width", bad, "-height", "100",
		}, &log)
		if err == nil {
			t.Errorf("-width %s: expected an error", bad)
		}
	}
}
