// cmd_init_test.go: tests for cmd_init.go. Runs on memFS, in the project from
// project_test.go.

package main

import (
	"os"
	"strings"
	"testing"
)

// runInit runs c in the project from withPackages(pkgs), with the Godot C++
// bindings 4.3 cached, and returns its output and the tree under the
// project root after.
func runInit(t *testing.T, c CmdInit, pkgs map[string]string) (out string, after map[string]string) {
	tree := withPackages(pkgs)
	tree["/games/my_game/.gd++proj/bind/4.3/a.h"] = "a"
	m := withMemFS(t, "/games/my_game", tree)
	withQuiet(t, false)
	withTTY(t, false)
	out = captureStderr(t, c.Run)
	return out, subtree(m.tree(), "/games/my_game/")
}

func intPtr(n int) *int { return &n }

func TestInitProject(t *testing.T) {
	out, after := runInit(t, CmdInit{Vcs: "git"}, nil)
	want := "" +
		"[-] Created res://gd++proj.toml.\n" +
		"[-] Set the VCS to git.\n" +
		"[-] Created res://.gitignore.\n" +
		"[-] Success!\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got, want := after[projectConfigFileName], "vcs = \"git\"\n"; got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
	if got := after[gitignoreFileName]; got != projectBlock {
		t.Errorf(".gitignore = %q, want %q", got, projectBlock)
	}
}

func TestInitProjectVcsNone(t *testing.T) {
	tree := withPackages(map[string]string{"src/pkg": syncPkgConfig})
	tree["/games/my_game/"+projectConfigFileName] = "vcs = \"git\"\n"
	tree["/games/my_game/.gitignore"] = "user\n\n" + projectBlock
	tree["/games/my_game/src/pkg/.gitignore"] = packageBlock
	m := withMemFS(t, "/games/my_game", tree)
	withQuiet(t, false)
	withTTY(t, false)
	want := "" +
		"[-] Set the VCS to none.\n" +
		"[-] Updated res://.gitignore.\n" +
		"[-] Deleted res://src/pkg/.gitignore.\n" +
		"[-] Success!\n"
	if out := captureStderr(t, (&CmdInit{Vcs: "none"}).Run); out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	after := subtree(m.tree(), "/games/my_game/")
	if got := after[gitignoreFileName]; got != "user\n" {
		t.Errorf(".gitignore = %q, want %q", got, "user\n")
	}
	if _, ok := after["src/pkg/"+gitignoreFileName]; ok {
		t.Errorf("tree = %v, want no src/pkg/.gitignore", after)
	}
}

func TestInitProjectNoVcsChange(t *testing.T) {
	// Only a change of VCS touches the .gitignore files.
	out, after := runInit(t, CmdInit{Vcs: "none"}, nil)
	if want := "[-] Created res://gd++proj.toml.\n[-] Success!\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if _, ok := after[gitignoreFileName]; ok {
		t.Errorf("tree = %v, want no .gitignore", after)
	}
}

func TestInitProjectNoChanges(t *testing.T) {
	tree := withPackages(nil)
	tree["/games/my_game/"+projectConfigFileName] = "vcs = \"git\"\n"
	withMemFS(t, "/games/my_game", tree)
	withQuiet(t, false)
	withTTY(t, false)
	if out, want := captureStderr(t, (&CmdInit{Vcs: "git"}).Run), "[!] No changes were made.\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestInitNewPackage(t *testing.T) {
	out, after := runInit(t, CmdInit{Path: "src/pkg", Bind: "4.3", Spec: "4.3", Std: "c++17"}, nil)
	want := "" +
		"[-] Created res://src/pkg/gd++pkg.toml.\n" +
		"[!] Missing Godot API spec 4.3, run `gd++ fetch --missing` to fix this.\n" +
		"[-] Success!\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got, want := after["src/pkg/"+packageFileName], "bind = \"4.3\"\nspec = \"4.3\"\nsyntax = 0\nstd = \"c++17\"\n"; got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
}

