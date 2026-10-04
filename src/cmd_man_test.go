// cmd_man_test.go: tests for cmd_man.go.

package main

import (
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"gd++/trans"
)

// manPageFiles lists the paths of the pages in manFiles, mapping the files
// of the tutorials' and the language's pages for syntax N (e.g. man/lang_N.txt
// and man/lang_N/) to tut and lang, and skipping those of other syntax versions.
func manPageFiles(t *testing.T, syntax int) []string {
	own, other := regexp.MustCompile(`^(tut|lang)_`+strconv.Itoa(syntax)+`\b`), regexp.MustCompile(`^(tut|lang)_[0-9]+\b`)
	var list []string
	err := fs.WalkDir(manFiles, "man", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		p = strings.TrimSuffix(strings.TrimPrefix(p, "man/"), ".txt")
		if own.MatchString(p) {
			list = append(list, own.ReplaceAllString(p, "$1"))
		} else if !other.MatchString(p) {
			list = append(list, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// TestManPages checks, for every syntax version, that the manual lists every
// page once, parents before children, with unique names, that each page has
// a title and no trailing spaces, and that pages only refer to pages that
// exist.
func TestManPages(t *testing.T) {
	syntaxes := trans.Syntaxes()
	for name, pages := range map[string]map[int][]string{"tut": tutManPages, "lang": langManPages} {
		var got []int
		for s := range pages {
			got = append(got, s)
		}
		if slices.Sort(got); !slices.Equal(got, syntaxes) {
			t.Errorf("%sManPages has syntaxes %v, want %v", name, got, syntaxes)
		}
		if tops, _ := fs.Glob(manFiles, "man/"+name+"_*.txt"); len(tops) != len(syntaxes) {
			t.Errorf("%s manuals = %v, want one per syntax in %v", name, tops, syntaxes)
		}
	}
	for _, syntax := range syntaxes {
		t.Run(strconv.Itoa(syntax), func(t *testing.T) { testManPages(t, loadManual(syntax)) })
	}
}

func testManPages(t *testing.T, m manual) {
	files := manPageFiles(t, m.syntax)
	if !slices.Equal(uniqueSorted(m.pages, strings.Compare), uniqueSorted(files, strings.Compare)) || len(files) != len(m.pages) {
		t.Errorf("pages = %v, want each of %v once", m.pages, files)
	}
	names := map[string]bool{}
	for i, p := range m.pages {
		if parent := path.Dir(p); parent != "." && !slices.Contains(m.pages[:i], parent) {
			t.Errorf("page %s comes before its parent %s", p, parent)
		}
		if names[path.Base(p)] {
			t.Errorf("page name %s is used twice", path.Base(p))
		}
		names[path.Base(p)] = true
	}
	ref := regexp.MustCompile("`gd\\+\\+ man (?:--[a-z]+ (?:[0-9]+ )?)*([a-z][a-z/-]*)`")
	for _, p := range m.pages {
		text := m.page(p)
		if title, _, _ := strings.Cut(text, "\n"); title == "" || strings.HasPrefix(title, "# ") {
			t.Errorf("page %s: the first line must be its title, got %q", p, title)
		}
		fence := ""
		for i, line := range strings.Split(text, "\n") {
			if strings.TrimRight(line, " \t") != line {
				t.Errorf("page %s, line %d: trailing whitespace", p, i+1)
			}
			if lang, ok := strings.CutPrefix(line, "```"); ok {
				if fence == "" && !slices.Contains([]string{"", "gd++", "cpp", "gdscript", "lua", "sh", "toml", "out"}, lang) {
					t.Errorf("page %s, line %d: unknown code block language %q", p, i+1, lang)
				}
				fence = map[bool]string{true: "open", false: ""}[fence == ""]
			} else if fence == "" && strings.HasPrefix(line, "    ") {
				t.Errorf("page %s, line %d: code outside of a code block", p, i+1)
			}
		}
		if fence != "" {
			t.Errorf("page %s: a code block is never closed", p)
		}
		for _, r := range ref.FindAllStringSubmatch(text, -1) {
			if !names[r[1]] && !slices.Contains(m.pages, r[1]) {
				t.Errorf("page %s refers to the missing page %s", p, r[1])
			}
		}
	}
}

func TestRenderMan(t *testing.T) {
	withTTY(t, false)
	text := "Title\n# Heading\naaaa bbbb cccc `dddd` eeee\n- aaaa bbbb cccc dddd eeee\n12. aaaa bbbb cccc dddd eeee\n--term\n  aaaa bbbb cccc dddd eeee\n```sh\ncode that is longer than the width stays\n\nx\n```\n"
	want := "Title\n-----\nHeading\naaaa bbbb cccc `dddd`\neeee\n- aaaa bbbb cccc dddd\n  eeee\n12. aaaa bbbb cccc dddd\n    eeee\n--term\n  aaaa bbbb cccc dddd\n  eeee\n    code that is longer than the width stays\n\n    x\n"
	if got := renderMan(text, "", 23); got != want {
		t.Errorf("renderMan = %q, want %q", got, want)
	}
	withTTY(t, true)
	withUnicode(t, false)
	want = Styled("Title", Bold, BrightBlue) + "\n" + Styled("-----", BrightBlue) + "\n" + Styled("Heading", Bold, Yellow) + "\n" +
		"aaaa bbbb cccc " + Styled("dddd", Cyan) + " eeee\n- aaaa bbbb cccc dddd eeee\n12. aaaa bbbb cccc dddd eeee\n" + Styled("--term", Bold, Green) +
		"\n  aaaa bbbb cccc dddd eeee\n    " + Styled("code", Bold) + " that is longer than the width stays\n\n    " + Styled("x", Bold) + "\n"
	if got := renderMan(text, "", 0); got != want {
		t.Errorf("renderMan with styles = %q, want %q", got, want)
	}
	want = Styled("Title", Bold, BrightBlue) + " " + Styled("[syntax 1]", Bold, Magenta) + "\n" + Styled("----------------", BrightBlue) + "\n"
	if got := renderMan("Title\n", "[syntax 1]", 0); got != want {
		t.Errorf("renderMan with a tag = %q, want %q", got, want)
	}
	want = Styled("Title", Bold, BrightBlue) + "\n" + Styled("-----", BrightBlue) + "\n- item\n  - nested\n"
	if got := renderMan("Title\n- item\n  - nested\n", "", 0); got != want {
		t.Errorf("renderMan with a nested list = %q, want %q", got, want)
	}
}

func TestMan(t *testing.T) {
	withTTY(t, false)
	withMemFS(t, "/games/my_game", withPackages(nil))
	m := loadManual(trans.LatestSyntax)
	for _, page := range []string{"signals", "lang/signals"} {
		if out := captureStdout(t, (&CmdMan{Page: page}).Run); out != renderMan(m.page("lang/signals"), m.tag("lang/signals"), 0) {
			t.Errorf("gd++ man %s printed %q, want the page without heading markers", page, out)
		}
	}
	out := captureStdout(t, (&CmdMan{}).Run)
	for _, want := range []string{"\n  intro  ", "\n  lang   ", "\n    signals", "Signals: declaring and emitting signals", "an overview [syntax 1]\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("gd++ man = %q, want it to contain %q", out, want)
		}
	}
	out = captureStdout(t, (&CmdMan{Page: "lang"}).Run)
	if !strings.HasPrefix(out, renderMan(m.page("lang"), m.tag("lang"), 0)+"\nPages in this section:\n  syntax ") || strings.Contains(out, "  build ") {
		t.Errorf("gd++ man lang = %q, want the page, then the pages under lang only", out)
	}
	if out, want := captureStdout(t, (&CmdMan{Page: "ship", Nightly: true}).Run), "Tutorial: ship a game on several platforms [syntax 0 (nightly)]\n"; !strings.HasPrefix(out, want) {
		t.Errorf("gd++ man --nightly ship = %q, want the prefix %q", out, want)
	}
	if out := captureStdout(t, (&CmdMan{Page: "intro"}).Run); strings.Contains(out, "[syntax") {
		t.Errorf("gd++ man intro = %q, want no syntax in the title", out)
	}
}

func TestManSyntax(t *testing.T) {
	withTTY(t, false)
	const nightly = "Signals: declaring and emitting signals [syntax 0 (nightly)]\n"
	pkgs := map[string]string{"src/pkg": "bind = \"4.3\"\nspec = \"4.3\"\nsyntax = 0\n"}
	withMemFS(t, "/games/my_game", withPackages(pkgs))
	for name, c := range map[string]CmdMan{"nightly": {Page: "signals", Nightly: true}, "syntax": {Page: "signals", Syntax: ptr(0)}} {
		if out := captureStdout(t, c.Run); !strings.HasPrefix(out, nightly) {
			t.Errorf("%s: output = %q, want the prefix %q", name, out, nightly)
		}
	}
	withMemFS(t, "/games/my_game/src/pkg", withPackages(pkgs))
	if out := captureStdout(t, (&CmdMan{Page: "signals"}).Run); !strings.HasPrefix(out, nightly) {
		t.Errorf("in a package: output = %q, want the prefix %q", out, nightly)
	}
}

func TestManFails(t *testing.T) {
	cases := map[string]struct{ page, want string }{
		"similar": {"export", "[x] There is no manual page export. Similar pages: exports.\n"},
		"unknown": {"nothing", "[x] There is no manual page nothing, run `gd++ man` to list them.\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if os.Getenv("GDPP_FAIL_HELPER") == "1" {
				withTTY(t, false)
				withMemFS(t, "/games/my_game", withPackages(nil))
				(&CmdMan{Page: tc.page}).Run()
				return
			}
			out, code := runFailHelper(t, t.Name())
			if code != 1 || out != tc.want {
				t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, tc.want)
			}
		})
	}
}
