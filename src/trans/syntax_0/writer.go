package syntax_0

import (
	"fmt"
	"slices"
	"strings"

	"github.com/alecthomas/participle/v2/lexer"
)

// writer builds a generated C++ file, counting lines so it can emit #line directives around user code.
type writer struct {
	sb     strings.Builder
	lines  int    // Lines written so far.
	self   string // How #line names the generated file.
	source string // How #line names the GD++ file.
}

// ln writes a line, formatted like fmt.Sprintf.
func (w *writer) ln(format string, args ...any) {
	s := fmt.Sprintf(format, args...)
	w.sb.WriteString(s + "\n")
	w.lines += strings.Count(s, "\n") + 1
}

// user writes prefix+code+suffix as one line, where code is user C++ starting at pos in the GD++ file, and assertions
// in it become the macro assert. #line directives around it make compilers report user code at its GD++ location.
// From a template, code comes from its origin instead.
func (w *writer) user(pos lexer.Position, origin Origin, prefix, code, suffix, assert string) {
	line, source := pos.Line, w.source
	if origin.Source != "" {
		line, source = origin.Line, origin.Source
	}
	w.ln("#line %d %q", line, source)
	w.ln("%s", prefix+cpp(code, assert)+suffix)
	w.ln("#line %d %q", w.lines+2, w.self)
}

// block writes the text of b, a C++ block, between prefix and suffix, like user. A template's code keeps its
// lines in the template. Other generated code all comes from its invocation, so each of its lines gets a #line
// naming the invocation's line.
func (w *writer) block(b *Block, prefix, suffix, assert string) {
	if !b.Generated || b.Origin.Source != "" {
		w.user(b.TextPos, b.Origin, prefix, b.Text, suffix, assert)
		return
	}
	at := fmt.Sprintf("#line %d %q", b.TextPos.Line, w.source)
	w.ln("%s", at)
	w.ln("%s", strings.ReplaceAll(prefix+cpp(b.Text, assert)+suffix, "\n", "\n"+at+"\n"))
	w.ln("#line %d %q", w.lines+2, w.self)
}

func (w *writer) String() string {
	return w.sb.String()
}

// The runtime's macros that assertions become: assertVoid, assertValue and assertReference return from a function whose
// return type is void, a value or a reference, and assertAny from any function, finding out which by its signature.
// assertDeduced fails to compile, with an explanation, since no return compiles where it's used.
const (
	assertVoid      = "GDPP_ASSERT_VOID"
	assertValue     = "GDPP_ASSERT_VALUE"
	assertReference = "GDPP_ASSERT_REFERENCE"
	assertAny       = "GDPP_ASSERT"
	assertDeduced   = "GDPP_ASSERT_DEDUCED"
)

// assertFor returns the assert macro for code in a function that returns void or not.
func assertFor(void bool) string {
	if void {
		return assertVoid
	}
	return assertValue
}

