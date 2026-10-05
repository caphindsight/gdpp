// package.go: Package, a GD++ package described by its .gd++pkg.toml file.

package main

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Package is a GD++ package: a directory inside a project with a
// .gd++pkg.toml file.
type Package struct {
	Root       Path
	Id         string // name of the root directory
	Config     PackageConfig
	BuildCache Path // <root>/.gd++pkg
}

// PackageConfig holds the settings from a package's .gd++pkg.toml.
type PackageConfig struct {
	Bindings    string         `toml:"bind"` // mandatory
	ApiSpec     string         `toml:"spec"` // mandatory
	Syntax      int            `toml:"syntax"`
	CppStandard string         `toml:"std"`
	Prefix      string         `toml:"prefix,omitempty"`       // default: the package ID in PascalCase
	QuitTimeout *float64       `toml:"quit_timeout,omitempty"` // seconds; default: 1
	HotReload   *bool          `toml:"hot_reload,omitempty"`   // default: true
	MacroDepth  *int           `toml:"macro_depth,omitempty"`  // default: defaultPackageMacroDepth
	Classes     []PackageClass `toml:"class,omitempty"`
}

// PackageClass is a C++ class the package exposes to Godot. Its paths are
// package-relative (pkg://) or project-relative (res://).
type PackageClass struct {
	Name     string `toml:"name"`
	Include  string `toml:"include,omitempty"` // the header declaring the class
	Icon     string `toml:"icon,omitempty"`
	Tool     bool   `toml:"tool,omitempty"`     // whether its code runs in the editor too, like @tool
	Abstract bool   `toml:"abstract,omitempty"` // whether only its subclasses' objects are created, like @abstract
	Kind     string `toml:"kind,omitempty"`     // how GD++ code uses it: "ptr" (T*) or "ref" (Ref<T>); if empty, it can't
}

var (
	classNameRegexp = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	nonAlnumRegexp  = regexp.MustCompile(`[^A-Za-z0-9]+`)
)

// Prefix returns the prefix of the names of the classes that GD++ adds to the
// package, e.g. "Foo" for FooAsync.
func (pkg Package) Prefix() string {
	if pkg.Config.Prefix != "" {
		return pkg.Config.Prefix
	}
	return pascalCase(pkg.Id)
}

// AsyncClass returns the name of the package's class of tasks, which Async
// types name, e.g. FooAsync.
func (pkg Package) AsyncClass() string {
	return pkg.Prefix() + "Async"
}

// ResPath returns the res:// path of the package's root, e.g. res://addons/foo.
func (pkg Package) ResPath() string {
	if rel := relPath(GetProjectRoot(pkg.Root), pkg.Root); rel != "." {
		return "res://" + rel
	}
	return "res://"
}

// QuitTimeout returns how many seconds the package's tasks may still run after
// the game started quitting, before the game exits anyway.
func (pkg Package) QuitTimeout() float64 {
	if pkg.Config.QuitTimeout != nil {
		return *pkg.Config.QuitTimeout
	}
	return 1
}

// HotReload reports whether the editor reloads the package's library when it
// changes.
func (pkg Package) HotReload() bool {
	return pkg.Config.HotReload == nil || *pkg.Config.HotReload
}

// MacroDepth returns how deeply the package's macro and template invocations
// may nest.
func (pkg Package) MacroDepth() int {
	if pkg.Config.MacroDepth != nil {
		return *pkg.Config.MacroDepth
	}
	return defaultPackageMacroDepth
}

// seconds renders a number of seconds, e.g. "2.5 seconds".
func seconds(s float64) string {
	if s == 1 {
		return "1 second"
	}
	return strconv.FormatFloat(s, 'f', -1, 64) + " seconds"
}

// pascalCase converts an ID such as "my_game" or "my-game" to a class name
// prefix, "MyGame". Other characters than letters and digits separate words,
// and a leading digit gets the prefix "Pkg".
func pascalCase(id string) string {
	var b strings.Builder
	for _, word := range nonAlnumRegexp.Split(id, -1) {
		if word != "" {
			b.WriteString(strings.ToUpper(word[:1]) + word[1:])
		}
	}
	if s := b.String(); s != "" && (s[0] < '0' || s[0] > '9') {
		return s
	}
	return "Pkg" + b.String()
}

// isClassPath reports whether s is a pkg:// or res:// path.
func isClassPath(s string) bool {
	return strings.HasPrefix(s, "pkg://") || strings.HasPrefix(s, "res://")
}

// classInclude returns what follows #include for a class's pkg:// or res://
// path s: "src/foo.h" for pkg://src/foo.h, relative to the package root, or
// <src/foo.h> for res://src/foo.h, relative to the project root.
func classInclude(s string) string {
	if rest, ok := strings.CutPrefix(s, "res://"); ok {
		return "<" + rest + ">"
	}
	return `"` + strings.TrimPrefix(s, "pkg://") + `"`
}

// ClassPath resolves a class's pkg:// or res:// path s. An empty s gives a
// zero Path.
func (pkg Package) ClassPath(s string) Path {
	if rest, ok := strings.CutPrefix(s, "pkg://"); ok {
		return pkg.Root.Cd(rest)
	}
	if rest, ok := strings.CutPrefix(s, "res://"); ok {
		return GetProjectRoot(pkg.Root).Cd(rest)
	}
	return Path{}
}

