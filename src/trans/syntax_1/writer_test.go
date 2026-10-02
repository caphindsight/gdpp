package syntax_1

import "testing"

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
		"g(string_name \"a\", string_name \"b\",);":                  "g(GDPP_STRING_NAME(\"a\"), GDPP_STRING_NAME(\"b\"));",
		"rpc f(1,); int a[] = {1, 2,};":                              "_gdpp_rpc_f(0, 1); int a[] = {1, 2,};",
		"p = body as Player *;":                                      "p = gdpp::cast<Player *>(body);",
		"x = this->a.b[i] as int;":                                   "x = gdpp::cast<int>(this->a.b[i]);",
		`f(get_node("X") as Node3D *, 1);`:                           `f(gdpp::cast<Node3D *>(get_node("X")), 1);`,
		"return (a + b) as float;":                                   "return gdpp::cast<float>((a + b));",
		"if (n as int < limit && m > 0)":                             "if (gdpp::cast<int>(n) < limit && m > 0)",
		"v as Ref<Mesh>; v as Async<Array<int>>;":                    "gdpp::cast<Ref<Mesh>>(v); gdpp::cast<Async<Array<int>>>(v);",
		"p as const Node3D *;":                                       "gdpp::cast<const Node3D *>(p);",
		"a + b as T; -x as T; a > b as T;":                           "a + gdpp::cast<T>(b); -gdpp::cast<T>(x); a > gdpp::cast<T>(b);",
		"claim t as int;":                                            "t.claim() as int;",
		"(claim t) as int;":                                          "gdpp::cast<int>((t.claim()));",
		"x as gd::Foo::Bar;":                                         "gdpp::cast<gd::Foo::Bar>(x);",
		"f(x\n  as\n  int);":                                         "f(gdpp::cast<int>(x)\n\n);",
		"int64_t(5) as Level; Array() as TypedArray<Node>;":          "gdpp::cast<Level>(int64_t(5)); gdpp::cast<TypedArray<Node>>(Array());",
		"assert child != nullptr;":                                   `GDPP_ASSERT("child != nullptr", child != nullptr);`,
		"if (a) assert x; else { assert y; }":                        `if (a) GDPP_ASSERT("x", x); else { GDPP_ASSERT("y", y); }`,
		"assert a &&\n  b;":                                          "GDPP_ASSERT(\"a && b\", a &&\n  b);",
		"assert x as int > 0;":                                       `GDPP_ASSERT("x as int > 0", gdpp::cast<int>(x) > 0);`,
		`assert f<A, B>(s) == "\\";`:                                 `GDPP_ASSERT("f<A, B>(s) == \"\\\\\"", f<A, B>(s) == "\\");`,
		"assert all([](int x) { return x; });":                       `GDPP_ASSERT("all([](int x) { return x; })", all([](int x) { return x; }));`,
		"assert !done; assert *p; assert(a || b);":                   `GDPP_ASSERT("!done", !done); GDPP_ASSERT("*p", *p); GDPP_ASSERT("a || b", (a || b));`,
		"assert (a)(b); assert (a) || (b);":                          `GDPP_ASSERT("(a)(b)", (a)(b)); GDPP_ASSERT("(a) || (b)", (a) || (b));`,
		"s.assert(x); assert = 1; assert; assert *= 2; f(assert x);": "s.assert(x); assert = 1; assert; assert *= 2; f(assert x);",
		"x as int * y; (int) x as T; x as A as B; get<A>(x) as T; as = 1; int as; s.as T;": "x as int * y; (int) x as T; x as A as B; get<A>(x) as T; as = 1; int as; s.as T;",
	} {
		if got := cpp(code, assertAny); got != want {
			t.Errorf("cpp(%q) = %q, want %q", code, got, want)
		}
	}
	if got, want := cpp("assert x;", assertVoid), `GDPP_ASSERT_VOID("x", x);`; got != want {
		t.Errorf("cpp with GDPP_ASSERT_VOID = %q, want %q", got, want)
	}
}
