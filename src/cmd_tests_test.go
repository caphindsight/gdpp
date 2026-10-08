// cmd_tests_test.go: tests for cmd_tests.go. Runs on memFS, in the project
// from project_test.go; Godot itself is only run by hand.

package main

import (
	"os"
	"strings"
	"testing"

	"gd++/trans"
)

// reportTests feeds lines, the output of a runner of the tests named names, to a testReport, and returns the logs
// and what it counted.
func reportTests(t *testing.T, quiet bool, names []string, lines ...string) (logs string, ran int, failed []string) {
	withTTY(t, false)
	withQuiet(t, quiet)
	withBuildFS(t)
	var tests []gdppTest
	for _, name := range names {
		tests = append(tests, gdppTest{Name: name})
	}
	logs = captureStderr(t, func() {
		r := testReport{timeout: 2.5}
		for _, line := range lines {
			r.line(line)
		}
		r.done(LoadPackage(Cwd()), tests)
		ran, failed = r.ran, r.failed
	})
	return logs, ran, failed
}

func TestTestReport(t *testing.T) {
	logs, ran, failed := reportTests(t, false, []string{"T.adds", "T.subtracts", "T.waits", "T.crashes"},
		"Godot Engine v4.7.2",
		testMarker+"start T.adds 0",
		"added",
		testMarker+"pass T.adds 420",
		testMarker+"start T.subtracts 0",
		"ERROR: Failed GD++ assertion: `x == 1`.",
		testMarker+"fail T.subtracts 1250000",
		testMarker+"start T.waits 0",
		testMarker+"timeout T.waits 2500000",
		testMarker+"start T.crashes 0",
		"Segmentation fault",
	)
	want := "" +
		"[$] Running task: running T.adds...\n" +
		"[-] Task succeeded: running T.adds\n" +
		"[-] Passed T.adds in 0.4 ms.\n" +
		"[$] Running task: running T.subtracts...\n" +
		"[-] Task succeeded: running T.subtracts\n" +
		"[x] Failed T.subtracts in 1.25 s.\n" +
		"    ERROR: Failed GD++ assertion: `x == 1`.\n" +
		"[$] Running task: running T.waits...\n" +
		"[-] Task succeeded: running T.waits\n" +
		"[x] Failed T.waits: it ran longer than 2.5 seconds.\n" +
		"[$] Running task: running T.crashes...\n" +
		"[-] Task succeeded: running T.crashes\n" +
		"[x] Failed T.crashes: Godot exited while it ran.\n" +
		"    Segmentation fault\n"
	if logs != want || ran != 4 || strings.Join(failed, ", ") != "T.subtracts, T.waits, T.crashes" {
		t.Errorf("logs = %q, ran = %d, failed = %q, want %q, 4, T.subtracts, T.waits, T.crashes", logs, ran, failed, want)
	}
}

func TestTestReportNotStarted(t *testing.T) {
	logs, ran, failed := reportTests(t, false, []string{"T.a", "T.b"}, "ERROR: Can't load the main loop.")
	want := "" +
		"[x] Godot exited before it ran 2 of the 2 tests of res://src/pkg, after this output.\n" +
		"    ERROR: Can't load the main loop.\n"
	if logs != want || ran != 2 || strings.Join(failed, ", ") != "T.a, T.b" {
		t.Errorf("logs = %q, ran = %d, failed = %q, want %q, 2, T.a, T.b", logs, ran, failed, want)
	}
}

func TestTestReportQuiet(t *testing.T) {
	logs, _, _ := reportTests(t, true, []string{"T.a", "T.b"},
		testMarker+"start T.a 0", "out", testMarker+"pass T.a 1", testMarker+"start T.b 0", "boom", testMarker+"fail T.b 1")
	want := "" +
		"[$] Running task: running T.a...\n" +
		"[-] Task succeeded: running T.a\n" +
		"[$] Running task: running T.b...\n" +
		"[-] Task succeeded: running T.b\n" +
		"[x] Failed T.b in 0.0 ms.\n" +
		"    boom\n"
	if logs != want {
		t.Errorf("logs = %q, want %q", logs, want)
	}
}

// withTestsFS installs withBuildFS's project, with GD++ files with tests in
// res://src/pkg, and returns its package.
func withTestsFS(t *testing.T) Package {
	withGdppFS(t, map[string]string{"a.gd++": "", "b.gd++": "", "sub/c.gd++": ""})
	return LoadPackage(Cwd())
}

