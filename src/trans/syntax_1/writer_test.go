package syntax_1

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCpp(t *testing.T) {
	for code, want := range map[string]string{
		"emit hit(1);":                                                           "(void) hit(1);",
		"rpc ping(); rpc(peer) x->ping(2);":                                      "_gdpp_rpc_ping(0); x->_gdpp_rpc_ping(peer, 2);",
		`rpc("ping"); rpc_id(1, "ping");`:                                        `rpc("ping"); rpc_id(1, "ping");`,
		"if (t && is_done t) x = claim t;":                                       "if (t && t.is_done()) x = t.claim();",
		"claim\n  this->tasks[i];":                                               "\n  this->tasks[i].claim();",
		"t.is_done(); claim (a);":                                                "t.is_done(); claim (a);",
		"cancel t; t.cancel(); cancel (t);":                                      "t.cancel(); t.cancel(); cancel (t);",
		"if (is_cancelled) return;":                                              "if (gdpp::is_cancelled()) return;",
		"call(string_name \"f\"); call(string_name\n  \"g\");":                   "call(GDPP_STRING_NAME(\"f\")); call(GDPP_STRING_NAME(\n\"g\"));",
		`string_name = "f"; s.string_name "f"; string_name u8"f";`:               `string_name = "f"; s.string_name "f"; string_name u8"f";`,
		"s.is_cancelled; p->is_cancelled; is_cancelled(); gdpp::is_cancelled();": "s.is_cancelled; p->is_cancelled; is_cancelled(); gdpp::is_cancelled();",
		"f(a, b,);":          "f(a, b);",
		"f(\n  a,\n  b,\n);": "f(\n  a,\n  b\n);",
		"g(string_name \"a\", string_name \"b\",);":                 "g(GDPP_STRING_NAME(\"a\"), GDPP_STRING_NAME(\"b\"));",
		"rpc f(1,); int a[] = {1, 2,};":                             "_gdpp_rpc_f(0, 1); int a[] = {1, 2,};",
		"p = body as Gd<Player>;":                                   "p = gdpp::cast<Gd<Player>>(body);",
		"if (auto t = body as Gd<Damageable>) {":                    "if (auto t = gdpp::cast<Gd<Damageable>>(body)) {",
		"p = body as Player *;":                                     "p = gdpp::cast<Player *>(body);",
		"x = this->a.b[i] as int;":                                  "x = gdpp::cast<int>(this->a.b[i]);",
		`f(get_node("X") as Gd<Node3D>, 1);`:                        `f(gdpp::cast<Gd<Node3D>>(get_node("X")), 1);`,
		"return (a + b) as float;":                                  "return gdpp::cast<float>((a + b));",
		"if (n as int < limit && m > 0)":                            "if (gdpp::cast<int>(n) < limit && m > 0)",
		"v as Gd<Mesh>; v as Async<Array<int>>;":                    "gdpp::cast<Gd<Mesh>>(v); gdpp::cast<Async<Array<int>>>(v);",
		"p as Gd<const Node3D>;":                                    "gdpp::cast<Gd<const Node3D>>(p);",
		"a + b as T; -x as T; a > b as T;":                          "a + gdpp::cast<T>(b); -gdpp::cast<T>(x); a > gdpp::cast<T>(b);",
		"claim t as int;":                                           "t.claim() as int;",
		"(claim t) as int;":                                         "gdpp::cast<int>((t.claim()));",
		"x as gd::Foo::Bar;":                                        "gdpp::cast<gd::Foo::Bar>(x);",
		"f(x\n  as\n  int);":                                        "f(gdpp::cast<int>(x)\n\n);",
		"int64_t(5) as Level; Array() as TypedArray<Node>;":         "gdpp::cast<Level>(int64_t(5)); gdpp::cast<TypedArray<Node>>(Array());",
		"auto m = create Mesh; auto t = create\n  godot::Timer;":    "auto m = gdpp::create<Mesh>(); auto t = gdpp::create<godot::Timer>()\n;",
		"Image::create(1, 2); create(x); s.create Foo; create = 1;": "Image::create(1, 2); create(x); s.create Foo; create = 1;",
		"destroy this->items[i]; destroy\n  b;":                     "gdpp::destroy(this->items[i]); gdpp::destroy(\n  b);",
		"queue_destroy this; queue_destroy b->c; p->queue_destroy x; queue_destroy(x);": "gdpp::queue_destroy(this); gdpp::queue_destroy(b->c); p->queue_destroy x; queue_destroy(x);",
		"destroy(x); s.destroy x; destroy = 1; destroy f(create A);":                    "destroy(x); s.destroy x; destroy = 1; gdpp::destroy(f(gdpp::create<A>()));",
		"assert child != nullptr;":                                                         `GDPP_ASSERT("child != nullptr", child != nullptr);`,
		"if (a) assert x; else { assert y; }":                                              `if (a) GDPP_ASSERT("x", x); else { GDPP_ASSERT("y", y); }`,
		"assert a &&\n  b;":                                                                "GDPP_ASSERT(\"a && b\", a &&\n  b);",
		"assert x as int > 0;":                                                             `GDPP_ASSERT("x as int > 0", gdpp::cast<int>(x) > 0);`,
		`assert f<A, B>(s) == "\\";`:                                                       `GDPP_ASSERT("f<A, B>(s) == \"\\\\\"", f<A, B>(s) == "\\");`,
		"assert all([](int x) { return x; });":                                             `GDPP_ASSERT("all([](int x) { return x; })", all([](int x) { return x; }));`,
		"assert !done; assert *p; assert(a || b);":                                         `GDPP_ASSERT("!done", !done); GDPP_ASSERT("*p", *p); GDPP_ASSERT("a || b", (a || b));`,
		"assert (a)(b); assert (a) || (b);":                                                `GDPP_ASSERT("(a)(b)", (a)(b)); GDPP_ASSERT("(a) || (b)", (a) || (b));`,
		"s.assert(x); assert = 1; assert; assert *= 2; f(assert x);":                       "s.assert(x); assert = 1; assert; assert *= 2; f(assert x);",
		"assert_void x; assert_val(y); [] { assert_val z; };":                              `GDPP_ASSERT_VOID("x", x); GDPP_ASSERT_VALUE("y", (y)); [] { GDPP_ASSERT_VALUE("z", z); };`,
		"s.assert_void(x); assert_val = 1; f(assert_void x);":                              "s.assert_void(x); assert_val = 1; f(assert_void x);",
		"#line 6 \"a.gd++\"\nassert x;\n#ifdef A\n  assert y;\n#endif":                     "#line 6 \"a.gd++\"\nGDPP_ASSERT(\"x\", x);\n#ifdef A\n  GDPP_ASSERT(\"y\", y);\n#endif",
		"f(a,\n  assert x);":                                                               "f(a,\n  assert x);",
		"#line 6 \"a.gd++\"\nx as int;\n#if A\nf(x).y as T;":                               "#line 6 \"a.gd++\"\ngdpp::cast<int>(x);\n#if A\ngdpp::cast<T>(f(x).y);",
		"x as int * y; (int) x as T; x as A as B; get<A>(x) as T; as = 1; int as; s.as T;": "x as int * y; (int) x as T; x as A as B; get<A>(x) as T; as = 1; int as; s.as T;",
	} {
		if got := cpp(code, assertAny); got != want {
			t.Errorf("cpp(%q) = %q, want %q", code, got, want)
		}
	}
	if got, want := cpp("assert x;", assertVoid), `GDPP_ASSERT_VOID("x", x);`; got != want {
		t.Errorf("cpp with GDPP_ASSERT_VOID = %q, want %q", got, want)
	}
	// In a lambda, its own return type picks the macro.
	for code, want := range map[string]string{
		"f([&] { assert x; }); assert y;":                          `f([&] { GDPP_ASSERT_VOID("x", x); }); GDPP_ASSERT_VALUE("y", y);`,
		"auto g = [](int a) mutable -> int { assert a; };":         `auto g = [](int a) mutable -> int { GDPP_ASSERT_VALUE("a", a); };`,
		"return [=]() -> void { assert a; };":                      `return [=]() -> void { GDPP_ASSERT_VOID("a", a); };`,
		"[]() -> std::pair<A, B> { [] { assert a; }; assert b; };": `[]() -> std::pair<A, B> { [] { GDPP_ASSERT_VOID("a", a); }; GDPP_ASSERT_VALUE("b", b); };`,
		"a[i]; delete[] p; [[nodiscard]] int f() { assert a; }":    `a[i]; delete[] p; [[nodiscard]] int f() { GDPP_ASSERT_VALUE("a", a); }`,
	} {
		if got := cpp(code, assertValue); got != want {
			t.Errorf("cpp(%q) = %q, want %q", code, got, want)
		}
	}
	if got, want := cpp("f([&]() -> int { assert x; return 1; });", assertVoid), `f([&]() -> int { GDPP_ASSERT_VALUE("x", x); return 1; });`; got != want {
		t.Errorf("cpp with GDPP_ASSERT_VOID = %q, want %q", got, want)
	}
	// In a coroutine, whose class has the signal died.
	for code, want := range map[string]string{
		"await anim->animation_finished;":                                   `co_await gdpp::signal(anim, gdpp::string_name<"animation_finished">());`,
		"await get_tree()->create_timer(1.0)->timeout;":                     `co_await gdpp::signal(get_tree()->create_timer(1.0), gdpp::string_name<"timeout">());`,
		"await died; await this->died;":                                     `co_await gdpp::signal(this, gdpp::string_name<"died">()); co_await gdpp::signal(this, gdpp::string_name<"died">());`,
		"await pending; await (this->task); await s.task; await f(x);":      "co_await pending; co_await (this->task); co_await s.task; co_await f(x);",
		`await a->string_name "x"; await string_name "y";`:                  `co_await gdpp::signal(a, gdpp::string_name<"x">()); co_await gdpp::signal(this, gdpp::string_name<"y">());`,
		`await a->string_name(p + "_f"); await string_name(n[i]);`:          `co_await gdpp::signal(a, StringName(p + "_f")); co_await gdpp::signal(this, StringName(n[i]));`,
		`await string_name(string_name "a"); int n = await count(claim t);`: `co_await gdpp::signal(this, StringName(gdpp::string_name<"a">())); int n = co_await count(t.claim());`,
		"await\n  a->b;":                                        "co_await gdpp::signal(\n  a, gdpp::string_name<\"b\">());",
		"await = 1; x.await; await; await -1;":                  "await = 1; x.await; await; await -1;",
		"return 1; [] { return 2; }; assert x;":                 `co_return 1; [] { return 2; }; GDPP_ASSERT_CO_VALUE("x", x);`,
		`call(string_name "f"); [] { call(string_name "g"); };`: `call(gdpp::string_name<"f">()); [] { call(GDPP_STRING_NAME("g")); };`,
	} {
		if got := cpp(code, assertCoValue, "died"); got != want {
			t.Errorf("cpp(%q) = %q, want %q", code, got, want)
		}
	}
	if got, want := cpp("return; assert x;", assertCoVoid), `co_return; GDPP_ASSERT_CO_VOID("x", x);`; got != want {
		t.Errorf("cpp with GDPP_ASSERT_CO_VOID = %q, want %q", got, want)
	}
}

