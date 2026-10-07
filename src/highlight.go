// highlight.go: syntax highlighting of code, for the manual, trans and cat.

package main

import (
	"fmt"
	"regexp"
	"strings"
)

// styledLines is Styled, applied to each line of text on its own, so the
// pager can end styles at the end of each line.
func styledLines(text string, styles ...Style) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = Styled(line, styles...)
	}
	return strings.Join(lines, "\n")
}

// highlightCode returns the code block code, in the language lang, with
// syntax highlighting if styles are on.
func highlightCode(code, lang string) string {
	switch {
	case !isTTY:
		return code
	case lang == "sh" || lang == "out" || lang == "toml":
		lines := strings.Split(code, "\n")
		for i, line := range lines {
			lines[i] = map[string]func(string) string{"sh": highlightShell, "out": highlightOutput, "toml": highlightToml}[lang](line)
		}
		return strings.Join(lines, "\n")
	case lang == "gd++" || lang == "cpp" || lang == "gdscript":
		return highlightGdpp(code, lang == "gdscript")
	case lang == "lua":
		return highlightLua(code)
	}
	return code
}

// The words that highlightGdpp marks, by kind. Add new words to these lists.
const (
	gdppWords = "annotation class class_name ctor decl dtor enum enum_name extends extern extern_name func get impl implements import macro macro_library macro_name noimport " +
		"set shader shader_library signal template_name trait trait_name var"
	// GD++'s on blocks, e.g. on ready { ... }, whose keyword is also a name elsewhere: it's a keyword, with the
	// notification's name, where it starts a block, at the start of a line or after annotations, and alone where
	// another identifier follows it, e.g. in a list of keywords, since a name never has one right after it.
	onWord   = "on"
	cppWords = "if else for while do return switch case break continue default auto const static constexpr namespace using " +
		"typedef template typename public private protected virtual override struct true false nullptr this sizeof operator inline explicit mutable"
	gdscriptWords = "pass and or not in"
	// Lua's keywords, in the Lua code of GD++ macros.
	luaWords = "and break do else elseif end false for function goto if in local nil not or repeat return then true until while"
	// What macros see in Lua, besides their parameters.
	luaGlobalWords = "gd ctx"
	// GD++'s rewrites in C++ code that are keywords wherever they appear.
	rewriteWords = "emit rpc is_cancelled assert assert_void assert_val"
	// GD++'s rewrites in C++ code that are also method or variable names, e.g. in task.is_done(): they're keywords only
	// where a name follows them, which is where GD++ rewrites them.
	rewriteOperatorWords = "is_done claim cancel as create destroy queue_destroy"
	// GD++'s invocations of macros and templates, e.g. invoke log("hit"). C++ code may also use it as a name, e.g. in
	// std::invoke(f), so it's a keyword only where a name follows it, like the words above.
	invokeWord = "invoke"
	// GD++'s rewrites in C++ code that are keywords only where a string follows them, which is where GD++ rewrites them.
	rewriteStringWords = "string_name"
	// C++'s and godot-cpp's ways to create and delete objects, which stay plain, so that GD++'s create and destroy stand
	// out as the way to do it.
	plainWords = "new delete memnew memdelete"
	// The runtime's one type for objects, e.g. Gd<Node3D>: a keyword, so that it stands out from the class it holds.
	gdWord = "Gd"
	// The runtime's cast, which as becomes, and C++'s casts, which it replaces.
	castWords    = "cast static_cast dynamic_cast const_cast reinterpret_cast"
	cppTypeWords = "bool int float void char double long short unsigned signed size_t int8_t int16_t int32_t int64_t uint8_t uint16_t " +
		"uint32_t uint64_t"
	// Godot's names for types that aren't written in PascalCase, which is how highlightGdpp spots other types.
	godotTypeWords = "float64_t real_t gd RID AABB"
	// GLSL's types, in shaders and shader blocks.
	glslTypeWords = "uint vec2 vec3 vec4 ivec2 ivec3 ivec4 uvec2 uvec3 uvec4 bvec2 bvec3 bvec4 mat2 mat3 mat4 mat2x2 mat2x3 mat2x4 mat3x2 " +
		"mat3x3 mat3x4 mat4x2 mat4x3 mat4x4 sampler2D image2D"
	// GLSL's keywords and qualifiers, and the cell of a shader's body, in shaders and shader blocks.
	glslWords = "if else for while do return switch case break continue default const struct true false discard in out inout " +
		"uniform buffer shared layout highp mediump lowp precise coherent volatile restrict readonly writeonly id"
	// The formats of images and textures that shaders write, which are types in brackets, e.g. Texture2D[rgba8].
	gpuFormatWords = "r8 rg8 rgb8 rgba8 rf rgf rgbh rgbah rgbf rgbaf"
)

