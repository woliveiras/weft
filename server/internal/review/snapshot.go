// Package review owns durable human review and its application preview sessions.
package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Candidate copies regular files and internal relative symlinks without following
// them. A private copy, never the agent's mutable working directory, is executed.
// Git metadata is excluded: it may point outside a worktree and is not app input.
func Candidate(ctx context.Context, source, destination string) (string, error) {
	return walkCandidate(ctx, source, destination)
}
func Fingerprint(ctx context.Context, source string) (string, error) {
	return walkCandidate(ctx, source, "")
}
func walkCandidate(ctx context.Context, source, destination string) (string, error) {
	root, err := os.OpenRoot(source)
	if err != nil {
		return "", err
	}
	defer root.Close()
	h := sha256.New()
	var size int64
	count := 0
	type directoryMode struct {
		path string
		mode fs.FileMode
	}
	var directories []directoryMode
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		count++
		if count > 200000 {
			return fmt.Errorf("candidate exceeds 200000 entries")
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		target := filepath.Join(destination, filepath.FromSlash(p))
		fmt.Fprintf(h, "%d:%s:%o:", len(p), p, info.Mode().Perm())
		if d.IsDir() {
			if destination != "" {
				directories = append(directories, directoryMode{target, info.Mode().Perm()})
				return os.MkdirAll(target, 0755)
			}
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := root.Readlink(p)
			if err != nil {
				return err
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(p), link))
			if filepath.IsAbs(link) || resolved == ".." || strings.HasPrefix(resolved, ".."+string(filepath.Separator)) {
				return fmt.Errorf("candidate symlink escapes workspace: %s", p)
			}
			fmt.Fprintf(h, "link:%d:%s", len(link), link)
			if destination != "" {
				return os.Symlink(link, target)
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported candidate file: %s", p)
		}
		size += info.Size()
		if size > 2<<30 {
			return fmt.Errorf("candidate exceeds 2 GiB")
		}
		f, err := root.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		fmt.Fprintf(h, "file:%d:", info.Size())
		var out *os.File
		var w io.Writer = h
		if destination != "" {
			out, err = os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
			if err != nil {
				return err
			}
			defer out.Close()
			w = io.MultiWriter(h, out)
		}
		n, err := io.Copy(w, io.LimitReader(f, info.Size()+1))
		if err != nil {
			return err
		}
		after, err := f.Stat()
		if err != nil {
			return err
		}
		if n != info.Size() || !after.ModTime().Equal(info.ModTime()) {
			return fmt.Errorf("candidate changed during capture: %s", p)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	for i := len(directories) - 1; i >= 0; i-- {
		if err := os.Chmod(directories[i].path, directories[i].mode); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
