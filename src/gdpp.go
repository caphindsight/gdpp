// gdpp.go: the GD++ files of packages: finding them and what they declare,
// and transpiling them to C++ for builds.

package main

import (
	"encoding/json"
	"path"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"gd++/trans"
)

// gdppExtensions are the extensions of GD++ files.
var gdppExtensions = []string{".gd++", ".gdpp", ".gg"}

// cppExtensions are the extensions of C and C++ sources and headers.
var cppExtensions = []string{".c", ".cc", ".cpp", ".cxx", ".c++", ".h", ".hh", ".hpp", ".hxx", ".h++", ".inl"}

// gdppDirName is the build cache directory that holds the C++ generated from
// the package's GD++ files.
const gdppDirName = "gdpp"

// gdppFile is a GD++ file of a package, with what it declares. Err is set,
// and Decls empty, if the file has errors.
type gdppFile struct {
	File   Path
	Rel    string // Relative to the package root, e.g. "src/player.gd++".
	Src    string
	Decls  []trans.Declaration // Its classes, externs and enums, with those that macros generate.
	Macros []trans.Declaration // Its macros, templates and macro libraries.
	Err    error
}

// gdppClass is a class declared in a GD++ file.
type gdppClass struct {
	trans.Declaration
	File gdppFile
}

// listGdppFiles reads the package's GD++ files, and lists what each one
// declares, with macros limited to macroTimeout seconds (0: the default). A
// file's errors don't fail, so gd++ ls can show broken files.
func listGdppFiles(p Project, pkg Package, macroTimeout int) []gdppFile {
	var files []gdppFile
	for _, rel := range packageFiles(p, pkg, gdppExtensions...) {
		file := pkg.Root.Cd(rel)
		src := file.ReadString()
		macros, err := trans.ListMacros(file.ToString(), src, pkg.Config.Syntax)
		files = append(files, gdppFile{File: file, Rel: rel, Src: src, Macros: macros, Err: err})
	}
	// Macros may generate classes, so listing those needs every file's macros.
	for i, f := range files {
		if f.Err == nil {
			opts := packageOptions(pkg, macroDeps(files, f.Rel))
			opts.MacroTimeout = macroTimeout
			files[i].Decls, files[i].Err = trans.ListClasses(f.File.ToString(), f.Src, opts, pkg.Config.Syntax)
		}
	}
	return files
}

// packageOptions returns the options for transpiling the package's GD++
// files, with dependencies deps.
func packageOptions(pkg Package, deps []trans.Dependency) trans.Options {
	return trans.Options{Dependencies: deps, AsyncClass: pkg.AsyncClass(), PackagePath: pkg.ResPath(), PackageID: pkg.Id,
		PackagePrefix: pkg.Prefix(), CppStandard: pkg.Config.CppStandard, MacroDepth: pkg.MacroDepth()}
}

// macroDeps returns the macros, templates, macro libraries and annotations of files,
// except those of the file whose path relative to the package root is self,
// as dependencies.
func macroDeps(files []gdppFile, self string) []trans.Dependency {
	var deps []trans.Dependency
	for _, f := range files {
		for _, m := range f.Macros {
			if f.Rel != self {
				kind := map[trans.DeclKind]trans.Kind{trans.MacroDecl: trans.Macro, trans.TemplateDecl: trans.Template, trans.LibraryDecl: trans.MacroLibrary,
					trans.AnnotationDecl: trans.Annotation, trans.ShaderLibraryDecl: trans.ShaderLibrary}[m.Kind]
				deps = append(deps, trans.Dependency{Name: m.Name, Kind: kind, Source: f.Src, File: f.File.ToString()})
			}
		}
	}
	return deps
}

// assertNotGdppClass asserts that the package at root has no GD++ class
// named name: GD++ classes live in code, so commands can't change them.
func assertNotGdppClass(root Path, name string) {
	pkg := LoadPackageAt(root)
	for _, class := range gdppClasses(listGdppFiles(LoadProject(root), pkg, 0)) {
		if class.Name == name {
			LogFatal("Class %s is declared in %s, so it can only be changed there.", name, class.File.File.ToString())
		}
	}
}

// gdppClasses returns the classes that files declare, sorted by name.
func gdppClasses(files []gdppFile) []gdppClass {
	var classes []gdppClass
	for _, f := range files {
		for _, d := range f.Decls {
			if d.Kind == trans.ClassDecl {
				classes = append(classes, gdppClass{d, f})
			}
		}
	}
	slices.SortStableFunc(classes, func(a, b gdppClass) int { return strings.Compare(a.Name, b.Name) })
	return classes
}

