// build.go: building packages: build options, GDExtension builds, and the
// package build cache.

package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"text/template"

	"gd++/trans"
)

var (
	//go:embed templates/__register_types__.cpp
	registerTypesText     string
	registerTypesTemplate = template.Must(template.New("").Parse(registerTypesText))
	//go:embed templates/SConstruct
	sconstructText     string
	sconstructTemplate = template.Must(template.New("").Parse(sconstructText))
	//go:embed templates/package.gdextension
	gdextensionText     string
	gdextensionTemplate = template.Must(template.New("").Parse(gdextensionText))
)

// BuildOptions are the build options of the build command.
type BuildOptions struct {
	Opt    bool   `arg:"--opt" help:"optimize for speed [default: with --ship]"`
	Small  bool   `arg:"--small" help:"optimize for binary size"`
	NoOpt  bool   `arg:"--noopt" help:"don't optimize [default: without --ship]"`
	Ship   bool   `arg:"--ship" help:"build for release instead of debugging"`
	Jobs   int    `arg:"-j,--jobs" placeholder:"N" help:"run this many compile jobs at once [default: one per CPU core but one]"`
	Doc    bool   `arg:"--doc" help:"compile the documentation of GD++ classes into GDExtension libraries [default: without --ship]"`
	NoDoc  bool   `arg:"--nodoc" help:"don't compile the documentation of GD++ classes [default: with --ship]"`
	NoWarn bool   `arg:"--nowarn" help:"disable C++ warnings, which are errors by default, except in godot-cpp"`
	NoGpu  bool   `arg:"--nogpu" help:"run shaders on the CPU, e.g. to step through them in a debugger"`
	Asan   bool   `arg:"--asan" help:"detect memory errors with AddressSanitizer"`
	Ubsan  bool   `arg:"--ubsan" help:"detect undefined behavior with UndefinedBehaviorSanitizer"`
	Tsan   bool   `arg:"--tsan" help:"detect data races with ThreadSanitizer"`
	CC     string `arg:"--cc" placeholder:"gcc|clang|msvc|clang-cl" help:"compile with this C++ compiler; for Windows, gcc and clang are MinGW-w64's [default: godot-cpp's for the target]"`
	DebugOptions
}

// DebugOptions are the options of the build and trans commands that turn on
// @trace and @profile annotations, and limit how long macros may run.
type DebugOptions struct {
	Trace   []string `arg:"--trace" placeholder:"GROUP" help:"turn on the @trace annotations of these groups, or of all"`
	Profile []string `arg:"--profile" placeholder:"GROUP" help:"turn on the @profile annotations of these groups, or of all"`
	Print   bool     `arg:"--print" help:"with --profile, also print a table of the timings every few seconds while the game runs"`
	Period  *int     `arg:"--period" placeholder:"SECONDS" help:"with --print, the seconds between tables [default: 10]"`
	FPS     int      `arg:"--fps" placeholder:"FPS" help:"with --print, the frame rate that the table's budget column assumes [default: 60]"`
	// Seconds; 0 means the translator's default.
	MacroTimeout int `arg:"--macro-timeout" placeholder:"SECONDS" help:"stop a macro or template invocation that runs longer than this [default: 20]"`
}

// validate asserts the group names are valid.
func (o DebugOptions) validate() {
	for _, g := range slices.Concat(o.Trace, o.Profile) {
		Assert(classNameRegexp.MatchString(g), "Invalid arguments: %q is not a valid group name.", g)
	}
	Assert(!o.Print || len(o.Profile) > 0, "Invalid arguments: --print needs --profile.")
	Assert(o.Period == nil || *o.Period > 0, "Invalid arguments: --period must be positive.")
	Assert(o.Period == nil || o.Print, "Invalid arguments: --period needs --print.")
	Assert(o.FPS >= 0, "Invalid arguments: --fps cannot be negative.")
	Assert(o.FPS == 0 || o.Print, "Invalid arguments: --fps needs --print.")
	Assert(o.MacroTimeout >= 0, "Invalid arguments: --macro-timeout cannot be negative.")
}

