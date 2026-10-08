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
		{"gd++", "n = x as Node3D *;", "n = x " + Styled("as", CodeKeyword) + " " + Styled("Node3D", CodeType) + " *;"},
		{"gd++", "Gd<Node3D> n = x as Gd<Damageable>;", Styled("Gd", CodeKeyword) + "<" + Styled("Node3D", CodeType) + "> n = x " + Styled("as", CodeKeyword) + " " +
			Styled("Gd", CodeKeyword) + "<" + Styled("Damageable", CodeType) + ">;"},
		{"gd++", "as = x;", "as = x;"},
		{"gd++", "memnew(Node); memdelete(a); new Node; delete a;", "memnew(" + Styled("Node", CodeType) + "); memdelete(a); new " + Styled("Node", CodeType) + "; delete a;"},
		{"gd++", "queue_destroy b; n->queue_destroy(1);", Styled("queue_destroy", CodeKeyword) + " b; n->" + Styled("queue_destroy", CodeFunction) + "(" +
			Styled("1", CodeLiteral) + ");"},
		{"gd++", "b = create Bullet; destroy b; Image::create(1);", "b = " + Styled("create", CodeKeyword) + " " + Styled("Bullet", CodeType) + "; " +
			Styled("destroy", CodeKeyword) + " b; " + Styled("Image", CodeType) + "::" + Styled("create", CodeFunction) + "(" + Styled("1", CodeLiteral) + ");"},
		{"gd++", "call(string_name \"f\"); string_name = 1;", Styled("call", CodeFunction) + "(" + Styled("string_name", CodeKeyword) + " " +
			Styled("\"f\"", CodeLiteral) + "); string_name = " + Styled("1", CodeLiteral) + ";"},
		{"gd++", "assert x > 0;", Styled("assert", CodeKeyword) + " x > " + Styled("0", CodeLiteral) + ";"},
		{"gd++", "invoke f(glsl { float x; }); glsl = 1;", Styled("invoke", CodeKeyword) + " " + Styled("f", CodeFunction) + "(" + Styled("glsl", CodeKeyword) + " { " +
			Styled("float", CodeType) + " x; }); glsl = " + Styled("1", CodeLiteral) + ";"},
		{"gd++", "await a->done; await string_name(n); x.string_name(n);", Styled("await", CodeKeyword) + " a->done; " + Styled("await", CodeKeyword) + " " +
			Styled("string_name", CodeKeyword) + "(n); x." + Styled("string_name", CodeFunction) + "(n);"},
		{"gd++", "assert_void a; assert_val b;", Styled("assert_void", CodeKeyword) + " a; " + Styled("assert_val", CodeKeyword) + " b;"},
		{"gd++", "signal died\nsignal hit(damage: int)", Styled("signal", CodeKeyword) + " " + Styled("died", CodeFunction) + "\n" +
			Styled("signal", CodeKeyword) + " " + Styled("hit", CodeFunction) + "(damage: " + Styled("int", CodeType) + ")"},
		{"gd++", "@trace on process(dt: float) {\n  draw(dt);\n}\non ready {}\nvar ready: int\nvar on: int", Styled("@trace", CodePreProc) + " " +
			Styled("on process", CodeKeyword) + "(dt: " + Styled("float", CodeType) + ") {\n  " + Styled("draw", CodeFunction) + "(dt);\n}\n" +
			Styled("on ready", CodeKeyword) + " {}\n" + Styled("var", CodeKeyword) + " ready: " + Styled("int", CodeType) + "\n" +
			Styled("var", CodeKeyword) + " on: " + Styled("int", CodeType)},
		{"gd++", "@@save(\"a\") var x\n@@x on ready {}", Styled("@@save", CodePreProc) + "(" + Styled("\"a\"", CodeLiteral) + ") " + Styled("var", CodeKeyword) + " x\n" +
			Styled("@@x", CodePreProc) + " " + Styled("on ready", CodeKeyword) + " {}"},
		{"gd++", "annotation save", Styled("annotation", CodeKeyword) + " save"},
		{"gd++", "trait_name Damageable\nimplements Named, Saved\ntrait Named {}", Styled("trait_name", CodeKeyword) + " " + Styled("Damageable", CodeType) + "\n" +
			Styled("implements", CodeKeyword) + " " + Styled("Named", CodeType) + ", " + Styled("Saved", CodeType) + "\n" + Styled("trait", CodeKeyword) + " " +
			Styled("Named", CodeType) + " {}"},
		{"gd++", "// c\nmacro_library\nlocal x", Styled("// c", CodeComment) + "\n" + Styled("macro_library", CodeKeyword) + "\n" + Styled("local", CodeKeyword) + " x"},
		{"gd++", "macro  macro_library  var", Styled("macro", CodeKeyword) + "  " + Styled("macro_library", CodeKeyword) + "  " + Styled("var", CodeKeyword)},
		{"gd++", "template t(n) {\n  @@save var ${n}: int\n}", Styled("template", CodeKeyword) + " " + Styled("t", CodeFunction) + "(n) {\n  " +
			Styled("@@save", CodePreProc) + " " + Styled("var", CodeKeyword) + " " + Styled("${", CodePreProc) + "n" + Styled("}", CodePreProc) + ": " +
			Styled("int", CodeType) + "\n}"},
		{"gd++", "noimport  on  set", Styled("noimport", CodeKeyword) + "  " + Styled("on", CodeKeyword) + "  " + Styled("set", CodeKeyword)},
		{"gd++", "on = on || x;", "on = on || x;"},
		{"gd++", "@grid(size, 8, 8)\nshader f(size: Vector2i, t: Texture2D[rgba8]) -> void {\n  vec4 c = vec4(id);\n  emit x;\n}",
			Styled("@grid", CodePreProc) + "(size, " + Styled("8", CodeLiteral) + ", " + Styled("8", CodeLiteral) + ")\n" + Styled("shader", CodeKeyword) + " " +
				Styled("f", CodeFunction) + "(size: " + Styled("Vector2i", CodeType) + ", t: " + Styled("Texture2D", CodeType) + "[" + Styled("rgba8", CodeType) + "]) -> " +
				Styled("void", CodeType) + " {\n  " + Styled("vec4", CodeType) + " c = " + Styled("vec4", CodeType) + "(" + Styled("id", CodeKeyword) + ");\n  emit x;\n}"},
		{"gd++", "shader {\n  shared float t[4];\n}", Styled("shader", CodeKeyword) + " {\n  " + Styled("shared", CodeKeyword) + " " + Styled("float", CodeType) +
			" t[" + Styled("4", CodeLiteral) + "];\n}"},
		{"gd++", "// c\nshader_library\nout vec2 x;", Styled("// c", CodeComment) + "\n" + Styled("shader_library", CodeKeyword) + "\n" + Styled("out", CodeKeyword) + " " +
			Styled("vec2", CodeType) + " x;"},
		{"gd++", "shader  shader_library  var", Styled("shader", CodeKeyword) + "  " + Styled("shader_library", CodeKeyword) + "  " + Styled("var", CodeKeyword)},
		{"gd++", "var vec2 = id;", Styled("var", CodeKeyword) + " vec2 = id;"},
		{"gd++", "on(what: int) {}\non {}\nsignal s(on: int)", Styled("on", CodeKeyword) + "(what: " + Styled("int", CodeType) + ") {}\n" +
			Styled("on", CodeKeyword) + " {}\n" + Styled("signal", CodeKeyword) + " " + Styled("s", CodeFunction) + "(on: " + Styled("int", CodeType) + ")"},
		{"gd++", "var x: Node = $Hud/\"a b\" // c", Styled("var", CodeKeyword) + " x: " + Styled("Node", CodeType) + " = " +
			Styled("$Hud/\"a b\"", CodeLiteral) + " " + Styled("// c", CodeComment)},
		{"gd++", "x = %Health; y = a % b; y %= 2;", "x = " + Styled("%Health", CodeLiteral) + "; y = a % b; y %= " + Styled("2", CodeLiteral) + ";"},
		{"gdscript", "$Ünit.hide()", Styled("$Ünit", CodeLiteral) + "." + Styled("hide", CodeFunction) + "()"},
		{"", "var x", "var x"},
		{"lua", "local t = {} -- c\nif gd.pascal(x) then return [[s]] end --[[ a\nb ]]", Styled("local", CodeKeyword) + " t = {} " + Styled("-- c", CodeComment) +
			"\n" + Styled("if", CodeKeyword) + " " + Styled("gd", CodeType) + "." + Styled("pascal", CodeFunction) + "(x) " + Styled("then", CodeKeyword) + " " +
			Styled("return", CodeKeyword) + " " + Styled("[[s]]", CodeLiteral) + " " + Styled("end", CodeKeyword) + " " + Styled("--[[ a", CodeComment) + "\n" +
			Styled("b ]]", CodeComment)},
		{"lua", "x = ctx.scope .. 'a' // 2", "x = " + Styled("ctx", CodeType) + ".scope .. " + Styled("'a'", CodeLiteral) + " " + Styled("// 2", CodeComment)},
		{"lua", "/* a /* b */ c */ x", Styled("/* a /* b */ c */", CodeComment) + " x"},
		{"gd++", "macro stat(n, m = 1) {\n  local end_ = n // c\n}", Styled("macro", CodeKeyword) + " " + Styled("stat", CodeFunction) + "(n, m = " +
			Styled("1", CodeLiteral) + ") {\n  " + Styled("local", CodeKeyword) + " end_ = n " + Styled("// c", CodeComment) + "\n}"},
		{"gd++", "macro_name m(a)\nif a then end", Styled("macro_name", CodeKeyword) + " " + Styled("m", CodeFunction) + "(a)\n" + Styled("if", CodeKeyword) +
			" a " + Styled("then", CodeKeyword) + " " + Styled("end", CodeKeyword)},
		{"gd++", "template t(a) {\n  var ${a}: int\n}", Styled("template", CodeKeyword) + " " + Styled("t", CodeFunction) + "(a) {\n  " +
			Styled("var", CodeKeyword) + " " + Styled("${", CodePreProc) + "a" + Styled("}", CodePreProc) + ": " + Styled("int", CodeType) + "\n}"},
		{"gd++", "invoke stat(hp, body = code { x++; }); invoke s { n = 1 }", Styled("invoke", CodeKeyword) + " " + Styled("stat", CodeFunction) +
			"(hp, body = " + Styled("code", CodeKeyword) + " { x++; }); " + Styled("invoke", CodeKeyword) + " " + Styled("s", CodeFunction) + " { n = " +
			Styled("1", CodeLiteral) + " }"},
		{"gd++", "invoke {\n  local n = 1 // c\n}", Styled("invoke", CodeKeyword) + " {\n  " + Styled("local", CodeKeyword) + " n = " +
			Styled("1", CodeLiteral) + " " + Styled("// c", CodeComment) + "\n}"},
		{"gd++", "macro m() { gd.shader { name = 'f' } gd.shader_library {} }", Styled("macro", CodeKeyword) + " " + Styled("m", CodeFunction) + "() { " +
			Styled("gd", CodeType) + "." + Styled("shader", CodeFunction) + " { name = " + Styled("'f'", CodeLiteral) + " } " + Styled("gd", CodeType) + "." +
			Styled("shader_library", CodeFunction) + " {} }"},
		{"gd++", "macro { local n = 1 }", Styled("macro", CodeKeyword) + " { " + Styled("local", CodeKeyword) + " n = " + Styled("1", CodeLiteral) + " }"},
		{"gd++", "macro_library\nif a then end", Styled("macro_library", CodeKeyword) + "\n" + Styled("if", CodeKeyword) + " a " +
			Styled("then", CodeKeyword) + " " + Styled("end", CodeKeyword)},
		// A keyword where a name follows it, e.g. in a list of keywords.
		{"gd++", "import  invoke  macro", Styled("import", CodeKeyword) + "  " + Styled("invoke", CodeKeyword) + "  " + Styled("macro", CodeKeyword)},
		{"gd++", "std::invoke(f); invoke(f, 1); int invoke = 2;", "std::" + Styled("invoke", CodeFunction) + "(f); " + Styled("invoke", CodeFunction) +
			"(f, " + Styled("1", CodeLiteral) + "); " + Styled("int", CodeType) + " invoke = " + Styled("2", CodeLiteral) + ";"},
		{"gd++", "int end = local; code = 1;", Styled("int", CodeType) + " end = local; code = " + Styled("1", CodeLiteral) + ";"},
		{"gd++", "template <typename T> void f();", Styled("template", CodeKeyword) + " <" + Styled("typename", CodeKeyword) + " T> " +
			Styled("void", CodeType) + " " + Styled("f", CodeFunction) + "();"},
	}
	for _, tc := range cases {
		if got := highlightCode(tc.code, tc.lang); got != tc.want {
			t.Errorf("highlightCode(%q, %q) = %q, want %q", tc.code, tc.lang, got, tc.want)
		}
	}
}
