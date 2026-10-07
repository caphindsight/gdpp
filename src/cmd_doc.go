package main

import (
	"cmp"
	"slices"
	"strings"

	"gd++/trans"
)

// CmdDoc prints what godot-cpp or the GD++ runtime declares under a name,
// like `go doc`: the
// declaration, the header to include, and the public members of a class or
// the values of an enum, then the members it inherits, by base class. An
// alias is shown with the type it names. With NAME.MEMBER, it prints the
// members of that name, looking through aliases and base classes too. Names
// come from the bindings and the runtime header of a package. godot-cpp's own
// helpers, e.g. TypedArray, are documented nowhere else, while Godot's help describes the
// engine's classes. Without a name, or with --index, it lists the types
// instead, with what declares them and their bases. With --notifications, it
// lists the notifications of each Godot class, which on blocks handle.
type CmdDoc struct {
	Args          []string `arg:"positional" placeholder:"[PKG] [NAME]" help:"the name to show, e.g. TypedArray, Array.push_back or Ext, after a path in the package whose bindings to use [default package: the current directory's]"`
	Index         bool     `arg:"-i,--index" help:"list the types that godot-cpp and the GD++ runtime declare, with their kinds and bases [default: without a name]"`
	Notifications bool     `arg:"--notifications" help:"list the notifications of each Godot class, as on blocks name them, e.g. ready for on ready"`
}

// docType is a name that godot-cpp or the runtime declares, with its
// declarations, and where its header comes from: fromGodotCpp, fromRuntime or
// fromGodotAPI.
type docType struct {
	godotName
	docs []cppDoc
	from string
}

const (
	fromGodotCpp = "godot-cpp"
	fromRuntime  = "the GD++ runtime"
	fromGodotAPI = "the Godot API" // Generated from Godot's API spec.
)

// isAlias reports whether t is an alias, with no members of its own.
func (t docType) isAlias() bool {
	return len(t.docs) == 1 && t.docs[0].alias != "" && len(t.docs[0].members) == 0
}

