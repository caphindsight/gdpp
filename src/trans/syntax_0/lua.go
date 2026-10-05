package syntax_0

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
	lua "github.com/yuin/gopher-lua"
)

// defaultMacroTimeout is how long one invocation may run, unless Options.MacroTimeout says otherwise.
const defaultMacroTimeout = 20 * time.Second

// timeout returns how long one invocation may run.
func (x *expander) timeout() time.Duration {
	if x.opts.MacroTimeout > 0 {
		return time.Duration(x.opts.MacroTimeout) * time.Second
	}
	return defaultMacroTimeout
}

// luaKeywords are Lua's reserved words, which can't name macro parameters.
var luaKeywords = map[string]bool{"and": true, "break": true, "do": true, "else": true, "elseif": true, "end": true, "false": true,
	"for": true, "function": true, "if": true, "in": true, "local": true, "nil": true, "not": true, "or": true, "repeat": true,
	"return": true, "then": true, "true": true, "until": true, "while": true}

// codeValue is a Code value in Lua: C++ code, a block, or an expression.
type codeValue struct {
	block *Block
	expr  bool
}

// scope is where emitters put what they generate: the invocation's place, or the body of a gd.class, gd.extern or
// C++ field function. Kind is "file", "class", "extern" or "cpp".
type scope struct {
	kind        string
	owner       string        // The class or extern, if any.
	annotations []*Annotation // The owner's annotations, which ctx.annotations shows.
	fileLevel   bool          // Whether it's the body of the file-level class or extern, which may hold inline ones too.
	members     []any         // What ctx.members shows: *Member, or at the file's top level, *Class, *Extern and *Enum.
	chunks      []chunk
}

// chunk is a generated item, or text (gd.text) when item is nil.
type chunk struct {
	item *topItem
	text string
}

// run is one invocation of a macro or template.
type run struct {
	x       *expander
	L       *lua.LState
	def     *macroDef
	inv     *Invoke
	scopes  []*scope
	depth   int   // How deeply the invocation is nested in others.
	failure error // A GD++ error that stopped the run, e.g. from gd.error.
	// Each parameter's value, and where its argument is, for errors about it.
	values map[string]lua.LValue
	argPos map[string]lexer.Position
	argLen map[string]int
}

// newRun returns a run of def for inv, with a sandboxed Lua state where the package's macro libraries ran, and a
// function that frees it. The error is the libraries'.
func (x *expander) newRun(def *macroDef, inv *Invoke, sc *scope, depth int) (*run, func(), error) {
	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	for _, lib := range []struct {
		name string
		open lua.LGFunction
	}{{lua.BaseLibName, lua.OpenBase}, {lua.TabLibName, lua.OpenTable}, {lua.StringLibName, lua.OpenString}, {lua.MathLibName, lua.OpenMath}} {
		L.Push(L.NewFunction(lib.open))
		L.Push(lua.LString(lib.name))
		L.Call(1, 0)
	}
	// No I/O, no loading code, and nothing random: the same input always generates the same code.
	for _, name := range []string{"dofile", "loadfile", "load", "loadstring", "require", "module", "print", "collectgarbage", "newproxy"} {
		L.SetGlobal(name, lua.LNil)
	}
	m := L.GetGlobal("math").(*lua.LTable)
	m.RawSetString("random", lua.LNil)
	m.RawSetString("randomseed", lua.LNil)
	ctx, cancel := context.WithTimeout(context.Background(), x.timeout())
	L.SetContext(ctx)
	r := &run{x: x, L: L, def: def, inv: inv, scopes: []*scope{sc}, depth: depth, values: map[string]lua.LValue{}, argPos: map[string]lexer.Position{},
		argLen: map[string]int{}}
	L.SetGlobal("pairs", L.NewFunction(r.pairs))
	err := r.libraries()
	L.SetGlobal("gd", r.gdTable())
	L.SetGlobal("ctx", r.ctxTable(sc))
	return r, func() {
		cancel()
		L.Close()
	}, err
}

// libraries runs the package's macro libraries, which define functions for the run's code. Meanwhile, gd and ctx
// fail: libraries emit nothing, and don't depend on the invocation. Two libraries can't define the same global.
func (r *run) libraries() error {
	L, globals := r.L, r.L.G.Global
	guard := L.NewTable()
	L.SetMetatable(guard, L.SetFuncs(L.NewTable(), map[string]lua.LGFunction{"__index": func(L *lua.LState) int {
		L.RaiseError("gd and ctx only work inside functions, since macro libraries only define functions")
		return 0
	}}))
	L.SetGlobal("gd", guard)
	L.SetGlobal("ctx", guard)
	owners := map[lua.LValue]*macroDef{} // The library that defined each global.
	for _, lib := range r.x.libs {
		before := map[lua.LValue]lua.LValue{}
		globals.ForEach(func(k, v lua.LValue) { before[k] = v })
		lr := *r
		lr.def = lib
		fn, err := lr.load(lib.m.Body.Text, lib.m.Body.TextPos, lib.file, nil)
		if err == nil {
			err = lr.call(fn)
		}
		if err != nil {
			return err
		}
		for _, k := range sortedKeys(globals) {
			old, ok := before[k]
			switch {
			case ok && old == globals.RawGet(k):
			case ok:
				msg := fmt.Sprintf("This macro library redefines %s, which Lua or GD++ already defines.", k)
				if o := owners[k]; o != nil {
					msg = fmt.Sprintf("This macro library redefines %s, which the macro library at %s:%d defines already.", k, o.file, o.m.Pos.Line)
				}
				return r.x.lineError(lib.file, lib.m.Pos.Line, msg, "Choose another name, or make it local.")
			default:
				owners[k] = lib
			}
		}
	}
	return nil
}

// ctxTable returns ctx: where and how the macro was invoked.
func (r *run) ctxTable(sc *scope) *lua.LTable {
	L, inv, opts := r.L, r.inv, r.x.opts
	t := L.NewTable()
	t.RawSetString("scope", lua.LString(sc.kind))
	if sc.owner != "" {
		t.RawSetString("class", lua.LString(sc.owner))
	}
	if inv.Doc != nil {
		t.RawSetString("doc", lua.LString(inv.Doc.Text))
	}
	t.RawSetString("members", r.declTables(sc.members, ""))
	if a := r.annotationTables(sc.annotations); a != lua.LNil {
		t.RawSetString("annotations", a)
	}
	t.RawSetString("file", lua.LString(r.x.filename))
	t.RawSetString("line", lua.LNumber(inv.Pos.Line))
	if inv.Body == nil {
		t.RawSetString("macro", lua.LString(inv.Name))
	}
	pkg := L.NewTable()
	for k, v := range map[string]string{"id": opts.PackageID, "prefix": opts.PackagePrefix, "path": opts.PackagePath, "std": opts.CppStandard} {
		if v != "" {
			pkg.RawSetString(k, lua.LString(v))
		}
	}
	t.RawSetString("package", pkg)
	return t
}

// fail stops the run with the GD++ error e.
func (r *run) fail(e *Error) {
	r.failure = e
	r.L.RaiseError("%s", e.Msg)
}

// failAt stops the run with an error at the invocation.
func (r *run) failAt(msg, hint string) {
	r.fail(r.invocationError(msg, hint))
}

// invocationError returns an error at the invocation that started the run.
func (r *run) invocationError(msg, hint string) *Error {
	return r.x.invocationError(r.inv, msg, hint)
}

// argError returns an error at an argument, at pos, or at the invocation if it has a Site: its arguments' positions
// are only known in the expanded file.
func (r *run) argError(pos lexer.Position, n int, msg, hint string) *Error {
	if r.inv.Site != nil {
		return r.invocationError(msg, hint)
	}
	return r.x.errorAt(pos, n, msg, hint)
}

// lineError returns an error that underlines line of file, a GD++ file of the package, from its first character
// that isn't a space.
func (x *expander) lineError(file string, line int, msg, hint string) *Error {
	src := x.sourceOf(file)
	text := ""
	if lines := strings.Split(src, "\n"); line >= 1 && line <= len(lines) {
		text = lines[line-1]
	}
	pos := lexer.Position{Filename: file, Line: line, Column: len(text) - len(strings.TrimLeft(text, " \t")) + 1}
	pos.Offset = offsetOf(src, pos)
	return (&Error{Pos: pos, Len: max(1, len(strings.TrimSpace(text))), Msg: msg, Hint: hint}).withSource(src)
}

// sourceOf returns the source of file: the expanded file, or a file whose macros and templates it uses.
func (x *expander) sourceOf(file string) string {
	for _, d := range x.defs {
		if d.file == file {
			return d.src
		}
	}
	for _, d := range x.libs {
		if d.file == file {
			return d.src
		}
	}
	return x.src
}

