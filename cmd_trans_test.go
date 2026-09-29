// cmd_trans_test.go: tests for cmd_trans.go. Runs on the real disk, since
// the transpiler reads files itself.

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gd++/trans"
)

// writeTransFiles writes files (name to text) to a temporary directory on the
// real disk, and returns the directory.
func writeTransFiles(t *testing.T, files map[string]string) string {
	withRealFS(t)
	withTTY(t, false)
	dir := t.TempDir()
	for name, text := range files {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const transPlayer = "/// A player.\nclass_name Player\nextends Node\n\nenum Suit { A, B = 5, C }\n\nextern Ground {\n  extends Node\n}\n\nfunc jump(suit: Suit) -> void {}\n"

func TestTransList(t *testing.T) {
	dir := writeTransFiles(t, map[string]string{"player.gd++": transPlayer, "empty.gd++": "// Nothing.\n"})
	out := captureStdout(t, (&CmdTrans{File: filepath.Join(dir, "player.gd++")}).Run)
	want := "" +
		"class   Player  extends Node\n" +
		"enum    Suit    A = 0, B = 5, C = 6\n" +
		"extern  Ground  extends Node\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	var stdout string
	stderr := captureStderr(t, func() { stdout = captureStdout(t, (&CmdTrans{File: filepath.Join(dir, "empty.gd++")}).Run) })
	if want := "[-] The file declares no classes, externs or enums.\n"; stdout != "" || stderr != want {
		t.Errorf("stdout, stderr = %q, %q, want \"\", %q", stdout, stderr, want)
	}
}

func TestTransOutputs(t *testing.T) {
	dir := writeTransFiles(t, map[string]string{"player.gd++": transPlayer})
	file := filepath.Join(dir, "player.gd++")
	cases := map[string]struct {
		c    CmdTrans
		want []string
	}{
		"header": {CmdTrans{Header: true, Object: []string{"Node"}}, []string{
			"#include <godot_cpp/classes/node.hpp>", "class Player : public Node {", "GDPP_ENUM_TAG(_gdpp_Player_Suit, \"Player.Suit\")"}},
		"source": {CmdTrans{Source: true, Object: []string{"Node=my/node.h"}}, []string{
			"#include \"player.h\"", "ClassDB::bind_method(D_METHOD(\"jump\", \"suit\"), &Player::_gdpp_jump);"}},
		"doc": {CmdTrans{Doc: "Player", Object: []string{"Node"}}, []string{
			`<class name="Player" inherits="Node"`, "\t\tA player.\n", `<constant name="SUIT_B" value="5" enum="Suit">`}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			tc.c.File = file
			out := captureStdout(t, tc.c.Run)
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
		})
	}
}

// transSpecProject is a project with a cached API spec, in writeTransFiles format.
var transSpecProject = map[string]string{
	"project.godot":                         testProjectTree["/games/my_game/"+projectFileName],
	".gd++proj/spec/4.3/extension_api.json": `{"classes": [{"name": "RefCounted", "is_refcounted": true}, {"name": "Node3D", "is_refcounted": false}]}`,
	"src/a.gd++":                            "class A {}\nclass B {\n  extends Node3D\n}\n",
}

// chdir changes the working directory for the duration of a test.
func chdir(t *testing.T, dir string) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(orig) })
}

