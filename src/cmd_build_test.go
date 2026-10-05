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
  tool = true

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
	captureStderr(t, func() {
		generateBuildCache(p, LoadPackage(Cwd()))
		generateRegisterTypes(LoadPackage(Cwd()), nil)
	})
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
	if strings.Contains(register, "Async") || strings.Contains(register, "gdpp::") {
		t.Errorf("__register_types__.cpp = %s\nwant no class of tasks or GD++ runtime, since the package has no GD++ classes", register)
	}
	for _, want := range []string{
		"\n#include \"enemy/enemy.h\"\n#include <common/actor.h>\n\nusing",
		"= false || std::is_same_v<T, Enemy> || std::is_same_v<T, Actor> || std::is_same_v<T, Helper> || std::is_same_v<T, Hidden>;",
		"gdpp_is_runtime_class = false || std::is_same_v<T, Enemy> || std::is_same_v<T, Helper> || std::is_same_v<T, Hidden>;",
		"gdpp_is_abstract_class = false;",
		"\tgdpp_register_class<Enemy>();\n\tgdpp_register_class<Actor>();\n\tgdpp_register_class<Helper>();\n\tgdpp_register_class<Hidden>();\n}",
	} {
		if !strings.Contains(register, want) {
			t.Errorf("__register_types__.cpp = %s\nwant it to contain %q", register, want)
		}
	}
	sconstruct := after["SConstruct"]
	for _, want := range []string{
		`project_root = "../../.."`,
		`env.Append(CPPDEFINES=[("GDPP_ASYNC_CLASS", "PkgAsync")])`,
		`env.Append(CPPDEFINES=[("GDPP_QUIT_TIMEOUT_USEC", 1000000)])`,
		`"-std=") + "c++20"`,
		`objects + "package/" + "enemy/enemy.cc" + env["SHOBJSUFFIX"], sources_root + "/" + "enemy/enemy.cc"))` + "\n" +
			`    sources.append(env.SharedObject(objects + "package/" + "main.cpp" + env["SHOBJSUFFIX"], sources_root + "/" + "main.cpp"))` + "\n" +
			`    sources.append(env.SharedObject(objects + "package/" + "util.c++" + env["SHOBJSUFFIX"], sources_root + "/" + "util.c++"))` + "\n\n",
		`name = ".".join(["lib" + "pkg", env["platform"], env["target"].replace("template_", ""), env["arch"]])`,
	} {
		if !strings.Contains(sconstruct, want) {
			t.Errorf("SConstruct = %s\nwant it to contain %q", sconstruct, want)
		}
	}
}

func TestSyncSources(t *testing.T) {
	m := withBuildFS(t)
	NewPath("/games/my_game/src/pkg/main.cpp").WriteString("int main;")
	stale := NewPath("/games/my_game/src/pkg/.gd++pkg/package/gone/gone.cpp")
	stale.CreateParentDirectory()
	stale.WriteString("stale")
	// The GD++ file's copy is the text read for transpiling, not what's on disk.
	NewPath("/games/my_game/src/pkg/player.gd++").WriteString("edited")
	syncSources(LoadProject(Cwd()), LoadPackage(Cwd()), []gdppFile{{Rel: "player.gd++", Src: "read"}})
	got := subtree(m.tree(), "/games/my_game/src/pkg/.gd++pkg/package/")
	want := map[string]string{"main.cpp": "int main;", "util.c++": "", "enemy/": "", "enemy/enemy.cc": "", "enemy/enemy.h": "", "player.gd++": "read"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("package/ = %v, want %v", got, want)
	}
}

