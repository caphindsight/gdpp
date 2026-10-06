package syntax_1

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

// Error is a GD++ syntax error. It renders as the location, the message, the source line with a caret
// under the offending text, and an optional hint.
type Error struct {
	Pos  lexer.Position
	Len  int    // Width of the caret underline, in runes.
	Msg  string // A capitalized sentence ending with a period.
	Hint string // An optional suggestion, in the same style.
	// For errors that macros and templates cause: the invocations that led to it, innermost first. See frame.
	Stack []string
	line  string // The source line at Pos, set by withSource.
}

func (e *Error) Message() string          { return e.Msg }
func (e *Error) Position() lexer.Position { return e.Pos }

func (e *Error) Error() string {
	num := strconv.Itoa(e.Pos.Line)
	var caret strings.Builder
	for i, r := range []rune(e.line) {
		if i >= e.Pos.Column-1 {
			break
		}
		if r == '\t' {
			caret.WriteRune('\t')
		} else {
			caret.WriteRune(' ')
		}
	}
	caret.WriteString(strings.Repeat("^", e.Len))
	s := fmt.Sprintf("%s:%d:%d: %s\n %s | %s\n %s | %s",
		e.Pos.Filename, e.Pos.Line, e.Pos.Column, e.Msg, num, e.line, strings.Repeat(" ", len(num)), caret.String())
	if e.Hint != "" {
		s += "\nHint: " + e.Hint
	}
	if len(e.Stack) > 0 {
		s += "\nMacro call stack:"
		for i, frame := range e.Stack {
			if n := len(e.Stack); n > 2*stackEnds && i >= stackEnds && i < n-stackEnds {
				if i == stackEnds {
					s += fmt.Sprintf("\n  ... %d more", n-2*stackEnds)
				}
				continue
			}
			s += "\n  " + frame
		}
	}
	return s
}

// stackEnds is how many frames of a long macro call stack an error shows at each end.
const stackEnds = 5

// errorAt returns an Error that underlines token t.
func errorAt(t lexer.Token, msg, hint string) *Error {
	value, _, _ := strings.Cut(t.Value, "\n")
	return &Error{Pos: t.Pos, Len: utf8.RuneCountInString(value), Msg: msg, Hint: hint}
}

// withSource attaches the source line to e. Positions at or past the end of the content (EOF, trailing newlines)
// move to just after the last non-space character, so the caret points at something visible.
func (e *Error) withSource(src string) *Error {
	if end := len(strings.TrimRight(src, " \t\r\n")); e.Pos.Offset >= end {
		lineStart := strings.LastIndex(src[:end], "\n") + 1
		e.Pos.Offset = end
		e.Pos.Line = strings.Count(src[:end], "\n") + 1
		e.Pos.Column = utf8.RuneCountInString(src[lineStart:end]) + 1
	}
	lineStart := strings.LastIndex(src[:e.Pos.Offset], "\n") + 1
	line, _, _ := strings.Cut(src[lineStart:], "\n")
	e.line = strings.TrimRight(line, "\r")
	e.Len = max(1, min(e.Len, utf8.RuneCountInString(e.line)-e.Pos.Column+1))
	return e
}

var keywords = map[string]bool{
	"class": true, "class_name": true, "ctor": true, "decl": true, "dtor": true, "enum": true, "enum_name": true,
	"extends": true, "extern": true, "extern_name": true, "func": true, "get": true, "impl": true, "implements": true, "import": true,
	"invoke": true, "annotation": true, "macro": true, "macro_library": true, "macro_name": true, "noimport": true, "set": true, "signal": true, "template": true, "template_name": true, "trait": true, "trait_name": true, "var": true,
}

// declKeywords are the words that can start a declaration, in the order hints list them.
// Unlike the others, "on" isn't a keyword: it can be a name too.
var declKeywords = []string{"func", "var", "signal", "enum", "class", "extern", "trait", "decl", "impl", "ctor", "dtor", "on", "import", "noimport", "invoke"}

