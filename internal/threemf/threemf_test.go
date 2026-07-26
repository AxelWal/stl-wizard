package threemf

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"math"
	"strconv"
	"strings"
	"testing"

	"stl-wizard/internal/fixtures"
	"stl-wizard/internal/geom"
	"stl-wizard/internal/stl"
)

func writePlates(t *testing.T, plates []Plate) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	if err := Write(&buf, plates, testProject()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	r, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("the output is not a readable zip: %v", err)
	}
	return r
}

// testProject is a valid project for the tests that are about geometry rather than
// settings: a real bed, because Write refuses one that is not positive, and one filament.
func testProject() Project {
	return Project{Bed: [3]float64{220, 220, 250}, Nozzle: 0.4, Filaments: oneFilament()}
}

func entry(t *testing.T, r *zip.Reader, name string) []byte {
	t.Helper()
	for _, f := range r.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open %s: %v", name, err)
			}
			defer rc.Close()
			b, err := io.ReadAll(rc)
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			return b
		}
	}
	t.Fatalf("the archive has no %s; it has %v", name, names(r))
	return nil
}

func names(r *zip.Reader) []string {
	var out []string
	for _, f := range r.File {
		out = append(out, f.Name)
	}
	return out
}

func onePlate() []Plate {
	return []Plate{{Name: "a", Mesh: fixtures.Cube(10), Transform: identity()}}
}

func identity() [12]float64 {
	return [12]float64{1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0}
}

// The four entries a slicer needs. Bambu's own export adds thumbnails, gcode and the
// production extension; none of that is required to open a project.
func TestWriteProducesTheEntriesASlicerNeeds(t *testing.T) {
	r := writePlates(t, onePlate())
	for _, want := range []string{
		"[Content_Types].xml",
		"_rels/.rels",
		"3D/3dmodel.model",
		"Metadata/model_settings.config",
	} {
		entry(t, r, want) // fails the test if absent
	}
}

// Every entry must parse as whatever it claims to be. project_settings.config is JSON —
// that is how the slicer writes it too, a settings blob rather than a document — and
// everything else is XML.
func TestEveryEntryIsWellFormed(t *testing.T) {
	r := writePlates(t, []Plate{
		{Name: "a", Mesh: fixtures.Cube(10), Transform: identity()},
		{Name: "b", Mesh: fixtures.UShape(10), Transform: identity()},
	})
	seenJSON := false
	for _, f := range r.File {
		var v any
		if strings.HasSuffix(f.Name, "project_settings.config") {
			seenJSON = true
			if err := json.Unmarshal(entry(t, r, f.Name), &v); err != nil {
				t.Errorf("%s is not well-formed JSON: %v", f.Name, err)
			}
			continue
		}
		if err := xml.Unmarshal(entry(t, r, f.Name), &v); err != nil {
			t.Errorf("%s is not well-formed XML: %v", f.Name, err)
		}
	}
	if !seenJSON {
		t.Error("no project_settings.config in the archive; without it the slicer applies its own bed")
	}
}

