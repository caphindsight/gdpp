package syntax_0

import (
	"cmp"
	_ "embed"
	"fmt"
	"maps"
	"slices"
	"strings"

	"gd++/trans/meta"
)

// RuntimeHeaderName is how generated headers include RuntimeHeader.
const RuntimeHeaderName = "gd++/syntax_0.hpp"

// RuntimeHeader holds the helpers that all generated C++ uses. It doesn't depend on any GD++ file.
//
//go:embed runtime.hpp
var RuntimeHeader string

// ListMacros returns the macros, templates and user annotations that the GD++ source src declares, and a LibraryDecl
// if it has macro libraries. Other files need them before ListClasses can expand their invocations.
func ListMacros(filename, src string) ([]meta.Declaration, error) {
	file, err := Parse(filename, src)
	if err != nil {
		return nil, err
	}
	var decls []meta.Declaration
	for _, m := range append([]*Macro{file.FileMacro}, file.InlineMacros...) {
		if m != nil {
			decls = append(decls, meta.Declaration{Name: m.Name, Kind: map[bool]meta.DeclKind{false: meta.MacroDecl, true: meta.TemplateDecl}[m.Template]})
		}
	}
	if len(file.Libraries) > 0 {
		decls = append(decls, meta.Declaration{Kind: meta.LibraryDecl})
	}
	for _, a := range file.UserAnnotations {
		decls = append(decls, meta.Declaration{Name: a.Name, Kind: meta.AnnotationDecl})
	}
	return decls, nil
}

// ListClasses returns the classes, externs and enum types that the GD++ source src declares, with what its
// invocations of the macros and templates in opts.Dependencies generate.
func ListClasses(filename, src string, opts meta.Options) ([]meta.Declaration, error) {
	u, err := parseUnit(filename, src, opts)
	if err != nil {
		return nil, err
	}
	return u.declarations()
}

// Generate returns the C++ files for the GD++ source src, in the order of its declarations.
func Generate(filename, src string, opts meta.Options) ([]meta.File, error) {
	u, err := newUnit(filename, src, opts)
	if err != nil {
		return nil, err
	}
	var files []meta.File
	for _, s := range u.sortedSymbols() {
		d := u.only(s)
		if s.class == nil {
			files = append(files, meta.File{Name: s.name + ".h", Text: d.header(s.name)})
			continue
		}
		header, source := d.header(s.name), d.source(s.name)
		// With --trace, a class with assertions prints them as trace lines, so its header turns tracing on.
		if len(opts.Trace) > 0 && strings.Contains(header+source, assertAny) {
			d.tracing = true
			header = d.header(s.name)
		}
		files = append(files, meta.File{Name: s.name + ".h", Text: header}, meta.File{Name: s.name + ".cpp", Text: source})
	}
	return files, nil
}

// only returns a copy of the unit that generates just the declaration s, as if the file's other declarations
// were dependencies.
func (u *unit) only(s *symbol) *unit {
	d := *u
	self := *s
	self.include, self.gdpp = "", false // Not included by its own files.
	d.symbols = maps.Clone(u.symbols)
	d.symbols[s.name] = &self
	d.enums = slices.DeleteFunc(slices.Clone(u.enums), func(e *symbol) bool { return e != s })
	d.classes = slices.DeleteFunc(slices.Clone(u.classes), func(c *classModel) bool { return c.name != s.name })
	d.externs = slices.DeleteFunc(slices.Clone(u.externs), func(e *externModel) bool { return e.name != s.name })
	return &d
}

// DocumentBuiltinClasses returns the Godot XML documentation of the classes that the runtime adds to a package, e.g.
// its class of tasks, named opts.AsyncClass, for a package whose GD++ classes use Async: a file "<Class>.xml" per class.
func DocumentBuiltinClasses(opts meta.Options) []meta.File {
	name := cmp.Or(opts.AsyncClass, "GdppAsync")
	return []meta.File{{Name: name + ".xml", Text: documentAsyncClass(name)}}
}

// DocumentClass returns the Godot XML documentation of the class named class in the GD++ source src.
func DocumentClass(filename, src, class string, opts meta.Options) (string, error) {
	u, err := newUnit(filename, src, opts)
	if err != nil {
		return "", err
	}
	for _, c := range u.classes {
		if c.name == class {
			return u.document(c), nil
		}
	}
	return "", fmt.Errorf("There is no class %s in %s.", class, filename)
}
