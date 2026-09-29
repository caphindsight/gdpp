// Package meta holds the data exchanged between the GD++ build and the syntax forks (trans/syntax_N).
//
// Compatibility rules. This package is the contract between the build and every syntax fork, past and future.
// A released fork is never edited again, so everything it uses from here must keep compiling and keep meaning
// the same thing forever:
//   - Changes are additive only. Never remove, rename or retype an exported identifier, never change the meaning
//     of an existing field or constant, and never renumber constants (their values are explicit).
//   - A new field's zero value must mean the old behavior, so forks that don't know the field stay correct.
//   - The API surface stays at an absolute minimum: plain data types only (no functions, methods or interfaces),
//     and only what the build and the forks both need to talk to each other. Anything a single fork needs lives
//     in that fork. When in doubt, leave it out: adding later is cheap, removing is impossible.
//   - Import nothing, so no dependency can change underneath old forks.
//
// meta_test.go enforces the first rule against api.golden.
package meta

// Kind says how GD++ code uses a dependency. Stable: additive changes only.
type Kind int

const (
	Object           Kind = 1 // A Godot class that isn't refcounted, used as T*.
	RefCounted       Kind = 2 // A refcounted Godot class, used as Ref<T>.
	Extern           Kind = 3 // A GD++ extern whose base isn't refcounted, used as ExtPtr<T>.
	RefCountedExtern Kind = 4 // A GD++ extern whose base is refcounted, used as ExtRef<T>.
	Enum             Kind = 5 // A GD++ enum.
	Other            Kind = 6 // A name in namespace godot that isn't a class, e.g. TypedArray: code may use it, but not as a GD++ type.
)

// Dependency is a class, extern or enum that a GD++ file may use without declaring it.
// Stable: additive changes only.
type Dependency struct {
	Name    string      // E.g. "Node3D".
	Include string      // What follows #include, e.g. `<godot_cpp/classes/node3d.hpp>` or `"terrain.h"`.
	Kind    Kind        //
	Values  []EnumValue // For Kind Enum: its values, so classes using the enum can expose a copy.
	Cpp     string      // How C++ names it, if not Name, e.g. "::core_bind::OS". Generated code then aliases it.
}

// EnumValue is one value of a GD++ enum. Stable: additive changes only.
type EnumValue struct {
	Name  string // As written in GD++, e.g. "DIAMONDS".
	Value int64  //
	Doc   string // The doc comment's text, without comment markers.
}

// DeclKind says what a GD++ file declares. Stable: additive changes only.
type DeclKind int

const (
	ClassDecl  DeclKind = 1
	ExternDecl DeclKind = 2
	EnumDecl   DeclKind = 3
)

// Declaration is a class, extern or enum type declared in a GD++ file, which other files may use.
// Stable: additive changes only.
type Declaration struct {
	Name   string      //
	Kind   DeclKind    //
	Base   string      // For classes and externs: the base class.
	Values []EnumValue // For enums.
	Icon   string      // For classes: the icon's res:// or pkg:// path, from @icon.
	Tool   bool        // For classes: whether @tool makes its functions run in the editor too.
}

// Options configure code generation for one GD++ file. Stable: additive changes only.
type Options struct {
	Dependencies []Dependency //
	SourceName   string       // How #line names the GD++ file. Default: its path.
	HeaderName   string       // How the source includes the header, and how #line names it. Default: <name>.h.
	CodeName     string       // How #line names the generated source. Default: <name>.cpp.
}
