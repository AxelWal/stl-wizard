# Task 8 Report: Planes and Five-Half-Space Bounded Cutter

## Files Created
- `internal/cut/plane.go` (implementation)
- `internal/cut/plane_test.go` (test)

## Implementation Summary

Created a new package `internal/cut` that defines:
- `Plane` struct with methods `Dist()` and `Classify()`
- `Side` type with constants `Outside`, `On`, `Inside`
- `Spec` struct representing a bounded cutting plane
- `SpecFromNormal()` function to construct a Spec from a normal vector
- `Spec.Basis()` method to generate a right-handed orthonormal basis
- `Spec.Validate()` method to validate the spec
- `Spec.Planes()` method to generate five inward-facing half-spaces

## Test Execution

### First Run (Before Implementation)
```
# stl-cutter/internal/cut [stl-cutter/internal/cut.test]
internal/cut/plane_test.go:11:7: undefined: Plane
internal/cut/plane_test.go:14:56: undefined: Inside
internal/cut/plane_test.go:17:57: undefined: Outside
internal/cut/plane_test.go:20:62: undefined: On
internal/cut/plane_test.go:28:7: undefined: SpecFromNormal
internal/cut/plane_test.go:40:7: undefined: SpecFromNormal
internal/cut/plane_test.go:67:10: undefined: SpecFromNormal
internal/cut/plane_test.go:72:22: undefined: Spec
internal/cut/plane_test.go:74:21: undefined: SpecFromNormal
internal/cut/plane_test.go:75:22: undefined: SpecFromNormal
FAIL	stl-cutter/internal/cut [build failed]
FAIL
```

Confirmed: Test failed with undefined types as expected in Step 2 of the brief.

### Final Run (After Implementation)
```
=== RUN   TestPlaneClassifyRespectsEpsilon
--- PASS: TestPlaneClassifyRespectsEpsilon (0.00s)
=== RUN   TestPlanesAllPointInwardAtTheOrigin
--- PASS: TestPlanesAllPointInwardAtTheOrigin (0.00s)
=== RUN   TestPlanesBoundTheRectangle
--- PASS: TestPlanesBoundTheRectangle (0.00s)
=== RUN   TestValidateRejectsBadSpecs
--- PASS: TestValidateRejectsBadSpecs (0.00s)
=== RUN   TestBasisIsRightHanded
--- PASS: TestBasisIsRightHanded (0.00s)
PASS
ok  	stl-cutter/internal/cut	0.002s
```

All 5 tests pass.

## Commit Details
- SHA: `19071e3`
- Message: `feat: add planes and the five-half-space bounded cutter`
- Files: 2 changed, 225 insertions(+)

## Key Implementation Details

### Planes Method
The `Planes()` method returns exactly 5 half-spaces in the specified order:
1. Element 0: The cut plane itself (user-positioned plane)
2. Elements 1-4: The four bounded rectangle sides

All planes are designed to point inward (inside means >= 0 distance). The rectangle sides use negated normals for the max-side planes to ensure all planes point toward the center of the bounded region.

### Basis Method
The `Basis()` method ensures that `u × v == Normal` (exactly), which is critical for cap triangulation. This relies on:
1. Re-orthogonalizing the input basis vectors using Gram-Schmidt
2. Computing v as the cross product of the normalized normal and u
3. All basis vectors are unit-length

### Validation
The `Validate()` method checks:
- Normal vector is non-zero
- Width and Height are positive
- Basis vectors U and V are non-zero and non-parallel

## Concerns
None. The implementation exactly matches the brief's specification, all tests pass on the first run after implementation, and the two critical load-bearing requirements are correctly implemented:
1. All five planes point inward
2. Basis satisfies `u × v == Normal` exactly

## Fix: Validate rejects a U parallel to the normal

### Finding
`Spec.Validate()` checked that `U` and `V` were non-parallel to each other, but never checked either against `Normal`. `Basis()` re-orthogonalises `U` against `Normal` by Gram-Schmidt, so a `U` parallel to `Normal` collapsed to the zero vector — and `geom.Vec3.Unit()` returns zero for a zero vector rather than NaN, so nothing complained. With `N` zero on all four rectangle-side planes, `Dist` was always `>= 0`, so the cutter silently degenerated to an unbounded half-space instead of erroring.

### Change
In `internal/cut/plane.go`, added a guard in `Validate()`, placed after the existing zero-length checks and before the existing U/V-parallel check:

```go
	// U must not be parallel to Normal. Basis re-orthogonalises U against Normal,
	// so a parallel U collapses to the zero vector, and a zero basis makes all four
	// rectangle-side planes no-ops — the cutter silently loses its bound and cuts
	// the whole model instead of the region the user marked out.
	if math.Abs(s.U.Unit().Dot(s.Normal.Unit())) > 1-1e-6 {
		return errors.New("cutting plane basis vector U is parallel to the plane normal, which would leave the cut unbounded")
	}
```

