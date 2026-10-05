// package_test.go: tests for package.go.

package main

import (
	"maps"
	"os"
	"reflect"
	"testing"
)

// withPackages returns testProjectTree plus the given .gd++pkg files,
// keyed by package directory relative to the project root.
func withPackages(pkgs map[string]string) map[string]string {
	tree := maps.Clone(testProjectTree)
	for dir, config := range pkgs {
		tree["/games/my_game/"+dir+"/"+packageFileName] = config
	}
	return tree
}

func TestLoadPackage(t *testing.T) {
	m := withMemFS(t, "/", withPackages(map[string]string{"src/pkg": "bind = \"4.3\"\nspec = \"4.3-stable\"\n"}))
	m.nodes["/games/my_game/src/pkg/sub"] = &memNode{dir: true}
	before := m.tree()

	root := NewPath("/games/my_game/src/pkg")
	want := Package{
		Root:       root,
		Id:         "pkg",
		Config:     PackageConfig{Bindings: "4.3", ApiSpec: "4.3-stable", Syntax: 0, CppStandard: "c++20"},
		BuildCache: root.Cd(".gd++build"),
	}
	if got := LoadPackage(root.Cd("sub")); !reflect.DeepEqual(got, want) {
		t.Errorf("LoadPackage() = %+v, want %+v", got, want)
	}
	if got := m.tree(); !reflect.DeepEqual(got, before) {
		t.Errorf("LoadPackage() changed the tree to %v, want %v", got, before)
	}
}

func TestLoadPackageAllKeys(t *testing.T) {
	config := "bind = \"a\"\nspec = \"b\"\nsyntax = 2\nstd = \"c++23\"\nprefix = \"Pk\"\n\n" +
		"[[class]]\nname = \"A\"\ninclude = \"pkg://a.h\"\nicon = \"res://a.svg\"\n\n" +
		"[[class]]\nname = \"B\"\ninclude = \"res://b.hpp\"\ntool = true\n"
	withMemFS(t, "/", withPackages(map[string]string{"pkg": config}))
	want := PackageConfig{Bindings: "a", ApiSpec: "b", Syntax: 2, CppStandard: "c++23", Prefix: "Pk", Classes: []PackageClass{
		{Name: "A", Include: "pkg://a.h", Icon: "res://a.svg"},
		{Name: "B", Include: "res://b.hpp", Tool: true},
	}}
	got := LoadPackage(NewPath("/games/my_game/pkg")).Config
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Config = %+v, want %+v", got, want)
	}
}

// classes returns a package config with a [[class]] table per body.
func classes(bodies ...string) string {
	s := "bind = \"a\"\nspec = \"b\"\n"
	for _, body := range bodies {
		s += "\n[[class]]\n" + body + "\n"
	}
	return s
}

func TestAsyncClass(t *testing.T) {
	for id, want := range map[string]string{"my_game": "MyGameAsync", "my-game": "MyGameAsync", "game": "GameAsync",
		"HUD": "HUDAsync", "3d_tools": "Pkg3dToolsAsync", "__": "PkgAsync", "a.b c": "ABCAsync"} {
		if got := (Package{Id: id}).AsyncClass(); got != want {
			t.Errorf("AsyncClass of %q = %q, want %q", id, got, want)
		}
	}
	if got := (Package{Id: "my_game", Config: PackageConfig{Prefix: "Mg"}}).AsyncClass(); got != "MgAsync" {
		t.Errorf("AsyncClass with prefix Mg = %q, want MgAsync", got)
	}
}

func TestQuitTimeout(t *testing.T) {
	for _, tc := range []struct {
		config *float64
		want   float64
		text   string
	}{{nil, 1, "1 second"}, {ptr(2.5), 2.5, "2.5 seconds"}, {ptr(0.0), 0, "0 seconds"}} {
		if got := (Package{Config: PackageConfig{QuitTimeout: tc.config}}).QuitTimeout(); got != tc.want || seconds(got) != tc.text {
			t.Errorf("QuitTimeout = %v (%s), want %v (%s)", got, seconds(got), tc.want, tc.text)
		}
	}
}

