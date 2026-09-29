package main

import (
	"slices"
	"strings"
)

// CmdRm removes deps from the project's caches, packages from the project, or
// classes from a package; one of these per run. Every removal is confirmed
// first.
type CmdRm struct {
	Path            string   `arg:"positional" help:"the package to remove classes from"`
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
	Class           []string `arg:"--class" placeholder:"NAME" help:"remove these classes from the package"`
	ClassAll        bool     `arg:"--class-all" help:"remove all classes from the package"`
}

// rmKind is what to remove of one kind of dep: the named deps, or with no
// names, all deps; either way only their checked in and/or ephemeral copies.
type rmKind struct {
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
	if len(c.Class) > 0 || c.ClassAll {
		c.removeClasses(ParsePath(c.Path))
		return
	}
	p := LoadProject(Cwd())
	for i := range kinds {
		k := &kinds[i]
		k.cache = p.Caches[i]
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
				Confirm("Remove all %s?", rmGroup(k.checkedIn, k.ephemeral, k.cache.Plural))
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
	classes := len(c.Class) > 0 || c.ClassAll
	if c.Path != "" && !classes {
		LogFatal("Invalid arguments: the syntax for removing a package is `gd++ rm --pkg %s`.", c.Path)
	}
	Assert(c.Path != "" || !classes, "Invalid arguments: --class and --class-all require a package path.")
	Assert(len(c.Class) == 0 || !c.ClassAll, "Invalid arguments: --class and --class-all cannot be used together.")
	kinds := []rmKind{ // in the order of depKinds
		{names: c.Bind, checkedIn: c.BindAll || c.BindCheckedIn, ephemeral: c.BindAll || c.BindEphemeral},
		{names: c.Spec, checkedIn: c.SpecAll || c.SpecCheckedIn, ephemeral: c.SpecAll || c.SpecEphemeral},
		{names: c.Engine, checkedIn: c.EngineAll || c.EngineCheckedIn, ephemeral: c.EngineAll || c.EngineEphemeral},
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
		k, flag := &kinds[i], depKinds[i].Name
		Assert(counts[i] <= 1, "Invalid arguments: only one of --%s, --%s-all, --%s-checked-in and --%s-ephemeral can be used.", flag, flag, flag, flag)
		for _, name := range k.names {
			assertDepName(name)
		}
		k.names = uniqueSorted(k.names, compareDepNames) // so each is confirmed once
		if len(k.names) > 0 {
			k.checkedIn, k.ephemeral = true, true
		}
		k.checkedIn = k.checkedIn || c.DepAll || c.DepCheckedIn
		k.ephemeral = k.ephemeral || c.DepAll || c.DepEphemeral
	}
	Assert(len(c.Pkg) == 0 || !c.PkgAll, "Invalid arguments: --pkg and --pkg-all cannot be used together.")
	Assert(!c.Dir || len(c.Pkg) > 0 || c.PkgAll, "Invalid arguments: --dir can only be used with --pkg or --pkg-all.")
	switch countTrue(slices.Max(counts) > 0 || depFlags > 0, len(c.Pkg) > 0 || c.PkgAll, classes) {
	case 0:
		LogFatal("Invalid arguments: a --bind, --spec, --engine, --dep, --pkg or --class option is required.")
	case 2, 3:
		LogFatal("Invalid arguments: dependencies, packages and classes cannot be removed in the same run.")
	}
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

// removeClasses removes the classes given by --class or --class-all from the
// package at root.
func (c *CmdRm) removeClasses(root Path) {
	config := LoadPackageAt(root).Config
	names := uniqueSorted(c.Class, strings.Compare) // so each is confirmed once
	for _, name := range names {
		assertNotGdppClass(root, name)
		Assert(slices.ContainsFunc(config.Classes, func(k PackageClass) bool { return k.Name == name }), "There is no class %s in %s.", name, root.ToString())
	}
	for _, name := range names {
		Confirm("Remove the class %s from %s?", name, root.ToString())
	}
	if c.ClassAll {
		for _, class := range config.Classes {
			names = append(names, class.Name)
		}
		if len(names) == 0 {
			LogWarn("Nothing to remove.")
			return
		}
		Confirm("Remove all classes %s from %s?", strings.Join(names, ", "), root.ToString())
	}
	config.Classes = slices.DeleteFunc(config.Classes, func(k PackageClass) bool { return slices.Contains(names, k.Name) })
	root.Cd(packageFileName).WriteString(config.Encode())
	for _, name := range names {
		LogInfo("Removed the class %s.", name)
	}
	LogInfo("Success!")
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