// call runs fn with args, and turns what went wrong into a GD++ error.
func (r *run) call(fn *lua.LFunction, args ...lua.LValue) error {
	r.L.Push(fn)
	for _, a := range args {
		r.L.Push(a)
	}
	err := r.L.PCall(len(args), 0, nil)
	switch {
	case err == nil:
		return nil
	case r.failure != nil:
		return r.failure
	case r.L.Context().Err() != nil:
		limit := fmt.Sprintf("%d seconds", int(r.x.timeout().Seconds()))
		if r.x.timeout() == time.Second {
			limit = "1 second"
		}
		return r.invocationError(fmt.Sprintf("Macro %s ran for more than %s.", r.inv.Name, limit),
			"It probably loops forever. If it only needs more time, raise the limit with --macro-timeout.")
	}
	return r.luaError(err)
}

var (
	luaWhere        = regexp.MustCompile(`^(.*):(\d+):$`) // What L.Where returns: a chunk's file and line.
	luaRuntimeError = regexp.MustCompile(`^(.*?):(\d+): (?s)(.*)$`)
	luaSyntaxError  = regexp.MustCompile(`^(.*?) line:(\d+)\(column:(\d+)\) near '(.*)':\s*(.*)$`)
)

// luaError turns a Lua error into a GD++ error at the line of the Lua code that failed.
func (r *run) luaError(err error) error {
	msg := err.Error()
	if ae, ok := err.(*lua.ApiError); ok {
		msg = ae.Object.String()
	}
	msg = strings.TrimSpace(msg)
	file, line, col, near := "", 0, 1, ""
	if m := luaSyntaxError.FindStringSubmatch(msg); m != nil {
		file, near, msg = m[1], m[4], m[5]
		line, _ = strconv.Atoi(m[2])
		col, _ = strconv.Atoi(m[3])
		msg += fmt.Sprintf(" near %q", near)
	} else if m := luaRuntimeError.FindStringSubmatch(msg); m != nil {
		file, msg = m[1], m[3]
		line, _ = strconv.Atoi(m[2])
	}
	src := r.x.sourceOf(file)
	what := "Macro library"
	if r.def.m.Name != "" {
		what = capitalize(r.def.m.what()) + " " + r.def.m.Name
	}
	msg = fmt.Sprintf("%s failed: %s", what, strings.TrimSuffix(msg, "."))
	if line == 0 {
		return r.invocationError(msg+".", "")
	}
	lines := strings.Split(src, "\n")
	text := ""
	if line <= len(lines) {
		text = lines[line-1]
	}
	if near == "" {
		col = len(text) - len(strings.TrimLeft(text, " \t")) + 1
	}
	pos := lexer.Position{Filename: file, Line: line, Column: col}
	pos.Offset = offsetOf(src, pos)
	return (&Error{Pos: pos, Len: max(1, len(strings.TrimSpace(text))), Msg: msg + "."}).withSource(src)
}

// offsetOf returns the byte offset in src of pos's line and column.
func offsetOf(src string, pos lexer.Position) int {
	off := 0
	for line := 1; line < pos.Line; line++ {
		i := strings.IndexByte(src[off:], '\n')
		if i < 0 {
			return len(src)
		}
		off += i + 1
	}
	for col := 1; col < pos.Column && off < len(src) && src[off] != '\n'; col++ {
		_, n := utf8.DecodeRuneInString(src[off:])
		off += n
	}
	return off
}

// load compiles Lua code that starts at pos, in the named file, so that Lua reports that file's lines. Params are
// locals set to the chunk's arguments, with their defaults when nil.
func (r *run) load(code string, pos lexer.Position, file string, params []*MacroParam) (*lua.LFunction, error) {
	var prefix strings.Builder
	if len(params) > 0 {
		var names []string
		for _, p := range params {
			names = append(names, p.Name)
		}
		prefix.WriteString("local " + strings.Join(names, ", ") + " = ...; ")
		for _, p := range params {
			if d := p.Default; d != nil {
				expr := d.Expr
				if d.Block != nil {
					expr = "{" + d.Block.Text + "}"
				}
				expr = strings.ReplaceAll(expr, "\n", " ")
				fmt.Fprintf(&prefix, "if %s == nil then %s = (%s) end; ", p.Name, p.Name, expr)
			}
		}
	}
	chunk := strings.Repeat("\n", max(pos.Line-1, 0)) + prefix.String() + code
	fn, err := r.L.Load(strings.NewReader(chunk), file)
	if err != nil {
		return nil, r.luaError(err)
	}
	return fn, nil
}

// args returns the values of inv's arguments in Lua, in the order of params.
func (r *run) args(params []*MacroParam) ([]lua.LValue, error) {
	inv, def := r.inv, r.def
	values := make([]lua.LValue, len(params))
	for i := range values {
		values[i] = lua.LNil
	}
	index := func(name string) int {
		return slices.IndexFunc(params, func(p *MacroParam) bool { return p.Name == name })
	}
	set := make([]bool, len(params))
	bind := func(i int, key string, v lua.LValue, pos lexer.Position, n int) error {
		var names []string
		for _, p := range params {
			names = append(names, p.Name)
		}
		switch {
		case key != "" && i < 0:
			hint := fmt.Sprintf("Its parameters: %s.", strings.Join(names, ", "))
			if s := suggest(key, names...); s != "" {
				hint = fmt.Sprintf("Did you mean %q?", s)
			}
			if len(names) == 0 {
				hint = "It takes no parameters."
			}
			return r.argError(pos, n, fmt.Sprintf("%s %s has no parameter %q.", capitalize(def.m.what()), def.m.Name, key), hint)
		case i >= len(params):
			return r.argError(pos, n, fmt.Sprintf("%s %s takes %d arguments, but got more.", capitalize(def.m.what()), def.m.Name, len(params)), "")
		case set[i]:
			return r.argError(pos, n, fmt.Sprintf("Parameter %s is set twice.", params[i].Name), "")
		}
		values[i], set[i] = v, true
		r.values[params[i].Name], r.argPos[params[i].Name], r.argLen[params[i].Name] = v, pos, n
		return nil
	}
	if inv.Args.Table != nil {
		b := inv.Args.Table
		fn, err := r.load("return {"+b.Text+"}", b.TextPos, r.x.filename, nil)
		if err != nil {
			return nil, err
		}
		r.L.Push(fn)
		if err := r.L.PCall(0, 1, nil); err != nil {
			return nil, r.luaError(err)
		}
		t := r.L.Get(-1).(*lua.LTable)
		r.L.Pop(1)
		for i := 1; i <= t.Len(); i++ {
			if err := bind(i-1, "", t.RawGetInt(i), inv.Pos, inv.span()); err != nil {
				return nil, err
			}
		}
		var err2 error
		t.ForEach(func(k, v lua.LValue) {
			if s, ok := k.(lua.LString); ok && err2 == nil {
				err2 = bind(index(string(s)), string(s), v, inv.Pos, inv.span())
			}
		})
		return values, err2
	}
	for i, arg := range inv.Args.Args {
		v, err := r.toLua(arg.Value)
		if err != nil {
			return nil, err
		}
		j, n := i, valueLen(arg.Value)
		if arg.Key != "" {
			j, n = index(arg.Key), len(arg.Key)
		}
		if err := bind(j, arg.Key, v, arg.Pos, n); err != nil {
			return nil, err
		}
	}
	return values, nil
}

// toLua converts a value written in GD++ into Lua.
func (r *run) toLua(v *Value) (lua.LValue, error) {
	L := r.L
	switch v.Kind {
	case "string":
		s, err := strconv.Unquote(v.Text)
		if err != nil {
			s = v.Text[strings.IndexByte(v.Text, '"')+1 : len(v.Text)-1]
		}
		return lua.LString(s), nil
	case "number":
		raw := strings.ReplaceAll(v.Text, "'", "")
		if f, err := strconv.ParseFloat(raw, 64); err == nil {
			return lua.LNumber(f), nil
		}
		if n, err := strconv.ParseInt(raw, 0, 64); err == nil {
			return lua.LNumber(n), nil
		}
		return nil, r.x.errorAt(v.Pos, len(v.Text), fmt.Sprintf("%s isn't a number that macros understand.", v.Text), "Write a decimal number, e.g. 42 or -1.5, or a hexadecimal one, e.g. 0x10.")
	case "name":
		switch v.Text {
		case "true", "false":
			return lua.LBool(v.Text == "true"), nil
		case "null":
			return lua.LNil, nil
		}
		return lua.LString(v.Text), nil
	case "list", "dict":
		t := L.NewTable()
		for _, item := range v.Items {
			lv, err := r.toLua(item.Value)
			if err != nil {
				return nil, err
			}
			if v.Kind == "list" {
				t.Append(lv)
			} else {
				t.RawSetString(item.Key, lv)
			}
		}
		return t, nil
	}
	return r.code(&codeValue{block: v.Code, expr: v.Kind == "expr"}), nil
}

