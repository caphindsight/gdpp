// gdpp_test.go: tests for gdpp.go. Runs on memFS, in the package from
// cmd_build_test.go.

package main

import (
	"fmt"
	"os"
	"path"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gd++/trans"
)

const pkgDir = "/games/my_game/src/pkg/"

// gdppTestFiles are GD++ files of the package at res://src/pkg, which use
// each other.
var gdppTestFiles = map[string]string{
	"player.gd++": `/// A player.
@icon("pkg://player.svg")
class_name Player
extends Node3D

var weapon: Weapon

func hit(power: Power) -> void {
  TypedArray<int64_t> list;
}

@abstract
class Hitbox {
  extends Resource
}
`,
	"items/weapon.gdpp": `class Weapon {
  extends Resource
  var owner: Player
}

enum Power { WEAK, STRONG = 5 }

@factory
class Blade {
  extends Hitbox
}
`,
	"misc.gg": "@tool\nclass Tiny {\n  @onthread\n  func work() -> void {}\n}\n",
}

// testGodotNames stand in for what scanning godot-cpp finds.
var testGodotNames = []godotName{
	{"Node", "<godot_cpp/classes/node.hpp>", trans.Object, "class", "Object"},
	{"Node3D", "<godot_cpp/classes/node3d.hpp>", trans.Object, "class", "Node"},
	{"Object", "<godot_cpp/classes/object.hpp>", trans.Object, "class", ""},
	{"RefCounted", "<godot_cpp/classes/ref_counted.hpp>", trans.RefCounted, "class", "Object"},
	{"Resource", "<godot_cpp/classes/resource.hpp>", trans.RefCounted, "class", "RefCounted"},
	{"TypedArray", "<godot_cpp/variant/typed_array.hpp>", trans.Other, "template class", "Array"},
}

// withGdppFS is withBuildFS plus files (paths relative to the package root).
func withGdppFS(t *testing.T, files map[string]string) *memFS {
	m := withBuildFS(t)
	for rel, text := range files {
		if err := m.MkdirAll(path.Dir(pkgDir+rel), 0o755); err != nil {
			t.Fatal(err)
		}
		m.nodes[pkgDir+rel] = &memNode{data: []byte(text)}
	}
	return m
}

// transpileTestPackage runs the GD++ steps of a build of res://src/pkg, with
// the names cache already filled, and returns the classes, the logs, and
// whether the bindings were generated.
func transpileTestPackage(t *testing.T, docs bool) (classes []gdppClass, logs string, generated bool) {
	logs = captureStderr(t, func() { classes, generated = runGdppSteps(docs) })
	return classes, logs, generated
}

// runGdppSteps runs the GD++ steps of a build of res://src/pkg, with the
// names cache already filled.
func runGdppSteps(docs bool) (classes []gdppClass, generated bool) {
	p, pkg := LoadProject(Cwd()), LoadPackage(Cwd())
	generateBuildCache(p, pkg)
	if cache := pkg.BuildCache.Cd("godot_names.toml"); !cache.IsFile() {
		cache.WriteString(encodeToml(godotNamesCache{godotNamesVersion, testGodotNames}))
	}
	names := loadGodotNames(pkg, func() { generated = true })
	classes = transpilePackage(pkg, listGdppFiles(p, pkg, 0), names, BuildOptions{NoDoc: !docs})
	generateRegisterTypes(pkg, classes)
	return classes, generated
}

