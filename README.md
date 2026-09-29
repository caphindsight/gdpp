# GD++

Write Godot games in C++, without the boilerplate.

```
class_name HelloWorld
extends Node

@override
func _ready() -> void {
  Label *label = memnew(Label);
  label->set_text("Hello, world!");
  add_child(label);
}
```

Save this as `hello_world.gd++` anywhere in your Godot project and run:

```sh
gd++ init . --bind 10.0.0-stable --spec 4.7.2-stable
gd++ fetch --missing
gd++ build
```

Open the project in Godot. `HelloWorld` is now a node type, like any built-in node. Add it to a scene and run it.

GD++ is two things in one tool:

1. **A miniature build system** for Godot C++ code. It manages the versioned dependencies and builds GDExtension libraries.
2. **The GD++ language.** Definitions look like GDScript, but implementation is regular C++. You get the speed of C++, and you don't write the Godot glue code by hand.

GD++ is one CLI program, `gd++`. It follows the Godot way: everything you need ships in one place.

## Install

GD++ is written in Go. To build it, run:

```sh
make && sudo make install  # Builds gd++ and copies it to /usr/local/bin.
```

Then install the tools GD++ needs to compile C++ (Git, SCons, a C++ compiler, and so on):

```sh
# Caution: this is experimental, it hasn't been tested on all platforms.
gd++ install         # Uses your system's package manager.
gd++ install --echo  # Or just print the commands, to run them yourself.
```

## The build system

### Projects and packages

A **project** is a normal Godot project: a directory with a `project.godot` file.

A **package** is a directory inside the project with a `gd++pkg.toml` file. Each package builds into one GDExtension library. A project can have many packages.

Create a package with `gd++ init`:

```sh
gd++ init my_package --bind 10.0.0-stable --spec 4.7.2-stable
```

This writes `my_package/gd++pkg.toml`:

```toml
bind = "10.0.0-stable"  # The Godot C++ bindings (godot-cpp) to build with.
spec = "4.7.2-stable"   # The Godot API spec: the Godot version to target.
syntax = 0              # The GD++ language version.
std = "c++20"           # The C++ standard.
```

For small projects with a single package, it can be initialized at the project root directory `.` aka `res://`.

Every `.gd++` file in the package (also `.gdpp` or `.gg`) is part of the package. You don't need to list them anywhere.

You can also mix in plain C++ classes, though you'll be missing on many nice features.
Tell GD++ about them, so it registers them with Godot:

```sh
gd++ init my_package --class Foo --include pkg://foo.h [--icon pkg://foo.svg]
```

`pkg://` paths are relative to the package root. `res://` paths are relative to the project root, as in Godot.

### Dependencies

