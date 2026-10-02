// test_util_test.go: shared helpers for tests in this package.

package main

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMain sets Args defaults that arg.MustParse would normally fill in, since
// tests never call it. It also sets fsys to nil, so a test that touches the
// filesystem without withMemFS panics instead of using the real disk, and
// isTTY to false, so `go test` in a terminal never waits in the pager.
func TestMain(m *testing.M) {
	Args.LogDepth = 4
	Args.TabWidth = 2
	fsys = nil
	isTTY = false
	os.Exit(m.Run())
}

// memNode is a file or directory in memFS.
type memNode struct {
	dir  bool
	data []byte
}

// memInfo implements fs.FileInfo for a memNode.
type memInfo struct {
	name string
	node *memNode
}

func (i memInfo) Name() string       { return i.name }
func (i memInfo) Size() int64        { return int64(len(i.node.data)) }
func (i memInfo) ModTime() time.Time { return time.Time{} }
func (i memInfo) IsDir() bool        { return i.node.dir }
func (i memInfo) Sys() any           { return nil }
func (i memInfo) Mode() fs.FileMode {
	if i.node.dir {
		return fs.ModeDir | 0755
	}
	return 0644
}

// memFS is an in-memory fileSystem. Keys of nodes are clean absolute
// forward-slash paths. If fail names a method, that method returns
// fs.ErrPermission.
type memFS struct {
	cwd   string
	home  string
	nodes map[string]*memNode
	fail  string
}

func (m *memFS) err(op, name string, err error) error {
	return &fs.PathError{Op: op, Path: name, Err: err}
}

// check returns the injected error for op, if any.
func (m *memFS) check(op string) error {
	if m.fail == op {
		return fs.ErrPermission
	}
	return nil
}

// parentIsDir reports whether name's parent directory exists.
func (m *memFS) parentIsDir(name string) bool {
	n := m.nodes[path.Dir(name)]
	return n != nil && n.dir
}

func (m *memFS) Getwd() (string, error) {
	if err := m.check("Getwd"); err != nil {
		return "", err
	}
	return m.cwd, nil
}

func (m *memFS) UserHomeDir() (string, error) {
	if err := m.check("UserHomeDir"); err != nil {
		return "", err
	}
	return m.home, nil
}

func (m *memFS) Stat(name string) (fs.FileInfo, error) {
	if err := m.check("Stat"); err != nil {
		return nil, err
	}
	n := m.nodes[name]
	if n == nil {
		return nil, m.err("stat", name, fs.ErrNotExist)
	}
	return memInfo{path.Base(name), n}, nil
}

