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

// TestMain sets Args defaults that arg.MustParse would normally fill in, since
// tests never call it.
func TestMain(m *testing.M) {
	Args.LogDepth = 4
	os.Exit(m.Run())
}

// withTTY sets isTTY for the duration of a test and restores the prior value.
// Tests in this file must not call t.Parallel(): they mutate this package-level var.
func withTTY(t *testing.T, tty bool) {
	orig := isTTY
	isTTY = tty
	t.Cleanup(func() { isTTY = orig })
}

// withUnicode sets isUnicode for the duration of a test and restores the prior value.
func withUnicode(t *testing.T, unicode bool) {
	orig := isUnicode
	isUnicode = unicode
	t.Cleanup(func() { isUnicode = orig })
}

// withForce sets Args.Force for the duration of a test and restores the prior value.
func withForce(t *testing.T, force bool) {
	orig := Args.Force
	Args.Force = force
	t.Cleanup(func() { Args.Force = orig })
}

// withForceNo sets Args.ForceNo for the duration of a test and restores the prior value.
func withForceNo(t *testing.T, forceNo bool) {
	orig := Args.ForceNo
	Args.ForceNo = forceNo
	t.Cleanup(func() { Args.ForceNo = orig })
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
			withTTY(t, c.tty)
			if got := Styled(c.text, c.styles...); got != c.want {
				t.Errorf("Styled(%q, %v) = %q, want %q", c.text, c.styles, got, c.want)
			}
		})
	}
}

func TestLogInfo(t *testing.T) {
	// Force non-TTY so the "[>] " prefix is a plain literal, independent of
	// Styled's ANSI-wrapping behavior (covered separately by TestStyled).
	withTTY(t, false)

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
	withTTY(t, false)

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

func TestSilence(t *testing.T) {
	withTTY(t, false)

	if quiet() {
		t.Fatal("quiet() = true before any Silence, want false")
	}

	s1 := Silence()
	if !quiet() {
		t.Error("quiet() = false after Silence, want true")
	}
	if out := captureStdout(t, func() { LogInfo("hidden") }); out != "" {
		t.Errorf("LogInfo printed %q while silenced, want nothing", out)
	}

	s2 := Silence() // nested
	if !quiet() {
		t.Error("quiet() = false under nested Silence, want true")
	}

	s2.End()
	if !quiet() {
		t.Error("quiet() = false after ending inner Silence while outer is still active, want true")
	}

	s1.End()
	if quiet() {
		t.Error("quiet() = true after ending all Silences, want false")
	}
	if out := captureStdout(t, func() { LogInfo("visible") }); out != "[>] visible\n" {
		t.Errorf("LogInfo printed %q after Silences ended, want %q", out, "[>] visible\n")
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
	// isTTY must be true for Confirm to read stdin at all, so the prompt icon
	// comes out styled; build the expected prefix the same way Confirm does.
	withTTY(t, true)

	cases := []string{"y", "Y", "yes", "YES", "  yes  "}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			withStdin(t, in+"\n")
			out := captureStdout(t, func() { Confirm("Proceed?") })
			want := formatMsg(Styled("?", Bold, Magenta), "Proceed? [y/n]") + " "
			if out != want {
				t.Errorf("Confirm output = %q, want %q", out, want)
			}
		})
	}
}

func TestConfirmRetriesOnInvalidInput(t *testing.T) {
	withTTY(t, true)
	withStdin(t, "maybe\ny\n")

	out := captureStdout(t, func() { Confirm("Proceed?") })
	want := formatMsg(Styled("?", Bold, Magenta), "Proceed? [y/n]") + " Please answer yes or no: "
	if out != want {
		t.Errorf("Confirm output = %q, want %q", out, want)
	}
}

func TestConfirmNo(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = true
		Confirm("Delete %s?", "file.txt")
		return
	}

	out, code := runFailHelperWithStdin(t, "TestConfirmNo", "n\n")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	// Built by hand, not via Styled: this process's stdout is not a TTY.
	want := "[\x1b[1;35m?\x1b[0m] Delete file.txt? [y/n] [\x1b[1;31m!\x1b[0m] Operation canceled by user.\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestConfirmForce(t *testing.T) {
	withTTY(t, false)
	withForce(t, true)

	out := captureStdout(t, func() { Confirm("Delete %s?", "file.txt") })
	if out != "" {
		t.Errorf("Confirm printed %q while -f/--yes is set, want nothing", out)
	}
}

func TestConfirmForceNo(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		Args.ForceNo = true
		Confirm("Delete %s?", "file.txt")
		return
	}

	out, code := runFailHelper(t, "TestConfirmForceNo")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "[?] Delete file.txt? [y/n] n\n[!] Operation canceled by -n/--no.\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestConfirmEOF(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = true
		Confirm("Delete %s?", "file.txt")
		return
	}

	out, code := runFailHelperWithStdin(t, "TestConfirmEOF", "")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	// Built by hand, not via Styled: this process's stdout is not a TTY.
	want := "[\x1b[1;35m?\x1b[0m] Delete file.txt? [y/n] [\x1b[1;31m!\x1b[0m] Operation canceled by user.\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestConfirmNonTTY(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		Confirm("Delete %s?", "file.txt")
		return
	}

	out, code := runFailHelper(t, "TestConfirmNonTTY")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "[?] Delete file.txt? [y/n] n\n[!] Output is not a tty, use -f to confirm.\n"; out != want {
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
		isTTY = false
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

func TestAssertPass(t *testing.T) {
	withTTY(t, false)
	if out := captureStdout(t, func() { Assert(true, "should not print") }); out != "" {
		t.Errorf("Assert(true, ...) printed %q, want nothing", out)
	}
}

func TestAssertFail(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		Assert(1 == 2, "expected %d to equal %d", 1, 2)
		return
	}

	out, code := runFailHelper(t, "TestAssertFail")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "[!] expected 1 to equal 2\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestCheckPass(t *testing.T) {
	withTTY(t, false)
	if out := captureStdout(t, func() { Check(nil, "should not print") }); out != "" {
		t.Errorf("Check(nil, ...) printed %q, want nothing", out)
	}
}

