package syntax_0

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
		"assert child != nullptr;":                                                          `GDPP_ASSERT("child != nullptr", child != nullptr);`,
		"if (a) assert x; else { assert y; }":                                               `if (a) GDPP_ASSERT("x", x); else { GDPP_ASSERT("y", y); }`,
		"assert a &&\n  b;":                                                                 "GDPP_ASSERT(\"a && b\", a &&\n  b);",
		"assert x as int > 0;":                                                              `GDPP_ASSERT("x as int > 0", gdpp::cast<int>(x) > 0);`,
		`assert f<A, B>(s) == "\\";`:                                                        `GDPP_ASSERT("f<A, B>(s) == \"\\\\\"", f<A, B>(s) == "\\");`,
		"assert all([](int x) { return x; });":                                              `GDPP_ASSERT("all([](int x) { return x; })", all([](int x) { return x; }));`,
		"assert !done; assert *p; assert(a || b);":                                          `GDPP_ASSERT("!done", !done); GDPP_ASSERT("*p", *p); GDPP_ASSERT("a || b", (a || b));`,
		"assert (a)(b); assert (a) || (b);":                                                 `GDPP_ASSERT("(a)(b)", (a)(b)); GDPP_ASSERT("(a) || (b)", (a) || (b));`,
		"s.assert(x); assert = 1; assert; assert *= 2; f(assert x);":                        "s.assert(x); assert = 1; assert; assert *= 2; f(assert x);",
		"assert_void x; assert_val(y); [] { assert_val z; };":                               `GDPP_ASSERT_VOID("x", x); GDPP_ASSERT_VALUE("y", (y)); [] { GDPP_ASSERT_VALUE("z", z); };`,
		"s.assert_void(x); assert_val = 1; f(assert_void x);":                               "s.assert_void(x); assert_val = 1; f(assert_void x);",
		"#line 6 \"a.gd++\"\nassert x;\n#ifdef A\n  assert y;\n#endif":                      "#line 6 \"a.gd++\"\nGDPP_ASSERT(\"x\", x);\n#ifdef A\n  GDPP_ASSERT(\"y\", y);\n#endif",
		"f(a,\n  assert x);":                                                                "f(a,\n  assert x);",
		"#line 6 \"a.gd++\"\nx as int;\n#if A\nf(x).y as T;":                                "#line 6 \"a.gd++\"\ngdpp::cast<int>(x);\n#if A\ngdpp::cast<T>(f(x).y);",
		"x as int * y; (int) x as T; x as A as B; get<A>(x) as T; as = 1; int as; s.as T;":  "x as int * y; (int) x as T; x as A as B; get<A>(x) as T; as = 1; int as; s.as T;",
		"connect(n, callable on_hit); f(callable Bullet::spawn);":                           "connect(n, gdpp::callable_method(this, &This::on_hit)); f(gdpp::callable_method(this, &Bullet::spawn));",
		"f(callable hud->score); f(callable a.b -> c);":                                     `f(gdpp::callable_member(hud, [](auto *o) { return &std::remove_pointer_t<decltype(o)>::score; }, GDPP_STRING_NAME("score"))); f(gdpp::callable_member(a.b , [](auto *o) { return &std::remove_pointer_t<decltype(o)>::c; }, GDPP_STRING_NAME("c")));`,
		`f(callable "ping"); f(callable hud->"refresh");`:                                   `f(gdpp::callable_name(this, GDPP_STRING_NAME("ping"))); f(gdpp::callable_name(hud, GDPP_STRING_NAME("refresh")));`,
		"t(callable [=](int x) -> int { return x + n; }); t(callable [] { emit done(); });": "t(gdpp::callable(this, [=](int x) -> int { return x + n; })); t(gdpp::callable(this, [] { (void) done(); }));",
		"t(callable(enemy) [this] { hit(); }); t(callable(nullptr) step);":                  "t(gdpp::callable(enemy, [this] { hit(); })); t(gdpp::callable(nullptr, step));",
		"Callable callable = x; f(callable); callable.call(); s.callable f; callable f(1);": "Callable callable = x; f(callable); callable.call(); s.callable f; callable f(1);",
		"(callable on_hit).bind(5);":                                                        "(gdpp::callable_method(this, &This::on_hit)).bind(5);",
	} {
		if got := cpp(code, assertAny, "this"); got != want {
			t.Errorf("cpp(%q) = %q, want %q", code, got, want)
		}
	}
	if got, want := cpp("f(callable [] {}); f(callable g);", assertVoid, "nullptr"), "f(gdpp::callable(nullptr, [] {})); f(gdpp::callable_method(nullptr, &This::g));"; got != want {
		t.Errorf("cpp in a static function = %q, want %q", got, want)
	}
	if got, want := cpp("assert x;", assertVoid, "this"), `GDPP_ASSERT_VOID("x", x);`; got != want {
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
		if got := cpp(code, assertValue, "this"); got != want {
			t.Errorf("cpp(%q) = %q, want %q", code, got, want)
		}
	}
	if got, want := cpp("f([&]() -> int { assert x; return 1; });", assertVoid, "this"), `f([&]() -> int { GDPP_ASSERT_VALUE("x", x); return 1; });`; got != want {
		t.Errorf("cpp with GDPP_ASSERT_VOID = %q, want %q", got, want)
	}
	// In a coroutine, whose class has the signal died.
	for code, want := range map[string]string{
		"await anim->animation_finished;":                                   `co_await gdpp::signal<"animation_finished">(anim);`,
		"await get_tree()->create_timer(1.0)->timeout;":                     `co_await gdpp::signal<"timeout">(get_tree()->create_timer(1.0));`,
		"await died; await this->died;":                                     `co_await gdpp::signal<"died">(this); co_await gdpp::signal<"died">(this);`,
		"await pending; await (this->task); await s.task; await f(x);":      "co_await pending; co_await (this->task); co_await s.task; co_await f(x);",
		`await a->string_name "x"; await string_name "y";`:                  `co_await gdpp::signal<"x">(a); co_await gdpp::signal<"y">(this);`,
		`await a->string_name(p + "_f"); await string_name(n[i]);`:          `co_await gdpp::signal(a, StringName(p + "_f")); co_await gdpp::signal(this, StringName(n[i]));`,
		`await string_name(string_name "a"); int n = await count(claim t);`: `co_await gdpp::signal(this, StringName(gdpp::string_name<"a">())); int n = co_await count(t.claim());`,
		`await get_node<AnimationPlayer>("A")->animation_finished; await Object::cast_to<SceneTree>(l)->string_name "f"; await a < b;`: `co_await gdpp::signal<"animation_finished">(get_node<AnimationPlayer>("A")); co_await gdpp::signal<"f">(Object::cast_to<SceneTree>(l)); co_await a < b;`,
		"await\n  a->b;":                                        "co_await gdpp::signal<\"b\">(\n  a);",
		"await = 1; x.await; await; await -1;":                  "await = 1; x.await; await; await -1;",
		"return 1; [] { return 2; }; assert x;":                 `co_return 1; [] { return 2; }; GDPP_ASSERT_CO_VALUE("x", x);`,
		`call(string_name "f"); [] { call(string_name "g"); };`: `call(gdpp::string_name<"f">()); [] { call(GDPP_STRING_NAME("g")); };`,
		`f(callable "g"); f(callable x->h);`:                    `f(gdpp::callable_name(this, gdpp::string_name<"g">())); f(gdpp::callable_member(x, [](auto *o) { return &std::remove_pointer_t<decltype(o)>::h; }, gdpp::string_name<"h">()));`,
	} {
		if got := cpp(code, assertCoValue, "this", "died"); got != want {
			t.Errorf("cpp(%q) = %q, want %q", code, got, want)
		}
	}
	if got, want := cpp("return; assert x;", assertCoVoid, "this"), `co_return; GDPP_ASSERT_CO_VOID("x", x);`; got != want {
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
			got := cpp(string(data), contexts[filepath.Base(filepath.Dir(input))], "this")
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
	// The swizzle methods of a struct with the fields xy and rg, which only their letters tell apart.
	foo := "template <char... C> decltype(auto) swizzle() {" +
		" if constexpr (std::is_same_v<std::integer_sequence<char, C...>, std::integer_sequence<char, 'x', 'y'>>) { return (xy); }" +
		" else if constexpr (std::is_same_v<std::integer_sequence<char, C...>, std::integer_sequence<char, 'r', 'g'>>) { return (rg); } }" +
		" template <char... C> decltype(auto) swizzle() const {" +
		" if constexpr (std::is_same_v<std::integer_sequence<char, C...>, std::integer_sequence<char, 'x', 'y'>>) { return (xy); }" +
		" else if constexpr (std::is_same_v<std::integer_sequence<char, C...>, std::integer_sequence<char, 'r', 'g'>>) { return (rg); } }" +
		" template <char... C> decltype(auto) swizzle_ref() { return swizzle<C...>(); } "
	for code, want := range map[string]string{
		"v.xy":                  "v.swizzle<'x', 'y'>()",
		"c.rgb * 2.0":           "c.swizzle<'r', 'g', 'b'>() * 2.0",
		"p.stpq.x":              "p.swizzle<'s', 't', 'p', 'q'>().x",
		"v.x + v.r + v.s":       "v.x + v.r + v.s",
		"v.xy = w; v.zw += w;":  "v.swizzle_ref<'x', 'y'>() = w; v.swizzle_ref<'z', 'w'>() += w;",
		"if (v.xy == w)":        "if (v.swizzle<'x', 'y'>() == w)",
		"v.xr; s.size; v.xyzwx": "v.xr; s.size; v.xyzwx",
		"void f(in vec3 a, out vec3 b, inout float c)": "void f(vec3 a, vec3 &b, float &c)",
		"highp float x; lowp vec2 y;":                  "float x; vec2 y;",
		"bvec2 b = not(a);":                            "bvec2 b = glsl_not(a);",
		"shared float tile[64];":                       "static float tile[64];",
		"Ray r = Ray(o, vec3(0.0));":                   "Ray r = Ray{o, vec3(0.0)};",
		"struct Ray { vec3 o; };":                      "struct Ray { vec3 o; };",
		"Ray make(vec3 o)":                             "Ray make(vec3 o)",
		"v.xy\n  = w;":                                 "v.swizzle_ref<'x', 'y'>()\n  = w;",
		"struct Foo { int xy; float rg[2]; }; f.xy = 1; f.rg[0] = v.rg.x;": "struct Foo { int xy; float rg[2]; " + foo +
			"}; f.swizzle_ref<'x', 'y'>() = 1; f.swizzle<'r', 'g'>()[0] = v.swizzle<'r', 'g'>().x;",
	} {
		if got := glslToCpp(code, []string{"Ray"}); got != want {
			t.Errorf("glslToCpp(%q) = %q, want %q", code, got, want)
		}
	}
}
