// cmd_rm_test.go: tests for cmd_rm.go. Runs on memFS, in the project from
// project_test.go.

package main

import (
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

const testRmPackage = "bind = \"4.3\"\nspec = \"4.3\"\n"

// withRmFS installs testProjectTree plus files, given relative to the project
// root, and silences logs.
func withRmFS(t *testing.T, files map[string]string) *memFS {
	tree := maps.Clone(testProjectTree)
	for path, content := range files {
		tree["/games/my_game/"+path] = content
	}
	withQuiet(t, true)
	withForce(t, true)
	return withMemFS(t, "/games/my_game", tree)
}

// wantFiles reports an error unless the project has exactly the files from
// testProjectTree plus want, given relative to the project root.
func wantFiles(t *testing.T, m *memFS, want ...string) {
	t.Helper()
	var got []string
	for path := range subtree(m.tree(), "/games/my_game/") {
		if _, ok := testProjectTree["/games/my_game/"+path]; !ok && path[len(path)-1] != '/' {
			got = append(got, path)
		}
	}
	slices.Sort(got)
	slices.Sort(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
}

func TestRmDeps(t *testing.T) {
	m := withRmFS(t, map[string]string{
		"_gd++proj/bind/4.3/a.h": "", ".gd++proj/bind/4.3/a.h": "", ".gd++proj/bind/4.4/a.h": "",
		"_gd++proj/spec/4.3/a.h": "", ".gd++proj/spec/4.3/a.h": "", ".gd++proj/spec/4.4/a.h": "",
		".gd++proj/engine/4.3/a.h": "",
	})
	(&CmdRm{Bind: []string{"4.3", "4.3"}, SpecEphemeral: true}).Run()
	wantFiles(t, m, ".gd++proj/bind/4.4/a.h", "_gd++proj/spec/4.3/a.h", ".gd++proj/engine/4.3/a.h")
	(&CmdRm{DepCheckedIn: true}).Run()
	wantFiles(t, m, ".gd++proj/bind/4.4/a.h", ".gd++proj/engine/4.3/a.h")
	(&CmdRm{DepAll: true}).Run()
	wantFiles(t, m)
	for _, dir := range []string{"_gd++proj/", ".gd++proj/"} {
		if _, ok := m.tree()["/games/my_game/"+dir]; ok {
			t.Errorf("%s still exists, want the empty cache directories deleted", dir)
		}
	}
}

func TestRmPackages(t *testing.T) {
	block := packageGitignore.marker + "\n" + packageGitignore.text + "\n"
	m := withRmFS(t, map[string]string{
		".gd++proj/bind/4.3/a.h": "",
		packageFileName:          testRmPackage,
		".gitignore":             "/x\n\n" + block,
		"a/" + packageFileName:   testRmPackage,
		"a/.gitignore":           block,
		"a/.gd++pkg/b.o":         "",
		"a/src/a.cpp":            "",
	})
	(&CmdRm{Bind: []string{"4.3"}}).Run()
	(&CmdRm{PkgAll: true}).Run()
	wantFiles(t, m, ".gitignore", "a/src/a.cpp")
	if got := m.tree()["/games/my_game/.gitignore"]; got != "/x\n" {
		t.Errorf(".gitignore = %q, want %q", got, "/x\n")
	}
}

func TestRmPackageDirs(t *testing.T) {
	m := withRmFS(t, map[string]string{
		packageFileName:          testRmPackage,
		"a/" + packageFileName:   testRmPackage,
		"a/b/" + packageFileName: testRmPackage,
		"a/b/c.cpp":              "",
		"d/" + packageFileName:   testRmPackage,
	})
	(&CmdRm{Pkg: []string{"a/b", "a", "a/b"}, Dir: true}).Run()
	wantFiles(t, m, packageFileName, "d/"+packageFileName)
}

func TestRmFails(t *testing.T) {
	cases := map[string]struct {
		c    CmdRm
		want string
	}{
		"nothing":           {CmdRm{}, "Invalid arguments: a --bind, --spec, --engine, --dep, --pkg or --class option is required."},
		"dep_and_pkg":       {CmdRm{BindAll: true, Pkg: []string{"a"}}, "Invalid arguments: dependencies, packages and classes cannot be removed in the same run."},
		"pkg_and_class":     {CmdRm{Path: "a", Class: []string{"A"}, PkgAll: true}, "Invalid arguments: dependencies, packages and classes cannot be removed in the same run."},
		"path_alone":        {CmdRm{Path: "a"}, "Invalid arguments: the syntax for removing a package is `gd++ rm --pkg a`."},
		"class_alone":       {CmdRm{Class: []string{"A"}}, "Invalid arguments: --class and --class-all require a package path."},
		"class_all_alone":   {CmdRm{ClassAll: true}, "Invalid arguments: --class and --class-all require a package path."},
		"class_and_all":     {CmdRm{Path: "a", Class: []string{"A"}, ClassAll: true}, "Invalid arguments: --class and --class-all cannot be used together."},
		"pkg_and_class_all": {CmdRm{Path: "a", ClassAll: true, Pkg: []string{"a"}}, "Invalid arguments: dependencies, packages and classes cannot be removed in the same run."},
		"missing_class":     {CmdRm{Path: "a", Class: []string{"A", "B"}}, "There is no class B in res://a."},
		"names_and_all":     {CmdRm{Engine: []string{"a"}, EngineAll: true}, "Invalid arguments: only one of --engine, --engine-all, --engine-checked-in and --engine-ephemeral can be used."},
		"kind_both":         {CmdRm{SpecCheckedIn: true, SpecEphemeral: true}, "Invalid arguments: only one of --spec, --spec-all, --spec-checked-in and --spec-ephemeral can be used."},
		"dep_both":          {CmdRm{DepAll: true, DepEphemeral: true}, "Invalid arguments: only one of --dep-all, --dep-checked-in and --dep-ephemeral can be used."},
		"dep_and_kind":      {CmdRm{DepAll: true, Bind: []string{"a"}}, "Invalid arguments: --dep options cannot be used with --bind, --spec or --engine options."},
		"pkg_and_all":       {CmdRm{Pkg: []string{"a"}, PkgAll: true}, "Invalid arguments: --pkg and --pkg-all cannot be used together."},
		"dir_alone":         {CmdRm{Dir: true, BindAll: true}, "Invalid arguments: --dir can only be used with --pkg or --pkg-all."},
		"bad_name":          {CmdRm{Bind: []string{"../a"}}, `Invalid arguments: "../a" is not a valid dependency name.`},
		"missing_dep":       {CmdRm{Bind: []string{"4.3"}, Spec: []string{"4.4"}}, "The Godot API spec 4.4 is not in the project cache."},
		"missing_pkg":       {CmdRm{Pkg: []string{"b"}}, "There is no GD++ package at res://b."},
		"dir_root":          {CmdRm{PkgAll: true, Dir: true}, "Invalid arguments: --dir cannot delete the project root."},
		"nested_project":    {CmdRm{Pkg: []string{"n"}}, "The package n is not in this project."},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			files := map[string]string{
				".gd++proj/bind/4.3/a.h": "",
				packageFileName:          testRmPackage,
				"a/" + packageFileName:   testRmPackage + "\n[[class]]\nname = \"A\"\n",
				"n/" + projectFileName:   testProjectTree["/games/my_game/"+projectFileName],
				"n/" + packageFileName:   testRmPackage,
			}
			if os.Getenv("GDPP_FAIL_HELPER") == "1" {
				isTTY = false
				withRmFS(t, files)
				withQuiet(t, false)
				tc.c.Run()
				return
			}
			out, code := runFailHelper(t, t.Name())
			if want := "[x] " + tc.want + "\n"; code != 1 || out != want {
				t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, want)
			}
		})
	}
}