// describe names token t for humans, e.g. `keyword "func"` or `the end of the file`.
func describe(t lexer.Token) string {
	switch {
	case t.EOF():
		return "the end of the file"
	case t.Type == tokNewline:
		return "the end of the line"
	case t.Type == tokIdent && keywords[t.Value]:
		return fmt.Sprintf("keyword %q", t.Value)
	case t.Type == tokIdent:
		return fmt.Sprintf("name %q", t.Value)
	case t.Type == tokNumber:
		return "number " + t.Value
	case isOneOf(t, "String", "RawStringOpen"):
		return "a string"
	case t.Type == tokChar:
		return "a character literal"
	case isDoc(t):
		return "a doc comment"
	}
	return strconv.Quote(t.Value)
}

var (
	closers = map[string]string{")": "(", "]": "[", "}": "{"}
)

// checkBalance finds unterminated comments and raw strings, and unbalanced brackets, in the whole token stream.
// The lexer treats GD++ and C++ alike, so this covers embedded C++ too.
func checkBalance(src string, tokens []lexer.Token) *Error {
	var (
		stack        []int    // Indices of unclosed brackets.
		pairs        [][2]int // Matched bracket pairs.
		commentDepth int
		commentStart int
		rawStart     = -1
	)
	for i, t := range tokens {
		switch {
		case isOneOf(t, "CommentOpen", "DocBlockOpen", "DocBlockNest"):
			if commentDepth == 0 {
				commentStart = i
			}
			commentDepth++
		case isOneOf(t, "CommentClose", "DocBlockClose"):
			commentDepth--
		case isOneOf(t, "RawStringOpen"):
			rawStart = i
		case isOneOf(t, "RawStringClose"):
			rawStart = -1
		case t.Type != tokPunct:
		case strings.Contains("([{", t.Value):
			stack = append(stack, i)
		case closers[t.Value] != "":
			if len(stack) == 0 {
				return errorAt(t, fmt.Sprintf("This %q has no matching %q.", t.Value, closers[t.Value]),
					fmt.Sprintf("Remove it, or add the missing %q before it.", closers[t.Value]))
			}
			open := tokens[stack[len(stack)-1]]
			if open.Value != closers[t.Value] {
				return errorAt(t, fmt.Sprintf("Expected %q to close the %q on line %d, but found %q.",
					closerOf(open.Value), open.Value, open.Pos.Line, t.Value),
					fmt.Sprintf("Check for a missing %q or an extra %q.", closerOf(open.Value), t.Value))
			}
			pairs = append(pairs, [2]int{stack[len(stack)-1], i})
			stack = stack[:len(stack)-1]
		}
	}
	switch {
	case commentDepth > 0:
		t := tokens[commentStart]
		what := "comment"
		if isDoc(t) {
			what = "doc comment"
		}
		e := errorAt(t, fmt.Sprintf("This %s is never closed.", what),
			`Block comments nest in GD++, so every "/*" inside a comment needs its own "*/".`)
		e.Len = 2
		return e
	case rawStart >= 0:
		return errorAt(tokens[rawStart], "This raw string is never closed.",
			`A raw string R"delim( ... )delim" ends with ")", the same delimiter, and a quote.`)
	case len(stack) > 0:
		sig := significant(tokens)
		open := tokens[stack[len(stack)-1]]
		// A missing "}" makes a later "}" close the wrong block. Its indentation usually gives it away.
		for _, p := range pairs {
			o, c := tokens[p[0]], tokens[p[1]]
			if o.Value == "{" && p[0] > stack[0] && firstOnLine(src, c) && indent(src, o) != indent(src, c) {
				return errorAt(o, fmt.Sprintf("This \"{\"%s is probably never closed.", owner(sig, o)),
					fmt.Sprintf(`The "}" on line %d closes it, but that "}" is indented differently, so it probably belongs to an outer block. Add the missing "}".`, c.Pos.Line))
			}
		}
		return errorAt(open, fmt.Sprintf("This %q%s is never closed.", open.Value, owner(sig, open)),
			fmt.Sprintf("Add a matching %q.", closerOf(open.Value)))
	}
	return nil
}

func closerOf(open string) string {
	return map[string]string{"(": ")", "[": "]", "{": "}"}[open]
}

