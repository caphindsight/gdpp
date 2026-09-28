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

// isStdinTTY is true when stdin is a terminal.
var isStdinTTY = func() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}()

// isStdoutTTY is true when stdout is a terminal.
var isStdoutTTY = func() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}()

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

// unicodeOr returns unicode if stdout is a terminal that can display it, and
// ascii otherwise.
func unicodeOr(unicode, ascii string) string {
	if isUnicode && isStdoutTTY {
		return unicode
	}
	return ascii
}

// Styled wraps text in the given styles, e.g. Styled("done", Bold, Green).
// Returns text unchanged if stdout is not a terminal or no styles are given.
func Styled(text string, styles ...Style) string {
	if !isStdoutTTY || len(styles) == 0 {
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
	n, inEsc := 0, false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEsc = true
		case inEsc:
			inEsc = r == '[' || r < '@' || r > '~'
		default:
			n++
		}
	}
	return n
}

// formatMsg returns msg with a "[icon] " prefix, wrapped to the terminal width
// if stdout is a terminal, with continuation lines indented under the prefix.
func formatMsg(icon, msg string) string {
	width := 0
	if isStdoutTTY {
		width, _, _ = term.GetSize(int(os.Stdout.Fd()))
	}
	indent := visibleLen(icon) + 3 // "[", icon, "] "
	msg = WrapText(msg, width-indent)
	return "[" + icon + "] " + strings.ReplaceAll(msg, "\n", "\n"+strings.Repeat(" ", indent))
}

// LogInfo prints a formatted info message.
func LogInfo(format string, params ...any) {
	fmt.Println(formatMsg(Styled(">", Green), fmt.Sprintf(format, params...)))
}

// LogWarn prints a formatted warning.
func LogWarn(format string, params ...any) {
	fmt.Println(formatMsg(Styled("!", Bold, Yellow), fmt.Sprintf(format, params...)))
}

// LogError prints a formatted error.
func LogError(format string, params ...any) {
	fmt.Println(formatMsg(Styled("!", Bold, Red), fmt.Sprintf(format, params...)))
}

// LogFatal prints a formatted error, then exits the program via Fail.
func LogFatal(format string, params ...any) {
	LogError(format, params...)
	Fail()
}

// taskLogLines is how many of the latest log lines are shown under a running task.
const taskLogLines = 4

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
// the icon is animated and the latest log lines are shown under the message
// until Done is called.
func LogTask(format string, params ...any) *Task {
	t := &Task{msg: fmt.Sprintf(format, params...), stop: make(chan struct{}), exited: make(chan struct{})}
	if !isStdoutTTY {
		fmt.Println(formatMsg("$", t.label("Running task: ")))
		return t
	}
	t.render(spinnerFrame(0), "", true)
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
				t.render(spinnerFrame(t.frame), "", true)
				t.mu.Unlock()
			}
		}
	}()
	return t
}

// LogString adds a line to the task log.
func (t *Task) LogString(s string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.logs = append(t.logs, s)
	if !isStdoutTTY {
		fmt.Println(taskLogIndent + s)
		return
	}
	t.render(spinnerFrame(t.frame), "", true)
}

// Done marks the task as completed. On a terminal, it stops the animation and
// clears the log lines shown under the task message.
func (t *Task) Done() {
	t.finish(Styled(unicodeOr("✓", "+"), Bold, Green), "Task succeeded: ")
}

// Fail marks the task as failed, then exits the program via Fail. On a
// terminal, it stops the animation, prints the entire task log and, if it
// is longer than taskLogLines, repeats the failed task message below it.
func (t *Task) Fail() {
	failIcon := Styled(unicodeOr("✗", "x"), Bold, Red)
	t.finish(failIcon, "Task failed: ")
	if isStdoutTTY {
		width, _, _ := term.GetSize(int(os.Stdout.Fd()))
		for _, line := range t.logs {
			line = WrapText(line, width-len(taskLogIndent))
			fmt.Println(taskLogIndent + strings.ReplaceAll(line, "\n", "\n"+taskLogIndent))
		}
		if len(t.logs) > taskLogLines {
			fmt.Println(formatMsg(failIcon, t.label("Task failed: "))) // repeated so the failure is visible below a long log
		}
	}
	Fail()
}

// label returns the task message prefixed with status. If status is set, the
// first letter of the message is lowercased to follow it.
func (t *Task) label(status string) string {
	if status == "" {
		return t.msg
	}
	_, size := utf8.DecodeRuneInString(t.msg)
	return status + strings.ToLower(t.msg[:size]) + t.msg[size:]
}

// finish prints the task message with the final icon and status prefix. On a terminal, it stops
// the animation and clears the log lines shown under the task message.
func (t *Task) finish(icon, status string) {
	if !isStdoutTTY {
		fmt.Println(formatMsg(icon, t.label(status)))
		return
	}
	close(t.stop)
	<-t.exited
	t.mu.Lock()
	defer t.mu.Unlock()
	t.render(icon, status, false)
}

// render redraws the task message, prefixed with status, over the previous render, leaving the cursor
// at the start of the line below. While running, it also draws the last
// taskLogLines log lines, padded with empty rows so the block height is fixed.
// The caller must hold t.mu.
func (t *Task) render(icon, status string, running bool) {
	width, _, _ := term.GetSize(int(os.Stdout.Fd()))
	var out strings.Builder
	if t.drawn > 0 {
		fmt.Fprintf(&out, "\x1b[%dF\x1b[J", t.drawn) // up to the first drawn row, clear below
	}
	block := formatMsg(icon, t.label(status))
	if running {
		logs := t.logs[max(0, len(t.logs)-taskLogLines):]
		for i := 0; i < taskLogLines; i++ {
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
	fmt.Print(out.String())
}

// Confirm prints a formatted yes/no prompt and blocks until the user answers.
// If the user answers no, it exits the program via LogFatal. If stdin is not
// a terminal, it assumes no rather than blocking on an answer that can't come.
func Confirm(format string, params ...any) {
	fmt.Print(formatMsg(Styled("?", Bold, Magenta), fmt.Sprintf(format, params...)+" [y/n]") + " ")

	if !isStdinTTY {
		fmt.Println("n")
		LogFatal("Input is not a tty, use -f to confirm.")
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
			fmt.Print("Please answer yes or no: ")
		}
	}
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
