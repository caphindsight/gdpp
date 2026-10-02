package main

import (
	"slices"
	"strings"
)

// CmdFetch downloads deps from a git repository into one of the project's dep
// caches, or lists the deps the repository has. The repository is huge, so it
// is cloned sparsely: only index/ and the chosen data/<kind>/<name> are
// checked out.
type CmdFetch struct {
	Index       bool     `arg:"--index" help:"list all dependencies in the repository"`
	IndexBind   bool     `arg:"--index-bind" help:"list the Godot C++ bindings in the repository"`
	IndexSpec   bool     `arg:"--index-spec" help:"list the Godot API specs in the repository"`
	IndexEngine bool     `arg:"--index-engine" help:"list the Godot engines in the repository"`
	Plain       bool     `arg:"--plain" help:"list only the names, one per line"`
	Bind        []string `arg:"--bind" placeholder:"NAME" help:"fetch these Godot C++ bindings"`
	BindAll     bool     `arg:"--bind-all" help:"fetch all Godot C++ bindings"`
	Spec        []string `arg:"--spec" placeholder:"NAME" help:"fetch these Godot API specs"`
	SpecAll     bool     `arg:"--spec-all" help:"fetch all Godot API specs"`
	Engine      []string `arg:"--engine" placeholder:"NAME" help:"fetch these Godot engines"`
	EngineAll   bool     `arg:"--engine-all" help:"fetch all Godot engines"`
	Missing     bool     `arg:"--missing" help:"fetch the bindings and specs packages use but the project lacks"`
	CheckIn     bool     `arg:"--checkin" help:"fetch into the checked in cache instead of the ephemeral one"`
	Url         string   `arg:"--url" default:"https://github.com/caphindsight/gdpp-dep.git" placeholder:"URL" help:"the repository to fetch from"`
	Branch      string   `arg:"--branch" default:"master" placeholder:"BRANCH" help:"the branch of the repository"`
}

// fetchKind is one kind of dep, with the arguments given for it.
type fetchKind struct {
	names     []string
	all, list bool
	cache     ProjectDepCache
}

func (c *CmdFetch) Run() {
	kinds := []fetchKind{ // in the order of depKinds
		{names: c.Bind, all: c.BindAll, list: c.Index || c.IndexBind},
		{names: c.Spec, all: c.SpecAll, list: c.Index || c.IndexSpec},
		{names: c.Engine, all: c.EngineAll, list: c.Index || c.IndexEngine},
	}
	index := c.validate(kinds)
	p := LoadProject(Cwd())
	defer p.Cleanup()
	for i := range kinds {
		kinds[i].cache = p.Caches[i]
	}
	if c.Missing && !c.addMissing(p, kinds) {
		LogInfo("No dependencies are missing.")
		return
	}

	repo := c.clone(p)
	if index {
		sep := ""
		for _, k := range kinds {
			if !k.list {
				continue
			}
			idx := parseDepIndex(repo.Cd("index", k.cache.Name).ReadString())
			if c.Plain {
				for i := len(idx.versions) - 1; i >= 0; i-- {
					PrintResult(idx.versions[i] + "\n")
				}
				continue
			}
			PrintResult(sep + Styled(k.cache.Plural+":", Bold) + "\n" + idx.Table(k.cache))
			sep = "\n"
		}
		return
	}
	c.fetch(repo, kinds)
	LogInfo("Success!")
}

// validate asserts the arguments make sense together, and returns whether
// they choose index mode.
func (c *CmdFetch) validate(kinds []fetchKind) bool {
	index, deps := false, false
	for i, k := range kinds {
		assertDepFlags(depKinds[i].Name, k.names, k.all)
		index = index || k.list
		deps = deps || len(k.names) > 0 || k.all
	}
	deps = deps || c.Missing
	Assert(!c.Index || countTrue(c.IndexBind, c.IndexSpec, c.IndexEngine) == 0, "Invalid arguments: --index cannot be used with --index-bind, --index-spec or --index-engine.")
	Assert(index || deps, "Invalid arguments: an --index, --bind, --spec, --engine or --missing option is required.")
	Assert(!index || !deps, "Invalid arguments: --index options cannot be used with --bind, --spec, --engine or --missing options.")
	Assert(!index || !c.CheckIn, "Invalid arguments: --checkin cannot be used with --index options.")
	Assert(!c.Plain || countTrue(c.IndexBind, c.IndexSpec, c.IndexEngine) == 1, "Invalid arguments: --plain requires exactly one of --index-bind, --index-spec or --index-engine.")
	return index
}