func (c *CmdDoc) Run() {
	Assert(!c.Index || !c.Notifications, "Invalid arguments: --index cannot be used with --notifications.")
	Assert(!c.Notifications || len(c.Args) <= 1, "Invalid arguments: --notifications cannot be used with a name.")
	index := c.Index || c.Notifications || len(c.Args) == 0
	Assert(!index || len(c.Args) <= 1, "Invalid arguments: --index cannot be used with a name.")
	Assert(len(c.Args) <= 2, "Invalid arguments: expected a name, optionally after a package path.")
	path := Cwd()
	if len(c.Args) == 2 || index && len(c.Args) == 1 {
		path = ParsePath(c.Args[0])
	}
	root, ok := GetPackageRootMaybe(path)
	Assert(ok, "Path %s is not contained in a GD++ package, run this in one or pass one, e.g. `gd++ doc PKG NAME`.", path.ToString())
	pkg := LoadPackage(root)
	if c.Notifications {
		generateBuildCache(LoadProject(root), pkg)
		PageResult(notificationsText(readSpec(pkg.BuildCache.Cd("extension_api.json"))))
		return
	}
	names := docNames(LoadProject(root), pkg)
	if index {
		PageResult(indexText(names))
		return
	}
	name, member, _ := strings.Cut(c.Args[len(c.Args)-1], ".")
	if !slices.ContainsFunc(names, func(n godotName) bool { return n.Name == name }) {
		similar := similarGodotNames(names, name)
		Assert(len(similar) == 0, "There is no name %s in godot-cpp or the GD++ runtime. Similar names: %s.", name, strings.Join(similar, ", "))
		LogFatal("There is no name %s in godot-cpp or the GD++ runtime.", name)
	}
	chain, types := docChain(pkg, names, name), cppTypes(names)
	if member != "" {
		for _, t := range chain {
			text := ""
			for _, d := range t.docs {
				for _, m := range d.members {
					if m.name == member {
						text += styledLines(m.comments, CodeComment) + highlightCpp(m.text, types) + d.terminator() + "\n"
					}
				}
			}
			if text != "" {
				PageResult(docInclude(t) + "\n" + text)
				return
			}
		}
		LogFatal("Name %s has no public member %s.", name, member)
	}
	// The name is shown in full, and so is the type it aliases. Base classes only show their members, inside the
	// name's braces.
	k := slices.IndexFunc(chain, func(t docType) bool { return !t.isAlias() })
	if k < 0 {
		k = len(chain) - 1
	}
	inherited := ""
	for _, t := range chain[k+1:] {
		members := ""
		for _, d := range t.docs {
			members += memberText(d, types)
		}
		if members != "" {
			inherited += "\n" + indent(Styled("// Inherited from "+t.Name+":", CodeComment)) + members
		}
	}
	result, described := "", ""
	for i, t := range chain[:k+1] {
		if i == 0 || t.Include != chain[i-1].Include {
			result += docInclude(t)
		}
		result += "\n" + docText(t, types, inherited, i == k)
		if t.from == fromGodotAPI {
			described = t.Name
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
		text.WriteString(Styled(n.Decl, CodeKeyword) + " " + highlightCpp(line, types) + "\n")
	}
	return text.String()
}

// notificationsText returns each class of spec that has notifications, e.g.
// "CanvasItem:", then its notifications as on blocks name them, e.g. "draw",
// one per indented line. Both are sorted by name.
func notificationsText(spec apiSpec) string {
	var text strings.Builder
	var classes []string
	for class := range spec.notifs {
		classes = append(classes, class)
	}
	slices.Sort(classes)
	for _, class := range classes {
		names := strings.Split(strings.ToLower(strings.Join(spec.notifs[class], "\n")), "\n")
		slices.Sort(names)
		text.WriteString(Styled(class, CodeType) + ":\n" + indent(strings.Join(names, "\n")))
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
var cppKeywords = wordSet("alignas auto class const constexpr consteval decltype default delete enum explicit extern false final " +
	"friend inline mutable namespace noexcept nullptr operator override private protected public sizeof static struct template " +
	"this true typedef typename union using virtual volatile")

// cppBuiltinTypes are C++'s own types, and those of <cstdint> and <cstddef>.
var cppBuiltinTypes = wordSet("bool char char16_t char32_t wchar_t double float int long short signed unsigned void " +
	"int8_t int16_t int32_t int64_t uint8_t uint16_t uint32_t uint64_t size_t intptr_t uintptr_t")

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
			src = Styled(src, CodeLiteral)
		case cppKeywords[string(t)]:
			src = Styled(src, CodeKeyword)
		case cppBuiltinTypes[string(t)] || types[string(t)]:
			src = Styled(src, CodeType)
		case t.isIdent() && tokens.at(i+1) == "(":
			src = Styled(src, CodeFunction)
		}
		out.WriteString(src)
		end = spans[i][1]
	}
	return out.String() + text[end:]
}