// indent returns the leading whitespace of t's line.
func indent(src string, t lexer.Token) string {
	line := src[strings.LastIndex(src[:t.Pos.Offset], "\n")+1:]
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

func firstOnLine(src string, t lexer.Token) bool {
	return strings.TrimSpace(src[strings.LastIndex(src[:t.Pos.Offset], "\n")+1:t.Pos.Offset]) == ""
}

// significant returns the tokens the parser sees: everything except whitespace and plain comments. It keeps EOF.
func significant(tokens []lexer.Token) []lexer.Token {
	var sig []lexer.Token
	for _, t := range tokens {
		if t.Type != tokWhitespace && t.Type != tokNewline && !isComment(t) {
			sig = append(sig, t)
		}
	}
	return sig
}

// owner describes what the "{" token open starts, e.g. ` (the body of func "foo")`, or returns "".
func owner(sig []lexer.Token, open lexer.Token) string {
	if open.Value != "{" {
		return ""
	}
	j := 0
	for sig[j].Pos.Offset != open.Pos.Offset {
		j++
	}
	prev, prev2 := at(sig, j-1), at(sig, j-2)
	switch {
	case prev.Value == "impl" && prev2.Value == "decl":
		return " (the decl impl block)"
	case prev.Type == tokIdent && (prev.Value == "decl" || prev.Value == "impl" || prev.Value == "ctor" || prev.Value == "dtor" || prev.Value == "get"):
		return fmt.Sprintf(" (the %s block)", prev.Value)
	case onHead(sig, j-1) >= 0:
		return " (the on block)"
	case prev.Type == tokIdent && (prev2.Value == "class" || prev2.Value == "extern" || prev2.Value == "trait" || prev2.Value == "enum"):
		return fmt.Sprintf(" (the body of %s %q)", prev2.Value, prev.Value)
	}
	k := headKeyword(sig, j)
	switch {
	case k < 0:
		return ""
	case sig[k].Value == "func":
		return fmt.Sprintf(" (the body of func %q)", at(sig, k+1).Value)
	case sig[k].Value == "set":
		return " (the set block)"
	case sig[k].Value == "var" && prev.Value == "=":
		return fmt.Sprintf(" (the initial value of var %q)", at(sig, k+1).Value)
	case sig[k].Value == "var":
		return fmt.Sprintf(" (the body of property %q)", at(sig, k+1).Value)
	}
	return ""
}

// at returns sig[i], or a zero token if i is out of range.
func at(sig []lexer.Token, i int) lexer.Token {
	if i < 0 || i >= len(sig) {
		return lexer.Token{}
	}
	return sig[i]
}

// headKeyword scans back from sig[j-1] over tokens that can appear in a declaration head,
// e.g. `func foo(a: int) -> int`, and returns the index of the keyword starting it, or -1.
func headKeyword(sig []lexer.Token, j int) int {
	for i := j - 1; i >= 0; i-- {
		t := sig[i]
		switch {
		case t.Type == tokIdent && keywords[t.Value] && t.Value != "extends" && t.Value != "implements" && t.Value != "get":
			return i
		case t.Type == tokIdent || t.Type == tokNumber:
		case t.Type == tokPunct && strings.Contains("()[],:->=", t.Value):
		default:
			return -1
		}
	}
	return -1
}

// enclosing returns the index of the unclosed bracket open that encloses sig[j], or -1.
func enclosing(sig []lexer.Token, j int, open string) int {
	depth := 0
	for i := j - 1; i >= 0; i-- {
		switch t := sig[i]; {
		case t.Type != tokPunct:
		case closers[t.Value] != "":
			depth++
		case strings.Contains("([{", t.Value) && depth > 0:
			depth--
		case strings.Contains("([{", t.Value):
			if t.Value == open {
				return i
			}
			return -1
		}
	}
	return -1
}

// endsAnnotation reports whether sig[i] is the last token of an annotation.
func endsAnnotation(sig []lexer.Token, i int) bool {
	if isPunct(at(sig, i), ")") {
		depth := 0
		for ; i >= 0; i-- {
			if isPunct(sig[i], ")") {
				depth++
			} else if isPunct(sig[i], "(") {
				if depth--; depth == 0 {
					break
				}
			}
		}
		i--
	}
	return at(sig, i).Type == tokIdent && isPunct(at(sig, i-1), "@")
}

// names says what each keyword expects right after it.
var names = map[string]string{
	"@": "an annotation name", "class": "a class name", "class_name": "a class name", "enum": "an enum name",
	"enum_name": "an enum name", "extends": "a base class name", "extern": "an extern name",
	"extern_name": "an extern name", "func": "a function name", "import": "a type name", "invoke": "a macro or template name", "macro": "a macro name",
	"macro_name": "a macro name", "noimport": "a type name", "signal": "a signal name", "template": "a template name",
	"template_name": "a template name", "implements": "a trait name", "trait": "a trait name", "trait_name": "a trait name",
	"var": "a variable name",
}

// explain turns a participle error into an *Error with a human-readable message.
func explain(err error, tokens []lexer.Token) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	var perr participle.Error
	if !errors.As(err, &perr) {
		return &Error{Msg: err.Error()}
	}
	sig := significant(tokens)
	j := 0
	for j < len(sig)-1 && sig[j].Pos.Offset < perr.Position().Offset {
		j++
	}
	j, msg, hint := diagnose(sig, j)
	return errorAt(sig[j], msg, hint)
}

