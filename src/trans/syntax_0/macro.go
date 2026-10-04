package syntax_0

import (
	"cmp"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
	lua "github.com/yuin/gopher-lua"

	"gd++/trans/meta"
)

// defaultMacroDepth is how deeply invocations may nest, in what other invocations generate, unless
// Options.MacroDepth says otherwise.
const defaultMacroDepth = 64

// maxDepth returns how deeply invocations may nest.
func (x *expander) maxDepth() int {
	if x.opts.MacroDepth > 0 {
		return x.opts.MacroDepth
	}
	return defaultMacroDepth
}

// macroDef is a macro or template that code can invoke, declared in this file or another one of the package.
type macroDef struct {
	m      *Macro
	file   string // The name of the file that declares it.
	src    string // That file's source.
	source string // How #line names that file, for the C++ code of a template.
}

// expander expands the invocations of a file.
type expander struct {
	filename, src string
	opts          meta.Options
	defs          map[string]*macroDef
	generated     map[int]string   // The offsets of invocations, with what each one invokes, e.g. "macro stat".
	edits         []edit           // What the file's own invocations generated, where they are.
	lineStarts    []int            // The offsets of src's lines, for posAt.
	done          map[*Class]bool  // Classes whose members are expanded already.
	doneExterns   map[*Extern]bool // The same for externs.
	uniques       int              // How many names gd.unique made.
}

// cppKeywords are C++'s keywords, which macros can't be named after.
var cppKeywords = strings.Fields("alignas alignof and and_eq asm auto bitand bitor bool break case catch char char8_t char16_t char32_t " +
	"class compl concept const consteval constexpr constinit const_cast continue co_await co_return co_yield decltype default delete do " +
	"double dynamic_cast else enum explicit export extern false float for friend goto if inline int long mutable namespace new noexcept " +
	"not not_eq nullptr operator or or_eq private protected public register reinterpret_cast requires return short signed sizeof static " +
	"static_assert static_cast struct switch template this thread_local throw true try typedef typeid typename union unsigned using " +
	"virtual void volatile wchar_t while xor xor_eq")

// newExpander returns an expander for file, with its macros and templates and those of the dependencies in opts.
func newExpander(filename, src string, file *File, opts meta.Options) (*expander, error) {
	x := &expander{filename: filename, src: src, opts: opts, defs: map[string]*macroDef{}, generated: map[int]string{},
		done: map[*Class]bool{}, doneExterns: map[*Extern]bool{}}
	local := file.InlineMacros
	if file.FileMacro != nil {
		local = append([]*Macro{file.FileMacro}, local...)
	}
	for _, m := range local {
		if x.defs[m.Name] != nil {
			return nil, (&Error{Pos: m.Pos, Len: len(m.what()), Msg: fmt.Sprintf("The name %q is declared twice in this file.", m.Name),
				Hint: "Macro and template names must be unique in the package, like class, extern and enum names."}).withSource(src)
		}
		for _, p := range m.Params {
			if luaKeywords[p.Name] {
				return nil, (&Error{Pos: p.Pos, Len: len(p.Name), Msg: fmt.Sprintf("Parameter %q is a Lua keyword, so Lua code can't use it.", p.Name),
					Hint: "Rename it."}).withSource(src)
			}
		}
		if keywords[m.Name] || slices.Contains(cppKeywords, m.Name) {
			return nil, (&Error{Pos: m.Pos, Len: len(m.what()), Msg: fmt.Sprintf("%ss can't be named %q, since it's a keyword.", capitalize(m.what()), m.Name),
				Hint: "Choose another name."}).withSource(src)
		}
		x.defs[m.Name] = &macroDef{m: m, file: filename, src: src, source: cmp.Or(opts.SourceName, filename)}
	}
	parsed := map[string]*File{}
	for _, d := range opts.Dependencies {
		if d.Kind != meta.Macro && d.Kind != meta.Template || x.defs[d.Name] != nil {
			continue
		}
		f := parsed[d.File]
		if f == nil {
			var err error
			if f, err = Parse(d.File, d.Source); err != nil {
				return nil, err
			}
			parsed[d.File] = f
		}
		for _, m := range append([]*Macro{f.FileMacro}, f.InlineMacros...) {
			if m != nil && m.Name == d.Name {
				x.defs[d.Name] = &macroDef{m: m, file: d.File, src: d.Source, source: cmp.Or(d.SourceName, d.File)}
			}
		}
	}
	return x, nil
}