func TestTestSelected(t *testing.T) {
	pkg := withTestsFS(t)
	test := func(name, file string) gdppTest {
		return gdppTest{name, gdppFile{File: pkg.Root.Cd(file)}}
	}
	tests := []gdppTest{test("A.one", "a.gd++"), test("A.two", "a.gd++"), test("B.one", "b.gd++"), test("C.one", "sub/c.gd++")}
	cases := map[string]struct {
		c    CmdTest
		want string
	}{
		"all":          {CmdTest{}, "A.one A.two B.one C.one"},
		"dir":          {CmdTest{Paths: []string{"sub"}}, "A.one A.two B.one C.one"},
		"file":         {CmdTest{Paths: []string{"a.gd++"}}, "A.one A.two"},
		"files":        {CmdTest{Paths: []string{"sub/c.gd++", "b.gd++"}}, "B.one C.one"},
		"file_and_pkg": {CmdTest{Paths: []string{"a.gd++", "."}}, "A.one A.two B.one C.one"},
		"names":        {CmdTest{Names: []string{"*.one"}}, "A.one B.one C.one"},
		"class":        {CmdTest{Names: []string{"A.*"}}, "A.one A.two"},
		"file_names":   {CmdTest{Paths: []string{"a.gd++"}, Names: []string{"A.t*", "B.one"}}, "A.two"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var got []string
			for _, test := range tc.c.selected(append([]gdppTest(nil), tests...), pkg, tc.c.files()) {
				got = append(got, test.Name)
			}
			if strings.Join(got, " ") != tc.want {
				t.Errorf("selected = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTestGodotPath(t *testing.T) {
	pkg := withTestsFS(t)
	if got := (&CmdTest{Engine: "./a.gd++"}).godot(LoadProject(Cwd()), pkg); got != pkg.Root.Cd("a.gd++") {
		t.Errorf("godot = %s, want res://src/pkg/a.gd++", got.ToString())
	}
}

func TestTestGodotErrors(t *testing.T) {
	cases := map[string]struct {
		c      CmdTest
		engine string
		want   string
	}{
		"no_engine":      {CmdTest{}, "", "Package res://src/pkg has no Godot engine to run its tests, set one with `gd++ init res://src/pkg --update --engine NAME`, or name a Godot binary with --engine PATH."},
		"missing_engine": {CmdTest{}, "4.7", "Missing Godot engine 4.7, run `gd++ fetch --missing` to fix this."},
		"missing_flag":   {CmdTest{Engine: "4.8"}, "4.7", "Missing Godot engine 4.8, run `gd++ fetch --missing` to fix this."},
		"missing_binary": {CmdTest{Engine: "bin/godot"}, "", "Invalid arguments: res://src/pkg/bin/godot is not a file."},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if os.Getenv("GDPP_FAIL_HELPER") == "1" {
				isTTY = false
				pkg := withTestsFS(t)
				pkg.Config.Engine = tc.engine
				tc.c.godot(LoadProject(Cwd()), pkg)
				return
			}
			out, code := runFailHelper(t, t.Name())
			if want := "[x] " + tc.want + "\n"; code != 1 || out != want {
				t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, want)
			}
		})
	}
}

func TestTestInvalidArgs(t *testing.T) {
	cases := map[string]struct {
		c    CmdTest
		want string
	}{
		"ship":    {CmdTest{BuildOptions: BuildOptions{Ship: true}}, "gd++ test cannot use --ship, since release builds have no tests"},
		"timeout": {CmdTest{Timeout: ptr(0.0)}, "--timeout must be positive"},
		"pattern": {CmdTest{Names: []string{"a["}}, `"a[" is not a valid name pattern`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if os.Getenv("GDPP_FAIL_HELPER") == "1" {
				isTTY = false
				tc.c.Run()
				return
			}
			out, code := runFailHelper(t, t.Name())
			if want := "[x] Invalid arguments: " + tc.want + ".\n"; code != 1 || out != want {
				t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, want)
			}
		})
	}
}

