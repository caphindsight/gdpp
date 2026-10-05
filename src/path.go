// path.go: Path, an absolute file or directory path stored with forward slashes.

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// fileSystem is the set of filesystem calls Path makes. Tests swap fsys for
// an in-memory fake.
type fileSystem interface {
	Getwd() (string, error)
	UserHomeDir() (string, error)
	Stat(name string) (fs.FileInfo, error)
	ReadDir(name string) ([]fs.DirEntry, error)
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte, perm fs.FileMode) error
	Mkdir(name string, perm fs.FileMode) error
	MkdirAll(name string, perm fs.FileMode) error
	RemoveAll(name string) error
	Rename(oldName, newName string) error
	Open(name string) (readFile, error)
	Create(name string, perm fs.FileMode) (io.WriteCloser, error)
}

// readFile is a file opened for reading by fileSystem.Open. Zip needs
// ReaderAt and Stat, to read entries in any order.
type readFile interface {
	io.Reader
	io.ReaderAt
	io.Closer
	Stat() (fs.FileInfo, error)
}

// osFS is the real filesystem.
type osFS struct{}

func (osFS) Open(name string) (readFile, error) { return os.Open(name) }
func (osFS) Create(name string, perm fs.FileMode) (io.WriteCloser, error) {
	return os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
}

func (osFS) Getwd() (string, error)                     { return os.Getwd() }
func (osFS) UserHomeDir() (string, error)               { return os.UserHomeDir() }
func (osFS) Stat(name string) (fs.FileInfo, error)      { return os.Stat(name) }
func (osFS) ReadDir(name string) ([]fs.DirEntry, error) { return os.ReadDir(name) }
func (osFS) ReadFile(name string) ([]byte, error)       { return os.ReadFile(name) }
func (osFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	return os.WriteFile(name, data, perm)
}
func (osFS) Mkdir(name string, perm fs.FileMode) error    { return os.Mkdir(name, perm) }
func (osFS) MkdirAll(name string, perm fs.FileMode) error { return os.MkdirAll(name, perm) }
func (osFS) RemoveAll(name string) error                  { return os.RemoveAll(name) }
func (osFS) Rename(oldName, newName string) error         { return os.Rename(oldName, newName) }

var fsys fileSystem = osFS{}

// Path is an absolute file or directory path. Internally it always uses
// forward slashes, regardless of platform.
type Path struct {
	absolutePath string
}

// Windows absolute path prefix, e.g. "C:/".
var drivePathPattern = regexp.MustCompile(`^[A-Za-z]:/`)

// NewPath returns a Path for p, an absolute path in OS or forward-slash form.
func NewPath(p string) Path {
	p = filepath.ToSlash(p)
	Assert(path.IsAbs(p) || drivePathPattern.MatchString(p), "Path %q is not absolute.", p)
	return Path{absolutePath: cleanPath(p)}
}

// cleanPath is path.Clean, but keeps the slash of a drive root ("C:/", not
// "C:", which on Windows means the drive's current directory).
func cleanPath(p string) string {
	p = path.Clean(p)
	if driveRootPattern.MatchString(p) {
		return p[:2] + "/"
	}
	return p
}

// GetOsPath returns the path in the current OS's native format.
func (p Path) GetOsPath() string {
	return filepath.FromSlash(p.absolutePath)
}

// Cwd returns the current working directory as a Path.
func Cwd() Path {
	wd, err := fsys.Getwd()
	Check(err, "Failed to get current working directory")
	return NewPath(wd)
}

// Home returns the user's home directory as a Path.
func Home() Path {
	home, err := fsys.UserHomeDir()
	Check(err, "Failed to get the home directory")
	return NewPath(home)
}

// ParsePath resolves a string into a Path. Absolute paths are used as-is,
// relative paths are resolved against Cwd(), "~" and "~/" paths are resolved
// against Home() ("~\" too on Windows), "res://" paths are resolved against
// the project root, and "pkg://" paths are resolved against the package root.
func ParsePath(s string) Path {
	if rest, ok := strings.CutPrefix(s, "~"); ok && (rest == "" || os.IsPathSeparator(rest[0])) {
		return Home().Cd(rest)
	}
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
	return Path{absolutePath: cleanPath(path.Dir(p.absolutePath))}
}

// Cd returns the path joined with the given segments.
func (p Path) Cd(segments ...string) Path {
	joined := append([]string{p.absolutePath}, segments...)
	for i, s := range joined {
		joined[i] = filepath.ToSlash(s)
	}
	return Path{absolutePath: cleanPath(path.Join(joined...))}
}

