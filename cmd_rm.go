package main

import (
	"slices"
	"strings"
)

// CmdRm removes deps from the project's caches, and packages from the project.
// Both can be removed in one run. Every removal is confirmed first.
type CmdRm struct {
	Bind            []string `arg:"--bind" placeholder:"NAME" help:"remove these Godot C++ bindings"`
	BindAll         bool     `arg:"--bind-all" help:"remove all Godot C++ bindings"`
	BindCheckedIn   bool     `arg:"--bind-checked-in" help:"remove all checked in Godot C++ bindings"`
	BindEphemeral   bool     `arg:"--bind-ephemeral" help:"remove all ephemeral Godot C++ bindings"`
	Spec            []string `arg:"--spec" placeholder:"NAME" help:"remove these Godot API specs"`
	SpecAll         bool     `arg:"--spec-all" help:"remove all Godot API specs"`
	SpecCheckedIn   bool     `arg:"--spec-checked-in" help:"remove all checked in Godot API specs"`
	SpecEphemeral   bool     `arg:"--spec-ephemeral" help:"remove all ephemeral Godot API specs"`
	Engine          []string `arg:"--engine" placeholder:"NAME" help:"remove these Godot engines"`
	EngineAll       bool     `arg:"--engine-all" help:"remove all Godot engines"`
	EngineCheckedIn bool     `arg:"--engine-checked-in" help:"remove all checked in Godot engines"`
	EngineEphemeral bool     `arg:"--engine-ephemeral" help:"remove all ephemeral Godot engines"`
	DepAll          bool     `arg:"--dep-all" help:"remove all dependencies of all kinds"`
	DepCheckedIn    bool     `arg:"--dep-checked-in" help:"remove all checked in dependencies of all kinds"`
	DepEphemeral    bool     `arg:"--dep-ephemeral" help:"remove all ephemeral dependencies of all kinds"`
	Pkg             []string `arg:"--pkg" placeholder:"PATH" help:"remove the packages at these paths"`
	PkgAll          bool     `arg:"--pkg-all" help:"remove all packages"`
	Dir             bool     `arg:"--dir" help:"delete the package directories with everything inside, instead of just the GD++ files"`
}

// rmKind is what to remove of one kind of dep: the named deps, or with no
// names, all deps; either way only their checked in and/or ephemeral copies.
type rmKind struct {
	flag, plural         string // e.g. "bind", "Godot C++ bindings"
	names                []string
	checkedIn, ephemeral bool
	cache                ProjectDepCache
}

// paths returns the copies of the dep that k removes.
func (k rmKind) paths(name string) (paths []Path) {
	if k.checkedIn && k.cache.IsCheckedIn(name) {
		paths = append(paths, k.cache.CheckedInDir.Cd(name))
	}
	if k.ephemeral && k.cache.IsEphemeral(name) {
		paths = append(paths, k.cache.EphemeralDir.Cd(name))
	}
	return paths
}

// rmGroup describes a group of deps in prompts, e.g. "ephemeral Godot engines".
func rmGroup(checkedIn, ephemeral bool, plural string) string {
	switch {
	case !ephemeral:
		return "checked in " + plural
	case !checkedIn:
		return "ephemeral " + plural
	}
	return plural
}

func (c *CmdRm) Run() {
	kinds := c.validate()
	p := LoadProject(Cwd())
	kinds[0].cache, kinds[1].cache, kinds[2].cache = p.BindingsCache, p.ApiSpecsCache, p.EnginesCache
	for _, k := range kinds {
		for _, name := range k.names {
			k.cache.GetPath(name) // asserts it exists, before any prompt
		}
	}
	roots := c.packageRoots(p)

	// Confirm everything before removing anything, so a "no" changes nothing.
	// Deps chosen by a group option are confirmed once per group.
	depGroup := c.DepAll || c.DepCheckedIn || c.DepEphemeral
	var deps []Path
	for _, k := range kinds {
		for _, name := range k.names {
			Confirm("Remove the %s %s?", k.cache.Desc, name)
			deps = append(deps, k.paths(name)...)
		}
		if len(k.names) == 0 && (k.checkedIn || k.ephemeral) {
			var group []Path
			for _, name := range k.cache.Ls() {
				group = append(group, k.paths(name)...)
			}
			if len(group) > 0 && !depGroup {
				Confirm("Remove all %s?", rmGroup(k.checkedIn, k.ephemeral, k.plural))
			}
			deps = append(deps, group...)
		}
	}
	if len(deps) == 0 && len(roots) == 0 {
		LogWarn("Nothing to remove.")
		return
	}
	if depGroup && len(deps) > 0 {
		Confirm("Remove all %s?", rmGroup(kinds[0].checkedIn, kinds[0].ephemeral, "dependencies"))
	}
	if c.PkgAll && len(roots) > 0 {
		var names []string
		for _, root := range roots {
			names = append(names, root.ToString())
		}
		c.confirmPackages("all packages "+strings.Join(names, ", "), true)
	} else {
		for _, root := range roots {
			c.confirmPackages("the package "+root.ToString(), false)
		}
	}

	for _, dep := range deps {
		rmPath(dep)
	}
	// A nested package's path extends its parent's, so sorting in reverse
	// removes it first, before --dir deletes its parent.
	slices.SortFunc(roots, func(a, b Path) int { return strings.Compare(b.GetOsPath(), a.GetOsPath()) })
	for _, root := range roots {
		c.removePackage(root)
	}
	p.RemoveEmptyCacheDirs()
	LogInfo("Success!")
}

