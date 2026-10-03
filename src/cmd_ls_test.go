// cmd_ls_test.go: tests for cmd_ls.go. Runs on memFS, in the project from
// project_test.go.

package main

import (
	"maps"
	"strings"
	"testing"
)

// withLsProject sets up testProjectTree with some cached dependencies.
func withLsProject(t *testing.T) {
	tree := maps.Clone(testProjectTree)
	maps.Copy(tree, map[string]string{
		"/games/my_game/.gd++proj/bind/10.0.0-stable/a.h": "a",
		"/games/my_game/_gd++proj/bind/9.1.0-stable/a.h":  "a",
		"/games/my_game/.gd++proj/spec/4.3-stable/a.json": "a",
		"/games/my_game/foo/icons/tree.svg":               "svg",
		"/games/my_game/foo/tree.h":                       "h",
	})
	withMemFS(t, "/games/my_game", tree)
	withTTY(t, false)
}

// lsHeader is the output of ls before the dependencies, in withLsProject.
const lsHeader = "" +
	"Project: My \"Game\" [my_game]\n" +
	"  Godot:  4.3\n" +
	"  VCS:    none\n" +
	"\n" +
	"Dependencies:\n"

func TestLsCmd(t *testing.T) {
	withLsProject(t)
	out := captureStdout(t, (&CmdLs{}).Run)
	want := lsHeader +
		"  Godot C++ bindings:  1 checked in  *  1 cached\n" +
		"  Godot API specs:     1 cached\n" +
		"  Godot engines:       none\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestLsCmdDeps(t *testing.T) {
	deps := "" +
		"  Godot C++ bindings:  10.0.0-stable  cached\n" +
		"                       9.1.0-stable   checked in\n" +
		"  Godot API specs:     4.3-stable     cached\n" +
		"  Godot engines:       none\n"
	allHeader := "" +
		"Project: My \"Game\" [my_game]\n" +
		"  Godot:           4.3\n" +
		"  VCS:             none\n" +
		"  Manage presets:  yes\n" +
		"\n" +
		"Dependencies:\n"
	for name, c := range map[string]struct {
		cmd  CmdLs
		want string
	}{"deps": {CmdLs{Deps: true}, lsHeader + deps}, "all": {CmdLs{All: true}, allHeader + deps}} {
		t.Run(name, func(t *testing.T) {
			withLsProject(t)
			if out := captureStdout(t, c.cmd.Run); out != c.want {
				t.Errorf("output = %q, want %q", out, c.want)
			}
		})
	}
}

func TestLsCmdVCS(t *testing.T) {
	withLsProject(t)
	NewPath("/games/my_game").Cd(projectConfigFileName).WriteString(`vcs = "git"`)
	out := captureStdout(t, (&CmdLs{}).Run)
	if want := "  Godot:  4.3\n  VCS:    git\n"; !strings.Contains(out, want) {
		t.Errorf("output = %q, want it to contain %q", out, want)
	}
}

func TestLsCmdPresets(t *testing.T) {
	withLsProject(t)
	NewPath("/games/my_game").Cd(projectConfigFileName).WriteString("presets = false")
	out := captureStdout(t, (&CmdLs{All: true}).Run)
	if want := "  Manage presets:  no\n"; !strings.Contains(out, want) {
		t.Errorf("output = %q, want it to contain %q", out, want)
	}
}

// withLsPackages adds packages res://foo, res://foo/inner (missing its
// bindings) and res://zed to withLsProject.
func withLsPackages(t *testing.T) {
	withLsProject(t)
	NewPath("/games/my_game/zed").CreateDirectory()
	for _, dir := range []string{"foo", "zed"} {
		NewPath("/games/my_game").Cd(dir, packageFileName).WriteString("bind = \"10.0.0-stable\"\nspec = \"4.3-stable\"\nsyntax = 1\n")
	}
	NewPath("/games/my_game/foo/icons").Cd(packageFileName).WriteString("bind = \"b\"\nspec = \"4.3-stable\"\nsyntax = 1\n")
}

// lsDepsOut is the dependencies section of ls in withLsPackages.
const lsDepsOut = lsHeader +
	"  Godot C++ bindings:  1 checked in  *  1 cached  *  1 unused\n" +
	"  Godot API specs:     1 cached\n" +
	"  Godot engines:       none\n"