func (m *memFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if err := m.check("ReadDir"); err != nil {
		return nil, err
	}
	n := m.nodes[name]
	if n == nil {
		return nil, m.err("readdir", name, fs.ErrNotExist)
	}
	if !n.dir {
		return nil, m.err("readdir", name, syscall.ENOTDIR)
	}
	var entries []fs.DirEntry
	for k, child := range m.nodes {
		if k != "/" && path.Dir(k) == name {
			entries = append(entries, fs.FileInfoToDirEntry(memInfo{path.Base(k), child}))
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

func (m *memFS) ReadFile(name string) ([]byte, error) {
	if err := m.check("ReadFile"); err != nil {
		return nil, err
	}
	n := m.nodes[name]
	if n == nil {
		return nil, m.err("open", name, fs.ErrNotExist)
	}
	if n.dir {
		return nil, m.err("read", name, syscall.EISDIR)
	}
	return append([]byte(nil), n.data...), nil
}

func (m *memFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	if err := m.check("WriteFile"); err != nil {
		return err
	}
	if !m.parentIsDir(name) {
		return m.err("open", name, fs.ErrNotExist)
	}
	if n := m.nodes[name]; n != nil && n.dir {
		return m.err("open", name, syscall.EISDIR)
	}
	m.nodes[name] = &memNode{data: append([]byte(nil), data...)}
	return nil
}

func (m *memFS) Mkdir(name string, perm fs.FileMode) error {
	if err := m.check("Mkdir"); err != nil {
		return err
	}
	if m.nodes[name] != nil {
		return m.err("mkdir", name, fs.ErrExist)
	}
	if !m.parentIsDir(name) {
		return m.err("mkdir", name, fs.ErrNotExist)
	}
	m.nodes[name] = &memNode{dir: true}
	return nil
}

func (m *memFS) MkdirAll(name string, perm fs.FileMode) error {
	if err := m.check("MkdirAll"); err != nil {
		return err
	}
	if n := m.nodes[name]; n != nil {
		if n.dir {
			return nil
		}
		return m.err("mkdir", name, syscall.ENOTDIR)
	}
	if name != "/" {
		if err := m.MkdirAll(path.Dir(name), perm); err != nil {
			return err
		}
	}
	m.nodes[name] = &memNode{dir: true}
	return nil
}

func (m *memFS) RemoveAll(name string) error {
	if err := m.check("RemoveAll"); err != nil {
		return err
	}
	for k := range m.nodes {
		if k == name || strings.HasPrefix(k, name+"/") {
			delete(m.nodes, k)
		}
	}
	return nil
}

func (m *memFS) Rename(oldName, newName string) error {
	if err := m.check("Rename"); err != nil {
		return err
	}
	if m.nodes[oldName] == nil {
		return m.err("rename", oldName, fs.ErrNotExist)
	}
	if m.nodes[newName] != nil {
		return m.err("rename", newName, fs.ErrExist)
	}
	if !m.parentIsDir(newName) {
		return m.err("rename", newName, fs.ErrNotExist)
	}
	for k, n := range m.nodes {
		if rest, ok := strings.CutPrefix(k, oldName); ok && (rest == "" || rest[0] == '/') {
			delete(m.nodes, k)
			m.nodes[newName+rest] = n
		}
	}
	return nil
}

// memFile is a memFS file opened for reading, over its contents when opened.
type memFile struct {
	*bytes.Reader
	info memInfo
}

func (f memFile) Close() error               { return nil }
func (f memFile) Stat() (fs.FileInfo, error) { return f.info, nil }

func (m *memFS) Open(name string) (readFile, error) {
	if err := m.check("Open"); err != nil {
		return nil, err
	}
	n := m.nodes[name]
	if n == nil {
		return nil, m.err("open", name, fs.ErrNotExist)
	}
	if n.dir {
		return nil, m.err("read", name, syscall.EISDIR)
	}
	return memFile{bytes.NewReader(n.data), memInfo{path.Base(name), n}}, nil
}

// memWriter is a memFS file opened for writing. Its contents are saved on Close.
type memWriter struct {
	bytes.Buffer
	node *memNode
}

func (w *memWriter) Close() error {
	w.node.data = w.Bytes()
	return nil
}

// Create creates or truncates the file right away, like os.OpenFile.
func (m *memFS) Create(name string, perm fs.FileMode) (io.WriteCloser, error) {
	if err := m.check("Create"); err != nil {
		return nil, err
	}
	if !m.parentIsDir(name) {
		return nil, m.err("open", name, fs.ErrNotExist)
	}
	if n := m.nodes[name]; n != nil && n.dir {
		return nil, m.err("open", name, syscall.EISDIR)
	}
	n := &memNode{}
	m.nodes[name] = n
	return &memWriter{node: n}, nil
}

// tree returns the filesystem contents in the format withMemFS takes, except
// that "/" is left out.
func (m *memFS) tree() map[string]string {
	t := map[string]string{}
	for k, n := range m.nodes {
		switch {
		case k == "/":
		case n.dir:
			t[k+"/"] = ""
		default:
			t[k] = string(n.data)
		}
	}
	return t
}

// withMemFS installs an in-memory fileSystem for the duration of a test, with
// cwd as the working directory. Each key in tree is an absolute path: a key
// ending in "/" is a directory, other keys are files with the value as their
// contents. Missing parent directories are created.
func withMemFS(t *testing.T, cwd string, tree map[string]string) *memFS {
	m := &memFS{cwd: cwd, home: "/home/user", nodes: map[string]*memNode{"/": {dir: true}}}
	mkdirAll := func(p string) {
		for ; m.nodes[p] == nil; p = path.Dir(p) {
			m.nodes[p] = &memNode{dir: true}
		}
	}
	mkdirAll(cwd)
	for k, v := range tree {
		if strings.HasSuffix(k, "/") {
			mkdirAll(path.Clean(k))
		} else {
			mkdirAll(path.Dir(k))
			m.nodes[k] = &memNode{data: []byte(v)}
		}
	}
	orig := fsys
	fsys = m
	t.Cleanup(func() { fsys = orig })
	return m
}

// withRealFS installs the real, disk-backed fileSystem for the duration of a
// test, for tests (like Exec's) that run real subprocesses against real
// directories, so memFS's fake paths can't be used.
func withRealFS(t *testing.T) {
	orig := fsys
	fsys = osFS{}
	t.Cleanup(func() { fsys = orig })
}

// withTTY sets isTTY for the duration of a test and restores the prior value.
// Tests in this file must not call t.Parallel(): they mutate this package-level var.
func withTTY(t *testing.T, tty bool) {
	orig := isTTY
	isTTY = tty
	t.Cleanup(func() { isTTY = orig })
}

// withUnicode sets isUnicode for the duration of a test and restores the prior value.
func withUnicode(t *testing.T, unicode bool) {
	orig := isUnicode
	isUnicode = unicode
	t.Cleanup(func() { isUnicode = orig })
}

// withForce sets Args.Force for the duration of a test and restores the prior value.
func withForce(t *testing.T, force bool) {
	orig := Args.Force
	Args.Force = force
	t.Cleanup(func() { Args.Force = orig })
}

// withQuiet sets Args.Quiet for the duration of a test and restores the prior value.
func withQuiet(t *testing.T, quiet bool) {
	orig := Args.Quiet
	Args.Quiet = quiet
	t.Cleanup(func() { Args.Quiet = orig })
}

// withForceNo sets Args.ForceNo for the duration of a test and restores the prior value.
func withForceNo(t *testing.T, forceNo bool) {
	orig := Args.ForceNo
	Args.ForceNo = forceNo
	t.Cleanup(func() { Args.ForceNo = orig })
}

// withAudit sets Args.Audit for the duration of a test and restores the prior value.
func withAudit(t *testing.T, audit bool) {
	orig := Args.Audit
	Args.Audit = audit
	t.Cleanup(func() { Args.Audit = orig })
}

// withStdin replaces os.Stdin with a pipe fed with content, for the duration of a test.
func withStdin(t *testing.T, content string) {
	orig := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	go func() {
		io.WriteString(w, content)
		w.Close()
	}()
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })
}

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what it wrote.
func captureStdout(t *testing.T, fn func()) string {
	return capture(t, &os.Stdout, fn)
}

// captureStderr runs fn with os.Stderr redirected to a pipe and returns what
// it wrote: the logs.
func captureStderr(t *testing.T, fn func()) string {
	return capture(t, &os.Stderr, fn)
}

func capture(t *testing.T, f **os.File, fn func()) string {
	orig := *f
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	*f = w
	// Read while fn runs, since a pipe only buffers so much: a long output would block fn.
	done := make(chan []byte)
	go func() {
		out, _ := io.ReadAll(r)
		done <- out
	}()
	fn()
	w.Close()
	*f = orig
	return string(<-done)
}

// runFailHelper re-execs the test binary to run only the test (or subtest)
// called name, since Fail calls os.Exit and would otherwise kill the test
// process.
func runFailHelper(t *testing.T, name string) (output string, exitCode int) {
	return runFailHelperWithStdin(t, name, "")
}

// runFailHelperWithStdin is like runFailHelper, but feeds stdin to the child process.
func runFailHelperWithStdin(t *testing.T, name string, stdin string) (output string, exitCode int) {
	// -test.run matches each "/"-separated level separately, so anchor every level.
	cmd := exec.Command(os.Args[0], "-test.run=^"+strings.ReplaceAll(name, "/", "$/^")+"$")
	cmd.Env = append(os.Environ(), "GDPP_FAIL_HELPER=1")
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput() // logs go to stderr, results to stdout
	if err == nil {
		t.Fatalf("%s: process exited 0, want nonzero", name)
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("%s: cmd.CombinedOutput: %v", name, err)
	}
	return string(out), exitErr.ExitCode()
}

// ptr returns a pointer to a copy of v, e.g. for optional flags.
func ptr[T any](v T) *T { return &v }
