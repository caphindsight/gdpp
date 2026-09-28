package main

import (
	_ "embed"
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
)

// CmdBuild compiles a package, or with --proj every package in the project,
// into a GDExtension library with SCons. Each package's build cache gets a
// copy of its bindings and API spec, a generated __register_types__.cpp
// registering its classes, and a generated SConstruct.
type CmdBuild struct {
	Path     string   `arg:"positional" help:"build the package containing this path [default: the current directory]"`
	For      []string `arg:"--for" placeholder:"PLATFORM.ARCH" help:"build for each of these targets, e.g. windows.x86_64 or w.x64; platforms: windows|win|w, linux|lin|l, macos|mac|m; archs: x86_32|x32, x86_64|x64, arm64|a64 [default: this machine]"`
	Platform string   `arg:"--platform" placeholder:"windows|linux|macos" help:"the target platform [default: this one]"`
	Windows  bool     `arg:"-w" help:"shorthand for --platform=windows"`
	Arch     string   `arg:"--arch" placeholder:"x86_32|x86_64|arm64" help:"the target CPU architecture [default: this one]"`
	Opt      bool     `arg:"--opt" help:"optimize for speed"`
	Small    bool     `arg:"--small" help:"optimize for binary size"`
	Ship     bool     `arg:"--ship" help:"build a release library instead of a debug one"`
	Proj     bool     `arg:"--proj" help:"build all packages in the project, one after another"`
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
		for _, target := range targets {
			Exec("Building "+packageName(pkg.Root)+" for "+c.describe(target)+"...", pkg.BuildCache, "scons", c.sconsArgs(target)...)
		}
	}
}

// targets asserts the arguments make sense together, and returns the
// PLATFORM.ARCH targets to build for, with full names.
func (c *CmdBuild) targets() []string {
	Assert(!c.Windows || c.Platform == "", "Invalid arguments: -w and --platform cannot be used together.")
	Assert(len(c.For) == 0 || !c.Windows && c.Platform == "" && c.Arch == "", "Invalid arguments: --for cannot be used together with -w, --platform or --arch.")
	Assert(!c.Opt || !c.Small, "Invalid arguments: --opt and --small cannot be used together.")
	Assert(!c.Proj || c.Path == "", "Invalid arguments: a path and --proj cannot be used together.")
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
	args := []string{"platform=" + platform, "arch=" + arch, "target=template_debug"}
	if c.Ship {
		args[2] = "target=template_release"
	}
	if c.Opt {
		args = append(args, "optimize=speed")
	} else if c.Small {
		args = append(args, "optimize=size")
	}
	return args
}

// describe returns the build details for target shown in the task name, e.g.
// "windows.x86_64, release build, optimized", styling non-default values.
func (c *CmdBuild) describe(target string) string {
	desc := target
	if target != hostPlatform+"."+hostArch {
		desc = Styled(target, Bold, Cyan)
	}
	if c.Ship {
		desc += ", " + Styled("release build", Bold, Magenta)
	} else {
		desc += ", debug build"
	}
	if c.Opt {
		desc += ", " + Styled("optimized", Bold, Blue)
	} else if c.Small {
		desc += ", " + Styled("optimized for binary size", Bold, Yellow)
	}
	return desc
}

// generateBuildCache syncs the package's bindings and API spec into its build
// cache, and writes the generated files there.
func generateBuildCache(p Project, pkg Package) {
	dep := func(cache ProjectDepCache, name string) Path {
		Assert(cache.Has(name), "Missing %s %s, run `gd++ fetch --missing` to fix this.", cache.Desc, name)
		return cache.GetPath(name)
	}
	bind, spec := dep(p.Caches[0], pkg.Config.Bindings), dep(p.Caches[1], pkg.Config.ApiSpec)
	cache := pkg.BuildCache
	if !cache.Exists() {
		cache.CreateDirectory()
	}
	bind.Sync(cache.Cd("godot-cpp"))
	spec.Cd("extension_api.json").Sync(cache.Cd("extension_api.json"))

	var classes, includes []string
	for _, class := range pkg.Config.Classes {
		classes = append(classes, class.Name)
		if rest, ok := strings.CutPrefix(class.Include, "pkg://"); ok {
			includes = append(includes, `"`+rest+`"`)
		} else if rest, ok := strings.CutPrefix(class.Include, "res://"); ok {
			includes = append(includes, "<"+rest+">")
		}
	}
	writeTemplate(cache.Cd("__register_types__.cpp"), registerTypesTemplate, map[string]any{
		"Classes":  classes,
		"Includes": uniqueSorted(includes, strings.Compare),
	})

	projectRoot, err := filepath.Rel(cache.GetOsPath(), p.Root.GetOsPath())
	Check(err, "Failed to compute a relative path")
	writeTemplate(cache.Cd("SConstruct"), sconstructTemplate, map[string]any{
		"Id":          pkg.Id,
		"CppStandard": pkg.Config.CppStandard,
		"Color":       isTTY,
		"ProjectRoot": filepath.ToSlash(projectRoot),
		"Sources":     packageSources(p, pkg),
	})
}

// packageSources returns the paths of the package's C and C++ source files,
// relative to its root. Like ListPackages, it skips hidden directories,
// res://_gd++proj and nested Godot projects, and also nested packages, whose
// sources are their own.
func packageSources(p Project, pkg Package) []string {
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
			case slices.Contains([]string{".c", ".cc", ".cpp", ".cxx"}, path.Ext(childRel)):
				sources = append(sources, childRel)
			}
		}
	}
	walk(pkg.Root, "")
	return sources
}

// writeTemplate writes the template executed with data to file.
func writeTemplate(file Path, t *template.Template, data any) {
	var b strings.Builder
	Check(t.Execute(&b, data), "Failed to generate %s", file.ToString())
	file.WriteString(b.String())
}
