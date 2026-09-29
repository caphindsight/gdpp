package main

import "github.com/alexflint/go-arg"

// Args holds the parsed CLI arguments, available to any function that needs them.
var Args struct {
	Build    *CmdBuild   `arg:"subcommand:build" help:"compile a package into a GDExtension library, or the project into the engine"`
	CheckIn  *CmdCheckIn `arg:"subcommand:checkin" help:"check in dependencies from the ephemeral cache, or undo it"`
	Clean    *CmdClean   `arg:"subcommand:clean" help:"delete the build caches of packages"`
	Export   *CmdExport  `arg:"subcommand:export" help:"set up export presets that leave out C++ and GD++ files, building what they export"`
	Fetch    *CmdFetch   `arg:"subcommand:fetch" help:"download dependencies from a repository, or list them"`
	Fix      *CmdFix     `arg:"subcommand:fix" help:"tidy up the project, e.g. delete leftover temporary files"`
	Init     *CmdInit    `arg:"subcommand:init" help:"set up the project, or create or update a package"`
	Ls       *CmdLs      `arg:"subcommand:ls" help:"show an overview of the project, its dependencies and packages"`
	Rm       *CmdRm      `arg:"subcommand:rm" help:"remove dependencies from the project cache, or packages from the project"`
	Trans    *CmdTrans   `arg:"subcommand:trans" help:"transpile a GD++ file and print the result, to try out GD++"`
	Vendor   *CmdVendor  `arg:"subcommand:vendor" help:"copy a dependency into or out of the project's cache"`
	Quiet    bool        `arg:"-q,--quiet" help:"print fewer logs"`
	Verbose  bool        `arg:"-v,--verbose" help:"print more logs"`
	Force    bool        `arg:"-f,--yes" help:"assume yes on confirmation prompts"`
	ForceNo  bool        `arg:"-n,--no" help:"assume no on confirmation prompts"`
	Audit    bool        `arg:"--audit" help:"audit potentially dangerous operations"`
	LogDepth int         `arg:"-L,--log-depth" default:"4" help:"show this many lines of subprocess logs"`
	Version  bool        `arg:"--version" help:"print the version of GD++ and exit"`
}

func main() {
	p := arg.MustParse(&Args)

	if Args.Version {
		if p.Subcommand() != nil {
			LogFatal("Invalid arguments: --version cannot be used with a subcommand.")
		}
		PrintResult(gdppVersion + "\n")
		return
	}

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
	case Args.Build != nil:
		Args.Build.Run()
	case Args.CheckIn != nil:
		Args.CheckIn.Run()
	case Args.Clean != nil:
		Args.Clean.Run()
	case Args.Export != nil:
		Args.Export.Run()
	case Args.Fetch != nil:
		Args.Fetch.Run()
	case Args.Fix != nil:
		Args.Fix.Run()
	case Args.Init != nil:
		Args.Init.Run()
	case Args.Ls != nil:
		Args.Ls.Run()
	case Args.Rm != nil:
		Args.Rm.Run()
	case Args.Trans != nil:
		Args.Trans.Run()
	case Args.Vendor != nil:
		Args.Vendor.Run()
	default:
		LogFatal("Missing subcommand, see `gd++ --help`.")
	}
}
