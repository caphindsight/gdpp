package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"slices"
	"strconv"
	"strings"
)

// CmdTest builds the packages its paths name, like CmdBuild does for this
// machine in debug mode, and runs their tests in a headless Godot: the binary
// that --engine names, or else an engine dependency, which it compiles the
// first time. Its builds also register the @test classes, which hold the
// tests. A GD++ file's path runs only the tests it declares. Each test shows
// as a task while it runs, then as a line with its result, followed by its
// output if it failed.
type CmdTest struct {
	Paths   []string `arg:"positional" placeholder:"PATH" help:"run the tests of the packages containing these paths, or only those of these GD++ files; PATH/... runs those of all packages inside PATH, e.g. res://... the whole project [default: ..., all packages in the current directory]"`
	Names   []string `arg:"--run" placeholder:"NAME" help:"run only the tests with these names, CLASS.NAME, which may have wildcards, e.g. 'Player.*'"`
	Engine  string   `arg:"--engine" placeholder:"NAME|PATH" help:"run the tests in this Godot engine of the project's cache, or in this Godot binary if it has a path separator, e.g. ./godot [default: the package's engine]"`
	Timeout *float64 `arg:"--timeout" placeholder:"SECONDS" help:"fail an @async test that runs longer than this [default: the package's test_timeout, or 10]"`
	BuildOptions
}

func (c *CmdTest) Run() {
	c.validate()
	Assert(!c.Ship, "Invalid arguments: gd++ test cannot use --ship, since release builds have no tests.")
	Assert(c.Timeout == nil || *c.Timeout > 0, "Invalid arguments: --timeout must be positive.")
	for _, glob := range c.Names {
		_, err := path.Match(glob, "")
		Assert(err == nil, "Invalid arguments: %q is not a valid name pattern.", glob)
	}
	target := buildOption("platform", "", hostPlatform, buildPlatforms) + "." + buildOption("arch", "", hostArch, buildArchs)
	c.assertCompiler(hostPlatform)
	assertScons()
	roots := ParsePackagePaths(c.Paths)
	if len(roots) == 0 {
		LogWarn("Nothing to test.")
		return
	}
	p := LoadProject(roots[0])
	var pkgs []Package
	for _, root := range roots {
		Assert(GetProjectRoot(root) == p.Root, "Invalid arguments: all packages must be in one Godot project.")
		pkgs = append(pkgs, LoadPackage(root))
	}
	files := c.files()
	SyncExportPresets(p)
	LogInfo("Build parameters: %s.", c.describe([]string{target}, true))
	ran, failed := 0, []string(nil)
	for _, pkg := range pkgs {
		pkg.HideInGodot()
		tests := c.selected(buildExtension(p, pkg, c.BuildOptions, []string{target}, true), pkg, files)
		if len(tests) == 0 {
			LogWarn("No tests to run in %s.", pkg.Root.ToString())
			continue
		}
		timeout := pkg.TestTimeout()
		if c.Timeout != nil {
			timeout = *c.Timeout
		}
		r, f := runTests(p, pkg, c.godot(p, pkg), tests, timeout)
		ran, failed = ran+r, append(failed, f...)
	}
	if len(failed) > 0 {
		LogFatal("Failed %d of %s: %s.", len(failed), countTests(ran), strings.Join(failed, ", "))
	}
	if ran > 0 {
		LogInfo("All %s passed.", countTests(ran))
	}
}

// files returns, by package root, the GD++ files among the paths, whose tests
// alone run. Packages that other paths name run all their tests.
func (c *CmdTest) files() map[Path][]Path {
	files, all := map[Path][]Path{}, map[Path]bool{}
	for _, s := range c.Paths {
		if f := ParsePath(s); !strings.HasSuffix(s, "...") && f.IsFile() && slices.Contains(gdppExtensions, path.Ext(f.Name())) {
			root := GetPackageRoot(f)
			files[root] = append(files[root], f)
			continue
		}
		for _, root := range ParsePackagePaths([]string{s}) {
			all[root] = true
		}
	}
	for root := range all {
		delete(files, root)
	}
	return files
}