// godotNamesVersion is the version of the names cache's format and of the
// scanner that fills it. Bump it when either changes, to rescan.
const godotNamesVersion = 6

// godotNamesCache is a names cache: the names that godot-cpp declares.
type godotNamesCache struct {
	Version int         `toml:"version"`
	Names   []godotName `toml:"name"`
}

// loadNamesCache returns the names in the names cache file, if it's there and
// has godotNamesVersion.
func loadNamesCache(file Path) ([]godotName, bool) {
	var cache godotNamesCache
	if !file.IsFile() {
		return nil, false
	}
	// Not decodeToml: older versions may have other keys.
	_, err := toml.Decode(file.ReadString(), &cache)
	return cache.Names, err == nil && cache.Version == godotNamesVersion
}

// loadGodotNames returns the names that the package's godot-cpp declares:
// from the build cache's names cache, or else by calling generateBindings to
// generate the bindings, then scanning godot-cpp's headers.
func loadGodotNames(pkg Package, generateBindings func()) []godotName {
	file := pkg.BuildCache.Cd("godot_names.toml")
	if names, ok := loadNamesCache(file); ok {
		return names
	}
	generateBindings()
	s := Silence()
	t := LogTask("Scanning bindings for %s...", pkg.Root.ToString())
	names := scanGodotNames(bindingRoots(pkg))
	file.WriteString(encodeToml(godotNamesCache{godotNamesVersion, names}))
	t.Done()
	s.End()
	return names
}

// bindingRoots returns the directories of godot-cpp's headers in the
// package's build cache: the hand-written ones, then the generated ones.
func bindingRoots(pkg Package) []Path {
	return []Path{pkg.BuildCache.Cd("godot-cpp/include"), pkg.BuildCache.Cd("build/godot-cpp/gen/include")}
}

// bindingNames returns the names that the package's godot-cpp declares, like
// loadGodotNames, generating the bindings with SCons arguments bindArgs.
func bindingNames(pkg Package, bindArgs []string) []godotName {
	return loadGodotNames(pkg, func() {
		assertScons()
		s := Silence()
		Exec("Compiling bindings for "+pkg.Root.ToString()+"...", pkg.BuildCache, "scons", append(bindArgs, "--gdpp-bindings")...)
		s.End()
	})
}

// packageGodotNames returns the names that the package's godot-cpp declares,
// after syncing its build cache. Bindings are generated like a default build
// for this machine does, if needed.
func packageGodotNames(p Project, pkg Package) []godotName {
	generateBuildCache(p, pkg)
	return bindingNames(pkg, BuildOptions{}.sconsArgs(hostPlatform+"."+hostArch))
}

// cppClassNames returns the package's C++ classes that GD++ code may use: those
// with a kind, each with its include like __register_types__.cpp has it, and
// its base if its header declares it in namespace godot.
func cppClassNames(pkg Package) []godotName {
	var names []godotName
	for _, class := range pkg.Config.Classes {
		if class.Kind == "" {
			continue
		}
		kind := map[string]trans.Kind{"ptr": trans.Object, "ref": trans.RefCounted}[class.Kind]
		names = append(names, godotName{Name: class.Name, Include: classInclude(class.Include), Kind: kind, Decl: "class", Base: cppClassBase(pkg, class)})
	}
	return names
}

// cppClassBase returns the public base of the package's C++ class, as its
// header declares it in namespace godot or at global scope, or "" if unknown.
func cppClassBase(pkg Package, class PackageClass) string {
	base := ""
	if header := pkg.ClassPath(class.Include); header.IsFile() {
		src := header.ReadString()
		for _, d := range slices.Concat(scanCppDecls(src, "godot"), scanCppDecls(src, "")) {
			if d.name == class.Name && d.baseAccess == "public" {
				base = d.base
			}
		}
	}
	return base
}

// nonRuntimeClasses returns the package's classes, C++ ones and GD++ ones of
// decls, that Godot registers as non-runtime classes: those with tool or
// abstract, and those that extend one of them. Godot doesn't let
// runtime classes extend them, so their GD++ subclasses are guarded instead.
func nonRuntimeClasses(pkg Package, decls []trans.Declaration) map[string]bool {
	own, bases := map[string]bool{}, map[string]string{}
	for _, c := range pkg.Config.Classes {
		own[c.Name], bases[c.Name] = c.Tool || c.Abstract, cppClassBase(pkg, c)
	}
	for _, d := range decls {
		if d.Kind == trans.ClassDecl {
			own[d.Name], bases[d.Name] = d.Tool || d.Abstract, d.Base
		}
	}
	var nonRuntime func(name string, depth int) bool
	nonRuntime = func(name string, depth int) bool { // depth stops at cycles, which other checks report.
		return depth <= len(bases) && (own[name] || bases[name] != "" && nonRuntime(bases[name], depth+1))
	}
	classes := map[string]bool{}
	for name := range bases {
		classes[name] = nonRuntime(name, 0)
	}
	return classes
}

