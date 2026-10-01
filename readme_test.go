package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "Rewrite the README's code images.")

// readmeColors are the SVG colors of the highlighting styles, on a dark background.
var readmeColors = map[string]string{
	"":                   "#e6edf3",
	string(CodeKeyword):  "#e3b341",
	string(CodeType):     "#7ee787",
	string(CodeFunction): "#56d4dd",
	string(CodeLiteral):  "#d2a8ff",
	string(CodeComment):  "#8b949e",
	string(CodePreProc):  "#79c0ff",
}

// TestReadmeImages checks that each readme/NAME.gd++ has its highlighted image,
// readme/NAME.svg, which README.md shows, since GitHub can't highlight GD++.
// Run with -update to rewrite the images.
func TestReadmeImages(t *testing.T) {
	withTTY(t, true)
	files, _ := filepath.Glob("readme/*.gd++")
	if len(files) == 0 {
		t.Fatal("No readme/*.gd++ files.")
	}
	for _, file := range files {
		code, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		svg, svgFile := readmeSVG(highlightCode(strings.TrimRight(string(code), "\n"), "gd++")), strings.TrimSuffix(file, ".gd++")+".svg"
		if *update {
			if err := os.WriteFile(svgFile, []byte(svg), 0o644); err != nil {
				t.Fatal(err)
			}
		} else if old, _ := os.ReadFile(svgFile); string(old) != svg {
			t.Errorf("%s is out of date: run `go test -run TestReadmeImages -update .`", svgFile)
		}
	}
}

// readmeSVG renders highlighted code, with the ANSI styles of Styled, as an SVG image.
func readmeSVG(code string) string {
	const fontSize, lineHeight, pad = 14, 20, 16
	lines, cols := strings.Split(code, "\n"), 0
	var body strings.Builder
	for i, line := range lines {
		cols = max(cols, visibleLen(line))
		fmt.Fprintf(&body, "  <text x=\"%d\" y=\"%d\">", pad, pad+fontSize+i*lineHeight)
		for j, part := range strings.Split(line, "\x1b[") {
			style, text := "", part
			if j > 0 {
				style, text, _ = strings.Cut(part, "m")
				if style == "0" {
					style = ""
				}
			}
			// Runs flow one after another, so the font's own spacing applies, with no gaps between them.
			// Spaces are non-breaking, since renderers collapse plain ones, e.g. the indentation.
			if text != "" {
				escaped := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", " ", "\u00a0").Replace(text)
				fmt.Fprintf(&body, "<tspan fill=\"%s\">%s</tspan>", readmeColors[style], escaped)
			}
		}
		body.WriteString("</text>\n")
	}
	width, height := 2*pad+cols*fontSize*6/10+1, 2*pad+len(lines)*lineHeight-(lineHeight-fontSize)/2
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="100%%" height="100%%" rx="8" fill="#0d1117"/>
  <g font-family="ui-monospace, SFMono-Regular, Menlo, Consolas, 'Liberation Mono', monospace" font-size="%d">
%s  </g>
</svg>
`, width, height, width, height, fontSize, body.String())
}
