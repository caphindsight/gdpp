// path_test.go: tests for path.go.

package main

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestNewPath(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"already clean", "/a/b/c", "/a/b/c"},
		{"root", "/", "/"},
		{"cleans dot segments", "/a/./b", "/a/b"},
		{"cleans dotdot segments", "/a/b/../c", "/a/c"},
		{"trailing slash removed", "/a/b/", "/a/b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NewPath(c.input); got.absolutePath != c.want {
				t.Errorf("NewPath(%q).absolutePath = %q, want %q", c.input, got.absolutePath, c.want)
			}
		})
	}
}

func TestNewPathRejectsRelative(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		NewPath("rel/path")
		return
	}

	out, code := runFailHelper(t, "TestNewPathRejectsRelative")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "[!] Path \"rel/path\" is not absolute.\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestGetOsPath(t *testing.T) {
	p := Path{absolutePath: "/a/b/c"}
	// filepath.FromSlash is a no-op on this platform's separator being "/",
	// so this only exercises the conversion, not a real separator change.
	if got := p.GetOsPath(); got != "/a/b/c" {
		t.Errorf("GetOsPath() = %q, want %q", got, "/a/b/c")
	}
}

func TestName(t *testing.T) {
	cases := []struct{ input, want string }{
		{"/a/b/c", "c"},
		{"/a", "a"},
		{"/", "/"},
	}
	for _, c := range cases {
		p := Path{absolutePath: c.input}
		if got := p.Name(); got != c.want {
			t.Errorf("Path{%q}.Name() = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestBaseDir(t *testing.T) {
	cases := []struct{ input, want string }{
		{"/a/b/c", "/a/b"},
		{"/a", "/"},
		{"/", "/"},
	}
	for _, c := range cases {
		p := Path{absolutePath: c.input}
		if got := p.BaseDir(); got.absolutePath != c.want {
			t.Errorf("Path{%q}.BaseDir().absolutePath = %q, want %q", c.input, got.absolutePath, c.want)
		}
	}
}

func TestIsGlobalRoot(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"/", true},
		{"/a", false},
		{"/a/b", false},
		{"C:", true},
		{"C:/", true},
		{"c:", true},
		{"C:/Users", false},
	}
	for _, c := range cases {
		p := Path{absolutePath: c.input}
		if got := p.IsGlobalRoot(); got != c.want {
			t.Errorf("Path{%q}.IsGlobalRoot() = %v, want %v", c.input, got, c.want)
		}
	}
}

func TestCd(t *testing.T) {
	cases := []struct {
		base     string
		segments []string
		want     string
	}{
		{"/a", []string{"b", "c"}, "/a/b/c"},
		{"/a", nil, "/a"},
		{"/a", []string{"b", "..", "c"}, "/a/c"},
		{"/a/b", []string{"../c"}, "/a/c"},
	}
	for _, c := range cases {
		p := Path{absolutePath: c.base}
		if got := p.Cd(c.segments...); got.absolutePath != c.want {
			t.Errorf("Path{%q}.Cd(%v).absolutePath = %q, want %q", c.base, c.segments, got.absolutePath, c.want)
		}
	}
}

func TestExistsIsDirIsFile(t *testing.T) {
	dir := NewPath(t.TempDir())
	file := dir.Cd("file.txt")
	if err := os.WriteFile(file.GetOsPath(), []byte("x"), 0644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}
	missing := dir.Cd("missing")

	cases := []struct {
		name                  string
		p                     Path
		exists, isDir, isFile bool
	}{
		{"dir", dir, true, true, false},
		{"file", file, true, false, true},
		{"missing", missing, false, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.p.Exists(); got != c.exists {
				t.Errorf("Exists() = %v, want %v", got, c.exists)
			}
			if got := c.p.IsDir(); got != c.isDir {
				t.Errorf("IsDir() = %v, want %v", got, c.isDir)
			}
			if got := c.p.IsFile(); got != c.isFile {
				t.Errorf("IsFile() = %v, want %v", got, c.isFile)
			}
		})
	}
}