GD++ needs three kinds of dependencies. It downloads them for you from the [GD++ dependency repository](https://github.com/caphindsight/gdpp-dep):

- **Godot C++ bindings**: the godot-cpp library.
- **Godot API specs**: descriptions of all Godot classes, for one Godot version.
- **Godot engines**: the Godot source code. This is currently unused, so don't bother with them.

```sh
gd++ fetch --index            # List what's available.
gd++ fetch --missing          # Download what your packages need.
```

Dependencies live in the project, in one of two caches:

- `.gd++proj/`: the ephemeral cache. Keep it out of version control.
- `_gd++proj/`: the checked in cache. Commit it, so your team gets the same versions.

Use `gd++ checkin` to move dependencies between the caches, `gd++ vendor` to copy them in or out (for example from a `.tar.gz`), and `gd++ rm` to delete them.
So if you're using a custom build of the engine, for example, you can pack its `extension_api.json` into an archive and then `gd++ vendor` it into a GD++ project.

### Building

```sh
gd++ build                        # Build the package in the current directory.
gd++ build --proj                 # Build every package in the project.
gd++ build -w                     # Build for Windows.
gd++ build --for l.x64 w.x64      # Build for Linux and Windows at once.
gd++ build --ship                 # Release build: optimized, no debug symbols or embedded documentation.
```

The build writes the libraries and a `.gdextension` file into the package root, so Godot loads them right away. Debug builds support hot reload: rebuild while the editor is open, and Godot picks up the changes.

Build caches live in `.gd++pkg/` in each package. `gd++ clean` deletes them.

### Exporting your game

Build every package for each platform you ship, in release mode, then export from Godot's Export dialog as usual:

```sh
gd++ build --proj --ship --for l.x64 --for w.x64
```

Add C++ and GD++ source files, `gd++pkg.toml` and `gd++proj.toml` to the preset's exclude filter to keep them out of the export.

### Other commands

- `gd++ ls`: show an overview of the project, its dependencies and packages.
- `gd++ init --vcs git`: set up `.gitignore` files for GD++.
- `gd++ fix`: tidy up the project, e.g. reformat config files and delete leftover temporary files.
- `gd++ trans file.gd++`: print the C++ that GD++ makes from a file. Great for learning the language.

Run `gd++ --help` or `gd++ <command> --help` for all options.

## The GD++ language

GD++ is a small language that compiles to regular C++. Its one goal: make C++ for Godot easy.

To make a Godot class in plain C++, you write a header, a source file, a `_bind_methods` function that registers every method, property and signal, getters and setters, `#include` lines, and code to register the class. In GD++ you write only the parts that matter. GD++ writes the rest.

The syntax looks like GDScript, but it is **not** GDScript.

- Blocks use braces, not indentation.
- Function bodies are plain C++.
- Types are Godot types, and GD++ maps them to C++ types for you.

The full tour of the language is in [tutorial/tutorial.gd++](tutorial/tutorial.gd++). Here are the main parts.

### Classes

A file can start with a file-level class, like in GDScript:

```
class_name Player
extends CharacterBody3D  // Without `extends`, the class extends RefCounted.
```

A file can also have inline classes, in braces:

```
class Inventory {
  extends Node
}
```

Both kinds work the same. GD++ has no nested classes.

Add `@tool` to run a class in the editor, and `@icon("res://player.svg")` to give it an icon.

### Types

Every Godot type maps to one C++ type:

| GD++ type | C++ type |
|---|---|
| `bool` | `bool` |
| `int` | `int64_t` |
| `float` | `float64_t` (a `double`) |
| `Vector3`, `String`, ... | the struct of the same name |
| `Node` and other objects | `Node*` |
| `Resource` and other refcounted objects | `Ref<Resource>` |
| your GD++ enums | a C++ `enum class` |

A value with no type is a `Variant`.

### Functions

```
func greet(name: String, count: int = 1) -> void {
  for (int64_t i = 0; i < count; i++) {
    gd::print("Hello, ", name, "!");
  }
}
```

The signature is GD++. The body is C++. GD++ registers the function with Godot, so GDScript can call it.

`gd` is short for `UtilityFunctions`, Godot's global functions like `print`.
`This` is the name of the current class, like `Self` in Rust.

Attributes change how a function works:

- `@override`: override an engine function, like `_ready` or `_process`. The signature must match exactly.
- `@virtual`: let scripts override this function.
- `@const`: make it a `const` method.
- `@static`: make it `static`.
- `@rpc(...)`: make it callable over the network, with the same arguments as in GDScript.

```
@rpc("any_peer", "call_local", "reliable")
func take_damage(amount: int) -> void {
  gd::print("Took ", amount, " damage.");
}

func hurt_everyone() -> void {
  rpc take_damage(10);  // Like take_damage.rpc(10) in GDScript.
}


func hurt_server() -> void {
  rpc_id(1) take_damage(10);  // Like take_damage.rpc_id(1, 10) in GDScript.
}
```

### Variables and properties

```
var speed: float = 5.0

@export_range(0, 100, 1)
var health: int = 100

@onready
var camera: Camera3D = get_node<Camera3D>("Camera")
```

Each variable becomes a C++ field, and GD++ makes a getter and setter for Godot. The initial value is a C++ expression, or a block of C++ that returns it:

```
@onready
var marker: Node3D = {
  Node3D* res = memnew(Node3D);
  add_child(res);
  return res;
}
```

For custom logic, write a property with its own getter and setter:

```
var ammo: int {
  decl {
    int64_t ammo = 0;
  }
  get {
    return ammo;
  }
  set(val) {
    if (val < 0) val = 0;  // Never negative.
    ammo = val;
  }
}
```

The export annotations of GDScript work too: `@export`, `@export_range`, `@export_enum`, `@export_file`, `@export_group` and more.

### Signals

```
signal died
signal health_changed(new_health: int)

func hit() -> void {
  emit health_changed(health);  // Like health_changed.emit(health) in GDScript.
}
```

`emit` is a bit of magical syntax, it makes sending a signal visually different from calling a function.

### Constructors and destructors

```
ctor {
  gd::print("Created.");
}

dtor {
  gd::print("Deleted.");
}
```

### Enums and constants

```
enum MAX_PLAYERS = 4

enum Suit {
  DIAMONDS
  CLUBS
  SPADES
  HEARTS
}
```

In GD++, an enum is a global type: every file in the package can use it. Godot needs every enum to belong to a class, so GD++ adds it to each class that uses it. In Godot, the values show up as `SUIT_DIAMONDS`, `SUIT_CLUBS` and so on.

### No more #include

You never write `#include` lines. GD++ scans the Godot classes and your own classes, and includes each type you use, in the API or in C++ code. To use a class, just use it.

In the rare case this fails, force an include with `import`:

```
import MeshInstance3D
```

### Documentation

Doc comments become real Godot docs. You see them in the editor's help, like the docs of built-in classes.

```
/// How fast the player runs, in meters per second.
var speed: float = 5.0

/**
  Makes the player jump.
  Does nothing in the air.
*/
func jump() -> void {}
```

### Plain C++ when you need it

You can put any C++ into a class, with `decl` and `impl` blocks:

```
decl {
  // Goes into the class, in its header file.
  void helper();
}

impl {
  // Goes into the class's source file.
  void This::helper() {
    gd::print("Helping.");
  }
}

@global decl {
  // Goes before the class, outside the godot namespace.
  #include <vector>
}
```

Each class gets its own generated header and source file, e.g. `Player.h` and `Player.cpp`.

Use them rarely: they bring the boilerplate back.

You can also have regular `.h` and `.cpp` files in your package.
They participate in the build, and you can include them from `decl` and `impl` blocks.

### Externs

One package can't use another package's classes at compile time. But it can at runtime, with an **extern**: a list of declarations of a class that lives somewhere else.

```
extern Terrain {
  extends Node3D
  func height_at(x: float, z: float) -> float
  signal changed
}

@export var terrain: Terrain
```

Now you can use `terrain` like any other object, e.g. `add_child(terrain.base())`.
Under the hood, GD++ calls its methods by name hash. This works with other GD++ packages, and even with GDExtensions not made with GD++. Extern method calls are slow, so use externs only when you need them.