// lsFooOut is res://foo, expanded.
const lsFooOut = "" +
	"Package: res://foo\n" +
	"  Godot C++ bindings:  10.0.0-stable\n" +
	"  Godot API spec:      4.3-stable\n" +
	"  GD++ syntax:         1\n" +
	"  C++ standard:        c++20\n" +
	"  Class prefix:        Foo\n" +
	"  Quit timeout:        1 second\n" +
	"  Hot reload:          on\n"

func TestLsCmdPackages(t *testing.T) {
	collapsed := lsDepsOut +
		"\n" +
		"Package: res://foo\n" +
		"Package: res://foo/icons  missing dependencies\n" +
		"Package: res://zed\n" +
		"\n" +
		"To fix: gd++ fetch --missing\n"
	fooExpanded := lsDepsOut +
		"\n" + lsFooOut +
		"\n" +
		"Package: res://foo/icons  missing dependencies\n" +
		"Package: res://zed\n" +
		"\n" +
		"To fix: gd++ fetch --missing\n"
	for name, c := range map[string]struct {
		cmd  CmdLs
		cwd  string
		want string
	}{
		"root":    {CmdLs{}, "/games/my_game", collapsed},
		"cwd":     {CmdLs{}, "/games/my_game/foo", fooExpanded},
		"path":    {CmdLs{Path: "foo"}, "/games/my_game", fooExpanded},
		"inner":   {CmdLs{Path: "res://foo"}, "/games/my_game/foo/icons", fooExpanded},
		"not_pkg": {CmdLs{Path: "res://src"}, "/games/my_game/foo", collapsed},
	} {
		t.Run(name, func(t *testing.T) {
			withLsPackages(t)
			fsys.(*memFS).cwd = c.cwd
			if out := captureStdout(t, c.cmd.Run); out != c.want {
				t.Errorf("output = %q, want %q", out, c.want)
			}
		})
	}
}

func TestLsCmdRootPackage(t *testing.T) {
	withLsPackages(t)
	NewPath("/games/my_game").Cd(packageFileName).WriteString("bind = \"b\"\nspec = \"4.3-stable\"\nsyntax = 1\n")
	out := captureStdout(t, (&CmdLs{Path: "res://src"}).Run)
	want := "" +
		"Package: res:// [my_game]\n" +
		"  Godot C++ bindings:  b           missing\n"
	if !strings.Contains(out, want) {
		t.Errorf("output = %q, want it to contain %q", out, want)
	}
	out = captureStdout(t, (&CmdLs{Path: "res://zed"}).Run)
	if want := "\nPackage: res:// [my_game]  missing dependencies\nPackage: res://foo\n"; !strings.Contains(out, want) {
		t.Errorf("output = %q, want it to contain %q", out, want)
	}
}

func TestLsCmdOnePackage(t *testing.T) {
	withLsProject(t)
	NewPath("/games/my_game/foo").Cd(packageFileName).WriteString("bind = \"10.0.0-stable\"\nspec = \"4.3-stable\"\nsyntax = 1\n\n" +
		"[[class]]\nname = \"Tree\"\ninclude = \"pkg://tree.h\"\nicon = \"res://foo/icons/tree.svg\"\n\n" +
		"[[class]]\nname = \"Bush\"\ntool = true\n")
	out := captureStdout(t, (&CmdLs{}).Run)
	want := "\n" + lsFooOut +
		"\n" +
		"  Classes\n" +
		"                      Bush    @tool\n" +
		"    res://foo/tree.h  Tree    @icon\n"
	if !strings.HasSuffix(out, want) {
		t.Errorf("output = %q, want it to end with %q", out, want)
	}
}

func TestLsCmdGdppClasses(t *testing.T) {
	withLsProject(t)
	foo := NewPath("/games/my_game/foo")
	foo.Cd(packageFileName).WriteString("bind = \"10.0.0-stable\"\nspec = \"4.3-stable\"\nsyntax = 1\n\n" +
		"[[class]]\nname = \"Tree\"\ninclude = \"pkg://tree.h\"\nicon = \"pkg://icons/tree.svg\"\nkind = \"ref\"\n\n[[class]]\nname = \"Player\"\n")
	foo.Cd("player.gd++").WriteString("@icon(\"pkg://icons/tree.svg\")\n@tool\nclass_name Player\nextends Node\n")
	foo.Cd("icons", "helper.gg").WriteString("class Helper {}\nenum Mood { HAPPY }\n")
	foo.Cd("broken.gdpp").WriteString("fun f() {}\n")
	foo.Cd("spawner.gd++").WriteString("@game_only\n@trace\nclass_name Spawner\nextends Node3D\n\n" +
		"@profile\n@icon(\"pkg://icons/gone.svg\")\nclass Wave {\n  extends Resource\n}\n")
	out := captureStdout(t, (&CmdLs{}).Run)
	want := "" +
		"  C++ standard:        c++20\n" +
		"  Class prefix:        Foo\n" +
		"  Quit timeout:        1 second\n" +
		"  Hot reload:          on\n" +
		"\n" +
		"  Classes\n" +
		"                           Player: declared twice\n" +
		"    res://foo/tree.h       Tree                                        [ref] @icon\n" +
		"    pkg://icons/helper.gg  Helper                  extends RefCounted\n" +
		"    pkg://player.gd++      Player: declared twice  extends Node        @tool @icon\n" +
		"    pkg://spawner.gd++     Spawner                 extends Node3D      @game_only\n" +
		"                           Wave                    extends Resource    @icon (missing)\n" +
		"    pkg://broken.gdpp      has errors              see gd++ build\n"
	if !strings.HasSuffix(out, want) {
		t.Errorf("output = %q, want it to end with %q", out, want)
	}
}

