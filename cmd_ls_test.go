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
	})
	withMemFS(t, "/games/my_game", tree)
	withTTY(t, false)
}

// lsHeader is the output of ls before the dependencies, in withLsProject.
const lsHeader = "" +
	"Project: My \"Game\"  [my_game]\n" +
	"  Godot: 4.3  *  GD++ CLI: nightly  *  VCS: none\n" +
	"\n" +
	"Dependencies\n"

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
	want := lsHeader +
		"  Godot C++ bindings:  10.0.0-stable  cached\n" +
		"                       9.1.0-stable   checked in\n" +
		"  Godot API specs:     4.3-stable     cached\n" +
		"  Godot engines:       none\n"
	for name, c := range map[string]CmdLs{"deps": {Deps: true}, "all": {All: true}} {
		t.Run(name, func(t *testing.T) {
			withLsProject(t)
			if out := captureStdout(t, c.Run); out != want {
				t.Errorf("output = %q, want %q", out, want)
			}
		})
	}
}

func TestLsCmdVCS(t *testing.T) {
	withLsProject(t)
	NewPath("/games/my_game").Cd(projectConfigFileName).WriteString(`vcs = "git"`)
	out := captureStdout(t, (&CmdLs{}).Run)
	if want := "  Godot: 4.3  *  GD++ CLI: nightly  *  VCS: git\n"; !strings.Contains(out, want) {
		t.Errorf("output = %q, want it to contain %q", out, want)
	}
}

func TestLsCmdPackages(t *testing.T) {
	withLsProject(t)
	NewPath("/games/my_game/foo").Cd(packageFileName).WriteString("bind = \"10.0.0-stable\"\nspec = \"4.3-stable\"\n")
	out := captureStdout(t, (&CmdLs{}).Run)
	want := lsHeader +
		"  Godot C++ bindings:  1 checked in  *  1 cached  *  1 unused\n" +
		"  Godot API specs:     1 cached\n" +
		"  Godot engines:       none\n" +
		"\n" +
		"Package res://foo\n" +
		"  Godot C++ bindings:  10.0.0-stable\n" +
		"  Godot API spec:      4.3-stable\n" +
		"  GD++ syntax:         0\n" +
		"  C++ standard:        c++20\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestLsPackages(t *testing.T) {
	withLsProject(t)
	root := NewPath("/games/my_game/foo")
	pkgs := []lsPackage{{
		Package: Package{Root: root, Config: PackageConfig{Bindings: "b", ApiSpec: "4.3-stable", Syntax: 0, CppStandard: "c++23"}},
		Classes: []lsClass{
			{"Tree", "foo/tree.h", root.Cd("icons", "tree.svg")},
			{"Bush", "foo/bush.h", root.Cd("icons", "bush.svg")},
			{"GrassPatch", "foo/grass_patch.h", Path{}},
		},
	}}
	pkgWant := "" +
		"\n" +
		"Package res://foo\n" +
		"  Godot C++ bindings:  b                  x missing\n" +
		"  Godot API spec:      4.3-stable\n" +
		"  GD++ syntax:         0\n" +
		"  C++ standard:        c++23\n" +
		"\n" +
		"  Classes              Include            Icon\n" +
		"  Tree                 foo/tree.h         res://foo/icons/tree.svg\n" +
		"  Bush                 foo/bush.h         x res://foo/icons/bush.svg\n" +
		"  GrassPatch           foo/grass_patch.h  none\n" +
		"\n" +
		"To fix: gd++ fetch --bind b\n"
	for _, c := range []struct {
		deps bool
		want string
	}{
		{false, lsHeader +
			"  Godot C++ bindings:  1 checked in  *  1 cached  *  2 unused\n" +
			"  Godot API specs:     1 cached\n" +
			"  Godot engines:       none\n" + pkgWant},
		{true, lsHeader +
			"  Godot C++ bindings:  10.0.0-stable  cached  *  unused\n" +
			"                       9.1.0-stable   checked in  *  unused\n" +
			"  Godot API specs:     4.3-stable     cached\n" +
			"  Godot engines:       none\n" + pkgWant},
	} {
		if got := lsProject(LoadProject(Cwd()), pkgs, c.deps); got != c.want {
			t.Errorf("deps %v: output = %q, want %q", c.deps, got, c.want)
		}
	}
}
