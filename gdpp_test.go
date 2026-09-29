// gdpp_test.go: tests for gdpp.go. Runs on memFS, in the package from
// cmd_build_test.go.

package main

import (
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
	"misc.gg": "class Tiny {}\n",
}

// testGodotNames stand in for what scanning godot-cpp finds.
var testGodotNames = []godotName{
	{"Node", "<godot_cpp/classes/node.hpp>", trans.Object},
	{"Node3D", "<godot_cpp/classes/node3d.hpp>", trans.Object},
	{"Object", "<godot_cpp/classes/object.hpp>", trans.Object},
	{"RefCounted", "<godot_cpp/classes/ref_counted.hpp>", trans.RefCounted},
	{"Resource", "<godot_cpp/classes/resource.hpp>", trans.RefCounted},
	{"TypedArray", "<godot_cpp/variant/typed_array.hpp>", trans.Other},
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
	classes = transpilePackage(pkg, listGdppFiles(p, pkg), names, docs)
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
	if !strings.Contains(logs, "[-] Transpiling GD++ code for res://src/pkg...\n") {
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
	wantFiles := []string{"doc_classes/", "doc_classes/Hitbox.xml", "doc_classes/Player.xml", "doc_classes/Tiny.xml", "doc_classes/Weapon.xml",
		"gd++/", "gd++/syntax_0.hpp", "items/", "items/weapon.gdpp.cpp", "items/weapon.gdpp.h", "misc.gg.cpp", "misc.gg.h", "player.gd++.cpp", "player.gd++.h"}
	slices.Sort(files)
	if !reflect.DeepEqual(files, wantFiles) {
		t.Errorf("generated files = %q, want %q", files, wantFiles)
	}
	for file, wants := range map[string][]string{
		"player.gd++.h": {`#include "items/weapon.gdpp.h"`, "#include <godot_cpp/classes/node3d.hpp>",
			"Ref<Weapon> weapon{};", "void hit(Power power);", `GDPP_ENUM_TAG(_gdpp_Player_Power, "Player.Power")`},
		"player.gd++.cpp":        {`#include "player.gd++.h"`, "#include <godot_cpp/variant/typed_array.hpp>", `#line 8 "../player.gd++"`},
		"items/weapon.gdpp.h":    {`#include "player.gd++.h"`, "Player *owner{};", "enum class Power : int64_t {"},
		"doc_classes/Player.xml": {"A player."},
	} {
		for _, want := range wants {
			if !strings.Contains(gen[file], want) {
				t.Errorf("%s = %s\nwant it to contain %q", file, gen[file], want)
			}
		}
	}
	register := m.tree()[pkgDir+".gd++pkg/__register_types__.cpp"]
	for _, want := range []string{`#include "items/weapon.gdpp.h"`, `#include "player.gd++.h"`, "gdpp_register_class<Hidden>();\n\tgdpp_register_class<Hitbox>();"} {
		if !strings.Contains(register, want) {
			t.Errorf("__register_types__.cpp = %s\nwant it to contain %q", register, want)
		}
	}

	// Unchanged code isn't rewritten; removed code and docs are deleted.
	if _, logs, _ := transpileTestPackage(t, true); strings.Contains(logs, "Transpiling") {
		t.Errorf("logs = %q, want nothing transpiled", logs)
	}
	delete(m.nodes, pkgDir+"misc.gg")
	transpileTestPackage(t, false)
	gen = subtree(m.tree(), pkgDir+".gd++pkg/gdpp/")
	for _, file := range []string{"misc.gg.h", "misc.gg.cpp", "doc_classes/", "doc_classes/Player.xml"} {
		if _, ok := gen[file]; ok {
			t.Errorf("%s wasn't deleted.", file)
		}
	}
	if _, ok := gen["player.gd++.h"]; !ok {
		t.Error("player.gd++.h was deleted.")
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
	names := loadGodotNames(pkg, func() { generated = true })
	if want := []godotName{{"Ref", "<godot_cpp/classes/ref.hpp>", trans.Other}}; !generated || !reflect.DeepEqual(names, want) {
		t.Errorf("generated, names = %v, %v, want true, %v", generated, names, want)
	}
	if cache := m.tree()[pkgDir+".gd++pkg/godot_names.toml"]; !strings.HasPrefix(cache, "version = 1\n") || !strings.Contains(cache, `name = "Ref"`) {
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
