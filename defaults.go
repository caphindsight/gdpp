// defaults.go: naming conventions the tool assumes in projects.

package main

const (
	projectFileName = "project.godot"
	packageFileName = "gd++pkg.toml"

	// Dep caches: each lives in a subdirectory of both the checked in and the
	// ephemeral directory, at the project root.
	checkedInDepsDirName = "_gd++proj"
	ephemeralDepsDirName = ".gd++proj"
	bindingsCacheDirName = "bind"
	apiSpecsCacheDirName = "spec"
	enginesCacheDirName  = "engine"

	// Temporary directories live here, inside the ephemeral directory.
	tempDirName = "temp"
)
