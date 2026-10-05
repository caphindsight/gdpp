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
	Object           Kind = 1  // A Godot class that isn't refcounted, used as T*.
	RefCounted       Kind = 2  // A refcounted Godot class, used as Ref<T>.
	Extern           Kind = 3  // A GD++ extern whose base isn't refcounted, used as ExtPtr<T>.
	RefCountedExtern Kind = 4  // A GD++ extern whose base is refcounted, used as ExtRef<T>.
	Enum             Kind = 5  // A GD++ enum.
	Other            Kind = 6  // A name in namespace godot that isn't a class, e.g. TypedArray: code may use it, but not as a GD++ type.
	GodotEnum        Kind = 7  // An enum of Godot's API, e.g. Node.ProcessMode or Error: GD++ enums may extend it, but it isn't a GD++ type.
	Macro            Kind = 8  // A GD++ macro, which code invokes as "invoke name(...)". Source and File hold its file.
	Template         Kind = 9  // A GD++ template, which code invokes as "invoke name(...)". Source and File hold its file.
	MacroLibrary     Kind = 10 // The macro libraries of a GD++ file, which every macro can use. It has no Name. Source and File hold its file.
	Annotation       Kind = 11 // A user annotation that a GD++ file declares, which code writes as "@@name". Only its Name matters.
	Trait            Kind = 12 // A GD++ trait whose base isn't refcounted, used as TraitPtr<T>. Source and File hold its file.
	RefCountedTrait  Kind = 13 // A GD++ trait whose base is refcounted, used as TraitRef<T>. Source and File hold its file.
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
	Virtuals []string    // For classes: the names of its virtual functions, which subclasses override with @override: a GD++ class's @virtual functions, or a Godot class's own virtual methods, e.g. Node's _input.
	// For classes: the names of its own notifications, without NOTIFICATION_, which on blocks handle: a GD++ class's
	// constants named NOTIFICATION_..., or a Godot class's, e.g. Node's READY.
	Notifications []string
	// For classes of the package: whether Godot registers it as a non-runtime class, with tool or abstract, or as a
	// subclass of such a class. Godot doesn't let runtime classes extend it, so GD++ guards its subclasses instead.
	NonRuntime bool
	Source     string   // For Kind Macro, Template, Trait and RefCountedTrait: the whole GD++ file that declares it, which the translator parses.
	File       string   // For Kind Macro, Template, Trait and RefCountedTrait: that file's name, for errors.
	SourceName string   // For Kind Template, Trait and RefCountedTrait: how #line names that file, like Options.SourceName. Default: File.
	Traits     []string // For classes of the package: the traits it implements itself, without those of its bases.
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
	ClassDecl    DeclKind = 1
	ExternDecl   DeclKind = 2
	EnumDecl     DeclKind = 3
	MacroDecl    DeclKind = 4
	TemplateDecl DeclKind = 5
	LibraryDecl  DeclKind = 6 // The file's macro libraries, "macro { ... }" and macro_library. It has no Name.
	// A user annotation, "annotation name". Unlike other names, several files may declare the same one.
	AnnotationDecl DeclKind = 7
	TraitDecl      DeclKind = 8
)

// Declaration is a class, extern, trait, enum type, macro or template declared in a GD++ file, which other files may use.
// Stable: additive changes only.
type Declaration struct {
	Name       string      //
	Kind       DeclKind    //
	Base       string      // For classes and externs: the base class. For enums: the enum it extends, if any.
	Values     []EnumValue // For enums: its own values, without those of its base.
	Icon       string      // For classes: the icon's res:// or pkg:// path, from @icon.
	Tool       bool        // For classes: whether @tool makes its functions run in the editor too.
	Bitfield   bool        // For enums: whether @bitfield makes it a bitfield.
	GameOnly   bool        // For classes: whether @game_only keeps its code from running in the editor.
	EditorOnly bool        // For classes: whether @editor_only, or @tool("editor_only"), keeps its code from running in the game.
	Abstract   bool        // For classes: whether @abstract keeps the editor and GD++ code from creating its objects.
	Async      bool        // For classes: whether it uses Async, e.g. in an @onthread function, so the package needs its class of tasks.
	Virtuals   []string    // For classes: the names of its @virtual functions, which subclasses can override.
	// For classes: the names of its own notifications, without NOTIFICATION_: its constants named NOTIFICATION_....
	Notifications []string
	Traits        []string // For classes: the traits it implements itself, without those of its bases.
}

// Options configure code generation for one GD++ file. Stable: additive changes only.
type Options struct {
	Dependencies  []Dependency //
	SourceName    string       // How #line names the GD++ file. Default: its path.
	Trace         []string     // The groups whose @trace annotations generate code, or "all". Default: none.
	Profile       []string     // The groups whose @profile annotations generate code, or "all". Default: none.
	ProfilePeriod int          // With Profile, the seconds between the tables of timings printed while the game runs. Default: 0, none.
	ProfileFPS    int          // With ProfilePeriod, the frame rate that the table's budget column assumes. Default: 0, 60 FPS.
	AsyncClass    string       // The name of the package's class of tasks, e.g. "FooAsync", which Async types name. Default: "GdppAsync".
	PackagePath   string       // The package root's res:// path, e.g. "res://addons/foo", which pkg:// paths resolve against. Default: "res://".
	PackageID     string       // The package's ID, e.g. "foo", which macros see. Default: none.
	PackagePrefix string       // The prefix of the classes that GD++ adds to the package, e.g. "Foo", which macros see. Default: none.
	CppStandard   string       // The package's C++ standard, e.g. "c++17", which macros see. Default: none.
	MacroTimeout  int          // The seconds that one macro or template invocation may run. Default: 0, 20 seconds.
	MacroDepth    int          // How deeply macro and template invocations may nest. Default: 0, 64 levels.
	// For each trait of the package, by name: the package's classes that implement it, themselves or through a base,
	// which the inspector offers for exported variables of the trait's type. Default: none, only the file's own classes.
	Implementers map[string][]string
}

// File is a generated C++ file. Each declaration of a GD++ file gets a header, named "<Name>.h", and each class
// also a source, named "<Name>.cpp". Stable: additive changes only.
type File struct {
	Name string //
	Text string //
}
