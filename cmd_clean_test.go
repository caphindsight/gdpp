// cmd_clean_test.go: tests for cmd_clean.go. Runs on memFS, in the project
// from project_test.go.

package main

import (
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// withCleanFS installs a project with packages at res:// and res://src/pkg,
// both with a build cache, and a package at res://src/other without one.
func withCleanFS(t *testing.T, cwd string) *memFS {
	tree := withPackages(map[string]string{"src/pkg": testRmPackage, "src/other": testRmPackage})
	tree["/games/my_game/"+packageFileName] = testRmPackage
	tree["/games/my_game/.gd++pkg/a.o"] = ""
	tree["/games/my_game/src/pkg/.gd++pkg/a.o"] = ""
	tree["/games/my_game/src/pkg/main.cpp"] = ""
	withQuiet(t, false)
	withTTY(t, false)
	return withMemFS(t, cwd, tree)
}

// buildCaches returns the build caches left in the project.
func buildCaches(m *memFS) []string {
	var caches []string
	for path := range subtree(m.tree(), "/games/my_game/") {
		if strings.HasSuffix(path, ".gd++pkg/") {
			caches = append(caches, path)
		}
	}
	slices.Sort(caches)
	return caches
}

func TestClean(t *testing.T) {
	m := withCleanFS(t, "/games/my_game/src/pkg")
	out := captureStderr(t, (&CmdClean{}).Run)
	if want := "[>] Cleaning res://src/pkg...\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got, want := buildCaches(m), []string{".gd++pkg/"}; !reflect.DeepEqual(got, want) {
		t.Errorf("caches = %v, want %v", got, want)
	}
	if _, ok := m.tree()["/games/my_game/src/pkg/main.cpp"]; !ok {
		t.Errorf("src/pkg/main.cpp was deleted")
	}
}

func TestCleanPaths(t *testing.T) {
	m := withCleanFS(t, "/games/my_game")
	out := captureStderr(t, (&CmdClean{Paths: []string{"src/pkg/main.cpp", "res://", "src/other", "src/pkg"}}).Run)
	if want := "[>] Cleaning res://src/pkg...\n[>] Cleaning res:// [my_game]...\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got := buildCaches(m); len(got) > 0 {
		t.Errorf("caches = %v, want none", got)
	}
}

func TestCleanProj(t *testing.T) {
	m := withCleanFS(t, "/games/my_game/src/other")
	out := captureStderr(t, (&CmdClean{Proj: true}).Run)
	if want := "[>] Cleaning res:// [my_game]...\n[>] Cleaning res://src/pkg...\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got := buildCaches(m); len(got) > 0 {
		t.Errorf("caches = %v, want none", got)
	}
}

func TestCleanProjWithPaths(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		withCleanFS(t, "/games/my_game")
		(&CmdClean{Proj: true, Paths: []string{"src/pkg"}}).Run()
		return
	}
	out, code := runFailHelper(t, "TestCleanProjWithPaths")
	if want := "[!] Invalid arguments: paths and --proj cannot be used together.\n"; code != 1 || out != want {
		t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, want)
	}
}

func TestCleanNothing(t *testing.T) {
	m := withCleanFS(t, "/games/my_game/src/other")
	before := m.tree()
	out := captureStderr(t, (&CmdClean{}).Run)
	if want := "[>] Nothing to clean.\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if !reflect.DeepEqual(m.tree(), before) {
		t.Errorf("tree changed, want it unchanged")
	}
}