// confirmPackages asks to remove the packages described by what, stressing
// it if --dir deletes their directories.
func (c *CmdRm) confirmPackages(what string, plural bool) {
	suffix := ""
	if c.Dir {
		dirs := "its whole directory"
		if plural {
			dirs = "their whole directories"
		}
		suffix = " " + Styled("and delete "+dirs, Bold, Blink, Red)
	}
	Confirm("Remove %s%s?", what, suffix)
}

// validate asserts the arguments make sense together, and returns what to
// remove of each kind of dep, without the caches.
func (c *CmdRm) validate() []rmKind {
	kinds := []rmKind{
		{"bind", "Godot C++ bindings", c.Bind, c.BindAll || c.BindCheckedIn, c.BindAll || c.BindEphemeral, ProjectDepCache{}},
		{"spec", "Godot API specs", c.Spec, c.SpecAll || c.SpecCheckedIn, c.SpecAll || c.SpecEphemeral, ProjectDepCache{}},
		{"engine", "Godot engines", c.Engine, c.EngineAll || c.EngineCheckedIn, c.EngineAll || c.EngineEphemeral, ProjectDepCache{}},
	}
	counts := []int{
		countTrue(len(c.Bind) > 0, c.BindAll, c.BindCheckedIn, c.BindEphemeral),
		countTrue(len(c.Spec) > 0, c.SpecAll, c.SpecCheckedIn, c.SpecEphemeral),
		countTrue(len(c.Engine) > 0, c.EngineAll, c.EngineCheckedIn, c.EngineEphemeral),
	}
	depFlags := countTrue(c.DepAll, c.DepCheckedIn, c.DepEphemeral)
	Assert(depFlags <= 1, "Invalid arguments: only one of --dep-all, --dep-checked-in and --dep-ephemeral can be used.")
	Assert(depFlags == 0 || counts[0]+counts[1]+counts[2] == 0, "Invalid arguments: --dep options cannot be used with --bind, --spec or --engine options.")
	for i := range kinds {
		k := &kinds[i]
		Assert(counts[i] <= 1, "Invalid arguments: only one of --%s, --%s-all, --%s-checked-in and --%s-ephemeral can be used.", k.flag, k.flag, k.flag, k.flag)
		for _, name := range k.names {
			assertDepName(name)
		}
		k.names = slices.Clone(k.names) // deduplicated, so each is confirmed once
		slices.SortFunc(k.names, compareDepNames)
		k.names = slices.Compact(k.names)
		if len(k.names) > 0 {
			k.checkedIn, k.ephemeral = true, true
		}
		k.checkedIn = k.checkedIn || c.DepAll || c.DepCheckedIn
		k.ephemeral = k.ephemeral || c.DepAll || c.DepEphemeral
	}
	Assert(len(c.Pkg) == 0 || !c.PkgAll, "Invalid arguments: --pkg and --pkg-all cannot be used together.")
	Assert(!c.Dir || len(c.Pkg) > 0 || c.PkgAll, "Invalid arguments: --dir can only be used with --pkg or --pkg-all.")
	Assert(slices.Max(counts) > 0 || depFlags > 0 || len(c.Pkg) > 0 || c.PkgAll, "Invalid arguments: a --bind, --spec, --engine, --dep or --pkg option is required.")
	return kinds
}

// packageRoots returns the roots of the packages to remove.
func (c *CmdRm) packageRoots(p Project) []Path {
	var roots []Path
	if c.PkgAll {
		for _, pkg := range p.ListPackages() {
			roots = append(roots, pkg.Root)
		}
	}
	for _, s := range c.Pkg {
		root := ParsePath(s)
		Assert(root.IsPackageRoot(), "There is no GD++ package at %s.", root.ToString())
		projectRoot, _ := GetProjectRootMaybe(root)
		Assert(projectRoot == p.Root, "The package %s is not in this project.", s)
		if !slices.Contains(roots, root) {
			roots = append(roots, root)
		}
	}
	for _, root := range roots {
		Assert(!c.Dir || root != p.Root, "Invalid arguments: --dir cannot delete the project root.")
	}
	return roots
}

// removePackage deletes the package's directory with --dir; otherwise just
// its config file, its build cache and its .gitignore block.
func (c *CmdRm) removePackage(root Path) {
	if c.Dir {
		rmPath(root)
		return
	}
	rmPath(root.Cd(packageFileName))
	if cache := root.Cd(packageBuildCacheDirName); cache.Exists() {
		rmPath(cache)
	}
	EditGitignore(root, func(text string) string { return packageGitignore.set(text, false) })
}

// rmPath deletes the file or directory at p, and logs it.
func rmPath(p Path) {
	name := p.ToString() // before p is gone
	p.Remove()
	LogInfo("Deleted %s.", name)
}
