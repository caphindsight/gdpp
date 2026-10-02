// presets_test.go: tests for presets.go.

package main

import (
	"maps"
	"strings"
	"testing"
)

// testExportPresets returns an export_presets.cfg with two presets, both with
// the exclude filter value.
func testExportPresets(value string) string {
	preset := func(i, name string) string {
		return "[preset." + i + "]\n\n" +
			"name=\"" + name + "\"\n" +
			"export_filter=\"all_resources\"\n" +
			"include_filter=\"*\"\n" +
			"exclude_filter=\"" + value + "\"\n" +
			"export_path=\"\"\n\n" +
			"[preset." + i + ".options]\n\n" +
			"custom_template/debug=\"\"\n"
	}
	return preset("0", "Linux") + "\n" + preset("1", "Windows Desktop")
}

func TestEditExcludeFilter(t *testing.T) {
	all := strings.Join(presetExcludes, ", ")
	cases := []struct {
		value string
		want  bool
		out   string
	}{
		{"", true, all},
		{"", false, ""},
		{"*.txt,docs/*", true, "*.txt,docs/*, " + all},
		{"*.txt,docs/*, ", true, "*.txt,docs/*, " + all},
		{"*.txt,docs/*", false, "*.txt,docs/*"},                                                                         // unchanged, with its formatting
		{"*.txt,  " + all + " ", true, "*.txt,  " + all + " "},                                                          // unchanged, with its formatting
		{presetExcludes[0] + ", *.txt", true, presetExcludes[0] + ", *.txt, " + strings.Join(presetExcludes[1:], ", ")}, // only the missing ones are appended
		{all + ", *.txt", true, all + ", *.txt"},                                                                        // GD++'s filters stay where they are
		{"*.txt, " + all, false, "*.txt"},
		{"*.cpp, *.txt,docs/*", false, "*.txt,docs/*"},
		{"*.txt,, *.cpp,  docs/* ", false, "*.txt,,  docs/*"}, // others keep their formatting
		{"*.cpp,*.txt,, gd++pkg.toml", false, "*.txt,"},
	}
	for _, tc := range cases {
		if got := editExcludeFilter(tc.value, tc.want); got != tc.out {
			t.Errorf("editExcludeFilter(%q, %v) = %q, want %q", tc.value, tc.want, got, tc.out)
		}
	}
}

func TestSyncExportPresets(t *testing.T) {
	all := strings.Join(presetExcludes, ", ")
	tree := maps.Clone(testProjectTree)
	tree["/games/my_game/"+exportPresetsFileName] = testExportPresets(`a\"b`)
	m := withMemFS(t, "/games/my_game", tree)
	withQuiet(t, false)
	withTTY(t, false)
	root := NewPath("/games/my_game")
	file := "/games/my_game/" + exportPresetsFileName
	for _, tc := range []struct {
		want    bool
		out     string
		changed bool
		filter  string
	}{
		{true, "[-] Updated res://export_presets.cfg.\n", true, `a\"b, ` + all},
		{true, "", false, `a\"b, ` + all},
		{false, "[-] Updated res://export_presets.cfg.\n", true, `a\"b`},
		{false, "", false, `a\"b`},
	} {
		var changed bool
		out := captureStderr(t, func() { changed = SyncExportPresets(Project{Root: root, Config: ProjectConfig{Presets: tc.want}}) })
		if out != tc.out || changed != tc.changed {
			t.Errorf("SyncExportPresets(%v): output = %q, changed = %v, want %q, %v", tc.want, out, changed, tc.out, tc.changed)
		}
		if got, want := m.tree()[file], testExportPresets(tc.filter); got != want {
			t.Errorf("SyncExportPresets(%v): file = %q, want %q", tc.want, got, want)
		}
	}
}

func TestSyncExportPresetsCRLF(t *testing.T) {
	// Line endings alone are no reason to rewrite the file.
	tree := maps.Clone(testProjectTree)
	text := strings.ReplaceAll(testExportPresets(strings.Join(presetExcludes, ", ")), "\n", "\r\n")
	tree["/games/my_game/"+exportPresetsFileName] = text
	m := withMemFS(t, "/games/my_game", tree)
	if SyncExportPresets(Project{Root: NewPath("/games/my_game"), Config: DefaultProjectConfig()}) {
		t.Errorf("SyncExportPresets() = true, want false")
	}
	if got := m.tree()["/games/my_game/"+exportPresetsFileName]; got != text {
		t.Errorf("file = %q, want %q", got, text)
	}
}

func TestSyncExportPresetsNoFile(t *testing.T) {
	m := withMemFS(t, "/games/my_game", testProjectTree)
	before := m.tree()
	if SyncExportPresets(Project{Root: NewPath("/games/my_game"), Config: DefaultProjectConfig()}) {
		t.Errorf("SyncExportPresets() = true, want false")
	}
	if !maps.Equal(m.tree(), before) {
		t.Errorf("tree = %v, want it unchanged", m.tree())
	}
}
