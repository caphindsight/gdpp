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

func TestFixNothingToDo(t *testing.T) {
	out, before, after := runFix(t, map[string]string{"/games/my_game/.gd++proj/bind/4.3/a.h": "a"})
	if want := "[>] The project is already tidy.\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if !reflect.DeepEqual(after, before) {
		t.Errorf("tree = %v, want it unchanged: %v", after, before)
	}
}
