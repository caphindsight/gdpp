// shell_test.go: tests for shell.go.

package main

import (
	"os"
	"strings"
	"testing"
)

func TestExec(t *testing.T) {
	withTTY(t, false)
	withRealFS(t)

	var got string
	out := captureStdout(t, func() {
		got = Exec("Running a task...", Cwd(), "sh", "-c", "echo one; echo two >&2")
	})
	if got != "one\ntwo\n" {
		t.Errorf("Exec returned %q, want %q", got, "one\ntwo\n")
	}

	want := "[$] Running task: running a task...\n" +
		"    .$ sh -c echo one; echo two >&2\n" +
		"    one\n" +
		"    two\n" +
		"[+] Task succeeded: running a task\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

// TestExecRunsInDir checks that the subprocess actually runs in the given
// directory, not just that it's mentioned in the logged command line.
func TestExecRunsInDir(t *testing.T) {
	withTTY(t, false)
	withRealFS(t)
	dir := NewPath(t.TempDir())

	var got string
	out := captureStdout(t, func() {
		got = Exec("Running a task...", dir, "pwd")
	})
	if got = strings.TrimSuffix(got, "\n"); got != dir.GetOsPath() {
		t.Errorf("subprocess ran in %q, want %q", got, dir.GetOsPath())
	}

	wantLine := "    " + dir.ToString() + "$ pwd\n"
	if !strings.Contains(out, wantLine) {
		t.Errorf("output = %q, want it to contain %q", out, wantLine)
	}
}

func TestExecFail(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		fsys = osFS{}
		Exec("Running a task...", Cwd(), "sh", "-c", "echo boom; exit 3")
		return
	}

	out, code := runFailHelper(t, "TestExecFail")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	want := "[$] Running task: running a task...\n" +
		"    .$ sh -c echo boom; exit 3\n" +
		"    boom\n" +
		"[x] Task failed: running a task\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}