// cpp turns user C++ into plain C++:
//   - `emit f(x);` becomes `(void) f(x);`, which uses the [[nodiscard]] result,
//   - `rpc x->f(a)` and `rpc(peer) x->f(a)` become `x->_gdpp_rpc_f(0, a)` and `x->_gdpp_rpc_f(peer, a)`,
//   - `claim x`, `is_done x` and `cancel x` become `x.claim()`, `x.is_done()` and `x.cancel()`, for an Async x,
//   - `create T`, `destroy x` and `queue_destroy x` become `gdpp::create<T>()`, `gdpp::destroy(x)` and
//     `gdpp::queue_destroy(x)`,
//   - `is_cancelled`, a bare word, becomes `gdpp::is_cancelled()`,
//   - `string_name "x"` becomes `GDPP_STRING_NAME("x")`,
//   - `x as T` becomes `gdpp::cast<T>(x)`,
//   - `assert x;` becomes `GDPP_ASSERT("x", x);`, where assert names the macro, e.g. GDPP_ASSERT_VOID in a void function,
//     and a lambda's own return type picks it in the lambda (see assertMacros), while the fallbacks `assert_void x;`
//     and `assert_val x;` always become GDPP_ASSERT_VOID and GDPP_ASSERT_VALUE,
//   - a `,` before `)` is dropped, so calls may end with a trailing comma.
func cpp(code, assert string) string {
	lex, err := gdppLexer.LexString("", code)
	if err != nil {
		return code
	}
	var ts []lexer.Token
	for {
		t, err := lex.Next()
		if err != nil || t.EOF() {
			break
		}
		ts = append(ts, t)
	}
	var out []string
	for _, t := range ts {
		out = append(out, t.Value)
	}
	macros := assertMacros(ts, assert)
	for i := 0; i < len(ts); i++ {
		if isPunct(ts[i], ",") {
			if j := skipSpace(ts, i+1); j < len(ts) && isPunct(ts[j], ")") {
				out[i] = ""
			}
		}
		if ts[i].Type == tokIdent && ts[i].Value == "emit" {
			out[i] = "(void)"
		}
		if ts[i].Type == tokIdent && ts[i].Value == "is_cancelled" && !isMember(ts, i) {
			if next := skipSpace(ts, i+1); next == len(ts) || !isPunct(ts[next], "(") {
				out[i] = "gdpp::is_cancelled()"
			}
		}
		if ts[i].Type == tokIdent && ts[i].Value == "string_name" && !isMember(ts, i) {
			if j := skipSpace(ts, i+1); j < len(ts) && ts[j].Type == tokString && ts[j].Value[0] == '"' {
				// Drop the spaces after the keyword, keeping their newlines so the lines still match.
				for k := i + 1; k < j; k++ {
					out[k] = strings.Repeat("\n", strings.Count(ts[k].Value, "\n"))
				}
				out[i], out[j] = "GDPP_STRING_NAME(", out[j]+")"
				i = j
				continue
			}
		}
		if cond, semi, ok := assertTarget(ts, i); ok {
			// Drop the spaces after the keyword, keeping their newlines so the lines still match.
			for k := i + 1; k < cond; k++ {
				out[k] = strings.Repeat("\n", strings.Count(ts[k].Value, "\n"))
			}
			// The text drops brackets around all of it, as in `assert(x);`.
			first, last := cond, prevToken(ts, semi)
			if isPunct(ts[first], "(") && closing(ts, first) == last {
				first, last = first+1, last-1
			}
			var sb strings.Builder
			for _, t := range ts[first : last+1] {
				if !isComment(t) {
					sb.WriteString(t.Value)
				}
			}
			text := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(strings.Join(strings.Fields(sb.String()), " "))
			macro := macros[i]
			if m := assertKeywords[ts[i].Value]; m != "" {
				macro = m
			}
			out[i], out[semi] = macro+`("`+text+`", `, ");"
			i = cond - 1 // The condition may hold more rewrites.
			continue
		}
		if ts[i].Type == tokIdent && ts[i].Value == "create" && !isMember(ts, i) {
			if j := skipSpace(ts, i+1); j < len(ts) && ts[j].Type == tokIdent {
				end := j + 1
				for end+2 < len(ts) && isPunct(ts[end], ":") && isPunct(ts[end+1], ":") && ts[end+2].Type == tokIdent {
					end += 3
				}
				// Drop the spaces and the type, keeping their newlines so the lines still match.
				var sb strings.Builder
				for k := i + 1; k < end; k++ {
					if k >= j {
						sb.WriteString(ts[k].Value)
					}
					out[k] = strings.Repeat("\n", strings.Count(ts[k].Value, "\n"))
				}
				out[i] = "gdpp::create<" + sb.String() + ">()"
				i = end - 1
				continue
			}
		}
		if ts[i].Type == tokIdent && (asyncWords[ts[i].Value] || destroyWords[ts[i].Value] && !isMember(ts, i)) {
			j := skipSpace(ts, i+1)
			if end := postfixEnd(ts, j); end > j {
				// Drop the keyword and the spaces after it, but keep a line break and the indentation after it.
				for k := i; k < j && ts[k].Type != tokNewline; k++ {
					out[k] = ""
				}
				if destroyWords[ts[i].Value] {
					out[i], out[end-1] = "gdpp::"+ts[i].Value+"(", out[end-1]+")"
				} else {
					out[end-1] += "." + ts[i].Value + "()"
				}
				i = j - 1 // The operand may hold more rewrites, e.g. in a call's arguments.
				continue
			}
		}
		if start, typeEnd, ok := asCast(ts, i); ok {
			// Drop `as`, the type and the spaces around them, keeping their newlines so the lines still match.
			var sb strings.Builder
			end := prevToken(ts, i)
			for k := end + 1; k < typeEnd; k++ {
				if k > i {
					sb.WriteString(ts[k].Value)
				}
				out[k] = strings.Repeat("\n", strings.Count(ts[k].Value, "\n"))
			}
			out[start] = "gdpp::cast<" + strings.Join(strings.Fields(sb.String()), " ") + ">(" + out[start]
			out[end] += ")"
			i = typeEnd - 1
			continue
		}
		peer, chain, name, paren, ok := rpcTarget(ts, i)
		if !ok {
			continue
		}
		// Drop the keyword and the peer, keeping their newlines so the lines still match.
		for k := i; k < chain; k++ {
			out[k] = strings.Repeat("\n", strings.Count(ts[k].Value, "\n"))
		}
		p := "0"
		if peer != nil {
			var sb strings.Builder
			for _, t := range peer {
				sb.WriteString(t.Value)
			}
			p = strings.TrimSpace(strings.ReplaceAll(sb.String(), "\n", " "))
		}
		if next := skipSpace(ts, paren+1); next == len(ts) || !isPunct(ts[next], ")") {
			p += ", "
		}
		out[name] = "_gdpp_rpc_" + ts[name].Value
		out[paren] = "(" + p
		i = paren
	}
	return strings.Join(out, "")
}

