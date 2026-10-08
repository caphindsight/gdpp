package syntax_1

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

var parser = participle.MustBuild[parsedFile](
	participle.Lexer(gdppLexer),
	participle.Elide(elided...),
	participle.UseLookahead(participle.MaxLookahead),
)

// Parse parses the GD++ source src. Errors are *Error values, with a location, a source excerpt and a hint.
func Parse(filename, src string) (*File, error) {
	tree, err := parseTree(parser, filename, src)
	if err != nil {
		return nil, err
	}
	file, e := tree.toFile()
	if e != nil {
		return nil, e.withSource(src)
	}
	return file, nil
}

// parseTree parses src with p, and finishes the tree: converts integers, and moves doc comments and positions.
func parseTree[T any](p *participle.Parser[T], filename, src string) (*T, error) {
	tokens, err := p.Lex(filename, strings.NewReader(src))
	if err != nil {
		return nil, err
	}
	if e := checkBalance(src, tokens); e != nil {
		return nil, e.withSource(src)
	}
	tree, err := p.ParseString(filename, src)
	if err != nil {
		return nil, explain(err, tokens).withSource(src)
	}
	sig := significant(tokens)
	var e *Error
	forEachNode(tree, func(node any) {
		if n, ok := node.(*Int); ok && e == nil {
			e = n.convert()
		}
		if e == nil {
			e = liftDoc(node, sig)
		}
		moveToKeyword(node, sig)
		if n, ok := node.(*Invoke); ok && n.Body != nil {
			n.Name, n.Args = anonymous, &ArgList{Pos: n.Pos}
		}
	})
	if e != nil {
		return nil, e.withSource(src)
	}
	return tree, nil
}

// forEachNode calls fn with a pointer to every AST struct reachable from node, parents first.
func forEachNode(node any, fn func(any)) {
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Pointer:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		case reflect.Struct:
			if v.Type() == reflect.TypeOf(lexer.Position{}) {
				return
			}
			fn(v.Addr().Interface())
			for i := 0; i < v.NumField(); i++ {
				walk(v.Field(i))
			}
		}
	}
	walk(reflect.ValueOf(node))
}

// liftDoc moves a doc comment written among a declaration's annotations to its Doc, as if it came before them.
func liftDoc(node any, sig []lexer.Token) *Error {
	v := reflect.ValueOf(node).Elem()
	annotations, doc := v.FieldByName("Annotations"), v.FieldByName("Doc")
	if !annotations.IsValid() {
		return nil
	}
	var kept []*Annotation
	for _, a := range annotations.Interface().([]*Annotation) {
		switch {
		case a.Doc == nil:
			kept = append(kept, a)
		case !doc.IsNil():
			i := slices.IndexFunc(sig, func(t lexer.Token) bool { return t.Pos.Offset == a.Doc.Pos.Offset })
			return errorAt(sig[i], "A declaration can have only one doc comment.", "Merge the doc comments into one.")
		default:
			doc.Set(reflect.ValueOf(a.Doc))
		}
	}
	annotations.Set(reflect.ValueOf(kept))
	return nil
}

// moveToKeyword sets the Pos of a declaration with a Doc or Annotations to its keyword, skipping over them.
func moveToKeyword(node any, sig []lexer.Token) {
	v := reflect.ValueOf(node).Elem()
	doc, annotations := v.FieldByName("Doc"), v.FieldByName("Annotations")
	if !annotations.IsValid() || (!doc.IsValid() || doc.IsNil()) && annotations.Len() == 0 {
		return
	}
	pos := v.FieldByName("Pos").Addr().Interface().(*lexer.Position)
	i := 0
	for sig[i].Pos.Offset < pos.Offset {
		i++
	}
	for depth := 0; ; i++ {
		t := sig[i]
		switch {
		case isOneOf(t, "DocBlockOpen", "DocBlockNest") || depth > 0 && isPunct(t, "("):
			depth++
		case isOneOf(t, "DocBlockClose") || depth > 0 && isPunct(t, ")"):
			depth--
		case depth > 0 || isDoc(t) || isPunct(t, "@"):
		case t.Type == tokIdent && isPunct(at(sig, i-1), "@"):
			if isPunct(at(sig, i+1), "(") {
				depth, i = 1, i+1
			}
		default:
			*pos = t.Pos
			return
		}
	}
}

