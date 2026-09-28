package main

import (
	"fmt"
	"time"
)

// logDelay is the pause between log lines in tasks below, so the output can be watched as it happens.
const logDelay = 200 * time.Millisecond

// CmdHello demonstrates the logging helpers.
type CmdHello struct{}

func (c *CmdHello) Run() {
	LogInfo("Starting gd++ demo.")
	LogWarn("This is a demonstration, no real work is done.")
	LogError("Errors look like this: %v.", "example error")

	t := LogTask("Running a task that succeeds...")
	time.Sleep(logDelay)
	t.LogString("step 1 complete")
	time.Sleep(logDelay)
	t.LogString("step 2 complete")
	t.Done()

	t = LogTask("Running a task with few log lines...")
	time.Sleep(logDelay)
	t.LogString("just one line of output")
	t.Done()

	t = LogTask("Running a task with many log lines...")
	for i := 1; i <= 10; i++ {
		time.Sleep(logDelay)
		t.LogString(fmt.Sprintf("processing item %d/10", i))
	}
	t.Done()

	silence := Silence()
	LogInfo("You should not see this, it's silenced.")
	t = LogTask("This task's progress stays visible, but its log lines don't...")
	time.Sleep(logDelay)
	t.LogString("you should not see this either")
	t.Done()
	silence.End()
	LogInfo("Silence lifted, logging is back.")

  silence = Silence()
	Exec("Running a subprocess...", Cwd(), "bash", "-c", "echo one; sleep 5; echo two; sleep 5; echo three; echo four; exit 1")
  silence.End()

	Confirm("Continue to the failing task?")
	Audit("Run a potentially dangerous operation?")

	t = LogTask("Running a task that fails...")
	time.Sleep(logDelay)
	t.LogString("something went wrong")
	t.Fail() // exits the program, like a real failing task would
}