`math` and `errors` were already imported; no import changes were needed. Nothing else in `plane.go` was touched — `Basis()`, `Planes()`, `Classify`, and `SpecFromNormal` are unchanged.

### Tests added
Appended to `internal/cut/plane_test.go`:
- `TestValidateRejectsUParallelToNormal` — the reviewer's repro case must now be rejected.
- `TestBasisReorthogonalisesASkewedU` — a skewed-but-usable `U` should be accepted and `Basis()` should still produce a correct orthonormal, right-handed pair.
- `TestPlanesElementZeroIsTheCutPlane` — `Planes()[0]` must be the user-placed cut plane (needed by later tasks that index into it directly).

### Verification experiment
Before committing, confirmed `TestValidateRejectsUParallelToNormal` actually exercises the new guard by disabling it (commenting out the `if` block) and running just that test:

```
$ go test ./internal/cut/ -run TestValidateRejectsUParallelToNormal -v
=== RUN   TestValidateRejectsUParallelToNormal
    plane_test.go:118: expected an error for a U parallel to the normal
--- FAIL: TestValidateRejectsUParallelToNormal (0.00s)
FAIL
FAIL	stl-cutter/internal/cut	0.002s
FAIL
```

Confirmed FAIL (validation returned nil, matching the reviewer's repro). Restored the guard and reran:

```
$ go test ./internal/cut/ -run TestValidateRejectsUParallelToNormal -v
=== RUN   TestValidateRejectsUParallelToNormal
--- PASS: TestValidateRejectsUParallelToNormal (0.00s)
PASS
ok  	stl-cutter/internal/cut	0.002s
```

Confirmed PASS.

Also confirmed the existing `TestValidateRejectsBadSpecs` "non-orthogonal basis" case (`Normal: {0,0,1}, U: {1,0,0}, V: {1,0,0}`) still reaches the U/V-parallel error it originally expected: `U` there is perpendicular to `Normal` (dot = 0), so the new guard does not fire, and validation falls through to the existing parallel-U/V check as before.

### Full test suite

Command: `go test ./... -v`

Output (all packages, all tests passed):

```
=== RUN   TestPlaneClassifyRespectsEpsilon
--- PASS: TestPlaneClassifyRespectsEpsilon (0.00s)
=== RUN   TestPlanesAllPointInwardAtTheOrigin
--- PASS: TestPlanesAllPointInwardAtTheOrigin (0.00s)
=== RUN   TestPlanesBoundTheRectangle
--- PASS: TestPlanesBoundTheRectangle (0.00s)
=== RUN   TestValidateRejectsBadSpecs
--- PASS: TestValidateRejectsBadSpecs (0.00s)
=== RUN   TestBasisIsRightHanded
--- PASS: TestBasisIsRightHanded (0.00s)
=== RUN   TestValidateRejectsUParallelToNormal
--- PASS: TestValidateRejectsUParallelToNormal (0.00s)
=== RUN   TestBasisReorthogonalisesASkewedU
--- PASS: TestBasisReorthogonalisesASkewedU (0.00s)
=== RUN   TestPlanesElementZeroIsTheCutPlane
--- PASS: TestPlanesElementZeroIsTheCutPlane (0.00s)
PASS
ok  	stl-cutter/internal/cut	0.002s
=== RUN   TestCubeVolumeAndTriangleCount
--- PASS: TestCubeVolumeAndTriangleCount (0.00s)
=== RUN   TestBoxRespectsBounds
--- PASS: TestBoxRespectsBounds (0.00s)
=== RUN   TestUVSphereVolumeIsCloseToIdeal
--- PASS: TestUVSphereVolumeIsCloseToIdeal (0.00s)
=== RUN   TestTubeVolumeMatchesAnnulus
--- PASS: TestTubeVolumeMatchesAnnulus (0.00s)
=== RUN   TestHollowBoxVolumeIsShellOnly
--- PASS: TestHollowBoxVolumeIsShellOnly (0.00s)
=== RUN   TestUShapeVolumeIsProfileTimesDepth
--- PASS: TestUShapeVolumeIsProfileTimesDepth (0.00s)
=== RUN   TestUShapeGeometryIsAsDocumented
--- PASS: TestUShapeGeometryIsAsDocumented (0.00s)
=== RUN   TestNonManifoldHasAHole
--- PASS: TestNonManifoldHasAHole (0.00s)
=== RUN   TestFixtureVolumesAreTranslationInvariant
--- PASS: TestFixtureVolumesAreTranslationInvariant (0.00s)
PASS
ok  	stl-cutter/internal/fixtures	(cached)
=== RUN   TestCrossIsRightHanded
--- PASS: TestCrossIsRightHanded (0.00s)
=== RUN   TestDotAndLen
--- PASS: TestDotAndLen (0.00s)
=== RUN   TestUnitOfZeroVectorDoesNotProduceNaN
--- PASS: TestUnitOfZeroVectorDoesNotProduceNaN (0.00s)
=== RUN   TestLessIsLexicographicAndAntisymmetric
--- PASS: TestLessIsLexicographicAndAntisymmetric (0.00s)
=== RUN   TestWelderReusesIndexForNearCoincidentPoints
--- PASS: TestWelderReusesIndexForNearCoincidentPoints (0.00s)
=== RUN   TestWelderSeparatesDistinctPoints
--- PASS: TestWelderSeparatesDistinctPoints (0.00s)
=== RUN   TestWelderWeldsAcrossACellBoundary
--- PASS: TestWelderWeldsAcrossACellBoundary (0.00s)
=== RUN   TestWelderPointsReturnsInsertionOrder
--- PASS: TestWelderPointsReturnsInsertionOrder (0.00s)
PASS
ok  	stl-cutter/internal/geom	(cached)
=== RUN   TestClosedSolidsAreWatertight
--- PASS: TestClosedSolidsAreWatertight (0.00s)
=== RUN   TestNonManifoldIsDetected
--- PASS: TestNonManifoldIsDetected (0.00s)
=== RUN   TestMisorientedTriangleIsDetected
--- PASS: TestMisorientedTriangleIsDetected (0.00s)
=== RUN   TestDegenerateTriangleCannotPassAsClosed
--- PASS: TestDegenerateTriangleCannotPassAsClosed (0.00s)
=== RUN   TestDegenerateTriangleIsReportedAlongsideAClosedSolid
--- PASS: TestDegenerateTriangleIsReportedAlongsideAClosedSolid (0.00s)
PASS
ok  	stl-cutter/internal/meshcheck	(cached)
=== RUN   TestReadASCIITetrahedron
--- PASS: TestReadASCIITetrahedron (0.00s)
=== RUN   TestReadASCIIAcceptsScientificNotationAndTabs
--- PASS: TestReadASCIIAcceptsScientificNotationAndTabs (0.00s)
=== RUN   TestReadASCIIRejectsVertexCountNotMultipleOfThree
--- PASS: TestReadASCIIRejectsVertexCountNotMultipleOfThree (0.00s)
=== RUN   TestReadASCIIRejectsEmptySolid
--- PASS: TestReadASCIIRejectsEmptySolid (0.00s)
=== RUN   TestReadASCIIRejectsMalformedVertex
--- PASS: TestReadASCIIRejectsMalformedVertex (0.00s)
=== RUN   TestReadRejectsCompleteGarbage
--- PASS: TestReadRejectsCompleteGarbage (0.00s)
=== RUN   TestBinaryRoundTripPreservesTriangles
--- PASS: TestBinaryRoundTripPreservesTriangles (0.00s)
=== RUN   TestWrittenHeaderIsNotMistakableForAscii
--- PASS: TestWrittenHeaderIsNotMistakableForAscii (0.00s)
=== RUN   TestDetectsBinaryEvenWhenHeaderSaysSolid
--- PASS: TestDetectsBinaryEvenWhenHeaderSaysSolid (0.00s)
=== RUN   TestReadRejectsEmptyTriangleCount
--- PASS: TestReadRejectsEmptyTriangleCount (0.00s)
=== RUN   TestReadRejectsTruncatedFile
--- PASS: TestReadRejectsTruncatedFile (0.00s)
=== RUN   TestReadRejectsNonSTLBinaryWithoutInventingATriangleCount
--- PASS: TestReadRejectsNonSTLBinaryWithoutInventingATriangleCount (0.00s)
=== RUN   TestWrittenNormalsMatchWinding
--- PASS: TestWrittenNormalsMatchWinding (0.00s)
=== RUN   TestTriNormalAndArea
--- PASS: TestTriNormalAndArea (0.00s)
=== RUN   TestTriReversedFlipsNormal
--- PASS: TestTriReversedFlipsNormal (0.00s)
=== RUN   TestBBoxOfUnitCube
--- PASS: TestBBoxOfUnitCube (0.00s)
=== RUN   TestVolumeOfUnitCubeIsPositiveOne
--- PASS: TestVolumeOfUnitCubeIsPositiveOne (0.00s)
=== RUN   TestEpsilonScalesWithModelSize
--- PASS: TestEpsilonScalesWithModelSize (0.00s)
=== RUN   TestEpsilonHasAFloorForDegenerateMeshes
--- PASS: TestEpsilonHasAFloorForDegenerateMeshes (0.00s)
PASS
ok  	stl-cutter/internal/stl	(cached)
```

### Commit
`fix: reject a plane basis that would silently unbound the cutter`
Commit SHA: 3848d469abda0291db36bd11ce345bb1c74eecd1

## Fix: basis test now pins the derived value

### Finding
`TestBasisReorthogonalisesASkewedU` in `internal/cut/plane_test.go` did not test what its name claimed. It asserted only generic properties of *some* valid orthonormal basis — perpendicularity, unit length, right-handed cross product. A `Basis()` stub that ignored the `U` field entirely and picked an arbitrary perpendicular vector would satisfy every assertion in the test. So the test gave no regression protection for the Gram-Schmidt re-orthogonalisation it was named after.

### Change
In `internal/cut/plane_test.go`, inside `TestBasisReorthogonalisesASkewedU`, added an assertion immediately after the existing `u.Dot(s.Normal)` perpendicularity check that pins the exact value of `u`:

```go
	// Assert the actual value, not just that u is some valid perpendicular.
	// Projecting U = {1, 0, 0.5} onto the plane normal to {0, 0, 1} and normalising
	// gives exactly {1, 0, 0}; a Basis that ignored U could still satisfy every
	// generic invariant below.
	if want := (geom.Vec3{1, 0, 0}); u.Sub(want).Len() > 1e-12 {
		t.Errorf("u = %v, want %v — Basis is not deriving u from U", u, want)
	}
```

The input is `Normal = {0, 0, 1}` with a skewed `U = {1, 0, 0.5}`. Projecting that `U` onto the plane perpendicular to `Normal` and normalising gives exactly `{1, 0, 0}`. A `Basis()` ignoring `U` could not satisfy this specific value while still satisfying the generic invariants below.

### Verification experiment
Before committing, confirmed the new assertion would catch a `Basis()` that ignores `U` by temporarily mutating `Basis()` in `internal/cut/plane.go`:

**Mutation applied:**
```go
func (s Spec) Basis() (u, v geom.Vec3) {
	n := s.Normal.Unit()
	// MUTATION: ignore s.U and return fixed seed
	u = geom.Vec3{0, 1, 0}.Sub(n.Scale(geom.Vec3{0, 1, 0}.Dot(n))).Unit()
	return u, n.Cross(u)
}
```

**Test result with mutation (EXPECTED FAILURE):**
```
=== RUN   TestBasisReorthogonalisesASkewedU
    plane_test.go:146: u = [0 1 0], want [1 0 0] — Basis is not deriving u from U
--- FAIL: TestBasisReorthogonalisesASkewedU (0.00s)
```

Confirmed the new assertion failed with the broken implementation.

**Reverted mutation:**
```
$ git checkout -- internal/cut/plane.go
```

**Confirmation of revert:**
```
$ git status
Auf Branch main
Änderungen, die nicht zum Commit vorgemerkt sind:
  (benutzen Sie "git add" -Datei>..., um die Änderungen zum Commit vorzumerken)
	geändert:       internal/cut/plane_test.go

keine Änderungen zum Commit vorgemerkt.
```

`plane.go` was unmodified as required.

**Test result with original implementation (EXPECTED PASS):**
```
=== RUN   TestBasisReorthogonalisesASkewedU
--- PASS: TestBasisReorthogonalisesASkewedU (0.00s)
```

Confirmed the test passed with the original implementation.

### Full test suite

Command: `go test ./internal/cut/ -v`

Output (all tests passed):
```
=== RUN   TestPlaneClassifyRespectsEpsilon
--- PASS: TestPlaneClassifyRespectsEpsilon (0.00s)
=== RUN   TestPlanesAllPointInwardAtTheOrigin
--- PASS: TestPlanesAllPointInwardAtTheOrigin (0.00s)
=== RUN   TestPlanesBoundTheRectangle
--- PASS: TestPlanesBoundTheRectangle (0.00s)
=== RUN   TestValidateRejectsBadSpecs
--- PASS: TestValidateRejectsBadSpecs (0.00s)
=== RUN   TestBasisIsRightHanded
--- PASS: TestBasisIsRightHanded (0.00s)
=== RUN   TestValidateRejectsUParallelToNormal
--- PASS: TestValidateRejectsUParallelToNormal (0.00s)
=== RUN   TestBasisReorthogonalisesASkewedU
--- PASS: TestBasisReorthogonalisesASkewedU (0.00s)
=== RUN   TestPlanesElementZeroIsTheCutPlane
--- PASS: TestPlanesElementZeroIsTheCutPlane (0.00s)
PASS
ok  	stl-cutter/internal/cut	0.002s
```

All 8 tests in the package pass.

### Commit
`test: assert Basis derives u from U, not just some perpendicular`
Commit SHA: 52e1a45