// transOptions returns opts with the options' groups, the table's period and
// frame rate if it's printed, and the macros' time limit.
func (o DebugOptions) transOptions(opts trans.Options) trans.Options {
	opts.Trace, opts.Profile, opts.MacroTimeout = o.Trace, o.Profile, o.MacroTimeout
	if o.Print {
		opts.ProfilePeriod, opts.ProfileFPS = 10, o.FPS
		if o.Period != nil {
			opts.ProfilePeriod = *o.Period
		}
	}
	return opts
}

// Build platforms and CPU architectures, each a full name followed by its
// aliases, and the ones of this machine, empty if unsupported.
var (
	buildPlatforms = [][]string{{"windows", "win", "w"}, {"linux", "lin", "l"}, {"macos", "mac", "m"}}
	buildArchs     = [][]string{{"x86_32", "x32"}, {"x86_64", "x64"}, {"arm64", "a64"}}
	hostPlatform   = map[string]string{"linux": "linux", "darwin": "macos", "windows": "windows"}[runtime.GOOS]
	hostArch       = map[string]string{"386": "x86_32", "amd64": "x86_64", "arm64": "arm64"}[runtime.GOARCH]
)

// validate asserts the options make sense together.
func (o BuildOptions) validate() {
	Assert(countTrue(o.Opt, o.Small, o.NoOpt) <= 1, "Invalid arguments: --opt, --small and --noopt cannot be used together.")
	Assert(!o.Doc || !o.NoDoc, "Invalid arguments: --doc and --nodoc cannot be used together.")
	Assert(!o.Asan || !o.Tsan, "Invalid arguments: --asan and --tsan cannot be used together.")
	Assert(o.Jobs >= 0, "Invalid arguments: --jobs cannot be negative.")
	Assert(o.CC == "" || compilers[o.CC] != nil, "Invalid arguments: --cc must be one of gcc, clang, msvc, clang-cl.")
	o.DebugOptions.validate()
}

// buildOption returns the full name of the value of the --flag option,
// asserting it's one of names, or the detected one if the option isn't given.
func buildOption(flag, value, detected string, names [][]string) string {
	if value == "" {
		Assert(detected != "", "Failed to detect the %s, use --%s to choose one.", flag, flag)
		return detected
	}
	full := fullName(names, value)
	if full == "" {
		var allowed []string
		for _, n := range names {
			allowed = append(allowed, n[0])
		}
		LogFatal("Invalid arguments: --%s must be one of %s.", flag, strings.Join(allowed, ", "))
	}
	return full
}

// fullName returns the full name that s is or abbreviates, or "" if none.
func fullName(names [][]string, s string) string {
	for _, n := range names {
		if slices.Contains(n, s) {
			return n[0]
		}
	}
	return ""
}

// compilers maps each --cc compiler to the platforms it builds for, and the
// godot-cpp options that choose it there.
var compilers = map[string]map[string][]string{
	"gcc":      {"linux": {"use_llvm=no"}, "windows": {"use_mingw=yes", "use_llvm=no"}},
	"clang":    {"linux": {"use_llvm=yes"}, "windows": {"use_mingw=yes", "use_llvm=yes"}, "macos": nil},
	"msvc":     {"windows": {"use_mingw=no", "use_llvm=no"}},
	"clang-cl": {"windows": {"use_mingw=no", "use_llvm=yes"}},
}

// assertCompiler asserts the --cc compiler, if any, can build for platform on this machine.
func (o BuildOptions) assertCompiler(platform string) {
	if o.CC == "" {
		return
	}
	_, ok := compilers[o.CC][platform]
	Assert(ok, "Invalid arguments: --cc %s cannot build for %s.", o.CC, platform)
	// MSVC only runs on Windows, and there godot-cpp's MinGW-w64 is always GCC.
	msvc := o.CC == "msvc" || o.CC == "clang-cl"
	Assert(platform != "windows" || o.CC == "gcc" || msvc == (hostPlatform == "windows"),
		"Invalid arguments: --cc %s cannot build for windows on this machine.", o.CC)
}

