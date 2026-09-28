// test_util_test.go: shared helpers for tests in this package.

package main

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestMain sets Args defaults that arg.MustParse would normally fill in, since
// tests never call it.
func TestMain(m *testing.M) {
	Args.LogDepth = 4
	os.Exit(m.Run())
}

// withTTY sets isTTY for the duration of a test and restores the prior value.
// Tests in this file must not call t.Parallel(): they mutate this package-level var.
func withTTY(t *testing.T, tty bool) {
	orig := isTTY
	isTTY = tty
	t.Cleanup(func() { isTTY = orig })
}

// withUnicode sets isUnicode for the duration of a test and restores the prior value.
func withUnicode(t *testing.T, unicode bool) {
	orig := isUnicode
	isUnicode = unicode
	t.Cleanup(func() { isUnicode = orig })
}

// withForce sets Args.Force for the duration of a test and restores the prior value.
func withForce(t *testing.T, force bool) {
	orig := Args.Force
	Args.Force = force
	t.Cleanup(func() { Args.Force = orig })
}

// withForceNo sets Args.ForceNo for the duration of a test and restores the prior value.
func withForceNo(t *testing.T, forceNo bool) {
	orig := Args.ForceNo
	Args.ForceNo = forceNo
	t.Cleanup(func() { Args.ForceNo = orig })
}

// withStdin replaces os.Stdin with a pipe fed with content, for the duration of a test.
func withStdin(t *testing.T, content string) {
	orig := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	go func() {
		io.WriteString(w, content)
		w.Close()
	}()
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })
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

// runFailHelper re-execs the test binary to run f (a top-level function in
// this file whose name is passed as -test.run), since Fail calls os.Exit and
// would otherwise kill the test process.
func runFailHelper(t *testing.T, name string) (stdout string, exitCode int) {
	cmd := exec.Command(os.Args[0], "-test.run=^"+name+"$")
	cmd.Env = append(os.Environ(), "GDPP_FAIL_HELPER=1")
	out, err := cmd.Output()
	if err == nil {
		t.Fatalf("%s: process exited 0, want nonzero", name)
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("%s: cmd.Output: %v", name, err)
	}
	return string(out), exitErr.ExitCode()
}

// runFailHelperWithStdin is like runFailHelper, but feeds stdin to the child process.
func runFailHelperWithStdin(t *testing.T, name string, stdin string) (stdout string, exitCode int) {
	cmd := exec.Command(os.Args[0], "-test.run=^"+name+"$")
	cmd.Env = append(os.Environ(), "GDPP_FAIL_HELPER=1")
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.Output()
	if err == nil {
		t.Fatalf("%s: process exited 0, want nonzero", name)
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("%s: cmd.Output: %v", name, err)
	}
	return string(out), exitErr.ExitCode()
}