var (
	codeKeywords  = wordSet(gdppWords, cppWords, gdscriptWords, rewriteWords, gdWord, castWords)
	codeOperators = wordSet(rewriteOperatorWords, invokeWord)
	codeStringOps = wordSet(rewriteStringWords)
	codeTypes     = wordSet(cppTypeWords, godotTypeWords)
	codePlain     = wordSet(plainWords)
	luaKeywords   = wordSet(luaWords)
	luaGlobals    = wordSet(luaGlobalWords)
	glslKeywords  = wordSet(glslWords)
	glslTypes     = wordSet(glslTypeWords, cppTypeWords)
	gpuFormats    = wordSet(gpuFormatWords)
)

// wordSet returns the set of the words in lists, which are separated by spaces.
func wordSet(lists ...string) map[string]bool {
	set := map[string]bool{}
	for _, list := range lists {
		for _, w := range strings.Fields(list) {
			set[w] = true
		}
	}
	return set
}

// highlightGdpp highlights GD++ or C++ code, or with gdscript, GDScript code,
// where # starts a comment rather than a preprocessor directive: keywords,
// annotations, types, function names, literals and comments.
func highlightGdpp(code string, gdscript bool) string {
	return highlightCodeOf(code, gdscript, false)
}

// shaderHeadRegexp matches the start of a shader block, "shader {", or of a shader, "shader NAME(".
var shaderHeadRegexp = regexp.MustCompile(`^shader(\s*\{|\s+\w+\s*\()`)

// shaderLibraryRegexp matches a file-level shader library's keyword, which only starts one before any declaration.
var shaderLibraryRegexp = regexp.MustCompile(`^shader_library\b`)

