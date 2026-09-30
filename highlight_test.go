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
		{"", "var x", "var x"},
	}
	for _, tc := range cases {
		if got := highlightCode(tc.code, tc.lang); got != tc.want {
			t.Errorf("highlightCode(%q, %q) = %q, want %q", tc.code, tc.lang, got, tc.want)
		}
	}
}
