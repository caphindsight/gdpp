// project_test.go: tests for project.go.

package main

import (
	"maps"
	"os"
	"reflect"
	"testing"
)

// testProjectTree is a small project at /games/my_game, in withMemFS format.
var testProjectTree = map[string]string{
	"/games/my_game/" + projectFileName: `; Engine configuration file.
; It's best edited using the editor UI and not directly,

config_version=5

[application]

config/name="My \"Game\""
run/main_scene="res://main.tscn"
config/features=PackedStringArray("4.3", "Forward Plus")
config/icon="res://icon.svg"

[rendering]

config/name="Not this one"
`,
	"/games/my_game/src/": "",
}

func TestLoadProject(t *testing.T) {
	m := withMemFS(t, "/", testProjectTree)
	before := m.tree()

	root := NewPath("/games/my_game")
	want := Project{
		Root:         root,
		Id:           "my_game",
		Name:         `My "Game"`,
		GodotVersion: "4.3",
		Config:       DefaultProjectConfig(),
		Caches: []ProjectDepCache{
			{DepKind{"bind", "Godot C++ bindings", "Godot C++ bindings"}, root.Cd("_gd++/bind"), root.Cd(".gd++proj/bind")},
			{DepKind{"spec", "Godot API spec", "Godot API specs"}, root.Cd("_gd++/spec"), root.Cd(".gd++proj/spec")},
			{DepKind{"engine", "Godot engine", "Godot engines"}, root.Cd("_gd++/engine"), root.Cd(".gd++proj/engine")},
		},
	}
	if got := LoadProject(NewPath("/games/my_game/src")); !reflect.DeepEqual(got, want) {
		t.Errorf("LoadProject() = %+v, want %+v", got, want)
	}
	if got := m.tree(); !reflect.DeepEqual(got, before) {
		t.Errorf("LoadProject() changed the tree to %v, want %v", got, before)
	}
}

func TestLoadProjectConfig(t *testing.T) {
	tree := maps.Clone(testProjectTree)
	tree["/games/my_game/"+projectConfigFileName] = `vcs = "git"`
	withMemFS(t, "/", tree)
	if got, want := LoadProject(NewPath("/games/my_game")).Config, (ProjectConfig{VCS: "git", Presets: true}); got != want {
		t.Errorf("Config = %+v, want %+v", got, want)
	}
}

func TestLoadProjectConfigNoPresets(t *testing.T) {
	tree := maps.Clone(testProjectTree)
	tree["/games/my_game/"+projectConfigFileName] = "presets = false"
	withMemFS(t, "/", tree)
	if got, want := LoadProject(NewPath("/games/my_game")).Config, (ProjectConfig{VCS: "none"}); got != want {
		t.Errorf("Config = %+v, want %+v", got, want)
	}
}

func TestLoadProjectConfigUnknownKey(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		tree := maps.Clone(testProjectTree)
		tree["/games/my_game/"+projectConfigFileName] = "vcs = \"git\"\n[build]\njobs = 4\n"
		withMemFS(t, "/games/my_game", tree)
		withQuiet(t, false)
		LoadProject(NewPath("/games/my_game"))
		return
	}
	out, code := runFailHelper(t, "TestLoadProjectConfigUnknownKey")
	if want := "[x] Unknown key build in res://.gd++proj.toml.\n"; code != 1 || out != want {
		t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, want)
	}
}

func TestCreateTempDir(t *testing.T) {
	withMemFS(t, "/", testProjectTree)
	p := LoadProject(NewPath("/games/my_game"))

	a, b := p.CreateTempDir(), p.CreateTempDir()
	for _, dir := range []Path{a, b} {
		if want := NewPath("/games/my_game/.gd++proj/temp"); dir.BaseDir() != want {
			t.Errorf("CreateTempDir() = %v, want a child of %v", dir, want)
		}
		if !dir.IsDir() || len(dir.Ls()) != 0 {
			t.Errorf("CreateTempDir() = %v, want an empty directory", dir)
		}
	}
	if a == b {
		t.Errorf("CreateTempDir() returned %v twice", a)
	}
}

func TestProjectCleanup(t *testing.T) {
	m := withMemFS(t, "/", testProjectTree)
	before := m.tree()
	p := LoadProject(NewPath("/games/my_game"))

	p.Cleanup() // no temp directory yet: does nothing
	p.CreateTempDir().Cd("f.txt").WriteString("x")
	p.CreateTempDir()
	p.Cleanup()

	// .gd++proj itself is left behind, now empty but for its .gdignore.
	want := mergeTrees(before, map[string]string{"/games/my_game/.gd++proj/": "", "/games/my_game/.gd++proj/.gdignore": ""})
	if got := m.tree(); !reflect.DeepEqual(got, want) {
		t.Errorf("tree after Cleanup() = %v, want %v", got, want)
	}
}
