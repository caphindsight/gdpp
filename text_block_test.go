// text_block_test.go: tests for text_block.go.

package main

import "testing"

func TestSplitLines(t *testing.T) {
	cases := []struct {
		name, input string
		want        []string
	}{
		{"empty", "", []string{""}},
		{"single line, no terminator", "abc", []string{"abc"}},
		{"lf terminated", "a\nb\n", []string{"a", "b", ""}},
		{"crlf terminated", "a\r\nb\r\n", []string{"a", "b", ""}},
		{"mixed terminators", "a\nb\r\nc", []string{"a", "b", "c"}},
		{"blank lines", "a\n\nb", []string{"a", "", "b"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SplitLines(c.input); !equalStrings(got, c.want) {
				t.Errorf("SplitLines(%q) = %q, want %q", c.input, got, c.want)
			}
		})
	}
}

func TestMergeLines(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"empty", []string{""}, ""},
		{"single line", []string{"abc"}, "abc"},
		{"multiple lines", []string{"a", "b", ""}, "a\nb\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MergeLines(c.in); got != c.want {
				t.Errorf("MergeLines(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestSplitMergeLinesRoundTrip(t *testing.T) {
	cases := []string{"", "abc", "a\nb\n", "a\r\nb\r\n", "a\nb", "a\n\n\nb"}
	for _, text := range cases {
		norm := MergeLines(SplitLines(text)) // \r\n normalized to \n
		if got := MergeLines(SplitLines(norm)); got != norm {
			t.Errorf("round trip of %q = %q, want %q", text, got, norm)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestGetTextBlock(t *testing.T) {
	cases := []struct {
		name                         string
		text, startMarker, endMarker string
		allowEmpty                   bool
		want                         string
	}{
		{"found, single line", "before\nSTART\ncontent\nEND\nafter", "START", "END", false, "content"},
		{"found, multiple lines", "START\na\nb\nEND", "START", "END", false, "a\nb"},
		{"not found, no start", "a\nb\nc", "START", "END", false, ""},
		{"unterminated block matches to end of file", "a\nSTART\nb\nc", "START", "END", false, "b\nc"},
		{"unterminated empty block not allowed", "a\nSTART", "START", "END", false, ""},
		{"unterminated empty block allowed", "a\nSTART", "START", "END", true, ""},
		{"empty block, allowEmpty true", "START\nEND", "START", "END", true, ""},
		{"empty block, allowEmpty false skips to later end", "START\nEND\ncontent\nEND", "START", "END", false, "END\ncontent"},
		{"empty block, allowEmpty false with no later end folds into unterminated content", "START\nEND", "START", "END", false, "END"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := GetTextBlock(c.text, c.startMarker, c.endMarker, c.allowEmpty); got != c.want {
				t.Errorf("GetTextBlock(%q, %q, %q, %v) = %q, want %q", c.text, c.startMarker, c.endMarker, c.allowEmpty, got, c.want)
			}
		})
	}
}

func TestEditTextBlockReplacesExisting(t *testing.T) {
	cases := []struct {
		name                                string
		text, startMarker, endMarker, block string
		allowEmpty                          bool
		want                                string
	}{
		{"replaces content", "before\nSTART\nold\nEND\nafter", "START", "END", "new", false, "before\nSTART\nnew\nEND\nafter"},
		{"replaces with multiple lines", "START\nold\nEND", "START", "END", "a\nb", false, "START\na\nb\nEND"},
		{"replaces with empty", "START\nold\nEND", "START", "END", "", true, "START\n\nEND"},
		{"trims trailing empty lines after", "START\nold\nEND\n\n\n", "START", "END", "new", false, "START\nnew\nEND"},
		{"skips empty candidate end", "START\nEND\nx\nEND", "START", "END", "new", false, "START\nnew\nEND"},
		{"replaces unterminated block, no end marker written back", "before\nSTART\nold\nmore", "START", "END", "new", false, "before\nSTART\nnew"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := EditTextBlock(c.text, c.startMarker, c.endMarker, c.allowEmpty, c.block); got != c.want {
				t.Errorf("EditTextBlock(%q, ..., %q) = %q, want %q", c.text, c.block, got, c.want)
			}
		})
	}
}

func TestEditTextBlockAppendsWhenNotFound(t *testing.T) {
	cases := []struct {
		name                                string
		text, startMarker, endMarker, block string
		want                                string
	}{
		{"empty file", "", "START", "END", "content", "START\ncontent\nEND"},
		{"ends with newline", "a\n", "START", "END", "content", "a\n\nSTART\ncontent\nEND"},
		{"no trailing newline", "a", "START", "END", "content", "a\n\nSTART\ncontent\nEND"},
		{"already ends with blank line", "a\n\n", "START", "END", "content", "a\n\nSTART\ncontent\nEND"},
		{"unterminated but empty block, not allowed, so appended instead", "a\nSTART", "START", "END", "content", "a\nSTART\n\nSTART\ncontent\nEND"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := EditTextBlock(c.text, c.startMarker, c.endMarker, false, c.block); got != c.want {
				t.Errorf("EditTextBlock(%q, ..., %q) = %q, want %q", c.text, c.block, got, c.want)
			}
		})
	}
}

func TestRemoveTextBlock(t *testing.T) {
	cases := []struct {
		name                         string
		text, startMarker, endMarker string
		allowEmpty                   bool
		want                         string
	}{
		{"removes block and markers", "before\nSTART\ncontent\nEND\nafter", "START", "END", false, "before\nafter"},
		{"not found leaves text unchanged", "a\nb\nc", "START", "END", false, "a\nb\nc"},
		{"trims trailing empty lines left behind", "a\nSTART\nx\nEND\n\n\n", "START", "END", false, "a"},
		{"skips empty candidate end", "START\nEND\nx\nEND", "START", "END", false, ""},
		{"unterminated block removes everything to end of file", "a\nSTART\nb\nc", "START", "END", false, "a"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RemoveTextBlock(c.text, c.startMarker, c.endMarker, c.allowEmpty); got != c.want {
				t.Errorf("RemoveTextBlock(%q, %q, %q, %v) = %q, want %q", c.text, c.startMarker, c.endMarker, c.allowEmpty, got, c.want)
			}
		})
	}
}