// Dep returns the name of the dep of the given kind (a DepKind's Name) the
// package uses, and whether packages choose deps of that kind at all.
func (c PackageConfig) Dep(kind string) (string, bool) {
	switch kind {
	case "bind":
		return c.Bindings, true
	case "spec":
		return c.ApiSpec, true
	}
	return "", false
}

// DefaultPackageConfig returns the config with defaults for the optional
// keys; the mandatory keys are left empty.
func DefaultPackageConfig() PackageConfig {
	return PackageConfig{Syntax: defaultPackageSyntax, CppStandard: defaultPackageCppStandard}
}

// SortClasses sorts c's classes by name.
func (c *PackageConfig) SortClasses() {
	slices.SortFunc(c.Classes, func(a, b PackageClass) int { return strings.Compare(a.Name, b.Name) })
}

// Encode returns c in TOML format.
func (c PackageConfig) Encode() string {
	return encodeToml(c)
}

// LoadPackage reads the package containing p, which must be inside a
// project. It doesn't create any directories.
func LoadPackage(p Path) Package {
	root := GetPackageRoot(p)
	GetProjectRoot(root)
	file := root.Cd(packageFileName)

	config := DefaultPackageConfig()
	meta := decodeToml(file, &config)
	for _, key := range []string{"bind", "spec"} {
		Assert(meta.IsDefined(key), "Missing key %s in %s.", key, file.ToString())
	}
	if q := config.QuitTimeout; q != nil {
		Assert(*q >= 0, "Invalid quit_timeout %v in %s: it can't be negative.", *q, file.ToString())
	}
	if d := config.MacroDepth; d != nil {
		Assert(*d > 0, "Invalid macro_depth %d in %s: it must be positive.", *d, file.ToString())
	}
	names := map[string]bool{}
	for _, class := range config.Classes {
		Assert(classNameRegexp.MatchString(class.Name), "Invalid class name %q in %s.", class.Name, file.ToString())
		Assert(!names[class.Name], "Duplicate class %s in %s.", class.Name, file.ToString())
		names[class.Name] = true
		for _, path := range []string{class.Include, class.Icon} {
			Assert(path == "" || isClassPath(path), "Path %s of class %s in %s must start with pkg:// or res://.", path, class.Name, file.ToString())
		}
		Assert(class.Kind == "" || class.Kind == "ptr" || class.Kind == "ref", "Invalid kind %q of class %s in %s: it must be ptr or ref.", class.Kind, class.Name, file.ToString())
		Assert(class.Kind == "" || class.Include != "", "Class %s in %s has kind %s, which requires an include.", class.Name, file.ToString(), class.Kind)
	}

	return Package{
		Root:       root,
		Id:         root.Name(),
		Config:     config,
		BuildCache: root.Cd(packageBuildCacheDirName),
	}
}

// packageName describes the package at root in logs: its path, plus its id
// for the project root package, whose path doesn't show it.
func packageName(root Path) string {
	name := root.ToString()
	if name == "res://" {
		name += " [" + root.Name() + "]"
	}
	return name
}

// styledPackageName is packageName styled like `gd++ ls` shows packages.
func styledPackageName(root Path) string {
	name := root.ToString()
	styled := Styled(name, Bold, Cyan)
	if name == "res://" {
		styled += " " + Styled("["+root.Name()+"]", Gray)
	}
	return styled
}

// packageGarbage returns what cleaning the package at root deletes: its build
// cache, and with bin all libraries and .gdextension (.uid) files in root,
// whatever their names, since the package may have been moved or renamed.
func packageGarbage(root Path, bin bool) []Path {
	var paths []Path
	if cache := root.Cd(packageBuildCacheDirName); cache.Exists() {
		paths = append(paths, cache)
	}
	if bin {
		for _, child := range root.Ls() {
			if packageBinRegexp.MatchString(child.Name()) {
				paths = append(paths, child)
			}
		}
	}
	return paths
}

var packageBinRegexp = regexp.MustCompile(`^(lib.+\.(dll|so|dylib)|.+\.gdextension(\.uid)?)$`)

// cleanPackage deletes packageGarbage, saying so if there is any.
func cleanPackage(root Path, bin bool) {
	garbage := packageGarbage(root, bin)
	if len(garbage) == 0 {
		return
	}
	t := LogTask("Cleaning %s...", styledPackageName(root))
	for _, p := range garbage {
		p.Remove()
	}
	t.Done()
}

// LoadPackageAt reads the package at root, asserting root is a package
// root rather than a directory inside one.
func LoadPackageAt(root Path) Package {
	Assert(root.IsPackageRoot(), "There is no GD++ package at %s.", root.ToString())
	return LoadPackage(root)
}

// ListPackages returns all packages in the project, including the root and
// packages nested in other packages, sorted by path. It skips hidden
// directories, res://_gd++, and nested Godot projects.
func (p *Project) ListPackages() []Package {
	var pkgs []Package
	var walk func(dir Path)
	walk = func(dir Path) {
		if dir.IsPackageRoot() {
			pkgs = append(pkgs, LoadPackage(dir))
		}
		for _, child := range dir.Ls() {
			if child.IsDir() && !child.IsProjectRoot() && child != p.Root.Cd(checkedInDepsDirName) {
				walk(child)
			}
		}
	}
	walk(p.Root)
	return pkgs
}
