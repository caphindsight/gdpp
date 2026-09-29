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

// cpp turns user C++ into plain C++: `emit f(x);` becomes `(void) f(x);`, which uses the [[nodiscard]] result.
func cpp(code string) string {
	lex, err := gdppLexer.LexString("", code)
	if err != nil {
		return code
	}
	var sb strings.Builder
	for {
		t, err := lex.Next()
		if err != nil || t.EOF() {
			return sb.String()
		}
		if t.Type == tokIdent && t.Value == "emit" {
			t.Value = "(void)"
		}
		sb.WriteString(t.Value)
	}
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
