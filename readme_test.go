package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "Rewrite the README's code images.")

// TestReadmeImages checks that each readme/NAME.gd++ has its highlighted image,
// readme/NAME.svg, which README.md shows, since GitHub can't highlight GD++.
// Run with -update to rewrite the images.
func TestReadmeImages(t *testing.T) {
	files, _ := filepath.Glob("readme/*.gd++")
	if len(files) == 0 {
		t.Fatal("No readme/*.gd++ files.")
	}
	for _, file := range files {
		code, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		svg, svgFile := codeSVG(string(code), "gd++"), strings.TrimSuffix(file, ".gd++")+".svg"
		if *update {
			if err := os.WriteFile(svgFile, []byte(svg), 0o644); err != nil {
				t.Fatal(err)
			}
		} else if old, _ := os.ReadFile(svgFile); string(old) != svg {
			t.Errorf("%s is out of date: run `go test -run TestReadmeImages -update .`", svgFile)
		}
	}
}