func TestInitNewPackageGit(t *testing.T) {
	for _, path := range []string{"src/pkg", "."} {
		tree := withPackages(nil)
		tree["/games/my_game/"+projectConfigFileName] = "vcs = \"git\"\n"
		tree["/games/my_game/.gitignore"] = "user\n"
		m := withMemFS(t, "/games/my_game", tree)
		withQuiet(t, true)
		(&CmdInit{Path: path, Bind: "4.3", Spec: "4.3"}).Run()
		after := subtree(m.tree(), "/games/my_game/")
		want := map[string]string{"src/pkg": packageBlock, ".": "user\n\n" + packageBlock}[path]
		if got := after[strings.TrimPrefix(path+"/", "./")+gitignoreFileName]; got != want {
			t.Errorf("%s: .gitignore = %q, want %q", path, got, want)
		}
	}
}

func TestInitRootPackage(t *testing.T) {
	out, after := runInit(t, CmdInit{Path: ".", Bind: "4.3", Spec: "4.3"}, nil)
	want := "" +
		"[-] Created res://gd++pkg.toml.\n" +
		"[!] Missing Godot API spec 4.3, run `gd++ fetch --missing` to fix this.\n" +
		"[-] Success!\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if _, ok := after[packageFileName]; !ok {
		t.Errorf("tree = %v, want it to have %s", after, packageFileName)
	}
}

func TestInitNestedPackages(t *testing.T) {
	pkgs := map[string]string{"src/pkg": "bind = \"4.3\"\nspec = \"4.3\"\n"}
	for _, path := range []string{"src", "src/pkg/sub"} {
		_, after := runInit(t, CmdInit{Path: path, Bind: "4.3", Spec: "4.3"}, pkgs)
		if _, ok := after[path+"/"+packageFileName]; !ok {
			t.Errorf("%s: tree = %v, want it to have %s", path, after, path+"/"+packageFileName)
		}
	}
}

func TestInitUpdatePackage(t *testing.T) {
	pkgs := map[string]string{"src/pkg": "bind = \"4.2\"\nspec = \"4.3\"\n"}
	out, after := runInit(t, CmdInit{Path: "src/pkg", Update: true, Bind: "4.3", Syntax: intPtr(1)}, pkgs)
	want := "" +
		"[-] Set the Godot C++ bindings to 4.3.\n" +
		"[-] Set the GD++ syntax to 1.\n" +
		"[!] Missing Godot API spec 4.3, run `gd++ fetch --missing` to fix this.\n" +
		"[-] Success!\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got, want := after["src/pkg/"+packageFileName], "bind = \"4.3\"\nspec = \"4.3\"\nsyntax = 1\nstd = \"c++20\"\n"; got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
}