func TestTranspilePackage(t *testing.T) {
	m := withGdppFS(t, gdppTestFiles)
	withTTY(t, false)
	withQuiet(t, false)
	classes, logs, generated := transpileTestPackage(t, true)
	if generated {
		t.Error("The bindings were generated, although the names cache was filled.")
	}
	if !strings.Contains(logs, "[-] Task succeeded: transpiling GD++ code for res://src/pkg\n") {
		t.Errorf("logs = %q, want them to say it transpiled", logs)
	}
	var names, icons []string
	for _, c := range classes {
		names, icons = append(names, c.Name), append(icons, c.Icon)
	}
	if want := []string{"Blade", "Hitbox", "Player", "Tiny", "Weapon"}; !reflect.DeepEqual(names, want) {
		t.Errorf("classes = %q, want %q", names, want)
	}
	if want := []string{"", "", "pkg://player.svg", "", ""}; !reflect.DeepEqual(icons, want) {
		t.Errorf("icons = %q, want %q", icons, want)
	}

	gen := subtree(m.tree(), pkgDir+".gd++build/gdpp/")
	var files []string
	for file := range gen {
		files = append(files, file)
	}
	wantFiles := []string{"Blade.cpp", "Blade.h", "Hitbox.cpp", "Hitbox.h", "Player.cpp", "Player.h", "Power.h", "Tiny.cpp", "Tiny.h", "Weapon.cpp", "Weapon.h",
		"doc_classes/", "doc_classes/Blade.xml", "doc_classes/Hitbox.xml", "doc_classes/PkgAsync.xml", "doc_classes/Player.xml", "doc_classes/Tiny.xml", "doc_classes/Weapon.xml", "gd++/", "gd++/syntax_0.hpp", "gd++/syntax_0_gpu.hpp"}
	slices.Sort(files)
	if !reflect.DeepEqual(files, wantFiles) {
		t.Errorf("generated files = %q, want %q", files, wantFiles)
	}
	for file, wants := range map[string][]string{
		"Player.h": {`#include "Weapon.h"`, `#include "Power.h"`, "#include <godot_cpp/classes/node3d.hpp>",
			"gdpp::Gd<Weapon> weapon{};", "void hit(Power power);", `GDPP_ENUM_TAG(_gdpp_Player_Power, "Player.Power")`},
		"Player.cpp":               {`#include "Player.h"`, "#include <godot_cpp/variant/typed_array.hpp>", `#line 8 "package/player.gd++"`},
		"Weapon.h":                 {`#include "Player.h"`, "gdpp::Gd<Player> owner{};"},
		"Blade.cpp":                {"Engine::get_singleton()->is_editor_hint()"}, // Guarded, since it extends the abstract Hitbox.
		"Power.h":                  {"enum class Power : int64_t {"},
		"doc_classes/Player.xml":   {"A player."},
		"doc_classes/PkgAsync.xml": {`<class name="PkgAsync" inherits="RefCounted"`, `<member name="done" type="bool" setter="" getter="is_done">`},
	} {
		for _, want := range wants {
			if !strings.Contains(gen[file], want) {
				t.Errorf("%s = %s\nwant it to contain %q", file, gen[file], want)
			}
		}
	}
	register := m.tree()[pkgDir+".gd++build/__register_types__.cpp"]
	for _, want := range []string{`#include "Hitbox.h"`, `#include "Player.h"`, `#include "Weapon.h"`, "gdpp_register_class<Hidden>();\n\tgdpp_register_class<Blade>();\n\tgdpp_register_class<Hitbox>();",
		"gdpp_is_runtime_class = false || std::is_same_v<T, Enemy> || std::is_same_v<T, Helper> || std::is_same_v<T, Hidden> || std::is_same_v<T, Player> || std::is_same_v<T, Weapon>;",
		"gdpp_is_abstract_class = false || std::is_same_v<T, Hitbox>;",
		"if constexpr (std::is_abstract_v<T>) {\n\t\tGDREGISTER_ABSTRACT_CLASS(T);\n\t} else if constexpr (gdpp_is_abstract_class<T>) {\n\t\tGDREGISTER_VIRTUAL_CLASS(T);\n\t} else if constexpr (gdpp_is_runtime_class<T>) {\n\t\tGDREGISTER_RUNTIME_CLASS(T);\n\t} else {\n\t\tGDREGISTER_CLASS(T);\n\t}",
		"#include <gd++/syntax_0.hpp>\n", "\tGDREGISTER_CLASS(gdpp::GDPP_ASYNC_CLASS);\n", "\tgdpp::uninitialize();\n"} {
		if !strings.Contains(register, want) {
			t.Errorf("__register_types__.cpp = %s\nwant it to contain %q", register, want)
		}
	}

	// Unchanged classes aren't registered again; removed code and docs are deleted.
	if _, logs, _ := transpileTestPackage(t, true); strings.Contains(logs, "Registering classes") {
		t.Errorf("logs = %q, want no classes registered", logs)
	}
	delete(m.nodes, pkgDir+"misc.gg")
	transpileTestPackage(t, false)
	gen = subtree(m.tree(), pkgDir+".gd++build/gdpp/")
	for _, file := range []string{"Tiny.h", "Tiny.cpp", "doc_classes/", "doc_classes/Player.xml", "doc_classes/PkgAsync.xml"} {
		if _, ok := gen[file]; ok {
			t.Errorf("%s wasn't deleted.", file)
		}
	}
	if _, ok := gen["Player.h"]; !ok {
		t.Error("Player.h was deleted.")
	}
	// Without Async, the package has no class of tasks, but still unloads its GD++ code.
	register = m.tree()[pkgDir+".gd++build/__register_types__.cpp"]
	if strings.Contains(register, "GDPP_ASYNC_CLASS") || !strings.Contains(register, "\tgdpp::uninitialize();\n") {
		t.Errorf("__register_types__.cpp = %s\nwant gdpp::uninitialize() and no GDPP_ASYNC_CLASS", register)
	}
}

