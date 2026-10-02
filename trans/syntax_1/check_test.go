package syntax_1

import "testing"

func TestNodePath(t *testing.T) {
	cases := []struct {
		expr, path string
		off        int
		msg        string
	}{
		{expr: "$Foo", path: "Foo"},
		{expr: "$Foo/Bar", path: "Foo/Bar"},
		{expr: "$ Foo / Bar", path: "Foo/Bar"},
		{expr: "$/root/Main", path: "/root/Main"},
		{expr: "$%Foo", path: "%Foo"},
		{expr: "$/%Foo", path: "/%Foo"},
		{expr: "$Foo/%Bar", path: "Foo/%Bar"},
		{expr: "%Foo", path: "%Foo"},
		{expr: "%Foo/Bar", path: "%Foo/Bar"},
		{expr: `$"Spaced name"/X`, path: "Spaced name/X"},
		{expr: `$"../Sibling"`, path: "../Sibling"},
		{expr: `%"Foo"`, path: "%Foo"},
		{expr: `$'a"b'`, path: `a"b`},
		{expr: `$"""a"b"""`, path: `a"b`},
		{expr: `$r"a\d\"b"`, path: `a\d\"b`},
		{expr: `$"a\tb\\"`, path: "a\tb\\"},
		{expr: `$"ü\U01F600😀"`, path: "ü😀😀"},
		{expr: `$""`, path: ""},
		{expr: "$if/class/PI/_", path: "if/class/PI/_"},
		{expr: "$Ünit2", path: "Ünit2"},
		{expr: "$r", path: "r"},
		{expr: "$", off: 1, msg: `Expected a node name or a string after "$".`},
		{expr: "$/", off: 2, msg: `Expected a node name or a string after "/".`},
		{expr: "$Foo/", off: 5, msg: `Expected a node name or a string after "/".`},
		{expr: "$Foo//Bar", off: 5, msg: `A "/" is only valid at the start of the path, or after a node name.`},
		{expr: "$Foo%Bar", off: 4, msg: `A "%" is only valid at the start of a node name, after "$" or "/".`},
		{expr: "%%Foo", off: 1, msg: `A "%" is only valid at the start of a node name, after "$" or "/".`},
		{expr: "%/Foo", off: 1, msg: `A "/" is only valid at the start of the path, or after a node name.`},
		{expr: "$%/Foo", off: 2, msg: `A "/" is only valid at the start of the path, or after a node name.`},
		{expr: "$true", off: 1, msg: `Expected a node name or a string after "$".`},
		{expr: "$Foo/null", off: 5, msg: `Expected a node name or a string after "/".`},
		{expr: "$2D", off: 1, msg: `Expected a node name or a string after "$".`},
		{expr: `$&"x"`, off: 1, msg: `Expected a node name or a string after "$".`},
		{expr: `$^"x"`, off: 1, msg: `Expected a node name or a string after "$".`},
		{expr: `$"a\q"`, off: 3, msg: "Invalid escape in string."},
		{expr: `$"\ude00"`, off: 2, msg: "Invalid escape in string."},
		{expr: `$"\ud83d"`, off: 2, msg: "Invalid escape in string."},
		{expr: `$"abc`, off: 1, msg: "This string is never closed."},
		{expr: "$Foo.bar()", off: 4, msg: "A node path must be the whole initial value."},
		{expr: "$Foo Bar", off: 5, msg: "A node path must be the whole initial value."},
	}
	for _, tc := range cases {
		path, off, msg, _ := nodePath(tc.expr)
		if path != tc.path || off != tc.off || msg != tc.msg {
			t.Errorf("nodePath(%q) = %q, %d, %q; want %q, %d, %q", tc.expr, path, off, msg, tc.path, tc.off, tc.msg)
		}
	}
}

func TestCppString(t *testing.T) {
	for in, want := range map[string]string{"Hud/Score": `"Hud/Score"`, "a\"b\\c\td1": `"a\"b\\c\011d1"`, "Ünit": `U"Ünit"`} {
		if got := cppString(in); got != want {
			t.Errorf("cppString(%q) = %s, want %s", in, got, want)
		}
	}
}