func TestInitExistingPackage(t *testing.T) {
	withForce(t, true)
	pkgs := map[string]string{"src/pkg": "bind = \"4.3\"\nspec = \"4.3\"\nsyntax = 0\nstd = \"c++20\"\n"}
	out, _ := runInit(t, CmdInit{Path: "src/pkg", Bind: "4.3"}, pkgs)
	want := "" +
		"[!] No changes were made.\n" +
		"[!] Missing Godot API spec 4.3, run `gd++ fetch --missing` to fix this.\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestInitInvalidArgs(t *testing.T) {
	cases := map[string]struct {
		c    CmdInit
		want string
	}{
		"project_bind":    {CmdInit{Bind: "4.3"}, "--update, --bind, --spec, --syntax, --std and --class require a package path"},
		"project_update":  {CmdInit{Update: true}, "--update, --bind, --spec, --syntax, --std and --class require a package path"},
		"project_icon":    {CmdInit{Icon: "pkg://a.svg"}, "--include, --noinclude, --icon and --noicon require --class"},
		"package_icon":    {CmdInit{Path: "src/pkg", Icon: "pkg://a.svg"}, "--include, --noinclude, --icon and --noicon require --class"},
		"class_bind":      {CmdInit{Path: "src/pkg", Class: "A", Include: "pkg://a.h", Bind: "4.3"}, "--bind, --spec, --syntax and --std cannot be used with --class"},
		"class_name":      {CmdInit{Path: "src/pkg", Class: "my node", Include: "pkg://a.h"}, `"my node" is not a valid class name`},
		"class_include":   {CmdInit{Path: "src/pkg", Class: "B"}, "a new class requires --include or --noinclude"},
		"class_noinclude": {CmdInit{Path: "src/pkg", Class: "A", Include: "pkg://a.h", NoInclude: true}, "--include and --noinclude cannot be used together"},
		"class_noicon":    {CmdInit{Path: "src/pkg", Class: "A", Include: "pkg://a.h", Icon: "pkg://a.svg", NoIcon: true}, "--icon and --noicon cannot be used together"},
		"class_path":      {CmdInit{Path: "src/pkg", Class: "A", Include: "pkg://a.h", Icon: "a.svg"}, "a.svg must start with pkg:// or res://"},
		"project_vcs":     {CmdInit{Vcs: "svn"}, "--vcs must be none or git"},
		"package_vcs":     {CmdInit{Path: "src/pkg", Vcs: "git"}, "--vcs cannot be used with a package path"},
		"new_no_spec":     {CmdInit{Path: "lib", Bind: "4.3"}, "--bind and --spec are required for a new package"},
		"bad_name":        {CmdInit{Path: "src/pkg", Bind: "../a"}, `"../a" is not a valid dependency name`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if os.Getenv("GDPP_FAIL_HELPER") == "1" {
				isTTY = false
				withMemFS(t, "/games/my_game", withPackages(map[string]string{"src/pkg": "bind = \"4.3\"\nspec = \"4.3\"\n"}))
				tc.c.Run()
				return
			}
			out, code := runFailHelper(t, t.Name())
			if want := "[x] Invalid arguments: " + tc.want + ".\n"; code != 1 || out != want {
				t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, want)
			}
		})
	}
}

const classPkgConfig = "bind = \"4.3\"\nspec = \"4.3\"\nsyntax = 0\nstd = \"c++20\"\n"

func TestInitNewClass(t *testing.T) {
	out, after := runInit(t, CmdInit{Path: "src/pkg", Class: "MyNode", Include: "pkg://my_node.h", Icon: "res://my_node.svg"}, map[string]string{"src/pkg": classPkgConfig})
	want := "" +
		"[-] Set the include path of class MyNode to pkg://my_node.h.\n" +
		"[-] Set the icon of class MyNode to res://my_node.svg.\n" +
		"[-] Success!\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	wantConfig := classPkgConfig + "\n[[class]]\n  name = \"MyNode\"\n  include = \"pkg://my_node.h\"\n  icon = \"res://my_node.svg\"\n"
	if got := after["src/pkg/"+packageFileName]; got != wantConfig {
		t.Errorf("config = %q, want %q", got, wantConfig)
	}
}

func TestInitUpdateClass(t *testing.T) {
	config := classPkgConfig + "\n[[class]]\nname = \"A\"\ninclude = \"pkg://a.h\"\nicon = \"pkg://a.svg\"\n"
	pkgs := map[string]string{"src/pkg": config}
	// Without --icon the icon stays.
	out, after := runInit(t, CmdInit{Path: "src/pkg", Class: "A", Include: "res://a.hpp", Update: true}, pkgs)
	want := "" +
		"[-] Set the include path of class A to res://a.hpp.\n" +
		"[-] Success!\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	wantConfig := classPkgConfig + "\n[[class]]\n  name = \"A\"\n  include = \"res://a.hpp\"\n  icon = \"pkg://a.svg\"\n"
	if got := after["src/pkg/"+packageFileName]; got != wantConfig {
		t.Errorf("config = %q, want %q", got, wantConfig)
	}
	// --noicon removes it.
	out, after = runInit(t, CmdInit{Path: "src/pkg", Class: "A", Include: "pkg://a.h", NoIcon: true, Update: true}, pkgs)
	if want := "[-] Set the icon of class A to none.\n[-] Success!\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	wantConfig = classPkgConfig + "\n[[class]]\n  name = \"A\"\n  include = \"pkg://a.h\"\n"
	if got := after["src/pkg/"+packageFileName]; got != wantConfig {
		t.Errorf("config = %q, want %q", got, wantConfig)
	}
}