func (n *Int) convert() *Error {
	raw := strings.ReplaceAll(n.Raw, "'", "") // C++14 digit separators.
	value, err := strconv.ParseInt(raw, 0, 64)
	e := &Error{Pos: n.Pos, Len: utf8.RuneCountInString(n.Raw)}
	switch {
	case err == nil && !strings.ContainsAny(raw, "_oO"):
		n.Value = value
		return nil
	case strings.HasSuffix(err.Error(), strconv.ErrRange.Error()):
		e.Msg = fmt.Sprintf("The value %s does not fit in a 64-bit integer.", n.Raw)
		e.Hint = "Enum values must be between -9223372036854775808 and 9223372036854775807."
	default:
		e.Msg = fmt.Sprintf("Enum values must be integers, but found %s.", n.Raw)
		e.Hint = "Write a whole number, such as 42, -1 or 0x10."
	}
	return e
}

// enumOps are the binary operators of enum values, by precedence. Unary operators bind tighter: unaryPrec.
var enumOps = map[string]int{"|": 1, "^": 2, "&": 3, "+": 4, "-": 4, "*": 5, "/": 5, "%": 5}

const unaryPrec = 6

func (x *EnumExpr) Parse(lex *lexer.PeekingLexer) error {
	e, err := parseEnumBinary(lex, "=", 1)
	if err == nil {
		*x = *e
	}
	return err
}

// parseEnumBinary parses operands joined by operators of precedence prec or higher. after is the token before.
func parseEnumBinary(lex *lexer.PeekingLexer, after string, prec int) (*EnumExpr, error) {
	left, err := parseEnumOperand(lex, after)
	for err == nil {
		op := *lex.Peek()
		p := enumOps[op.Value]
		if op.Type != tokPunct || p < prec {
			return left, nil
		}
		lex.Next()
		var right *EnumExpr
		right, err = parseEnumBinary(lex, op.Value, p+1)
		left = &EnumExpr{Pos: left.Pos, Op: op.Value, Left: left, Right: right}
	}
	return nil, err
}

// parseEnumOperand parses an integer, a value, ~ or - and an operand, or an expression in parentheses.
func parseEnumOperand(lex *lexer.PeekingLexer, after string) (*EnumExpr, error) {
	t := *lex.Next()
	x := &EnumExpr{Pos: t.Pos}
	switch {
	case isPunct(t, "~") || isPunct(t, "-") && lex.Peek().Type != tokNumber:
		right, err := parseEnumOperand(lex, t.Value)
		x.Op, x.Right = t.Value, right
		return x, err
	case isPunct(t, "("):
		inner, err := parseEnumBinary(lex, "(", 1)
		if close := *lex.Next(); err == nil && !isPunct(close, ")") {
			return nil, errorAt(close, fmt.Sprintf("Expected an operator or \")\", but found %s.", describe(close)),
				"Combine values with |, &, ^, ~, +, -, *, / and %.")
		}
		return inner, err
	case t.Type == tokNumber || isPunct(t, "-"): // A negative number is one integer, so the smallest one fits.
		x.Int = &Int{Pos: t.Pos, Raw: t.Value}
		if t.Value == "-" {
			x.Int.Raw += lex.Next().Value
		}
		return x, nil
	case t.Type == tokIdent:
		x.Ref = &EnumRef{Pos: t.Pos, Name: t.Value}
		for isPunct(*lex.Peek(), ".") {
			lex.Next()
			name := *lex.Next()
			if name.Type != tokIdent {
				return nil, errorAt(name, fmt.Sprintf("Expected a name after \".\", but found %s.", describe(name)),
					"Values of other enums are written Enum.VALUE, e.g. Suit.HEARTS or Node.ProcessMode.PROCESS_MODE_ALWAYS.")
			}
			x.Ref.Name += "." + name.Value
		}
		return x, nil
	}
	hint := ""
	if after == "=" {
		hint = "Enum values are integers, such as 42 or 0x10, earlier values, such as RED, values of other enums, " +
			"such as Suit.HEARTS, or these combined with |, &, ^, ~, +, -, *, / and % and parentheses."
	}
	return nil, errorAt(t, fmt.Sprintf("Expected a value after %q, but found %s.", after, describe(t)), hint)
}

