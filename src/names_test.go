package main

import (
	"strings"
	"testing"
)

func TestProjectNames(t *testing.T) {
	m := withGdppFS(t, map[string]string{
		"a.gd++": "class_name Mover\nextends Node\n\nvar mask: Physics3D = PLAYER | WORLD\n\nfunc tick() -> void {\n  StringName s = Action::jump;\n}\n",
	})
	m.nodes["/games/my_game/.gd++proj"] = &memNode{data: []byte("vcs = \"none\"\npresets = true\n\n[names]\nactions = \"Action\"\ngroups = \"Group\"\nphysics_layers_3d = \"Physics3D\"\n")}
	m.nodes["/games/my_game/project.godot"].data = append(m.nodes["/games/my_game/project.godot"].data, []byte(`
[global_group]

enemies=""

[input]

jump={
"deadzone": 0.5,
"events": [Object(InputEventKey,"resource_local_to_scene":false)
]
}
"move left"={
"deadzone": 0.5,
"events": []
}

[layer_names]

3d_physics/layer_1="Player Body"
3d_physics/layer_3="World"
2d_physics/layer_1="Ignored"
`)...)
	withTTY(t, false)
	withQuiet(t, false)
	transpileTestPackage(t, false)
	gen := subtree(m.tree(), pkgDir+".gd++build/gdpp/")
	for file, wants := range map[string][]string{
		"_gdpp_names.h": {"namespace Action {\nnamespace _gdpp_text {\n\tinline constexpr char ui_accept[] = \"ui_accept\";",
			"\tinline constexpr char jump[] = \"jump\";\n\tinline constexpr char move_left[] = \"move left\";\n}",
			"inline constexpr gdpp::Name<_gdpp_text::move_left> move_left{};", "namespace Group {\nnamespace _gdpp_text {\n\tinline constexpr char enemies[] = \"enemies\";\n}"},
		"Physics3D.h": {"PLAYER_BODY = 1,", "WORLD = 4,"},
		"Mover.cpp":   {`#include "_gdpp_names.h"`},
	} {
		for _, want := range wants {
			if !strings.Contains(gen[file], want) {
				t.Errorf("%s = %s\nwant it to contain %q", file, gen[file], want)
			}
		}
	}
	if strings.Contains(gen["_gdpp_names.h"], "Ignored") || strings.Contains(gen["_gdpp_names.h"], "deadzone") {
		t.Errorf("_gdpp_names.h = %s\nwant only actions and groups", gen["_gdpp_names.h"])
	}
}

func TestProjectNamesOff(t *testing.T) {
	m := withGdppFS(t, map[string]string{"a.gd++": "class_name Mover\nextends Node\n"})
	withTTY(t, false)
	withQuiet(t, false)
	transpileTestPackage(t, false)
	for file := range subtree(m.tree(), pkgDir+".gd++build/gdpp/") {
		if strings.Contains(file, "_gdpp_names") || strings.Contains(file, "Physics") {
			t.Errorf("Without [names], GD++ generated %s.", file)
		}
	}
}

func TestUpperSnakeName(t *testing.T) {
	for in, want := range map[string]string{"Player Body": "PLAYER_BODY", "PlayerBody": "PLAYER_BODY", "world": "WORLD", " Ghost-2 ": "GHOST_2"} {
		if got := upperSnakeName(in); got != want {
			t.Errorf("upperSnakeName(%q) = %q, want %q", in, got, want)
		}
	}
}