// destroyWords are the words that call the runtime's function of the same name: `destroy x` is `gdpp::destroy(x)`.
var destroyWords = map[string]bool{"destroy": true, "queue_destroy": true}

// asyncWords are the words that call the method of the same name on an Async: `claim x` is `x.claim()`.
var asyncWords = map[string]bool{"claim": true, "is_done": true, "cancel": true}

// postfixEnd returns the end of the expression at ts[j] that an Async word applies to: a name, followed by
// member accesses, scopes, calls and subscripts, e.g. `tasks[i]`, `this->pending` or `find(a).task`. It returns j if
// there's none.
func postfixEnd(ts []lexer.Token, j int) int {
	if j >= len(ts) || ts[j].Type != tokIdent {
		return j
	}
	end := j + 1
	for {
		k := skipSpace(ts, end)
		switch {
		case k < len(ts) && (isPunct(ts[k], "(") || isPunct(ts[k], "[")):
			close := closing(ts, k)
			if close == len(ts) {
				return end
			}
			end = close + 1
		case k+1 < len(ts) && (isPunct(ts[k], "->") || isPunct(ts[k], ".") || isPunct(ts[k], ":") && isPunct(ts[k+1], ":")):
			if isPunct(ts[k], ":") {
				k++
			}
			m := skipSpace(ts, k+1)
			if m == len(ts) || ts[m].Type != tokIdent {
				return end
			}
			end = m + 1
		default:
			return end
		}
	}
}