func TestTranspilePackageEnumBases(t *testing.T) {
	m := withGdppFS(t, map[string]string{
		"a.gd++": "enum Big { extends Small HUGE }\n",
		"b.gd++": "enum Small { extends Node.ProcessMode TINY }\nenum Result { extends Error }\n",
		"c.gd++": "class Code {\n  func f() -> void { Error e = OK; }\n}\n",
	})
	m.nodes["/games/my_game/.gd++cache/spec/4.3/extension_api.json"].data = []byte(`{
		"global_enums": [{"name": "Error", "values": [{"name": "OK", "value": 0}, {"name": "FAILED", "value": 1}]}],
		"classes": [{"name": "Node", "enums": [{"name": "ProcessMode", "values": [{"name": "PROCESS_MODE_INHERIT", "value": 0}]}]}]}`)
	old := testGodotNames
	testGodotNames = append(slices.Clone(old), godotName{"Error", "<godot_cpp/core/error_macros.hpp>", trans.Other, "enum", ""})
	t.Cleanup(func() { testGodotNames = old })
	withTTY(t, false)
	withQuiet(t, false)
	transpileTestPackage(t, false)
	gen := subtree(m.tree(), pkgDir+".gd++build/gdpp/")
	for file, want := range map[string]string{
		"Big.h":    "\tINHERIT = 0,\n\tTINY = 1,\n\tHUGE = 2,\n",
		"Small.h":  "\tINHERIT = 0,\n\tTINY = 1,\n",
		"Result.h": "\tOK = 0,\n\tFAILED = 1,\n",
		"Code.cpp": "#include <godot_cpp/core/error_macros.hpp>",
	} {
		if !strings.Contains(gen[file], want) {
			t.Errorf("%s = %s\nwant it to contain %q", file, gen[file], want)
		}
	}
}

