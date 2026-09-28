// io.go: functions that report to or prompt the user of the CLI tool.

package main

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

// Style is an ANSI SGR code, used with Styled.
type Style string

const (
	Bold      Style = "1"
	Dim       Style = "2"
	Italic    Style = "3"
	Underline Style = "4"
	Blink     Style = "5"
	Reverse   Style = "7"
	Red       Style = "31"
	Green     Style = "32"
	Yellow    Style = "33"
	Blue      Style = "34"
	Magenta   Style = "35"
	Cyan      Style = "36"
	Gray      Style = "90"
)

// Logs, tasks and prompts go to stderr, keeping stdout for command results,
// such as a list meant for scripts.

// isTerminal reports whether f is a terminal.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// isTTY is true when stderr is a terminal, meaning we can be interactive.
var isTTY = isTerminal(os.Stderr)

// isStdoutTTY is true when stdout is a terminal, so results can be styled.
var isStdoutTTY = isTerminal(os.Stdout)

// isUnicode is true when the terminal can likely display the Unicode icons.
var isUnicode = func() bool {
	if runtime.GOOS == "windows" {
		return os.Getenv("WT_SESSION") != "" // Windows Terminal; the old console may not
	}
	if os.Getenv("TERM") == "linux" {
		return false // Linux text console, whose fonts lack the icons
	}
	for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := strings.ToLower(os.Getenv(name)); v != "" {
			return strings.Contains(v, "utf-8") || strings.Contains(v, "utf8")
		}
	}
	return false
}()

// unicodeOr returns unicode if stderr is a terminal that can display it, and
// ascii otherwise.
func unicodeOr(unicode, ascii string) string {
	if isUnicode && isTTY {
		return unicode
	}
	return ascii
}

// Styled wraps text in the given styles, e.g. Styled("done", Bold, Green).
// Returns text unchanged if stderr is not a terminal or no styles are given.
func Styled(text string, styles ...Style) string {
	if !isTTY || len(styles) == 0 {
		return text
	}
	codes := make([]string, len(styles))
	for i, s := range styles {
		codes[i] = string(s)
	}
	return "\x1b[" + strings.Join(codes, ";") + "m" + text + "\x1b[0m"
}

// WrapText splits each line of text into lines of at most width visible
// characters, breaking at spaces where possible and inside words otherwise.
// ANSI escape codes count as zero width. Returns text unchanged if width < 1.
func WrapText(text string, width int) string {
	if width < 1 {
		return text
	}
	var out strings.Builder
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			out.WriteByte('\n')
		}
		col := 0
		for j, word := range strings.Split(line, " ") {
			if j > 0 {
				if col+1+visibleLen(word) <= width {
					out.WriteByte(' ')
					col++
				} else {
					out.WriteByte('\n')
					col = 0
				}
			}
			inEsc := false
			for _, r := range word {
				switch {
				case r == '\x1b':
					inEsc = true
				case inEsc:
					inEsc = r == '[' || r < '@' || r > '~'
				default:
					if col == width {
						out.WriteByte('\n')
						col = 0
					}
					col++
				}
				out.WriteRune(r)
			}
		}
	}
	return out.String()
}

// visibleLen returns the number of runes in s, not counting ANSI escape codes.
func visibleLen(s string) int {
	return utf8.RuneCountInString(stripStyles(s))
}

