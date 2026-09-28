// io_test.go: tests for io.go.

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// withStdoutTTY sets isStdoutTTY for the duration of a test and restores the prior value.
// Tests in this file must not call t.Parallel(): they mutate this package-level var.
func withStdoutTTY(t *testing.T, tty bool) {
	orig := isStdoutTTY
	isStdoutTTY = tty
	t.Cleanup(func() { isStdoutTTY = orig })
}

// withStdinTTY sets isStdinTTY for the duration of a test and restores the prior value.
func withStdinTTY(t *testing.T, tty bool) {
	orig := isStdinTTY
	isStdinTTY = tty
	t.Cleanup(func() { isStdinTTY = orig })
}

// withStdin replaces os.Stdin with a pipe fed with content, for the duration of a test.
func withStdin(t *testing.T, content string) {
	orig := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	go func() {
		io.WriteString(w, content)
		w.Close()
	}()
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })
}

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what it wrote.
func captureStdout(t *testing.T, fn func()) string {
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = orig

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("io.ReadAll: %v", err)
	}
	return string(out)
}

func TestStyled(t *testing.T) {
	cases := []struct {
		name   string
		tty    bool
		text   string
		styles []Style
		want   string
	}{
		{"no styles, tty", true, "done", nil, "done"},
		{"no styles, no tty", false, "done", nil, "done"},
		{"styles given, no tty", false, "done", []Style{Bold}, "done"},
		{"one style, tty", true, "done", []Style{Green}, "\x1b[32mdone\x1b[0m"},
		{"multiple styles, tty, order preserved", true, "done", []Style{Bold, Green}, "\x1b[1;32mdone\x1b[0m"},
		{"empty text, tty", true, "", []Style{Red}, "\x1b[31m\x1b[0m"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withStdoutTTY(t, c.tty)
			if got := Styled(c.text, c.styles...); got != c.want {
				t.Errorf("Styled(%q, %v) = %q, want %q", c.text, c.styles, got, c.want)
			}
		})
	}
}

func TestLogInfo(t *testing.T) {
	// Force non-TTY so the "[>] " prefix is a plain literal, independent of
	// Styled's ANSI-wrapping behavior (covered separately by TestStyled).
	withStdoutTTY(t, false)

	cases := []struct {
		name   string
		format string
		params []any
		want   string
	}{
		{"plain message", "hello", nil, "[>] hello\n"},
		{"formatted message", "hello, %s! count=%d", []any{"world", 3}, "[>] hello, world! count=3\n"},
		{"multiline message re-indented", "line1\nline2", nil, "[>] line1\n    line2\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := captureStdout(t, func() { LogInfo(c.format, c.params...) })
			if got != c.want {
				t.Errorf("LogInfo(%q, %v) printed %q, want %q", c.format, c.params, got, c.want)
			}
		})
	}
}

func TestLogWarn(t *testing.T) {
	withStdoutTTY(t, false)

	cases := []struct {
		name   string
		format string
		params []any
		want   string
	}{
		{"plain message", "hello", nil, "[!] hello\n"},
		{"formatted message", "hello, %s! count=%d", []any{"world", 3}, "[!] hello, world! count=3\n"},
		{"multiline message re-indented", "line1\nline2", nil, "[!] line1\n    line2\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := captureStdout(t, func() { LogWarn(c.format, c.params...) })
			if got != c.want {
				t.Errorf("LogWarn(%q, %v) printed %q, want %q", c.format, c.params, got, c.want)
			}
		})
	}
}

func TestCleanup(t *testing.T) {
	orig := cleanups
	cleanups = nil
	t.Cleanup(func() { cleanups = orig })

	Cleanup(func() {})
	Cleanup(func() {})
	if len(cleanups) != 2 {
		t.Errorf("len(cleanups) = %d, want 2", len(cleanups))
	}
}

// runFailHelper re-execs the test binary to run f (a top-level function in
// this file whose name is passed as -test.run), since Fail calls os.Exit and
// would otherwise kill the test process.
func runFailHelper(t *testing.T, name string) (stdout string, exitCode int) {
	cmd := exec.Command(os.Args[0], "-test.run=^"+name+"$")
	cmd.Env = append(os.Environ(), "GDPP_FAIL_HELPER=1")
	out, err := cmd.Output()
	if err == nil {
		t.Fatalf("%s: process exited 0, want nonzero", name)
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("%s: cmd.Output: %v", name, err)
	}
	return string(out), exitErr.ExitCode()
}

