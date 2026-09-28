package main

import (
	"slices"
	"strings"
)

// CmdVendor copies a dep between a directory or archive on disk and one of
// the project's dep caches, which always store deps as directories.
type CmdVendor struct {
	Bind    string `arg:"--bind" placeholder:"NAME" help:"vendor these Godot C++ bindings"`
	Spec    string `arg:"--spec" placeholder:"NAME" help:"vendor this Godot API spec"`
	Engine  string `arg:"--engine" placeholder:"NAME" help:"vendor this Godot engine"`
	From    string `arg:"--from" placeholder:"PATH" help:"copy into the cache from this path"`
	To      string `arg:"--to" placeholder:"PATH" help:"copy out of the cache to this path"`
	CheckIn bool   `arg:"--checkin" help:"with --from, use the checked in cache instead of the ephemeral one"`
	Tar     bool   `arg:"--tar" help:"the path is a .tar.gz file instead of a directory"`
	Zip     bool   `arg:"--zip" help:"the path is a .zip file instead of a directory"`
}

func (c *CmdVendor) Run() {
	name := c.validate()
	p := LoadProject(Cwd())
	defer p.Cleanup()
	cache := c.depCache(p)
	c.confirmExtension()

	if c.From != "" {
		c.vendorFrom(p, cache, name)
	} else {
		c.vendorTo(cache, name)
	}

	LogInfo("Success!")
}

// validate asserts the arguments make sense together, and returns the dep name.
func (c *CmdVendor) validate() string {
	Assert(countTrue(c.Bind != "", c.Spec != "", c.Engine != "") == 1, "Invalid arguments: exactly one of --bind, --spec and --engine is required.")
	Assert(countTrue(c.From != "", c.To != "") == 1, "Invalid arguments: exactly one of --from and --to is required.")
	Assert(!c.Tar || !c.Zip, "Invalid arguments: --tar and --zip cannot be used together.")
	Assert(!c.CheckIn || c.From != "", "Invalid arguments: --checkin can only be used with --from.")
	name := c.Bind + c.Spec + c.Engine // only one is set
	assertDepName(name)
	return name
}

// depCache returns the project's cache for the kind of dep chosen by the
// arguments.
func (c *CmdVendor) depCache(p Project) ProjectDepCache {
	return p.Caches[slices.IndexFunc([]string{c.Bind, c.Spec, c.Engine}, func(s string) bool { return s != "" })]
}

// confirmExtension asks the user to confirm an archive path whose extension
// doesn't match --tar or --zip.
func (c *CmdVendor) confirmExtension() {
	if !c.Tar && !c.Zip {
		return
	}
	exts := []string{".tar.gz", ".tgz"}
	if c.Zip {
		exts = []string{".zip"}
	}
	file := c.From + c.To // only one is set
	if !slices.ContainsFunc(exts, func(ext string) bool { return strings.HasSuffix(strings.ToLower(file), ext) }) {
		Confirm("Path %s doesn't end with %s, use it anyway?", ParsePath(file).ToString(), strings.Join(exts, " or "))
	}
}

// vendorFrom copies --from into the ephemeral cache, or the checked in one
// with --checkin, unpacking archives into a temp dir first.
func (c *CmdVendor) vendorFrom(p Project, cache ProjectDepCache, name string) {
	from := ParsePath(c.From)
	src := from
	if c.Tar || c.Zip {
		Assert(src.IsFile(), "Path %s is not a file.", src.ToString())
		src = p.CreateTempDir()
		c.unpack(from, src)
	} else {
		Assert(src.IsDir(), "Path %s is not a directory.", src.ToString())
	}

	dir, move := cache.EphemeralDir, cache.MakeEphemeral
	if c.CheckIn {
		dir, move = cache.CheckedInDir, cache.CheckIn
	}
	if cache.Has(name) {
		Confirm("Overwrite %s %s in the cache?", cache.Desc, name)
		move(name) // so the dep is overwritten where --checkin wants it
	}
	to := dir.Cd(name)
	to.CreateParentDirectory()
	src.Sync(to)
}

// vendorTo copies the dep from the cache to --to, packing it into an archive
// with --tar or --zip.
func (c *CmdVendor) vendorTo(cache ProjectDepCache, name string) {
	from, to := cache.GetPath(name), ParsePath(c.To)
	if to.Exists() {
		Confirm("Overwrite %s?", to.ToString())
	}
	to.CreateParentDirectory()
	if c.Tar || c.Zip {
		to.RemoveIfExists()
		c.pack(from, to)
	} else {
		from.Sync(to)
	}
}

// pack archives the directory src into dst, as a tarball or zip per the arguments.
func (c *CmdVendor) pack(src, dst Path) {
	if c.Tar {
		src.Tar(dst)
	} else {
		src.Zip(dst)
	}
}

// unpack extracts the archive src into dst, as a tarball or zip per the arguments.
func (c *CmdVendor) unpack(src, dst Path) {
	if c.Tar {
		src.Untar(dst)
	} else {
		src.Unzip(dst)
	}
}
