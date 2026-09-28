// project_dep_cache.go: ProjectDepCache, a project's cache of versioned
// dependencies ("deps"), such as the Godot C++ bindings.

package main

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

// ProjectDepCache keeps deps as directories named after the dep, e.g.
// "10.0.0-stable", in one of two cache directories: a checked in one, meant
// for version control, or an ephemeral one, which isn't. Otherwise the two
// are treated the same.
type ProjectDepCache struct {
	CheckedInDir Path
	EphemeralDir Path
}

// newProjectDepCache returns the cache stored under res://_gd++proj/<dir>
// (checked in) and res://.gd++proj/<dir> (ephemeral).
func newProjectDepCache(root Path, dir string) ProjectDepCache {
	return ProjectDepCache{
		CheckedInDir: root.Cd(checkedInDepsDirName, dir),
		EphemeralDir: root.Cd(ephemeralDepsDirName, dir),
	}
}

// Ls returns the names of all deps, checked in or ephemeral, sorted by
// compareDepNames.
func (c ProjectDepCache) Ls() []string {
	seen := map[string]bool{}
	for _, dir := range []Path{c.CheckedInDir, c.EphemeralDir} {
		if !dir.IsDir() {
			continue
		}
		for _, dep := range dir.Ls() {
			if dep.IsDir() {
				seen[dep.Name()] = true
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	slices.SortFunc(names, compareDepNames)
	return names
}

// compareDepNames orders dep names like versions: names are split into parts
// on '.', '-' and '_', and parts are compared in order. Two integer parts
// compare as integers, an integer part is greater than a non-integer one, and
// other parts compare as strings. If all parts match, a name with fewer parts
// comes first. Names that still tie compare as plain strings.
func compareDepNames(a, b string) int {
	isSep := func(r rune) bool { return r == '.' || r == '-' || r == '_' }
	aParts, bParts := strings.FieldsFunc(a, isSep), strings.FieldsFunc(b, isSep)
	for i := 0; i < len(aParts) && i < len(bParts); i++ {
		aInt, aErr := strconv.Atoi(aParts[i])
		bInt, bErr := strconv.Atoi(bParts[i])
		var c int
		switch {
		case aErr == nil && bErr == nil:
			c = cmp.Compare(aInt, bInt)
		case aErr == nil:
			c = 1
		case bErr == nil:
			c = -1
		default:
			c = strings.Compare(aParts[i], bParts[i])
		}
		if c != 0 {
			return c
		}
	}
	if c := cmp.Compare(len(aParts), len(bParts)); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

// Has reports whether the dep exists, checked in or ephemeral.
func (c ProjectDepCache) Has(name string) bool {
	return c.IsCheckedIn(name) || c.IsEphemeral(name)
}

// IsCheckedIn reports whether the dep exists in the checked in directory.
func (c ProjectDepCache) IsCheckedIn(name string) bool {
	return c.CheckedInDir.Cd(name).IsDir()
}

// IsEphemeral reports whether the dep exists in the ephemeral directory.
func (c ProjectDepCache) IsEphemeral(name string) bool {
	return c.EphemeralDir.Cd(name).IsDir()
}

// GetPath returns the directory of the dep, asserting it exists.
func (c ProjectDepCache) GetPath(name string) Path {
	if c.IsCheckedIn(name) {
		return c.CheckedInDir.Cd(name)
	}
	Assert(c.IsEphemeral(name), "Dep %s does not exist in %s or %s.", name, c.CheckedInDir.ToString(), c.EphemeralDir.ToString())
	return c.EphemeralDir.Cd(name)
}

// CheckIn moves the dep to the checked in directory, asserting it exists.
// Does nothing if it's already checked in.
func (c ProjectDepCache) CheckIn(name string) {
	if src := c.GetPath(name); !c.IsCheckedIn(name) {
		moveDep(src, c.CheckedInDir.Cd(name))
	}
}

// MakeEphemeral moves the dep to the ephemeral directory, asserting it
// exists. Does nothing if it's already ephemeral.
func (c ProjectDepCache) MakeEphemeral(name string) {
	if src := c.GetPath(name); !c.IsEphemeral(name) {
		moveDep(src, c.EphemeralDir.Cd(name))
	}
}

// Remove deletes the dep, asserting it exists.
func (c ProjectDepCache) Remove(name string) {
	c.GetPath(name).Remove()
}

// moveDep moves a dep directory to dst, creating dst's parents as needed.
func moveDep(src, dst Path) {
	if parent := dst.BaseDir(); !parent.Exists() {
		parent.CreateDirectory()
	}
	src.Move(dst)
}
