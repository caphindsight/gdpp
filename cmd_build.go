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
	Path     string `arg:"positional" help:"build the package containing this path [default: the current directory]"`
	Platform string `arg:"--platform" placeholder:"windows|linux|macos" help:"the target platform [default: this one]"`
	Windows  bool   `arg:"-w" help:"shorthand for --platform=windows"`
	Arch     string `arg:"--arch" placeholder:"x86_32|x86_64|arm64" help:"the target CPU architecture [default: this one]"`
	Opt      bool   `arg:"--opt" help:"optimize for speed"`
	Small    bool   `arg:"--small" help:"optimize for binary size"`
	Ship     bool   `arg:"--ship" help:"build a release library instead of a debug one"`
	Proj     bool   `arg:"--proj" help:"build all packages in the project, one after another"`
}

func (c *CmdBuild) Run() {
	args := c.sconsArgs()
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
		Exec("Building "+packageName(pkg.Root)+"...", pkg.BuildCache, "scons", args...)
	}
}

// sconsArgs asserts the arguments make sense together, and returns the
// matching godot-cpp build options.
func (c *CmdBuild) sconsArgs() []string {
	Assert(!c.Windows || c.Platform == "", "Invalid arguments: -w and --platform cannot be used together.")
	Assert(!c.Opt || !c.Small, "Invalid arguments: --opt and --small cannot be used together.")
	Assert(!c.Proj || c.Path == "", "Invalid arguments: a path and --proj cannot be used together.")
	if c.Windows {
		c.Platform = "windows"
	}
	hostPlatform := map[string]string{"linux": "linux", "darwin": "macos", "windows": "windows"}[runtime.GOOS]
	hostArch := map[string]string{"386": "x86_32", "amd64": "x86_64", "arm64": "arm64"}[runtime.GOARCH]
	target := "template_debug"
	if c.Ship {
		target = "template_release"
	}
	args := []string{
		"platform=" + buildOption("platform", c.Platform, hostPlatform, "windows", "linux", "macos"),
		"arch=" + buildOption("arch", c.Arch, hostArch, "x86_32", "x86_64", "arm64"),
		"target=" + target,
	}
	if c.Opt {
		args = append(args, "optimize=speed")
	} else if c.Small {
		args = append(args, "optimize=size")
	}
	return args
}

// buildOption returns the value of the --flag option, asserting it's one of
// allowed, or the detected one if the option isn't given.
func buildOption(flag, value, detected string, allowed ...string) string {
	if value == "" {
		Assert(detected != "", "Failed to detect the %s, use --%s to choose one.", flag, flag)
		return detected
	}
	Assert(slices.Contains(allowed, value), "Invalid arguments: --%s must be one of %s.", flag, strings.Join(allowed, ", "))
	return value
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
