package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"text/template"
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

// CmdBuild compiles a package, or with --proj every package in the project,
// into a GDExtension library with SCons. Each package's build cache gets a
// copy of its bindings and API spec, a generated __register_types__.cpp
// registering its classes, and a generated SConstruct. After building, the
// package root gets a generated <id>.gdextension file and its .uid file.
// Debug builds are godot-cpp dev builds with debug symbols and hot reload;
// release builds use link-time optimization.
type CmdBuild struct {
	Path     string   `arg:"positional" help:"build the package containing this path [default: the current directory]"`
	For      []string `arg:"--for" placeholder:"PLATFORM.ARCH" help:"build for each of these targets, e.g. windows.x86_64 or w.x64; platforms: windows|win|w, linux|lin|l, macos|mac|m; archs: x86_32|x32, x86_64|x64, arm64|a64 [default: this machine]"`
	Platform string   `arg:"-p,--platform" placeholder:"windows|linux|macos" help:"the target platform [default: this one]"`
	Windows  bool     `arg:"-w" help:"shorthand for --platform=windows"`
	Arch     string   `arg:"--arch" placeholder:"x86_32|x86_64|arm64" help:"the target CPU architecture [default: this one]"`
	Opt      bool     `arg:"--opt" help:"optimize for speed [default: with --ship]"`
	Small    bool     `arg:"--small" help:"optimize for binary size"`
	NoOpt    bool     `arg:"--noopt" help:"don't optimize [default: without --ship]"`
	Ship     bool     `arg:"--ship" help:"build a release library instead of a debug one"`
	Jobs     int      `arg:"-j,--jobs" placeholder:"N" help:"run this many compile jobs at once [default: one per CPU core but one]"`
	Proj     bool     `arg:"--proj" help:"build all packages in the project, one after another"`
	Doc      bool     `arg:"--doc" help:"compile the documentation of GD++ classes into the library [default: without --ship]"`
	NoDoc    bool     `arg:"--nodoc" help:"don't compile the documentation of GD++ classes [default: with --ship]"`
}

// Build platforms and CPU architectures, each a full name followed by its
// aliases, and the ones of this machine, empty if unsupported.
var (
	buildPlatforms = [][]string{{"windows", "win", "w"}, {"linux", "lin", "l"}, {"macos", "mac", "m"}}
	buildArchs     = [][]string{{"x86_32", "x32"}, {"x86_64", "x64"}, {"arm64", "a64"}}
	hostPlatform   = map[string]string{"linux": "linux", "darwin": "macos", "windows": "windows"}[runtime.GOOS]
	hostArch       = map[string]string{"386": "x86_32", "amd64": "x86_64", "arm64": "arm64"}[runtime.GOARCH]
)

func (c *CmdBuild) Run() {
	targets := c.targets()
	_, err := exec.LookPath("scons")
	Check(err, "Failed to find SCons, see https://scons.org to install it")
	path := Cwd()
	if c.Path != "" {
		path = ParsePath(c.Path)
	}
	p := LoadProject(path)
	pkgs := []Package{}
	if c.Proj {
		pkgs = p.ListPackages()
		if len(pkgs) == 0 {
			LogWarn("Nothing to build.")
			return
		}
	} else {
		pkgs = append(pkgs, LoadPackage(path))
	}
	for _, pkg := range pkgs {
		generateBuildCache(p, pkg)
		var classes []gdppClass
		files := listGdppFiles(p, pkg)
		if len(files) > 0 {
			// The first build's arguments, so the full build finds the generated bindings up to date.
			names := loadGodotNames(pkg, func() {
				s := Silence()
				Exec("Compiling bindings for "+styledPackageName(pkg.Root)+"...", pkg.BuildCache, "scons", append(c.sconsArgs(targets[0]), "--gdpp-bindings")...)
				s.End()
			})
			classes = transpilePackage(pkg, files, names, c.docs())
		}
		generateRegisterTypes(pkg, classes)
		for _, target := range targets {
			Exec("Building "+styledPackageName(pkg.Root)+" for "+c.describe(target, len(files) > 0)+"...", pkg.BuildCache, "scons", c.sconsArgs(target)...)
		}
		generateGdextension(pkg, classes)
	}
}