// code returns c as a Lua value.
func (r *run) code(c *codeValue) lua.LValue {
	ud := r.L.NewUserData()
	ud.Value = c
	r.L.SetMetatable(ud, r.L.GetTypeMetatable("gdpp_code"))
	return ud
}

// asCode returns the code that v holds, if it's a Code value.
func asCode(v lua.LValue) (*codeValue, bool) {
	if ud, ok := v.(*lua.LUserData); ok {
		c, ok := ud.Value.(*codeValue)
		return c, ok
	}
	return nil, false
}

// declTables returns nodes as ctx.members: a list of tables, shaped like the fields of the emitter that kind names.
// by is the macro or template that generated nodes, or empty: otherwise each node's own, from x.generatedBy.
func (r *run) declTables(nodes []any, by string) *lua.LTable {
	list := r.L.NewTable()
	for _, n := range nodes {
		list.Append(r.declTable(n, cmp.Or(by, r.x.generatedBy[n])))
	}
	return list
}

// declTable returns n, a *Member, *Class, *Extern or *Enum, as a table of ctx.members.
func (r *run) declTable(n any, by string) *lua.LTable {
	t := r.L.NewTable()
	set := func(k string, v lua.LValue) {
		if v != lua.LNil {
			t.RawSetString(k, v)
		}
	}
	setStr := func(k, v string) {
		if v != "" {
			t.RawSetString(k, lua.LString(v))
		}
	}
	setType := func(k string, v *Type) {
		if v != nil {
			t.RawSetString(k, lua.LString(typeText(v)))
		}
	}
	setCode := func(k string, b *Block) {
		if b != nil {
			t.RawSetString(k, r.code(&codeValue{block: b}))
		}
	}
	head := func(kind string, doc *Doc, annotations []*Annotation) {
		t.RawSetString("kind", lua.LString(kind))
		if doc != nil {
			t.RawSetString("doc", lua.LString(doc.Text))
		}
		set("annotations", r.annotationTables(annotations))
	}
	// A class or extern, with its members, without invocations.
	body := func(kind string, doc *Doc, annotations []*Annotation, name string, extends *Type, members []*Member) {
		head(kind, doc, annotations)
		setStr("name", name)
		setType("extends", extends)
		var nodes []any
		for _, m := range members {
			if m.Invoke == nil {
				nodes = append(nodes, m)
			}
		}
		t.RawSetString("members", r.declTables(nodes, by))
	}
	if m, ok := n.(*Member); ok {
		switch {
		case m.Var != nil:
			n = m.Var
		case m.Func != nil:
			n = m.Func
		case m.Signal != nil:
			n = m.Signal
		case m.Enum != nil:
			n = m.Enum
		case m.Ctor != nil:
			head("ctor", nil, m.Ctor.Annotations)
			setCode("body", m.Ctor.Body)
		case m.Dtor != nil:
			head("dtor", nil, m.Dtor.Annotations)
			setCode("body", m.Dtor.Body)
		case m.On != nil:
			head("on", nil, m.On.Annotations)
			setStr("name", m.On.Name)
			if m.On.Param != nil {
				setStr("param", m.On.Param.Name)
			}
			setType("param_type", m.On.ParamType)
			setCode("body", m.On.Body)
		case m.Code != nil:
			head(map[[2]bool]string{{true, false}: "decl", {false, true}: "impl", {true, true}: "decl_impl"}[[2]bool{m.Code.Decl, m.Code.Impl}], nil, m.Code.Annotations)
			setCode("body", m.Code.Body)
		case m.Import != nil:
			head("import", nil, nil)
			setType("type", m.Import)
		case m.NoImport != nil:
			head("noimport", nil, nil)
			setType("type", m.NoImport)
		}
	}
	switch n := n.(type) {
	case *Var:
		head("var", n.Doc, n.Annotations)
		setStr("name", n.Name)
		setType("type", n.Type)
		set("init", r.initCode((*Default)(n.Init)))
		if n.Property != nil {
			for _, a := range n.Property.Accessors {
				setCode("decl", a.Decl)
				setCode("get", a.Get)
				if a.Set != nil {
					setCode("set", a.Set.Body)
					setStr("set_param", a.Set.Param.Name)
				}
			}
		}
	case *Func:
		head("func", n.Doc, n.Annotations)
		setStr("name", n.Name)
		set("params", r.paramTables(n.Params))
		setType("ret", n.Return)
		setCode("body", n.Body)
	case *Signal:
		head("signal", n.Doc, n.Annotations)
		setStr("name", n.Name)
		set("params", r.paramTables(n.Params))
	case *Enum:
		head("enum", n.Doc, n.Annotations)
		setStr("name", n.Name)
		if n.Value != nil {
			t.RawSetString("value", lua.LNumber(n.Value.Value))
			break
		}
		if n.Extends != nil {
			setStr("extends", n.Extends.Name)
		}
		values := r.L.NewTable()
		for _, e := range n.Entries {
			v := r.L.NewTable()
			v.RawSetString("name", lua.LString(e.Name))
			switch {
			case e.Value == nil:
			case e.Value.Int != nil:
				v.RawSetString("value", lua.LNumber(e.Value.Int.Value))
			default:
				v.RawSetString("value", lua.LString(enumExprText(e.Value)))
			}
			if e.Doc != nil {
				v.RawSetString("doc", lua.LString(e.Doc.Text))
			}
			values.Append(v)
		}
		t.RawSetString("values", values)
	case *Class:
		body("class", n.Doc, n.Annotations, n.Name, n.Extends, n.Members)
	case *Extern:
		body("extern", n.Doc, n.Annotations, n.Name, n.Extends, n.Members)
	}
	t.RawSetString("generated", lua.LBool(by != ""))
	setStr("generated_by", by)
	return t
}

// annotationTables returns annotations as the annotations field of emitters: names, or lists of a name and its
// arguments. Names keep their @, or @@ for user annotations. It's nil if there are none.
func (r *run) annotationTables(annotations []*Annotation) lua.LValue {
	if len(annotations) == 0 {
		return lua.LNil
	}
	list := r.L.NewTable()
	for _, a := range annotations {
		name := lua.LString(a.label())
		if len(a.Args) == 0 {
			list.Append(name)
			continue
		}
		item := r.L.NewTable()
		item.Append(name)
		for _, arg := range a.Args {
			item.Append(r.argValue(arg.Value))
		}
		list.Append(item)
	}
	return list
}

// argValue returns an annotation argument as the annotations field of emitters takes it back: a string, a number,
// a boolean, or else code.
func (r *run) argValue(s string) lua.LValue {
	switch {
	case s == "true" || s == "false":
		return lua.LBool(s == "true")
	case strings.HasPrefix(s, `"`):
		v, _ := r.toLua(&Value{Kind: "string", Text: s})
		return v
	}
	if v, err := r.toLua(&Value{Kind: "number", Text: s}); err == nil {
		return v
	}
	return r.code(&codeValue{block: &Block{Text: s}, expr: true})
}

// paramTables returns params as the params field of emitters: tables with a name, and maybe a type and a default.
// It's nil if there are none.
func (r *run) paramTables(params []*Param) lua.LValue {
	if len(params) == 0 {
		return lua.LNil
	}
	list := r.L.NewTable()
	for _, p := range params {
		t := r.L.NewTable()
		t.RawSetString("name", lua.LString(p.Name))
		if p.Type != nil {
			t.RawSetString("type", lua.LString(typeText(p.Type)))
		}
		if v := r.initCode(p.Default); v != lua.LNil {
			t.RawSetString("default", v)
		}
		list.Append(t)
	}
	return list
}

// initCode returns an initial or default value as a Code value, or nil if there's none.
func (r *run) initCode(d *Default) lua.LValue {
	switch {
	case d == nil:
		return lua.LNil
	case d.Block != nil:
		return r.code(&codeValue{block: d.Block})
	}
	return r.code(&codeValue{block: &Block{Pos: d.Pos, TextPos: d.Pos, Text: d.Expr, Generated: d.Generated, Origin: d.Origin}, expr: true})
}

// str returns v as text: a string, a number, a boolean or code.
func str(v lua.LValue) (string, bool) {
	switch v := v.(type) {
	case lua.LString:
		return string(v), true
	case lua.LNumber:
		return formatNumber(float64(v)), true
	case lua.LBool:
		return strconv.FormatBool(bool(v)), true
	}
	if c, ok := asCode(v); ok {
		return c.block.Text, true
	}
	return "", false
}

