package main

import (
	"slices"
	"strings"
)

// CmdBuild compiles the packages its paths name (see ParsePackagePaths), one
// after another, into GDExtension libraries with SCons. Each package's build cache gets a
// copy of its GD++ and C/C++ files, which the build compiles instead, a
// copy of its bindings and API spec, a generated __register_types__.cpp
// registering its classes, and a generated SConstruct. After building, the
// package root gets a generated <id>.gdextension file and its .uid file.
// Debug builds are godot-cpp dev builds with debug symbols and hot reload;
// release builds use link-time optimization. The build parameters are logged
// once, before the packages are built. With --clean, packages with a build
// cache are cleaned first like `gd++ clean --bin` does. Each package's hidden
// directories get a .gdignore file, if missing.
type CmdBuild struct {
	Paths    []string `arg:"positional" placeholder:"PATH" help:"build the packages containing these paths; PATH/... builds all packages inside PATH, e.g. res://... the whole project [default: ..., all packages in the current directory]"`
	For      []string `arg:"--for" placeholder:"PLATFORM.ARCH" help:"build for each of these targets, e.g. windows.x86_64 or w.x64; platforms: windows|win|w, linux|lin|l, macos|mac|m; archs: x86_32|x32, x86_64|x64, arm64|a64 [default: this machine]"`
	Platform string   `arg:"-p,--platform" placeholder:"windows|linux|macos" help:"the target platform [default: this one]"`
	Windows  bool     `arg:"-w" help:"shorthand for --platform=windows"`
	Arch     string   `arg:"--arch" placeholder:"x86_32|x86_64|arm64" help:"the target CPU architecture [default: this one]"`
	BuildOptions
	Clean bool `arg:"--clean" help:"clean the build caches and libraries first, so the build starts from scratch"`
}

func (c *CmdBuild) Run() {
	targets := c.targets()
	assertScons()
	roots := ParsePackagePaths(c.Paths)
	if len(roots) == 0 {
		LogWarn("Nothing to build.")
		return
	}
	p := LoadProject(roots[0])
	var pkgs []Package
	for _, root := range roots {
		Assert(GetProjectRoot(root) == p.Root, "Invalid arguments: all packages must be in one Godot project.")
		pkgs = append(pkgs, LoadPackage(root))
	}
	SyncExportPresets(p)
	gdpp := slices.ContainsFunc(pkgs, func(pkg Package) bool { return len(packageFiles(p, pkg, gdppExtensions...)) > 0 })
	LogInfo("Build parameters: %s.", c.describe(targets, gdpp))
	for _, pkg := range pkgs {
		if c.Clean && pkg.BuildCache.Exists() {
			cleanPackage(pkg.Root, true)
		}
		pkg.HideInGodot()
		buildExtension(p, pkg, c.BuildOptions, targets)
	}
}

// targets asserts the arguments make sense together, and returns the
// PLATFORM.ARCH targets to build for, with full names.
func (c *CmdBuild) targets() []string {
	c.validate()
	Assert(!c.Windows || c.Platform == "", "Invalid arguments: -w and --platform cannot be used together.")
	Assert(len(c.For) == 0 || !c.Windows && c.Platform == "" && c.Arch == "", "Invalid arguments: --for cannot be used together with -w, --platform or --arch.")
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