// parseEnumExpr parses src, the text of an enum value's expression. It returns nil if src is broken.
func parseEnumExpr(src string) *EnumExpr {
	l, err := gdppLexer.LexString("", src)
	if err != nil {
		return nil
	}
	var elide []lexer.TokenType
	for _, name := range elided {
		elide = append(elide, sym[name])
	}
	lex, err := lexer.Upgrade(l, elide...)
	x := &EnumExpr{}
	if err != nil || x.Parse(lex) != nil || !lex.Peek().EOF() {
		return nil
	}
	var e *Error
	forEachNode(x, func(node any) {
		if n, ok := node.(*Int); ok && e == nil {
			e = n.convert()
		}
	})
	if e != nil {
		return nil
	}
	return x
}

// String returns the expression as GD++ source, with only the parentheses it needs.
func (x *EnumExpr) String() string {
	switch {
	case x.Int != nil:
		return x.Int.Raw
	case x.Ref != nil:
		return x.Ref.Name
	case x.Left == nil:
		return x.Op + x.Right.operand(unaryPrec)
	}
	p := enumOps[x.Op]
	return x.Left.operand(p) + " " + x.Op + " " + x.Right.operand(p+1) // Operators group left to right.
}

// operand returns x as an operand of an operator of precedence prec, in parentheses if it binds looser.
func (x *EnumExpr) operand(prec int) string {
	if x.Left != nil && enumOps[x.Op] < prec {
		return "(" + x.String() + ")"
	}
	return x.String()
}

// peekRaw returns the next token, even if it is elided (whitespace, newline, comment).
func peekRaw(lex *lexer.PeekingLexer) lexer.Token {
	t, _ := lex.PeekAny(func(lexer.Token) bool { return true })
	return t
}

// nextRaw consumes and returns the next token, even if it is elided.
func nextRaw(lex *lexer.PeekingLexer) lexer.Token {
	t, cursor := lex.PeekAny(func(lexer.Token) bool { return true })
	lex.FastForward(cursor)
	return t
}

// codeBuilder rebuilds C++ source from raw tokens. Each comment is replaced by the newlines it spans,
// or by a space if it spans none, so line numbers stay aligned with the GD++ source.
type codeBuilder struct {
	sb        strings.Builder
	inComment bool // Whether the last token was a comment on a single line.
	keepDocs  bool // Whether to keep doc comments, e.g. in a template's body.
}

func (c *codeBuilder) add(t lexer.Token) {
	if isComment(t) || isDoc(t) && !c.keepDocs {
		n := strings.Count(t.Value, "\n")
		c.sb.WriteString(strings.Repeat("\n", n))
		c.inComment = n == 0
		return
	}
	if c.inComment {
		c.sb.WriteByte(' ')
		c.inComment = false
	}
	c.sb.WriteString(t.Value)
}

// String returns the code with trailing whitespace removed from every line.
func (c *codeBuilder) String() string {
	lines := strings.Split(c.sb.String(), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t\r\f\v")
	}
	return strings.Join(lines, "\n")
}

func (b *Block) Parse(lex *lexer.PeekingLexer) error {
	open := *lex.Peek()
	if !isPunct(open, "{") {
		return participle.NextMatch
	}
	lex.Next()
	b.Pos, b.TextPos = open.Pos, open.Pos
	b.TextPos.Advance("{")
	var code codeBuilder
	for depth := 1; ; {
		t := nextRaw(lex)
		switch {
		case t.EOF():
			return errorAt(open, "This \"{\" is never closed.", "Add a matching \"}\".")
		case isPunct(t, "{"):
			depth++
		case isPunct(t, "}"):
			if depth--; depth == 0 {
				b.Text = code.String()
				return nil
			}
		}
		code.add(t)
	}
}

func (in *Init) Parse(lex *lexer.PeekingLexer) error {
	return in.parse(lex, false)
}

func (d *Default) Parse(lex *lexer.PeekingLexer) error {
	return (*Init)(d).parse(lex, true)
}

