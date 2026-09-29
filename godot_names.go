// godot_names.go: finds the names that godot-cpp's or the engine's headers
// declare, so GD++ code can use them and get the right includes.

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
	Include string     `toml:"include"`       // E.g. "<godot_cpp/classes/node3d.hpp>".
	Kind    trans.Kind `toml:"kind"`          // Object, RefCounted or Other.
	Cpp     string     `toml:"cpp,omitempty"` // How C++ names it, if not Name, e.g. "::core_bind::OS".
}

// cppToken is a token of C++ code, without comments, literals and
// preprocessor lines: an identifier, "::", or a single punctuation character.
type cppToken string

// tokenizeCpp splits C++ code into tokens. String, character and number
// literals become the token "0". Preprocessor lines are dropped: the scanner
// doesn't expand macros.
func tokenizeCpp(src string) []cppToken {
	var tokens []cppToken
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
				return tokens
			}
			i += end + 4
		case c == '"' || c == '\'':
			i += literalLen(rest)
			tokens = append(tokens, "0")
		case c >= '0' && c <= '9':
			j := 1
			for j < len(rest) && (isIdentByte(rest[j]) || rest[j] == '.' || rest[j] == '\'') {
				j++
			}
			i += j
			tokens = append(tokens, "0")
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
						i += j + open + end + open + 1
						tokens = append(tokens, "0")
						continue
					}
				}
			}
			tokens = append(tokens, cppToken(rest[:j]))
			i += j
		case strings.HasPrefix(rest, "::"):
			tokens = append(tokens, "::")
			i += 2
		default:
			tokens = append(tokens, cppToken(rest[:1]))
			i++
		}
	}
	return tokens
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