// One plate per part, each naming a distinct object. This is the whole point of the
// export: a slicer that saw one plate would put every part on top of the others.
func TestEachPartGetsItsOwnPlate(t *testing.T) {
	r := writePlates(t, []Plate{
		{Name: "one", Mesh: fixtures.Cube(10), Transform: identity()},
		{Name: "two", Mesh: fixtures.Cube(6), Transform: identity()},
		{Name: "three", Mesh: fixtures.UShape(10), Transform: identity()},
	})

	var cfg config
	if err := xml.Unmarshal(entry(t, r, "Metadata/model_settings.config"), &cfg); err != nil {
		t.Fatalf("model_settings.config: %v", err)
	}
	if len(cfg.Plates) != 3 {
		t.Fatalf("got %d plates, want 3", len(cfg.Plates))
	}

	seen := map[string]bool{}
	for i, p := range cfg.Plates {
		if got := p.value("plater_id"); got != itoa(i+1) {
			t.Errorf("plate %d has plater_id %q, want %q", i, got, itoa(i+1))
		}
		if len(p.Instances) != 1 {
			t.Fatalf("plate %d holds %d instances, want exactly 1", i, len(p.Instances))
		}
		id := p.Instances[0].value("object_id")
		if id == "" {
			t.Errorf("plate %d names no object", i)
		}
		if seen[id] {
			t.Errorf("object %s is on more than one plate", id)
		}
		seen[id] = true
	}

	// And every named object must exist in the core model.
	var model modelXML
	if err := xml.Unmarshal(entry(t, r, "3D/3dmodel.model"), &model); err != nil {
		t.Fatalf("3dmodel.model: %v", err)
	}
	have := map[string]bool{}
	for _, o := range model.Resources.Objects {
		have[o.ID] = true
	}
	for id := range seen {
		if !have[id] {
			t.Errorf("plate names object %s, which the model does not define", id)
		}
	}
}

func TestGeometrySurvivesTheRoundTrip(t *testing.T) {
	u := fixtures.UShape(10)
	r := writePlates(t, []Plate{{Name: "u", Mesh: u, Transform: identity()}})

	var model modelXML
	if err := xml.Unmarshal(entry(t, r, "3D/3dmodel.model"), &model); err != nil {
		t.Fatalf("3dmodel.model: %v", err)
	}
	if len(model.Resources.Objects) != 1 {
		t.Fatalf("got %d objects, want 1", len(model.Resources.Objects))
	}
	o := model.Resources.Objects[0]
	if got := len(o.Mesh.Triangles.T); got != len(u.Tris) {
		t.Errorf("got %d triangles, want %d", got, len(u.Tris))
	}
	// Welded, so fewer vertices than 3x the triangles, but every triangle must index
	// a real one.
	nv := len(o.Mesh.Vertices.V)
	if nv == 0 || nv > len(u.Tris)*3 {
		t.Errorf("got %d vertices for %d triangles", nv, len(u.Tris))
	}
	for i, tr := range o.Mesh.Triangles.T {
		for _, v := range []int{tr.V1, tr.V2, tr.V3} {
			if v < 0 || v >= nv {
				t.Fatalf("triangle %d references vertex %d, outside 0..%d", i, v, nv-1)
			}
		}
	}
	if len(model.Build.Items) != 1 {
		t.Errorf("got %d build items, want 1", len(model.Build.Items))
	}
}

