// cmd_doc_test.go: tests for cmd_doc.go. Runs on memFS, in the package from
// cmd_build_test.go.

package main

import (
	"os"
	"path"
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
	write("/games/my_game/.gd++proj/bind/4.3/include/godot_cpp/variant/typed_array.hpp",
		"namespace godot {\n// An array of T.\ntemplate <typename T>\nclass TypedArray : public Array {\npublic:\n\t// Makes one.\n\tTypedArray();\n\tvoid assign(const Array &p_array);\n};\n}\n")
	write("/games/my_game/.gd++proj/bind/4.3/include/godot_cpp/variant/char_string.hpp",
		"namespace godot {\ntemplate <typename T>\nclass CharStringT {\npublic:\n\tconst T *get_data() const;\n};\n// A string of chars.\nusing CharString = CharStringT<char>;\n}\n")
	captureStderr(t, func() { generateBuildCache(LoadProject(Cwd()), LoadPackage(Cwd())) })
	write(pkgDir+".gd++pkg/build/godot-cpp/gen/include/godot_cpp/variant/array.hpp",
		"namespace godot {\nclass Array : public Object {\npublic:\n\tvoid push_back(const Variant &p_value);\n\tvoid push_back(int p_value);\n};\n}\n")
	write(pkgDir+".gd++pkg/build/godot-cpp/gen/include/godot_cpp/classes/object.hpp",
		"namespace godot {\nclass Object {\npublic:\n\t// Frees it.\n\tvoid free();\n};\n}\n")
	write(pkgDir+".gd++pkg/godot_names.toml", encodeToml(godotNamesCache{godotNamesVersion, []godotName{
		{"Array", "<godot_cpp/variant/array.hpp>", trans.Other, "class", "Object"},
		{"CharString", "<godot_cpp/variant/char_string.hpp>", trans.Other, "alias", "CharStringT<char>"},
		{"CharStringT", "<godot_cpp/variant/char_string.hpp>", trans.Other, "template class", ""},
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
		"class": {[]string{"TypedArray"}, "// An array of T.\ntemplate <typename T>\nclass TypedArray : public Array\n    #include <godot_cpp/variant/typed_array.hpp>\n\n" +
			"    // Makes one.\n    TypedArray()\n    void assign(const Array &p_array)\n" +
			"\nInherited from Array:\n    void push_back(const Variant &p_value)\n    void push_back(int p_value)\n" +
			"\nInherited from Object:\n    // Frees it.\n    void free()\n"},
		"generated class": {[]string{"Array"}, "class Array : public Object\n    #include <godot_cpp/variant/array.hpp>\n\n" +
			"    void push_back(const Variant &p_value)\n    void push_back(int p_value)\n" +
			"\nInherited from Object:\n    // Frees it.\n    void free()\n\nSee Godot's help for a description of Array.\n"},
		"member of a base's base": {[]string{"TypedArray.free"}, "// Frees it.\nvoid free()\n    #include <godot_cpp/classes/object.hpp>\n"},
		"alias": {[]string{"CharString"}, "// A string of chars.\nusing CharString = CharStringT<char>\n    #include <godot_cpp/variant/char_string.hpp>\n\n" +
			"template <typename T>\nclass CharStringT\n    #include <godot_cpp/variant/char_string.hpp>\n\n    const T *get_data() const\n"},
		"alias member":     {[]string{"CharString.get_data"}, "const T *get_data() const\n    #include <godot_cpp/variant/char_string.hpp>\n"},
		"member":           {[]string{"TypedArray.assign"}, "void assign(const Array &p_array)\n    #include <godot_cpp/variant/typed_array.hpp>\n"},
		"inherited member": {[]string{"TypedArray.push_back"}, "void push_back(const Variant &p_value)\nvoid push_back(int p_value)\n    #include <godot_cpp/variant/array.hpp>\n"},
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

func TestDocPackagePath(t *testing.T) {
	m := withDocFS(t)
	m.cwd = "/games/my_game"
	want := "void assign(const Array &p_array)\n    #include <godot_cpp/variant/typed_array.hpp>\n"
	if out := captureStdout(t, (&CmdDoc{Args: []string{"src/pkg", "TypedArray.assign"}}).Run); out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestDocIndex(t *testing.T) {
	want := "class Array: Object\n" +
		"alias CharString = CharStringT<char>\n" +
		"template class CharStringT\n" +
		"class Object\n" +
		"template class TypedArray: Array\n" +
		"template class TypedDictionary: private Dictionary\n"
	m := withDocFS(t)
	if out := captureStdout(t, (&CmdDoc{}).Run); out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	m.cwd = "/games/my_game"
	if out := captureStdout(t, (&CmdDoc{Args: []string{"src/pkg"}, Index: true}).Run); out != want {
		t.Errorf("output with a package path = %q, want %q", out, want)
	}
}

func TestDocFails(t *testing.T) {
	cases := map[string]struct {
		args  []string
		want  string
		index bool
	}{
		"index and name": {[]string{"src/pkg", "Array"}, "[x] Invalid arguments: --index cannot be used with a name.\n", true},
		"too many":       {[]string{"a", "b", "c"}, "[x] Invalid arguments: expected a name, optionally after a package path.\n", false},
		"not in package": {[]string{"..", "Array"}, "[x] Path res://src is not contained in a GD++ package, run this in one or pass one, e.g. `gd++ doc PKG NAME`.\n", false},
		"similar names":  {[]string{"typed"}, "[x] There is no name typed in godot-cpp. Similar names: TypedArray, TypedDictionary.\n", false},
		"unknown name":   {[]string{"Nothing"}, "[x] There is no name Nothing in godot-cpp.\n", false},
		"unknown member": {[]string{"TypedArray.nothing"}, "[x] Name TypedArray has no public member nothing.\n", false},
		"missing header": {[]string{"TypedDictionary"}, "[x] Failed to find the header <godot_cpp/variant/typed_dictionary.hpp>, run `gd++ clean` to fix this.\n", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if os.Getenv("GDPP_FAIL_HELPER") == "1" {
				withDocFS(t)
				(&CmdDoc{Args: tc.args, Index: tc.index}).Run()
				return
			}
			out, code := runFailHelper(t, t.Name())
			if code != 1 || out != tc.want {
				t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, tc.want)
			}
		})
	}
}
