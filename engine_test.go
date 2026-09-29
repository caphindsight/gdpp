// engine_test.go: tests for engine.go. Runs on memFS, in the project from
// build_test.go; SCons itself is only run by hand.

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gd++/trans"
)

// withEngineFS is withBuildFS with engines 4.3 and 4.4 cached.
func withEngineFS(t *testing.T) *memFS {
	m := withBuildFS(t)
	for name, minor := range map[string]string{"4.3": "3", "4.4": "4"} {
		for file, text := range map[string]string{
			"version.py":                   "short_name = \"godot\"\nmajor = 4\nminor = " + minor + "\n",
			"core/math/vector3.h":          "struct Vector3 {};\n",
			"core/core_bind.h":             "namespace core_bind {\nclass OS : public Object {\n\tGDCLASS(OS, Object);\n};\nclass ClassDB : public Object {\n\tGDCLASS(ClassDB, Object);\n};\n}\n",
			"core/object/ref.h":            "class RefCounted : public Object {\n\tGDCLASS(RefCounted, Object);\n};\n",
			"core/os/os.h":                 "class OS {};\n",
			"core/error/error.h":           "enum Error {\n\tOK,\n\tFAILED = 1,\n};\n",
			"scene/main/node.h":            "class Node : public Object {\n\tGDCLASS(Node, Object);\n};\n",
			"scene/tests/test.h":           "class Vector3 {};\n",
			"editor/editor_node.h":         "class EditorNode : public Node {\n\tGDCLASS(EditorNode, Node);\n};\n",
			"modules/regex/regex.h":        "namespace util {}\n",
			"core/object/object.h":         testObjectHeader,
			"core/object/make_virtuals.py": testMakeVirtuals,
		} {
			path := "/games/my_game/.gd++proj/engine/" + name + "/" + file
			if err := m.MkdirAll(path[:strings.LastIndex(path, "/")], 0o755); err != nil {
				t.Fatal(err)
			}
			m.nodes[path] = &memNode{data: []byte(text)}
		}
	}
	return m
}

// testObjectHeader and testMakeVirtuals are the parts of the engine's files that patchEngine patches.
const (
	testObjectHeader = "class Object {\n\t_ALWAYS_INLINE_ const ObjectGDExtension *_get_extension() const { return _extension; }\n};\n"
	testMakeVirtuals = `proto = """#define GDVIRTUAL$VER($RET m_name $ARG)\\
	_FORCE_INLINE_ bool _gdvirtual_##m_name##_call($CALLARGS) $CONST {\\
		$SCRIPTCALL\\
			$CALLPTRARGS\\
			$CALLPTRRETDEF\\
				$CALLPTRRET\\
	}\\
	_FORCE_INLINE_ bool _gdvirtual_##m_name##_overridden() const {\\
		return false;\\
	}
"""
`
)

// testEnginePackages returns the project's packages, as buildEngine sees them without GD++ files.
func testEnginePackages() []enginePackage {
	var pkgs []enginePackage
	p := LoadProject(Cwd())
	for _, pkg := range p.ListPackages() {
		classes, runtime, includes := registeredClasses(pkg, nil)
		pkgs = append(pkgs, enginePackage{pkg, classes, runtime, includes})
	}
	return pkgs
}

func TestEngineSconsArgs(t *testing.T) {
	cases := []struct {
		o    BuildOptions
		want []string
	}{
		{BuildOptions{engine: true}, []string{"platform=linuxbsd", "arch=x86_64", "custom_modules=../modules", "target=template_debug", "optimize=speed"}},
		{BuildOptions{engine: true, Ship: true, Jobs: 4}, []string{"platform=linuxbsd", "arch=x86_64", "custom_modules=../modules", "target=template_release", "production=yes", "optimize=speed", "-j4"}},
		{BuildOptions{engine: true, Small: true}, []string{"platform=linuxbsd", "arch=x86_64", "custom_modules=../modules", "target=template_debug", "optimize=size"}},
	}
	for _, tc := range cases {
		if got := tc.o.engineSconsArgs("linux.x86_64"); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%+v.engineSconsArgs() = %q, want %q", tc.o, got, tc.want)
		}
	}
	if got, want := (BuildOptions{engine: true}).describe(hostPlatform+"."+hostArch, false), hostPlatform+"."+hostArch+", debug, optimized"; got != want {
		t.Errorf("describe() = %q, want %q", got, want)
	}
}

