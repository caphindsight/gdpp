// engine.go: engine builds, which compile all packages of a project into the
// Godot engine as a module, building export templates, in the project build
// cache res://.gd++proj/build.

package main

import (
	"cmp"
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
)

// engineBuildVersion is the version of the project build cache's layout, of
// the engine names scanner and of patchEngine. Bump it when any of them
// changes, to start over.
const engineBuildVersion = 5

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
// transpiled into the project build cache, against the names the engine
// declares. Package build caches aren't used.
func buildEngine(p Project, name string, o BuildOptions, targets []string) {
	engine := p.Caches[2]
	Assert(engine.Has(name), "Missing %s %s, run `gd++ fetch --engine=%s` to fix this.", engine.Desc, name, name)
	cache := prepareEngineBuild(p, name)
	names := loadEngineNames(cache)
	var pkgs []enginePackage
	owners := map[string]Package{}
	for i, pkg := range p.ListPackages() {
		files := listGdppFiles(p, pkg)
		var gdpp []gdppClass
		if dir := engineGdppDir(cache, i); len(files) > 0 {
			gdpp = transpilePackage(pkg, dir, files, packageEngineNames(names, files), false)
		} else if dir.Exists() {
			dir.Remove()
		}
		classes, runtime, includes := registeredClasses(pkg, gdpp)
		for _, class := range classes {
			if other, ok := owners[class]; ok {
				LogFatal("Class %s is declared in both %s and %s.", class, packageName(other.Root), packageName(pkg.Root))
			}
			owners[class] = pkg
		}
		pkgs = append(pkgs, enginePackage{pkg, classes, runtime, includes})
	}
	generateEngineModule(p, cache, pkgs)
	for _, target := range targets {
		ExecEnv("Building engine "+Styled(name, Bold)+" for "+o.describe(target, false)+"...", cache.Cd("godot"), engineEnv(cache), "scons", o.engineSconsArgs(target)...)
	}
}

// engineGdppDir returns the directory of the C++ generated from the GD++
// files of the i-th package, in the project build cache.
func engineGdppDir(cache Path, i int) Path {
	return cache.Cd(gdppDirName, strconv.Itoa(i))
}

// packageEngineNames returns the engine names without those that files
// declare: unlike godot-cpp's, the engine's global namespace is crowded, so
// GD++ names may shadow its names.
func packageEngineNames(names []godotName, files []gdppFile) []godotName {
	declared := map[string]bool{}
	for _, f := range files {
		for _, d := range f.Decls {
			declared[d.Name] = true
		}
	}
	return slices.DeleteFunc(slices.Clone(names), func(n godotName) bool { return declared[n.Name] })
}

// engineBuildState is the project build cache's build.toml.
type engineBuildState struct {
	Engine  string `toml:"engine"`
	Version int    `toml:"version"`
}

// prepareEngineBuild returns the project build cache, with a copy of the
// engine called name. The cache's build.toml records the engine's name and
// engineBuildVersion; if either changed, the cache is deleted and the engine
// copied again. The copy is only made once, since SCons builds inside it,
// along with installing the engine's dependencies.
func prepareEngineBuild(p Project, name string) Path {
	cache := projectBuildCache(p)
	state := cache.Cd("build.toml")
	stateText := encodeToml(engineBuildState{name, engineBuildVersion})
	if !state.IsFile() || state.ReadString() != stateText {
		// Cleaned first, so an interrupted copy isn't taken for a finished one.
		cleanProjectBuildCache(p)
		cache.CreateDirectory()
		s := Silence()
		t := LogTask("Copying engine %s...", name)
		p.Caches[2].GetPath(name).Copy(cache.Cd("godot"))
		patchEngine(cache.Cd("godot"))
		t.Done()
		s.End()
		installEngineDeps(cache)
		state.WriteString(stateText)
	}

	major, minor := engineVersion(cache.Cd("godot", "version.py"))
	if version := fmt.Sprintf("%d.%d", major, minor); version != p.GodotVersion {
		LogWarn("Engine %s is Godot %s, but the project is made for Godot %s.", name, version, p.GodotVersion)
	}
	return cache
}

// patchEngine patches the engine copy godot, so that Godot's virtuals call the
// overrides of GD++ classes, as they call those of GDExtension classes: the
// engine only calls them for scripts and GDExtension classes.
func patchEngine(godot Path) {
	for _, file := range []struct {
		path  Path
		patch func(string) (string, bool)
	}{
		{godot.Cd("core", "object", "object.h"), patchObjectHeader},
		{godot.Cd("core", "object", "make_virtuals.py"), patchMakeVirtuals},
	} {
		text, ok := file.patch(file.path.ReadString())
		Assert(ok, "Failed to patch %s, GD++ doesn't support this engine version yet.", file.path.ToString())
		file.path.WriteString(text)
	}
}

// patchObjectHeader adds to Object the virtuals that GD++ classes override to
// have their overrides called. Reports whether it found where to add them.
func patchObjectHeader(src string) (string, bool) {
	return insertAfter(src, "_ALWAYS_INLINE_ const ObjectGDExtension *_get_extension() const { return _extension; }",
		"\t// Added by GD++, for GD++ classes: whether they override the virtual p_name, and a ptrcall of their override.",
		"\tvirtual bool _gdpp_has_virtual(const StringName &p_name) const { return false; }",
		"\tvirtual bool _gdpp_call_virtual(const StringName &p_name, const void **p_args, void *r_ret) const { return false; }")
}

