// project.go: Project, a Godot project described by its project.godot file.

package main

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// Project is a Godot project, with settings read from its project.godot file.
type Project struct {
	Root         Path
	Id           string // name of the root directory
	Name         string // application/config/name
	GodotVersion string // e.g. "4.3", from application/config/features
	Config       ProjectConfig

	BindingsCache ProjectDepCache // Godot C++ bindings
	ApiSpecsCache ProjectDepCache // Godot API specs
	EnginesCache  ProjectDepCache // Godot engine binaries
}

// ProjectConfig holds the GD++ settings from res://gd++proj.toml.
type ProjectConfig struct {
	VCS string `toml:"vcs"`
}

// DefaultProjectConfig returns the config used when res://gd++proj.toml
// doesn't exist.
func DefaultProjectConfig() ProjectConfig {
	return ProjectConfig{VCS: "none"}
}

// Encode returns c in TOML format.
func (c ProjectConfig) Encode() string {
	var b strings.Builder
	Check(toml.NewEncoder(&b).Encode(c), "Failed to encode the project config")
	return b.String()
}

// project.godot is written by Godot's ConfigFile: "[section]" headers, then
// "key=value" lines in Godot's Variant text format, e.g.
//
//	[application]
//
//	config/name="My Game"
//	config/features=PackedStringArray("4.3", "Forward Plus")
var (
	projectNamePattern    = regexp.MustCompile(`(?m)^config/name="((?:[^"\\]|\\.)*)"`)
	projectVersionPattern = regexp.MustCompile(`(?m)^config/features=PackedStringArray\([^)]*?"(\d+\.\d+)"`)
	godotStringUnescaper  = strings.NewReplacer(`\"`, `"`, `\\`, `\`)
)

// CreateTempDir creates a new, randomly named, empty directory inside
// res://.gd++proj/temp, creating that directory too if needed.
func (p *Project) CreateTempDir() Path {
	bytes := make([]byte, 8)
	_, err := rand.Read(bytes)
	Check(err, "Failed to generate a random directory name")
	dir := p.tempDir().Cd(hex.EncodeToString(bytes))
	dir.CreateDirectory()
	return dir
}

// Cleanup deletes all directories made by CreateTempDir.
func (p *Project) Cleanup() {
	if temp := p.tempDir(); temp.Exists() {
		temp.Remove()
	}
}

// tempDir returns res://.gd++proj/temp, which holds temporary directories.
func (p *Project) tempDir() Path {
	return p.Root.Cd(ephemeralDepsDirName, tempDirName)
}

// RemoveEmptyCacheDirs deletes the empty dep cache directories, and their
// parents if that leaves them empty. Returns whether any was deleted.
func (p *Project) RemoveEmptyCacheDirs() bool {
	// Caches first, since deleting them may leave their parents empty.
	var dirs []Path
	for _, cache := range []ProjectDepCache{p.BindingsCache, p.ApiSpecsCache, p.EnginesCache} {
		dirs = append(dirs, cache.CheckedInDir, cache.EphemeralDir)
	}
	dirs = append(dirs, p.Root.Cd(checkedInDepsDirName), p.Root.Cd(ephemeralDepsDirName))
	removed := false
	for _, dir := range dirs {
		if dir.IsEmptyDir() {
			dir.Remove()
			LogInfo("Deleted the empty directory %s.", dir.ToString())
			removed = true
		}
	}
	return removed
}

// LoadProject reads the project containing p. It doesn't create any
// directories: those are created only when first needed.
func LoadProject(p Path) Project {
	root := GetProjectRoot(p)
	file := root.Cd(projectFileName)
	// Godot separates sections with an empty line, so the section runs from
	// its header to the first empty line after its contents.
	app := GetTextBlock(file.ReadString(), "[application]", "", false)

	name := projectNamePattern.FindStringSubmatch(app)
	Assert(name != nil, "Failed to find the project name in %s.", file.ToString())
	version := projectVersionPattern.FindStringSubmatch(app)
	Assert(version != nil, "Failed to find the Godot version in %s.", file.ToString())

	config := DefaultProjectConfig()
	if configFile := root.Cd(projectConfigFileName); configFile.Exists() {
		meta, err := toml.Decode(configFile.ReadString(), &config)
		Check(err, "Failed to parse %s", configFile.ToString())
		if unknown := meta.Undecoded(); len(unknown) > 0 {
			LogFatal("Unknown key %s in %s.", unknown[0], configFile.ToString())
		}
	}

	return Project{
		Root:         root,
		Id:           root.Name(),
		Name:         godotStringUnescaper.Replace(name[1]),
		GodotVersion: version[1],
		Config:       config,

		BindingsCache: newProjectDepCache(root, bindingsCacheDirName, "Godot C++ bindings"),
		ApiSpecsCache: newProjectDepCache(root, apiSpecsCacheDirName, "Godot API spec"),
		EnginesCache:  newProjectDepCache(root, enginesCacheDirName, "Godot engine"),
	}
}