func TestEngineBinary(t *testing.T) {
	for _, tc := range []struct {
		platform, arch string
		ship           bool
		want           string
	}{
		{"linux", "x86_64", false, "godot.linuxbsd.template_debug.x86_64"},
		{"windows", "arm64", true, "godot.windows.template_release.arm64.exe"},
	} {
		if got := engineBinary(tc.platform, tc.arch, tc.ship); got != tc.want {
			t.Errorf("engineBinary(%q, %q, %v) = %q, want %q", tc.platform, tc.arch, tc.ship, got, tc.want)
		}
	}
}

func TestEngineEnv(t *testing.T) {
	m := withEngineFS(t)
	t.Setenv("LOCALAPPDATA", `C:\Users\me\AppData\Local`)
	t.Setenv("TMPDIR", "/tmp")
	env := engineEnv(projectBuildCache(LoadProject(Cwd())))
	tmp := "/games/my_game/.gd++proj/build/tmp"
	for _, kv := range env {
		if strings.HasPrefix(kv, "LOCALAPPDATA=") || kv == "TMPDIR=/tmp" {
			t.Errorf("engineEnv() has %q", kv)
		}
	}
	for _, key := range []string{"TMPDIR", "TEMP", "TMP"} {
		if !slices.Contains(env, key+"="+tmp) {
			t.Errorf("engineEnv() lacks %s=%s", key, tmp)
		}
	}
	if _, ok := m.nodes[tmp]; !ok {
		t.Errorf("engineEnv() didn't create %s", tmp)
	}
}