func TestHasCoroutines(t *testing.T) {
	for std, want := range map[string]bool{"": true, "c++20": true, "gnu++2a": true, "c++23": true, "/std:c++latest": true, "C++26": true,
		"c++17": false, "gnu++14": false, "c++11": false, "c++98": false, "c++03": false} {
		if got := hasCoroutines(std); got != want {
			t.Errorf("hasCoroutines(%q) = %v, want %v", std, got, want)
		}
	}
}

// TestAssertMacros rewrites the C++ code in each testdata/assert/<context>/<case>.cpp as code in its context: the body
// of a function that returns void or a value, or a decl or impl block. It compares the result with <case>.golden.
func TestAssertMacros(t *testing.T) {
	contexts := map[string]string{"void": assertVoid, "value": assertValue, "decl": assertAny, "impl": assertAny}
	inputs, _ := filepath.Glob("testdata/assert/*/*.cpp")
	if len(inputs) == 0 {
		t.Fatal("No cases in testdata/assert.")
	}
	for _, input := range inputs {
		t.Run(strings.TrimPrefix(input, "testdata/assert/"), func(t *testing.T) {
			data, err := os.ReadFile(input)
			if err != nil {
				t.Fatal(err)
			}
			got := cpp(string(data), contexts[filepath.Base(filepath.Dir(input))])
			golden := strings.TrimSuffix(input, ".cpp") + ".golden"
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			if want, err := os.ReadFile(golden); err != nil {
				t.Errorf("Missing golden %s:\n%s", golden, got)
			} else if string(want) != got {
				t.Errorf("%s mismatch.\n--- got:\n%s\n--- want:\n%s", golden, got, want)
			}
		})
	}
}

