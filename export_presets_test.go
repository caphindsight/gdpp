// export_presets_test.go: tests for export_presets.go and cmd_export.go.

package main

import (
	"os"
	"strings"
	"testing"
)

// testPresets has a Linux preset with a multi-line string, and an Android
// preset, which GD++ can't export for.
const testPresets = `[preset.0]

name="Linux"
platform="Linux"
runnable=true
exclude_filter="notes/*"
export_path="out/game.x86_64"

[preset.0.options]

custom_template/debug=""
custom_template/release=""
binary_format/architecture="x86_64"
ssh_remote_deploy/run_script="#!/usr/bin/env bash
[not.a.section]
unzip -o -q \"{temp_dir}/{archive_name}\""

[preset.1]

name="Android"
platform="Android"
runnable=true

[preset.1.options]

gradle_build/use_gradle_build=false
`

// withPresetsFS installs the build project with export_presets.cfg holding text.
func withPresetsFS(t *testing.T, text string) *memFS {
	m := withBuildFS(t)
	m.nodes["/games/my_game/"+exportPresetsFileName] = &memNode{data: []byte(text)}
	return m
}

func presetsFile() Path { return LoadProject(Cwd()).Root.Cd(exportPresetsFileName) }

func TestExportPresetsRoundTrip(t *testing.T) {
	withPresetsFS(t, testPresets)
	e := parseExportPresets(presetsFile())
	if len(e.presets) != 2 {
		t.Fatalf("parsed %d presets, want 2", len(e.presets))
	}
	if got := e.presets[0].options.get("ssh_remote_deploy/run_script"); !strings.Contains(got, "[not.a.section]\nunzip -o -q \"{temp_dir}") {
		t.Errorf("run_script = %q", got)
	}
	if got := e.presets[0].target(); got != "linux.x86_64" {
		t.Errorf("target() = %q, want linux.x86_64", got)
	}
	if got := e.presets[1].target(); got != "" {
		t.Errorf("Android target() = %q, want none", got)
	}
	if got := e.render(true); got != testPresets {
		t.Errorf("render() = %q, want the file unchanged", got)
	}
}

func TestExportPresetsTwins(t *testing.T) {
	withPresetsFS(t, testPresets)
	e := parseExportPresets(presetsFile())
	bin := NewPath("/bin")
	synced := e.syncTwins([]string{"Linux"}, []string{extensionTwinSuffix, engineTwinSuffix}, bin).render(true)
	if !strings.HasPrefix(synced, testPresets+"\n[preset.2]\n\nname=\"Linux [GD++ GDExtension]\"\nplatform=\"Linux\"\nrunnable=false\n") {
		t.Errorf("synced = %s\nwant the user's presets unchanged, then the GDExtension twin", synced)
	}
	for _, want := range []string{
		`exclude_filter="notes/*, *.c, *.cc, *.cpp, *.cxx, *.c++, *.h, *.hh, *.hpp, *.hxx, *.gd++, *.gdpp, *.gg, gd++pkg.toml, gd++proj.toml, res://_gd++proj/*"` + "\n",
		"\n[preset.3]\n\nname=\"Linux [GD++ Engine]\"\n",
		`exclude_filter="notes/*, *.c, *.cc, *.cpp, *.cxx, *.c++, *.h, *.hh, *.hpp, *.hxx, *.gd++, *.gdpp, *.gg, gd++pkg.toml, gd++proj.toml, res://_gd++proj/*, *.gdextension, *.gdextension.uid, lib*.so, lib*.dll, lib*.dylib"` + "\n",
		"\n[preset.3.options]\n\ncustom_template/debug=\"/bin/godot.linuxbsd.template_debug.x86_64\"\ncustom_template/release=\"/bin/godot.linuxbsd.template_release.x86_64\"\n",
	} {
		if !strings.Contains(synced, want) {
			t.Errorf("synced = %s\nwant it to contain %q", synced, want)
		}
	}
	if n := strings.Count(synced, "[not.a.section]"); n != 3 {
		t.Errorf("the multi-line string appears %d times, want 3", n)
	}

	// Syncing again changes nothing; syncing only one kind keeps the other twin.
	presetsFile().WriteString(synced)
	e = parseExportPresets(presetsFile())
	if got := e.syncTwins([]string{"Linux"}, []string{engineTwinSuffix}, bin).render(true); got != synced {
		t.Errorf("synced again = %s\nwant %s", got, synced)
	}

	// Twins whose original is gone are removed.
	e.presets = e.presets[1:]
	if got := e.syncTwins(nil, nil, bin).render(true); strings.Contains(got, "GD++") || !strings.HasPrefix(got, "[preset.0]\n\nname=\"Android\"") {
		t.Errorf("synced without Linux = %s\nwant only the Android preset", got)
	}
}

