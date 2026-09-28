package main

import (
	"cmp"
	"path"
	"slices"
	"strconv"
	"strings"
)

// CmdInit sets up GD++ in a project, or creates or updates a package. With
// no path it inits the project; with one it inits the package there; with
// --class it adds or updates a class of the package there.
type CmdInit struct {
	Path      string `arg:"positional" help:"the package directory; omit to init the project"`
	Vcs       string `arg:"--vcs" placeholder:"none|git" help:"the project's version control system"`
	Update    bool   `arg:"--update" help:"update an existing package or class instead of creating one"`
	Bind      string `arg:"--bind" placeholder:"NAME" help:"the package's Godot C++ bindings"`
	Spec      string `arg:"--spec" placeholder:"NAME" help:"the package's Godot API spec"`
	Syntax    *int   `arg:"--syntax" placeholder:"N" help:"the package's GD++ syntax version"`
	Std       string `arg:"--std" placeholder:"STD" help:"the package's C++ standard, e.g. c++20"`
	Class     string `arg:"--class" placeholder:"NAME" help:"the name of a class to add or update in the package"`
	Include   string `arg:"--include" placeholder:"PATH" help:"the class's header, e.g. pkg://my_node.h"`
	NoInclude bool   `arg:"--noinclude" help:"remove the class's header, or create the class without one"`
	Icon      string `arg:"--icon" placeholder:"PATH" help:"the class's icon, e.g. pkg://my_node.svg"`
	NoIcon    bool   `arg:"--noicon" help:"remove the class's icon"`
}

func (c *CmdInit) Run() {
	if c.Path == "" {
		Assert(!c.Update && c.Bind == "" && c.Spec == "" && c.Syntax == nil && c.Std == "" && c.Class == "", "Invalid arguments: --update, --bind, --spec, --syntax, --std and --class require a package path.")
		Assert(c.Include == "" && !c.NoInclude && c.Icon == "" && !c.NoIcon, "Invalid arguments: --include, --noinclude, --icon and --noicon require --class.")
		c.initProject()
		return
	}
	Assert(c.Vcs == "", "Invalid arguments: --vcs cannot be used with a package path.")
	if c.Class != "" {
		Assert(c.Bind == "" && c.Spec == "" && c.Syntax == nil && c.Std == "", "Invalid arguments: --bind, --spec, --syntax and --std cannot be used with --class.")
		c.initClass(ParsePath(c.Path))
		return
	}
	Assert(c.Include == "" && !c.NoInclude && c.Icon == "" && !c.NoIcon, "Invalid arguments: --include, --noinclude, --icon and --noicon require --class.")
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

// initClass adds the class given by flags to the package at root, or updates
// the class of the same name.
func (c *CmdInit) initClass(root Path) {
	Assert(classNameRegexp.MatchString(c.Class), "Invalid arguments: %q is not a valid class name.", c.Class)
	Assert(c.Include == "" || !c.NoInclude, "Invalid arguments: --include and --noinclude cannot be used together.")
	Assert(c.Icon == "" || !c.NoIcon, "Invalid arguments: --icon and --noicon cannot be used together.")
	for _, p := range []string{c.Include, c.Icon} {
		Assert(p == "" || isClassPath(p), "Invalid arguments: %s must start with pkg:// or res://.", p)
	}
	Assert(root.IsPackageRoot(), "There is no GD++ package at %s.", root.ToString())
	config := LoadPackage(root).Config
	i := slices.IndexFunc(config.Classes, func(k PackageClass) bool { return k.Name == c.Class })
	Assert(i >= 0 || !c.Update, "There is no class %s in %s.", c.Class, root.ToString())
	if i < 0 && c.Include == "" && !c.NoInclude {
		LogFatal("Invalid arguments: a new class requires --include or --noinclude.")
	}
	if i >= 0 && !c.Update {
		Confirm("Class %s already exists in %s. Update it?", c.Class, root.ToString())
	}
	if ext := path.Ext(c.Include); c.Include != "" && ext != ".h" && ext != ".hpp" {
		Confirm("Include path %s is not a .h or .hpp file. Continue?", c.Include)
	}
	var old PackageClass
	isNew := i < 0
	if isNew {
		config.Classes = append(config.Classes, PackageClass{})
		i = len(config.Classes) - 1
	} else {
		old = config.Classes[i]
	}
	// A path flag sets the path, its --no flag clears it, and neither keeps it.
	pick := func(old, flag string, clear bool) string {
		if flag != "" || clear {
			return flag
		}
		return old
	}
	class := PackageClass{Name: c.Class, Include: pick(old.Include, c.Include, c.NoInclude), Icon: pick(old.Icon, c.Icon, c.NoIcon)}
	config.Classes[i] = class
	config.SortClasses()
	var changes []string
	for _, f := range []struct{ desc, old, new string }{{"include path", old.Include, class.Include}, {"icon", old.Icon, class.Icon}} {
		if isNew || f.new != f.old {
			changes = append(changes, "the "+f.desc+" of class "+c.Class+" to "+cmp.Or(f.new, "none"))
		}
	}
	if writeConfig(root.Cd(packageFileName), config.Encode(), changes) {
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