// asCast matches `x as T` at ts[i], the `as`. It returns the index where x starts and the end of T. It only matches
// where both are clear, since a wrong guess could compile: x is a name or a group in brackets, followed by member
// accesses, scopes, calls and subscripts, with nothing before it that could continue it, e.g. `)` in `(int) x as T`;
// T is a name, followed by scopes, template arguments, `*` and `const`, with no operand after it, e.g. `y` in
// `x as int * y`.
func asCast(ts []lexer.Token, i int) (start, typeEnd int, ok bool) {
	if ts[i].Type != tokIdent || ts[i].Value != "as" || i == 0 || isMember(ts, i) {
		return
	}
	// Walk x backward.
	start = len(ts)
	for j := prevToken(ts, i); j >= 0; {
		switch {
		case ts[j].Type == tokIdent && !exprWords[ts[j].Value]:
			start = j
		case isPunct(ts[j], ")") || isPunct(ts[j], "]"):
			if start = opening(ts, j); start < 0 {
				return
			}
		default:
			return
		}
		p := prevToken(ts, start)
		switch {
		case p >= 0 && (isPunct(ts[p], ".") || isPunct(ts[p], "->") || isPunct(ts[p], ":") && p > 0 && isPunct(ts[p-1], ":")):
			if isPunct(ts[p], ":") {
				p--
			}
			j = prevToken(ts, p)
		case p >= 0 && (isPunct(ts[start], "(") || isPunct(ts[start], "[")) && (ts[p].Type == tokIdent && !exprWords[ts[p].Value] ||
			isPunct(ts[p], ")") || isPunct(ts[p], "]")):
			j = p
		case p >= 0 && (isOneOf(ts[p], "Ident", "Number", "String", "Char") && !exprWords[ts[p].Value] ||
			isPunct(ts[p], ")") || isPunct(ts[p], "]") || isPunct(ts[p], ">") && isPunct(ts[start], "(")):
			return
		default:
			j = -1
		}
	}
	if start == len(ts) {
		return
	}
	// Walk T forward.
	j := skipSpace(ts, i+1)
	if j < len(ts) && ts[j].Type == tokIdent && ts[j].Value == "const" {
		j = skipSpace(ts, j+1)
	}
	if j == len(ts) || ts[j].Type != tokIdent {
		return
	}
	typeEnd = j + 1
	for {
		k := skipSpace(ts, typeEnd)
		switch {
		case k+2 < len(ts) && isPunct(ts[k], ":") && isPunct(ts[k+1], ":") && ts[skipSpace(ts, k+2)].Type == tokIdent:
			typeEnd = skipSpace(ts, k+2) + 1
		case k < len(ts) && isPunct(ts[k], "<") && templateEnd(ts, k) > k:
			typeEnd = templateEnd(ts, k)
		case k < len(ts) && (isPunct(ts[k], "*") || ts[k].Type == tokIdent && ts[k].Value == "const"):
			typeEnd = k + 1
		default:
			if k < len(ts) && (isOneOf(ts[k], "Ident", "Number", "String", "Char") || isPunct(ts[k], "(")) {
				return
			}
			return start, typeEnd, true
		}
	}
}

// exprWords are the C++ words that an expression may follow, which `as` doesn't apply to.
var exprWords = map[string]bool{"return": true, "if": true, "while": true, "for": true, "switch": true, "catch": true,
	"throw": true, "case": true, "else": true, "do": true, "co_return": true, "co_yield": true, "co_await": true,
	"new": true, "delete": true, "and": true, "or": true, "not": true, "assert": true, "assert_void": true, "assert_val": true}

// templateEnd returns the end of the template arguments that start with the '<' at ts[open], or open if they aren't
// template arguments, e.g. in `n < limit && m > 0`.
func templateEnd(ts []lexer.Token, open int) int {
	depth := 0
	for j := open; j < len(ts); j++ {
		switch {
		case isPunct(ts[j], "<"):
			depth++
		case isPunct(ts[j], ">"):
			if depth--; depth == 0 {
				return j + 1
			}
		case !isOneOf(ts[j], "Ident", "Number", "Whitespace", "Newline") && !isPunct(ts[j], ":") && !isPunct(ts[j], ",") &&
			!isPunct(ts[j], "*"):
			return open
		}
	}
	return open
}

