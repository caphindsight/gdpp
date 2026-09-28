package main

import (
	"regexp"
	"slices"
)

// CmdClean deletes the build caches of packages, so their next build starts
// from scratch, and with --bin also their built libraries and generated
// .gdextension files.
type CmdClean struct {
	Paths []string `arg:"positional" placeholder:"PATH" help:"clean the packages containing these paths [default: the current directory]"`
	Proj  bool     `arg:"--proj" help:"clean all packages in the project"`
	Bin   bool     `arg:"--bin" help:"also delete the built libraries, for all platforms, and the .gdextension files"`
}

func (c *CmdClean) Run() {
	Assert(!c.Proj || len(c.Paths) == 0, "Invalid arguments: paths and --proj cannot be used together.")
	// Check every path before deleting anything.
	var roots []Path
	if c.Proj {
		p := LoadProject(Cwd())
		for _, pkg := range p.ListPackages() {
			roots = append(roots, pkg.Root)
		}
	} else if len(c.Paths) == 0 {
		roots = []Path{GetPackageRoot(Cwd())}
	} else {
		for _, s := range c.Paths {
			roots = append(roots, GetPackageRoot(ParsePath(s)))
		}
	}
	var dirty []Path
	for _, root := range roots {
		GetProjectRoot(root)
		if len(c.garbage(root)) > 0 && !slices.Contains(dirty, root) {
			dirty = append(dirty, root)
		}
	}
	if len(dirty) == 0 {
		LogInfo("Nothing to clean.")
		return
	}
	for _, root := range dirty {
		LogInfo("Cleaning %s...", packageName(root))
		for _, p := range c.garbage(root) {
			p.Remove()
		}
	}
}

// garbage returns what to delete from the package at root: its build cache,
// and with --bin its libraries, named like SConstruct names them, and the
// .gdextension and .uid files build generates.
func (c *CmdClean) garbage(root Path) []Path {
	var paths []Path
	if cache := root.Cd(packageBuildCacheDirName); cache.Exists() {
		paths = append(paths, cache)
	}
	if c.Bin {
		id := regexp.QuoteMeta(root.Name())
		bin := regexp.MustCompile(`^(lib` + id + `\..+\.(dll|so|dylib)|` + id + `\.gdextension(\.uid)?)$`)
		for _, child := range root.Ls() {
			if bin.MatchString(child.Name()) {
				paths = append(paths, child)
			}
		}
	}
	return paths
}
