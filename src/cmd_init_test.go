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
	tree["/games/my_game/.gd++cache/bind/4.3/a.h"] = "a"
	m := withMemFS(t, "/games/my_game", tree)
	withQuiet(t, false)
	withTTY(t, false)
	out = captureStderr(t, c.Run)
	return out, subtree(m.tree(), "/games/my_game/")
}

func TestInitProject(t *testing.T) {
	out, after := runInit(t, CmdInit{Vcs: "git"}, nil)
	want := "" +
		"[-] Created res://.gd++proj.\n" +
		"[-] Set the VCS to git.\n" +
		"[-] Created res://.gitignore.\n" +
		"[-] Success!\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got, want := after[projectConfigFileName], "vcs = \"git\"\npresets = true\n"; got != want {
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
	if want := "[-] Created res://.gd++proj.\n[-] Success!\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if _, ok := after[gitignoreFileName]; ok {
		t.Errorf("tree = %v, want no .gitignore", after)
	}
}

func TestInitProjectNoChanges(t *testing.T) {
	tree := withPackages(nil)
	tree["/games/my_game/"+projectConfigFileName] = "vcs = \"git\"\npresets = true\n"
	withMemFS(t, "/games/my_game", tree)
	withQuiet(t, false)
	withTTY(t, false)
	if out, want := captureStderr(t, (&CmdInit{Vcs: "git", Presets: true}).Run), "[!] No changes were made.\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestInitProjectPresets(t *testing.T) {
	// init only sets the config: build and fix edit the presets.
	tree := withPackages(nil)
	tree["/games/my_game/"+exportPresetsFileName] = testExportPresets("*.txt")
	m := withMemFS(t, "/games/my_game", tree)
	withQuiet(t, false)
	withTTY(t, false)
	for _, tc := range []struct {
		c      CmdInit
		want   string
		config string
	}{
		{CmdInit{NoPresets: true}, "[-] Created res://.gd++proj.\n[-] Set the export preset filters to off.\n[-] Success!\n", "vcs = \"none\"\npresets = false\n"},
		{CmdInit{Presets: true}, "[-] Set the export preset filters to on.\n[-] Success!\n", "vcs = \"none\"\npresets = true\n"},
	} {
		if out := captureStderr(t, tc.c.Run); out != tc.want {
			t.Errorf("%+v: output = %q, want %q", tc.c, out, tc.want)
		}
		tree := m.tree()
		if got := tree["/games/my_game/"+projectConfigFileName]; got != tc.config {
			t.Errorf("%+v: config = %q, want %q", tc.c, got, tc.config)
		}
		if got, want := tree["/games/my_game/"+exportPresetsFileName], testExportPresets("*.txt"); got != want {
			t.Errorf("%+v: export_presets.cfg = %q, want %q", tc.c, got, want)
		}
	}
}

