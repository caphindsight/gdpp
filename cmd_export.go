package main

import (
	"path/filepath"
	"slices"
	"strings"
)

// CmdExport sets up twins of the Godot editor's export presets, which export
// the project without its C++ and GD++ files: for each chosen preset, a
// GDExtension twin, which exports the GDExtension libraries it builds for the
// preset first, and with --engine an engine twin, which exports with export
// templates it builds with all packages compiled into the engine. Twins are
// made anew each time, from their original. The user's presets are never
// changed. The build caches of packages, and with --engine the project build
// cache, are cleaned first, unless --noclean is given. With --godot, it runs
// the twins' exports too. With --undo, it removes all twins instead.
type CmdExport struct {
	Gdext  bool     `arg:"--gdext" help:"build GDExtension libraries and set up GDExtension twins [default: without --engine]"`
	Engine string   `arg:"--engine" placeholder:"NAME" help:"build export templates with this Godot engine and set up engine twins"`
	Preset []string `arg:"--preset" placeholder:"NAME" help:"only set up twins of these presets [default: all Windows and Linux presets]"`
	BuildOptions
	NoClean bool   `arg:"--noclean" help:"don't clean the build caches first, for faster builds; for development only, avoid in ship builds, since a stale cache may pollute them"`
	Godot   string `arg:"--godot" placeholder:"PATH" help:"also export the twins with this Godot editor, headless"`
	Undo    bool   `arg:"--undo" help:"remove all twin presets"`
}

func (c *CmdExport) Run() {
	p := LoadProject(Cwd())
	file := p.Root.Cd(exportPresetsFileName)
	if c.Undo {
		Assert(!c.Gdext && c.Engine == "" && len(c.Preset) == 0 && c.BuildOptions == (BuildOptions{}) && !c.NoClean && c.Godot == "",
			"Invalid arguments: --undo cannot be used with other options.")
		undoTwins(file)
		return
	}
	suffixes := c.validate()
	Assert(file.IsFile(), "Failed to find %s, create an export preset in Godot's Export dialog first.", file.ToString())
	presets := parseExportPresets(file)
	chosen, targets := c.choose(presets)
	assertScons()
	if !c.NoClean {
		for _, pkg := range p.ListPackages() {
			cleanPackage(pkg.Root, false)
		}
		if c.Engine != "" {
			cleanProjectBuildCache(p)
		}
	}
	if c.Gdext {
		for _, pkg := range p.ListPackages() {
			buildExtension(p, pkg, c.BuildOptions, targets)
		}
	}
	if c.Engine != "" {
		o := c.BuildOptions
		o.engine = true
		buildEngine(p, c.Engine, o, targets)
	}
	var names []string
	for _, preset := range chosen {
		names = append(names, preset.name())
	}
	if writeIfChanged(file, presets.syncTwins(names, suffixes, engineBinDir(p)).render(true)) {
		LogWarn("Reload the project if the Godot editor has it open, or the editor may overwrite %s.", file.ToString())
	}
	if c.Godot != "" {
		c.export(p, chosen, suffixes[0])
	}
	LogInfo("Success!")
}

// validate asserts the arguments make sense together, and returns the twin
// kinds to set up.
func (c *CmdExport) validate() []string {
	c.BuildOptions.validate()
	if c.Engine == "" {
		c.Gdext = true
	}
	var suffixes []string
	if c.Gdext {
		suffixes = append(suffixes, extensionTwinSuffix)
	}
	if c.Engine != "" {
		assertDepName(c.Engine)
		Assert(c.Gdext || !c.Doc && !c.NoDoc, "Invalid arguments: --doc and --nodoc require --gdext when used with --engine.")
		suffixes = append(suffixes, engineTwinSuffix)
	}
	// Both twins of a preset export to its export path.
	Assert(c.Godot == "" || len(suffixes) == 1, "Invalid arguments: --godot cannot be used with both --gdext and --engine.")
	return suffixes
}

// choose returns the presets to set up twins of, and their build targets.
// Presets for platforms GD++ can't build for are skipped.
func (c *CmdExport) choose(e exportPresets) ([]exportPreset, []string) {
	originals := e.originals()
	for _, name := range c.Preset {
		Assert(slices.ContainsFunc(originals, func(p exportPreset) bool { return p.name() == name }), "There is no export preset named %q.", name)
	}
	var chosen []exportPreset
	var targets []string
	for _, preset := range originals {
		if len(c.Preset) > 0 && !slices.Contains(c.Preset, preset.name()) {
			continue
		}
		target := preset.target()
		if target == "" || strings.HasPrefix(target, "macos.") {
			LogWarn("Skipping the export preset %q, since GD++ can't export for its platform yet.", preset.name())
			continue
		}
		Assert(c.Godot == "" || preset.main.get("export_path") != "", "Set the export path of the preset %q in Godot's Export dialog.", preset.name())
		chosen = append(chosen, preset)
		if !slices.Contains(targets, target) {
			targets = append(targets, target)
		}
	}
	Assert(len(chosen) > 0, "Found no Windows or Linux export presets to set up twins of.")
	return chosen, targets
}

// export runs the exports of the twins of chosen of the kind suffix, with the
// Godot editor given by --godot, into their export paths.
func (c *CmdExport) export(p Project, chosen []exportPreset, suffix string) {
	godot := c.Godot
	if strings.ContainsAny(godot, `/\`) {
		godot = ParsePath(godot).GetOsPath()
	}
	mode := map[bool]string{true: "--export-release", false: "--export-debug"}[c.Ship]
	for _, preset := range chosen {
		exportPath := preset.main.get("export_path")
		out := p.Root.Cd(exportPath)
		if filepath.IsAbs(exportPath) {
			out = NewPath(exportPath)
		}
		out.CreateParentDirectory()
		name := preset.name() + suffix
		Exec("Exporting "+Styled(name, Bold)+"...", p.Root, godot, "--headless", "--path", ".", mode, name, exportPath)
	}
}

// undoTwins removes all twin presets from file, after confirming.
func undoTwins(file Path) {
	if !file.IsFile() {
		LogInfo("Nothing to undo.")
		return
	}
	presets := parseExportPresets(file)
	var twins []string
	for _, preset := range presets.presets {
		if preset.twinSuffix() != "" {
			twins = append(twins, `"`+preset.name()+`"`)
		}
	}
	if len(twins) == 0 {
		LogInfo("Nothing to undo.")
		return
	}
	Confirm("Remove the twin presets %s from %s?", strings.Join(twins, ", "), file.ToString())
	file.WriteString(exportPresets{presets.head, presets.originals()}.render(true))
	LogInfo("Success!")
}