// selected returns the tests of pkg that run: those of the files that select
// them, if any, and whose names match --run, if given.
func (c *CmdTest) selected(tests []gdppTest, pkg Package, files map[Path][]Path) []gdppTest {
	return slices.DeleteFunc(tests, func(t gdppTest) bool {
		if only, ok := files[pkg.Root]; ok && !slices.Contains(only, t.File.File) {
			return true
		}
		return len(c.Names) > 0 && !slices.ContainsFunc(c.Names, func(glob string) bool {
			ok, _ := path.Match(glob, t.Name)
			return ok
		})
	})
}

// godot returns the Godot binary that runs pkg's tests: the one --engine
// names, or else the engine dependency that --engine or the package names,
// compiled the first time.
func (c *CmdTest) godot(p Project, pkg Package) Path {
	if strings.ContainsAny(c.Engine, `/\`) {
		bin := ParsePath(c.Engine)
		Assert(bin.IsFile(), "Invalid arguments: %s is not a file.", bin.ToString())
		return bin
	}
	name := c.Engine
	if name == "" {
		name = pkg.Config.Engine
		Assert(name != "", "Package %s has no Godot engine to run its tests, set one with `gd++ init %s --update --engine NAME`, or name a Godot binary with --engine PATH.",
			pkg.Root.ToString(), pkg.Root.ToString())
	}
	return compileEngine(p, name, c.Jobs)
}

// testMarker starts the lines that the runner of tests prints for gd++, see
// GDPP_TESTS_CLASS in the runtime: "\x1fgdpp-test WHAT NAME USEC".
const testMarker = "\x1fgdpp-test "

// runTests runs tests, of pkg, in a headless Godot, the binary godot, with
// @async tests timing out after timeout seconds. It logs each test's result
// as it ends, and returns how many tests it ran, and those that failed.
func runTests(p Project, pkg Package, godot Path, tests []gdppTest, timeout float64) (ran int, failed []string) {
	importProject(p, pkg, godot)
	// Editors only run a project's main loop with a script or a scene, so a script runs the package's runner of tests.
	runner := pkg.BuildCache.Cd("tests.gd")
	writeIfChanged(runner, "# Generated by GD++, do not edit. gd++ test runs it.\nextends "+pkg.TestsClass()+"\n")
	args := []string{"--headless", "--path", ".", "--script", "res://" + relPath(p.Root, runner), "--", "--gdpp-timeout", strconv.FormatFloat(timeout, 'f', -1, 64)}
	for _, t := range tests {
		args = append(args, t.Name)
	}
	cmd := exec.Command(godot.GetOsPath(), args...)
	cmd.Dir = p.Root.GetOsPath()
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw // One pipe, so the output stays in order.
	Check(cmd.Start(), "Failed to run %s", godot.ToString())
	go func() {
		cmd.Wait()
		pw.Close()
	}()
	LogInfo("Testing %s.", pkg.Root.ToString())
	r := testReport{timeout: timeout}
	scanner := bufio.NewScanner(pr)
	scanner.Buffer(nil, 1<<20)
	for scanner.Scan() {
		r.line(scanner.Text())
	}
	r.done(pkg, tests)
	return r.ran, r.failed
}

// importProject runs Godot's import of the project's files, if Godot doesn't
// know pkg's library yet: Godot only loads the libraries that
// res://.godot/extension_list.cfg lists, which the editor writes when it scans
// the project.
func importProject(p Project, pkg Package, godot Path) {
	gdextension := strings.TrimSuffix(pkg.ResPath(), "/") + "/" + pkg.Id + ".gdextension"
	list := p.Root.Cd(".godot", "extension_list.cfg")
	listed := func() bool { return list.IsFile() && slices.Contains(strings.Fields(list.ReadString()), gdextension) }
	if listed() {
		return
	}
	t := LogTask("Importing the project's files...")
	cmd := exec.Command(godot.GetOsPath(), "--headless", "--path", ".", "--import")
	cmd.Dir = p.Root.GetOsPath()
	out, _ := cmd.CombinedOutput()
	// Godot may crash as it quits after importing, so only the list counts.
	if !listed() {
		for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
			t.LogString(line)
		}
		t.Fail()
	}
	t.Done()
}

// testReport turns the output of the runner of tests into logs: a task per
// test, which shows the test's output while it runs, and a line with its
// result, followed by its output if it failed.
type testReport struct {
	timeout float64
	task    *Task     // The running test's, or nil.
	silence *Silencer // Erases the running test's task once it ends, for the line with its result.
	name    string    // The running test's.
	output  []string  // The running test's output.
	other   []string  // The output outside of tests.
	started []string  // The tests that started.
	ran     int
	failed  []string // The tests that failed.
}

// line handles a line of the runner's output.
func (r *testReport) line(s string) {
	rest, ok := strings.CutPrefix(s, testMarker)
	if !ok {
		if r.task != nil {
			r.task.LogString(s)
			r.output = append(r.output, s)
		} else {
			r.other = append(r.other, s)
		}
		return
	}
	fields := strings.Fields(rest)
	if len(fields) != 3 {
		return
	}
	what, name := fields[0], fields[1]
	usec, _ := strconv.ParseUint(fields[2], 10, 64)
	if what == "start" || r.task == nil { // Without a start, a test that failed to start.
		r.started = append(r.started, name)
		r.name, r.output, r.silence = name, nil, Silence()
		r.task = LogTask("Running %s...", name)
	}
	switch what {
	case "start":
	case "pass":
		r.end(true, "Passed %s in %s.", name, formatUsec(usec))
	case "timeout":
		r.end(false, "Failed %s: it ran longer than %s seconds.", name, strconv.FormatFloat(r.timeout, 'f', -1, 64))
	case "missing":
		r.end(false, "Failed %s: the library has no such test.", name)
	default:
		r.end(false, "Failed %s in %s.", name, formatUsec(usec))
	}
}

// end ends the running test's task, and logs its result, with its output if it failed.
func (r *testReport) end(passed bool, format string, params ...any) {
	r.task.Done()
	r.silence.End()
	r.task = nil
	r.ran++
	if passed {
		LogInfo(format, params...)
		return
	}
	r.failed = append(r.failed, r.name)
	LogError(format, params...)
	for _, line := range r.output {
		fmt.Fprintln(os.Stderr, taskLogIndent+line)
	}
}

// done handles the end of the runner's output, after the tests of pkg were to
// run. A test that was running when Godot exited failed, and so did those that
// never started.
func (r *testReport) done(pkg Package, tests []gdppTest) {
	if r.task != nil {
		r.end(false, "Failed %s: Godot exited while it ran.", r.name)
	}
	var missed []string
	for _, t := range tests {
		if !slices.Contains(r.started, t.Name) {
			missed = append(missed, t.Name)
		}
	}
	if len(missed) > 0 {
		r.ran += len(missed)
		r.failed = append(r.failed, missed...)
		LogError("Godot exited before it ran %d of the %s of %s, after this output.", len(missed), countTests(len(tests)), pkg.Root.ToString())
		for _, line := range r.other {
			fmt.Fprintln(os.Stderr, taskLogIndent+line)
		}
	}
}

// countTests returns n with the noun "test", e.g. "1 test" or "3 tests".
func countTests(n int) string {
	if n == 1 {
		return "1 test"
	}
	return strconv.Itoa(n) + " tests"
}

// formatUsec returns a duration of usec microseconds for humans, e.g. "0.4 ms" or "1.25 s".
func formatUsec(usec uint64) string {
	if usec < 1000000 {
		return strconv.FormatFloat(float64(usec)/1000, 'f', 1, 64) + " ms"
	}
	return strconv.FormatFloat(float64(usec)/1000000, 'f', 2, 64) + " s"
}
