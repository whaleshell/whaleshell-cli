package gwconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("current: old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	in := File{Current: "dev", Gateways: map[string]Gateway{"dev": {URL: "http://127.0.0.1:7443", Token: "secret", RefreshToken: "r"}}}
	if err := Save(in); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("config with tokens has mode %o, want 600", st.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}

	out, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if out.Current != "dev" || out.Gateways["dev"].Token != "secret" {
		t.Fatalf("round-trip: %+v", out)
	}
}