// fileDecls returns what the files declare.
func fileDecls(files []gdppFile) []trans.Declaration {
	var decls []trans.Declaration
	for _, f := range files {
		decls = append(decls, f.Decls...)
	}
	return decls
}

// apiSpec is what GD++ code needs from the API spec file.
type apiSpec struct {
	enums    []trans.Dependency        // The enums, which GD++ enums may extend: those of classes, e.g. Node.ProcessMode, and global ones, e.g. Error.
	virtuals map[string][]string       // Each class's own virtual methods, e.g. _input for Node, which GD++ code overrides with @override.
	notifs   map[string][]string       // Each class's own notifications, without NOTIFICATION_, e.g. READY for Node, which on blocks handle.
	signals  map[string][]trans.Signal // Each class's own signals, which on blocks connect to.
	editor   map[string]bool           // The editor's classes, e.g. EditorPlugin, which only exist in the editor.
}

// readSpec reads the API spec file. Returns an empty spec if there is no file.
func readSpec(file Path) apiSpec {
	if !file.IsFile() {
		return apiSpec{}
	}
	type enum struct {
		Name       string            `json:"name"`
		IsBitfield bool              `json:"is_bitfield"`
		Values     []trans.EnumValue `json:"values"`
	}
	var api struct {
		GlobalEnums []enum `json:"global_enums"`
		Classes     []struct {
			Name      string `json:"name"`
			APIType   string `json:"api_type"`
			Enums     []enum `json:"enums"`
			Constants []struct {
				Name string `json:"name"`
			} `json:"constants"`
			Methods []struct {
				Name      string `json:"name"`
				IsVirtual bool   `json:"is_virtual"`
			} `json:"methods"`
			Signals []struct {
				Name      string `json:"name"`
				Arguments []struct {
					Name string `json:"name"`
					Type string `json:"type"`
				} `json:"arguments"`
			} `json:"signals"`
		} `json:"classes"`
	}
	Check(json.Unmarshal([]byte(file.ReadString()), &api), "Failed to parse %s", file.ToString())
	spec := apiSpec{virtuals: map[string][]string{}, notifs: map[string][]string{}, signals: map[string][]trans.Signal{}, editor: map[string]bool{}}
	for _, e := range api.GlobalEnums {
		spec.enums = append(spec.enums, trans.Dependency{Name: e.Name, Kind: trans.GodotEnum, Values: e.Values, Bitfield: e.IsBitfield})
	}
	for _, c := range api.Classes {
		for _, e := range c.Enums {
			spec.enums = append(spec.enums, trans.Dependency{Name: c.Name + "." + e.Name, Kind: trans.GodotEnum, Values: e.Values, Bitfield: e.IsBitfield})
		}
		for _, k := range c.Constants {
			if name, ok := strings.CutPrefix(k.Name, "NOTIFICATION_"); ok {
				spec.notifs[c.Name] = append(spec.notifs[c.Name], name)
			}
		}
		for _, m := range c.Methods {
			if m.IsVirtual {
				spec.virtuals[c.Name] = append(spec.virtuals[c.Name], m.Name)
			}
		}
		spec.editor[c.Name] = c.APIType == "editor"
		for _, sig := range c.Signals {
			s := trans.Signal{Name: sig.Name}
			for _, a := range sig.Arguments {
				s.Params = append(s.Params, trans.SignalParam{Name: a.Name, Type: specType(a.Type)})
			}
			spec.signals[c.Name] = append(spec.signals[c.Name], s)
		}
	}
	return spec
}

// specType returns the GD++ type of a type of the API spec, e.g. "Array[Node]" for "typedarray::Node", or "int" for
// an enum, e.g. "enum::Error", since engine enums aren't GD++ types.
func specType(t string) string {
	switch kind, name, _ := strings.Cut(t, "::"); kind {
	case "enum", "bitfield":
		return "int"
	case "typedarray":
		return "Array[" + specType(name) + "]"
	case "typeddictionary":
		return "Dictionary"
	}
	return t
}

