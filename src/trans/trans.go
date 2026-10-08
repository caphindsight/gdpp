// Package trans transpiles GD++ files to C++, choosing the syntax fork (trans/syntax_N) at runtime.
package trans

import (
	"fmt"
	"sort"

	"gd++/trans/meta"
	"gd++/trans/syntax_0"
	"gd++/trans/syntax_1"
)

type (
	Declaration = meta.Declaration
	DeclKind    = meta.DeclKind
	Dependency  = meta.Dependency
	EnumValue   = meta.EnumValue
	File        = meta.File
	Kind        = meta.Kind
	Options     = meta.Options
	Signal      = meta.Signal
	SignalParam = meta.SignalParam
)

const (
	Object            = meta.Object
	RefCounted        = meta.RefCounted
	Extern            = meta.Extern
	RefCountedExtern  = meta.RefCountedExtern
	Enum              = meta.Enum
	Other             = meta.Other
	GodotEnum         = meta.GodotEnum
	Macro             = meta.Macro
	Template          = meta.Template
	MacroLibrary      = meta.MacroLibrary
	ShaderLibrary     = meta.ShaderLibrary
	Annotation        = meta.Annotation
	Trait             = meta.Trait
	RefCountedTrait   = meta.RefCountedTrait
	Struct            = meta.Struct
	ClassDecl         = meta.ClassDecl
	ExternDecl        = meta.ExternDecl
	EnumDecl          = meta.EnumDecl
	MacroDecl         = meta.MacroDecl
	TemplateDecl      = meta.TemplateDecl
	LibraryDecl       = meta.LibraryDecl
	ShaderLibraryDecl = meta.ShaderLibraryDecl
	AnnotationDecl    = meta.AnnotationDecl
	TraitDecl         = meta.TraitDecl
	StructDecl        = meta.StructDecl
)

// fork is what every syntax fork provides.
type fork struct {
	listMacros    func(filename, src string) ([]meta.Declaration, error)
	listClasses   func(filename, src string, opts meta.Options) ([]meta.Declaration, error)
	documentClass func(filename, src, class string, opts meta.Options) (string, error)
	builtinDocs   func(opts meta.Options) []meta.File
	generate      func(filename, src string, opts meta.Options) ([]meta.File, error)
	expand        func(filename, src string, opts meta.Options) (string, error)
	runtimeName   string
	runtimeText   string
	gpuName       string // The header of shaders and GpuArray types, empty if the fork has none.
	gpuText       string
}

// Syntax 0 is nightly: it may break code at any time. Every other syntax is a stable snapshot.
var forks = map[int]fork{
	0: {syntax_0.ListMacros, syntax_0.ListClasses, syntax_0.DocumentClass, syntax_0.DocumentBuiltinClasses, syntax_0.Generate,
		syntax_0.Expand, syntax_0.RuntimeHeaderName, syntax_0.RuntimeHeader, syntax_0.GpuRuntimeHeaderName, syntax_0.GpuRuntimeHeader},
	1: {syntax_1.ListMacros, syntax_1.ListClasses, syntax_1.DocumentClass, syntax_1.DocumentBuiltinClasses, syntax_1.Generate,
		syntax_1.Expand, syntax_1.RuntimeHeaderName, syntax_1.RuntimeHeader, syntax_1.GpuRuntimeHeaderName, syntax_1.GpuRuntimeHeader},
}

const (
	NightlySyntax = 0
	LatestSyntax  = 1 // The latest stable syntax.
)

// Syntaxes returns the syntax versions that this version of GD++ supports, in order.
func Syntaxes() []int {
	var syntaxes []int
	for s := range forks {
		syntaxes = append(syntaxes, s)
	}
	sort.Ints(syntaxes)
	return syntaxes
}

// Supports reports whether this version of GD++ supports syntax.
func Supports(syntax int) bool {
	_, ok := forks[syntax]
	return ok
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

// ListMacros returns the macros and templates that the GD++ file declares. ListClasses needs those of the other
// files of the package.
func ListMacros(name, src string, syntax int) ([]Declaration, error) {
	f, err := get(syntax)
	if err != nil {
		return nil, err
	}
	return f.listMacros(name, src)
}

// ListClasses returns the classes, externs and enum types that the GD++ file declares, with what the macros and
// templates in opts.Dependencies generate.
func ListClasses(name, src string, opts Options, syntax int) ([]Declaration, error) {
	f, err := get(syntax)
	if err != nil {
		return nil, err
	}
	return f.listClasses(name, src, opts)
}

// DocumentClass returns the Godot XML documentation of the class named class in the GD++ file.
func DocumentClass(name, src, class string, opts Options, syntax int) (string, error) {
	f, err := get(syntax)
	if err != nil {
		return "", err
	}
	return f.documentClass(name, src, class, opts)
}

// DocumentBuiltinClasses returns the Godot XML documentation of the classes that the runtime adds to a package, e.g.
// its class of tasks, for a package whose GD++ classes use Async: a file "<Class>.xml" per class.
func DocumentBuiltinClasses(opts Options, syntax int) ([]File, error) {
	f, err := get(syntax)
	if err != nil {
		return nil, err
	}
	return f.builtinDocs(opts), nil
}

// Generate returns the C++ files for the GD++ file, in the order of its declarations.
func Generate(name, src string, opts Options, syntax int) ([]File, error) {
	f, err := get(syntax)
	if err != nil {
		return nil, err
	}
	return f.generate(name, src, opts)
}

// Expand returns the GD++ file with its invocations of macros and templates expanded: the GD++ that Generate
// turns into C++.
func Expand(name, src string, opts Options, syntax int) (string, error) {
	f, err := get(syntax)
	if err != nil {
		return "", err
	}
	return f.expand(name, src, opts)
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

// GpuRuntimeHeader returns the name (as generated headers include it) and contents of the header of shaders and
// GpuArray types, which files generated with the given syntax include if they use them. It's empty for a syntax
// without shaders.
func GpuRuntimeHeader(syntax int) (name, text string, err error) {
	f, err := get(syntax)
	if err != nil {
		return "", "", err
	}
	return f.gpuName, f.gpuText, nil
}
