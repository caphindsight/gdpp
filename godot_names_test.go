// godot_names_test.go: tests for godot_names.go.

package main

import (
	"os"
	"reflect"
	"testing"

	"gd++/trans"
)

func TestScanCppDecls(t *testing.T) {
	src := `// A header. namespace godot { class InComment {}; }
#pragma once
#define MACRO_CLASS(m_name) \
	class m_name {};
#include <godot_cpp/core/defs.hpp>

class Global {};
typedef int GlobalInt;

namespace godot {

class Forward;
struct Point { int x = '}'; const char *s = "{"; };
class Node3D : public Node {
	GDEXTENSION_CLASS(Node3D, Node)
	class Inner {};
	struct Nested { int y; };
public:
	void method();
};
class RefCounted final : public Object {};
template <typename T, class U = Point>
class Ref {
	T *ptr;
};
template <>
struct PtrToArg<Point> {};
template <typename T>
struct PtrToArg<Ref<T>> {};
union Bits { int i; float f; };
enum Error { OK, FAILED };
enum class Side : int64_t { LEFT, RIGHT };
enum Opaque : int;
using Callback = void (*)(int);
typedef long long Int64;
typedef void (*Handler)(const char *);
MAKE_PTRARG(Point);
MAKE_TYPED_ARRAY(Point, Variant::OBJECT)
static_assert(sizeof(Point) == 8, "size");

namespace Math {
inline double sin(double x) { return x; }
}

namespace internal {
class Hidden {};
}

inline void print_line(const String &s) {
	auto lambda = [](int a) { return a; };
}
template <typename... Args>
void print_verbose(const Args &...args);
extern int counter;
constexpr int LIMIT_VALUE = 3;
int table[4] = { 1, 2, 3, 4 };
void Node3D::method() {}
bool operator==(const Point &a, const Point &b);

} // namespace godot

namespace godot::internal {
class AlsoHidden {};
}

namespace other {
class Elsewhere {};
}

extern "C" {
typedef struct { int x; } CStruct;
}
`
	var got []string
	bases := map[string]string{}
	for _, d := range scanCppDecls(src) {
		got = append(got, d.name)
		if d.base != "" {
			bases[d.name] = d.base
		}
	}
	want := []string{"Point", "Node3D", "RefCounted", "Ref", "Bits", "Error", "Side", "Callback", "Int64", "Handler",
		"Math", "internal", "print_line", "print_verbose", "counter", "LIMIT_VALUE", "table"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("names = %q, want %q", got, want)
	}
	if want := map[string]string{"Node3D": "Node", "RefCounted": "Object"}; !reflect.DeepEqual(bases, want) {
		t.Errorf("bases = %v, want %v", bases, want)
	}
}

func TestScanGodotNames(t *testing.T) {
	withMemFS(t, "/", map[string]string{
		"/cpp/include/godot_cpp/classes/wrapped.hpp":   "namespace godot { class Wrapped {}; }",
		"/cpp/include/godot_cpp/variant/array.hpp":     "namespace godot { class Array {}; template <typename T> class TypedArray : public Array {}; }",
		"/cpp/include/godot_cpp/variant/dup.hpp":       "namespace godot { class Array {}; }",
		"/cpp/include/notes.txt":                       "namespace godot { class NotAHeader {}; }",
		"/gen/include/godot_cpp/classes/object.hpp":    "namespace godot { class Object : public Wrapped {}; }",
		"/gen/include/godot_cpp/classes/node.hpp":      "#include <godot_cpp/classes/object.hpp>\n  #  include \"godot_cpp/variant/array.hpp\"\n#include <vector>\n#include <godot_cpp/classes/object.hpp>\nnamespace godot { class Node : public Object {}; }",
		"/gen/include/godot_cpp/classes/ref_counted.h": "namespace godot { class RefCounted : public Object {}; }",
		"/gen/include/godot_cpp/classes/resource.hpp":  "namespace godot { class Resource : public RefCounted {}; }",
	})
	got := scanGodotNames([]Path{ParsePath("/cpp/include"), ParsePath("/gen/include"), ParsePath("/missing")})
	want := []godotName{
		{"Array", "<godot_cpp/variant/array.hpp>", trans.Other},
		{"Node", "<godot_cpp/classes/node.hpp>", trans.Object},
		{"Object", "<godot_cpp/classes/object.hpp>", trans.Object},
		{"RefCounted", "<godot_cpp/classes/ref_counted.h>", trans.RefCounted},
		{"Resource", "<godot_cpp/classes/resource.hpp>", trans.RefCounted},
		{"TypedArray", "<godot_cpp/variant/array.hpp>", trans.Other},
		{"Wrapped", "<godot_cpp/classes/wrapped.hpp>", trans.Other},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("names = %v\nwant %v", got, want)
	}
}

// TestScanGodotCpp scans a real godot-cpp checkout, named by GDPP_GODOT_CPP.
// Its gen/include, with the generated bindings, is optional.
func TestScanGodotCpp(t *testing.T) {
	root := os.Getenv("GDPP_GODOT_CPP")
	if root == "" {
		t.Skip("Set GDPP_GODOT_CPP to a godot-cpp checkout to scan its headers.")
	}
	withRealFS(t)
	names := scanGodotNames([]Path{ParsePath(root).Cd("include"), ParsePath(root).Cd("gen/include")})
	found := map[string]godotName{}
	for _, n := range names {
		found[n.Name] = n
	}
	want := map[string]string{
		"TypedArray": "<godot_cpp/variant/typed_array.hpp>", "TypedDictionary": "<godot_cpp/variant/typed_dictionary.hpp>",
		"Ref": "<godot_cpp/classes/ref.hpp>", "ClassDB": "<godot_cpp/core/class_db.hpp>", "Math": "<godot_cpp/core/math.hpp>",
		"PropertyInfo": "<godot_cpp/core/property_info.hpp>", "MethodInfo": "<godot_cpp/core/object.hpp>",
	}
	for name, include := range want {
		if found[name].Include != include || found[name].Kind != trans.Other {
			t.Errorf("%s = %+v, want include %s and kind Other", name, found[name], include)
		}
	}
	for _, name := range []string{"memnew", "GDCLASS", "internal", "GDExtensionBool"} {
		if _, ok := found[name]; ok && name != "internal" {
			t.Errorf("%s shouldn't be found, but is %+v.", name, found[name])
		}
	}
	if _, ok := found["Node3D"]; ok {
		for name, kind := range map[string]trans.Kind{"Node3D": trans.Object, "Object": trans.Object, "Resource": trans.RefCounted, "Vector3": trans.Other} {
			if found[name].Kind != kind {
				t.Errorf("%s = %+v, want kind %d", name, found[name], kind)
			}
		}
	}
	t.Logf("Found %d names.", len(names))
}