func TestInitNewPackage(t *testing.T) {
	out, after := runInit(t, CmdInit{Path: "src/pkg", Bind: "4.3", Spec: "4.3", Std: "c++17"}, nil)
	want := "" +
		"[-] Created res://src/pkg/.gd++pkg.\n" +
		"[!] Missing Godot API spec 4.3, run `gd++ fetch --missing` to fix this.\n" +
		"[-] Success!\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got, want := after["src/pkg/"+packageFileName], "bind = \"4.3\"\nspec = \"4.3\"\nsyntax = 1\nstd = \"c++17\"\n"; got != want {
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
		"[-] Created res://.gd++pkg.\n" +
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
	pkgs := map[string]string{"src/pkg": "bind = \"4.2\"\nspec = \"4.3\"\nsyntax = 0\n"}
	out, after := runInit(t, CmdInit{Path: "src/pkg", Update: true, Bind: "4.3", Syntax: ptr(1)}, pkgs)
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

func TestInitPackagePrefix(t *testing.T) {
	pkgs := map[string]string{"src/pkg": "bind = \"4.3\"\nspec = \"4.3\"\n"}
	out, after := runInit(t, CmdInit{Path: "src/pkg", Update: true, Prefix: "Foo"}, pkgs)
	want := "" +
		"[-] Set the class name prefix to Foo.\n" +
		"[!] Missing Godot API spec 4.3, run `gd++ fetch --missing` to fix this.\n" +
		"[-] Success!\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got, want := after["src/pkg/"+packageFileName], "bind = \"4.3\"\nspec = \"4.3\"\nsyntax = 0\nstd = \"c++20\"\nprefix = \"Foo\"\n"; got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
}

func TestInitPackageQuitTimeout(t *testing.T) {
	pkgs := map[string]string{"src/pkg": "bind = \"4.3\"\nspec = \"4.3\"\n"}
	out, after := runInit(t, CmdInit{Path: "src/pkg", Update: true, QuitTimeout: ptr(2.5)}, pkgs)
	if want := "[-] Set the quit timeout to 2.5 seconds.\n"; !strings.HasPrefix(out, want) {
		t.Errorf("output = %q, want it to start with %q", out, want)
	}
	if got, want := after["src/pkg/"+packageFileName], "bind = \"4.3\"\nspec = \"4.3\"\nsyntax = 0\nstd = \"c++20\"\nquit_timeout = 2.5\n"; got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
}

func TestInitPackageHotReload(t *testing.T) {
	base := "bind = \"4.3\"\nspec = \"4.3\"\nsyntax = 0\nstd = \"c++20\"\n"
	off := base + "hot_reload = false\n"
	pkgs := map[string]string{"src/pkg": base}
	out, after := runInit(t, CmdInit{Path: "src/pkg", Update: true, NoHotReload: true}, pkgs)
	if want := "[-] Set hot reload to off.\n"; !strings.HasPrefix(out, want) {
		t.Errorf("output = %q, want it to start with %q", out, want)
	}
	if got := after["src/pkg/"+packageFileName]; got != off {
		t.Errorf("config = %q, want %q", got, off)
	}
	// Back on: the default, so the key goes away.
	out, after = runInit(t, CmdInit{Path: "src/pkg", Update: true, HotReload: true}, map[string]string{"src/pkg": off})
	if want := "[-] Set hot reload to on.\n"; !strings.HasPrefix(out, want) {
		t.Errorf("output = %q, want it to start with %q", out, want)
	}
	if got := after["src/pkg/"+packageFileName]; got != base {
		t.Errorf("config = %q, want %q", got, base)
	}
}

func TestInitPackageMacroDepth(t *testing.T) {
	base := "bind = \"4.3\"\nspec = \"4.3\"\nsyntax = 0\nstd = \"c++20\"\n"
	deep := base + "macro_depth = 100\n"
	out, after := runInit(t, CmdInit{Path: "src/pkg", Update: true, MacroDepth: ptr(100)}, map[string]string{"src/pkg": base})
	if want := "[-] Set the macro depth to 100.\n"; !strings.HasPrefix(out, want) {
		t.Errorf("output = %q, want it to start with %q", out, want)
	}
	if got := after["src/pkg/"+packageFileName]; got != deep {
		t.Errorf("config = %q, want %q", got, deep)
	}
	// Back to 64: the default, so the key goes away.
	out, after = runInit(t, CmdInit{Path: "src/pkg", Update: true, MacroDepth: ptr(64)}, map[string]string{"src/pkg": deep})
	if want := "[-] Set the macro depth to 64.\n"; !strings.HasPrefix(out, want) {
		t.Errorf("output = %q, want it to start with %q", out, want)
	}
	if got := after["src/pkg/"+packageFileName]; got != base {
		t.Errorf("config = %q, want %q", got, base)
	}
}

func TestInitPackageNightlySyntax(t *testing.T) {
	pkgs := map[string]string{"src/pkg": "bind = \"4.3\"\nspec = \"4.3\"\nsyntax = 1\n"}
	out, after := runInit(t, CmdInit{Path: "src/pkg", Update: true, Nightly: true}, pkgs)
	want := "[!] Syntax 0 is reserved for the nightly version of GD++, which is explicitly not backward compatible: never use it in production!\n" +
		"[-] Set the GD++ syntax to 0.\n"
	if !strings.HasPrefix(out, want) {
		t.Errorf("output = %q, want it to start with %q", out, want)
	}
	if got, want := after["src/pkg/"+packageFileName], "bind = \"4.3\"\nspec = \"4.3\"\nsyntax = 0\nstd = \"c++20\"\n"; got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
}

func TestInitPackageUnknownSyntax(t *testing.T) {
	pkgs := map[string]string{"src/pkg": "bind = \"4.3\"\nspec = \"4.3\"\n"}
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		withMemFS(t, "/games/my_game", withPackages(pkgs))
		(&CmdInit{Path: "src/pkg", Update: true, Syntax: ptr(2)}).Run()
		return
	}
	out, code := runFailHelper(t, t.Name())
	if want := "[?] Unknown GD++ syntax 2: either the value is wrong, or this version of gd++ is outdated. Continue? [y/n] n\n"; code != 1 || !strings.HasPrefix(out, want) {
		t.Errorf("exit code = %d, output = %q, want 1, prefix %q", code, out, want)
	}
	withForce(t, true)
	out, after := runInit(t, CmdInit{Path: "src/pkg", Update: true, Syntax: ptr(2)}, pkgs)
	if want := "[-] Set the GD++ syntax to 2.\n"; !strings.HasPrefix(out, want) {
		t.Errorf("output = %q, want it to start with %q", out, want)
	}
	if got, want := after["src/pkg/"+packageFileName], "bind = \"4.3\"\nspec = \"4.3\"\nsyntax = 2\nstd = \"c++20\"\n"; got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
}

func TestInitExistingPackage(t *testing.T) {
	withForce(t, true)
	pkgs := map[string]string{"src/pkg": "bind = \"4.3\"\nspec = \"4.3\"\nsyntax = 1\nstd = \"c++20\"\n"}
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
		"project_bind":        {CmdInit{Bind: "4.3"}, "--update, --bind, --spec, --syntax, --std, --prefix, --quit-timeout, --hotreload, --nohotreload, --macro-depth, --hide and --class require a package path"},
		"project_update":      {CmdInit{Update: true}, "--update, --bind, --spec, --syntax, --std, --prefix, --quit-timeout, --hotreload, --nohotreload, --macro-depth, --hide and --class require a package path"},
		"project_prefix":      {CmdInit{Prefix: "Foo"}, "--update, --bind, --spec, --syntax, --std, --prefix, --quit-timeout, --hotreload, --nohotreload, --macro-depth, --hide and --class require a package path"},
		"package_prefix":      {CmdInit{Path: "src/pkg", Prefix: "my pkg"}, `"my pkg" is not a valid class name prefix`},
		"negative_quit":       {CmdInit{Path: "src/pkg", QuitTimeout: ptr(-1.0)}, "--quit-timeout cannot be negative"},
		"hotreload_both":      {CmdInit{Path: "src/pkg", HotReload: true, NoHotReload: true}, "--hotreload and --nohotreload cannot be used together"},
		"zero_macro_depth":    {CmdInit{Path: "src/pkg", MacroDepth: ptr(0)}, "--macro-depth must be positive"},
		"project_reload":      {CmdInit{NoHotReload: true}, "--update, --bind, --spec, --syntax, --std, --prefix, --quit-timeout, --hotreload, --nohotreload, --macro-depth, --hide and --class require a package path"},
		"class_reload":        {CmdInit{Path: "src/pkg", Class: "A", Include: "pkg://a.h", HotReload: true}, "--bind, --spec, --syntax, --std, --prefix, --quit-timeout, --hotreload, --nohotreload, --macro-depth and --hide cannot be used with --class"},
		"hide_outside":        {CmdInit{Path: "src/pkg", Hide: []string{"../x"}}, "../x is not a directory inside the package"},
		"hide_res":            {CmdInit{Path: "src/pkg", Hide: []string{"res://x"}}, "res://x is not a directory inside the package"},
		"negative_syntax":     {CmdInit{Path: "src/pkg", Syntax: ptr(-1)}, "--syntax cannot be negative"},
		"syntax_nightly":      {CmdInit{Path: "src/pkg", Syntax: ptr(1), Nightly: true}, "--syntax and --nightly cannot be used together"},
		"project_quit":        {CmdInit{QuitTimeout: ptr(1.0)}, "--update, --bind, --spec, --syntax, --std, --prefix, --quit-timeout, --hotreload, --nohotreload, --macro-depth, --hide and --class require a package path"},
		"class_prefix":        {CmdInit{Path: "src/pkg", Class: "A", Include: "pkg://a.h", Prefix: "Foo"}, "--bind, --spec, --syntax, --std, --prefix, --quit-timeout, --hotreload, --nohotreload, --macro-depth and --hide cannot be used with --class"},
		"project_icon":        {CmdInit{Icon: "pkg://a.svg"}, "--include, --noinclude, --icon, --noicon, --tool, --notool, --abstract, --noabstract, --ptr, --ref and --nogdpp require --class"},
		"package_icon":        {CmdInit{Path: "src/pkg", Icon: "pkg://a.svg"}, "--include, --noinclude, --icon, --noicon, --tool, --notool, --abstract, --noabstract, --ptr, --ref and --nogdpp require --class"},
		"class_bind":          {CmdInit{Path: "src/pkg", Class: "A", Include: "pkg://a.h", Bind: "4.3"}, "--bind, --spec, --syntax, --std, --prefix, --quit-timeout, --hotreload, --nohotreload, --macro-depth and --hide cannot be used with --class"},
		"class_name":          {CmdInit{Path: "src/pkg", Class: "my node", Include: "pkg://a.h"}, `"my node" is not a valid class name`},
		"class_include":       {CmdInit{Path: "src/pkg", Class: "B"}, "a new class requires --include or --noinclude"},
		"class_noinclude":     {CmdInit{Path: "src/pkg", Class: "A", Include: "pkg://a.h", NoInclude: true}, "--include and --noinclude cannot be used together"},
		"class_noicon":        {CmdInit{Path: "src/pkg", Class: "A", Include: "pkg://a.h", Icon: "pkg://a.svg", NoIcon: true}, "--icon and --noicon cannot be used together"},
		"project_tool":        {CmdInit{Tool: true}, "--include, --noinclude, --icon, --noicon, --tool, --notool, --abstract, --noabstract, --ptr, --ref and --nogdpp require --class"},
		"class_notool":        {CmdInit{Path: "src/pkg", Class: "A", Include: "pkg://a.h", Tool: true, NoTool: true}, "--tool and --notool cannot be used together"},
		"class_noabstract":    {CmdInit{Path: "src/pkg", Class: "A", Include: "pkg://a.h", Abstract: true, NoAbstract: true}, "--abstract and --noabstract cannot be used together"},
		"project_ptr":         {CmdInit{Ptr: true}, "--include, --noinclude, --icon, --noicon, --tool, --notool, --abstract, --noabstract, --ptr, --ref and --nogdpp require --class"},
		"class_ptr_ref":       {CmdInit{Path: "src/pkg", Class: "A", Include: "pkg://a.h", Ptr: true, Ref: true}, "--ptr, --ref and --nogdpp cannot be used together"},
		"class_ref_nogdpp":    {CmdInit{Path: "src/pkg", Class: "A", Include: "pkg://a.h", Ref: true, NoGdpp: true}, "--ptr, --ref and --nogdpp cannot be used together"},
		"class_ptr_noinclude": {CmdInit{Path: "src/pkg", Class: "A", NoInclude: true, Ptr: true}, "GD++ code can only use class A if it has an include"},
		"class_path":          {CmdInit{Path: "src/pkg", Class: "A", Include: "pkg://a.h", Icon: "a.svg"}, "a.svg must start with pkg:// or res://"},
		"project_vcs":         {CmdInit{Vcs: "svn"}, "--vcs must be none or git"},
		"package_vcs":         {CmdInit{Path: "src/pkg", Vcs: "git"}, "--vcs, --presets and --nopresets cannot be used with a package path"},
		"package_presets":     {CmdInit{Path: "src/pkg", NoPresets: true}, "--vcs, --presets and --nopresets cannot be used with a package path"},
		"presets_both":        {CmdInit{Presets: true, NoPresets: true}, "--presets and --nopresets cannot be used together"},
		"new_no_spec":         {CmdInit{Path: "lib", Bind: "4.3"}, "--bind and --spec are required for a new package"},
		"bad_name":            {CmdInit{Path: "src/pkg", Bind: "../a"}, `"../a" is not a valid dependency name`},
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

const classPkgConfig = "bind = \"4.3\"\nspec = \"4.3\"\nsyntax = 1\nstd = \"c++20\"\n"

func TestInitNewClass(t *testing.T) {
	out, after := runInit(t, CmdInit{Path: "src/pkg", Class: "MyNode", Include: "pkg://my_node.h", Icon: "res://my_node.svg"}, map[string]string{"src/pkg": classPkgConfig})
	want := "" +
		"[-] Set the include path of class MyNode to pkg://my_node.h.\n" +
		"[-] Set the icon of class MyNode to res://my_node.svg.\n" +
		"[-] Set class MyNode to a runtime class.\n" +
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

func TestInitClassTool(t *testing.T) {
	config := classPkgConfig + "\n[[class]]\nname = \"A\"\ninclude = \"pkg://a.h\"\n"
	pkgs := map[string]string{"src/pkg": config}
	// --tool makes it a tool class.
	out, after := runInit(t, CmdInit{Path: "src/pkg", Class: "A", Tool: true, Update: true}, pkgs)
	if want := "[-] Set class A to a tool class.\n[-] Success!\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	toolConfig := classPkgConfig + "\n[[class]]\n  name = \"A\"\n  include = \"pkg://a.h\"\n  tool = true\n"
	if got := after["src/pkg/"+packageFileName]; got != toolConfig {
		t.Errorf("config = %q, want %q", got, toolConfig)
	}
	// Without --tool or --notool it stays a tool class.
	out, after = runInit(t, CmdInit{Path: "src/pkg", Class: "A", Icon: "pkg://a.svg", Update: true}, map[string]string{"src/pkg": toolConfig})
	if want := "[-] Set the icon of class A to pkg://a.svg.\n[-] Success!\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got, want := after["src/pkg/"+packageFileName], classPkgConfig+"\n[[class]]\n  name = \"A\"\n  include = \"pkg://a.h\"\n  icon = \"pkg://a.svg\"\n  tool = true\n"; got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
	// --notool makes it a runtime class again.
	out, after = runInit(t, CmdInit{Path: "src/pkg", Class: "A", NoTool: true, Update: true}, map[string]string{"src/pkg": toolConfig})
	if want := "[-] Set class A to a runtime class.\n[-] Success!\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got, want := after["src/pkg/"+packageFileName], classPkgConfig+"\n[[class]]\n  name = \"A\"\n  include = \"pkg://a.h\"\n"; got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
}

func TestInitClassAbstract(t *testing.T) {
	config := classPkgConfig + "\n[[class]]\nname = \"A\"\ninclude = \"pkg://a.h\"\n"
	// --abstract makes it an abstract class.
	out, after := runInit(t, CmdInit{Path: "src/pkg", Class: "A", Abstract: true, Update: true}, map[string]string{"src/pkg": config})
	if want := "[-] Set class A to an abstract class.\n[-] Success!\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	abstractConfig := classPkgConfig + "\n[[class]]\n  name = \"A\"\n  include = \"pkg://a.h\"\n  abstract = true\n"
	if got := after["src/pkg/"+packageFileName]; got != abstractConfig {
		t.Errorf("config = %q, want %q", got, abstractConfig)
	}
	// --noabstract makes it concrete again.
	out, after = runInit(t, CmdInit{Path: "src/pkg", Class: "A", NoAbstract: true, Update: true}, map[string]string{"src/pkg": abstractConfig})
	if want := "[-] Set class A to a concrete class.\n[-] Success!\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got, want := after["src/pkg/"+packageFileName], classPkgConfig+"\n[[class]]\n  name = \"A\"\n  include = \"pkg://a.h\"\n"; got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
}

func TestInitClassKind(t *testing.T) {
	config := classPkgConfig + "\n[[class]]\nname = \"A\"\ninclude = \"pkg://a.h\"\n"
	// --ptr marks a new class.
	out, after := runInit(t, CmdInit{Path: "src/pkg", Class: "B", Include: "pkg://b.h", Ptr: true}, map[string]string{"src/pkg": config})
	want := "" +
		"[-] Set the include path of class B to pkg://b.h.\n" +
		"[-] Set the icon of class B to none.\n" +
		"[-] Set class B to a runtime class.\n" +
		"[-] Set class B to [ptr], usable from GD++ as a pointer.\n" +
		"[-] Success!\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got, want := after["src/pkg/"+packageFileName], classPkgConfig+"\n[[class]]\n  name = \"A\"\n  include = \"pkg://a.h\"\n\n[[class]]\n  name = \"B\"\n  include = \"pkg://b.h\"\n  kind = \"ptr\"\n"; got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
	// --ref marks an existing class.
	out, after = runInit(t, CmdInit{Path: "src/pkg", Class: "A", Ref: true, Update: true}, map[string]string{"src/pkg": config})
	if want := "[-] Set class A to [ref], usable from GD++ as a Ref.\n[-] Success!\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	refConfig := classPkgConfig + "\n[[class]]\n  name = \"A\"\n  include = \"pkg://a.h\"\n  kind = \"ref\"\n"
	if got := after["src/pkg/"+packageFileName]; got != refConfig {
		t.Errorf("config = %q, want %q", got, refConfig)
	}
	// Without a kind flag the marker stays.
	out, after = runInit(t, CmdInit{Path: "src/pkg", Class: "A", Tool: true, Update: true}, map[string]string{"src/pkg": refConfig})
	if want := "[-] Set class A to a tool class.\n[-] Success!\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got, want := after["src/pkg/"+packageFileName], classPkgConfig+"\n[[class]]\n  name = \"A\"\n  include = \"pkg://a.h\"\n  tool = true\n  kind = \"ref\"\n"; got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
	// --nogdpp removes it.
	out, after = runInit(t, CmdInit{Path: "src/pkg", Class: "A", NoGdpp: true, Update: true}, map[string]string{"src/pkg": refConfig})
	if want := "[-] Set class A to not usable from GD++.\n[-] Success!\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got, want := after["src/pkg/"+packageFileName], classPkgConfig+"\n[[class]]\n  name = \"A\"\n  include = \"pkg://a.h\"\n"; got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
}

func TestInitClassNoInclude(t *testing.T) {
	config := classPkgConfig + "\n[[class]]\nname = \"A\"\ninclude = \"pkg://a.h\"\nicon = \"pkg://a.svg\"\n"
	pkgs := map[string]string{"src/pkg": config}
	// A new class without a header.
	out, after := runInit(t, CmdInit{Path: "src/pkg", Class: "B", NoInclude: true}, pkgs)
	if want := "[-] Set the include path of class B to none.\n[-] Set the icon of class B to none.\n[-] Set class B to a runtime class.\n[-] Success!\n"; out != want {
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
		"gdpp":      {CmdInit{Path: "src/pkg", Class: "Player", Include: "pkg://p.h"}, "[x] Class Player is declared in res://src/pkg/player.gd++, so it can only be changed there.\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if os.Getenv("GDPP_FAIL_HELPER") == "1" {
				isTTY = false
				tree := withPackages(map[string]string{"src/pkg": config})
				tree["/games/my_game/src/pkg/player.gd++"] = "class_name Player\n"
				withMemFS(t, "/games/my_game", tree)
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

func TestInitPackageHide(t *testing.T) {
	tree := withPackages(map[string]string{"src/pkg": "bind = \"4.3\"\nspec = \"4.3\"\nhide = [\"b\"]\n"})
	tree["/games/my_game/.gd++cache/bind/4.3/a.h"] = "a"
	tree["/games/my_game/.gd++cache/spec/4.3/a.json"] = "a"
	tree["/games/my_game/src/pkg/src/"] = ""
	tree["/games/my_game/src/pkg/b/"] = ""
	m := withMemFS(t, "/games/my_game", tree)
	withQuiet(t, false)
	withTTY(t, false)
	withForce(t, true)
	out := captureStderr(t, (&CmdInit{Path: "src/pkg", Update: true, Hide: []string{"pkg://src/", "b", "./a"}}).Run)
	want := "" +
		"[-] Set directory src to hidden from Godot.\n" +
		"[-] Set directory a to hidden from Godot.\n" +
		"[-] Created res://src/pkg/b/.gdignore.\n" +
		"[-] Created res://src/pkg/src/.gdignore.\n" +
		"[-] Success!\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	after := subtree(m.tree(), "/games/my_game/src/pkg/")
	if got, want := after[packageFileName], "bind = \"4.3\"\nspec = \"4.3\"\nsyntax = 0\nstd = \"c++20\"\nhide = [\"a\", \"b\", \"src\"]\n"; got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
	if _, ok := after["a/"]; ok {
		t.Errorf("a/ was created")
	}
}

func TestInitPackageHideConfirms(t *testing.T) {
	tree := withPackages(map[string]string{"src/pkg": "bind = \"4.3\"\nspec = \"4.3\"\n", "src/pkg/addons/inv": "bind = \"4.3\"\nspec = \"4.3\"\n"})
	withMemFS(t, "/games/my_game", tree)
	withForce(t, false)
	withQuiet(t, false)
	withTTY(t, true)
	withStdin(t, "y\ny\n")
	out := stripStyles(captureStderr(t, (&CmdInit{Path: "src/pkg", Update: true, Hide: []string{"gen", "addons"}}).Run))
	want := "" +
		"[?] Directory res://src/pkg/gen doesn't exist. Hide it anyway? [y/n] " +
		"[?] Directory res://src/pkg/addons contains the package res://src/pkg/addons/inv, which Godot can't load if it's hidden. Hide it anyway? [y/n] "
	if !strings.HasPrefix(out, want) {
		t.Errorf("output = %q, want prefix %q", out, want)
	}
}
