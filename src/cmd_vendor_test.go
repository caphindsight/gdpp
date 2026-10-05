// cmd_vendor_test.go: tests for cmd_vendor.go. Runs on memFS, in the project
// from project_test.go.

package main

import (
	"fmt"
	"maps"
	"os"
	"reflect"
	"testing"
)

// withVendorFS installs testProjectTree plus extra on memFS, with the project
// root as the working directory, and silences logs.
func withVendorFS(t *testing.T, extra map[string]string) *memFS {
	tree := maps.Clone(testProjectTree)
	maps.Copy(tree, extra)
	withQuiet(t, true)
	return withMemFS(t, "/games/my_game", tree)
}

// testDep is a dep's contents, in withMemFS format relative to its directory.
var testDep = map[string]string{"a.h": "a", ".hidden": "h", "sub/b.h": "b"}

// withPrefix returns tree with prefix prepended to every key.
func withPrefix(prefix string, tree map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range tree {
		out[prefix+k] = v
	}
	return out
}

// wantDep reports an error if the directory at prefix doesn't hold exactly testDep.
func wantDep(t *testing.T, m *memFS, prefix string) {
	t.Helper()
	want := maps.Clone(testDep)
	want["sub/"] = ""
	if got := subtree(m.tree(), prefix); !reflect.DeepEqual(got, want) {
		t.Errorf("%s holds %v, want %v", prefix, got, want)
	}
}

func TestVendorFromDir(t *testing.T) {
	m := withVendorFS(t, withPrefix("/src/", testDep))
	(&CmdVendor{Bind: "4.3", From: "/src"}).Run()
	wantDep(t, m, "/games/my_game/.gd++cache/bind/4.3/")
	if NewPath("/games/my_game/.gd++cache/temp").Exists() {
		t.Errorf("temp directory was not cleaned up")
	}
}

func TestVendorFromDirCheckIn(t *testing.T) {
	extra := withPrefix("/src/", testDep)
	extra["/games/my_game/.gd++cache/bind/4.3/old.h"] = "old"
	m := withVendorFS(t, extra)
	withForce(t, true)
	(&CmdVendor{Bind: "4.3", From: "/src", CheckIn: true}).Run()
	wantDep(t, m, "/games/my_game/_gd++/bind/4.3/")
	if NewPath("/games/my_game/.gd++cache/bind/4.3").Exists() {
		t.Errorf("ephemeral copy of the dep was not moved")
	}
}

func TestVendorToExistingDir(t *testing.T) {
	extra := withPrefix("/games/my_game/_gd++/engine/4.3/", testDep)
	extra["/out/stale"] = "x"
	m := withVendorFS(t, extra)
	withForce(t, true)
	(&CmdVendor{Engine: "4.3", To: "/out"}).Run()
	wantDep(t, m, "/out/")
}

func TestVendorArchiveRoundTrip(t *testing.T) {
	cases := map[string]CmdVendor{
		"/out/dep.tar.gz": {Tar: true},
		"/out/dep.TGZ":    {Tar: true},
		"/out/dep.zip":    {Zip: true},
	}
	for file, c := range cases {
		t.Run(file, func(t *testing.T) {
			m := withVendorFS(t, withPrefix("/games/my_game/.gd++cache/spec/4.3/", testDep))
			out, in := c, c
			out.Spec, out.To = "4.3", file
			out.Run()
			in.Spec, in.From, in.CheckIn = "4.4", file, true
			in.Run()
			wantDep(t, m, "/games/my_game/_gd++/spec/4.4/")
		})
	}
}

func TestVendorWrongExtensionConfirmed(t *testing.T) {
	m := withVendorFS(t, withPrefix("/games/my_game/.gd++cache/spec/4.3/", testDep))
	withForce(t, true)
	(&CmdVendor{Spec: "4.3", To: "/out/dep.zip", Tar: true}).Run()
	(&CmdVendor{Spec: "4.4", From: "/out/dep.zip", Tar: true}).Run()
	wantDep(t, m, "/games/my_game/.gd++cache/spec/4.4/")
}

func TestVendorWrongExtensionDeclined(t *testing.T) {
	if os.Getenv("GDPP_FAIL_HELPER") == "1" {
		isTTY = false
		withVendorFS(t, withPrefix("/games/my_game/.gd++cache/spec/4.3/", testDep))
		withForceNo(t, true)
		Cleanup(func() { fmt.Println("created:", NewPath("/out/dep.tar.gz").Exists()) })
		(&CmdVendor{Spec: "4.3", To: "/out/dep.tar.gz", Zip: true}).Run()
		return
	}

	out, code := runFailHelper(t, "TestVendorWrongExtensionDeclined")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	want := "[?] Path ../../out/dep.tar.gz doesn't end with .zip, use it anyway? [y/n] n\n" +
		"[x] Operation canceled by -n/--no.\ncreated: false\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestVendorInvalidArgs(t *testing.T) {
	cases := map[string]struct {
		c    CmdVendor
		want string
	}{
		"no_dep":        {CmdVendor{From: "/src"}, "exactly one of --bind, --spec and --engine is required"},
		"two_deps":      {CmdVendor{Bind: "a", Spec: "b", From: "/src"}, "exactly one of --bind, --spec and --engine is required"},
		"no_direction":  {CmdVendor{Bind: "a"}, "exactly one of --from and --to is required"},
		"two_direction": {CmdVendor{Bind: "a", From: "/x", To: "/y"}, "exactly one of --from and --to is required"},
		"tar_and_zip":   {CmdVendor{Bind: "a", From: "/x", Tar: true, Zip: true}, "--tar and --zip cannot be used together"},
		"checkin_to":    {CmdVendor{Bind: "a", To: "/x", CheckIn: true}, "--checkin can only be used with --from"},
		"bad_name":      {CmdVendor{Bind: "../a", From: "/x"}, `"../a" is not a valid dependency name`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if os.Getenv("GDPP_FAIL_HELPER") == "1" {
				isTTY = false
				tc.c.Run()
				return
			}
			out, code := runFailHelper(t, t.Name())
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if want := "[x] Invalid arguments: " + tc.want + ".\n"; out != want {
				t.Errorf("output = %q, want %q", out, want)
			}
		})
	}
}