// errorAt returns an error at pos in the expanded file.
func (x *expander) errorAt(pos lexer.Position, n int, msg, hint string) *Error {
	return (&Error{Pos: pos, Len: max(n, 1), Msg: msg, Hint: hint}).withSource(x.src)
}

// invocationError returns an error at inv: at its Site, if it has one.
func (x *expander) invocationError(inv *Invoke, msg, hint string) *Error {
	if inv.Site != nil {
		e := *inv.Site
		e.Msg, e.Hint = msg, hint
		return &e
	}
	return x.errorAt(inv.Pos, inv.span(), msg, hint)
}

// expandFile replaces every invocation in f with what it generates, in declarations and in C++ code.
func (x *expander) expandFile(f *File) error {
	for _, inv := range f.Invokes {
		items, err := x.invoke(inv, &scope{kind: "file"}, 0)
		if err != nil {
			return err
		}
		x.addDeclEdit(inv, items)
		for _, item := range items {
			switch {
			case item.Class != nil:
				f.InlineClasses = append(f.InlineClasses, item.Class)
			case item.Extern != nil:
				f.InlineExterns = append(f.InlineExterns, item.Extern)
			case item.Member.Enum != nil && item.Member.Enum.Value == nil:
				f.InlineEnums = append(f.InlineEnums, item.Member.Enum)
			default:
				keyword, _ := item.Member.keyword()
				return x.errorAt(inv.Pos, inv.span(), fmt.Sprintf("%s generated a %s outside of any class.", x.what(inv), keyword),
					"Invoke it inside a class, or have it generate a class.")
			}
		}
	}
	f.Invokes = nil
	var err error
	if c := f.FileClass; c != nil {
		if c.Members, err = x.expandMembers(c.Members, &scope{kind: "class", owner: c.Name, fileLevel: true}, f, 0); err != nil {
			return err
		}
		x.done[c] = true
	}
	if e := f.FileExtern; e != nil {
		if e.Members, err = x.expandMembers(e.Members, &scope{kind: "extern", owner: e.Name, fileLevel: true}, f, 0); err != nil {
			return err
		}
		x.doneExterns[e] = true
	}
	for i := 0; i < len(f.InlineClasses); i++ {
		if c := f.InlineClasses[i]; !x.done[c] {
			if c.Members, err = x.expandMembers(c.Members, &scope{kind: "class", owner: c.Name}, nil, 0); err != nil {
				return err
			}
			x.done[c] = true
		}
	}
	for i := 0; i < len(f.InlineExterns); i++ {
		if e := f.InlineExterns[i]; !x.doneExterns[e] {
			if e.Members, err = x.expandMembers(e.Members, &scope{kind: "extern", owner: e.Name}, nil, 0); err != nil {
				return err
			}
			x.doneExterns[e] = true
		}
	}
	// Then invocations in C++ code, which only generate C++.
	expandIn := func(owner string, node any) error {
		var err error
		forEachNode(node, func(n any) {
			if err != nil {
				return
			}
			switch n := n.(type) {
			case *Block:
				lines := n.Origin
				if lines.Source == "" {
					lines = Origin{Source: x.source(), Line: n.TextPos.Line}
				}
				n.Text, err = x.expandCode(n.Text, n.TextPos, owner, 0, &lines, x.codeErrors(n.Origin, n.Generated, n.TextPos))
			case *Init:
				if n.Expr != "" {
					n.Expr, err = x.expandCode(n.Expr, n.Pos, owner, 0, nil, x.codeErrors(n.Origin, n.Generated, n.Pos))
				}
			case *Default:
				if n.Expr != "" {
					n.Expr, err = x.expandCode(n.Expr, n.Pos, owner, 0, nil, x.codeErrors(n.Origin, n.Generated, n.Pos))
				}
			}
		})
		return err
	}
	classes, externs := f.InlineClasses, f.InlineExterns
	if f.FileClass != nil {
		classes = append([]*Class{f.FileClass}, classes...)
	}
	if f.FileExtern != nil {
		externs = append([]*Extern{f.FileExtern}, externs...)
	}
	for _, c := range classes {
		if err := expandIn(c.Name, c); err != nil {
			return err
		}
	}
	for _, e := range externs {
		if err := expandIn(e.Name, e); err != nil {
			return err
		}
	}
	return nil
}

