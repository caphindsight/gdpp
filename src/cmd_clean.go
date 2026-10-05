package main

// CmdClean deletes the build caches of packages, so their next build starts
// from scratch, and with --bin also all libraries and .gdextension files in
// their roots. Their hidden directories get a .gdignore file, if missing.
type CmdClean struct {
	Paths []string `arg:"positional" placeholder:"PATH" help:"clean the packages containing these paths; PATH/... cleans all packages inside PATH, e.g. res://... the whole project [default: ..., all packages in the current directory]"`
	Bin   bool     `arg:"--bin" help:"also delete all libraries and .gdextension files in the package roots"`
}

func (c *CmdClean) Run() {
	// Check every path before deleting anything.
	var pkgs []Package
	dirty := false
	for _, root := range ParsePackagePaths(c.Paths) {
		pkgs = append(pkgs, LoadPackage(root))
		dirty = dirty || len(packageGarbage(root, c.Bin)) > 0
	}
	if !dirty {
		LogInfo("Nothing to clean.")
	}
	for _, pkg := range pkgs {
		cleanPackage(pkg.Root, c.Bin)
		pkg.HideInGodot()
	}
}
