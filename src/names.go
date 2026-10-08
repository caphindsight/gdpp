// names.go: the names that GD++ generates from project.godot, as res://.gd++proj's [names] asks.

package main

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gd++/trans"
)

// ProjectNames holds the [names] settings of res://.gd++proj: the name of each declaration that GD++ generates from
// project.godot, or "" for none, the default.
type ProjectNames struct {
	Actions            string `toml:"actions,omitempty"`
	Groups             string `toml:"groups,omitempty"`
	PhysicsLayers2D    string `toml:"physics_layers_2d,omitempty"`
	PhysicsLayers3D    string `toml:"physics_layers_3d,omitempty"`
	RenderLayers2D     string `toml:"render_layers_2d,omitempty"`
	RenderLayers3D     string `toml:"render_layers_3d,omitempty"`
	NavigationLayers2D string `toml:"navigation_layers_2d,omitempty"`
	NavigationLayers3D string `toml:"navigation_layers_3d,omitempty"`
	AvoidanceLayers    string `toml:"avoidance_layers,omitempty"`
}

// layerKinds are the kinds of layers that project.godot names, with their settings, their keys' prefix in
// [layer_names], and how the manual calls them.
func (n ProjectNames) layerKinds() []struct{ name, prefix, what string } {
	return []struct{ name, prefix, what string }{
		{n.PhysicsLayers2D, "2d_physics", "2D physics layers"}, {n.PhysicsLayers3D, "3d_physics", "3D physics layers"},
		{n.RenderLayers2D, "2d_render", "2D render layers"}, {n.RenderLayers3D, "3d_render", "3D render layers"},
		{n.NavigationLayers2D, "2d_navigation", "2D navigation layers"}, {n.NavigationLayers3D, "3d_navigation", "3D navigation layers"},
		{n.AvoidanceLayers, "avoidance", "avoidance layers"},
	}
}

// uiActions are Godot's built-in input actions, which project.godot only lists once they're changed.
var uiActions = []string{"ui_accept", "ui_select", "ui_cancel", "ui_focus_next", "ui_focus_prev", "ui_left", "ui_right", "ui_up", "ui_down",
	"ui_page_up", "ui_page_down", "ui_home", "ui_end", "ui_cut", "ui_copy", "ui_paste", "ui_undo", "ui_redo"}

// namesHeader is the generated header that declares the namespaces of actions and groups.
const namesHeader = "_gdpp_names.h"

var (
	// projectKeyPattern matches a key of project.godot at the start of a line, e.g. jump= or "move left"=.
	projectKeyPattern = regexp.MustCompile(`(?m)^(?:"((?:[^"\\]|\\.)*)"|([^\s="{}\[\],]+))=`)
	// layerPattern matches a layer's name in [layer_names], e.g. 3d_physics/layer_1="Player".
	layerPattern   = regexp.MustCompile(`(?m)^(\w+)/layer_(\d+)="((?:[^"\\]|\\.)*)"`)
	notNamePattern = regexp.MustCompile(`[^A-Za-z0-9_]+`)
)

// cppStatementWords are the C++ keywords of statements and operators, which names can't be either.
var cppStatementWords = wordSet("and asm break case catch continue do else for goto if new not or register return switch throw try typeid while xor")

// projectNames is what GD++ generates from project.godot for a package.
type projectNames struct {
	files  []gdppFile  // A GD++ file for each enum of layers.
	header string      // The C++ header of the namespaces of actions and groups, or "" without any.
	names  []godotName // The namespaces, which the header declares.
}

// nameList is a declaration that GD++ generates from project.godot: its name, the kind of entries it names, e.g.
// "input actions", the entries' names in project.godot, and their C++ names. Layers also have their numbers.
type nameList struct {
	decl, what    string
	names, idents []string
	layers        []int // For layers: each one's number, from 1.
	enum          bool  // Whether it's an enum of layers, else a namespace.
}