// diagnose explains why the parser could not accept sig[j], judging from the tokens before it.
// It returns the index of the token to blame, which is j or, rarely, the token before it.
func diagnose(sig []lexer.Token, j int) (int, string, string) {
	if isDoc(at(sig, j-1)) && !isDoc(sig[j]) && sig[j].Type != tokIdent && !isPunct(sig[j], "@") {
		d := j - 1
		for d > 0 && isDoc(sig[d-1]) && !isOneOf(sig[d], "DocBlockOpen") {
			d--
		}
		return d, "This doc comment is not followed by a declaration.",
			"Doc comments document the declaration right after them. Use \"//\" for a regular comment."
	}
	msg, hint := diagnoseAt(sig, j)
	if p, p2 := at(sig, j-1), at(sig, j-2); names[p2.Value] != "" && (p2.Type == tokIdent || isPunct(p2, "@")) &&
		p.Type == tokIdent && slices.Contains(append(declKeywords, "class_name", "enum_name", "extern_name", "macro_library", "macro_name", "template_name", "trait_name"), p.Value) {
		// The keyword was taken as a name, e.g. "class_name" followed by "var" on the next line.
		return j - 1, fmt.Sprintf("Expected %s after %q, but found %s.", names[p2.Value], p2.Value, describe(p)),
			fmt.Sprintf("%q is a keyword, so it can't be used as a name.", p.Value)
	}
	return j, msg, hint
}