// opening returns the index of the bracket that opens the one at ts[close], or -1 if there's none.
func opening(ts []lexer.Token, close int) int {
	depth := 0
	for j := close; j >= 0; j-- {
		if isPunct(ts[j], ")") || isPunct(ts[j], "]") {
			depth++
		} else if isPunct(ts[j], "(") || isPunct(ts[j], "[") {
			if depth--; depth == 0 {
				return j
			}
		}
	}
	return -1
}

// prevToken returns the index of the last token before i that isn't whitespace, or -1 if there's none.
func prevToken(ts []lexer.Token, i int) int {
	for i--; i >= 0 && (ts[i].Type == tokWhitespace || ts[i].Type == tokNewline); i-- {
	}
	return i
}

// closing returns the index of the bracket that closes the one at ts[open], or len(ts) if there's none.
func closing(ts []lexer.Token, open int) int {
	depth := 0
	for j := open; j < len(ts); j++ {
		if isPunct(ts[j], "(") || isPunct(ts[j], "[") {
			depth++
		} else if isPunct(ts[j], ")") || isPunct(ts[j], "]") {
			if depth--; depth == 0 {
				return j
			}
		}
	}
	return len(ts)
}

// assertTarget matches an assertion at ts[i], `assert cond;`, where assert starts a statement and cond starts like an
// expression, so `s.assert(x);` and `assert = 1;` aren't assertions. It returns the index where cond starts and
// the index of the ';'.
func assertTarget(ts []lexer.Token, i int) (cond, semi int, ok bool) {
	if _, found := assertKeywords[ts[i].Value]; ts[i].Type != tokIdent || !found {
		return
	}
	p := prevToken(ts, i)
	if p >= 0 && !isPunct(ts[p], ";") && !isPunct(ts[p], "{") && !isPunct(ts[p], "}") && !isPunct(ts[p], ")") &&
		!(isPunct(ts[p], ":") && (p == 0 || !isPunct(ts[p-1], ":"))) && !(ts[p].Type == tokIdent && ts[p].Value == "else") {
		return
	}
	cond = skipSpace(ts, i+1)
	if cond == len(ts) || !isOneOf(ts[cond], "Ident", "Number", "String", "Char") && !isPunct(ts[cond], "(") &&
		!((isPunct(ts[cond], "!") || isPunct(ts[cond], "*") || isPunct(ts[cond], "&")) && cond+1 < len(ts) && !isPunct(ts[cond+1], "=")) {
		return
	}
	// The condition ends at the first ';' outside of brackets, e.g. not one in a lambda.
	for j, depth := cond, 0; j < len(ts) && depth >= 0; j++ {
		switch {
		case isPunct(ts[j], "(") || isPunct(ts[j], "[") || isPunct(ts[j], "{"):
			depth++
		case isPunct(ts[j], ")") || isPunct(ts[j], "]") || isPunct(ts[j], "}"):
			depth--
		case depth == 0 && isPunct(ts[j], ";"):
			return cond, j, true
		}
	}
	return
}

// assertKeywords are the words that start an assertion, and the macros that the fallbacks always become.
var assertKeywords = map[string]string{"assert": "", "assert_void": assertVoid, "assert_val": assertValue}

