package main

import (
	"slices"
	"strings"
)

// CmdBuild compiles a package, or with --proj every package in the project,
// into a GDExtension library with SCons. Each package's build cache gets a
// copy of its GD++ and C/C++ files, which the build compiles instead, a
// copy of its bindings and API spec, a generated __register_types__.cpp
// registering its classes, and a generated SConstruct. After building, the
// package root gets a generated <id>.gdextension file and its .uid file.
// Debug builds are godot-cpp dev builds with debug symbols and hot reload;
// release builds use link-time optimization. The build parameters are logged
// once, before the packages are built. With --clean, packages are
// cleaned first like `gd++ clean --bin` does.
type CmdBuild struct {
	Path     string   `arg:"positional" help:"build the package containing this path [default: the current directory]"`
	For      []string `arg:"--for" placeholder:"PLATFORM.ARCH" help:"build for each of these targets, e.g. windows.x86_64 or w.x64; platforms: windows|win|w, linux|lin|l, macos|mac|m; archs: x86_32|x32, x86_64|x64, arm64|a64 [default: this machine]"`
	Platform string   `arg:"-p,--platform" placeholder:"windows|linux|macos" help:"the target platform [default: this one]"`
	Windows  bool     `arg:"-w" help:"shorthand for --platform=windows"`
	Arch     string   `arg:"--arch" placeholder:"x86_32|x86_64|arm64" help:"the target CPU architecture [default: this one]"`
	BuildOptions
	Proj  bool `arg:"--proj" help:"build all packages in the project, one after another"`
	Clean bool `arg:"--clean" help:"clean the build caches and libraries first, so the build starts from scratch"`
}

func (c *CmdBuild) Run() {
	targets := c.targets()
	assertScons()
	path := Cwd()
	if c.Path != "" {
		path = ParsePath(c.Path)
	}
	p := LoadProject(path)
	SyncExportPresets(p)
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
	gdpp := slices.ContainsFunc(pkgs, func(pkg Package) bool { return len(packageFiles(p, pkg, gdppExtensions...)) > 0 })
	LogInfo("Build parameters: %s.", c.describe(targets, gdpp))
	for _, pkg := range pkgs {
		if c.Clean {
			cleanPackage(pkg.Root, true)
		}
		buildExtension(p, pkg, c.BuildOptions, targets)
	}
}

// targets asserts the arguments make sense together, and returns the
// PLATFORM.ARCH targets to build for, with full names.
func (c *CmdBuild) targets() []string {
	c.validate()
	Assert(!c.Windows || c.Platform == "", "Invalid arguments: -w and --platform cannot be used together.")
	Assert(len(c.For) == 0 || !c.Windows && c.Platform == "" && c.Arch == "", "Invalid arguments: --for cannot be used together with -w, --platform or --arch.")
	Assert(!c.Proj || c.Path == "", "Invalid arguments: a path and --proj cannot be used together.")
	if len(c.For) == 0 {
		if c.Windows {
			c.Platform = "windows"
		}
		platform := buildOption("platform", c.Platform, hostPlatform, buildPlatforms)
		c.assertCompiler(platform)
		return []string{platform + "." + buildOption("arch", c.Arch, hostArch, buildArchs)}
	}
	var targets []string
	for _, s := range c.For {
		platform, arch, _ := strings.Cut(s, ".")
		platform, arch = fullName(buildPlatforms, platform), fullName(buildArchs, arch)
		Assert(platform != "" && arch != "", "Invalid arguments: %q is not a valid target, use PLATFORM.ARCH, e.g. windows.x86_64 or w.x64.", s)
		c.assertCompiler(platform)
		if target := platform + "." + arch; !slices.Contains(targets, target) {
			targets = append(targets, target)
		}
	}
	return targets
}