func TestTranspilePackageEnumBitfields(t *testing.T) {
	m := withGdppFS(t, map[string]string{
		"a.gd++": "@bitfield enum Sizes { extends Control.SizeFlags HUGE }\n",
		"b.gd++": "@bitfield enum More { extends Sizes BIG }\n",
	})
	m.nodes["/games/my_game/.gd++cache/spec/4.3/extension_api.json"].data = []byte(`{"classes": [{"name": "Control", "enums": [
		{"name": "SizeFlags", "is_bitfield": true, "values": [{"name": "SIZE_FILL", "value": 1}, {"name": "SIZE_SHRINK_END", "value": 8}]}]}]}`)
	withTTY(t, false)
	withQuiet(t, false)
	transpileTestPackage(t, false)
	gen := subtree(m.tree(), pkgDir+".gd++build/gdpp/")
	for file, want := range map[string]string{
		"Sizes.h": "\tHUGE = 16,\n};\nGDPP_BITFIELD(Sizes)\n",
		"More.h":  "\tBIG = 32,\n};\nGDPP_BITFIELD(More)\n",
	} {
		if !strings.Contains(gen[file], want) {
			t.Errorf("%s = %s\nwant it to contain %q", file, gen[file], want)
		}
	}
}

func TestTranspilePackageExternBases(t *testing.T) {
	m := withGdppFS(t, map[string]string{
		"a.gd++": "extern_name Big\nextends Small\n",
		"b.gd++": "extern_name Small\nextends RefCounted\n",
		"c.gd++": "class_name User\nextends Node\nvar big: Big\n",
	})
	withTTY(t, false)
	withQuiet(t, false)
	transpileTestPackage(t, false)
	gen := subtree(m.tree(), pkgDir+".gd++build/gdpp/")
	for file, want := range map[string]string{
		"Big.h":  "class Big : public Small {",
		"User.h": "gdpp::Gd<Big> big",
	} {
		if !strings.Contains(gen[file], want) {
			t.Errorf("%s = %s\nwant it to contain %q", file, gen[file], want)
		}
	}
}

func TestTranspilePackageTraits(t *testing.T) {
	m := withGdppFS(t, map[string]string{
		"a.gd++": "trait_name Damageable\nextends Node\nfunc hit() -> void\nfunc alive() -> bool {\n  return true;\n}\n",
		"b.gd++": "class_name Crate\nextends Node\nimplements Damageable\nfunc hit() -> void {}\n",
		"c.gd++": "class_name BigCrate\nextends Crate\n",
		"d.gd++": "class_name Gun\nextends Node\n@export var target: Damageable\n",
	})
	withTTY(t, false)
	withQuiet(t, false)
	transpileTestPackage(t, false)
	gen := subtree(m.tree(), pkgDir+".gd++build/gdpp/")
	for file, want := range map[string]string{
		"Damageable.h": "virtual bool alive() = 0;",
		"Crate.h":      "class Crate : public Node, public Damageable {",
		"Crate.cpp":    "return true;",
		"BigCrate.cpp": "gdpp::implement<Damageable, BigCrate>();",
		"Gun.cpp":      `PROPERTY_HINT_NODE_TYPE, "BigCrate,Crate")`,
	} {
		if !strings.Contains(gen[file], want) {
			t.Errorf("%s = %s\nwant it to contain %q", file, gen[file], want)
		}
	}
}

