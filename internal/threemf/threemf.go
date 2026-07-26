// Package threemf writes a 3MF project with one build plate per part.
//
// Multiple build plates are not in the core 3MF specification — they are a Bambu
// Studio and OrcaSlicer extension, and PrusaSlicer and Cura have no plate concept at
// all. The schema written here was read off a reference file Bambu Studio was asked
// to produce, rather than inferred: six cubes exported as six plates, and
// Metadata/model_settings.config copied from the result.
//
// Bambu's own export uses the production extension — external 3D/Objects/object_N.model
// files, p:UUID on every element, requiredextensions="p". None of that is needed. A
// single 3D/3dmodel.model with an inline mesh per object is plain core 3MF, and it is
// far less to write and to get wrong.
package threemf

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"stl-cutter/internal/geom"
	"stl-cutter/internal/stl"
)

// Plate is one part on one build plate.
type Plate struct {
	Name string
	Mesh *stl.Mesh
	// Transform is what the build item carries: a 4x3 matrix, the rotation's nine
	// components followed by the translation.
	//
	// 3MF transforms a point as a *row vector*, p' = p·M, so the nine components are
	// the transpose of the usual column-vector rotation. Writing a column-vector
	// matrix here yields a part rotated the wrong way or mirrored, and it looks
	// entirely plausible in the file.
	Transform [12]float64
}

// Write emits the whole project to w.
func Write(w io.Writer, plates []Plate) error {
	if len(plates) == 0 {
		return errors.New("a 3MF project needs at least one plate")
	}
	for i, p := range plates {
		if p.Mesh == nil || len(p.Mesh.Tris) == 0 {
			return fmt.Errorf("plate %d (%q) has no geometry", i+1, p.Name)
		}
	}

	z := zip.NewWriter(w)
	// Object ids start at 1 and step by 1. Bambu numbers them 2,4,6… with odd part
	// ids in between; nothing reads a meaning into the numbers, only that the plate
	// and the model agree on them.
	ids := make([]int, len(plates))
	for i := range plates {
		ids[i] = i + 1
	}

	for _, f := range []struct {
		name string
		body func() string
	}{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", rels},
		{"3D/3dmodel.model", func() string { return model(plates, ids) }},
		{"Metadata/model_settings.config", func() string { return settings(plates, ids) }},
	} {
		e, err := z.Create(f.name)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(e, f.body()); err != nil {
			return err
		}
	}
	return z.Close()
}

