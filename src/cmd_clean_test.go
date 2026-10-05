// cmd_clean_test.go: tests for cmd_clean.go. Runs on memFS, in the project
// from project_test.go.

package main

import (
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
	tree["/games/my_game/.gd++build/a.o"] = ""
	tree["/games/my_game/src/pkg/.gd++build/a.o"] = ""
	tree["/games/my_game/src/pkg/main.cpp"] = ""
	withQuiet(t, false)
	withTTY(t, false)
	return withMemFS(t, cwd, tree)
}

// buildCaches returns the build caches left in the project.
func buildCaches(m *memFS) []string {
	var caches []string
	for path := range subtree(m.tree(), "/games/my_game/") {
		if strings.HasSuffix(path, ".gd++build/") {
			caches = append(caches, path)
		}
	}
	slices.Sort(caches)
	return caches
}

func TestClean(t *testing.T) {
	m := withCleanFS(t, "/games/my_game/src/pkg")
	out := captureStderr(t, (&CmdClean{}).Run)
	if want := "[$] Running task: cleaning res://src/pkg...\n[-] Task succeeded: cleaning res://src/pkg\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got, want := buildCaches(m), []string{".gd++build/"}; !reflect.DeepEqual(got, want) {
		t.Errorf("caches = %v, want %v", got, want)
	}
	if _, ok := m.tree()["/games/my_game/src/pkg/main.cpp"]; !ok {
		t.Errorf("src/pkg/main.cpp was deleted")
	}
}

func TestCleanPaths(t *testing.T) {
	m := withCleanFS(t, "/games/my_game")
	out := captureStderr(t, (&CmdClean{Paths: []string{"src/pkg/main.cpp", "res://", "src/other", "src/pkg"}}).Run)
	if want := "[$] Running task: cleaning res://src/pkg...\n[-] Task succeeded: cleaning res://src/pkg\n[$] Running task: cleaning res:// [my_game]...\n[-] Task succeeded: cleaning res:// [my_game]\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got := buildCaches(m); len(got) > 0 {
		t.Errorf("caches = %v, want none", got)
	}
}

func TestCleanRecursive(t *testing.T) {
	m := withCleanFS(t, "/games/my_game/src/other")
	out := captureStderr(t, (&CmdClean{Paths: []string{"res://..."}}).Run)
	if want := "[$] Running task: cleaning res:// [my_game]...\n[-] Task succeeded: cleaning res:// [my_game]\n[$] Running task: cleaning res://src/pkg...\n[-] Task succeeded: cleaning res://src/pkg\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got := buildCaches(m); len(got) > 0 {
		t.Errorf("caches = %v, want none", got)
	}
}

func TestCleanBin(t *testing.T) {
	m := withCleanFS(t, "/games/my_game")
	libs := []string{"libpkg.linux.debug.x86_64.so", "libpkg.windows.release.x86_32.dll", "libpkg.macos.debug.arm64.dylib", "pkg.gdextension", "pkg.gdextension.uid",
		"libpkg.so", "libold.linux.debug.x86_64.so", "old.gdextension", "old.gdextension.uid"}
	kept := []string{"lib.so", "pkg.so", "libpkg.linux.debug.x86_64.so.txt", "pkg.gdextension.txt", ".gdextension"}
	for _, name := range append(libs, kept...) {
		m.nodes["/games/my_game/src/pkg/"+name] = &memNode{}
	}
	m.nodes["/games/my_game/src/other/libother.linux.debug.x86_64.so"] = &memNode{}
	out := captureStderr(t, (&CmdClean{Bin: true, Paths: []string{"src/pkg", "src/other"}}).Run)
	if want := "[$] Running task: cleaning res://src/pkg...\n[-] Task succeeded: cleaning res://src/pkg\n[$] Running task: cleaning res://src/other...\n[-] Task succeeded: cleaning res://src/other\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	tree := m.tree()
	for _, name := range libs {
		if _, ok := tree["/games/my_game/src/pkg/"+name]; ok {
			t.Errorf("%s was not deleted", name)
		}
	}
	for _, name := range kept {
		if _, ok := tree["/games/my_game/src/pkg/"+name]; !ok {
			t.Errorf("%s was deleted", name)
		}
	}
	if _, ok := tree["/games/my_game/src/other/libother.linux.debug.x86_64.so"]; ok {
		t.Errorf("libother.linux.debug.x86_64.so was not deleted")
	}
}

func TestCleanRecursiveSubdir(t *testing.T) {
	m := withCleanFS(t, "/games/my_game/src")
	out := captureStderr(t, (&CmdClean{Paths: []string{"..."}}).Run)
	if want := "[$] Running task: cleaning res://src/pkg...\n[-] Task succeeded: cleaning res://src/pkg\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got, want := buildCaches(m), []string{".gd++build/"}; !reflect.DeepEqual(got, want) {
		t.Errorf("caches = %v, want %v", got, want)
	}
}

func TestCleanNothing(t *testing.T) {
	m := withCleanFS(t, "/games/my_game/src/other")
	before := m.tree()
	out := captureStderr(t, (&CmdClean{}).Run)
	if want := "[-] Nothing to clean.\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if !reflect.DeepEqual(m.tree(), before) {
		t.Errorf("tree changed, want it unchanged")
	}
}
