package main

import (
	"os"

	"github.com/alexflint/go-arg"
)

// Args holds the parsed CLI arguments, available to any function that needs them.
var Args cliArgs

type cliArgs struct {
	Build    *CmdBuild   `arg:"subcommand:build" help:"compile a package into a GDExtension library"`
	Cat      *CmdCat     `arg:"subcommand:cat" help:"show files in the pager, highlighting C++ and GD++ code"`
	CheckIn  *CmdCheckIn `arg:"subcommand:checkin" help:"check in dependencies from the ephemeral cache, or undo it"`
	Clean    *CmdClean   `arg:"subcommand:clean" help:"delete the build caches of packages"`
	Doc      *CmdDoc     `arg:"subcommand:doc" help:"show the declaration and members of a godot-cpp class or function"`
	Fetch    *CmdFetch   `arg:"subcommand:fetch" help:"download dependencies from a repository, or list them"`
	Fix      *CmdFix     `arg:"subcommand:fix" help:"tidy up the project, e.g. delete leftover temporary files"`
	Init     *CmdInit    `arg:"subcommand:init" help:"set up the project, or create or update a package"`
	Install  *CmdInstall `arg:"subcommand:install" help:"install the tools GD++ needs to build, e.g. SCons and a C++ compiler"`
	Ls       *CmdLs      `arg:"subcommand:ls" help:"show an overview of the project, its dependencies and packages"`
	Man      *CmdMan     `arg:"subcommand:man" help:"read the reference manual of GD++ and its language, or list its pages"`
	Rm       *CmdRm      `arg:"subcommand:rm" help:"remove dependencies from the project cache, or packages from the project"`
	Trans    *CmdTrans   `arg:"subcommand:trans" help:"transpile a GD++ file and print the C++ it generates, to try out GD++; in a package, with its dependencies"`
	Vendor   *CmdVendor  `arg:"subcommand:vendor" help:"copy a dependency into or out of the project's cache"`
	Quiet    bool        `arg:"-q,--quiet" help:"print fewer logs"`
	Verbose  bool        `arg:"-v,--verbose" help:"print more logs"`
	Force    bool        `arg:"-f,--yes" help:"assume yes on confirmation prompts"`
	ForceNo  bool        `arg:"-n,--no" help:"assume no on confirmation prompts"`
	Audit    bool        `arg:"--audit" help:"audit potentially dangerous operations"`
	TTY      bool        `arg:"--tty" help:"use colors, spinners and the pager, as if the output were a terminal"`
	NoTTY    bool        `arg:"--notty" help:"print plain output, as if the output were not a terminal"`
	LogDepth int         `arg:"-L,--log-depth" default:"4" help:"show this many lines of subprocess logs"`
	TabWidth int         `arg:"-T,--tab-width" default:"2" placeholder:"N" help:"show tabs as this many columns in the pager, and indent gd++ doc output by this many spaces"`
	Version  bool        `arg:"--version" help:"print the version of GD++ and exit"`
}

// Epilogue documents --syntax, which main handles before parsing: as a field,
// go-arg would take the --syntax N of commands for it.
func (cliArgs) Epilogue() string {
	return "Run `gd++ --syntax` to list the GD++ syntax versions that this gd++ supports."
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--syntax" {
		Assert(len(os.Args) == 2, "Invalid arguments: --syntax cannot be used with other arguments.")
		PrintResult(syntaxVersions())
		return
	}
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
	if Args.TabWidth < 1 {
		LogFatal("Invalid arguments: -T/--tab-width must be at least 1.")
	}
	if Args.Force && Args.Audit {
		LogFatal("Invalid arguments: -f/--yes and --audit cannot be used together.")
	}
	if Args.TTY && Args.NoTTY {
		LogFatal("Invalid arguments: --tty and --notty cannot be used together.")
	}
	if Args.TTY || Args.NoTTY {
		isTTY = Args.TTY
	}

	switch {
	case Args.Build != nil:
		Args.Build.Run()
	case Args.Cat != nil:
		Args.Cat.Run()
	case Args.CheckIn != nil:
		Args.CheckIn.Run()
	case Args.Clean != nil:
		Args.Clean.Run()
	case Args.Doc != nil:
		Args.Doc.Run()
	case Args.Fetch != nil:
		Args.Fetch.Run()
	case Args.Fix != nil:
		Args.Fix.Run()
	case Args.Init != nil:
		Args.Init.Run()
	case Args.Install != nil:
		Args.Install.Run()
	case Args.Ls != nil:
		Args.Ls.Run()
	case Args.Man != nil:
		Args.Man.Run()
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
