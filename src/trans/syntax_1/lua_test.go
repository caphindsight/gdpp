package syntax_1

import (
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
	text, err := x.invokeCode(&Invoke{Pos: pos, Name: "test", Args: &ArgList{}}, "Owner", 0, &Origin{Source: "test.gd++", Line: 1})
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
