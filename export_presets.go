// export_presets.go: the Godot editor's export presets, and the twin presets
// GD++ keeps next to the user's: copies that leave GD++ files out of exports,
// and for engine builds, export with GD++'s engine templates. The user's
// presets are never changed.

package main

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Twin preset kinds: a twin is named after its original plus the suffix.
const (
	extensionTwinSuffix = " [GD++ GDExtension]"
	engineTwinSuffix    = " [GD++ Engine]"
)

// twinSuffixes lists the twin kinds, in the order twins follow their original.
var twinSuffixes = []string{extensionTwinSuffix, engineTwinSuffix}

// Export filters of twins: sources and GD++ files for both kinds, and for
// engine twins also the GDExtension files, since the classes are built in.
var (
	sourceExcludes = []string{"*.c", "*.cc", "*.cpp", "*.cxx", "*.c++", "*.h", "*.hh", "*.hpp", "*.hxx",
		"*.gd++", "*.gdpp", "*.gg", packageFileName, projectConfigFileName, "res://" + checkedInDepsDirName + "/*"}
	extensionExcludes = []string{"*.gdextension", "*.gdextension.uid", "lib*.so", "lib*.dll", "lib*.dylib"}
)

// Export platforms by Godot's preset platform names: Linux was "Linux/X11"
// before Godot 4.3.
var presetPlatforms = map[string]string{"Windows Desktop": "windows", "Linux": "linux", "Linux/X11": "linux", "macOS": "macos"}

// cfgSection is a section of a Godot config file: its name, and its raw lines
// after the header line.
type cfgSection struct {
	name  string
	lines []string
}

// scanQuotes returns whether a string is open at the end of line, given
// whether one is open at its start.
func scanQuotes(line string, inString bool) bool {
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '\\' && inString:
			i++
		case line[i] == '"':
			inString = !inString
		}
	}
	return inString
}

// find returns the range of lines holding key's entry, or -1, -1. An entry
// spans several lines if it has a string with newlines.
func (s *cfgSection) find(key string) (int, int) {
	start, inString := -1, false
	for i, line := range s.lines {
		if !inString {
			if start >= 0 {
				return start, i
			}
			if strings.HasPrefix(line, key+"=") {
				start = i
			}
		}
		inString = scanQuotes(line, inString)
	}
	if start >= 0 {
		return start, len(s.lines)
	}
	return -1, -1
}

// get returns the value of key, unquoted if it's a string, or "".
func (s *cfgSection) get(key string) string {
	start, end := s.find(key)
	if start < 0 {
		return ""
	}
	value := strings.TrimPrefix(MergeLines(s.lines[start:end]), key+"=")
	if len(value) >= 2 && strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		return godotStringUnescaper.Replace(value[1 : len(value)-1])
	}
	return value
}

// set sets key to the raw value, e.g. `true` or quoteGodot("text"), adding the
// entry after the last one if it's missing.
func (s *cfgSection) set(key, value string) {
	entry := SplitLines(key + "=" + value)
	start, end := s.find(key)
	if start < 0 {
		start = len(trimTrailingEmptyLines(s.lines))
		end = start
	}
	s.lines = slices.Concat(s.lines[:start], entry, s.lines[end:])
}

func (s *cfgSection) clone() *cfgSection {
	return &cfgSection{s.name, slices.Clone(s.lines)}
}