func TestLsCmdAllPackages(t *testing.T) {
	want := lsDepsOut +
		"\n" + lsFooOut +
		"\n" +
		"Package: res://foo/icons\n" +
		"  Godot C++ bindings:  b           missing\n" +
		"  Godot API spec:      4.3-stable\n" +
		"  GD++ syntax:         1\n" +
		"  C++ standard:        c++20\n" +
		"  Class prefix:        Icons\n" +
		"  Quit timeout:        1 second\n" +
		"  Hot reload:          on\n" +
		"\n" +
		strings.NewReplacer("foo", "zed", "Foo", "Zed").Replace(lsFooOut) +
		"\n" +
		"To fix: gd++ fetch --missing\n"
	// With a path, from outside the project.
	for _, c := range []struct{ cwd, path string }{{"/games/my_game", ""}, {"/games", "my_game/foo"}} {
		withLsPackages(t)
		fsys.(*memFS).cwd = c.cwd
		if out := captureStdout(t, (&CmdLs{Path: c.path, Pkgs: true}).Run); out != want {
			t.Errorf("cwd %s, path %q: output = %q, want %q", c.cwd, c.path, out, want)
		}
	}
}

func TestLsPackages(t *testing.T) {
	withLsProject(t)
	root := NewPath("/games/my_game/foo")
	pkgs := []lsPackage{{
		Package:  Package{Root: root, Config: PackageConfig{Bindings: "b", ApiSpec: "4.3-stable", Syntax: 0, CppStandard: "c++23", Prefix: "Pk", QuitTimeout: ptr(2.5)}},
		Expanded: true,
		Classes: []lsClass{
			{Name: "Tree", File: root.Cd("tree.h"), Icon: root.Cd("icons", "tree.svg")},
			{Name: "Bush", File: root.Cd("bush.h"), Icon: root.Cd("icons", "bush.svg")},
			{Name: "GrassPatch"},
		},
	}}
	pkgWant := "" +
		"\n" +
		"Package: res://foo\n" +
		"  Godot C++ bindings:  b                                missing\n" +
		"  Godot API spec:      4.3-stable\n" +
		"  GD++ syntax:         0 (nightly, not for production)\n" +
		"  C++ standard:        c++23\n" +
		"  Class prefix:        Pk\n" +
		"  Quit timeout:        2.5 seconds\n" +
		"  Hot reload:          on\n" +
		"\n" +
		"  Classes\n" +
		"                                GrassPatch\n" +
		"    res://foo/tree.h            Tree          @icon\n" +
		"    res://foo/bush.h (missing)  Bush          @icon (missing)\n" +
		"\n" +
		"To fix: gd++ fetch --missing\n"
	for _, c := range []struct {
		deps bool
		want string
	}{
		{false, lsHeader +
			"  Godot C++ bindings:  1 checked in  *  1 cached  *  2 unused\n" +
			"  Godot API specs:     1 cached\n" +
			"  Godot engines:       none\n" + pkgWant},
		{true, lsHeader +
			"  Godot C++ bindings:  10.0.0-stable  cached      unused\n" +
			"                       9.1.0-stable   checked in  unused\n" +
			"  Godot API specs:     4.3-stable     cached\n" +
			"  Godot engines:       none\n" + pkgWant},
	} {
		if got := lsProject(LoadProject(Cwd()), pkgs, c.deps, false); got != c.want {
			t.Errorf("deps %v: output = %q, want %q", c.deps, got, c.want)
		}
	}
}