// assertScons asserts SCons is installed.
func assertScons() {
	_, err := exec.LookPath("scons")
	Check(err, "Failed to find SCons, see https://scons.org to install it")
}

// sconsArgs returns the godot-cpp build options for target.
func (o BuildOptions) sconsArgs(target string) []string {
	platform, arch, _ := strings.Cut(target, ".")
	args := []string{"platform=" + platform, "arch=" + arch}
	if o.Ship {
		args = append(args, "target=template_release", "lto=auto")
	} else {
		args = append(args, "target=template_debug", "dev_build=yes", "use_hot_reload=yes")
	}
	args = append(args, "optimize="+o.optimize())
	args = append(args, compilers[o.CC][platform]...)
	if o.NoWarn {
		args = append(args, "--gdpp-nowarn")
	}
	if o.NoGpu {
		args = append(args, "--gdpp-nogpu")
	}
	if s := o.sanitizers(); len(s) > 0 {
		args = append(args, "--gdpp-sanitize="+strings.Join(s, ","))
	}
	if o.Jobs > 0 {
		args = append(args, fmt.Sprintf("-j%d", o.Jobs))
	}
	return args
}

// docs reports whether to compile the documentation of GD++ classes: by
// default only into debug builds, since only the editor shows it.
func (o BuildOptions) docs() bool {
	return o.Doc || !o.NoDoc && !o.Ship
}

// sanitizers returns the names of the chosen sanitizers, as -fsanitize takes them.
func (o BuildOptions) sanitizers() []string {
	var s []string
	if o.Asan {
		s = append(s, "address")
	}
	if o.Ubsan {
		s = append(s, "undefined")
	}
	if o.Tsan {
		s = append(s, "thread")
	}
	return s
}

// optimize returns the SCons optimize option: the chosen one, or by default
// speed for release builds, and none for debug builds.
func (o BuildOptions) optimize() string {
	switch {
	case o.Small:
		return "size"
	case o.NoOpt:
		return "none"
	case o.Opt || o.Ship:
		return "speed"
	}
	return "none"
}

// describe returns the build parameters, e.g. "windows.x86_64, release,
// optimized", with non-default values in magenta. gdpp adds whether docs are
// built, which only exist for GD++ classes.
func (o BuildOptions) describe(targets []string, gdpp bool) string {
	var styled []string
	for _, t := range targets {
		if t != hostPlatform+"."+hostArch {
			t = Styled(t, Magenta)
		}
		styled = append(styled, t)
	}
	desc := strings.Join(styled, " ")
	if o.Ship {
		desc += ", " + Styled("release", Magenta)
	} else {
		desc += ", debug"
	}
	defaults := BuildOptions{Ship: o.Ship}
	opt := map[string]string{"speed": "optimized", "size": "size-optimized", "none": "unoptimized"}[o.optimize()]
	if o.optimize() != defaults.optimize() {
		opt = Styled(opt, Magenta)
	}
	desc += ", " + opt
	if o.CC != "" {
		desc += ", " + Styled(o.CC, Magenta)
	}
	if o.NoWarn {
		desc += ", " + Styled("no warnings", Magenta)
	}
	for _, s := range []struct {
		on   bool
		name string
	}{{o.Asan, "asan"}, {o.Ubsan, "ubsan"}, {o.Tsan, "tsan"}} {
		if s.on {
			desc += ", " + Styled(s.name, Magenta)
		}
	}
	if gdpp {
		docs := map[bool]string{true: "with docs", false: "no docs"}[o.docs()]
		if o.docs() != defaults.docs() {
			docs = Styled(docs, Magenta)
		}
		desc += ", " + docs
		if len(o.Trace) > 0 {
			desc += ", " + Styled("trace "+strings.Join(o.Trace, " "), Magenta)
		}
		if len(o.Profile) > 0 {
			desc += ", " + Styled("profiling", Magenta)
		}
		if o.NoGpu {
			desc += ", " + Styled("shaders on the CPU", Magenta)
		}
	}
	return desc
}

