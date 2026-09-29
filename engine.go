// engine.go: engine builds, which compile all packages of a project into the
// Godot engine as a module, building export templates, in the project build
// cache res://.gd++proj/build.

package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"text/template"

	"github.com/BurntSushi/toml"
)

var (
	//go:embed templates/engine/SCsub
	engineSCsubText     string
	engineSCsubTemplate = template.Must(template.New("").Parse(engineSCsubText))
	//go:embed templates/engine/config.py
	engineConfigText string
	//go:embed templates/engine/register_types.h
	engineRegisterTypesHeaderText string
	//go:embed templates/engine/register_types.cpp
	engineRegisterTypesText     string
	engineRegisterTypesTemplate = template.Must(template.New("").Parse(engineRegisterTypesText))
	//go:embed templates/engine/register_package.cpp
	engineRegisterPackageText     string
	engineRegisterPackageTemplate = parseRegisterTemplate(engineRegisterPackageText)
	//go:embed templates/engine/compat.toml
	engineCompatText string
	//go:embed templates/engine/utility_functions.hpp
	engineUtilityFunctionsText string
)

// engineBuildVersion is the version of the project build cache's layout and
// of the engine names scanner. Bump it when either changes, to start over.
const engineBuildVersion = 2

// projectBuildCache returns res://.gd++proj/build, the project build cache.
func projectBuildCache(p Project) Path {
	return p.Root.Cd(ephemeralDepsDirName, projectBuildCacheDirName)
}

// engineBinDir returns the directory of the engine binaries that engine
// builds make.
func engineBinDir(p Project) Path {
	return projectBuildCache(p).Cd("godot", "bin")
}

// cleanProjectBuildCache deletes the project build cache, if any, saying so.
func cleanProjectBuildCache(p Project) {
	cache := projectBuildCache(p)
	if !cache.Exists() {
		return
	}
	t := LogTask("Cleaning %s...", cache.ToString())
	cache.Remove()
	t.Done()
}

// enginePlatform returns the engine's name for a build platform.
func enginePlatform(platform string) string {
	if platform == "linux" {
		return "linuxbsd"
	}
	return platform
}

// engineBinary returns the file name of the export template the engine
// builds for a platform and CPU architecture.
func engineBinary(platform, arch string, ship bool) string {
	name := fmt.Sprintf("godot.%s.template_%s.%s", enginePlatform(platform), map[bool]string{true: "release", false: "debug"}[ship], arch)
	if platform == "windows" {
		name += ".exe"
	}
	return name
}

// engineSconsArgs returns the engine build options for target. The custom
// module path is relative to the engine, where SCons runs.
func (o BuildOptions) engineSconsArgs(target string) []string {
	platform, arch, _ := strings.Cut(target, ".")
	args := []string{"platform=" + enginePlatform(platform), "arch=" + arch, "custom_modules=../modules"}
	if o.Ship {
		args = append(args, "target=template_release", "production=yes")
	} else {
		args = append(args, "target=template_debug")
	}
	return append(args, o.commonSconsArgs()...)
}

// enginePackage is a package as the engine module builds it.
type enginePackage struct {
	Package
	Classes, RuntimeClasses, Includes []string
}

// buildEngine builds export templates for targets from the engine called
// name, with every package of the project compiled in. The GD++ code is
// transpiled just like for GDExtension builds, and compiles against
// generated compat headers standing in for godot-cpp's.
func buildEngine(p Project, name string, o BuildOptions, targets []string) {
	engine := p.Caches[2]
	Assert(engine.Has(name), "Missing %s %s, run `gd++ fetch --engine=%s` to fix this.", engine.Desc, name, name)
	var pkgs []enginePackage
	var names []godotName
	owners := map[string]Package{}
	for _, pkg := range p.ListPackages() {
		// The bindings are only generated, not compiled, so the host's defaults do.
		_, gdpp, pkgNames := preparePackage(p, pkg, nil, o.docs(), true)
		names = append(names, pkgNames...)
		classes, runtime, includes := registeredClasses(pkg, gdpp)
		for _, class := range classes {
			if other, ok := owners[class]; ok {
				LogFatal("Class %s is declared in both %s and %s.", class, packageName(other.Root), packageName(pkg.Root))
			}
			owners[class] = pkg
		}
		pkgs = append(pkgs, enginePackage{pkg, classes, runtime, includes})
	}
	cache := prepareEngineBuild(p, name, pkgs)
	generateCompat(cache, names, loadEngineNames(cache))
	generateEngineModule(p, cache, pkgs)
	for _, target := range targets {
		ExecEnv("Building engine "+Styled(name, Bold)+" for "+o.describe(target, false)+"...", cache.Cd("godot"), engineEnv(cache), "scons", o.engineSconsArgs(target)...)
	}
}