func TestInitClassNoInclude(t *testing.T) {
	config := classPkgConfig + "\n[[class]]\nname = \"A\"\ninclude = \"pkg://a.h\"\nicon = \"pkg://a.svg\"\n"
	pkgs := map[string]string{"src/pkg": config}
	// A new class without a header.
	out, after := runInit(t, CmdInit{Path: "src/pkg", Class: "B", NoInclude: true}, pkgs)
	if want := "[-] Set the include path of class B to none.\n[-] Set the icon of class B to none.\n[-] Success!\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	wantConfig := classPkgConfig + "\n[[class]]\n  name = \"A\"\n  include = \"pkg://a.h\"\n  icon = \"pkg://a.svg\"\n\n[[class]]\n  name = \"B\"\n"
	if got := after["src/pkg/"+packageFileName]; got != wantConfig {
		t.Errorf("config = %q, want %q", got, wantConfig)
	}
	// Removing the header of an existing class; the icon stays.
	out, after = runInit(t, CmdInit{Path: "src/pkg", Class: "A", NoInclude: true, Update: true}, pkgs)
	if want := "[-] Set the include path of class A to none.\n[-] Success!\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	wantConfig = classPkgConfig + "\n[[class]]\n  name = \"A\"\n  icon = \"pkg://a.svg\"\n"
	if got := after["src/pkg/"+packageFileName]; got != wantConfig {
		t.Errorf("config = %q, want %q", got, wantConfig)
	}
}

func TestInitClassConfirms(t *testing.T) {
	config := classPkgConfig + "\n[[class]]\nname = \"A\"\ninclude = \"pkg://a.h\"\n"
	cases := map[string]struct {
		c    CmdInit
		want string
	}{
		"exists":    {CmdInit{Path: "src/pkg", Class: "A", Include: "pkg://a.h"}, "[?] Class A already exists in res://src/pkg. Update it? [y/n] n\n"},
		"extension": {CmdInit{Path: "src/pkg", Class: "B", Include: "pkg://b.cpp"}, "[?] Include path pkg://b.cpp is not a .h or .hpp file. Continue? [y/n] n\n"},
		"missing":   {CmdInit{Path: "src/pkg", Class: "B", Include: "pkg://b.h", Update: true}, "[x] There is no class B in res://src/pkg.\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if os.Getenv("GDPP_FAIL_HELPER") == "1" {
				isTTY = false
				withMemFS(t, "/games/my_game", withPackages(map[string]string{"src/pkg": config}))
				tc.c.Run()
				return
			}
			out, code := runFailHelper(t, t.Name())
			if code != 1 || !strings.HasPrefix(out, tc.want) {
				t.Errorf("exit code = %d, output = %q, want 1, prefix %q", code, out, tc.want)
			}
		})
	}
}

func TestInitClassSorted(t *testing.T) {
	config := classPkgConfig + "\n[[class]]\nname = \"C\"\n\n[[class]]\nname = \"A\"\n"
	_, after := runInit(t, CmdInit{Path: "src/pkg", Class: "B", NoInclude: true}, map[string]string{"src/pkg": config})
	want := classPkgConfig + "\n[[class]]\n  name = \"A\"\n\n[[class]]\n  name = \"B\"\n\n[[class]]\n  name = \"C\"\n"
	if got := after["src/pkg/"+packageFileName]; got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
}