func TestRmPrompts(t *testing.T) {
	m := withRmFS(t, map[string]string{
		".gd++proj/bind/4.3/a.h": "", ".gd++proj/bind/4.4/a.h": "", ".gd++proj/bind/4.5/a.h": "",
		"_gd++proj/spec/4.3/a.h": "", ".gd++proj/spec/4.4/a.h": "",
		".gd++proj/engine/4.3/a.h": "", "_gd++proj/engine/4.4/a.h": "",
		packageFileName: testRmPackage, "a/" + packageFileName: testRmPackage, "d/" + packageFileName: testRmPackage,
	})
	withForce(t, false)
	withTTY(t, true)
	withStdin(t, strings.Repeat("y\n", 6))
	out := captureStderr(t, (&CmdRm{Bind: []string{"4.4", "4.3", "4.4"}, SpecAll: true}).Run)
	out += captureStderr(t, (&CmdRm{Pkg: []string{"a"}, Dir: true}).Run)
	out += captureStderr(t, (&CmdRm{DepEphemeral: true}).Run)
	out += captureStderr(t, (&CmdRm{PkgAll: true}).Run)
	var prompts []string
	for _, line := range strings.SplitAfter(stripStyles(out), "[y/n]") {
		if _, prompt, ok := strings.Cut(line, "[?] "); ok {
			prompts = append(prompts, prompt)
		}
	}
	want := []string{
		"Remove the Godot C++ bindings 4.3? [y/n]",
		"Remove the Godot C++ bindings 4.4? [y/n]",
		"Remove all Godot API specs? [y/n]",
		"Remove the package res://a and delete its whole directory? [y/n]",
		"Remove all ephemeral dependencies? [y/n]",
		"Remove all packages res://, res://d? [y/n]",
	}
	if !reflect.DeepEqual(prompts, want) {
		t.Errorf("prompts = %q, want %q", prompts, want)
	}
	if !strings.Contains(out, Styled("and delete its whole directory", Bold, Blink, Red)) {
		t.Errorf("output = %q, want the --dir warning to blink", out)
	}
	wantFiles(t, m, "_gd++proj/engine/4.4/a.h")
}