func TestExportPresetsRunnablePresets(t *testing.T) {
	text := "[runnable_presets]\n\n\"Linux\"=\"Linux\"\n\n" + strings.Replace(testPresets, "runnable=true\n", "", 1)
	withPresetsFS(t, text)
	e := parseExportPresets(presetsFile())
	synced := e.syncTwins([]string{"Linux"}, []string{extensionTwinSuffix}, NewPath("/bin")).render(true)
	if !strings.HasPrefix(synced, text+"\n[preset.2]\n\nname=\"Linux [GD++ GDExtension]\"\nplatform=\"Linux\"\nexclude_filter=") {
		t.Errorf("synced = %s\nwant the file unchanged, then a twin without a runnable key", synced)
	}
	presetsFile().WriteString(synced)
	e = parseExportPresets(presetsFile())
	if got := (exportPresets{e.head, e.originals()}).render(true); got != text {
		t.Errorf("without twins = %s\nwant the original file", got)
	}
}

func TestExportPresetsTwinWithoutOptions(t *testing.T) {
	withPresetsFS(t, "[preset.0]\n\nname=\"Web\"\n")
	e := parseExportPresets(presetsFile())
	got := e.syncTwins([]string{"Web"}, []string{extensionTwinSuffix}, NewPath("/bin")).render(true)
	if want := "[preset.0]\n\nname=\"Web\"\n\n[preset.1]\n\nname=\"Web [GD++ GDExtension]\"\nexclude_filter="; !strings.HasPrefix(got, want) {
		t.Errorf("synced = %q, want it to start with %q", got, want)
	}
	if !strings.HasSuffix(got, "\n[preset.1.options]\n\n") {
		t.Errorf("synced = %q, want an empty options section", got)
	}
}

func TestExportUndo(t *testing.T) {
	m := withPresetsFS(t, testPresets)
	withTTY(t, false)
	withQuiet(t, false)
	withForce(t, true)
	file := "/games/my_game/" + exportPresetsFileName
	if out := captureStderr(t, func() { (&CmdExport{Undo: true}).Run() }); out != "[-] Nothing to undo.\n" {
		t.Errorf("output without twins = %q", out)
	}
	e := parseExportPresets(presetsFile())
	m.nodes[file].data = []byte(e.syncTwins([]string{"Linux"}, []string{extensionTwinSuffix, engineTwinSuffix}, NewPath("/bin")).render(true))
	if out := captureStderr(t, func() { (&CmdExport{Undo: true}).Run() }); !strings.HasSuffix(out, "[-] Success!\n") {
		t.Errorf("output = %q", out)
	}
	if got := m.tree()[file]; got != testPresets {
		t.Errorf("after undo = %s\nwant the original file", got)
	}
}

func TestExportUndoDeclined(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		m := withPresetsFS(t, testPresets)
		e := parseExportPresets(presetsFile())
		m.nodes["/games/my_game/"+exportPresetsFileName].data = []byte(e.syncTwins([]string{"Linux"}, []string{engineTwinSuffix}, NewPath("/bin")).render(true))
		(&CmdExport{Undo: true}).Run()
		return
	}
	out, code := runFailHelper(t, "TestExportUndoDeclined")
	if want := "[?] Remove the twin presets \"Linux [GD++ Engine]\" from res://export_presets.cfg? [y/n] n\n[x] Output is not a tty, use -f to confirm.\n"; code != 1 || out != want {
		t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, want)
	}
}

