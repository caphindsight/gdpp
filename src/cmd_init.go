package main

import (
	"cmp"
	"path"
	"slices"
	"strconv"
	"strings"

	"gd++/trans"
)

// CmdInit sets up GD++ in a project, or creates or updates a package. With
// no path it inits the project; with one it inits the package there; with
// --class it adds or updates a class of the package there.
type CmdInit struct {
	Path        string   `arg:"positional" help:"the package directory; omit to init the project"`
	Vcs         string   `arg:"--vcs" placeholder:"none|git" help:"the project's version control system"`
	Presets     bool     `arg:"--presets" help:"keep GD++'s files out of the project's export presets"`
	NoPresets   bool     `arg:"--nopresets" help:"stop keeping GD++'s files out of the project's export presets"`
	Update      bool     `arg:"--update" help:"update an existing package or class instead of creating one"`
	Bind        string   `arg:"--bind" placeholder:"NAME" help:"the package's Godot C++ bindings"`
	Spec        string   `arg:"--spec" placeholder:"NAME" help:"the package's Godot API spec"`
	Syntax      *int     `arg:"--syntax" placeholder:"N" help:"the package's GD++ syntax version"`
	Nightly     bool     `arg:"--nightly" help:"the same as --syntax 0, the nightly syntax"`
	Std         string   `arg:"--std" placeholder:"STD" help:"the package's C++ standard, e.g. c++20"`
	Prefix      string   `arg:"--prefix" placeholder:"NAME" help:"the prefix of the classes GD++ adds to the package, e.g. Foo for FooAsync [default: the package directory's name in PascalCase]"`
	QuitTimeout *float64 `arg:"--quit-timeout" placeholder:"SECONDS" help:"how long the package's tasks may still run after the game started quitting, before it exits anyway [default: 1]"`
	HotReload   bool     `arg:"--hotreload" help:"make the editor reload the package's library when it changes (default)"`
	NoHotReload bool     `arg:"--nohotreload" help:"make the editor load the package's library only once, e.g. to work around Godot crashing on reload"`
	Class       string   `arg:"--class" placeholder:"NAME" help:"the name of a class to add or update in the package"`
	Include     string   `arg:"--include" placeholder:"PATH" help:"the class's header, e.g. pkg://my_node.h"`
	NoInclude   bool     `arg:"--noinclude" help:"remove the class's header, or create the class without one"`
	Icon        string   `arg:"--icon" placeholder:"PATH" help:"the class's icon, e.g. pkg://my_node.svg"`
	NoIcon      bool     `arg:"--noicon" help:"remove the class's icon"`
	Tool        bool     `arg:"--tool" help:"make the class run in the editor too"`
	NoTool      bool     `arg:"--notool" help:"make the class run only in the game"`
	Abstract    bool     `arg:"--abstract" help:"make the class only a base for other classes, like @abstract"`
	NoAbstract  bool     `arg:"--noabstract" help:"make the class concrete"`
	Ptr         bool     `arg:"--ptr" help:"let GD++ code use the class by name, as a pointer; for classes that aren't refcounted"`
	Ref         bool     `arg:"--ref" help:"let GD++ code use the class by name, as a Ref; for refcounted classes"`
	NoGdpp      bool     `arg:"--nogdpp" help:"stop GD++ code from using the class by name"`
}