// stat returns the info of the path, or nil if it doesn't exist.
func (p Path) stat() fs.FileInfo {
	info, err := fsys.Stat(p.GetOsPath())
	if os.IsNotExist(err) {
		return nil
	}
	// Not p.ToString(): stat is on ToString's own dependency path (via
	// GetProjectRootMaybe), so calling it here would recurse forever.
	Check(err, "Failed to stat path")
	return info
}

// Exists reports whether the path exists.
func (p Path) Exists() bool {
	return p.stat() != nil
}

// IsDir reports whether the path exists and is a directory.
func (p Path) IsDir() bool {
	info := p.stat()
	return info != nil && info.IsDir()
}

// IsFile reports whether the path exists and is a regular file.
func (p Path) IsFile() bool {
	info := p.stat()
	return info != nil && info.Mode().IsRegular()
}

// IsProjectRoot reports whether the path is a directory containing a
// project.godot file.
func (p Path) IsProjectRoot() bool {
	return p.IsDir() && p.Cd(projectFileName).IsFile()
}

// IsPackageRoot reports whether the path is a directory containing a
// .gd++pkg.toml file.
func (p Path) IsPackageRoot() bool {
	return p.IsDir() && p.Cd(packageFileName).IsFile()
}

// IsCacheDir reports whether the path is one of GD++'s cache directories: a
// project's res://_gd++ or res://.gd++proj, or a package's build cache.
func (p Path) IsCacheDir() bool {
	switch p.Name() {
	case checkedInDepsDirName, ephemeralDepsDirName:
		return p.BaseDir().IsProjectRoot()
	case packageBuildCacheDirName:
		return p.BaseDir().IsPackageRoot()
	}
	return false
}

