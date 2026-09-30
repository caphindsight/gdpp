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
//   - `claim x` and `is_done x` become `x.claim()` and `x.is_done()`, for an Async x.
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
		if ts[i].Type == tokIdent && ts[i].Value == "emit" {
			out[i] = "(void)"
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
var asyncWords = map[string]bool{"claim": true, "is_done": true}

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