// assertMacros returns the assert macro for each token of ts, in code where assert is the macro. Compilers' signatures
// of lambdas lack the return type, so in a lambda, it's GDPP_ASSERT_REFERENCE if the lambda returns a reference,
// GDPP_ASSERT_VALUE if it returns another type than void, else GDPP_ASSERT_VOID. Among declarations, i.e. where assert
// is GDPP_ASSERT, and in local classes, a function's body gets GDPP_ASSERT, which reads the signature, but a
// constructor's or destructor's gets GDPP_ASSERT_VOID, since C++ forbids GDPP_ASSERT's `return EXPRESSION;` there. In
// a lambda or function whose return type C++ deduces and that returns a value, no return compiles, so it's
// GDPP_ASSERT_DEDUCED, which explains that.
func assertMacros(ts []lexer.Token, assert string) []string {
	macros := make([]string, len(ts))
	decls := make([]bool, len(ts)) // Whether the token is among declarations, rather than statements.
	owners := make([]int, len(ts)) // The '{' of the innermost lambda's or function's body around the token, or -1.
	for i := range ts {
		macros[i], decls[i], owners[i] = assert, assert == assertAny, -1
	}
	type lambda struct {
		macro   string
		deduced bool
	}
	lambdas := map[int]lambda{} // The '{' of each lambda's body, and its return type.
	for i := range ts {
		if open, macro, deduced, ok := lambdaBody(ts, i); ok {
			lambdas[open] = lambda{macro, deduced}
		}
	}
	deduced := map[int]bool{} // The '{' of each body of a lambda or function whose return type C++ deduces.
	// Outer blocks come first, so inner ones overwrite them.
	for open := range ts {
		if !isPunct(ts[open], "{") {
			continue
		}
		macro, inDecls, owner := macros[open], false, open
		kind := blockKind(ts, open)
		switch l, isLambda := lambdas[open]; {
		case isLambda:
			macro, deduced[open] = l.macro, l.deduced
		case kind == "class":
			inDecls, owner = true, owners[open]
		case kind == "ctor" && decls[open]:
			macro = assertVoid
		case (kind == "func" || kind == "deduced") && decls[open]:
			macro, deduced[open] = assertAny, kind == "deduced"
		default: // A block of statements, or an initializer.
			continue
		}
		for j, end := open+1, groupEnd(ts, open); j < end; j++ {
			macros[j], decls[j], owners[j] = macro, inDecls, owner
		}
	}
	valued := map[int]bool{} // The '{' of each body of a lambda or function that returns a value.
	for j, t := range ts {
		if k := skipSpace(ts, j+1); t.Type == tokIdent && t.Value == "return" && k < len(ts) && !isPunct(ts[k], ";") {
			valued[owners[j]] = true
		}
	}
	for j, owner := range owners {
		if deduced[owner] && valued[owner] {
			macros[j] = assertDeduced
		}
	}
	return macros
}

// lambdaBody matches a lambda at ts[i], `[captures]<template>(params) specifiers -> T {body}` where all but the
// captures and body are optional. It returns the index of the body's '{', the assert macro for T, and whether C++
// deduces T, without T or with `auto`. Subscripts, e.g. `a[i]` or `FOO(x)[i]`, don't match, since no body follows them.
func lambdaBody(ts []lexer.Token, i int) (open int, macro string, deduced, ok bool) {
	if !isPunct(ts[i], "[") {
		return
	}
	// Not an attribute, `[[nodiscard]]`, nor `operator[]`, `new[]` or `delete[]`.
	if p := prevToken(ts, i); p >= 0 && (isPunct(ts[p], "[") || ts[p].Type == tokIdent &&
		(ts[p].Value == "operator" || ts[p].Value == "new" || ts[p].Value == "delete")) {
		return
	}
	if n := skipSpace(ts, i+1); n < len(ts) && isPunct(ts[n], "[") {
		return
	}
	j := closing(ts, i) + 1
	if k := skipSpace(ts, j); k < len(ts) && isPunct(ts[k], "<") {
		j = groupEnd(ts, k)
	}
	ret := -1 // Where T starts.
	for ; j < len(ts); j++ {
		switch {
		case isPunct(ts[j], "{"):
			var words []string // T's.
			for k := ret; ret >= 0 && k < j; k++ {
				if ts[k].Type != tokWhitespace && ts[k].Type != tokNewline && !isComment(ts[k]) {
					words = append(words, ts[k].Value)
				}
			}
			n := len(words)
			switch {
			case n == 0 || slices.Contains(words, "auto"):
				return j, assertVoid, true, true
			case n == 1 && words[0] == "void":
				return j, assertVoid, false, true
			case words[n-1] == "&" && (n == 1 || words[n-2] != "&"):
				return j, assertReference, false, true
			}
			return j, assertValue, false, true
		case isPunct(ts[j], "(") || isPunct(ts[j], "["):
			j = closing(ts, j)
		case isPunct(ts[j], ";") || isPunct(ts[j], "}") || isPunct(ts[j], ")") || isPunct(ts[j], "]"):
			return
		case ret >= 0:
		case isPunct(ts[j], "->"):
			ret = j + 1
		case !isOneOf(ts[j], "Ident", "Whitespace", "Newline") && !isComment(ts[j]):
			return
		}
	}
	return
}

