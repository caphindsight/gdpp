// cmd_ls_test.go: tests for cmd_ls.go. Runs on memFS, in the project from
// project_test.go.

package main

import (
	"maps"
	"os"
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
	if want := "  Godot:  4.3\n  VCS:    git\n"; !strings.Contains(out, want) {
		t.Errorf("output = %q, want it to contain %q", out, want)
	}
}

// withLsPackages adds packages res://foo, res://foo/inner (missing its
// bindings) and res://zed to withLsProject.
func withLsPackages(t *testing.T) {
	withLsProject(t)
	NewPath("/games/my_game/zed").CreateDirectory()
	for _, dir := range []string{"foo", "zed"} {
		NewPath("/games/my_game").Cd(dir, packageFileName).WriteString("bind = \"10.0.0-stable\"\nspec = \"4.3-stable\"\n")
	}
	NewPath("/games/my_game/foo/icons").Cd(packageFileName).WriteString("bind = \"b\"\nspec = \"4.3-stable\"\n")
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
	"  GD++ syntax:         0\n" +
	"  C++ standard:        c++20\n"

func TestLsCmdPackages(t *testing.T) {
	collapsed := lsDepsOut +
		"\n" +
		"Package: res://foo\n" +
		"Package: res://foo/icons  x missing dependencies\n" +
		"Package: res://zed\n" +
		"\n" +
		"To fix: gd++ fetch --missing\n"
	fooExpanded := lsDepsOut +
		"\n" + lsFooOut +
		"\n" +
		"Package: res://foo/icons  x missing dependencies\n" +
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
	NewPath("/games/my_game").Cd(packageFileName).WriteString("bind = \"b\"\nspec = \"4.3-stable\"\n")
	out := captureStdout(t, (&CmdLs{Path: "res://src"}).Run)
	want := "" +
		"Package: res:// [my_game]\n" +
		"  Godot C++ bindings:  b           x missing\n"
	if !strings.Contains(out, want) {
		t.Errorf("output = %q, want it to contain %q", out, want)
	}
	out = captureStdout(t, (&CmdLs{Path: "res://zed"}).Run)
	if want := "\nPackage: res:// [my_game]  x missing dependencies\nPackage: res://foo\n"; !strings.Contains(out, want) {
		t.Errorf("output = %q, want it to contain %q", out, want)
	}
}

func TestLsCmdOnePackage(t *testing.T) {
	withLsProject(t)
	NewPath("/games/my_game/foo").Cd(packageFileName).WriteString("bind = \"10.0.0-stable\"\nspec = \"4.3-stable\"\n\n" +
		"[[class]]\nname = \"Tree\"\ninclude = \"pkg://tree.h\"\nicon = \"res://foo/icons/tree.svg\"\n\n" +
		"[[class]]\nname = \"Bush\"\n")
	out := captureStdout(t, (&CmdLs{}).Run)
	want := "\n" + lsFooOut +
		"\n" +
		"  Classes              Include           Icon\n" +
		"  Tree                 res://foo/tree.h  res://foo/icons/tree.svg\n" +
		"  Bush                 none              none\n"
	if !strings.HasSuffix(out, want) {
		t.Errorf("output = %q, want it to end with %q", out, want)
	}
}

func TestLsCmdAllPackages(t *testing.T) {
	withLsPackages(t)
	out := captureStdout(t, (&CmdLs{Pkgs: true}).Run)
	want := lsDepsOut +
		"\n" + lsFooOut +
		"\n" +
		"Package: res://foo/icons\n" +
		"  Godot C++ bindings:  b           x missing\n" +
		"  Godot API spec:      4.3-stable\n" +
		"  GD++ syntax:         0\n" +
		"  C++ standard:        c++20\n" +
		"\n" +
		strings.Replace(lsFooOut, "foo", "zed", 1) +
		"\n" +
		"To fix: gd++ fetch --missing\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestLsCmdInvalidArgs(t *testing.T) {
	for name, c := range map[string]CmdLs{"pkgs": {Path: "foo", Pkgs: true}, "all": {Path: "foo", All: true}} {
		t.Run(name, func(t *testing.T) {
			if os.Getenv("GDPP_FAIL_HELPER") == "1" {
				isTTY = false
				c.Run()
				return
			}
			out, code := runFailHelper(t, t.Name())
			if want := "[x] Invalid arguments: -l/--pkgs and -a/--all cannot be used with a path.\n"; code != 1 || out != want {
				t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, want)
			}
		})
	}
}

func TestLsPackages(t *testing.T) {
	withLsProject(t)
	root := NewPath("/games/my_game/foo")
	pkgs := []lsPackage{{
		Package:  Package{Root: root, Config: PackageConfig{Bindings: "b", ApiSpec: "4.3-stable", Syntax: 0, CppStandard: "c++23"}},
		Expanded: true,
		Classes: []lsClass{
			{"Tree", root.Cd("tree.h"), root.Cd("icons", "tree.svg")},
			{"Bush", root.Cd("bush.h"), root.Cd("icons", "bush.svg")},
			{"GrassPatch", Path{}, Path{}},
		},
	}}
	pkgWant := "" +
		"\n" +
		"Package: res://foo\n" +
		"  Godot C++ bindings:  b                   x missing\n" +
		"  Godot API spec:      4.3-stable\n" +
		"  GD++ syntax:         0\n" +
		"  C++ standard:        c++23\n" +
		"\n" +
		"  Classes              Include             Icon\n" +
		"  Tree                 res://foo/tree.h    res://foo/icons/tree.svg\n" +
		"  Bush                 x res://foo/bush.h  x res://foo/icons/bush.svg\n" +
		"  GrassPatch           none                none\n" +
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
		if got := lsProject(LoadProject(Cwd()), pkgs, c.deps); got != c.want {
			t.Errorf("deps %v: output = %q, want %q", c.deps, got, c.want)
		}
	}
}