// addMissing adds the bindings and specs packages use but the caches lack to
// kinds, and returns whether there are any deps to fetch.
func (c *CmdFetch) addMissing(p Project, kinds []fetchKind) bool {
	for _, pkg := range p.ListPackages() {
		for i := range kinds {
			k := &kinds[i]
			if name, ok := pkg.Config.Dep(k.cache.Name); ok && !k.cache.Has(name) && !slices.Contains(k.names, name) {
				k.names = append(k.names, name)
			}
		}
	}
	return slices.ContainsFunc(kinds, func(k fetchKind) bool { return len(k.names) > 0 || k.all })
}

// clone makes a sparse clone of the dep repository in a temp dir, with only
// index/ checked out, and returns its directory.
func (c *CmdFetch) clone(p Project) Path {
	tmp := p.CreateTempDir()
	git("Cloning the repository...", tmp, "clone", "--depth", "1", "--filter=blob:none", "--no-checkout", "--branch", c.Branch, c.Url, "repo")
	repo := tmp.Cd("repo")
	git("Selecting the index...", repo, "sparse-checkout", "set", "index")
	git("Fetching the index...", repo, "checkout")
	return repo
}

// git runs git with args in dir, like Exec, but silenced: its output is
// rarely interesting, and is still shown if it fails.
func git(taskName string, dir Path, args ...string) {
	defer Silence().End()
	Exec(taskName, dir, "git", args...)
}

// fetch checks out the chosen deps in repo, then moves them into the cache.
// Deps already in the cache are overwritten if named, after confirming, and
// skipped if chosen by --<kind>-all.
func (c *CmdFetch) fetch(repo Path, kinds []fetchKind) {
	type dep struct {
		k    fetchKind
		name string
	}
	var deps []dep
	var paths []string
	for _, k := range kinds {
		idx := parseDepIndex(repo.Cd("index", k.cache.Name).ReadString())
		names := k.names
		if k.all {
			names = idx.versions
		}
		for _, name := range names {
			name = idx.resolve(k.cache.Desc, name)
			path := "data/" + k.cache.Name + "/" + name
			if slices.Contains(paths, path) {
				continue // e.g. both "latest" and the version it names
			}
			if k.cache.Has(name) {
				if k.all {
					LogWarn("Skipping %s %s, since it is already in the cache.", k.cache.Desc, name)
					continue
				}
				Confirm("Overwrite %s %s in the cache?", k.cache.Desc, name)
			}
			deps, paths = append(deps, dep{k, name}), append(paths, path)
		}
	}
	if len(deps) == 0 {
		return
	}
	git("Fetching dependencies...", repo, append([]string{"sparse-checkout", "add"}, paths...)...)

	for _, d := range deps {
		dir := d.k.cache.EphemeralDir
		if c.CheckIn {
			dir = d.k.cache.CheckedInDir
		}
		for d.k.cache.Has(d.name) { // it may be in both caches
			d.k.cache.Remove(d.name)
		}
		to := dir.Cd(d.name)
		to.CreateParentDirectory()
		repo.Cd("data", d.k.cache.Name, d.name).Move(to)
	}
}

// depIndex is a parsed index file from the dep repository: the versions it
// declares, sorted by compareDepNames, and tags naming versions, e.g.
// latest=4.7.2-stable.
type depIndex struct {
	versions []string
	tags     map[string]string
}

func parseDepIndex(text string) depIndex {
	idx := depIndex{tags: map[string]string{}}
	for _, line := range SplitLines(text) {
		line = strings.TrimSpace(line)
		if tag, version, ok := strings.Cut(line, "="); ok {
			idx.tags[tag] = version
		} else if line != "" {
			idx.versions = append(idx.versions, line)
		}
	}
	slices.SortFunc(idx.versions, compareDepNames)
	return idx
}

// resolve returns the version called name, or named by the tag name,
// asserting it exists. desc is the kind of dep, for messages.
func (idx depIndex) resolve(desc, name string) string {
	if version, ok := idx.tags[name]; ok {
		name = version
	}
	Assert(slices.Contains(idx.versions, name), "The %s %s is not in the repository.", desc, name)
	return name
}

// Table lists the versions newest first, one indented row each, in aligned
// columns: the version, the tags naming it, and whether cache has it.
func (idx depIndex) Table(cache ProjectDepCache) string {
	if len(idx.versions) == 0 {
		return "  none\n"
	}
	var rows [][]string
	for i := len(idx.versions) - 1; i >= 0; i-- {
		version := idx.versions[i]
		var tags []string
		for tag, v := range idx.tags {
			if v == version {
				tags = append(tags, tag)
			}
		}
		slices.Sort(tags)
		rows = append(rows, []string{version, Styled(strings.Join(tags, ", "), Cyan), cache.Status(version)})
	}
	return AlignColumns(rows, "  ")
}