// prepareEngineBuild returns the project build cache, with a copy of the
// engine called name. The cache's build.toml records the engine's name; if it
// changed, the cache is deleted and the engine copied again. The copy is only
// made once, since SCons builds inside it, along with installing the engine's
// dependencies. Asserts the engine is at least as
// new as the API specs of pkgs.
func prepareEngineBuild(p Project, name string, pkgs []enginePackage) Path {
	cache := projectBuildCache(p)
	state := cache.Cd("build.toml")
	stateText := encodeToml(struct {
		Engine  string `toml:"engine"`
		Version int    `toml:"version"`
	}{name, engineBuildVersion})
	if !state.IsFile() || state.ReadString() != stateText {
		// Cleaned first, so an interrupted copy isn't taken for a finished one.
		cleanProjectBuildCache(p)
		cache.CreateDirectory()
		s := Silence()
		t := LogTask("Copying engine %s...", name)
		p.Caches[2].GetPath(name).Copy(cache.Cd("godot"))
		t.Done()
		s.End()
		installEngineDeps(cache)
		state.WriteString(stateText)
	}

	major, minor := engineVersion(cache.Cd("godot", "version.py"))
	for _, pkg := range pkgs {
		specMajor, specMinor := apiSpecVersion(pkg.Package)
		Assert(specMajor < major || specMajor == major && specMinor <= minor,
			"Package %s uses the Godot %d.%d API, which engine %s (Godot %d.%d) lacks.", packageName(pkg.Root), specMajor, specMinor, name, major, minor)
	}
	if version := fmt.Sprintf("%d.%d", major, minor); version != p.GodotVersion {
		LogWarn("Engine %s is Godot %s, but the project is made for Godot %s.", name, version, p.GodotVersion)
	}
	return cache
}

// installEngineDeps runs the engine copy's misc/scripts/install_*.py, which
// download the libraries that some engine drivers need, e.g. Direct3D 12's.
func installEngineDeps(cache Path) {
	scripts := cache.Cd("godot", "misc", "scripts")
	if !scripts.IsDir() {
		return
	}
	python := ""
	for _, script := range scripts.Ls() {
		name := script.Name()
		if !strings.HasPrefix(name, "install_") || path.Ext(name) != ".py" {
			continue
		}
		if python == "" {
			python = findPython()
		}
		ExecEnv("Installing engine dependencies with "+name+"...", cache.Cd("godot"), engineEnv(cache), python, "misc/scripts/"+name)
	}
}

// findPython returns the name of the Python interpreter.
func findPython() string {
	for _, name := range []string{"python3", "python"} {
		if _, err := exec.LookPath(name); err == nil {
			return name
		}
	}
	LogFatal("Failed to find Python, see https://www.python.org to install it.")
	return ""
}

// engineEnv returns the environment of the engine's SCons and install
// scripts, which keeps everything they write in the project build cache: with
// $LOCALAPPDATA unset, the engine's dependencies go to and are looked up in
// the engine copy's bin/build_deps, instead of $LOCALAPPDATA/Godot/build_deps.
// Temporary files go to the cache's tmp directory, which it creates.
func engineEnv(cache Path) []string {
	tmp := cache.Cd("tmp")
	if !tmp.Exists() {
		tmp.CreateDirectory()
	}
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		key, _, _ := strings.Cut(kv, "=")
		return slices.ContainsFunc([]string{"LOCALAPPDATA", "TMPDIR", "TEMP", "TMP"}, func(k string) bool { return strings.EqualFold(k, key) })
	})
	for _, key := range []string{"TMPDIR", "TEMP", "TMP"} {
		env = append(env, key+"="+tmp.GetOsPath())
	}
	return env
}

var engineVersionPattern = regexp.MustCompile(`(?m)^(major|minor)\s*=\s*(\d+)`)

