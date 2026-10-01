// highlight_test.go: tests for highlight.go.

package main

import "testing"

func TestHighlightCode(t *testing.T) {
	withTTY(t, true)
	cases := []struct{ lang, code, want string }{
		{"gd++", "@export var x: Node3D = f(\"a\", 1) // c\n/* a /* b */ c */",
			Styled("@export", CodePreProc) + " " + Styled("var", CodeKeyword) + " x: " + Styled("Node3D", CodeType) + " = " + Styled("f", CodeFunction) + "(" +
				Styled("\"a\"", CodeLiteral) + ", " + Styled("1", CodeLiteral) + ") " + Styled("// c", CodeComment) + "\n" + Styled("/* a /* b */ c */", CodeComment)},
		{"cpp", "#include <vector>\nint64_t x;", Styled("#include", CodePreProc) + " <vector>\n" + Styled("int64_t", CodeType) + " x;"},
		{"gdscript", "# c\nreturn", Styled("# c", CodeComment) + "\n" + Styled("return", CodeKeyword)},
		{"sh", "gd++ build --ship . # c", Styled("gd++", Bold) + " " + Styled("build", Bold) + " " + Styled("--ship", Cyan) + " . " + Styled("# c", CodeComment)},
		{"sh", "gd++ x [-j N] [--a | --b]\n  x | y", Styled("gd++", Bold) + " " + Styled("x", Bold) + " [" + Styled("-j", Cyan) + " N] [" + Styled("--a", Cyan) +
			" | " + Styled("--b", Cyan) + "]\n  x | " + Styled("y", Bold)},
		{"out", "[×] Oops.\n 3 | x\n   | ^\nHint: Fix.", Styled("[×]", Bold, Red) + " Oops.\n " + Styled("3 |", Gray) + " x\n   " + Styled("|", Gray) + " " +
			Styled("^", Red) + "\n" + Styled("Hint:", Bold) + " Fix."},
		{"toml", "[[class]]\nname = \"A\"", Styled("[[class]]", Bold) + "\n" + Styled("name", Cyan) + " = " + Styled("\"A\"", CodeLiteral)},
		{"gd++", "if (is_done t) x = claim\n  t; t.is_done(); claim = 1;", Styled("if", CodeKeyword) + " (" + Styled("is_done", CodeKeyword) + " t) x = " +
			Styled("claim", CodeKeyword) + "\n  t; t." + Styled("is_done", CodeFunction) + "(); claim = " + Styled("1", CodeLiteral) + ";"},
		{"gd++", "emit f(); rpc(1) g(); rpc_id(1, \"g\");", Styled("emit", CodeKeyword) + " " + Styled("f", CodeFunction) + "(); " + Styled("rpc", CodeKeyword) +
			"(" + Styled("1", CodeLiteral) + ") " + Styled("g", CodeFunction) + "(); " + Styled("rpc_id", CodeFunction) + "(" + Styled("1", CodeLiteral) + ", " +
			Styled("\"g\"", CodeLiteral) + ");"},
		{"gd++", "if (is_cancelled) x = is_done t;", Styled("if", CodeKeyword) + " (" + Styled("is_cancelled", CodeKeyword) + ") x = " +
			Styled("is_done", CodeKeyword) + " t;"},
		{"gd++", "cancel t; t.cancel();", Styled("cancel", CodeKeyword) + " t; t." + Styled("cancel", CodeFunction) + "();"},
		{"gd++", "n = cast<Node3D *>(x);", "n = " + Styled("cast", CodeKeyword) + "<" + Styled("Node3D", CodeType) + " *>(x);"},
		{"", "var x", "var x"},
	}
	for _, tc := range cases {
		if got := highlightCode(tc.code, tc.lang); got != tc.want {
			t.Errorf("highlightCode(%q, %q) = %q, want %q", tc.code, tc.lang, got, tc.want)
		}
	}
}