func TestIsProjectRootIsPackageRoot(t *testing.T) {
	projectDir := NewPath(t.TempDir())
	if err := os.WriteFile(projectDir.Cd(projectFileName).GetOsPath(), nil, 0644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	packageDir := NewPath(t.TempDir())
	if err := os.WriteFile(packageDir.Cd(packageFileName).GetOsPath(), nil, 0644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	emptyDir := NewPath(t.TempDir())
	file := projectDir.Cd(projectFileName)

	cases := []struct {
		name             string
		p                Path
		isProject, isPkg bool
	}{
		{"project dir", projectDir, true, false},
		{"package dir", packageDir, false, true},
		{"empty dir", emptyDir, false, false},
		{"file", file, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.p.IsProjectRoot(); got != c.isProject {
				t.Errorf("IsProjectRoot() = %v, want %v", got, c.isProject)
			}
			if got := c.p.IsPackageRoot(); got != c.isPkg {
				t.Errorf("IsPackageRoot() = %v, want %v", got, c.isPkg)
			}
		})
	}
}

func TestGetProjectRootGetPackageRoot(t *testing.T) {
	root := NewPath(t.TempDir())
	if err := os.WriteFile(root.Cd(projectFileName).GetOsPath(), nil, 0644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}
	if err := os.WriteFile(root.Cd(packageFileName).GetOsPath(), nil, 0644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}
	nested := root.Cd("a", "b")
	if err := os.MkdirAll(nested.GetOsPath(), 0755); err != nil {
		t.Fatalf("os.MkdirAll: %v", err)
	}
	outside := NewPath(t.TempDir())

	if got, ok := GetProjectRootMaybe(nested); !ok || got != root {
		t.Errorf("GetProjectRootMaybe(nested) = (%v, %v), want (%v, true)", got, ok, root)
	}
	if got := GetProjectRoot(nested); got != root {
		t.Errorf("GetProjectRoot(nested) = %v, want %v", got, root)
	}
	if _, ok := GetProjectRootMaybe(outside); ok {
		t.Errorf("GetProjectRootMaybe(outside) ok = true, want false")
	}

	if got, ok := GetPackageRootMaybe(nested); !ok || got != root {
		t.Errorf("GetPackageRootMaybe(nested) = (%v, %v), want (%v, true)", got, ok, root)
	}
	if got := GetPackageRoot(nested); got != root {
		t.Errorf("GetPackageRoot(nested) = %v, want %v", got, root)
	}
	if _, ok := GetPackageRootMaybe(outside); ok {
		t.Errorf("GetPackageRootMaybe(outside) ok = true, want false")
	}
}

func TestGetProjectRootFailsOutsideProject(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		GetProjectRoot(NewPath(os.TempDir()))
		return
	}

	out, code := runFailHelper(t, "TestGetProjectRootFailsOutsideProject")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.HasSuffix(out, "is not contained in a Godot project.\n") {
		t.Errorf("output = %q, want suffix %q", out, "is not contained in a Godot project.\n")
	}
}

func TestGetPackageRootFailsOutsidePackage(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		GetPackageRoot(NewPath(os.TempDir()))
		return
	}

	out, code := runFailHelper(t, "TestGetPackageRootFailsOutsidePackage")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.HasSuffix(out, "is not contained in a GD++ package.\n") {
		t.Errorf("output = %q, want suffix %q", out, "is not contained in a GD++ package.\n")
	}
}

func TestLs(t *testing.T) {
	dir := NewPath(t.TempDir())
	for _, name := range []string{"b.txt", "a.txt", ".hidden", "sub", ".git"} {
		p := dir.Cd(name).GetOsPath()
		var err error
		if name == "sub" || name == ".git" {
			err = os.Mkdir(p, 0755)
		} else {
			err = os.WriteFile(p, nil, 0644)
		}
		if err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
	}

	var names []string
	for _, child := range dir.Ls() {
		names = append(names, child.Name())
	}

	want := []string{"a.txt", "b.txt", "sub"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("Ls() names = %v, want %v", names, want)
	}
}
