// presets.go: the exclude filters GD++ keeps in the project's export presets,
// so its sources and config files stay out of exported games.

package main

import (
	"regexp"
	"slices"
	"strings"
)

// presetExcludes are GD++'s exclude filters. Godot matches a filter against
// the whole res:// path, with or without "res://", and its * matches "/" too.
// The caches need none: Godot skips directories that are hidden or have a
// .gdignore file. The config files do: on Windows, Godot only skips files
// with the hidden attribute, not those whose name starts with ".".
var presetExcludes = func() []string {
	var filters []string
	for _, ext := range slices.Concat(gdppExtensions, cppExtensions) {
		filters = append(filters, "*"+ext)
	}
	return append(filters, projectConfigFileName, packageFileName, "*/"+packageFileName)
}()

// export_presets.cfg has one such line in each [preset.N] section.
var excludeFilterPattern = regexp.MustCompile(`^exclude_filter="((?:[^"\\]|\\.)*)"$`)

// editExcludeFilter returns the comma-separated filter list value with GD++'s
// missing filters appended if want, or its filters cut out otherwise. Other
// filters are kept as they are, and value is returned unchanged if its
// filters are already right.
func editExcludeFilter(value string, want bool) string {
	isOurs := func(f string) bool { return slices.Contains(presetExcludes, strings.TrimSpace(f)) }
	entries := strings.Split(value, ",")
	if !want {
		if kept := slices.DeleteFunc(slices.Clone(entries), isOurs); len(kept) < len(entries) {
			return strings.TrimSpace(strings.Join(kept, ","))
		}
		return value
	}
	var missing []string
	for _, f := range presetExcludes {
		if !slices.ContainsFunc(entries, func(e string) bool { return strings.TrimSpace(e) == f }) {
			missing = append(missing, f)
		}
	}
	if len(missing) == 0 {
		return value
	}
	if value = strings.TrimRight(value, ", \t"); value != "" {
		value += ", "
	}
	return value + strings.Join(missing, ", ")
}

// SyncExportPresets writes GD++'s exclude filters into every preset in the
// project's res://export_presets.cfg if its config enables them, or removes
// them otherwise. build and fix call it. Returns whether the file changed.
func SyncExportPresets(p Project) bool {
	file := p.Root.Cd(exportPresetsFileName)
	if !file.IsFile() {
		return false
	}
	lines := SplitLines(file.ReadString())
	changed := false
	for i, line := range lines {
		if m := excludeFilterPattern.FindStringSubmatch(line); m != nil {
			if value := editExcludeFilter(m[1], p.Config.Presets); value != m[1] {
				lines[i] = `exclude_filter="` + value + `"`
				changed = true
			}
		}
	}
	if !changed {
		return false
	}
	file.WriteString(MergeLines(lines))
	LogInfo("Updated %s.", file.ToString())
	return true
}
