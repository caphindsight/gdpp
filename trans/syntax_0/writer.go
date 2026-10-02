package syntax_0

import (
	"fmt"
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

// user writes prefix+code+suffix as one line, where code is user C++ starting at pos in the GD++ file.
// #line directives around it make compilers report user code at its GD++ location.
func (w *writer) user(pos lexer.Position, prefix, code, suffix string) {
	w.ln("#line %d %q", pos.Line, w.source)
	w.ln("%s", prefix+cpp(code)+suffix)
	w.ln("#line %d %q", w.lines+2, w.self)
}

// block writes the text of b, a C++ block, between prefix and suffix.
func (w *writer) block(b *Block, prefix, suffix string) {
	w.user(b.TextPos, prefix, b.Text, suffix)
}

func (w *writer) String() string {
	return w.sb.String()
}

// cpp turns user C++ into plain C++:
//   - `emit f(x);` becomes `(void) f(x);`, which uses the [[nodiscard]] result,
//   - `rpc x->f(a)` and `rpc(peer) x->f(a)` become `x->_gdpp_rpc_f(0, a)` and `x->_gdpp_rpc_f(peer, a)`,
//   - `claim x`, `is_done x` and `cancel x` become `x.claim()`, `x.is_done()` and `x.cancel()`, for an Async x,
//   - `is_cancelled`, a bare word, becomes `gdpp::is_cancelled()`,
//   - `string_name "x"` becomes `GDPP_STRING_NAME("x")`,
//   - `x as T` becomes `gdpp::cast<T>(x)`,
//   - `guard (x; "m") {` and `guard (x) {` become `GDPP_GUARD("x", "m", x) {` and `GDPP_GUARD("x", "", x) {`,
//   - a `,` before `)` is dropped, so calls may end with a trailing comma.
func cpp(code string) string {
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
		if cond, semi, close, ok := guardTarget(ts, i); ok {
			// Drop the '(', the message and the spaces around them, keeping their newlines so the lines still match.
			msg, end := `""`, close
			if semi >= 0 {
				msg, end = ts[skipSpace(ts, semi+1)].Value, prevToken(ts, semi)+1
			}
			for k := i + 1; k < close; k++ {
				if k < cond || k >= end {
					out[k] = strings.Repeat("\n", strings.Count(ts[k].Value, "\n"))
				}
			}
			var sb strings.Builder
			for _, t := range ts[cond:end] {
				if !isComment(t) {
					sb.WriteString(t.Value)
				}
			}
			text := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(strings.Join(strings.Fields(sb.String()), " "))
			out[i] = `GDPP_GUARD("` + text + `", ` + msg + ", "
			i = cond - 1 // The condition may hold more rewrites.
			continue
		}
		if ts[i].Type == tokIdent && asyncWords[ts[i].Value] {
			j := skipSpace(ts, i+1)
			if end := postfixEnd(ts, j); end > j {
				// Drop the keyword and the spaces after it, but keep a line break and the indentation after it.
				for k := i; k < j && ts[k].Type != tokNewline; k++ {
					out[k] = ""
				}
				out[end-1] += "." + ts[i].Value + "()"
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
	"new": true, "delete": true, "and": true, "or": true, "not": true}

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

// guardTarget matches a guard at ts[i], `guard (cond; "message") {` or `guard (cond) {`. It returns the index where
// cond starts, the index of the ';' (-1 without a message) and the index of the ')'.
func guardTarget(ts []lexer.Token, i int) (cond, semi, close int, ok bool) {
	if ts[i].Type != tokIdent || ts[i].Value != "guard" || isMember(ts, i) {
		return
	}
	open := skipSpace(ts, i+1)
	if open == len(ts) || !isPunct(ts[open], "(") {
		return
	}
	if close = closing(ts, open); close == len(ts) {
		return
	}
	if j := skipSpace(ts, close+1); j == len(ts) || !isPunct(ts[j], "{") {
		return
	}
	// The message follows the first ';' outside of brackets, e.g. not one in a lambda.
	cond, semi = skipSpace(ts, open+1), -1
	for j, depth := open+1, 0; j < close && semi < 0; j++ {
		switch {
		case isPunct(ts[j], "(") || isPunct(ts[j], "[") || isPunct(ts[j], "{"):
			depth++
		case isPunct(ts[j], ")") || isPunct(ts[j], "]") || isPunct(ts[j], "}"):
			depth--
		case depth == 0 && isPunct(ts[j], ";"):
			semi = j
		}
	}
	end := close
	if semi >= 0 {
		m := skipSpace(ts, semi+1)
		if m == close || ts[m].Type != tokString || ts[m].Value[0] != '"' || skipSpace(ts, m+1) != close {
			return
		}
		end = semi
	}
	return cond, semi, close, prevToken(ts, end) >= cond
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