// parse parses a value after "=". With endAtComma set, a "," outside brackets ends the expression.
func (in *Init) parse(lex *lexer.PeekingLexer, endAtComma bool) error {
	for t := peekRaw(lex); t.Type == tokWhitespace || isComment(t) || isDoc(t); t = peekRaw(lex) {
		nextRaw(lex)
	}
	first := peekRaw(lex)
	if first.EOF() || first.Type == tokNewline || isPunct(first, ",") || closers[first.Value] != "" && first.Type == tokPunct {
		return errorAt(first, fmt.Sprintf("Expected a value after \"=\", but found %s.", describe(first)),
			"Write a C++ expression, or a { ... } block that returns the value.")
	}
	in.Pos = first.Pos
	if isPunct(first, "{") {
		in.Block = &Block{}
		return in.Block.Parse(lex)
	}
	var code codeBuilder
	for depth := 0; ; {
		t := peekRaw(lex)
		if t.EOF() || depth == 0 && (t.Type == tokNewline || endAtComma && isPunct(t, ",") || t.Type == tokPunct && closers[t.Value] != "") {
			break
		}
		nextRaw(lex)
		if t.Type == tokPunct && strings.Contains("([{", t.Value) {
			depth++
		} else if t.Type == tokPunct && closers[t.Value] != "" {
			depth--
		}
		code.add(t)
	}
	in.Expr = strings.TrimSpace(code.String())
	return nil
}

func (d *Doc) Parse(lex *lexer.PeekingLexer) error {
	first := *lex.Peek()
	d.Pos = first.Pos
	switch first.Type {
	case tokDocLine:
		var lines []string
		for lex.Peek().Type == tokDocLine {
			line := strings.TrimPrefix(lex.Next().Value, "///")
			lines = append(lines, strings.TrimRight(strings.TrimPrefix(line, " "), " \t\r"))
		}
		d.Text = strings.Join(lines, "\n")
	case sym["DocBlockOpen"]:
		var text strings.Builder
		text.WriteString(strings.TrimPrefix(lex.Next().Value, "/**"))
		for depth := 1; ; {
			t := *lex.Next()
			if isOneOf(t, "DocBlockNest") {
				depth++
			} else if isOneOf(t, "DocBlockClose") {
				depth--
			}
			if depth == 0 || t.EOF() {
				break
			}
			text.WriteString(t.Value)
		}
		d.Text = dedent(text.String())
	default:
		return participle.NextMatch
	}
	return nil
}

// dedent removes trailing whitespace, the indentation common to all non-blank lines, and leading and
// trailing blank lines. The first line follows "/**", so its indentation is dropped and not counted.
func dedent(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t\r")
	}
	lines[0] = strings.TrimLeft(lines[0], " \t")
	common, set := "", false
	for _, line := range lines[1:] {
		ind := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if line == "" {
			continue
		} else if !set {
			common, set = ind, true
		}
		for !strings.HasPrefix(ind, common) {
			common = common[:len(common)-1]
		}
	}
	for i := 1; i < len(lines); i++ {
		lines[i] = strings.TrimPrefix(lines[i], common)
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}

func (b *MacroBody) Parse(lex *lexer.PeekingLexer) error {
	open := *lex.Peek()
	if !isPunct(open, "{") {
		return participle.NextMatch
	}
	lex.Next()
	return b.parse(lex, open, false)
}

func (b *fileBody) Parse(lex *lexer.PeekingLexer) error {
	return (*MacroBody)(b).parse(lex, peekRaw(lex), true)
}

// parse reads a macro or template body after open, its "{", up to the matching "}", or with toEOF, from open to
// the end of the file. It cuts out the macros and templates declared at the body's top level, into Helpers.
func (b *MacroBody) parse(lex *lexer.PeekingLexer, open lexer.Token, toEOF bool) error {
	b.Pos, b.TextPos = open.Pos, open.Pos
	if !toEOF {
		b.TextPos.Advance("{")
	}
	var code, template codeBuilder
	template.keepDocs = true
	for depth := 1; ; {
		t := peekRaw(lex)
		switch {
		case t.EOF() && toEOF:
			b.Text, b.Template = code.String(), template.String()
			return nil
		case t.EOF():
			return errorAt(open, "This \"{\" is never closed.", "Add a matching \"}\".")
		case depth == 1 && isHelper(lex):
			h, newlines, err := parseHelper(lex)
			if err != nil {
				return err
			}
			b.Helpers = append(b.Helpers, h)
			code.sb.WriteString(strings.Repeat("\n", newlines))
			template.sb.WriteString(strings.Repeat("\n", newlines))
			continue
		case isPunct(t, "{"):
			depth++
		case isPunct(t, "}"):
			if depth--; depth == 0 && !toEOF {
				nextRaw(lex)
				b.Text, b.Template = code.String(), template.String()
				return nil
			}
		}
		nextRaw(lex)
		code.add(t)
		template.add(t)
	}
}

