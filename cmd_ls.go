package main

import (
	"slices"
	"strconv"
	"strings"
)

// CmdLs prints an overview of the project: its settings, its cached
// dependencies and its packages.
type CmdLs struct {
	All  bool `arg:"--all" help:"show all details"`
	Deps bool `arg:"--deps" help:"show project dependencies"`
}

// lsPackage is what ls shows about a package.
type lsPackage struct {
	Root                              Path
	Bindings, ApiSpec, Syntax, CppStd string
	Classes                           []lsClass
}

// lsClass is a class of a package. A zero Icon means the class has none.
type lsClass struct {
	Name, Include string
	Icon          Path
}

func (c *CmdLs) Run() {
	// Packages will be listed once they can be loaded.
	PrintResult(lsProject(LoadProject(Cwd()), nil, c.Deps || c.All))
}

// lsProject renders the overview of p and its packages. With deps, it lists
// every dependency instead of counting them.
func lsProject(p Project, pkgs []lsPackage, deps bool) string {
	sep, cross := Styled(unicodeOr("  •  ", "  *  "), Bold), unicodeOr("✗ ", "x ")
	key := func(s string) string { return Styled(s+":", Bold) }
	var out strings.Builder
	out.WriteString(Styled("Project:", Bold, BrightBlue) + " " + Styled(p.Name, Bold) + "  " + Styled("["+p.Id+"]", Gray) + "\n")
	// The VCS will be a project setting.
	out.WriteString("  " + strings.Join([]string{key("Godot") + " " + p.GodotVersion, key("GD++ CLI") + " " + gdppVersion, key("VCS") + " none"}, sep) + "\n")

	// of returns the version of the kind a package uses; nil if packages
	// don't choose one.
	kinds := []struct {
		header, flag string
		cache        ProjectDepCache
		of           func(lsPackage) string
	}{
		{"Godot C++ bindings", "--bind", p.BindingsCache, func(pkg lsPackage) string { return pkg.Bindings }},
		{"Godot API specs", "--spec", p.ApiSpecsCache, func(pkg lsPackage) string { return pkg.ApiSpec }},
		{"Godot engines", "--engine", p.EnginesCache, nil},
	}

	var rows [][]string
	for _, k := range kinds {
		names := k.cache.Ls()
		slices.Reverse(names) // newest first
		checkedIn, cached, unused := 0, 0, 0
		for i, name := range names {
			status := k.cache.Status(name)
			if k.cache.IsCheckedIn(name) {
				checkedIn++
			} else {
				cached++
			}
			if k.of != nil && len(pkgs) > 0 && !slices.ContainsFunc(pkgs, func(pkg lsPackage) bool { return k.of(pkg) == name }) {
				status += sep + Styled("unused", Yellow)
				unused++
			}
			if deps {
				label := ""
				if i == 0 {
					label = key(k.header)
				}
				rows = append(rows, []string{label, name, status})
			}
		}
		if len(names) == 0 {
			rows = append(rows, []string{key(k.header), "none"})
		} else if !deps {
			var counts []string
			for _, c := range []struct {
				n    int
				what string
			}{{checkedIn, Styled("checked in", Magenta)}, {cached, "cached"}, {unused, Styled("unused", Yellow)}} {
				if c.n > 0 {
					counts = append(counts, strconv.Itoa(c.n)+" "+c.what)
				}
			}
			rows = append(rows, []string{key(k.header), strings.Join(counts, sep)})
		}
	}
	out.WriteString("\n" + Styled("Dependencies", Bold, BrightBlue) + "\n" + AlignColumns(rows, "  "))

	var fix []string
	for _, pkg := range pkgs {
		rows = nil
		for _, k := range kinds {
			if k.of == nil {
				continue
			}
			name, status := k.of(pkg), ""
			if !k.cache.Has(name) {
				status = Styled(cross+"missing", Red)
				if arg := k.flag + " " + name; !slices.Contains(fix, arg) {
					fix = append(fix, arg)
				}
			}
			rows = append(rows, []string{key(k.cache.Desc), name, status})
		}
		rows = append(rows, []string{key("GD++ syntax"), pkg.Syntax}, []string{key("C++ standard"), pkg.CppStd})
		if len(pkg.Classes) > 0 {
			// In the same table, so both align.
			rows = append(rows, nil, []string{Styled("Classes", Bold), Styled("Include", Bold), Styled("Icon", Bold)})
		}
		for _, class := range pkg.Classes {
			icon := "none"
			if class.Icon != (Path{}) {
				icon = class.Icon.ToString()
				if !class.Icon.IsFile() {
					icon = Styled(cross+icon, Red)
				}
			}
			rows = append(rows, []string{class.Name, class.Include, icon})
		}
		out.WriteString("\n" + Styled("Package "+pkg.Root.ToString(), Bold, BrightBlue) + "\n" + AlignColumns(rows, "  "))
	}
	if len(fix) > 0 {
		out.WriteString("\nTo fix: gd++ fetch " + strings.Join(fix, " ") + "\n")
	}
	return out.String()
}
