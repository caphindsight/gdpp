# GD++
<p align="right">
  <img src="gdpp_logo.png" alt="GD++ logo" width="128" align="right">
</p>

Write Godot games in C++, without the boilerplate.

> [!NOTE]
> The code was mostly written by AI, but a senior engineer made all design decisions.

## Overview

GD++ is one command line program, `gd++`, that does two jobs:

1. **A small build system for Godot C++ code.** It downloads the Godot C++ bindings (godot-cpp) and the Godot API specs your code needs, keeps them in your project, and compiles your code into GDExtension libraries, which Godot loads like its own modules.
2. **The compiler of the GD++ language.** GD++ compiles to plain C++. Its declarations look like GDScript, and its function bodies are C++. GD++ writes the glue code that Godot needs for you: headers, `_bind_methods`, getters and setters, `#include` lines and class registration.

Since GD++ compiles to C++, your own logic, like math and loops, runs 15 to 50 times faster than in GDScript, in release builds.

GD++ follows Godot's "all in one" approach: everything ships in one tool, and a new project takes one or two commands to set up.

### Status

GD++ is ready to be used, but it's still experimental. It comes with **absolutely no warranty**. Language versions are designed for backward compatibility (see the table below), and breaking changes are unlikely, but at this point no guarantees of any kind can be given.

| Syntax | Status |
|---|---|
| 0 | Nightly: the language as it's being developed, with no backward compatibility. Never use it in production. |
| 1 | Stable in theory, but experimental in practice. |

Each package chooses its syntax version in its `gd++pkg.toml`. Future syntax versions are expected to be stable in practice, not just in theory.

GD++ is licensed under the [MIT License](LICENSE).

## A taste of GD++

```gdscript
/// The player's character.
class_name Player
extends CharacterBody3D

signal died

@export_range(0, 100, 1)
var health: int = 100

@export
var speed: float = 5.0

func hurt(amount: int) -> void {
  health -= amount;
  if (health <= 0) {
    emit died();
  }
}

@override
func _physics_process(delta: float) -> void {
  Vector2 input = Input::get_singleton()->get_vector("ui_left", "ui_right", "ui_up", "ui_down");
  set_velocity(Vector3(input.x, 0, input.y) * speed);
  move_and_slide();
}
```

Save it as `player.gd++` anywhere in a Godot project, and run:

```sh
gd++ init . --bind 10.0.0-stable --spec 4.7.2-stable
gd++ fetch --missing
gd++ build
```

Open the project in Godot: `Player` is now a node type, like any built-in node, with `health` and `speed` in the inspector, and its doc comment in the editor's help.

## Installation

You need Godot 4, and Go to build `gd++` itself:

```sh
make && sudo make install   # builds gd++ and copies it to /usr/local/bin
```

Then install the tools that compile C++ (Git, SCons, Python and a C++ compiler) with your system's package manager:

```sh
gd++ install          # asks before running anything
gd++ install --echo   # or just prints the commands, to run them yourself
```

`gd++ install` is experimental: it hasn't been tested on every platform. It supports Debian, Ubuntu, Fedora, Arch, openSUSE, macOS (with Homebrew) and Windows (with winget). On Linux and macOS, it also installs MinGW-w64, to build for Windows.

You never download godot-cpp or anything else from Godot yourself: GD++ does it for you.

## The build tool

A **project** is a normal Godot project. A **package** is a directory in it with a `gd++pkg.toml` file, and builds into one GDExtension library, from all the GD++ and C++ files in it. **Dependencies** are the Godot C++ bindings and the Godot API specs that packages build with.

```sh
gd++ init --vcs git                                       # keep GD++'s files out of git
gd++ init . --bind 10.0.0-stable --spec 4.7.2-stable      # make the project root a package
gd++ init tools --bind 10.0.0-stable --spec 4.7.2-stable  # or add a package anywhere

gd++ fetch --index            # list the dependencies that are available
gd++ fetch --missing          # download what the packages need
gd++ checkin --spec 4.7.2-stable   # commit a dependency with the project
gd++ vendor --spec my-4.7 --from my-4.7   # add a custom one, e.g. from your own Godot build

gd++ build                    # build the package you're in, for this machine, in debug mode
gd++ build --proj             # build every package of the project
gd++ build --ship --for l.x64 w.x64   # release builds for Linux and Windows
gd++ clean                    # delete the build cache

gd++ ls                       # an overview of the project
gd++ trans player.gd++        # print the C++ that GD++ generates from a file
gd++ doc TypedArray           # show what godot-cpp declares under a name
gd++ cat player.gd++          # show a file, highlighted
gd++ fix                      # tidy up the project
gd++ man                      # the reference manual, built in
```

