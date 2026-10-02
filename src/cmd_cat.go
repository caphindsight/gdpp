package main

import (
	"path"
	"slices"
	"strings"
)

// CmdCat shows any files in the pager, with syntax highlighting for C++ and
// GD++ files. Several files are concatenated, each after a line naming it.
// With --svg, it prints one file as a highlighted SVG image instead.
type CmdCat struct {
	Files []string `arg:"positional,required" placeholder:"FILE" help:"the files to show"`
	SVG   bool     `arg:"--svg" help:"print the file as a highlighted SVG image, e.g. for a README; takes exactly one file"`
}

// catCppExtensions are the extensions of C++ files.
var catCppExtensions = []string{".c", ".cc", ".cpp", ".cxx", ".c++", ".h", ".hh", ".hpp", ".hxx", ".h++", ".inl"}

func (c *CmdCat) Run() {
	Assert(!c.SVG || len(c.Files) == 1, "Invalid arguments: --svg takes exactly one file.")
	var out strings.Builder
	for i, name := range c.Files {
		file := ParsePath(name)
		Assert(file.IsFile(), "There is no file at %s.", file.ToString())
		text, ext, lang := file.ReadString(), strings.ToLower(path.Ext(name)), ""
		switch {
		case slices.Contains(catCppExtensions, ext):
			lang = "cpp"
		case slices.Contains(gdppExtensions, ext):
			lang = "gd++"
		}
		if c.SVG {
			PrintResult(codeSVG(text, lang))
			return
		}
		text = highlightCode(text, lang)
		if len(c.Files) > 1 {
			if i > 0 {
				out.WriteString("\n")
			}
			out.WriteString(Styled("// ==== "+file.ToString()+" ====", Gray) + "\n\n")
			if !strings.HasSuffix(text, "\n") && i < len(c.Files)-1 {
				text += "\n"
			}
		}
		out.WriteString(text)
	}
	PageResult(out.String())
}
