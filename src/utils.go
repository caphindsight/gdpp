// utils.go: small helpers shared between commands and types.

package main

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"gd++/trans"
)

// chosenSyntax returns the GD++ syntax that the --syntax and --nightly flags
// choose, or nil if neither is given.
func chosenSyntax(syntax *int, nightly bool) *int {
	Assert(syntax == nil || !nightly, "Invalid arguments: --syntax and --nightly cannot be used together.")
	if nightly {
		s := trans.NightlySyntax
		return &s
	}
	return syntax
}

// syntaxVersions lists the GD++ syntax versions that this gd++ supports, one
// per line, marking the nightly and the latest stable one.
func syntaxVersions() string {
	var text strings.Builder
	for _, s := range trans.Syntaxes() {
		text.WriteString(strconv.Itoa(s))
		switch s {
		case trans.NightlySyntax:
			text.WriteString(" (nightly, not for production)")
		case trans.LatestSyntax:
			text.WriteString(" (latest stable)")
		}
		text.WriteString("\n")
	}
	return text.String()
}

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

// onOff returns "on" or "off".
func onOff(b bool) string {
	return map[bool]string{true: "on", false: "off"}[b]
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
