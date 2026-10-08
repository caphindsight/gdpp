package syntax_1

import (
	"cmp"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"gd++/trans/meta"
)

// TestGenerate generates code for every case in testdata/gen/<case>/input.gd++ and compares
// it with the golden files next to it: the generated <Name>.h and <Name>.cpp, decls.txt and <Class>.xml, or input.err.
// An options.toml next to input.gd++ sets the groups of @trace and @profile that are on, as `trace = ["all"]`.
func TestGenerate(t *testing.T) {
	var file struct {
		Dep []struct {
			Name, Include, Kind, Base  string
			Values                     []meta.EnumValue
			Gdpp, Bitfield, NonRuntime bool
			Virtuals, Notifications    []string
			Traits                     []string
			NoscriptVirtuals           []string `toml:"noscript_virtuals"`
			Signals                    []meta.Signal
			File                       string // For macros and templates: their file in testdata/gen.
			SourceName                 string `toml:"source_name"`
		}
	}
	if _, err := toml.DecodeFile("testdata/gen/deps.toml", &file); err != nil {
		t.Fatal(err)
	}
	kinds := map[string]meta.Kind{"Object": meta.Object, "RefCounted": meta.RefCounted, "Extern": meta.Extern,
		"RefCountedExtern": meta.RefCountedExtern, "Enum": meta.Enum, "Other": meta.Other, "GodotEnum": meta.GodotEnum, "Macro": meta.Macro,
		"Template": meta.Template, "Annotation": meta.Annotation, "Trait": meta.Trait, "RefCountedTrait": meta.RefCountedTrait,
		"ShaderLibrary": meta.ShaderLibrary}
	var opts meta.Options
	for _, d := range file.Dep {
		source := ""
		if d.File != "" {
			data, err := os.ReadFile(filepath.Join("testdata/gen", d.File))
			if err != nil {
				t.Fatal(err)
			}
			source = string(data)
		}
		opts.Dependencies = append(opts.Dependencies, meta.Dependency{Name: d.Name, Include: d.Include, Kind: kinds[d.Kind], Values: d.Values, Base: d.Base, Gdpp: d.Gdpp, Bitfield: d.Bitfield, Virtuals: d.Virtuals, NoscriptVirtuals: d.NoscriptVirtuals,
			Notifications: d.Notifications, NonRuntime: d.NonRuntime, Source: source, File: d.File, SourceName: d.SourceName, Traits: d.Traits, Signals: d.Signals})
	}
	opts.PackageID, opts.PackagePrefix, opts.CppStandard = "shooter", "Shooter", "c++17"
	dirs, err := filepath.Glob("testdata/gen/*/input.gd++")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{}
	for _, input := range dirs {
		cases[filepath.Dir(input)] = input
	}
	for dir, input := range cases {
		t.Run(strings.TrimPrefix(dir, "testdata/gen/"), func(t *testing.T) {
			data, err := os.ReadFile(input)
			if err != nil {
				t.Fatal(err)
			}
			src := string(data)
			caseOpts := opts
			if _, err := toml.DecodeFile(filepath.Join(dir, "options.toml"), &caseOpts); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			got := generate(t, src, caseOpts)
			if *update {
				old, _ := filepath.Glob(filepath.Join(dir, "*"))
				for _, path := range old {
					if name := filepath.Base(path); name != "input.gd++" && name != "options.toml" {
						os.Remove(path)
					}
				}
				os.MkdirAll(dir, 0o755)
				for name, text := range got {
					if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				return
			}
			files, _ := filepath.Glob(filepath.Join(dir, "*"))
			for _, path := range files {
				if name := filepath.Base(path); name != "input.gd++" && name != "options.toml" && got[name] == "" {
					t.Errorf("Golden %s wasn't generated.", name)
				}
			}
			for name, text := range got {
				want, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					t.Errorf("Missing golden %s:\n%s", name, text)
				} else if string(want) != text {
					t.Errorf("%s mismatch.\n--- got:\n%s\n--- want:\n%s", name, text, want)
				}
			}
		})
	}
}

