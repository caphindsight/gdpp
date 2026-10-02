package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadmeImages checks that each readme/svg/NAME.svg, which the README shows
// since GitHub can't highlight GD++, is the image of readme/svg/NAME.gd++, as
// readme/render.sh renders it with `gd++ cat --svg`.
func TestReadmeImages(t *testing.T) {
	files, _ := filepath.Glob("../readme/svg/*.gd++")
	for _, file := range files {
		code, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		svgFile := strings.TrimSuffix(file, ".gd++") + ".svg"
		if old, _ := os.ReadFile(svgFile); string(old) != codeSVG(string(code), "gd++") {
			t.Errorf("%s is out of date: run `make readme`.", svgFile)
		}
	}
}
