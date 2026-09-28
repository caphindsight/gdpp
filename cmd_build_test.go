// cmd_build_test.go: tests for cmd_build.go. Runs on memFS, in the project
// from project_test.go; SCons itself is only run by hand.

package main

import (
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

const buildPkgConfig = `bind = "4.3"
spec = "4.3"

[[class]]
  name = "Enemy"
  include = "pkg://enemy/enemy.h"

[[class]]
  name = "Actor"
  include = "res://common/actor.h"

[[class]]
  name = "Helper"
  include = "pkg://enemy/enemy.h"

[[class]]
  name = "Hidden"
`

// withBuildFS installs a project with a package at res://src/pkg, its
// bindings and API spec cached, and returns the fs.
func withBuildFS(t *testing.T) *memFS {
	tree := withPackages(map[string]string{"src/pkg": buildPkgConfig, "src/pkg/nested": syncPkgConfig})
	for file, text := range map[string]string{
		".gd++proj/bind/4.3/SConstruct":         "bind",
		".gd++proj/bind/4.3/src/godot.cpp":      "godot",
		".gd++proj/spec/4.3/extension_api.json": "{}",
		"src/pkg/main.cpp":                      "",
		"src/pkg/enemy/enemy.cc":                "",
		"src/pkg/enemy/enemy.h":                 "",
		"src/pkg/enemy/notes.txt":               "",
		"src/pkg/.hidden/skip.cpp":              "",
		"src/pkg/nested/skip.cpp":               "",
		"src/pkg/.gd++pkg/godot-cpp/stale.cpp":  "",
	} {
		tree["/games/my_game/"+file] = text
	}
	return withMemFS(t, "/games/my_game/src/pkg", tree)
}

func TestGenerateBuildCache(t *testing.T) {
	m := withBuildFS(t)
	p := LoadProject(Cwd())
	generateBuildCache(p, LoadPackage(Cwd()))
	after := subtree(m.tree(), "/games/my_game/src/pkg/.gd++pkg/")

	for file, want := range map[string]string{"godot-cpp/SConstruct": "bind", "godot-cpp/src/godot.cpp": "godot", "extension_api.json": "{}"} {
		if after[file] != want {
			t.Errorf("%s = %q, want %q", file, after[file], want)
		}
	}
	if _, ok := after["godot-cpp/stale.cpp"]; ok {
		t.Errorf("godot-cpp/stale.cpp was not deleted")
	}
	register := after["__register_types__.cpp"]
	for _, want := range []string{
		"\n#include \"enemy/enemy.h\"\n#include <common/actor.h>\n\nusing",
		"= false || std::is_same_v<T, Enemy> || std::is_same_v<T, Actor> || std::is_same_v<T, Helper> || std::is_same_v<T, Hidden>;",
		"\tgdpp_register_class<Enemy>();\n\tgdpp_register_class<Actor>();\n\tgdpp_register_class<Helper>();\n\tgdpp_register_class<Hidden>();\n}",
	} {
		if !strings.Contains(register, want) {
			t.Errorf("__register_types__.cpp = %s\nwant it to contain %q", register, want)
		}
	}
	sconstruct := after["SConstruct"]
	for _, want := range []string{
		`project_root = "../../.."`,
		`"-std=") + "c++20"`,
		`objects + "package/" + "enemy/enemy.cc" + env["SHOBJSUFFIX"], package_root + "/" + "enemy/enemy.cc"))` + "\n" +
			`sources.append(env.SharedObject(objects + "package/" + "main.cpp" + env["SHOBJSUFFIX"], package_root + "/" + "main.cpp"))` + "\n\n",
		`package_root + "/lib" + "pkg" + env["suffix"]`,
	} {
		if !strings.Contains(sconstruct, want) {
			t.Errorf("SConstruct = %s\nwant it to contain %q", sconstruct, want)
		}
	}
}

func TestBuildMissingDep(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		m := withBuildFS(t)
		delete(m.nodes, "/games/my_game/.gd++proj/spec/4.3/extension_api.json")
		delete(m.nodes, "/games/my_game/.gd++proj/spec/4.3")
		generateBuildCache(LoadProject(Cwd()), LoadPackage(Cwd()))
		return
	}
	out, code := runFailHelper(t, "TestBuildMissingDep")
	if want := "[!] Missing Godot API spec 4.3, run `gd++ fetch --missing` to fix this.\n"; code != 1 || out != want {
		t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, want)
	}
}

func TestBuildSconsArgs(t *testing.T) {
	host := map[string]string{"linux": "linux", "darwin": "macos", "windows": "windows"}[runtime.GOOS]
	arch := map[string]string{"386": "x86_32", "amd64": "x86_64", "arm64": "arm64"}[runtime.GOARCH]
	cases := []struct {
		c    CmdBuild
		want []string
	}{
		{CmdBuild{}, []string{"platform=" + host, "arch=" + arch, "target=template_debug"}},
		{CmdBuild{Windows: true, Arch: "arm64", Opt: true}, []string{"platform=windows", "arch=arm64", "target=template_debug", "optimize=speed"}},
		{CmdBuild{Platform: "macos", Arch: "x86_32", Small: true, Ship: true}, []string{"platform=macos", "arch=x86_32", "target=template_release", "optimize=size"}},
	}
	for _, tc := range cases {
		if got := tc.c.sconsArgs(); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%+v.sconsArgs() = %q, want %q", tc.c, got, tc.want)
		}
	}
}

func TestBuildInvalidArgs(t *testing.T) {
	cases := map[string]struct {
		c    CmdBuild
		want string
	}{
		"windows":  {CmdBuild{Windows: true, Platform: "linux"}, "-w and --platform cannot be used together"},
		"opt":      {CmdBuild{Opt: true, Small: true}, "--opt and --small cannot be used together"},
		"platform": {CmdBuild{Platform: "web"}, "--platform must be one of windows, linux, macos"},
		"arch":     {CmdBuild{Arch: "mips"}, "--arch must be one of x86_32, x86_64, arm64"},
		"proj":     {CmdBuild{Proj: true, Path: "src"}, "a path and --proj cannot be used together"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if os.Getenv("GDPP_FAIL_HELPER") == "1" {
				isTTY = false
				tc.c.sconsArgs()
				return
			}
			out, code := runFailHelper(t, t.Name())
			if want := "[!] Invalid arguments: " + tc.want + ".\n"; code != 1 || out != want {
				t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, want)
			}
		})
	}
}