func TestGenerateGdextension(t *testing.T) {
	m := withBuildFS(t)
	m.nodes["/games/my_game/.gd++proj/spec/4.3/extension_api.json"].data = []byte(`{"header": {"version_major": 4, "version_minor": 5, "version_patch": 1}}`)
	captureStderr(t, func() { generateBuildCache(LoadProject(Cwd()), LoadPackage(Cwd())) })
	withTTY(t, false)
	withQuiet(t, false)
	generate := func() string { return captureStderr(t, func() { generateGdextension(LoadPackage(Cwd()), nil) }) }
	if got, want := generate(), "[-] Generating .gdextension for res://src/pkg...\n"; got != want {
		t.Errorf("first output = %q, want %q", got, want)
	}
	if got := generate(); got != "" {
		t.Errorf("unchanged output = %q, want none", got)
	}
	tree := m.tree()
	gdextension := tree["/games/my_game/src/pkg/pkg.gdextension"]
	for _, want := range []string{
		"entry_symbol = \"gdpp_library_init\"\ncompatibility_minimum = \"4.5\"\nreloadable = true\n",
		"\n\n[libraries]\n\nlinux.debug.arm64 = \"res://src/pkg/libpkg.linux.debug.arm64.so\"\n",
		"macos.release.x86_64 = \"res://src/pkg/libpkg.macos.release.x86_64.dylib\"\n",
		"windows.release.x86_64 = \"res://src/pkg/libpkg.windows.release.x86_64.dll\"\n\n[icons]\n\n" +
			"Actor = \"res://common/actor.svg\"\nEnemy = \"res://src/pkg/enemy/enemy.svg\"\n",
	} {
		if !strings.Contains(gdextension, want) {
			t.Errorf("pkg.gdextension = %s\nwant it to contain %q", gdextension, want)
		}
	}
	if n := strings.Count(gdextension, "res://src/pkg/libpkg."); n != 16 {
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
		return captureStderr(t, func() {
			generateBuildCache(LoadProject(Cwd()), LoadPackage(Cwd()))
			generateRegisterTypes(LoadPackage(Cwd()), nil)
		})
	}
	sync := "[$] Running task: cleaning res://src/pkg...\n[-] Task succeeded: cleaning res://src/pkg\n[$] Running task: syncing dependencies for res://src/pkg...\n[-] Task succeeded: syncing dependencies for res://src/pkg\n"
	if got, want := generate(), sync+"[-] Registering classes for res://src/pkg...\n"; got != want {
		t.Errorf("first output = %q, want %q", got, want)
	}
	if got := generate(); got != "" {
		t.Errorf("unchanged output = %q, want none", got)
	}
}

