package syntax_1

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/participle/v2/lexer"

	"gd++/trans/meta"
)

// runLua runs code as the body of a macro invoked in C++ code, and returns the text it emits with gd.text.
func runLua(t *testing.T, code string) (string, error) {
	return runLuaWith(t, code, meta.Options{})
}

// runLuaWith is runLua with opts, whose package ID and prefix it sets.
func runLuaWith(t *testing.T, code string, opts meta.Options) (string, error) {
	t.Helper()
	pos := lexer.Position{Filename: "test.gd++", Line: 1, Column: 1}
	m := &Macro{Pos: pos, Name: "test", Body: &MacroBody{Block: Block{Pos: pos, TextPos: pos, Text: code}}}
	opts.PackageID, opts.PackagePrefix = "foo", "Foo"
	x := &expander{filename: "test.gd++", src: code, opts: opts,
		defs: map[string]*macroDef{"test": {m: m, file: "test.gd++", src: code}}, generated: map[int]string{}}
	text, err := x.invokeCode(&Invoke{Pos: pos, Name: "test", Args: &ArgList{}}, &scope{owner: "Owner"}, 0, &Origin{Source: "test.gd++", Line: 1})
	return oneLine(text), err
}

func TestLuaHelpers(t *testing.T) {
	for code, want := range map[string]string{
		// Case conversion, from every case into every other.
		`for _, s in ipairs({"max_health", "MaxHealth", "maxHealth", "MAX_HEALTH", "max-health", "Max Health"}) do
		   gd.text(gd.pascal(s) .. gd.camel(s) .. gd.snake(s) .. gd.upper_snake(s) .. gd.kebab(s) .. gd.title(s) .. ";")
		 end`: strings.Repeat("MaxHealthmaxHealthmax_healthMAX_HEALTHmax-healthMax Health;", 6),
		`gd.text(gd.snake("HTTPServer2D") .. " " .. gd.pascal("load_png_v2"))`: "http_server2_d LoadPngV2",
		// Quoting.
		`gd.text(gd.quote("a\"b\\c") .. gd.string_name("ready") .. gd.string_name("ü") .. gd.gdquote("x\"y"))`: `"a\"b\\c"string_name "ready"StringName(U"ü")"x\"y"`,
		// Strings.
		`gd.text(gd.join(gd.split("a,b,c", ","), "+") .. "|" .. gd.trim("  x ") .. "|" .. tostring(gd.starts_with("abc", "ab")) ..
		   tostring(gd.ends_with("abc", "bc")) .. "|" .. gd.replace("a.b.c", ".", "::") .. "|" .. #gd.lines("1\n2") .. "|" .. gd.indent("x\n\ny", 2))`: "a+b+c|x|truetrue|a::b::c|2|  x    y",
		// Tables, always in sorted order.
		`local t = {b = 2, a = 1, [3] = "c", [1] = "a"}
		 gd.text(gd.join(gd.keys(t), ",") .. "|" .. gd.join(gd.values(t), ","))
		 for k, v in pairs(t) do gd.text("|" .. k .. "=" .. v) end
		 for k, v in gd.sorted_pairs(t) do gd.text(";" .. k) end`: "1,3,a,b|a,c,1,2|1=a|3=c|a=1|b=2;1;3;a;b",
		`gd.text(gd.join(gd.map({1, 2, 3}, function(v) return v * 2 end), ",") .. "|" ..
		   gd.join(gd.filter({1, 2, 3, 4}, function(v) return v % 2 == 0 end), ",") .. "|" ..
		   tostring(gd.contains({1, "x"}, "x")) .. tostring(gd.contains({1}, 2)) .. "|" ..
		   gd.join(gd.range(3), ",") .. "|" .. gd.join(gd.range(2, 4), ",") .. "|" ..
		   gd.join(gd.values(gd.merge({a = 1, b = 2}, {b = 3})), ","))`: "2,4,6|2,4|truefalse|1,2,3|2,3,4|1,3",
		`local t = {x = {1}}; local c = gd.copy(t); c.x[1] = 2; gd.text(t.x[1])`: "1",
		// Values.
		`local c = gd.code("a + b"); gd.text(gd.type(c) .. gd.type(1) .. tostring(gd.is_code(c)) .. "|" .. c .. "|" .. tostring(c))`: "codenumbertrue|a + b|a + b",
		`gd.text(tostring(gd.is_ident("ok_1")) .. tostring(gd.is_ident("1x")) .. gd.ident("fine"))`:                                  "truefalsefine",
		// Names, lookup and context.
		`gd.text(gd.unique("tmp") .. " " .. gd.unique("tmp"))`:                                             "_gdpp_tmp_1 _gdpp_tmp_2",
		`gd.text(tostring(gd.has_macro("test")) .. tostring(gd.has_template("test")))`:                     "truefalse",
		`gd.text(ctx.scope .. ctx.class .. ctx.macro .. ctx.package.id .. ctx.package.prefix .. ctx.line)`: "cppOwnertestfooFoo1",
	} {
		got, err := runLua(t, code)
		if err != nil {
			t.Errorf("%s\nfailed: %v", code, err)
		} else if got != want {
			t.Errorf("%s\n got: %s\nwant: %s", code, got, want)
		}
	}
}

