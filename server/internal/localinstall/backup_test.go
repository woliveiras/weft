package localinstall

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOfflineBackupRoundTripAndNoOverwrite(t *testing.T) {
	root, web := fixture(t)
	if err := Init(root, web, 8787); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspaces", "one")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "result"), []byte("review me"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("result", filepath.Join(workspace, "link")); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(filepath.Dir(root), "backup.tar.gz")
	_, release, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = Backup(t.Context(), root, archive); err == nil {
		t.Fatal("backed up active profile")
	}
	release()
	if err = Backup(t.Context(), root, filepath.Join(root, "inside.tar.gz")); err == nil {
		t.Fatal("recursive archive")
	}
	if err = Backup(t.Context(), root, archive); err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(archive)
	if err != nil || stat.Mode().Perm() != 0600 {
		t.Fatal("archive privacy", err)
	}
	if err = Restore(t.Context(), archive, root); err == nil {
		t.Fatal("overwrote profile")
	}
	if err = Restore(t.Context(), archive, root+"-different"); err == nil {
		t.Fatal("relocated worktree metadata")
	}
	if err = os.Rename(root, root+"-retained"); err != nil {
		t.Fatal(err)
	}
	if err = Restore(t.Context(), archive, root); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(workspace, "link")); err != nil || string(b) != "review me" {
		t.Fatal("lost workspace", err)
	}
	// Check only byte equality of synthetic fixture credentials; never print them.
	old, e1 := os.ReadFile(filepath.Join(root+"-retained", "admin.token"))
	restored, e2 := os.ReadFile(filepath.Join(root, "admin.token"))
	if e1 != nil || e2 != nil || string(old) != string(restored) {
		t.Fatal("fixture credential was not preserved")
	}
	if err = Backup(t.Context(), root, archive); err == nil {
		t.Fatal("overwrote archive")
	}
}
func TestBackupCancellationAndRestoreTraversal(t *testing.T) {
	root, web := fixture(t)
	if err := Init(root, web, 8787); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	dest := filepath.Join(filepath.Dir(root), "cancelled.tar.gz")
	if err := Backup(ctx, root, dest); err == nil {
		t.Fatal("cancel ignored")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("partial published")
	}
	archive := filepath.Join(filepath.Dir(root), "invalid.tar.gz")
	target := root + "-restore"
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	meta, _ := json.Marshal(backupManifest{Version: formatVersion, Root: target})
	if err = tw.WriteHeader(&tar.Header{Name: backupHeader, Typeflag: tar.TypeReg, Size: int64(len(meta))}); err != nil {
		t.Fatal(err)
	}
	if _, err = tw.Write(meta); err != nil {
		t.Fatal(err)
	}
	if err = tw.WriteHeader(&tar.Header{Name: "../escape", Typeflag: tar.TypeReg, Size: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = tw.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err = tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if err = Restore(t.Context(), archive, target); err == nil {
		t.Fatal("traversal accepted")
	}
	if _, err = os.Stat(filepath.Join(filepath.Dir(root), "escape")); !os.IsNotExist(err) {
		t.Fatal("escaped restore root")
	}
}
func TestNativeCredentialSymlinkIsRejected(t *testing.T) {
	root, web := fixture(t)
	if err := Init(root, web, 8787); err != nil {
		t.Fatal(err)
	}
	token := filepath.Join(root, "admin.token")
	if err := os.Rename(token, token+"-retained"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(token+"-retained", token); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(root); err == nil {
		t.Fatal("credential symlink accepted")
	}
}

func TestBackupRejectsDestinationAliasAndCorruptFooter(t *testing.T) {
	root, web := fixture(t)
	if err := Init(root, web, 8787); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(filepath.Dir(root), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	if err := Backup(t.Context(), root, filepath.Join(alias, "recursive.tar.gz")); err == nil {
		t.Fatal("accepted destination inside profile through alias")
	}
	archive := filepath.Join(filepath.Dir(root), "valid.tar.gz")
	if err := Backup(t.Context(), root, archive); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)-8] ^= 0xff
	corrupt := archive + "-corrupt"
	if err := os.WriteFile(corrupt, b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, root+"-retained"); err != nil {
		t.Fatal(err)
	}
	if err := Restore(t.Context(), corrupt, root); err == nil {
		t.Fatal("corrupt gzip footer accepted")
	}
}
