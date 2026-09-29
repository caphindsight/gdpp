// godot_names.go: finds the names that godot-cpp's headers declare, so GD++ code can use them and get the right includes,
// and their declarations, for gd++ doc.

package main

import (
	"path"
	"slices"
	"strings"
	"unicode"

	"gd++/trans"
)

// godotName is a name declared directly in namespace godot, e.g. "Node3D".
type godotName struct {
	Name    string     `toml:"name"`
	Include string     `toml:"include"` // E.g. "<godot_cpp/classes/node3d.hpp>".
	Kind    trans.Kind `toml:"kind"`    // Object, RefCounted or Other.
}

// cppToken is a token of C++ code, without comments, literals and
// preprocessor lines: an identifier, "::", or a single punctuation character.
type cppToken string

// tokenizeCpp splits C++ code into tokens, and returns each token's start
// and end offsets in src. String, character and number literals become the
// token "0". Preprocessor lines are dropped: the scanner doesn't expand macros.
// Of each #if, only the first branch is kept, so the braces of the others
// don't unbalance the code.
func tokenizeCpp(src string) (cppTokens, [][2]int) {
	var tokens cppTokens
	var spans [][2]int
	skip := 0 // How many #ifs deep the tokens are in a branch that isn't kept.
	add := func(t cppToken, start, n int) {
		if skip == 0 {
			tokens = append(tokens, t)
			spans = append(spans, [2]int{start, start + n})
		}
	}
	lineStart := true
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '\n':
			lineStart = true
			i++
			continue
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			i++
			continue
		case c == '#' && lineStart:
			directive := strings.TrimLeft(src[i+1:], " \t")
			directive = directive[:len(directive)-len(strings.TrimLeft(directive, "abcdefghijklmnopqrstuvwxyz"))]
			switch {
			case skip > 0 && strings.HasPrefix(directive, "if"):
				skip++
			case skip > 0 && directive == "endif":
				skip--
			case skip == 0 && (directive == "else" || strings.HasPrefix(directive, "elif")):
				skip = 1
			}
			for i < len(src) && src[i] != '\n' {
				if src[i] == '\\' && i+1 < len(src) && src[i+1] == '\n' {
					i++
				}
				i++
			}
			continue
		}
		lineStart = false
		switch rest := src[i:]; {
		case strings.HasPrefix(rest, "//"):
			i += strings.IndexByte(rest+"\n", '\n')
		case strings.HasPrefix(rest, "/*"):
			end := strings.Index(rest[2:], "*/")
			if end < 0 {
				return tokens, spans
			}
			i += end + 4
		case c == '"' || c == '\'':
			n := literalLen(rest)
			add("0", i, n)
			i += n
		case c >= '0' && c <= '9':
			j := 1
			for j < len(rest) && (isIdentByte(rest[j]) || rest[j] == '.' || rest[j] == '\'') {
				j++
			}
			add("0", i, j)
			i += j
		case isIdentByte(c):
			j := 1
			for j < len(rest) && isIdentByte(rest[j]) {
				j++
			}
			// A raw string, maybe with a prefix: R"delim( ... )delim".
			if prefix := rest[:j]; strings.HasSuffix(prefix, "R") && j < len(rest) && rest[j] == '"' && len(prefix) <= 3 {
				open := strings.IndexByte(rest[j:], '(')
				if open >= 0 {
					end := strings.Index(rest[j+open:], ")"+rest[j+1:j+open]+"\"")
					if end >= 0 {
						n := j + open + end + open + 1
						add("0", i, n)
						i += n
						continue
					}
				}
			}
			add(cppToken(rest[:j]), i, j)
			i += j
		case strings.HasPrefix(rest, "::"):
			add("::", i, 2)
			i += 2
		default:
			add(cppToken(rest[:1]), i, 1)
			i++
		}
	}
	return tokens, spans
}

func isIdentByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// literalLen returns the length of the string or character literal at the start of s.
func literalLen(s string) int {
	quote := s[0]
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case quote, '\n':
			return i + 1
		}
	}
	return len(s)
}

func (t cppToken) isIdent() bool {
	return t != "" && t != "0" && (t[0] == '_' || unicode.IsLetter(rune(t[0])))
}

