// path_test.go: tests for path.go. Filesystem tests run on memFS and never
// touch the real disk.

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

func TestCwd(t *testing.T) {
	withMemFS(t, "/work/dir", nil)
	if got, want := Cwd(), NewPath("/work/dir"); got != want {
		t.Errorf("Cwd() = %v, want %v", got, want)
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
	withMemFS(t, "/", map[string]string{"/d/file.txt": "x"})

	cases := []struct {
		name                  string
		p                     string
		exists, isDir, isFile bool
	}{
		{"dir", "/d", true, true, false},
		{"file", "/d/file.txt", true, false, true},
		{"missing", "/d/missing", false, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := NewPath(c.p)
			if got := p.Exists(); got != c.exists {
				t.Errorf("Exists() = %v, want %v", got, c.exists)
			}
			if got := p.IsDir(); got != c.isDir {
				t.Errorf("IsDir() = %v, want %v", got, c.isDir)
			}
			if got := p.IsFile(); got != c.isFile {
				t.Errorf("IsFile() = %v, want %v", got, c.isFile)
			}
		})
	}
}

func TestIsProjectRootIsPackageRoot(t *testing.T) {
	withMemFS(t, "/", map[string]string{
		"/proj/" + projectFileName: "",
		"/pkg/" + packageFileName:  "",
		"/empty/":                  "",
	})

	cases := []struct {
		name             string
		p                string
		isProject, isPkg bool
	}{
		{"project dir", "/proj", true, false},
		{"package dir", "/pkg", false, true},
		{"empty dir", "/empty", false, false},
		{"file", "/proj/" + projectFileName, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := NewPath(c.p)
			if got := p.IsProjectRoot(); got != c.isProject {
				t.Errorf("IsProjectRoot() = %v, want %v", got, c.isProject)
			}
			if got := p.IsPackageRoot(); got != c.isPkg {
				t.Errorf("IsPackageRoot() = %v, want %v", got, c.isPkg)
			}
		})
	}
}