func (c *CmdInit) Run() {
	c.Syntax = chosenSyntax(c.Syntax, c.Nightly)
	if c.Path == "" {
		Assert(!c.Update && c.Bind == "" && c.Spec == "" && c.Syntax == nil && c.Std == "" && c.Prefix == "" && c.QuitTimeout == nil && !c.HotReload && !c.NoHotReload && c.Class == "",
			"Invalid arguments: --update, --bind, --spec, --syntax, --std, --prefix, --quit-timeout, --hotreload, --nohotreload and --class require a package path.")
		c.assertNoClassFlags()
		c.initProject()
		return
	}
	Assert(c.Vcs == "" && !c.Presets && !c.NoPresets, "Invalid arguments: --vcs, --presets and --nopresets cannot be used with a package path.")
	if c.Class != "" {
		Assert(c.Bind == "" && c.Spec == "" && c.Syntax == nil && c.Std == "" && c.Prefix == "" && c.QuitTimeout == nil && !c.HotReload && !c.NoHotReload,
			"Invalid arguments: --bind, --spec, --syntax, --std, --prefix, --quit-timeout, --hotreload and --nohotreload cannot be used with --class.")
		c.initClass(ParsePath(c.Path))
		return
	}
	c.assertNoClassFlags()
	for _, name := range []string{c.Bind, c.Spec} {
		if name != "" {
			assertDepName(name)
		}
	}
	Assert(c.Prefix == "" || classNameRegexp.MatchString(c.Prefix), "Invalid arguments: %q is not a valid class name prefix.", c.Prefix)
	Assert(c.QuitTimeout == nil || *c.QuitTimeout >= 0, "Invalid arguments: --quit-timeout cannot be negative.")
	Assert(!c.HotReload || !c.NoHotReload, "Invalid arguments: --hotreload and --nohotreload cannot be used together.")
	if s := c.Syntax; s != nil {
		Assert(*s >= 0, "Invalid arguments: --syntax cannot be negative.")
		if *s == trans.NightlySyntax {
			LogWarn("Syntax %d is reserved for the nightly version of GD++, which is explicitly not backward compatible: never use it in production!", *s)
		} else if !trans.Supports(*s) {
			Confirm("Unknown GD++ syntax %d: either the value is wrong, or this version of gd++ is outdated. Continue?", *s)
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

func (c *CmdInit) assertNoClassFlags() {
	Assert(c.Include == "" && !c.NoInclude && c.Icon == "" && !c.NoIcon && !c.Tool && !c.NoTool && !c.Abstract && !c.NoAbstract &&
		!c.Ptr && !c.Ref && !c.NoGdpp,
		"Invalid arguments: --include, --noinclude, --icon, --noicon, --tool, --notool, --abstract, --noabstract, --ptr, --ref and --nogdpp require --class.")
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
	Assert(!c.Presets || !c.NoPresets, "Invalid arguments: --presets and --nopresets cannot be used together.")
	p := LoadProject(Cwd())
	var changes []string
	vcsChanged := c.Vcs != "" && c.Vcs != p.Config.VCS
	if vcsChanged {
		p.Config.VCS = c.Vcs
		changes = append(changes, "the VCS to "+c.Vcs)
	}
	if (c.Presets || c.NoPresets) && c.Presets != p.Config.Presets {
		p.Config.Presets = c.Presets
		changes = append(changes, "the export preset filters to "+onOff(c.Presets))
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
	config.Syntax = trans.LatestSyntax
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
	config := LoadPackageAt(root).Config
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
	Assert(!c.Tool || !c.NoTool, "Invalid arguments: --tool and --notool cannot be used together.")
	Assert(!c.Abstract || !c.NoAbstract, "Invalid arguments: --abstract and --noabstract cannot be used together.")
	Assert(!(c.Ptr && c.Ref || c.Ptr && c.NoGdpp || c.Ref && c.NoGdpp), "Invalid arguments: --ptr, --ref and --nogdpp cannot be used together.")
	for _, p := range []string{c.Include, c.Icon} {
		Assert(p == "" || isClassPath(p), "Invalid arguments: %s must start with pkg:// or res://.", p)
	}
	assertNotGdppClass(root, c.Class)
	config := LoadPackageAt(root).Config
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
	class := PackageClass{Name: c.Class, Include: pick(old.Include, c.Include, c.NoInclude), Icon: pick(old.Icon, c.Icon, c.NoIcon),
		Tool: c.Tool || old.Tool && !c.NoTool, Abstract: c.Abstract || old.Abstract && !c.NoAbstract, Kind: old.Kind}
	switch {
	case c.Ptr:
		class.Kind = "ptr"
	case c.Ref:
		class.Kind = "ref"
	case c.NoGdpp:
		class.Kind = ""
	}
	Assert(class.Kind == "" || class.Include != "", "Invalid arguments: GD++ code can only use class %s if it has an include.", c.Class)
	config.Classes[i] = class
	config.SortClasses()
	var changes []string
	for _, f := range []struct{ desc, old, new string }{{"include path", old.Include, class.Include}, {"icon", old.Icon, class.Icon}} {
		if isNew || f.new != f.old {
			changes = append(changes, "the "+f.desc+" of class "+c.Class+" to "+cmp.Or(f.new, "none"))
		}
	}
	if isNew || class.Tool != old.Tool {
		changes = append(changes, "class "+c.Class+" to "+map[bool]string{true: "a tool class", false: "a runtime class"}[class.Tool])
	}
	if class.Abstract != old.Abstract {
		changes = append(changes, "class "+c.Class+" to "+map[bool]string{true: "an abstract class", false: "a concrete class"}[class.Abstract])
	}
	if class.Kind != old.Kind {
		changes = append(changes, "class "+c.Class+" to "+map[string]string{"ptr": "[ptr], usable from GD++ as a pointer", "ref": "[ref], usable from GD++ as a Ref", "": "not usable from GD++"}[class.Kind])
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
	set(&config.Prefix, c.Prefix, "the class name prefix")
	if q := c.QuitTimeout; q != nil && (config.QuitTimeout == nil || *config.QuitTimeout != *q) {
		config.QuitTimeout = q
		changes = append(changes, "the quit timeout to "+seconds(*q))
	}
	if (c.HotReload || c.NoHotReload) && c.HotReload != (config.HotReload == nil || *config.HotReload) {
		config.HotReload = nil // on, the default
		if c.NoHotReload {
			config.HotReload = &c.HotReload
		}
		changes = append(changes, "hot reload to "+onOff(c.HotReload))
	}
	return changes
}

// warnMissingDeps warns once if the project's caches lack the package's
// bindings or API spec.
func warnMissingDeps(p Project, config PackageConfig) {
	var missing []string
	for _, cache := range p.Caches {
		if name, ok := config.Dep(cache.Name); ok && !cache.Has(name) {
			missing = append(missing, cache.Desc+" "+name)
		}
	}
	if len(missing) > 0 {
		LogWarn("Missing %s, run `gd++ fetch --missing` to fix this.", strings.Join(missing, " and "))
	}
}