// highlightCodeOf highlights code like highlightGdpp does, or with glsl, the GLSL of shaders: with GLSL's keywords
// and types, and without GD++'s rewrites.
func highlightCodeOf(code string, gdscript, glsl bool) string {
	var out strings.Builder
	for i := 0; i < len(code); {
		c, rest := code[i], code[i:]
		n, style := 1, []Style(nil)
		switch {
		case strings.HasPrefix(rest, "//") || gdscript && c == '#':
			n, style = lineEnd(rest), []Style{CodeComment}
		case strings.HasPrefix(rest, "/*"):
			n, style = blockCommentLen(rest), []Style{CodeComment}
		case c == '#':
			n, style = 1+identLen(strings.TrimLeft(rest[1:], " "))+len(rest[1:])-len(strings.TrimLeft(rest[1:], " ")), []Style{CodePreProc}
		case c == '"' || c == '\'':
			for n < lineEnd(rest) && rest[n] != c {
				if rest[n] == '\\' {
					n++
				}
				n++
			}
			n, style = min(n+1, lineEnd(rest)), []Style{CodeLiteral}
		case c == '@' && identLen(rest[1:]) > 0:
			n, style = 1+identLen(rest[1:]), []Style{CodePreProc}
		case !gdscript && strings.HasPrefix(rest, "@@") && identLen(rest[2:]) > 0: // A user annotation.
			n, style = 2+identLen(rest[2:]), []Style{CodePreProc}
		case !gdscript && strings.HasPrefix(rest, "${"): // A template's hole, which holds Lua.
			end := closingBrace(rest, 1)
			out.WriteString(Styled("${", CodePreProc) + highlightLua(rest[2:end]) + styledLines(rest[end:min(end+1, len(rest))], CodePreProc))
			i += min(end+1, len(rest))
			continue
		case !gdscript && macroHeadRegexp.MatchString(rest):
			out.WriteString(highlightMacro(rest, &i))
			continue
		case !gdscript && macroBlockRegexp.MatchString(rest):
			m := macroBlockRegexp.FindStringSubmatch(rest)
			end := closingBrace(rest, len(m[0])-1)
			out.WriteString(Styled(m[1], CodeKeyword) + m[0][len(m[1]):] + highlightLua(rest[len(m[0]):end]) + rest[end:min(end+1, len(rest))])
			i += min(end+1, len(rest))
			continue
		case !gdscript && macroLibraryRegexp.MatchString(rest) && onlyComments(code[:i]): // The rest of the file is Lua.
			out.WriteString(Styled("macro_library", CodeKeyword) + highlightLua(rest[len("macro_library"):]))
			i += len(rest)
			continue
		case !gdscript && !glsl && shaderLibraryRegexp.MatchString(rest) && onlyComments(code[:i]): // The rest of the file is GLSL.
			out.WriteString(Styled("shader_library", CodeKeyword) + highlightCodeOf(rest[len("shader_library"):], false, true))
			i += len(rest)
			continue
		case !gdscript && !glsl && shaderHeadRegexp.MatchString(rest) && (i == 0 || !isIdentByte(code[i-1])) && strings.IndexByte(rest, '{') >= 0:
			// A shader or a shader block: its head is GD++, and its body GLSL.
			open := strings.IndexByte(rest, '{')
			end := closingBrace(rest, open)
			out.WriteString(Styled("shader", CodeKeyword) + highlightCodeOf(rest[len("shader"):open+1], false, false) +
				highlightCodeOf(rest[open+1:end], false, true) + rest[end:min(end+1, len(rest))])
			i += min(end+1, len(rest))
			continue
		case c == '$' || c == '%' && strings.HasSuffix(strings.TrimRight(code[:i], " \t"), "="):
			n, style = nodePathLen(rest), []Style{CodeLiteral}
		case c >= '0' && c <= '9':
			for n < len(rest) && (isIdentByte(rest[n]) || rest[n] == '.' || rest[n] == '\'') {
				n++
			}
			style = []Style{CodeLiteral}
		case identLen(rest) > 0:
			n = identLen(rest)
			word := rest[:n]
			isOn := false
			if word == onWord && annotationsRegexp.MatchString(code[strings.LastIndexByte(code[:i], '\n')+1:i]) {
				if m := onHeadRegexp.FindStringSubmatch(rest[n:]); m != nil {
					n, isOn = n+len(m[1]), true // With the notification's name.
				}
			}
			if word == onWord && !isOn {
				after := strings.TrimLeft(rest[n:], " \t")
				isOn = len(after) < len(rest[n:]) && identLen(after) > 0
			}
			after := strings.TrimLeft(rest[n:], " \t\n")
			if m := invokeRegexp.FindStringSubmatch(rest); !gdscript && !glsl && m != nil && strings.ContainsAny(rest[len(m[0]):min(len(m[0])+1, len(rest))], "({") {
				// A macro invocation, maybe with a Lua table.
				out.WriteString(Styled(m[1], CodeKeyword) + m[2] + Styled(m[3], CodeFunction) + m[4])
				i += len(m[0])
				if strings.HasPrefix(rest[len(m[0]):], "{") {
					end := closingBrace(rest, len(m[0]))
					out.WriteString("{" + highlightLua(rest[len(m[0])+1:end]) + rest[end:min(end+1, len(rest))])
					i += min(end+1, len(rest)) - len(m[0])
				}
				continue
			}
			switch {
			case glsl && glslKeywords[word]:
				style = []Style{CodeKeyword}
			case glsl && glslTypes[word]:
				style = []Style{CodeType}
			case glsl && strings.HasPrefix(strings.TrimLeft(rest[n:], " "), "("):
				style = []Style{CodeFunction}
			case glsl:
			case gpuFormats[word] && strings.HasSuffix(strings.TrimRight(code[:i], " "), "["):
				style = []Style{CodeType}
			case !gdscript && word == "code" && strings.HasPrefix(after, "{"):
				style = []Style{CodeKeyword}
			case isOn, codeKeywords[word], codeOperators[word] && identLen(strings.TrimLeft(rest[n:], " \t\n")) > 0,
				codeStringOps[word] && strings.HasPrefix(strings.TrimLeft(rest[n:], " \t\n"), "\""):
				style = []Style{CodeKeyword}
			case codePlain[word]:
			case codeTypes[word] || word[0] >= 'A' && word[0] <= 'Z' && strings.ToUpper(word) != word:
				style = []Style{CodeType}
			case strings.HasPrefix(strings.TrimLeft(rest[n:], " "), "(") || lastWord(code[:i]) == "signal":
				style = []Style{CodeFunction}
			}
		}
		out.WriteString(styledLines(rest[:n], style...))
		i += n
	}
	return out.String()
}