// blockKind tells what the block that starts with the '{' at ts[open] is, from its header, the code before it:
// "class" for the body of a class, struct, union, enum or namespace, "ctor" for that of a constructor or destructor,
// "deduced" for that of a function whose return type C++ deduces, "func" for that of another function, and "" for
// anything else, e.g. a block of statements or an initializer.
func blockKind(ts []lexer.Token, open int) string {
	// The header starts after the previous statement or access specifier. It may hold the brace initializers of a
	// constructor's initializer list, e.g. `b{2}` in `X() : a(1), b{2} {`.
	start := 0
	for j := prevToken(ts, open); j >= 0; j = prevToken(ts, j) {
		if isPunct(ts[j], ")") || isPunct(ts[j], "]") {
			if j = opening(ts, j); j < 0 {
				break
			}
			continue
		}
		if isPunct(ts[j], "}") {
			if o := braceOpening(ts, j); o >= 0 {
				if p := prevToken(ts, o); p >= 0 && ts[p].Type == tokIdent {
					if q := prevToken(ts, p); q >= 0 && (isPunct(ts[q], ",") || isPunct(ts[q], ":")) {
						j = o
						continue
					}
				}
			}
		}
		access := isPunct(ts[j], ":") && j > 0 && ts[j-1].Type == tokIdent &&
			(ts[j-1].Value == "public" || ts[j-1].Value == "protected" || ts[j-1].Value == "private")
		if isPunct(ts[j], ";") || isPunct(ts[j], "{") || isPunct(ts[j], "}") || access {
			start = j + 1
			break
		}
	}
	var ws []lexer.Token // The header's words.
	for _, t := range ts[start:open] {
		if t.Type != tokWhitespace && t.Type != tokNewline && !isComment(t) {
			ws = append(ws, t)
		}
	}
	// Drop `template <...>` and attributes, `[[...]]`.
	for len(ws) > 1 && (ws[0].Value == "template" && isPunct(ws[1], "<") || isPunct(ws[0], "[") && isPunct(ws[1], "[")) {
		ws = ws[groupEnd(ws, 1):]
	}
	if len(ws) == 0 {
		return ""
	}
	switch ws[0].Value {
	case "class", "struct", "union", "enum", "namespace", "typedef":
		return "class"
	case "extern":
		if len(ws) > 1 && ws[1].Type == tokString {
			return "class"
		}
	}
	// The function's parameters are the last brackets before its initializer list or trailing return type, that
	// follow a name, e.g. not noexcept's.
	paren, trailing := -1, false
	for k := 0; k < len(ws); k++ {
		init := paren >= 0 && isPunct(ws[k], ":") && !isPunct(ws[k-1], ":") && (k+1 == len(ws) || !isPunct(ws[k+1], ":"))
		if trailing = isPunct(ws[k], "->"); trailing || init {
			break
		}
		if ws[k].Type == tokIdent && ws[k].Value == "operator" {
			return "func"
		}
		if isPunct(ws[k], "(") {
			if k > 0 && ws[k-1].Type == tokIdent && !specifierWords[ws[k-1].Value] {
				paren = k
			}
			k = groupEnd(ws, k) - 1
		}
	}
	if paren < 0 {
		return ""
	}
	name := ws[paren-1].Value
	if paren >= 2 && isPunct(ws[paren-2], "~") ||
		paren >= 4 && isPunct(ws[paren-2], ":") && isPunct(ws[paren-3], ":") && ws[paren-4].Value == name {
		return "ctor"
	}
	var ret []string // The return type, without specifiers.
	for _, t := range ws[:paren-1] {
		if !specifierWords[t.Value] || t.Value == "decltype" {
			ret = append(ret, t.Value)
		}
	}
	switch strings.Join(ret, "") {
	case "":
		return "ctor"
	case "auto", "decltype(auto)":
		if !trailing {
			return "deduced"
		}
	}
	return "func"
}

