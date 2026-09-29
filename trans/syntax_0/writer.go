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

// cpp turns user C++ into plain C++: `emit f(x);` becomes `(void) f(x);`, which uses the [[nodiscard]] result,
// and `rpc x->f(a)` and `rpc_id(peer) x->f(a)` become `x->_gdpp_rpc_f(0, a)` and `x->_gdpp_rpc_f(peer, a)`.
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

// rpcTarget matches an RPC call at ts[i], `rpc x->f(` or `rpc_id(peer) x->f(`. It returns the peer's tokens (nil
// for rpc), the index where x->f starts, the index of f and the index of its '('.
func rpcTarget(ts []lexer.Token, i int) (peer []lexer.Token, chain, name, paren int, ok bool) {
	if ts[i].Type != tokIdent {
		return
	}
	j := skipSpace(ts, i+1)
	switch {
	case ts[i].Value == "rpc_id" && j < len(ts) && isPunct(ts[j], "("):
		start, depth := j+1, 0
		for ; j < len(ts); j++ {
			if isPunct(ts[j], "(") {
				depth++
			} else if isPunct(ts[j], ")") {
				if depth--; depth == 0 {
					break
				}
			}
		}
		if j == len(ts) {
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