// invokeRegexp matches the start of a macro invocation: "invoke" and a name, which "(" or "{" follows.
var invokeRegexp = regexp.MustCompile(`^(invoke)(\s+)(\w+)(\s*)`)

// macroHeadRegexp matches the start of a macro or template: its keyword and name, up to "(".
var macroHeadRegexp = regexp.MustCompile(`^(macro|template|macro_name|template_name)(\s+)(\w+)(\s*)\(`)

// macroBlockRegexp matches the start of a macro block or macro library: "invoke" or "macro", and "{".
var macroBlockRegexp = regexp.MustCompile(`^(invoke|macro)\s*\{`)

// macroLibraryRegexp matches a file-level macro library's keyword, which only starts one before any declaration.
var macroLibraryRegexp = regexp.MustCompile(`^macro_library\b`)

// highlightMacro highlights the macro or template that starts code[*i:], and moves *i past it: its head, and its
// body, which is Lua for macros and GD++ for templates. A file-level one's body is the rest of the file.
func highlightMacro(rest string, i *int) string {
	m := macroHeadRegexp.FindStringSubmatch(rest)
	open := len(m[0]) - 1
	end := closingBrace(rest, open)
	var out strings.Builder
	out.WriteString(Styled(m[1], CodeKeyword) + m[2] + Styled(m[3], CodeFunction) + m[4] + "(" + highlightLua(rest[open+1:end]))
	n := min(end+1, len(rest))
	out.WriteString(rest[end:n])
	lua := strings.HasPrefix(m[1], "macro")
	if strings.HasSuffix(m[1], "_name") {
		if lua {
			out.WriteString(highlightLua(rest[n:]))
		} else {
			out.WriteString(highlightGdpp(rest[n:], false))
		}
		*i += len(rest)
		return out.String()
	}
	gap := len(rest[n:]) - len(strings.TrimLeft(rest[n:], " \t\n"))
	if !strings.HasPrefix(rest[n+gap:], "{") {
		*i += n
		return out.String()
	}
	open = n + gap
	end = closingBrace(rest, open)
	body := rest[open+1 : end]
	if lua {
		body = highlightLua(body)
	} else {
		body = highlightGdpp(body, false)
	}
	out.WriteString(rest[n:open+1] + body + rest[end:min(end+1, len(rest))])
	*i += min(end+1, len(rest))
	return out.String()
}

