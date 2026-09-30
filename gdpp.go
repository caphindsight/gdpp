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

// gdppDirName is the build cache directory that holds the C++ generated from
// the package's GD++ files.
const gdppDirName = "gdpp"

// gdppFile is a GD++ file of a package, with what it declares. Err is set,
// and Decls empty, if the file has errors.
type gdppFile struct {
	File  Path
	Rel   string // Relative to the package root, e.g. "src/player.gd++".
	Src   string
	Decls []trans.Declaration
	Err   error
}

// gdppClass is a class declared in a GD++ file.
type gdppClass struct {
	trans.Declaration
	File gdppFile
}

// listGdppFiles reads the package's GD++ files, and lists what each one
// declares. A file's errors don't fail, so gd++ ls can show broken files.
func listGdppFiles(p Project, pkg Package) []gdppFile {
	var files []gdppFile
	for _, rel := range packageFiles(p, pkg, gdppExtensions...) {
		file := pkg.Root.Cd(rel)
		src := file.ReadString()
		decls, err := trans.ListClasses(file.ToString(), src, pkg.Config.Syntax)
		files = append(files, gdppFile{File: file, Rel: rel, Src: src, Decls: decls, Err: err})
	}
	return files
}

// assertNotGdppClass asserts that the package at root has no GD++ class
// named name: GD++ classes live in code, so commands can't change them.
func assertNotGdppClass(root Path, name string) {
	pkg := LoadPackageAt(root)
	for _, class := range gdppClasses(listGdppFiles(LoadProject(root), pkg)) {
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
	t := LogTask("Scanning bindings for %s...", styledPackageName(pkg.Root))
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
		Exec("Compiling bindings for "+styledPackageName(pkg.Root)+"...", pkg.BuildCache, "scons", append(bindArgs, "--gdpp-bindings")...)
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

// specEnums returns the enums of the API spec file, which GD++ enums may
// extend: those of classes, e.g. Node.ProcessMode, and global ones, e.g.
// Error. Returns nil if there is no file.
func specEnums(file Path) []trans.Dependency {
	if !file.IsFile() {
		return nil
	}
	type enum struct {
		Name       string            `json:"name"`
		IsBitfield bool              `json:"is_bitfield"`
		Values     []trans.EnumValue `json:"values"`
	}
	var api struct {
		GlobalEnums []enum `json:"global_enums"`
		Classes     []struct {
			Name  string `json:"name"`
			Enums []enum `json:"enums"`
		} `json:"classes"`
	}
	Check(json.Unmarshal([]byte(file.ReadString()), &api), "Failed to parse %s", file.ToString())
	var deps []trans.Dependency
	for _, e := range api.GlobalEnums {
		deps = append(deps, trans.Dependency{Name: e.Name, Kind: trans.GodotEnum, Values: e.Values, Bitfield: e.IsBitfield})
	}
	for _, c := range api.Classes {
		for _, e := range c.Enums {
			deps = append(deps, trans.Dependency{Name: c.Name + "." + e.Name, Kind: trans.GodotEnum, Values: e.Values, Bitfield: e.IsBitfield})
		}
	}
	return deps
}

// packageDeps returns the dependencies of the package's GD++ file whose path
// relative to the package root is self: the spec's enums, names, e.g.
// godot-cpp's, then what the package's other GD++ files declare. The spec's
// enums come first, so e.g. Error is an enum, not just a name.
func packageDeps(files []gdppFile, names []godotName, enums []trans.Dependency, self string) []trans.Dependency {
	godot := map[string]godotName{}
	for _, n := range names {
		godot[n.Name] = n
	}
	deps := slices.Clone(enums)
	for i, e := range deps {
		// A class's enum, e.g. Node.ProcessMode, is in the class's header; a global one, e.g. Error, in its own.
		class, _, _ := strings.Cut(e.Name, ".")
		deps[i].Include = godot[class].Include
	}
	for _, n := range names {
		deps = append(deps, trans.Dependency{Name: n.Name, Include: n.Include, Kind: n.Kind})
	}
	kinds := gdppKinds(files, godot)
	for _, f := range files {
		for _, d := range f.Decls {
			if f.Rel != self {
				deps = append(deps, trans.Dependency{Name: d.Name, Include: `"` + d.Name + `.h"`, Kind: kinds[d.Name], Values: d.Values, Base: d.Base, Gdpp: true, Bitfield: d.Bitfield})
			}
		}
	}
	return deps
}

// gdppKinds returns the kinds of the declarations in files, following the
// bases of classes and externs through the files and godot-cpp's names. A
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
		if d, ok := decls[name]; ok && d.Kind == trans.ClassDecl && depth < 100 {
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
		default:
			kinds[name] = trans.Enum
		}
	}
	return kinds
}

// transpilePackage transpiles the package's GD++ files into the build cache's
// gdpp directory: a header per class, extern and enum, a source per class, the
// runtime header, and with docs, each class's XML documentation. Files that
// nothing generates any more are deleted. Returns the classes the files declare.
func transpilePackage(pkg Package, files []gdppFile, names []godotName, docs bool) []gdppClass {
	dir := pkg.BuildCache.Cd(gdppDirName)
	s := Silence()
	t := LogTask("Transpiling GD++ code for %s...", styledPackageName(pkg.Root))
	owner := map[string]gdppFile{}
	for _, f := range files {
		if f.Err != nil {
			FailWithText(f.Err)
		}
		for _, d := range f.Decls {
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
	enums := specEnums(pkg.BuildCache.Cd("extension_api.json"))
	check := func(text string, err error) string {
		if err != nil {
			FailWithText(err)
		}
		return text
	}
	for _, f := range files {
		// #line names the GD++ file relative to the build cache, where SCons runs, like it names C++ sources.
		opts := trans.Options{Dependencies: packageDeps(files, names, enums, f.Rel), SourceName: "../" + f.Rel}
		name := f.File.ToString()
		generated, err := trans.Generate(name, f.Src, opts, syntax)
		if err != nil {
			FailWithText(err)
		}
		for _, gen := range generated {
			write(gen.Name, gen.Text)
		}
		for _, d := range f.Decls {
			if docs && d.Kind == trans.ClassDecl {
				write("doc_classes/"+d.Name+".xml", check(trans.DocumentClass(name, f.Src, d.Name, opts, syntax)))
			}
		}
	}
	runtimeName, runtimeText, err := trans.RuntimeHeader(syntax)
	Check(err, "Failed to generate the GD++ runtime header")
	write(runtimeName, runtimeText)

	removeStale(dir, "", written)
	t.Done()
	s.End()
	return gdppClasses(files)
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
