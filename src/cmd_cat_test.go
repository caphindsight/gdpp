// cmd_cat_test.go: tests for cmd_cat.go. Runs on the real disk, like
// cmd_trans_test.go.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCat(t *testing.T) {
	dir := writeTransFiles(t, map[string]string{"a.gd++": "var x", "a.hpp": "#include <x>", "a.txt": "var x"})
	withTTY(t, true)
	cases := map[string]string{
		"a.gd++": Styled("var", CodeKeyword) + " x",
		"a.hpp":  Styled("#include", CodePreProc) + " <x>",
		"a.txt":  "var x",
	}
	for name, want := range cases {
		if got := captureStdout(t, (&CmdCat{Files: []string{filepath.Join(dir, name)}}).Run); got != want {
			t.Errorf("cat %s = %q, want %q", name, got, want)
		}
	}
	withTTY(t, false)
	chdir(t, dir)
	want := "// ==== a.txt ====\n\nvar x\n\n// ==== a.hpp ====\n\n#include <x>"
	if got := captureStdout(t, (&CmdCat{Files: []string{"a.txt", "a.hpp"}}).Run); got != want {
		t.Errorf("cat a.txt a.hpp = %q, want %q", got, want)
	}
}

func TestCatSVG(t *testing.T) {
	dir := writeTransFiles(t, map[string]string{"a.gd++": "var\tx\r\n\x1b[31m<y>\n\n", "a.txt": "var x"})
	withTTY(t, false)
	got := captureStdout(t, (&CmdCat{Files: []string{filepath.Join(dir, "a.gd++")}, SVG: true}).Run)
	// \r and the escape code are left out, and the tab is expanded.
	want := `<text x="16" y="30"><tspan fill="#e3b341">var</tspan><tspan fill="#e6edf3">` + "\u00a0x</tspan></text>\n" +
		`  <text x="16" y="50"><tspan fill="#e6edf3">[</tspan><tspan fill="#d2a8ff">31m</tspan><tspan fill="#e6edf3">&lt;y&gt;</tspan></text>` + "\n  </g>"
	if !strings.Contains(got, want) {
		t.Errorf("cat --svg a.gd++ = %q, want it to contain %q", got, want)
	}
	if got := captureStdout(t, (&CmdCat{Files: []string{filepath.Join(dir, "a.txt")}, SVG: true}).Run); !strings.Contains(got, `<tspan fill="#e6edf3">var`+"\u00a0x</tspan>") {
		t.Errorf("cat --svg a.txt = %q, want plain text", got)
	}
}

func TestCatSVGArgs(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		(&CmdCat{Files: []string{"a", "b"}, SVG: true}).Run()
		return
	}
	if out, code := runFailHelper(t, t.Name()); code != 1 || out != "[x] Invalid arguments: --svg takes exactly one file.\n" {
		t.Errorf("cat --svg a b = %q, exit code %d", out, code)
	}
}
