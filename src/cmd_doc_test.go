// cmd_doc_test.go: tests for cmd_doc.go. Runs on memFS, in the package from
// cmd_build_test.go.

package main

import (
	"os"
	"path"
	"strings"
	"testing"

	"gd++/trans"
)

// withDocFS is withBuildFS plus godot-cpp headers, with the package's build
// cache synced and its names cache filled.
func withDocFS(t *testing.T) *memFS {
	m := withBuildFS(t)
	withTTY(t, false)
	write := func(file, text string) {
		m.MkdirAll(path.Dir(file), 0o755)
		m.nodes[file] = &memNode{data: []byte(text)}
	}
	write("/games/my_game/.gd++cache/bind/4.3/include/godot_cpp/variant/typed_array.hpp",
		"namespace godot {\n// An array of T.\ntemplate <typename T>\nclass TypedArray : public Array {\npublic:\n\t// Makes one.\n\tTypedArray();\n\tvoid assign(const Array &p_array);\n};\n}\n")
	write("/games/my_game/.gd++cache/bind/4.3/include/godot_cpp/variant/char_string.hpp",
		"namespace godot {\ntemplate <typename T>\nclass CharStringT {\npublic:\n\tconst T *get_data() const;\n};\n// A string of chars.\nusing CharString = CharStringT<char>;\n}\n")
	captureStderr(t, func() { generateBuildCache(LoadProject(Cwd()), LoadPackage(Cwd())) })
	write(pkgDir+".gd++build/build/godot-cpp/gen/include/godot_cpp/variant/array.hpp",
		"namespace godot {\nclass Array : public Object {\npublic:\n\tvoid push_back(const Variant &p_value);\n\tvoid push_back(int p_value);\n};\n}\n")
	write(pkgDir+".gd++build/build/godot-cpp/gen/include/godot_cpp/classes/object.hpp",
		"namespace godot {\nclass Object {\npublic:\n\t// Frees it.\n\tvoid free();\n};\nenum Error {\n\tOK,\n\tFAILED = 1\n};\n}\n")
	write(pkgDir+".gd++build/godot_names.toml", encodeToml(godotNamesCache{godotNamesVersion, []godotName{
		{"Array", "<godot_cpp/variant/array.hpp>", trans.Other, "class", "Object"},
		{"CharString", "<godot_cpp/variant/char_string.hpp>", trans.Other, "alias", "CharStringT<char>"},
		{"CharStringT", "<godot_cpp/variant/char_string.hpp>", trans.Other, "template class", ""},
		{"Error", "<godot_cpp/classes/object.hpp>", trans.Other, "enum", ""},
		{"Object", "<godot_cpp/classes/object.hpp>", trans.Object, "class", ""},
		{"TypedArray", "<godot_cpp/variant/typed_array.hpp>", trans.Other, "template class", "Array"},
		{"TypedDictionary", "<godot_cpp/variant/typed_dictionary.hpp>", trans.Other, "template class", "private Dictionary"},
		{"print_line", "<godot_cpp/variant/utility_functions.hpp>", trans.Other, "function", ""},
	}}))
	return m
}

