// package.go: Package, a GD++ package described by its gd++pkg.toml file.

package main

import "github.com/BurntSushi/toml"

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
	Bindings    string `toml:"bind"` // mandatory
	ApiSpec     string `toml:"spec"` // mandatory
	Syntax      int    `toml:"syntax"`
	CppStandard string `toml:"std"`
}

// DefaultPackageConfig returns the config with defaults for the optional
// keys; the mandatory keys are left empty.
func DefaultPackageConfig() PackageConfig {
	return PackageConfig{Syntax: defaultPackageSyntax, CppStandard: defaultPackageCppStandard}
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

	return Package{
		Root:       root,
		Id:         root.Name(),
		Config:     config,
		BuildCache: root.Cd(packageBuildCacheDirName),
	}
}

// ListPackages returns all packages in the project, sorted by path. It
// skips hidden directories, res://_gd++proj, nested Godot projects, and
// the insides of packages.
func (p *Project) ListPackages() []Package {
	var pkgs []Package
	var walk func(dir Path)
	walk = func(dir Path) {
		for _, child := range dir.Ls() {
			switch {
			case !child.IsDir() || child.IsProjectRoot() || child == p.Root.Cd(checkedInDepsDirName):
			case child.IsPackageRoot():
				pkgs = append(pkgs, LoadPackage(child))
			default:
				walk(child)
			}
		}
	}
	walk(p.Root)
	return pkgs
}