func diagnoseAt(sig []lexer.Token, j int) (msg, hint string) {
	u, p, p2 := sig[j], at(sig, j-1), at(sig, j-2)
	found := describe(u)
	k := headKeyword(sig, j)
	kw := ""
	if k >= 0 {
		kw = sig[k].Value
	}
	isName := u.Type == tokIdent
	switch {
	case (isDoc(p) || endsAnnotation(sig, j-1)) && isName && slices.Contains([]string{"decl", "impl", "ctor", "dtor", "on", "import", "noimport"}, u.Value):
		what := "an annotation"
		if isDoc(p) || u.Value == "decl" || u.Value == "impl" { // These take annotations, so a doc comment is before them.
			what = "a doc comment"
		}
		return fmt.Sprintf("Expected a declaration that can take %s, but found %s.", what, found),
			"Doc comments and annotations belong to func, var, signal, enum, class and extern declarations, and to enum values. " +
				"Only @global belongs to decl and impl blocks."

	case isName && u.Value == "implements" && (kw == "extern_name" || at(sig, enclosing(sig, j, "{")-2).Value == "extern"):
		return "Externs can't implement traits.", "Only classes implement traits. Remove \"implements\" from the extern."

	case isDoc(u):
		return "This doc comment is not followed by a declaration.",
			"Doc comments document the declaration right after them. Use \"//\" for a regular comment."

	case (p.Type == tokIdent && p.Value == "extends" || isPunct(p, ".") && at(sig, j-3).Value == "extends") && !isName && inEnumValues(sig, j):
		return fmt.Sprintf("Expected a base enum name after %q, but found %s.", p.Value, found),
			"E.g. \"extends Suit\" for a GD++ enum, or \"extends Node.ProcessMode\" for an engine enum."

	case isPunct(u, ".") && p.Type == tokIdent && (isPunct(p2, ":") || isPunct(p2, "->")):
		name := p.Value + "." + at(sig, j+1).Value
		return fmt.Sprintf("Types can't contain \".\", but found %s.", name),
			fmt.Sprintf("For an engine enum, declare a GD++ enum that extends it, and use that: \"enum %s { extends %s }\".", at(sig, j+1).Value, name)

	case slices.ContainsFunc(sig[:j], func(t lexer.Token) bool { return t.Type == tokIdent && t.Value == "enum_name" }):
		return fmt.Sprintf("Expected an enum value name, but found %s.", found),
			"Everything after \"enum_name Name\" is a value of that enum, e.g. \"A\", \"B = 2\" or \"C,\"."

	case (u.Value == "class" || u.Value == "extern" || u.Value == "trait") && isName && enclosing(sig, j, "{") >= 0:
		return fmt.Sprintf("Classes, externs and traits can't be nested, but found %s inside%s.", found, strings.TrimSuffix(strings.Replace(owner(sig, sig[enclosing(sig, j, "{")]), " (", " ", 1), ")")),
			"Move it out to the top level of the file. Inline classes, externs and traits are listed next to each other."

	case isPunct(p, ",") && !isName && slices.ContainsFunc(sig[max(k, 0):j], func(t lexer.Token) bool { return t.Type == tokIdent && t.Value == "implements" }):
		return fmt.Sprintf("Expected a trait name after \",\", but found %s.", found), "Write \"implements A, B\", without a \",\" at the end."

	case names[p.Value] != "" && (p.Type == tokIdent || isPunct(p, "@")) && !isName:
		if p.Value == "func" && isPunct(u, "(") {
			hint = "Every function needs a name, e.g. \"func my_function()\"."
		}
		return fmt.Sprintf("Expected %s after %q, but found %s.", names[p.Value], p.Value, found), hint

	case p.Type == tokIdent && (p.Value == "decl" || p.Value == "impl" || p.Value == "ctor" || p.Value == "dtor" || p.Value == "get") && !isPunct(u, "{"):
		if (p.Value == "ctor" || p.Value == "dtor") && isPunct(u, "(") {
			hint = fmt.Sprintf("Constructors and destructors take no arguments: \"%s { ... }\".", p.Value)
		}
		return fmt.Sprintf("Expected \"{\" to start the %s block, but found %s.", p.Value, found), hint

	case onHead(sig, j-1) == j-1 && !isName && !isPunct(u, "("):
		return fmt.Sprintf("Expected a notification name, \"(\" or \"{\" after \"on\", but found %s.", found),
			"E.g. \"on ready { ... }\", or \"on(what: int) { ... }\" for every notification."

	case p.Type == tokIdent && p.Value == "set" && !isPunct(u, "("):
		return fmt.Sprintf("Expected \"(\" after \"set\", but found %s.", found),
			"Name the setter's parameter: \"set(value) { ... }\"."

	case onHead(sig, j-1) >= 0:
		name := onName(sig, onHead(sig, j-1))
		return fmt.Sprintf("Expected \"{\" to start the on block, but found %s.", found), fmt.Sprintf("Write \"%s\".", onExample(name))

	case enclosing(sig, j, "(") >= 0 && onHead(sig, enclosing(sig, j, "(")-1) >= 0:
		name := onName(sig, onHead(sig, enclosing(sig, j, "(")-1))
		return fmt.Sprintf("Expected the parameter's name, then \":\" and its type, but found %s.", found), fmt.Sprintf("Write \"%s\".", onExample(name))

	case (isPunct(p, ":") || isPunct(p, "->") || isPunct(p, "[") || isPunct(p, ",") && enclosing(sig, j, "[") >= 0) && !isName:
		if isPunct(p, ":") && isPunct(u, "=") {
			hint = "GD++ does not infer types, so \":=\" is not supported. Write the type, as in \"var x: int = 5\", or leave out the \":\" to get a Variant."
		}
		return fmt.Sprintf("Expected a type after %q, but found %s.", p.Value, found), hint

	case (kw == "func" || kw == "signal" || kw == "set") && enclosing(sig, j, "(") > k:
		if isPunct(p, "(") || isPunct(p, ",") {
			return fmt.Sprintf("Expected a parameter name or \")\", but found %s.", found), ""
		}
		if isName && p.Type == tokIdent {
			hint = fmt.Sprintf("Parameter types are written after a colon: \"%s: %s\".", p.Value, u.Value)
		}
		return fmt.Sprintf("Expected \",\" or \")\" in the parameter list, but found %s.", found), hint

	case kw == "func" && isPunct(u, ":"):
		return "Expected \"{\" to start the function body, but found \":\".",
			"GD++ uses braces for function bodies, not a colon as in GDScript: \"func foo() -> void { ... }\"."

	case kw == "func" && isPunct(p, ")") && isName && !keywords[u.Value]:
		return fmt.Sprintf("Expected \"->\" or \"{\" after the parameter list, but found %s.", found),
			fmt.Sprintf("Return types are written after an arrow: \"-> %s\".", u.Value)

	case kw == "var" && k == j-2 && isName && !keywords[u.Value]:
		return fmt.Sprintf("Expected \":\", \"=\" or \"{\" after the variable name, but found %s.", found),
			fmt.Sprintf("Types are written after a colon: \"var %s: %s\".", p.Value, u.Value)

	case kw == "enum" && k == j-2 && !isPunct(u, "=") && !isPunct(u, "{"):
		return fmt.Sprintf("Expected \"=\" or \"{\" after the enum name, but found %s.", found),
			"Write \"enum NAME = 42\" for a constant, or \"enum Name { A B C }\" for an enum type."

	case isPunct(p, "=") && kw == "enum" && u.Type != tokNumber && !isPunct(u, "-"):
		return fmt.Sprintf("Expected an integer value after \"=\", but found %s.", found),
			"Constants are integers, such as 42, -1 or 0x10."

	case (p2.Value == "class" || p2.Value == "extern" || p2.Value == "trait") && p2.Type == tokIdent && p.Type == tokIdent && !isPunct(u, "{"):
		if u.Value == "extends" {
			hint = fmt.Sprintf("In an inline %s, \"extends\" goes inside the braces: \"%s %s { extends ... }\".", p2.Value, p2.Value, p.Value)
		}
		return fmt.Sprintf("Expected \"{\" after the %s name, but found %s.", p2.Value, found), hint

	case (p2.Value == "class_name" || p2.Value == "extern_name" || p2.Value == "trait_name" || p2.Value == "extends" || p2.Value == "implements") &&
		isPunct(u, "{"):
		head, kind := "class_name", "class"
		for _, t := range sig[:j] {
			if t.Type == tokIdent && (t.Value == "extern_name" || t.Value == "trait_name") {
				head, kind = t.Value, strings.TrimSuffix(t.Value, "_name")
			}
		}
		return "Unexpected \"{\".",
			fmt.Sprintf("The file-level %s declared with \"%s\" has no braces: its body is the rest of the file. Use \"%s Name { ... }\" for an inline %s.", kind, head, kind, kind)

	case inEnum(sig, j):
		return fmt.Sprintf("Expected an enum value name or \"}\", but found %s.", found), ""

	case inProperty(sig, j):
		hint = "A property body looks like: \"var x: int { get { ... } set(value) { ... } }\"."
		if s := suggest(u.Value, "get", "set", "decl"); isName && s != "" {
			hint = fmt.Sprintf("Did you mean %q?", s)
		}
		return fmt.Sprintf("Expected \"get\", \"set\", \"decl\" or \"}\" in the property body, but found %s.", found), hint

	case enclosing(sig, j, "(") >= 0 && isPunct(at(sig, enclosing(sig, j, "(")-2), "@"):
		if isPunct(p, "(") || isPunct(p, ",") {
			return fmt.Sprintf("Expected an annotation argument (a number, a string or a name), but found %s.", found), ""
		}
		return fmt.Sprintf("Expected \",\" or \")\" after the annotation argument, but found %s.", found), ""

	case endsAnnotation(sig, j-1):
		return fmt.Sprintf("Expected a declaration after the annotation, but found %s.", found),
			"Annotations apply to the declaration right after them."
	}
	return fmt.Sprintf("Expected a declaration, but found %s.", found), declarationHint(u)
}

