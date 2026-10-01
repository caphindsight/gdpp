package syntax_1

import "github.com/alecthomas/participle/v2/lexer"

// The lexer is context free: it splits GD++ and embedded C++ alike into C-like tokens.
// The parser decides which tokens form C++ code (see Block and Init).
// Block comments nest, so they get their own states.
var gdppLexer = lexer.MustStateful(lexer.Rules{
	"Root": {
		{Name: "DocBlockOpen", Pattern: `/\*\*[^*/]`, Action: lexer.Push("DocBlock")},
		{Name: "CommentOpen", Pattern: `/\*`, Action: lexer.Push("Comment")},
		{Name: "LineComment", Pattern: `(?m:////[^\n]*|//[^/\n][^\n]*|//$)`},
		{Name: "DocLine", Pattern: `///[^\n]*`},
		{Name: "RawStringOpen", Pattern: `(?:u8|[uUL])?R"([^()\\\s]{0,16})\(`, Action: lexer.Push("RawString")},
		{Name: "String", Pattern: `(?:u8|[uUL])?"(?:\\.|[^"\\\n])*"`},
		{Name: "Char", Pattern: `(?:u8|[uUL])?'(?:\\.|[^'\\\n])*'`},
		{Name: "Number", Pattern: `\.?[0-9](?:[eEpP][+-]|'[0-9A-Za-z_]|[0-9A-Za-z_.])*`},
		{Name: "Ident", Pattern: `[A-Za-z_][A-Za-z0-9_]*`},
		{Name: "Newline", Pattern: `\n`},
		{Name: "Whitespace", Pattern: `[ \t\r\f\v]+`},
		{Name: "Punct", Pattern: `->|[^\s\w]`},
	},
	"Comment": {
		{Name: "CommentOpen", Pattern: `/\*`, Action: lexer.Push("Comment")},
		{Name: "CommentClose", Pattern: `\*/`, Action: lexer.Pop()},
		{Name: "CommentText", Pattern: `[^*/]+|[*/]`},
	},
	"DocBlock": {
		{Name: "DocBlockNest", Pattern: `/\*`, Action: lexer.Push("DocBlock")},
		{Name: "DocBlockClose", Pattern: `\*/`, Action: lexer.Pop()},
		{Name: "DocBlockText", Pattern: `[^*/]+|[*/]`},
	},
	"RawString": {
		{Name: "RawStringClose", Pattern: `\)\1"`, Action: lexer.Pop()},
		{Name: "RawStringText", Pattern: `[^)]+|\)`},
	},
})

// elided are the token types the grammar never sees.
var elided = []string{"Whitespace", "Newline", "LineComment", "CommentOpen", "CommentClose", "CommentText"}

var sym = gdppLexer.Symbols()

var (
	tokChar       = sym["Char"]
	tokDocLine    = sym["DocLine"]
	tokIdent      = sym["Ident"]
	tokNewline    = sym["Newline"]
	tokNumber     = sym["Number"]
	tokPunct      = sym["Punct"]
	tokString     = sym["String"]
	tokWhitespace = sym["Whitespace"]
)

// isOneOf reports whether t has one of the named token types.
func isOneOf(t lexer.Token, names ...string) bool {
	for _, name := range names {
		if t.Type == sym[name] {
			return true
		}
	}
	return false
}

// isComment reports whether t is (part of) a plain comment.
func isComment(t lexer.Token) bool {
	return isOneOf(t, "LineComment", "CommentOpen", "CommentClose", "CommentText")
}

// isDoc reports whether t is (part of) a doc comment.
func isDoc(t lexer.Token) bool {
	return isOneOf(t, "DocLine", "DocBlockOpen", "DocBlockNest", "DocBlockClose", "DocBlockText")
}

// isPunct reports whether t is the punctuation token s.
func isPunct(t lexer.Token, s string) bool {
	return t.Type == tokPunct && t.Value == s
}
