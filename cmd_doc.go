package main

import (
	"cmp"
	"slices"
	"strings"
)

// CmdDoc prints what godot-cpp declares under a name, like `go doc`: the
// declaration, the header to include, and the public members of a class or
// the values of an enum, then the members it inherits, by base class. An
// alias is shown with the type it names. With NAME.MEMBER, it prints the
// members of that name, looking through aliases and base classes too. Names
// come from the bindings of a package. godot-cpp's own helpers, e.g.
// TypedArray, are documented nowhere else, while Godot's help describes the
// engine's classes. Without a name, or with --index, it lists the types
// instead, with what declares them and their bases.
type CmdDoc struct {
	Args  []string `arg:"positional" placeholder:"[PKG] [NAME]" help:"the name to show, e.g. TypedArray or Array.push_back, after a path in the package whose bindings to use [default package: the current directory's]"`
	Index bool     `arg:"-i,--index" help:"list the types that godot-cpp declares, with their kinds and bases [default: without a name]"`
}

// docType is a name that godot-cpp declares, with its declarations, and
// whether its header was generated from Godot's API spec.
type docType struct {
	godotName
	docs      []cppDoc
	generated bool
}

// isAlias reports whether t is an alias, with no members of its own.
func (t docType) isAlias() bool {
	return len(t.docs) == 1 && t.docs[0].alias != "" && len(t.docs[0].members) == 0
}

func (c *CmdDoc) Run() {
	index := c.Index || len(c.Args) == 0
	Assert(!index || len(c.Args) <= 1, "Invalid arguments: --index cannot be used with a name.")
	Assert(len(c.Args) <= 2, "Invalid arguments: expected a name, optionally after a package path.")
	path := Cwd()
	if len(c.Args) == 2 || index && len(c.Args) == 1 {
		path = ParsePath(c.Args[0])
	}
	root, ok := GetPackageRootMaybe(path)
	Assert(ok, "Path %s is not contained in a GD++ package, run this in one or pass one, e.g. `gd++ doc PKG NAME`.", path.ToString())
	pkg := LoadPackage(root)
	names := packageGodotNames(LoadProject(root), pkg)
	if index {
		PrintResult(indexText(names))
		return
	}
	name, member, _ := strings.Cut(c.Args[len(c.Args)-1], ".")
	if !slices.ContainsFunc(names, func(n godotName) bool { return n.Name == name }) {
		similar := similarGodotNames(names, name)
		Assert(len(similar) == 0, "There is no name %s in godot-cpp. Similar names: %s.", name, strings.Join(similar, ", "))
		LogFatal("There is no name %s in godot-cpp.", name)
	}
	chain := docChain(pkg, names, name)
	if member != "" {
		for _, t := range chain {
			text := ""
			for _, d := range t.docs {
				for _, m := range d.members {
					if m.name == member {
						text += Styled(m.comments, Gray) + m.text + "\n"
					}
				}
			}
			if text != "" {
				PrintResult(text + docInclude(t))
				return
			}
		}
		LogFatal("Name %s has no public member %s.", name, member)
	}
	// The name is shown in full, and so is the type it aliases. Base classes only show their members.
	result, full, described := "", true, ""
	for _, t := range chain {
		switch members := memberText(t.docs); {
		case full:
			result += docText(t) + members
			if t.generated {
				described = t.Name
			}
			if full = t.isAlias(); full {
				result += "\n"
			}
		case members != "":
			result += "\nInherited from " + t.Name + ":" + members
		}
	}
	if described != "" {
		result += "\nSee Godot's help for a description of " + described + ".\n"
	}
	PrintResult(result)
}

// indexText returns a line per type in names, i.e. not functions and
// variables: what declares it and its name, then its base or what it
// aliases, e.g. "template class TypedArray: Array".
func indexText(names []godotName) string {
	var text strings.Builder
	for _, n := range names {
		if strings.HasSuffix(n.Decl, "function") || strings.HasSuffix(n.Decl, "variable") {
			continue
		}
		text.WriteString(n.Decl + " " + n.Name)
		switch {
		case n.Base != "" && strings.HasSuffix(n.Decl, "alias"):
			text.WriteString(" = " + n.Base)
		case n.Base != "":
			text.WriteString(": " + n.Base)
		}
		text.WriteString("\n")
	}
	return text.String()
}

// docChain returns the type called name, then the type it aliases or its
// base class, and so on, as far as godot-cpp declares them.
func docChain(pkg Package, names []godotName, name string) []docType {
	var chain []docType
	for name != "" && len(chain) < 100 {
		i := slices.IndexFunc(names, func(n godotName) bool { return n.Name == name })
		if i < 0 {
			break
		}
		header, generated := godotHeader(pkg, names[i])
		docs := scanCppDocs(header.ReadString(), name)
		Assert(len(docs) > 0, "Failed to find the declaration of %s in %s.", name, header.ToString())
		chain = append(chain, docType{names[i], docs, generated})
		next := ""
		for _, d := range docs {
			next = cmp.Or(next, d.alias, d.base)
		}
		name = next
	}
	return chain
}

// similarGodotNames returns up to 10 of names that contain name, ignoring
// case, or none if there are more.
func similarGodotNames(names []godotName, name string) []string {
	var similar []string
	for _, n := range names {
		if strings.Contains(strings.ToLower(n.Name), strings.ToLower(name)) {
			similar = append(similar, n.Name)
		}
	}
	if len(similar) > 10 {
		return nil
	}
	return similar
}

// godotHeader returns the header in the package's build cache that declares
// n, and whether it was generated from Godot's API spec.
func godotHeader(pkg Package, n godotName) (Path, bool) {
	roots := bindingRoots(pkg)
	for i, root := range roots {
		if header := root.Cd(strings.Trim(n.Include, "<>")); header.IsFile() {
			return header, i == len(roots)-1
		}
	}
	LogFatal("Failed to find the header %s, run `gd++ clean` to fix this.", n.Include)
	return Path{}, false
}

// docText returns the declarations of t, each with its comments, then its
// include.
func docText(t docType) string {
	text := ""
	for _, d := range t.docs {
		text += Styled(d.comments, Gray) + d.head + "\n"
	}
	return text + docInclude(t)
}

// docInclude returns the include line of t.
func docInclude(t docType) string {
	return Styled("    #include "+t.Include, Gray) + "\n"
}

// memberText returns the members of docs, indented, after a blank line.
// Members with comments get a blank line before them too.
func memberText(docs []cppDoc) string {
	var text strings.Builder
	for _, d := range docs {
		for _, m := range d.members {
			if text.Len() == 0 || m.comments != "" {
				text.WriteString("\n")
			}
			text.WriteString(indent(Styled(m.comments, Gray) + m.text))
		}
	}
	return text.String()
}

// indent returns the lines of text, each indented and ending with "\n".
func indent(text string) string {
	return "    " + strings.ReplaceAll(strings.TrimSuffix(text, "\n"), "\n", "\n    ") + "\n"
}
