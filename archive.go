// archive.go: packing directories into .tar.gz and .zip files, and back. All
// data is streamed, so memory use doesn't grow with file or archive size.

package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"io/fs"
	"path"
	"path/filepath"
)

// walkTree calls fn for every file and directory under dir, parents before
// children, with its path relative to dir. Unlike Ls, dotfiles are included.
// For files, r streams the contents and info comes from the opened file; for
// directories, r is nil.
func walkTree(dir Path, fn func(rel string, p Path, info fs.FileInfo, r io.Reader)) {
	var walk func(rel string)
	walk = func(rel string) {
		entries, err := fsys.ReadDir(dir.Cd(rel).GetOsPath())
		Check(err, "Failed to list %s", dir.Cd(rel).ToString())
		for _, entry := range entries {
			childRel, child := path.Join(rel, entry.Name()), dir.Cd(rel, entry.Name())
			if entry.IsDir() {
				info, err := entry.Info()
				Check(err, "Failed to stat %s", child.ToString())
				fn(childRel, child, info, nil)
				walk(childRel)
				continue
			}
			f, info := child.open()
			fn(childRel, child, info, f)
			f.Close()
		}
	}
	walk("")
}

// writeAtomically calls write with a writer to a temporary file next to dst,
// then moves that file to dst, so a failure never leaves a partial dst.
func writeAtomically(dst Path, write func(w io.Writer)) {
	tmp := dst.BaseDir().Cd("." + dst.Name() + ".part")
	Cleanup(func() { fsys.RemoveAll(tmp.GetOsPath()) })
	w, err := fsys.Create(tmp.GetOsPath(), 0644)
	Check(err, "Failed to write %s", dst.ToString())
	write(w)
	Check(w.Close(), "Failed to write %s", dst.ToString())
	Check(fsys.Rename(tmp.GetOsPath(), dst.GetOsPath()), "Failed to write %s", dst.ToString())
}

// Tar packs the contents of the directory at p into a gzip-compressed tarball
// at dst, keeping file permissions. dst must not be a directory.
func (p Path) Tar(dst Path) {
	writeAtomically(dst, func(w io.Writer) {
		gz := gzip.NewWriter(w)
		tw := tar.NewWriter(gz)
		walkTree(p, func(rel string, f Path, info fs.FileInfo, r io.Reader) {
			hdr := &tar.Header{Name: rel, Mode: int64(info.Mode().Perm()), ModTime: info.ModTime(), Size: info.Size(), Typeflag: tar.TypeReg}
			if r == nil {
				hdr.Name, hdr.Size, hdr.Typeflag = rel+"/", 0, tar.TypeDir
			}
			Check(tw.WriteHeader(hdr), "Failed to archive %s", f.ToString())
			if r != nil {
				_, err := io.Copy(tw, r)
				Check(err, "Failed to archive %s", f.ToString())
			}
		})
		Check(tw.Close(), "Failed to write %s", dst.ToString())
		Check(gz.Close(), "Failed to write %s", dst.ToString())
	})
}

// Untar extracts the gzip-compressed tarball at p into the directory dst,
// creating it if needed.
func (p Path) Untar(dst Path) {
	f, _ := p.open()
	defer f.Close()
	gz, err := gzip.NewReader(f)
	Check(err, "Failed to read %s", p.ToString())
	tr := tar.NewReader(gz)
	Check(fsys.MkdirAll(dst.GetOsPath(), 0755), "Failed to create %s", dst.ToString())
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return
		}
		Check(err, "Failed to read %s", p.ToString())
		isDir := hdr.Typeflag == tar.TypeDir
		Assert(isDir || hdr.Typeflag == tar.TypeReg, "Unsupported entry %q in %s.", hdr.Name, p.ToString())
		extractEntry(p, dst, hdr.Name, isDir, fs.FileMode(hdr.Mode), tr)
	}
}

// Zip packs the contents of the directory at p into a zip file at dst,
// keeping file permissions. dst must not be a directory.
func (p Path) Zip(dst Path) {
	writeAtomically(dst, func(w io.Writer) {
		zw := zip.NewWriter(w)
		walkTree(p, func(rel string, f Path, info fs.FileInfo, r io.Reader) {
			hdr := &zip.FileHeader{Name: rel, Method: zip.Deflate, Modified: info.ModTime()}
			hdr.SetMode(info.Mode())
			if r == nil {
				hdr.Name, hdr.Method = rel+"/", zip.Store
			}
			zf, err := zw.CreateHeader(hdr)
			Check(err, "Failed to archive %s", f.ToString())
			if r != nil {
				_, err = io.Copy(zf, r)
				Check(err, "Failed to archive %s", f.ToString())
			}
		})
		Check(zw.Close(), "Failed to write %s", dst.ToString())
	})
}

// Unzip extracts the zip file at p into the directory dst, creating it if
// needed.
func (p Path) Unzip(dst Path) {
	f, info := p.open()
	defer f.Close()
	zr, err := zip.NewReader(f, info.Size())
	Check(err, "Failed to read %s", p.ToString())
	Check(fsys.MkdirAll(dst.GetOsPath(), 0755), "Failed to create %s", dst.ToString())
	for _, zf := range zr.File {
		r, err := zf.Open()
		Check(err, "Failed to read %s", p.ToString())
		extractEntry(p, dst, zf.Name, zf.FileInfo().IsDir(), zf.Mode(), r)
		r.Close()
	}
}

// extractEntry writes the entry called name of the archive at archive into
// dst, streaming its contents from r, and asserting it doesn't point outside
// of dst.
func extractEntry(archive, dst Path, name string, isDir bool, mode fs.FileMode, r io.Reader) {
	Assert(filepath.IsLocal(name), "Entry %q in %s points outside of the archive.", name, archive.ToString())
	target := dst.Cd(name)
	if isDir {
		Check(fsys.MkdirAll(target.GetOsPath(), 0755), "Failed to create %s", target.ToString())
		return
	}
	Check(fsys.MkdirAll(target.BaseDir().GetOsPath(), 0755), "Failed to create %s", target.BaseDir().ToString())
	target.writeFrom(r, mode.Perm())
}
