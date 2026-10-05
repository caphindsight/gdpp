// project_dep_cache_test.go: tests for project_dep_cache.go. Runs on memFS.

package main

import (
	"cmp"
	"reflect"
	"testing"
)

func TestProjectDepCache(t *testing.T) {
	withMemFS(t, "/", map[string]string{
		"/p/_gd++/bind/1.0/a.h":      "a",
		"/p/.gd++cache/bind/2.0/b.h": "b",
		"/p/_gd++/bind/stray":        "not a dep",
	})
	c := newProjectDepCache(NewPath("/p"), DepKind{"bind", "bind", "bind"})

	if got, want := c.Ls(), []string{"1.0", "2.0"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Ls() = %v, want %v", got, want)
	}
	if !c.Has("1.0") || !c.Has("2.0") || c.Has("3.0") || c.Has("stray") {
		t.Errorf("Has() gave wrong results")
	}
	if !c.IsCheckedIn("1.0") || c.IsEphemeral("1.0") || c.IsCheckedIn("2.0") || !c.IsEphemeral("2.0") {
		t.Errorf("IsCheckedIn()/IsEphemeral() gave wrong results")
	}

	c.CheckIn("2.0")
	c.CheckIn("2.0") // no-op
	if !c.IsCheckedIn("2.0") || c.IsEphemeral("2.0") {
		t.Errorf("CheckIn() did not move 2.0")
	}
	if got := NewPath("/p/_gd++/bind/2.0/b.h").ReadString(); got != "b" {
		t.Errorf("b.h = %q, want %q", got, "b")
	}

	c.MakeEphemeral("1.0")
	if c.IsCheckedIn("1.0") || !c.IsEphemeral("1.0") {
		t.Errorf("MakeEphemeral() did not move 1.0")
	}

	c.Remove("1.0")
	if got, want := c.Ls(), []string{"2.0"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Ls() after Remove = %v, want %v", got, want)
	}
}

func TestCompareDepNames(t *testing.T) {
	// Each name must sort strictly before the next.
	sorted := []string{
		"a",
		"b-1",
		"4.3",
		"4.3-beta",
		"4.3-stable",
		"4.3.1-stable",
		"4.3-2",
		"4.10-dev",
		"10.0.0-stable",
	}
	for i := range sorted {
		for j := range sorted {
			if got, want := compareDepNames(sorted[i], sorted[j]), cmp.Compare(i, j); got != want {
				t.Errorf("compareDepNames(%q, %q) = %d, want %d", sorted[i], sorted[j], got, want)
			}
		}
	}
}

func TestProjectDepCacheDeletesDuplicate(t *testing.T) {
	withMemFS(t, "/", map[string]string{
		"/p/_gd++/bind/1.0/a.h":      "checked in",
		"/p/.gd++cache/bind/1.0/a.h": "ephemeral",
		"/p/_gd++/bind/2.0/a.h":      "checked in",
		"/p/.gd++cache/bind/2.0/a.h": "ephemeral",
	})
	c := newProjectDepCache(NewPath("/p"), DepKind{"bind", "bind", "bind"})

	c.CheckIn("1.0")
	c.MakeEphemeral("2.0")
	if c.IsEphemeral("1.0") || c.IsCheckedIn("2.0") {
		t.Errorf("copy in the wrong cache was not deleted")
	}
	for path, want := range map[string]string{"/p/_gd++/bind/1.0/a.h": "checked in", "/p/.gd++cache/bind/2.0/a.h": "ephemeral"} {
		if got := NewPath(path).ReadString(); got != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
}

func TestProjectDepCacheCreatesMissingDirs(t *testing.T) {
	withMemFS(t, "/", map[string]string{"/p/_gd++/spec/4.3/api.json": "{}"})
	c := newProjectDepCache(NewPath("/p"), DepKind{"spec", "spec", "spec"})

	c.MakeEphemeral("4.3")
	if !c.IsEphemeral("4.3") {
		t.Errorf("MakeEphemeral() did not create the missing ephemeral directory")
	}
}
