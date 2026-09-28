// io.go: functions that report to or prompt the user of the CLI tool.

package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

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
