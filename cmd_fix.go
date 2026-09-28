package main

// CmdFix brings the project into its canonical state: it reformats the config
// files of the project and its packages (sorting their classes), writes or
// removes the GD++ blocks in their .gitignore files to match the VCS, and
// deletes leftover temporary files and empty cache directories.
type CmdFix struct{}

func (c *CmdFix) Run() {
	p := LoadProject(Cwd())
	changed := false
	remove := func(dir Path, what string) {
		dir.Remove()
		LogInfo("Deleted %s %s.", what, dir.ToString())
		changed = true
	}
	format := func(file Path, text string) {
		if file.Exists() && file.ReadString() != text {
			file.WriteString(text)
			LogInfo("Reformatted %s.", file.ToString())
			changed = true
		}
	}

	format(p.Root.Cd(projectConfigFileName), p.Config.Encode())
	for _, pkg := range p.ListPackages() {
		pkg.Config.SortClasses()
		format(pkg.Root.Cd(packageFileName), pkg.Config.Encode())
	}
	changed = SyncGitignores(p) || changed
	if temp := p.tempDir(); temp.Exists() {
		remove(temp, "the temporary directory")
	}
	changed = p.RemoveEmptyCacheDirs() || changed

	if !changed {
		LogInfo("The project is already tidy.")
		return
	}
	LogInfo("Success!")
}
