package main

import (
	"slices"
	"strconv"
	"strings"

	"gd++/trans"
)

// CmdLs prints an overview of the project: its settings, its cached
// dependencies and its packages. It expands the package containing the path,
// or the only package, and lists the others in one line each.
type CmdLs struct {
	Path string `arg:"positional" help:"show the project containing this path, expanding its package [default: the current directory]"`
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

// lsClass is a class of a package. A zero File or Icon means the class has
// none. GD++ classes live in GD++ files rather than in the package's config,
// so commands can't change them. A GD++ class with an empty Name stands for a
// GD++ file with errors.
type lsClass struct {
	Name                                 string
	File, Icon                           Path     // The header of a C++ class, or the GD++ file of a GD++ class.
	FileText                             string   // For GD++ classes, the file's pkg:// path; if empty, File's.
	Base                                 string   // For GD++ classes.
	Traits                               []string // For GD++ classes: the traits it implements itself.
	Kind                                 string   // For C++ classes: "ptr", "ref", or "" if GD++ code can't use it.
	Gdpp                                 bool
	Abstract, Tool, GameOnly, EditorOnly bool
	Clash                                bool // Another class has the same name.
}

func (c *CmdLs) Run() {
	all := c.Pkgs || c.All
	path := Cwd()
	if c.Path != "" {
		path = ParsePath(c.Path)
	}
	p := LoadProject(path)
	root, _ := GetPackageRootMaybe(path)
	list := p.ListPackages()
	var pkgs []lsPackage
	for _, pkg := range list {
		expanded := all || len(list) == 1 || pkg.Root == root
		var classes []lsClass
		if expanded {
			classes = lsClasses(p, pkg)
		}
		pkgs = append(pkgs, lsPackage{Package: pkg, Expanded: expanded, Classes: classes})
	}
	PrintResult(lsProject(p, pkgs, c.Deps || c.All, c.All))
}

// lsClasses returns the classes of the package: those in its config, then
// those of each GD++ file, in order, and last a class per GD++ file with
// errors.
func lsClasses(p Project, pkg Package) []lsClass {
	var classes, broken []lsClass
	for _, class := range pkg.Config.Classes {
		classes = append(classes, lsClass{Name: class.Name, File: pkg.ClassPath(class.Include), Icon: pkg.ClassPath(class.Icon), Tool: class.Tool,
			Abstract: class.Abstract, Kind: class.Kind})
	}
	for _, f := range listGdppFiles(p, pkg, 0) {
		file := lsClass{File: f.File, FileText: "pkg://" + f.Rel, Gdpp: true}
		if f.Err != nil {
			broken = append(broken, file)
		}
		for _, d := range f.Decls {
			if d.Kind == trans.ClassDecl {
				class := file
				class.Name, class.Base, class.Traits, class.Icon = d.Name, d.Base, d.Traits, pkg.ClassPath(d.Icon)
				class.Abstract, class.Tool, class.GameOnly, class.EditorOnly = d.Abstract, d.Tool, d.GameOnly, d.EditorOnly
				classes = append(classes, class)
			}
		}
	}
	classes = append(classes, broken...)
	count := map[string]int{}
	for _, class := range classes {
		count[class.Name]++
	}
	for i := range classes {
		classes[i].Clash = classes[i].Name != "" && count[classes[i].Name] > 1
	}
	return classes
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
func lsMissing(s string) string { return Styled(s, Red) }

// lsProject renders the overview of p and its packages. With deps, it lists
// every dependency instead of counting them. With all, it shows the engines.
func lsProject(p Project, pkgs []lsPackage, deps, all bool) string {
	caches := slices.DeleteFunc(slices.Clone(p.Caches), func(c ProjectDepCache) bool { return !all && c.Name == "engine" })
	return lsSummary(p) + "\n" + Styled("Dependencies:", Bold, BrightBlue) + "\n" + lsDeps(caches, pkgs, deps) + lsPackages(p.Caches, pkgs)
}

// lsSummary renders the project's name and settings.
func lsSummary(p Project) string {
	rows := [][]string{{lsKey("Godot"), p.GodotVersion}, {lsKey("VCS"), p.Config.VCS},
		{lsKey("Manage presets"), map[bool]string{true: "yes", false: "no"}[p.Config.Presets]}}
	return Styled("Project:", Bold, BrightBlue) + " " + Styled(p.Name, Bold, Cyan) + " " + Styled("["+p.Id+"]", Gray) + "\n" +
		AlignColumns(rows, "  ")
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
			out.WriteString("\n" + AlignColumns(rows, "  ") + lsClassTable(pkg.Classes))
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
	rows = append(rows, []string{lsKey("GD++ syntax"), lsSyntax(pkg.Config.Syntax)}, []string{lsKey("C++ standard"), pkg.Config.CppStandard},
		[]string{lsKey("Class prefix"), pkg.Prefix()}, []string{lsKey("Quit timeout"), seconds(pkg.QuitTimeout())},
		[]string{lsKey("Hot reload"), onOff(pkg.HotReload())}, []string{lsKey("Macro depth"), strconv.Itoa(pkg.MacroDepth())})
	if len(pkg.Config.Hidden) > 0 {
		rows = append(rows, []string{lsKey("Hidden from Godot"), strings.Join(pkg.Config.Hidden, ", ")})
	}
	return rows, missing
}

// lsSyntax shows the GD++ syntax, warning if it's the nightly one.
func lsSyntax(syntax int) string {
	if syntax == trans.NightlySyntax {
		return strconv.Itoa(syntax) + " " + Styled("(nightly, not for production)", Yellow)
	}
	return strconv.Itoa(syntax)
}

// lsClassTable renders the classes as a table of file, name, base class and
// tags. A file is shown only on its first class, so the classes without a
// file come first, lest they look like part of the file above.
func lsClassTable(classes []lsClass) string {
	if len(classes) == 0 {
		return ""
	}
	var sorted []lsClass
	for _, noFile := range []bool{true, false} {
		for _, class := range classes {
			if (class.File == Path{}) == noFile {
				sorted = append(sorted, class)
			}
		}
	}
	classes = sorted
	var rows [][]string
	for i, class := range classes {
		file, base := lsClassPath(class.File, ""), ""
		if class.Gdpp {
			file, base = class.FileText, "extends "+class.Base
			if len(class.Traits) > 0 {
				base += " implements " + strings.Join(class.Traits, ", ")
			}
		}
		if i > 0 && class.File == classes[i-1].File {
			file = ""
		}
		name := Styled(class.Name, Green)
		switch {
		case class.Name == "":
			name, base = lsMissing("has errors"), "see gd++ build"
		case class.Clash:
			name = lsMissing(class.Name + ": declared twice")
		}
		rows = append(rows, []string{file, name, base, lsTags(class)})
	}
	return "\n  " + Styled("Classes", Bold) + "\n" + AlignColumns(rows, "    ")
}

// lsTags renders the annotations of a class that change what it is, e.g.
// "[ptr] @tool @icon", but not those for debugging, like @trace.
func lsTags(class lsClass) string {
	var tags []string
	if class.Kind != "" {
		tags = append(tags, "["+class.Kind+"]")
	}
	for _, t := range []struct {
		on   bool
		name string
	}{{class.Abstract, "@abstract"}, {class.Tool, "@tool"}, {class.GameOnly, "@game_only"}, {class.EditorOnly, "@editor_only"}} {
		if t.on {
			tags = append(tags, t.name)
		}
	}
	if class.Icon != (Path{}) {
		tags = append(tags, lsClassPath(class.Icon, "@icon"))
	}
	return strings.Join(tags, " ")
}

// lsClassPath renders a class's file as text, or if empty as p's path, and
// marks it if it's missing. A zero p renders as "".
func lsClassPath(p Path, text string) string {
	if p == (Path{}) {
		return ""
	}
	if text == "" {
		text = p.ToString()
	}
	if !p.IsFile() {
		return lsMissing(text + " (missing)")
	}
	return text
}
