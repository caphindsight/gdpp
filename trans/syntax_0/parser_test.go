package syntax_0

import (
	"errors"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/alecthomas/participle/v2/lexer"
)

var update = flag.Bool("update", false, "Rewrite the golden files.")

// TestGolden parses every .gd++ file in testdata (plus the tutorial) and compares the result with the golden
// file next to it: <name>.ast holds the AST dump of a successful parse, <name>.err the expected error.
func TestGolden(t *testing.T) {
	inputs := map[string]string{"../../testsrc/tutorial.gd++": "testdata/tutorial"}
	err := filepath.WalkDir("testdata", func(path string, d fs.DirEntry, err error) error {
		if strings.HasSuffix(path, ".gd++") {
			inputs[path] = strings.TrimSuffix(path, ".gd++")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for path := range inputs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		base := inputs[path]
		t.Run(strings.TrimPrefix(base, "testdata/"), func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			name := strings.TrimPrefix(base, "testdata/") + ".gd++"
			file, err := Parse(name, string(src))
			var got, ext, other string
			if err != nil {
				got, ext, other = err.Error()+"\n", ".err", ".ast"
				checkError(t, name, string(src), err)
			} else {
				got, ext, other = Dump(file), ".ast", ".err"
				checkPositions(t, name, file)
			}
			if *update {
				os.Remove(base + other)
				if err := os.WriteFile(base+ext, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			if _, err := os.Stat(base + other); err == nil {
				t.Fatalf("Expected %s%s, but got %s:\n%s", base, other, ext, got)
			}
			want, err := os.ReadFile(base + ext)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("%s%s mismatch.\n--- got:\n%s\n--- want:\n%s", base, ext, got, want)
			}
		})
	}
}

// checkPositions asserts that every AST struct knows its file and line.
func checkPositions(t *testing.T, name string, file *File) {
	forEachNode(file, func(node any) {
		v := reflect.ValueOf(node).Elem()
		for i := 0; i < v.NumField(); i++ {
			if pos, ok := v.Field(i).Interface().(lexer.Position); ok && (pos.Filename != name || pos.Line < 1 || pos.Column < 1) {
				t.Errorf("%s.%s has a bad position: %+v", v.Type().Name(), v.Type().Field(i).Name, pos)
			}
		}
	})
}

var errorHead = regexp.MustCompile(`^(.+):(\d+):(\d+): [A-Z].*[.?!]$`)

// checkError asserts that err has a location, a readable message, the source line and a caret at the location.
func checkError(t *testing.T, name, src string, err error) {
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("Expected an *Error, but got %T: %v", err, err)
	}
	lines := strings.Split(err.Error(), "\n")
	m := errorHead.FindStringSubmatch(lines[0])
	if m == nil || m[1] != name || len(lines) < 3 {
		t.Fatalf("Badly formatted error:\n%s", err)
	}
	line, _ := strconv.Atoi(m[2])
	col, _ := strconv.Atoi(m[3])
	srcLine := strings.TrimRight(strings.Split(src, "\n")[line-1], "\r")
	gutter := " " + m[2] + " | "
	if lines[1] != gutter+srcLine {
		t.Errorf("Expected the source line %q, but got %q.", srcLine, lines[1])
	}
	caret := []rune(strings.TrimPrefix(lines[2], strings.Repeat(" ", len(gutter)-2)+"| "))
	if len(caret) < col || caret[col-1] != '^' || strings.TrimSpace(string(caret[:col-1])) != "" {
		t.Errorf("The caret is not at column %d:\n%s", col, err)
	}
	if len(lines) > 3 && !strings.HasPrefix(lines[3], "Hint: ") || len(lines) > 4 {
		t.Errorf("Unexpected lines after the caret:\n%s", err)
	}
	for _, raw := range []string{"unexpected token", "expected <", "lexer:"} {
		if strings.Contains(err.Error(), raw) {
			t.Errorf("The error contains participle's raw wording %q:\n%s", raw, err)
		}
	}
}
