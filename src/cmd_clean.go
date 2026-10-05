package main

// CmdClean deletes the build caches of packages, so their next build starts
// from scratch, and with --bin also all libraries and .gdextension files in
// their roots.
type CmdClean struct {
	Paths []string `arg:"positional" placeholder:"PATH" help:"clean the packages containing these paths; PATH/... cleans all packages inside PATH, e.g. res://... the whole project [default: ..., all packages in the current directory]"`
	Bin   bool     `arg:"--bin" help:"also delete all libraries and .gdextension files in the package roots"`
}

func (c *CmdClean) Run() {
	// Check every path before deleting anything.
	var dirty []Path
	for _, root := range ParsePackagePaths(c.Paths) {
		GetProjectRoot(root)
		if len(packageGarbage(root, c.Bin)) > 0 {
			dirty = append(dirty, root)
		}
	}
	if len(dirty) == 0 {
		LogInfo("Nothing to clean.")
		return
	}
	for _, root := range dirty {
		cleanPackage(root, c.Bin)
	}
}
