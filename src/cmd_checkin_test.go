// cmd_checkin_test.go: tests for cmd_checkin.go. Runs on memFS, in the project
// from project_test.go.

package main

import (
	"maps"
	"os"
	"reflect"
	"strings"
	"testing"
)

// withCheckInFS installs testProjectTree plus one file per dep in deps, given
// relative to the project root, and silences logs.
func withCheckInFS(t *testing.T, deps ...string) *memFS {
	tree := maps.Clone(testProjectTree)
	for _, dep := range deps {
		tree["/games/my_game/"+dep+"/a.h"] = dep
	}
	withQuiet(t, true)
	return withMemFS(t, "/games/my_game", tree)
}

// wantDeps reports an error unless the deps from withCheckInFS were moved to
// the paths in to, in the same order, and nowhere else.
func wantDeps(t *testing.T, m *memFS, from []string, to ...string) {
	t.Helper()
	got, want := map[string]string{}, map[string]string{}
	for path, content := range subtree(m.tree(), "/games/my_game/") {
		if dep, ok := strings.CutSuffix(path, "/a.h"); ok {
			got[dep] = content
		}
	}
	for i, dep := range to {
		want[dep] = from[i]
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("deps = %v, want %v", got, want)
	}
}

func TestCheckInNames(t *testing.T) {
	from := []string{".gd++proj/bind/4.3", ".gd++proj/bind/4.4", "_gd++/spec/4.3", ".gd++proj/engine/4.3"}
	m := withCheckInFS(t, from...)
	(&CmdCheckIn{Bind: []string{"4.3"}, Spec: []string{"4.3"}, Engine: []string{"4.3"}}).Run()
	wantDeps(t, m, from, "_gd++/bind/4.3", ".gd++proj/bind/4.4", "_gd++/spec/4.3", "_gd++/engine/4.3")
}

func TestCheckInKindAll(t *testing.T) {
	from := []string{".gd++proj/bind/4.3", "_gd++/bind/4.4", ".gd++proj/spec/4.3"}
	m := withCheckInFS(t, from...)
	(&CmdCheckIn{BindAll: true}).Run()
	wantDeps(t, m, from, "_gd++/bind/4.3", "_gd++/bind/4.4", ".gd++proj/spec/4.3")
}

func TestCheckInAllUndo(t *testing.T) {
	from := []string{"_gd++/bind/4.3", ".gd++proj/bind/4.4", "_gd++/spec/4.3", "_gd++/engine/4.3"}
	m := withCheckInFS(t, from...)
	(&CmdCheckIn{All: true, Undo: true}).Run()
	wantDeps(t, m, from, ".gd++proj/bind/4.3", ".gd++proj/bind/4.4", ".gd++proj/spec/4.3", ".gd++proj/engine/4.3")
}

func TestCheckInDuplicate(t *testing.T) {
	from := []string{"_gd++/bind/4.3", ".gd++proj/bind/4.3", "_gd++/spec/4.3", ".gd++proj/spec/4.3"}
	m := withCheckInFS(t, from...)
	(&CmdCheckIn{BindAll: true}).Run()
	(&CmdCheckIn{Spec: []string{"4.3"}, Undo: true}).Run()
	wantDeps(t, m, []string{from[0], from[3]}, "_gd++/bind/4.3", ".gd++proj/spec/4.3")
}

func TestCheckInWarnsIfDone(t *testing.T) {
	withCheckInFS(t, "_gd++/bind/4.3", ".gd++proj/bind/4.4", ".gd++proj/engine/4.3")
	withQuiet(t, false)
	withTTY(t, false)
	out := captureStderr(t, (&CmdCheckIn{Engine: []string{"4.3"}, Undo: true}).Run)
	out += captureStderr(t, (&CmdCheckIn{All: true}).Run)
	for _, want := range []string{"[!] The Godot engine 4.3 is already ephemeral.\n", "[!] The Godot C++ bindings 4.3 is already checked in.\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want it to contain %q", out, want)
		}
	}
	if strings.Count(out, "already") != 2 {
		t.Errorf("output = %q, want exactly 2 warnings", out)
	}
}

func TestCheckInMissing(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		withCheckInFS(t, ".gd++proj/bind/4.3")
		withQuiet(t, false)
		(&CmdCheckIn{Bind: []string{"4.3"}, Spec: []string{"4.4"}}).Run()
		return
	}
	out, code := runFailHelper(t, "TestCheckInMissing")
	if want := "[x] The Godot API spec 4.4 is not in the project cache.\n"; code != 1 || out != want {
		t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, want)
	}
}

func TestCheckInInvalidArgs(t *testing.T) {
	cases := map[string]struct {
		c    CmdCheckIn
		want string
	}{
		"nothing":       {CmdCheckIn{}, "a --bind, --spec, --engine or --all option is required"},
		"undo_alone":    {CmdCheckIn{Undo: true}, "a --bind, --spec, --engine or --all option is required"},
		"names_and_all": {CmdCheckIn{Engine: []string{"a"}, EngineAll: true}, "--engine and --engine-all cannot be used together"},
		"all_and_names": {CmdCheckIn{All: true, Spec: []string{"a"}}, "--all cannot be used with other --bind, --spec or --engine options"},
		"all_and_all":   {CmdCheckIn{All: true, BindAll: true}, "--all cannot be used with other --bind, --spec or --engine options"},
		"bad_name":      {CmdCheckIn{Bind: []string{"../a"}}, `"../a" is not a valid dependency name`},
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
			if want := "[x] Invalid arguments: " + tc.want + ".\n"; out != want {
				t.Errorf("output = %q, want %q", out, want)
			}
		})
	}
}
