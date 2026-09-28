// path.go: Path, an absolute file or directory path stored with forward slashes.

package main

import (
	"os"
	"path"
	"path/filepath"
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
	Check(err, "failed to stat %s", p.absolutePath)
	return true
}

// IsDir reports whether the path exists and is a directory.
func (p Path) IsDir() bool {
	info, err := os.Stat(p.GetOsPath())
	if os.IsNotExist(err) {
		return false
	}
	Check(err, "failed to stat %s", p.absolutePath)
	return info.IsDir()
}

// IsFile reports whether the path exists and is a regular file.
func (p Path) IsFile() bool {
	info, err := os.Stat(p.GetOsPath())
	if os.IsNotExist(err) {
		return false
	}
	Check(err, "failed to stat %s", p.absolutePath)
	return info.Mode().IsRegular()
}

// Ls returns the children of a directory in lexicographic order, skipping
// entries whose name starts with ".".
func (p Path) Ls() []Path {
	entries, err := os.ReadDir(p.GetOsPath())
	Check(err, "failed to list %s", p.absolutePath)

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
