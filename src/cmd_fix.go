package main

// CmdFix brings the project into its canonical state: it reformats the config
// files of the project and its packages (sorting their lists), writes or
// removes the GD++ blocks in their .gitignore files to match the VCS, and
// GD++'s filters in the export presets to match the config, deletes
// leftover temporary files and empty cache directories, and writes missing
// .gdignore files into the caches and the packages' hidden directories.
type CmdFix struct{}

func (c *CmdFix) Run() {
	p := LoadProject(Cwd())
	changed := false
	format := func(file Path, text string) {
		if file.Exists() && file.ReadString() != text {
			file.WriteString(text)
			LogInfo("Reformatted %s.", file.ToString())
			changed = true
		}
	}

	format(p.Root.Cd(projectConfigFileName), p.Config.Encode())
	for _, pkg := range p.ListPackages() {
		pkg.Config.Sort()
		format(pkg.Root.Cd(packageFileName), pkg.Config.Encode())
	}
	changed = SyncGitignores(p) || changed
	changed = SyncExportPresets(p) || changed
	if temp := p.tempDir(); temp.RemoveIfExists() {
		LogInfo("Deleted the temporary directory %s.", temp.ToString())
		changed = true
	}
	changed = p.RemoveEmptyCacheDirs() || changed
	for _, dir := range []Path{p.Root.Cd(checkedInDepsDirName), p.Root.Cd(ephemeralDepsDirName)} {
		if dir.IsDir() && dir.IgnoreInGodot() {
			LogInfo("Created %s.", dir.Cd(gdignoreFileName).ToString())
			changed = true
		}
	}
	for _, pkg := range p.ListPackages() {
		changed = pkg.HideInGodot() || changed
	}

	if !changed {
		LogInfo("The project is already tidy.")
		return
	}
	LogInfo("Success!")
}
