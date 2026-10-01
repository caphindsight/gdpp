package trans

import (
	"fmt"
	"strings"
	"testing"
)

func TestDispatch(t *testing.T) {
	const name, src = "player.gd++", "/// A player.\nclass_name Player\nextends Node\n\nfunc jump() -> void {}\n"
	opts := Options{Dependencies: []Dependency{{Name: "Node", Include: "<godot_cpp/classes/node.hpp>", Kind: Object}}}
	for syntax := range forks {
		t.Run(fmt.Sprint(syntax), func(t *testing.T) {
			decls, err := ListClasses(name, src, syntax)
			if err != nil || len(decls) != 1 || decls[0].Name != "Player" || decls[0].Base != "Node" {
				t.Errorf("ListClasses: %+v, %v", decls, err)
			}
			files, err := Generate(name, src, opts, syntax)
			if err != nil || len(files) != 2 || files[0].Name != "Player.h" || !strings.Contains(files[0].Text, "class Player : public Node {") ||
				files[1].Name != "Player.cpp" || !strings.Contains(files[1].Text, `#include "Player.h"`) {
				t.Errorf("Generate: %v\n%+v", err, files)
			}
			doc, err := DocumentClass(name, src, "Player", opts, syntax)
			if err != nil || !strings.Contains(doc, "A player.") {
				t.Errorf("DocumentClass: %v\n%s", err, doc)
			}
			if _, err := DocumentClass(name, src, "Enemy", opts, syntax); err == nil || err.Error() != "There is no class Enemy in player.gd++." {
				t.Errorf("DocumentClass of a missing class: %v", err)
			}
			runtime, text, err := RuntimeHeader(syntax)
			if err != nil || runtime != fmt.Sprintf("gd++/syntax_%d.hpp", syntax) || !strings.Contains(text, "namespace gdpp") {
				t.Errorf("RuntimeHeader: %q, %v", runtime, err)
			}
		})
	}
}

func TestErrors(t *testing.T) {
	if _, err := Generate("bad.gd++", "fun foo() {}\n", Options{}, 0); err == nil || !strings.Contains(err.Error(), `Did you mean "func"?`) {
		t.Errorf("Expected a syntax error, but got %v.", err)
	}
	if _, err := ListClasses("bad.gd++", "", 7); err == nil || err.Error() != "Unsupported GD++ syntax 7." {
		t.Errorf("Expected an unsupported syntax error, but got %v.", err)
	}
	if _, _, err := RuntimeHeader(-1); err == nil {
		t.Error("Expected an unsupported syntax error.")
	}
}

func TestSupports(t *testing.T) {
	if !Supports(NightlySyntax) || !Supports(LatestSyntax) || Supports(LatestSyntax+1) || Supports(-1) {
		t.Error("Supports: wrong answer.")
	}
	if got := Syntaxes(); fmt.Sprint(got) != "[0 1]" {
		t.Errorf("Syntaxes() = %v, want [0 1]", got)
	}
}