// anonymous is the name of macro blocks, invoke { ... }, in messages.
const anonymous = "{ ... }"

// def returns what inv invokes: a macro or template of the package, or a macro block's body. It's nil if there's none.
func (x *expander) def(inv *Invoke) *macroDef {
	if inv.Body == nil {
		return x.defs[inv.Name]
	}
	return &macroDef{m: &Macro{Pos: inv.Pos, Name: anonymous, Body: inv.Body}, file: x.filename, src: x.src, source: x.source()}
}

// what names what inv invokes, e.g. "Macro stat".
func (x *expander) what(inv *Invoke) string {
	if d := x.def(inv); d != nil {
		return capitalize(d.m.what()) + " " + inv.Name
	}
	return "Macro " + inv.Name
}

// expandMembers replaces the invocations among members with what they generate. Inline classes and externs that
// they generate go into f, if sc is the body of the file-level class or extern.
func (x *expander) expandMembers(members []*Member, sc *scope, f *File, depth int) ([]*Member, error) {
	var out []*Member
	for _, m := range members {
		if m.Invoke == nil {
			out = append(out, m)
			continue
		}
		items, err := x.invoke(m.Invoke, sc, depth)
		if err != nil {
			return nil, err
		}
		if depth == 0 {
			x.addDeclEdit(m.Invoke, items)
		}
		for _, item := range items {
			switch {
			case item.Member != nil:
				out = append(out, item.Member)
			case f != nil && item.Class != nil:
				f.InlineClasses = append(f.InlineClasses, item.Class)
			case f != nil && item.Extern != nil:
				f.InlineExterns = append(f.InlineExterns, item.Extern)
			default:
				return nil, x.errorAt(m.Invoke.Pos, m.Invoke.span(), fmt.Sprintf("%s generated a class or extern inside %s %s, but they can't be nested.",
					x.what(m.Invoke), sc.kind, sc.owner), "Invoke it at the top level of the file.")
			}
		}
	}
	return out, nil
}

