package main

// CmdCheckIn moves deps from the project's ephemeral caches to the checked in
// ones, or back with --undo.
type CmdCheckIn struct {
	Bind      []string `arg:"--bind" placeholder:"NAME" help:"check in these Godot C++ bindings"`
	BindAll   bool     `arg:"--bind-all" help:"check in all Godot C++ bindings"`
	Spec      []string `arg:"--spec" placeholder:"NAME" help:"check in these Godot API specs"`
	SpecAll   bool     `arg:"--spec-all" help:"check in all Godot API specs"`
	Engine    []string `arg:"--engine" placeholder:"NAME" help:"check in these Godot engines"`
	EngineAll bool     `arg:"--engine-all" help:"check in all Godot engines"`
	All       bool     `arg:"-a,--all" help:"check in all dependencies of all kinds"`
	Undo      bool     `arg:"--undo" help:"make the dependencies ephemeral instead"`
}

func (c *CmdCheckIn) Run() {
	type kind struct {
		flag  string
		names []string
		all   bool
		cache ProjectDepCache
	}
	kinds := []kind{
		{"bind", c.Bind, c.BindAll || c.All, ProjectDepCache{}},
		{"spec", c.Spec, c.SpecAll || c.All, ProjectDepCache{}},
		{"engine", c.Engine, c.EngineAll || c.All, ProjectDepCache{}},
	}
	Assert(!c.All || countTrue(len(c.Bind) > 0, len(c.Spec) > 0, len(c.Engine) > 0, c.BindAll, c.SpecAll, c.EngineAll) == 0, "Invalid arguments: --all cannot be used with other --bind, --spec or --engine options.")
	chosen := false
	for _, k := range kinds {
		Assert(len(k.names) == 0 || !k.all, "Invalid arguments: --%s and --%s-all cannot be used together.", k.flag, k.flag)
		for _, name := range k.names {
			assertDepName(name)
		}
		chosen = chosen || len(k.names) > 0 || k.all
	}
	Assert(chosen, "Invalid arguments: a --bind, --spec, --engine or --all option is required.")

	p := LoadProject(Cwd())
	Cleanup(p.Cleanup)
	kinds[0].cache, kinds[1].cache, kinds[2].cache = p.BindingsCache, p.ApiSpecsCache, p.EnginesCache
	for i, k := range kinds {
		if k.all {
			kinds[i].names = k.cache.Ls()
		}
		for _, name := range k.names { // before moving anything, so a typo changes nothing
			k.cache.GetPath(name) // asserts it exists
		}
	}
	for _, k := range kinds {
		isDone, move, state := k.cache.IsCheckedIn, k.cache.CheckIn, "checked in"
		if c.Undo {
			isDone, move, state = k.cache.IsEphemeral, k.cache.MakeEphemeral, "ephemeral"
		}
		for _, name := range k.names {
			if isDone(name) {
				LogWarn("The %s %s is already %s.", k.cache.Desc, name, state)
			}
			move(name) // still needed, to delete a copy in the other cache
		}
	}
	p.Cleanup()
	LogInfo("Success!")
}
