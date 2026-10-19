// utils_test.go: tests for utils.go.

package main

import (
	"sync/atomic"
	"testing"
)

func TestSyntaxVersions(t *testing.T) {
	if got, want := syntaxVersions(), "0 (nightly, not for production)\n1 (latest stable)\n"; got != want {
		t.Errorf("syntaxVersions() = %q, want %q", got, want)
	}
}

func TestParallelFor(t *testing.T) {
	for _, n := range []int{0, 1, 100} {
		calls := make([]atomic.Int32, n)
		parallelFor(n, func(i int) { calls[i].Add(1) })
		for i := range calls {
			if got := calls[i].Load(); got != 1 {
				t.Errorf("parallelFor(%d): f(%d) ran %d times, want 1", n, i, got)
			}
		}
	}
}