// engineVersion returns the Godot version from the engine's version.py.
func engineVersion(file Path) (major, minor int) {
	Assert(file.IsFile(), "Failed to find %s, the engine is not a Godot source tree.", file.ToString())
	versions := map[string]int{}
	for _, m := range engineVersionPattern.FindAllStringSubmatch(file.ReadString(), -1) {
		versions[m[1]], _ = strconv.Atoi(m[2])
	}
	Assert(versions["major"] > 0, "Failed to find the Godot version in %s.", file.ToString())
	return versions["major"], versions["minor"]
}

// engineName is a name the engine declares.
type engineName struct {
	Name   string   `toml:"name"`             // e.g. "OS"
	Cpp    string   `toml:"cpp"`              // qualified, e.g. "::core_bind::OS"
	Header string   `toml:"header"`           // relative to the engine root, e.g. "core/core_bind.h"
	Values []string `toml:"values,omitempty"` // an unscoped enum's values
	Scope  bool     `toml:"scope,omitempty"`  // whether it's a namespace
}

// engineNames is the engine names cache: the names declared in the global
// namespace, and the classes registered with GDCLASS, by their ClassDB name.
type engineNames struct {
	Version int          `toml:"version"`
	Globals []engineName `toml:"global"`
	Classes []engineName `toml:"class"`
}

// loadEngineNames returns the names the engine in the project build cache
// declares, from the cache's engine_names.toml, or else by scanning the
// headers of the engine parts export templates have. When a name is declared
// in several headers, the first one wins.
func loadEngineNames(cache Path) engineNames {
	file := cache.Cd("engine_names.toml")
	var names engineNames
	if file.IsFile() {
		decodeToml(file, &names)
		if names.Version == engineBuildVersion {
			return names
		}
	}
	s := Silence()
	t := LogTask("Scanning the engine...")
	names = engineNames{Version: engineBuildVersion}
	seen := map[string]bool{}
	var walk func(dir Path, rel string)
	walk = func(dir Path, rel string) {
		for _, child := range dir.Ls() {
			childRel := path.Join(rel, child.Name())
			switch {
			case child.IsDir():
				if !slices.Contains([]string{"tests", "thirdparty", "doc"}, child.Name()) {
					walk(child, childRel)
				}
			case path.Ext(childRel) == ".h":
				src := child.ReadString()
				for _, d := range scanCppDecls(src, "") {
					if !seen[d.name] {
						seen[d.name] = true
						names.Globals = append(names.Globals, engineName{Name: d.name, Cpp: "::" + d.name, Header: childRel, Values: d.values, Scope: d.namespace})
					}
				}
				for _, class := range scanGdclasses(src) {
					name := class[strings.LastIndex(class, ":")+1:]
					if !seen["class:"+name] {
						seen["class:"+name] = true
						names.Classes = append(names.Classes, engineName{Name: name, Cpp: "::" + class, Header: childRel})
					}
				}
			}
		}
	}
	for _, dir := range []string{"core", "scene", "servers", "modules"} {
		if d := cache.Cd("godot", dir); d.IsDir() {
			walk(d, dir)
		}
	}
	file.WriteString(encodeToml(names))
	t.Done()
	s.End()
	return names
}

// engineCompat is templates/engine/compat.toml.
type engineCompat struct {
	Headers map[string][]string `toml:"headers"`
	Renames map[string]string   `toml:"renames"`
}

