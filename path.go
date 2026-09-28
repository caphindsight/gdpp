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

// Cwd returns the current working directory as a Path.
func Cwd() Path {
	wd, err := os.Getwd()
	Check(err, "Failed to get current working directory")
	return NewPath(wd)
}

// ParsePath resolves a string into a Path. Absolute paths are used as-is,
// relative paths are resolved against Cwd(), "res://" paths are resolved
// against the project root, and "pkg://" paths are resolved against the
// package root.
func ParsePath(s string) Path {
	if rest, ok := strings.CutPrefix(s, "res://"); ok {
		return GetProjectRoot(Cwd()).Cd(rest)
	}
	if rest, ok := strings.CutPrefix(s, "pkg://"); ok {
		return GetPackageRoot(Cwd()).Cd(rest)
	}
	if filepath.IsAbs(s) {
		return NewPath(s)
	}
	return Cwd().Cd(s)
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
	joined := append([]string{p.absolutePath}, segments...)
	for i, s := range joined {
		joined[i] = filepath.ToSlash(s)
	}
	return Path{absolutePath: path.Join(joined...)}
}

// Exists reports whether the path exists.
func (p Path) Exists() bool {
	_, err := os.Stat(p.GetOsPath())
	if os.IsNotExist(err) {
		return false
	}
	// Not p.ToString(): IsDir/IsFile/Exists are on ToString's own dependency
	// path (via GetProjectRootMaybe), so calling it here would recurse forever.
	Check(err, "Failed to stat path")
	return true
}

// IsDir reports whether the path exists and is a directory.
func (p Path) IsDir() bool {
	info, err := os.Stat(p.GetOsPath())
	if os.IsNotExist(err) {
		return false
	}
	Check(err, "Failed to stat path") // not p.ToString(); see Exists
	return info.IsDir()
}

// IsFile reports whether the path exists and is a regular file.
func (p Path) IsFile() bool {
	info, err := os.Stat(p.GetOsPath())
	if os.IsNotExist(err) {
		return false
	}
	Check(err, "Failed to stat path") // not p.ToString(); see Exists
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
	Assert(ok, "Path %s is not contained in a Godot project.", p.ToString())
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
	Assert(ok, "Path %s is not contained in a GD++ package.", p.ToString())
	return root
}

// ToString returns a project-relative path (res://...) if p is inside a
// project, or otherwise a path relative to the current working directory, to
// avoid leaking absolute paths.
func (p Path) ToString() string {
	if root, ok := GetProjectRootMaybe(p); ok {
		rel, err := filepath.Rel(root.GetOsPath(), p.GetOsPath())
		Check(err, "Failed to compute a relative path") // not ToString(): would recurse into itself
		if rel == "." {
			return "res://"
		}
		return "res://" + filepath.ToSlash(rel)
	}
	rel, err := filepath.Rel(Cwd().GetOsPath(), p.GetOsPath())
	Check(err, "Failed to compute a relative path") // not ToString(): would recurse into itself
	return filepath.ToSlash(rel)
}

// Ls returns the children of a directory in lexicographic order, skipping
// entries whose name starts with ".".
func (p Path) Ls() []Path {
	entries, err := os.ReadDir(p.GetOsPath())
	Check(err, "Failed to list %s", p.ToString())

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

// ReadString returns the contents of the file at p.
func (p Path) ReadString() string {
	data, err := os.ReadFile(p.GetOsPath())
	Check(err, "Failed to read %s", p.ToString())
	return string(data)
}

// WriteString writes text to the file at p, creating it if needed and
// truncating any existing contents.
func (p Path) WriteString(text string) {
	err := os.WriteFile(p.GetOsPath(), []byte(text), 0644)
	Check(err, "Failed to write %s", p.ToString())
}

// CreateFile creates an empty file at p, asserting it doesn't already exist.
func (p Path) CreateFile() {
	Assert(!p.Exists(), "Path %s already exists.", p.ToString())
	f, err := os.Create(p.GetOsPath())
	Check(err, "Failed to create %s", p.ToString())
	Check(f.Close(), "Failed to create %s", p.ToString())
}

// RemoveFile removes the file at p, asserting it is a regular file.
func (p Path) RemoveFile() {
	Assert(p.IsFile(), "Path %s is not a regular file.", p.ToString())
	err := os.Remove(p.GetOsPath())
	Check(err, "Failed to remove %s", p.ToString())
}
