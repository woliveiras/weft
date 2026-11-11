package localinstall

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const backupHeader = ".weft-backup.json"

type backupManifest struct {
	Version int    `json:"version"`
	Root    string `json:"root"`
}
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}

// Backup requires the native instance lock. Symlinks are archived as links and
// never followed. A failed archive remains explicitly partial, never successful.
func Backup(ctx context.Context, root, destination string) error {
	_, release, err := Open(root)
	if err != nil {
		return err
	}
	defer release()
	destination, err = filepath.Abs(destination)
	if err != nil {
		return err
	}
	if rel, e := filepath.Rel(root, destination); e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("backup destination must be outside the profile")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(destination))
	if err != nil {
		return err
	}
	destination = filepath.Join(parent, filepath.Base(destination))
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	if rel, e := filepath.Rel(canonicalRoot, destination); e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("backup destination must be outside the profile")
	}
	partial := destination + ".partial"
	f, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	finish := func() error { return errors.Join(tw.Close(), gz.Close(), f.Close()) }
	b, err := json.Marshal(backupManifest{Version: formatVersion, Root: root})
	if err != nil {
		_ = finish()
		return err
	}
	if err = tw.WriteHeader(&tar.Header{Name: backupHeader, Mode: 0600, Size: int64(len(b)), Typeflag: tar.TypeReg}); err == nil {
		_, err = tw.Write(b)
	}
	if err != nil {
		_ = finish()
		return err
	}
	source, err := os.OpenRoot(root)
	if err != nil {
		_ = finish()
		return err
	}
	defer source.Close()
	err = fs.WalkDir(source.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if name == "." || name == "instance.lock" {
			return nil
		}
		if name == backupHeader {
			return errors.New("reserved backup metadata path exists")
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			if !strings.HasPrefix(name, "workspaces/") {
				return errors.New("symlinks outside workspaces are not supported in a native profile backup")
			}
			link, err = source.Readlink(name)
			if err != nil {
				return err
			}
		}
		if !info.Mode().IsRegular() && !info.IsDir() && link == "" {
			return errors.New("profile contains an unsupported special file")
		}
		h, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		h.Name = name
		if err = tw.WriteHeader(h); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		input, err := source.Open(name)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, contextReader{ctx, input})
		return errors.Join(copyErr, input.Close())
	})
	closeErr := finish()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	// Hard-link publication is atomic and fails if destination already exists.
	if err = os.Link(partial, destination); err != nil {
		return err
	}
	return os.Remove(partial)
}

// Restore accepts only this native backup format at its original absolute path.
// Existing profiles are never replaced. External source repositories are separate
// backup obligations because Git worktree metadata refers to their locations.
func Restore(ctx context.Context, archive, root string) error {
	if !filepath.IsAbs(root) {
		return errors.New("restore needs an absolute original profile path")
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(contextReader{ctx, gz})
	h, err := tr.Next()
	if err != nil {
		return err
	}
	if h.Name != backupHeader || h.Typeflag != tar.TypeReg || h.Size > 8192 {
		return errors.New("not a supported native backup")
	}
	var manifest backupManifest
	if err = json.NewDecoder(tr).Decode(&manifest); err != nil || manifest.Version != formatVersion || manifest.Root != root {
		return errors.New("restore requires the original profile path and a supported format")
	}
	if err = os.MkdirAll(filepath.Dir(root), 0700); err != nil {
		return err
	}
	if err = os.Mkdir(root, 0700); err != nil {
		return fmt.Errorf("restore refuses to replace existing data: %w", err)
	}
	target, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer target.Close()
	seen := map[string]bool{}
	var total int64
	for count := 0; ; count++ {
		h, err = tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if count >= 1000000 || !fs.ValidPath(h.Name) || h.Name == "." || h.Name == backupHeader || h.Name == "instance.lock" || seen[h.Name] {
			return errors.New("invalid or duplicate backup entry")
		}
		seen[h.Name] = true
		if h.Size < 0 || h.Size > (100<<30)-total {
			return errors.New("backup exceeds the 100 GiB restore limit")
		}
		total += h.Size
		parent := filepath.Dir(h.Name)
		if parent != "." {
			if err = target.MkdirAll(parent, 0700); err != nil {
				return err
			}
		}
		switch h.Typeflag {
		case tar.TypeDir:
			err = target.MkdirAll(h.Name, 0700)
		case tar.TypeSymlink:
			if !strings.HasPrefix(h.Name, "workspaces/") {
				return errors.New("backup symlinks are permitted only in workspaces")
			}
			err = target.Symlink(h.Linkname, h.Name)
		case tar.TypeReg:
			var out *os.File
			out, err = target.OpenFile(h.Name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fs.FileMode(h.Mode)&0777)
			if err == nil {
				_, copyErr := io.Copy(out, tr)
				err = errors.Join(copyErr, out.Close())
			}
		default:
			return errors.New("unsupported backup entry type")
		}
		if err != nil {
			return err
		}
	}
	// Consume the gzip footer to verify its checksum after tar EOF. Bound
	// trailing padding so a malformed archive cannot force unbounded reads.
	trailing, err := io.Copy(io.Discard, io.LimitReader(contextReader{ctx, gz}, 1<<20))
	if err != nil || trailing >= 1<<20 {
		return errors.New("backup integrity check failed; inspect the partial restore")
	}
	_, release, err := Open(root)
	if err != nil {
		return fmt.Errorf("restored profile requires inspection: %w", err)
	}
	release()
	return nil
}
