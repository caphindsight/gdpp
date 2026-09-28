// archive_test.go: tests for archive.go.

package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// subtree returns the entries of tree under prefix, with prefix cut off.
func subtree(tree map[string]string, prefix string) map[string]string {
	sub := map[string]string{}
	for k, v := range tree {
		if rest, ok := strings.CutPrefix(k, prefix); ok && rest != "" {
			sub[rest] = v
		}
	}
	return sub
}

func TestArchiveRoundTrip(t *testing.T) {
	for _, format := range []string{"tar", "zip"} {
		t.Run(format, func(t *testing.T) {
			m := withMemFS(t, "/", map[string]string{
				"/src/a.h":     "a",
				"/src/.hidden": "h",
				"/src/sub/b.h": "b",
				"/src/empty/":  "",
			})
			src, archive, dst := NewPath("/src"), NewPath("/archive"), NewPath("/dst")
			if format == "tar" {
				src.Tar(archive)
				archive.Untar(dst)
			} else {
				src.Zip(archive)
				archive.Unzip(dst)
			}
			tree := m.tree()
			if got, want := subtree(tree, "/dst/"), subtree(tree, "/src/"); !reflect.DeepEqual(got, want) {
				t.Errorf("extracted %v, want %v", got, want)
			}
		})
	}
}

func TestArchiveKeepsPermissions(t *testing.T) {
	withRealFS(t)
	dir := NewPath(t.TempDir())
	src := dir.Cd("src")
	src.CreateDirectory()
	if err := os.WriteFile(src.Cd("godot").GetOsPath(), []byte("bin"), 0755); err != nil {
		t.Fatal(err)
	}
	src.Tar(dir.Cd("a.tar.gz"))
	dir.Cd("a.tar.gz").Untar(dir.Cd("tar"))
	src.Zip(dir.Cd("a.zip"))
	dir.Cd("a.zip").Unzip(dir.Cd("zip"))
	src.Copy(dir.Cd("copy"))
	for _, d := range []string{"tar", "zip", "copy"} {
		info, err := os.Stat(dir.Cd(d, "godot").GetOsPath())
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0100 == 0 {
			t.Errorf("%s: mode = %v, want executable", d, info.Mode())
		}
	}
}

func TestTarFailureLeavesNoPartialFile(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		m := withMemFS(t, "/", map[string]string{"/src/a.h": "a"})
		// Registered first, so it runs last: after Tar's cleanup.
		Cleanup(func() {
			var paths []string
			for k := range m.tree() {
				paths = append(paths, k)
			}
			sort.Strings(paths)
			fmt.Println(paths)
		})
		m.fail = "Open"
		NewPath("/src").Tar(NewPath("/out.tar.gz"))
		return
	}

	out, code := runFailHelper(t, "TestTarFailureLeavesNoPartialFile")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "[!] Failed to read src/a.h: permission denied.\n[/src/ /src/a.h]\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestUnzipRejectsEscapingEntries(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		zw.Create("../evil")
		zw.Close()
		withMemFS(t, "/", map[string]string{"/a.zip": buf.String()})
		NewPath("/a.zip").Unzip(NewPath("/dst"))
		return
	}

	out, code := runFailHelper(t, "TestUnzipRejectsEscapingEntries")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "[!] Entry \"../evil\" in a.zip points outside of the archive.\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}