The build writes the libraries and a `.gdextension` file into the package root, so Godot loads them right away. Debug builds support hot reload: rebuild while the editor is open, and it picks up the changes. A package can also hold plain C++ classes, written the usual godot-cpp way: `gd++ init . --class Foo --include pkg://foo.h` registers them with Godot.

## The GD++ language

The declarations look like GDScript, but GD++ is not GDScript: blocks use braces, not indentation, function bodies are C++, and every value has a static type. Types are Godot's, e.g. `int`, `Vector3` or `Node3D`, and each one maps to a C++ type, e.g. `int64_t`, `Vector3` and `Node3D *`.

### Classes

```gdscript
class_name Player            // a file-level class: the rest of the file
extends CharacterBody3D

class Bullet {               // an inline class
  extends Area3D
  var damage: int = 10
}
```

Add `@tool` to run a class's code in the editor too, and `@icon("res://player.svg")` to give it an icon.

### Functions

```gdscript
func greet(name: String, times: int = 1) -> void {
  for (int64_t i = 0; i < times; i++) {
    gd::print("Hello, ", name, "!");
  }
}

@override
func _ready() -> void {
  greet("world");
}
```

The signature is GD++, and the body is C++. GD++ binds the function, so GDScript can call it. `gd` is short for `UtilityFunctions`, Godot's global functions, and `This` is the name of the current class.

### Variables and properties

```gdscript
@export var speed: float = 5.0

@onready var camera: Camera3D = get_node<Camera3D>("Camera")

var ammo: int {
  decl {
    int64_t ammo_ = 0;
  }
  get {
    return ammo_;
  }
  set(value) {
    ammo_ = value < 0 ? 0 : value;
  }
}
```

### Signals

```gdscript
signal health_changed(health: int)

func heal() -> void {
  health = 100;
  emit health_changed(health);
}
```

### Enums and constants

```gdscript
enum Suit { DIAMONDS, CLUBS, SPADES, HEARTS }
enum MAX_PLAYERS = 4
```

### Casts

```gdscript
func _on_body_entered(body: Node3D) -> void {
  Player *player = body as Player *;   // null if body isn't a Player
  if (player) {
    player->hurt(10);
  }
}
```

### No more #include

You never write `#include` lines: GD++ finds every class that your code uses, and includes its header.

### Everything else

Each of these has its page in the manual (`gd++ man lang`):

- Classes: `@game_only`, typed arrays and dictionaries (`Array[T]`, `Dictionary[K, V]`), externs for using classes of other packages and GDExtensions at runtime (`extern`, `extern_name`), and file-level enums (`enum_name`).
- Functions: `@virtual`, `@final`, `@const`, `@static`, `@deferred` and `@thread_safe`, and default values written as C++ blocks.
- Exports: `@export_range`, `@export_enum`, `@export_flags`, `@export_file`, `@export_dir`, `@export_multiline`, `@export_placeholder`, `@export_storage`, and the inspector sections `@export_category`, `@export_group` and `@export_subgroup`.
- Enums: bitfields (`@bitfield`), extending GD++ and engine enums (`extends Node.ProcessMode`), and values computed from other values.
- Lifecycle: constructors and destructors (`ctor`, `dtor`), and notification handlers (`notif(PREDELETE) { ... }`).
- Networking: `@rpc` functions, called with `rpc NAME(ARGS)` or `rpc(PEER) NAME(ARGS)`.
- Threads: `@onthread` functions, which return `Async` tasks, used with `is_done`, `claim`, `cancel` and `is_cancelled`.
- Debugging: `@trace` prints calls, variable changes, signals and object lifetimes, and `@profile` times code, live in the editor's monitors. Both generate nothing unless a build turns them on.
- Plain C++: `decl`, `impl` and `@global` blocks, and `import` and `noimport` to adjust includes.
- Doc comments (`///` and `/** */`), which become the editor's help, with tutorials and `@deprecated` and `@experimental` marks.
- Runtime helpers: `gd_assert`, `GDPP_STRING_NAME` and more.

## Using GD++

Read the manual: `gd++ man` lists its pages, `gd++ man intro` is the place to start, and `gd++ man tour` walks through a first project. The file [tutorial/tutorial.gd++](tutorial/tutorial.gd++) is a tour of the language in one file. To see what GD++ does with your code, run `gd++ trans` on it.

## Credits

GD++ was created by [caphindsight](https://github.com/caphindsight). The MIT License doesn't require it, but if GD++ helps you make a game, a mention in its credits would be very much appreciated.
