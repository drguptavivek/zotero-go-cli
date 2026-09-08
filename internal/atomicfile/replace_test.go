package atomicfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReplaceExistingDestination(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	destination := filepath.Join(dir, "destination")
	if err := os.WriteFile(source, []byte("new contents"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("old contents"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := Replace(source, destination); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new contents" {
		t.Fatalf("destination = %q, want new contents", got)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source still exists or stat failed: %v", err)
	}
}

func TestReplacePreservesSourcePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not provide Unix permission semantics")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	destination := filepath.Join(dir, "destination")
	if err := os.WriteFile(source, []byte("new contents"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("old contents"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := Replace(source, destination); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0640 {
		t.Fatalf("destination mode = %04o, want 0640", got)
	}
}
