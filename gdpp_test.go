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

class Hitbox {
  extends Resource
}
`,
	"items/weapon.gdpp": `class Weapon {
  extends Resource
  var owner: Player
}

enum Power { WEAK, STRONG = 5 }
`,
	"misc.gg": "@tool\nclass Tiny {}\n",
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
	classes = transpilePackage(pkg, listGdppFiles(p, pkg), names, BuildOptions{NoDoc: !docs})
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
	if want := []string{"Hitbox", "Player", "Tiny", "Weapon"}; !reflect.DeepEqual(names, want) {
		t.Errorf("classes = %q, want %q", names, want)
	}
	if want := []string{"", "pkg://player.svg", "", ""}; !reflect.DeepEqual(icons, want) {
		t.Errorf("icons = %q, want %q", icons, want)
	}

	gen := subtree(m.tree(), pkgDir+".gd++pkg/gdpp/")
	var files []string
	for file := range gen {
		files = append(files, file)
	}
	wantFiles := []string{"Hitbox.cpp", "Hitbox.h", "Player.cpp", "Player.h", "Power.h", "Tiny.cpp", "Tiny.h", "Weapon.cpp", "Weapon.h",
		"doc_classes/", "doc_classes/Hitbox.xml", "doc_classes/Player.xml", "doc_classes/Tiny.xml", "doc_classes/Weapon.xml", "gd++/", "gd++/syntax_0.hpp"}
	slices.Sort(files)
	if !reflect.DeepEqual(files, wantFiles) {
		t.Errorf("generated files = %q, want %q", files, wantFiles)
	}
	for file, wants := range map[string][]string{
		"Player.h": {`#include "Weapon.h"`, `#include "Power.h"`, "#include <godot_cpp/classes/node3d.hpp>",
			"Ref<Weapon> weapon{};", "void hit(Power power);", `GDPP_ENUM_TAG(_gdpp_Player_Power, "Player.Power")`},
		"Player.cpp":             {`#include "Player.h"`, "#include <godot_cpp/variant/typed_array.hpp>", `#line 8 "../player.gd++"`},
		"Weapon.h":               {`#include "Player.h"`, "Player *owner{};"},
		"Power.h":                {"enum class Power : int64_t {"},
		"doc_classes/Player.xml": {"A player."},
	} {
		for _, want := range wants {
			if !strings.Contains(gen[file], want) {
				t.Errorf("%s = %s\nwant it to contain %q", file, gen[file], want)
			}
		}
	}
	register := m.tree()[pkgDir+".gd++pkg/__register_types__.cpp"]
	for _, want := range []string{`#include "Hitbox.h"`, `#include "Player.h"`, `#include "Weapon.h"`, "gdpp_register_class<Hidden>();\n\tgdpp_register_class<Hitbox>();",
		"gdpp_is_runtime_class = false || std::is_same_v<T, Enemy> || std::is_same_v<T, Helper> || std::is_same_v<T, Hidden> || std::is_same_v<T, Hitbox> || std::is_same_v<T, Player> || std::is_same_v<T, Weapon>;",
		"if constexpr (gdpp_is_runtime_class<T>) {\n\t\tGDREGISTER_RUNTIME_CLASS(T);\n\t} else {\n\t\tGDREGISTER_CLASS(T);\n\t}"} {
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
	gen = subtree(m.tree(), pkgDir+".gd++pkg/gdpp/")
	for _, file := range []string{"Tiny.h", "Tiny.cpp", "doc_classes/", "doc_classes/Player.xml"} {
		if _, ok := gen[file]; ok {
			t.Errorf("%s wasn't deleted.", file)
		}
	}
	if _, ok := gen["Player.h"]; !ok {
		t.Error("Player.h was deleted.")
	}
}

