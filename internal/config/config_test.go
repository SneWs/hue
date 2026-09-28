package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWritePrivateFileIgnoresPredictableTempSymlink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	planted := filepath.Join(dir, "bridge.json.tmp")
	if err := os.Symlink(victim, planted); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "bridge.json")
	if err := writePrivateFile(dest, []byte("secret\n")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep" {
		t.Fatalf("symlink target was overwritten: %q", got)
	}
	saved, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != "secret\n" {
		t.Fatalf("saved %q", saved)
	}
	info, err := os.Lstat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
}