func TestGenerateBuildCacheState(t *testing.T) {
	m := withBuildFS(t)
	generate := func() {
		captureStderr(t, func() { generateBuildCache(LoadProject(Cwd()), LoadPackage(Cwd())) })
	}
	generate()
	state := "/games/my_game/src/pkg/.gd++pkg/build.toml"
	if got, want := m.tree()[state], "id = \"pkg\"\n\n[config]\n  bind = \"4.3\"\n  spec = \"4.3\"\n"; !strings.HasPrefix(got, want) {
		t.Errorf("build.toml = %q, want it to start with %q", got, want)
	}

	if strings.Contains(m.tree()[state], "class") {
		t.Errorf("build.toml = %q, want no classes", m.tree()[state])
	}

	// Same id and config but classes: nothing is cleaned or synced, even if the bindings changed.
	NewPath("/games/my_game/.gd++proj/bind/4.3/SConstruct").WriteString("edited")
	m.nodes["/games/my_game/src/pkg/.gd++pkg.toml"].data = []byte(strings.Replace(buildPkgConfig, `name = "Hidden"`, `name = "Shown"`, 1))
	m.nodes["/games/my_game/src/pkg/libpkg.linux.debug.x86_64.so"] = &memNode{}
	generate()
	tree := m.tree()
	if got := tree["/games/my_game/src/pkg/.gd++pkg/godot-cpp/SConstruct"]; got != "bind" {
		t.Errorf("godot-cpp/SConstruct = %q, want %q", got, "bind")
	}
	if _, ok := tree["/games/my_game/src/pkg/libpkg.linux.debug.x86_64.so"]; !ok {
		t.Errorf("libpkg.linux.debug.x86_64.so was deleted")
	}

	// Other config: cleaned, libraries included, and synced again.
	m.nodes["/games/my_game/.gd++proj/spec/4.4/extension_api.json"] = &memNode{data: []byte("{4.4}")}
	m.nodes["/games/my_game/.gd++proj/spec/4.4"] = &memNode{dir: true}
	m.nodes["/games/my_game/src/pkg/.gd++pkg.toml"].data = []byte(strings.Replace(buildPkgConfig, `spec = "4.3"`, `spec = "4.4"`, 1))
	m.nodes["/games/my_game/src/pkg/.gd++pkg/a.o"] = &memNode{}
	generate()
	tree = m.tree()
	if !strings.Contains(tree[state], "spec = \"4.4\"") {
		t.Errorf("build.toml = %q, want it to contain spec 4.4", tree[state])
	}
	if got := tree["/games/my_game/src/pkg/.gd++pkg/extension_api.json"]; got != "{4.4}" {
		t.Errorf("extension_api.json = %q, want %q", got, "{4.4}")
	}
	if got := tree["/games/my_game/src/pkg/.gd++pkg/godot-cpp/SConstruct"]; got != "edited" {
		t.Errorf("godot-cpp/SConstruct = %q, want %q", got, "edited")
	}
	for _, file := range []string{".gd++pkg/a.o", "libpkg.linux.debug.x86_64.so"} {
		if _, ok := tree["/games/my_game/src/pkg/"+file]; ok {
			t.Errorf("%s was not deleted", file)
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
	if want := "[x] Missing Godot API spec 4.3, run `gd++ fetch --missing` to fix this.\n"; code != 1 || out != want {
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
		c    BuildOptions
		want []string
	}{
		{BuildOptions{}, []string{"platform=linux", "arch=x86_64", "target=template_debug", "dev_build=yes", "use_hot_reload=yes", "optimize=none"}},
		{BuildOptions{Ship: true}, []string{"platform=linux", "arch=x86_64", "target=template_release", "lto=auto", "optimize=speed"}},
		{BuildOptions{Opt: true}, []string{"platform=linux", "arch=x86_64", "target=template_debug", "dev_build=yes", "use_hot_reload=yes", "optimize=speed"}},
		{BuildOptions{NoOpt: true, Ship: true}, []string{"platform=linux", "arch=x86_64", "target=template_release", "lto=auto", "optimize=none"}},
		{BuildOptions{Small: true, Ship: true}, []string{"platform=linux", "arch=x86_64", "target=template_release", "lto=auto", "optimize=size"}},
		{BuildOptions{Jobs: 8}, []string{"platform=linux", "arch=x86_64", "target=template_debug", "dev_build=yes", "use_hot_reload=yes", "optimize=none", "-j8"}},
		{BuildOptions{NoWarn: true}, []string{"platform=linux", "arch=x86_64", "target=template_debug", "dev_build=yes", "use_hot_reload=yes", "optimize=none", "--gdpp-nowarn"}},
		{BuildOptions{Asan: true}, []string{"platform=linux", "arch=x86_64", "target=template_debug", "dev_build=yes", "use_hot_reload=yes", "optimize=none", "--gdpp-sanitize=address"}},
		{BuildOptions{Tsan: true, Ubsan: true}, []string{"platform=linux", "arch=x86_64", "target=template_debug", "dev_build=yes", "use_hot_reload=yes", "optimize=none", "--gdpp-sanitize=undefined,thread"}},
		{BuildOptions{CC: "clang"}, []string{"platform=linux", "arch=x86_64", "target=template_debug", "dev_build=yes", "use_hot_reload=yes", "optimize=none", "use_llvm=yes"}},
	}
	for _, tc := range cases {
		if got := tc.c.sconsArgs("linux.x86_64"); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%+v.sconsArgs() = %q, want %q", tc.c, got, tc.want)
		}
	}
	want := []string{"platform=windows", "arch=x86_64", "target=template_debug", "dev_build=yes", "use_hot_reload=yes", "optimize=none", "use_mingw=yes", "use_llvm=no"}
	if got := (BuildOptions{CC: "gcc"}).sconsArgs("windows.x86_64"); !reflect.DeepEqual(got, want) {
		t.Errorf("sconsArgs() with --cc gcc for windows = %q, want %q", got, want)
	}
}

func TestBuildDescribe(t *testing.T) {
	withTTY(t, true)
	host := hostPlatform + "." + hostArch
	cases := []struct {
		c      BuildOptions
		target string
		gdpp   bool
		want   string
	}{
		{BuildOptions{}, host, false, host + ", debug, unoptimized"},
		{BuildOptions{}, host, true, host + ", debug, unoptimized, with docs"},
		{BuildOptions{Ship: true}, host, true, host + ", \x1b[35mrelease\x1b[0m, optimized, no docs"},
		{BuildOptions{NoDoc: true}, host, true, host + ", debug, unoptimized, \x1b[35mno docs\x1b[0m"},
		{BuildOptions{Ship: true, Doc: true}, host, true, host + ", \x1b[35mrelease\x1b[0m, optimized, \x1b[35mwith docs\x1b[0m"},
		{BuildOptions{Ship: true}, host, false, host + ", \x1b[35mrelease\x1b[0m, optimized"},
		{BuildOptions{Opt: true}, host, false, host + ", debug, \x1b[35moptimized\x1b[0m"},
		{BuildOptions{NoWarn: true}, host, true, host + ", debug, unoptimized, \x1b[35mno warnings\x1b[0m, with docs"},
		{BuildOptions{Ship: true, NoOpt: true}, host, false, host + ", \x1b[35mrelease\x1b[0m, \x1b[35munoptimized\x1b[0m"},
		{BuildOptions{Small: true}, "windows.arm64", false, "\x1b[35mwindows.arm64\x1b[0m, debug, \x1b[35msize-optimized\x1b[0m"},
		{BuildOptions{}, host + " windows.arm64", false, host + " \x1b[35mwindows.arm64\x1b[0m, debug, unoptimized"},
		{BuildOptions{CC: "clang", NoWarn: true}, host, false, host + ", debug, unoptimized, \x1b[35mclang\x1b[0m, \x1b[35mno warnings\x1b[0m"},
		{BuildOptions{Asan: true, Ubsan: true}, host, true, host + ", debug, unoptimized, \x1b[35masan\x1b[0m, \x1b[35mubsan\x1b[0m, with docs"},
		{BuildOptions{DebugOptions: DebugOptions{Trace: []string{"combat", "ai"}, Profile: []string{"all"}}}, host, true, host + ", debug, unoptimized, with docs, \x1b[35mtrace combat ai\x1b[0m, \x1b[35mprofiling\x1b[0m"},
		{BuildOptions{DebugOptions: DebugOptions{Profile: []string{"Player"}, Print: true, Period: ptr(5), FPS: 144}}, host, true, host + ", debug, unoptimized, with docs, \x1b[35mprofiling\x1b[0m"},
		{BuildOptions{DebugOptions: DebugOptions{Trace: []string{"all"}}}, host, false, host + ", debug, unoptimized"},
	}
	for _, tc := range cases {
		if got := tc.c.describe(strings.Fields(tc.target), tc.gdpp); got != tc.want {
			t.Errorf("%+v.describe(%q, %v) = %q, want %q", tc.c, tc.target, tc.gdpp, got, tc.want)
		}
	}
}

func TestBuildDocs(t *testing.T) {
	for _, tc := range []struct {
		c    BuildOptions
		want bool
	}{
		{BuildOptions{}, true},
		{BuildOptions{Ship: true}, false},
		{BuildOptions{Ship: true, Doc: true}, true},
		{BuildOptions{NoDoc: true}, false},
	} {
		if got := tc.c.docs(); got != tc.want {
			t.Errorf("%+v.docs() = %v, want %v", tc.c, got, tc.want)
		}
	}
}

func TestGenerateBuildCacheGdpp(t *testing.T) {
	m := withGdppFS(t, map[string]string{"player.gd++": "class_name Player\n", "items/sword.gg": "class Sword {}\n"})
	captureStderr(t, func() { generateBuildCache(LoadProject(Cwd()), LoadPackage(Cwd())) })
	sconstruct := m.tree()[pkgDir+".gd++pkg/SConstruct"]
	for _, want := range []string{
		`AddOption("--gdpp-bindings"`,
		"if GetOption(\"gdpp_bindings\"):\n    Default(None)\n    Default(Dir(\"build/godot-cpp/gen\"))\nelse:",
		`env.Append(CPPPATH=[project_root, "gdpp"])`,
		`env["_CPPINCFLAGS"] = "-iquote " + sources_root`,
		"    for source in Glob(\"gdpp/*.cpp\"):\n        sources.append(env.SharedObject(objects + \"gdpp/\" + source.name + env[\"SHOBJSUFFIX\"], source))\n",
		`docs = Glob("gdpp/doc_classes/*.xml")`,
	} {
		if !strings.Contains(sconstruct, want) {
			t.Errorf("SConstruct = %s\nwant it to contain %q", sconstruct, want)
		}
	}
}

func TestBuildInvalidArgs(t *testing.T) {
	// A compiler that can't build for windows on this machine.
	hostCC := map[bool]string{true: "clang", false: "msvc"}[hostPlatform == "windows"]
	cases := map[string]struct {
		c    CmdBuild
		want string
	}{
		"windows":       {CmdBuild{Windows: true, Platform: "linux"}, "-w and --platform cannot be used together"},
		"for":           {CmdBuild{For: []string{"w.x64"}, Arch: "x86_64"}, "--for cannot be used together with -w, --platform or --arch"},
		"target":        {CmdBuild{For: []string{"w.x64", "web.x64"}}, `"web.x64" is not a valid target, use PLATFORM.ARCH, e.g. windows.x86_64 or w.x64`},
		"noarch":        {CmdBuild{For: []string{"linux"}}, `"linux" is not a valid target, use PLATFORM.ARCH, e.g. windows.x86_64 or w.x64`},
		"opt":           {CmdBuild{BuildOptions: BuildOptions{Opt: true, Small: true}}, "--opt, --small and --noopt cannot be used together"},
		"noopt":         {CmdBuild{BuildOptions: BuildOptions{Small: true, NoOpt: true}}, "--opt, --small and --noopt cannot be used together"},
		"platform":      {CmdBuild{Platform: "web"}, "--platform must be one of windows, linux, macos"},
		"arch":          {CmdBuild{Arch: "mips"}, "--arch must be one of x86_32, x86_64, arm64"},
		"proj":          {CmdBuild{Proj: true, Path: "src"}, "a path and --proj cannot be used together"},
		"jobs":          {CmdBuild{BuildOptions: BuildOptions{Jobs: -1}}, "--jobs cannot be negative"},
		"macro timeout": {CmdBuild{BuildOptions: BuildOptions{DebugOptions: DebugOptions{MacroTimeout: -1}}}, "--macro-timeout cannot be negative"},
		"tsan":          {CmdBuild{BuildOptions: BuildOptions{Asan: true, Tsan: true}}, "--asan and --tsan cannot be used together"},
		"cc":            {CmdBuild{BuildOptions: BuildOptions{CC: "icc"}}, "--cc must be one of gcc, clang, msvc, clang-cl"},
		"ccmac":         {CmdBuild{For: []string{"l.x64", "m.a64"}, BuildOptions: BuildOptions{CC: "gcc"}}, "--cc gcc cannot build for macos"},
		"cclinux":       {CmdBuild{Platform: "linux", BuildOptions: BuildOptions{CC: "msvc"}}, "--cc msvc cannot build for linux"},
		"cchost":        {CmdBuild{Windows: true, BuildOptions: BuildOptions{CC: hostCC}}, "--cc " + hostCC + " cannot build for windows on this machine"},
		"doc":           {CmdBuild{BuildOptions: BuildOptions{Doc: true, NoDoc: true}}, "--doc and --nodoc cannot be used together"},
		"group":         {CmdBuild{BuildOptions: BuildOptions{DebugOptions: DebugOptions{Trace: []string{"combat"}, Profile: []string{"a-b"}}}}, `"a-b" is not a valid group name`},
		"print":         {CmdBuild{BuildOptions: BuildOptions{DebugOptions: DebugOptions{Print: true}}}, "--print needs --profile"},
		"period":        {CmdBuild{BuildOptions: BuildOptions{DebugOptions: DebugOptions{Profile: []string{"all"}, Period: ptr(5)}}}, "--period needs --print"},
		"budget":        {CmdBuild{BuildOptions: BuildOptions{DebugOptions: DebugOptions{Profile: []string{"all"}, FPS: 144}}}, "--fps needs --print"},
		"negative":      {CmdBuild{BuildOptions: BuildOptions{DebugOptions: DebugOptions{Profile: []string{"all"}, Print: true, Period: ptr(0)}}}, "--period must be positive"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if os.Getenv("GDPP_FAIL_HELPER") == "1" {
				isTTY = false
				tc.c.targets()
				return
			}
			out, code := runFailHelper(t, t.Name())
			if want := "[x] Invalid arguments: " + tc.want + ".\n"; code != 1 || out != want {
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

func TestGenerateRegisterTypesAbstract(t *testing.T) {
	withBuildFS(t)
	pkg := NewPath("/games/my_game/src/pkg")
	pkg.Cd(packageFileName).WriteString(strings.Replace(buildPkgConfig, "name = \"Hidden\"", "name = \"Hidden\"\n  abstract = true", 1))
	captureStderr(t, func() { generateRegisterTypes(LoadPackage(Cwd()), nil) })
	register := pkg.Cd(".gd++pkg", "__register_types__.cpp").ReadString()
	for _, want := range []string{
		"gdpp_is_runtime_class = false || std::is_same_v<T, Enemy> || std::is_same_v<T, Helper>;",
		"gdpp_is_abstract_class = false || std::is_same_v<T, Hidden>;",
		"if constexpr (std::is_abstract_v<T>) {\n\t\tGDREGISTER_ABSTRACT_CLASS(T);\n\t} else if constexpr (gdpp_is_abstract_class<T>) {\n\t\tGDREGISTER_VIRTUAL_CLASS(T);",
	} {
		if !strings.Contains(register, want) {
			t.Errorf("__register_types__.cpp = %s\nwant it to contain %q", register, want)
		}
	}
}

func TestGenerateRegisterTypesRuntimeSubclass(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		withBuildFS(t)
		NewPath("/games/my_game/src/pkg/enemy/enemy.h").WriteString("class Helper : public Actor {};\n")
		generateRegisterTypes(LoadPackage(Cwd()), nil)
		return
	}
	out, code := runFailHelper(t, "TestGenerateRegisterTypesRuntimeSubclass")
	want := "[x] Class Helper extends Actor, which isn't a runtime class, so it needs tool or abstract too: Godot doesn't let runtime classes extend it. " +
		"Set one with e.g. `gd++ init res://src/pkg --class Helper --update --tool`.\n"
	if code != 1 || out != want {
		t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, want)
	}
}