func TestScanEngineHeaders(t *testing.T) {
	src := "enum Error {\n\tOK,\n\tFAILED = (1 << 2),\n\tERR_BUG,\n};\nenum class Key { A, B };\nnamespace core_bind::special {\nclass ClassDB : public Object {\n\tGDCLASS(ClassDB, Object);\n};\n}\nclass Node : public Object {\n\tGDCLASS(Node, Object);\n\tenum Flags { X };\n};\nclass Forward;\n"
	var got []cppDecl
	for _, d := range scanCppDecls(src, "") {
		got = append(got, d)
	}
	want := []cppDecl{
		{name: "Error"},
		{name: "Key"},
		{name: "Node", base: "Object"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scanCppDecls() = %+v, want %+v", got, want)
	}
	if got, want := scanGdclasses(src), []gdclass{{"core_bind::special::ClassDB", "Object"}, {"Node", "Object"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("scanGdclasses() = %q, want %q", got, want)
	}
}

func TestPrepareEngineBuild(t *testing.T) {
	m := withEngineFS(t)
	withTTY(t, false)
	withQuiet(t, false)
	p := LoadProject(Cwd())
	var cache Path
	out := captureStderr(t, func() { cache = prepareEngineBuild(p, "4.3") })
	if want := "[-] Task succeeded: copying engine 4.3\n"; !strings.HasSuffix(out, want) {
		t.Errorf("output = %q, want it to end with %q", out, want)
	}
	if got := m.tree()["/games/my_game/.gd++proj/build/godot/core/math/vector3.h"]; got != "struct Vector3 {};\n" {
		t.Errorf("the engine copy has vector3.h = %q", got)
	}
	if got := m.tree()["/games/my_game/.gd++proj/build/godot/core/object/object.h"]; !strings.Contains(got, "_gdpp_call_virtual") {
		t.Errorf("the engine copy isn't patched, object.h = %q", got)
	}

	// Built files stay: the engine is only copied once.
	cache.Cd("godot", "bin").CreateDirectory()
	captureStderr(t, func() { prepareEngineBuild(p, "4.3") })
	if !cache.Cd("godot", "bin").IsDir() {
		t.Errorf("building again deleted the engine's bin directory")
	}

	// A new engineBuildVersion starts over.
	state := cache.Cd("build.toml")
	state.WriteString(encodeToml(engineBuildState{"4.3", engineBuildVersion - 1}))
	captureStderr(t, func() { prepareEngineBuild(p, "4.3") })
	if cache.Cd("godot", "bin").Exists() {
		t.Errorf("a new version didn't start over")
	}
	cache.Cd("godot", "bin").CreateDirectory()

	// Another engine starts over, and warns that it's for another Godot version.
	out = captureStderr(t, func() { prepareEngineBuild(p, "4.4") })
	if want := "[!] Engine 4.4 is Godot 4.4, but the project is made for Godot 4.3.\n"; !strings.HasSuffix(out, want) {
		t.Errorf("output = %q, want it to end with %q", out, want)
	}
	if cache.Cd("godot", "bin").Exists() || !strings.Contains(m.tree()["/games/my_game/.gd++proj/build/godot/version.py"], "minor = 4") {
		t.Errorf("switching engines didn't copy the engine anew")
	}

	out = captureStderr(t, func() { (&CmdClean{Proj: true}).Run() })
	if want := "[-] Task succeeded: cleaning res://.gd++proj/build\n"; !strings.HasSuffix(out, want) {
		t.Errorf("clean --proj output = %q, want it to end with %q", out, want)
	}
	if cache.Exists() {
		t.Errorf("clean --proj didn't delete the project build cache")
	}
}

func TestPatchEngine(t *testing.T) {
	src, ok := patchMakeVirtuals(testMakeVirtuals)
	if !ok || strings.Count(src, "_gdpp_has_virtual(_gdpp_sn)") != 2 || strings.Count(src, "$CALLPTRRET\\\\\n") != 2 {
		t.Errorf("patchMakeVirtuals = %q, %t", src, ok)
	}
	if !strings.Contains(src, "\t\t\t$CALLPTRARGS\\\\\n\t\t\t$CALLPTRRETDEF\\\\\n\t\t\t_gdpp_call_virtual(") {
		t.Errorf("patchMakeVirtuals didn't keep the indentation of the placeholders, which the script needs to remove them: %q", src)
	}
	if _, ok := patchMakeVirtuals(strings.ReplaceAll(testMakeVirtuals, "_overridden", "_is_overridden")); !ok {
		t.Errorf("patchMakeVirtuals failed on an anchor within a longer name")
	}
	if _, ok := patchMakeVirtuals(strings.ReplaceAll(testMakeVirtuals, "$CONST {", "$CONST\n{")); ok {
		t.Errorf("patchMakeVirtuals succeeded without the _call anchor")
	}
	if _, ok := patchObjectHeader("class Object {};\n"); ok {
		t.Errorf("patchObjectHeader succeeded without its anchor")
	}
}

// TestPatchRealEngines checks that patchEngine supports the engines in the directory GDPP_ENGINES, e.g.
// ~/gdpp-dep/data/engine, and that their patched make_virtuals.py generates the GDVIRTUAL macros.
func TestPatchRealEngines(t *testing.T) {
	dir := os.Getenv("GDPP_ENGINES")
	if dir == "" {
		t.Skip("Set GDPP_ENGINES to a directory of engines to patch them.")
	}
	engines, _ := os.ReadDir(dir)
	for _, e := range engines {
		godot := dir + "/" + e.Name() + "/core/object/"
		header, err := os.ReadFile(godot + "object.h")
		if err != nil {
			continue
		}
		if _, ok := patchObjectHeader(string(header)); !ok {
			t.Errorf("%s: patchObjectHeader failed", e.Name())
		}
		script, _ := os.ReadFile(godot + "make_virtuals.py")
		patched, ok := patchMakeVirtuals(string(script))
		if !ok {
			t.Errorf("%s: patchMakeVirtuals failed", e.Name())
			continue
		}
		file := t.TempDir() + "/make_virtuals.py"
		os.WriteFile(file, []byte(patched), 0o644)
		out, err := exec.Command("python3", "-c", "import sys; sys.path.insert(0, sys.argv[1]); import make_virtuals as m; "+
			"m.run([sys.argv[1] + '/out.inc'], None, None); print(open(sys.argv[1] + '/out.inc').read())", filepath.Dir(file)).CombinedOutput()
		text := string(out)
		switch {
		case err != nil:
			t.Errorf("%s: make_virtuals.py failed: %v\n%s", e.Name(), err, text)
		case strings.Contains(text, "$"):
			t.Errorf("%s: make_virtuals.py left a placeholder: %s", e.Name(), text[strings.Index(text, "$")-200:][:400])
		case !strings.Contains(text, "_gdpp_call_virtual(_gdpp_sn, (const void **)(reinterpret_cast<"):
			t.Errorf("%s: make_virtuals.py doesn't pass arguments to GD++ classes", e.Name())
		}
	}
}

func TestBuildEngineMissing(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		withEngineFS(t)
		buildEngine(LoadProject(Cwd()), "9.9", BuildOptions{engine: true}, []string{"linux.x86_64"})
		return
	}
	out, code := runFailHelper(t, "TestBuildEngineMissing")
	if want := "[x] Missing Godot engine 9.9, run `gd++ fetch --engine=9.9` to fix this.\n"; code != 1 || out != want {
		t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, want)
	}
}