func TestGlslToCpp(t *testing.T) {
	for code, want := range map[string]string{
		"v.xy":                  "v.swizzle<0, 1>()",
		"c.rgb * 2.0":           "c.swizzle<0, 1, 2>() * 2.0",
		"p.stpq.x":              "p.swizzle<0, 1, 2, 3>().x",
		"v.x + v.r + v.s":       "v.x + v.r + v.s",
		"v.xy = w; v.zw += w;":  "v.swizzle_ref<0, 1>() = w; v.swizzle_ref<2, 3>() += w;",
		"if (v.xy == w)":        "if (v.swizzle<0, 1>() == w)",
		"v.xr; s.size; v.xyzwx": "v.xr; s.size; v.xyzwx",
		"void f(in vec3 a, out vec3 b, inout float c)": "void f(vec3 a, vec3 &b, float &c)",
		"highp float x; lowp vec2 y;":                  "float x; vec2 y;",
		"bvec2 b = not(a);":                            "bvec2 b = glsl_not(a);",
		"shared float tile[64];":                       "static float tile[64];",
		"Ray r = Ray(o, vec3(0.0));":                   "Ray r = Ray{o, vec3(0.0)};",
		"struct Ray { vec3 o; };":                      "struct Ray { vec3 o; };",
		"Ray make(vec3 o)":                             "Ray make(vec3 o)",
		"v.xy\n  = w;":                                 "v.swizzle_ref<0, 1>()\n  = w;",
	} {
		if got := glslToCpp(code, []string{"Ray"}); got != want {
			t.Errorf("glslToCpp(%q) = %q, want %q", code, got, want)
		}
	}
}
