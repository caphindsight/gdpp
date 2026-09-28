package main

import "github.com/alexflint/go-arg"

// Args holds the parsed CLI arguments, available to any function that needs them.
var Args struct {
	Hello   *CmdHello `arg:"subcommand:hello" help:"run the demo"`
	Quiet   bool      `arg:"-q,--quiet" help:"print fewer logs"`
	Verbose bool      `arg:"-v,--verbose" help:"print more logs"`
	Force   bool      `arg:"-f,--yes" help:"assume yes on confirmation prompts"`
	ForceNo bool      `arg:"-n,--no" help:"assume no on confirmation prompts"`
}

func main() {
	arg.MustParse(&Args)

	if Args.Quiet && Args.Verbose {
		LogFatal("Invalid arguments: -q/--quiet and -v/--verbose cannot be used together.")
	}
	if Args.Force && Args.ForceNo {
		LogFatal("Invalid arguments: -f/--yes and -n/--no cannot be used together.")
	}

	switch {
	case Args.Hello != nil:
		Args.Hello.Run()
	default:
		LogFatal("Missing subcommand, see `gd++ --help`.")
	}
}
