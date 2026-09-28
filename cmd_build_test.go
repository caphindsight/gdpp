// cmd_build_test.go: tests for cmd_build.go. Runs on memFS, in the project
// from project_test.go; SCons itself is only run by hand.

package main

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

const buildPkgConfig = `bind = "4.3"
spec = "4.3"

[[class]]
  name = "Enemy"
  include = "pkg://enemy/enemy.h"
  icon = "pkg://enemy/enemy.svg"

[[class]]
  name = "Actor"
  include = "res://common/actor.h"
  icon = "res://common/actor.svg"

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
		"src/pkg/util.c++":                      "",
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
	captureStderr(t, func() { generateBuildCache(p, LoadPackage(Cwd())) })
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
			`sources.append(env.SharedObject(objects + "package/" + "main.cpp" + env["SHOBJSUFFIX"], package_root + "/" + "main.cpp"))` + "\n" +
			`sources.append(env.SharedObject(objects + "package/" + "util.c++" + env["SHOBJSUFFIX"], package_root + "/" + "util.c++"))` + "\n\n",
		`package_root + "/lib" + "pkg" + env["suffix"]`,
	} {
		if !strings.Contains(sconstruct, want) {
			t.Errorf("SConstruct = %s\nwant it to contain %q", sconstruct, want)
		}
	}
}

func TestGenerateGdextension(t *testing.T) {
	m := withBuildFS(t)
	m.nodes["/games/my_game/.gd++proj/spec/4.3/extension_api.json"].data = []byte(`{"header": {"version_major": 4, "version_minor": 5, "version_patch": 1}}`)
	captureStderr(t, func() { generateBuildCache(LoadProject(Cwd()), LoadPackage(Cwd())) })
	generateGdextension(LoadPackage(Cwd()))
	tree := m.tree()
	gdextension := tree["/games/my_game/src/pkg/pkg.gdextension"]
	for _, want := range []string{
		"entry_symbol = \"gdpp_library_init\"\ncompatibility_minimum = \"4.5\"\n",
		"\n\n[libraries]\n\nlinux.debug.arm64 = \"res://src/pkg/libpkg.linux.template_debug.arm64.so\"\n",
		"macos.release.x86_64 = \"res://src/pkg/libpkg.macos.template_release.x86_64.dylib\"\n",
		"windows.release.x86_64 = \"res://src/pkg/libpkg.windows.template_release.x86_64.dll\"\n\n[icons]\n\n" +
			"Actor = \"res://common/actor.svg\"\nEnemy = \"res://src/pkg/enemy/enemy.svg\"\n",
	} {
		if !strings.Contains(gdextension, want) {
			t.Errorf("pkg.gdextension = %s\nwant it to contain %q", gdextension, want)
		}
	}
	if n := strings.Count(gdextension, ".template_"); n != 16 {
		t.Errorf("pkg.gdextension lists %d libraries, want 16", n)
	}
	if strings.Contains(gdextension, "macos.debug.x86_32") {
		t.Errorf("pkg.gdextension lists macos.x86_32, which godot-cpp doesn't support")
	}
	if got, want := tree["/games/my_game/src/pkg/pkg.gdextension.uid"], godotUid("res://src/pkg")+"\n"; got != want {
		t.Errorf("pkg.gdextension.uid = %q, want %q", got, want)
	}
}

func TestGodotUid(t *testing.T) {
	uid := godotUid("res://src/pkg")
	if !regexp.MustCompile(`^uid://[a-y0-8]{1,13}$`).MatchString(uid) || uid != godotUid("res://src/pkg") || uid == godotUid("res://src/other") {
		t.Errorf("godotUid() = %q, want a stable, distinct Godot UID", uid)
	}
}

func TestGenerateBuildCacheLogs(t *testing.T) {
	withBuildFS(t)
	withTTY(t, false)
	withQuiet(t, false)
	generate := func() string {
		return captureStderr(t, func() { generateBuildCache(LoadProject(Cwd()), LoadPackage(Cwd())) })
	}
	sync := "[$] Running task: syncing dependencies for res://src/pkg...\n[+] Task succeeded: syncing dependencies for res://src/pkg\n"
	if got, want := generate(), sync+"[>] Generating type registrations for res://src/pkg...\n"; got != want {
		t.Errorf("first output = %q, want %q", got, want)
	}
	if got := generate(); got != "" {
		t.Errorf("unchanged output = %q, want none", got)
	}
}