// isHelper reports whether the next tokens start a macro or template declared in a body: "macro" or "template",
// a name and "(". That's never valid Lua, nor GD++ in a template.
func isHelper(lex *lexer.PeekingLexer) bool {
	t := peekRaw(lex)
	if t.Type != tokIdent || t.Value != "macro" && t.Value != "template" {
		return false
	}
	cp := lex.MakeCheckpoint()
	defer lex.LoadCheckpoint(cp)
	lex.Next()
	return lex.Next().Type == tokIdent && isPunct(*lex.Next(), "(")
}

// parseHelper parses a macro or template declared in a body. It returns it, and the newlines it spans.
func parseHelper(lex *lexer.PeekingLexer) (*Macro, int, error) {
	start := lex.MakeCheckpoint().RawCursor()
	kw := *lex.Next()
	m := &Macro{Pos: kw.Pos, Template: kw.Value == "template", Name: lex.Next().Value, Body: &MacroBody{}}
	lex.Next() // "("
	for !isPunct(*lex.Peek(), ")") {
		name := *lex.Next()
		if name.Type != tokIdent {
			return nil, 0, errorAt(name, fmt.Sprintf("Expected a parameter name or \")\", but found %s.", describe(name)), "")
		}
		p := &MacroParam{Pos: name.Pos, Name: name.Value}
		if isPunct(*lex.Peek(), "=") {
			lex.Next()
			p.Default = &Default{}
			if err := p.Default.Parse(lex); err != nil {
				return nil, 0, err
			}
		}
		m.Params = append(m.Params, p)
		if sep := *lex.Peek(); isPunct(sep, ",") {
			lex.Next()
		} else if !isPunct(sep, ")") {
			return nil, 0, errorAt(sep, fmt.Sprintf("Expected \",\" or \")\" in the parameter list, but found %s.", describe(sep)), "")
		}
	}
	lex.Next() // ")"
	open := *lex.Peek()
	if !isPunct(open, "{") {
		return nil, 0, errorAt(open, fmt.Sprintf("Expected \"{\" to start the body of %s %q, but found %s.", m.what(), m.Name, describe(open)), "")
	}
	lex.Next()
	if err := m.Body.parse(lex, open, false); err != nil {
		return nil, 0, err
	}
	newlines := 0
	for _, t := range lex.Range(start, lex.MakeCheckpoint().RawCursor()) {
		newlines += strings.Count(t.Value, "\n")
	}
	return m, newlines, nil
}

func (a *ArgList) Parse(lex *lexer.PeekingLexer) error {
	open := *lex.Peek()
	a.Pos = open.Pos
	switch {
	case isPunct(open, "{"):
		a.Table = &Block{}
		return a.Table.Parse(lex)
	case !isPunct(open, "("):
		return errorAt(open, fmt.Sprintf("Expected \"(\" or \"{\" after the macro's name, but found %s.", describe(open)),
			"Invoke a macro with \"invoke name(args)\", or with a Lua table: \"invoke name { ... }\".")
	}
	lex.Next()
	var err error
	a.Args, err = parseItems(lex, open, ")", true)
	for i, arg := range a.Args {
		if err == nil && arg.Key == "" && i > 0 && a.Args[i-1].Key != "" {
			err = &Error{Pos: arg.Pos, Len: 1, Msg: "Positional arguments must come before named ones.", Hint: "Name this argument too, or move it."}
		}
	}
	return err
}

