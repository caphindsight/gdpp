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
	"cmd", "cmd/build", "cmd/cat", "cmd/checkin", "cmd/clean", "cmd/doc", "cmd/fetch", "cmd/fix", "cmd/init", "cmd/install", "cmd/ls", "cmd/man",
	"cmd/rm", "cmd/trans", "cmd/vendor",
	"lang", "lang/syntax", "lang/files", "lang/classes", "lang/types", "lang/functions", "lang/variables", "lang/exports", "lang/signals",
	"lang/enums", "lang/lifecycle", "lang/rpc", "lang/async", "lang/externs", "lang/debugging", "lang/performance", "lang/code", "lang/rewrites", "lang/includes", "lang/annotations", "lang/docs",
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
