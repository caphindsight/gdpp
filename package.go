// package.go: Package, a GD++ package described by its gd++pkg.toml file.

package main

import (
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// Package is a GD++ package: a directory inside a project with a
// gd++pkg.toml file.
type Package struct {
	Root       Path
	Id         string // name of the root directory
	Config     PackageConfig
	BuildCache Path // <root>/.gd++pkg
}

// PackageConfig holds the settings from a package's gd++pkg.toml.
type PackageConfig struct {
	Bindings    string         `toml:"bind"` // mandatory
	ApiSpec     string         `toml:"spec"` // mandatory
	Syntax      int            `toml:"syntax"`
	CppStandard string         `toml:"std"`
	Classes     []PackageClass `toml:"class,omitempty"`
}

// PackageClass is a C++ class the package exposes to Godot. Its paths are
// package-relative (pkg://) or project-relative (res://).
type PackageClass struct {
	Name    string `toml:"name"`
	Include string `toml:"include,omitempty"` // the header declaring the class
	Icon    string `toml:"icon,omitempty"`
}

var classNameRegexp = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// isClassPath reports whether s is a pkg:// or res:// path.
func isClassPath(s string) bool {
	return strings.HasPrefix(s, "pkg://") || strings.HasPrefix(s, "res://")
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
	var b strings.Builder
	Check(toml.NewEncoder(&b).Encode(c), "Failed to encode the package config")
	return b.String()
}

// LoadPackage reads the package containing p, which must be inside a
// project. It doesn't create any directories.
func LoadPackage(p Path) Package {
	root := GetPackageRoot(p)
	GetProjectRoot(root)
	file := root.Cd(packageFileName)

	config := DefaultPackageConfig()
	meta, err := toml.Decode(file.ReadString(), &config)
	Check(err, "Failed to parse %s", file.ToString())
	if unknown := meta.Undecoded(); len(unknown) > 0 {
		LogFatal("Unknown key %s in %s.", unknown[0], file.ToString())
	}
	for _, key := range []string{"bind", "spec"} {
		Assert(meta.IsDefined(key), "Missing key %s in %s.", key, file.ToString())
	}
	names := map[string]bool{}
	for _, class := range config.Classes {
		Assert(classNameRegexp.MatchString(class.Name), "Invalid class name %q in %s.", class.Name, file.ToString())
		Assert(!names[class.Name], "Duplicate class %s in %s.", class.Name, file.ToString())
		names[class.Name] = true
		for _, path := range []string{class.Include, class.Icon} {
			Assert(path == "" || isClassPath(path), "Path %s of class %s in %s must start with pkg:// or res://.", path, class.Name, file.ToString())
		}
	}

	return Package{
		Root:       root,
		Id:         root.Name(),
		Config:     config,
		BuildCache: root.Cd(packageBuildCacheDirName),
	}
}

// ListPackages returns all packages in the project, including the root and
// packages nested in other packages, sorted by path. It skips hidden
// directories, res://_gd++proj, and nested Godot projects.
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
