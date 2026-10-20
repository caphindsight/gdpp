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
	"time"
)

// CmdTest builds the packages its paths name, like CmdBuild does for this
// machine in debug mode, and runs their tests in a headless Godot: the binary
// that --engine names, or else an engine dependency, which it compiles the
// first time, or else the godot in the PATH. Its builds also register the @test classes, which hold the
// tests. A GD++ file's path runs only the tests it declares. Each test shows
// as a task while it runs, then as a line with its result, followed by its
// output if it failed. With --bench, it runs the benchmarks instead, in
// builds optimized for speed by default.
type CmdTest struct {
	Paths   []string `arg:"positional" placeholder:"PATH" help:"run the tests of the packages containing these paths, or only those of these GD++ files; PATH/... runs those of all packages inside PATH, e.g. res://... the whole project [default: ..., all packages in the current directory]"`
	Names   []string `arg:"--run" placeholder:"NAME" help:"run only the tests with these names, CLASS.NAME, which may have wildcards, e.g. 'Player.*'"`
	Engine  string   `arg:"--engine" placeholder:"NAME|PATH" help:"run the tests in this Godot engine of the project's cache, or in this Godot binary if it has a path separator, e.g. ./godot [default: the package's engine, or else godot in the PATH]"`
	Timeout *float64 `arg:"--timeout" placeholder:"SECONDS" help:"fail a test that runs longer than this [default: the package's test_timeout, or 10]"`
	Bench   bool     `arg:"--bench" help:"run the @bench functions, the benchmarks, instead of the tests, built with --opt unless --noopt or --small is given"`
	BuildOptions
}