// packageDeps returns the dependencies of the package's GD++ file whose path
// relative to the package root is self: the spec's enums, names, e.g.
// godot-cpp's and the package's C++ classes', with the spec's virtual methods, then what the package's other GD++
// files declare, macros and templates last. The spec's enums come first, so e.g. Error is an enum, not just a name.
func packageDeps(files []gdppFile, names []godotName, spec apiSpec, self string, nonRuntime map[string]bool) []trans.Dependency {
	godot := map[string]godotName{}
	for _, n := range names {
		godot[n.Name] = n
	}
	deps := slices.Clone(spec.enums)
	for i, e := range deps {
		// A class's enum, e.g. Node.ProcessMode, is in the class's header; a global one, e.g. Error, in its own.
		class, _, _ := strings.Cut(e.Name, ".")
		deps[i].Include = godot[class].Include
	}
	for _, n := range names {
		dep := trans.Dependency{Name: n.Name, Include: n.Include, Kind: n.Kind, NonRuntime: nonRuntime[n.Name]}
		if n.Kind != trans.Other {
			dep.Base, dep.Virtuals, dep.Notifications, dep.Signals = n.Base, spec.virtuals[n.Name], spec.notifs[n.Name], spec.signals[n.Name]
		}
		deps = append(deps, dep)
	}
	kinds := gdppKinds(files, godot)
	for _, f := range files {
		for _, d := range f.Decls {
			if f.Rel != self {
				dep := trans.Dependency{Name: d.Name, Include: `"` + d.Name + `.h"`, Kind: kinds[d.Name], Values: d.Values, Base: d.Base, Gdpp: true, Bitfield: d.Bitfield, Virtuals: d.Virtuals, NoscriptVirtuals: d.NoscriptVirtuals, Notifications: d.Notifications,
					NonRuntime: nonRuntime[d.Name], Traits: d.Traits, Signals: d.Signals}
				if d.Kind == trans.TraitDecl { // Its classes check and copy its functions.
					dep.Source, dep.File = f.Src, f.File.ToString()
				}
				deps = append(deps, dep)
			}
		}
	}
	return append(deps, macroDeps(files, self)...)
}

// traitImplementers returns, for each trait of files, the sorted names of the
// classes of files that implement it, themselves or through a base.
func traitImplementers(files []gdppFile) map[string][]string {
	classes := map[string]trans.Declaration{}
	for _, d := range fileDecls(files) {
		if d.Kind == trans.ClassDecl {
			classes[d.Name] = d
		}
	}
	implementers := map[string][]string{}
	for name := range classes {
		seen := map[string]bool{} // Also stops at cycles, which transpiling reports.
		for d, ok := classes[name]; ok && !seen[d.Name]; d, ok = classes[d.Base] {
			seen[d.Name] = true
			for _, t := range d.Traits {
				if !slices.Contains(implementers[t], name) {
					implementers[t] = append(implementers[t], name)
				}
			}
		}
	}
	for _, names := range implementers {
		slices.Sort(names)
	}
	return implementers
}

// gdppKinds returns the kinds of the declarations in files, following the
// bases of classes, externs and traits through the files and godot-cpp's names. A
// base that can't be resolved counts as Object: transpiling its file then
// reports it.
func gdppKinds(files []gdppFile, godot map[string]godotName) map[string]trans.Kind {
	decls := map[string]trans.Declaration{}
	for _, f := range files {
		for _, d := range f.Decls {
			decls[d.Name] = d
		}
	}
	kinds := map[string]trans.Kind{}
	var classKind func(name string, depth int) trans.Kind
	classKind = func(name string, depth int) trans.Kind {
		if g, ok := godot[name]; ok && g.Kind == trans.RefCounted {
			return trans.RefCounted
		}
		if d, ok := decls[name]; ok && d.Kind != trans.EnumDecl && depth < 100 {
			return classKind(d.Base, depth+1)
		}
		return trans.Object
	}
	for name, d := range decls {
		switch d.Kind {
		case trans.ClassDecl:
			kinds[name] = classKind(name, 0)
		case trans.ExternDecl:
			kinds[name] = map[trans.Kind]trans.Kind{trans.Object: trans.Extern, trans.RefCounted: trans.RefCountedExtern}[classKind(d.Base, 0)]
		case trans.TraitDecl:
			kinds[name] = map[trans.Kind]trans.Kind{trans.Object: trans.Trait, trans.RefCounted: trans.RefCountedTrait}[classKind(d.Base, 0)]
		default:
			kinds[name] = trans.Enum
		}
	}
	return kinds
}