func TestLuaSandbox(t *testing.T) {
	for _, code := range []string{`io.write("x")`, `os.exit(1)`, `require("x")`, `dofile("x")`, `load("x")`, `math.random()`, `print("x")`} {
		if _, err := runLua(t, code); err == nil {
			t.Errorf("%s ran, but the sandbox should stop it.", code)
		}
	}
	start := time.Now()
	if _, err := runLuaWith(t, "while true do end", meta.Options{MacroTimeout: 1}); err == nil || !strings.Contains(err.Error(), "ran for more than 1 second.") {
		t.Errorf("An endless loop gave %v.", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("An endless loop ran for %v, despite a limit of 1 second.", d)
	}
}

func TestLuaErrors(t *testing.T) {
	for code, want := range map[string]string{
		`gd.error("bad input", "fix it")`:  "test.gd++:1:1: Bad input.",
		`gd.error_at("nope", "bad input")`: "test.gd++:1:1: Bad input.",
		`gd.func { name = "f" }`:           "gd.func can't be used in C++ code: only gd.text can.",
		`gd.text({})`:                      "expected text",
		`gd.annotation({}, "export")`:      `annotation names start with @, or @@ for user annotations, e.g. "@export"`,
	} {
		if _, err := runLua(t, code); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s gave %v, want %q.", code, err, want)
		}
	}
}

func TestLuaDepth(t *testing.T) {
	// Each gd.invoke nests one level deeper, like an invocation in generated code.
	for _, tc := range []struct {
		depth int
		want  string
	}{{0, "nested more than 64 levels"}, {3, "nested more than 3 levels"}} {
		if _, err := runLuaWith(t, `gd.invoke("test")`, meta.Options{MacroDepth: tc.depth}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("MacroDepth %d gave %v, want %q.", tc.depth, err, tc.want)
		}
	}
}

// expandPackage expands main.gd++ of a package of files, with the others' macros, templates and macro libraries as
// dependencies, like the build does.
func expandPackage(t *testing.T, files map[string]string) (string, error) {
	t.Helper()
	var opts meta.Options
	for name, src := range files {
		decls, err := ListMacros(name, src)
		if err != nil {
			return "", err
		}
		for _, d := range decls {
			if name != "main.gd++" {
				kind := map[meta.DeclKind]meta.Kind{meta.MacroDecl: meta.Macro, meta.TemplateDecl: meta.Template, meta.LibraryDecl: meta.MacroLibrary, meta.AnnotationDecl: meta.Annotation}[d.Kind]
				opts.Dependencies = append(opts.Dependencies, meta.Dependency{Name: d.Name, Kind: kind, Source: src, File: name})
			}
		}
	}
	return Expand("main.gd++", files["main.gd++"], opts)
}

