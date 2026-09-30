package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gd++/trans"
)

// CmdTrans transpiles a GD++ file and prints the C++ files it generates, each
// after a line naming it. It's for trying out GD++, not for builds. In a
// package, the file's dependencies are the package's other GD++ files and
// Godot's classes from the package's spec and bindings, like in a build.
// Outside of one, they come from flags only. Either way, flags add
// dependencies, and --spec takes Godot's classes from a spec in the project's
// cache instead.
type CmdTrans struct {
	File             string   `arg:"positional" help:"the GD++ file to transpile"`
	Runtime          bool     `arg:"--runtime" help:"print the runtime header that all generated C++ includes, without a file"`
	Syntax           *int     `arg:"--syntax" placeholder:"N" help:"the GD++ syntax version [default: the package's, or else 0]"`
	Spec             string   `arg:"--spec" placeholder:"NAME" help:"take Godot's classes and enums from this Godot API spec in the project's cache, instead of the package's"`
	NoSpec           bool     `arg:"--nospec" help:"don't take Godot's classes and enums from the package's spec"`
	Object           []string `arg:"--object,separate" placeholder:"NAME[=INCLUDE]" help:"a class that isn't refcounted; the include defaults to godot-cpp's header"`
	RefCounted       []string `arg:"--refcounted,separate" placeholder:"NAME[=INCLUDE]" help:"a refcounted class; the include defaults to godot-cpp's header"`
	Extern           []string `arg:"--extern,separate" placeholder:"NAME[=INCLUDE]" help:"an extern of another GD++ file, whose base isn't refcounted [default include: \"NAME.h\"]"`
	RefCountedExtern []string `arg:"--refcounted-extern,separate" placeholder:"NAME[=INCLUDE]" help:"an extern of another GD++ file, whose base is refcounted [default include: \"NAME.h\"]"`
	Enum             []string `arg:"--enum,separate" placeholder:"NAME[=INCLUDE][:VALUES]" help:"an enum of another GD++ file, with its values, e.g. Suit:HEARTS,SPADES=5 [default include: \"NAME.h\"]"`
	DebugOptions
}

func (c *CmdTrans) Run() {
	syntax := 0
	if c.Syntax != nil {
		syntax = *c.Syntax
	}
	if c.Runtime {
		Assert(c.File == "", "Invalid arguments: --runtime cannot be used with a file.")
		_, text, err := trans.RuntimeHeader(syntax)
		Check(err, "Failed to print the runtime header")
		PageResult(highlightCode(text, "cpp"))
		return
	}
	Assert(c.File != "", "Invalid arguments: missing the GD++ file.")
	Assert(c.Spec == "" || !c.NoSpec, "Invalid arguments: --spec and --nospec cannot be used together.")
	c.DebugOptions.validate()
	file := ParsePath(c.File)
	Assert(file.IsFile(), "There is no file at %s.", file.ToString())
	var files []gdppFile
	var names []godotName
	var enums []trans.Dependency
	self := ""
	if root, ok := GetPackageRootMaybe(file); ok {
		p, pkg := LoadProject(root), LoadPackage(root)
		files, self = listGdppFiles(p, pkg), relPath(root, file)
		if c.Syntax == nil {
			syntax = pkg.Config.Syntax
		}
		if c.Spec == "" && !c.NoSpec {
			names = packageGodotNames(p, pkg)
			enums = specEnums(pkg.BuildCache.Cd("extension_api.json"))
		}
	}
	if c.Spec != "" {
		names, enums = specNames(c.Spec)
	}
	// Flags come first, so they win over other dependencies of the same name.
	deps := append(c.flagDependencies(), packageDeps(files, names, enums, self)...)
	generated, err := trans.Generate(c.File, file.ReadString(), c.transOptions(trans.Options{Dependencies: deps}), syntax)
	if err != nil {
		FailWithText(err)
	}
	if len(generated) == 0 {
		LogInfo("The file declares no classes, externs or enums.")
	}
	var text strings.Builder
	for i, f := range generated {
		if i > 0 {
			text.WriteString("\n")
		}
		text.WriteString(Styled("// ==== "+f.Name+" ====", Gray) + "\n\n" + highlightCode(f.Text, "cpp"))
	}
	PageResult(text.String())
}