// formatNumber formats f like Lua's tostring: whole numbers without a fraction.
func formatNumber(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// sortedKeys returns t's keys: numbers, then strings, then booleans, each in order, then the others.
func sortedKeys(t *lua.LTable) []lua.LValue {
	var keys []lua.LValue
	t.ForEach(func(k, _ lua.LValue) { keys = append(keys, k) })
	rank := func(v lua.LValue) int {
		return map[lua.LValueType]int{lua.LTNumber: 0, lua.LTString: 1, lua.LTBool: 2}[v.Type()] + map[bool]int{true: 0, false: 3}[v.Type() == lua.LTNumber || v.Type() == lua.LTString || v.Type() == lua.LTBool]
	}
	slices.SortStableFunc(keys, func(a, b lua.LValue) int {
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra - rb
		}
		switch a := a.(type) {
		case lua.LNumber:
			return compare(float64(a), float64(b.(lua.LNumber)))
		case lua.LString:
			return strings.Compare(string(a), string(b.(lua.LString)))
		case lua.LBool:
			return map[bool]int{false: 0, true: 1}[bool(a)] - map[bool]int{false: 0, true: 1}[bool(b.(lua.LBool))]
		}
		return 0
	})
	return keys
}

func compare(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// pairs is Lua's pairs, in sorted key order, so generated code never depends on how a table stores its keys.
func (r *run) pairs(L *lua.LState) int {
	t := L.CheckTable(1)
	keys, i := sortedKeys(t), 0
	L.Push(L.NewFunction(func(L *lua.LState) int {
		if i >= len(keys) {
			L.Push(lua.LNil)
			return 1
		}
		k := keys[i]
		i++
		L.Push(k)
		L.Push(t.RawGet(k))
		return 2
	}))
	L.Push(t)
	L.Push(lua.LNil)
	return 3
}

// words splits s into words, at "_", "-", spaces and changes of case, e.g. "maxHTTPSpeed" into max, HTTP and Speed.
func words(s string) []string {
	var out []string
	rs := []rune(s)
	start := -1
	for i, c := range rs {
		sep := c == '_' || c == '-' || unicode.IsSpace(c)
		if sep {
			if start >= 0 {
				out = append(out, string(rs[start:i]))
			}
			start = -1
			continue
		}
		if start >= 0 && unicode.IsUpper(c) && (unicode.IsLower(rs[i-1]) || unicode.IsDigit(rs[i-1]) ||
			unicode.IsUpper(rs[i-1]) && i+1 < len(rs) && unicode.IsLower(rs[i+1])) {
			out = append(out, string(rs[start:i]))
			start = i
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, string(rs[start:]))
	}
	return out
}

// cased joins the words of s with sep, each word changed by fn, which gets its index.
func cased(s, sep string, fn func(i int, w string) string) string {
	ws := words(s)
	for i, w := range ws {
		ws[i] = fn(i, w)
	}
	return strings.Join(ws, sep)
}

func title(w string) string {
	rs := []rune(strings.ToLower(w))
	if len(rs) > 0 {
		rs[0] = unicode.ToUpper(rs[0])
	}
	return string(rs)
}

// caseFuncs are the case conversions of gd, by name.
var caseFuncs = map[string]func(string) string{
	"pascal": func(s string) string { return cased(s, "", func(_ int, w string) string { return title(w) }) },
	"camel": func(s string) string {
		return cased(s, "", func(i int, w string) string {
			return map[bool]string{true: strings.ToLower(w), false: title(w)}[i == 0]
		})
	},
	"snake": func(s string) string {
		return cased(s, "_", func(_ int, w string) string { return strings.ToLower(w) })
	},
	"upper_snake": func(s string) string {
		return cased(s, "_", func(_ int, w string) string { return strings.ToUpper(w) })
	},
	"kebab": func(s string) string {
		return cased(s, "-", func(_ int, w string) string { return strings.ToLower(w) })
	},
	"title": func(s string) string { return cased(s, " ", func(_ int, w string) string { return title(w) }) },
}

// gdTable returns gd, the functions that macros call.
func (r *run) gdTable() *lua.LTable {
	L := r.L
	meta := L.NewTypeMetatable("gdpp_code")
	meta.RawSetString("__tostring", L.NewFunction(func(L *lua.LState) int {
		s, _ := str(L.Get(1))
		L.Push(lua.LString(s))
		return 1
	}))
	meta.RawSetString("__concat", L.NewFunction(func(L *lua.LState) int {
		a, ok1 := str(L.Get(1))
		b, ok2 := str(L.Get(2))
		if !ok1 || !ok2 {
			L.RaiseError("Code values can only be joined with strings, numbers, booleans and code.")
		}
		L.Push(lua.LString(a + b))
		return 1
	}))
	gd := L.NewTable()
	fn := func(name string, f lua.LGFunction) { gd.RawSetString(name, L.NewFunction(f)) }
	pure := func(name string, f func(L *lua.LState) lua.LValue) {
		fn(name, func(L *lua.LState) int {
			L.Push(f(L))
			return 1
		})
	}
	text := func(L *lua.LState, i int) string {
		s, ok := str(L.Get(i))
		if !ok {
			L.ArgError(i, "expected text: a string, a number or code")
		}
		return s
	}

	// Emitters.
	for _, name := range []string{"class", "extern"} {
		fn(name, func(L *lua.LState) int { return r.emitClass(name, L.CheckTable(1)) })
	}
	fn("enum", func(L *lua.LState) int { return r.emitEnum(L.CheckTable(1)) })
	fn("func", func(L *lua.LState) int { return r.emitFunc(L.CheckTable(1)) })
	fn("var", func(L *lua.LState) int { return r.emitVar(L.CheckTable(1)) })
	fn("signal", func(L *lua.LState) int { return r.emitSignal(L.CheckTable(1)) })
	fn("ctor", func(L *lua.LState) int { return r.emitLifecycle("ctor", L.CheckTable(1)) })
	fn("dtor", func(L *lua.LState) int { return r.emitLifecycle("dtor", L.CheckTable(1)) })
	fn("on", func(L *lua.LState) int { return r.emitOn(L.CheckTable(1)) })
	for _, name := range []string{"decl", "impl", "decl_impl"} {
		fn(name, func(L *lua.LState) int { return r.emitCode(name, L.CheckTable(1)) })
	}
	for _, name := range []string{"import", "noimport"} {
		fn(name, func(L *lua.LState) int {
			t := r.parseType("gd."+name, text(L, 1))
			m := &Member{Pos: r.inv.Pos}
			if name == "import" {
				m.Import = t
			} else {
				m.NoImport = t
			}
			r.emit("gd."+name, &topItem{Pos: r.inv.Pos, Member: m})
			return 0
		})
	}
	fn("text", func(L *lua.LState) int {
		sc := r.scopes[len(r.scopes)-1]
		sc.chunks = append(sc.chunks, chunk{text: text(L, 1)})
		return 0
	})
	fn("invoke", func(L *lua.LState) int { return r.emitInvoke(L.CheckString(1), L.OptTable(2, L.NewTable())) })

	// Case conversion.
	for name, f := range caseFuncs {
		pure(name, func(L *lua.LState) lua.LValue { return lua.LString(f(text(L, 1))) })
	}

	// Quoting.
	pure("quote", func(L *lua.LState) lua.LValue { return lua.LString(cppString(text(L, 1))) })
	pure("string_name", func(L *lua.LState) lua.LValue {
		s := text(L, 1)
		if q := cppString(s); strings.HasPrefix(q, "\"") {
			return lua.LString("string_name " + q)
		}
		return lua.LString("StringName(" + cppString(s) + ")") // The rewrite only takes plain strings.
	})
	pure("gdquote", func(L *lua.LState) lua.LValue { return lua.LString(strconv.Quote(text(L, 1))) })

	// Strings.
	list := func(ss []string) lua.LValue {
		t := L.NewTable()
		for _, s := range ss {
			t.Append(lua.LString(s))
		}
		return t
	}
	pure("split", func(L *lua.LState) lua.LValue { return list(strings.Split(text(L, 1), text(L, 2))) })
	pure("join", func(L *lua.LState) lua.LValue {
		t := L.CheckTable(1)
		var parts []string
		for i := 1; i <= t.Len(); i++ {
			s, ok := str(t.RawGetInt(i))
			if !ok {
				L.ArgError(1, fmt.Sprintf("item %d isn't text", i))
			}
			parts = append(parts, s)
		}
		return lua.LString(strings.Join(parts, L.OptString(2, "")))
	})
	pure("trim", func(L *lua.LState) lua.LValue { return lua.LString(strings.TrimSpace(text(L, 1))) })
	pure("starts_with", func(L *lua.LState) lua.LValue { return lua.LBool(strings.HasPrefix(text(L, 1), text(L, 2))) })
	pure("ends_with", func(L *lua.LState) lua.LValue { return lua.LBool(strings.HasSuffix(text(L, 1), text(L, 2))) })
	pure("replace", func(L *lua.LState) lua.LValue {
		return lua.LString(strings.ReplaceAll(text(L, 1), text(L, 2), text(L, 3)))
	})
	pure("lines", func(L *lua.LState) lua.LValue { return list(strings.Split(text(L, 1), "\n")) })
	pure("indent", func(L *lua.LState) lua.LValue {
		lines, pad := strings.Split(text(L, 1), "\n"), strings.Repeat(" ", L.CheckInt(2))
		for i, line := range lines {
			if line != "" {
				lines[i] = pad + line
			}
		}
		return lua.LString(strings.Join(lines, "\n"))
	})

	// Tables.
	pure("keys", func(L *lua.LState) lua.LValue {
		t := L.NewTable()
		for _, k := range sortedKeys(L.CheckTable(1)) {
			t.Append(k)
		}
		return t
	})
	pure("values", func(L *lua.LState) lua.LValue {
		src, t := L.CheckTable(1), L.NewTable()
		for _, k := range sortedKeys(src) {
			t.Append(src.RawGet(k))
		}
		return t
	})
	fn("sorted_pairs", r.pairs)
	isList := func(t *lua.LTable) bool {
		n := 0
		t.ForEach(func(_, _ lua.LValue) { n++ })
		return n == t.Len()
	}
	callFn := func(f *lua.LFunction, args ...lua.LValue) lua.LValue {
		L.Push(f)
		for _, a := range args {
			L.Push(a)
		}
		L.Call(len(args), 1)
		v := L.Get(-1)
		L.Pop(1)
		return v
	}
	pure("map", func(L *lua.LState) lua.LValue {
		src, f, t := L.CheckTable(1), L.CheckFunction(2), L.NewTable()
		for _, k := range sortedKeys(src) {
			t.RawSet(k, callFn(f, src.RawGet(k), k))
		}
		return t
	})
	pure("filter", func(L *lua.LState) lua.LValue {
		src, f, t := L.CheckTable(1), L.CheckFunction(2), L.NewTable()
		list := isList(src)
		for _, k := range sortedKeys(src) {
			if v := src.RawGet(k); lua.LVAsBool(callFn(f, v, k)) {
				if list {
					t.Append(v)
				} else {
					t.RawSet(k, v)
				}
			}
		}
		return t
	})
	pure("contains", func(L *lua.LState) lua.LValue {
		found := false
		L.CheckTable(1).ForEach(func(_, v lua.LValue) { found = found || L.Equal(v, L.Get(2)) })
		return lua.LBool(found)
	})
	var deepCopy func(v lua.LValue) lua.LValue
	deepCopy = func(v lua.LValue) lua.LValue {
		src, ok := v.(*lua.LTable)
		if !ok {
			return v
		}
		t := L.NewTable()
		for _, k := range sortedKeys(src) {
			t.RawSet(k, deepCopy(src.RawGet(k)))
		}
		return t
	}
	pure("copy", func(L *lua.LState) lua.LValue { return deepCopy(L.CheckTable(1)) })
	pure("merge", func(L *lua.LState) lua.LValue {
		t := L.NewTable()
		for _, src := range []*lua.LTable{L.CheckTable(1), L.CheckTable(2)} {
			for _, k := range sortedKeys(src) {
				t.RawSet(k, src.RawGet(k))
			}
		}
		return t
	})
	pure("range", func(L *lua.LState) lua.LValue {
		from, to := 1, L.CheckInt(1)
		if L.GetTop() >= 2 {
			from, to = to, L.CheckInt(2)
		}
		t := L.NewTable()
		for i := from; i <= to; i++ {
			t.Append(lua.LNumber(i))
		}
		return t
	})

	// Values.
	pure("code", func(L *lua.LState) lua.LValue {
		return r.code(&codeValue{block: r.block(text(L, 1)), expr: !strings.ContainsAny(text(L, 1), ";{}\n")})
	})
	pure("is_code", func(L *lua.LState) lua.LValue {
		_, ok := asCode(L.Get(1))
		return lua.LBool(ok)
	})
	pure("type", func(L *lua.LState) lua.LValue {
		if _, ok := asCode(L.Get(1)); ok {
			return lua.LString("code")
		}
		return lua.LString(L.Get(1).Type().String())
	})
	pure("is_ident", func(L *lua.LState) lua.LValue { return lua.LBool(identRegexp.MatchString(text(L, 1))) })
	pure("ident", func(L *lua.LState) lua.LValue {
		s := text(L, 1)
		if !identRegexp.MatchString(s) {
			r.failAt(fmt.Sprintf("Macro %s needs a name, but got %q.", r.inv.Name, s), "Names are letters, digits and underscores, and don't start with a digit.")
		}
		return lua.LString(s)
	})

	// Annotations: the arguments of t's annotation named name, e.g. "@export" or "@@save", or nil without it.
	pure("annotation", func(L *lua.LState) lua.LValue {
		t, name := L.CheckTable(1), L.CheckString(2)
		if !strings.HasPrefix(name, "@") {
			L.ArgError(2, fmt.Sprintf("annotation names start with @, or @@ for user annotations, e.g. \"@%s\"", name))
		}
		name = name[1:]
		list, _ := t.RawGetString("annotations").(*lua.LTable)
		for i := 1; list != nil && i <= list.Len(); i++ {
			switch a := list.RawGetInt(i).(type) {
			case lua.LString:
				if strings.TrimPrefix(string(a), "@") == name {
					return L.NewTable()
				}
			case *lua.LTable:
				if strings.TrimPrefix(lua.LVAsString(a.RawGetInt(1)), "@") == name {
					args := L.NewTable()
					for j := 2; j <= a.Len(); j++ {
						args.Append(a.RawGetInt(j))
					}
					return args
				}
			}
		}
		return lua.LNil
	})

	// Names and lookup.
	pure("unique", func(L *lua.LState) lua.LValue {
		r.x.uniques++
		return lua.LString(fmt.Sprintf("_gdpp_%s_%d", L.OptString(1, "m"), r.x.uniques))
	})
	pure("has_macro", func(L *lua.LState) lua.LValue {
		d := r.x.defs[L.CheckString(1)]
		return lua.LBool(d != nil && !d.m.Template)
	})
	pure("has_template", func(L *lua.LState) lua.LValue {
		d := r.x.defs[L.CheckString(1)]
		return lua.LBool(d != nil && d.m.Template)
	})

	// Diagnostics: GD++ errors at the invocation, or at an argument (named by its parameter) or a Code value.
	fn("error", func(L *lua.LState) int {
		r.failAt(sentence(text(L, 1)), sentence(L.OptString(2, "")))
		return 0
	})
	fn("error_at", func(L *lua.LState) int {
		r.failWhere(L.Get(1), sentence(text(L, 2)), sentence(L.OptString(3, "")))
		return 0
	})
	return gd
}

// failWhere stops the run with an error at where: the argument of a parameter, named by a string, or a Code value.
// Anything else, or a parameter left to its default, puts it at the invocation.
func (r *run) failWhere(where lua.LValue, msg, hint string) {
	if c, ok := asCode(where); ok && !c.block.Generated && c.block.Pos.Filename == r.x.filename {
		r.fail(r.x.errorAt(c.block.Pos, max(1, len(strings.SplitN(c.block.Text, "\n", 2)[0])), msg, hint))
	}
	if s, ok := where.(lua.LString); ok {
		if pos, ok := r.argPos[string(s)]; ok {
			r.fail(r.argError(pos, r.argLen[string(s)], msg, hint))
		}
	}
	r.failAt(msg, hint)
}

// valueLen returns how many characters an error about v underlines.
func valueLen(v *Value) int {
	switch {
	case v.Text != "":
		return utf8.RuneCountInString(v.Text)
	case v.Kind == "code":
		return 4
	}
	return 1
}

// sentence capitalizes s and ends it with a period, unless it ends with "." "!" or "?" already.
func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	rs := []rune(s)
	rs[0] = unicode.ToUpper(rs[0])
	if !strings.ContainsRune(".!?", rs[len(rs)-1]) {
		rs = append(rs, '.')
	}
	return string(rs)
}

