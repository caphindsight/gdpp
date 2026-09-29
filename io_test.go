// io_test.go: tests for io.go.

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

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
		{"empty text, tty", true, "", []Style{Red}, ""},
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

func TestPrintResult(t *testing.T) {
	withTTY(t, true)
	withQuiet(t, true)
	styled := Styled("done", Bold) + " ok\n"
	if out := captureStdout(t, func() { PrintResult(styled) }); out != styled {
		t.Errorf("printed %q, want %q", out, styled)
	}
}

func TestLogsGoToStderr(t *testing.T) {
	withTTY(t, false)
	withQuiet(t, false)
	out := captureStdout(t, func() {
		captureStderr(t, func() { LogInfo("Hi.") })
	})
	if out != "" {
		t.Errorf("stdout = %q, want logs only on stderr", out)
	}
}

func TestLogInfo(t *testing.T) {
	// Force non-TTY so the "[-] " prefix is a plain literal, independent of
	// Styled's ANSI-wrapping behavior (covered separately by TestStyled).
	withTTY(t, false)

	cases := []struct {
		name   string
		format string
		params []any
		want   string
	}{
		{"plain message", "hello", nil, "[-] hello\n"},
		{"formatted message", "hello, %s! count=%d", []any{"world", 3}, "[-] hello, world! count=3\n"},
		{"multiline message re-indented", "line1\nline2", nil, "[-] line1\n    line2\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := captureStderr(t, func() { LogInfo(c.format, c.params...) })
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
			got := captureStderr(t, func() { LogWarn(c.format, c.params...) })
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
	if out := captureStderr(t, func() { LogInfo("hidden") }); out != "" {
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
	if out := captureStderr(t, func() { LogInfo("visible") }); out != "[-] visible\n" {
		t.Errorf("LogInfo printed %q after Silences ended, want %q", out, "[-] visible\n")
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

func TestConfirmYes(t *testing.T) {
	// isTTY must be true for Confirm to read stdin at all, so the prompt icon
	// comes out styled; build the expected prefix the same way Confirm does.
	withTTY(t, true)

	cases := []string{"y", "Y", "yes", "YES", "  yes  "}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			withStdin(t, in+"\n")
			out := captureStderr(t, func() { Confirm("Proceed?") })
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

	out := captureStderr(t, func() { Confirm("Proceed?") })
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
	want := "[\x1b[1;35m?\x1b[0m] Delete file.txt? [y/n] [\x1b[1;31m×\x1b[0m] Operation canceled by user.\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestConfirmForce(t *testing.T) {
	withTTY(t, false)
	withForce(t, true)

	out := captureStderr(t, func() { Confirm("Delete %s?", "file.txt") })
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
	if want := "[?] Delete file.txt? [y/n] n\n[x] Operation canceled by -n/--no.\n"; out != want {
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
	want := "[\x1b[1;35m?\x1b[0m] Delete file.txt? [y/n] [\x1b[1;31m×\x1b[0m] Operation canceled by user.\n"
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
	if want := "[?] Delete file.txt? [y/n] n\n[x] Output is not a tty, use -f to confirm.\n"; out != want {
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
		LogFatal("Boom: %s.", "oops")
		return
	}

	out, code := runFailHelper(t, "TestLogFatal")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "[x] Boom: oops.\ncleaned up"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestAssertPass(t *testing.T) {
	withTTY(t, false)
	if out := captureStderr(t, func() { Assert(true, "Should not print.") }); out != "" {
		t.Errorf("Assert(true, ...) printed %q, want nothing", out)
	}
}

func TestAssertFail(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		Assert(1 == 2, "Expected %d to equal %d.", 1, 2)
		return
	}

	out, code := runFailHelper(t, "TestAssertFail")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "[x] Expected 1 to equal 2.\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestCheckPass(t *testing.T) {
	withTTY(t, false)
	if out := captureStderr(t, func() { Check(nil, "Should not print") }); out != "" {
		t.Errorf("Check(nil, ...) printed %q, want nothing", out)
	}
}

func TestCheckFail(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		Check(fmt.Errorf("disk full"), "Failed to write %s", "file.txt")
		return
	}

	out, code := runFailHelper(t, "TestCheckFail")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "[x] Failed to write file.txt: disk full.\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestCheckFailErrorWithPeriod(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		Check(fmt.Errorf("Disk full."), "Failed to write %s", "file.txt")
		return
	}

	out, _ := runFailHelper(t, "TestCheckFailErrorWithPeriod")
	if want := "[x] Failed to write file.txt: Disk full.\n"; out != want {
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
		{"plain message", "hello", nil, "[x] hello\n"},
		{"formatted message", "hello, %s! count=%d", []any{"world", 3}, "[x] hello, world! count=3\n"},
		{"multiline message re-indented", "line1\nline2", nil, "[x] line1\n    line2\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := captureStderr(t, func() { LogError(c.format, c.params...) })
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
	got := captureStderr(t, func() {
		task = LogTask("Build %d.", 1)
		task.LogString("a")
		task.LogString("b")
		task.Done()
	})
	if want := "[$] Running task: build 1.\n    a\n    b\n[-] Task succeeded: build 1.\n"; got != want {
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
	got := captureStderr(t, func() {
		task = LogTask("Build...")
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
	// Done clears the header and 4 log rows and prints just the task name,
	// case preserved, with "..." replaced by ".".
	want := fmt.Sprintf("\x1b[%dF\x1b[J%s\n", 1+Args.LogDepth, formatMsg(infoIcon(), "Build."))
	if !strings.HasSuffix(got, want) {
		t.Errorf("output ends with %q, want suffix %q", got[max(0, len(got)-40):], want)
	}
	select {
	case <-task.exited:
	default:
		t.Error("animation goroutine still running after Done")
	}
}

func TestLogTaskQuietNonTTY(t *testing.T) {
	withTTY(t, false)
	withUnicode(t, true)
	withQuiet(t, true)
	var task *Task
	got := captureStderr(t, func() {
		task = LogTask("Build %d.", 1)
		task.LogString("a")
		task.LogString("b")
		task.Done()
	})
	if want := "[$] Running task: build 1.\n[-] Task succeeded: build 1.\n"; got != want {
		t.Errorf("printed %q, want %q", got, want)
	}
	if len(task.logs) != 2 {
		t.Errorf("stored %d log lines, want 2", len(task.logs))
	}
}

func TestLogTaskQuietTTY(t *testing.T) {
	withQuiet(t, true)
	testLogTaskErasedTTY(t)
}

// TestLogTaskSilenceTTY checks that a silenced task on a terminal shows its log
// lines while running, then erases them along with its progress message.
func TestLogTaskSilenceTTY(t *testing.T) {
	defer Silence().End()
	withTTY(t, true)
	withUnicode(t, true)
	got := captureStderr(t, func() {
		task := LogTask("Build...")
		task.LogString("shown")
		task.Done()
	})
	if !strings.Contains(got, "    shown\n") || strings.Contains(got, "•") {
		t.Errorf("output = %q, want the log line shown and no success message", got)
	}
	// The message and 4 log rows were drawn, and Done erases all 5.
	if want := "\x1b[5F\x1b[J"; !strings.HasSuffix(got, want) {
		t.Errorf("output ends with %q, want suffix %q", got[max(0, len(got)-40):], want)
	}
}

// testLogTaskErasedTTY checks that a successful task on a terminal, while
// quiet, hides its log lines and then erases its progress message.
func testLogTaskErasedTTY(t *testing.T) {
	withTTY(t, true)
	withUnicode(t, true)
	got := captureStderr(t, func() {
		task := LogTask("Build...")
		task.LogString("hidden")
		task.Done()
	})
	if strings.Contains(got, "hidden") || strings.Contains(got, "•") {
		t.Errorf("log line or success message leaked while quiet: %q", got)
	}
	// The progress message is drawn without log-line rows under it, so only 1
	// row was ever drawn, and Done erases just that row.
	if want := "\x1b[1F\x1b[J"; !strings.HasSuffix(got, want) {
		t.Errorf("output ends with %q, want suffix %q", got[max(0, len(got)-40):], want)
	}
}

func TestTaskFailQuietNonTTY(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		isUnicode = true
		Args.Quiet = true
		task := LogTask("Build.")
		task.LogString("a")
		task.Fail()
		return
	}

	out, code := runFailHelper(t, "TestTaskFailQuietNonTTY")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	// The running-task header is shown despite -q/--quiet, its log line is not,
	// and the failure still catches up on the entire log.
	if want := "[$] Running task: build.\n[x] Task failed: build.\n    a\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestTaskFailNonTTY(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		isUnicode = true
		task := LogTask("Build.")
		task.LogString("a")
		task.Fail()
		return
	}

	out, code := runFailHelper(t, "TestTaskFailNonTTY")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "[$] Running task: build.\n    a\n[x] Task failed: build.\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

// runTaskFailTTY is the child-process body of the TaskFailTTY tests: it logs
// n lines to a TTY task, then fails it.
func runTaskFailTTY(n int) {
	isTTY = true
	isUnicode = true
	task := LogTask("Build...")
	for i := 0; i < n; i++ {
		task.LogString(fmt.Sprint("line ", i))
	}
	task.Fail()
}

// wantTaskFailTTY returns the expected tail of a failed TTY task's output: the
// failed header replacing the last render, then the entire log of n lines,
// then a "Failed: ..." repeat of the header if n > Args.LogDepth. Built by
// hand: this process's stdout is not a TTY, so Styled would not style.
func wantTaskFailTTY(n int) string {
	header := "[\x1b[1;31m×\x1b[0m] Build.\n"
	want := fmt.Sprintf("\x1b[%dF\x1b[J", 1+Args.LogDepth) + header
	for i := 0; i < n; i++ {
		want += fmt.Sprint("    line ", i, "\n")
	}
	if n > Args.LogDepth {
		want += "[\x1b[1;31m×\x1b[0m] Failed: build.\n"
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
		{"Build...", "Task failed: ", "Task failed: build"},
		{"Build...", "Task succeeded: ", "Task succeeded: build"},
		{"Build...", "Running task: ", "Running task: build..."},
	}
	for _, c := range cases {
		if got := (&Task{msg: c.msg}).label(c.status); got != c.want {
			t.Errorf("Task{msg: %q}.label(%q) = %q, want %q", c.msg, c.status, got, c.want)
		}
	}
}

// TestLogStyle checks that literal messages passed to the logging helpers are
// capitalized sentences ending with a period or exclamation mark, or with a
// question mark for prompts. Check messages must not end with a period, since Check appends the
// error and a period.
func TestLogStyle(t *testing.T) {
	// Index of the format argument, per helper.
	helpers := map[string]int{"LogInfo": 0, "LogWarn": 0, "LogError": 0, "LogFatal": 0, "LogTask": 0, "Assert": 1, "Check": 1, "Confirm": 0, "Audit": 0}
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			// Helpers forward messages already checked at their call sites.
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
				if _, ok := helpers[fd.Name.Name]; ok {
					continue
				}
			}
			ast.Inspect(d, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name, ok := call.Fun.(*ast.Ident)
				if !ok {
					return true
				}
				i, ok := helpers[name.Name]
				if !ok || len(call.Args) <= i {
					return true
				}
				lit, ok := call.Args[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				msg, _ := strconv.Unquote(lit.Value)
				rule, ok := "end with \".\" or \"!\"", strings.HasSuffix(msg, ".") || strings.HasSuffix(msg, "!")
				switch name.Name {
				case "Confirm", "Audit":
					rule, ok = "end with \"?\"", strings.HasSuffix(msg, "?")
				case "Check": // the error is appended, then a period
					rule, ok = "not end with \".\"", !strings.HasSuffix(msg, ".")
				case "LogTask":
					rule, ok = "end with \"...\"", strings.HasSuffix(msg, "...")
				}
				first, _ := utf8.DecodeRuneInString(msg)
				if !unicode.IsUpper(first) || !ok {
					t.Errorf("%s: %s(%s) must start uppercase and %s", fset.Position(lit.Pos()), name.Name, lit.Value, rule)
				}
				return true
			})
		}
	}
}

func TestPagerKey(t *testing.T) {
	for key, want := range map[string]struct {
		top  int
		quit bool
	}{
		"\x1b[A": {9, false}, "\x1bOB": {11, false}, "\x1b[5~": {5, false}, "\x1b[6~": {15, false}, " ": {15, false},
		"q": {10, true}, "Q": {10, true}, "\x1b": {10, true}, "\x03": {10, true}, "x": {10, false},
	} {
		if top, quit := pagerKey(key, 10, 5); top != want.top || quit != want.quit {
			t.Errorf("pagerKey(%q) = %d, %v, want %d, %v", key, top, quit, want.top, want.quit)
		}
	}
}

func TestPagerFrame(t *testing.T) {
	withTTY(t, false)
	lines := []string{"a", "b", "c", "d", "e"}
	cases := []struct {
		top, height, wantTop int
		want                 string
	}{
		{0, 3, 0, "\x1b[Ha\x1b[K\r\nb\x1b[K\r\n Lines 1-2 of 5, Up/Down to scroll, PgUp/PgDn to page, Q to quit \x1b[K"},
		{9, 3, 3, "\x1b[Hd\x1b[K\r\ne\x1b[K\r\n Lines 4-5 of 5, Up/Down to scroll, PgUp/PgDn to page, Q to quit \x1b[K"},
		{-4, 4, 0, "\x1b[Ha\x1b[K\r\nb\x1b[K\r\nc\x1b[K\r\n Lines 1-3 of 5, Up/Down to scroll, PgUp/PgDn to page, Q to quit \x1b[K"},
	}
	for _, tc := range cases {
		if got, top := pagerFrame(lines, tc.top, tc.height); got != tc.want || top != tc.wantTop {
			t.Errorf("pagerFrame(top %d, height %d) = %q, %d\nwant %q, %d", tc.top, tc.height, got, top, tc.want, tc.wantTop)
		}
	}
}