func contentTypes() string {
	return xmlHeader + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
 <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
 <Default Extension="model" ContentType="application/vnd.ms-package.3dmanufacturing-3dmodel+xml"/>
</Types>
`
}

func rels() string {
	return xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
 <Relationship Target="/3D/3dmodel.model" Id="rel-1" Type="http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"/>
</Relationships>
`
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8"?>` + "\n"

// model writes the core document: one object per plate with its mesh inline, and one
// build item per object carrying that part's placement.
func model(plates []Plate, ids []int) string {
	var b strings.Builder
	b.WriteString(xmlHeader)
	b.WriteString(`<model unit="millimeter" xml:lang="en-US" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02">` + "\n")
	b.WriteString(" <metadata name=\"Application\">stl-cutter</metadata>\n")
	b.WriteString(" <resources>\n")
	for i, p := range plates {
		fmt.Fprintf(&b, "  <object id=\"%d\" type=\"model\" name=\"%s\">\n", ids[i], escape(p.Name))
		writeMesh(&b, p.Mesh)
		b.WriteString("  </object>\n")
	}
	b.WriteString(" </resources>\n <build>\n")
	for i, p := range plates {
		fmt.Fprintf(&b, "  <item objectid=\"%d\" transform=\"%s\" printable=\"1\"/>\n",
			ids[i], transformString(p.Transform))
	}
	b.WriteString(" </build>\n</model>\n")
	return b.String()
}

// writeMesh emits welded vertices and the triangles indexing them.
//
// STL stores every triangle's corners separately, so writing them straight through
// would triple the vertex count and leave a slicer to weld them itself — and some
// then report the mesh as non-manifold because nothing is shared. Welding here at the
// mesh's own epsilon is the same tolerance the rest of this project uses.
func writeMesh(b *strings.Builder, m *stl.Mesh) {
	w := geom.NewWelder(m.Epsilon())
	idx := make([][3]int, 0, len(m.Tris))
	for _, t := range m.Tris {
		idx = append(idx, [3]int{w.ID(t.A), w.ID(t.B), w.ID(t.C)})
	}

	b.WriteString("   <mesh>\n    <vertices>\n")
	for _, v := range w.Points() {
		fmt.Fprintf(b, "     <vertex x=\"%s\" y=\"%s\" z=\"%s\"/>\n", num(v[0]), num(v[1]), num(v[2]))
	}
	b.WriteString("    </vertices>\n    <triangles>\n")
	for _, t := range idx {
		// A triangle whose corners welded together has no area and no valid winding.
		// 3MF readers reject such a triangle outright, so it is dropped rather than
		// written — it described nothing.
		if t[0] == t[1] || t[1] == t[2] || t[2] == t[0] {
			continue
		}
		fmt.Fprintf(b, "     <triangle v1=\"%d\" v2=\"%d\" v3=\"%d\"/>\n", t[0], t[1], t[2])
	}
	b.WriteString("    </triangles>\n   </mesh>\n")
}

// settings writes the Bambu/Orca extension: one plate per part, each naming the one
// object that belongs on it.
func settings(plates []Plate, ids []int) string {
	var b strings.Builder
	b.WriteString(xmlHeader)
	b.WriteString("<config>\n")
	for i, p := range plates {
		fmt.Fprintf(&b, "  <object id=\"%d\">\n", ids[i])
		fmt.Fprintf(&b, "    <metadata key=\"name\" value=\"%s\"/>\n", escape(p.Name))
		b.WriteString("    <metadata key=\"extruder\" value=\"1\"/>\n")
		fmt.Fprintf(&b, "    <part id=\"%d\" subtype=\"normal_part\">\n", ids[i])
		fmt.Fprintf(&b, "      <metadata key=\"name\" value=\"%s\"/>\n", escape(p.Name))
		b.WriteString("    </part>\n  </object>\n")
	}
	for i, p := range plates {
		b.WriteString("  <plate>\n")
		fmt.Fprintf(&b, "    <metadata key=\"plater_id\" value=\"%d\"/>\n", i+1)
		fmt.Fprintf(&b, "    <metadata key=\"plater_name\" value=\"%s\"/>\n", escape(p.Name))
		b.WriteString("    <metadata key=\"locked\" value=\"false\"/>\n")
		// Empty means this plate has not been sliced, which is exactly the case.
		b.WriteString("    <metadata key=\"gcode_file\" value=\"\"/>\n")
		b.WriteString("    <model_instance>\n")
		fmt.Fprintf(&b, "      <metadata key=\"object_id\" value=\"%d\"/>\n", ids[i])
		b.WriteString("      <metadata key=\"instance_id\" value=\"0\"/>\n")
		b.WriteString("    </model_instance>\n  </plate>\n")
	}
	b.WriteString("  <assemble>\n  </assemble>\n</config>\n")
	return b.String()
}

func transformString(t [12]float64) string {
	parts := make([]string, 12)
	for i, v := range t {
		parts[i] = num(v)
	}
	return strings.Join(parts, " ")
}

// num formats without an exponent and without trailing zeroes. 'g' would emit
// "1e-07", which some 3MF readers reject.
func num(v float64) string {
	s := strconv.FormatFloat(v, 'f', 6, 64)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

// escape makes a value safe inside an XML attribute. A part name reaches the slicer,
// and a name holding a quote or an ampersand would otherwise produce a file that
// cannot be parsed at all.
func escape(s string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return "" // strings.Builder never fails
	}
	// EscapeText leaves double quotes as &#34;, which is valid; it also escapes
	// newlines and tabs. Nothing further is needed.
	return b.String()
}