func TestTranspilePackageCppClasses(t *testing.T) {
	m := withGdppFS(t, map[string]string{
		"boss.gd++":     "class_name Boss\nextends Enemy\non ready {}\n",
		"kid.gd++":      "class Kid {\n  extends Actor\n  enum NOTIFICATION_HIT = 2000\n}\n",
		"tot.gd++":      "class_name Tot\nextends Kid\non hit {}\n",
		"user.gd++":     "class_name User\nextends Node\nvar actor: Actor\nvar enemy: Enemy\nvar kid: Kid\n",
		"enemy/enemy.h": "#pragma once\nnamespace godot {\nclass Enemy : public Node3D {\n  GDCLASS(Enemy, Node3D)\n};\n}\n",
	})
	// Enemy's base, from its header, makes it a node, whose notifications come from the spec.
	m.nodes["/games/my_game/.gd++cache/spec/4.3/extension_api.json"].data = []byte(`{"classes": [{"name": "Node", "constants": [{"name": "NOTIFICATION_READY", "value": 13}]}]}`)
	config := strings.Replace(buildPkgConfig, `icon = "pkg://enemy/enemy.svg"`, `icon = "pkg://enemy/enemy.svg"`+"\n  kind = \"ptr\"", 1)
	m.nodes[pkgDir+packageFileName].data = []byte(strings.Replace(config, "tool = true", "tool = true\n  kind = \"ref\"", 1))
	withTTY(t, false)
	withQuiet(t, false)
	transpileTestPackage(t, false)
	gen := subtree(m.tree(), pkgDir+".gd++build/gdpp/")
	for file, want := range map[string]string{
		"Boss.h":   "class Boss : public Enemy {",
		"Boss.cpp": "_gdpp_body__ready();",
		"Tot.cpp":  "_gdpp_body__hit();", // Kid's notification, from another file.
		"User.h":   "#include \"enemy/enemy.h\"\n#include <common/actor.h>\n",
		"User.cpp": "gdpp::Gd<Kid>",
	} {
		if !strings.Contains(gen[file], want) {
			t.Errorf("%s = %s\nwant it to contain %q", file, gen[file], want)
		}
	}
	for _, want := range []string{"gdpp::Gd<Actor> actor", "gdpp::Gd<Enemy> enemy", "gdpp::kind::RefCounted gdpp_kind(Kid *);"} {
		if !strings.Contains(gen["User.h"], want) {
			t.Errorf("User.h = %s\nwant it to contain %q", gen["User.h"], want)
		}
	}
}

func TestLoadGodotNamesOutdated(t *testing.T) {
	m := withBuildFS(t)
	pkg := LoadPackage(Cwd())
	m.nodes[pkgDir+".gd++build/godot_names.toml"] = &memNode{data: []byte("version = 0\n")}
	if err := m.MkdirAll(pkgDir+".gd++build/godot-cpp/include/godot_cpp/classes", 0o755); err != nil {
		t.Fatal(err)
	}
	m.nodes[pkgDir+".gd++build/godot-cpp/include/godot_cpp/classes/ref.hpp"] = &memNode{data: []byte("namespace godot { template <typename T> class Ref {}; }")}
	generated := false
	var names []godotName
	captureStderr(t, func() { names = loadGodotNames(pkg, func() { generated = true }) })
	if want := []godotName{{"Ref", "<godot_cpp/classes/ref.hpp>", trans.Other, "template class", ""}}; !generated || !reflect.DeepEqual(names, want) {
		t.Errorf("generated, names = %v, %v, want true, %v", generated, names, want)
	}
	if cache := m.tree()[pkgDir+".gd++build/godot_names.toml"]; !strings.HasPrefix(cache, fmt.Sprintf("version = %d\n", godotNamesVersion)) || !strings.Contains(cache, `name = "Ref"`) {
		t.Errorf("godot_names.toml = %q, want the new names", cache)
	}
}

func TestTranspilePackageMacros(t *testing.T) {
	m := withGdppFS(t, map[string]string{
		"macros.gd++": "macro_name tagged(name)\n\ngd.class { name = name, body = function()\n  gd.invoke(\"tag\", { value = ctx.package.prefix .. \"/\" .. ctx.package.id })\nend }\n" +
			"template tag(value) {\n  func tag() -> String { return ${gd.quote(value)}; }\n}\n",
		"use.gd++": "invoke tagged(Crate)\n",
	})
	withTTY(t, false)
	withQuiet(t, false)
	classes, _, _ := transpileTestPackage(t, false)
	if len(classes) != 1 || classes[0].Name != "Crate" {
		t.Fatalf("classes = %+v, want Crate", classes)
	}
	gen := subtree(m.tree(), pkgDir+".gd++build/gdpp/")
	// The template's C++ keeps its line in macros.gd++, which #line names by its copy, like the invoking file's.
	for _, want := range []string{`return "Pkg/pkg";`, `#line 7 "package/macros.gd++"`} {
		if !strings.Contains(gen["Crate.cpp"], want) {
			t.Errorf("Crate.cpp = %s\nwant it to contain %q", gen["Crate.cpp"], want)
		}
	}
}