func TestGetProjectRootGetPackageRoot(t *testing.T) {
	withMemFS(t, "/", map[string]string{
		"/root/" + projectFileName: "",
		"/root/" + packageFileName: "",
		"/root/a/b/":               "",
		"/outside/":                "",
	})
	root, nested, outside := NewPath("/root"), NewPath("/root/a/b"), NewPath("/outside")

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

func TestToString(t *testing.T) {
	withMemFS(t, "/base/x/y", map[string]string{
		"/proj/" + projectFileName: "",
		"/proj/foo/bar/":           "",
		"/base/a/b/":               "",
	})

	cases := []struct {
		name string
		p    string
		want string
	}{
		{"project root", "/proj", "res://"},
		{"nested in project", "/proj/foo/bar", "res://foo/bar"},
		{"cwd itself", "/base/x/y", "."},
		{"nested under cwd", "/base/x/y/a/b", "a/b"},
		{"through parents", "/base/a/b", "../../a/b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NewPath(c.p).ToString(); got != c.want {
				t.Errorf("ToString() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestParsePath(t *testing.T) {
	withMemFS(t, "/proj/src", map[string]string{
		"/proj/" + projectFileName: "",
		"/proj/" + packageFileName: "",
	})

	cases := []struct {
		name string
		s    string
		want string
	}{
		{"absolute", "/a/b", "/a/b"},
		{"relative", "a/../b", "/proj/src/b"},
		{"res", "res://a/b", "/proj/a/b"},
		{"pkg", "pkg://a", "/proj/a"},
		{"home", "~", "/home/user"},
		{"under home", "~/a/b", "/home/user/a/b"},
		{"home with trailing slash", "~/", "/home/user"},
		{"tilde name", "~foo", "/proj/src/~foo"},
		{"tilde user", "~bob/a", "/proj/src/~bob/a"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ParsePath(c.s); got != NewPath(c.want) {
				t.Errorf("ParsePath(%q) = %q, want %q", c.s, got.absolutePath, c.want)
			}
		})
	}
}

func TestLs(t *testing.T) {
	withMemFS(t, "/", map[string]string{
		"/d/b.txt":   "",
		"/d/a.txt":   "",
		"/d/.hidden": "",
		"/d/sub/":    "",
		"/d/.git/":   "",
	})

	var names []string
	for _, child := range NewPath("/d").Ls() {
		names = append(names, child.Name())
	}

	want := []string{"a.txt", "b.txt", "sub"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("Ls() names = %v, want %v", names, want)
	}
}

func TestReadStringWriteString(t *testing.T) {
	withMemFS(t, "/work", nil)
	file := NewPath("/work/f.txt")
	file.WriteString("hello")
	if got := file.ReadString(); got != "hello" {
		t.Errorf("ReadString() = %q, want %q", got, "hello")
	}
	file.WriteString("world") // overwrites
	if got := file.ReadString(); got != "world" {
		t.Errorf("ReadString() = %q, want %q", got, "world")
	}
}

func TestCreateFileCreateDirectory(t *testing.T) {
	withMemFS(t, "/work", nil)
	file, dir := NewPath("/work/f.txt"), NewPath("/work/d")
	file.CreateFile()
	dir.CreateDirectory()
	if !file.IsFile() {
		t.Errorf("IsFile() = false after CreateFile(), want true")
	}
	if got := file.ReadString(); got != "" {
		t.Errorf("ReadString() = %q, want empty", got)
	}
	if !dir.IsDir() {
		t.Errorf("IsDir() = false after CreateDirectory(), want true")
	}
	nested := NewPath("/work/x/y/z")
	nested.CreateDirectory()
	if !nested.IsDir() {
		t.Errorf("IsDir() = false after nested CreateDirectory(), want true")
	}
}

// dirTree returns a small directory tree rooted at root, in withMemFS format.
// It has a dot-file, a nested dir, and an empty dir.
func dirTree(root string) map[string]string {
	return map[string]string{
		root + "/":           "",
		root + "/.hidden":    "h",
		root + "/sub/":       "",
		root + "/sub/c.txt":  "c",
		root + "/sub/empty/": "",
	}
}

// mergeTrees returns the union of the given trees.
func mergeTrees(trees ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, t := range trees {
		for k, v := range t {
			out[k] = v
		}
	}
	return out
}

// Trees shared by the Remove, Move and Copy tests. cwd is /work.
var (
	workDir  = map[string]string{"/work/": ""}
	fileA    = map[string]string{"/work/a.txt": "a"}
	emptyDir = map[string]string{"/work/empty/": ""}
	fullDir  = dirTree("/work/full")
	baseTree = mergeTrees(workDir, fileA, emptyDir, fullDir)
)

// runFileOp runs op on a fresh memFS holding baseTree, and checks the
// resulting tree against want.
func runFileOp(t *testing.T, op func(), want map[string]string) {
	m := withMemFS(t, "/work", baseTree)
	op()
	if got := m.tree(); !reflect.DeepEqual(got, want) {
		t.Errorf("tree = %v, want %v", got, want)
	}
}

func TestRemove(t *testing.T) {
	cases := []struct {
		name   string
		target string
		audit  bool // with audit on and no TTY, a prompt would fail the test
		want   map[string]string
	}{
		{"file", "/work/a.txt", true, mergeTrees(workDir, emptyDir, fullDir)},
		{"empty dir", "/work/empty", true, mergeTrees(workDir, fileA, fullDir)},
		{"non-empty dir", "/work/full", false, mergeTrees(workDir, fileA, emptyDir)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withTTY(t, false)
			withAudit(t, c.audit)
			runFileOp(t, func() { NewPath(c.target).Remove() }, c.want)
		})
	}
}

func TestRemoveAuditYes(t *testing.T) {
	withTTY(t, true)
	withAudit(t, true)
	withStdin(t, "y\n")
	var out string
	runFileOp(t, func() {
		out = captureStderr(t, func() { NewPath("/work/full").Remove() })
	}, mergeTrees(workDir, fileA, emptyDir))
	if want := "Delete full and everything inside it?"; !strings.Contains(out, want) {
		t.Errorf("output = %q, want to contain %q", out, want)
	}
}

func TestRemoveAuditNo(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = true
		withAudit(t, true)
		withMemFS(t, "/work", baseTree)
		NewPath("/work/full").Remove()
		return
	}
	out, code := runFailHelperWithStdin(t, "TestRemoveAuditNo", "n\n")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "Operation canceled by user."; !strings.Contains(out, want) {
		t.Errorf("output = %q, want to contain %q", out, want)
	}
}

func TestMove(t *testing.T) {
	cases := []struct {
		name     string
		src, dst string
		want     map[string]string
	}{
		{"file", "/work/a.txt", "/work/b.txt",
			mergeTrees(workDir, map[string]string{"/work/b.txt": "a"}, emptyDir, fullDir)},
		{"dir", "/work/full", "/work/empty/moved",
			mergeTrees(workDir, fileA, emptyDir, dirTree("/work/empty/moved"))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runFileOp(t, func() { NewPath(c.src).Move(NewPath(c.dst)) }, c.want)
		})
	}
}

