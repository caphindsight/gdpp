// io_test.go: tests for io.go.

package main

import (
	"io"
	"os"
	"testing"
)

// withTTY sets isTTY for the duration of a test and restores the prior value.
// Tests in this file must not call t.Parallel(): they mutate this package-level var.
func withTTY(t *testing.T, tty bool) {
	orig := isTTY
	isTTY = tty
	t.Cleanup(func() { isTTY = orig })
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