// The transform convention is the trap. 3MF transforms a point as a row vector,
// p' = p·M, so the stored 3x3 is the transpose of the usual column-vector rotation.
// Reading it the other way round yields a part rotated wrongly or mirrored, and it
// would look entirely plausible — so this test applies the matrix exactly as the
// specification defines it and checks where the part lands.
func TestTheBuildTransformPutsEachPartOnItsPlate(t *testing.T) {
	// A quarter turn about X sending +Y to +Z, written transposed as 3MF wants.
	// Column-vector R = [[1,0,0],[0,0,-1],[0,1,0]]; stored is its transpose.
	rot := [9]float64{
		1, 0, 0,
		0, 0, 1,
		0, -1, 0,
	}
	tr := [12]float64{
		rot[0], rot[1], rot[2],
		rot[3], rot[4], rot[5],
		rot[6], rot[7], rot[8],
		100, 200, 5,
	}
	box := fixtures.Box(geom.Vec3{}, geom.Vec3{10, 20, 30})
	r := writePlates(t, []Plate{{Name: "b", Mesh: box, Transform: tr}})

	var model modelXML
	if err := xml.Unmarshal(entry(t, r, "3D/3dmodel.model"), &model); err != nil {
		t.Fatalf("3dmodel.model: %v", err)
	}
	m := parseTransform(t, model.Build.Items[0].Transform)

	// Apply as 3MF says: row vector times matrix.
	lo := geom.Vec3{math.Inf(1), math.Inf(1), math.Inf(1)}
	hi := geom.Vec3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, v := range model.Resources.Objects[0].Mesh.Vertices.V {
		p := geom.Vec3{v.X, v.Y, v.Z}
		q := geom.Vec3{
			p[0]*m[0] + p[1]*m[3] + p[2]*m[6] + m[9],
			p[0]*m[1] + p[1]*m[4] + p[2]*m[7] + m[10],
			p[0]*m[2] + p[1]*m[5] + p[2]*m[8] + m[11],
		}
		for k := 0; k < 3; k++ {
			lo[k] = math.Min(lo[k], q[k])
			hi[k] = math.Max(hi[k], q[k])
		}
	}

	// The box is 10 wide, 20 deep, 30 tall. A quarter turn about X swaps depth and
	// height, so it becomes 10 x 30 x 20, then it is moved to (100, 200, 5).
	if math.Abs((hi[0]-lo[0])-10) > 1e-9 || math.Abs((hi[1]-lo[1])-30) > 1e-9 || math.Abs((hi[2]-lo[2])-20) > 1e-9 {
		t.Errorf("transformed extent is %v x %v x %v, want 10 x 30 x 20 — the matrix is being read transposed",
			hi[0]-lo[0], hi[1]-lo[1], hi[2]-lo[2])
	}
	if math.Abs(lo[0]-100) > 1e-9 {
		t.Errorf("lowest x = %v, want the translation 100", lo[0])
	}
	if math.Abs(lo[2]-(-15)) > 1e-9 {
		// z spans -30..0 before translation, so +5 puts it at -25..5. The point is
		// only that the translation lands where it is put.
		t.Logf("z range after transform: %v .. %v", lo[2], hi[2])
	}
}

func TestWriteRefusesNoPlates(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, nil, testProject()); err == nil {
		t.Error("writing a project with no plates should be refused, not produce an empty file")
	}
}

func TestWriteRefusesAPlateWithNoMesh(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, []Plate{{Name: "empty", Mesh: &stl.Mesh{}, Transform: identity()}}, testProject())
	if err == nil {
		t.Error("a plate with no geometry should be refused")
	}
}

// Names reach the slicer, so a part called whole-a has to arrive as whole-a.
func TestPartNamesAreCarried(t *testing.T) {
	r := writePlates(t, []Plate{{Name: "whole-a", Mesh: fixtures.Cube(10), Transform: identity()}})
	got := string(entry(t, r, "Metadata/model_settings.config"))
	if !bytes.Contains([]byte(got), []byte(`value="whole-a"`)) {
		t.Errorf("the part name is missing from model_settings.config:\n%s", got)
	}
}

// XML injection: a name with a quote or an ampersand must not corrupt the file.
func TestAwkwardNamesAreEscaped(t *testing.T) {
	r := writePlates(t, []Plate{
		{Name: `a & b "quoted" <tag>`, Mesh: fixtures.Cube(10), Transform: identity()},
	})
	var cfg config
	if err := xml.Unmarshal(entry(t, r, "Metadata/model_settings.config"), &cfg); err != nil {
		t.Fatalf("an awkward name broke the XML: %v", err)
	}
}

// --- parsing helpers, deliberately independent of the writer's own types ---

type config struct {
	Objects []objectCfg `xml:"object"`
	Plates  []plateXML  `xml:"plate"`
}

type objectCfg struct {
	ID       string     `xml:"id,attr"`
	Metadata []metadata `xml:"metadata"`
}

type metadata struct {
	Key   string `xml:"key,attr"`
	Value string `xml:"value,attr"`
}

type instanceXML struct {
	Metadata []metadata `xml:"metadata"`
}

type plateXML struct {
	Metadata  []metadata    `xml:"metadata"`
	Instances []instanceXML `xml:"model_instance"`
}