// IgnoreInGodot writes a .gdignore file into the directory at p, if missing,
// so Godot skips the directory. Returns whether it wrote one.
func (p Path) IgnoreInGodot() bool {
	file := p.Cd(gdignoreFileName)
	if file.Exists() {
		return false
	}
	file.WriteString("")
	return true
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
	entries := p.readDir()
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

// IsEmptyDir reports whether p is a directory with no entries, counting
// hidden ones too.
func (p Path) IsEmptyDir() bool {
	return p.IsDir() && len(p.readDir()) == 0
}

// readDir returns the entries of the directory at p, counting hidden ones too.
func (p Path) readDir() []fs.DirEntry {
	entries, err := fsys.ReadDir(p.GetOsPath())
	Check(err, "Failed to list %s", p.ToString())
	return entries
}

// ReadString returns the contents of the file at p.
func (p Path) ReadString() string {
	data, err := fsys.ReadFile(p.GetOsPath())
	Check(err, "Failed to read %s", p.ToString())
	return string(data)
}

// WriteString writes text to the file at p, creating it if needed and
// truncating any existing contents.
func (p Path) WriteString(text string) {
	err := fsys.WriteFile(p.GetOsPath(), []byte(text), 0644)
	Check(err, "Failed to write %s", p.ToString())
}

// open opens the file at p for reading, and returns it with its info. The
// caller must close it.
func (p Path) open() (readFile, fs.FileInfo) {
	f, err := fsys.Open(p.GetOsPath())
	Check(err, "Failed to read %s", p.ToString())
	info, err := f.Stat()
	Check(err, "Failed to stat %s", p.ToString())
	return f, info
}

// writeFrom streams r into the file at p, creating it with perm if needed and
// truncating any existing contents.
func (p Path) writeFrom(r io.Reader, perm fs.FileMode) {
	w, err := fsys.Create(p.GetOsPath(), perm)
	Check(err, "Failed to write %s", p.ToString())
	_, err = io.Copy(w, r)
	if closeErr := w.Close(); err == nil {
		err = closeErr
	}
	Check(err, "Failed to write %s", p.ToString())
}

// GetFileSha256 returns the hex-encoded sha256 sum of the file at p.
func (p Path) GetFileSha256() string {
	f, _ := p.open()
	defer f.Close()
	h := sha256.New()
	_, err := io.Copy(h, f)
	Check(err, "Failed to read %s", p.ToString())
	return hex.EncodeToString(h.Sum(nil))
}

// CreateFile creates an empty file at p, asserting it doesn't already exist.
func (p Path) CreateFile() {
	Assert(!p.Exists(), "Path %s already exists.", p.ToString())
	err := fsys.WriteFile(p.GetOsPath(), nil, 0644)
	Check(err, "Failed to create %s", p.ToString())
}

// CreateDirectory creates an empty directory at p and any missing parents,
// asserting p doesn't already exist.
func (p Path) CreateDirectory() {
	Assert(!p.Exists(), "Path %s already exists.", p.ToString())
	err := fsys.MkdirAll(p.GetOsPath(), 0755)
	Check(err, "Failed to create %s", p.ToString())
	// Godot must never import GD++'s caches, however they get created.
	for dir := p; !dir.IsGlobalRoot(); dir = dir.BaseDir() {
		if dir.IsCacheDir() {
			dir.IgnoreInGodot()
		}
	}
}

// CreateParentDirectory creates the parent directory of p and any missing
// parents, if they don't exist yet.
func (p Path) CreateParentDirectory() {
	if parent := p.BaseDir(); !parent.Exists() {
		parent.CreateDirectory()
	}
}

// Remove deletes the file or directory at p, asserting it exists. Removing a
// non-empty directory drops everything inside it, so this audits first.
func (p Path) Remove() {
	Assert(p.Exists(), "Path %s does not exist.", p.ToString())
	if p.IsDir() && len(p.readDir()) > 0 {
		Audit("Delete %s and everything inside it?", p.ToString())
	}
	err := fsys.RemoveAll(p.GetOsPath())
	Check(err, "Failed to remove %s", p.ToString())
}

// RemoveIfExists deletes the file or directory at p, like Remove, if it
// exists. Returns whether it did.
func (p Path) RemoveIfExists() bool {
	if !p.Exists() {
		return false
	}
	p.Remove()
	return true
}

// Move moves the file or directory at p to another, asserting p exists and
// another doesn't.
func (p Path) Move(another Path) {
	Assert(p.Exists(), "Path %s does not exist.", p.ToString())
	Assert(!another.Exists(), "Path %s already exists.", another.ToString())
	err := fsys.Rename(p.GetOsPath(), another.GetOsPath())
	Check(err, "Failed to move %s to %s", p.ToString(), another.ToString())
}

// Copy copies the file or directory at p to another, asserting p exists and
// another doesn't. Directories are copied recursively.
func (p Path) Copy(another Path) {
	Assert(p.Exists(), "Path %s does not exist.", p.ToString())
	Assert(!another.Exists(), "Path %s already exists.", another.ToString())
	if p.IsDir() {
		copyDir(p, another)
	} else {
		copyFile(p, another)
	}
}

// copyFile copies the regular file at src to dst, keeping its permissions.
func copyFile(src, dst Path) {
	f, info := src.open()
	defer f.Close()
	dst.writeFrom(f, info.Mode().Perm())
}

// copyDir recursively copies the directory at src to dst.
func copyDir(src, dst Path) {
	err := fsys.Mkdir(dst.GetOsPath(), 0755)
	Check(err, "Failed to create %s", dst.ToString())
	for _, entry := range src.readDir() {
		childSrc, childDst := src.Cd(entry.Name()), dst.Cd(entry.Name())
		if entry.IsDir() {
			copyDir(childSrc, childDst)
		} else {
			copyFile(childSrc, childDst)
		}
	}
}

// Sync copies the file or directory at p to another, like Copy, but merges
// into a destination that may already exist: files are only overwritten
// when their contents differ, and destination entries missing from p are
// deleted.
func (p Path) Sync(to Path) {
	Assert(p.Exists(), "Path %s does not exist.", p.ToString())
	syncPath(p, to)
}

// syncPath merges src into dst, recursing into matching directories.
func syncPath(src, dst Path) {
	if src.IsDir() {
		syncDir(src, dst)
	} else {
		syncFile(src, dst)
	}
}

// syncFile copies the regular file at src to dst if dst is missing or its
// contents differ. Anything else at dst is removed first.
func syncFile(src, dst Path) {
	if dst.IsDir() {
		dst.Remove()
	}
	if !dst.Exists() || src.GetFileSha256() != dst.GetFileSha256() {
		copyFile(src, dst)
	}
}

// syncDir merges the directory at src into dst, creating dst if needed,
// syncing each entry of src, and deleting dst entries absent from src.
// Anything other than a directory at dst is removed first.
func syncDir(src, dst Path) {
	if dst.IsFile() {
		dst.Remove()
	}
	if !dst.Exists() {
		err := fsys.Mkdir(dst.GetOsPath(), 0755)
		Check(err, "Failed to create %s", dst.ToString())
	}

	srcEntries := src.readDir()
	srcNames := make(map[string]bool, len(srcEntries))
	for _, entry := range srcEntries {
		srcNames[entry.Name()] = true
		syncPath(src.Cd(entry.Name()), dst.Cd(entry.Name()))
	}

	for _, entry := range dst.readDir() {
		if !srcNames[entry.Name()] {
			dst.Cd(entry.Name()).Remove()
		}
	}
}