// projectNameLists returns the declarations that GD++ generates from project.godot, as res://.gd++proj's [names] asks.
func projectNameLists(p Project) []nameList {
	n, src := p.Config.Names, ""
	if n != (ProjectNames{}) {
		src = p.Root.Cd(projectFileName).ReadString()
	}
	var lists []nameList
	for _, kind := range []struct{ name, section, what string }{{n.Actions, "[input]", "input actions"}, {n.Groups, "[global_group]", "global groups"}} {
		if kind.name == "" {
			continue
		}
		keys := []string{}
		if kind.section == "[input]" {
			keys = slices.Clone(uiActions)
		}
		for _, m := range projectKeyPattern.FindAllStringSubmatch(GetTextBlock(src, kind.section, "", false), -1) {
			if key := godotStringUnescaper.Replace(m[1] + m[2]); !slices.Contains(keys, key) {
				keys = append(keys, key)
			}
		}
		idents := projectIdents(keys, kind.what, kind.name, func(s string) string { return notNamePattern.ReplaceAllString(s, "_") })
		lists = append(lists, nameList{decl: kind.name, what: kind.what, names: keys, idents: idents})
	}
	layers := layerPattern.FindAllStringSubmatch(GetTextBlock(src, "[layer_names]", "", false), -1)
	for _, kind := range n.layerKinds() {
		if kind.name == "" {
			continue
		}
		l := nameList{decl: kind.name, what: kind.what, enum: true}
		for _, m := range layers {
			if m[1] == kind.prefix && m[3] != "" {
				layer, _ := strconv.Atoi(m[2])
				l.names, l.layers = append(l.names, godotStringUnescaper.Replace(m[3])), append(l.layers, layer)
			}
		}
		l.idents = projectIdents(l.names, kind.what, kind.name, upperSnakeName)
		lists = append(lists, l)
	}
	return lists
}

// loadProjectNames returns what GD++ generates from project.godot for pkg, as res://.gd++proj's [names] asks.
func loadProjectNames(p Project, pkg Package) projectNames {
	var out projectNames
	var namespaces []string
	for _, l := range projectNameLists(p) {
		if l.enum {
			var lines []string
			for i, ident := range l.idents {
				lines = append(lines, fmt.Sprintf("/// Layer %d, %s.\n%s = %d", l.layers[i], strconv.Quote(l.names[i]), ident, int64(1)<<(l.layers[i]-1)))
			}
			rel := ".gd++names/" + l.decl + ".gd++"
			text := fmt.Sprintf("// Generated by GD++ from res://project.godot, do not edit.\n\n/// The %s that the project settings name.\n@bitfield\nenum_name %s\n\n%s\n",
				l.what, l.decl, strings.Join(lines, "\n"))
			out.files = append(out.files, gdppFile{File: pkg.BuildCache.Cd(sourcesDirName, rel), Rel: rel, Src: text})
			continue
		}
		var texts, lines []string
		for i, ident := range l.idents {
			texts = append(texts, fmt.Sprintf("\tinline constexpr char %s[] = %s;", ident, strconv.Quote(l.names[i])))
			lines = append(lines, fmt.Sprintf("inline constexpr gdpp::Name<_gdpp_text::%s> %s{};", ident, ident))
		}
		namespaces = append(namespaces, fmt.Sprintf("// The %s of the project settings.\nnamespace %s {\nnamespace _gdpp_text {\n%s\n}\n%s\n} // namespace %s",
			l.what, l.decl, strings.Join(texts, "\n"), strings.Join(lines, "\n"), l.decl))
		out.names = append(out.names, godotName{Name: l.decl, Include: `"` + namesHeader + `"`, Kind: trans.Other, Decl: "namespace"})
	}
	if len(namespaces) > 0 {
		runtime, _, err := trans.RuntimeHeader(pkg.Config.Syntax)
		Check(err, "Failed to find the GD++ runtime header")
		out.header = fmt.Sprintf("// Generated by GD++ from res://project.godot, do not edit.\n\n#pragma once\n\n#include <%s>\n\nnamespace godot {\n\n%s\n\n} // namespace godot\n",
			runtime, strings.Join(namespaces, "\n\n"))
	}
	return out
}

// projectIdents returns the C++ names of names, the names of the entries of the kind what in project.godot, which
// ident turns into names, for the declaration called decl. It fails on names that clash, or aren't names.
func projectIdents(names []string, what, decl string, ident func(string) string) []string {
	var idents []string
	for _, name := range names {
		id := ident(name)
		Assert(id != "" && (id[0] < '0' || id[0] > '9') && !cppKeywords[id] && !cppBuiltinTypes[id] && !cppStatementWords[id],
			"The %s %q of res://project.godot becomes %q in %s, which isn't a C++ name. Rename it in the project settings.", what, name, id, decl)
		j := slices.Index(idents, id)
		Assert(j < 0, "The %s %q and %q of res://project.godot both become %s in %s. Rename one in the project settings.", what, names[max(j, 0)], name, id, decl)
		idents = append(idents, id)
	}
	return idents
}

// upperSnakeName turns a layer's name into the name of its enum value, e.g. "Player Body" into PLAYER_BODY.
func upperSnakeName(s string) string {
	s = strings.Trim(notNamePattern.ReplaceAllString(s, "_"), "_")
	var sb strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' && s[i-1] >= 'a' && s[i-1] <= 'z' { // PlayerBody is PLAYER_BODY too.
			sb.WriteByte('_')
		}
		sb.WriteRune(r)
	}
	return strings.ToUpper(sb.String())
}
