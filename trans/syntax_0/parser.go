package syntax_0

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
	tokens, err := parser.Lex(filename, strings.NewReader(src))
	if err != nil {
		return nil, err
	}
	if e := checkBalance(src, tokens); e != nil {
		return nil, e.withSource(src)
	}
	tree, err := parser.ParseString(filename, src)
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
	})
	if e != nil {
		return nil, e.withSource(src)
	}
	file, e := tree.toFile()
	if e != nil {
		return nil, e.withSource(src)
	}
	return file, nil
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
	if !annotations.IsValid() || doc.IsNil() && annotations.Len() == 0 {
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
}

func (c *codeBuilder) add(t lexer.Token) {
	if isComment(t) || isDoc(t) {
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
		if t.EOF() || depth == 0 && (t.Type == tokNewline || isPunct(t, ",") || t.Type == tokPunct && closers[t.Value] != "") {
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