func TestCheckFail(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		Check(fmt.Errorf("disk full"), "failed to write %s", "file.txt")
		return
	}

	out, code := runFailHelper(t, "TestCheckFail")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "[!] failed to write file.txt: disk full\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestLogError(t *testing.T) {
	withTTY(t, false)

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

func TestLogTaskNonTTY(t *testing.T) {
	withTTY(t, false)
	withUnicode(t, true) // ASCII icons are used anyway, since stdout is not a TTY
	var task *Task
	got := captureStdout(t, func() {
		task = LogTask("Build %d", 1)
		task.LogString("a")
		task.LogString("b")
		task.Done()
	})
	if want := "[$] Running task: build 1\n    a\n    b\n[+] Task succeeded: build 1\n"; got != want {
		t.Errorf("printed %q, want %q", got, want)
	}
	if len(task.logs) != 2 {
		t.Errorf("stored %d log lines, want 2", len(task.logs))
	}
}

func TestLogTaskTTY(t *testing.T) {
	withTTY(t, true)
	withUnicode(t, true)
	var task *Task
	got := captureStdout(t, func() {
		task = LogTask("Build")
		for i := 0; i < 6; i++ {
			task.LogString(fmt.Sprint("line ", i))
		}
		task.Done()
	})
	if len(task.logs) != 6 {
		t.Errorf("stored %d log lines, want 6", len(task.logs))
	}
	// Last render before Done shows the header and the last Args.LogDepth logs.
	if !strings.Contains(got, "\n    line 2\n    line 3\n    line 4\n    line 5\n") {
		t.Errorf("last logs not shown: %q", got)
	}
	// Done clears the header and 4 log rows and prints only the done header.
	want := fmt.Sprintf("\x1b[%dF\x1b[J%s\n", 1+Args.LogDepth, formatMsg(Styled("✓", Bold, Green), "Task succeeded: build"))
	if !strings.HasSuffix(got, want) {
		t.Errorf("output ends with %q, want suffix %q", got[max(0, len(got)-40):], want)
	}
	select {
	case <-task.exited:
	default:
		t.Error("animation goroutine still running after Done")
	}
}

func TestTaskFailNonTTY(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		isUnicode = true
		task := LogTask("Build")
		task.LogString("a")
		task.Fail()
		return
	}

	out, code := runFailHelper(t, "TestTaskFailNonTTY")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "[$] Running task: build\n    a\n[x] Task failed: build\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

// runTaskFailTTY is the child-process body of the TaskFailTTY tests: it logs
// n lines to a TTY task, then fails it.
func runTaskFailTTY(n int) {
	isTTY = true
	isUnicode = true
	task := LogTask("Build")
	for i := 0; i < n; i++ {
		task.LogString(fmt.Sprint("line ", i))
	}
	task.Fail()
}

// wantTaskFailTTY returns the expected tail of a failed TTY task's output: the
// failed header replacing the last render, then the entire log of n lines, then
// the failed header again if n > Args.LogDepth. Built by hand: this process's
// stdout is not a TTY, so Styled would not style.
func wantTaskFailTTY(n int) string {
	header := "[\x1b[1;31m✗\x1b[0m] Task failed: build\n"
	want := fmt.Sprintf("\x1b[%dF\x1b[J", 1+Args.LogDepth) + header
	for i := 0; i < n; i++ {
		want += fmt.Sprint("    line ", i, "\n")
	}
	if n > Args.LogDepth {
		want += header
	}
	return want
}

func TestTaskFailTTYLongLog(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		runTaskFailTTY(Args.LogDepth + 2)
		return
	}
	out, code := runFailHelper(t, "TestTaskFailTTYLongLog")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := wantTaskFailTTY(Args.LogDepth + 2); !strings.HasSuffix(out, want) {
		t.Errorf("output = %q, want suffix %q", out, want)
	}
}

func TestTaskFailTTYShortLog(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		runTaskFailTTY(Args.LogDepth)
		return
	}
	out, code := runFailHelper(t, "TestTaskFailTTYShortLog")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := wantTaskFailTTY(Args.LogDepth); !strings.HasSuffix(out, want) {
		t.Errorf("output = %q, want suffix %q", out, want)
	}
}

func TestSpinnerFrame(t *testing.T) {
	withTTY(t, false)
	withUnicode(t, false)
	if got := spinnerFrame(5); got != "/" {
		t.Errorf("ASCII spinnerFrame(5) = %q, want %q", got, "/")
	}
	withUnicode(t, true)
	if got := spinnerFrame(11); got != "⠙" {
		t.Errorf("Unicode spinnerFrame(11) = %q, want %q", got, "⠙")
	}
}

func TestTaskLabel(t *testing.T) {
	cases := []struct{ msg, status, want string }{
		{"Build", "", "Build"},
		{"Build", "Task failed: ", "Task failed: build"},
		{"build", "Task failed: ", "Task failed: build"},
		{"ÄB", "Task failed: ", "Task failed: äB"},
		{"", "Task failed: ", "Task failed: "},
	}
	for _, c := range cases {
		if got := (&Task{msg: c.msg}).label(c.status); got != c.want {
			t.Errorf("Task{msg: %q}.label(%q) = %q, want %q", c.msg, c.status, got, c.want)
		}
	}
}