// closingBrace returns the index in s of the bracket that closes the one at open, skipping strings, or len(s).
func closingBrace(s string, open int) int {
	closer := map[byte]byte{'(': ')', '{': '}', '[': ']'}[s[open]]
	for i, depth := open, 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"' || c == '\'':
			for i++; i < len(s) && s[i] != c && s[i] != '\n'; i++ {
				if s[i] == '\\' {
					i++
				}
			}
		case c == s[open]:
			depth++
		case c == closer:
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return len(s)
}

// highlightLua highlights the Lua code of GD++ macros: keywords, gd and ctx, function names, literals and comments,
// Lua's and GD++'s, and the templates declared inside.
func highlightLua(code string) string {
	var out strings.Builder
	for i := 0; i < len(code); {
		c, rest := code[i], code[i:]
		n, style := 1, []Style(nil)
		switch {
		case strings.HasPrefix(rest, "--") && longBracket(rest[2:]) > 0:
			n, style = 2+longBracketEnd(rest[2:]), []Style{CodeComment}
		case strings.HasPrefix(rest, "--") || strings.HasPrefix(rest, "//"):
			n, style = lineEnd(rest), []Style{CodeComment}
		case strings.HasPrefix(rest, "/*"):
			n, style = blockCommentLen(rest), []Style{CodeComment}
		case longBracket(rest) > 0:
			n, style = longBracketEnd(rest), []Style{CodeLiteral}
		case c == '"' || c == '\'':
			for n < lineEnd(rest) && rest[n] != c {
				if rest[n] == '\\' {
					n++
				}
				n++
			}
			n, style = min(n+1, lineEnd(rest)), []Style{CodeLiteral}
		case c >= '0' && c <= '9':
			for n < len(rest) && (isIdentByte(rest[n]) || rest[n] == '.') {
				n++
			}
			style = []Style{CodeLiteral}
		case macroHeadRegexp.MatchString(rest) && strings.HasPrefix(rest, "template "):
			out.WriteString(highlightMacro(rest, &i))
			continue
		case identLen(rest) > 0:
			n = identLen(rest)
			word, before := rest[:n], strings.TrimRight(code[:i], " \t")
			switch {
			case luaKeywords[word]:
				style = []Style{CodeKeyword}
			case luaGlobals[word] && !strings.HasSuffix(before, "."):
				style = []Style{CodeType}
			case strings.HasPrefix(strings.TrimLeft(rest[n:], " "), "(") || strings.HasPrefix(strings.TrimLeft(rest[n:], " "), "{") ||
				strings.HasSuffix(before, ".") && lastWord(before[:len(before)-1]) == "gd":
				style = []Style{CodeFunction}
			}
		}
		out.WriteString(styledLines(rest[:n], style...))
		i += n
	}
	return out.String()
}

// blockCommentLen returns the length of the block comment that opens s. Block comments nest, like in GD++.
func blockCommentLen(s string) int {
	n := 2
	for depth := 1; n < len(s) && depth > 0; n++ {
		if strings.HasPrefix(s[n:], "/*") {
			depth, n = depth+1, n+1
		} else if strings.HasPrefix(s[n:], "*/") {
			depth, n = depth-1, n+1
		}
	}
	return min(n, len(s))
}

// onlyComments reports whether s holds only whitespace and comments, like the code before a file's head, e.g.
// macro_library.
func onlyComments(s string) bool {
	for i := 0; i < len(s); {
		switch rest := s[i:]; {
		case strings.HasPrefix(rest, "//"):
			i += lineEnd(rest)
		case strings.HasPrefix(rest, "/*"):
			i += blockCommentLen(rest)
		case strings.ContainsRune(" \t\r\n", rune(s[i])):
			i++
		default:
			return false
		}
	}
	return true
}

// longBracket returns the length of the Lua long bracket that opens s, e.g. 2 for "[[" or 4 for "[==[", or 0.
func longBracket(s string) int {
	n := 1
	for n < len(s) && s[n] == '=' {
		n++
	}
	if len(s) > n && s[0] == '[' && s[n] == '[' {
		return n + 1
	}
	return 0
}

