// path.go: Path, an absolute file or directory path stored with forward slashes.

package main

import (
	"path"
	"path/filepath"
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
