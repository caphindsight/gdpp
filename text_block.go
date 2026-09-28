// text_block.go: functions for reading and rewriting a text block delimited
// by exact-match marker lines, e.g. an auto-generated section of a file.

package main

import "strings"

// SplitLines splits text into lines on "\n" or "\r\n"; neither character
// appears in the returned lines.
func SplitLines(text string) []string {
	return strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
}

// MergeLines joins lines with "\n", the inverse of SplitLines.
func MergeLines(lines []string) string {
	return strings.Join(lines, "\n")
}

// findTextBlock returns the indices in lines of the first line matching
// startMarker and the following line matching endMarker that closes it, and
// whether such a block was found. A line matching endMarker only closes the
// block if the lines between it and startMarker aren't all empty, unless
// allowEmpty is set; otherwise it's treated as ordinary content and the
// search continues. If no line closes the block, it runs to the end of the
// file instead, and end is returned as len(lines): there's no marker line
// there to skip over.
func findTextBlock(lines []string, startMarker, endMarker string, allowEmpty bool) (start, end int, ok bool) {
	for start = range lines {
		if lines[start] != startMarker {
			continue
		}
		for end = start + 1; end < len(lines); end++ {
			if lines[end] == endMarker && (allowEmpty || !isEmptyTextBlock(lines[start+1:end])) {
				return start, end, true
			}
		}
		if allowEmpty || !isEmptyTextBlock(lines[start+1:end]) {
			return start, end, true
		}
		return 0, 0, false
	}
	return 0, 0, false
}

// isEmptyTextBlock reports whether lines contains only empty lines.
func isEmptyTextBlock(lines []string) bool {
	for _, line := range lines {
		if line != "" {
			return false
		}
	}
	return true
}

// trimTrailingEmptyLines drops empty lines from the end of lines.
func trimTrailingEmptyLines(lines []string) []string {
	n := len(lines)
	for n > 0 && lines[n-1] == "" {
		n--
	}
	return lines[:n]
}

// GetTextBlock returns the contents of the block between the lines matching
// startMarker and endMarker, not including the marker lines, or "" if no such
// block exists. If startMarker is found but no line closes the block, its
// contents run to the end of the file.
func GetTextBlock(text, startMarker, endMarker string, allowEmpty bool) string {
	lines := SplitLines(text)
	start, end, ok := findTextBlock(lines, startMarker, endMarker, allowEmpty)
	if !ok {
		return ""
	}
	return MergeLines(lines[start+1 : end])
}

// EditTextBlock replaces the contents of the block between startMarker and
// endMarker with block, keeping the marker lines. If startMarker is found but
// no line closes the block, its contents run to the end of the file, and no
// endMarker line is written back, since there wasn't one to begin with. If no
// block is found at all, the markers and block are appended to the end of
// text instead, preceded by an empty line if text doesn't already end with
// one. In every case, trailing empty lines are removed from the result.
func EditTextBlock(text, startMarker, endMarker string, allowEmpty bool, block string) string {
	lines := SplitLines(text)
	start, end, ok := findTextBlock(lines, startMarker, endMarker, allowEmpty)

	var result []string
	if ok {
		result = append(append([]string{}, lines[:start+1]...), SplitLines(block)...)
		result = append(result, lines[end:]...)
	} else {
		result = trimTrailingEmptyLines(lines)
		if len(result) > 0 {
			result = append(result, "")
		}
		result = append(result, startMarker)
		result = append(result, SplitLines(block)...)
		result = append(result, endMarker)
	}
	return MergeLines(trimTrailingEmptyLines(result))
}

// RemoveTextBlock strips the block between startMarker and endMarker, including
// the marker lines, and removes trailing empty lines from the result. If
// startMarker is found but no line closes the block, everything from
// startMarker to the end of the file is stripped. Returns text unchanged if
// no such block exists.
func RemoveTextBlock(text, startMarker, endMarker string, allowEmpty bool) string {
	lines := SplitLines(text)
	start, end, ok := findTextBlock(lines, startMarker, endMarker, allowEmpty)
	if !ok {
		return text
	}
	after := end // no endMarker line to skip over when the block ran to the end of file
	if end < len(lines) {
		after = end + 1
	}
	result := append(append([]string{}, lines[:start]...), lines[after:]...)
	return MergeLines(trimTrailingEmptyLines(result))
}
