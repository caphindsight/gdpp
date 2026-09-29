// shell.go: running subprocesses.

package main

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Exec runs cmd with args as a subprocess in dir, under a LogTask named
// taskName. The combined stdout and stderr are streamed to the task log line
// by line as they arrive, and returned once the subprocess exits. Done is
// called on success, Fail on failure.
func Exec(taskName string, dir Path, cmd string, args ...string) string {
	return ExecEnv(taskName, dir, nil, cmd, args...)
}

// ExecEnv is Exec with the environment env, or this process's if nil.
func ExecEnv(taskName string, dir Path, env []string, cmd string, args ...string) string {
	t := LogTask(taskName)
	t.LogString(fmt.Sprintf("%s$ %s", dir.ToString(), strings.Join(append([]string{cmd}, args...), " ")))

	c := exec.Command(cmd, args...)
	c.Dir = dir.GetOsPath()
	c.Env = env
	pr, pw := io.Pipe()
	c.Stdout, c.Stderr = pw, pw

	var output strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			line := scanner.Text()
			output.WriteString(line)
			output.WriteByte('\n')
			t.LogString(line)
		}
	}()

	err := c.Start()
	if err == nil {
		err = c.Wait()
	}
	pw.Close()
	<-done

	if err != nil {
		t.Fail()
	}
	t.Done()
	return output.String()
}