// block returns generated C++ code as a block at the invocation.
func (r *run) block(text string) *Block {
	return &Block{Pos: r.inv.Pos, TextPos: r.inv.Pos, Text: text, Generated: true}
}

// emit adds item to the current scope, if it allows items of item's kind. what names the emitter.
func (r *run) emit(what string, item *topItem) {
	sc := r.scopes[len(r.scopes)-1]
	m := item.Member
	var msg string
	switch {
	case sc.kind == "cpp":
		msg = fmt.Sprintf("%s can't be used in C++ code: only gd.text can.", what)
	case item.Class != nil || item.Extern != nil:
		if sc.kind != "file" && !sc.fileLevel {
			msg = fmt.Sprintf("%s can't be used in a class or extern: they can't be nested.", what)
		}
	case sc.kind == "file" && (m.Enum == nil || m.Enum.Value != nil):
		msg = fmt.Sprintf("%s can't be used outside of a class: only gd.class, gd.extern and gd.enum without a value can.", what)
	}
	if msg != "" {
		r.L.RaiseError("%s", msg)
	}
	sc.chunks = append(sc.chunks, chunk{item: item})
}

// field returns t's field name, which must be a string, unless it's optional and missing.
func (r *run) field(what string, t *lua.LTable, name string, optional bool) string {
	v := t.RawGetString(name)
	if v == lua.LNil && optional {
		return ""
	}
	s, ok := str(v)
	if !ok {
		r.L.RaiseError("%s: %s must be a string, but it's %s.", what, name, typeName(v))
	}
	return s
}

