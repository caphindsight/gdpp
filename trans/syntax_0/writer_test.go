package syntax_0

import "testing"

func TestCpp(t *testing.T) {
	for code, want := range map[string]string{
		"emit hit(1);":                      "(void) hit(1);",
		"rpc ping(); rpc(peer) x->ping(2);": "_gdpp_rpc_ping(0); x->_gdpp_rpc_ping(peer, 2);",
		`rpc("ping"); rpc_id(1, "ping");`:   `rpc("ping"); rpc_id(1, "ping");`,
		"if (t && is_done t) x = claim t;":  "if (t && t.is_done()) x = t.claim();",
		"claim\n  this->tasks[i];":          "\n  this->tasks[i].claim();",
		"t.is_done(); claim (a);":           "t.is_done(); claim (a);",
		"cancel t; t.cancel(); cancel (t);": "t.cancel(); t.cancel(); cancel (t);",
		"if (is_cancelled) return;":         "if (gdpp::is_cancelled()) return;",
		"s.is_cancelled; p->is_cancelled; is_cancelled(); gdpp::is_cancelled();": "s.is_cancelled; p->is_cancelled; is_cancelled(); gdpp::is_cancelled();",
		"p = body as Player *;":                             "p = gdpp::cast<Player *>(body);",
		"x = this->a.b[i] as int;":                          "x = gdpp::cast<int>(this->a.b[i]);",
		`f(get_node("X") as Node3D *, 1);`:                  `f(gdpp::cast<Node3D *>(get_node("X")), 1);`,
		"return (a + b) as float;":                          "return gdpp::cast<float>((a + b));",
		"if (n as int < limit && m > 0)":                    "if (gdpp::cast<int>(n) < limit && m > 0)",
		"v as Ref<Mesh>; v as Async<Array<int>>;":           "gdpp::cast<Ref<Mesh>>(v); gdpp::cast<Async<Array<int>>>(v);",
		"p as const Node3D *;":                              "gdpp::cast<const Node3D *>(p);",
		"a + b as T; -x as T; a > b as T;":                  "a + gdpp::cast<T>(b); -gdpp::cast<T>(x); a > gdpp::cast<T>(b);",
		"claim t as int;":                                   "t.claim() as int;",
		"(claim t) as int;":                                 "gdpp::cast<int>((t.claim()));",
		"x as gd::Foo::Bar;":                                "gdpp::cast<gd::Foo::Bar>(x);",
		"f(x\n  as\n  int);":                                "f(gdpp::cast<int>(x)\n\n);",
		"int64_t(5) as Level; Array() as TypedArray<Node>;": "gdpp::cast<Level>(int64_t(5)); gdpp::cast<TypedArray<Node>>(Array());",
		"x as int * y; (int) x as T; x as A as B; get<A>(x) as T; as = 1; int as; s.as T;": "x as int * y; (int) x as T; x as A as B; get<A>(x) as T; as = 1; int as; s.as T;",
	} {
		if got := cpp(code); got != want {
			t.Errorf("cpp(%q) = %q, want %q", code, got, want)
		}
	}
}