// onHead returns the index of the "on" that starts the head of an on block ending at sig[i], e.g.
// "on process(delta: float)", "on ready" or "on", or -1 if there's none.
func onHead(sig []lexer.Token, i int) int {
	switch {
	case isPunct(at(sig, i), ")") && isPunct(at(sig, i-1), "("):
		i -= 2
	case isPunct(at(sig, i), ")") && isPunct(at(sig, i-2), "(") && at(sig, i-1).Type == tokIdent:
		i -= 3
	case isPunct(at(sig, i), ")") && isPunct(at(sig, i-4), "(") && isPunct(at(sig, i-2), ":"):
		i -= 5
	}
	if at(sig, i).Type == tokIdent && at(sig, i).Value != "on" && at(sig, i-1).Value == "on" {
		i--
	}
	t, prev := at(sig, i), at(sig, i-1)
	if t.Type != tokIdent || t.Value != "on" || prev.Type == tokIdent && keywords[prev.Value] ||
		prev.Type == tokPunct && strings.Contains("(,:->=.[@", prev.Value) {
		return -1
	}
	return i
}

// onName returns the notification name of the on block whose "on" is sig[i], or "" for the nameless one.
func onName(sig []lexer.Token, i int) string {
	if t := at(sig, i+1); t.Type == tokIdent {
		return t.Value
	}
	return ""
}

