// cmd_install_test.go: tests for cmd_install.go. Only --echo runs, since
// running would install packages.

package main

import (
	"os"
	"strings"
	"testing"
)

// withHostPlatform sets hostPlatform for the duration of a test.
func withHostPlatform(t *testing.T, platform string) {
	orig := hostPlatform
	hostPlatform = platform
	t.Cleanup(func() { hostPlatform = orig })
}

func TestInstallEcho(t *testing.T) {
	out := captureStdout(t, (&CmdInstall{Echo: true, Uname: "mac"}).Run)
	if want := "# Detected platform: macos.\nbrew install scons mingw-w64\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestInstallEchoQuotes(t *testing.T) {
	out := captureStdout(t, (&CmdInstall{Echo: true, Uname: "windows"}).Run)
	want := `--override "--wait --passive --add Microsoft.VisualStudio.Workload.VCTools --includeRecommended"`
	if !strings.Contains(out, want) {
		t.Errorf("output = %q, want it to contain %q", out, want)
	}
}

func TestInstallCommands(t *testing.T) {
	for _, names := range installPlatforms {
		if len(installCommands[names[0]]) == 0 {
			t.Errorf("platform %s has no install commands", names[0])
		}
	}
	if len(installCommands) != len(installPlatforms) {
		t.Errorf("%d platforms have install commands, want %d", len(installCommands), len(installPlatforms))
	}
}

func TestDetectInstallPlatform(t *testing.T) {
	cases := map[string]string{
		"ID=ubuntu\nID_LIKE=debian\n":                                    "debian",
		"NAME=\"Linux Mint\"\nID=linuxmint\nID_LIKE=\"ubuntu debian\"\n": "debian",
		"ID=fedora\n":                "fedora",
		"ID=manjaro\nID_LIKE=arch\n": "arch",
		"ID=\"opensuse-tumbleweed\"\nID_LIKE=\"opensuse suse\"\n": "opensuse",
		"ID=gentoo\n": "",
	}
	withHostPlatform(t, "linux")
	for release, want := range cases {
		withMemFS(t, "/", map[string]string{"/etc/os-release": release})
		if got := detectInstallPlatform(); got != want {
			t.Errorf("detectInstallPlatform() with %q = %q, want %q", release, got, want)
		}
	}
	withMemFS(t, "/", nil)
	if got := detectInstallPlatform(); got != "" {
		t.Errorf("detectInstallPlatform() without /etc/os-release = %q, want \"\"", got)
	}
	withHostPlatform(t, "macos")
	if got := detectInstallPlatform(); got != "macos" {
		t.Errorf("detectInstallPlatform() on macOS = %q, want macos", got)
	}
}

func TestInstallInvalidArgs(t *testing.T) {
	cases := map[string]struct {
		c    CmdInstall
		host string
		want string
	}{
		"uname":  {CmdInstall{Echo: true, Uname: "beos"}, "linux", "[x] Invalid arguments: --uname must be one of debian, fedora, arch, opensuse, macos, windows.\n"},
		"detect": {CmdInstall{Echo: true}, "", "[x] Failed to detect the uname, use --uname to choose one.\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if os.Getenv("GDPP_FAIL_HELPER") == "1" {
				isTTY = false
				hostPlatform = tc.host
				tc.c.Run()
				return
			}
			out, code := runFailHelper(t, t.Name())
			if code != 1 || out != tc.want {
				t.Errorf("exit code = %d, output = %q, want 1, %q", code, out, tc.want)
			}
		})
	}
}
