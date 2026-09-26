package sshconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderHostBlockMatchesOpenShellShape(t *testing.T) {
	pc := ProxyCommand("/usr/local/bin/whaleshell", "--gateway-name", "local", "--name", "demo")
	got := RenderHostBlock(Alias("demo"), pc)
	want := `Host whaleshell-demo
    User sandbox
    StrictHostKeyChecking no
    UserKnownHostsFile /dev/null
    GlobalKnownHostsFile /dev/null
    LogLevel ERROR
    ServerAliveInterval 15
    ServerAliveCountMax 3
    ForwardAgent no
    ForwardX11 no
    ProxyCommand /usr/local/bin/whaleshell ssh-proxy --gateway-name local --name demo
`
	if got != want {
		t.Fatalf("block mismatch:\n%s\nwant:\n%s", got, want)
	}
}

func TestQuote(t *testing.T) {
	for in, want := range map[string]string{
		"/usr/bin/whaleshell":             "/usr/bin/whaleshell",
		"/Applications/My App/whaleshell": `"/Applications/My App/whaleshell"`,
		`C:\Program Files\whaleshell.exe`: `"C:\\Program Files\\whaleshell.exe"`,
		"https://gw.example:7443":         "https://gw.example:7443",
		"a;rm -rf /":                      `"a;rm -rf /"`,
		"":                                `""`,
	} {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestUpsertHostBlock(t *testing.T) {
	b1 := RenderHostBlock("whaleshell-a", "x ssh-proxy --name a")
	b2 := RenderHostBlock("whaleshell-b", "x ssh-proxy --name b")
	s := UpsertHostBlock("", "whaleshell-a", b1)
	s = UpsertHostBlock(s, "whaleshell-b", b2)
	if strings.Count(s, "Host whaleshell-") != 2 {
		t.Fatalf("want 2 hosts:\n%s", s)
	}
	b1new := RenderHostBlock("whaleshell-a", "y ssh-proxy --name a")
	s2 := UpsertHostBlock(s, "whaleshell-a", b1new)
	if strings.Contains(s2, "x ssh-proxy --name a") || !strings.Contains(s2, "y ssh-proxy --name a") {
		t.Fatalf("block not replaced:\n%s", s2)
	}
	if !strings.Contains(s2, "x ssh-proxy --name b") {
		t.Fatalf("other block damaged:\n%s", s2)
	}
	if UpsertHostBlock(s2, "whaleshell-a", b1new) != s2 {
		t.Fatal("upsert is not idempotent")
	}
	// A host whose alias only shares a prefix must not be touched.
	s3 := UpsertHostBlock("Host whaleshell-ab\n    User x\n", "whaleshell-a", b1)
	if !strings.Contains(s3, "Host whaleshell-ab\n    User x") || !strings.Contains(s3, "Host whaleshell-a\n") {
		t.Fatalf("prefix host mishandled:\n%s", s3)
	}
}

func TestRemoveHostBlock(t *testing.T) {
	s := UpsertHostBlock("", "whaleshell-a", RenderHostBlock("whaleshell-a", "p"))
	s = UpsertHostBlock(s, "whaleshell-b", RenderHostBlock("whaleshell-b", "p"))
	s = RemoveHostBlock(s, "whaleshell-a")
	if strings.Contains(s, "whaleshell-a") || !strings.Contains(s, "Host whaleshell-b") {
		t.Fatalf("remove:\n%s", s)
	}
}

func TestEnsureIncludeBeforeFirstHost(t *testing.T) {
	user := "# personal\nServerAliveInterval 30\n\nHost github.com\n    User git\n"
	got := EnsureInclude(user, "/home/u/.config/whaleshell/ssh_config")
	inc := strings.Index(got, "Include /home/u/.config/whaleshell/ssh_config")
	host := strings.Index(got, "Host github.com")
	if inc < 0 || host < 0 || inc > host {
		t.Fatalf("include must precede first Host:\n%s", got)
	}
	if EnsureInclude(got, "/home/u/.config/whaleshell/ssh_config") != got {
		t.Fatal("EnsureInclude is not idempotent")
	}
	if got := EnsureInclude("", "/p"); got != "Include /p\n" {
		t.Fatalf("empty config = %q", got)
	}
	if got := EnsureInclude("Match host x\n  User y\n", "/p"); !strings.HasPrefix(got, "Include /p\n") {
		t.Fatalf("Match not treated as block start: %q", got)
	}
	quoted := `Include "/My Dir/ssh_config"` + "\n"
	if EnsureInclude(quoted, "/My Dir/ssh_config") != quoted {
		t.Fatal("quoted include not detected")
	}
}

func TestInstallWritesPrivateFiles(t *testing.T) {
	dir := t.TempDir()
	p := Paths{Managed: filepath.Join(dir, "cfg", "whaleshell", "ssh_config"), User: filepath.Join(dir, "ssh", "config")}
	if err := os.MkdirAll(filepath.Dir(p.User), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.User, []byte("Host box\n    User me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	block := RenderHostBlock("whaleshell-demo", "w ssh-proxy --name demo")
	if err := Install(p, "whaleshell-demo", block); err != nil {
		t.Fatal(err)
	}
	if err := Install(p, "whaleshell-demo", block); err != nil {
		t.Fatal(err)
	}
	m, _ := os.ReadFile(p.Managed)
	if strings.Count(string(m), "Host whaleshell-demo") != 1 {
		t.Fatalf("managed:\n%s", m)
	}
	u, _ := os.ReadFile(p.User)
	if strings.Count(string(u), "Include") != 1 || !strings.HasPrefix(string(u), "Include ") {
		t.Fatalf("user config:\n%s", u)
	}
	fi, _ := os.Stat(p.Managed)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("managed mode %o", fi.Mode().Perm())
	}
	if err := Uninstall(p, "whaleshell-demo"); err != nil {
		t.Fatal(err)
	}
	m, _ = os.ReadFile(p.Managed)
	if strings.Contains(string(m), "whaleshell-demo") {
		t.Fatalf("uninstall left block:\n%s", m)
	}
}
