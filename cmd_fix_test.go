// cmd_fix_test.go: tests for cmd_fix.go. Runs on memFS, in the project from
// project_test.go.

package main

import (
	"maps"
	"reflect"
	"strings"
	"testing"
)

// runFix runs gd++ fix on testProjectTree plus extra, and returns its output
// and the trees under the project root before and after.
func runFix(t *testing.T, extra map[string]string) (out string, before, after map[string]string) {
	tree := maps.Clone(testProjectTree)
	maps.Copy(tree, extra)
	m := withMemFS(t, "/games/my_game", tree)
	withQuiet(t, false)
	withTTY(t, false)
	before = subtree(m.tree(), "/games/my_game/")
	out = captureStderr(t, (&CmdFix{}).Run)
	return out, before, subtree(m.tree(), "/games/my_game/")
}

func TestFix(t *testing.T) {
	out, before, after := runFix(t, map[string]string{
		"/games/my_game/.gd++proj/temp/abc/x": "x",
		"/games/my_game/.gd++proj/bind/":      "",
		"/games/my_game/.gd++proj/spec/":      "",
		"/games/my_game/_gd++proj/bind/":      "",
		"/games/my_game/_gd++proj/spec/.keep": "",
		"/games/my_game/_gd++proj/engine/":    "",
	})
	want := "" +
		"[>] Deleted the temporary directory res://.gd++proj/temp.\n" +
		"[>] Deleted the empty directory res://_gd++proj/bind.\n" +
		"[>] Deleted the empty directory res://.gd++proj/bind.\n" +
		"[>] Deleted the empty directory res://.gd++proj/spec.\n" +
		"[>] Deleted the empty directory res://_gd++proj/engine.\n" +
		"[>] Deleted the empty directory res://.gd++proj.\n" +
		"[>] Success!\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	wantTree := maps.Clone(before)
	for path := range wantTree {
		if strings.HasPrefix(path, ".gd++proj/") || path == "_gd++proj/bind/" || path == "_gd++proj/engine/" {
			delete(wantTree, path)
		}
	}
	if !reflect.DeepEqual(after, wantTree) {
		t.Errorf("tree = %v, want %v", after, wantTree)
	}
}

func TestFixConfig(t *testing.T) {
	out, _, after := runFix(t, map[string]string{"/games/my_game/" + projectConfigFileName: `vcs="git"`})
	if want := "[>] Reformatted res://gd++proj.toml.\n[>] Created res://.gitignore.\n[>] Success!\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got, want := after[projectConfigFileName], "vcs = \"git\"\n"; got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
}

func TestFixNothingToDo(t *testing.T) {
	out, before, after := runFix(t, map[string]string{
		"/games/my_game/.gd++proj/bind/4.3/a.h":   "a",
		"/games/my_game/" + projectConfigFileName: "vcs = \"git\"\n",
		"/games/my_game/.gitignore":               projectBlock,
	})
	if want := "[>] The project is already tidy.\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if !reflect.DeepEqual(after, before) {
		t.Errorf("tree = %v, want it unchanged: %v", after, before)
	}
}

func TestFixPackageConfig(t *testing.T) {
	out, _, after := runFix(t, map[string]string{"/games/my_game/src/pkg/" + packageFileName: `bind="4.3"` + "\n" + `spec="4.3"`})
	if want := "[>] Reformatted res://src/pkg/gd++pkg.toml.\n[>] Success!\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	want := "bind = \"4.3\"\nspec = \"4.3\"\nsyntax = 0\nstd = \"c++20\"\n"
	if got := after["src/pkg/"+packageFileName]; got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
}

func TestFixGitignores(t *testing.T) {
	out, _, after := runFix(t, map[string]string{
		"/games/my_game/src/pkg/" + packageFileName:   syncPkgConfig + "syntax = 0\nstd = \"c++20\"\n",
		"/games/my_game/.gitignore":                   "user\n\n" + projectBlock,
		"/games/my_game/src/pkg/" + gitignoreFileName: packageBlock,
	})
	want := "[>] Updated res://.gitignore.\n[>] Deleted res://src/pkg/.gitignore.\n[>] Success!\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got := after[gitignoreFileName]; got != "user\n" {
		t.Errorf(".gitignore = %q, want %q", got, "user\n")
	}
}

func TestFixPackageClasses(t *testing.T) {
	config := "bind = \"4.3\"\nspec = \"4.3\"\nsyntax = 0\nstd = \"c++20\"\n"
	out, _, after := runFix(t, map[string]string{"/games/my_game/src/pkg/" + packageFileName: config +
		"\n[[class]]\n  name = \"B\"\n\n[[class]]\n  name = \"A\"\n"})
	if want := "[>] Reformatted res://src/pkg/gd++pkg.toml.\n[>] Success!\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	want := config + "\n[[class]]\n  name = \"A\"\n\n[[class]]\n  name = \"B\"\n"
	if got := after["src/pkg/"+packageFileName]; got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
}