// patchMakeVirtuals makes the GDVIRTUAL macros call the overrides of GD++
// classes after those of scripts, encoding the arguments as for GDExtension
// classes. Reports whether it found where to add the calls.
func patchMakeVirtuals(src string) (string, bool) {
	// The script's own lines that encode the arguments and decode the result, which it removes when not needed.
	var args, retDef, ret string
	for _, line := range strings.Split(src, "\n") {
		switch strings.TrimSpace(line) {
		case `$CALLPTRARGS\\`:
			args = cmp.Or(args, line)
		case `$CALLPTRRETDEF\\`:
			retDef = cmp.Or(retDef, line)
		case `$CALLPTRRET\\`:
			ret = cmp.Or(ret, line)
		}
	}
	if args == "" || retDef == "" || ret == "" {
		return "", false
	}
	sn := `static const StringName _gdpp_sn = StringName(#m_name, true);\\`
	src, ok1 := insertAfter(src, `_call($CALLARGS) $CONST {`,
		"\t\t"+sn,
		`		if (_gdpp_has_virtual(_gdpp_sn) && !(((Object *)(this))->get_script_instance() && ((Object *)(this))->get_script_instance()->has_method(_gdpp_sn))) {\\`,
		args,
		retDef,
		`			_gdpp_call_virtual(_gdpp_sn, (const void **)($CALLPTRARGPASS), $CALLPTRRETPASS);\\`,
		ret,
		`			return true;\\`,
		`		}\\`)
	src, ok2 := insertAfter(src, `_overridden() const {`,
		"\t\t"+sn,
		`		if (_gdpp_has_virtual(_gdpp_sn)) {\\`,
		`			return true;\\`,
		`		}\\`)
	return src, ok1 && ok2
}

// insertAfter inserts lines after the only line of src that contains anchor.
// Reports whether there was exactly one such line.
func insertAfter(src, anchor string, lines ...string) (string, bool) {
	if strings.Count(src, anchor) != 1 {
		return src, false
	}
	i := strings.Index(src, anchor)
	end := i + strings.Index(src[i:], "\n") + 1
	if end == i {
		return src, false
	}
	return src[:end] + strings.Join(lines, "\n") + "\n" + src[end:], true
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
		s := Silence()
		ExecEnv("Installing engine dependencies with "+name+"...", cache.Cd("godot"), engineEnv(cache), python, "misc/scripts/"+name)
		s.End()
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

// loadEngineNames returns the names that the engine in the project build
// cache declares, for GD++ code, from the cache's engine_names.toml, or else
// by scanning the headers of the engine parts export templates have: the
// classes registered with GDCLASS, by their ClassDB name, and the other names
// in the global namespace. When a name is declared in several headers, the
// first one wins, and classes win over other names.
func loadEngineNames(cache Path) []godotName {
	file := cache.Cd("engine_names.toml")
	if names, ok := loadNamesCache(file, engineBuildVersion); ok {
		return names
	}
	s := Silence()
	t := LogTask("Scanning the engine...")
	var classes, globals []godotName
	bases, seen := map[string]string{}, map[string]bool{}
	for _, dir := range []string{"core", "scene", "servers", "modules"} {
		walkHeaders(cache.Cd("godot", dir), func(rel, src string) {
			include := `"` + path.Join(dir, rel) + `"`
			src = stripDebugOnly(src)
			for _, d := range scanCppDecls(src, "") {
				if !seen[d.name] {
					seen[d.name] = true
					globals = append(globals, godotName{Name: d.name, Include: include})
				}
			}
			for _, class := range scanGdclasses(src) {
				name := class.name[strings.LastIndex(class.name, ":")+1:]
				if name == "ClassDB" {
					name = "ClassDBSingleton" // Like godot-cpp: ClassDB is the engine's static class.
				}
				if _, ok := bases[name]; !ok {
					bases[name] = class.base
					n := godotName{Name: name, Include: include}
					if class.name != name {
						n.Cpp = "::" + class.name
					}
					classes = append(classes, n)
				}
			}
		}, "tests", "thirdparty", "doc")
	}
	names := append(classes, slices.DeleteFunc(globals, func(n godotName) bool { _, ok := bases[n.Name]; return ok })...)
	names = withKinds(names, bases)
	file.WriteString(encodeToml(godotNamesCache{engineBuildVersion, names}))
	t.Done()
	s.End()
	return names
}

var (
	debugOnlyPattern = regexp.MustCompile(`^#\s*(ifdef\s+|if\s+defined\s*\(?\s*)(DEBUG_ENABLED|DEBUG_METHODS_ENABLED|TOOLS_ENABLED)\s*\)?\s*(//.*)?$`)
	directivePattern = regexp.MustCompile(`^#\s*(if|ifdef|ifndef|else|elif|endif)\b`)
)

// stripDebugOnly returns the header src without the code that only debug
// or editor builds compile, e.g. #ifdef DEBUG_ENABLED blocks, whose names
// release export templates lack.
func stripDebugOnly(src string) string {
	var out []string
	depth := 0 // the nesting depth inside a stripped block, 0 when not in one
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if depth == 0 {
			if debugOnlyPattern.MatchString(trimmed) {
				depth = 1
			}
			out = append(out, line)
			continue
		}
		switch m := directivePattern.FindStringSubmatch(trimmed); {
		case m == nil:
			continue
		case strings.HasPrefix(m[1], "if"):
			depth++
		case m[1] == "endif":
			depth--
		case depth == 1: // #else or #elif
			depth = 0
		}
		if depth == 0 {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
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
		root, gdpp := relPath(module, pkg.Root), relPath(module, engineGdppDir(cache, i))
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
			IncludePaths: []string{root, relPath(module, p.Root), gdpp},
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
