package main

import (
	"embed"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/term"
)

// CmdMan shows a page of the reference manual of GD++, the build tool and
// the language, in the pager. Without a page, it lists them all, as a tree.
type CmdMan struct {
	Page string `arg:"positional" help:"the page to show, by name or path, e.g. functions or lang/functions [default: list all pages]"`
}

// manFiles holds the manual's pages. The first line of a page is its title.
//
//go:embed man
var manFiles embed.FS

// manPages lists the paths of the manual's pages, in reading order. The page
// at a path is man/<path>.txt. A page's children are the pages under its
// path, e.g. lang/functions under lang, and come right after it. The last
// part of a path is the page's name, which is unique.
var manPages = []string{
	"intro", "tour",
	"tool", "tool/cli", "tool/paths", "tool/projects", "tool/packages", "tool/dependencies", "tool/building", "tool/build-cache",
	"tool/cpp-classes", "tool/vcs", "tool/shipping",
	"cmd", "cmd/build", "cmd/checkin", "cmd/clean", "cmd/doc", "cmd/fetch", "cmd/fix", "cmd/init", "cmd/install", "cmd/ls", "cmd/man",
	"cmd/rm", "cmd/trans", "cmd/vendor",
	"lang", "lang/syntax", "lang/files", "lang/classes", "lang/types", "lang/functions", "lang/variables", "lang/exports", "lang/signals",
	"lang/enums", "lang/lifecycle", "lang/rpc", "lang/externs", "lang/code", "lang/includes", "lang/annotations", "lang/docs",
	"lang/runtime", "lang/generated", "lang/errors", "lang/grammar",
}

func (c *CmdMan) Run() {
	if c.Page == "" {
		PageResult("The reference manual of GD++. Run `gd++ man PAGE` to read a page.\n\n" + manContents(""))
		return
	}
	i := slices.IndexFunc(manPages, func(p string) bool { return p == c.Page || path.Base(p) == c.Page })
	if i < 0 {
		var similar []string
		for _, p := range manPages {
			name, page := path.Base(p), strings.ToLower(c.Page)
			if strings.Contains(name, page) || strings.Contains(page, name) {
				similar = append(similar, name)
			}
		}
		Assert(len(similar) == 0, "There is no manual page %s. Similar pages: %s.", c.Page, strings.Join(similar, ", "))
		LogFatal("There is no manual page %s, run `gd++ man` to list them.", c.Page)
	}
	width := 0
	if isTTY {
		width, _, _ = term.GetSize(int(os.Stdout.Fd()))
	}
	text := renderMan(manPage(manPages[i]), width)
	if children := manContents(manPages[i]); children != "" {
		text += "\n" + Styled("Pages in this section:", Bold) + "\n" + children
	}
	PageResult(text)
}

// manPage returns the text of the page at path p.
func manPage(p string) string {
	text, err := manFiles.ReadFile("man/" + p + ".txt")
	Check(err, "Failed to read the manual page %s", p)
	return string(text)
}

// manContents lists the pages under the path parent, or all pages if it's
// empty, each with its title, indented by its depth below parent.
func manContents(parent string) string {
	var rows [][]string
	for _, p := range manPages {
		rel, ok := strings.CutPrefix(p, parent+"/")
		if parent == "" {
			rel, ok = p, true
		}
		if !ok {
			continue
		}
		title, _, _ := strings.Cut(manPage(p), "\n")
		name := strings.Repeat("  ", strings.Count(rel, "/")) + path.Base(p)
		if strings.Contains(rel, "/") {
			name = Styled(name, Cyan)
		} else {
			name, title = Styled(name, Bold, Cyan), Styled(title, Bold)
		}
		rows = append(rows, []string{name, title})
	}
	if len(rows) == 0 {
		return ""
	}
	return AlignColumns(rows, "  ")
}

// A page is plain text with a little markup:
//   - The first line is the title.
//   - "# " starts a heading.
//   - A line followed by a line indented by 2 spaces is a term, e.g. an option,
//     and the indented lines describe it.
//   - "- " and "1. " start list items.
//   - `...` is inline code.
//   - A line "```LANG" starts a code block, and a line "```" ends it. LANG is
//     gd++, cpp, gdscript, sh, toml, out (gd++'s output), or empty for plain
//     text.

var manItemRegexp = regexp.MustCompile(`^(-|[0-9]+\.) `)

// renderMan renders the page text for a terminal width columns wide, or 0 to
// keep lines as they are. Prose is wrapped, with continuation lines indented
// like the line, or like the text of a list item. Code is indented by 4
// spaces, highlighted, and never wrapped.
func renderMan(text string, width int) string {
	var out []string
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		body := strings.TrimLeft(line, " ")
		indent := len(line) - len(body)
		switch {
		case i == 0:
			rule := strings.Repeat(unicodeOr("━", "-"), visibleLen(line))
			out = append(out, Styled(line, Bold, BrightBlue), Styled(rule, BrightBlue))
		case strings.HasPrefix(line, "```"):
			lang, end := strings.TrimPrefix(line, "```"), i+1
			for end < len(lines) && lines[end] != "```" {
				end++
			}
			for _, code := range strings.Split(highlightCode(strings.Join(lines[i+1:end], "\n"), lang), "\n") {
				if code != "" {
					code = "    " + code
				}
				out = append(out, code)
			}
			i = end
		case strings.HasPrefix(line, "# "):
			out = append(out, Styled(strings.TrimPrefix(line, "# "), Bold, Yellow))
		case indent == 0 && body != "" && i+1 < len(lines) && len(lines[i+1]) > 2 && lines[i+1][:2] == "  " && lines[i+1][2] != ' ':
			out = append(out, Styled(line, Bold, Green))
		default:
			indent += len(manItemRegexp.FindString(body))
			if width > 0 {
				line = line[:indent] + strings.ReplaceAll(WrapText(line[indent:], max(width-indent, 20)), "\n", "\n"+strings.Repeat(" ", indent))
			}
			out = append(out, styleInlineCode(line))
		}
	}
	return strings.Join(out, "\n") + "\n"
}

// styleInlineCode styles the `...` spans of text, dropping the backticks, if
// styles are on.
func styleInlineCode(text string) string {
	parts := strings.Split(text, "`")
	if !isTTY || len(parts)%2 == 0 {
		return text
	}
	for i := 1; i < len(parts); i += 2 {
		parts[i] = styledLines(parts[i], Cyan)
	}
	return strings.Join(parts, "")
}

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

// manKeywords are the keywords of GD++, C++ and GDScript, and GD++'s own words
// in C++ code.
var manKeywords = map[string]bool{}

// manTypes are the names of built-in types of Godot and C++ that aren't
// written in PascalCase, which is how highlightGdpp spots other types.
var manTypes = map[string]bool{}

func init() {
	for _, k := range strings.Fields("class class_name ctor decl dtor enum enum_name extends extern extern_name func get impl import noimport " +
		"set signal var emit rpc rpc_id if else for while do return switch case break continue default new delete auto const static " +
		"constexpr namespace using typedef template typename public private protected virtual override struct true false nullptr this " +
		"sizeof operator inline explicit mutable pass and or not in") {
		manKeywords[k] = true
	}
	for _, t := range strings.Fields("bool int float void char double long short unsigned signed size_t int8_t int16_t int32_t int64_t " +
		"uint8_t uint16_t uint32_t uint64_t float64_t real_t gd RID AABB") {
		manTypes[t] = true
	}
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
			case manKeywords[word]:
				style = []Style{CodeKeyword}
			case manTypes[word] || word[0] >= 'A' && word[0] <= 'Z' && strings.ToUpper(word) != word:
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