func TestDoc(t *testing.T) {
	cases := map[string]struct {
		args []string
		want string
	}{
		"class": {[]string{"TypedArray"}, "#include <godot_cpp/variant/typed_array.hpp> // From godot-cpp.\n\n// An array of T.\ntemplate <typename T>\nclass TypedArray : public Array {\n" +
			"  // Makes one.\n  TypedArray();\n  void assign(const Array &p_array);\n" +
			"\n  // Inherited from Array:\n  void push_back(const Variant &p_value);\n  void push_back(int p_value);\n" +
			"\n  // Inherited from Object:\n\n  // Frees it.\n  void free();\n};\n"},
		"generated class": {[]string{"Array"}, "#include <godot_cpp/variant/array.hpp> // From the Godot API.\n\nclass Array : public Object {\n" +
			"  void push_back(const Variant &p_value);\n  void push_back(int p_value);\n" +
			"\n  // Inherited from Object:\n\n  // Frees it.\n  void free();\n};\n\nSee Godot's help for a description of Array.\n"},
		"enum":                    {[]string{"Error"}, "#include <godot_cpp/classes/object.hpp> // From the Godot API.\n\nenum Error {\n  OK,\n  FAILED = 1,\n};\n\nSee Godot's help for a description of Error.\n"},
		"enum value":              {[]string{"Error.FAILED"}, "#include <godot_cpp/classes/object.hpp> // From the Godot API.\n\nFAILED = 1,\n"},
		"member of a base's base": {[]string{"TypedArray.free"}, "#include <godot_cpp/classes/object.hpp> // From the Godot API.\n\n// Frees it.\nvoid free();\n"},
		"alias": {[]string{"CharString"}, "#include <godot_cpp/variant/char_string.hpp> // From godot-cpp.\n\n// A string of chars.\nusing CharString = CharStringT<char>;\n" +
			"\ntemplate <typename T>\nclass CharStringT {\n  const T *get_data() const;\n};\n"},
		"alias member": {[]string{"CharString.get_data"}, "#include <godot_cpp/variant/char_string.hpp> // From godot-cpp.\n\nconst T *get_data() const;\n"},
		"member":       {[]string{"TypedArray.assign"}, "#include <godot_cpp/variant/typed_array.hpp> // From godot-cpp.\n\nvoid assign(const Array &p_array);\n"},
		"runtime": {[]string{"Emitted"}, "#include <gd++/syntax_0.hpp> // From the GD++ runtime.\n\n// Emitted is the result of a signal's emit function. It's [[nodiscard]], so emitting a signal must be spelled\n" +
			"// `emit my_signal(42);`, which reads differently from a function call.\nstruct [[nodiscard]] Emitted {\n  // The result of emit_signal: OK, or why the emission failed.\n  Error error;\n};\n"},
		"runtime member":   {[]string{"Emitted.error"}, "#include <gd++/syntax_0.hpp> // From the GD++ runtime.\n\n// The result of emit_signal: OK, or why the emission failed.\nError error;\n"},
		"inherited member": {[]string{"TypedArray.push_back"}, "#include <godot_cpp/variant/array.hpp> // From the Godot API.\n\nvoid push_back(const Variant &p_value);\nvoid push_back(int p_value);\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			withDocFS(t)
			if out := captureStdout(t, (&CmdDoc{Args: tc.args}).Run); out != tc.want {
				t.Errorf("output = %q, want %q", out, tc.want)
			}
		})
	}
}

func TestDocNotifications(t *testing.T) {
	m := withDocFS(t)
	m.nodes[pkgDir+".gd++build/extension_api.json"] = &memNode{data: []byte(`{"classes": [
		{"name": "Object", "constants": [{"name": "NOTIFICATION_PREDELETE", "value": 1}, {"name": "NOTIFICATION_POSTINITIALIZE", "value": 0}]},
		{"name": "Array"},
		{"name": "CanvasItem", "constants": [{"name": "NOTIFICATION_DRAW", "value": 30}, {"name": "MARGIN", "value": 3}]}]}`)}
	want := "CanvasItem:\n  draw\nObject:\n  postinitialize\n  predelete\n"
	if out := captureStdout(t, (&CmdDoc{Notifications: true}).Run); out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestDocPackagePath(t *testing.T) {
	m := withDocFS(t)
	m.cwd = "/games/my_game"
	want := "#include <godot_cpp/variant/typed_array.hpp> // From godot-cpp.\n\nvoid assign(const Array &p_array);\n"
	if out := captureStdout(t, (&CmdDoc{Args: []string{"src/pkg", "TypedArray.assign"}}).Run); out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestDocIndex(t *testing.T) {
	// The runtime's types come between godot-cpp's, sorted by name.
	want := "class Array: Object\n" +
		"template class Async\n" +
		"alias CharString = CharStringT<char>\n" +
		"template class CharStringT\n" +
		"struct Emitted\n" +
		"enum Error\n" +
		"template class Gd: GdTrait\n" +
		"class Object\n" +
		"template class TypedArray: Array\n" +
		"template class TypedDictionary: private Dictionary\n" +
		"template class Weak\n"
	check := func(out string) {
		var got []string
		for _, line := range strings.SplitAfter(out, "\n") {
			if strings.Contains(want, line) {
				got = append(got, line)
			}
		}
		if strings.Join(got, "") != want {
			t.Errorf("output = %q, want these lines in order: %q", out, want)
		}
	}
	m := withDocFS(t)
	check(captureStdout(t, (&CmdDoc{}).Run))
	m.cwd = "/games/my_game"
	check(captureStdout(t, (&CmdDoc{Args: []string{"src/pkg"}, Index: true}).Run))
}

func TestDocFails(t *testing.T) {
	cases := map[string]struct {
		args          []string
		want          string
		index, notifs bool
	}{
		"index and name":          {[]string{"src/pkg", "Array"}, "[x] Invalid arguments: --index cannot be used with a name.\n", true, false},
		"notifications and name":  {[]string{"src/pkg", "Array"}, "[x] Invalid arguments: --notifications cannot be used with a name.\n", false, true},
		"index and notifications": {nil, "[x] Invalid arguments: --index cannot be used with --notifications.\n", true, true},
		"too many":                {[]string{"a", "b", "c"}, "[x] Invalid arguments: expected a name, optionally after a package path.\n", false, false},
		"not in package":          {[]string{"..", "Array"}, "[x] Path res://src is not contained in a GD++ package, run this in one or pass one, e.g. `gd++ doc PKG NAME`.\n", false, false},
		"similar names":           {[]string{"typed"}, "[x] There is no name typed in godot-cpp or the GD++ runtime. Similar names: TypedArray, TypedDictionary.\n", false, false},
		"unknown name":            {[]string{"Nothing"}, "[x] There is no name Nothing in godot-cpp or the GD++ runtime.\n", false, false},
		"unknown member":          {[]string{"TypedArray.nothing"}, "[x] Name TypedArray has no public member nothing.\n", false, false},
		"missing header":          {[]string{"TypedDictionary"}, "[x] Failed to find the header <godot_cpp/variant/typed_dictionary.hpp>, run `gd++ clean` to fix this.\n", false, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if os.Getenv("GDPP_FAIL_HELPER") == "1" {
				withDocFS(t)
				(&CmdDoc{Args: tc.args, Index: tc.index, Notifications: tc.notifs}).Run()
				return
			}
			out, code := runFailHelper(t, t.Name())
			if code != 1 || out != tc.want {
				t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, tc.want)
			}
		})
	}
}