// preparePackage syncs the package's build cache and transpiles its GD++
// files. It returns them and the classes they declare. Bindings are generated
// with SCons arguments bindArgs, if needed.
func preparePackage(p Project, pkg Package, bindArgs []string, o BuildOptions) ([]gdppFile, []gdppClass) {
	generateBuildCache(p, pkg)
	files := listGdppFiles(p, pkg, o.MacroTimeout)
	syncSources(p, pkg, files)
	if len(files) == 0 {
		return nil, nil
	}
	return files, transpilePackage(pkg, files, bindingNames(pkg, bindArgs), o)
}

// buildExtension compiles the package into GDExtension libraries for
// targets, and generates its .gdextension file. With testing, for gd++ test,
// the libraries also register the @test classes and the runner of tests. It
// returns the package's tests.
func buildExtension(p Project, pkg Package, o BuildOptions, targets []string, testing bool) []gdppTest {
	// The first build's arguments, so the full build finds the generated bindings up to date.
	files, classes := preparePackage(p, pkg, o.sconsArgs(targets[0]), o)
	tests := gdppTests(files)
	generateRegisterTypes(pkg, classes, testing)
	for _, target := range targets {
		// With several targets, the task names the one it builds.
		name := pkg.Root.ToString()
		if len(targets) > 1 {
			name += " for " + target
		}
		Exec("Building "+name+"...", pkg.BuildCache, "scons", o.sconsArgs(target)...)
	}
	generateGdextension(pkg, classes)
	return tests
}

// generateBuildCache syncs the package's bindings and API spec into its build
// cache, and writes the generated files there. The cache's build.toml records
// the package's id and config, except classes, which only affect the generated
// files, and hidden directories, which don't affect the build; if they changed, e.g. because the package was
// moved or its dependencies changed, the package is cleaned like `gd++ clean
// --bin` does and synced again.
func generateBuildCache(p Project, pkg Package) {
	dep := func(cache ProjectDepCache, name string) Path {
		Assert(cache.Has(name), "Missing %s %s, run `gd++ fetch --missing` to fix this.", cache.Desc, name)
		return cache.GetPath(name)
	}
	bind, spec := dep(p.Caches[0], pkg.Config.Bindings), dep(p.Caches[1], pkg.Config.ApiSpec)
	name := pkg.Root.ToString()
	cache := pkg.BuildCache
	state := cache.Cd("build.toml")
	config := pkg.Config
	// Names only affect the generated files, and the engine and the test timeout only run tests.
	config.Classes, config.Hidden, config.Names, config.Engine, config.TestTimeout = nil, nil, ProjectNames{}, "", nil
	stateText := encodeToml(struct {
		Id     string        `toml:"id"`
		Config PackageConfig `toml:"config"`
	}{pkg.Id, config})
	if !state.IsFile() || state.ReadString() != stateText {
		// Cleaned first, so an interrupted sync isn't taken for a finished one.
		cleanPackage(pkg.Root, true)
		cache.CreateDirectory()
		s := Silence()
		t := LogTask("Syncing dependencies for %s...", name)
		bind.Sync(cache.Cd("godot-cpp"))
		spec.Cd("extension_api.json").Sync(cache.Cd("extension_api.json"))
		t.Done()
		s.End()
		state.WriteString(stateText)
	}

	writeTemplate(cache.Cd("SConstruct"), sconstructTemplate, map[string]any{
		"Id":              pkg.Id,
		"CppStandard":     pkg.Config.CppStandard,
		"Color":           isTTY,
		"ProjectRoot":     relPath(cache, p.Root),
		"Sources":         cppSources(p, pkg),
		"AsyncClass":      pkg.AsyncClass(),
		"TestsClass":      pkg.TestsClass(),
		"GpuArrayClass":   pkg.GpuArrayClass(),
		"GpuTextureClass": pkg.GpuTextureClass(),
		"QuitTimeout":     int64(math.Round(pkg.QuitTimeout() * 1e6)),
	})
}

// relPath returns the path of to relative to from, with forward slashes.
func relPath(from, to Path) string {
	rel, err := filepath.Rel(from.GetOsPath(), to.GetOsPath())
	Check(err, "Failed to compute a relative path")
	return filepath.ToSlash(rel)
}