func TestLoadPackageFails(t *testing.T) {
	tests := []struct {
		name, config, want string
	}{
		{"MissingBind", `spec = "b"`, "[x] Missing key bind in res://pkg/.gd++pkg.\n"},
		{"MissingSpec", `bind = "a"`, "[x] Missing key spec in res://pkg/.gd++pkg.\n"},
		{"UnknownKey", "bind = \"a\"\nspec = \"b\"\njobs = 4\n", "[x] Unknown key jobs in res://pkg/.gd++pkg.\n"},
		{"ClassName", classes(`name = "a-b"`), "[x] Invalid class name \"a-b\" in res://pkg/.gd++pkg.\n"},
		{"ClassDup", classes(`name = "A"`+"\ninclude = \"pkg://a.h\"", `name = "A"`+"\ninclude = \"pkg://a.h\""), "[x] Duplicate class A in res://pkg/.gd++pkg.\n"},
		{"QuitTimeout", "bind = \"a\"\nspec = \"b\"\nquit_timeout = -1.5\n", "[x] Invalid quit_timeout -1.5 in res://pkg/.gd++pkg: it can't be negative.\n"},
		{"MacroDepth", "bind = \"a\"\nspec = \"b\"\nmacro_depth = 0\n", "[x] Invalid macro_depth 0 in res://pkg/.gd++pkg: it must be positive.\n"},
		{"ClassIcon", classes(`name = "A"` + "\ninclude = \"pkg://a.h\"\nicon = \"a.svg\""), "[x] Path a.svg of class A in res://pkg/.gd++pkg must start with pkg:// or res://.\n"},
		{"ClassKind", classes(`name = "A"` + "\ninclude = \"pkg://a.h\"\nkind = \"val\""), "[x] Invalid kind \"val\" of class A in res://pkg/.gd++pkg: it must be ptr or ref.\n"},
		{"ClassKindInclude", classes(`name = "A"` + "\nkind = \"ptr\""), "[x] Class A in res://pkg/.gd++pkg has kind ptr, which requires an include.\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if os.Getenv("GDPP_FAIL_HELPER") == "1" {
				isTTY = false
				withMemFS(t, "/games/my_game", withPackages(map[string]string{"pkg": tt.config}))
				withQuiet(t, false)
				LoadPackage(NewPath("/games/my_game/pkg"))
				return
			}
			out, code := runFailHelper(t, "TestLoadPackageFails/"+tt.name)
			if code != 1 || out != tt.want {
				t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, tt.want)
			}
		})
	}
}

func TestLoadPackageOutsideProject(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		withMemFS(t, "/work", map[string]string{"/work/pkg/" + packageFileName: "bind = \"a\"\nspec = \"b\"\n"})
		withQuiet(t, false)
		LoadPackage(NewPath("/work/pkg"))
		return
	}
	out, code := runFailHelper(t, "TestLoadPackageOutsideProject")
	if want := "[x] Path pkg is not contained in a Godot project.\n"; code != 1 || out != want {
		t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, want)
	}
}

func TestListPackagesRoot(t *testing.T) {
	config := "bind = \"a\"\nspec = \"b\"\n"
	tree := withPackages(map[string]string{"b": config})
	tree["/games/my_game/"+packageFileName] = config
	withMemFS(t, "/", tree)
	p := LoadProject(NewPath("/games/my_game"))
	pkgs := p.ListPackages()
	var got []string
	for _, pkg := range pkgs {
		got = append(got, pkg.Root.ToString())
	}
	if want := []string{"res://", "res://b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ListPackages() = %v, want %v", got, want)
	}
}

func TestListPackages(t *testing.T) {
	config := "bind = \"a\"\nspec = \"b\"\n"
	withMemFS(t, "/", withPackages(map[string]string{
		"b":          config,
		"a/deep/pkg": config,
		"b/nested":   config, // inside a package: listed too
		".hidden":    config, // hidden: skipped
		"_gd++/pkg":  config, // checked in caches: skipped
		"other":      config, // gets a project.godot below: skipped
	}))
	NewPath("/games/my_game/other").Cd(projectFileName).WriteString("")

	p := LoadProject(NewPath("/games/my_game"))
	var got []string
	for _, pkg := range p.ListPackages() {
		got = append(got, pkg.Root.ToString())
	}
	if want := []string{"res://a/deep/pkg", "res://b", "res://b/nested"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ListPackages() = %v, want %v", got, want)
	}
}

func TestParsePackagePaths(t *testing.T) {
	config := "bind = \"a\"\nspec = \"b\"\n"
	tree := withPackages(map[string]string{"a": config, "b": config, "b/nested": config})
	tree["/games/my_game/b/src/main.cpp"] = ""
	withMemFS(t, "/games/my_game/b", tree)
	tests := []struct {
		paths []string
		want  []string
	}{
		{nil, []string{"res://b", "res://b/nested"}},
		{[]string{"src/main.cpp", "."}, []string{"res://b"}},
		{[]string{"..."}, []string{"res://b", "res://b/nested"}},
		{[]string{"./..."}, []string{"res://b", "res://b/nested"}},
		{[]string{"nested", "res://..."}, []string{"res://b/nested", "res://a", "res://b"}},
		{[]string{"src/..."}, nil},
	}
	for _, tt := range tests {
		var got []string
		for _, root := range ParsePackagePaths(tt.paths) {
			got = append(got, root.ToString())
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("ParsePackagePaths(%q) = %v, want %v", tt.paths, got, tt.want)
		}
	}
}

func TestParsePackagePathsNotDir(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		withMemFS(t, "/games/my_game", withPackages(map[string]string{"a": "bind = \"a\"\nspec = \"b\"\n"}))
		withQuiet(t, false)
		ParsePackagePaths([]string{"missing/..."})
		return
	}
	out, code := runFailHelper(t, "TestParsePackagePathsNotDir")
	if want := "[x] Path res://missing is not a directory.\n"; code != 1 || out != want {
		t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, want)
	}
}