// scanCppDecls returns the names declared directly in namespace ns ("" for
// the global namespace) in the header src: class, struct, union and enum
// definitions (not forward declarations or specializations), aliases, nested
// namespaces, functions and variables. Macro invocations and anything in
// nested scopes are skipped.
func scanCppDecls(src, ns string) []cppDecl {
	tokens := tokenizeCpp(src)
	var decls []cppDecl
	var scopes []string // "namespace:NAME" for namespaces (NAME may be a::b), "" for other braces.
	inGodot := func() bool {
		if ns == "" {
			return len(scopes) == 0
		}
		return len(scopes) == 1 && scopes[0] == "namespace:"+ns
	}
	i := 0
	at := func(j int) cppToken {
		if j < len(tokens) {
			return tokens[j]
		}
		return ""
	}
	// skip returns the index after the bracket that closes the one at j.
	skip := func(j int, open, close cppToken) int {
		for depth := 0; j < len(tokens); j++ {
			switch tokens[j] {
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
	for i < len(tokens) {
		t := tokens[i]
		switch {
		case t == "namespace":
			j := i + 1
			var name []string
			for at(j).isIdent() || at(j) == "::" {
				if at(j) != "::" {
					name = append(name, string(at(j)))
				}
				j++
			}
			if at(j) != "{" {
				i = j // An alias, e.g. namespace fs = std::filesystem;
				continue
			}
			if inGodot() && len(name) == 1 {
				decls = append(decls, cppDecl{name: name[0]})
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
			var d *cppDecl
			d, i = scanStatement(tokens, i, at, skip)
			if d != nil {
				decls = append(decls, *d)
			}
		}
	}
	return decls
}

// scanStatement reads the statement at tokens[i], in the scanned namespace. It
// returns the name it declares, if any, and the index of the next token to
// scan. A body ("{") is left for the caller, which tracks scopes.
func scanStatement(tokens []cppToken, i int, at func(int) cppToken, skip func(int, cppToken, cppToken) int) (*cppDecl, int) {
	switch t := tokens[i]; {
	case t == ";":
		return nil, i + 1
	case t == "template":
		if at(i+1) == "<" {
			return nil, skip(i+1, "<", ">")
		}
		return nil, i + 1
	case t == "class" || t == "struct" || t == "union" || t == "enum":
		j := i + 1
		if t == "enum" && (at(j) == "class" || at(j) == "struct") {
			j++
		}
		for at(j) == "[" || at(j) == "alignas" || at(j).isMacroName() && at(j+1) != "{" && at(j+1) != ":" && at(j+1) != ";" {
			switch {
			case at(j) == "[":
				j = skip(j, "[", "]")
			case at(j+1) == "(":
				j = skip(j+1, "(", ")")
			default:
				j++
			}
		}
		name := at(j)
		if !name.isIdent() {
			return nil, j // An anonymous class or enum: its body is a scope.
		}
		j++
		if at(j) == "final" {
			j++
		}
		switch at(j) {
		case "{":
			return &cppDecl{name: string(name)}, j
		case ":":
			if t == "enum" {
				for at(j) != "{" && at(j) != ";" && at(j) != "" {
					j++
				}
				if at(j) == "{" {
					return &cppDecl{name: string(name)}, j
				}
				return nil, j
			}
			base := ""
			for j++; at(j) != "{" && at(j) != ";" && at(j) != ""; j++ {
				if at(j) == "<" {
					j = skip(j, "<", ">") - 1
				} else if at(j).isIdent() && base == "" && at(j+1) != "::" && at(j) != "public" && at(j) != "protected" && at(j) != "private" && at(j) != "virtual" {
					base = string(at(j))
				}
			}
			if at(j) == "{" {
				return &cppDecl{name: string(name), base: base}, j
			}
			return nil, j
		}
		// A forward declaration, a specialization or a variable of this type: no new name.
		return endStatement(tokens, j, at, skip)
	case t == "using":
		if at(i+1).isIdent() && at(i+2) == "=" {
			_, next := endStatement(tokens, i, at, skip)
			return &cppDecl{name: string(at(i + 1))}, next
		}
		return endStatement(tokens, i, at, skip)
	case t == "typedef":
		j := i + 1
		name := cppToken("")
		for ; at(j) != ";" && at(j) != ""; j++ {
			switch {
			case at(j) == "(" && at(j+1) == "*" && at(j+2).isIdent():
				name = at(j + 2) // A function pointer: typedef void (*Name)(...);
				j = skip(j, "(", ")") - 1
			case at(j) == "(" || at(j) == "[" || at(j) == "<":
				j = skip(j, at(j), map[cppToken]cppToken{"(": ")", "[": "]", "<": ">"}[at(j)]) - 1
			case at(j).isIdent() && name == "" || at(j).isIdent() && at(j+1) == ";":
				name = at(j)
			}
		}
		if name.isIdent() {
			return &cppDecl{name: string(name)}, j + 1
		}
		return nil, j + 1
	case t == "extern" && at(i+1) == "0":
		return nil, i + 2 // extern "C": its braces are a scope.
	case t == "static_assert" || t == "friend" || t == "public" || t == "private" || t == "protected":
		return endStatement(tokens, i, at, skip)
	case t.isMacroName() && at(i+1) == "(":
		j := skip(i+1, "(", ")")
		if at(j) == ";" {
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
	for ; at(j) != ";" && at(j) != "{" && at(j) != "=" && at(j) != ""; j++ {
		switch at(j) {
		case "operator":
			return endStatement(tokens, j, at, skip) // An operator overload: no name to include.
		case "(":
			if !isFunc && name == "" && at(j-1).isIdent() && at(j-2) != "::" && !at(j-1).isMacroName() && at(j-1) != "operator" {
				name = at(j - 1)
			}
			isFunc = true
			j = skip(j, "(", ")") - 1
		case "<":
			j = skip(j, "<", ">") - 1
		case "[":
			if !isFunc && name == "" && at(j-1).isIdent() && at(j-2) != "::" {
				name = at(j - 1)
			}
			j = skip(j, "[", "]") - 1
		}
	}
	if !isFunc && name == "" && at(j-1).isIdent() && at(j-2) != "::" && j > i {
		name = at(j - 1)
	}
	next := j
	if at(j) == "=" {
		_, next = endStatement(tokens, j, at, skip)
	} else if at(j) == ";" {
		next = j + 1
	}
	if name.isIdent() {
		return &cppDecl{name: string(name)}, next
	}
	return nil, next
}

// gdclass is a class registered with GDCLASS(Name, Base).
type gdclass struct {
	name, base string // name is qualified with its namespaces, e.g. "core_bind::OS"
}

// scanGdclasses returns the classes that the header src registers with GDCLASS.
func scanGdclasses(src string) []gdclass {
	tokens := tokenizeCpp(src)
	var classes []gdclass
	var scopes []string // namespace names, "" for other braces
	for i := 0; i < len(tokens); i++ {
		switch t := tokens[i]; {
		case t == "namespace":
			j := i + 1
			var name []string
			for ; j < len(tokens) && (tokens[j].isIdent() || tokens[j] == "::"); j++ {
				if tokens[j] != "::" {
					name = append(name, string(tokens[j]))
				}
			}
			if j < len(tokens) && tokens[j] == "{" {
				scopes = append(scopes, strings.Join(name, "::"))
				i = j
			}
		case t == "{":
			scopes = append(scopes, "")
		case t == "}":
			if len(scopes) > 0 {
				scopes = scopes[:len(scopes)-1]
			}
		case t == "GDCLASS" && i+2 < len(tokens) && tokens[i+1] == "(" && tokens[i+2].isIdent():
			qualified := slices.DeleteFunc(slices.Clone(scopes), func(s string) bool { return s == "" })
			base := ""
			for j := i + 3; j < len(tokens) && tokens[j] != ")"; j++ {
				if tokens[j].isIdent() {
					base = string(tokens[j]) // The last identifier, e.g. "Object" in core_bind::Object.
				}
			}
			classes = append(classes, gdclass{strings.Join(append(qualified, string(tokens[i+2])), "::"), base})
		}
	}
	return classes
}

// endStatement returns the index after the ";" that ends the statement at
// tokens[i], skipping brackets, or of the "{" of a body.
func endStatement(tokens []cppToken, i int, at func(int) cppToken, skip func(int, cppToken, cppToken) int) (*cppDecl, int) {
	for j := i; j < len(tokens); j++ {
		switch at(j) {
		case ";":
			return nil, j + 1
		case "{":
			if at(j-1) == "=" || at(j-1) == "," || at(j-1) == "(" {
				j = skip(j, "{", "}") - 1 // A braced initializer.
				continue
			}
			return nil, j
		case "(":
			j = skip(j, "(", ")") - 1
		}
	}
	return nil, len(tokens)
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
			for _, d := range scanCppDecls(src, "godot") {
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
// path relative to root, skipping directories named in skip.
func walkHeaders(root Path, visit func(rel, src string), skip ...string) {
	var walk func(dir Path, rel string)
	walk = func(dir Path, rel string) {
		for _, child := range dir.Ls() {
			childRel := path.Join(rel, child.Name())
			switch {
			case child.IsDir():
				if !slices.Contains(skip, child.Name()) {
					walk(child, childRel)
				}
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
