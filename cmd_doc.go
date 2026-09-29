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
		PageResult(indexText(names))
		return
	}
	name, member, _ := strings.Cut(c.Args[len(c.Args)-1], ".")
	if !slices.ContainsFunc(names, func(n godotName) bool { return n.Name == name }) {
		similar := similarGodotNames(names, name)
		Assert(len(similar) == 0, "There is no name %s in godot-cpp. Similar names: %s.", name, strings.Join(similar, ", "))
		LogFatal("There is no name %s in godot-cpp.", name)
	}
	chain, types := docChain(pkg, names, name), cppTypes(names)
	if member != "" {
		for _, t := range chain {
			text := ""
			for _, d := range t.docs {
				for _, m := range d.members {
					if m.name == member {
						text += Styled(m.comments, Gray) + highlightCpp(m.text, types) + "\n"
					}
				}
			}
			if text != "" {
				PageResult(text + docInclude(t))
				return
			}
		}
		LogFatal("Name %s has no public member %s.", name, member)
	}
	// The name is shown in full, and so is the type it aliases. Base classes only show their members.
	result, full, described := "", true, ""
	for _, t := range chain {
		switch members := memberText(t.docs, types); {
		case full:
			result += docText(t, types) + members
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
	PageResult(result)
}

// indexText returns a line per type in names, i.e. not functions and
// variables: what declares it and its name, then its base or what it
// aliases, e.g. "template class TypedArray: Array".
func indexText(names []godotName) string {
	var text strings.Builder
	types := cppTypes(names)
	for _, n := range names {
		if !types[n.Name] {
			continue
		}
		line := n.Name
		switch {
		case n.Base != "" && strings.HasSuffix(n.Decl, "alias"):
			line += " = " + n.Base
		case n.Base != "":
			line += ": " + n.Base
		}
		text.WriteString(Styled(n.Decl, Yellow) + " " + highlightCpp(line, types) + "\n")
	}
	return text.String()
}

// cppTypes returns the names of the types in names, i.e. not functions and
// variables.
func cppTypes(names []godotName) map[string]bool {
	types := map[string]bool{}
	for _, n := range names {
		if !strings.HasSuffix(n.Decl, "function") && !strings.HasSuffix(n.Decl, "variable") {
			types[n.Name] = true
		}
	}
	return types
}

// cppKeywords are the C++ keywords that declarations use.
var cppKeywords = map[string]bool{}

// cppBuiltinTypes are C++'s own types, and those of <cstdint> and <cstddef>.
var cppBuiltinTypes = map[string]bool{}

func init() {
	for _, k := range strings.Fields("alignas auto class const constexpr consteval decltype default delete enum explicit extern false final " +
		"friend inline mutable namespace noexcept nullptr operator override private protected public sizeof static struct template " +
		"this true typedef typename union using virtual volatile") {
		cppKeywords[k] = true
	}
	for _, t := range strings.Fields("bool char char16_t char32_t wchar_t double float int long short signed unsigned void " +
		"int8_t int16_t int32_t int64_t uint8_t uint16_t uint32_t uint64_t size_t intptr_t uintptr_t") {
		cppBuiltinTypes[t] = true
	}
}

// highlightCpp returns the C++ code text with syntax highlighting, if styles
// are on: keywords, types, i.e. C++'s own and those in types, the names of
// functions, and literals.
func highlightCpp(text string, types map[string]bool) string {
	if !isTTY {
		return text
	}
	tokens, spans := tokenizeCpp(text)
	var out strings.Builder
	end := 0
	for i, t := range tokens {
		out.WriteString(text[end:spans[i][0]])
		src := text[spans[i][0]:spans[i][1]]
		switch {
		case t == "0":
			src = Styled(src, Magenta)
		case cppKeywords[string(t)]:
			src = Styled(src, Yellow)
		case cppBuiltinTypes[string(t)] || types[string(t)]:
			src = Styled(src, Green)
		case t.isIdent() && tokens.at(i+1) == "(":
			src = Styled(src, Cyan)
		}
		out.WriteString(src)
		end = spans[i][1]
	}
	return out.String() + text[end:]
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
func docText(t docType, types map[string]bool) string {
	text := ""
	for _, d := range t.docs {
		text += Styled(d.comments, Gray) + highlightCpp(d.head, types) + "\n"
	}
	return text + docInclude(t)
}

// docInclude returns the include line of t.
func docInclude(t docType) string {
	return Styled("    #include "+t.Include, Gray) + "\n"
}

// memberText returns the members of docs, indented, after a blank line.
// Members with comments get a blank line before them too.
func memberText(docs []cppDoc, types map[string]bool) string {
	var text strings.Builder
	for _, d := range docs {
		for _, m := range d.members {
			if text.Len() == 0 || m.comments != "" {
				text.WriteString("\n")
			}
			text.WriteString(indent(Styled(m.comments, Gray) + highlightCpp(m.text, types)))
		}
	}
	return text.String()
}

// indent returns the lines of text, each indented and ending with "\n".
func indent(text string) string {
	return "    " + strings.ReplaceAll(strings.TrimSuffix(text, "\n"), "\n", "\n    ") + "\n"
}