func TestLuaLibraries(t *testing.T) {
	main := `class_name Foo
extends Node

var early: int = invoke { gd.text(add(2, 3)) }
invoke getter("x")
invoke counter()

func f() -> void {
  invoke { gd.text("print(" .. add(1, 1) .. ");") }
  invoke twice(4);
}

macro { function add(a, b) return a + b end }
macro twice(n) { gd.text("print(" .. add(n, n) .. ");") }
macro counter(start = add(10, 1)) { gd.var { name = "count", type = "int", init = start } }
template getter(name) {
  func get_${name}() -> int { return ${add(1, 2)}; }
}
`
	for _, tc := range []struct {
		files map[string]string
		want  []string
	}{
		// In main.gd++ itself, before and after the library: macro blocks, macros, templates and parameter defaults.
		{map[string]string{"main.gd++": main},
			[]string{"var early: int = 5", "return 3;", "var count: int = 11", "print(2);", "print(8);"}},
		// Libraries in other files call each other's functions, whatever their order, and gd.invoke sees them too.
		{map[string]string{
			"main.gd++": "class_name Foo\nextends Node\nvar e: bool = invoke { gd.invoke(\"show\", { 10 }) }\n",
			"a.gd++":    "macro { function is_even(n) if n == 0 then return true end return is_odd(n - 1) end }\nmacro show(n) { gd.text(tostring(is_even(n))) }\n",
			"b.gd++":    "macro_library\nfunction is_odd(n) if n == 0 then return false end return is_even(n - 1) end\n",
		}, []string{"var e: bool = true"}},
	} {
		got, err := expandPackage(t, tc.files)
		if err != nil {
			t.Errorf("%v failed: %v", tc.files, err)
			continue
		}
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("%v\n got: %s\nwant: %s", tc.files, got, w)
			}
		}
	}
}

func TestLuaLibraryErrors(t *testing.T) {
	use := "class_name Foo\nextends Node\nvar x: int = invoke { gd.text(1) }\n"
	for _, tc := range []struct {
		files map[string]string
		want  string
	}{
		{map[string]string{"main.gd++": use + "macro { gd.text(\"x\") }\n"},
			"main.gd++:4:1: Macro library failed: gd and ctx only work inside functions, since macro libraries only define functions."},
		{map[string]string{"main.gd++": use + "macro { local f = ctx.file }\n"}, "gd and ctx only work inside functions"},
		{map[string]string{"main.gd++": use + "macro {\n  error(\"broken\")\n}\n"}, "main.gd++:5:3: Macro library failed: broken."},
		{map[string]string{"main.gd++": use + "macro { function pairs() end }\n"},
			"main.gd++:4:1: This macro library redefines pairs, which Lua or GD++ already defines."},
		{map[string]string{"main.gd++": use, "a.gd++": "macro { function f() end }\n", "b.gd++": "\n\nmacro { function f() end }\n"},
			"b.gd++:3:1: This macro library redefines f, which the macro library at a.gd++:1 defines already."},
		{map[string]string{"main.gd++": use + "template { }\n"}, "main.gd++:4:1: Templates need a name and parameters"},
		{map[string]string{"main.gd++": use + "/// Doc.\nmacro { }\n"}, "Macro libraries have no doc comments"},
		{map[string]string{"main.gd++": use, "lib.gd++": "macro_library\nlocal = 1\n"}, "lib.gd++:2:7: Macro library failed:"},
		{map[string]string{"main.gd++": "class Foo {\n  macro { }\n}\n"}, "main.gd++:2:3:"},
	} {
		if _, err := expandPackage(t, tc.files); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v gave %v, want %q.", tc.files, err, tc.want)
		}
	}
}