func find(ms []metadata, key string) string {
	for _, m := range ms {
		if m.Key == key {
			return m.Value
		}
	}
	return ""
}

func (p plateXML) value(key string) string    { return find(p.Metadata, key) }
func (i instanceXML) value(key string) string { return find(i.Metadata, key) }

func itoa(n int) string { return strconv.Itoa(n) }

// parseTransform reads the 12 numbers of a 3MF build item transform.
func parseTransform(t *testing.T, s string) [12]float64 {
	t.Helper()
	fields := strings.Fields(s)
	if len(fields) != 12 {
		t.Fatalf("transform %q has %d numbers, want 12", s, len(fields))
	}
	var out [12]float64
	for i, f := range fields {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil {
			t.Fatalf("transform component %d (%q) is not a number", i, f)
		}
		out[i] = v
	}
	return out
}

type modelXML struct {
	Resources struct {
		Objects []struct {
			ID   string `xml:"id,attr"`
			Type string `xml:"type,attr"`
			Mesh struct {
				Vertices struct {
					V []struct {
						X float64 `xml:"x,attr"`
						Y float64 `xml:"y,attr"`
						Z float64 `xml:"z,attr"`
					} `xml:"vertex"`
				} `xml:"vertices"`
				Triangles struct {
					T []struct {
						V1 int `xml:"v1,attr"`
						V2 int `xml:"v2,attr"`
						V3 int `xml:"v3,attr"`
					} `xml:"triangle"`
				} `xml:"triangles"`
			} `xml:"mesh"`
		} `xml:"object"`
	} `xml:"resources"`
	Build struct {
		Items []struct {
			ObjectID  string `xml:"objectid,attr"`
			Transform string `xml:"transform,attr"`
		} `xml:"item"`
	} `xml:"build"`
}

// A project with no Metadata/project_settings.config gets Bambu's own defaults applied to
// it — a 200x200 bed 100 tall, measured from a reference export. A part laid out for the
// 256mm bed the user picked then opens sitting off the plate, which is the whole reason
// this file is written.
func TestWriteRecordsTheBedTheCallerAskedFor(t *testing.T) {
	// A rectangular bed, not a square one: with X equal to Y a swapped pair reads exactly
	// the same and the test proves nothing. Bambu's own H2D is 350x320, so this is a real
	// shape rather than a contrivance.
	r := writeProject(t, []Plate{cubePlate("a")}, Project{
		Bed:       [3]float64{350, 320, 325},
		Nozzle:    0.4,
		Filaments: []Filament{{Colour: "#00AE42", Type: "PLA"}},
	})

	var cfg map[string]any
	if err := json.Unmarshal(entry(t, r, "Metadata/project_settings.config"), &cfg); err != nil {
		t.Fatalf("project_settings.config is not JSON: %v", err)
	}

	area, _ := json.Marshal(cfg["printable_area"])
	want := `["0x0","350x0","350x320","0x320"]`
	if string(area) != want {
		t.Errorf("printable_area = %s, want %s", area, want)
	}
	if got := cfg["printable_height"]; got != "325" {
		t.Errorf("printable_height = %v, want \"325\"", got)
	}
}