// docChain returns the type called name, then the type it aliases or its
// base class, and so on, as far as godot-cpp and the runtime declare them.
func docChain(pkg Package, names []godotName, name string) []docType {
	var chain []docType
	for name != "" && len(chain) < 100 {
		i := slices.IndexFunc(names, func(n godotName) bool { return n.Name == name })
		if i < 0 {
			break
		}
		src, ns, from := docSource(pkg, names[i])
		docs := scanCppDocs(src, ns, name)
		Assert(len(docs) > 0, "Failed to find the declaration of %s in %s.", name, names[i].Include)
		chain = append(chain, docType{names[i], docs, from})
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

// docNames returns the names that the package's godot-cpp declares, then
// those that its runtime header declares in namespace gdpp but godot-cpp
// doesn't, sorted.
func docNames(p Project, pkg Package) []godotName {
	names := packageGodotNames(p, pkg)
	for _, h := range runtimeHeaders(pkg) {
		for _, d := range scanCppDecls(h[1], "gdpp") {
			if !slices.ContainsFunc(names, func(n godotName) bool { return n.Name == d.name }) {
				names = append(names, godotName{Name: d.name, Include: h[0], Decl: d.kind, Base: cmp.Or(d.base, d.target)})
			}
		}
	}
	slices.SortStableFunc(names, func(a, b godotName) int { return strings.Compare(a.Name, b.Name) })
	return names
}

// runtimeHeaders returns the include of each of the package's runtime
// headers, e.g. "<gd++/syntax_0.hpp>", and its contents: the runtime, and the
// runtime of shaders, if its syntax has one.
func runtimeHeaders(pkg Package) [][2]string {
	name, src, err := trans.RuntimeHeader(pkg.Config.Syntax)
	Check(err, "Failed to find the GD++ runtime header")
	headers := [][2]string{{"<" + name + ">", src}}
	if name, src, err := trans.GpuRuntimeHeader(pkg.Config.Syntax); err == nil && name != "" {
		headers = append(headers, [2]string{"<" + name + ">", src})
	}
	return headers
}

// docSource returns the source of the header that declares n, its namespace,
// and where it comes from: the runtime header, or one in the package's build
// cache.
func docSource(pkg Package, n godotName) (string, string, string) {
	for _, h := range runtimeHeaders(pkg) {
		if n.Include == h[0] {
			return h[1], "gdpp", fromRuntime
		}
	}
	roots := bindingRoots(pkg)
	for i, root := range roots {
		if header := root.Cd(strings.Trim(n.Include, "<>")); header.IsFile() {
			return header.ReadString(), "godot", map[bool]string{true: fromGodotAPI, false: fromGodotCpp}[i == len(roots)-1]
		}
	}
	LogFatal("Failed to find the header %s, run `gd++ clean` to fix this.", n.Include)
	return "", "", ""
}

// docText returns the declarations of t, each with its comments and public
// members in braces. inherited goes in the braces of the last one if last is
// set.
func docText(t docType, types map[string]bool, inherited string, last bool) string {
	text := ""
	for i, d := range t.docs {
		body := strings.TrimPrefix(memberText(d, types), "\n")
		if last && i == len(t.docs)-1 {
			body += inherited
		}
		text += styledLines(d.comments, CodeComment) + highlightCpp(d.head, types)
		switch {
		case body == "":
			text += ";\n"
		case strings.HasPrefix(d.head, "namespace"):
			text += " {\n" + body + "}\n"
		default:
			text += " {\n" + body + "};\n"
		}
	}
	return text
}

// docInclude returns the include line of t, with where it comes from.
func docInclude(t docType) string {
	return Styled("#include", CodePreProc) + " " + t.Include + " " + Styled("// From "+t.from+".", CodeComment) + "\n"
}

// terminator returns what ends a member of d: "," for an enum's values, ";"
// otherwise.
func (d cppDoc) terminator() string {
	if strings.HasPrefix(d.head, "enum") {
		return ","
	}
	return ";"
}

// memberText returns the members of d, indented, each with a blank line
// before it if it has comments.
func memberText(d cppDoc, types map[string]bool) string {
	var text strings.Builder
	for _, m := range d.members {
		if m.comments != "" {
			text.WriteString("\n")
		}
		text.WriteString(indent(styledLines(m.comments, CodeComment) + highlightCpp(m.text, types) + d.terminator()))
	}
	return text.String()
}

// indent returns the lines of text, each indented by --tab-width spaces and
// ending with "\n".
func indent(text string) string {
	tab := strings.Repeat(" ", Args.TabWidth)
	return tab + strings.ReplaceAll(strings.TrimSuffix(text, "\n"), "\n", "\n"+tab) + "\n"
}
