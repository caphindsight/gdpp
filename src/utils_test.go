// utils_test.go: tests for utils.go.

package main

import "testing"

func TestSyntaxVersions(t *testing.T) {
	if got, want := syntaxVersions(), "0 (nightly, not for production)\n1 (latest stable)\n"; got != want {
		t.Errorf("syntaxVersions() = %q, want %q", got, want)
	}
}
