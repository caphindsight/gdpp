// Package trans transpiles GD++ files to C++, choosing the syntax fork (trans/syntax_N) at runtime.
package trans

import (
	"fmt"
	"os"

	"gd++/trans/meta"
	"gd++/trans/syntax_0"
)

type (
	Declaration = meta.Declaration
	DeclKind    = meta.DeclKind
	Dependency  = meta.Dependency
	EnumValue   = meta.EnumValue
	Kind        = meta.Kind
	Options     = meta.Options
)

const (
	Object           = meta.Object
	RefCounted       = meta.RefCounted
	Extern           = meta.Extern
	RefCountedExtern = meta.RefCountedExtern
	Enum             = meta.Enum
	ClassDecl        = meta.ClassDecl
	ExternDecl       = meta.ExternDecl
	EnumDecl         = meta.EnumDecl
)

// fork is what every syntax fork provides.
type fork struct {
	listClasses    func(filename, src string) ([]meta.Declaration, error)
	documentClass  func(filename, src, class string, opts meta.Options) (string, error)
	generateHeader func(filename, src string, opts meta.Options) (string, error)
	generateSource func(filename, src string, opts meta.Options) (string, error)
	runtimeName    string
	runtimeText    string
}

var forks = map[int]fork{
	0: {syntax_0.ListClasses, syntax_0.DocumentClass, syntax_0.GenerateHeader, syntax_0.GenerateSource,
		syntax_0.RuntimeHeaderName, syntax_0.RuntimeHeader},
}

// load returns the fork for syntax and the contents of the file at path.
func load(path string, syntax int) (fork, string, error) {
	f, ok := forks[syntax]
	if !ok {
		return f, "", fmt.Errorf("Unsupported GD++ syntax %d.", syntax)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return f, "", fmt.Errorf("Failed to read %s: %w", path, err)
	}
	return f, string(src), nil
}

// ListClasses returns the classes, externs and enum types declared in the GD++ file at path.
func ListClasses(path string, syntax int) ([]Declaration, error) {
	f, src, err := load(path, syntax)
	if err != nil {
		return nil, err
	}
	return f.listClasses(path, src)
}

// DocumentClass returns the Godot XML documentation of the class named class in the GD++ file at path.
func DocumentClass(path, class string, opts Options, syntax int) (string, error) {
	f, src, err := load(path, syntax)
	if err != nil {
		return "", err
	}
	return f.documentClass(path, src, class, opts)
}

// GenerateHeader returns the C++ header for the GD++ file at path.
func GenerateHeader(path string, opts Options, syntax int) (string, error) {
	f, src, err := load(path, syntax)
	if err != nil {
		return "", err
	}
	return f.generateHeader(path, src, opts)
}

// GenerateSource returns the C++ source file for the GD++ file at path.
func GenerateSource(path string, opts Options, syntax int) (string, error) {
	f, src, err := load(path, syntax)
	if err != nil {
		return "", err
	}
	return f.generateSource(path, src, opts)
}

// RuntimeHeader returns the name (as generated headers include it) and contents of the header that every
// file generated with the given syntax needs. It doesn't depend on any GD++ file.
func RuntimeHeader(syntax int) (name, text string, err error) {
	f, ok := forks[syntax]
	if !ok {
		return "", "", fmt.Errorf("Unsupported GD++ syntax %d.", syntax)
	}
	return f.runtimeName, f.runtimeText, nil
}
