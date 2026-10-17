// cmd_tests_test.go: tests for cmd_tests.go. Runs on memFS, in the project
// from project_test.go; Godot itself is only run by hand.

package main

import (
	"os"
	"os/exec"
	"path/filepath"
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
	logs, ran, failed := reportTests(t, false, []string{"T.adds", "T.subtracts", "T.crashes"},
		"Godot Engine v4.7.2",
		testMarker+"start T.adds 0",
		"added",
		testMarker+"pass T.adds 420",
		testMarker+"start T.subtracts 0",
		"ERROR: Failed GD++ assertion: `x == 1`.",
		testMarker+"fail T.subtracts 1250000",
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
		"[$] Running task: running T.crashes...\n" +
		"[-] Task succeeded: running T.crashes\n" +
		"[x] Failed T.crashes: Godot exited while it ran.\n" +
		"    Segmentation fault\n"
	if logs != want || ran != 3 || strings.Join(failed, ", ") != "T.subtracts, T.crashes" {
		t.Errorf("logs = %q, ran = %d, failed = %q, want %q, 3, T.subtracts, T.crashes", logs, ran, failed, want)
	}
}

// A test that blocks Godot's main thread, without output, times out: watch kills the runner.
func TestTestReportTimeout(t *testing.T) {
	withTTY(t, false)
	withQuiet(t, false)
	withBuildFS(t)
	var killed bool
	logs := captureStderr(t, func() {
		r := testReport{timeout: 0.2}
		cmd := exec.Command("sh", "-c", `printf '\037gdpp-test start T.a 0\n\037gdpp-test pass T.a 1\n\037gdpp-test start T.loops 0\nlooping\n'; exec sleep 10`)
		killed = r.watch(cmd, NewPath(cmd.Path))
	})
	want := "" +
		"[$] Running task: running T.a...\n" +
		"[-] Task succeeded: running T.a\n" +
		"[-] Passed T.a in 0.0 ms.\n" +
		"[$] Running task: running T.loops...\n" +
		"[-] Task succeeded: running T.loops\n" +
		"[x] Failed T.loops: it ran longer than 0.2 seconds.\n" +
		"    looping\n"
	if logs != want || !killed {
		t.Errorf("logs = %q, killed = %v, want %q, true", logs, killed, want)
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

func TestTestReportBench(t *testing.T) {
	withTTY(t, false)
	withQuiet(t, false)
	withBuildFS(t)
	var ran int
	var failed []string
	logs := captureStderr(t, func() {
		r := testReport{timeout: 2.5, bench: true}
		for _, line := range []string{
			testMarker + "start T.adds 0",
			testMarker + "pass T.adds 1000000 400000",
			testMarker + "start T.sorts 0",
			testMarker + "pass T.sorts 1200000 1",
			testMarker + "start T.checks 0",
			"ERROR: Failed GD++ assertion: `ok`.",
			testMarker + "fail T.checks 3 1",
			testMarker + "start T.gone 0",
			testMarker + "missing T.gone 0",
		} {
			r.line(line)
		}
		r.done(LoadPackage(Cwd()), []gdppTest{{Name: "T.adds"}, {Name: "T.sorts"}, {Name: "T.checks"}, {Name: "T.gone"}})
		ran, failed = r.ran, r.failed
	})
	want := "" +
		"[$] Running task: running T.adds...\n" +
		"[-] Task succeeded: running T.adds\n" +
		"[-] Benched T.adds: 2.50 µs per call, 400000 calls.\n" +
		"[$] Running task: running T.sorts...\n" +
		"[-] Task succeeded: running T.sorts\n" +
		"[-] Benched T.sorts: 1.20 s per call, 1 call.\n" +
		"[$] Running task: running T.checks...\n" +
		"[-] Task succeeded: running T.checks\n" +
		"[x] Failed T.checks in 0.0 ms.\n" +
		"    ERROR: Failed GD++ assertion: `ok`.\n" +
		"[$] Running task: running T.gone...\n" +
		"[-] Task succeeded: running T.gone\n" +
		"[x] Failed T.gone: the library has no such benchmark.\n"
	if logs != want || ran != 4 || strings.Join(failed, ", ") != "T.checks, T.gone" {
		t.Errorf("logs = %q, ran = %d, failed = %q, want %q, 4, T.checks, T.gone", logs, ran, failed, want)
	}
}

func TestFormatNsec(t *testing.T) {
	for nsec, want := range map[float64]string{0.25: "0.2 ns", 12.34: "12.3 ns", 4560: "4.56 µs", 2500000: "2.5 ms", 1250000000: "1.25 s"} {
		if got := formatNsec(nsec); got != want {
			t.Errorf("formatNsec(%v) = %q, want %q", nsec, got, want)
		}
	}
}

// With bench, gdppTests returns only the benchmarks, without the tests.
func TestGdppTestsBench(t *testing.T) {
	files := []gdppFile{{Decls: []trans.Declaration{
		{Name: "B", Kind: trans.ClassDecl, Test: true, Tests: []string{"works"}, Benches: []string{"runs"}},
		{Name: "A", Kind: trans.ClassDecl, Test: true, Tests: []string{"one", "two"}},
	}}}
	for bench, want := range map[bool]string{false: "A.one A.two B.works", true: "B.runs"} {
		var got []string
		for _, test := range gdppTests(files, bench) {
			got = append(got, test.Name)
		}
		if strings.Join(got, " ") != want {
			t.Errorf("gdppTests(bench = %v) = %q, want %q", bench, got, want)
		}
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
	// Without an engine, the godot in the PATH.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "godot"), nil, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if got, want := (&CmdTest{}).godot(LoadProject(Cwd()), pkg), NewPath(filepath.Join(dir, "godot")); got != want {
		t.Errorf("godot = %s, want %s", got.ToString(), want.ToString())
	}
}

func TestTestGodotErrors(t *testing.T) {
	cases := map[string]struct {
		c      CmdTest
		engine string
		want   string
	}{
		"no_engine":      {CmdTest{}, "", "Package res://src/pkg has no Godot engine to run its tests, and there's no godot in the PATH. Set one with `gd++ init res://src/pkg --update --engine NAME`, or name a Godot binary with --engine PATH."},
		"missing_engine": {CmdTest{}, "4.7", "Missing Godot engine 4.7, run `gd++ fetch --missing` to fix this."},
		"missing_flag":   {CmdTest{Engine: "4.8"}, "4.7", "Missing Godot engine 4.8, run `gd++ fetch --missing` to fix this."},
		"missing_binary": {CmdTest{Engine: "bin/godot"}, "", "Invalid arguments: res://src/pkg/bin/godot is not a file."},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if os.Getenv("GDPP_FAIL_HELPER") == "1" {
				isTTY = false
				t.Setenv("PATH", "")
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