func TestEngineBinary(t *testing.T) {
	withMemFS(t, "/e", map[string]string{"/e/bin/godot.linuxbsd.editor.x86_64.pdb": "", "/e/bin/godot.windows.editor.x86_64.exe": "",
		"/e/bin/godot.windows.editor.x86_64.console.exe": "", "/e/bin/godot.linuxbsd.editor.x86_64": "", "/e/bin/godot.linuxbsd.template_debug.x86_64": ""})
	want := map[string]string{"linux": "godot.linuxbsd.editor.x86_64", "windows": "godot.windows.editor.x86_64.console.exe"}[hostPlatform]
	if want == "" {
		t.Skip("No binaries for this platform in the test.")
	}
	if bin, ok := engineBinary(ParsePath("/e")); !ok || bin.Name() != want {
		t.Errorf("engineBinary = %s, %v, want %s", bin.Name(), ok, want)
	}
	if _, ok := engineBinary(ParsePath("/e/bin")); ok {
		t.Errorf("engineBinary found a binary in a directory without bin")
	}
}

// Only gd++ test registers the @test classes, and the runner of tests.
func TestGenerateRegisterTypesTests(t *testing.T) {
	for _, testing := range []bool{false, true} {
		m := withBuildFS(t)
		captureStderr(t, func() {
			generateBuildCache(LoadProject(Cwd()), LoadPackage(Cwd()))
			file := gdppFile{File: Cwd().Cd("a.gd++")}
			generateRegisterTypes(LoadPackage(Cwd()), []gdppClass{{trans.Declaration{Name: "Player", Kind: trans.ClassDecl, Base: "Node"}, file},
				{trans.Declaration{Name: "PlayerTest", Kind: trans.ClassDecl, Base: "Node", Test: true, Tests: []string{"jumps"}}, file}}, testing)
		})
		register := m.tree()["/games/my_game/src/pkg/.gd++build/__register_types__.cpp"]
		for _, want := range []string{"\n#include \"PlayerTest.h\"\n", "std::is_same_v<T, PlayerTest>", "\tgdpp_register_class<PlayerTest>();\n",
			"\tgdpp_registered<PlayerTest> = false;\n", "\tGDREGISTER_CLASS(gdpp::GDPP_TESTS_CLASS);\n"} {
			if strings.Contains(register, want) != testing {
				t.Errorf("for gd++ test = %v, __register_types__.cpp = %s\nwant it to contain %q: %v", testing, register, want, testing)
			}
		}
		if !strings.Contains(register, "\tgdpp_register_class<Player>();\n") {
			t.Errorf("__register_types__.cpp = %s\nwant it to register Player", register)
		}
		if s := m.tree()["/games/my_game/src/pkg/.gd++build/SConstruct"]; !strings.Contains(s, `("GDPP_TESTS_CLASS", "PkgTests")`) {
			t.Errorf("SConstruct = %s\nwant it to define GDPP_TESTS_CLASS as PkgTests", s)
		}
	}
}

func TestBuildCacheIgnoresEngine(t *testing.T) {
	m := withBuildFS(t)
	p := LoadProject(Cwd())
	pkg := LoadPackage(Cwd())
	captureStderr(t, func() { generateBuildCache(p, pkg) })
	before := m.tree()["/games/my_game/src/pkg/.gd++build/build.toml"]
	pkg.Config.Engine = "4.7"
	captureStderr(t, func() { generateBuildCache(p, pkg) })
	if after := m.tree()["/games/my_game/src/pkg/.gd++build/build.toml"]; after != before || strings.Contains(after, "engine") {
		t.Errorf("build.toml = %q, want %q: the engine only runs tests, so it doesn't change builds", after, before)
	}
}

// The sources of @test classes go in gdpp/test, which release builds don't compile.
func TestTranspileTestClass(t *testing.T) {
	m := withGdppFS(t, map[string]string{"player_test.gd++": "@test\nclass_name PlayerTest\nextends Node\n\n@test\nfunc jumps() {\n  assert true;\n}\n"})
	withTTY(t, false)
	transpileTestPackage(t, false)
	gen := subtree(m.tree(), pkgDir+".gd++build/gdpp/")
	for _, file := range []string{"PlayerTest.h", "test/PlayerTest.cpp"} {
		if _, ok := gen[file]; !ok {
			t.Errorf("generated files = %v, want %s", gen, file)
		}
	}
	if _, ok := gen["PlayerTest.cpp"]; ok {
		t.Errorf("generated files = %v, want no gdpp/PlayerTest.cpp", gen)
	}
}