// transpilePackage transpiles the package's GD++ files into the build cache's
// gdpp directory, with names, e.g. godot-cpp's, and the package's C++ classes
// as dependencies: a header per class, extern and enum, a source per class, the
// runtime header, and with docs, each class's XML documentation. Files that
// nothing generates any more are deleted. Returns the classes the files declare.
func transpilePackage(pkg Package, files []gdppFile, names []godotName, o BuildOptions) []gdppClass {
	dir := pkg.BuildCache.Cd(gdppDirName)
	s := Silence()
	t := LogTask("Transpiling GD++ code for %s...", pkg.Root.ToString())
	owner := map[string]gdppFile{}
	for _, f := range files {
		if f.Err != nil {
			FailWithText(f.Err)
		}
		for _, d := range slices.Concat(f.Decls, f.Macros) {
			if d.Kind == trans.LibraryDecl || d.Kind == trans.ShaderLibraryDecl || d.Kind == trans.AnnotationDecl {
				continue // No name, or one that files may share.
			}
			if prev, ok := owner[d.Name]; ok {
				LogFatal("The name %s is declared in both %s and %s.", d.Name, prev.File.ToString(), f.File.ToString())
			}
			owner[d.Name] = f
		}
	}
	written := map[string]bool{}
	write := func(rel, text string) {
		written[rel] = true
		dir.Cd(rel).CreateParentDirectory()
		writeIfChanged(dir.Cd(rel), text)
	}
	syntax := pkg.Config.Syntax
	spec := readSpec(pkg.BuildCache.Cd("extension_api.json"))
	projectNames := loadProjectNames(LoadProject(pkg.Root), pkg)
	names = append(slices.Clip(names), slices.Concat(cppClassNames(pkg), projectNames.names)...)
	if projectNames.header != "" {
		write(namesHeader, projectNames.header)
	}
	nonRuntime, implementers := nonRuntimeClasses(pkg, fileDecls(files)), traitImplementers(files)
	check := func(text string, err error) string {
		if err != nil {
			FailWithText(err)
		}
		return text
	}
	copies := map[string]string{} // How #line names each GD++ file: by its copy, see below.
	for _, f := range files {
		copies[f.File.ToString()] = sourcesDirName + "/" + f.Rel
	}
	for _, f := range files {
		// #line names the GD++ file's copy, relative to the build cache, where SCons runs, like it names C++ sources.
		// So do the templates of other files, whose C++ code keeps its lines.
		opts := packageOptions(pkg, packageDeps(files, names, spec, f.Rel, nonRuntime))
		opts.Implementers = implementers
		opts.SourceName = copies[f.File.ToString()]
		for i, d := range opts.Dependencies {
			if d.Source != "" {
				opts.Dependencies[i].SourceName = copies[d.File]
			}
		}
		opts = o.transOptions(opts)
		name := f.File.ToString()
		generated, err := trans.Generate(name, f.Src, opts, syntax)
		if err != nil {
			FailWithText(err)
		}
		for _, gen := range generated {
			write(gen.Name, gen.Text)
		}
		for _, d := range f.Decls {
			if o.docs() && d.Kind == trans.ClassDecl {
				write("doc_classes/"+d.Name+".xml", check(trans.DocumentClass(name, f.Src, d.Name, opts, syntax)))
			}
		}
	}
	classes := gdppClasses(files)
	if o.docs() {
		docs, err := trans.DocumentBuiltinClasses(trans.Options{AsyncClass: pkg.AsyncClass(), PackagePrefix: pkg.Prefix()}, syntax)
		Check(err, "Failed to document the classes that GD++ adds")
		used := map[string]bool{pkg.AsyncClass(): slices.ContainsFunc(classes, func(c gdppClass) bool { return c.Async }),
			pkg.GpuArrayClass(): slices.ContainsFunc(classes, func(c gdppClass) bool { return c.Gpu })}
		for _, d := range docs {
			if used[strings.TrimSuffix(d.Name, ".xml")] {
				write("doc_classes/"+d.Name, d.Text)
			}
		}
	}
	runtimeName, runtimeText, err := trans.RuntimeHeader(syntax)
	Check(err, "Failed to generate the GD++ runtime header")
	write(runtimeName, runtimeText)
	if gpuName, gpuText, err := trans.GpuRuntimeHeader(syntax); err == nil && gpuName != "" {
		write(gpuName, gpuText)
	}

	removeStale(dir, "", written)
	t.Done()
	s.End()
	return classes
}

// removeStale deletes the files under dir (rel is dir's path relative to the
// walk's root) that aren't in keep, and directories left empty.
func removeStale(dir Path, rel string, keep map[string]bool) {
	if !dir.IsDir() {
		return
	}
	for _, child := range dir.Ls() {
		childRel := path.Join(rel, child.Name())
		if child.IsDir() {
			removeStale(child, childRel, keep)
			if len(child.Ls()) == 0 {
				child.Remove()
			}
		} else if !keep[childRel] {
			child.Remove()
		}
	}
}
