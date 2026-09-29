package main

import "slices"

// CmdClean deletes the build caches of packages, so their next build starts
// from scratch, and with --bin also all libraries and .gdextension files in
// their roots.
type CmdClean struct {
	Paths []string `arg:"positional" placeholder:"PATH" help:"clean the packages containing these paths [default: the current directory]"`
	Proj  bool     `arg:"--proj" help:"clean all packages in the project"`
	Bin   bool     `arg:"--bin" help:"also delete all libraries and .gdextension files in the package roots"`
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
		if len(packageGarbage(root, c.Bin)) > 0 && !slices.Contains(dirty, root) {
			dirty = append(dirty, root)
		}
	}
	if len(dirty) == 0 {
		LogInfo("Nothing to clean.")
		return
	}
	for _, root := range dirty {
		cleanPackage(root, c.Bin)
	}
}