// inProperty reports whether sig[j] is inside the braces of a property.
func inProperty(sig []lexer.Token, j int) bool {
	o := enclosing(sig, j, "{")
	k := headKeyword(sig, o)
	return o >= 0 && k >= 0 && sig[k].Value == "var" && !isPunct(at(sig, o-1), "=")
}

// inEnum reports whether sig[j] is inside the braces of an enum.
func inEnum(sig []lexer.Token, j int) bool {
	o := enclosing(sig, j, "{")
	return o >= 2 && at(sig, o-2).Value == "enum" && at(sig, o-1).Type == tokIdent
}

// inEnumValues reports whether sig[j] is among the values of an enum: in its braces, or after enum_name.
func inEnumValues(sig []lexer.Token, j int) bool {
	return inEnum(sig, j) || slices.ContainsFunc(sig[:j], func(t lexer.Token) bool { return t.Type == tokIdent && t.Value == "enum_name" })
}

// declarationHint suggests a fix for token u found where a declaration should start.
func declarationHint(u lexer.Token) string {
	switch u.Value {
	case "class_name", "enum_name", "extern_name", "macro_library", "macro_name", "template_name", "trait_name":
		return fmt.Sprintf("A file has at most one %q, at the very top. Only decl and impl blocks may come before it.", u.Value)
	case "extends":
		return "\"extends\" goes right after \"class_name Name\", or right after the \"{\" of an inline class."
	case "implements":
		return "\"implements\" goes right after \"extends Base\", or in its place, before the class's members."
	case "get", "set":
		return fmt.Sprintf("%q is only allowed in a property body: \"var x: int { get { ... } set(value) { ... } }\".", u.Value)
	case "const":
		return "GD++ has no const. Define an integer constant with \"enum NAME = 42\"."
	case "static":
		return "Use the @static annotation: \"@static func foo() -> void { ... }\"."
	case "onready", "export", "tool", "virtual", "override":
		return fmt.Sprintf("Annotations start with \"@\": \"@%s\".", u.Value)
	case "pass":
		return "An empty function body is written \"{}\"."
	}
	if u.Type == tokIdent {
		if s := suggest(u.Value, slices.Concat(declKeywords, []string{"extends", "implements", "class_name", "enum_name", "extern_name", "trait_name", "macro",
			"template", "macro_library", "macro_name", "template_name"})...); s != "" {
			return fmt.Sprintf("Did you mean %q?", s)
		}
	}
	if u.EOF() {
		return ""
	}
	return "A declaration starts with one of: " + strings.Join(declKeywords, ", ") + "."
}

// suggest returns the candidate closest to word, if it is close enough to be a likely typo.
func suggest(word string, candidates ...string) string {
	limit := 1
	if len(word) > 4 {
		limit = 2
	}
	best := ""
	for _, c := range candidates {
		if d := editDistance(word, c); d <= limit && d > 0 {
			best, limit = c, d-1
		}
	}
	return best
}

// editDistance is the optimal string alignment distance: edits and adjacent transpositions each cost 1.
func editDistance(a, b string) int {
	d := make([][]int, len(a)+1)
	for i := range d {
		d[i] = make([]int, len(b)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(a)][len(b)]
}