func typeName(v lua.LValue) string {
	if _, ok := asCode(v); ok {
		return "code"
	}
	return v.Type().String()
}

// name returns t's name field, which must be an identifier.
func (r *run) name(what string, t *lua.LTable) string {
	s := r.field(what, t, "name", false)
	if !identRegexp.MatchString(s) {
		r.L.RaiseError("%s: name must be a name, but it's %q.", what, s)
	}
	return s
}

var (
	typeParser  = participle.MustBuild[Type](participle.Lexer(gdppLexer), participle.Elide(elided...))
	paramParser = participle.MustBuild[Param](participle.Lexer(gdppLexer), participle.Elide(elided...), participle.UseLookahead(participle.MaxLookahead))
	baseParser  = participle.MustBuild[EnumBase](participle.Lexer(gdppLexer), participle.Elide(elided...))
)

// parseType parses a type, e.g. "Array[int]".
func (r *run) parseType(what, s string) *Type {
	t, err := typeParser.ParseString(r.x.filename, s)
	if err != nil {
		r.L.RaiseError("%s: %q isn't a type.", what, s)
	}
	r.place(t)
	return t
}

// optType parses t's field name as a type, if it's there.
func (r *run) optType(what string, t *lua.LTable, name string) *Type {
	if s := r.field(what, t, name, true); s != "" {
		return r.parseType(what, s)
	}
	return nil
}

// place moves node and all nodes in it to the invocation, and marks its blocks as generated.
func (r *run) place(node any) {
	pos := r.inv.Pos
	forEachNode(node, func(n any) {
		switch n := n.(type) {
		case *Block:
			n.Pos, n.TextPos, n.Generated = pos, pos, true
		case *Init:
			n.Pos, n.Generated = pos, true
		case *Default:
			n.Pos, n.Generated = pos, true
		case *MacroBody, *Error:
		default:
			if p := reflectPos(n); p != nil {
				*p = pos
			}
		}
	})
}

// doc returns t's doc field as a doc comment, if it's there.
func (r *run) doc(what string, t *lua.LTable) *Doc {
	if s := r.field(what, t, "doc", true); s != "" {
		return &Doc{Pos: r.inv.Pos, Text: s}
	}
	return nil
}

// annotations returns t's annotations field: a list of names, or of lists of a name and its arguments.
func (r *run) annotations(what string, t *lua.LTable) []*Annotation {
	v := t.RawGetString("annotations")
	if v == lua.LNil {
		return nil
	}
	list, ok := v.(*lua.LTable)
	if !ok {
		r.L.RaiseError("%s: annotations must be a list.", what)
	}
	var out []*Annotation
	for i := 1; i <= list.Len(); i++ {
		a := &Annotation{Pos: r.inv.Pos}
		switch item := list.RawGetInt(i).(type) {
		case lua.LString:
			a.Name = string(item)
		case *lua.LTable:
			a.Name = lua.LVAsString(item.RawGetInt(1))
			for j := 2; j <= item.Len(); j++ {
				arg := &Arg{Pos: r.inv.Pos}
				switch v := item.RawGetInt(j).(type) {
				case lua.LString:
					arg.Value = strconv.Quote(string(v))
				case lua.LNumber:
					arg.Value = formatNumber(float64(v))
				case lua.LBool:
					arg.Value = strconv.FormatBool(bool(v))
				default:
					c, ok := asCode(v)
					if !ok {
						r.L.RaiseError("%s: annotation arguments must be strings, numbers, booleans or code.", what)
					}
					arg.Value = c.block.Text
				}
				a.Args = append(a.Args, arg)
			}
		default:
			r.L.RaiseError("%s: each annotation must be a name, or a list of a name and its arguments.", what)
		}
		written := a.Name
		if !strings.HasPrefix(written, "@") {
			r.L.RaiseError("%s: annotation names start with @, or @@ for user annotations, e.g. \"@%s\".", what, written)
		}
		a.User = strings.HasPrefix(written, "@@")
		a.Name = strings.TrimPrefix(written[1:], "@")
		if !identRegexp.MatchString(a.Name) {
			r.L.RaiseError("%s: %q isn't an annotation name.", what, written)
		}
		out = append(out, a)
	}
	return out
}

// params returns t's params field: a list of "name: type = default" strings, or of {name=, type=, default=}.
func (r *run) params(what string, t *lua.LTable) []*Param {
	v := t.RawGetString("params")
	if v == lua.LNil {
		return nil
	}
	list, ok := v.(*lua.LTable)
	if !ok {
		r.L.RaiseError("%s: params must be a list.", what)
	}
	var out []*Param
	for i := 1; i <= list.Len(); i++ {
		var p *Param
		switch item := list.RawGetInt(i).(type) {
		case lua.LString:
			var err error
			if p, err = paramParser.ParseString(r.x.filename, string(item)); err != nil {
				r.L.RaiseError("%s: %q isn't a parameter, like \"speed: float = 1.0\".", what, string(item))
			}
		case *lua.LTable:
			p = &Param{Name: r.name(what, item), Type: r.optType(what, item, "type")}
			if d := item.RawGetString("default"); d != lua.LNil {
				s, _ := str(d)
				p.Default = &Default{Expr: s}
			}
		default:
			r.L.RaiseError("%s: each parameter must be a string or a table.", what)
		}
		r.place(p)
		out = append(out, p)
	}
	return out
}

// cpp returns t's field name as C++: a string, a Code value, or a function that writes it with gd.text.
// It returns nil if it's missing.
func (r *run) cpp(what string, t *lua.LTable, name string) (b *Block, expr bool) {
	switch v := t.RawGetString(name).(type) {
	case *lua.LNilType:
		return nil, false
	case *lua.LFunction:
		r.scopes = append(r.scopes, &scope{kind: "cpp"})
		r.L.Push(v)
		r.L.Call(0, 0)
		sc := r.scopes[len(r.scopes)-1]
		r.scopes = r.scopes[:len(r.scopes)-1]
		var sb strings.Builder
		for _, c := range sc.chunks {
			sb.WriteString(c.text)
		}
		return r.block(sb.String()), false
	default:
		if c, ok := asCode(v); ok {
			b := *c.block
			if !b.Generated && b.Origin.Source == "" && b.Pos.Filename == r.x.filename { // The user's code keeps its lines.
				start := b.TextPos
				b.Origin = Origin{r.x.source(), start.Line, &start, r.x.src}
			}
			return &b, c.expr
		}
		s, ok := str(v)
		if !ok {
			r.L.RaiseError("%s: %s must be C++: a string, code, or a function that calls gd.text.", what, name)
		}
		return r.block(s), false
	}
}

func (r *run) emitClass(kind string, t *lua.LTable) int {
	what := "gd." + kind
	name, annotations := r.name(what, t), r.annotations(what, t)
	sc := &scope{kind: kind, owner: name, annotations: annotations}
	if body := t.RawGetString("body"); body != lua.LNil {
		f, ok := body.(*lua.LFunction)
		if !ok {
			r.L.RaiseError("%s: body must be a function that emits the members.", what)
		}
		r.scopes = append(r.scopes, sc)
		r.L.Push(f)
		r.L.Call(0, 0)
		r.scopes = r.scopes[:len(r.scopes)-1]
	}
	items, err := r.collect(sc)
	if err != nil {
		r.fail(err.(*Error))
	}
	var members []*Member
	for _, item := range items {
		if item.Member == nil {
			r.L.RaiseError("%s: classes and externs can't be nested.", what)
		}
		members = append(members, item.Member)
	}
	extends, doc := r.optType(what, t, "extends"), r.doc(what, t)
	item := &topItem{Pos: r.inv.Pos}
	if kind == "class" {
		item.Class = &Class{Pos: r.inv.Pos, Doc: doc, Annotations: annotations, Name: name, Extends: extends, Members: members}
	} else {
		item.Extern = &Extern{Pos: r.inv.Pos, Doc: doc, Annotations: annotations, Name: name, Extends: extends, Members: members}
	}
	r.emit(what, item)
	return 0
}

// enumInt returns v as an integer value.
func (r *run) enumInt(what string, v lua.LValue) *Int {
	n, ok := v.(lua.LNumber)
	if !ok || float64(n) != math.Trunc(float64(n)) {
		r.L.RaiseError("%s: values must be integers, or strings with expressions.", what)
	}
	return &Int{Pos: r.inv.Pos, Raw: formatNumber(float64(n)), Value: int64(n)}
}