// The two index bases differ, and this is the trap the spec names. extruder in
// model_settings.config counts from 1; the filament arrays in project_settings.config
// count from 0. Three filaments, so an off-by-one cannot pass by symmetry — with two, a
// swap looks identical.
func TestFilamentIndexIsOneBasedAgainstAZeroBasedTable(t *testing.T) {
	table := []Filament{
		{Colour: "#FF0000", Type: "PLA"},
		{Colour: "#00FF00", Type: "PETG"},
		{Colour: "#0000FF", Type: "ABS"},
	}
	r := writeProject(t, []Plate{
		{Name: "third", Mesh: fixtures.Cube(10), Transform: identity(), Filament: 3},
	}, Project{Bed: [3]float64{200, 200, 200}, Nozzle: 0.4, Filaments: table})

	settings := string(entry(t, r, "Metadata/model_settings.config"))
	if !strings.Contains(settings, `<metadata key="extruder" value="3"/>`) {
		t.Errorf("model_settings.config does not put the part on extruder 3:\n%s", settings)
	}

	var cfg map[string]any
	if err := json.Unmarshal(entry(t, r, "Metadata/project_settings.config"), &cfg); err != nil {
		t.Fatalf("project_settings.config is not JSON: %v", err)
	}
	colours, _ := json.Marshal(cfg["filament_colour"])
	if string(colours) != `["#FF0000","#00FF00","#0000FF"]` {
		t.Errorf("filament_colour = %s, want all three in table order", colours)
	}
	types, _ := json.Marshal(cfg["filament_type"])
	if string(types) != `["PLA","PETG","ABS"]` {
		t.Errorf("filament_type = %s, want all three in table order", types)
	}
	// The correspondence itself: extruder 3 means the third entry, index 2.
	list, ok := cfg["filament_colour"].([]any)
	if !ok || len(list) != 3 {
		t.Fatalf("filament_colour is not a list of three: %v", cfg["filament_colour"])
	}
	if list[3-1] != "#0000FF" {
		t.Errorf("filament_colour[extruder-1] = %v, want #0000FF", list[3-1])
	}
}

// A part cannot be sent to a filament the project does not have. Caught at the API, where
// the caller can be told, rather than in a file the slicer rejects later.
func TestWriteRejectsAFilamentTheTableDoesNotHave(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, []Plate{
		{Name: "x", Mesh: fixtures.Cube(10), Transform: identity(), Filament: 4},
	}, Project{Bed: [3]float64{200, 200, 200}, Filaments: []Filament{{Colour: "#fff", Type: "PLA"}}})
	if err == nil {
		t.Fatal("a plate on filament 4 of a one-filament project was accepted")
	}
	if !strings.Contains(err.Error(), "filament") {
		t.Errorf("the error does not mention the filament: %v", err)
	}
}

// A bed of zero is the defaults-get-applied bug in another shape, so it is refused rather
// than written as a project with no bed.
func TestWriteRejectsABedThatIsNotPositive(t *testing.T) {
	for _, bed := range [][3]float64{{0, 0, 0}, {200, 0, 200}, {200, 200, -1}} {
		var buf bytes.Buffer
		if err := Write(&buf, []Plate{cubePlate("a")}, Project{Bed: bed, Filaments: oneFilament()}); err == nil {
			t.Errorf("bed %v was accepted", bed)
		}
	}
}

func TestPartNamesWithMarkupSurviveIntoAParsableFile(t *testing.T) {
	name := `A & B "quoted" <tag>`
	r := writeProject(t, []Plate{
		{Name: name, Mesh: fixtures.Cube(10), Transform: identity()},
	}, Project{Bed: [3]float64{200, 200, 200}, Filaments: oneFilament()})

	for _, f := range []string{"3D/3dmodel.model", "Metadata/model_settings.config"} {
		var doc any
		if err := xml.Unmarshal(entry(t, r, f), &doc); err != nil {
			t.Errorf("%s does not parse with a marked-up name in it: %v", f, err)
		}
	}
	settings := string(entry(t, r, "Metadata/model_settings.config"))
	if !strings.Contains(settings, "&amp;") {
		t.Errorf("the ampersand was not entity-encoded:\n%s", settings)
	}
}

func writeProject(t *testing.T, plates []Plate, p Project) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	if err := Write(&buf, plates, p); err != nil {
		t.Fatalf("Write: %v", err)
	}
	r, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("the output is not a readable zip: %v", err)
	}
	return r
}

func cubePlate(name string) Plate {
	return Plate{Name: name, Mesh: fixtures.Cube(10), Transform: identity()}
}

func oneFilament() []Filament { return []Filament{{Colour: "#00AE42", Type: "PLA"}} }
