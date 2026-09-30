package syntax_0

import (
	"cmp"
	_ "embed"
	"fmt"
	"maps"
	"slices"

	"gd++/trans/meta"
)

// RuntimeHeaderName is how generated headers include RuntimeHeader.
const RuntimeHeaderName = "gd++/syntax_0.hpp"

// RuntimeHeader holds the helpers that all generated C++ uses. It doesn't depend on any GD++ file.
//
//go:embed runtime.hpp
var RuntimeHeader string

// ListClasses returns the classes, externs and enum types that the GD++ source src declares.
func ListClasses(filename, src string) ([]meta.Declaration, error) {
	u, err := parseUnit(filename, src)
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
		files = append(files, meta.File{Name: s.name + ".h", Text: d.header(s.name)})
		if s.class != nil {
			files = append(files, meta.File{Name: s.name + ".cpp", Text: d.source(s.name)})
		}
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
