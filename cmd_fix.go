package main

// CmdFix brings the project into its canonical state: it reformats the config
// file, and deletes leftover temporary files and empty cache directories.
type CmdFix struct{}

func (c *CmdFix) Run() {
	p := LoadProject(Cwd())
	changed := false
	remove := func(dir Path, what string) {
		dir.Remove()
		LogInfo("Deleted %s %s.", what, dir.ToString())
		changed = true
	}

	if file := p.Root.Cd(projectConfigFileName); file.Exists() && file.ReadString() != p.Config.Encode() {
		file.WriteString(p.Config.Encode())
		LogInfo("Reformatted %s.", file.ToString())
		changed = true
	}
	if temp := p.tempDir(); temp.Exists() {
		remove(temp, "the temporary directory")
	}
	// Caches first, since deleting them may leave their parents empty.
	var dirs []Path
	for _, cache := range []ProjectDepCache{p.BindingsCache, p.ApiSpecsCache, p.EnginesCache} {
		dirs = append(dirs, cache.CheckedInDir, cache.EphemeralDir)
	}
	dirs = append(dirs, p.Root.Cd(checkedInDepsDirName), p.Root.Cd(ephemeralDepsDirName))
	for _, dir := range dirs {
		if dir.IsEmptyDir() {
			remove(dir, "the empty directory")
		}
	}

	if !changed {
		LogInfo("The project is already tidy.")
		return
	}
	LogInfo("Success!")
}
