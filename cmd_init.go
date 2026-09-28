package main

import (
	"strconv"
	"strings"
)

// CmdInit sets up GD++ in a project, or creates or updates a package. With
// no path it inits the project; with one it inits the package there.
type CmdInit struct {
	Path   string `arg:"positional" help:"the package directory; omit to init the project"`
	Vcs    string `arg:"--vcs" placeholder:"none|git" help:"the project's version control system"`
	Update bool   `arg:"--update" help:"update an existing package instead of creating one"`
	Bind   string `arg:"--bind" placeholder:"NAME" help:"the package's Godot C++ bindings"`
	Spec   string `arg:"--spec" placeholder:"NAME" help:"the package's Godot API spec"`
	Syntax *int   `arg:"--syntax" placeholder:"N" help:"the package's GD++ syntax version"`
	Std    string `arg:"--std" placeholder:"STD" help:"the package's C++ standard, e.g. c++20"`
}

func (c *CmdInit) Run() {
	if c.Path == "" {
		Assert(!c.Update && c.Bind == "" && c.Spec == "" && c.Syntax == nil && c.Std == "", "Invalid arguments: --update, --bind, --spec, --syntax and --std require a package path.")
		c.initProject()
		return
	}
	Assert(c.Vcs == "", "Invalid arguments: --vcs cannot be used with a package path.")
	for _, name := range []string{c.Bind, c.Spec} {
		if name != "" {
			assertDepName(name)
		}
	}
	root := ParsePath(c.Path)
	p := LoadProject(root)
	switch {
	case c.Update:
		c.updatePackage(p, root)
	case root.IsPackageRoot():
		Confirm("Package %s already exists. Update it?", root.ToString())
		c.updatePackage(p, root)
	default:
		c.newPackage(p, root)
	}
}

// writeConfig writes text to the config file and logs what changed, which
// changes describe (e.g. "the VCS to git"). If the file already holds text,
// it warns instead, and returns false.
func writeConfig(file Path, text string, changes []string) bool {
	switch {
	case !file.Exists():
		LogInfo("Created %s.", file.ToString())
	case file.ReadString() == text:
		LogWarn("No changes were made.")
		return false
	case len(changes) == 0:
		LogInfo("Reformatted %s.", file.ToString())
	}
	for _, change := range changes {
		LogInfo("Set %s.", change)
	}
	file.WriteString(text)
	return true
}

// Project-level inits.

func (c *CmdInit) initProject() {
	Assert(c.Vcs == "" || c.Vcs == "none" || c.Vcs == "git", "Invalid arguments: --vcs must be none or git.")
	p := LoadProject(Cwd())
	var changes []string
	vcsChanged := c.Vcs != "" && c.Vcs != p.Config.VCS
	if vcsChanged {
		p.Config.VCS = c.Vcs
		changes = append(changes, "the VCS to "+c.Vcs)
	}
	if !writeConfig(p.Root.Cd(projectConfigFileName), p.Config.Encode(), changes) {
		return
	}
	if vcsChanged {
		SyncGitignores(p)
	}
	LogInfo("Success!")
}

// Package-level inits.

func (c *CmdInit) newPackage(p Project, root Path) {
	Assert(c.Bind != "" && c.Spec != "", "Invalid arguments: --bind and --spec are required for a new package.")
	config := DefaultPackageConfig()
	c.setPackageFlags(&config)
	if !root.Exists() {
		root.CreateDirectory()
	}
	writeConfig(root.Cd(packageFileName), config.Encode(), nil)
	if p.Config.VCS == "git" {
		EditGitignore(root, func(text string) string { return packageGitignore.set(text, true) })
	}
	warnMissingDeps(p, config)
	LogInfo("Success!")
}

func (c *CmdInit) updatePackage(p Project, root Path) {
	Assert(root.IsPackageRoot(), "There is no GD++ package at %s.", root.ToString())
	config := LoadPackage(root).Config
	changes := c.setPackageFlags(&config)
	changed := writeConfig(root.Cd(packageFileName), config.Encode(), changes)
	warnMissingDeps(p, config)
	if changed {
		LogInfo("Success!")
	}
}

// setPackageFlags sets the config values given by flags, and describes those
// that changed.
func (c *CmdInit) setPackageFlags(config *PackageConfig) (changes []string) {
	set := func(field *string, value, desc string) {
		if value != "" && value != *field {
			*field = value
			changes = append(changes, desc+" to "+value)
		}
	}
	set(&config.Bindings, c.Bind, "the Godot C++ bindings")
	set(&config.ApiSpec, c.Spec, "the Godot API spec")
	if c.Syntax != nil && *c.Syntax != config.Syntax {
		config.Syntax = *c.Syntax
		changes = append(changes, "the GD++ syntax to "+strconv.Itoa(*c.Syntax))
	}
	set(&config.CppStandard, c.Std, "the C++ standard")
	return changes
}

// warnMissingDeps warns once if the project's caches lack the package's
// bindings or API spec.
func warnMissingDeps(p Project, config PackageConfig) {
	var missing []string
	for _, d := range []struct {
		cache ProjectDepCache
		name  string
	}{{p.BindingsCache, config.Bindings}, {p.ApiSpecsCache, config.ApiSpec}} {
		if !d.cache.Has(d.name) {
			missing = append(missing, d.cache.Desc+" "+d.name)
		}
	}
	if len(missing) > 0 {
		LogWarn("Missing %s, run `gd++ fetch --missing` to fix this.", strings.Join(missing, " and "))
	}
}