// stripStyles returns s without ANSI escape codes.
func stripStyles(s string) string {
	var out strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEsc = true
		case inEsc:
			inEsc = r == '[' || r < '@' || r > '~'
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// PrintResult prints s, a command's result, to stdout. Never suppressed by
// -q/--quiet or Silence. Styles in s are dropped if stdout is not a terminal.
func PrintResult(s string) {
	if !isStdoutTTY {
		s = stripStyles(s)
	}
	fmt.Fprint(os.Stdout, s)
}

// formatMsg returns msg with a "[icon] " prefix, wrapped to the terminal width
// if stderr is a terminal, with continuation lines indented under the prefix.
func formatMsg(icon, msg string) string {
	width := 0
	if isTTY {
		width, _, _ = term.GetSize(int(os.Stderr.Fd()))
	}
	indent := visibleLen(icon) + 3 // "[", icon, "] "
	msg = WrapText(msg, width-indent)
	return "[" + icon + "] " + strings.ReplaceAll(msg, "\n", "\n"+strings.Repeat(" ", indent))
}

// silenceDepth counts active Silence calls, so nested silences compose:
// logging stays suppressed until every one of them has been ended.
var silenceDepth int

// quiet reports whether logs should currently be suppressed, per -q/--quiet
// or an active Silence. -v/--verbose overrides both, always returning false.
func quiet() bool {
	return !Args.Verbose && (Args.Quiet || silenceDepth > 0)
}

// Silencer is a silenced region started by Silence, ended by calling End.
type Silencer struct{}

// Silence suppresses logs, the same as -q/--quiet, until End is called.
// Silences nest: logging only resumes once every active Silencer has ended.
func Silence() *Silencer {
	silenceDepth++
	return &Silencer{}
}

// End stops this silenced region.
func (s *Silencer) End() {
	silenceDepth--
}

// Log messages are capitalized sentences ending with a period or exclamation
// mark, and prompts end with a question mark. Check messages have no period, since Check appends the
// error. TestLogStyle enforces this.

// LogInfo prints a formatted info message. Suppressed by -q/--quiet or Silence.
func LogInfo(format string, params ...any) {
	if quiet() {
		return
	}
	fmt.Fprintln(os.Stderr, formatMsg(Styled(">", Green), fmt.Sprintf(format, params...)))
}

// LogWarn prints a formatted warning. Suppressed by -q/--quiet or Silence.
func LogWarn(format string, params ...any) {
	if quiet() {
		return
	}
	fmt.Fprintln(os.Stderr, formatMsg(Styled("!", Bold, Yellow), fmt.Sprintf(format, params...)))
}

// LogError prints a formatted error.
func LogError(format string, params ...any) {
	fmt.Fprintln(os.Stderr, formatMsg(Styled("!", Bold, Red), fmt.Sprintf(format, params...)))
}

// LogFatal prints a formatted error, then exits the program via Fail.
func LogFatal(format string, params ...any) {
	LogError(format, params...)
	Fail()
}

// Assert calls LogFatal with the given message if cond is false.
func Assert(cond bool, format string, params ...any) {
	if !cond {
		LogFatal(format, params...)
	}
}

// Check calls LogFatal with the given message, plus err's details and a final
// period, if err is not nil.
func Check(err error, format string, params ...any) {
	if err != nil {
		LogFatal("%s: %s.", fmt.Sprintf(format, params...), strings.TrimSuffix(err.Error(), "."))
	}
}

// taskLogIndent prefixes every task log line.
const taskLogIndent = "    "

// spinnerFrame returns frame n of the icon animation of a running task.
func spinnerFrame(n int) string {
	frames := []string{"|", "/", "-", "\\"}
	if isUnicode {
		frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	}
	return Styled(frames[n%len(frames)], Cyan)
}

// Task is a running task started by LogTask.
type Task struct {
	msg    string
	logs   []string   // all log lines, kept in memory
	mu     sync.Mutex // guards logs, frame, drawn and drawing
	frame  int
	drawn  int           // rows drawn by the last render
	stop   chan struct{} // closed by Done
	exited chan struct{} // closed when the animation goroutine returns
}

// LogTask prints a formatted task message and returns the task. On a terminal,
// the icon is animated until Done is called, with the latest log lines shown
// under the message; -q/--quiet or Silence hides those log lines but not the
// animated message itself, which is erased once the task succeeds.
func LogTask(format string, params ...any) *Task {
	t := &Task{msg: fmt.Sprintf(format, params...), stop: make(chan struct{}), exited: make(chan struct{})}
	if !isTTY {
		fmt.Fprintln(os.Stderr, formatMsg("$", t.label("Running task: ")))
		return t
	}
	t.render(spinnerFrame(0), t.msg, true)
	go func() {
		defer close(t.exited)
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-t.stop:
				return
			case <-ticker.C:
				t.mu.Lock()
				t.frame++
				t.render(spinnerFrame(t.frame), t.msg, true)
				t.mu.Unlock()
			}
		}
	}()
	return t
}

// LogString adds a line to the task log. Suppressed by -q/--quiet or Silence,
// though the line is kept so Fail can still report it.
func (t *Task) LogString(s string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.logs = append(t.logs, s)
	if !isTTY {
		if !quiet() {
			fmt.Fprintln(os.Stderr, taskLogIndent+s)
		}
		return
	}
	if !quiet() {
		t.render(spinnerFrame(t.frame), t.msg, true)
	}
}

// Done marks the task as completed. On a terminal, it stops the animation and
// clears the log lines shown under the task message; under -q/--quiet or
// Silence, it erases the task message too, leaving no trace. Otherwise, it
// prints the final task message.
func (t *Task) Done() {
	t.finish(Styled(unicodeOr("✓", "+"), Bold, Green), "Task succeeded: ", quiet())
}

// Fail marks the task as failed, then exits the program via Fail. Never
// suppressed by -q/--quiet or Silence: it stops the animation, prints the
// entire task log (catching up on lines that were hidden while running) and,
// on a terminal, repeats the failed task message below a long log.
func (t *Task) Fail() {
	failIcon := Styled(unicodeOr("✗", "x"), Bold, Red)
	t.finish(failIcon, "Task failed: ", false)
	if isTTY || quiet() {
		width, _, _ := term.GetSize(int(os.Stderr.Fd()))
		for _, line := range t.logs {
			line = WrapText(line, width-len(taskLogIndent))
			fmt.Fprintln(os.Stderr, taskLogIndent+strings.ReplaceAll(line, "\n", "\n"+taskLogIndent))
		}
		if isTTY && len(t.logs) > Args.LogDepth {
			fmt.Fprintln(os.Stderr, formatMsg(failIcon, t.failedName())) // repeated so the failure is visible below a long log
		}
	}
	Fail()
}

