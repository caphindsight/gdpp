// Package trans transpiles GD++ files to C++, choosing the syntax fork (trans/syntax_N) at runtime.
package trans

import (
	"fmt"

	"gd++/trans/meta"
	"gd++/trans/syntax_0"
)

type (
	Declaration = meta.Declaration
	DeclKind    = meta.DeclKind
	Dependency  = meta.Dependency
	EnumValue   = meta.EnumValue
	File        = meta.File
	Kind        = meta.Kind
	Options     = meta.Options
)

const (
	Object           = meta.Object
	RefCounted       = meta.RefCounted
	Extern           = meta.Extern
	RefCountedExtern = meta.RefCountedExtern
	Enum             = meta.Enum
	Other            = meta.Other
	GodotEnum        = meta.GodotEnum
	ClassDecl        = meta.ClassDecl
	ExternDecl       = meta.ExternDecl
	EnumDecl         = meta.EnumDecl
)

// fork is what every syntax fork provides.
type fork struct {
	listClasses   func(filename, src string) ([]meta.Declaration, error)
	documentClass func(filename, src, class string, opts meta.Options) (string, error)
	generate      func(filename, src string, opts meta.Options) ([]meta.File, error)
	runtimeName   string
	runtimeText   string
}

var forks = map[int]fork{
	0: {syntax_0.ListClasses, syntax_0.DocumentClass, syntax_0.Generate,
		syntax_0.RuntimeHeaderName, syntax_0.RuntimeHeader},
}

// get returns the fork for syntax.
func get(syntax int) (fork, error) {
	f, ok := forks[syntax]
	if !ok {
		return f, fmt.Errorf("Unsupported GD++ syntax %d.", syntax)
	}
	return f, nil
}

// Each function takes a GD++ file's name and contents. The name appears in errors, and in #line directives unless
// Options.SourceName is set.

// ListClasses returns the classes, externs and enum types that the GD++ file declares.
func ListClasses(name, src string, syntax int) ([]Declaration, error) {
	f, err := get(syntax)
	if err != nil {
		return nil, err
	}
	return f.listClasses(name, src)
}

// DocumentClass returns the Godot XML documentation of the class named class in the GD++ file.
func DocumentClass(name, src, class string, opts Options, syntax int) (string, error) {
	f, err := get(syntax)
	if err != nil {
		return "", err
	}
	return f.documentClass(name, src, class, opts)
}

// Generate returns the C++ files for the GD++ file, in the order of its declarations.
func Generate(name, src string, opts Options, syntax int) ([]File, error) {
	f, err := get(syntax)
	if err != nil {
		return nil, err
	}
	return f.generate(name, src, opts)
}

// RuntimeHeader returns the name (as generated headers include it) and contents of the header that every
// file generated with the given syntax needs. It doesn't depend on any GD++ file.
func RuntimeHeader(syntax int) (name, text string, err error) {
	f, err := get(syntax)
	if err != nil {
		return "", "", err
	}
	return f.runtimeName, f.runtimeText, nil
}