// parseItems parses the arguments, list items or dict entries after open, up to closer. With keys, an item may
// have a key: a name, then "=".
func parseItems(lex *lexer.PeekingLexer, open lexer.Token, closer string, keys bool) ([]*MacroArg, error) {
	var items []*MacroArg
	for {
		t := *lex.Peek()
		switch {
		case isPunct(t, closer):
			lex.Next()
			return items, nil
		case t.EOF():
			return nil, errorAt(open, fmt.Sprintf("This %q is never closed.", open.Value), fmt.Sprintf("Add a matching %q.", closer))
		}
		item := &MacroArg{Pos: t.Pos}
		cp := lex.MakeCheckpoint()
		lex.Next()
		eq, after := *lex.Next(), *lex.Peek()
		lex.LoadCheckpoint(cp)
		if keys && t.Type == tokIdent && isPunct(eq, "=") && !isPunct(after, "=") { // Not C++'s "==".
			item.Key = t.Value
			lex.Next()
			lex.Next()
		}
		var err error
		if item.Value, err = parseValue(lex, closer); err != nil {
			return nil, err
		}
		items = append(items, item)
		if sep := *lex.Peek(); isPunct(sep, ",") {
			lex.Next()
		} else if !isPunct(sep, closer) {
			return nil, errorAt(sep, fmt.Sprintf("Expected \",\" or %q after the value, but found %s.", closer, describe(sep)), "")
		}
	}
}

// parseValue parses a value that ends at "," or closer. Anything that isn't a single literal, name, list, dict or
// code block there is a C++ expression.
func parseValue(lex *lexer.PeekingLexer, closer string) (*Value, error) {
	cp := lex.MakeCheckpoint()
	t := *lex.Next()
	v := &Value{Pos: t.Pos}
	var err error
	switch {
	case t.Type == tokIdent && (t.Value == "code" || t.Value == "glsl") && isPunct(*lex.Peek(), "{"):
		v.Kind, v.Code = t.Value, &Block{}
		err = v.Code.Parse(lex)
	case isPunct(t, "["):
		v.Kind = "list"
		v.Items, err = parseItems(lex, t, "]", false)
	case isPunct(t, "{"):
		v.Kind = "dict"
		v.Items, err = parseItems(lex, t, "}", true)
		for _, item := range v.Items {
			if err == nil && item.Key == "" {
				err = &Error{Pos: item.Pos, Len: 1, Msg: "Expected a name, then \"=\", in the dictionary.", Hint: "Dictionaries look like {name = \"Sword\", damage = 10}."}
			}
		}
	case t.Type == tokString:
		v.Kind, v.Text = "string", t.Value
	case t.Type == tokNumber:
		v.Kind, v.Text = "number", t.Value
	case isPunct(t, "-") && lex.Peek().Type == tokNumber:
		v.Kind, v.Text = "number", "-"+lex.Next().Value
	case t.Type == tokIdent:
		v.Kind, v.Text = "name", t.Value
		for isPunct(*lex.Peek(), ".") || isPunct(*lex.Peek(), "[") {
			if s := lex.Next().Value; s == "." {
				v.Text += "." + lex.Next().Value
			} else {
				v.Text += s + typeArgs(lex)
			}
		}
	}
	if end := *lex.Peek(); err == nil && v.Kind != "" && (isPunct(end, ",") || isPunct(end, closer)) {
		return v, nil
	}
	// A C++ expression, up to "," or closer outside brackets.
	lex.LoadCheckpoint(cp)
	first := *lex.Peek()
	var code codeBuilder
	for depth := 0; ; {
		t := peekRaw(lex)
		if t.EOF() || depth == 0 && (isPunct(t, ",") || isPunct(t, closer)) {
			break
		}
		nextRaw(lex)
		if t.Type == tokPunct && strings.Contains("([{", t.Value) {
			depth++
		} else if t.Type == tokPunct && closers[t.Value] != "" {
			depth--
		}
		code.add(t)
	}
	text := strings.TrimSpace(code.String())
	if text == "" {
		return nil, errorAt(first, fmt.Sprintf("Expected a value, but found %s.", describe(first)),
			"Values are numbers, strings, names, [lists], {key: value} dictionaries, code { C++ } blocks and C++ expressions.")
	}
	return &Value{Pos: first.Pos, Kind: "expr", Code: &Block{Pos: first.Pos, TextPos: first.Pos, Text: text}}, nil
}

// typeArgs reads the rest of a type's arguments after "[", e.g. "int, String]", without spaces but after commas.
func typeArgs(lex *lexer.PeekingLexer) string {
	var sb strings.Builder
	for depth := 1; depth > 0; {
		t := *lex.Next()
		switch {
		case t.EOF():
			return sb.String()
		case isPunct(t, "["):
			depth++
		case isPunct(t, "]"):
			depth--
		}
		sb.WriteString(t.Value)
		if isPunct(t, ",") {
			sb.WriteString(" ")
		}
	}
	return sb.String()
}