// cppSources returns the paths of the package's C and C++ sources, relative
// to its root.
func cppSources(p Project, pkg Package) []string {
	return packageFiles(p, pkg, ".c", ".cc", ".cpp", ".cxx", ".c++")
}

// sourcesDirName is the build cache directory that holds copies of the
// package's GD++ and C/C++ files, which builds compile instead of the files
// themselves, so compiler errors show the code that was compiled, even if the
// files are edited during the build.
const sourcesDirName = "package"

// syncSources syncs the build cache's copies of the package's GD++ files
// (their text as read for transpiling) and C/C++ sources and headers, and
// deletes the copies of files that are gone.
func syncSources(p Project, pkg Package, files []gdppFile) {
	dir := pkg.BuildCache.Cd(sourcesDirName)
	keep := map[string]bool{}
	for _, f := range files {
		keep[f.Rel] = true
		dir.Cd(f.Rel).CreateParentDirectory()
		writeIfChanged(dir.Cd(f.Rel), f.Src)
	}
	for _, rel := range slices.Concat(cppSources(p, pkg), packageFiles(p, pkg, ".h", ".hh", ".hpp", ".hxx", ".h++", ".inl")) {
		keep[rel] = true
		dir.Cd(rel).CreateParentDirectory()
		pkg.Root.Cd(rel).Sync(dir.Cd(rel))
	}
	removeStale(dir, "", keep)
}