func TestLoadEngineNames(t *testing.T) {
	m := withEngineFS(t)
	p := LoadProject(Cwd())
	captureStderr(t, func() { prepareEngineBuild(p, "4.3") })
	cache := projectBuildCache(p)
	names := loadEngineNames(cache)
	want := []godotName{
		{"ClassDBSingleton", `"core/core_bind.h"`, trans.Object, "::core_bind::ClassDB"},
		{"Error", `"core/error/error.h"`, trans.Other, ""},
		{"Node", `"scene/main/node.h"`, trans.Object, ""},
		{"OS", `"core/core_bind.h"`, trans.Object, "::core_bind::OS"},
		{"Object", `"core/object/object.h"`, trans.Object, ""},
		{"RefCounted", `"core/object/ref.h"`, trans.RefCounted, ""},
		{"Vector3", `"core/math/vector3.h"`, trans.Other, ""},
		{"core_bind", `"core/core_bind.h"`, trans.Other, ""},
		{"util", `"modules/regex/regex.h"`, trans.Other, ""},
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("loadEngineNames() = %+v, want %+v", names, want)
	}
	// Cached: later scans don't read the headers.
	m.nodes["/games/my_game/.gd++proj/build/godot/core/math/vector3.h"].data = []byte("")
	if got := loadEngineNames(cache); !reflect.DeepEqual(got, want) {
		t.Errorf("cached loadEngineNames() = %+v, want %+v", got, want)
	}
}

func TestPackageEngineNames(t *testing.T) {
	names := []godotName{{Name: "Node"}, {Name: "Player"}}
	files := []gdppFile{{Decls: []trans.Declaration{{Name: "Player"}}}}
	if got, want := packageEngineNames(names, files), []godotName{{Name: "Node"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("packageEngineNames() = %+v, want %+v", got, want)
	}
}

func TestGenerateEngineModule(t *testing.T) {
	m := withEngineFS(t)
	p := LoadProject(Cwd())
	cache := projectBuildCache(p)
	cache.CreateDirectory()
	generateEngineModule(p, cache, testEnginePackages())
	module := subtree(m.tree(), "/games/my_game/.gd++proj/build/modules/gdpp/")
	for file, wants := range map[string][]string{
		"SCsub": {
			"env.add_source_files(env.modules_sources, \"register_types.cpp\")\n\n# Package res://src/pkg: ",
			`env_package.Append(CXXFLAGS=[("/std:" if env.msvc else "-std=") + "c++20"])`,
			`env_package.Prepend(CPPPATH=["../../../../src/pkg", "../../../..", "../../gdpp/0"])`,
			`env.modules_sources += env_package.Object("../../obj/0/register_0.cpp" + env["OBJSUFFIX"], "register_0.cpp")` + "\n" +
				`env.modules_sources += env_package.Object("../../obj/0/package/enemy/enemy.cc" + env["OBJSUFFIX"], "../../../../src/pkg/enemy/enemy.cc")`,
			`"../../obj/1/package/skip.cpp" + env["OBJSUFFIX"], "../../../../src/pkg/nested/skip.cpp")`,
		},
		"register_0.cpp": {
			"#include \"core/object/class_db.h\"\n\n#include \"enemy/enemy.h\"\n#include <common/actor.h>\n\nnamespace godot {}\nusing namespace godot;\n\n// In an anonymous namespace, since every package has its own.\nnamespace {\n\ntemplate <typename T>",
			"} // namespace\n\nvoid gdpp_register_package_0() {\n\tgdpp_register_class<Enemy>();\n\tgdpp_register_class<Actor>();\n\tgdpp_register_class<Helper>();\n\tgdpp_register_class<Hidden>();\n}\n",
		},
		"register_1.cpp":     {"void gdpp_register_package_1() {\n}\n"},
		"register_types.cpp": {"\nvoid gdpp_register_package_0();\nvoid gdpp_register_package_1();\n", "\t\treturn;\n\t}\n\tgdpp_register_package_0();\n\tgdpp_register_package_1();\n}\n"},
		"register_types.h":   {"void initialize_gdpp_module(ModuleInitializationLevel p_level);"},
		"config.py":          {"def can_build(env, platform):\n    return True\n"},
	} {
		for _, want := range wants {
			if !strings.Contains(module[file], want) {
				t.Errorf("%s = %s\nwant it to contain %q", file, module[file], want)
			}
		}
	}
}

func TestStripDebugOnly(t *testing.T) {
	src := "struct A {};\n#ifdef DEBUG_ENABLED\nstruct B {};\n#ifdef X\nstruct C {};\n#endif\n#else\nstruct D {};\n#endif // DEBUG_ENABLED\n#if defined(TOOLS_ENABLED)\nstruct E {};\n#endif\n#ifdef X\nstruct F {};\n#endif\n"
	var got []string
	for _, d := range scanCppDecls(stripDebugOnly(src), "") {
		got = append(got, d.name)
	}
	if want := []string{"A", "D", "F"}; !reflect.DeepEqual(got, want) {
		t.Errorf("stripDebugOnly() leaves %q, want %q", got, want)
	}
}