func TestExportParseFailure(t *testing.T) {
	cases := map[string]struct{ text, section string }{
		"after":      {"[preset.0]\nname=\"A\"\n[other]\n", "other"},
		"before":     {"[other]\n[preset.0]\nname=\"A\"\n", "other"},
		"runnable":   {"[preset.0]\nname=\"A\"\n[runnable_presets]\n", "runnable_presets"},
		"orphan":     {"[preset.0.options]\n", "preset.0.options"},
		"subsection": {"[preset.0]\n[preset.0.extra]\n", "preset.0.extra"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if os.Getenv("GDPP_FAIL_HELPER") == "1" {
				isTTY = false
				withPresetsFS(t, tc.text)
				parseExportPresets(presetsFile())
				return
			}
			out, code := runFailHelper(t, t.Name())
			if want := "[x] Failed to parse res://export_presets.cfg: unexpected section [" + tc.section + "].\n"; code != 1 || out != want {
				t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, want)
			}
		})
	}
}

func TestExportChoose(t *testing.T) {
	withPresetsFS(t, testPresets)
	withTTY(t, false)
	withQuiet(t, false)
	e := parseExportPresets(presetsFile())
	var chosen []exportPreset
	var targets []string
	out := captureStderr(t, func() { chosen, targets = (&CmdExport{}).choose(e) })
	if len(chosen) != 1 || chosen[0].name() != "Linux" || len(targets) != 1 || targets[0] != "linux.x86_64" {
		t.Errorf("choose() = %d presets, %q", len(chosen), targets)
	}
	if want := "[!] Skipping the export preset \"Android\", since GD++ can't export for its platform yet.\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got := (&CmdExport{Engine: "4.5"}).validate(); len(got) != 1 || got[0] != engineTwinSuffix {
		t.Errorf("validate() with --engine = %q", got)
	}
	if got := (&CmdExport{Engine: "4.5", Gdext: true}).validate(); len(got) != 2 {
		t.Errorf("validate() with --gdext --engine = %q", got)
	}
	if got := (&CmdExport{}).validate(); len(got) != 1 || got[0] != extensionTwinSuffix {
		t.Errorf("validate() by default = %q", got)
	}
}

func TestExportInvalidArgs(t *testing.T) {
	cases := map[string]struct {
		c    CmdExport
		want string
	}{
		"undo":   {CmdExport{Undo: true, Engine: "4.5"}, "Invalid arguments: --undo cannot be used with other options."},
		"doc":    {CmdExport{Engine: "4.5", BuildOptions: BuildOptions{Doc: true}}, "Invalid arguments: --doc and --nodoc require --gdext when used with --engine."},
		"godot":  {CmdExport{Engine: "4.5", Gdext: true, Godot: "godot"}, "Invalid arguments: --godot cannot be used with both --gdext and --engine."},
		"preset": {CmdExport{Preset: []string{"Mac"}}, `There is no export preset named "Mac".`},
		"path":   {CmdExport{Godot: "godot"}, `Set the export path of the preset "Linux" in Godot's Export dialog.`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if os.Getenv("GDPP_FAIL_HELPER") == "1" {
				isTTY = false
				m := withPresetsFS(t, testPresets)
				if name == "path" {
					m.nodes["/games/my_game/"+exportPresetsFileName].data = []byte(strings.Replace(testPresets, "export_path=\"out/game.x86_64\"", "export_path=\"\"", 1))
				}
				tc.c.Run()
				return
			}
			out, code := runFailHelper(t, t.Name())
			if want := "[x] " + tc.want + "\n"; code != 1 || !strings.HasSuffix(out, want) {
				t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, want)
			}
		})
	}
}
