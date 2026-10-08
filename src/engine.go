// engine.go: Godot engines, which gd++ test compiles from engine dependencies
// to run tests.

package main

import (
	"strconv"
	"strings"
)

// engineBuildDir returns where gd++ test compiles the engine dependency name of
// cache, the cache of engines: a copy of its source in
// res://.gd++cache/godot/NAME, since builds never change the caches.
func engineBuildDir(cache ProjectDepCache, name string) Path {
	return cache.EphemeralDir.BaseDir().Cd(engineBuildsDirName, name)
}

// compileEngine returns a Godot binary for this machine, compiled from the
// project's engine dependency name the first time, with SCons running jobs
// compile jobs at once (0: its default). The binary is an editor, which can
// run projects and import their files, without a window or sound: it needs no
// desktop libraries.
func compileEngine(p Project, name string, jobs int) Path {
	cache := p.Caches[2]
	Assert(cache.Has(name), "Missing Godot engine %s, run `gd++ fetch --missing` to fix this.", name)
	dir := engineBuildDir(cache, name)
	if bin, ok := engineBinary(dir); ok {
		return bin
	}
	assertScons()
	LogInfo("Compiling Godot engine %s for this machine. This takes a while, but only once.", name)
	dir.CreateDirectory()
	dir.BaseDir().BaseDir().IgnoreInGodot()
	s := Silence()
	t := LogTask("Copying Godot engine %s...", name)
	cache.GetPath(name).Sync(dir)
	t.Done()
	s.End()
	platform := map[string]string{"linux": "linuxbsd", "macos": "macos", "windows": "windows"}[hostPlatform]
	args := []string{"platform=" + platform, "arch=" + hostArch, "target=editor", "tests=no"}
	if hostPlatform == "linux" {
		args = append(args, "x11=no", "wayland=no", "alsa=no", "pulseaudio=no", "dbus=no", "speechd=no", "fontconfig=no", "udev=no")
	}
	if jobs > 0 {
		args = append(args, "-j"+strconv.Itoa(jobs))
	}
	Exec("Compiling Godot engine "+name+"...", dir, "scons", args...)
	bin, ok := engineBinary(dir)
	Assert(ok, "Failed to find the Godot binary in %s after compiling it.", dir.Cd("bin").ToString())
	return bin
}

// engineBinary returns the editor binary that compiling Godot's source in dir
// made, if any, e.g. bin/godot.linuxbsd.editor.x86_64. On Windows, it's the
// console one, whose output gd++ can read.
func engineBinary(dir Path) (Path, bool) {
	if !dir.Cd("bin").IsDir() {
		return Path{}, false
	}
	for _, f := range dir.Cd("bin").Ls() {
		name := f.Name()
		if !strings.HasPrefix(name, "godot.") || !strings.Contains(name, ".editor.") || !f.IsFile() {
			continue
		}
		if hostPlatform == "windows" && strings.HasSuffix(name, ".console.exe") ||
			hostPlatform != "windows" && !strings.Contains(name[len("godot."):], ".exe") && !strings.HasSuffix(name, ".pdb") {
			return f, true
		}
	}
	return Path{}, false
}
