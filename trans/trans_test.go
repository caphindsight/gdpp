package trans

import (
	"strings"
	"testing"
)

func TestDispatch(t *testing.T) {
	const name, src = "player.gd++", "/// A player.\nclass_name Player\nextends Node\n\nfunc jump() -> void {}\n"
	opts := Options{Dependencies: []Dependency{{Name: "Node", Include: "<godot_cpp/classes/node.hpp>", Kind: Object}}}

	decls, err := ListClasses(name, src, 0)
	if err != nil || len(decls) != 1 || decls[0].Name != "Player" || decls[0].Base != "Node" {
		t.Errorf("ListClasses: %+v, %v", decls, err)
	}
	header, err := GenerateHeader(name, src, opts, 0)
	if err != nil || !strings.Contains(header, "class Player : public Node {") {
		t.Errorf("GenerateHeader: %v\n%s", err, header)
	}
	source, err := GenerateSource(name, src, opts, 0)
	if err != nil || !strings.Contains(source, `#include "player.h"`) {
		t.Errorf("GenerateSource: %v\n%s", err, source)
	}
	doc, err := DocumentClass(name, src, "Player", opts, 0)
	if err != nil || !strings.Contains(doc, "A player.") {
		t.Errorf("DocumentClass: %v\n%s", err, doc)
	}
	if _, err := DocumentClass(name, src, "Enemy", opts, 0); err == nil || err.Error() != "There is no class Enemy in player.gd++." {
		t.Errorf("DocumentClass of a missing class: %v", err)
	}
	runtime, text, err := RuntimeHeader(0)
	if err != nil || runtime != "gd++/syntax_0.hpp" || !strings.Contains(text, "namespace gdpp") {
		t.Errorf("RuntimeHeader: %q, %v", runtime, err)
	}
}

func TestErrors(t *testing.T) {
	if _, err := GenerateHeader("bad.gd++", "fun foo() {}\n", Options{}, 0); err == nil || !strings.Contains(err.Error(), `Did you mean "func"?`) {
		t.Errorf("Expected a syntax error, but got %v.", err)
	}
	if _, err := ListClasses("bad.gd++", "", 7); err == nil || err.Error() != "Unsupported GD++ syntax 7." {
		t.Errorf("Expected an unsupported syntax error, but got %v.", err)
	}
	if _, _, err := RuntimeHeader(-1); err == nil {
		t.Error("Expected an unsupported syntax error.")
	}
}
