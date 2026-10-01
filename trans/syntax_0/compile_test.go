package syntax_0

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// stubs stand in for the headers generated from the other GD++ files that testdata/gen/deps.toml names.
var stubs = map[string]string{
	"Door.h":       "class Door : public Node {\n\tGDCLASS(Door, Node)\n\nprotected:\n\tstatic void _bind_methods() {}\n};\n",
	"Key.h":        "class Key : public RefCounted {\n\tGDCLASS(Key, RefCounted)\n\nprotected:\n\tstatic void _bind_methods() {}\n};\n",
	"Wall.h":       "class Wall : public Node {\n\tGDCLASS(Wall, Node)\n\nprotected:\n\tstatic void _bind_methods() {}\n};\n",
	"Level.h":      "enum class Level : int64_t {\n\tEASY = 0,\n\tHARD = 5,\n};\n",
	"Difficulty.h": "enum class Difficulty : int64_t {\n\tEASY = 0,\n\tHARD = 5,\n\tINSANE = 6,\n\tCUSTOM = 100,\n};\n",
	"Road.h": "class Road {\npublic:\n\tusing Base = Node;\n\tstatic constexpr const char *gdpp_name = \"Road\";\n" +
		"\texplicit Road(Base *p_object) :\n\t\t\t_gdpp_base(p_object) {}\n\nprotected:\n\tBase *_gdpp_base;\n};\n",
	"Settings.h": "class Settings {\npublic:\n\tusing Base = Resource;\n\tstatic constexpr const char *gdpp_name = \"Settings\";\n" +
		"\texplicit Settings(Base *p_object) :\n\t\t\t_gdpp_base(p_object) {}\n\nprotected:\n\tBase *_gdpp_base;\n};\n",
}

// TestCompile checks that the generated goldens compile. It needs a godot-cpp checkout with generated bindings
// (gen/include), named by GDPP_GODOT_CPP; the compiler is $CXX, or c++.
func TestCompile(t *testing.T) {
	root := os.Getenv("GDPP_GODOT_CPP")
	if root == "" {
		t.Skip("Set GDPP_GODOT_CPP to a godot-cpp checkout with generated bindings to compile the generated code.")
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	include := t.TempDir()
	os.MkdirAll(filepath.Join(include, "gd++"), 0o755)
	os.WriteFile(filepath.Join(include, RuntimeHeaderName), []byte(RuntimeHeader), 0o644)
	for name, text := range stubs {
		text = "#pragma once\n\n#include <gd++/syntax_0.hpp>\n\nnamespace godot {\n\n" + text + "\n} // namespace godot\n"
		os.WriteFile(filepath.Join(include, name), []byte(text), 0o644)
	}
	// Headers too, since those of externs and enums have no source that includes them.
	files, _ := filepath.Glob("testdata/gen/*/*.[ch]*")
	for _, file := range files {
		dir := filepath.Dir(file)
		if filepath.Base(dir) == "tutorial" {
			continue // The tutorial's C++ is illustrative, e.g. it uses my_value, which it never declares.
		}
		t.Run(filepath.Base(dir)+"/"+filepath.Base(file), func(t *testing.T) {
			args := []string{"-std=c++20", "-fsyntax-only", "-Wno-pragma-once-outside-header", "-I", dir, "-I", include,
				"-I", filepath.Join(root, "include"), "-I", filepath.Join(root, "gen", "include"), "-I", filepath.Join(root, "gdextension"), "-x", "c++", file}
			if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
				t.Errorf("%s failed to compile:\n%s", file, out)
			}
		})
	}
}