// isMacroName reports whether t looks like a macro, e.g. MAKE_PTRARG: all caps.
func (t cppToken) isMacroName() bool {
	return t.isIdent() && strings.ToUpper(string(t)) == string(t) && strings.ContainsAny(string(t), "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
}

// cppDecl is a name that a header declares in the scanned namespace.
type cppDecl struct {
	name, base string // base: a class's first base class, if any.
}

// scanCppDecls returns the names declared directly in namespace godot in the
// header src: class, struct, union and enum
// definitions (not forward declarations or specializations), aliases, nested
// namespaces, functions and variables. Macro invocations and anything in
// nested scopes are skipped.
func scanCppDecls(src string) []cppDecl {
	tokens, _ := tokenizeCpp(src)
	var decls []cppDecl
	walkCppDecls(tokens, func(d cppDecl, start, end int) { decls = append(decls, d) })
	return decls
}

// cppTokens are the tokens of a header.
type cppTokens []cppToken

// at returns the token at j, or "" past the end.
func (ts cppTokens) at(j int) cppToken {
	if j >= 0 && j < len(ts) {
		return ts[j]
	}
	return ""
}

// skip returns the index after the bracket that closes the one at j.
func (ts cppTokens) skip(j int, open, close cppToken) int {
	for depth := 0; j < len(ts); j++ {
		switch ts[j] {
		case open:
			depth++
		case close:
			if depth--; depth == 0 {
				return j + 1
			}
		}
	}
	return j
}

// walkCppDecls calls visit with each declaration that scanCppDecls finds,
// the index of its first token, and the index after its head: of the "{" of
// its body, or after its ";".
func walkCppDecls(tokens cppTokens, visit func(d cppDecl, start, end int)) {
	var scopes []string // "namespace:NAME" for namespaces (NAME may be a::b), "" for other braces.
	inGodot := func() bool { return len(scopes) == 1 && scopes[0] == "namespace:godot" }
	for i := 0; i < len(tokens); {
		switch t := tokens[i]; {
		case t == "namespace":
			j := i + 1
			var name []string
			for tokens.at(j).isIdent() || tokens.at(j) == "::" {
				if tokens.at(j) != "::" {
					name = append(name, string(tokens.at(j)))
				}
				j++
			}
			if tokens.at(j) != "{" {
				i = j // An alias, e.g. namespace fs = std::filesystem;
				continue
			}
			if inGodot() && len(name) == 1 {
				visit(cppDecl{name: name[0]}, i, j)
			}
			scopes = append(scopes, "namespace:"+strings.Join(name, "::"))
			i = j + 1
		case t == "{":
			scopes = append(scopes, "")
			i++
		case t == "}":
			if len(scopes) > 0 {
				scopes = scopes[:len(scopes)-1]
			}
			i++
		case !inGodot():
			i++
		default:
			start := i
			var d *cppDecl
			d, i = scanStatement(tokens, i)
			if d != nil {
				visit(*d, start, i)
			}
		}
	}
}

// scanStatement reads the statement at tokens[i], in the scanned namespace. It
// returns the name it declares, if any, and the index of the next token to
// scan. A body ("{") is left for the caller, which tracks scopes.
func scanStatement(tokens cppTokens, i int) (*cppDecl, int) {
	switch t := tokens[i]; {
	case t == ";":
		return nil, i + 1
	case t == "template":
		if tokens.at(i+1) == "<" {
			return nil, tokens.skip(i+1, "<", ">")
		}
		return nil, i + 1
	case t == "class" || t == "struct" || t == "union" || t == "enum":
		j := i + 1
		if t == "enum" && (tokens.at(j) == "class" || tokens.at(j) == "struct") {
			j++
		}
		for tokens.at(j) == "[" || tokens.at(j) == "alignas" || tokens.at(j).isMacroName() && tokens.at(j+1) != "{" && tokens.at(j+1) != ":" && tokens.at(j+1) != ";" {
			switch {
			case tokens.at(j) == "[":
				j = tokens.skip(j, "[", "]")
			case tokens.at(j+1) == "(":
				j = tokens.skip(j+1, "(", ")")
			default:
				j++
			}
		}
		name := tokens.at(j)
		if !name.isIdent() {
			return nil, j // An anonymous class or enum: its body is a scope.
		}
		j++
		if tokens.at(j) == "final" {
			j++
		}
		switch tokens.at(j) {
		case "{":
			return &cppDecl{name: string(name)}, j
		case ":":
			if t == "enum" {
				for tokens.at(j) != "{" && tokens.at(j) != ";" && tokens.at(j) != "" {
					j++
				}
				if tokens.at(j) == "{" {
					return &cppDecl{name: string(name)}, j
				}
				return nil, j
			}
			base := ""
			for j++; tokens.at(j) != "{" && tokens.at(j) != ";" && tokens.at(j) != ""; j++ {
				if tokens.at(j) == "<" {
					j = tokens.skip(j, "<", ">") - 1
				} else if tokens.at(j).isIdent() && base == "" && tokens.at(j+1) != "::" && tokens.at(j) != "public" && tokens.at(j) != "protected" && tokens.at(j) != "private" && tokens.at(j) != "virtual" {
					base = string(tokens.at(j))
				}
			}
			if tokens.at(j) == "{" {
				return &cppDecl{name: string(name), base: base}, j
			}
			return nil, j
		}
		// A forward declaration, a specialization or a variable of this type: no new name.
		return endStatement(tokens, j)
	case t == "using":
		if tokens.at(i+1).isIdent() && tokens.at(i+2) == "=" {
			_, next := endStatement(tokens, i)
			return &cppDecl{name: string(tokens.at(i + 1))}, next
		}
		return endStatement(tokens, i)
	case t == "typedef":
		j := i + 1
		name := cppToken("")
		for ; tokens.at(j) != ";" && tokens.at(j) != ""; j++ {
			switch {
			case tokens.at(j) == "(" && tokens.at(j+1) == "*" && tokens.at(j+2).isIdent():
				name = tokens.at(j + 2) // A function pointer: typedef void (*Name)(...);
				j = tokens.skip(j, "(", ")") - 1
			case tokens.at(j) == "(" || tokens.at(j) == "[" || tokens.at(j) == "<":
				j = tokens.skip(j, tokens.at(j), map[cppToken]cppToken{"(": ")", "[": "]", "<": ">"}[tokens.at(j)]) - 1
			case tokens.at(j).isIdent() && name == "" || tokens.at(j).isIdent() && tokens.at(j+1) == ";":
				name = tokens.at(j)
			}
		}
		if name.isIdent() {
			return &cppDecl{name: string(name)}, j + 1
		}
		return nil, j + 1
	case t == "extern" && tokens.at(i+1) == "0":
		return nil, i + 2 // extern "C": its braces are a scope.
	case t == "static_assert" || t == "friend" || t == "public" || t == "private" || t == "protected":
		return endStatement(tokens, i)
	case t.isMacroName() && tokens.at(i+1) == "(":
		j := tokens.skip(i+1, "(", ")")
		if tokens.at(j) == ";" {
			j++
		}
		return nil, j
	case t == "{" || t == "}":
		return nil, i
	}
	// A function or a variable: the name is the identifier before the first
	// "(", or else before "=", "{", "[" or ";". Qualified names (Foo::bar)
	// define members declared elsewhere.
	j := i
	var name cppToken
	isFunc := false
	for ; tokens.at(j) != ";" && tokens.at(j) != "{" && tokens.at(j) != "=" && tokens.at(j) != ""; j++ {
		switch tokens.at(j) {
		case "operator":
			return endStatement(tokens, j) // An operator overload: no name to include.
		case "(":
			if !isFunc && name == "" && tokens.at(j-1).isIdent() && tokens.at(j-2) != "::" && !tokens.at(j-1).isMacroName() && tokens.at(j-1) != "operator" {
				name = tokens.at(j - 1)
			}
			isFunc = true
			j = tokens.skip(j, "(", ")") - 1
		case "<":
			j = tokens.skip(j, "<", ">") - 1
		case "[":
			if !isFunc && name == "" && tokens.at(j-1).isIdent() && tokens.at(j-2) != "::" {
				name = tokens.at(j - 1)
			}
			j = tokens.skip(j, "[", "]") - 1
		}
	}
	if !isFunc && name == "" && tokens.at(j-1).isIdent() && tokens.at(j-2) != "::" && j > i {
		name = tokens.at(j - 1)
	}
	next := j
	if tokens.at(j) == "=" {
		_, next = endStatement(tokens, j)
	} else if tokens.at(j) == ";" {
		next = j + 1
	}
	if name.isIdent() {
		return &cppDecl{name: string(name)}, next
	}
	return nil, next
}

// endStatement returns the index after the ";" that ends the statement at
// tokens[i], skipping brackets, or of the "{" of a body.
func endStatement(tokens cppTokens, i int) (*cppDecl, int) {
	for j := i; j < len(tokens); j++ {
		switch tokens.at(j) {
		case ";":
			return nil, j + 1
		case "{":
			if tokens.at(j-1) == "=" || tokens.at(j-1) == "," || tokens.at(j-1) == "(" {
				j = tokens.skip(j, "{", "}") - 1 // A braced initializer.
				continue
			}
			return nil, j
		case "(":
			j = tokens.skip(j, "(", ")") - 1
		}
	}
	return nil, len(tokens)
}

// cppDoc is a declaration as gd++ doc shows it: the comments right before
// it, its head (e.g. "template <typename T>\nclass TypedArray : public
// Array"), its first base class if any, what it aliases if it's an alias, and
// its public members or values.
type cppDoc struct {
	comments, head, base string
	alias                string // For an alias, e.g. using CharString = CharStringT<char>, the type's name: CharStringT.
	members              []cppMember
}

// cppMember is a public member of a class, struct, union or namespace, or a
// value of an enum: its name, e.g. "push_back", the comments right before it,
// and its declaration, without body.
type cppMember struct{ name, comments, text string }

// cppSource is a header's source, tokens and their spans.
type cppSource struct {
	src    string
	tokens cppTokens
	spans  [][2]int
}

// scanCppDocs returns the declarations of name directly in namespace godot
// in the header src, as scanCppDecls finds them.
func scanCppDocs(src, name string) []cppDoc {
	tokens, spans := tokenizeCpp(src)
	h := cppSource{src, tokens, spans}
	var docs []cppDoc
	walkCppDecls(tokens, func(d cppDecl, start, end int) {
		if d.name != name {
			return
		}
		first := tokens.templateStart(start)
		doc := cppDoc{comments: h.comments(first), head: h.head(first, end), base: d.base}
		if tokens[start] == "using" && tokens.at(start+2) == "=" {
			doc.alias = tokens.aliasedName(start+3, end)
		}
		if tokens.at(end) == "{" {
			close := tokens.skip(end, "{", "}") - 1
			switch tokens[start] {
			case "class", "struct", "union", "namespace":
				doc.members = h.members(end+1, close, tokens[start] != "class")
			case "enum":
				doc.members = h.values(end+1, close)
			}
		}
		docs = append(docs, doc)
	})
	return docs
}

// aliasedName returns the last part of the name of the type that the tokens
// [a, b) name, e.g. "Bar" for "const ::foo::Bar<int>", or "" if they name
// none.
func (ts cppTokens) aliasedName(a, b int) string {
	name := ""
	for j := a; j < b && ts[j] != "<" && ts[j] != ";"; j++ {
		switch {
		case ts[j] == "::" || ts[j] == "const" || ts[j] == "typename" || ts[j] == "struct" || ts[j] == "class":
		case ts[j].isIdent() && (name == "" || ts.at(j-1) == "::"):
			name = string(ts[j])
		default:
			return ""
		}
	}
	return name
}

// templateStart returns the index of the "template" whose parameters end
// right before the token at i, or else i.
func (ts cppTokens) templateStart(i int) int {
	if ts.at(i-1) != ">" {
		return i
	}
	for j, depth := i-1, 0; j >= 0; j-- {
		switch ts[j] {
		case ">":
			depth++
		case "<":
			if depth--; depth == 0 {
				if ts.at(j-1) == "template" {
					return j - 1
				}
				return i
			}
		}
	}
	return i
}

// text returns the tokens [a, b) as in the source, on one line, without
// macros like _FORCE_INLINE_.
func (h cppSource) text(a, b int) string {
	var text strings.Builder
	prev := -1
	for j := a; j < b; j++ {
		if h.tokens.isSpecifierMacro(j) {
			continue
		}
		if prev >= 0 && h.spans[j][0] > h.spans[prev][1] {
			text.WriteByte(' ')
		}
		text.WriteString(h.src[h.spans[j][0]:h.spans[j][1]])
		prev = j
	}
	return text.String()
}

// isSpecifierMacro reports whether the token at j looks like a macro that
// specifies the declaration after it, e.g. _FORCE_INLINE_ or GDE_EXPORT.
func (ts cppTokens) isSpecifierMacro(j int) bool {
	return ts.at(j).isMacroName() && strings.Contains(string(ts.at(j)), "_") && ts.at(j+1).isIdent()
}

// head returns the tokens [a, b) of a declaration like text, without a
// trailing ";", with template parameters on a line of their own.
func (h cppSource) head(a, b int) string {
	prefix := ""
	if h.tokens.at(a) == "template" {
		t := h.tokens.skip(a+1, "<", ">")
		prefix, a = h.text(a, t)+"\n", t
	}
	if b > a && h.tokens[b-1] == ";" {
		b--
	}
	return prefix + h.text(a, b)
}

// comments returns the // comment lines right before the token at i, each
// ending with "\n".
func (h cppSource) comments(i int) string {
	prev := 0
	if i > 0 {
		prev = h.spans[i-1][1]
	}
	lines := strings.Split(h.src[prev:h.spans[i][0]], "\n")
	comments := ""
	// The first line ends the previous token's line, and the last one indents the token.
	for j := len(lines) - 2; j >= 1 && strings.HasPrefix(strings.TrimSpace(lines[j]), "//"); j-- {
		comments = strings.TrimSpace(lines[j]) + "\n" + comments
	}
	return comments
}

// values returns the values of an enum whose body's tokens are [a, b).
func (h cppSource) values(a, b int) []cppMember {
	var values []cppMember
	for i := a; i < b; {
		j := i
		for j < b && h.tokens[j] != "," {
			if h.tokens[j] == "(" {
				j = h.tokens.skip(j, "(", ")")
			} else {
				j++
			}
		}
		if j > i {
			values = append(values, cppMember{string(h.tokens[i]), h.comments(i), h.text(i, j)})
		}
		i = j + 1
	}
	return values
}

// members returns the public members of a class, struct, union or namespace
// whose body's tokens are [a, b). Members before any access label are public
// if public is set.
func (h cppSource) members(a, b int, public bool) []cppMember {
	var members []cppMember
	for i := a; i < b; {
		if t := h.tokens[i]; (t == "public" || t == "private" || t == "protected") && h.tokens.at(i+1) == ":" {
			public = t == "public"
			i += 2
			continue
		}
		m, next := h.member(i, b)
		if public && m.name != "" {
			members = append(members, m)
		}
		i = next
	}
	return members
}

// member reads the member declaration at token i, before b, and returns it
// and the index after it. Its name is empty for what gd++ doc skips: macro
// invocations, friends, static asserts and forward declarations.
func (h cppSource) member(i, b int) (cppMember, int) {
	ts := h.tokens
	switch t := ts[i]; {
	case t.isMacroName() && (ts.at(i+1) == "(" || ts.at(i+1) == ";"):
		j := i + 1
		if ts.at(j) == "(" {
			j = ts.skip(j, "(", ")")
		}
		if ts.at(j) == ";" {
			j++
		}
		return cppMember{}, j
	case t == "friend" || t == "static_assert" || t == ";":
		_, j := endStatement(ts, i)
		if ts.at(j) == "{" {
			j = ts.skip(j, "{", "}")
		}
		return cppMember{}, max(j, i+1)
	}
	m := cppMember{comments: h.comments(i)}
	k := i // The start of the declaration proper, after template parameters and macros.
	if ts[k] == "template" {
		k = ts.skip(k+1, "<", ">")
	}
	for ts.isSpecifierMacro(k) {
		k++
	}
	if kw := ts.at(k); kw == "class" || kw == "struct" || kw == "union" || kw == "enum" {
		j := k + 1
		if kw == "enum" && (ts.at(j) == "class" || ts.at(j) == "struct") {
			j++
		}
		m.name = string(ts.at(j))
		for j < b && ts[j] != "{" && ts[j] != ";" {
			j++
		}
		if ts.at(j) != "{" {
			return cppMember{}, j + 1
		}
		close := ts.skip(j, "{", "}")
		m.text = h.head(i, j) + " { ... }"
		if kw == "enum" {
			m.text = h.head(i, j) + " " + h.text(j, close)
		}
		if ts.at(close) == ";" {
			close++
		}
		return m, close
	}
	// A function, variable or alias. Its declaration ends at the ";", at the body, or at a constructor's
	// initializer list.
	end, isFunc := -1, false
	j := k
	for ; j < b && ts[j] != ";"; j++ {
		switch ts[j] {
		case "operator":
			n := j + 1
			if ts.at(n) == "(" {
				n += 2
			}
			for n < b && ts[n] != "(" {
				n++
			}
			m.name, j = h.text(j, n), n-1
		case "(", "[":
			if m.name == "" {
				m.name = string(ts.at(j - 1))
			}
			isFunc = isFunc || ts[j] == "("
			j = ts.skip(j, ts[j], map[cppToken]cppToken{"(": ")", "[": "]"}[ts[j]]) - 1
		case "<":
			if ts.at(j-1) == "template" {
				j = ts.skip(j, "<", ">") - 1
			}
		case "=":
			if m.name == "" {
				m.name = string(ts.at(j - 1))
			}
		case ":":
			if isFunc && end < 0 {
				end = j
			}
		case "{":
			if m.name == "" {
				m.name = string(ts.at(j - 1))
			}
			if isFunc && ts.at(j-1) != "=" && (end < 0 || ts.at(j-1) == ")" || ts.at(j-1) == "}") {
				if end < 0 {
					end = j
				}
				m.text = h.head(i, end)
				return m, ts.skip(j, "{", "}")
			}
			j = ts.skip(j, "{", "}") - 1 // A braced initializer.
		}
	}
	if end < 0 {
		end = j
	}
	if m.name == "" {
		m.name = string(ts.at(end - 1))
	}
	m.text = h.head(i, end)
	return m, j + 1
}

// scanGodotNames returns the names declared directly in namespace godot in
// the headers under roots, each with its include path relative to its root.
// Classes derived from Object are Object or RefCounted; everything else is
// Other. When a name is declared in several headers, the first one wins.
func scanGodotNames(roots []Path) []godotName {
	var names []godotName
	bases := map[string]string{}
	for _, root := range roots {
		walkHeaders(root, func(rel, src string) {
			if strings.HasSuffix(rel, ".inc.hpp") {
				return // A fragment, included from inside other headers.
			}
			for _, d := range scanCppDecls(src) {
				if _, ok := bases[d.name]; !ok {
					bases[d.name] = d.base
					names = append(names, godotName{Name: d.name, Include: "<" + rel + ">"})
				}
			}
		})
	}
	return withKinds(names, bases)
}

// walkHeaders calls visit with each .h and .hpp header under root, and its
// path relative to root.
func walkHeaders(root Path, visit func(rel, src string)) {
	var walk func(dir Path, rel string)
	walk = func(dir Path, rel string) {
		for _, child := range dir.Ls() {
			childRel := path.Join(rel, child.Name())
			switch {
			case child.IsDir():
				walk(child, childRel)
			case path.Ext(childRel) == ".hpp" || path.Ext(childRel) == ".h":
				visit(childRel, child.ReadString())
			}
		}
	}
	if root.IsDir() {
		walk(root, "")
	}
}

// withKinds returns names, sorted, with the kinds that their chains of base
// classes (bases, by name) give: Object or RefCounted, or else Other.
func withKinds(names []godotName, bases map[string]string) []godotName {
	for i, n := range names {
		names[i].Kind = trans.Other
		for name, seen := n.Name, 0; name != "" && seen < 100; name, seen = bases[name], seen+1 {
			if name == "RefCounted" {
				names[i].Kind = trans.RefCounted
				break
			}
			if name == "Object" {
				names[i].Kind = trans.Object
				break
			}
		}
	}
	slices.SortFunc(names, func(a, b godotName) int { return strings.Compare(a.Name, b.Name) })
	return names
}
