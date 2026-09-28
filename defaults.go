// defaults.go: naming conventions the tool assumes in projects.

package main

const (
	gdppVersion = "nightly" // the version of this CLI tool

	projectFileName       = "project.godot"
	projectConfigFileName = "gd++proj.toml"
	packageFileName       = "gd++pkg.toml"
	gitignoreFileName     = ".gitignore"

	// Dep caches: each lives in a subdirectory of both the checked in and the
	// ephemeral directory, at the project root.
	checkedInDepsDirName = "_gd++proj"
	ephemeralDepsDirName = ".gd++proj"
	bindingsCacheDirName = "bind"
	apiSpecsCacheDirName = "spec"
	enginesCacheDirName  = "engine"

	// Temporary directories live here, inside the ephemeral directory.
	tempDirName = "temp"

	// A package's build cache, at the package root.
	packageBuildCacheDirName = ".gd++pkg"

	// Defaults for the optional keys in gd++pkg.toml.
	defaultPackageSyntax      = 0
	defaultPackageCppStandard = "c++20"
)