func TestHighlightCpp(t *testing.T) {
	withTTY(t, true)
	got := highlightCpp("template <typename T>\nstatic const Array &get(int64_t p_x = 5, Other p_o) const", map[string]bool{"Array": true})
	want := Styled("template", CodeKeyword) + " <" + Styled("typename", CodeKeyword) + " T>\n" + Styled("static", CodeKeyword) + " " + Styled("const", CodeKeyword) + " " +
		Styled("Array", CodeType) + " &" + Styled("get", CodeFunction) + "(" + Styled("int64_t", CodeType) + " p_x = " + Styled("5", CodeLiteral) + ", Other p_o) " +
		Styled("const", CodeKeyword)
	if got != want {
		t.Errorf("highlightCpp = %q\nwant %q", got, want)
	}
	withTTY(t, false)
	if got := highlightCpp("const Array", nil); got != "const Array" {
		t.Errorf("highlightCpp without styles = %q, want it unchanged", got)
	}
}

// TestDocCommentStyles checks that each line of a multi-line comment is styled on its own, so the pager, which ends
// styles at the end of each line, shows them all as comments.
func TestDocCommentStyles(t *testing.T) {
	withDocFS(t)
	withTTY(t, true)
	out := captureStdout(t, (&CmdDoc{Args: []string{"Emitted"}}).Run)
	if line := Styled("// `emit my_signal(42);`, which reads differently from a function call.", CodeComment); !strings.Contains(out, line+"\n") {
		t.Errorf("output = %q, want the comment's second line styled on its own", out)
	}
}

func TestDocTabWidth(t *testing.T) {
	withDocFS(t)
	Args.TabWidth = 4
	t.Cleanup(func() { Args.TabWidth = 2 })
	want := "#include <godot_cpp/variant/char_string.hpp> // From godot-cpp.\n\ntemplate <typename T>\nclass CharStringT {\n    const T *get_data() const;\n};\n"
	if out := captureStdout(t, (&CmdDoc{Args: []string{"CharStringT"}}).Run); out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

// TestRuntimeComments checks that gd++ doc shows a comment for everything that the runtime headers declare in
// namespace gdpp, and for their public members.
func TestRuntimeComments(t *testing.T) {
	for syntax := 0; syntax <= trans.LatestSyntax; syntax++ {
		_, src, err := trans.RuntimeHeader(syntax)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range scanCppDecls(src, "gdpp") {
			for _, doc := range scanCppDocs(src, "gdpp", d.name) {
				if doc.comments == "" {
					t.Errorf("syntax %d: %s has no comment", syntax, doc.head)
				}
				for _, m := range doc.members {
					if m.comments == "" {
						t.Errorf("syntax %d: %s.%s has no comment", syntax, d.name, m.name)
					}
				}
			}
		}
	}
}