func TestCopy(t *testing.T) {
	cases := []struct {
		name     string
		src, dst string
		want     map[string]string
	}{
		{"file", "/work/a.txt", "/work/b.txt",
			mergeTrees(baseTree, map[string]string{"/work/b.txt": "a"})},
		{"dir", "/work/full", "/work/empty/copy",
			mergeTrees(baseTree, dirTree("/work/empty/copy"))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runFileOp(t, func() { NewPath(c.src).Copy(NewPath(c.dst)) }, c.want)
		})
	}
}

// TestFileOpsFail checks operations that exit via Fail. Each case re-execs the
// test binary, which runs op on a memFS holding baseTree (cwd /work) with the
// fail method returning fs.ErrPermission.
func TestSync(t *testing.T) {
	m := withMemFS(t, "/work", map[string]string{"/work/src/a.txt": "aaa", "/work/src/sub/b.txt": "b", "/work/dst/stale.txt": ""})
	src, dst := NewPath("/work/src"), NewPath("/work/dst")
	src.Sync(dst)
	want := map[string]string{"a.txt": "aaa", "sub/": "", "sub/b.txt": "b"}
	if got := subtree(m.tree(), "/work/dst/"); !reflect.DeepEqual(got, want) {
		t.Errorf("after the first sync, dst = %v, want %v", got, want)
	}

	// Nothing changed: no file is written.
	m.fail = "Create"
	src.Sync(dst)
	m.fail = ""

	// Same size, other contents: copied.
	src.Cd("a.txt").WriteString("xxx")
	src.Sync(dst)
	want["a.txt"] = "xxx"
	if got := subtree(m.tree(), "/work/dst/"); !reflect.DeepEqual(got, want) {
		t.Errorf("after the edit, dst = %v, want %v", got, want)
	}
}

func TestFileOpsFail(t *testing.T) {
	p := NewPath
	cases := []struct {
		name string
		fail string
		op   func()
		want string
	}{
		{"project root outside project", "", func() { GetProjectRoot(p("/work")) },
			"Path . is not contained in a Godot project."},
		{"package root outside package", "", func() { GetPackageRoot(p("/work")) },
			"Path . is not contained in a GD++ package."},
		{"home error", "UserHomeDir", func() { ParsePath("~/a") },
			"Failed to get the home directory: permission denied."},
		{"read missing", "", func() { p("/work/missing").ReadString() },
			"Failed to read missing: open /work/missing: file does not exist."},
		{"read dir", "", func() { p("/work/full").ReadString() },
			"Failed to read full: read /work/full: is a directory."},
		{"write dir", "", func() { p("/work/full").WriteString("x") },
			"Failed to write full: open /work/full: is a directory."},
		{"write error", "WriteFile", func() { p("/work/a.txt").WriteString("x") },
			"Failed to write a.txt: permission denied."},
		{"create file exists", "", func() { p("/work/a.txt").CreateFile() },
			"Path a.txt already exists."},
		{"create dir exists", "", func() { p("/work/full").CreateDirectory() },
			"Path full already exists."},
		{"remove missing", "", func() { p("/work/missing").Remove() },
			"Path missing does not exist."},
		{"remove error", "RemoveAll", func() { p("/work/a.txt").Remove() },
			"Failed to remove a.txt: permission denied."},
		{"move missing", "", func() { p("/work/missing").Move(p("/work/b.txt")) },
			"Path missing does not exist."},
		{"move onto existing", "", func() { p("/work/a.txt").Move(p("/work/full")) },
			"Path full already exists."},
		{"move error", "Rename", func() { p("/work/a.txt").Move(p("/work/b.txt")) },
			"Failed to move a.txt to b.txt: permission denied."},
		{"copy missing", "", func() { p("/work/missing").Copy(p("/work/b.txt")) },
			"Path missing does not exist."},
		{"copy onto existing", "", func() { p("/work/a.txt").Copy(p("/work/full")) },
			"Path full already exists."},
		{"copy error", "Create", func() { p("/work/a.txt").Copy(p("/work/b.txt")) },
			"Failed to write b.txt: permission denied."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if os.Getenv("GDPP_FAIL_HELPER") == "1" {
				isTTY = false
				withMemFS(t, "/work", baseTree).fail = c.fail
				c.op()
				return
			}
			out, code := runFailHelper(t, t.Name())
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if want := "[!] " + c.want + "\n"; out != want {
				t.Errorf("output = %q, want %q", out, want)
			}
		})
	}
}