// generateCompat writes the compat headers into the project build cache:
// one per godot-cpp header that declares names (names), or that
// compat.toml lists. Each includes the engine headers declaring the names,
// and brings them into namespace godot, where godot-cpp has them. Names under
// godot_cpp/classes are classes as ClassDB registers them, which aren't
// always the engine's global classes. Names the engine lacks are left out.
func generateCompat(cache Path, names []godotName, engine engineNames) {
	var compat engineCompat
	_, err := toml.Decode(engineCompatText, &compat)
	Check(err, "Failed to parse compat.toml")
	globals, classes := map[string]engineName{}, map[string]engineName{}
	for _, n := range engine.Globals {
		globals[n.Name] = n
	}
	for _, n := range engine.Classes {
		classes[n.Name] = n
	}
	headers := map[string][]string{}
	for header := range compat.Headers {
		headers[header] = nil
	}
	for _, n := range names {
		header := strings.Trim(n.Include, "<>")
		if !slices.Contains(headers[header], n.Name) {
			headers[header] = append(headers[header], n.Name)
		}
	}

	dir := cache.Cd("compat")
	written := map[string]bool{}
	for header, names := range headers {
		written[header] = true
		dir.Cd(header).CreateParentDirectory()
		if header == "godot_cpp/variant/utility_functions.hpp" {
			writeIfChanged(dir.Cd(header), engineUtilityFunctionsText)
			continue
		}
		includes := slices.Clone(compat.Headers[header])
		var usings []string
		slices.Sort(names)
		for _, name := range names {
			lookup := name
			if renamed, ok := compat.Renames[name]; ok {
				lookup = renamed
			}
			e, ok := classes[lookup]
			if !ok || !strings.HasPrefix(header, "godot_cpp/classes/") {
				e, ok = globals[lookup]
			}
			if !ok || e.Scope {
				continue
			}
			includes = append(includes, e.Header)
			if e.Cpp == "::"+name {
				usings = append(usings, "using "+e.Cpp+";")
			} else {
				usings = append(usings, "using "+name+" = "+e.Cpp+";")
			}
			for _, v := range e.Values {
				usings = append(usings, "using ::"+v+";")
			}
		}
		text := "// Generated by GD++, do not edit.\n\n#pragma once\n"
		if includes = uniqueSorted(includes, strings.Compare); len(includes) > 0 {
			text += "\n"
		}
		for _, include := range includes {
			text += "#include \"" + include + "\"\n"
		}
		if len(usings) > 0 {
			text += "\nnamespace godot {\n" + strings.Join(usings, "\n") + "\n} // namespace godot\n"
		}
		writeIfChanged(dir.Cd(header), text)
	}
	removeStale(dir, "", written)
}

// engineSource is a source file that the engine module compiles, and its
// object file, both relative to the module.
type engineSource struct {
	Source, Object string
}

// generateEngineModule writes the gdpp engine module into the project build
// cache. It compiles each package with its own SCons environment, into
// object files in the cache's obj directory, and a register_<i>.cpp per
// package, which registers its classes.
func generateEngineModule(p Project, cache Path, pkgs []enginePackage) {
	module := cache.Cd("modules", "gdpp")
	module.Cd("register_types.cpp").CreateParentDirectory()
	type scsubPackage struct {
		Name, CppStandard string
		IncludePaths      []string
		Sources           []engineSource
	}
	var packages []scsubPackage
	var indices []int
	written := map[string]bool{"SCsub": true, "config.py": true, "register_types.h": true, "register_types.cpp": true}
	for i, pkg := range pkgs {
		register := fmt.Sprintf("register_%d.cpp", i)
		written[register] = true
		writeTemplate(module.Cd(register), engineRegisterPackageTemplate, map[string]any{
			"Index":          i,
			"Classes":        pkg.Classes,
			"RuntimeClasses": pkg.RuntimeClasses,
			"Includes":       pkg.Includes,
		})
		obj := fmt.Sprintf("../../obj/%d/", i)
		root, gdpp := relPath(module, pkg.Root), relPath(module, pkg.BuildCache.Cd(gdppDirName))
		sources := []engineSource{{register, obj + register}}
		for _, rel := range cppSources(p, pkg.Package) {
			sources = append(sources, engineSource{root + "/" + rel, obj + "package/" + rel})
		}
		for _, rel := range gdppSources(p, pkg.Package) {
			sources = append(sources, engineSource{gdpp + "/" + rel, obj + "gdpp/" + rel})
		}
		packages = append(packages, scsubPackage{
			Name:         packageName(pkg.Root),
			CppStandard:  pkg.Config.CppStandard,
			IncludePaths: []string{relPath(module, cache.Cd("compat")), root, relPath(module, p.Root), gdpp},
			Sources:      sources,
		})
		indices = append(indices, i)
	}
	writeTemplate(module.Cd("SCsub"), engineSCsubTemplate, packages)
	writeIfChanged(module.Cd("config.py"), engineConfigText)
	writeIfChanged(module.Cd("register_types.h"), engineRegisterTypesHeaderText)
	writeTemplate(module.Cd("register_types.cpp"), engineRegisterTypesTemplate, indices)
	removeStale(module, "", written)
}