func TestTranspilePackageMacroLibraries(t *testing.T) {
	m := withGdppFS(t, map[string]string{
		"a.gd++":   "macro { function greeting() return \"Hi \" .. name() end }\n",
		"b.gd++":   "macro_library\nfunction name() return \"there\" end\n",
		"use.gd++": "class Crate {\n  func f() -> String { invoke { gd.text(\"return \" .. gd.quote(greeting()) .. \";\") } }\n}\n",
	})
	withTTY(t, false)
	withQuiet(t, false)
	transpileTestPackage(t, false)
	if gen := subtree(m.tree(), pkgDir+".gd++build/gdpp/"); !strings.Contains(gen["Crate.cpp"], `return "Hi there";`) {
		t.Errorf("Crate.cpp = %s\nwant it to contain the greeting", gen["Crate.cpp"])
	}
}

func TestTranspilePackageAnnotations(t *testing.T) {
	m := withGdppFS(t, map[string]string{
		"a.gd++":   "annotation save\n",
		"b.gd++":   "annotation save\nannotation key\n",
		"use.gd++": "class Crate {\n  @@save @@key(\"k\") var x: int\n  var n: String = invoke { gd.text(gd.quote(gd.annotation(ctx.members[1], \"@@key\")[1])) }\n}\n",
	})
	withTTY(t, false)
	withQuiet(t, false)
	transpileTestPackage(t, false)
	if gen := subtree(m.tree(), pkgDir+".gd++build/gdpp/"); !strings.Contains(gen["Crate.cpp"], `"k"`) {
		t.Errorf("Crate.cpp = %s\nwant it to contain the key", gen["Crate.cpp"])
	}
}

func TestTranspilePackageFails(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		want  string
	}{
		"duplicate": {map[string]string{"a.gd++": "class Twin {}\n", "b.gd++": "enum Twin { A }\n"},
			"[x] The name Twin is declared in both res://src/pkg/a.gd++ and res://src/pkg/b.gd++.\n"},
		"duplicate macro": {map[string]string{"a.gd++": "macro twin() {\n}\n", "b.gd++": "template twin() {\n}\n"},
			"[x] The name twin is declared in both res://src/pkg/a.gd++ and res://src/pkg/b.gd++.\n"},
		"unknown annotation": {map[string]string{"a.gd++": "class Crate {\n  @@sav var x: int\n}\n", "b.gd++": "annotation save\n"},
			"[x] res://src/pkg/a.gd++:2:3: Unknown annotation @@sav.\n     2 |   @@sav var x: int\n       |   ^^^^^\n    Hint: Did you mean \"@@save\"?\n"},
		"config clash": {map[string]string{"enemy.gd++": "class Enemy {}\n"},
			"[x] Class Enemy is declared in res://src/pkg/enemy.gd++ and in res://src/pkg/.gd++pkg.\n"},
		"class of tasks clash": {map[string]string{"a.gd++": "class PkgAsync {\n  @onthread\n  func f() -> void {}\n}\n"},
			"[x] Class PkgAsync is declared in res://src/pkg/a.gd++, but GD++ adds a class of that name for Async types. Set another prefix with `gd++ init res://src/pkg --prefix NAME`.\n"},
		"syntax error": {map[string]string{"bad.gd++": "fun f() {}\n"},
			"[x] res://src/pkg/bad.gd++:1:1: Expected a declaration, but found name \"fun\".\n     1 | fun f() {}\n       | ^^^\n    Hint: Did you mean \"func\"?\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if os.Getenv("GDPP_FAIL_HELPER") == "1" {
				withGdppFS(t, tc.files)
				withTTY(t, false)
				withQuiet(t, true)
				runGdppSteps(true)
				return
			}
			out, code := runFailHelper(t, t.Name())
			if code != 1 || !strings.HasSuffix(out, tc.want) {
				t.Errorf("exit code = %d, output = %q, want 1 and output ending in %q", code, out, tc.want)
			}
		})
	}
}
