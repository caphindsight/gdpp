// path.go: Path, an absolute file or directory path stored with forward slashes.

package main

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Path is an absolute file or directory path. Internally it always uses
// forward slashes, regardless of platform.
type Path struct {
	absolutePath string
}

// NewPath returns a Path for p, an absolute path in OS or forward-slash form.
func NewPath(p string) Path {
	p = filepath.ToSlash(p)
	Assert(path.IsAbs(p), "Path %q is not absolute.", p)
	return Path{absolutePath: path.Clean(p)}
}

// GetOsPath returns the path in the current OS's native format.
func (p Path) GetOsPath() string {
	return filepath.FromSlash(p.absolutePath)
}

// Name returns the file or directory name part of the path.
func (p Path) Name() string {
	return path.Base(p.absolutePath)
}

// Windows drive root, e.g. "C:/".
var driveRootPattern = regexp.MustCompile(`^[A-Za-z]:/?$`)

// IsGlobalRoot reports whether the path is a filesystem root: "/" on Unix,
// or a drive root such as "C:/" on Windows.
func (p Path) IsGlobalRoot() bool {
	return p.absolutePath == "/" || driveRootPattern.MatchString(p.absolutePath)
}

// BaseDir returns the parent directory of the path.
func (p Path) BaseDir() Path {
	return Path{absolutePath: path.Dir(p.absolutePath)}
}

// Cd returns the path joined with the given segments.
func (p Path) Cd(segments ...string) Path {
	return Path{absolutePath: path.Join(append([]string{p.absolutePath}, segments...)...)}
}

// Exists reports whether the path exists.
func (p Path) Exists() bool {
	_, err := os.Stat(p.GetOsPath())
	if os.IsNotExist(err) {
		return false
	}
	Check(err, "Failed to stat %s", p.absolutePath)
	return true
}

// IsDir reports whether the path exists and is a directory.
func (p Path) IsDir() bool {
	info, err := os.Stat(p.GetOsPath())
	if os.IsNotExist(err) {
		return false
	}
	Check(err, "Failed to stat %s", p.absolutePath)
	return info.IsDir()
}

// IsFile reports whether the path exists and is a regular file.
func (p Path) IsFile() bool {
	info, err := os.Stat(p.GetOsPath())
	if os.IsNotExist(err) {
		return false
	}
	Check(err, "Failed to stat %s", p.absolutePath)
	return info.Mode().IsRegular()
}

// IsProjectRoot reports whether the path is a directory containing a
// project.godot file.
func (p Path) IsProjectRoot() bool {
	return p.IsDir() && p.Cd(projectFileName).IsFile()
}

// IsPackageRoot reports whether the path is a directory containing a
// gd++pkg.toml file.
func (p Path) IsPackageRoot() bool {
	return p.IsDir() && p.Cd(packageFileName).IsFile()
}

// findRoot walks up from p, returning the first ancestor (including p) that
// satisfies isRoot, or false if none is found before the filesystem root.
func findRoot(p Path, isRoot func(Path) bool) (Path, bool) {
	for {
		if isRoot(p) {
			return p, true
		}
		if p.IsGlobalRoot() {
			return Path{}, false
		}
		p = p.BaseDir()
	}
}

// GetProjectRootMaybe returns the nearest project root containing p, and
// whether one was found.
func GetProjectRootMaybe(p Path) (Path, bool) {
	return findRoot(p, Path.IsProjectRoot)
}

// GetProjectRoot returns the nearest project root containing p, asserting
// that one exists.
func GetProjectRoot(p Path) Path {
	root, ok := GetProjectRootMaybe(p)
	Assert(ok, "Path %s is not contained in a Godot project.", p.absolutePath)
	return root
}

// GetPackageRootMaybe returns the nearest package root containing p, and
// whether one was found.
func GetPackageRootMaybe(p Path) (Path, bool) {
	return findRoot(p, Path.IsPackageRoot)
}

// GetPackageRoot returns the nearest package root containing p, asserting
// that one exists.
func GetPackageRoot(p Path) Path {
	root, ok := GetPackageRootMaybe(p)
	Assert(ok, "Path %s is not contained in a GD++ package.", p.absolutePath)
	return root
}

// Ls returns the children of a directory in lexicographic order, skipping
// entries whose name starts with ".".
func (p Path) Ls() []Path {
	entries, err := os.ReadDir(p.GetOsPath())
	Check(err, "Failed to list %s", p.absolutePath)

	children := make([]Path, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		children = append(children, p.Cd(entry.Name()))
	}
	sort.Slice(children, func(i, j int) bool { return children[i].absolutePath < children[j].absolutePath })
	return children
}
