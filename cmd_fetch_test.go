// cmd_fetch_test.go: tests for cmd_fetch.go. The end-to-end test runs real git
// on a small fixture repository, on the real filesystem.

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseDepIndex(t *testing.T) {
	idx := parseDepIndex("latest=4.10-stable\nstable=4.10-stable\r\n4.10-stable\n4.9-stable\n\n")
	if want := []string{"4.9-stable", "4.10-stable"}; !reflect.DeepEqual(idx.versions, want) {
		t.Errorf("versions = %v, want %v", idx.versions, want)
	}
	if got := idx.resolve("spec", "latest"); got != "4.10-stable" {
		t.Errorf("resolve(latest) = %q, want %q", got, "4.10-stable")
	}
	if got := idx.resolve("spec", "4.9-stable"); got != "4.9-stable" {
		t.Errorf("resolve(4.9-stable) = %q, want %q", got, "4.9-stable")
	}
	if got, want := idx.String(), "4.9-stable, 4.10-stable (latest, stable)"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if got := parseDepIndex("").String(); got != "none" {
		t.Errorf("empty String() = %q, want %q", got, "none")
	}
}

func TestParseDepIndexUnknown(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		parseDepIndex("latest=4.3\n4.3\n").resolve("spec", "4.4")
		return
	}
	out, code := runFailHelper(t, "TestParseDepIndexUnknown")
	if want := "[!] Dep 4.4 is not in the spec index.\n"; code != 1 || out != want {
		t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, want)
	}
}

func TestFetchInvalidArgs(t *testing.T) {
	cases := map[string]struct {
		c    CmdFetch
		want string
	}{
		"nothing":         {CmdFetch{}, "an --index, --bind, --spec or --engine option is required"},
		"index_and_dep":   {CmdFetch{IndexSpec: true, Bind: []string{"a"}}, "--index options cannot be used with --bind, --spec or --engine options"},
		"index_and_all":   {CmdFetch{Index: true, EngineAll: true}, "--index options cannot be used with --bind, --spec or --engine options"},
		"index_twice":     {CmdFetch{Index: true, IndexBind: true}, "--index cannot be used with --index-bind, --index-spec or --index-engine"},
		"index_checkin":   {CmdFetch{IndexEngine: true, CheckIn: true}, "--checkin cannot be used with --index options"},
		"names_and_all":   {CmdFetch{Spec: []string{"a"}, SpecAll: true}, "--spec and --spec-all cannot be used together"},
		"bad_name":        {CmdFetch{Engine: []string{"4.3", "../a"}}, `"../a" is not a valid dep name`},
		"checkin_nothing": {CmdFetch{CheckIn: true}, "an --index, --bind, --spec or --engine option is required"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if os.Getenv("GDPP_FAIL_HELPER") == "1" {
				isTTY = false
				tc.c.Run()
				return
			}
			out, code := runFailHelper(t, t.Name())
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if want := "[!] Invalid arguments: " + tc.want + ".\n"; out != want {
				t.Errorf("output = %q, want %q", out, want)
			}
		})
	}
}

// writeTree writes files, given as paths relative to dir mapped to contents.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// withFetchFixture creates a fixture dep repository and a project, makes the
// project the working directory, and returns the repository's URL and the
// project's root.
func withFetchFixture(t *testing.T) (url, root string) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	withRealFS(t)
	withQuiet(t, true)
	withTTY(t, false)
	tmp := t.TempDir()

	repo := filepath.Join(tmp, "repo")
	writeTree(t, repo, map[string]string{
		"index/bind":                  "",
		"index/engine":                "",
		"index/spec":                  "latest=4.4-stable\n4.3-stable\n4.4-stable\n",
		"data/spec/latest":            "4.4-stable",
		"data/spec/4.3-stable/a.json": "4.3",
		"data/spec/4.4-stable/a.json": "4.4",
	})
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"add", "."},
		{"-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	root = filepath.Join(tmp, "my_game")
	writeTree(t, root, map[string]string{projectFileName: testProjectTree["/games/my_game/"+projectFileName]})
	wd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })
	return "file://" + repo, root
}

func TestFetchDeps(t *testing.T) {
	url, root := withFetchFixture(t)
	withForce(t, true)
	stale := filepath.Join(root, ".gd++proj/spec/4.4-stable/stale.json")
	writeTree(t, root, map[string]string{".gd++proj/spec/4.4-stable/stale.json": "x"})

	captureStdout(t, (&CmdFetch{Url: url, Branch: "main", Spec: []string{"latest", "4.4-stable"}, CheckIn: true}).Run)

	got, err := os.ReadFile(filepath.Join(root, "_gd++proj/spec/4.4-stable/a.json"))
	if err != nil || string(got) != "4.4" {
		t.Errorf("a.json = %q, %v, want %q", got, err, "4.4")
	}
	for _, p := range []string{stale, filepath.Join(root, "_gd++proj/spec/4.3-stable"), filepath.Join(root, ".gd++proj/temp")} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s exists, want it gone", p)
		}
	}
}

func TestFetchIndex(t *testing.T) {
	url, root := withFetchFixture(t)
	withQuiet(t, false)
	out := captureStdout(t, func() {
		(&CmdFetch{Url: url, Branch: "main", IndexSpec: true, IndexEngine: true}).Run()
	})
	for _, want := range []string{
		"[>] Available Godot API spec versions: 4.3-stable, 4.4-stable (latest).\n",
		"[>] Available Godot engine versions: none.\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want it to contain %q", out, want)
		}
	}
	if strings.Contains(out, "bindings") || strings.Contains(out, "Success") {
		t.Errorf("output = %q, want only the chosen indexes and no success message", out)
	}
	if _, err := os.Stat(filepath.Join(root, ".gd++proj/temp")); err == nil {
		t.Errorf("temp directory was not cleaned up")
	}
}