// generate returns the generated files for src by name, checking the invariants of each.
func generate(t *testing.T, src string, opts meta.Options) map[string]string {
	const name = "input.gd++"
	out := map[string]string{}
	decls, err := ListClasses(name, src, opts)
	if err == nil {
		var lines []string
		for _, d := range decls {
			lines = append(lines, fmt.Sprintf("%+v", d))
		}
		out["decls.txt"] = strings.Join(lines, "\n") + "\n"
	}
	// The GD++ files that #line may name, and that errors may point into: this one, and those of the macros and
	// templates it uses.
	sources, errSources := map[string]string{name: src}, map[string]string{name: src}
	for _, d := range opts.Dependencies {
		if d.Source != "" {
			sources[cmp.Or(d.SourceName, d.File)] = d.Source
			errSources[d.File] = d.Source
		}
	}
	files, err := Generate(name, src, opts)
	if err != nil {
		checkError(t, errSources, err)
		return map[string]string{"input.err": err.Error() + "\n"}
	}
	for _, f := range files {
		out[f.Name] = f.Text
		checkLines(t, sources, f.Name, f.Text)
	}
	// The source that later stages parse, if expanding the invocations changes it.
	if expanded, err := Expand(name, src, opts); err != nil {
		t.Fatal(err)
	} else if expanded != src {
		out["expanded.gd++"] = expanded
	}
	u, err := newUnit(name, src, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range u.classes {
		doc, err := DocumentClass(name, src, c.name, opts)
		if err != nil {
			t.Fatal(err)
		}
		checkXML(t, doc)
		out[c.name+".xml"] = doc
	}
	return out
}

var (
	lineDirective = regexp.MustCompile(`^#line (\d+) "(.*)"$`)
	invocation    = regexp.MustCompile(`\binvoke(\s+\w+)?\s*[({]`)
)

// checkLines asserts that #line directives name the right lines: each user code fragment matches the lines of the
// GD++ file in sources it claims to come from, and each directive back to the generated file names its own next line.
func checkLines(t *testing.T, sources map[string]string, self, text string) {
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		m := lineDirective.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		if m[2] == self {
			if n != i+2 {
				t.Errorf("%s:%d: %s should name line %d.", self, i+1, lines[i], i+2)
			}
			continue
		}
		src, ok := sources[m[2]]
		if !ok {
			t.Errorf("%s:%d: %s names an unknown file.", self, i+1, lines[i])
			continue
		}
		srcLines := strings.Split(src, "\n")
		// Lines after the first come straight from the source (minus comments), so their identifiers match, except
		// for the rewrites of emit, rpc, is_cancelled, string_name, claim, is_done, cancel, as, assert, await, return and callable, whose operand may start
		// on the next line,
		// and in shaders, of swizzles, not and shared, and the swizzle methods of structs.
		for k := 1; i+1+k < len(lines) && !lineDirective.MatchString(lines[i+1+k]); k++ {
			if n+k > len(srcLines) {
				t.Fatalf("%s:%d: %s claims more lines than the source has.", self, i+1, lines[i])
			}
			if invocation.MatchString(srcLines[n+k-1]) || strings.Contains(srcLines[n+k-1], "${") || strings.HasSuffix(lines[i+k], "\\") {
				continue // The line holds what a macro generated, or a template's holes, or the line before continues on it.
			}
			want := identifiers(srcLines[n+k-1])
			for _, id := range identifiers(lines[i+1+k]) {
				if !slices.Contains([]string{"void", "gdpp", "GDPP_STRING_NAME", "GDPP_ASSERT", "GDPP_ASSERT_VOID", "GDPP_ASSERT_VALUE", "GDPP_ASSERT_CO_VOID", "GDPP_ASSERT_CO_VALUE", "claim", "is_done", "cancel", "cast",
					"co_await", "co_return", "signal", "StringName", "string_name", "this",
					"callable", "callable_method", "callable_member", "callable_name", "This", "nullptr", "auto", "o", "return", "std", "remove_pointer_t", "decltype",
					"swizzle", "swizzle_ref", "glsl_not", "static", "template", "char", "C", "if", "else", "constexpr", "is_same_v", "integer_sequence", "const"}, id) && !swizzleRegexp.MatchString(id) && !slices.Contains(want, strings.TrimPrefix(id, "_gdpp_rpc_")) {
					t.Errorf("%s:%d: %q is not on line %d of the source: %q", self, i+2+k, id, n+k, srcLines[n+k-1])
				}
			}
		}
	}
}

// checkXML asserts that doc is well-formed, with <class> elements in class.xsd's order.
func checkXML(t *testing.T, doc string) {
	order := []string{"brief_description", "description", "tutorials", "constructors", "methods", "members", "signals", "constants"}
	dec := xml.NewDecoder(strings.NewReader(doc))
	depth, last := 0, -1
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Malformed XML: %v\n%s", err, doc)
		}
		switch e := tok.(type) {
		case xml.StartElement:
			depth++
			switch {
			case depth == 1 && e.Name.Local != "class":
				t.Errorf("The root element is <%s>, not <class>.", e.Name.Local)
			case depth == 2:
				i := slices.Index(order, e.Name.Local)
				if i <= last {
					t.Errorf("<%s> is out of class.xsd's order.", e.Name.Local)
				}
				last = i
			}
		case xml.EndElement:
			depth--
		}
	}
}

// TestDocumentBuiltinClasses checks the documentation of the classes that the runtime adds, against
// testdata/builtin, and that it's well-formed.
func TestDocumentBuiltinClasses(t *testing.T) {
	for _, f := range DocumentBuiltinClasses(meta.Options{AsyncClass: "FooAsync"}) {
		checkXML(t, f.Text)
		path := filepath.Join("testdata", "builtin", f.Name)
		if *update {
			os.MkdirAll(filepath.Dir(path), 0o755)
			os.WriteFile(path, []byte(f.Text), 0o644)
			continue
		}
		if want, _ := os.ReadFile(path); string(want) != f.Text {
			t.Errorf("%s mismatch.\n--- got:\n%s\n--- want:\n%s", path, f.Text, want)
		}
	}
}