func TestRmLogs(t *testing.T) {
	block := packageGitignore.marker + "\n" + packageGitignore.text + "\n"
	withRmFS(t, map[string]string{
		"_gd++proj/bind/4.3/a.h": "",
		"a/" + packageFileName:   testRmPackage,
		"a/.gitignore":           block,
		"a/.gd++pkg/b.o":         "",
		"d/" + packageFileName:   testRmPackage,
	})
	withQuiet(t, false)
	withTTY(t, false)
	out := captureStderr(t, (&CmdRm{BindAll: true}).Run)
	out += captureStderr(t, (&CmdRm{Pkg: []string{"a"}}).Run)
	out += captureStderr(t, (&CmdRm{Pkg: []string{"d"}, Dir: true}).Run)
	want := "[-] Deleted res://_gd++proj/bind/4.3.\n" +
		"[-] Deleted the empty directory res://_gd++proj/bind.\n" +
		"[-] Deleted the empty directory res://_gd++proj.\n" +
		"[-] Success!\n" +
		"[-] Deleted res://a/gd++pkg.toml.\n" +
		"[-] Deleted res://a/.gd++pkg.\n" +
		"[-] Deleted res://a/.gitignore.\n" +
		"[-] Success!\n" +
		"[-] Deleted res://d.\n" +
		"[-] Success!\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRmClasses(t *testing.T) {
	config := testRmPackage +
		"\n[[class]]\nname = \"A\"\ninclude = \"pkg://a.h\"\n" +
		"\n[[class]]\nname = \"B\"\n" +
		"\n[[class]]\nname = \"C\"\nicon = \"pkg://c.svg\"\n"
	m := withRmFS(t, map[string]string{"a/" + packageFileName: config, "a/a.h": ""})
	withForce(t, false)
	withQuiet(t, false)
	withTTY(t, true)
	withStdin(t, "y\ny\n")
	out := stripStyles(captureStderr(t, (&CmdRm{Path: "a", Class: []string{"C", "A", "C"}}).Run))
	want := "" +
		"[?] Remove the class A from res://a? [y/n] " +
		"[?] Remove the class C from res://a? [y/n] " +
		"[•] Removed the class A.\n" +
		"[•] Removed the class C.\n" +
		"[•] Success!\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	wantConfig := "bind = \"4.3\"\nspec = \"4.3\"\nsyntax = 0\nstd = \"c++20\"\n\n[[class]]\n  name = \"B\"\n"
	if got := m.tree()["/games/my_game/a/"+packageFileName]; got != wantConfig {
		t.Errorf("config = %q, want %q", got, wantConfig)
	}
	wantFiles(t, m, "a/"+packageFileName, "a/a.h")
	withForce(t, true)
	withQuiet(t, true)
	(&CmdRm{Path: "a", Class: []string{"B"}}).Run()
	if got, want := m.tree()["/games/my_game/a/"+packageFileName], "bind = \"4.3\"\nspec = \"4.3\"\nsyntax = 0\nstd = \"c++20\"\n"; got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
}

func TestRmGdppClass(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		withRmFS(t, map[string]string{"a/" + packageFileName: testRmPackage, "a/src/player.gdpp": "class_name Player\n"})
		(&CmdRm{Path: "a", Class: []string{"Player"}}).Run()
		return
	}
	out, code := runFailHelper(t, t.Name())
	if want := "[x] Class Player is declared in res://a/src/player.gdpp, so it can only be changed there.\n"; code != 1 || out != want {
		t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, want)
	}
}

func TestRmClassAll(t *testing.T) {
	m := withRmFS(t, map[string]string{
		"a/" + packageFileName: testRmPackage + "\n[[class]]\nname = \"A\"\n\n[[class]]\nname = \"B\"\n",
		"b/" + packageFileName: testRmPackage,
	})
	withForce(t, false)
	withQuiet(t, false)
	withTTY(t, true)
	withStdin(t, "y\n")
	out := stripStyles(captureStderr(t, (&CmdRm{Path: "a", ClassAll: true}).Run))
	want := "" +
		"[?] Remove all classes A, B from res://a? [y/n] " +
		"[•] Removed the class A.\n" +
		"[•] Removed the class B.\n" +
		"[•] Success!\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got, want := m.tree()["/games/my_game/a/"+packageFileName], "bind = \"4.3\"\nspec = \"4.3\"\nsyntax = 0\nstd = \"c++20\"\n"; got != want {
		t.Errorf("config = %q, want %q", got, want)
	}
	if out, want := stripStyles(captureStderr(t, (&CmdRm{Path: "b", ClassAll: true}).Run)), "[!] Nothing to remove.\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}