func TestTransSpec(t *testing.T) {
	dir := writeTransFiles(t, transSpecProject)
	chdir(t, filepath.Join(dir, "src"))
	out := captureStdout(t, (&CmdTrans{File: "a.gd++", Header: true, Spec: "4.3"}).Run)
	for _, want := range []string{"#include <godot_cpp/classes/node3d.hpp>", "#include <godot_cpp/classes/ref_counted.hpp>", "class B : public Node3D {"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestTransRuntime(t *testing.T) {
	_, want, _ := trans.RuntimeHeader(0)
	if out := captureStdout(t, (&CmdTrans{Runtime: true}).Run); out != want {
		t.Errorf("output = %q, want the runtime header", out)
	}
}

func TestParseTransDep(t *testing.T) {
	cases := []struct {
		s    string
		kind trans.Kind
		want trans.Dependency
		err  string
	}{
		{"MeshInstance3D", trans.Object, trans.Dependency{Name: "MeshInstance3D", Include: "<godot_cpp/classes/mesh_instance3d.hpp>", Kind: trans.Object}, ""},
		{"Resource=<my/res.hpp>", trans.RefCounted, trans.Dependency{Name: "Resource", Include: "<my/res.hpp>", Kind: trans.RefCounted}, ""},
		{"Ground=ground.h", trans.Extern, trans.Dependency{Name: "Ground", Include: `"ground.h"`, Kind: trans.Extern}, ""},
		{"GroundCache", trans.RefCountedExtern, trans.Dependency{Name: "GroundCache", Include: `"ground_cache.h"`, Kind: trans.RefCountedExtern}, ""},
		{"Suit=\"cards.h\":A, B=0x10,C", trans.Enum, trans.Dependency{Name: "Suit", Include: `"cards.h"`, Kind: trans.Enum,
			Values: []trans.EnumValue{{Name: "A", Value: 0}, {Name: "B", Value: 16}, {Name: "C", Value: 17}}}, ""},
		{"Suit", trans.Enum, trans.Dependency{Name: "Suit", Include: `"suit.h"`, Kind: trans.Enum}, ""},
		{"3D", trans.Object, trans.Dependency{}, `"3D" is not a valid name`},
		{"Node=", trans.Object, trans.Dependency{}, "the include after = is empty"},
		{"Node:A", trans.Object, trans.Dependency{}, "only enums have values"},
		{"Suit:A,", trans.Enum, trans.Dependency{}, `"" is not a valid enum value name`},
		{"Suit:A=x", trans.Enum, trans.Dependency{}, `"x" is not an integer`},
	}
	for _, tc := range cases {
		got, err := parseTransDep(tc.s, tc.kind)
		switch {
		case tc.err != "" && (err == nil || err.Error() != tc.err):
			t.Errorf("parseTransDep(%q) error = %v, want %q", tc.s, err, tc.err)
		case tc.err == "" && (err != nil || !reflect.DeepEqual(got, tc.want)):
			t.Errorf("parseTransDep(%q) = %+v, %v, want %+v", tc.s, got, err, tc.want)
		}
	}
}

func TestSnakeCase(t *testing.T) {
	for name, want := range map[string]string{"Node": "node", "Node3D": "node3d", "MeshInstance2D": "mesh_instance2d",
		"HTTPRequest": "http_request", "GLTFDocument": "gltf_document", "AnimationNodeBlendSpace2D": "animation_node_blend_space2d"} {
		if got := snakeCase(name); got != want {
			t.Errorf("snakeCase(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestTransFails(t *testing.T) {
	cases := map[string]struct {
		c    CmdTrans
		want string
	}{
		"syntax error": {CmdTrans{File: "bad.gd++"},
			"[x] bad.gd++:1:1: Expected a declaration, but found name \"fun\".\n     1 | fun f() {}\n       | ^^^\n    Hint: Did you mean \"func\"?\n"},
		"semantic error": {CmdTrans{File: "player.gd++", Header: true},
			"[x] player.gd++:3:9: Unknown base class \"Node\".\n     3 | extends Node\n       |         ^^^^\n" +
				"    Hint: Types are Godot types, or classes, externs and enums from the dependencies or this file.\n"},
		"two outputs":               {CmdTrans{File: "player.gd++", Header: true, Source: true}, "[x] Invalid arguments: -H/--header, -S/--source, -D/--doc and --runtime cannot be used together.\n"},
		"runtime and file":          {CmdTrans{File: "player.gd++", Runtime: true}, "[x] Invalid arguments: --runtime cannot be used with a file.\n"},
		"no file":                   {CmdTrans{}, "[x] Invalid arguments: missing the GD++ file.\n"},
		"missing file":              {CmdTrans{File: "missing.gd++"}, "[x] There is no file at missing.gd++.\n"},
		"bad dependency":            {CmdTrans{File: "player.gd++", Enum: []string{"Suit:A=x"}}, "[x] Invalid arguments: --enum Suit:A=x: \"x\" is not an integer.\n"},
		"unknown class":             {CmdTrans{File: "player.gd++", Doc: "Enemy", Object: []string{"Node"}}, "[x] There is no class Enemy in player.gd++.\n"},
		"unknown syntax":            {CmdTrans{File: "player.gd++", Syntax: 9}, "[x] Unsupported GD++ syntax 9.\n"},
		"spec outside of a project": {CmdTrans{File: "player.gd++", Spec: "4.3"}, "[x] Path . is not contained in a Godot project.\n"},
		"missing spec": {CmdTrans{File: "src/a.gd++", Spec: "4.4"},
			"[x] Missing Godot API spec 4.4, run `gd++ fetch --spec 4.4` to fetch it.\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if os.Getenv("GDPP_FAIL_HELPER") == "1" {
				files := map[string]string{"player.gd++": transPlayer, "bad.gd++": "fun f() {}\n"}
				if tc.c.Spec == "4.4" {
					files = transSpecProject
				}
				chdir(t, writeTransFiles(t, files))
				tc.c.Run()
				return
			}
			out, code := runFailHelper(t, t.Name())
			if code != 1 || out != tc.want {
				t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, tc.want)
			}
		})
	}
}
