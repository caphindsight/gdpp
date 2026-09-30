// cmd_cat_test.go: tests for cmd_cat.go. Runs on the real disk, like
// cmd_trans_test.go.

package main

import (
	"path/filepath"
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
