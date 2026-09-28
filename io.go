// io.go: functions that report to or prompt the user of the CLI tool.

package main

import (
	"fmt"
	"os"
	"strings"
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

// isTTY is true when stdout is a terminal.
var isTTY = func() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}()

// Styled wraps text in the given styles, e.g. Styled("done", Bold, Green).
// Returns text unchanged if stdout is not a terminal or no styles are given.
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

// LogInfo prints a formatted info message.
func LogInfo(format string, params ...any) {
	msg := fmt.Sprintf(format, params...)
	msg = strings.ReplaceAll(msg, "\n", "\n    ")
	fmt.Println("[" + Styled(">", Green) + "] " + msg)
}

// LogWarn prints a formatted warning.
func LogWarn(format string, params ...any) {
	msg := fmt.Sprintf(format, params...)
	msg = strings.ReplaceAll(msg, "\n", "\n    ")
	fmt.Println("[" + Styled("!", Bold, Yellow) + "] " + msg)
}

// LogError prints a formatted error.
func LogError(format string, params ...any) {
	msg := fmt.Sprintf(format, params...)
	msg = strings.ReplaceAll(msg, "\n", "\n    ")
	fmt.Println("[" + Styled("!", Bold, Red) + "] " + msg)
}