// runFailHelperWithStdin is like runFailHelper, but feeds stdin to the child process.
func runFailHelperWithStdin(t *testing.T, name string, stdin string) (stdout string, exitCode int) {
	cmd := exec.Command(os.Args[0], "-test.run=^"+name+"$")
	cmd.Env = append(os.Environ(), "GDPP_FAIL_HELPER=1")
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.Output()
	if err == nil {
		t.Fatalf("%s: process exited 0, want nonzero", name)
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("%s: cmd.Output: %v", name, err)
	}
	return string(out), exitErr.ExitCode()
}

func TestConfirmYes(t *testing.T) {
	withStdoutTTY(t, false)
	withStdinTTY(t, true)

	cases := []string{"y", "Y", "yes", "YES", "  yes  "}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			withStdin(t, in+"\n")
			out := captureStdout(t, func() { Confirm("Proceed?") })
			if want := "[?] Proceed? [y/n] "; out != want {
				t.Errorf("Confirm output = %q, want %q", out, want)
			}
		})
	}
}

func TestConfirmRetriesOnInvalidInput(t *testing.T) {
	withStdoutTTY(t, false)
	withStdinTTY(t, true)
	withStdin(t, "maybe\ny\n")

	out := captureStdout(t, func() { Confirm("Proceed?") })
	if want := "[?] Proceed? [y/n] Please answer yes or no: "; out != want {
		t.Errorf("Confirm output = %q, want %q", out, want)
	}
}

func TestConfirmNo(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isStdoutTTY = false
		isStdinTTY = true
		Confirm("Delete %s?", "file.txt")
		return
	}

	out, code := runFailHelperWithStdin(t, "TestConfirmNo", "n\n")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "[?] Delete file.txt? [y/n] [!] Operation canceled by user.\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestConfirmEOF(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isStdoutTTY = false
		isStdinTTY = true
		Confirm("Delete %s?", "file.txt")
		return
	}

	out, code := runFailHelperWithStdin(t, "TestConfirmEOF", "")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "[?] Delete file.txt? [y/n] [!] Operation canceled by user.\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestConfirmNonTTY(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isStdoutTTY = false
		isStdinTTY = false
		Confirm("Delete %s?", "file.txt")
		return
	}

	out, code := runFailHelper(t, "TestConfirmNonTTY")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "[?] Delete file.txt? [y/n] n\n[!] Input is not a tty, use -f to confirm.\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestFail(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		Cleanup(func() { fmt.Print("first;") })
		Cleanup(func() { fmt.Print("second;") })
		Fail()
		return
	}

	out, code := runFailHelper(t, "TestFail")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "second;first;"; out != want {
		t.Errorf("cleanups ran in order %q, want %q (LIFO)", out, want)
	}
}

func TestLogFatal(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isStdoutTTY = false
		Cleanup(func() { fmt.Print("cleaned up") })
		LogFatal("boom: %s", "oops")
		return
	}

	out, code := runFailHelper(t, "TestLogFatal")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "[!] boom: oops\ncleaned up"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestLogError(t *testing.T) {
	withStdoutTTY(t, false)

	cases := []struct {
		name   string
		format string
		params []any
		want   string
	}{
		{"plain message", "hello", nil, "[!] hello\n"},
		{"formatted message", "hello, %s! count=%d", []any{"world", 3}, "[!] hello, world! count=3\n"},
		{"multiline message re-indented", "line1\nline2", nil, "[!] line1\n    line2\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := captureStdout(t, func() { LogError(c.format, c.params...) })
			if got != c.want {
				t.Errorf("LogError(%q, %v) printed %q, want %q", c.format, c.params, got, c.want)
			}
		})
	}
}

func TestWrapText(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		width int
		want  string
	}{
		{"fits", "hello world", 20, "hello world"},
		{"exact fit", "hello world", 11, "hello world"},
		{"wraps at space", "hello big world", 10, "hello big\nworld"},
		{"splits long word", "abcdefghij", 4, "abcd\nefgh\nij"},
		{"long word after short one", "hi abcdefgh", 4, "hi\nabcd\nefgh"},
		{"keeps existing newlines", "aaa bbb\nccc", 4, "aaa\nbbb\nccc"},
		{"ansi codes are zero width", "\x1b[1;32mhello\x1b[0m world", 11, "\x1b[1;32mhello\x1b[0m world"},
		{"zero width disables wrapping", "hello world", 0, "hello world"},
		{"negative width disables wrapping", "hello world", -4, "hello world"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := WrapText(c.text, c.width); got != c.want {
				t.Errorf("WrapText(%q, %d) = %q, want %q", c.text, c.width, got, c.want)
			}
		})
	}
}