// label returns the task message prefixed with status. If status is set, the
// first letter of the message is lowercased to follow it. The succeeded/failed
// statuses also drop a trailing "...", since the task is no longer pending.
func (t *Task) label(status string) string {
	msg := t.msg
	if status == "Task succeeded: " || status == "Task failed: " {
		msg = strings.TrimSuffix(msg, "...")
	}
	if status == "" {
		return msg
	}
	_, size := utf8.DecodeRuneInString(msg)
	return status + strings.ToLower(msg[:size]) + msg[size:]
}

// finalName returns the task name shown on a terminal once a task is done,
// case preserved, with a trailing "..." replaced by ".".
func (t *Task) finalName() string {
	return strings.TrimSuffix(t.msg, "...") + "."
}

// failedName returns finalName prefixed with "Failed: ", lowercasing the
// first letter to follow it. Used for the failure line repeated below a long
// log, since the header replacing the progress line already showed the name.
func (t *Task) failedName() string {
	name := t.finalName()
	_, size := utf8.DecodeRuneInString(name)
	return "Failed: " + strings.ToLower(name[:size]) + name[size:]
}

// finish prints icon with the task's final message. On a terminal, where the
// running message already showed the task name, it's just finalName;
// otherwise (no running message to overwrite) it's status-prefixed and
// lowercased, via label. On a terminal, it also stops the animation and
// clears the log lines shown under the task message, and with erase set it
// clears the task message too instead of printing anything.
func (t *Task) finish(icon, status string, erase bool) {
	if !isTTY {
		fmt.Fprintln(os.Stderr, formatMsg(icon, t.label(status)))
		return
	}
	close(t.stop)
	<-t.exited
	t.mu.Lock()
	defer t.mu.Unlock()
	if erase {
		fmt.Fprintf(os.Stderr, "\x1b[%dF\x1b[J", t.drawn) // up to the first drawn row, clear below
		t.drawn = 0
		return
	}
	t.render(icon, t.finalName(), false)
}

// render redraws msg over the previous render, leaving the cursor at the
// start of the line below. While running, it also draws the last
// --log-depth log lines, padded with empty rows so the block height is
// fixed, unless suppressed by -q/--quiet or Silence. The caller must hold
// t.mu.
func (t *Task) render(icon, msg string, running bool) {
	width, _, _ := term.GetSize(int(os.Stderr.Fd()))
	var out strings.Builder
	if t.drawn > 0 {
		fmt.Fprintf(&out, "\x1b[%dF\x1b[J", t.drawn) // up to the first drawn row, clear below
	}
	block := formatMsg(icon, msg)
	if running && !quiet() {
		logs := t.logs[max(0, len(t.logs)-Args.LogDepth):]
		for i := 0; i < Args.LogDepth; i++ {
			block += "\n"
			if i < len(logs) {
				// Keep each log line to one row, so the row count stays exact.
				line, _, _ := strings.Cut(WrapText(logs[i], width-len(taskLogIndent)), "\n")
				block += taskLogIndent + line
			}
		}
	}
	out.WriteString(block + "\n")
	t.drawn = strings.Count(block, "\n") + 1
	fmt.Fprint(os.Stderr, out.String())
}

// Confirm prints a formatted yes/no prompt and blocks until the user answers.
// If the user answers no, it exits the program via LogFatal. If stderr is not
// a terminal, it assumes no rather than blocking on an answer that can't come.
// -f/--yes skips the prompt and assumes yes. -n/--no skips the prompt, assumes
// no, and says so.
func Confirm(format string, params ...any) {
	if Args.Force {
		return
	}

	fmt.Fprint(os.Stderr, formatMsg(Styled("?", Bold, Magenta), fmt.Sprintf(format, params...)+" [y/n]")+" ")

	if Args.ForceNo {
		fmt.Fprintln(os.Stderr, "n")
		LogFatal("Operation canceled by -n/--no.")
	}

	if !isTTY {
		fmt.Fprintln(os.Stderr, "n")
		LogFatal("Output is not a tty, use -f to confirm.")
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			LogFatal("Operation canceled by user.")
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			return
		case "n", "no":
			LogFatal("Operation canceled by user.")
		default:
			fmt.Fprint(os.Stderr, "Please answer yes or no: ")
		}
	}
}

// Audit prints a formatted yes/no prompt via Confirm, but only if --audit was
// passed. Otherwise it does nothing.
func Audit(format string, params ...any) {
	if !Args.Audit {
		return
	}
	Confirm(format, params...)
}

// cleanups holds functions to run before the program exits via Fail.
var cleanups []func()

// Cleanup registers a function to run if the program exits via Fail.
func Cleanup(f func()) {
	cleanups = append(cleanups, f)
}

// Fail runs all registered cleanups in reverse order, then exits with status 1.
func Fail() {
	for i := len(cleanups) - 1; i >= 0; i-- {
		cleanups[i]()
	}
	os.Exit(1)
}