func (c *CmdTest) Run() {
	c.validate()
	c.Opt = c.Opt || c.Bench && !c.NoOpt && !c.Small // Benchmarks time the code that ships.
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
		pkgs = append(pkgs, c.loadPackage(root))
	}
	files := c.files()
	SyncExportPresets(p)
	LogInfo("Build parameters: %s.", c.describe([]string{target}, true))
	ran, failed := 0, []string(nil)
	for _, pkg := range pkgs {
		pkg.HideInGodot()
		tests := c.selected(gdppTests(buildExtension(p, pkg, c.BuildOptions, []string{target}, true), c.Bench), pkg, files)
		if len(tests) == 0 {
			LogWarn("No %ss to run in %s.", testNoun(c.Bench), pkg.Root.ToString())
			continue
		}
		timeout := pkg.TestTimeout()
		if c.Timeout != nil {
			timeout = *c.Timeout
		}
		r, f := runTests(p, pkg, c.godot(p, pkg), tests, timeout, c.Bench)
		ran, failed = ran+r, append(failed, f...)
	}
	if len(failed) > 0 {
		LogFatal("Failed %d of %s: %s.", len(failed), count(ran, testNoun(c.Bench)), strings.Join(failed, ", "))
	}
	if ran > 0 {
		LogInfo("All %s passed.", count(ran, testNoun(c.Bench)))
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
// compiled the first time, or else the godot in the PATH.
func (c *CmdTest) godot(p Project, pkg Package) Path {
	if strings.ContainsAny(c.Engine, `/\`) {
		bin := ParsePath(c.Engine)
		Assert(bin.IsFile(), "Invalid arguments: %s is not a file.", bin.ToString())
		return bin
	}
	name := c.Engine
	if name == "" {
		name = pkg.Config.Engine
	}
	if name == "" {
		bin, err := exec.LookPath("godot")
		Assert(err == nil, "Package %s has no Godot engine to run its tests, and there's no godot in the PATH. Set one with `gd++ init %s --update --engine NAME`, or name a Godot binary with --engine PATH.",
			pkg.Root.ToString(), pkg.Root.ToString())
		return ParsePath(bin)
	}
	return compileEngine(p, name, c.Jobs)
}

// testMarker starts the lines that the runner of tests prints for gd++, see
// GDPP_TESTS_CLASS in the runtime: "\x1fgdpp-test WHAT NAME USEC".
const testMarker = "\x1fgdpp-test "

// runTests runs tests, of pkg, in a headless Godot, the binary godot, with
// each test timing out after timeout seconds. With bench, they're benchmarks.
// It logs each test's result as it ends, and returns how many tests it ran,
// and those that failed.
func runTests(p Project, pkg Package, godot Path, tests []gdppTest, timeout float64, bench bool) (ran int, failed []string) {
	importProject(p, pkg, godot)
	// Editors only run a project's main loop with a script or a scene, so a script runs the package's runner of tests.
	runner := pkg.BuildCache.Cd("tests.gd")
	writeIfChanged(runner, "# Generated by GD++, do not edit. gd++ test runs it.\nextends "+pkg.TestsClass()+"\n")
	if bench {
		LogInfo("Benchmarking %s.", pkg.Root.ToString())
	} else {
		LogInfo("Testing %s.", pkg.Root.ToString())
	}
	r := testReport{timeout: timeout, bench: bench}
	// A test that times out may block Godot's main thread, so gd++ kills Godot, and runs the other tests in a new one.
	for left := tests; len(left) > 0 && r.run(godot, p.Root, runner, left); {
		left = slices.DeleteFunc(slices.Clone(left), func(t gdppTest) bool { return slices.Contains(r.started, t.Name) })
	}
	r.done(pkg, tests)
	return r.ran, r.failed
}

// run runs tests in a headless Godot, the binary godot, in the project at
// root, with the script runner. It reports whether it killed Godot, since a
// test timed out.
func (r *testReport) run(godot, root, runner Path, tests []gdppTest) (killed bool) {
	args := []string{"--headless", "--path", ".", "--script", "res://" + relPath(root, runner), "--"}
	if r.bench {
		args = append(args, "--bench")
	}
	for _, t := range tests {
		args = append(args, t.Name)
	}
	cmd := exec.Command(godot.GetOsPath(), args...)
	cmd.Dir = root.GetOsPath()
	return r.watch(cmd, godot)
}

// watch starts cmd, a runner of tests in the Godot binary godot, and handles
// its output, until it exits, or until a test runs longer than the timeout:
// then it kills cmd, and reports true.
func (r *testReport) watch(cmd *exec.Cmd, godot Path) (killed bool) {
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw // One pipe, so the output stays in order.
	Check(cmd.Start(), "Failed to run %s", godot.ToString())
	go func() {
		cmd.Wait()
		pw.Close()
	}()
	lines := make(chan string)
	go func() {
		scanner := bufio.NewScanner(pr)
		scanner.Buffer(nil, 1<<20)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	running := 0                 // len(r.started) when the running test started.
	var expired <-chan time.Time // Fires when the running test times out.
	for {
		select {
		case s, ok := <-lines:
			if !ok {
				// The test may have passed just in time, in the output that was left.
				if killed && r.task != nil {
					r.end(false, "Failed %s: it ran longer than %s seconds.", r.name, strconv.FormatFloat(r.timeout, 'f', -1, 64))
				}
				return killed
			}
			r.line(s)
			if r.task != nil && running != len(r.started) {
				running, expired = len(r.started), time.After(time.Duration(r.timeout*float64(time.Second)))
			}
		case <-expired:
			if r.task != nil {
				cmd.Process.Kill()
				killed = true
			}
			expired = nil
		}
	}
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
	bench   bool      // Whether the tests are benchmarks.
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
	if len(fields) != 3 && len(fields) != 4 {
		return
	}
	what, name := fields[0], fields[1]
	usec, _ := strconv.ParseUint(fields[2], 10, 64)
	calls := uint64(0) // A benchmark's, at its end.
	if len(fields) == 4 {
		calls, _ = strconv.ParseUint(fields[3], 10, 64)
	}
	if what == "start" || r.task == nil { // Without a start, a test that failed to start.
		r.started = append(r.started, name)
		r.name, r.output, r.silence = name, nil, Silence()
		r.task = LogTask("Running %s...", name)
	}
	switch what {
	case "start":
	case "pass":
		if calls > 0 {
			r.end(true, "Benched %s: %s per call, %s.", name, formatNsec(float64(usec)*1000/float64(calls)), count(int(calls), "call"))
		} else {
			r.end(true, "Passed %s in %s.", name, formatUsec(usec))
		}
	case "missing":
		r.end(false, "Failed %s: the library has no such %s.", name, testNoun(r.bench))
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
		LogError("Godot exited before it ran %d of the %s of %s, after this output.", len(missed), count(len(tests), testNoun(r.bench)), pkg.Root.ToString())
		for _, line := range r.other {
			fmt.Fprintln(os.Stderr, taskLogIndent+line)
		}
	}
}

// testNoun returns what gd++ test runs: "benchmark" with bench, else "test".
func testNoun(bench bool) string {
	if bench {
		return "benchmark"
	}
	return "test"
}

// count returns n with noun, e.g. "1 test" or "3 tests".
func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// formatNsec returns a duration of nsec nanoseconds for humans, e.g. "12.3 ns", "4.56 µs", or else like formatUsec.
func formatNsec(nsec float64) string {
	switch {
	case nsec < 1000:
		return strconv.FormatFloat(nsec, 'f', 1, 64) + " ns"
	case nsec < 1000000:
		return strconv.FormatFloat(nsec/1000, 'f', 2, 64) + " µs"
	}
	return formatUsec(uint64(nsec / 1000))
}

// formatUsec returns a duration of usec microseconds for humans, e.g. "0.4 ms" or "1.25 s".
func formatUsec(usec uint64) string {
	if usec < 1000000 {
		return strconv.FormatFloat(float64(usec)/1000, 'f', 1, 64) + " ms"
	}
	return strconv.FormatFloat(float64(usec)/1000000, 'f', 2, 64) + " s"
}