// longBracketEnd returns the length of the Lua long string or comment body that opens s, up to its closing bracket.
func longBracketEnd(s string) int {
	open := longBracket(s)
	closer := "]" + strings.Repeat("=", open-2) + "]"
	if end := strings.Index(s[open:], closer); end >= 0 {
		return open + end + len(closer)
	}
	return len(s)
}

var (
	annotationsRegexp = regexp.MustCompile(`^\s*(@@?\w+(\([^)]*\))?\s*)*$`)
	// What follows "on" in an on block's head: the notification's name, if any, then the parameter, if any, and "{".
	onHeadRegexp = regexp.MustCompile(`^(\s+\w+)?\s*(\(\s*\w+\s*(:\s*\w+\s*)?\)\s*)?\{`)
)

// lastWord returns the identifier at the end of s, before any spaces, e.g. signal in "signal ".
func lastWord(s string) string {
	s = strings.TrimRight(s, " \t")
	n := len(s)
	for n > 0 && isIdentByte(s[n-1]) {
		n--
	}
	return s[n:]
}

// identLen returns the length of the identifier at the start of s.
func identLen(s string) int {
	n := 0
	for n < len(s) && isIdentByte(s[n]) && (n > 0 || s[0] < '0' || s[0] > '9') {
		n++
	}
	return n
}

// nodePathLen returns the length of the node path at the start of s, e.g. $Hud/"Score Label" or %Health.
func nodePathLen(s string) int {
	n := 1
	for n < len(s) {
		switch c := s[n]; {
		case c == '"' || c == '\'':
			end := n + 1
			for end < lineEnd(s) && s[end] != c {
				if s[end] == '\\' {
					end++
				}
				end++
			}
			n = min(end+1, lineEnd(s))
		case c == '/' && n+1 < len(s) && (s[n+1] == '/' || s[n+1] == '*'): // A comment.
			return n
		case c == '/' || c == '%' || c >= 0x80 || isIdentByte(c):
			n++
		default:
			return n
		}
	}
	return n
}

// lineEnd returns the index of the end of the first line of s.
func lineEnd(s string) int {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return i
	}
	return len(s)
}

// highlightShell highlights a shell command line: commands, options, strings
// and comments.
func highlightShell(line string) string {
	var out strings.Builder
	command := !strings.HasPrefix(line, " ")
	for i := 0; i < len(line); {
		rest := line[i:]
		n := strings.IndexAny(rest, " ")
		if n < 0 {
			n = len(rest)
		}
		word, bare := rest[:n], strings.TrimLeft(rest[:n], "[(|")
		switch {
		case n == 0:
			n = len(rest) - len(strings.TrimLeft(rest, " "))
			out.WriteString(rest[:n])
		case word[0] == '#':
			n = len(rest)
			out.WriteString(Styled(rest, CodeComment))
		case word[0] == '"':
			n = strings.IndexByte(rest[1:], '"') + 2
			if n == 1 {
				n = len(rest)
			}
			out.WriteString(Styled(rest[:n], CodeLiteral))
		case strings.HasPrefix(bare, "-"):
			option := strings.TrimRight(bare, "])|,")
			out.WriteString(word[:len(word)-len(bare)] + Styled(option, Cyan) + bare[len(option):])
			command = false
		case word == "|" || word == "&&":
			out.WriteString(word)
			command = true
		case command:
			out.WriteString(Styled(word, Bold))
			command = word == "gd++" || word == "sudo"
		default:
			out.WriteString(word)
		}
		i += n
	}
	return out.String()
}

