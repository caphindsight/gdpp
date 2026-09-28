package main

import (
	"slices"
	"strconv"
	"strings"
)

// CmdLs prints an overview of the project: its settings, its cached
// dependencies and its packages. It expands the package containing the path,
// or the only package, and lists the others in one line each.
type CmdLs struct {
	Path string `arg:"positional" help:"expand the package containing this path [default: the current directory]"`
	All  bool   `arg:"-a,--all" help:"show all details"`
	Deps bool   `arg:"--deps" help:"show project dependencies"`
	Pkgs bool   `arg:"-l,--pkgs" help:"expand all packages"`
}

// lsPackage is what ls shows about a package. Only expanded packages show
// their details.
type lsPackage struct {
	Package
	Expanded bool
	Classes  []lsClass
}

// lsClass is a class of a package. A zero Include or Icon means the class
// has none.
type lsClass struct {
	Name          string
	Include, Icon Path
}

func (c *CmdLs) Run() {
	all := c.Pkgs || c.All
	Assert(!all || c.Path == "", "Invalid arguments: -l/--pkgs and -a/--all cannot be used with a path.")
	path := Cwd()
	if c.Path != "" {
		path = ParsePath(c.Path)
	}
	p := LoadProject(path)
	root, _ := GetPackageRootMaybe(path)
	list := p.ListPackages()
	var pkgs []lsPackage
	for _, pkg := range list {
		var classes []lsClass
		for _, class := range pkg.Config.Classes {
			classes = append(classes, lsClass{class.Name, pkg.ClassPath(class.Include), pkg.ClassPath(class.Icon)})
		}
		pkgs = append(pkgs, lsPackage{Package: pkg, Expanded: all || len(list) == 1 || pkg.Root == root, Classes: classes})
	}
	PrintResult(lsProject(p, pkgs, c.Deps || c.All))
}

// isUnused reports whether packages choose deps of cache's kind, but none
// uses the dep name. Without packages, nothing is unused.
func isUnused(cache ProjectDepCache, name string, pkgs []lsPackage) bool {
	for _, pkg := range pkgs {
		if dep, ok := pkg.Config.Dep(cache.Name); !ok || dep == name {
			return false
		}
	}
	return len(pkgs) > 0
}

func lsKey(s string) string     { return Styled(s+":", Bold) }
func lsSep() string             { return Styled(unicodeOr("  •  ", "  *  "), Bold) }
func lsMissing(s string) string { return Styled(unicodeOr("✗ ", "x ")+s, Red) }

// lsProject renders the overview of p and its packages. With deps, it lists
// every dependency instead of counting them.
func lsProject(p Project, pkgs []lsPackage, deps bool) string {
	return lsSummary(p) + "\n" + Styled("Dependencies:", Bold, BrightBlue) + "\n" + lsDeps(p.Caches, pkgs, deps) + lsPackages(p.Caches, pkgs)
}

// lsSummary renders the project's name and settings.
func lsSummary(p Project) string {
	return Styled("Project:", Bold, BrightBlue) + " " + Styled(p.Name, Bold, Cyan) + " " + Styled("["+p.Id+"]", Gray) + "\n" +
		AlignColumns([][]string{{lsKey("Godot"), p.GodotVersion}, {lsKey("VCS"), p.Config.VCS}}, "  ")
}

// lsDeps renders the cached dependencies: with deps, one row each,
// otherwise their counts per kind.
func lsDeps(caches []ProjectDepCache, pkgs []lsPackage, deps bool) string {
	var rows [][]string
	for _, cache := range caches {
		names := cache.Ls()
		slices.Reverse(names) // newest first
		if len(names) == 0 {
			rows = append(rows, []string{lsKey(cache.Plural), "none"})
			continue
		}
		checkedIn, unused := 0, 0
		for i, name := range names {
			usage := ""
			if cache.IsCheckedIn(name) {
				checkedIn++
			}
			if isUnused(cache, name, pkgs) {
				usage = Styled("unused", Yellow)
				unused++
			}
			if deps {
				label := ""
				if i == 0 {
					label = lsKey(cache.Plural)
				}
				rows = append(rows, []string{label, name, cache.Status(name), usage})
			}
		}
		if !deps {
			rows = append(rows, []string{lsKey(cache.Plural), lsCounts(checkedIn, len(names)-checkedIn, unused)})
		}
	}
	return AlignColumns(rows, "  ")
}

// lsCounts renders the nonzero counts of a kind's dependencies.
func lsCounts(checkedIn, cached, unused int) string {
	var counts []string
	for _, c := range []struct {
		n    int
		what string
	}{{checkedIn, Styled("checked in", Magenta)}, {cached, "cached"}, {unused, Styled("unused", Yellow)}} {
		if c.n > 0 {
			counts = append(counts, strconv.Itoa(c.n)+" "+c.what)
		}
	}
	return strings.Join(counts, lsSep())
}

// lsPackages renders the packages: expanded ones in full, with blank lines
// around them, and collapsed ones in one line each, grouped together. It ends
// with a hint if any package misses dependencies.
func lsPackages(caches []ProjectDepCache, pkgs []lsPackage) string {
	var out strings.Builder
	anyMissing, prevExpanded := false, true
	for _, pkg := range pkgs {
		rows, missing := lsPackageRows(caches, pkg)
		anyMissing = anyMissing || missing
		if pkg.Expanded || prevExpanded {
			out.WriteString("\n")
		}
		prevExpanded = pkg.Expanded
		name := pkg.Root.ToString()
		out.WriteString(Styled("Package:", Bold, BrightBlue) + " " + Styled(name, Bold, Cyan))
		if name == "res://" { // otherwise the path shows the id
			out.WriteString(" " + Styled("["+pkg.Id+"]", Gray))
		}
		switch {
		case pkg.Expanded:
			out.WriteString("\n" + AlignColumns(rows, "  "))
		case missing:
			out.WriteString("  " + lsMissing("missing dependencies") + "\n")
		default:
			out.WriteString("\n")
		}
	}
	if anyMissing {
		out.WriteString("\nTo fix: gd++ fetch --missing\n")
	}
	return out.String()
}

// lsPackageRows returns the rows of pkg's details, and whether it misses any
// dependencies.
func lsPackageRows(caches []ProjectDepCache, pkg lsPackage) (rows [][]string, missing bool) {
	for _, cache := range caches {
		name, ok := pkg.Config.Dep(cache.Name)
		if !ok {
			continue
		}
		status := ""
		if !cache.Has(name) {
			status, missing = lsMissing("missing"), true
		}
		rows = append(rows, []string{lsKey(cache.Desc), name, status})
	}
	rows = append(rows, []string{lsKey("GD++ syntax"), strconv.Itoa(pkg.Config.Syntax)}, []string{lsKey("C++ standard"), pkg.Config.CppStandard})
	if len(pkg.Classes) > 0 {
		// In the same table, so both align.
		rows = append(rows, nil, []string{Styled("Classes", Bold), Styled("Include", Bold), Styled("Icon", Bold)})
	}
	for _, class := range pkg.Classes {
		rows = append(rows, []string{class.Name, lsClassPath(class.Include), lsClassPath(class.Icon)})
	}
	return rows, missing
}

// lsClassPath renders a class's file, marked if it's missing.
func lsClassPath(p Path) string {
	switch {
	case p == Path{}:
		return "none"
	case !p.IsFile():
		return lsMissing(p.ToString())
	}
	return p.ToString()
}
