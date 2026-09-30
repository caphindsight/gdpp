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
	GodotEnum        Kind = 7 // An enum of Godot's API, e.g. Node.ProcessMode or Error: GD++ enums may extend it, but it isn't a GD++ type.
)

// Dependency is a class, extern or enum that a GD++ file may use without declaring it.
// Stable: additive changes only.
type Dependency struct {
	Name     string      // E.g. "Node3D".
	Include  string      // What follows #include, e.g. `<godot_cpp/classes/node3d.hpp>`, or `"Terrain.h"` for GD++ declarations.
	Kind     Kind        //
	Values   []EnumValue // For Kind Enum and GodotEnum: its values, so classes using the enum can expose a copy.
	Base     string      // For Kind Enum: the enum it extends, if any. Values then holds only its own values. For classes and externs: the base class, if known.
	Gdpp     bool        // Whether another GD++ file declares it. That file may depend on this one in turn.
	Bitfield bool        // For Kind Enum and GodotEnum: whether it's a bitfield, whose values are flags.
}

// EnumValue is one value of a GD++ enum. Stable: additive changes only.
type EnumValue struct {
	Name  string // As written in GD++, e.g. "DIAMONDS".
	Value int64  //
	Doc   string // The doc comment's text, without comment markers.
	// Whether the value has no "=", so it's the previous value plus one, or in a bitfield the next flag. The
	// translator computes Value, after the values of the enum's Base.
	Implicit bool
	Expr     string // For a value written as an expression other than an integer, e.g. "Suit.HEARTS" or "RED | BOLD": its text. The translator computes Value.
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
	Name     string      //
	Kind     DeclKind    //
	Base     string      // For classes and externs: the base class. For enums: the enum it extends, if any.
	Values   []EnumValue // For enums: its own values, without those of its base.
	Icon     string      // For classes: the icon's res:// or pkg:// path, from @icon.
	Tool     bool        // For classes: whether @tool makes its functions run in the editor too.
	Bitfield bool        // For enums: whether @bitfield makes it a bitfield.
}

// Options configure code generation for one GD++ file. Stable: additive changes only.
type Options struct {
	Dependencies  []Dependency //
	SourceName    string       // How #line names the GD++ file. Default: its path.
	Trace         []string     // The groups whose @trace annotations generate code, or "all". Default: none.
	Profile       []string     // The groups whose @profile annotations generate code, or "all". Default: none.
	ProfilePeriod int          // With Profile, the seconds between the tables of timings printed while the game runs. Default: 0, none.
	ProfileFPS    int          // With ProfilePeriod, the frame rate that the table's budget column assumes. Default: 0, 60 FPS.
}

// File is a generated C++ file. Each declaration of a GD++ file gets a header, named "<Name>.h", and each class
// also a source, named "<Name>.cpp". Stable: additive changes only.
type File struct {
	Name string //
	Text string //
}