// generateRegisterTypes writes the build cache's __register_types__.cpp,
// which registers the package's classes: those in its config, and the GD++
// classes, which must not clash with them, and the class of tasks, if a GD++
// class uses Async. With GD++ classes, it unloads them with the runtime's
// gdpp::uninitialize. With testing, for gd++ test, it registers the @test
// classes too, and the package's runner of tests. Runtime classes don't run their code in the editor:
// classes without tool or abstract, unless they extend a class with one of
// those, see nonRuntimeClasses. It creates the object of each @singleton
// class, which Godot knows by the class's name.
func generateRegisterTypes(pkg Package, gdpp []gdppClass, testing bool) {
	var decls []trans.Declaration
	for _, class := range gdpp {
		decls = append(decls, class.Declaration)
	}
	nonRuntime := nonRuntimeClasses(pkg, decls)
	var classes, runtime, abstract, includes, singletons, editor, plugins []string
	add := func(name string, isAbstract bool) {
		classes = append(classes, name)
		if !nonRuntime[name] {
			runtime = append(runtime, name)
		}
		if isAbstract {
			abstract = append(abstract, name)
		}
	}
	for _, class := range pkg.Config.Classes {
		Assert(!nonRuntime[class.Name] || class.Tool || class.Abstract,
			"Class %s extends %s, which isn't a runtime class, so it needs tool or abstract too: Godot doesn't let runtime classes extend it. Set one with e.g. `gd++ init %s --class %s --update --tool`.",
			class.Name, cppClassBase(pkg, class), pkg.Root.ToString(), class.Name)
		add(class.Name, class.Abstract)
		if class.Include != "" {
			includes = append(includes, classInclude(class.Include))
		}
	}
	// Packages with GD++ classes include the runtime, which unloads their code;
	// GD++ adds the class of tasks, which Async types name, to those whose GD++
	// classes use Async.
	asyncClass, runtimeName, gpuRuntimeName := "", "", ""
	if len(gdpp) > 0 {
		var err error
		runtimeName, _, err = trans.RuntimeHeader(pkg.Config.Syntax)
		Check(err, "Failed to find the GD++ runtime header")
	}
	// The classes that GD++ adds, by name, with what they are for.
	added := map[string]string{}
	if slices.ContainsFunc(gdpp, func(c gdppClass) bool { return c.Async }) {
		asyncClass = pkg.AsyncClass()
		added[asyncClass] = "Async types"
	}
	added[pkg.TestsClass()] = "running tests" // Even without testing, so that gd++ test doesn't break the build.
	if slices.ContainsFunc(gdpp, func(c gdppClass) bool { return c.Gpu }) {
		var err error
		gpuRuntimeName, _, err = trans.GpuRuntimeHeader(pkg.Config.Syntax)
		Check(err, "Failed to find the GD++ runtime header of shaders")
		added[pkg.GpuArrayClass()] = "GpuArray types"
		added[pkg.GpuTextureClass()] = "the textures that shaders create"
	}
	for name, what := range added {
		Assert(!slices.Contains(classes, name), "Class %s is declared in %s, but GD++ adds a class of that name for %s. Set another prefix with `gd++ init %s --prefix NAME`.",
			name, pkg.Root.Cd(packageFileName).ToString(), what, pkg.Root.ToString())
	}
	for _, class := range gdpp {
		what, clash := added[class.Name]
		Assert(!clash, "Class %s is declared in %s, but GD++ adds a class of that name for %s. Set another prefix with `gd++ init %s --prefix NAME`.",
			class.Name, class.File.File.ToString(), what, pkg.Root.ToString())
		Assert(!slices.Contains(classes, class.Name), "Class %s is declared in %s and in %s.",
			class.Name, class.File.File.ToString(), pkg.Root.Cd(packageFileName).ToString())
		if class.Test && !testing {
			continue // Builds compile @test classes, but only gd++ test registers them.
		}
		add(class.Name, class.Abstract)
		includes = append(includes, `"`+class.Name+`.h"`)
		if class.Singleton {
			singletons = append(singletons, class.Name)
		}
	}
	// Classes that extend the editor's, which only exist in the editor, register with them, at the editor's level.
	// EditorPlugins add the plugins among them to the editor.
	bases := map[string]string{}
	for _, class := range pkg.Config.Classes {
		bases[class.Name] = cppClassBase(pkg, class)
	}
	for _, d := range decls {
		bases[d.Name] = d.Base
	}
	spec := readSpec(pkg.BuildCache.Cd("extension_api.json"))
	for _, name := range classes {
		base := name
		for depth := 0; bases[base] != "" && depth <= len(bases); depth++ { // depth stops at cycles, which other checks report.
			if base = bases[base]; base == "EditorPlugin" && !slices.Contains(abstract, name) {
				plugins = append(plugins, name)
			}
		}
		if spec.editor[base] {
			editor = append(editor, name)
		}
	}
	if writeTemplate(pkg.BuildCache.Cd("__register_types__.cpp"), registerTypesTemplate, map[string]any{
		"Classes":         classes,
		"RuntimeClasses":  runtime,
		"AbstractClasses": abstract,
		"Includes":        uniqueSorted(includes, strings.Compare),
		"AsyncClass":      asyncClass,
		"Runtime":         runtimeName,
		"GpuRuntime":      gpuRuntimeName,
		"Singletons":      singletons,
		"SceneClasses":    slices.DeleteFunc(slices.Clone(classes), func(c string) bool { return slices.Contains(editor, c) }),
		"EditorClasses":   editor,
		"EditorPlugins":   plugins,
		"Testing":         testing,
	}) {
		LogInfo("Registering classes for %s...", pkg.Root.ToString())
	}
}

// packageFiles returns the paths of the package's files with the given
// extensions, relative to its root. Like packageRootsIn, it skips hidden
// directories, res://_gd++ and nested Godot projects, and also nested
// packages, whose files are their own.
func packageFiles(p Project, pkg Package, exts ...string) []string {
	var sources []string
	var walk func(dir Path, rel string)
	walk = func(dir Path, rel string) {
		for _, child := range dir.Ls() {
			childRel := path.Join(rel, child.Name())
			switch {
			case child.IsDir():
				if !child.IsPackageRoot() && !child.IsProjectRoot() && child != p.Root.Cd(checkedInDepsDirName) {
					walk(child, childRel)
				}
			case slices.Contains(exts, path.Ext(childRel)):
				sources = append(sources, childRel)
			}
		}
	}
	walk(pkg.Root, "")
	return sources
}

