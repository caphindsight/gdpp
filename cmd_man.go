package main

import (
	"embed"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/term"

	"gd++/trans"
)

// CmdMan shows a page of the reference manual of GD++, the build tool and
// the language, in the pager. Without a page, it lists them all, as a tree.
// The language's pages are those of one syntax version: in a package, the
// package's, and elsewhere the latest stable one, unless flags choose it.
type CmdMan struct {
	Page    string `arg:"positional" help:"the page to show, by name or path, e.g. functions or lang/functions [default: list all pages]"`
	Syntax  *int   `arg:"--syntax" placeholder:"N" help:"the GD++ syntax version of the language's pages [default: the package's, or else the latest stable one]"`
	Nightly bool   `arg:"--nightly" help:"the same as --syntax 0, the nightly syntax"`
}

// manFiles holds the manual's pages. The first line of a page is its title.
//
//go:embed man
var manFiles embed.FS

// toolManPages lists the paths of the build tool's pages, in reading order.
// The page at a path is man/<path>.txt. A page's children are the pages
// under its path, e.g. cmd/build under cmd, and come right after it. The last
// part of a path is the page's name, which is unique in the whole manual.
var toolManPages = []string{
	"intro", "tour",
	"tool", "tool/cli", "tool/paths", "tool/projects", "tool/packages", "tool/dependencies", "tool/building", "tool/build-cache",
	"tool/cpp-classes", "tool/vcs", "tool/shipping",
	"cmd", "cmd/build", "cmd/cat", "cmd/checkin", "cmd/clean", "cmd/doc", "cmd/fetch", "cmd/fix", "cmd/init", "cmd/install", "cmd/ls", "cmd/man",
	"cmd/rm", "cmd/trans", "cmd/vendor",
}

// langManPages lists the paths of the language's pages, in reading order,
// for each syntax version N, like toolManPages. The page at lang/<path> is
// man/lang_N/<path>.txt, and the page at lang is man/lang_N.txt. The lists of stable versions never change.
var langManPages = map[int][]string{
	0: {
		"lang", "lang/syntax", "lang/files", "lang/classes", "lang/types", "lang/cast", "lang/functions", "lang/variables", "lang/exports",
		"lang/signals", "lang/enums", "lang/lifecycle", "lang/rpc", "lang/async", "lang/externs", "lang/debugging", "lang/performance", "lang/code",
		"lang/rewrites", "lang/includes", "lang/annotations", "lang/docs", "lang/runtime", "lang/generated", "lang/errors", "lang/grammar",
	},
	1: {
		"lang", "lang/syntax", "lang/files", "lang/classes", "lang/types", "lang/cast", "lang/functions", "lang/variables", "lang/exports",
		"lang/signals", "lang/enums", "lang/lifecycle", "lang/rpc", "lang/async", "lang/externs", "lang/debugging", "lang/performance", "lang/code",
		"lang/rewrites", "lang/includes", "lang/annotations", "lang/docs", "lang/runtime", "lang/generated", "lang/errors", "lang/grammar",
	},
}

// manual is the reference manual for one GD++ syntax version.
type manual struct {
	syntax int
	pages  []string // in reading order: the tool's pages, then the language's
}

// loadManual returns the manual for syntax.
func loadManual(syntax int) manual {
	pages, ok := langManPages[syntax]
	Assert(ok, "There is no manual for GD++ syntax %d, run `gd++ --syntax` to list the versions.", syntax)
	return manual{syntax, append(slices.Clone(toolManPages), pages...)}
}

func (c *CmdMan) Run() {
	syntax := trans.LatestSyntax
	if s := chosenSyntax(c.Syntax, c.Nightly); s != nil {
		syntax = *s
	} else if root, ok := GetPackageRootMaybe(Cwd()); ok {
		syntax = LoadPackage(root).Config.Syntax
	}
	m := loadManual(syntax)
	if c.Page == "" {
		PageResult("The reference manual of GD++. Run `gd++ man PAGE` to read a page.\n\n" + m.contents(""))
		return
	}
	i := slices.IndexFunc(m.pages, func(p string) bool { return p == c.Page || path.Base(p) == c.Page })
	if i < 0 {
		var similar []string
		for _, p := range m.pages {
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
	text := renderMan(m.page(m.pages[i]), m.tag(m.pages[i]), width)
	if children := m.contents(m.pages[i]); children != "" {
		text += "\n" + Styled("Pages in this section:", Bold) + "\n" + children
	}
	PageResult(text)
}

// isLangPage reports whether the page at path p is one of the language's.
func isLangPage(p string) bool {
	return p == "lang" || strings.HasPrefix(p, "lang/")
}

// page returns the text of the page at path p.
func (m manual) page(p string) string {
	file := p
	if isLangPage(p) {
		file = "lang_" + strconv.Itoa(m.syntax) + strings.TrimPrefix(p, "lang")
	}
	text, err := manFiles.ReadFile("man/" + file + ".txt")
	Check(err, "Failed to read the manual page %s", p)
	return string(text)
}

// tag returns the syntax version that the page at path p describes, e.g.
// "[syntax 0 (nightly)]", or "" for the build tool's pages.
func (m manual) tag(p string) string {
	if !isLangPage(p) {
		return ""
	}
	if m.syntax == trans.NightlySyntax {
		return "[syntax " + strconv.Itoa(m.syntax) + " (nightly)]"
	}
	return "[syntax " + strconv.Itoa(m.syntax) + "]"
}

// contents lists the pages under the path parent, or all pages if it's
// empty, each with its title, indented by its depth below parent. The
// language's top page also shows its syntax version.
func (m manual) contents(parent string) string {
	var rows [][]string
	for _, p := range m.pages {
		rel, ok := strings.CutPrefix(p, parent+"/")
		if parent == "" {
			rel, ok = p, true
		}
		if !ok {
			continue
		}
		title, _, _ := strings.Cut(m.page(p), "\n")
		name := strings.Repeat("  ", strings.Count(rel, "/")) + path.Base(p)
		if strings.Contains(rel, "/") {
			name = Styled(name, Cyan)
		} else {
			name, title = Styled(name, Bold, Cyan), Styled(title, Bold)
			if tag := m.tag(p); tag != "" {
				title += " " + Styled(tag, Magenta)
			}
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
// keep lines as they are. A non-empty tag follows the title, in another color. Prose is wrapped, with continuation lines indented
// like the line, or like the text of a list item. Code is indented by 4
// spaces, highlighted, and never wrapped.
func renderMan(text, tag string, width int) string {
	var out []string
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		body := strings.TrimLeft(line, " ")
		indent := len(line) - len(body)
		switch {
		case i == 0:
			title := Styled(line, Bold, BrightBlue)
			if tag != "" {
				line, title = line+" "+tag, title+" "+Styled(tag, Bold, Magenta)
			}
			out = append(out, title, Styled(strings.Repeat(unicodeOr("━", "-"), visibleLen(line)), BrightBlue))
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
