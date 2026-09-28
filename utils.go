// utils.go: small helpers shared between commands.

package main

import "path/filepath"

// countTrue returns how many of bs are true.
func countTrue(bs ...bool) int {
	n := 0
	for _, b := range bs {
		if b {
			n++
		}
	}
	return n
}

// assertDepName asserts name can be used as a dep's directory name.
func assertDepName(name string) {
	Assert(name == filepath.Base(name) && name != "." && name != "..", "Invalid arguments: %q is not a valid dep name.", name)
}
