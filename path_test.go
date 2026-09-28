// path_test.go: tests for path.go.

package main

import (
	"os"
	"testing"
)

func TestNewPath(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"already clean", "/a/b/c", "/a/b/c"},
		{"root", "/", "/"},
		{"cleans dot segments", "/a/./b", "/a/b"},
		{"cleans dotdot segments", "/a/b/../c", "/a/c"},
		{"trailing slash removed", "/a/b/", "/a/b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NewPath(c.input); got.absolutePath != c.want {
				t.Errorf("NewPath(%q).absolutePath = %q, want %q", c.input, got.absolutePath, c.want)
			}
		})
	}
}

func TestNewPathRejectsRelative(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		NewPath("rel/path")
		return
	}

	out, code := runFailHelper(t, "TestNewPathRejectsRelative")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "[!] Path \"rel/path\" is not absolute.\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestGetOsPath(t *testing.T) {
	p := Path{absolutePath: "/a/b/c"}
	// filepath.FromSlash is a no-op on this platform's separator being "/",
	// so this only exercises the conversion, not a real separator change.
	if got := p.GetOsPath(); got != "/a/b/c" {
		t.Errorf("GetOsPath() = %q, want %q", got, "/a/b/c")
	}
}

func TestName(t *testing.T) {
	cases := []struct{ input, want string }{
		{"/a/b/c", "c"},
		{"/a", "a"},
		{"/", "/"},
	}
	for _, c := range cases {
		p := Path{absolutePath: c.input}
		if got := p.Name(); got != c.want {
			t.Errorf("Path{%q}.Name() = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestBaseDir(t *testing.T) {
	cases := []struct{ input, want string }{
		{"/a/b/c", "/a/b"},
		{"/a", "/"},
		{"/", "/"},
	}
	for _, c := range cases {
		p := Path{absolutePath: c.input}
		if got := p.BaseDir(); got.absolutePath != c.want {
			t.Errorf("Path{%q}.BaseDir().absolutePath = %q, want %q", c.input, got.absolutePath, c.want)
		}
	}
}

func TestCd(t *testing.T) {
	cases := []struct {
		base     string
		segments []string
		want     string
	}{
		{"/a", []string{"b", "c"}, "/a/b/c"},
		{"/a", nil, "/a"},
		{"/a", []string{"b", "..", "c"}, "/a/c"},
		{"/a/b", []string{"../c"}, "/a/c"},
	}
	for _, c := range cases {
		p := Path{absolutePath: c.base}
		if got := p.Cd(c.segments...); got.absolutePath != c.want {
			t.Errorf("Path{%q}.Cd(%v).absolutePath = %q, want %q", c.base, c.segments, got.absolutePath, c.want)
		}
	}
}