func TestGenerateBuildCacheDeps(t *testing.T) {
	m := withBuildFS(t)
	generate := func() {
		captureStderr(t, func() { generateBuildCache(LoadProject(Cwd()), LoadPackage(Cwd())) })
	}
	generate()
	deps := "/games/my_game/src/pkg/.gd++pkg/deps.toml"
	if got, want := m.tree()[deps], "bind = \"4.3\"\nspec = \"4.3\"\n"; got != want {
		t.Errorf("deps.toml = %q, want %q", got, want)
	}

	// Same versions: the sync is skipped, even if the bindings changed.
	NewPath("/games/my_game/.gd++proj/bind/4.3/SConstruct").WriteString("edited")
	generate()
	if got := m.tree()["/games/my_game/src/pkg/.gd++pkg/godot-cpp/SConstruct"]; got != "bind" {
		t.Errorf("godot-cpp/SConstruct = %q, want %q", got, "bind")
	}

	// Other versions: synced again.
	m.nodes["/games/my_game/.gd++proj/spec/4.4/extension_api.json"] = &memNode{data: []byte("{4.4}")}
	m.nodes["/games/my_game/.gd++proj/spec/4.4"] = &memNode{dir: true}
	m.nodes["/games/my_game/src/pkg/gd++pkg.toml"].data = []byte(strings.Replace(buildPkgConfig, `spec = "4.3"`, `spec = "4.4"`, 1))
	generate()
	tree := m.tree()
	if got, want := tree[deps], "bind = \"4.3\"\nspec = \"4.4\"\n"; got != want {
		t.Errorf("deps.toml = %q, want %q", got, want)
	}
	if got := tree["/games/my_game/src/pkg/.gd++pkg/extension_api.json"]; got != "{4.4}" {
		t.Errorf("extension_api.json = %q, want %q", got, "{4.4}")
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

func TestBuildTargets(t *testing.T) {
	host := hostPlatform + "." + hostArch
	cases := []struct {
		c    CmdBuild
		want []string
	}{
		{CmdBuild{}, []string{host}},
		{CmdBuild{Windows: true, Arch: "arm64"}, []string{"windows.arm64"}},
		{CmdBuild{Platform: "mac", Arch: "x32"}, []string{"macos.x86_32"}},
		{CmdBuild{For: []string{"w.x64", "linux.a64", "win.x86_64", "mac.x32"}}, []string{"windows.x86_64", "linux.arm64", "macos.x86_32"}},
	}
	for _, tc := range cases {
		if got := tc.c.targets(); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%+v.targets() = %q, want %q", tc.c, got, tc.want)
		}
	}
}

func TestBuildSconsArgs(t *testing.T) {
	cases := []struct {
		c    CmdBuild
		want []string
	}{
		{CmdBuild{}, []string{"platform=linux", "arch=x86_64", "target=template_debug"}},
		{CmdBuild{Opt: true}, []string{"platform=linux", "arch=x86_64", "target=template_debug", "optimize=speed"}},
		{CmdBuild{Small: true, Ship: true}, []string{"platform=linux", "arch=x86_64", "target=template_release", "optimize=size"}},
	}
	for _, tc := range cases {
		if got := tc.c.sconsArgs("linux.x86_64"); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%+v.sconsArgs() = %q, want %q", tc.c, got, tc.want)
		}
	}
}

func TestBuildDescribe(t *testing.T) {
	withTTY(t, true)
	host := hostPlatform + "." + hostArch
	cases := []struct {
		c      CmdBuild
		target string
		want   string
	}{
		{CmdBuild{}, host, host + ", debug build"},
		{CmdBuild{Ship: true, Opt: true}, host, host + ", \x1b[1;35mrelease build\x1b[0m, \x1b[1;34moptimized\x1b[0m"},
		{CmdBuild{Small: true}, "windows.arm64", "\x1b[1;36mwindows.arm64\x1b[0m, debug build, \x1b[1;33moptimized for binary size\x1b[0m"},
	}
	for _, tc := range cases {
		if got := tc.c.describe(tc.target); got != tc.want {
			t.Errorf("%+v.describe(%q) = %q, want %q", tc.c, tc.target, got, tc.want)
		}
	}
}

func TestBuildInvalidArgs(t *testing.T) {
	cases := map[string]struct {
		c    CmdBuild
		want string
	}{
		"windows":  {CmdBuild{Windows: true, Platform: "linux"}, "-w and --platform cannot be used together"},
		"for":      {CmdBuild{For: []string{"w.x64"}, Arch: "x86_64"}, "--for cannot be used together with -w, --platform or --arch"},
		"target":   {CmdBuild{For: []string{"w.x64", "web.x64"}}, `"web.x64" is not a valid target, use PLATFORM.ARCH, e.g. windows.x86_64 or w.x64`},
		"noarch":   {CmdBuild{For: []string{"linux"}}, `"linux" is not a valid target, use PLATFORM.ARCH, e.g. windows.x86_64 or w.x64`},
		"opt":      {CmdBuild{Opt: true, Small: true}, "--opt and --small cannot be used together"},
		"platform": {CmdBuild{Platform: "web"}, "--platform must be one of windows, linux, macos"},
		"arch":     {CmdBuild{Arch: "mips"}, "--arch must be one of x86_32, x86_64, arm64"},
		"proj":     {CmdBuild{Proj: true, Path: "src"}, "a path and --proj cannot be used together"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if os.Getenv("GDPP_FAIL_HELPER") == "1" {
				isTTY = false
				tc.c.targets()
				return
			}
			out, code := runFailHelper(t, t.Name())
			if want := "[!] Invalid arguments: " + tc.want + ".\n"; code != 1 || out != want {
				t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, want)
			}
		})
	}
}

func TestGenerateBuildCacheColor(t *testing.T) {
	for _, tty := range []bool{false, true} {
		m := withBuildFS(t)
		withTTY(t, tty)
		captureStderr(t, func() { generateBuildCache(LoadProject(Cwd()), LoadPackage(Cwd())) })
		sconstruct := m.tree()["/games/my_game/src/pkg/.gd++pkg/SConstruct"]
		if got := strings.Contains(sconstruct, `"-fdiagnostics-color=always"`); got != tty {
			t.Errorf("with a terminal = %v, SConstruct forces colors = %v, want %v", tty, got, tty)
		}
	}
}
