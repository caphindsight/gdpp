package meta

import (
	"bytes"
	"flag"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"slices"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "Append new API entries to api.golden.")

// TestFrozenAPI fails if an exported identifier, field, type or constant value in api.golden is gone or changed.
// New API must be added to api.golden with -update, which only appends, so a break needs a visible hand edit.
func TestFrozenAPI(t *testing.T) {
	got := api(t)
	data, err := os.ReadFile("api.golden")
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	want := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, line := range want {
		if line != "" && !slices.Contains(got, line) {
			t.Errorf("Removed or changed API, which breaks old syntax forks: %s", line)
		}
	}
	var added []string
	for _, line := range got {
		if !slices.Contains(want, line) {
			added = append(added, line)
		}
	}
	if len(added) == 0 {
		return
	}
	if !*update {
		t.Fatalf("New API, run `go test ./trans/meta -update` to accept it:\n%s", strings.Join(added, "\n"))
	}
	f, err := os.OpenFile("api.golden", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.WriteString(strings.Join(added, "\n") + "\n")
}

// api lists the package's exported API, one entry per line.
func api(t *testing.T) []string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "meta.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	str := func(node any) string {
		var b bytes.Buffer
		printer.Fprint(&b, fset, node)
		return b.String()
	}
	var lines []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			t.Errorf("Package meta must only declare data, but found %s.", str(decl))
			continue
		}
		for _, spec := range gen.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				if st, ok := s.Type.(*ast.StructType); ok {
					lines = append(lines, "type "+s.Name.Name+" struct")
					for _, field := range st.Fields.List {
						for _, name := range field.Names {
							lines = append(lines, "field "+s.Name.Name+"."+name.Name+" "+str(field.Type))
						}
					}
				} else {
					lines = append(lines, "type "+s.Name.Name+" "+str(s.Type))
				}
			case *ast.ValueSpec:
				for i, name := range s.Names {
					lines = append(lines, "const "+name.Name+" "+str(s.Type)+" = "+str(s.Values[i]))
				}
			case *ast.ImportSpec:
				t.Errorf("Package meta must import nothing, but imports %s.", s.Path.Value)
			}
		}
	}
	return lines
}