func TestTranspilePackageEnumBases(t *testing.T) {
	m := withGdppFS(t, map[string]string{
		"a.gd++": "enum Big { extends Small HUGE }\n",
		"b.gd++": "enum Small { extends Node.ProcessMode TINY }\nenum Result { extends Error }\n",
		"c.gd++": "class Code {\n  func f() -> void { Error e = OK; }\n}\n",
	})
	m.nodes["/games/my_game/.gd++proj/spec/4.3/extension_api.json"].data = []byte(`{
		"global_enums": [{"name": "Error", "values": [{"name": "OK", "value": 0}, {"name": "FAILED", "value": 1}]}],
		"classes": [{"name": "Node", "enums": [{"name": "ProcessMode", "values": [{"name": "PROCESS_MODE_INHERIT", "value": 0}]}]}]}`)
	old := testGodotNames
	testGodotNames = append(slices.Clone(old), godotName{"Error", "<godot_cpp/core/error_macros.hpp>", trans.Other, "enum", ""})
	t.Cleanup(func() { testGodotNames = old })
	withTTY(t, false)
	withQuiet(t, false)
	transpileTestPackage(t, false)
	gen := subtree(m.tree(), pkgDir+".gd++pkg/gdpp/")
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
	m.nodes["/games/my_game/.gd++proj/spec/4.3/extension_api.json"].data = []byte(`{"classes": [{"name": "Control", "enums": [
		{"name": "SizeFlags", "is_bitfield": true, "values": [{"name": "SIZE_FILL", "value": 1}, {"name": "SIZE_SHRINK_END", "value": 8}]}]}]}`)
	withTTY(t, false)
	withQuiet(t, false)
	transpileTestPackage(t, false)
	gen := subtree(m.tree(), pkgDir+".gd++pkg/gdpp/")
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
	gen := subtree(m.tree(), pkgDir+".gd++pkg/gdpp/")
	for file, want := range map[string]string{
		"Big.h":  "class Big : public Small {",
		"User.h": "gdpp::ExtRef<Big> big",
	} {
		if !strings.Contains(gen[file], want) {
			t.Errorf("%s = %s\nwant it to contain %q", file, gen[file], want)
		}
	}
}

func TestLoadGodotNamesOutdated(t *testing.T) {
	m := withBuildFS(t)
	pkg := LoadPackage(Cwd())
	m.nodes[pkgDir+".gd++pkg/godot_names.toml"] = &memNode{data: []byte("version = 0\n")}
	if err := m.MkdirAll(pkgDir+".gd++pkg/godot-cpp/include/godot_cpp/classes", 0o755); err != nil {
		t.Fatal(err)
	}
	m.nodes[pkgDir+".gd++pkg/godot-cpp/include/godot_cpp/classes/ref.hpp"] = &memNode{data: []byte("namespace godot { template <typename T> class Ref {}; }")}
	generated := false
	var names []godotName
	captureStderr(t, func() { names = loadGodotNames(pkg, func() { generated = true }) })
	if want := []godotName{{"Ref", "<godot_cpp/classes/ref.hpp>", trans.Other, "template class", ""}}; !generated || !reflect.DeepEqual(names, want) {
		t.Errorf("generated, names = %v, %v, want true, %v", generated, names, want)
	}
	if cache := m.tree()[pkgDir+".gd++pkg/godot_names.toml"]; !strings.HasPrefix(cache, fmt.Sprintf("version = %d\n", godotNamesVersion)) || !strings.Contains(cache, `name = "Ref"`) {
		t.Errorf("godot_names.toml = %q, want the new names", cache)
	}
}

func TestTranspilePackageFails(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		want  string
	}{
		"duplicate": {map[string]string{"a.gd++": "class Twin {}\n", "b.gd++": "enum Twin { A }\n"},
			"[x] The name Twin is declared in both res://src/pkg/a.gd++ and res://src/pkg/b.gd++.\n"},
		"config clash": {map[string]string{"enemy.gd++": "class Enemy {}\n"},
			"[x] Class Enemy is declared in res://src/pkg/enemy.gd++ and in res://src/pkg/gd++pkg.toml.\n"},
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