func (r *run) emitEnum(t *lua.LTable) int {
	const what = "gd.enum"
	e := &Enum{Pos: r.inv.Pos, Doc: r.doc(what, t), Annotations: r.annotations(what, t), Name: r.name(what, t)}
	if v := t.RawGetString("value"); v != lua.LNil {
		e.Value = r.enumInt(what, v)
		r.emit(what, &topItem{Pos: r.inv.Pos, Member: &Member{Pos: r.inv.Pos, Enum: e}})
		return 0
	}
	e.Extends = &EnumBase{Pos: r.inv.Pos}
	if s := r.field(what, t, "extends", true); s != "" {
		base, err := baseParser.ParseString(r.x.filename, s)
		if err != nil {
			r.L.RaiseError("%s: %q isn't an enum, like \"Suit\" or \"Node.ProcessMode\".", what, s)
		}
		e.Extends.Name = base.Name
	} else {
		e.Extends = nil
	}
	values, _ := t.RawGetString("values").(*lua.LTable)
	if values == nil {
		r.L.RaiseError("%s: values must be a list, or value an integer for a constant.", what)
	}
	for i := 1; i <= values.Len(); i++ {
		entry := &EnumEntry{Pos: r.inv.Pos}
		var value lua.LValue = lua.LNil
		switch v := values.RawGetInt(i).(type) {
		case lua.LString:
			entry.Name = string(v)
		case *lua.LTable:
			entry.Name, value = lua.LVAsString(v.RawGetInt(1)), v.RawGetInt(2)
			if n := v.RawGetString("name"); n != lua.LNil {
				entry.Name, value = lua.LVAsString(n), v.RawGetString("value")
			}
			entry.Doc = r.doc(what, v)
		default:
			r.L.RaiseError("%s: each value must be a name, or a table with its name, value and doc.", what)
		}
		if !identRegexp.MatchString(entry.Name) {
			r.L.RaiseError("%s: %q isn't a value name.", what, entry.Name)
		}
		switch v := value.(type) {
		case *lua.LNilType:
		case lua.LString:
			if entry.Value = parseEnumExpr(string(v)); entry.Value == nil {
				r.L.RaiseError("%s: %q isn't an enum value, like \"A | B\" or \"Suit.HEARTS\".", what, string(v))
			}
			r.place(entry.Value)
		default:
			entry.Value = &EnumExpr{Pos: r.inv.Pos, Int: r.enumInt(what, v)}
		}
		e.Entries = append(e.Entries, entry)
	}
	r.emit(what, &topItem{Pos: r.inv.Pos, Member: &Member{Pos: r.inv.Pos, Enum: e}})
	return 0
}

func (r *run) emitFunc(t *lua.LTable) int {
	const what = "gd.func"
	f := &Func{Pos: r.inv.Pos, Doc: r.doc(what, t), Annotations: r.annotations(what, t), Name: r.name(what, t), Params: r.params(what, t),
		Return: r.optType(what, t, "ret")}
	f.Body, _ = r.cpp(what, t, "body")
	r.emit(what, &topItem{Pos: r.inv.Pos, Member: &Member{Pos: r.inv.Pos, Func: f}})
	return 0
}

func (r *run) emitVar(t *lua.LTable) int {
	const what = "gd.var"
	v := &Var{Pos: r.inv.Pos, Doc: r.doc(what, t), Annotations: r.annotations(what, t), Name: r.name(what, t), Type: r.optType(what, t, "type")}
	if b, expr := r.cpp(what, t, "init"); b != nil {
		v.Init = &Init{Pos: b.Pos, Generated: true}
		if expr || !strings.ContainsAny(b.Text, ";\n") {
			v.Init.Expr = strings.TrimSpace(b.Text)
		} else {
			v.Init.Block = b
		}
	}
	prop := &Property{Pos: r.inv.Pos}
	for _, name := range []string{"decl", "get", "set"} {
		b, _ := r.cpp(what, t, name)
		switch {
		case b == nil:
		case name == "decl":
			prop.Accessors = append(prop.Accessors, &Accessor{Pos: r.inv.Pos, Decl: b})
		case name == "get":
			prop.Accessors = append(prop.Accessors, &Accessor{Pos: r.inv.Pos, Get: b})
		default:
			param := r.field(what, t, "set_param", true)
			if param == "" {
				param = "value"
			}
			prop.Accessors = append(prop.Accessors, &Accessor{Pos: r.inv.Pos, Set: &Setter{Pos: r.inv.Pos, Param: &Param{Pos: r.inv.Pos, Name: param}, Body: b}})
		}
	}
	if len(prop.Accessors) > 0 {
		if v.Init != nil {
			r.L.RaiseError("%s: a var has either init, or get, set and decl.", what)
		}
		v.Property = prop
	}
	r.emit(what, &topItem{Pos: r.inv.Pos, Member: &Member{Pos: r.inv.Pos, Var: v}})
	return 0
}

func (r *run) emitSignal(t *lua.LTable) int {
	const what = "gd.signal"
	s := &Signal{Pos: r.inv.Pos, Doc: r.doc(what, t), Annotations: r.annotations(what, t), Name: r.name(what, t), Params: r.params(what, t)}
	r.emit(what, &topItem{Pos: r.inv.Pos, Member: &Member{Pos: r.inv.Pos, Signal: s}})
	return 0
}

// body returns t's body field, which an emitter needs.
func (r *run) body(what string, t *lua.LTable) *Block {
	b, _ := r.cpp(what, t, "body")
	if b == nil {
		r.L.RaiseError("%s: body is missing.", what)
	}
	return b
}

func (r *run) emitLifecycle(kind string, t *lua.LTable) int {
	what := "gd." + kind
	m := &Member{Pos: r.inv.Pos}
	if kind == "ctor" {
		m.Ctor = &Ctor{Pos: r.inv.Pos, Annotations: r.annotations(what, t), Body: r.body(what, t)}
	} else {
		m.Dtor = &Dtor{Pos: r.inv.Pos, Annotations: r.annotations(what, t), Body: r.body(what, t)}
	}
	r.emit(what, &topItem{Pos: r.inv.Pos, Member: m})
	return 0
}

func (r *run) emitOn(t *lua.LTable) int {
	const what = "gd.on"
	o := &On{Pos: r.inv.Pos, Annotations: r.annotations(what, t), Name: r.field(what, t, "name", true), Body: r.body(what, t)}
	if p := r.field(what, t, "param", true); p != "" {
		o.Parens, o.Param, o.ParamType = true, &Name{Pos: r.inv.Pos, Name: p}, r.optType(what, t, "param_type")
	}
	r.emit(what, &topItem{Pos: r.inv.Pos, Member: &Member{Pos: r.inv.Pos, On: o}})
	return 0
}

func (r *run) emitCode(kind string, t *lua.LTable) int {
	what := "gd." + kind
	c := &Code{Pos: r.inv.Pos, Annotations: r.annotations(what, t), Decl: kind != "impl", Impl: kind != "decl", Body: r.body(what, t)}
	r.emit(what, &topItem{Pos: r.inv.Pos, Member: &Member{Pos: r.inv.Pos, Code: c}})
	return 0
}

