// io.go: functions that report to or prompt the user of the CLI tool.

package main

import (
	"fmt"
	"os"
	"regexp"
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
	Bold       Style = "1"
	Dim        Style = "2"
	Italic     Style = "3"
	Underline  Style = "4"
	Blink      Style = "5"
	Reverse    Style = "7"
	Red        Style = "31"
	Green      Style = "32"
	Yellow     Style = "33"
	Blue       Style = "34"
	Magenta    Style = "35"
	Cyan       Style = "36"
	Gray       Style = "90"
	BrightBlue Style = "94"
)

// The styles of syntax highlighting, shared by all languages, after Vim's
// default colors.
const (
	CodeKeyword  = Yellow
	CodeType     = Green
	CodeFunction = Cyan
	CodeLiteral  = Magenta
	CodeComment  = Gray
	CodePreProc  = BrightBlue // Preprocessor directives and annotations
)

// Logs, tasks and prompts go to stderr, keeping stdout for command results,
// such as a list meant for scripts.

// isTerminal reports whether f is a terminal.
func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// isTTY is true when both stderr and stdout are terminals, meaning we can be
// interactive and use styles. If either is redirected, e.g. to a file or a
// pipe, output stays plain on both. --tty and --notty override it.
var isTTY = isTerminal(os.Stderr) && isTerminal(os.Stdout)

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
// Returns text unchanged if it's empty, stderr is not a terminal or no styles
// are given.
func Styled(text string, styles ...Style) string {
	if text == "" || !isTTY || len(styles) == 0 {
		return text
	}
	codes := make([]string, len(styles))
	for i, s := range styles {
		codes[i] = string(s)
	}
	return "\x1b[" + strings.Join(codes, ";") + "m" + text + "\x1b[0m"
}

