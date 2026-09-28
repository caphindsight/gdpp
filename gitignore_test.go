// gitignore_test.go: tests for gitignore.go. Runs on memFS, in the project
// from project_test.go.

package main

import (
	"strings"
	"testing"
)

var (
	projectBlock = projectGitignore.marker + "\n" + projectGitignoreTemplate
	packageBlock = packageGitignore.marker + "\n" + packageGitignoreTemplate
)

func TestGitignoreTemplates(t *testing.T) {
	for _, b := range []gitignoreBlock{projectGitignore, packageGitignore} {
		if b.text == "" || strings.Contains(b.text, "\n\n") || strings.HasSuffix(b.text, "\n") {
			t.Errorf("%s: text = %q, want non-empty lines only", b.marker, b.text)
		}
	}
}

// runSync runs SyncGitignores with the given VCS on withPackages(pkgs) plus
// files, keyed by path under the project root, and returns its output,
// result, and the .gitignore files after.
func runSync(t *testing.T, vcs string, pkgs, files map[string]string) (out string, changed bool, after map[string]string) {
	tree := withPackages(pkgs)
	for path, text := range files {
		tree["/games/my_game/"+path] = text
	}
	m := withMemFS(t, "/games/my_game", tree)
	withQuiet(t, false)
	withTTY(t, false)
	out = captureStderr(t, func() {
		p := LoadProject(Cwd())
		p.Config.VCS = vcs
		changed = SyncGitignores(p)
	})
	after = map[string]string{}
	for path, text := range subtree(m.tree(), "/games/my_game/") {
		if strings.HasSuffix(path, gitignoreFileName) {
			after[path] = text
		}
	}
	return out, changed, after
}

func checkGitignores(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("gitignores = %q, want %q", got, want)
	}
	for path, text := range want {
		if got[path] != text {
			t.Errorf("%s = %q, want %q", path, got[path], text)
		}
	}
}

const syncPkgConfig = "bind = \"4.3\"\nspec = \"4.3\"\n"

// syncPkgs has packages at res://src/pkg and, via the files, at res://.
var syncPkgs = map[string]string{"src/pkg": syncPkgConfig}

func TestSyncGitignoresGit(t *testing.T) {
	out, changed, after := runSync(t, "git", syncPkgs, map[string]string{
		packageFileName:      syncPkgConfig,
		".gitignore":         "user\n",
		"src/pkg/.gitignore": "# GD++ package-level ignores\nstale\n\n!keep\r\n\n\n",
	})
	want := "[>] Updated res://.gitignore.\n[>] Updated res://src/pkg/.gitignore.\n"
	if !changed || out != want {
		t.Errorf("changed = %v, output = %q, want true, %q", changed, out, want)
	}
	checkGitignores(t, after, map[string]string{
		".gitignore":         "user\n\n" + projectBlock + "\n" + packageBlock,
		"src/pkg/.gitignore": packageBlock + "\n!keep\n",
	})

	// Already canonical, including with CRLF line endings and extra empty lines.
	crlf := strings.ReplaceAll(packageBlock, "\n", "\r\n") + "\r\n\r\n"
	out, changed, _ = runSync(t, "git", syncPkgs, map[string]string{packageFileName: syncPkgConfig, ".gitignore": after[".gitignore"], "src/pkg/.gitignore": crlf})
	if changed || out != "" {
		t.Errorf("second run: changed = %v, output = %q, want false, \"\"", changed, out)
	}
}

func TestSyncGitignoresCreate(t *testing.T) {
	out, _, after := runSync(t, "git", syncPkgs, nil)
	want := "[>] Created res://.gitignore.\n[>] Created res://src/pkg/.gitignore.\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	checkGitignores(t, after, map[string]string{".gitignore": projectBlock, "src/pkg/.gitignore": packageBlock})
}

func TestSyncGitignoresStaleRootPackageBlock(t *testing.T) {
	// res:// isn't a package, so its package-level block goes.
	_, _, after := runSync(t, "git", nil, map[string]string{".gitignore": packageBlock + "\nuser\n"})
	checkGitignores(t, after, map[string]string{".gitignore": "user\n\n" + projectBlock})
}

func TestSyncGitignoresNone(t *testing.T) {
	out, changed, after := runSync(t, "none", syncPkgs, map[string]string{
		packageFileName:      syncPkgConfig,
		".gitignore":         "user\n\n" + projectBlock + "\n" + packageBlock,
		"src/pkg/.gitignore": "\n" + packageBlock + "\n\n",
		"src/.gitignore":     packageBlock, // not a package: left alone
	})
	want := "[>] Updated res://.gitignore.\n[>] Deleted res://src/pkg/.gitignore.\n"
	if !changed || out != want {
		t.Errorf("changed = %v, output = %q, want true, %q", changed, out, want)
	}
	checkGitignores(t, after, map[string]string{".gitignore": "user\n", "src/.gitignore": packageBlock})
}

func TestSyncGitignoresKeepsEmptyFile(t *testing.T) {
	out, changed, after := runSync(t, "none", nil, map[string]string{".gitignore": "\n"})
	if changed || out != "" {
		t.Errorf("changed = %v, output = %q, want false, \"\"", changed, out)
	}
	checkGitignores(t, after, map[string]string{".gitignore": "\n"})
}

func TestSyncGitignoresEmptyBlock(t *testing.T) {
	// An empty block ends at the first empty line, keeping what follows.
	_, _, after := runSync(t, "git", nil, map[string]string{".gitignore": projectGitignore.marker + "\n\nuser\n"})
	checkGitignores(t, after, map[string]string{".gitignore": projectBlock + "\nuser\n"})
}