func TestLuaMembers(t *testing.T) {
	// describe is a Lua function that lists ctx.members as text, e.g. "var health int gen=false".
	const describe = `macro {
  function describe()
    local out = {}
    for _, m in ipairs(ctx.members) do
      table.insert(out, m.kind .. " " .. (m.name or m.type or "") .. " " .. (m.type or m.ret or "") .. " gen=" .. tostring(m.generated) .. (m.generated_by or ""))
    end
    return gd.quote(gd.join(out, "/"))
  end
}
`
	for _, tc := range []struct {
		src  string
		want []string
	}{
		// Reset functions for the int vars: written before and after the invocation, but not generated.
		{`class_name Foo
extends Node
var health: int = 100
var label: String
invoke extra()
invoke {
  for _, m in ipairs(ctx.members) do
    if m.kind == "var" and m.type == "int" and not m.generated then
      gd.func { name = "reset_" .. m.name, ret = "void", body = m.name .. " = 0;" }
    end
  end
}
var mana: int = 50
macro extra() { gd.var { name = "bonus", type = "int" } }
`, []string{"func reset_health() -> void {health = 0;}", "func reset_mana() -> void {mana = 0;}"}},
		// What earlier invocations generated is visible, with who generated it, and later ones' isn't.
		{`class_name Foo
extends Node
var a: int
invoke extra()
var all: String = invoke { gd.text(describe()) }
invoke { gd.var { name = "seen", type = "String", init = describe() } }
invoke extra2()
var b: float
macro extra() { gd.var { name = "x", type = "int" } }
macro extra2() { gd.var { name = "y", type = "int" } }
` + describe, []string{
			`var seen: String = "var a int gen=false/var x int gen=trueextra/var all String gen=false/var b float gen=false"`,
			`var all: String = "var a int gen=false/var x int gen=trueextra/var all String gen=false/` +
				`var seen String gen=true{ ... }/var y int gen=trueextra2/var b float gen=false"`,
		}},
		// Every kind of member, with its fields.
		{`class_name Foo
extends Node
/// The health.
@export_range(0, 100, "suffix", true) var health: int = 10
var p: int { get { return 1; } set(v) { } }
func f(a: int, b = 2) -> void { a++; }
signal hit(by: Node)
enum MAX = 3
enum Suit { HEARTS, SPADES = HEARTS | 8 }
ctor { }
on process(delta: float) { }
decl impl { }
import Node3D
invoke {
  local m = ctx.members
  local h = m[1]
  local a = h.annotations[1]
  gd.var { name = "out", type = "String", init = gd.quote(gd.join({
    h.kind, h.doc, a[1], a[2], type(a[2]), a[4], tostring(a[5]), tostring(h.init),
    m[2].kind, tostring(m[2].get), m[2].set_param,
    m[3].kind, m[3].params[1].type, tostring(m[3].params[2].default), tostring(m[3].body), m[3].ret,
    m[4].kind, m[4].params[1].type,
    m[5].kind, m[5].value, m[6].kind, m[6].values[1].name, m[6].values[2].value,
    m[7].kind, m[8].kind, m[8].name, m[8].param, m[8].param_type, m[9].kind, m[10].kind, m[10].type,
  }, "/")) }
}
`, []string{`var out: String = {"var/The health./@export_range/0/number/suffix/true/10/var/ return 1;/v/` +
			`func/int/2/ a++;/void/signal/Node/enum/3/enum/HEARTS/HEARTS | 8/ctor/on/process/delta/float/decl_impl/import/Node3D"}`}},
		// At the file's top level: classes and enum types, and copies made by emitting them again.
		{`invoke {
  for _, c in ipairs(ctx.members) do
    if c.kind == "class" then
      gd.class { name = c.name .. "Copy", extends = c.extends, body = function()
        for _, m in ipairs(c.members) do gd[m.kind](m) end
      end }
    end
  end
}
class Foo {
  extends Node
  @export @@tag(1, "a") var x: int = 1
  func f(a: int) -> int { return a; }
}
enum Suit { HEARTS }
`, []string{"class FooCopy {", "extends Node", `@export @@tag(1, "a") var x: int = 1`, "func f(a: int) -> int { return a;}"}},
		// Shaders and shader libraries, read and emitted again.
		{`invoke {
  for _, c in ipairs(ctx.members) do
    if c.kind == "class" then
      gd.class { name = c.name .. "Copy", extends = c.extends, body = function()
        for _, m in ipairs(c.members) do gd[m.kind](m) end
      end }
    end
  end
}
class Foo {
  extends RefCounted
  shader { const float K = 2.0; }
  @sync shader twice(n: int, v: PackedFloat32Array) -> PackedFloat32Array { return v[id] * K; }
}
`, []string{"class FooCopy {", "shader { const float K = 2.0;}",
			"@sync shader twice(n: int, v: PackedFloat32Array) -> PackedFloat32Array { return v[id] * K;}"}},
		// A class's traits, read as implements, and passed on to a generated class.
		{`invoke {
  for _, c in ipairs(ctx.members) do
    if c.kind == "class" then
      gd.class { name = c.name .. "Copy", extends = c.extends, implements = c.implements, body = function()
        for _, m in ipairs(c.members) do gd[m.kind](m) end
      end }
    end
  end
}
class Foo {
  extends Node
  implements Saveable, Named
  implements Labeled
}
`, []string{"class FooCopy {", "extends Node", "implements Saveable, Named, Labeled"}},
		// gd.annotation finds built-in and user annotations, of members and of ctx.
		{`@@kind(item)
class_name Foo
extends Node
@export @@save var x: int
@@key("k", 2) var y: int
invoke {
  local x, y = ctx.members[1], ctx.members[2]
  local function show(args) return args and (#args .. ":" .. gd.join(gd.map(args, tostring), ",")) or "nil" end
  gd.var { name = "out", type = "String", annotations = { "@@made" }, init = gd.quote(gd.join({ show(gd.annotation(x, "@export")),
    show(gd.annotation(x, "@@save")), show(gd.annotation(x, "@save")), show(gd.annotation(x, "@@export")), show(gd.annotation(y, "@@key")),
    show(gd.annotation(ctx, "@@kind")), show(gd.annotation(y, "@@nope")) }, "/")) }
}
`, []string{`@@made var out: String = "0:/0:/nil/nil/2:k,2/1:item/nil"`}},
	} {
		got, err := expandPackage(t, map[string]string{"main.gd++": tc.src})
		if err != nil {
			t.Errorf("%s\nfailed: %v", tc.src, err)
			continue
		}
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s\n got: %s\nwant: %s", tc.src, got, w)
			}
		}
	}
}

func TestLuaShaderLibraries(t *testing.T) {
	// other.gd++ generates a shader block for the package, with a macro of main.gd++.
	const main = `macro consts(k) { gd.shader_library { body = "const float K = " .. k .. ";" } }
class Foo {
  extends RefCounted
  @sync shader twice(n: int, v: PackedFloat32Array) -> PackedFloat32Array { return v[id] * K; }
}
`
	const other = "invoke consts(2)\n"
	decls, err := ListMacros("other.gd++", other)
	if err != nil || !slices.ContainsFunc(decls, func(d meta.Declaration) bool { return d.Kind == meta.ShaderLibraryDecl }) {
		t.Fatalf("ListMacros(other.gd++) = %v, %v: want a ShaderLibraryDecl", decls, err)
	}
	u, err := parseUnit("main.gd++", main, meta.Options{Dependencies: []meta.Dependency{{Kind: meta.ShaderLibrary, Source: other, File: "other.gd++"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(u.shaderLibs) != 1 || u.shaderLibs[0].file != "other.gd++" || !strings.Contains(u.shaderLibs[0].b.Text, "const float K = 2;") {
		t.Errorf("shaderLibs = %+v, want K from other.gd++", u.shaderLibs)
	}
}