// endStyles returns line followed by the ANSI code that ends all styles, if
// it has any, so styles in a subprocess's log line, e.g. compiler colors,
// can't leak past it even if it's cut short.
func endStyles(line string) string {
	if !strings.Contains(line, "\x1b") {
		return line
	}
	return line + "\x1b[0m"
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
				if !isEscape(r, &inEsc) {
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

// AlignColumns renders rows as lines starting with indent, with each column
// padded to its widest cell plus two spaces, and no trailing spaces. ANSI
// escape codes count as zero width, so styled cells stay aligned.
func AlignColumns(rows [][]string, indent string) string {
	var widths []int
	for _, row := range rows {
		for i, cell := range row {
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], visibleLen(cell))
		}
	}
	var out strings.Builder
	for _, row := range rows {
		line := indent
		for i, cell := range row {
			line += cell + strings.Repeat(" ", widths[i]-visibleLen(cell)+2)
		}
		out.WriteString(strings.TrimRight(line, " ") + "\n")
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
		if !isEscape(r, &inEsc) {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// expandTabs returns s with tabs replaced by spaces up to the next tab stop,
// every width columns, ignoring ANSI escape codes. The pager needs it, since a
// tab moves the cursor without overwriting what was drawn before.
func expandTabs(s string, width int) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	var out strings.Builder
	inEsc, col := false, 0
	for _, r := range s {
		switch {
		case isEscape(r, &inEsc):
			out.WriteRune(r)
		case r == '\t':
			out.WriteString(strings.Repeat(" ", width-col%width))
			col += width - col%width
		case r == '\n':
			out.WriteRune(r)
			col = 0
		default:
			out.WriteRune(r)
			col++
		}
	}
	return out.String()
}

// isEscape reports whether r, the next rune of a string, is part of an ANSI
// escape code. inEsc tracks whether an escape code is open, starting false.
func isEscape(r rune, inEsc *bool) bool {
	switch {
	case r == '\x1b':
		*inEsc = true
	case *inEsc:
		*inEsc = r == '[' || r < '@' || r > '~'
	default:
		return false
	}
	return true
}

// PrintResult prints s, a command's result, to stdout. Never suppressed by
// -q/--quiet or Silence.
func PrintResult(s string) {
	fmt.Fprint(os.Stdout, s)
}

// PageResult prints s like PrintResult, but in a pager if the terminal can't
// show it all at once: up and down scroll by a line, page up and page down
// (or space) by a page, and q, escape or Ctrl+C quit.
func PageResult(s string) {
	stdin, stdout := int(os.Stdin.Fd()), int(os.Stdout.Fd())
	if !isTTY || !isTerminal(os.Stdin) {
		PrintResult(s)
		return
	}
	s = expandTabs(s, Args.TabWidth)
	width, height, err := term.GetSize(stdout)
	if err != nil || strings.Count(WrapText(s, width), "\n") < height {
		PrintResult(s)
		return
	}
	state, err := term.MakeRaw(stdin)
	if err != nil {
		PrintResult(s)
		return
	}
	defer term.Restore(stdin, state)
	fmt.Fprint(os.Stdout, "\x1b[?1049h\x1b[?25l") // The alternate screen, without the cursor.
	defer fmt.Fprint(os.Stdout, "\x1b[?25h\x1b[?1049l")
	key := make([]byte, 16)
	for top := 0; ; {
		width, height, _ = term.GetSize(stdout) // Again, in case the terminal was resized.
		lines := strings.Split(strings.TrimSuffix(WrapText(s, width), "\n"), "\n")
		var frame string
		frame, top = pagerFrame(lines, top, height)
		fmt.Fprint(os.Stdout, frame)
		n, err := os.Stdin.Read(key)
		quit := err != nil
		if !quit {
			top, quit = pagerKey(string(key[:n]), top, height-1)
		}
		if quit {
			return
		}
	}
}

// pagerKey returns the pager's top line after the key press key, given the
// page size, or whether to quit.
func pagerKey(key string, top, page int) (int, bool) {
	switch key {
	case "\x1b[A", "\x1bOA":
		return top - 1, false
	case "\x1b[B", "\x1bOB":
		return top + 1, false
	case "\x1b[5~":
		return top - page, false
	case "\x1b[6~", " ":
		return top + page, false
	case "q", "Q", "\x1b", "\x03":
		return top, true
	}
	return top, false
}

// pagerFrame returns the ANSI codes that draw lines from top, clamped so the
// last page is full, on a terminal height lines high, with a status line at
// the bottom. It also returns the clamped top.
func pagerFrame(lines []string, top, height int) (string, int) {
	page := max(height-1, 1)
	top = max(min(top, len(lines)-page), 0)
	var frame strings.Builder
	frame.WriteString("\x1b[H")
	for i := top; i < top+page; i++ {
		if i < len(lines) {
			frame.WriteString(endStyles(lines[i]))
		}
		frame.WriteString("\x1b[K\r\n")
	}
	status := fmt.Sprintf(" Lines %d-%d of %d, %s to scroll, PgUp/PgDn to page, Q to quit ", top+1, min(top+page, len(lines)), len(lines), unicodeOr("↑/↓", "Up/Down"))
	frame.WriteString(Styled(status, Reverse) + "\x1b[K")
	return frame.String(), top
}

// sourceQuote matches the source and caret lines of a GD++ error, e.g. " 12 | var x".
var sourceQuote = regexp.MustCompile(`^ [0-9 ]+ \| `)

// formatMsg returns msg with a "[icon] " prefix, wrapped to the terminal width
// if stderr is a terminal, with continuation lines indented under the prefix.
// Source and caret lines of GD++ errors are not wrapped, which would misalign the caret.
func formatMsg(icon, msg string) string {
	width := 0
	if isTTY {
		width, _, _ = term.GetSize(int(os.Stderr.Fd()))
	}
	indent := visibleLen(icon) + 3 // "[", icon, "] "
	lines := strings.Split(msg, "\n")
	for i, line := range lines {
		if !sourceQuote.MatchString(line) {
			lines[i] = WrapText(line, width-indent)
		}
	}
	msg = strings.Join(lines, "\n")
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

// logMsg prints a formatted message with icon, unless suppressible and
// suppressed by -q/--quiet or Silence.
func logMsg(icon string, suppressible bool, format string, params []any) {
	if !suppressible {
		failRunningTask()
	}
	if !suppressible || !quiet() {
		fmt.Fprintln(os.Stderr, formatMsg(icon, fmt.Sprintf(format, params...)))
	}
}

// LogInfo prints a formatted info message. Suppressed by -q/--quiet or Silence.
func LogInfo(format string, params ...any) { logMsg(infoIcon(), true, format, params) }

// LogWarn prints a formatted warning. Suppressed by -q/--quiet or Silence.
func LogWarn(format string, params ...any) { logMsg(Styled("!", Bold, Yellow), true, format, params) }

// LogError prints a formatted error.
func LogError(format string, params ...any) { logMsg(errorIcon(), false, format, params) }

// infoIcon is the icon of info messages and succeeded tasks.
func infoIcon() string { return Styled(unicodeOr("•", "-"), Green) }

// errorIcon is the icon of errors and failed tasks.
func errorIcon() string { return Styled(unicodeOr("×", "x"), Bold, Red) }

// FailWithText prints err as an error, then exits the program via Fail. Unlike
// LogFatal, it takes err's text as is, e.g. a GD++ error with a caret under the problem.
func FailWithText(err error) {
	logMsg(errorIcon(), false, "%s", []any{err.Error()})
	Fail()
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

// runningTask is the task started by LogTask and not yet finished, if any.
var runningTask *Task

// failRunningTask marks the running task, if any, as failed, so an error
// printed during it lands below its final message rather than below its log
// rows, and its animation can't draw over the error.
func failRunningTask() {
	if runningTask != nil {
		runningTask.finish(errorIcon(), "Task failed: ", false)
	}
}

// LogTask prints a formatted task message and returns the task. On a terminal,
// the icon is animated until Done is called, with the latest log lines shown
// under the message, unless hidden per taskLogsHidden. Under -q/--quiet or
// Silence, the message and log lines are erased once the task succeeds.
func LogTask(format string, params ...any) *Task {
	t := &Task{msg: fmt.Sprintf(format, params...), stop: make(chan struct{}), exited: make(chan struct{})}
	runningTask = t
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

// taskLogsHidden reports whether task log lines are hidden: per -q/--quiet,
// and off a terminal also per Silence, since there they can't be erased once
// the task is done.
func taskLogsHidden() bool {
	return !Args.Verbose && Args.Quiet || !isTTY && quiet()
}

// LogString adds a line to the task log. Hidden per taskLogsHidden, though the
// line is kept so Fail can still report it.
func (t *Task) LogString(s string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.logs = append(t.logs, s)
	if !isTTY {
		if !taskLogsHidden() {
			fmt.Fprintln(os.Stderr, taskLogIndent+s)
		}
		return
	}
	if !taskLogsHidden() {
		t.render(spinnerFrame(t.frame), t.msg, true)
	}
}

// Done marks the task as completed. On a terminal, it stops the animation and
// clears the log lines shown under the task message; under -q/--quiet or
// Silence, it erases the task message too, leaving no trace. Otherwise, it
// prints the final task message.
func (t *Task) Done() {
	t.finish(infoIcon(), "Task succeeded: ", quiet())
}

// Fail marks the task as failed, then exits the program via Fail. Never
// suppressed by -q/--quiet or Silence: it stops the animation, prints the
// entire task log (catching up on lines that were hidden while running) and,
// on a terminal, repeats the failed task message below a long log.
func (t *Task) Fail() {
	failIcon := errorIcon()
	t.finish(failIcon, "Task failed: ", false)
	if isTTY || quiet() {
		width, _, _ := term.GetSize(int(os.Stderr.Fd()))
		for _, line := range t.logs {
			line = WrapText(line, width-len(taskLogIndent))
			fmt.Fprintln(os.Stderr, endStyles(taskLogIndent+strings.ReplaceAll(line, "\n", "\n"+taskLogIndent)))
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
	return status + lowerFirst(msg)
}

// lowerFirst returns s with its first letter lowercased.
func lowerFirst(s string) string {
	_, size := utf8.DecodeRuneInString(s)
	return strings.ToLower(s[:size]) + s[size:]
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
	return "Failed: " + lowerFirst(t.finalName())
}

// finish prints icon with the task's final message. On a terminal, where the
// running message already showed the task name, it's just finalName;
// otherwise (no running message to overwrite) it's status-prefixed and
// lowercased, via label. On a terminal, it also stops the animation and
// clears the log lines shown under the task message, and with erase set it
// clears the task message too instead of printing anything.
func (t *Task) finish(icon, status string, erase bool) {
	runningTask = nil
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
// fixed, unless hidden per taskLogsHidden. The caller must hold t.mu.
func (t *Task) render(icon, msg string, running bool) {
	width, _, _ := term.GetSize(int(os.Stderr.Fd()))
	var out strings.Builder
	if t.drawn > 0 {
		fmt.Fprintf(&out, "\x1b[%dF\x1b[J", t.drawn) // up to the first drawn row, clear below
	}
	block := formatMsg(icon, msg)
	if running && !taskLogsHidden() {
		logs := t.logs[max(0, len(t.logs)-Args.LogDepth):]
		for i := 0; i < Args.LogDepth; i++ {
			block += "\n"
			if i < len(logs) {
				// Keep each log line to one row, so the row count stays exact.
				line, _, _ := strings.Cut(WrapText(logs[i], width-len(taskLogIndent)), "\n")
				block += taskLogIndent + endStyles(line)
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

	for {
		line, err := readLine()
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

// readLine reads a line from stdin one byte at a time, so that input after
// the line stays unread for the next prompt.
func readLine() (string, error) {
	var line []byte
	b := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(b)
		if n == 1 && b[0] == '\n' {
			return string(line), nil
		}
		if err != nil {
			return "", err
		}
		line = append(line, b[:n]...)
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