// highlightOutput highlights a line of gd++'s output: the icons of messages,
// hints, and the source excerpts of errors.
func highlightOutput(line string) string {
	body := strings.TrimLeft(line, " ")
	indent := line[:len(line)-len(body)]
	icons := map[string][]Style{"[•]": {Green}, "[!]": {Bold, Yellow}, "[×]": {Bold, Red}, "[?]": {Bold, Magenta}}
	for icon, styles := range icons {
		if rest, ok := strings.CutPrefix(body, icon); ok {
			return indent + Styled(icon, styles...) + rest
		}
	}
	if rest, ok := strings.CutPrefix(body, "Hint:"); ok {
		return indent + Styled("Hint:", Bold) + rest
	}
	if gutter, source, ok := strings.Cut(body, "| "); ok && strings.Trim(gutter, " 0123456789") == "" {
		if strings.Trim(source, " ^") == "" {
			source = Styled(source, Red)
		}
		return indent + Styled(gutter+"|", Gray) + " " + source
	}
	return line
}

// highlightToml highlights a line of TOML: tables, keys, values and comments.
func highlightToml(line string) string {
	key, value, ok := strings.Cut(line, " = ")
	switch {
	case strings.HasPrefix(line, "["):
		return Styled(line, Bold)
	case ok:
		value, comment, _ := strings.Cut(value, "#")
		if comment != "" {
			comment = Styled("#"+comment, CodeComment)
		}
		trimmed := strings.TrimRight(value, " ")
		return Styled(key, Cyan) + " = " + Styled(trimmed, CodeLiteral) + value[len(trimmed):] + comment
	}
	return line
}

// svgColors are the SVG colors of the highlighting styles, on a dark background.
var svgColors = map[string]string{
	"":                   "#e6edf3",
	string(CodeKeyword):  "#e3b341",
	string(CodeType):     "#7ee787",
	string(CodeFunction): "#56d4dd",
	string(CodeLiteral):  "#d2a8ff",
	string(CodeComment):  "#8b949e",
	string(CodePreProc):  "#79c0ff",
}

// codeSVG renders code, in the language lang, highlighted as an SVG image.
func codeSVG(code, lang string) string {
	// Control characters, e.g. \r or the file's own ANSI codes, are invalid in SVG and would be mistaken for styles.
	code = strings.Map(func(r rune) rune {
		if r < ' ' && r != '\n' && r != '\t' || r == 0x7f {
			return -1
		}
		return r
	}, strings.ToValidUTF8(code, ""))
	tty := isTTY
	isTTY = true // Styled styles only on terminals.
	code = expandTabs(highlightCode(strings.TrimRight(code, "\n"), lang), Args.TabWidth)
	isTTY = tty
	const fontSize, lineHeight, pad = 14, 20, 16
	lines, cols := strings.Split(code, "\n"), 0
	var body strings.Builder
	for i, line := range lines {
		cols = max(cols, visibleLen(line))
		fmt.Fprintf(&body, "  <text x=\"%d\" y=\"%d\">", pad, pad+fontSize+i*lineHeight)
		for j, part := range strings.Split(line, "\x1b[") {
			style, text := "", part
			if j > 0 {
				style, text, _ = strings.Cut(part, "m")
				if style == "0" {
					style = ""
				}
			}
			// Runs flow one after another, so the font's own spacing applies, with no gaps between them.
			// Spaces are non-breaking, since renderers collapse plain ones, e.g. the indentation.
			if text != "" {
				escaped := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", " ", "\u00a0").Replace(text)
				fmt.Fprintf(&body, "<tspan fill=\"%s\">%s</tspan>", svgColors[style], escaped)
			}
		}
		body.WriteString("</text>\n")
	}
	width, height := 2*pad+cols*fontSize*6/10+1, 2*pad+len(lines)*lineHeight-(lineHeight-fontSize)/2
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="100%%" height="100%%" rx="8" fill="#0d1117"/>
  <g font-family="ui-monospace, SFMono-Regular, Menlo, Consolas, 'Liberation Mono', monospace" font-size="%d">
%s  </g>
</svg>
`, width, height, width, height, fontSize, body.String())
}
