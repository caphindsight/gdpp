// utils.go: small helpers shared between commands and types.

package main

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// countTrue returns how many of bs are true.
func countTrue(bs ...bool) int {
	n := 0
	for _, b := range bs {
		if b {
			n++
		}
	}
	return n
}

// assertDepName asserts name can be used as a dep's directory name.
func assertDepName(name string) {
	Assert(name == filepath.Base(name) && name != "." && name != "..", "Invalid arguments: %q is not a valid dependency name.", name)
}

// assertDepFlags asserts the --<flag> names and --<flag>-all options of a
// kind of dep make sense together.
func assertDepFlags(flag string, names []string, all bool) {
	Assert(len(names) == 0 || !all, "Invalid arguments: --%s and --%s-all cannot be used together.", flag, flag)
	for _, name := range names {
		assertDepName(name)
	}
}

// uniqueSorted returns a copy of names sorted by cmp, without duplicates.
func uniqueSorted(names []string, cmp func(a, b string) int) []string {
	names = slices.Clone(names)
	slices.SortFunc(names, cmp)
	return slices.Compact(names)
}

// decodeToml parses the TOML file into v, asserting it has no unknown keys.
func decodeToml(file Path, v any) toml.MetaData {
	meta, err := toml.Decode(file.ReadString(), v)
	Check(err, "Failed to parse %s", file.ToString())
	if unknown := meta.Undecoded(); len(unknown) > 0 {
		LogFatal("Unknown key %s in %s.", unknown[0], file.ToString())
	}
	return meta
}

// encodeToml returns v in TOML format.
func encodeToml(v any) string {
	var b strings.Builder
	Check(toml.NewEncoder(&b).Encode(v), "Failed to encode the config")
	return b.String()
}