// apiSpecVersion returns the Godot version of the API spec in the package's
// build cache.
func apiSpecVersion(pkg Package) (major, minor int) {
	var spec struct {
		Header struct {
			Major int `json:"version_major"`
			Minor int `json:"version_minor"`
		} `json:"header"`
	}
	specFile := pkg.BuildCache.Cd("extension_api.json")
	Check(json.Unmarshal([]byte(specFile.ReadString()), &spec), "Failed to parse %s", specFile.ToString())
	Assert(spec.Header.Major > 0, "Failed to find the Godot version in %s.", specFile.ToString())
	return spec.Header.Major, spec.Header.Minor
}

// generateGdextension writes the package's .gdextension file, which points
// Godot to its libraries for all targets and to its class icons, and the
// .uid file next to it, with a UID derived from the package name, so Godot
// doesn't generate one. The minimum Godot version is the API spec's.
func generateGdextension(pkg Package, gdpp []gdppClass) {
	major, minor := apiSpecVersion(pkg)
	libs := map[string]string{}
	for _, platform := range buildPlatforms {
		for _, arch := range buildArchs {
			if platform[0] == "macos" && arch[0] == "x86_32" {
				continue // unsupported by godot-cpp
			}
			ext := map[string]string{"windows": "dll", "linux": "so", "macos": "dylib"}[platform[0]]
			for _, target := range []string{"debug", "release"} {
				// Named like SConstruct names them.
				lib := fmt.Sprintf("lib%s.%s.%s.%s.%s", pkg.Id, platform[0], target, arch[0], ext)
				libs[platform[0]+"."+target+"."+arch[0]] = pkg.Root.Cd(lib).ToString()
			}
		}
	}
	icons := map[string]string{}
	for _, class := range pkg.Config.Classes {
		if class.Icon != "" {
			icons[class.Name] = pkg.ClassPath(class.Icon).ToString()
		}
	}
	for _, class := range gdpp {
		if class.Icon != "" {
			icons[class.Name] = pkg.ClassPath(class.Icon).ToString()
		}
	}
	file := pkg.Root.Cd(pkg.Id + ".gdextension")
	var text strings.Builder
	Check(gdextensionTemplate.Execute(&text, map[string]any{
		"GodotVersion": fmt.Sprintf("%d.%d", major, minor),
		"Libraries":    libs,
		"Icons":        icons,
		"Reloadable":   pkg.HotReload(),
	}), "Failed to generate %s", file.ToString())
	changed := !file.IsFile() || file.ReadString() != text.String()
	// Written even if unchanged: its new modification time makes the Godot
	// editor reload the extension, when the editor window gets focus.
	file.WriteString(text.String())
	if writeIfChanged(pkg.Root.Cd(pkg.Id+".gdextension.uid"), godotUid(packageName(pkg.Root))+"\n") || changed {
		LogInfo("Generating .gdextension for %s...", pkg.Root.ToString())
	}
}

// godotUid returns a Godot resource UID derived from the hash of s, in
// Godot's text format: "uid://" and the non-negative 64-bit id in base 34,
// with digits a-y then 0-8.
func godotUid(s string) string {
	const digits = "abcdefghijklmnopqrstuvwxy012345678"
	sum := sha256.Sum256([]byte(s))
	id := binary.BigEndian.Uint64(sum[:]) >> 1
	text := ""
	for {
		text = string(digits[id%34]) + text
		if id /= 34; id == 0 {
			return "uid://" + text
		}
	}
}

// writeTemplate writes the template executed with data to file, like
// writeIfChanged, and returns whether it did.
func writeTemplate(file Path, t *template.Template, data any) bool {
	var b strings.Builder
	Check(t.Execute(&b, data), "Failed to generate %s", file.ToString())
	return writeIfChanged(file, b.String())
}

// writeIfChanged writes text to file, unless file already holds it, so
// tools watching file (e.g. the Godot editor) don't see needless changes.
// Returns whether it wrote.
func writeIfChanged(file Path, text string) bool {
	if file.IsFile() && file.ReadString() == text {
		return false
	}
	file.WriteString(text)
	return true
}