// flagDependencies returns the dependencies from the flags.
func (c *CmdTrans) flagDependencies() []trans.Dependency {
	var deps []trans.Dependency
	for _, flag := range []struct {
		name   string
		values []string
		kind   trans.Kind
	}{
		{"--object", c.Object, trans.Object},
		{"--refcounted", c.RefCounted, trans.RefCounted},
		{"--extern", c.Extern, trans.Extern},
		{"--refcounted-extern", c.RefCountedExtern, trans.RefCountedExtern},
		{"--enum", c.Enum, trans.Enum},
	} {
		for _, value := range flag.values {
			dep, err := parseTransDep(value, flag.kind)
			Check(err, "Invalid arguments: %s %s", flag.name, value)
			deps = append(deps, dep)
		}
	}
	return deps
}

// specNames returns Godot's classes and enums in the API spec named name in
// the project's cache.
func specNames(name string) ([]godotName, []trans.Dependency) {
	cache := LoadProject(Cwd()).Caches[slices.IndexFunc(depKinds, func(k DepKind) bool { return k.Name == "spec" })]
	Assert(cache.Has(name), "Missing %s %s, run `gd++ fetch --spec %s` to fetch it.", cache.Desc, name, name)
	spec := cache.GetPath(name).Cd("extension_api.json")
	var api struct {
		Classes []struct {
			Name         string `json:"name"`
			Inherits     string `json:"inherits"`
			IsRefcounted bool   `json:"is_refcounted"`
		} `json:"classes"`
	}
	Check(json.Unmarshal([]byte(spec.ReadString()), &api), "Failed to parse %s", spec.ToString())
	Assert(len(api.Classes) > 0, "Failed to find Godot's classes in %s.", spec.ToString())
	var names []godotName
	for _, class := range api.Classes {
		kind := trans.Object
		if class.IsRefcounted {
			kind = trans.RefCounted
		}
		names = append(names, godotName{Name: class.Name, Include: godotCppInclude(class.Name), Kind: kind, Base: class.Inherits})
	}
	return names, specEnums(spec)
}

// parseTransDep parses a dependency flag's value: NAME[=INCLUDE], plus
// [:VALUES] for enums, where VALUES are like A,B=5,C. An include without <>
// or quotes gets quotes.
func parseTransDep(s string, kind trans.Kind) (trans.Dependency, error) {
	head, values, hasValues := strings.Cut(s, ":")
	name, include, hasInclude := strings.Cut(head, "=")
	dep := trans.Dependency{Name: name, Include: include, Kind: kind}
	switch {
	case !classNameRegexp.MatchString(name):
		return dep, fmt.Errorf("%q is not a valid name", name)
	case hasInclude && include == "":
		return dep, fmt.Errorf("the include after = is empty")
	case hasValues && kind != trans.Enum:
		return dep, fmt.Errorf("only enums have values")
	case !hasInclude && (kind == trans.Object || kind == trans.RefCounted):
		dep.Include = godotCppInclude(name)
	case !hasInclude:
		dep.Include = `"` + name + `.h"`
	case !strings.HasPrefix(include, "<") && !strings.HasPrefix(include, `"`):
		dep.Include = `"` + include + `"`
	}
	next := int64(0)
	for _, v := range strings.Split(values, ",") {
		if !hasValues {
			break
		}
		valueName, number, hasNumber := strings.Cut(strings.TrimSpace(v), "=")
		if !classNameRegexp.MatchString(valueName) {
			return dep, fmt.Errorf("%q is not a valid enum value name", valueName)
		}
		if hasNumber {
			n, err := strconv.ParseInt(number, 0, 64)
			if err != nil {
				return dep, fmt.Errorf("%q is not an integer", number)
			}
			next = n
		}
		dep.Values = append(dep.Values, trans.EnumValue{Name: valueName, Value: next})
		next++
	}
	return dep, nil
}

var (
	snakeWordRegexp  = regexp.MustCompile(`(.)([A-Z][a-z]+)`)
	snakeUpperRegexp = regexp.MustCompile(`([a-z0-9])([A-Z])`)
)

// snakeCase converts a class name to snake case like godot-cpp names its
// headers, e.g. MeshInstance3D to mesh_instance3d.
func snakeCase(name string) string {
	name = snakeWordRegexp.ReplaceAllString(name, "${1}_${2}")
	name = snakeUpperRegexp.ReplaceAllString(name, "${1}_${2}")
	return strings.ToLower(strings.NewReplacer("2_D", "2D", "3_D", "3D").Replace(name))
}

// godotCppInclude returns the include of godot-cpp's header of a Godot class.
func godotCppInclude(name string) string {
	return "<godot_cpp/classes/" + snakeCase(name) + ".hpp>"
}
