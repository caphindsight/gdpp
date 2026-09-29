package trans

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDispatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "player.gd++")
	os.WriteFile(path, []byte("/// A player.\nclass_name Player\nextends Node\n\nfunc jump() -> void {}\n"), 0o644)
	opts := Options{Dependencies: []Dependency{{Name: "Node", Include: "<godot_cpp/classes/node.hpp>", Kind: Object}}}

	decls, err := ListClasses(path, 0)
	if err != nil || len(decls) != 1 || decls[0].Name != "Player" || decls[0].Base != "Node" {
		t.Errorf("ListClasses: %+v, %v", decls, err)
	}
	header, err := GenerateHeader(path, opts, 0)
	if err != nil || !strings.Contains(header, "class Player : public Node {") {
		t.Errorf("GenerateHeader: %v\n%s", err, header)
	}
	source, err := GenerateSource(path, opts, 0)
	if err != nil || !strings.Contains(source, `#include "player.h"`) {
		t.Errorf("GenerateSource: %v\n%s", err, source)
	}
	doc, err := DocumentClass(path, "Player", opts, 0)
	if err != nil || !strings.Contains(doc, "A player.") {
		t.Errorf("DocumentClass: %v\n%s", err, doc)
	}
	if _, err := DocumentClass(path, "Enemy", opts, 0); err == nil || err.Error() != "There is no class Enemy in "+path+"." {
		t.Errorf("DocumentClass of a missing class: %v", err)
	}
	name, text, err := RuntimeHeader(0)
	if err != nil || name != "gd++/syntax_0.hpp" || !strings.Contains(text, "namespace gdpp") {
		t.Errorf("RuntimeHeader: %q, %v", name, err)
	}
}

func TestErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.gd++")
	os.WriteFile(path, []byte("fun foo() {}\n"), 0o644)
	if _, err := GenerateHeader(path, Options{}, 0); err == nil || !strings.Contains(err.Error(), `Did you mean "func"?`) {
		t.Errorf("Expected a syntax error, but got %v.", err)
	}
	if _, err := ListClasses(path, 7); err == nil || err.Error() != "Unsupported GD++ syntax 7." {
		t.Errorf("Expected an unsupported syntax error, but got %v.", err)
	}
	if _, _, err := RuntimeHeader(-1); err == nil {
		t.Error("Expected an unsupported syntax error.")
	}
	if _, err := ListClasses(filepath.Join(t.TempDir(), "missing.gd++"), 0); err == nil || !strings.HasPrefix(err.Error(), "Failed to read ") {
		t.Errorf("Expected a read error, but got %v.", err)
	}
}
