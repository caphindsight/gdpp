package main

import (
	"errors"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
)

// CmdInstall installs the tools GD++ needs to build projects, besides the ones
// it manages itself (bindings, API specs and engines), with the platform's
// package manager:
//   - Git 2.25+, to fetch dependencies;
//   - SCons 4.0+ and Python 3.8+, to build;
//   - a C++20 compiler: GCC 10+ or Clang 10+ on Linux, Xcode 14+ on macOS,
//     Visual Studio 2022 (MSVC) on Windows;
//   - on Linux and macOS, MinGW-w64 (GCC 10+, POSIX threads), to build for Windows;
//   - on Linux, pkg-config and the X11, Wayland, OpenGL, ALSA, PulseAudio and
//     udev headers, which engine builds need.
type CmdInstall struct {
	Echo  bool   `arg:"--echo" help:"print the install commands instead of running them"`
	Uname string `arg:"--uname" placeholder:"PLATFORM" help:"install for this platform: debian (Debian 11+, Ubuntu 22.04+ and derivatives), fedora, arch, opensuse (Tumbleweed), macos (with Homebrew), windows (10+, with winget) [default: this one]"`
}

// installPlatforms are the platforms gd++ install supports, each a full name
// followed by its aliases, which include their ids in /etc/os-release.
var installPlatforms = [][]string{
	{"debian", "ubuntu"}, {"fedora"}, {"arch"}, {"opensuse", "opensuse-tumbleweed"}, {"macos", "mac"}, {"windows", "win"},
}

// installCommands are the install commands of each platform.
var installCommands = map[string][][]string{
	"debian": {
		{"sudo", "apt-get", "update"},
		{"sudo", "apt-get", "install", "-y", "git", "scons", "build-essential", "pkg-config", "g++-mingw-w64-x86-64-posix",
			"libx11-dev", "libxcursor-dev", "libxinerama-dev", "libxi-dev", "libxrandr-dev", "libwayland-dev",
			"libgl1-mesa-dev", "libglu1-mesa-dev", "libasound2-dev", "libpulse-dev", "libudev-dev"},
		// MinGW-w64 defaults to Win32 threads, which lack std::thread and std::mutex.
		{"sudo", "update-alternatives", "--set", "x86_64-w64-mingw32-gcc", "/usr/bin/x86_64-w64-mingw32-gcc-posix"},
		{"sudo", "update-alternatives", "--set", "x86_64-w64-mingw32-g++", "/usr/bin/x86_64-w64-mingw32-g++-posix"},
	},
	"fedora": {
		{"sudo", "dnf", "install", "-y", "git", "scons", "gcc-c++", "libstdc++-static", "libatomic-static", "pkgconfig",
			"mingw64-gcc-c++", "mingw64-winpthreads-static",
			"libX11-devel", "libXcursor-devel", "libXinerama-devel", "libXi-devel", "libXrandr-devel", "wayland-devel",
			"mesa-libGL-devel", "mesa-libGLU-devel", "alsa-lib-devel", "pulseaudio-libs-devel", "libudev-devel"},
	},
	"arch": {
		{"sudo", "pacman", "-S", "--needed", "--noconfirm", "git", "scons", "gcc", "pkgconf", "mingw-w64-gcc",
			"libx11", "libxcursor", "libxinerama", "libxi", "libxrandr", "wayland",
			"mesa", "glu", "libglvnd", "alsa-lib", "libpulse"},
	},
	"opensuse": {
		{"sudo", "zypper", "--non-interactive", "install", "git", "scons", "gcc-c++", "pkgconfig", "mingw64-cross-gcc-c++",
			"libX11-devel", "libXcursor-devel", "libXinerama-devel", "libXi-devel", "libXrandr-devel", "wayland-devel",
			"Mesa-libGL-devel", "glu-devel", "alsa-devel", "libpulse-devel", "libudev-devel"},
	},
	// Homebrew installs the Xcode Command Line Tools, which have Git and Clang.
	"macos": {
		{"brew", "install", "scons", "mingw-w64"},
	},
	"windows": {
		{"winget", "install", "-e", "--id", "Git.Git", "--accept-source-agreements", "--accept-package-agreements"},
		{"winget", "install", "-e", "--id", "Python.Python.3.13", "--custom", "PrependPath=1", "--accept-source-agreements", "--accept-package-agreements"},
		{"winget", "install", "-e", "--id", "Microsoft.VisualStudio.2022.BuildTools", "--override",
			"--wait --passive --add Microsoft.VisualStudio.Workload.VCTools --includeRecommended", "--accept-source-agreements", "--accept-package-agreements"},
		{"py", "-m", "pip", "install", "scons"},
	},
}

// wingetInstalled are the exit codes winget returns for packages that are
// already installed and up to date.
var wingetInstalled = []int{0x8A15002B, 0x8A150061}

func (c *CmdInstall) Run() {
	detected := ""
	if c.Uname == "" {
		detected = detectInstallPlatform()
	}
	platform := buildOption("uname", c.Uname, detected, installPlatforms)
	cmds := installCommands[platform]
	if c.Echo {
		text := "# Detected platform: " + platform + ".\n"
		for _, cmd := range cmds {
			text += commandLine(cmd) + "\n"
		}
		PrintResult(text)
		return
	}
	Confirm("Install the build tools for %s, with the commands `gd++ install --echo` prints?", platform)
	for _, cmd := range cmds {
		if cmd[0] == "sudo" && os.Geteuid() == 0 {
			cmd = cmd[1:]
		}
		LogInfo("Running `%s`...", commandLine(cmd))
		// Attached to the terminal, since sudo and installers may ask for input.
		e := exec.Command(cmd[0], cmd[1:]...)
		e.Stdin, e.Stdout, e.Stderr = os.Stdin, os.Stderr, os.Stderr
		err := e.Run()
		var exit *exec.ExitError
		if errors.As(err, &exit) && cmd[0] == "winget" && slices.Contains(wingetInstalled, exit.ExitCode()) {
			continue
		}
		// E.g. Windows' py, which the running process can't see right after installing it.
		Assert(!errors.Is(err, exec.ErrNotFound), "Failed to find %s, install it, or run `gd++ install` again in a new terminal if an earlier step did.", cmd[0])
		Check(err, "Failed to run `%s`", commandLine(cmd))
	}
	LogInfo("Success!")
}

var osReleaseIdPattern = regexp.MustCompile(`(?m)^ID(?:_LIKE)?=["']?([^"'\n]*)`)

// detectInstallPlatform returns the install platform of this machine, going
// by the ids in /etc/os-release on Linux, or "" if unsupported.
func detectInstallPlatform() string {
	if hostPlatform != "linux" {
		return hostPlatform
	}
	release := NewPath("/etc/os-release")
	if !release.IsFile() {
		return ""
	}
	for _, m := range osReleaseIdPattern.FindAllStringSubmatch(release.ReadString(), -1) {
		for _, id := range strings.Fields(m[1]) {
			if name := fullName(installPlatforms, id); name != "" {
				return name
			}
		}
	}
	return ""
}

// commandLine returns cmd as a line to type into a shell, quoting arguments
// with spaces.
func commandLine(cmd []string) string {
	var args []string
	for _, arg := range cmd {
		if strings.Contains(arg, " ") {
			arg = `"` + arg + `"`
		}
		args = append(args, arg)
	}
	return strings.Join(args, " ")
}
