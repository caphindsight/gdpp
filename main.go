package main

import "github.com/alexflint/go-arg"

// Args holds the parsed CLI arguments, available to any function that needs them.
var Args struct {
	CheckIn  *CmdCheckIn `arg:"subcommand:checkin" help:"check in dependencies from the ephemeral cache, or undo it"`
	Fetch    *CmdFetch   `arg:"subcommand:fetch" help:"download dependencies from a repository, or list them"`
	Fix      *CmdFix     `arg:"subcommand:fix" help:"tidy up the project, e.g. delete leftover temporary files"`
	Ls       *CmdLs      `arg:"subcommand:ls" help:"show an overview of the project, its dependencies and packages"`
	Vendor   *CmdVendor  `arg:"subcommand:vendor" help:"copy a dependency into or out of the project's cache"`
	Quiet    bool        `arg:"-q,--quiet" help:"print fewer logs"`
	Verbose  bool        `arg:"-v,--verbose" help:"print more logs"`
	Force    bool        `arg:"-f,--yes" help:"assume yes on confirmation prompts"`
	ForceNo  bool        `arg:"-n,--no" help:"assume no on confirmation prompts"`
	Audit    bool        `arg:"--audit" help:"audit potentially dangerous operations"`
	LogDepth int         `arg:"-l,--log-depth" default:"4" help:"show this many lines of subprocess logs"`
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
	case Args.CheckIn != nil:
		Args.CheckIn.Run()
	case Args.Fetch != nil:
		Args.Fetch.Run()
	case Args.Fix != nil:
		Args.Fix.Run()
	case Args.Ls != nil:
		Args.Ls.Run()
	case Args.Vendor != nil:
		Args.Vendor.Run()
	default:
		LogFatal("Missing subcommand, see `gd++ --help`.")
	}
}
