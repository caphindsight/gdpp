package main

import "github.com/alexflint/go-arg"

// Args holds the parsed CLI arguments, available to any function that needs them.
var Args struct {
	Fetch    *CmdFetch  `arg:"subcommand:fetch" help:"download deps from the dep repository, or list it"`
	Vendor   *CmdVendor `arg:"subcommand:vendor" help:"copy a dep into or out of the project's cache"`
	Quiet    bool       `arg:"-q,--quiet" help:"print fewer logs"`
	Verbose  bool       `arg:"-v,--verbose" help:"print more logs"`
	Force    bool       `arg:"-f,--yes" help:"assume yes on confirmation prompts"`
	ForceNo  bool       `arg:"-n,--no" help:"assume no on confirmation prompts"`
	Audit    bool       `arg:"--audit" help:"audit potentially dangerous operations"`
	LogDepth int        `arg:"-l,--log-depth" default:"4" help:"show this many lines of subprocess logs"`
}

func main() {
	arg.MustParse(&Args)

	if Args.Quiet && Args.Verbose {
		LogFatal("Invalid arguments: -q/--quiet and -v/--verbose cannot be used together.")
	}
	if Args.Force && Args.ForceNo {
		LogFatal("Invalid arguments: -f/--yes and -n/--no cannot be used together.")
	}
	if Args.Force && Args.Audit {
		LogFatal("Invalid arguments: -f/--yes and --audit cannot be used together.")
	}

	switch {
	case Args.Fetch != nil:
		Args.Fetch.Run()
	case Args.Vendor != nil:
		Args.Vendor.Run()
	default:
		LogFatal("Missing subcommand, see `gd++ --help`.")
	}
}
