// gitignore.go: the blocks of ignores GD++ manages in .gitignore files.

package main

import (
	_ "embed"
	"strings"
)

var (
	//go:embed templates/project.gitignore
	projectGitignoreTemplate string
	//go:embed templates/package.gitignore
	packageGitignoreTemplate string
)

// gitignoreBlock is a block of ignores GD++ manages in .gitignore files. It
// runs from its marker line to the first empty line, so its text must not
// contain empty lines.
type gitignoreBlock struct{ marker, text string }

var (
	projectGitignore = gitignoreBlock{"# GD++ project-level ignores", strings.TrimSuffix(projectGitignoreTemplate, "\n")}
	packageGitignore = gitignoreBlock{"# GD++ package-level ignores", strings.TrimSuffix(packageGitignoreTemplate, "\n")}
)

// set returns text with the block written if want, or removed otherwise.
func (b gitignoreBlock) set(text string, want bool) string {
	if want {
		return EditTextBlock(text, b.marker, "", true, b.text)
	}
	return RemoveTextBlock(text, b.marker, "", true)
}

// canonicalLines returns text with "\n" line endings, no trailing empty lines,
// and a final "\n", or "" if text has only empty lines.
func canonicalLines(text string) string {
	lines := trimTrailingEmptyLines(SplitLines(text))
	if len(lines) == 0 {
		return ""
	}
	return MergeLines(lines) + "\n"
}

// EditGitignore applies edit to the .gitignore in dir, read as "" if missing,
// and logs the change. If edit leaves only empty lines, the file is deleted.
// Differences in line endings and trailing empty lines don't count as
// changes. Returns whether the file changed.
func EditGitignore(dir Path, edit func(text string) string) bool {
	file := dir.Cd(gitignoreFileName)
	old := ""
	if file.Exists() {
		old = file.ReadString()
	}
	text := canonicalLines(edit(old))
	switch {
	case text == canonicalLines(old):
		return false
	case text == "":
		file.Remove()
		LogInfo("Deleted %s.", file.ToString())
	case file.Exists():
		file.WriteString(text)
		LogInfo("Updated %s.", file.ToString())
	default:
		file.WriteString(text)
		LogInfo("Created %s.", file.ToString())
	}
	return true
}

// SyncGitignores writes the GD++ blocks into the .gitignore files of the
// project root and each package root if the project uses git, or removes them
// otherwise. The project root gets the project-level block, plus the
// package-level one if it's also a package root. Returns whether any file
// changed.
func SyncGitignores(p Project) bool {
	git := p.Config.VCS == "git"
	changed := EditGitignore(p.Root, func(text string) string {
		return packageGitignore.set(projectGitignore.set(text, git), git && p.Root.IsPackageRoot())
	})
	for _, pkg := range p.ListPackages() {
		if pkg.Root != p.Root {
			changed = EditGitignore(pkg.Root, func(text string) string { return packageGitignore.set(text, git) }) || changed
		}
	}
	return changed
}
