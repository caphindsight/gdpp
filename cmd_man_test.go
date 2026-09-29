// cmd_man_test.go: tests for cmd_man.go.

package main

import (
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestManPages checks that manPages lists every page once, parents before
// children, with unique names, that each page has a title and no trailing
// spaces, and that pages only refer to pages that exist.
func TestManPages(t *testing.T) {
	var files []string
	err := fs.WalkDir(manFiles, "man", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, strings.TrimSuffix(strings.TrimPrefix(p, "man/"), ".txt"))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(uniqueSorted(manPages, strings.Compare), uniqueSorted(files, strings.Compare)) || len(files) != len(manPages) {
		t.Errorf("manPages = %v, want each of %v once", manPages, files)
	}
	names := map[string]bool{}
	for i, p := range manPages {
		if parent := path.Dir(p); parent != "." && !slices.Contains(manPages[:i], parent) {
			t.Errorf("page %s comes before its parent %s", p, parent)
		}
		if names[path.Base(p)] {
			t.Errorf("page name %s is used twice", path.Base(p))
		}
		names[path.Base(p)] = true
	}
	ref := regexp.MustCompile("`gd\\+\\+ man ([a-z/-]+)`")
	for _, p := range manPages {
		text := manPage(p)
		if title, _, _ := strings.Cut(text, "\n"); title == "" || strings.HasPrefix(title, "# ") {
			t.Errorf("page %s: the first line must be its title, got %q", p, title)
		}
		fence := ""
		for i, line := range strings.Split(text, "\n") {
			if strings.TrimRight(line, " \t") != line {
				t.Errorf("page %s, line %d: trailing whitespace", p, i+1)
			}
			if lang, ok := strings.CutPrefix(line, "```"); ok {
				if fence == "" && !slices.Contains([]string{"", "gd++", "cpp", "gdscript", "sh", "toml", "out"}, lang) {
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
		for _, m := range ref.FindAllStringSubmatch(text, -1) {
			if !names[m[1]] && !slices.Contains(manPages, m[1]) {
				t.Errorf("page %s refers to the missing page %s", p, m[1])
			}
		}
	}
}

func TestRenderMan(t *testing.T) {
	withTTY(t, false)
	text := "Title\n# Heading\naaaa bbbb cccc `dddd` eeee\n- aaaa bbbb cccc dddd eeee\n12. aaaa bbbb cccc dddd eeee\n--term\n  aaaa bbbb cccc dddd eeee\n```sh\ncode that is longer than the width stays\n\nx\n```\n"
	want := "Title\n-----\nHeading\naaaa bbbb cccc `dddd`\neeee\n- aaaa bbbb cccc dddd\n  eeee\n12. aaaa bbbb cccc dddd\n    eeee\n--term\n  aaaa bbbb cccc dddd\n  eeee\n    code that is longer than the width stays\n\n    x\n"
	if got := renderMan(text, 23); got != want {
		t.Errorf("renderMan = %q, want %q", got, want)
	}
	withTTY(t, true)
	withUnicode(t, false)
	want = Styled("Title", Bold, BrightBlue) + "\n" + Styled("-----", BrightBlue) + "\n" + Styled("Heading", Bold, Yellow) + "\n" +
		"aaaa bbbb cccc " + Styled("dddd", Cyan) + " eeee\n- aaaa bbbb cccc dddd eeee\n12. aaaa bbbb cccc dddd eeee\n" + Styled("--term", Bold, Green) +
		"\n  aaaa bbbb cccc dddd eeee\n    " + Styled("code", Bold) + " that is longer than the width stays\n\n    " + Styled("x", Bold) + "\n"
	if got := renderMan(text, 0); got != want {
		t.Errorf("renderMan with styles = %q, want %q", got, want)
	}
}

func TestHighlightCode(t *testing.T) {
	withTTY(t, true)
	cases := []struct{ lang, code, want string }{
		{"gd++", "@export var x: Node3D = f(\"a\", 1) // c\n/* a /* b */ c */",
			Styled("@export", Yellow) + " " + Styled("var", Magenta) + " x: " + Styled("Node3D", Cyan) + " = " + Styled("f", BrightBlue) + "(" +
				Styled("\"a\"", Green) + ", " + Styled("1", Green) + ") " + Styled("// c", Gray) + "\n" + Styled("/* a /* b */ c */", Gray)},
		{"cpp", "#include <vector>\nint64_t x;", Styled("#include", Magenta) + " <vector>\n" + Styled("int64_t", Cyan) + " x;"},
		{"gdscript", "# c\nreturn", Styled("# c", Gray) + "\n" + Styled("return", Magenta)},
		{"sh", "gd++ build --ship . # c", Styled("gd++", Bold) + " " + Styled("build", Bold) + " " + Styled("--ship", Cyan) + " . " + Styled("# c", Gray)},
		{"sh", "gd++ x [-j N] [--a | --b]\n  x | y", Styled("gd++", Bold) + " " + Styled("x", Bold) + " [" + Styled("-j", Cyan) + " N] [" + Styled("--a", Cyan) +
			" | " + Styled("--b", Cyan) + "]\n  x | " + Styled("y", Bold)},
		{"out", "[×] Oops.\n 3 | x\n   | ^\nHint: Fix.", Styled("[×]", Bold, Red) + " Oops.\n " + Styled("3 |", Gray) + " x\n   " + Styled("|", Gray) + " " +
			Styled("^", Red) + "\n" + Styled("Hint:", Bold) + " Fix."},
		{"toml", "[[class]]\nname = \"A\"", Styled("[[class]]", Bold) + "\n" + Styled("name", Cyan) + " = " + Styled("\"A\"", Green)},
		{"", "var x", "var x"},
	}
	for _, tc := range cases {
		if got := highlightCode(tc.code, tc.lang); got != tc.want {
			t.Errorf("highlightCode(%q, %q) = %q, want %q", tc.code, tc.lang, got, tc.want)
		}
	}
}

func TestMan(t *testing.T) {
	withTTY(t, false)
	for _, page := range []string{"signals", "lang/signals"} {
		if out := captureStdout(t, (&CmdMan{Page: page}).Run); out != renderMan(manPage("lang/signals"), 0) {
			t.Errorf("gd++ man %s printed %q, want the page without heading markers", page, out)
		}
	}
	out := captureStdout(t, (&CmdMan{}).Run)
	for _, want := range []string{"\n  intro  ", "\n  lang   ", "\n    signals", "Signals: declaring signals"} {
		if !strings.Contains(out, want) {
			t.Errorf("gd++ man = %q, want it to contain %q", out, want)
		}
	}
	out = captureStdout(t, (&CmdMan{Page: "lang"}).Run)
	if !strings.HasPrefix(out, renderMan(manPage("lang"), 0)+"\nPages in this section:\n  syntax ") || strings.Contains(out, "  build ") {
		t.Errorf("gd++ man lang = %q, want the page, then the pages under lang only", out)
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
