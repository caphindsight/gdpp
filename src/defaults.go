// defaults.go: naming conventions the tool assumes in projects.

package main

const (
	gdppVersion = "nightly" // the version of this CLI tool

	projectFileName       = "project.godot"
	projectConfigFileName = ".gd++proj"
	packageFileName       = ".gd++pkg"
	gitignoreFileName     = ".gitignore"
	exportPresetsFileName = "export_presets.cfg"
	gdignoreFileName      = ".gdignore" // makes Godot skip its directory

	// Dep caches: each lives in a subdirectory named after its kind (see
	// depKinds) of both the checked in and the ephemeral directory, at the
	// project root.
	checkedInDepsDirName = "_gd++"
	ephemeralDepsDirName = ".gd++cache"

	// Temporary directories live here, inside the ephemeral directory.
	tempDirName = "temp"
	// Engines compiled for gd++ test live here, inside the ephemeral directory.
	engineBuildsDirName = "godot"

	// A package's build cache, at the package root.
	packageBuildCacheDirName = ".gd++build"

	// Defaults for the optional keys in .gd++pkg.
	defaultPackageSyntax      = 0
	defaultPackageCppStandard = "c++20"
	defaultPackageMacroDepth  = 64
)
