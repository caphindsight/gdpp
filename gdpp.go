// gdpp.go: the GD++ files of packages: finding them and what they declare,
// and transpiling them to C++ for builds.

package main

import (
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
const godotNamesVersion = 2

// godotNamesCache is a names cache: the names that godot-cpp, or the engine,
// declares.
type godotNamesCache struct {
	Version int         `toml:"version"`
	Names   []godotName `toml:"name"`
}

// loadNamesCache returns the names in the names cache file, if it's there and
// has the version.
func loadNamesCache(file Path, version int) ([]godotName, bool) {
	var cache godotNamesCache
	if !file.IsFile() {
		return nil, false
	}
	// Not decodeToml: older versions may have other keys.
	_, err := toml.Decode(file.ReadString(), &cache)
	return cache.Names, err == nil && cache.Version == version
}

// loadGodotNames returns the names that the package's godot-cpp declares:
// from the build cache's names cache, or else by calling generateBindings to
// generate the bindings, then scanning godot-cpp's headers.
func loadGodotNames(pkg Package, generateBindings func()) []godotName {
	file := pkg.BuildCache.Cd("godot_names.toml")
	if names, ok := loadNamesCache(file, godotNamesVersion); ok {
		return names
	}
	generateBindings()
	s := Silence()
	t := LogTask("Scanning bindings for %s...", styledPackageName(pkg.Root))
	names := scanGodotNames([]Path{
		pkg.BuildCache.Cd("godot-cpp/include"),
		pkg.BuildCache.Cd("build/godot-cpp/gen/include"),
	})
	file.WriteString(encodeToml(godotNamesCache{godotNamesVersion, names}))
	t.Done()
	s.End()
	return names
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

// transpilePackage transpiles the package's GD++ files into dir: a header and
// a source per file, the runtime header, and with docs, each class's XML
// documentation. Files that nothing generates any more are deleted. Returns
// the classes the files declare.
func transpilePackage(pkg Package, dir Path, files []gdppFile, names []godotName, docs bool) []gdppClass {
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
	godot := map[string]godotName{}
	var godotDeps []trans.Dependency
	for _, n := range names {
		godot[n.Name] = n
		godotDeps = append(godotDeps, trans.Dependency{Name: n.Name, Include: n.Include, Kind: n.Kind, Cpp: n.Cpp})
	}
	kinds := gdppKinds(files, godot)

	written := map[string]bool{}
	write := func(rel, text string) {
		written[rel] = true
		dir.Cd(rel).CreateParentDirectory()
		writeIfChanged(dir.Cd(rel), text)
	}
	syntax := pkg.Config.Syntax
	check := func(text string, err error) string {
		if err != nil {
			FailWithText(err)
		}
		return text
	}
	for _, f := range files {
		deps := slices.Clone(godotDeps)
		for _, g := range files {
			for _, d := range g.Decls {
				if g.Rel != f.Rel {
					deps = append(deps, trans.Dependency{Name: d.Name, Include: `"` + g.Rel + `.h"`, Kind: kinds[d.Name], Values: d.Values})
				}
			}
		}
		// #line names the GD++ file relative to the build cache, where SCons runs, like it names C++ sources.
		opts := trans.Options{Dependencies: deps, SourceName: "../" + f.Rel, HeaderName: f.Rel + ".h", CodeName: f.Rel + ".cpp"}
		name := f.File.ToString()
		write(f.Rel+".h", check(trans.GenerateHeader(name, f.Src, opts, syntax)))
		write(f.Rel+".cpp", check(trans.GenerateSource(name, f.Src, opts, syntax)))
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