// quoteGodot returns s as a string in Godot's text format.
func quoteGodot(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// exportPreset is a [preset.N] section and its [preset.N.options] section.
type exportPreset struct {
	main, options *cfgSection
}

func (e exportPreset) name() string { return e.main.get("name") }

// twinSuffix returns the suffix of the twin kind e is, or "".
func (e exportPreset) twinSuffix() string {
	for _, suffix := range twinSuffixes {
		if strings.HasSuffix(e.name(), suffix) {
			return suffix
		}
	}
	return ""
}

// targets returns the preset's build targets, e.g. linux.x86_64, or none if
// GD++ can't build for it. Universal macOS presets need both of macOS's
// architectures, whose libraries Godot combines when exporting.
func (e exportPreset) targets() []string {
	platform := presetPlatforms[e.main.get("platform")]
	if platform == "" || e.options == nil {
		return nil
	}
	arch := e.options.get("binary_format/architecture")
	if platform == "macos" && arch == "universal" {
		return []string{"macos.x86_64", "macos.arm64"}
	}
	if arch = fullName(buildArchs, arch); arch == "" {
		return nil
	}
	return []string{platform + "." + arch}
}

// exportPresets is a parsed export_presets.cfg.
type exportPresets struct {
	head    []string // lines before the first preset, e.g. Godot 4.5's [runnable_presets]
	presets []exportPreset
}

var presetSectionPattern = regexp.MustCompile(`^preset\.(\d+)(\.options)?$`)

// headSections are the sections GD++ knows that come before the presets, and
// keeps as they are.
var headSections = []string{"runnable_presets"}

// parseExportPresets parses the export presets in file. So that editing it
// can't corrupt it, e.g. when a new Godot version changes its format, it
// fails on any section it doesn't know, and asserts that the file renders back
// unchanged.
func parseExportPresets(file Path) exportPresets {
	text := file.ReadString()
	var e exportPresets
	var sections []*cfgSection
	inString := false
	for _, line := range SplitLines(text) {
		switch {
		case !inString && strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			sections = append(sections, &cfgSection{name: line[1 : len(line)-1]})
		case len(sections) == 0:
			e.head = append(e.head, line)
		default:
			sections[len(sections)-1].lines = append(sections[len(sections)-1].lines, line)
		}
		inString = scanQuotes(line, inString)
	}
	for i, s := range sections {
		m := presetSectionPattern.FindStringSubmatch(s.name)
		if slices.Contains(headSections, s.name) && len(e.presets) == 0 {
			e.head = append(append(e.head, "["+s.name+"]"), s.lines...)
			continue
		}
		Assert(m != nil, "Failed to parse %s: unexpected section [%s].", file.ToString(), s.name)
		if m[2] == "" {
			e.presets = append(e.presets, exportPreset{main: s})
			continue
		}
		// Options follow their preset.
		Assert(i > 0 && sections[i-1].name == "preset."+m[1], "Failed to parse %s: unexpected section [%s].", file.ToString(), s.name)
		e.presets[len(e.presets)-1].options = s
	}
	Assert(e.render(false) == text, "Failed to parse %s: it has an unexpected format.", file.ToString())
	return e
}

// render returns the file's text. With renumber, presets are numbered
// preset.0, preset.1, ... in order, as Godot requires.
func (e exportPresets) render(renumber bool) string {
	lines := slices.Clone(e.head)
	for i, preset := range e.presets {
		if renumber {
			preset.main.name = "preset." + strconv.Itoa(i)
		}
		lines = append(append(lines, "["+preset.main.name+"]"), preset.main.lines...)
		if preset.options != nil {
			if renumber {
				preset.options.name = preset.main.name + ".options"
			}
			lines = append(append(lines, "["+preset.options.name+"]"), preset.options.lines...)
		}
	}
	return MergeLines(lines)
}

// twin returns a twin of the preset original, of the kind suffix: a copy that
// isn't runnable, since that is the user's preset's job, and leaves GD++ files
// out. Since Godot 4.5, presets are only runnable if [runnable_presets] names
// them, so twins aren't. Engine twins export with the engine templates in binDir.
func twin(original exportPreset, suffix string, binDir Path) exportPreset {
	t := exportPreset{original.main.clone(), &cfgSection{lines: []string{"", ""}}}
	if original.options != nil {
		t.options = original.options.clone()
	}
	t.main.set("name", quoteGodot(original.name()+suffix))
	if start, _ := t.main.find("runnable"); start >= 0 {
		t.main.set("runnable", "false")
	}
	excludes := sourceExcludes
	if suffix == engineTwinSuffix {
		excludes = slices.Concat(sourceExcludes, extensionExcludes)
		platform, arch, _ := strings.Cut(original.targets()[0], ".")
		// Absolute paths: Godot copies templates without resolving res:// paths.
		t.options.set("custom_template/debug", quoteGodot(binDir.Cd(engineBinary(platform, arch, false)).GetOsPath()))
		t.options.set("custom_template/release", quoteGodot(binDir.Cd(engineBinary(platform, arch, true)).GetOsPath()))
	}
	t.main.set("exclude_filter", quoteGodot(addFilters(original.main.get("exclude_filter"), excludes)))
	return t
}

// addFilters returns the comma-separated filter list with the filters it
// lacks appended.
func addFilters(list string, filters []string) string {
	var have, missing []string
	for _, f := range strings.Split(list, ",") {
		have = append(have, strings.TrimSpace(f))
	}
	for _, f := range filters {
		if !slices.Contains(have, f) {
			missing = append(missing, f)
		}
	}
	if strings.TrimSpace(list) == "" {
		return strings.Join(missing, ", ")
	}
	return strings.Join(append([]string{list}, missing...), ", ")
}

// originals returns the presets that aren't twins.
func (e exportPresets) originals() []exportPreset {
	return slices.DeleteFunc(slices.Clone(e.presets), func(p exportPreset) bool { return p.twinSuffix() != "" })
}

// syncTwins returns e with the twins of the kinds suffixes made anew for the
// presets named in chosen. Other twins are kept if their original still
// exists. The originals come first, then each original's twins.
func (e exportPresets) syncTwins(chosen, suffixes []string, binDir Path) exportPresets {
	twins := map[string]exportPreset{}
	for _, p := range e.presets {
		if p.twinSuffix() != "" {
			twins[p.name()] = p
		}
	}
	synced := exportPresets{e.head, e.originals()}
	for _, original := range e.originals() {
		for _, suffix := range twinSuffixes {
			if slices.Contains(chosen, original.name()) && slices.Contains(suffixes, suffix) {
				synced.presets = append(synced.presets, twin(original, suffix, binDir))
			} else if t, ok := twins[original.name()+suffix]; ok {
				synced.presets = append(synced.presets, t)
			}
		}
	}
	return synced
}
