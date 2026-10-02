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
// The caches need none: Godot skips hidden and .gdignore'd directories.
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
// filters appended if want, or removed otherwise. value is returned unchanged
// if its filters are already right.
func editExcludeFilter(value string, want bool) string {
	var old, filters []string
	for _, f := range strings.Split(value, ",") {
		if f = strings.TrimSpace(f); f != "" {
			old = append(old, f)
			if !slices.Contains(presetExcludes, f) {
				filters = append(filters, f)
			}
		}
	}
	if want {
		filters = append(filters, presetExcludes...)
	}
	if slices.Equal(filters, old) {
		return value
	}
	return strings.Join(filters, ", ")
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
