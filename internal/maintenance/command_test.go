package maintenance

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVersion(t *testing.T) {
	for _, s := range []string{"v1.2.3", "1.2", "1.2.3.4", "1.2.3\nx", "9999999999999999999999.0.0"} {
		if _, e := version(s); e == nil {
			t.Fatalf("accepted %q", s)
		}
	}
	if !newer("2.10.0", "2.9.9") || newer("1.0.0", "1.0.0") {
		t.Fatal("numeric version comparison")
	}
}
func TestUpdatesDisabledAndVersionChange(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "VERSION"), []byte("2.1.0"), 0600)
	state := filepath.Join(root, "state.json")
	atomicJSON(state, map[string]any{"installedVersion": "2.1.0", "nextCheckAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	t.Setenv("ZOTERO_USE_UPDATE_CHECK", "0")
	cmd := NewCommand()
	var b bytes.Buffer
	cmd.SetOut(&b)
	cmd.SetArgs([]string{"check-updates", "--root", root, "--state-file", state, "--force", "--json", "--remote-url", "http://invalid", "--strict"})
	if e := cmd.Execute(); e != nil {
		t.Fatal(e)
	}
	var result map[string]any
	json.Unmarshal(b.Bytes(), &result)
	if result["due"] != false || result["checked"] != false {
		t.Fatal(result)
	}
	t.Setenv("ZOTERO_USE_UPDATE_CHECK", "1")
	os.WriteFile(filepath.Join(root, "VERSION"), []byte("2.2.0"), 0600)
	cmd = NewCommand()
	b.Reset()
	cmd.SetOut(&b)
	cmd.SetArgs([]string{"check-updates", "--root", root, "--state-file", state, "--json", "--remote-url", "http://invalid", "--strict", "--no-write"})
	if e := cmd.Execute(); e == nil {
		t.Fatal("insecure URL accepted")
	}
	json.Unmarshal(b.Bytes(), &result)
	if result["due"] != true {
		t.Fatal("version change must trigger check")
	}
}
func TestInvalidStateStrict(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "VERSION"), []byte("1.0.0"), 0600)
	state := filepath.Join(root, "state")
	os.WriteFile(state, []byte("[]"), 0600)
	cmd := NewCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"check-updates", "--root", root, "--state-file", state, "--strict", "--no-write"})
	if e := cmd.Execute(); e == nil {
		t.Fatal("invalid state ignored")
	}
}
func TestManifestDetectsChangedFiles(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "scripts"), 0700)
	p := filepath.Join(root, "scripts", "x.py")
	os.WriteFile(p, []byte("one"), 0600)
	a, e := Manifest(root)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(p, []byte("two"), 0600)
	b, _ := Manifest(root)
	if a["scripts/x.py"] == b["scripts/x.py"] {
		t.Fatal("change missed")
	}
	errs := CheckPackage(root)
	if len(errs) == 0 || !strings.Contains(strings.Join(errs, "\n"), "missing required") {
		t.Fatal(errs)
	}
}
