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
#if GODOT_VERSION_MINOR >= 4
void branch(int a) {
#elif FOO
#if BAR
void nested() {
#endif
void branch(float a) {
#else
void branch() {
#endif
}

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
		"Math", "internal", "print_line", "print_verbose", "counter", "LIMIT_VALUE", "table", "branch"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("names = %q, want %q", got, want)
	}
	if want := map[string]string{"Node3D": "Node", "RefCounted": "Object"}; !reflect.DeepEqual(bases, want) {
		t.Errorf("bases = %v, want %v", bases, want)
	}
}

func TestScanCppDocs(t *testing.T) {
	src := `/* License. */
#pragma once
#define MAKE_TYPED(m_type) template <> class Typed<m_type> {};

namespace godot {

class Object;

// Unrelated.

// An array of T.
// Really.
template <typename T>
class GDE_EXPORT Typed : public Array {
	GDCLASS(Typed, Array)
	CLASSDB_FORWARD_METHODS;
	int hidden;
	friend class Other;
	friend bool operator==(const Typed &a, const Typed &b) { return true; }

public:
	enum Mode : int64_t { A = 1, B };
	struct Inner { int x; };
	class Later;
	static const int LIMIT = 4;
	int count{3};
	using Base = Array;

	// Assigns an array.
	_FORCE_INLINE_ void operator=(const Array &p_array) {
		set(p_array);
	}
	_FORCE_INLINE_ Typed(const Variant &p_variant) :
			Typed(Array(p_variant)), extra{1} {
	}
	Typed() = default;
	T *operator->() const { return nullptr; }
	operator Variant() const;
	Variant operator()(int a) const;
	template <typename U>
	static _FORCE_INLINE_ U get(const Variant &p_default = Variant::NIL, T p_items[] = {});
#if GODOT_VERSION_MINOR >= 4
	void grow(int64_t p_size) {
#else
	void grow() {
#endif
	}

protected:
	void _bind();
};
MAKE_TYPED(int64_t)

struct Point {
	int x = 0;
private:
	int y;
};

enum Side {
	// The left.
	LEFT = 1 << 0,
	RIGHT = MAKE(2, 3),
};

namespace Math {
double sin(double p_x);
float sin(float p_x);
}

void print(const String &p_text);
void print(int p_value) {}
using Str = const ::godot::StrT<char>;
using Callback = void (*)(int);
class Priv : Array {
public:
	void f();
};

} // namespace godot
`
	cases := map[string][]cppDoc{
		"Typed": {{
			comments: "// An array of T.\n// Really.\n", head: "template <typename T>\nclass Typed : public Array", base: "Array",
			members: []cppMember{
				{"Mode", "", "enum Mode : int64_t { A = 1, B }"},
				{"Inner", "", "struct Inner { ... }"},
				{"LIMIT", "", "static const int LIMIT = 4"},
				{"count", "", "int count{3}"},
				{"Base", "", "using Base = Array"},
				{"operator=", "// Assigns an array.\n", "void operator=(const Array &p_array)"},
				{"Typed", "", "Typed(const Variant &p_variant)"},
				{"Typed", "", "Typed() = default"},
				{"operator->", "", "T *operator->() const"},
				{"operator Variant", "", "operator Variant() const"},
				{"operator()", "", "Variant operator()(int a) const"},
				{"get", "", "template <typename U>\nstatic U get(const Variant &p_default = Variant::NIL, T p_items[] = {})"},
				{"grow", "", "void grow(int64_t p_size)"},
			},
		}},
		"Point":    {{head: "struct Point", members: []cppMember{{"x", "", "int x = 0"}}}},
		"Side":     {{head: "enum Side", members: []cppMember{{"LEFT", "// The left.\n", "LEFT = 1 << 0"}, {"RIGHT", "", "RIGHT = MAKE(2, 3)"}}}},
		"Math":     {{head: "namespace Math", members: []cppMember{{"sin", "", "double sin(double p_x)"}, {"sin", "", "float sin(float p_x)"}}}},
		"print":    {{head: "void print(const String &p_text)"}, {head: "void print(int p_value)"}},
		"Str":      {{head: "using Str = const ::godot::StrT<char>", alias: "StrT"}},
		"Callback": {{head: "using Callback = void (*)(int)"}},
		"Priv":     {{head: "class Priv : Array", members: []cppMember{{"f", "", "void f()"}}}},
		"Other":    nil,
	}
	for name, want := range cases {
		if got := scanCppDocs(src, name); !reflect.DeepEqual(got, want) {
			t.Errorf("scanCppDocs(%s) = %q\nwant %q", name, got, want)
		}
	}
}

func TestScanGodotNames(t *testing.T) {
	withMemFS(t, "/", map[string]string{
		"/cpp/include/godot_cpp/classes/wrapped.hpp": "namespace godot { class Wrapped {}; }",
		"/cpp/include/godot_cpp/variant/array.hpp":   "namespace godot { class Array {}; template <typename T> class TypedArray : public Array {}; using Arr = Array; typedef void (*Handler)(const char *); using Chars = CharStringT<char>; class Hash : Array {}; struct Pair : protected Array {}; void print_line(); }",
		"/cpp/include/godot_cpp/variant/dup.hpp":     "namespace godot { class Array {}; }",
		"/cpp/include/godot_cpp/core/defs.hpp": "namespace godot {\n#ifdef REAL_T_IS_DOUBLE\ntypedef double real_t;\n#else\ntypedef float real_t;\n#endif\n" +
			"#ifndef THREADS_ENABLED\nusing Lock = char;\n#elif FOO\nusing Lock = long;\n#else\nusing Lock = int;\n#endif\n}",
		"/cpp/include/notes.txt":                       "namespace godot { class NotAHeader {}; }",
		"/gen/include/godot_cpp/classes/object.hpp":    "namespace godot { class Object : public Wrapped {}; }",
		"/gen/include/godot_cpp/classes/node.hpp":      "#include <godot_cpp/classes/object.hpp>\n  #  include \"godot_cpp/variant/array.hpp\"\n#include <vector>\n#include <godot_cpp/classes/object.hpp>\nnamespace godot { class Node : public Object {}; }",
		"/gen/include/godot_cpp/classes/ref_counted.h": "namespace godot { class RefCounted : public Object {}; }",
		"/gen/include/godot_cpp/classes/resource.hpp":  "namespace godot { class Resource : public RefCounted {}; }",
	})
	got := scanGodotNames([]Path{ParsePath("/cpp/include"), ParsePath("/gen/include"), ParsePath("/missing")})
	want := []godotName{
		{"Arr", "<godot_cpp/variant/array.hpp>", trans.Other, "alias", "Array"},
		{"Array", "<godot_cpp/variant/array.hpp>", trans.Other, "class", ""},
		{"Chars", "<godot_cpp/variant/array.hpp>", trans.Other, "alias", "CharStringT<char>"},
		{"Handler", "<godot_cpp/variant/array.hpp>", trans.Other, "alias", "void (*)(const char *)"},
		{"Hash", "<godot_cpp/variant/array.hpp>", trans.Other, "class", "private Array"},
		{"Lock", "<godot_cpp/core/defs.hpp>", trans.Other, "alias", "long"},
		{"Node", "<godot_cpp/classes/node.hpp>", trans.Object, "class", "Object"},
		{"Object", "<godot_cpp/classes/object.hpp>", trans.Object, "class", "Wrapped"},
		{"Pair", "<godot_cpp/variant/array.hpp>", trans.Other, "struct", "protected Array"},
		{"RefCounted", "<godot_cpp/classes/ref_counted.h>", trans.RefCounted, "class", "Object"},
		{"Resource", "<godot_cpp/classes/resource.hpp>", trans.RefCounted, "class", "RefCounted"},
		{"TypedArray", "<godot_cpp/variant/array.hpp>", trans.Other, "template class", "Array"},
		{"Wrapped", "<godot_cpp/classes/wrapped.hpp>", trans.Other, "class", ""},
		{"print_line", "<godot_cpp/variant/array.hpp>", trans.Other, "function", ""},
		{"real_t", "<godot_cpp/core/defs.hpp>", trans.Other, "alias", "float"},
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
