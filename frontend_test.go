package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The frontend runs as native ES modules with no bundler, so the browser resolves
// every import literally: a relative specifier must carry its extension and must
// name a file that actually exists. A bundler would paper over both, and this
// project deliberately has none — Wails' asset server has no extension fallback
// either, so a missing ".js" 404s, the module graph fails to load, and the window
// silently shows nothing while the build stays green.
//
// This is the only automated check on the frontend. Everything else about it
// needs a human looking at a window, which makes the few lines here worth having.
func TestFrontendRelativeImportsResolve(t *testing.T) {
	// Matches: import ... from "<spec>"  and  import "<spec>"
	importRe := regexp.MustCompile(`(?m)^\s*import\s+(?:[^'"]*\bfrom\s+)?['"]([^'"]+)['"]`)

	var checked int
	err := filepath.WalkDir("frontend", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Vendored three.js and the generated bindings are not ours to police.
			if d.Name() == "vendor" || d.Name() == "wailsjs" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".js" {
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range importRe.FindAllStringSubmatch(string(src), -1) {
			spec := m[1]
			if !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") {
				continue // bare specifier — resolved by the import map, checked separately
			}
			checked++
			if filepath.Ext(spec) == "" {
				t.Errorf("%s imports %q with no file extension; native ES module resolution does not guess it", path, spec)
				continue
			}
			target := filepath.Join(filepath.Dir(path), spec)
			if _, err := os.Stat(target); err != nil {
				t.Errorf("%s imports %q, which resolves to %s and does not exist", path, spec, target)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking frontend: %v", err)
	}
	if checked == 0 {
		t.Fatal("no relative imports were checked; the scanner is not matching anything")
	}
}

// The import map is what makes the bare "three" and "three/addons/..." specifiers
// resolve. If one of its targets is missing, every module importing three.js fails
// to load.
func TestImportMapTargetsExist(t *testing.T) {
	html, err := os.ReadFile(filepath.Join("frontend", "index.html"))
	if err != nil {
		t.Fatalf("reading index.html: %v", err)
	}
	if !strings.Contains(string(html), `type="importmap"`) {
		t.Fatal("index.html has no import map; the bare \"three\" specifiers cannot resolve without one")
	}

	// Every mapped target, whether a file or a directory prefix, must exist.
	targetRe := regexp.MustCompile(`"[^"]*"\s*:\s*"(\./[^"]+)"`)
	matches := targetRe.FindAllStringSubmatch(string(html), -1)
	if len(matches) == 0 {
		t.Fatal("the import map declares no targets")
	}
	for _, m := range matches {
		target := filepath.Join("frontend", strings.TrimPrefix(m[1], "./"))
		if _, err := os.Stat(strings.TrimSuffix(target, string(filepath.Separator))); err != nil {
			t.Errorf("import map target %q does not exist at %s", m[1], target)
		}
	}
}

// The vendored three.js is not ours to police, but it must at least be COMPLETE.
// Modern three.js splits build/ into three.core.js plus thin re-export shims, and
// vendoring only the shim leaves an import that resolves to nothing: every module
// importing three fails to load and the window renders blank, while the Go build
// and every Go test stay green. That is exactly what happened once.
func TestVendoredImportsResolve(t *testing.T) {
	root := filepath.Join("frontend", "vendor")
	if _, err := os.Stat(root); err != nil {
		t.Skip("no vendored assets")
	}

	importRe := regexp.MustCompile(`(?m)(?:^|\s)(?:import|export)[^'"\n]*\bfrom\s+['"]([^'"]+)['"]`)

	var checked int
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".js" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range importRe.FindAllStringSubmatch(string(src), -1) {
			spec := m[1]
			if !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") {
				continue // bare specifiers are the import map's problem
			}
			checked++
			target := filepath.Join(filepath.Dir(path), spec)
			if _, err := os.Stat(target); err != nil {
				t.Errorf("%s imports %q, which resolves to %s and is not vendored",
					path, spec, target)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if checked == 0 {
		t.Fatal("no relative imports found in the vendored tree; the scanner is not matching anything")
	}
}

// ourJSFiles walks the frontend modules we wrote, skipping the vendored three.js
// and the generated Wails bindings, neither of which is ours to police.
func ourJSFiles(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir("frontend", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "vendor" || d.Name() == "wailsjs" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".js" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[path] = string(src)
		return nil
	})
	if err != nil {
		t.Fatalf("walking frontend: %v", err)
	}
	return out
}

// Every getElementById("x") must find an id="x" in index.html. A renamed or
// dropped element makes the call return null, and the very next line that touches
// it throws — during module evaluation, so the whole page blanks. The Go build
// and every Go test stay green, because none of them parse the HTML.
func TestElementIDsUsedByJSExistInTheHTML(t *testing.T) {
	html, err := os.ReadFile(filepath.Join("frontend", "index.html"))
	if err != nil {
		t.Fatalf("reading index.html: %v", err)
	}
	idRe := regexp.MustCompile(`\bid="([^"]+)"`)
	ids := map[string]bool{}
	for _, m := range idRe.FindAllStringSubmatch(string(html), -1) {
		ids[m[1]] = true
	}

	getRe := regexp.MustCompile(`getElementById\(\s*["']([^"']+)["']\s*\)`)
	var checked int
	for path, src := range ourJSFiles(t) {
		for _, m := range getRe.FindAllStringSubmatch(src, -1) {
			checked++
			if !ids[m[1]] {
				t.Errorf("%s calls getElementById(%q), but index.html has no element with that id", path, m[1])
			}
		}
	}
	if checked == 0 {
		t.Fatal("no getElementById calls were checked; the scanner is not matching anything")
	}
}

// Every name in `import { A, B } from "./y.js"` must actually be exported by
// y.js. A missing named export is not a runtime error you can catch — the module
// graph fails to link and nothing on the page runs at all. Nothing in the Go
// build reads these files, so only a check like this one can see it.
func TestNamedImportsHaveMatchingExports(t *testing.T) {
	files := ourJSFiles(t)

	// Matches the braces of `import { a, b as c } from "./spec.js"`.
	importRe := regexp.MustCompile(`(?s)import\s*\{([^}]*)\}\s*from\s*['"](\./[^'"]+)['"]`)

	var checked int
	for path, src := range files {
		for _, m := range importRe.FindAllStringSubmatch(src, -1) {
			target := filepath.Join(filepath.Dir(path), m[2])
			targetSrc, ok := files[target]
			if !ok {
				continue // existence is TestFrontendRelativeImportsResolve's job
			}
			for _, name := range strings.Split(m[1], ",") {
				name = strings.TrimSpace(name)
				// `x as y` imports x; the local alias is irrelevant here.
				if i := strings.Index(name, " as "); i >= 0 {
					name = strings.TrimSpace(name[:i])
				}
				if name == "" {
					continue
				}
				checked++
				exportRe := regexp.MustCompile(`(?m)^\s*export\s+(?:default\s+)?(?:async\s+)?(?:function\*?|class|const|let|var)\s+` + regexp.QuoteMeta(name) + `\b`)
				listRe := regexp.MustCompile(`(?s)export\s*\{[^}]*\b` + regexp.QuoteMeta(name) + `\b[^}]*\}`)
				if !exportRe.MatchString(targetSrc) && !listRe.MatchString(targetSrc) {
					t.Errorf("%s imports %q from %s, which does not export it", path, name, m[2])
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no named imports were checked; the scanner is not matching anything")
	}
}

// Every bare specifier our own modules import must be covered by the import map,
// or the browser will refuse to resolve it.
func TestBareImportsAreCoveredByTheImportMap(t *testing.T) {
	html, err := os.ReadFile(filepath.Join("frontend", "index.html"))
	if err != nil {
		t.Fatalf("reading index.html: %v", err)
	}
	mapSrc := string(html)

	importRe := regexp.MustCompile(`(?m)^\s*import\s+(?:[^'"]*\bfrom\s+)?['"]([^'"]+)['"]`)
	err = filepath.WalkDir("frontend", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "vendor" || d.Name() == "wailsjs" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".js" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range importRe.FindAllStringSubmatch(string(src), -1) {
			spec := m[1]
			if strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") {
				continue
			}
			// Either the exact specifier or a prefix mapping must appear.
			covered := strings.Contains(mapSrc, `"`+spec+`"`)
			if !covered {
				for i := len(spec); i > 0; i-- {
					if spec[i-1] == '/' && strings.Contains(mapSrc, `"`+spec[:i]+`"`) {
						covered = true
						break
					}
				}
			}
			if !covered {
				t.Errorf("%s imports bare specifier %q, which the import map does not cover", path, spec)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking frontend: %v", err)
	}
}