// invoke runs the macro or template that inv invokes, in scope sc, and returns what it generates, expanded.
func (x *expander) invoke(inv *Invoke, sc *scope, depth int) (out []*topItem, err error) {
	def := x.def(inv)
	switch {
	case depth >= x.maxDepth():
		return nil, x.invocationError(inv, fmt.Sprintf("Invocations are nested more than %d levels deep here.", x.maxDepth()),
			"A macro or template probably invokes itself, directly or through others. If it only needs to nest deeper, raise macro_depth in gd++pkg.toml.")
	case def == nil:
		return nil, x.unknown(inv)
	case len(inv.Annotations) > 0:
		a := inv.Annotations[0]
		return nil, x.errorAt(a.Pos, len(a.Name)+1, "Invocations take no annotations.", "Have the macro add them to what it generates.")
	case len(def.m.Body.Helpers) > 0:
		return nil, x.helpersError(inv)
	}
	if _, ok := x.generated[inv.Pos.Offset]; !ok {
		x.generated[inv.Pos.Offset] = x.frame(def, inv, depth)
	}
	defer func() {
		if err != nil {
			err = addFrame(err, x.frame(def, inv, depth))
		}
	}()
	sc = &scope{kind: sc.kind, owner: sc.owner, fileLevel: sc.fileLevel} // Without what other invocations emitted.
	r, free := x.newRun(def, inv, sc, depth)
	defer free()
	values, err := r.args(def.m.Params)
	if err != nil {
		return nil, err
	}
	var items []*topItem
	if def.m.Template {
		items, err = r.instantiate(def, values)
	} else {
		var fn *lua.LFunction
		if fn, err = r.load(def.m.Body.Text, def.m.Body.TextPos, def.file, def.m.Params); err == nil {
			if err = r.call(fn, values...); err == nil {
				items, err = r.collect(sc)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	// Expand the invocations in what it generated.
	for _, item := range items {
		switch {
		case item.Member != nil && item.Member.Invoke != nil:
			nested, err := x.invoke(item.Member.Invoke, sc, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, nested...)
			continue
		case item.Class != nil:
			if item.Class.Members, err = x.expandMembers(item.Class.Members, &scope{kind: "class", owner: item.Class.Name}, nil, depth+1); err != nil {
				return nil, err
			}
			x.done[item.Class] = true
		case item.Extern != nil:
			if item.Extern.Members, err = x.expandMembers(item.Extern.Members, &scope{kind: "extern", owner: item.Extern.Name}, nil, depth+1); err != nil {
				return nil, err
			}
			x.doneExterns[item.Extern] = true
		}
		out = append(out, item)
	}
	return out, nil
}

// frame describes inv, at depth, as a line of a macro call stack: what it invokes, and where.
func (x *expander) frame(def *macroDef, inv *Invoke, depth int) string {
	if depth > 0 || inv.Site != nil {
		return fmt.Sprintf("%s %s, invoked in generated code", def.m.what(), inv.Name)
	}
	return fmt.Sprintf("%s %s, invoked at %s:%d", def.m.what(), inv.Name, inv.Pos.Filename, inv.Pos.Line)
}

// addFrame adds frame to the macro call stack of err, as the outermost invocation so far.
func addFrame(err error, frame string) error {
	var e *Error
	if errors.As(err, &e) {
		e.Stack = append(e.Stack, frame)
	}
	return err
}

// span returns how many characters an error about the invocation underlines: "invoke NAME", or "invoke".
func (inv *Invoke) span() int {
	if inv.Body != nil {
		return len("invoke")
	}
	return len("invoke ") + len(inv.Name)
}

// helpersError returns the error for a macro block, inv, that declares macros or templates.
func (x *expander) helpersError(inv *Invoke) *Error {
	h := inv.Body.Helpers[0]
	msg, hint := fmt.Sprintf("A macro block can't declare %ss.", h.what()), "Declare it at the top level of the file."
	if inv.Site != nil {
		return x.invocationError(inv, msg, hint)
	}
	return x.errorAt(h.Pos, len(h.what()), msg, hint)
}

// unknown returns the error for an invocation of a macro that doesn't exist.
func (x *expander) unknown(inv *Invoke) *Error {
	var names []string
	for name := range x.defs {
		names = append(names, name)
	}
	hint := fmt.Sprintf("Declare it with \"macro %s(...) { ... }\", in any file of the package.", inv.Name)
	if s := suggest(inv.Name, names...); s != "" {
		hint = fmt.Sprintf("Did you mean %q?", s)
	}
	return x.invocationError(inv, fmt.Sprintf("There is no macro or template %s.", inv.Name), hint)
}

// expandCode replaces the invocations in C++ code, which starts at pos, with the C++ that their macros generate.
// The code is a block, whose first line #line names as block says, or with a nil block, a one-line expression, e.g.
// an initial value.
//   - In a block, an invocation is a statement: "invoke NAME(...);", whose ";" it replaces too. A macro block's ";" is
//     optional. If what the macro generates spans several lines or holds preprocessor directives, it keeps its
//     lines, each named as the invocation's line by #line, and a #line after them names the line where the
//     invocation ends. Otherwise it goes on the invocation's line.
//   - In an expression, an invocation has no ";", and what it generates goes on its line.
//
// Either way, the lines after an invocation keep their #line numbers.
//
// For code that isn't in the expanded file, pos is the invocation that put it there, and errorAt makes the errors
// about the invocations in it, at their positions in code.
func (x *expander) expandCode(code string, pos lexer.Position, owner string, depth int, block *Origin, errorAt errorFunc) (_ string, err error) {
	if depth == 0 && errorAt != nil { // Code that an invocation in the file generated: its errors end with that one.
		defer func() {
			if err != nil {
				err = addFrame(err, x.generated[pos.Offset])
			}
		}()
	}
	if !strings.Contains(code, "invoke") {
		return code, nil
	}
	l, err := gdppLexer.LexString(x.filename, code)
	if err != nil {
		return code, nil // The C++ compiler reports it.
	}
	tokens, err := lexer.ConsumeAll(l)
	if err != nil {
		return code, nil
	}
	tokens = significant(tokens)
	var orig []lexer.Token // For code in the expanded file: its tokens in src, which match tokens.
	var out strings.Builder
	last := 0 // The end of what's copied to out.
	for i := 0; i+2 < len(tokens); i++ {
		// invoke NAME( or invoke NAME {, but not a member or a scope's name, e.g. std::invoke. Or invoke { at the start of
		// the code or of a statement.
		t, name, open := tokens[i], tokens[i+1], tokens[i+2]
		var prev lexer.Token
		if i > 0 {
			prev = tokens[i-1]
		}
		var body *MacroBody
		switch {
		case t.Type != tokIdent || t.Pos.Offset < last:
			continue
		case t.Value == "invoke" && isPunct(name, "{") && (i == 0 || isPunct(prev, ";") || isPunct(prev, "{") || isPunct(prev, "}")):
			open, name.Value, body = name, anonymous, &MacroBody{}
		case t.Value != "invoke" || name.Type != tokIdent || x.defs[name.Value] == nil || !isPunct(open, "(") && !isPunct(open, "{") ||
			isPunct(prev, ".") || isPunct(prev, ">") || isPunct(prev, ":"):
			continue
		}
		inv := &Invoke{Pos: shift(t.Pos, pos, x.src), Name: name.Value, Body: body}
		start := open.Pos.Offset
		if errorAt != nil {
			inv.Pos, inv.Site = pos, errorAt(t.Pos, inv.span(), "", "")
		}
		sub, err := gdppLexer.LexString(x.filename, code[start:])
		if err != nil {
			return "", err
		}
		lex, err := lexer.Upgrade(sub, elidedTypes()...)
		if err != nil {
			return "", err
		}
		inv.Args = &ArgList{}
		parse := inv.Args.Parse
		if inv.Body != nil {
			parse = inv.Body.Parse
		}
		if err := parse(lex); err != nil {
			var e *Error
			if errors.As(err, &e) {
				if errorAt != nil {
					return "", errorAt(shift(e.Pos, open.Pos, ""), e.Len, e.Msg, e.Hint)
				}
				e.Pos = shift(shift(e.Pos, open.Pos, ""), pos, x.src)
				return "", e.withSource(x.src)
			}
			return "", err
		}
		end := start + lex.RawPeek().Pos.Offset
		if lex.RawPeek().EOF() {
			end = len(code)
		}
		if block != nil {
			if semi := lex.Peek(); isPunct(*semi, ";") {
				end = start + semi.Pos.Offset + 1
			} else if inv.Body == nil { // A macro block's ";" is optional.
				// Right after the invocation, where the ";" is missing.
				p := shift(lex.RawPeek().Pos, open.Pos, "")
				if lex.RawPeek().EOF() {
					p = t.Pos
				}
				msg := fmt.Sprintf("Expected \";\" after the invocation of %s, but found %s.", inv.Name, describe(*semi))
				hint := "In a C++ block, an invocation is a statement, which ends with \";\": invoke log(\"hit\");. The macro's C++ replaces it, \";\" included."
				if errorAt != nil {
					return "", errorAt(p, 1, msg, hint)
				}
				return "", x.errorAt(shift(p, pos, x.src), 1, msg, hint)
			}
		}
		forEachNode(inv.Args, func(n any) {
			if p := reflectPos(n); p != nil {
				*p = shift(shift(*p, open.Pos, ""), pos, x.src)
			}
			if b, ok := n.(*Block); ok {
				b.TextPos = shift(shift(b.TextPos, open.Pos, ""), pos, x.src)
			}
		})
		if b := inv.Body; b != nil {
			b.Pos, b.TextPos = inv.Pos, shift(shift(b.TextPos, open.Pos, ""), pos, x.src)
			for _, h := range b.Helpers {
				h.Pos = shift(shift(h.Pos, open.Pos, ""), pos, x.src)
			}
		}
		var at *Origin // How #line names the invocation's line, in a block.
		if block != nil {
			at = &Origin{Source: block.Source, Line: block.Line + strings.Count(code[:t.Pos.Offset], "\n")}
		}
		text, err := x.invokeCode(inv, owner, depth, at)
		if err != nil {
			return "", err
		}
		var piece strings.Builder // What replaces code[t.Pos.Offset:end].
		if at != nil && (hasDirective(text) || strings.Contains(strings.Trim(text, "\n"), "\n")) {
			directive := fmt.Sprintf("#line %d %q", at.Line, at.Source)
			piece.WriteString("\n")
			prev := "" // The line before, which may already name the line, or continue on this one with "\".
			for _, l := range strings.Split(strings.Trim(text, "\n"), "\n") {
				if prev != directive && l != directive && !strings.HasSuffix(prev, "\\") {
					piece.WriteString(directive + "\n")
				}
				piece.WriteString(l + "\n")
				prev = l
			}
			fmt.Fprintf(&piece, "#line %d %q\n", at.Line+strings.Count(code[t.Pos.Offset:end], "\n"), at.Source)
		} else {
			piece.WriteString(oneLine(text))
			piece.WriteString(strings.Repeat("\n", strings.Count(code[t.Pos.Offset:end], "\n")))
		}
		out.WriteString(code[last:t.Pos.Offset] + piece.String())
		if errorAt == nil { // Code in the expanded file: its source changes too.
			if orig == nil { // The whole file lexed already, so the rest of it does too.
				l, _ := gdppLexer.LexString(x.filename, x.src[pos.Offset:])
				all, _ := lexer.ConsumeAll(l)
				orig = significant(all)
			}
			k := i // The invocation's last token.
			for k+1 < len(tokens) && tokens[k+1].Pos.Offset < end {
				k++
			}
			x.edits = append(x.edits, edit{start: pos.Offset + orig[i].Pos.Offset, end: pos.Offset + orig[k].Pos.Offset + len(orig[k].Value), text: piece.String()})
		}
		last = end
	}
	out.WriteString(code[last:])
	return out.String(), nil
}

// invokeCode runs the macro that inv invokes in C++ code, and returns the C++ it generates, expanded. In a block,
// at says how #line names inv's line. In an expression, it's nil.
func (x *expander) invokeCode(inv *Invoke, owner string, depth int, at *Origin) (_ string, err error) {
	def := x.def(inv)
	switch {
	case depth >= x.maxDepth():
		return "", x.invocationError(inv, fmt.Sprintf("Invocations are nested more than %d levels deep here.", x.maxDepth()),
			"A macro probably invokes itself, directly or through others. If it only needs to nest deeper, raise macro_depth in gd++pkg.toml.")
	case def.m.Template:
		return "", x.invocationError(inv, fmt.Sprintf("Template %s can't be used in C++ code, since templates generate declarations.", inv.Name),
			"Invoke it where declarations go, or use a macro that generates C++ with gd.text.")
	case len(def.m.Body.Helpers) > 0:
		return "", x.helpersError(inv)
	}
	defer func() {
		if err != nil {
			err = addFrame(err, x.frame(def, inv, depth))
		}
	}()
	sc := &scope{kind: "cpp", owner: owner}
	r, free := x.newRun(def, inv, sc, depth)
	defer free()
	values, err := r.args(def.m.Params)
	if err != nil {
		return "", err
	}
	fn, err := r.load(def.m.Body.Text, def.m.Body.TextPos, def.file, def.m.Params)
	if err != nil {
		return "", err
	}
	if err := r.call(fn, values...); err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, c := range sc.chunks {
		sb.WriteString(c.text)
	}
	// Its own invocations are statements, or expressions, like it. All its lines come from inv.
	text, err := x.expandCode(sb.String(), inv.Pos, owner, depth+1, at, func(_ lexer.Position, _ int, msg, hint string) *Error {
		return x.invocationError(inv, msg, hint)
	})
	if err != nil {
		return "", err
	}
	if at == nil && hasDirective(text) {
		return "", x.invocationError(inv, fmt.Sprintf("Macro %s generated a preprocessor directive, which only works in C++ blocks.", inv.Name),
			"Here, the C++ must be an expression on one line.")
	}
	return text, nil
}

// hasDirective reports whether C++ code holds a preprocessor directive: a line that starts with "#".
func hasDirective(code string) bool {
	for _, l := range strings.Split(code, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			return true
		}
	}
	return false
}

// source returns how #line names the expanded file.
func (x *expander) source() string {
	return cmp.Or(x.opts.SourceName, x.filename)
}

// oneLine returns C++ code on a single line, without comments.
func oneLine(code string) string {
	l, err := gdppLexer.LexString("", code)
	if err != nil {
		return strings.ReplaceAll(code, "\n", " ")
	}
	tokens, _ := lexer.ConsumeAll(l)
	var sb strings.Builder
	for _, t := range tokens {
		switch {
		case t.EOF():
		case isComment(t) || isDoc(t) || t.Type == tokNewline:
			sb.WriteByte(' ')
		default:
			sb.WriteString(t.Value)
		}
	}
	return strings.TrimSpace(sb.String())
}

// errorFunc makes an error at p, a position in some code, with msg and hint.
type errorFunc func(p lexer.Position, n int, msg, hint string) *Error

// codeErrors returns how to make errors at positions in C++ code at pos, from o: in the template's file for a
// template's code, and at the invocation, at pos, for code that a macro generated. For code in the expanded file,
// it's nil.
func (x *expander) codeErrors(o Origin, generated bool, pos lexer.Position) errorFunc {
	switch {
	case o.Src != "":
		return func(p lexer.Position, n int, msg, hint string) *Error {
			return (&Error{Pos: shift(p, *o.Start, o.Src), Len: max(n, 1), Msg: msg, Hint: hint}).withSource(o.Src)
		}
	case generated:
		return func(_ lexer.Position, _ int, msg, hint string) *Error {
			return x.lineError(x.filename, pos.Line, msg, hint)
		}
	}
	return nil
}

// shift turns p, a position in a text that starts at base, into a position in base's file. With src, it also sets
// the offset in src.
func shift(p, base lexer.Position, src string) lexer.Position {
	out := lexer.Position{Filename: base.Filename, Line: base.Line + p.Line - 1, Column: p.Column}
	if p.Line == 1 {
		out.Column = base.Column + p.Column - 1
	}
	if src != "" {
		out.Offset = offsetOf(src, out)
	} else {
		out.Offset = base.Offset + p.Offset
	}
	return out
}

// reflectPos returns a pointer to the Pos field of node, a pointer to an AST struct, or nil.
func reflectPos(node any) *lexer.Position {
	v := reflect.ValueOf(node).Elem()
	if f := v.FieldByName("Pos"); f.IsValid() && f.Type() == reflect.TypeOf(lexer.Position{}) {
		return f.Addr().Interface().(*lexer.Position)
	}
	return nil
}

// docField returns a pointer to the Doc field of item's declaration, or nil if it has none.
func docField(item *topItem) **Doc {
	var node any = item.Class
	switch {
	case item.Extern != nil:
		node = item.Extern
	case item.Member != nil:
		m := item.Member
		for _, n := range []any{m.Func, m.Var, m.Signal, m.Enum} {
			if !reflect.ValueOf(n).IsNil() {
				node = n
			}
		}
	}
	if node == nil || reflect.ValueOf(node).IsNil() {
		return nil
	}
	if f := reflect.ValueOf(node).Elem().FieldByName("Doc"); f.IsValid() {
		return f.Addr().Interface().(**Doc)
	}
	return nil
}

func elidedTypes() []lexer.TokenType {
	var types []lexer.TokenType
	for _, name := range elided {
		types = append(types, sym[name])
	}
	return types
}

// parsedItems is GD++ that a macro or template generates: classes, externs and members.
type parsedItems struct {
	Pos   lexer.Position
	Items []*topItem `parser:"@@*"`
}

var itemsParser = participle.MustBuild[parsedItems](
	participle.Lexer(gdppLexer),
	participle.Elide(elided...),
	participle.UseLookahead(participle.MaxLookahead),
)
