// package.go: Package, a GD++ package described by its .gd++pkg file.

package main

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Package is a GD++ package: a directory inside a project with a
// .gd++pkg file.
type Package struct {
	Root       Path
	Id         string // name of the root directory
	Config     PackageConfig
	BuildCache Path // <root>/.gd++build
}

// PackageConfig holds the settings from a package's .gd++pkg.
type PackageConfig struct {
	Bindings    string         `toml:"bind"`             // mandatory
	ApiSpec     string         `toml:"spec"`             // mandatory
	Engine      string         `toml:"engine,omitempty"` // the engine that runs the tests; default: none
	Syntax      int            `toml:"syntax"`
	CppStandard string         `toml:"std"`
	Prefix      string         `toml:"prefix,omitempty"`       // default: the package ID in PascalCase
	QuitTimeout *float64       `toml:"quit_timeout,omitempty"` // seconds; default: 1
	HotReload   *bool          `toml:"hot_reload,omitempty"`   // default: true
	MacroDepth  *int           `toml:"macro_depth,omitempty"`  // default: defaultPackageMacroDepth
	Hidden      []string       `toml:"hide,omitempty"`         // package-relative directories Godot skips
	Names       ProjectNames   `toml:"names,omitempty"`        // the C++ names generated from project.godot, see names.go
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

// TestsClass returns the name of the package's runner of tests, which gd++
// test runs as Godot's main loop, e.g. FooTests.
func (pkg Package) TestsClass() string {
	return pkg.Prefix() + "Tests"
}

// GpuArrayClass returns the name of the package's class of GPU arrays, which
// GpuArray types name, e.g. FooGpuArray.
func (pkg Package) GpuArrayClass() string {
	return pkg.Prefix() + "GpuArray"
}

// GpuTextureClass returns the name of the package's internal class of the
// textures that shaders create, e.g. FooGpuTexture.
func (pkg Package) GpuTextureClass() string {
	return pkg.Prefix() + "GpuTexture"
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
	case "engine":
		return c.Engine, c.Engine != ""
	}
	return "", false
}

// DefaultPackageConfig returns the config with defaults for the optional
// keys; the mandatory keys are left empty.
func DefaultPackageConfig() PackageConfig {
	return PackageConfig{Syntax: defaultPackageSyntax, CppStandard: defaultPackageCppStandard}
}

// Sort sorts c's classes by name, and its hidden directories.
func (c *PackageConfig) Sort() {
	slices.SortFunc(c.Classes, func(a, b PackageClass) int { return strings.Compare(a.Name, b.Name) })
	slices.Sort(c.Hidden)
}

// hiddenDir returns s, a directory inside a package, as the hide setting
// stores it: a clean relative path such as "src/gen", without "pkg://". It
// returns "" if s is not inside the package, e.g. is absolute, a res:// path
// or starts with "..".
func hiddenDir(s string) string {
	s = path.Clean(strings.TrimPrefix(filepath.ToSlash(s), "pkg://"))
	if s == "." || s == ".." || strings.HasPrefix(s, "../") || path.IsAbs(s) || strings.Contains(s, ":") {
		return ""
	}
	return s
}

// HideInGodot writes a .gdignore file into the package's build cache and
// hidden directories, where they exist and lack one, and logs each. Returns
// whether it wrote any.
func (pkg Package) HideInGodot() bool {
	changed := false
	for _, dir := range append([]Path{pkg.BuildCache}, pkg.HiddenDirs()...) {
		if dir.IsDir() && dir.IgnoreInGodot() {
			LogInfo("Created %s.", dir.Cd(gdignoreFileName).ToString())
			changed = true
		}
	}
	return changed
}

// HiddenDirs returns the paths of the package's hidden directories.
func (pkg Package) HiddenDirs() []Path {
	var dirs []Path
	for _, d := range pkg.Config.Hidden {
		dirs = append(dirs, pkg.Root.Cd(d))
	}
	return dirs
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
	for i, d := range config.Hidden {
		Assert(hiddenDir(d) == d, "Invalid hidden directory %q in %s: it must be a relative path inside the package, e.g. src.", d, file.ToString())
		Assert(!slices.Contains(config.Hidden[:i], d), "Duplicate hidden directory %s in %s.", d, file.ToString())
	}

	return Package{
		Root:       root,
		Id:         root.Name(),
		Config:     config,
		BuildCache: root.Cd(packageBuildCacheDirName),
	}
}

// packageName names the package at root for its .gdextension.uid: its path,
// plus its id for the project root package, whose path doesn't show it.
func packageName(root Path) string {
	name := root.ToString()
	if name == "res://" {
		name += " [" + root.Name() + "]"
	}
	return name
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
	t := LogTask("Cleaning %s...", root.ToString())
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
// packages nested in other packages, sorted by path.
func (p *Project) ListPackages() []Package {
	var pkgs []Package
	for _, root := range packageRootsIn(p.Root) {
		pkgs = append(pkgs, LoadPackage(root))
	}
	return pkgs
}

// packageRootsIn returns the roots of all packages at or inside dir, which
// must be in a project, sorted by path. It skips hidden directories,
// res://_gd++, and nested Godot projects.
func packageRootsIn(dir Path) []Path {
	deps := GetProjectRoot(dir).Cd(checkedInDepsDirName)
	var roots []Path
	var walk func(dir Path)
	walk = func(dir Path) {
		if dir.IsPackageRoot() {
			roots = append(roots, dir)
		}
		for _, child := range dir.Ls() {
			if child.IsDir() && !child.IsProjectRoot() && child != deps {
				walk(child)
			}
		}
	}
	walk(dir)
	return roots
}

// ParsePackagePaths returns the roots of the packages containing the given
// paths, without duplicates, in order. A path ending in "..." names every
// package at or inside the directory before it instead: "..." or "./..."
// the current directory, "res://..." the whole project. No paths means
// "...".
func ParsePackagePaths(paths []string) []Path {
	if len(paths) == 0 {
		paths = []string{"..."}
	}
	var roots []Path
	for _, s := range paths {
		var found []Path
		if dir, ok := strings.CutSuffix(s, "..."); ok && (dir == "" || os.IsPathSeparator(dir[len(dir)-1])) {
			p := ParsePath(dir)
			Assert(p.IsDir(), "Path %s is not a directory.", p.ToString())
			found = packageRootsIn(p)
		} else {
			found = append(found, GetPackageRoot(ParsePath(s)))
		}
		for _, root := range found {
			if !slices.Contains(roots, root) {
				roots = append(roots, root)
			}
		}
	}
	return roots
}
