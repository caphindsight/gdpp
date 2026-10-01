// highlight.go: syntax highlighting of code, for the manual and trans.

package main

import "strings"

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
	}
	return code
}

// The words that highlightGdpp marks, by kind. Add new words to these lists.
const (
	gdppWords = "class class_name ctor decl dtor enum enum_name extends extern extern_name func get impl import noimport set signal var"
	cppWords  = "if else for while do return switch case break continue default new delete auto const static constexpr namespace using " +
		"typedef template typename public private protected virtual override struct true false nullptr this sizeof operator inline explicit mutable"
	gdscriptWords = "pass and or not in"
	// GD++'s words in C++ code that are keywords wherever they appear.
	rewriteWords = "emit rpc is_cancelled"
	// GD++'s words in C++ code that are also method names, e.g. in task.is_done(): they're keywords only where a name
	// follows them, which is where GD++ rewrites them.
	rewriteOperatorWords = "is_done claim cancel"
	// The runtime's cast, and C++'s casts, which it replaces.
	castWords    = "cast static_cast dynamic_cast const_cast reinterpret_cast"
	cppTypeWords = "bool int float void char double long short unsigned signed size_t int8_t int16_t int32_t int64_t uint8_t uint16_t " +
		"uint32_t uint64_t"
	// Godot's names for types that aren't written in PascalCase, which is how highlightGdpp spots other types.
	godotTypeWords = "float64_t real_t gd RID AABB"
)

var (
	codeKeywords  = wordSet(gdppWords, cppWords, gdscriptWords, rewriteWords, castWords)
	codeOperators = wordSet(rewriteOperatorWords)
	codeTypes     = wordSet(cppTypeWords, godotTypeWords)
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
	var out strings.Builder
	for i := 0; i < len(code); {
		c, rest := code[i], code[i:]
		n, style := 1, []Style(nil)
		switch {
		case strings.HasPrefix(rest, "//") || gdscript && c == '#':
			n, style = lineEnd(rest), []Style{CodeComment}
		case strings.HasPrefix(rest, "/*"):
			for depth := 0; n < len(rest); n++ { // Block comments nest, like in GD++.
				if strings.HasPrefix(rest[n-1:], "/*") {
					depth, n = depth+1, n+1
				} else if strings.HasPrefix(rest[n-1:], "*/") {
					if depth, n = depth-1, n+1; depth == 0 {
						break
					}
				}
			}
			n, style = min(n, len(rest)), []Style{CodeComment}
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
		case c >= '0' && c <= '9':
			for n < len(rest) && (isIdentByte(rest[n]) || rest[n] == '.' || rest[n] == '\'') {
				n++
			}
			style = []Style{CodeLiteral}
		case identLen(rest) > 0:
			n = identLen(rest)
			word := rest[:n]
			switch {
			case codeKeywords[word], codeOperators[word] && identLen(strings.TrimLeft(rest[n:], " \t\n")) > 0:
				style = []Style{CodeKeyword}
			case codeTypes[word] || word[0] >= 'A' && word[0] <= 'Z' && strings.ToUpper(word) != word:
				style = []Style{CodeType}
			case strings.HasPrefix(strings.TrimLeft(rest[n:], " "), "("):
				style = []Style{CodeFunction}
			}
		}
		out.WriteString(styledLines(rest[:n], style...))
		i += n
	}
	return out.String()
}

// identLen returns the length of the identifier at the start of s.
func identLen(s string) int {
	n := 0
	for n < len(s) && isIdentByte(s[n]) && (n > 0 || s[0] < '0' || s[0] > '9') {
		n++
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