// specifierWords are the words that may come before a constructor's name, or a bracket after a function's parameters.
var specifierWords = map[string]bool{"inline": true, "explicit": true, "constexpr": true, "consteval": true,
	"virtual": true, "static": true, "friend": true, "noexcept": true, "throw": true, "requires": true,
	"alignas": true, "decltype": true, "__attribute__": true, "__declspec": true}

// groupEnd returns the index after the bracket that closes the one at ts[open], counting only that kind of bracket,
// or len(ts) if there's none.
func groupEnd(ts []lexer.Token, open int) int {
	closer := map[string]string{"(": ")", "[": "]", "{": "}", "<": ">"}[ts[open].Value]
	depth := 0
	for j := open; j < len(ts); j++ {
		if isPunct(ts[j], ts[open].Value) {
			depth++
		} else if isPunct(ts[j], closer) {
			if depth--; depth == 0 {
				return j + 1
			}
		}
	}
	return len(ts)
}

// braceOpening returns the index of the '{' that the '}' at ts[close] closes, or -1 if there's none.
func braceOpening(ts []lexer.Token, close int) int {
	depth := 0
	for j := close; j >= 0; j-- {
		if isPunct(ts[j], "}") {
			depth++
		} else if isPunct(ts[j], "{") {
			if depth--; depth == 0 {
				return j
			}
		}
	}
	return -1
}

// rpcTarget matches an RPC call at ts[i], `rpc x->f(` or `rpc(peer) x->f(`. It returns the peer's tokens (nil
// without one), the index where x->f starts, the index of f and the index of its '('.
func rpcTarget(ts []lexer.Token, i int) (peer []lexer.Token, chain, name, paren int, ok bool) {
	if ts[i].Type != tokIdent {
		return
	}
	j := skipSpace(ts, i+1)
	switch {
	case ts[i].Value == "rpc" && j < len(ts) && isPunct(ts[j], "("):
		start := j + 1
		if j = closing(ts, j); j == len(ts) {
			return
		}
		peer, j = ts[start:j], skipSpace(ts, j+1)
	case ts[i].Value != "rpc":
		return
	}
	chain = j
	for j < len(ts) && ts[j].Type == tokIdent {
		name, j = j, skipSpace(ts, j+1)
		switch {
		case j < len(ts) && isPunct(ts[j], "("):
			return peer, chain, name, j, true
		case j < len(ts) && (isPunct(ts[j], "->") || isPunct(ts[j], ".")):
			j = skipSpace(ts, j+1)
		default:
			return
		}
	}
	return
}

// isMember reports whether the name at ts[i] follows ".", "->" or "::", so it's a member or in a scope.
func isMember(ts []lexer.Token, i int) bool {
	j := prevToken(ts, i)
	return j >= 0 && (isPunct(ts[j], ".") || isPunct(ts[j], "->") || isPunct(ts[j], ":") && j > 0 && isPunct(ts[j-1], ":"))
}

// skipSpace returns the index of the first token at or after i that isn't whitespace.
func skipSpace(ts []lexer.Token, i int) int {
	for i < len(ts) && (ts[i].Type == tokWhitespace || ts[i].Type == tokNewline) {
		i++
	}
	return i
}

// identifiers returns the identifiers used in user C++ code.
func identifiers(code string) []string {
	lex, err := gdppLexer.LexString("", code)
	if err != nil {
		return nil
	}
	var ids []string
	for {
		t, err := lex.Next()
		if err != nil || t.EOF() {
			return ids
		}
		if t.Type == tokIdent {
			ids = append(ids, t.Value)
		}
	}
}
