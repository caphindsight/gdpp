package main

import (
	"strings"
	"testing"
)

func TestProjectNames(t *testing.T) {
	m := withGdppFS(t, map[string]string{
		"a.gd++": "class_name Mover\nextends Node\n\n@export_flags_3d_physics var mask: int = Physics3D::PLAYER_BODY | Physics3D::WORLD\n\nfunc tick() -> void {\n  StringName s = Action::jump;\n}\n",
	})
	config := m.nodes[pkgDir+packageFileName]
	config.data = append(config.data, []byte("\n[names]\nactions = \"Action\"\ngroups = \"Group\"\nphysics_layers_3d = \"Physics3D\"\n")...)
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
	transpileTestPackage(t, true)
	gen := subtree(m.tree(), pkgDir+".gd++build/gdpp/")
	for file, wants := range map[string][]string{
		"_gdpp_names.h": {"namespace Action {\nnamespace _gdpp_text {\n\tinline constexpr char ui_accept[] = \"ui_accept\";",
			"\tinline constexpr char jump[] = \"jump\";\n\tinline constexpr char move_left[] = \"move left\";\n}",
			"inline constexpr gdpp::Name<_gdpp_text::move_left> move_left{};", "namespace Group {\nnamespace _gdpp_text {\n\tinline constexpr char enemies[] = \"enemies\";\n}",
			"namespace Physics3D {\ninline constexpr int64_t PLAYER_BODY = 1; // Layer 1, \"Player Body\".\ninline constexpr int64_t WORLD = 4; // Layer 3, \"World\".\n}"},
		"Mover.cpp": {`#include "_gdpp_names.h"`},
	} {
		for _, want := range wants {
			if !strings.Contains(gen[file], want) {
				t.Errorf("%s = %s\nwant it to contain %q", file, gen[file], want)
			}
		}
	}
	if strings.Contains(gen["_gdpp_names.h"], "Ignored") || strings.Contains(gen["_gdpp_names.h"], "deadzone") {
		t.Errorf("_gdpp_names.h = %s\nwant only actions, groups and 3D physics layers", gen["_gdpp_names.h"])
	}
	// C++ only: no generated file binds them, documents them or registers them.
	for file, text := range gen {
		if file == "_gdpp_names.h" || strings.HasPrefix(file, "gd++/") {
			continue
		}
		for _, name := range []string{"Action", "Group", "Physics3D"} {
			for _, bound := range []string{`"` + name, name + `"`, "bind_integer_constant(get_class_static(), \"" + name} {
				if strings.Contains(text, bound) {
					t.Errorf("%s shows %s to Godot: %s", file, name, text)
				}
			}
		}
	}
	if register := m.tree()[pkgDir+".gd++build/__register_types__.cpp"]; strings.Contains(register, "Physics3D") || strings.Contains(register, "Action") {
		t.Errorf("__register_types__.cpp = %s\nwant no generated names", register)
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