// targets asserts the arguments make sense together, and returns the
// PLATFORM.ARCH targets to build for, with full names.
func (c *CmdBuild) targets() []string {
	Assert(!c.Windows || c.Platform == "", "Invalid arguments: -w and --platform cannot be used together.")
	Assert(len(c.For) == 0 || !c.Windows && c.Platform == "" && c.Arch == "", "Invalid arguments: --for cannot be used together with -w, --platform or --arch.")
	Assert(countTrue(c.Opt, c.Small, c.NoOpt) <= 1, "Invalid arguments: --opt, --small and --noopt cannot be used together.")
	Assert(!c.Proj || c.Path == "", "Invalid arguments: a path and --proj cannot be used together.")
	Assert(!c.Doc || !c.NoDoc, "Invalid arguments: --doc and --nodoc cannot be used together.")
	Assert(c.Jobs >= 0, "Invalid arguments: --jobs cannot be negative.")
	if len(c.For) == 0 {
		if c.Windows {
			c.Platform = "windows"
		}
		return []string{buildOption("platform", c.Platform, hostPlatform, buildPlatforms) + "." + buildOption("arch", c.Arch, hostArch, buildArchs)}
	}
	var targets []string
	for _, s := range c.For {
		platform, arch, _ := strings.Cut(s, ".")
		platform, arch = fullName(buildPlatforms, platform), fullName(buildArchs, arch)
		Assert(platform != "" && arch != "", "Invalid arguments: %q is not a valid target, use PLATFORM.ARCH, e.g. windows.x86_64 or w.x64.", s)
		if target := platform + "." + arch; !slices.Contains(targets, target) {
			targets = append(targets, target)
		}
	}
	return targets
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

// sconsArgs returns the godot-cpp build options for target.
func (c *CmdBuild) sconsArgs(target string) []string {
	platform, arch, _ := strings.Cut(target, ".")
	args := []string{"platform=" + platform, "arch=" + arch}
	if c.Ship {
		args = append(args, "target=template_release", "lto=auto")
	} else {
		args = append(args, "target=template_debug", "dev_build=yes", "use_hot_reload=yes")
	}
	args = append(args, "optimize="+c.optimize())
	if c.Jobs > 0 {
		args = append(args, fmt.Sprintf("-j%d", c.Jobs))
	}
	return args
}

// docs reports whether to compile the documentation of GD++ classes: by
// default only into debug builds, since only the editor shows it.
func (c *CmdBuild) docs() bool {
	return c.Doc || !c.NoDoc && !c.Ship
}

// optimize returns the godot-cpp optimize option: the chosen one, or by
// default speed for release builds and none for debug builds.
func (c *CmdBuild) optimize() string {
	switch {
	case c.Small:
		return "size"
	case c.NoOpt:
		return "none"
	case c.Opt || c.Ship:
		return "speed"
	}
	return "none"
}

// describe returns the build details for target shown in the task name, e.g.
// "windows.x86_64, release, optimized", with non-default values bold.
func (c *CmdBuild) describe(target string, gdpp bool) string {
	desc := target
	if target != hostPlatform+"."+hostArch {
		desc = Styled(target, Bold)
	}
	if c.Ship {
		desc += ", " + Styled("release", Bold)
	} else {
		desc += ", debug"
	}
	opt := map[string]string{"speed": "optimized", "size": "size-optimized", "none": "unoptimized"}[c.optimize()]
	if c.optimize() != (&CmdBuild{Ship: c.Ship}).optimize() {
		opt = Styled(opt, Bold)
	}
	desc += ", " + opt
	if gdpp {
		// Docs only exist for GD++ classes.
		docs := map[bool]string{true: "docs", false: "no docs"}[c.docs()]
		if c.docs() != (&CmdBuild{Ship: c.Ship}).docs() {
			docs = Styled(docs, Bold)
		}
		desc += ", " + docs
	}
	return desc
}

// generateBuildCache syncs the package's bindings and API spec into its build
// cache, and writes the generated files there. The cache's build.toml records
// the package's id and config, except classes, which only affect the generated
// files; if they changed, e.g. because the package was
// moved or its dependencies changed, the package is cleaned like `gd++ clean
// --bin` does and synced again.
func generateBuildCache(p Project, pkg Package) {
	dep := func(cache ProjectDepCache, name string) Path {
		Assert(cache.Has(name), "Missing %s %s, run `gd++ fetch --missing` to fix this.", cache.Desc, name)
		return cache.GetPath(name)
	}
	bind, spec := dep(p.Caches[0], pkg.Config.Bindings), dep(p.Caches[1], pkg.Config.ApiSpec)
	name := styledPackageName(pkg.Root)
	cache := pkg.BuildCache
	state := cache.Cd("build.toml")
	config := pkg.Config
	config.Classes = nil
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

	projectRoot, err := filepath.Rel(cache.GetOsPath(), p.Root.GetOsPath())
	Check(err, "Failed to compute a relative path")
	writeTemplate(cache.Cd("SConstruct"), sconstructTemplate, map[string]any{
		"Id":          pkg.Id,
		"CppStandard": pkg.Config.CppStandard,
		"Color":       isTTY,
		"ProjectRoot": filepath.ToSlash(projectRoot),
		"Sources":     packageFiles(p, pkg, ".c", ".cc", ".cpp", ".cxx", ".c++"),
		"GdppSources": gdppSources(p, pkg),
	})
}

// gdppSources returns the paths of the C++ sources generated from the
// package's GD++ files, relative to the build cache's gdpp directory.
func gdppSources(p Project, pkg Package) []string {
	var sources []string
	for _, rel := range packageFiles(p, pkg, gdppExtensions...) {
		sources = append(sources, rel+".cpp")
	}
	return sources
}

// generateRegisterTypes writes the build cache's __register_types__.cpp,
// which registers the package's classes: those in its config, and the GD++
// classes, which must not clash with them. GD++ classes without @tool are
// runtime classes, whose code doesn't run in the editor.
func generateRegisterTypes(pkg Package, gdpp []gdppClass) {
	var classes, runtime, includes []string
	for _, class := range pkg.Config.Classes {
		classes = append(classes, class.Name)
		if rest, ok := strings.CutPrefix(class.Include, "pkg://"); ok {
			includes = append(includes, `"`+rest+`"`)
		} else if rest, ok := strings.CutPrefix(class.Include, "res://"); ok {
			includes = append(includes, "<"+rest+">")
		}
	}
	for _, class := range gdpp {
		Assert(!slices.Contains(classes, class.Name), "Class %s is declared in %s and in %s.",
			class.Name, class.File.File.ToString(), pkg.Root.Cd(packageFileName).ToString())
		classes = append(classes, class.Name)
		includes = append(includes, `"`+class.File.Rel+`.h"`)
		if !class.Tool {
			runtime = append(runtime, class.Name)
		}
	}
	if writeTemplate(pkg.BuildCache.Cd("__register_types__.cpp"), registerTypesTemplate, map[string]any{
		"Classes":        classes,
		"RuntimeClasses": runtime,
		"Includes":       uniqueSorted(includes, strings.Compare),
	}) {
		LogInfo("Registering classes for %s...", styledPackageName(pkg.Root))
	}
}

// packageFiles returns the paths of the package's files with the given
// extensions, relative to its root. Like ListPackages, it skips hidden
// directories, res://_gd++proj and nested Godot projects, and also nested
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

// generateGdextension writes the package's .gdextension file, which points
// Godot to its libraries for all targets and to its class icons, and the
// .uid file next to it, with a UID derived from the package name, so Godot
// doesn't generate one. The minimum Godot version is the API spec's.
func generateGdextension(pkg Package, gdpp []gdppClass) {
	var spec struct {
		Header struct {
			Major int `json:"version_major"`
			Minor int `json:"version_minor"`
		} `json:"header"`
	}
	specFile := pkg.BuildCache.Cd("extension_api.json")
	Check(json.Unmarshal([]byte(specFile.ReadString()), &spec), "Failed to parse %s", specFile.ToString())
	Assert(spec.Header.Major > 0, "Failed to find the Godot version in %s.", specFile.ToString())

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
		"GodotVersion": fmt.Sprintf("%d.%d", spec.Header.Major, spec.Header.Minor),
		"Libraries":    libs,
		"Icons":        icons,
	}), "Failed to generate %s", file.ToString())
	changed := !file.IsFile() || file.ReadString() != text.String()
	// Written even if unchanged: its new modification time makes the Godot
	// editor reload the extension, when the editor window gets focus.
	file.WriteString(text.String())
	if writeIfChanged(pkg.Root.Cd(pkg.Id+".gdextension.uid"), godotUid(packageName(pkg.Root))+"\n") || changed {
		LogInfo("Generating .gdextension for %s...", styledPackageName(pkg.Root))
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