// emitInvoke invokes the macro or template name with args, a table like those of "invoke name { ... }", and adds
// what it generates to the current scope.
func (r *run) emitInvoke(name string, args *lua.LTable) int {
	L, sc := r.L, r.scopes[len(r.scopes)-1]
	def := r.x.defs[name]
	switch {
	case def == nil:
		L.RaiseError("gd.invoke: there is no macro or template %q.", name)
	case def.m.Template && sc.kind == "cpp":
		L.RaiseError("gd.invoke: template %s can't be used in C++ code, since templates generate declarations.", name)
	case r.depth+1 >= r.x.maxDepth():
		r.failAt(fmt.Sprintf("Invocations are nested more than %d levels deep here.", r.x.maxDepth()), "A macro probably invokes itself, directly or through others. If it only needs to nest deeper, raise macro_depth in .gd++pkg.")
	}
	params := def.m.Params
	values := make([]lua.LValue, len(params))
	for i := range values {
		values[i] = lua.LNil
	}
	if args.Len() > len(params) {
		L.RaiseError("gd.invoke: %s %s takes %d arguments, but got %d.", def.m.what(), name, len(params), args.Len())
	}
	for i := 1; i <= args.Len(); i++ {
		values[i-1] = args.RawGetInt(i)
	}
	args.ForEach(func(k, v lua.LValue) {
		key, ok := k.(lua.LString)
		if !ok {
			return
		}
		i := slices.IndexFunc(params, func(p *MacroParam) bool { return p.Name == string(key) })
		switch {
		case i < 0:
			L.RaiseError("gd.invoke: %s %s has no parameter %q.", def.m.what(), name, string(key))
		case i < args.Len():
			L.RaiseError("gd.invoke: parameter %s is set twice.", string(key))
		}
		values[i] = v
	})
	// Its line of the macro call stack, if it fails.
	frame := fmt.Sprintf("%s %s, invoked with gd.invoke at %s", def.m.what(), name, strings.TrimSuffix(L.Where(1), ":"))
	if def.m.Template {
		items, err := r.instantiate(def, values)
		if err != nil {
			r.fail(addFrame(err, frame).(*Error))
		}
		for _, item := range items {
			r.emit("gd.invoke", item)
		}
		return 0
	}
	// A macro runs in this Lua state, so tables and functions pass as they are, with its own gd and ctx.
	inner := &scope{kind: sc.kind, owner: sc.owner, annotations: sc.annotations, fileLevel: sc.fileLevel, members: sc.members}
	var site *Error
	if m := luaWhere.FindStringSubmatch(L.Where(1)); m != nil {
		line, _ := strconv.Atoi(m[2])
		site = r.x.lineError(m[1], line, "", "")
	}
	n := &run{x: r.x, L: L, def: def, inv: &Invoke{Pos: r.inv.Pos, Name: name, Site: site}, scopes: []*scope{inner}, depth: r.depth + 1,
		values: map[string]lua.LValue{}, argPos: map[string]lexer.Position{}, argLen: map[string]int{}}
	gd, ctx := L.GetGlobal("gd"), L.GetGlobal("ctx")
	L.SetGlobal("gd", n.gdTable())
	L.SetGlobal("ctx", n.ctxTable(inner))
	fn, err := n.load(def.m.Body.Text, def.m.Body.TextPos, def.file, params)
	if err == nil {
		err = n.call(fn, values...)
	}
	L.SetGlobal("gd", gd)
	L.SetGlobal("ctx", ctx)
	if err != nil {
		r.fail(addFrame(err, frame).(*Error))
	}
	if sc.kind == "cpp" {
		sc.chunks = append(sc.chunks, inner.chunks...)
		return 0
	}
	items, err := n.collect(inner)
	if err != nil {
		r.fail(addFrame(err, frame).(*Error))
	}
	for _, item := range items {
		r.emit("gd.invoke", item)
	}
	return 0
}

// collect returns what was emitted into sc: its items, with each run of text parsed as GD++.
func (r *run) collect(sc *scope) ([]*topItem, error) {
	var items []*topItem
	var text strings.Builder
	flush := func() error {
		if text.Len() == 0 {
			return nil
		}
		parsed, err := r.parseText(text.String())
		text.Reset()
		items = append(items, parsed...)
		return err
	}
	for _, c := range sc.chunks {
		if c.item == nil {
			text.WriteString(c.text)
			continue
		}
		if err := flush(); err != nil {
			return nil, err
		}
		items = append(items, c.item)
	}
	return items, flush()
}

// parseText parses GD++ that a macro emitted with gd.text.
func (r *run) parseText(text string) ([]*topItem, error) {
	tree, err := parseTree(itemsParser, r.x.filename, text)
	if err != nil {
		hint := ""
		var e *Error
		if errors.As(err, &e) {
			hint = fmt.Sprintf("%s The line it emitted: %q", e.Hint, e.line)
			err = r.invocationError(fmt.Sprintf("Macro %s emitted GD++ that doesn't parse: %s", r.inv.Name, lowerFirst(e.Msg)), strings.TrimSpace(hint+"."))
		}
		return nil, err
	}
	for _, item := range tree.Items {
		if item.Macro != nil {
			return nil, r.invocationError(fmt.Sprintf("Macro %s emitted a %s, but macros can't generate macros or templates.", r.inv.Name, item.Macro.what()),
				"Declare it in the macro's body instead, next to the code that uses it.")
		}
		r.place(item)
	}
	return tree.Items, nil
}

// lowerFirst makes the first letter of s lowercase.
func lowerFirst(s string) string {
	rs := []rune(s)
	if len(rs) > 0 {
		rs[0] = unicode.ToLower(rs[0])
	}
	return string(rs)
}

// instantiate fills the holes of template def with the values of its params, and parses the result.
func (r *run) instantiate(def *macroDef, values []lua.LValue) ([]*topItem, error) {
	body := def.m.Body
	var out strings.Builder
	type hole struct{ line, col, holeLen, valueLen int } // Where a value went in out, and the hole it filled.
	var holes []hole
	outCol := func() int { // The column in def's file where out ends, if nothing was filled in.
		s := out.String()
		col := utf8.RuneCountInString(s[strings.LastIndexByte(s, '\n')+1:]) + 1
		if !strings.Contains(s, "\n") {
			col += body.TextPos.Column - 1
		}
		return col
	}
	text := body.Template
	line, col := body.TextPos.Line, body.TextPos.Column // Where text[i] is in def's file.
	advance := func(s string) {
		for _, c := range s {
			if c == '\n' {
				line, col = line+1, 1
			} else {
				col++
			}
		}
	}
	for {
		i := strings.Index(text, "${")
		if i < 0 {
			out.WriteString(text)
			break
		}
		out.WriteString(text[:i])
		advance(text[:i])
		holePos := lexer.Position{Filename: def.file, Line: line, Column: col}
		holePos.Offset = offsetOf(def.src, holePos)
		end, depth := -1, 0
		for j := i + 2; j < len(text) && end < 0; j++ {
			switch text[j] {
			case '{':
				depth++
			case '}':
				if depth == 0 {
					end = j
				}
				depth--
			}
		}
		if end < 0 {
			return nil, (&Error{Pos: holePos, Len: 2, Msg: "This hole is never closed.", Hint: "Holes look like ${name}."}).withSource(def.src)
		}
		expr := text[i+2 : end]
		exprPos := holePos
		exprPos.Column += 2
		fn, err := r.load("return ("+expr+")", lexer.Position{Line: holePos.Line}, def.file, def.m.Params)
		if err != nil {
			return nil, err
		}
		r.L.Push(fn)
		for _, v := range values {
			r.L.Push(v)
		}
		if err := r.L.PCall(len(values), 1, nil); err != nil {
			if r.failure != nil {
				return nil, r.failure
			}
			return nil, r.luaError(err)
		}
		v := r.L.Get(-1)
		r.L.Pop(1)
		s, ok := str(v)
		if !ok {
			return nil, (&Error{Pos: holePos, Len: end - i + 1, Msg: fmt.Sprintf("This hole is %s, but holes need a string, a number, a boolean or code.", typeName(v))}).withSource(def.src)
		}
		s = strings.ReplaceAll(s, "\n", " ")
		holes = append(holes, hole{line, outCol(), utf8.RuneCountInString(text[i : end+1]), utf8.RuneCountInString(s)})
		out.WriteString(s)
		advance(text[i : end+1])
		text = text[end+1:]
	}
	// Parse it where the template is, so errors point into the template.
	padded := strings.Repeat("\n", body.TextPos.Line-1) + strings.Repeat(" ", body.TextPos.Column-1) + out.String()
	tree, err := parseTree(itemsParser, def.file, padded)
	if err != nil {
		var e *Error
		if errors.As(err, &e) {
			col := e.Pos.Column
			for _, h := range holes { // From the filled-in text back to the template's.
				switch {
				case h.line != e.Pos.Line || h.col > e.Pos.Column:
				case e.Pos.Column >= h.col+h.valueLen:
					col += h.holeLen - h.valueLen
				default:
					col, e.Len = col-(e.Pos.Column-h.col), h.holeLen
				}
			}
			e.Pos.Column = col
			e.Pos.Offset = offsetOf(def.src, e.Pos)
			return nil, e.withSource(def.src)
		}
		return nil, err
	}
	for _, item := range tree.Items {
		if item.Macro != nil {
			return nil, (&Error{Pos: item.Macro.Pos, Len: len(item.Macro.what()), Msg: "Templates can't generate macros or templates.",
				Hint: "Declare it next to the template: it's a package-level declaration either way."}).withSource(def.src)
		}
		// Its C++ code keeps the template's lines, so #line can name them. Errors point at the invocation, like
		// for the rest of the output.
		origin := func(pos lexer.Position) Origin { // Positions' offsets are in the padded output.
			pos.Offset = offsetOf(def.src, pos)
			return Origin{def.source, pos.Line, &pos, def.src}
		}
		forEachNode(item, func(n any) {
			switch n := n.(type) {
			case *Block:
				n.Origin = origin(n.TextPos)
			case *Init:
				n.Origin = origin(n.Pos)
			case *Default:
				n.Origin = origin(n.Pos)
			case *Invoke:
				n.Site = (&Error{Pos: *origin(n.Pos).Start, Len: n.span()}).withSource(def.src)
			}
		})
		r.place(item)
	}
	// The invocation's doc comment documents the first declaration that has none.
	if r.inv.Doc != nil && len(tree.Items) > 0 {
		if d := docField(tree.Items[0]); d != nil && *d == nil {
			*d = &Doc{Pos: r.inv.Pos, Text: r.inv.Doc.Text}
		}
	}
	return tree.Items, nil
}
